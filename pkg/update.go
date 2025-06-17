package dircachefilehash

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Update scans the directory and updates the index file following design workflow with context tracking
func (dc *DirectoryCache) Update(paths ...string) error {
	// Load main index with context
	if dc.skiplist.IsEmpty() {
		if err := dc.LoadIndex(dc.IndexFile, "main"); err != nil {
			return fmt.Errorf("failed to load index: %w", err)
		}
	}

	if len(paths) == 0 {
		// No specific paths: update entire repository - put everything in main index
		return dc.updateFullRepository()
	} else {
		// Specific paths: selective update - put only specified paths in main index
		return dc.updateSpecificPaths(paths)
	}
}

// updateFullRepository updates the entire repository and puts everything in main index
func (dc *DirectoryCache) updateFullRepository() error {
	// Scan entire repository with context
	scanSkiplist, err := dc.scanPathsToSkiplist([]string{dc.RootDir}, "scan")
	if err != nil {
		return fmt.Errorf("failed to scan repository: %w", err)
	}

	// Write everything to main index
	tempIndexPath := dc.generateTempFileName("index")
	if err := dc.writeCompleteIndexFromSkiplist(scanSkiplist, tempIndexPath); err != nil {
		return fmt.Errorf("failed to write new index: %w", err)
	}

	// Atomic replace main index
	if err := os.Rename(tempIndexPath, dc.IndexFile); err != nil {
		os.Remove(tempIndexPath) // Cleanup on failure
		return fmt.Errorf("failed to rename index file: %w", err)
	}

	// Remove cache file since everything is now in main index
	os.Remove(dc.CacheFile) // Non-fatal if it fails
	dc.CleanupTempFiles()

	// Reload main index with proper context
	dc.skiplist = scanSkiplist.Copy("main")
	return nil
}

// updateSpecificPaths updates only specified paths and manages main index vs cache with context tracking
func (dc *DirectoryCache) updateSpecificPaths(paths []string) error {
	// Step 1: Load existing cache if present
	cacheSkiplist, err := dc.LoadCacheIndex("cache")
	if err != nil {
		return fmt.Errorf("failed to load cache index: %w", err)
	}

	// Step 2: Scan specified paths only with context
	updatedSkiplist, err := dc.scanPathsToSkiplist(paths, "updated")
	if err != nil {
		return fmt.Errorf("failed to scan specified paths: %w", err)
	}

	// Step 3: Create updated main index with current main + newly updated paths
	mainSkiplist := dc.skiplist.Copy("main")

	// Remove any entries from main index that are in the updated paths
	// and add the new versions
	updatedPaths := make(map[string]bool)
	updatedSkiplist.ForEach(func(entry *binaryEntry) bool {
		updatedPaths[entry.RelativePath()] = true
		return true
	})

	// Filter main index to remove updated paths
	filteredMain := NewSkiplistWrapper(16, "main")
	mainSkiplist.ForEach(func(entry *binaryEntry) bool {
		if !updatedPaths[entry.RelativePath()] {
			filteredMain.Insert(entry, "main")
		}
		return true
	})

	// Merge updated entries into filtered main
	if err := filteredMain.Merge(updatedSkiplist); err != nil {
		return fmt.Errorf("failed to merge updated entries: %w", err)
	}

	// Step 4: Write new main index with updated entries
	tempIndexPath := dc.generateTempFileName("index")
	if err := dc.writeCompleteIndexFromSkiplist(filteredMain, tempIndexPath); err != nil {
		return fmt.Errorf("failed to write new index: %w", err)
	}

	// Atomic replace main index
	if err := os.Rename(tempIndexPath, dc.IndexFile); err != nil {
		os.Remove(tempIndexPath) // Cleanup on failure
		return fmt.Errorf("failed to rename index file: %w", err)
	}

	// Step 5: Update cache to contain non-updated files
	// Scan entire repository to get current state with context
	allCurrentSkiplist, err := dc.scanPathsToSkiplist([]string{dc.RootDir}, "current")
	if err != nil {
		return fmt.Errorf("failed to scan for cache update: %w", err)
	}

	// Filter cache to only contain files NOT in main index
	cacheOnlySkiplist := NewSkiplistWrapper(16, "cache")
	allCurrentSkiplist.ForEach(func(entry *binaryEntry) bool {
		if !updatedPaths[entry.RelativePath()] {
			// This file was not explicitly updated, so it belongs in cache
			cacheOnlySkiplist.Insert(entry, "cache")
		}
		return true
	})

	// Step 6: Write updated cache index
	if !cacheOnlySkiplist.IsEmpty() {
		tempCachePath := dc.generateTempFileName("cache")
		if err := dc.writeSparseIndexFromSkiplist(cacheOnlySkiplist, tempCachePath); err != nil {
			return fmt.Errorf("failed to write cache index: %w", err)
		}

		// Atomic replace cache
		if err := os.Rename(tempCachePath, dc.CacheFile); err != nil {
			os.Remove(tempCachePath) // Cleanup on failure
			return fmt.Errorf("failed to rename cache file: %w", err)
		}
	} else {
		// No cache entries needed, remove cache file
		os.Remove(dc.CacheFile)
	}

	dc.CleanupTempFiles()

	// Reload main index with proper context
	dc.skiplist = filteredMain.Copy("main")
	return nil
}

