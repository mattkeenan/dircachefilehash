package dircachefilehash

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// ============================================================================
// TYPE DEFINITIONS
// ============================================================================

// ScannedPath represents a file found during filesystem scanning
type ScannedPath struct {
	AbsPath  string
	RelPath  string
	Info     os.FileInfo
	StatInfo *syscall.Stat_t
}

// HwangLinResult represents the result of Hwang-Lin comparison
type HwangLinResult struct {
	Type        HwangLinType
	ScannedPath *ScannedPath // nil for deletions
	IndexEntry  *binaryEntry // nil for new files
	JobID       uint64       // for tracking hash jobs
	Hash        []byte       // computed hash (for new/modified files)
	HashType    uint16       // hash algorithm type
}

// HwangLinType represents the type of change detected
type HwangLinType int

const (
	HLUnchanged HwangLinType = iota // File exists in both and is unchanged
	HLNew                           // File only exists in scan (new file)
	HLModified                      // File exists in both but is modified
	HLDeleted                       // File only exists in index (deleted file)
)

// ProcessedEntry represents a processed file ready for index writing
type ProcessedEntry struct {
	RelPath   string
	Hash      []byte
	HashType  uint16
	FileInfo  os.FileInfo
	StatInfo  *syscall.Stat_t
	IsDeleted bool
}

// HashJobResult stores hash results for new files
type HashJobResult struct {
	Hash     []byte
	HashType uint16
}

// HashJobStart represents a hash job being started
type HashJobStart struct {
	JobID       uint64
	FilePath    string
	IndexEntry  *binaryEntry // Entry to update with hash
	ScannedPath *ScannedPath
}

// HashJobCompletion represents a hash job completion (just the ID)
type HashJobCompletion struct {
	JobID uint64
	Error error // Only set if there was an error
}

// HashJobMonitor manages asynchronous hash job tracking
type HashJobMonitor struct {
	startedJobs   map[uint64]bool
	completedJobs map[uint64]bool
	pendingCount  int
	mutex         sync.RWMutex
	allDoneChan   chan struct{}
	closed        bool
}

// StreamingHashJobManager coordinates streaming hash job processing
type StreamingHashJobManager struct {
	hashJobChan     chan *HashJobStart
	completionChan  chan *HashJobCompletion
	startNotifyChan chan uint64
	monitor         *HashJobMonitor
	hashResultStore map[uint64]HashJobResult // Store results for new files
	storeMutex      sync.RWMutex
	ctx             context.Context
	cancel          context.CancelFunc
}

// ============================================================================
// FILESYSTEM SCANNING FUNCTIONS
// ============================================================================

// scanPath scans filesystem paths in sorted order and sends them via channel as they're found
func (dc *DirectoryCache) scanPath(ctx context.Context, paths []string, resultChan chan<- *ScannedPath) error {
	defer close(resultChan)

	// If empty paths, scan entire root directory
	if len(paths) == 0 {
		paths = []string{dc.RootDir}
	}

	// Load ignore patterns if not already loaded
	if err := dc.ignoreManager.LoadIgnorePatterns(); err != nil {
		return fmt.Errorf("failed to load ignore patterns: %w", err)
	}

	// Convert to absolute paths and clean them
	var absPaths []string
	for _, inputPath := range paths {
		absPath := inputPath
		if !filepath.IsAbs(inputPath) {
			absPath = filepath.Join(dc.RootDir, inputPath)
		}
		absPaths = append(absPaths, filepath.Clean(absPath))
	}

	// Sort paths and remove redundant ones (subdirectories/subfiles of other paths)
	dedupedPaths := dc.deduplicatePaths(absPaths)

	// Scan each deduplicated path in sorted order, streaming results as found
	for _, absPath := range dedupedPaths {
		if err := dc.scanPathRecursive(ctx, absPath, resultChan); err != nil {
			return fmt.Errorf("failed to scan path %s: %w", absPath, err)
		}
	}

	return nil
}

