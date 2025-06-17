package dircachefilehash

import (
	"fmt"
	"os"
	"strings"
)

// FileStatus represents the status of a file
type FileStatus int

const (
	StatusUnchanged FileStatus = iota
	StatusModified
	StatusAdded
	StatusDeleted
)

// StatusResult represents the result of a status check
type StatusResult struct {
	Modified []string
	Added    []string
	Deleted  []string
}

// Status compares the current directory state with the loaded index using context-aware cache management
func (dc *DirectoryCache) Status() (*StatusResult, error) {
	// Load main index with context
	if dc.skiplist.IsEmpty() {
		if err := dc.LoadIndex(dc.IndexFile, "main"); err != nil {
			return nil, fmt.Errorf("failed to load index: %w", err)
		}
	}

	// Update cache index with current state using context-aware logic
	if err := dc.UpdateCacheIndex(); err != nil {
		return nil, fmt.Errorf("failed to update cache index: %w", err)
	}

	// Load cache index with context
	cacheSkiplist, err := dc.LoadCacheIndex("cache")
	if err != nil {
		return nil, fmt.Errorf("failed to load cache index: %w", err)
	}

	// Create combined view: main index + cache index for complete current state
	workingSkiplist := dc.skiplist.Copy("main")
	if err := workingSkiplist.Merge(cacheSkiplist); err != nil {
		return nil, fmt.Errorf("failed to merge cache with main index: %w", err)
	}

	// Scan current directory state for comparison with context
	currentSkiplist, err := dc.scanPathsToSkiplist([]string{dc.RootDir}, "current")
	if err != nil {
		return nil, fmt.Errorf("failed to scan directory: %w", err)
	}

	result := &StatusResult{
		Modified: make([]string, 0),
		Added:    make([]string, 0),
		Deleted:  make([]string, 0),
	}

	// Use Hwang-Lin merge algorithm to compare states (all context-aware)
	dc.hwangLinStatus(workingSkiplist, currentSkiplist, func(status FileStatus, path string, indexEntry, diskEntry *binaryEntry) {
		switch status {
		case StatusModified:
			result.Modified = append(result.Modified, path)
		case StatusAdded:
			result.Added = append(result.Added, path)
		case StatusDeleted:
			result.Deleted = append(result.Deleted, path)
		}
	})

	return result, nil
}

// UpdateCacheIndex updates the cache index with current state following the exclusive cache design
func (dc *DirectoryCache) UpdateCacheIndex() error {
	// Step 1: Load main index with context tracking
	if dc.skiplist.IsEmpty() {
		if err := dc.LoadIndex(dc.IndexFile, "main"); err != nil {
			return fmt.Errorf("failed to load main index: %w", err)
		}
	}

	// Step 2: Copy main index skiplist with main context
	mainSkiplist := dc.skiplist.Copy("main")

	// Step 3: Load and merge existing cache if present
	if _, err := os.Stat(dc.CacheFile); err == nil {
		cacheSkiplist, err := dc.LoadCacheIndex("cache")
		if err != nil {
			return fmt.Errorf("failed to load existing cache: %w", err)
		}

		// Merge existing cache into working skiplist
		if err := mainSkiplist.Merge(cacheSkiplist); err != nil {
			return fmt.Errorf("failed to merge existing cache: %w", err)
		}
	}

	// Step 4: Scan current directory state into temporary skiplist
	tempScanSkiplist, err := dc.scanPathsToSkiplist([]string{dc.RootDir}, "scan")
	if err != nil {
		return fmt.Errorf("failed to scan directory: %w", err)
	}

	// Step 5: Merge scan results into working skiplist
	if err := mainSkiplist.Merge(tempScanSkiplist); err != nil {
		return fmt.Errorf("failed to merge scan results: %w", err)
	}

	// Step 6: Filter out entries that have "main" context (exclusive cache)
	exclusiveCacheSkiplist := mainSkiplist.FilterExcluding("main")

	// Step 7: Write new cache index
	if !exclusiveCacheSkiplist.IsEmpty() {
		tempCachePath := dc.generateTempFileName("cache")
		if err := dc.writeSparseIndexFromSkiplist(exclusiveCacheSkiplist, tempCachePath); err != nil {
			return fmt.Errorf("failed to write cache index: %w", err)
		}

		// Atomic rename
		if err := os.Rename(tempCachePath, dc.CacheFile); err != nil {
			os.Remove(tempCachePath) // Cleanup on failure
			return fmt.Errorf("failed to rename cache file: %w", err)
		}
	} else {
		// No cache entries needed, ensure cache file doesn't exist
		os.Remove(dc.CacheFile)
	}

	return nil
}