// filterDeletedEntries returns a new skiplist without deleted entries while preserving context
func (dc *DirectoryCache) filterDeletedEntries(skiplist *SkiplistWrapper) *SkiplistWrapper {
	result := NewSkiplistWrapper(16, skiplist.GetContext())

	skiplist.ForEach(func(entry *binaryEntry) bool {
		if !entry.IsDeleted() {
			relativePath := entry.RelativePath()
			entryContext := skiplist.GetEntry(relativePath)
			result.Insert(entry, entryContext)
		}
		return true // Continue iteration
	})

	return result
}

// ScanDirectory scans the directory and creates file jobs for parallel processing with context
func (dc *DirectoryCache) ScanDirectory() error {
	jobs, err := dc.collectFileJobs([]string{dc.RootDir})
	if err != nil {
		return err
	}

	return dc.WriteIndex(jobs)
}

// collectFileJobs collects file jobs from specified paths (unchanged)
func (dc *DirectoryCache) collectFileJobs(paths []string) ([]fileJob, error) {
	var fileJobs []fileJob
	jobIndex := 0

	// Load ignore patterns if not already loaded
	if err := dc.ignoreManager.LoadIgnorePatterns(); err != nil {
		return nil, fmt.Errorf("failed to load ignore patterns: %w", err)
	}

	for _, inputPath := range paths {
		absPath := inputPath
		if !filepath.IsAbs(inputPath) {
			absPath = filepath.Join(dc.RootDir, inputPath)
		}
		absPath = filepath.Clean(absPath)

		info, err := os.Lstat(absPath)
		if err != nil {
			continue // Skip inaccessible paths
		}

		relPath, err := filepath.Rel(dc.RootDir, absPath)
		if err != nil {
			return nil, fmt.Errorf("path %s is not under root directory %s", absPath, dc.RootDir)
		}

		// Check if path should be ignored
		if dc.ignoreManager.ShouldIgnore(relPath) {
			continue
		}

		if info.IsDir() {
			if err := dc.scanPathRecursively(absPath, &fileJobs, &jobIndex); err != nil {
				return nil, fmt.Errorf("failed to scan directory %s: %w", absPath, err)
			}
		} else if info.Mode().IsRegular() {
			if absPath == dc.IndexFile {
				continue
			}

			fileJobs = append(fileJobs, fileJob{
				path:    absPath,
				info:    info,
				relPath: relPath,
				index:   jobIndex,
			})
			jobIndex++
		}
	}

	// Sort jobs by relative path for consistent ordering
	sort.Slice(fileJobs, func(i, j int) bool {
		return fileJobs[i].relPath < fileJobs[j].relPath
	})

	return fileJobs, nil
}

