package dircachefilehash

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"syscall"
	"unsafe"

	zcsl "github.com/mattkeenan/zerocopyskiplist"
)

// Update scans the directory and updates the index file using the new workflow
func (dc *DirectoryCache) Update(paths ...string) error {
	if len(paths) == 0 {
		// No specific paths: update entire repository - put everything in main index
		return dc.updateFullRepository()
	} else {
		// Specific paths: selective update - manage main vs cache indices
		return dc.updateSpecificPaths(paths)
	}
}

// updateFullRepository updates the entire repository and puts everything in main index
func (dc *DirectoryCache) updateFullRepository() error {
	// Scan entire repository
	scanSkiplist, err := dc.CreateTmpIndexFromScan(NewSkiplistWrapper(16, "empty"))
	if err != nil {
		return fmt.Errorf("failed to scan repository: %w", err)
	}

	// Write everything to main index using the new workflow
	tempIndexPath := dc.generateTempFileName("index")
	if err := dc.WriteSkiplistToTmpIndex(scanSkiplist, tempIndexPath, ""); err != nil {
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

	// Update our skiplist with the new data
	dc.skiplist = scanSkiplist.Copy()
	return nil
}

// updateSpecificPaths updates only specified paths and manages main index vs cache
func (dc *DirectoryCache) updateSpecificPaths(paths []string) error {
	// Load main index
	mainSkiplist, err := dc.LoadMainIndex()
	if err != nil {
		return fmt.Errorf("failed to load main index: %w", err)
	}

	// Scan specified paths only
	updatedSkiplist, err := dc.scanPathsToSkiplist(paths)
	if err != nil {
		return fmt.Errorf("failed to scan specified paths: %w", err)
	}

	// Create updated main index with current main + newly updated paths
	newMainSkiplist := mainSkiplist.Copy()

	// Remove any entries from main index that are in the updated paths
	// and add the new versions
	updatedPaths := make(map[string]bool)
	updatedSkiplist.ForEach(func(entry *binaryEntry, context string) bool {
		updatedPaths[entry.RelativePath()] = true
		return true
	})

	// Filter main index to remove updated paths
	filteredMain := NewSkiplistWrapper(16, MainContext)
	newMainSkiplist.ForEach(func(entry *binaryEntry, context string) bool {
		if !updatedPaths[entry.RelativePath()] {
			filteredMain.Insert(entry, MainContext)
		}
		return true
	})

	// Merge updated entries into filtered main
	if err := filteredMain.Merge(updatedSkiplist, zcsl.MergeTheirs); err != nil {
		return fmt.Errorf("failed to merge updated entries: %w", err)
	}

	// Write new main index
	tempIndexPath := dc.generateTempFileName("index")
	if err := dc.WriteSkiplistToTmpIndex(filteredMain, tempIndexPath, MainContext); err != nil {
		return fmt.Errorf("failed to write new index: %w", err)
	}

	// Atomic replace main index
	if err := os.Rename(tempIndexPath, dc.IndexFile); err != nil {
		os.Remove(tempIndexPath) // Cleanup on failure
		return fmt.Errorf("failed to rename index file: %w", err)
	}

	// Update cache using the new workflow
	if err := dc.UpdateCacheIndexWithWorkflow(); err != nil {
		return fmt.Errorf("failed to update cache: %w", err)
	}

	dc.CleanupTempFiles()

	// Update our skiplist
	dc.skiplist = filteredMain.Copy()
	return nil
}

// scanPathsToSkiplist scans paths and returns a skiplist (simplified version using new workflow)
func (dc *DirectoryCache) scanPathsToSkiplist(paths []string) (*SkiplistWrapper, error) {
	jobs, err := dc.collectFileJobs(paths)
	if err != nil {
		return nil, fmt.Errorf("failed to collect file jobs: %w", err)
	}

	if len(jobs) == 0 {
		return NewSkiplistWrapper(16, ScanContext), nil
	}

	// Create temporary index file for the scan
	tempScanPath := dc.generateTempFileName("scan")
	defer os.Remove(tempScanPath)

	// Create temporary cache to write the jobs
	tempCache := &DirectoryCache{
		RootDir:       dc.RootDir,
		IndexFile:     tempScanPath,
		CacheFile:     tempScanPath,
		skiplist:      NewSkiplistWrapper(16, ScanContext),
		signature:     dc.signature,
		version:       dc.version,
		hasher:        dc.hasher,
		ignoreManager: dc.ignoreManager,
	}

	// Write jobs to temporary index file
	if err := tempCache.WriteIndex(jobs); err != nil {
		return nil, fmt.Errorf("failed to write temp scan index: %w", err)
	}

	// Load the temporary index
	result, err := tempCache.LoadMainIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to load temp scan index: %w", err)
	}

	// Set context to scan for all entries
	scanResult := NewSkiplistWrapper(16, ScanContext)
	result.ForEach(func(entry *binaryEntry, context string) bool {
		scanResult.Insert(entry, ScanContext)
		return true
	})

	return scanResult, nil
}