// LoadIndex loads an index file and sets the context for all entries
func (dc *DirectoryCache) LoadIndex(filePath, context string) error {
	// Save current skiplist
	oldSkiplist := dc.skiplist

	// Create new skiplist with context
	dc.skiplist = NewSkiplistWrapper(16, context)

	// Load the index
	if err := dc.loadIndexFromFile(filePath); err != nil {
		dc.skiplist = oldSkiplist // Restore on error
		return err
	}

	return nil
}

// LoadCacheIndex loads the cache index file with context
func (dc *DirectoryCache) LoadCacheIndex(context string) (*SkiplistWrapper, error) {
	if _, err := os.Stat(dc.CacheFile); os.IsNotExist(err) {
		return NewSkiplistWrapper(16, context), nil
	}

	// Temporarily create a cache instance to load from cache file
	tempCache := NewDirectoryCache(dc.RootDir, "")
	tempCache.IndexFile = dc.CacheFile
	tempCache.skiplist = NewSkiplistWrapper(16, context)

	if err := tempCache.loadIndexFromFile(dc.CacheFile); err != nil {
		return nil, err
	}

	// Return the loaded skiplist
	return tempCache.skiplist.Copy(context), nil
}

// scanPathsToSkiplist scans paths and returns a skiplist with context
func (dc *DirectoryCache) scanPathsToSkiplist(paths []string, context string) (*SkiplistWrapper, error) {
	jobs, err := dc.collectFileJobs(paths)
	if err != nil {
		return nil, fmt.Errorf("failed to collect file jobs: %w", err)
	}

	if len(jobs) == 0 {
		return NewSkiplistWrapper(16, context), nil
	}

	// Create temporary index file for the scan
	tempScanPath := dc.generateTempFileName("scan")

	// Create temporary cache to write the jobs
	tempCache := NewDirectoryCache(dc.RootDir, "")
	tempCache.IndexFile = tempScanPath
	tempCache.skiplist = NewSkiplistWrapper(16, context)

	// Write jobs to temporary index file
	if err := tempCache.WriteIndex(jobs); err != nil {
		return nil, fmt.Errorf("failed to write temp scan index: %w", err)
	}

	// Load the temporary index with context
	if err := tempCache.LoadIndex(tempScanPath, context); err != nil {
		os.Remove(tempScanPath)
		return nil, fmt.Errorf("failed to load temp scan index: %w", err)
	}

	// Return the skiplist with proper context
	result := tempCache.skiplist.Copy(context)
	return result, nil
}

// StatusWithCallback compares directory state using Hwang-Lin algorithm with callback for zero-copy operation
func (dc *DirectoryCache) StatusWithCallback(callback func(status FileStatus, path string, indexEntry, diskEntry *binaryEntry)) error {
	// Load main index with context
	if dc.skiplist.IsEmpty() {
		if err := dc.LoadIndex(dc.IndexFile, "main"); err != nil {
			return fmt.Errorf("failed to load index: %w", err)
		}
	}

	// Update cache index first (status operation)
	if err := dc.UpdateCacheIndex(); err != nil {
		return fmt.Errorf("failed to update cache index: %w", err)
	}

	// Load cache index with context
	cacheSkiplist, err := dc.LoadCacheIndex("cache")
	if err != nil {
		return fmt.Errorf("failed to load cache index: %w", err)
	}

	// Create combined view: main index + cache index for complete current state
	workingSkiplist := dc.skiplist.Copy("main")
	if err := workingSkiplist.Merge(cacheSkiplist); err != nil {
		return fmt.Errorf("failed to merge cache with main index: %w", err)
	}

	// Scan current directory state using context-aware entries only
	currentSkiplist, err := dc.scanPathsToSkiplist([]string{dc.RootDir}, "current")
	if err != nil {
		return fmt.Errorf("failed to scan directory: %w", err)
	}

	// Use Hwang-Lin algorithm directly with context-aware entries
	dc.hwangLinStatus(workingSkiplist, currentSkiplist, callback)
	return nil
}