// scanPathRecursively scans a directory recursively (unchanged)
func (dc *DirectoryCache) scanPathRecursively(rootPath string, fileJobs *[]fileJob, jobIndex *int) error {
	pathQueue := []string{rootPath}

	for len(pathQueue) > 0 {
		currentPath := pathQueue[0]
		pathQueue = pathQueue[1:]

		info, err := os.Lstat(currentPath)
		if err != nil {
			continue
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
			indexDir := filepath.Dir(dc.IndexFile)
			if currentPath == indexDir {
				continue
			}

			entries, err := os.ReadDir(currentPath)
			if err != nil {
				continue
			}

			sort.Slice(entries, func(i, j int) bool {
				return entries[i].Name() < entries[j].Name()
			})

			for _, entry := range entries {
				fullPath := filepath.Join(currentPath, entry.Name())
				pathQueue = append(pathQueue, fullPath)
			}
		} else if info.Mode().IsRegular() {
			if currentPath == dc.IndexFile {
				continue
			}

			*fileJobs = append(*fileJobs, fileJob{
				path:    currentPath,
				info:    info,
				relPath: relPath,
				index:   *jobIndex,
			})
			*jobIndex++
		}
	}

	return nil
}

// hashJob represents a file hashing job with pointer to binEntry
type hashJob struct {
	entry    *binaryEntry
	filePath string
	deviceID uint64
}

// hashResult represents the result of a hash operation
type hashResult struct {
	entry    *binaryEntry
	hash     []byte
	hashType uint16
	err      error
}

// UpdatePaths updates only the specified paths using skiplist and Hwang-Lin merge algorithm with context tracking
func (dc *DirectoryCache) UpdatePaths(paths []string) error {
	// Load existing index into skiplist with context
	var existingSkiplist *SkiplistWrapper
	if err := dc.LoadIndex(dc.IndexFile, "main"); err == nil {
		existingSkiplist = dc.skiplist.Copy("main")
	} else {
		existingSkiplist = NewSkiplistWrapper(16, "main")
	}

	// Calculate approximate size for temporary mmap
	estimatedSize := HeaderSize + ChecksumSize + (1024 * 1024) // Start with 1MB

	// Create temporary mmap for building new index
	tempFile, err := os.CreateTemp(filepath.Dir(dc.IndexFile), "index_temp_*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	if err := tempFile.Truncate(int64(estimatedSize)); err != nil {
		return fmt.Errorf("failed to truncate temp file: %w", err)
	}

	tempData, err := unix.Mmap(int(tempFile.Fd()), 0, estimatedSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return fmt.Errorf("failed to mmap temp file: %w", err)
	}
	defer unix.Munmap(tempData)

	// Write temp header directly to mmap'd memory (zero-copy)
	tempHeader := (*IndexHeader)(unsafe.Pointer(&tempData[0]))
	tempHeader.SetHeader(dc.signature, dc.version, 0, 0)

	// Create new skiplist for updated entries with context
	newSkiplist := NewSkiplistWrapper(16, "updated")

	// Setup channels for worker communication
	jobChan := make(chan hashJob, 100)
	resultChan := make(chan hashResult, 100)
	doneChan := make(chan struct{})

	// Start worker processes
	var muxWg sync.WaitGroup
	muxWg.Add(1)
	go func() {
		defer muxWg.Done()
		dc.workerMuxHandler(jobChan, resultChan)
	}()

	var resultWg sync.WaitGroup
	resultWg.Add(1)
	go func() {
		defer resultWg.Done()
		dc.handleHashResults(resultChan, doneChan)
	}()

	// Process paths with parallel hashing using Hwang-Lin merge
	offset := HeaderSize
	entryCount := uint32(0)

	if err := dc.processPathsWithHwangLin(paths, existingSkiplist, newSkiplist, tempData, &offset,
		&entryCount, estimatedSize, jobChan); err != nil {
		close(jobChan)
		return err
	}

	// Signal no more jobs and wait for completion
	close(jobChan)
	muxWg.Wait()
	<-doneChan
	resultWg.Wait()

	// Update header with final count (direct access to mmap'd memory)
	tempHeader.EntryCount = entryCount

	// Calculate and write checksum
	checksum := dc.calculateChecksum(tempData[:offset])
	copy(tempData[offset:offset+ChecksumSize], checksum)

	// Sync temp file
	if err := unix.Msync(tempData[:offset+ChecksumSize], unix.MS_SYNC); err != nil {
		return fmt.Errorf("failed to sync temp file: %w", err)
	}

	// Replace index file with temp file
	if err := os.Rename(tempFile.Name(), dc.IndexFile); err != nil {
		return fmt.Errorf("failed to replace index file: %w", err)
	}

	// Update our skiplist with the new data and proper context
	dc.skiplist = newSkiplist.Copy("main")

	return nil
}