// deduplicatePaths sorts paths and removes any that are subdirectories/subfiles of others
// Example: ["/home/user/docs", "/home/user/docs/file.txt", "/home/user/photos"]
//
//	-> ["/home/user/docs", "/home/user/photos"]
//
// This optimization reduces redundant scanning since "/home/user/docs/file.txt"
// will be found when we scan "/home/user/docs" anyway.
func (dc *DirectoryCache) deduplicatePaths(paths []string) []string {
	if len(paths) <= 1 {
		return paths
	}

	// Sort paths - this ensures parent directories come before their children
	sort.Strings(paths)

	var deduplicated []string
	for i, path := range paths {
		isRedundant := false

		// Check if this path is a subdirectory/subfile of any previous path
		for j := 0; j < i; j++ {
			prevPath := paths[j]

			// Check if current path is under the previous path
			if dc.isPathUnder(path, prevPath) {
				isRedundant = true
				break
			}
		}

		if !isRedundant {
			deduplicated = append(deduplicated, path)
		}
	}

	return deduplicated
}

// isPathUnder checks if childPath is under parentPath
func (dc *DirectoryCache) isPathUnder(childPath, parentPath string) bool {
	// Make sure both paths are clean
	childPath = filepath.Clean(childPath)
	parentPath = filepath.Clean(parentPath)

	// If paths are identical, child is not "under" parent
	if childPath == parentPath {
		return false
	}

	// Check if childPath starts with parentPath + separator
	parentWithSep := parentPath + string(filepath.Separator)
	return strings.HasPrefix(childPath, parentWithSep)
}

