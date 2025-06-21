package dircachefilehash

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"

	"github.com/google/vectorio"
	zcsl "github.com/mattkeenan/zerocopyskiplist"
	"golang.org/x/sys/unix"
)

const (
	MainContext  = "main"
	CacheContext = "cache"
	ScanContext  = "scan"
	TempContext  = "temp"
)

// LoadMainIndex loads the main index file into a skiplist with "main" context
func (dc *DirectoryCache) LoadMainIndex() (*SkiplistWrapper, error) {
	if _, err := os.Stat(dc.IndexFile); os.IsNotExist(err) {
		// Create empty main index if it doesn't exist
		if err := dc.createEmptyIndex(); err != nil {
			return nil, fmt.Errorf("failed to create empty main index: %w", err)
		}
	}

	skiplist := NewSkiplistWrapper(16, MainContext)

	// Save current skiplist
	oldSkiplist := dc.skiplist
	dc.skiplist = skiplist

	// Load the index
	if err := dc.loadIndexFromFile(dc.IndexFile); err != nil {
		dc.skiplist = oldSkiplist // Restore on error
		return nil, fmt.Errorf("failed to load main index: %w", err)
	}

	// Set all entries to main context
	tempSkiplist := NewSkiplistWrapper(16, MainContext)
	dc.skiplist.ForEach(func(entry *binaryEntry, context string) bool {
		tempSkiplist.Insert(entry, MainContext)
		return true
	})

	// Restore original skiplist and return the loaded one
	dc.skiplist = oldSkiplist
	return tempSkiplist, nil
}

// LoadCacheIndex loads the cache index file into a skiplist with "cache" context
func (dc *DirectoryCache) LoadCacheIndex() (*SkiplistWrapper, error) {
	if _, err := os.Stat(dc.CacheFile); os.IsNotExist(err) {
		return NewSkiplistWrapper(16, CacheContext), nil
	}

	// Create temporary cache instance to load from cache file
	tempCache := &DirectoryCache{
		RootDir:       dc.RootDir,
		IndexFile:     dc.CacheFile, // Point to cache file
		CacheFile:     dc.CacheFile,
		skiplist:      NewSkiplistWrapper(16, CacheContext),
		signature:     dc.signature,
		version:       dc.version,
		hasher:        dc.hasher,
		ignoreManager: dc.ignoreManager,
	}

	if err := tempCache.loadIndexFromFile(dc.CacheFile); err != nil {
		return nil, fmt.Errorf("failed to load cache index: %w", err)
	}

	// Set all entries to cache context
	result := NewSkiplistWrapper(16, CacheContext)
	tempCache.skiplist.ForEach(func(entry *binaryEntry, context string) bool {
		result.Insert(entry, CacheContext)
		return true
	})

	return result, nil
}