// processPathsWithHwangLin uses Hwang-Lin algorithm to merge existing and new entries with context tracking
func (dc *DirectoryCache) processPathsWithHwangLin(paths []string, existingSkiplist, newSkiplist *SkiplistWrapper,
	tempData []byte, offset *int, entryCount *uint32, maxSize int, jobChan chan<- hashJob) error {

	// Collect all file jobs for the paths
	allJobs, err := dc.collectFileJobs(paths)
	if err != nil {
		return err
	}

	// Create a map for quick lookup of new files
	newFiles := make(map[string]fileJob)
	for _, job := range allJobs {
		newFiles[job.relPath] = job
	}

	// Get existing entries in sorted order
	existingEntries := existingSkiplist.GetSortedEntries()

	// Create sorted list of new file paths
	var newPaths []string
	for path := range newFiles {
		newPaths = append(newPaths, path)
	}
	sort.Strings(newPaths)

	// Hwang-Lin merge algorithm
	i, j := 0, 0
	for i < len(existingEntries) && j < len(newPaths) {
		existing := existingEntries[i]
		newPath := newPaths[j]
		newJob := newFiles[newPath]

		cmp := strings.Compare(existing.RelativePath(), newPath)

		if cmp == 0 {
			// File exists in both - check if changed
			if dc.fileChangedFromJob(existing, newJob) {
				// File changed - process with new data
				if err := dc.processNewFileEntry(newJob, tempData, offset, entryCount, maxSize, jobChan, newSkiplist); err != nil {
					return err
				}
			} else {
				// File unchanged - copy existing entry
				if err := dc.copyExistingEntry(existing, tempData, offset, entryCount, maxSize, newSkiplist); err != nil {
					return err
				}
			}
			i++
			j++
		} else if cmp < 0 {
			// Existing file not in new set - skip (effectively delete)
			i++
		} else {
			// New file not in existing set - add
			if err := dc.processNewFileEntry(newJob, tempData, offset, entryCount, maxSize, jobChan, newSkiplist); err != nil {
				return err
			}
			j++
		}
	}

	// Handle remaining new files
	for j < len(newPaths) {
		newJob := newFiles[newPaths[j]]
		if err := dc.processNewFileEntry(newJob, tempData, offset, entryCount, maxSize, jobChan, newSkiplist); err != nil {
			return err
		}
		j++
	}

	return nil
}

// fileChangedFromJob checks if a file has changed compared to existing entry (unchanged)
func (dc *DirectoryCache) fileChangedFromJob(existing *binaryEntry, job fileJob) bool {
	stat := job.info.Sys().(*syscall.Stat_t)

	if existing.FileSize != uint64(job.info.Size()) {
		return true
	}
	if existing.UID != stat.Uid || existing.GID != stat.Gid {
		return true
	}

	currentCTime := encodeWallTime(stat.Ctim.Sec, stat.Ctim.Nsec)
	currentMTime := encodeWallTime(stat.Mtim.Sec, stat.Mtim.Nsec)

	return existing.CTimeWall != currentCTime || existing.MTimeWall != currentMTime
}