// scanPathRecursive recursively scans a path and streams results as they're found
// This provides significant performance benefits:
// 1. No memory buildup - results are streamed immediately
// 2. Hwang-Lin comparison can start before scanning is complete
// 3. Maintains sorted order by processing paths alphabetically
func (dc *DirectoryCache) scanPathRecursive(ctx context.Context, rootPath string, resultChan chan<- *ScannedPath) error {
	// Use a priority queue (sorted slice) to ensure we process paths in alphabetical order
	// This ensures the output is naturally sorted
	pathQueue := []string{rootPath}

	for len(pathQueue) > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Always process the first path (lexicographically smallest)
		currentPath := pathQueue[0]
		pathQueue = pathQueue[1:]

		info, err := os.Lstat(currentPath)
		if err != nil {
			continue // Skip inaccessible paths
		}

		// Get relative path for ignore checking
		relPath, err := filepath.Rel(dc.RootDir, currentPath)
		if err != nil {
			continue
		}

		// Check if path should be ignored
		if dc.ignoreManager.ShouldIgnore(relPath) {
			continue
		}

		if info.IsDir() {
			// Skip the .dcfh directory
			indexDir := filepath.Dir(dc.IndexFile)
			if currentPath == indexDir {
				continue
			}

			// Read directory entries and add to queue in sorted order
			entries, err := os.ReadDir(currentPath)
			if err != nil {
				continue
			}

			// Sort entries for consistent ordering
			sort.Slice(entries, func(i, j int) bool {
				return entries[i].Name() < entries[j].Name()
			})

			// Add directory entries to queue, inserting in sorted position
			var newPaths []string
			for _, entry := range entries {
				fullPath := filepath.Join(currentPath, entry.Name())
				newPaths = append(newPaths, fullPath)
			}

			// Insert new paths into queue maintaining sorted order
			pathQueue = dc.insertSorted(pathQueue, newPaths)

		} else if info.Mode().IsRegular() {
			// Skip index files
			if currentPath == dc.IndexFile || currentPath == dc.CacheFile {
				continue
			}

			// Get system-specific file information
			stat := info.Sys().(*syscall.Stat_t)

			scannedPath := &ScannedPath{
				AbsPath:  currentPath,
				RelPath:  relPath,
				Info:     info,
				StatInfo: stat,
			}

			// Stream result immediately - this gives us better performance
			select {
			case resultChan <- scannedPath:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	return nil
}

// insertSorted inserts new paths into an existing sorted slice maintaining order
func (dc *DirectoryCache) insertSorted(existing []string, newPaths []string) []string {
	if len(newPaths) == 0 {
		return existing
	}
	if len(existing) == 0 {
		// Just sort and return new paths
		sort.Strings(newPaths)
		return newPaths
	}

	// Merge the two sorted slices
	result := make([]string, 0, len(existing)+len(newPaths))

	// Sort new paths first
	sort.Strings(newPaths)

	i, j := 0, 0
	for i < len(existing) && j < len(newPaths) {
		if existing[i] <= newPaths[j] {
			result = append(result, existing[i])
			i++
		} else {
			result = append(result, newPaths[j])
			j++
		}
	}

	// Append remaining elements
	for i < len(existing) {
		result = append(result, existing[i])
		i++
	}
	for j < len(newPaths) {
		result = append(result, newPaths[j])
		j++
	}

	return result
}

// ============================================================================
// HWANG-LIN COMPARISON ALGORITHM
// ============================================================================

// hwangLinCompare performs Hwang-Lin algorithm comparison between scanned filesystem and skiplist
// Now with asynchronous hash job processing - hash jobs don't block the comparison
func (dc *DirectoryCache) hwangLinCompare(
	ctx context.Context,
	scanChan <-chan *ScannedPath,
	skiplist *SkiplistWrapper,
	resultChan chan<- *HwangLinResult,
	hashJobManager *StreamingHashJobManager,
) error {
	defer close(resultChan)

	var currentScanned *ScannedPath
	var scanChanOpen bool = true
	currentIndex := skiplist.skiplist.First()
	jobIDCounter := uint64(1)

	// Read first scanned path
	if scanChanOpen {
		select {
		case currentScanned, scanChanOpen = <-scanChan:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	for scanChanOpen || currentIndex != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var cmp int
		if !scanChanOpen {
			cmp = 1 // No more scanned files, only index entries remain (deletions)
		} else if currentIndex == nil {
			cmp = -1 // No more index entries, only scanned files remain (new files)
		} else {
			// Compare paths
			indexPath := currentIndex.Item().RelativePath()
			cmp = strings.Compare(currentScanned.RelPath, indexPath)
		}

		if cmp == 0 {
			// File exists in both - check if changed
			indexEntry := currentIndex.Item()

			// Skip deleted entries in the index
			if indexEntry.IsDeleted() {
				currentIndex = currentIndex.Next()
				continue
			}

			if dc.isFileChangedFromScanned(indexEntry, currentScanned) {
				// File modified - submit for async hashing (don't wait!)
				jobID := jobIDCounter
				jobIDCounter++

				hashJob := &HashJobStart{
					JobID:       jobID,
					FilePath:    currentScanned.AbsPath,
					IndexEntry:  indexEntry, // Hash worker will update this directly
					ScannedPath: currentScanned,
				}

				// Submit hash job asynchronously - this doesn't block!
				hashJobManager.SubmitHashJob(hashJob)

				result := &HwangLinResult{
					Type:        HLModified,
					ScannedPath: currentScanned,
					IndexEntry:  indexEntry,
					JobID:       jobID,
					// Hash and HashType will be updated by async worker
				}

				select {
				case resultChan <- result:
				case <-ctx.Done():
					return ctx.Err()
				}
			} else {
				// File unchanged
				result := &HwangLinResult{
					Type:        HLUnchanged,
					ScannedPath: currentScanned,
					IndexEntry:  indexEntry,
					JobID:       0,
					// No hash needed for unchanged files
				}

				select {
				case resultChan <- result:
				case <-ctx.Done():
					return ctx.Err()
				}
			}

			// Advance both
			if scanChanOpen {
				select {
				case currentScanned, scanChanOpen = <-scanChan:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			currentIndex = currentIndex.Next()

		} else if cmp < 0 {
			// File only in scan - new file, needs async hashing
			jobID := jobIDCounter
			jobIDCounter++

			// For new files, we need to handle hash results differently since there's no existing entry
			hashJob := &HashJobStart{
				JobID:       jobID,
				FilePath:    currentScanned.AbsPath,
				IndexEntry:  nil, // New file, no existing entry
				ScannedPath: currentScanned,
			}

			// Submit hash job asynchronously
			hashJobManager.SubmitHashJob(hashJob)

			result := &HwangLinResult{
				Type:        HLNew,
				ScannedPath: currentScanned,
				IndexEntry:  nil,
				JobID:       jobID,
				// Hash will be available from hash job completion tracking
			}

			select {
			case resultChan <- result:
			case <-ctx.Done():
				return ctx.Err()
			}

			// Advance scan
			if scanChanOpen {
				select {
				case currentScanned, scanChanOpen = <-scanChan:
				case <-ctx.Done():
					return ctx.Err()
				}
			}

		} else {
			// File only in index - deleted file
			indexEntry := currentIndex.Item()

			// Skip already deleted entries
			if !indexEntry.IsDeleted() {
				result := &HwangLinResult{
					Type:        HLDeleted,
					ScannedPath: nil,
					IndexEntry:  indexEntry,
					JobID:       0,
					// No hash needed for deleted files
				}

				select {
				case resultChan <- result:
				case <-ctx.Done():
					return ctx.Err()
				}
			}

			// Advance index
			currentIndex = currentIndex.Next()
		}
	}

	return nil
}

// isFileChangedFromScanned checks if a file has changed by comparing with scanned info
func (dc *DirectoryCache) isFileChangedFromScanned(indexEntry *binaryEntry, scanned *ScannedPath) bool {
	stat := scanned.StatInfo

	// Quick size check
	if indexEntry.FileSize != uint64(scanned.Info.Size()) {
		return true
	}

	// Check ownership
	if indexEntry.UID != stat.Uid || indexEntry.GID != stat.Gid {
		return true
	}

	// Check mode
	if indexEntry.Mode != uint32(scanned.Info.Mode()) {
		return true
	}

	// Check timestamps using wall time encoding
	currentCTime := encodeWallTime(stat.Ctim.Sec, stat.Ctim.Nsec)
	currentMTime := encodeWallTime(stat.Mtim.Sec, stat.Mtim.Nsec)

	return indexEntry.CTimeWall != currentCTime || indexEntry.MTimeWall != currentMTime
}

// ============================================================================
// HASH JOB MANAGEMENT
// ============================================================================

// NewHashJobMonitor creates a new hash job monitor
func NewHashJobMonitor() *HashJobMonitor {
	return &HashJobMonitor{
		startedJobs:   make(map[uint64]bool),
		completedJobs: make(map[uint64]bool),
		pendingCount:  0,
		allDoneChan:   make(chan struct{}),
		closed:        false,
	}
}

// StartJob registers a new hash job as started
func (hjm *HashJobMonitor) StartJob(jobID uint64) {
	hjm.mutex.Lock()
	defer hjm.mutex.Unlock()

	if !hjm.closed {
		hjm.startedJobs[jobID] = true
		hjm.pendingCount++
	}
}

// CompleteJob marks a hash job as completed
func (hjm *HashJobMonitor) CompleteJob(jobID uint64, err error) {
	hjm.mutex.Lock()
	defer hjm.mutex.Unlock()

	if hjm.closed {
		return
	}

	// Only process if we know about this job
	if hjm.startedJobs[jobID] && !hjm.completedJobs[jobID] {
		hjm.completedJobs[jobID] = true
		hjm.pendingCount--

		if err != nil {
			// Log error but don't fail the whole process
			fmt.Printf("Hash job %d failed: %v\n", jobID, err)
		}

		// Check if all jobs are done
		if hjm.pendingCount == 0 {
			hjm.closed = true
			close(hjm.allDoneChan)
		}
	}
}

// FinishScanning signals that no more jobs will be started
func (hjm *HashJobMonitor) FinishScanning() {
	hjm.mutex.Lock()
	defer hjm.mutex.Unlock()

	// If no jobs were started, signal completion immediately
	if hjm.pendingCount == 0 && !hjm.closed {
		hjm.closed = true
		close(hjm.allDoneChan)
	}
}

// AllDone returns a channel that will be closed when all hash jobs are complete
func (hjm *HashJobMonitor) AllDone() <-chan struct{} {
	return hjm.allDoneChan
}

// GetStats returns current job statistics
func (hjm *HashJobMonitor) GetStats() (started, completed, pending int) {
	hjm.mutex.RLock()
	defer hjm.mutex.RUnlock()
	return len(hjm.startedJobs), len(hjm.completedJobs), hjm.pendingCount
}

// AsyncHashWorker processes hash jobs and updates binaryEntry directly or stores hash for new files
func (dc *DirectoryCache) AsyncHashWorker(
	ctx context.Context,
	hashJobChan <-chan *HashJobStart,
	completionChan chan<- *HashJobCompletion,
	hashResultStore map[uint64]HashJobResult, // Store results for new files
	storeMutex *sync.RWMutex,
) {
	for {
		select {
		case job, ok := <-hashJobChan:
			if !ok {
				return
			}

			// Hash the file
			hashStr, err := dc.hashFile(job.FilePath)

			if err == nil {
				// Convert hash string to bytes
				hashBytes, hexErr := hex.DecodeString(hashStr)
				if hexErr == nil {
					if job.IndexEntry != nil {
						// Update existing entry directly
						dc.updateBinaryEntryHash(job.IndexEntry, hashBytes, HashTypeSHA1)
					} else {
						// Store hash result for new files
						storeMutex.Lock()
						hashResultStore[job.JobID] = HashJobResult{
							Hash:     hashBytes,
							HashType: HashTypeSHA1,
						}
						storeMutex.Unlock()
					}
				} else {
					err = hexErr
				}
			}

			// Send completion notification
			completion := &HashJobCompletion{
				JobID: job.JobID,
				Error: err,
			}

			select {
			case completionChan <- completion:
			case <-ctx.Done():
				return
			}

		case <-ctx.Done():
			return
		}
	}
}

// updateBinaryEntryHash safely updates the hash in a binaryEntry
func (dc *DirectoryCache) updateBinaryEntryHash(entry *binaryEntry, hash []byte, hashType uint16) {
	// Clear the hash field first
	for i := range entry.Hash {
		entry.Hash[i] = 0
	}

	// Copy the new hash
	copy(entry.Hash[:], hash)
	entry.HashType = hashType
}

// AsyncHashWorkerPool manages a pool of async hash workers
func (dc *DirectoryCache) AsyncHashWorkerPool(
	ctx context.Context,
	hashJobChan <-chan *HashJobStart,
	completionChan chan<- *HashJobCompletion,
	hashResultStore map[uint64]HashJobResult,
	storeMutex *sync.RWMutex,
	numWorkers int,
) {
	var wg sync.WaitGroup

	// Start hash workers
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dc.AsyncHashWorker(ctx, hashJobChan, completionChan, hashResultStore, storeMutex)
		}()
	}

	// Wait for all workers to finish and close completion channel
	go func() {
		wg.Wait()
		close(completionChan)
	}()
}

// HashJobMonitorProcess runs the hash job monitoring process
func (hjm *HashJobMonitor) HashJobMonitorProcess(
	ctx context.Context,
	startChan <-chan uint64,
	completionChan <-chan *HashJobCompletion,
) {
	for {
		select {
		case jobID, ok := <-startChan:
			if !ok {
				// No more jobs will be started
				hjm.FinishScanning()
				goto waitForCompletions
			}
			hjm.StartJob(jobID)

		case completion, ok := <-completionChan:
			if !ok {
				// Workers are done, but we might still have pending jobs
				goto waitForCompletions
			}
			hjm.CompleteJob(completion.JobID, completion.Error)

		case <-ctx.Done():
			return
		}
	}

waitForCompletions:
	// Continue processing completions until all jobs are done
	for {
		select {
		case completion, ok := <-completionChan:
			if !ok {
				// Workers are done and channel closed
				hjm.FinishScanning()
				return
			}
			hjm.CompleteJob(completion.JobID, completion.Error)

		case <-hjm.AllDone():
			// All jobs completed
			return

		case <-ctx.Done():
			return
		}
	}
}

// NewStreamingHashJobManager creates a new streaming hash job manager
func (dc *DirectoryCache) NewStreamingHashJobManager(numWorkers int) *StreamingHashJobManager {
	ctx, cancel := context.WithCancel(context.Background())

	manager := &StreamingHashJobManager{
		hashJobChan:     make(chan *HashJobStart, 100),
		completionChan:  make(chan *HashJobCompletion, 100),
		startNotifyChan: make(chan uint64, 100),
		monitor:         NewHashJobMonitor(),
		hashResultStore: make(map[uint64]HashJobResult),
		ctx:             ctx,
		cancel:          cancel,
	}

	// Start the worker pool
	dc.AsyncHashWorkerPool(ctx, manager.hashJobChan, manager.completionChan, manager.hashResultStore, &manager.storeMutex, numWorkers)

	// Start the monitor process
	go manager.monitor.HashJobMonitorProcess(ctx, manager.startNotifyChan, manager.completionChan)

	return manager
}

// SubmitHashJob submits a hash job for async processing
func (hjm *StreamingHashJobManager) SubmitHashJob(job *HashJobStart) {
	select {
	case hjm.hashJobChan <- job:
		// Notify monitor that job was started
		select {
		case hjm.startNotifyChan <- job.JobID:
		case <-hjm.ctx.Done():
		}
	case <-hjm.ctx.Done():
	}
}

// FinishSubmitting signals that no more hash jobs will be submitted
func (hjm *StreamingHashJobManager) FinishSubmitting() {
	close(hjm.hashJobChan)
	close(hjm.startNotifyChan)
}

// WaitForCompletion waits for all submitted hash jobs to complete
func (hjm *StreamingHashJobManager) WaitForCompletion() {
	<-hjm.monitor.AllDone()
}

// Shutdown gracefully shuts down the hash job manager
func (hjm *StreamingHashJobManager) Shutdown() {
	hjm.cancel()
}

// GetHashResult retrieves hash result for a job ID (for new files)
func (hjm *StreamingHashJobManager) GetHashResult(jobID uint64) (HashJobResult, bool) {
	hjm.storeMutex.RLock()
	defer hjm.storeMutex.RUnlock()
	result, exists := hjm.hashResultStore[jobID]
	return result, exists
}

// GetStats returns current statistics
func (hjm *StreamingHashJobManager) GetStats() (started, completed, pending int) {
	return hjm.monitor.GetStats()
}

// ============================================================================
// RESULT PROCESSING FUNCTIONS
// ============================================================================

// ProcessHwangLinResults converts HwangLinResult array to entries for index writing
func (dc *DirectoryCache) ProcessHwangLinResults(results []*HwangLinResult) ([]ProcessedEntry, error) {
	var processedEntries []ProcessedEntry

	for _, result := range results {
		switch result.Type {
		case HLNew:
			entry := ProcessedEntry{
				RelPath:   result.ScannedPath.RelPath,
				Hash:      result.Hash,
				HashType:  result.HashType,
				FileInfo:  result.ScannedPath.Info,
				StatInfo:  result.ScannedPath.StatInfo,
				IsDeleted: false,
			}
			processedEntries = append(processedEntries, entry)

		case HLModified:
			// For modified files, extract info from existing entry + new hash
			entry := ProcessedEntry{
				RelPath:   result.IndexEntry.RelativePath(),
				Hash:      result.IndexEntry.Hash[:dc.getHashSize(result.IndexEntry.HashType)],
				HashType:  result.IndexEntry.HashType,
				FileInfo:  result.ScannedPath.Info,
				StatInfo:  result.ScannedPath.StatInfo,
				IsDeleted: false,
			}
			processedEntries = append(processedEntries, entry)

		case HLUnchanged:
			// Convert existing entry to processed format
			entry := ProcessedEntry{
				RelPath:  result.IndexEntry.RelativePath(),
				Hash:     result.IndexEntry.Hash[:dc.getHashSize(result.IndexEntry.HashType)],
				HashType: result.IndexEntry.HashType,
				// Note: FileInfo reconstruction needed or store differently
				IsDeleted: false,
			}
			processedEntries = append(processedEntries, entry)

		case HLDeleted:
			entry := ProcessedEntry{
				RelPath:   result.IndexEntry.RelativePath(),
				Hash:      result.IndexEntry.Hash[:dc.getHashSize(result.IndexEntry.HashType)],
				HashType:  result.IndexEntry.HashType,
				IsDeleted: true,
			}
			processedEntries = append(processedEntries, entry)
		}
	}

	return processedEntries, nil
}

// getHashSize returns hash size based on type
func (dc *DirectoryCache) getHashSize(hashType uint16) int {
	switch hashType {
	case HashTypeSHA1:
		return HashSizeSHA1
	case HashTypeSHA256:
		return HashSizeSHA256
	case HashTypeSHA512:
		return HashSizeSHA512
	default:
		return HashSizeSHA1
	}
}

// WriteProcessedEntries writes processed entries using pure file I/O
func (dc *DirectoryCache) WriteProcessedEntries(entries []ProcessedEntry, flags uint32) error {
	// Calculate total file size needed
	totalSize := HeaderSize + ChecksumSize
	for _, entry := range entries {
		totalSize += BESizeFromPathLen(len(entry.RelPath))
	}

	// Create and mmap file
	file, err := os.Create(dc.IndexFile)
	if err != nil {
		return fmt.Errorf("failed to create index file %s: %w", dc.IndexFile, err)
	}
	defer file.Close()

	if err := file.Truncate(int64(totalSize)); err != nil {
		return fmt.Errorf("failed to truncate file: %w", err)
	}

	data, err := unix.Mmap(int(file.Fd()), 0, totalSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return fmt.Errorf("failed to mmap file: %w", err)
	}
	defer unix.Munmap(data)

	// Write header
	header := (*IndexHeader)(unsafe.Pointer(&data[0]))
	header.SetHeader(dc.signature, dc.version, uint32(len(entries)), flags)

	// Write entries
	offset := HeaderSize
	for _, entry := range entries {
		entrySize := dc.writeProcessedEntryToMmap(data[offset:], entry)
		offset += entrySize
	}

	// Write checksum and sync
	checksum := dc.calculateChecksum(data[:offset])
	copy(data[offset:offset+ChecksumSize], checksum)

	return unix.Msync(data, unix.MS_SYNC)
}

// writeProcessedEntryToMmap writes a ProcessedEntry to mmap'd memory (pure binary operations)
func (dc *DirectoryCache) writeProcessedEntryToMmap(data []byte, entry ProcessedEntry) int {
	baseSize := int(unsafe.Sizeof(binaryEntry{}))
	totalSize := baseSize + len(entry.RelPath) + 1
	padding := (8 - (totalSize % 8)) % 8
	entrySize := totalSize + padding

	binEntry := (*binaryEntry)(unsafe.Pointer(&data[0]))
	binEntry.Size = uint32(entrySize)
	binEntry.CTimeWall = encodeWallTime(entry.StatInfo.Ctim.Sec, entry.StatInfo.Ctim.Nsec)
	binEntry.MTimeWall = encodeWallTime(entry.StatInfo.Mtim.Sec, entry.StatInfo.Mtim.Nsec)
	binEntry.Dev = uint32(entry.StatInfo.Dev)
	binEntry.Ino = uint32(entry.StatInfo.Ino)
	binEntry.Mode = uint32(entry.FileInfo.Mode())
	binEntry.UID = entry.StatInfo.Uid
	binEntry.GID = entry.StatInfo.Gid
	binEntry.FileSize = uint64(entry.FileInfo.Size())
	binEntry.HashType = entry.HashType
	binEntry.EntryFlags = 0

	if entry.IsDeleted {
		binEntry.SetDeleted()
	}

	// Clear and copy hash
	for i := range binEntry.Hash {
		binEntry.Hash[i] = 0
	}
	copy(binEntry.Hash[:], entry.Hash)

	// Write path
	pathOffset := int(unsafe.Sizeof(*binEntry))
	copy(data[pathOffset:pathOffset+len(entry.RelPath)], entry.RelPath)
	data[pathOffset+len(entry.RelPath)] = 0

	// Zero padding
	for i := 0; i < padding; i++ {
		data[totalSize+i] = 0
	}

	return entrySize
}

// ============================================================================
// MAIN SCAN FUNCTION
// ============================================================================

// PerformHwangLinScan performs a complete Hwang-Lin scan with asynchronous hash job coordination
func (dc *DirectoryCache) PerformHwangLinScan(ctx context.Context, paths []string, skiplist *SkiplistWrapper) ([]*HwangLinResult, error) {
	// Create channels with smaller buffers since we're streaming results
	// This reduces memory usage while maintaining good performance
	scanChan := make(chan *ScannedPath, 50)      // Smaller buffer for streaming
	resultChan := make(chan *HwangLinResult, 50) // Results stream as comparisons happen

	// Create streaming hash job manager (replaces old hash job channels)
	numWorkers := 4 // Configurable
	hashJobManager := dc.NewStreamingHashJobManager(numWorkers)
	defer hashJobManager.Shutdown()

	// Start filesystem scan
	var scanWg sync.WaitGroup
	scanWg.Add(1)
	go func() {
		defer scanWg.Done()
		if err := dc.scanPath(ctx, paths, scanChan); err != nil {
			fmt.Fprintf(os.Stderr, "Scan error: %v\n", err)
		}
	}()

	// Start Hwang-Lin comparison (now with async hash job manager)
	var compareWg sync.WaitGroup
	compareWg.Add(1)
	go func() {
		defer compareWg.Done()
		if err := dc.hwangLinCompare(ctx, scanChan, skiplist, resultChan, hashJobManager); err != nil {
			fmt.Fprintf(os.Stderr, "Compare error: %v\n", err)
		}
	}()

	// Collect results as they stream in (don't wait for hash jobs)
	var results []*HwangLinResult

	// Wait for scan to complete
	scanWg.Wait()

	// Wait for comparison to complete
	compareWg.Wait()

	// Signal that no more hash jobs will be submitted
	hashJobManager.FinishSubmitting()

	// Collect all results (this doesn't wait for hash completion)
	for result := range resultChan {
		results = append(results, result)
	}

	// NOW wait for all hash jobs to complete
	hashJobManager.WaitForCompletion()

	// Update results with hash information for new files
	for _, result := range results {
		if result.JobID != 0 && result.IndexEntry == nil {
			// New file - get hash from result store
			if hashResult, exists := hashJobManager.GetHashResult(result.JobID); exists {
				result.Hash = hashResult.Hash
				result.HashType = hashResult.HashType
			}
		}
	}

	started, completed, pending := hashJobManager.GetStats()
	fmt.Printf("Hash jobs completed: %d started, %d completed, %d pending\n", started, completed, pending)

	return results, nil
}