// CreateTmpIndexFromScan scans the directory and creates a temporary index using Hwang-Lin algorithm
// to efficiently compare against the provided comparison skiplist
func (dc *DirectoryCache) CreateTmpIndexFromScan(comparisonSkiplist *SkiplistWrapper) (*SkiplistWrapper, error) {
	// Collect all file jobs for scanning
	allJobs, err := dc.collectFileJobs([]string{dc.RootDir})
	if err != nil {
		return nil, fmt.Errorf("failed to collect file jobs: %w", err)
	}

	if len(allJobs) == 0 {
		return NewSkiplistWrapper(16, ScanContext), nil
	}

	result := NewSkiplistWrapper(16, ScanContext)

	// Calculate approximate size for temporary mmap
	estimatedSize := HeaderSize + ChecksumSize + (len(allJobs) * 512) // Conservative estimate

	// Create temporary mmap for building entries
	tempFile, err := os.CreateTemp(filepath.Dir(dc.IndexFile), "scan_temp_*.tmp")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	if err := tempFile.Truncate(int64(estimatedSize)); err != nil {
		return nil, fmt.Errorf("failed to truncate temp file: %w", err)
	}

	tempData, err := unix.Mmap(int(tempFile.Fd()), 0, estimatedSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return nil, fmt.Errorf("failed to mmap temp file: %w", err)
	}
	defer unix.Munmap(tempData)

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

	// Process files using Hwang-Lin algorithm with direct iteration (zero-copy)
	offset := 0

	// Sort jobs by relative path for Hwang-Lin merge
	sortJobsByPath(allJobs)

	// Use direct iteration instead of creating slices
	comparisonCurrent := comparisonSkiplist.skiplist.First()
	jobIndex := 0

	for jobIndex < len(allJobs) && comparisonCurrent != nil {
		job := allJobs[jobIndex]
		existing := comparisonCurrent.Item()

		cmp := compareStrings(job.relPath, existing.RelativePath())

		if cmp == 0 {
			// Same file - check if changed
			if dc.fileChangedFromJob(existing, job) {
				// File changed - create new entry and hash it
				entry, err := dc.createNewEntryFromJob(job, tempData, &offset, estimatedSize)
				if err != nil {
					close(jobChan)
					return nil, err
				}
				result.Insert(entry, ScanContext)

				// Queue for hashing
				jobChan <- hashJob{
					entry:    entry,
					filePath: job.path,
					deviceID: job.info.Sys().(*syscall.Stat_t).Dev,
				}
			} else {
				// File unchanged - reuse existing entry (zero-copy)
				result.Insert(existing, ScanContext)
			}
			jobIndex++
			comparisonCurrent = comparisonCurrent.Next()
		} else if cmp < 0 {
			// New file not in existing set - add
			entry, err := dc.createNewEntryFromJob(job, tempData, &offset, estimatedSize)
			if err != nil {
				close(jobChan)
				return nil, err
			}
			result.Insert(entry, ScanContext)

			// Queue for hashing
			jobChan <- hashJob{
				entry:    entry,
				filePath: job.path,
				deviceID: job.info.Sys().(*syscall.Stat_t).Dev,
			}
			jobIndex++
		} else {
			// Existing file not in current scan - skip (effectively deleted)
			comparisonCurrent = comparisonCurrent.Next()
		}
	}

	// Handle remaining new files
	for jobIndex < len(allJobs) {
		job := allJobs[jobIndex]
		entry, err := dc.createNewEntryFromJob(job, tempData, &offset, estimatedSize)
		if err != nil {
			close(jobChan)
			return nil, err
		}
		result.Insert(entry, ScanContext)

		// Queue for hashing
		jobChan <- hashJob{
			entry:    entry,
			filePath: job.path,
			deviceID: job.info.Sys().(*syscall.Stat_t).Dev,
		}
		jobIndex++
	}

	// Signal no more jobs and wait for completion
	close(jobChan)
	muxWg.Wait()
	close(resultChan)
	<-doneChan
	resultWg.Wait()

	return result, nil
}

// WriteSkiplistToTmpIndex writes a skiplist to a temporary index file using vectorio.WritevRaw
func (dc *DirectoryCache) WriteSkiplistToTmpIndex(skiplist *SkiplistWrapper, tmpFilePath string, context string) error {
	// Get Iovecs for entries matching the specified context
	var iovecs []syscall.Iovec
	if context == "" {
		// All entries
		iovecs = skiplist.ToIovecSlice()
	} else {
		// Only entries matching context
		iovecs = skiplist.ToContextIovecSlice(context)
	}

	if len(iovecs) == 0 {
		// Create empty index
		return dc.createEmptyIndexAt(tmpFilePath)
	}

	// Calculate total data size
	totalDataSize := 0
	for _, iovec := range iovecs {
		totalDataSize += int(iovec.Len)
	}
	totalSize := HeaderSize + totalDataSize + ChecksumSize

	// Create the file
	file, err := os.Create(tmpFilePath)
	if err != nil {
		return fmt.Errorf("failed to create temp index file %s: %w", tmpFilePath, err)
	}
	defer file.Close()

	// Truncate to exact size
	if err := file.Truncate(int64(totalSize)); err != nil {
		return fmt.Errorf("failed to truncate file: %w", err)
	}

	// Memory map the file
	data, err := unix.Mmap(int(file.Fd()), 0, totalSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return fmt.Errorf("failed to mmap file: %w", err)
	}
	defer unix.Munmap(data)

	// Write header
	header := (*IndexHeader)(unsafe.Pointer(&data[0]))
	entryCount := uint32(len(iovecs))
	flags := uint32(0)
	if context != MainContext {
		flags |= IndexFlagSparse // Mark as sparse if not main index
	}
	header.SetHeader(dc.signature, dc.version, entryCount, flags)

	// Seek to the position after the header before writing entries
	_, err = file.Seek(int64(HeaderSize), 0)
	if err != nil {
		return fmt.Errorf("failed to seek to entry position: %w", err)
	}

	// Write entries using vectorio.WritevRaw in chunks
	const maxIovecsPerWrite = 1024

	totalWritten := 0
	for i := 0; i < len(iovecs); i += maxIovecsPerWrite {
		end := i + maxIovecsPerWrite
		if end > len(iovecs) {
			end = len(iovecs)
		}

		chunk := iovecs[i:end]
		written, err := vectorio.WritevRaw(uintptr(file.Fd()), chunk)
		if err != nil {
			return fmt.Errorf("failed to write chunk at position %d: %w", i, err)
		}
		totalWritten += written
	}

	// Verify we wrote the expected amount
	if totalWritten != totalDataSize {
		return fmt.Errorf("write size mismatch: expected %d, wrote %d", totalDataSize, totalWritten)
	}

	// Calculate and write checksum
	checksum := dc.calculateChecksum(data[:HeaderSize+totalDataSize])
	copy(data[HeaderSize+totalDataSize:], checksum)

	// Sync to disk
	if err := unix.Msync(data, unix.MS_SYNC); err != nil {
		return fmt.Errorf("failed to sync mmap: %w", err)
	}

	return nil
}

