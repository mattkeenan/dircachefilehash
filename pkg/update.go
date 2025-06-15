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

	"golang.org/x/sys/unix"
)

// Update scans the directory and updates the index file
func (dc *DirectoryCache) Update(paths ...string) error {
	if len(paths) == 0 {
		paths = []string{dc.RootDir}
	}

	jobs, err := dc.collectFileJobs(paths)
	if err != nil {
		return fmt.Errorf("failed to collect file jobs: %w", err)
	}

	if err := dc.WriteIndex(jobs); err != nil {
		return fmt.Errorf("failed to write index: %w", err)
	}

	return nil
}

// ScanDirectory scans the directory and creates file jobs for parallel processing
func (dc *DirectoryCache) ScanDirectory() error {
	jobs, err := dc.collectFileJobs([]string{dc.RootDir})
	if err != nil {
		return err
	}

	return dc.WriteIndex(jobs)
}

// collectFileJobs collects file jobs from specified paths
func (dc *DirectoryCache) collectFileJobs(paths []string) ([]fileJob, error) {
	var fileJobs []fileJob
	jobIndex := 0

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

			relPath, err := filepath.Rel(dc.RootDir, currentPath)
			if err != nil {
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
	entry *binaryEntry
	hash  [20]byte
	err   error
}

// UpdatePaths updates only the specified paths with truly parallel processing
func (dc *DirectoryCache) UpdatePaths(paths []string) error {
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

	// Write temp header
	tempHeader := (*IndexHeader)(unsafe.Pointer(&tempData[0]))
	tempHeader.Signature = dc.signature
	tempHeader.Version = dc.version
	tempHeader.EntryCount = 0

	// Setup channels for worker communication
	jobChan := make(chan hashJob, 100)
	resultChan := make(chan hashResult, 100)
	doneChan := make(chan struct{})

	// Start workerMuxHandler BEFORE scanning paths
	var muxWg sync.WaitGroup
	muxWg.Add(1)
	go func() {
		defer muxWg.Done()
		dc.workerMuxHandler(jobChan, resultChan)
	}()

	// Start result handler
	var resultWg sync.WaitGroup
	resultWg.Add(1)
	go func() {
		defer resultWg.Done()
		dc.handleHashResults(resultChan, doneChan)
	}()

	// Load existing index for comparison
	existingEntries := make(map[string]*binaryEntry)
	if err := dc.LoadIndex(); err == nil {
		for _, entry := range dc.entries {
			existingEntries[entry.RelativePath()] = entry
		}
	}

	// Process paths with parallel hashing
	offset := HeaderSize
	entryCount := uint32(0)

	for _, inputPath := range paths {
		absPath := inputPath
		if !filepath.IsAbs(inputPath) {
			absPath = filepath.Join(dc.RootDir, inputPath)
		}
		absPath = filepath.Clean(absPath)

		if err := dc.processPathParallel(absPath, existingEntries, tempData, &offset, &entryCount,
			estimatedSize, jobChan); err != nil {
			close(jobChan)
			return err
		}
	}

	// Signal no more jobs and wait for completion
	close(jobChan)
	muxWg.Wait() // Wait for mux handler to finish
	<-doneChan   // Wait for result handler to finish
	resultWg.Wait()

	// Update header with final count
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

	return nil
}

// processPathParallel processes a single path with parallel hashing
func (dc *DirectoryCache) processPathParallel(absPath string, existingEntries map[string]*binaryEntry,
	tempData []byte, offset *int, entryCount *uint32, maxSize int, jobChan chan<- hashJob) error {

	info, err := os.Lstat(absPath)
	if err != nil {
		return nil // Skip inaccessible paths
	}

	relPath, err := filepath.Rel(dc.RootDir, absPath)
	if err != nil {
		return fmt.Errorf("path %s is not under root directory %s", absPath, dc.RootDir)
	}

	if info.IsDir() {
		return dc.scanDirParallel(absPath, existingEntries, tempData, offset, entryCount, maxSize, jobChan)
	} else if info.Mode().IsRegular() {
		if absPath == dc.IndexFile {
			return nil
		}
		return dc.processFileParallel(absPath, relPath, info, existingEntries, tempData, offset, entryCount, maxSize, jobChan)
	}

	return nil
}

// scanDirParallel scans directory recursively with parallel processing
func (dc *DirectoryCache) scanDirParallel(rootPath string, existingEntries map[string]*binaryEntry,
	tempData []byte, offset *int, entryCount *uint32, maxSize int, jobChan chan<- hashJob) error {

	pathQueue := []string{rootPath}

	for len(pathQueue) > 0 {
		currentPath := pathQueue[0]
		pathQueue = pathQueue[1:]

		info, err := os.Lstat(currentPath)
		if err != nil {
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

			relPath, err := filepath.Rel(dc.RootDir, currentPath)
			if err != nil {
				continue
			}

			if err := dc.processFileParallel(currentPath, relPath, info, existingEntries, tempData,
				offset, entryCount, maxSize, jobChan); err != nil {
				return err
			}
		}
	}

	return nil
}

// processFileParallel processes a single file with parallel hashing
func (dc *DirectoryCache) processFileParallel(filePath, relPath string, info os.FileInfo,
	existingEntries map[string]*binaryEntry, tempData []byte, offset *int, entryCount *uint32,
	maxSize int, jobChan chan<- hashJob) error {

	stat := info.Sys().(*syscall.Stat_t)

	// Check if file changed compared to existing entry
	if existing, exists := existingEntries[relPath]; exists {
		if dc.fileUnchanged(existing, info, stat) {
			// Copy unchanged entry to temp index
			existingSize := existing.EntrySize()
			if *offset+existingSize > maxSize-ChecksumSize {
				return fmt.Errorf("temp file too small")
			}

			// Create entry in temp mmap and copy data
			entryPtr := (*binaryEntry)(unsafe.Pointer(&tempData[*offset]))
			*entryPtr = *existing

			// Copy path data
			pathOffset := int(unsafe.Sizeof(*entryPtr))
			copy(tempData[*offset+pathOffset:], existing.RelativePathBytes())
			tempData[*offset+pathOffset+len(relPath)] = 0

			*offset += existingSize
			*entryCount++
			return nil
		}
	}

	// File is new or changed - create entry and queue for hashing
	entrySize := int(unsafe.Sizeof(binaryEntry{})) + len(relPath) + 1
	padding := (8 - (entrySize % 8)) % 8
	totalSize := entrySize + padding

	if *offset+totalSize > maxSize-ChecksumSize {
		return fmt.Errorf("temp file too small")
	}

	// Create entry in temp mmap
	entryPtr := (*binaryEntry)(unsafe.Pointer(&tempData[*offset]))
	entryPtr.CTimeWall = encodeWallTime(stat.Ctim.Sec, stat.Ctim.Nsec)
	entryPtr.MTimeWall = encodeWallTime(stat.Mtim.Sec, stat.Mtim.Nsec)
	entryPtr.Dev = uint32(stat.Dev)
	entryPtr.Ino = uint32(stat.Ino)
	entryPtr.Mode = uint32(info.Mode())
	entryPtr.UID = stat.Uid
	entryPtr.GID = stat.Gid
	entryPtr.Size = uint32(info.Size())
	entryPtr.Flags = uint16(len(relPath))
	entryPtr.PathLen = uint16(len(relPath))
	// Hash will be filled by worker

	// Write path
	pathOffset := int(unsafe.Sizeof(*entryPtr))
	copy(tempData[*offset+pathOffset:], relPath)
	tempData[*offset+pathOffset+len(relPath)] = 0

	// Zero padding
	for i := 0; i < padding; i++ {
		tempData[*offset+entrySize+i] = 0
	}

	// Queue for hashing
	job := hashJob{
		entry:    entryPtr,
		filePath: filePath,
		deviceID: stat.Dev,
	}

	select {
	case jobChan <- job:
	default:
		return fmt.Errorf("job channel full")
	}

	*offset += totalSize
	*entryCount++
	return nil
}

// fileUnchanged checks if file metadata indicates no changes
func (dc *DirectoryCache) fileUnchanged(existing *binaryEntry, info os.FileInfo, stat *syscall.Stat_t) bool {
	if existing.Size != uint32(info.Size()) {
		return false
	}
	if existing.UID != stat.Uid || existing.GID != stat.Gid {
		return false
	}

	currentCTime := encodeWallTime(stat.Ctim.Sec, stat.Ctim.Nsec)
	currentMTime := encodeWallTime(stat.Mtim.Sec, stat.Mtim.Nsec)

	return existing.CTimeWall == currentCTime && existing.MTimeWall == currentMTime
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

// deviceHashWorkerNew processes hash jobs for a device
func (dc *DirectoryCache) deviceHashWorkerNew(jobChan <-chan hashJob, resultChan chan<- hashResult) {
	for job := range jobChan {
		hashStr, err := dc.hashFile(job.filePath)

		var hash [20]byte
		if err == nil {
			hashBytes, hexErr := hex.DecodeString(hashStr)
			if hexErr == nil && len(hashBytes) == 20 {
				copy(hash[:], hashBytes)
			} else {
				err = hexErr
			}
		}

		result := hashResult{
			entry: job.entry,
			hash:  hash,
			err:   err,
		}

		resultChan <- result
		// Clear reference to prevent memory leak
		job.entry = nil
	}
}

// handleHashResults processes hash results and updates entries
func (dc *DirectoryCache) handleHashResults(resultChan <-chan hashResult, doneChan chan<- struct{}) {
	for result := range resultChan {
		if result.err != nil {
			fmt.Fprintf(os.Stderr, "Warning: hash error: %v\n", result.err)
			continue
		}

		// Update hash in-place via pointer
		result.entry.Hash = result.hash

		// Clear reference to prevent memory leak
		result.entry = nil
	}

	close(doneChan)
}

// processFilesParallelResults processes files using device-specific worker pools
func (dc *DirectoryCache) processFilesParallelResults(jobs []fileJob) ([]fileResult, error) {
	if len(jobs) == 0 {
		return nil, nil
	}

	// Group jobs by device
	deviceJobs := make(map[uint64][]fileJob)
	for _, job := range jobs {
		stat := job.info.Sys().(*syscall.Stat_t)
		deviceID := stat.Dev
		deviceJobs[deviceID] = append(deviceJobs[deviceID], job)
	}

	resultChan := make(chan fileResult, len(jobs))
	var wg sync.WaitGroup

	for deviceID, jobs := range deviceJobs {
		wg.Add(1)
		go func(devID uint64, deviceJobs []fileJob) {
			defer wg.Done()
			dc.processDeviceJobs(devID, deviceJobs, resultChan)
		}(deviceID, jobs)
	}

	wg.Wait()
	close(resultChan)

	var results []fileResult
	for result := range resultChan {
		results = append(results, result)
	}

	return results, nil
}

// processDeviceJobs processes jobs for a specific device using 2 workers
func (dc *DirectoryCache) processDeviceJobs(deviceID uint64, jobs []fileJob, resultChan chan<- fileResult) {
	numWorkers := 2
	if numWorkers > len(jobs) {
		numWorkers = len(jobs)
	}
	if numWorkers < 1 {
		numWorkers = 1
	}

	jobChan := make(chan fileJob, len(jobs))
	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go dc.deviceHashWorker(deviceID, jobChan, resultChan, &wg)
	}

	for _, job := range jobs {
		jobChan <- job
	}
	close(jobChan)

	wg.Wait()
}

// deviceHashWorker processes file hashing jobs for a specific device
func (dc *DirectoryCache) deviceHashWorker(deviceID uint64, jobs <-chan fileJob, results chan<- fileResult, wg *sync.WaitGroup) {
	defer wg.Done()

	for job := range jobs {
		hash, stat, err := dc.processFileJob(job)
		results <- fileResult{
			entry: nil, // We'll create the binaryEntry directly in mmap
			err:   err,
			index: job.index,
		}
		_ = hash
		_ = stat
	}
}
