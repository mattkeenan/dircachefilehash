package dircachefilehash

import (
	"fmt"
	"os"
)

// LoadMainIndex loads the main index file into a skiplist with "main" context
func (dc *DirectoryCache) LoadMainIndex() (*SkiplistWrapper, error) {
	if _, err := os.Stat(dc.IndexFile); os.IsNotExist(err) {
		// Create empty main index if it doesn't exist
		if err := dc.createEmptyIndex(); err != nil {
			return nil, fmt.Errorf("failed to create empty main index: %w", err)
		}
	}

	// Load entries from file using pure file I/O
	entries, err := dc.LoadIndexFromFile(dc.IndexFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load main index: %w", err)
	}

	// Create skiplist and insert all entries with main context
	skiplist := NewSkiplistWrapper(16, MainContext)
	for _, entry := range entries {
		skiplist.Insert(entry, MainContext)
	}

	return skiplist, nil
}

// LoadCacheIndex loads the cache index file into a skiplist with "cache" context
func (dc *DirectoryCache) LoadCacheIndex() (*SkiplistWrapper, error) {
	if _, err := os.Stat(dc.CacheFile); os.IsNotExist(err) {
		return NewSkiplistWrapper(16, CacheContext), nil
	}

	// Load entries from file using pure file I/O
	entries, err := dc.LoadIndexFromFile(dc.CacheFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load cache index: %w", err)
	}

	// Create skiplist and insert all entries with cache context
	skiplist := NewSkiplistWrapper(16, CacheContext)
	for _, entry := range entries {
		skiplist.Insert(entry, CacheContext)
	}

	return skiplist, nil
}


// CreateTmpIndexFromScan scans the directory and creates a temporary index using the new scan workflow
func (dc *DirectoryCache) CreateTmpIndexFromScan(comparisonSkiplist *SkiplistWrapper) (*SkiplistWrapper, error) {
	// Use the new PerformHwangLinScanToSkiplist workflow
	scanSkiplist, err := dc.PerformHwangLinScanToSkiplist([]string{}, comparisonSkiplist)
	if err != nil {
		return nil, fmt.Errorf("failed to perform scan to skiplist: %w", err)
	}

	return scanSkiplist, nil
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
	if err := workingSkiplist.Merge(cacheSkiplist, MergeTheirs); err != nil {
		return fmt.Errorf("failed to merge cache with main index: %w", err)
	}

	// Step 5: Create tmp index from scan using Hwang-Lin algorithm
	scanSkiplist, err := dc.CreateTmpIndexFromScan(workingSkiplist)
	if err != nil {
		return fmt.Errorf("failed to create scan index: %w", err)
	}

	// Steps 6-8 are handled inside CreateTmpIndexFromScan (Hwang-Lin, hashing, waiting)

	// Step 9: Filter cache entries (entries not in main context)
	cacheOnlySkiplist := scanSkiplist.FilterNotByContext(MainContext)

	// If no cache entries, remove cache file
	if cacheOnlySkiplist.IsEmpty() {
		os.Remove(dc.CacheFile)
		return nil
	}

	// Step 10 & 11: Write cache index using vectorio with atomic rename
	tempCachePath := dc.generateTempFileName("cache")

	// Write cache using vectorio for efficient bulk writes
	if err := dc.WriteSkiplistWithVectorIO(cacheOnlySkiplist, tempCachePath, CacheContext); err != nil {
		os.Remove(tempCachePath)
		return fmt.Errorf("failed to write cache index: %w", err)
	}

	// Atomic replace cache file
	if err := os.Rename(tempCachePath, dc.CacheFile); err != nil {
		os.Remove(tempCachePath) // Cleanup on failure
		return fmt.Errorf("failed to rename cache file: %w", err)
	}

	return nil
}