// hwangLinStatus implements the Hwang-Lin merge algorithm for comparing two sorted skiplists
func (dc *DirectoryCache) hwangLinStatus(indexSkiplist, diskSkiplist *SkiplistWrapper,
	callback func(status FileStatus, path string, indexEntry, diskEntry *binaryEntry)) {

	// Get sorted entries from both skiplists (zero-copy)
	indexEntries := indexSkiplist.GetSortedEntries()
	diskEntries := diskSkiplist.GetSortedEntries()

	i, j := 0, 0

	// Hwang-Lin merge
	for i < len(indexEntries) && j < len(diskEntries) {
		indexEntry := indexEntries[i]
		diskEntry := diskEntries[j]

		// Skip deleted entries from index
		if indexEntry.IsDeleted() {
			i++
			continue
		}

		cmp := strings.Compare(indexEntry.RelativePath(), diskEntry.RelativePath())

		if cmp == 0 {
			// Same file - check if modified
			if dc.isFileModified(indexEntry, diskEntry) {
				callback(StatusModified, indexEntry.RelativePath(), indexEntry, diskEntry)
			} else {
				callback(StatusUnchanged, indexEntry.RelativePath(), indexEntry, diskEntry)
			}
			i++
			j++
		} else if cmp < 0 {
			// File exists in index but not on disk - deleted
			callback(StatusDeleted, indexEntry.RelativePath(), indexEntry, nil)
			i++
		} else {
			// File exists on disk but not in index - added
			callback(StatusAdded, diskEntry.RelativePath(), nil, diskEntry)
			j++
		}
	}

	// Handle remaining entries from index (all deleted)
	for i < len(indexEntries) {
		if !indexEntries[i].IsDeleted() {
			callback(StatusDeleted, indexEntries[i].RelativePath(), indexEntries[i], nil)
		}
		i++
	}

	// Handle remaining entries from disk (all added)
	for j < len(diskEntries) {
		callback(StatusAdded, diskEntries[j].RelativePath(), nil, diskEntries[j])
		j++
	}
}

// isFileModified checks if a file has been modified using fast metadata comparison
func (dc *DirectoryCache) isFileModified(indexEntry, diskEntry *binaryEntry) bool {
	// Quick size check
	if indexEntry.FileSize != diskEntry.FileSize {
		return true
	}

	// Check ownership
	if indexEntry.UID != diskEntry.UID || indexEntry.GID != diskEntry.GID {
		return true
	}

	// Check timestamps using wall time
	indexCTime := timeFromWall(indexEntry.CTimeWall)
	diskCTime := timeFromWall(diskEntry.CTimeWall)
	if indexCTime.Unix() != diskCTime.Unix() || indexCTime.Nanosecond() != diskCTime.Nanosecond() {
		return true
	}

	indexMTime := timeFromWall(indexEntry.MTimeWall)
	diskMTime := timeFromWall(diskEntry.MTimeWall)
	if indexMTime.Unix() != diskMTime.Unix() || indexMTime.Nanosecond() != diskMTime.Nanosecond() {
		return true
	}

	return false
}

// GetModifiedFiles returns only the paths of modified files using Hwang-Lin algorithm
func (dc *DirectoryCache) GetModifiedFiles() ([]string, error) {
	var modified []string

	err := dc.StatusWithCallback(func(status FileStatus, path string, indexEntry, diskEntry *binaryEntry) {
		if status == StatusModified {
			modified = append(modified, path)
		}
	})

	return modified, err
}

// GetAddedFiles returns only the paths of added files using Hwang-Lin algorithm
func (dc *DirectoryCache) GetAddedFiles() ([]string, error) {
	var added []string

	err := dc.StatusWithCallback(func(status FileStatus, path string, indexEntry, diskEntry *binaryEntry) {
		if status == StatusAdded {
			added = append(added, path)
		}
	})

	return added, err
}

// GetDeletedFiles returns only the paths of deleted files using Hwang-Lin algorithm
func (dc *DirectoryCache) GetDeletedFiles() ([]string, error) {
	var deleted []string

	err := dc.StatusWithCallback(func(status FileStatus, path string, indexEntry, diskEntry *binaryEntry) {
		if status == StatusDeleted {
			deleted = append(deleted, path)
		}
	})

	return deleted, err
}

// HasChanges returns true if there are any changes
func (sr *StatusResult) HasChanges() bool {
	return len(sr.Modified) > 0 || len(sr.Added) > 0 || len(sr.Deleted) > 0
}

// TotalChanges returns the total number of changed files
func (sr *StatusResult) TotalChanges() int {
	return len(sr.Modified) + len(sr.Added) + len(sr.Deleted)
}

// HasChangesQuick performs a quick check for any changes using Hwang-Lin algorithm without collecting all results
func (dc *DirectoryCache) HasChangesQuick() (bool, error) {
	hasChanges := false

	err := dc.StatusWithCallback(func(status FileStatus, path string, indexEntry, diskEntry *binaryEntry) {
		if status != StatusUnchanged {
			hasChanges = true
		}
	})

	return hasChanges, err
}