// copyExistingEntry copies an unchanged entry to the new index with context preservation
func (dc *DirectoryCache) copyExistingEntry(existing *binaryEntry, tempData []byte, offset *int,
	entryCount *uint32, maxSize int, newSkiplist *SkiplistWrapper) error {

	existingSize := existing.EntrySize()
	if *offset+existingSize > maxSize-ChecksumSize {
		return fmt.Errorf("temp file too small")
	}

	// Copy entire entry
	copy(tempData[*offset:*offset+existingSize],
		(*[1 << 20]byte)(unsafe.Pointer(existing))[:existingSize:existingSize])

	// Get pointer to copied entry and add to new skiplist with context
	copiedEntry := (*binaryEntry)(unsafe.Pointer(&tempData[*offset]))
	newSkiplist.Insert(copiedEntry, "updated")

	*offset += existingSize
	*entryCount++
	return nil
}

// processNewFileEntry processes a new or changed file entry with context tracking
func (dc *DirectoryCache) processNewFileEntry(job fileJob, tempData []byte, offset *int,
	entryCount *uint32, maxSize int, jobChan chan<- hashJob, newSkiplist *SkiplistWrapper) error {

	stat := job.info.Sys().(*syscall.Stat_t)

	// Calculate entry size
	entrySize := int(unsafe.Sizeof(binaryEntry{})) + len(job.relPath) + 1
	padding := (8 - (entrySize % 8)) % 8
	totalSize := entrySize + padding

	if *offset+totalSize > maxSize-ChecksumSize {
		return fmt.Errorf("temp file too small")
	}

	// Create entry in temp mmap
	entryPtr := (*binaryEntry)(unsafe.Pointer(&tempData[*offset]))
	entryPtr.Size = uint32(totalSize)
	entryPtr.CTimeWall = encodeWallTime(stat.Ctim.Sec, stat.Ctim.Nsec)
	entryPtr.MTimeWall = encodeWallTime(stat.Mtim.Sec, stat.Mtim.Nsec)
	entryPtr.Dev = uint32(stat.Dev)
	entryPtr.Ino = uint32(stat.Ino)
	entryPtr.Mode = uint32(job.info.Mode())
	entryPtr.UID = stat.Uid
	entryPtr.GID = stat.Gid
	entryPtr.FileSize = uint64(job.info.Size())
	entryPtr.Flags = uint16(len(job.relPath))
	entryPtr.EntryFlags = 0

	// Write path
	pathOffset := int(unsafe.Sizeof(*entryPtr))
	copy(tempData[*offset+pathOffset:], job.relPath)
	tempData[*offset+pathOffset+len(job.relPath)] = 0

	// Zero padding
	for i := 0; i < padding; i++ {
		tempData[*offset+entrySize+i] = 0
	}

	// Add to skiplist with context
	newSkiplist.Insert(entryPtr, "updated")

	// Queue for hashing
	hashJob := hashJob{
		entry:    entryPtr,
		filePath: job.path,
		deviceID: stat.Dev,
	}

	select {
	case jobChan <- hashJob:
	default:
		return fmt.Errorf("job channel full")
	}

	*offset += totalSize
	*entryCount++
	return nil
}

// fileUnchanged checks if file metadata indicates no changes (unchanged)
func (dc *DirectoryCache) fileUnchanged(existing *binaryEntry, info os.FileInfo, stat *syscall.Stat_t) bool {
	if existing.FileSize != uint64(info.Size()) {
		return false
	}
	if existing.UID != stat.Uid || existing.GID != stat.Gid {
		return false
	}

	currentCTime := encodeWallTime(stat.Ctim.Sec, stat.Ctim.Nsec)
	currentMTime := encodeWallTime(stat.Mtim.Sec, stat.Mtim.Nsec)

	return existing.CTimeWall == currentCTime && existing.MTimeWall == currentMTime
}

