package dircachefilehash

import (
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

// HashJobStart represents a hash job being started
type HashJobStart struct {
	JobID       uint64
	FilePath    string
	IndexEntry  *binaryEntry // Entry to update with hash
	ScannedPath *ScannedPath
}

// Helper function for efficient slice removal (order doesn't matter)
func remove(s []uint64, i int) []uint64 {
	s[i] = s[len(s)-1]
	return s[:len(s)-1]
}

// SimpleHashManager - coordinates hash job completion
type SimpleHashManager struct {
	hashJobChan    chan *HashJobStart
	callFinishChan chan uint64 // job completion notifications
	wg             sync.WaitGroup
}

// ============================================================================
// FILESYSTEM SCANNING FUNCTIONS
// ============================================================================

// scanPath scans filesystem paths in sorted order and sends them via channel as they're found
func (dc *DirectoryCache) scanPath(paths []string, resultChan chan<- *ScannedPath) error {
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
		if err := dc.scanPathRecursive(absPath, resultChan); err != nil {
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
func (dc *DirectoryCache) scanPathRecursive(rootPath string, resultChan chan<- *ScannedPath) error {
	// Use a priority queue (sorted slice) to ensure we process paths in alphabetical order
	// This ensures the output is naturally sorted
	pathQueue := []string{rootPath}

	for len(pathQueue) > 0 {

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
			resultChan <- scannedPath
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

// hwangLinCompareToSkiplist performs Hwang-Lin comparison and builds scan index + skiplist
func (dc *DirectoryCache) hwangLinCompareToSkiplist(
	scanChan <-chan *ScannedPath,
	compareSkiplist *SkiplistWrapper,
	scanSkiplist *SkiplistWrapper,
	scanFileName string,
	hashJobManager *SimpleHashManager,
	callStartChan chan<- uint64,
) error {
	var currentScanned *ScannedPath
	var scanChanOpen bool = true
	currentIndex := compareSkiplist.skiplist.First()
	jobIDCounter := uint64(1)

	// Read first scanned path
	if scanChanOpen {
		currentScanned, scanChanOpen = <-scanChan
	}

	for scanChanOpen || currentIndex != nil {

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
				// File modified - create scan index entry and submit for hashing
				scanEntry, err := dc.AppendEntryToScanIndex(scanFileName, currentScanned.RelPath, currentScanned)
				if err != nil {
					return fmt.Errorf("failed to create scan index entry: %w", err)
				}

				// Insert into scan skiplist
				scanSkiplist.Insert(scanEntry, ScanContext)

				// Submit for async hashing
				jobID := jobIDCounter
				jobIDCounter++

				hashJob := &HashJobStart{
					JobID:       jobID,
					FilePath:    currentScanned.AbsPath,
					IndexEntry:  scanEntry, // Hash worker will update this directly
					ScannedPath: currentScanned,
				}

				hashJobManager.SubmitHashJob(hashJob, callStartChan)

			} else {
				// File unchanged - copy existing entry to scan index and skiplist
				scanEntry, err := dc.AppendEntryToScanIndex(scanFileName, currentScanned.RelPath, currentScanned)
				if err != nil {
					return fmt.Errorf("failed to create scan index entry: %w", err)
				}

				// Copy hash from existing entry
				copy(scanEntry.Hash[:], indexEntry.Hash[:])
				scanEntry.HashType = indexEntry.HashType

				// Insert into scan skiplist
				scanSkiplist.Insert(scanEntry, ScanContext)
			}

			// Advance both
			if scanChanOpen {
				currentScanned, scanChanOpen = <-scanChan
			}
			currentIndex = currentIndex.Next()

		} else if cmp < 0 {
			// File only in scan - new file, create scan index entry and submit for hashing
			scanEntry, err := dc.AppendEntryToScanIndex(scanFileName, currentScanned.RelPath, currentScanned)
			if err != nil {
				return fmt.Errorf("failed to create scan index entry: %w", err)
			}

			// Insert into scan skiplist
			scanSkiplist.Insert(scanEntry, ScanContext)

			// Submit for async hashing
			jobID := jobIDCounter
			jobIDCounter++

			hashJob := &HashJobStart{
				JobID:       jobID,
				FilePath:    currentScanned.AbsPath,
				IndexEntry:  scanEntry, // Hash worker will update this directly
				ScannedPath: currentScanned,
			}

			hashJobManager.SubmitHashJob(hashJob, callStartChan)

			// Advance scan
			if scanChanOpen {
				currentScanned, scanChanOpen = <-scanChan
			}

		} else {
			// File only in index - deleted file, mark as deleted in scan skiplist
			indexEntry := currentIndex.Item()

			// Skip already deleted entries
			if !indexEntry.IsDeleted() {
				// Create a deleted entry in scan index
				deletedEntry, err := dc.AppendEntryToScanIndex(scanFileName, indexEntry.RelativePath(), &ScannedPath{
					RelPath: indexEntry.RelativePath(),
				})
				if err != nil {
					return fmt.Errorf("failed to create deleted scan index entry: %w", err)
				}

				// Mark as deleted and copy hash
				deletedEntry.SetDeleted()
				copy(deletedEntry.Hash[:], indexEntry.Hash[:])
				deletedEntry.HashType = indexEntry.HashType

				// Insert into scan skiplist
				scanSkiplist.Insert(deletedEntry, ScanContext)
			}

			// Advance index
			currentIndex = currentIndex.Next()
		}
	}

	return nil
}

// hwangLinCompare performs Hwang-Lin algorithm comparison between scanned filesystem and skiplist
// Now with asynchronous hash job processing - hash jobs don't block the comparison
func (dc *DirectoryCache) hwangLinCompare(
	scanChan <-chan *ScannedPath,
	skiplist *SkiplistWrapper,
	resultChan chan<- *HwangLinResult,
	hashJobManager *SimpleHashManager,
	callStartChan chan<- uint64,
) error {
	defer close(resultChan)

	var currentScanned *ScannedPath
	var scanChanOpen bool = true
	currentIndex := skiplist.skiplist.First()
	jobIDCounter := uint64(1)

	// Read first scanned path
	if scanChanOpen {
		currentScanned, scanChanOpen = <-scanChan
	}

	for scanChanOpen || currentIndex != nil {

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
				hashJobManager.SubmitHashJob(hashJob, callStartChan)

				result := &HwangLinResult{
					Type:        HLModified,
					ScannedPath: currentScanned,
					IndexEntry:  indexEntry,
					JobID:       jobID,
					// Hash and HashType will be updated by async worker
				}

				resultChan <- result
			} else {
				// File unchanged
				result := &HwangLinResult{
					Type:        HLUnchanged,
					ScannedPath: currentScanned,
					IndexEntry:  indexEntry,
					JobID:       0,
					// No hash needed for unchanged files
				}

				resultChan <- result
			}

			// Advance both
			if scanChanOpen {
				currentScanned, scanChanOpen = <-scanChan
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
			hashJobManager.SubmitHashJob(hashJob, callStartChan)

			result := &HwangLinResult{
				Type:        HLNew,
				ScannedPath: currentScanned,
				IndexEntry:  nil,
				JobID:       jobID,
				// Hash will be available from hash job completion tracking
			}

			resultChan <- result

			// Advance scan
			if scanChanOpen {
				currentScanned, scanChanOpen = <-scanChan
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

				resultChan <- result
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

// NewSimpleHashManager creates a new simple hash manager
func (dc *DirectoryCache) NewSimpleHashManager(numWorkers int, callFinishChan chan uint64) *SimpleHashManager {
	manager := &SimpleHashManager{
		hashJobChan:    make(chan *HashJobStart, 100),
		callFinishChan: callFinishChan,
	}

	// Start workers
	for i := 0; i < numWorkers; i++ {
		manager.wg.Add(1)
		go manager.hashWorker(dc)
	}

	return manager
}

// SubmitHashJob submits a hash job and signals the start
func (hjm *SimpleHashManager) SubmitHashJob(job *HashJobStart, callStartChan chan<- uint64) {
	hjm.hashJobChan <- job
	callStartChan <- job.JobID // Signal job started
}

// FinishSubmitting signals that no more hash jobs will be submitted
func (hjm *SimpleHashManager) FinishSubmitting() {
	close(hjm.hashJobChan)
}

// hashWorker processes hash jobs and updates entries directly in scan index mmap
func (hjm *SimpleHashManager) hashWorker(dc *DirectoryCache) {
	defer hjm.wg.Done()

	for job := range hjm.hashJobChan {
		// Hash the file and update binaryEntry directly in mmap memory
		hashStr, err := dc.hashFile(job.FilePath)

		if err == nil {
			if hashBytes, hexErr := hex.DecodeString(hashStr); hexErr == nil {
				// Update the binaryEntry directly in the scan index mmap memory
				// This provides zero-copy updates to the scan index file
				dc.updateBinaryEntryHash(job.IndexEntry, hashBytes, HashTypeSHA1)
			}
		}

		// Signal completion
		hjm.callFinishChan <- job.JobID
	}
}

// Shutdown gracefully shuts down the hash manager
func (hjm *SimpleHashManager) Shutdown() {
	hjm.wg.Wait()
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

// monitorJobs tracks hash job starts and completions
func (dc *DirectoryCache) monitorJobs(
	callStartChan <-chan uint64,
	callFinishChan <-chan uint64,
	collectionStop <-chan struct{},
) {
	var jobs []uint64 // Track pending hash jobs
	stopped := false

	for {
		select {
		case jobID := <-callStartChan:
			jobs = append(jobs, jobID)

		case completedJobID := <-callFinishChan:
			// Remove completed job from jobs slice
			for i, id := range jobs {
				if id == completedJobID {
					jobs = remove(jobs, i)
					break
				}
			}

		case <-collectionStop:
			stopped = true
		}

		// If stopped and no pending jobs, we're done
		if stopped && len(jobs) == 0 {
			return
		}
	}
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


// ============================================================================
// MAIN SCAN FUNCTION
// ============================================================================

// PerformHwangLinScan performs a complete Hwang-Lin scan with asynchronous hash job coordination
func (dc *DirectoryCache) PerformHwangLinScan(paths []string, skiplist *SkiplistWrapper) ([]*HwangLinResult, error) {
	// Create channels with smaller buffers since we're streaming results
	// This reduces memory usage while maintaining good performance
	scanChan := make(chan *ScannedPath, 50)      // Smaller buffer for streaming
	resultChan := make(chan *HwangLinResult, 50) // Results stream as comparisons happen
	callStartChan := make(chan uint64, 100)      // Job start notifications
	callFinishChan := make(chan uint64, 100)     // Job finish notifications
	collectionStop := make(chan struct{})        // Stop monitoring signal

	// Create simple hash job manager
	hashJobManager := dc.NewSimpleHashManager(4, callFinishChan) // Configurable workers
	defer hashJobManager.Shutdown()

	var results []*HwangLinResult

	// Start filesystem scan
	var scanWg sync.WaitGroup
	scanWg.Add(1)
	go func() {
		defer scanWg.Done()
		if err := dc.scanPath(paths, scanChan); err != nil {
			fmt.Fprintf(os.Stderr, "Scan error: %v\n", err)
		}
	}()

	// Start Hwang-Lin comparison (now with simple hash job manager)
	var compareWg sync.WaitGroup
	compareWg.Add(1)
	go func() {
		defer compareWg.Done()
		if err := dc.hwangLinCompare(scanChan, skiplist, resultChan, hashJobManager, callStartChan); err != nil {
			fmt.Fprintf(os.Stderr, "Compare error: %v\n", err)
		}
	}()

	// Monitor hash jobs
	var monitorWg sync.WaitGroup
	monitorWg.Add(1)
	go func() {
		defer monitorWg.Done()
		dc.monitorJobs(callStartChan, callFinishChan, collectionStop)
	}()

	// Collect HwangLinResults separately
	go func() {
		for result := range resultChan {
			results = append(results, result)
		}
	}()

	// Wait for scan to complete
	scanWg.Wait()

	// Wait for comparison to complete
	compareWg.Wait()

	// Signal that no more hash jobs will be submitted
	hashJobManager.FinishSubmitting()

	// Signal monitoring to stop and wait for all jobs to finish
	close(collectionStop)
	monitorWg.Wait()

	fmt.Printf("Hash job processing completed\n")

	return results, nil
}

// PerformHwangLinScanToSkiplist performs Hwang-Lin scan and builds a skiplist directly with scan index files
func (dc *DirectoryCache) PerformHwangLinScanToSkiplist(paths []string, compareSkiplist *SkiplistWrapper) (*SkiplistWrapper, error) {
	// Create result skiplist for scan entries
	scanSkiplist := NewSkiplistWrapper(16, ScanContext)
	
	// Generate scan index filename for this operation
	scanFileName := dc.generateScanFileName()
	defer os.Remove(scanFileName) // Cleanup scan file when done
	
	// Create channels for streaming data
	scanChan := make(chan *ScannedPath, 50)
	callStartChan := make(chan uint64, 100)
	callFinishChan := make(chan uint64, 100)
	collectionStop := make(chan struct{})

	// Create hash job manager for concurrent hashing
	hashJobManager := dc.NewSimpleHashManager(4, callFinishChan)
	defer hashJobManager.Shutdown()

	// Start filesystem scan
	var scanWg sync.WaitGroup
	scanWg.Add(1)
	go func() {
		defer scanWg.Done()
		if err := dc.scanPath(paths, scanChan); err != nil {
			fmt.Fprintf(os.Stderr, "Scan error: %v\n", err)
		}
	}()

	// Start modified Hwang-Lin comparison that builds scan index and skiplist
	var compareWg sync.WaitGroup
	compareWg.Add(1)
	go func() {
		defer compareWg.Done()
		if err := dc.hwangLinCompareToSkiplist(scanChan, compareSkiplist, scanSkiplist, scanFileName, hashJobManager, callStartChan); err != nil {
			fmt.Fprintf(os.Stderr, "Compare error: %v\n", err)
		}
	}()

	// Monitor hash jobs
	var monitorWg sync.WaitGroup
	monitorWg.Add(1)
	go func() {
		defer monitorWg.Done()
		dc.monitorJobs(callStartChan, callFinishChan, collectionStop)
	}()

	// Wait for scan to complete
	scanWg.Wait()

	// Wait for comparison to complete
	compareWg.Wait()

	// Signal that no more hash jobs will be submitted
	hashJobManager.FinishSubmitting()

	// Signal monitoring to stop and wait for all jobs to finish
	close(collectionStop)
	monitorWg.Wait()

	fmt.Printf("Scan to skiplist completed\n")

	return scanSkiplist, nil
}