// UpdateCacheIndexWithWorkflow implements the cache update workflow as specified
func (dc *DirectoryCache) UpdateCacheIndexWithWorkflow() error {
	// Step 1: Load main index
	mainSkiplist, err := dc.LoadMainIndex()
	if err != nil {
		return fmt.Errorf("failed to load main index: %w", err)
	}

	// Step 2: Load current cache index
	cacheSkiplist, err := dc.LoadCacheIndex()
	if err != nil {
		return fmt.Errorf("failed to load cache index: %w", err)
	}

	// Step 3: Make a copy of the main index skiplist
	workingSkiplist := mainSkiplist.Copy()

	// Step 4: Merge the cache index skiplist
	if err := workingSkiplist.Merge(cacheSkiplist, zcsl.MergeTheirs); err != nil {
		return fmt.Errorf("failed to merge cache with main index: %w", err)
	}

	// Step 5: Create tmp index from scan using Hwang-Lin algorithm
	scanSkiplist, err := dc.CreateTmpIndexFromScan(workingSkiplist)
	if err != nil {
		return fmt.Errorf("failed to create scan index: %w", err)
	}

	// Steps 6-8 are handled inside CreateTmpIndexFromScan (Hwang-Lin, hashing, waiting)

	// Step 9: Get Iovecs for entries not in main context (i.e., cache entries)
	notMainIovecs := scanSkiplist.ToNotContextIovecSlice(MainContext)

	// If no cache entries, remove cache file
	if len(notMainIovecs) == 0 {
		os.Remove(dc.CacheFile)
		return nil
	}

	// Step 10: Create temporary cache index file and write using vectorio.WritevRaw
	tempCachePath := dc.generateTempFileName("cache")

	// Create cache skiplist from scan results (entries not in main)
	cacheOnlySkiplist := scanSkiplist.FilterNotByContext(MainContext)

	if err := dc.WriteSkiplistToTmpIndex(cacheOnlySkiplist, tempCachePath, CacheContext); err != nil {
		os.Remove(tempCachePath)
		return fmt.Errorf("failed to write cache index: %w", err)
	}

	// Step 11: Rename tmp index file over cache index file
	if err := os.Rename(tempCachePath, dc.CacheFile); err != nil {
		os.Remove(tempCachePath)
		return fmt.Errorf("failed to rename cache file: %w", err)
	}

	return nil
}

// createEmptyIndexAt creates an empty index file at the specified path
func (dc *DirectoryCache) createEmptyIndexAt(filePath string) error {
	totalSize := HeaderSize + ChecksumSize

	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create index file %s: %w", filePath, err)
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
	header.SetHeader(dc.signature, dc.version, 0, 0)

	// Write checksum
	checksum := dc.calculateChecksum(data[:HeaderSize])
	copy(data[HeaderSize:], checksum)

	if err := unix.Msync(data, unix.MS_SYNC); err != nil {
		return fmt.Errorf("failed to sync mmap: %w", err)
	}

	return nil
}