// workerMuxHandler manages device-specific worker pools, creating them on demand (unchanged)
func (dc *DirectoryCache) workerMuxHandler(jobChan <-chan hashJob, resultChan chan<- hashResult) {
	devicePools := make(map[uint64]chan hashJob)
	var poolWg sync.WaitGroup

	// Calculate workers per device pool
	workersPerDevice := runtime.NumCPU() / 2
	if workersPerDevice < 2 {
		workersPerDevice = 2
	}

	for job := range jobChan {
		deviceChan, exists := devicePools[job.deviceID]
		if !exists {
			// Create new device-specific worker pool
			deviceChan = make(chan hashJob, workersPerDevice*2) // Buffer for workers
			devicePools[job.deviceID] = deviceChan

			poolWg.Add(1)
			go func(devID uint64, devChan chan hashJob) {
				defer poolWg.Done()
				dc.runDevicePool(devID, devChan, resultChan, workersPerDevice)
			}(job.deviceID, deviceChan)
		}

		// Send job to device-specific worker pool
		deviceChan <- job
	}

	// Close all device pools
	for _, deviceChan := range devicePools {
		close(deviceChan)
	}

	// Wait for all device pools to finish
	poolWg.Wait()
	close(resultChan)
}

// runDevicePool runs workers for a specific device (unchanged)
func (dc *DirectoryCache) runDevicePool(deviceID uint64, jobChan <-chan hashJob, resultChan chan<- hashResult, numWorkers int) {
	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dc.deviceHashWorkerNew(jobChan, resultChan)
		}()
	}

	wg.Wait()
}

// deviceHashWorkerNew processes hash jobs for a device (unchanged)
func (dc *DirectoryCache) deviceHashWorkerNew(jobChan <-chan hashJob, resultChan chan<- hashResult) {
	for job := range jobChan {
		hashStr, err := dc.hashFile(job.filePath)

		var hashBytes []byte
		var hashType uint16
		if err == nil {
			var hexErr error
			hashBytes, hexErr = hex.DecodeString(hashStr)
			if hexErr == nil {
				hashType = HashTypeSHA1 // Currently using SHA1 - TODO: make configurable
			} else {
				err = hexErr
			}
		}

		result := hashResult{
			entry:    job.entry,
			hash:     hashBytes,
			hashType: hashType,
			err:      err,
		}

		resultChan <- result
		// Clear reference to prevent memory leak
		job.entry = nil
	}
}

// handleHashResults processes hash results and updates entries (unchanged)
func (dc *DirectoryCache) handleHashResults(resultChan <-chan hashResult, doneChan chan<- struct{}) {
	pageSize := os.Getpagesize()

	for result := range resultChan {
		if result.err != nil {
			fmt.Fprintf(os.Stderr, "Warning: hash error: %v\n", result.err)
			continue
		}

		// Update hash and hash type in-place via pointer
		result.entry.HashType = result.hashType

		// Clear hash field and copy new hash data
		for i := range result.entry.Hash {
			result.entry.Hash[i] = 0
		}
		copy(result.entry.Hash[:], result.hash)

		// Only hint to OS if entry is within Size bytes of page end
		entryAddr := uintptr(unsafe.Pointer(result.entry))
		nextPageBoundary := (entryAddr + uintptr(pageSize)) &^ uintptr(pageSize-1)
		distanceToPageEnd := nextPageBoundary - entryAddr

		if distanceToPageEnd <= uintptr(result.entry.Size) {
			sysUnusedOS(unsafe.Pointer(result.entry), int(result.entry.Size))
		}

		// Clear reference to prevent memory leak
		result.entry = nil
	}

	close(doneChan)
}