// collectFileJobs collects file jobs from specified paths
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

// scanPathRecursively scans a directory recursively
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

// fileChangedFromJob checks if a file has changed compared to existing entry
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

// deviceHashWorkerNew processes hash jobs for a device
func (dc *DirectoryCache) deviceHashWorkerNew(jobChan <-chan hashJob, resultChan chan<- hashResult) {
	for job := range jobChan {
		hashStr, err := dc.hashFile(job.filePath)

		var hashBytes []byte
		var hashType uint16
		if err == nil {
			var hexErr error
			hashBytes, hexErr = hex.DecodeString(hashStr)
			if hexErr == nil {
				hashType = HashTypeSHA1 // Currently using SHA1
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

// hashJob and hashResult types
type hashJob struct {
	entry    *binaryEntry
	filePath string
	deviceID uint64
}

type hashResult struct {
	entry    *binaryEntry
	hash     []byte
	hashType uint16
	err      error
}

// workerMuxHandler manages device-specific worker pools, creating them on demand
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

// runDevicePool runs workers for a specific device
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

// createNewEntryFromJob creates a new entry from a job in temporary mmap space
func (dc *DirectoryCache) createNewEntryFromJob(job fileJob, tempData []byte, offset *int, maxSize int) (*binaryEntry, error) {
	stat := job.info.Sys().(*syscall.Stat_t)

	// Calculate entry size
	baseSize := int(unsafe.Sizeof(binaryEntry{}))
	totalSize := baseSize + len(job.relPath) + 1
	padding := (8 - (totalSize % 8)) % 8
	entrySize := totalSize + padding

	if *offset+entrySize > maxSize-ChecksumSize {
		return nil, fmt.Errorf("temp file too small")
	}

	// Create entry in temp mmap
	entryPtr := (*binaryEntry)(unsafe.Pointer(&tempData[*offset]))
	entryPtr.Size = uint32(entrySize)
	entryPtr.CTimeWall = encodeWallTime(stat.Ctim.Sec, stat.Ctim.Nsec)
	entryPtr.MTimeWall = encodeWallTime(stat.Mtim.Sec, stat.Mtim.Nsec)
	entryPtr.Dev = uint32(stat.Dev)
	entryPtr.Ino = uint32(stat.Ino)
	entryPtr.Mode = uint32(job.info.Mode())
	entryPtr.UID = stat.Uid
	entryPtr.GID = stat.Gid
	entryPtr.FileSize = uint64(job.info.Size())
	entryPtr.HashType = HashTypeSHA1
	entryPtr.EntryFlags = 0

	// Clear hash field
	for i := range entryPtr.Hash {
		entryPtr.Hash[i] = 0
	}

	// Write path
	pathOffset := int(unsafe.Sizeof(*entryPtr))
	copy(tempData[*offset+pathOffset:], job.relPath)
	tempData[*offset+pathOffset+len(job.relPath)] = 0

	// Zero padding
	for i := 0; i < padding; i++ {
		tempData[*offset+totalSize+i] = 0
	}

	*offset += entrySize
	return entryPtr, nil
}

// Helper functions for sorting and comparison
func sortJobsByPath(jobs []fileJob) {
	for i := 0; i < len(jobs); i++ {
		for j := i + 1; j < len(jobs); j++ {
			if jobs[i].relPath > jobs[j].relPath {
				jobs[i], jobs[j] = jobs[j], jobs[i]
			}
		}
	}
}

func compareStrings(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
