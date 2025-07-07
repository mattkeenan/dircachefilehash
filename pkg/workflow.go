package dircachefilehash

import (
	"fmt"
	"os"
)

// LoadMainIndex loads the main index file into a skiplist with "main" context
func (dc *DirectoryCache) LoadMainIndex() (*skiplistWrapper, error) {
	if _, err := os.Stat(dc.IndexFile); os.IsNotExist(err) {
		// Create empty main index if it doesn't exist
		if err := dc.createEmptyIndex(); err != nil {
			return nil, fmt.Errorf("failed to create empty main index: %w", err)
		}
	}

	// Load entries from file as binaryEntryRef instances
	refs, err := dc.loadIndexFromFile(dc.IndexFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load main index: %w", err)
	}

	// Create skiplist and insert all entries with main context
	skiplist := NewSkiplistWrapper(16, MainContext)
	for _, ref := range refs {
		skiplist.Insert(ref, MainContext)
	}

	return skiplist, nil
}

// LoadCacheIndex loads the cache index file into a skiplist with "cache" context
func (dc *DirectoryCache) loadCacheIndex() (*skiplistWrapper, error) {
	if _, err := os.Stat(dc.CacheFile); os.IsNotExist(err) {
		return NewSkiplistWrapper(16, CacheContext), nil
	}

	// Load entries from file as binaryEntryRef instances
	refs, err := dc.loadIndexFromFile(dc.CacheFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load cache index: %w", err)
	}

	// Create skiplist and insert all entries with cache context
	skiplist := NewSkiplistWrapper(16, CacheContext)
	for _, ref := range refs {
		skiplist.Insert(ref, CacheContext)
	}

	return skiplist, nil
}

// CreateTmpIndexFromScan scans the directory and creates a temporary index using the new scan workflow
func (dc *DirectoryCache) createTmpIndexFromScan(shutdownChan <-chan struct{}, comparisonSkiplist *skiplistWrapper) (*skiplistWrapper, error) {
	// Use the new PerformHwangLinScanToSkiplist workflow
	scanSkiplist, err := dc.performHwangLinScanToSkiplist(shutdownChan, []string{}, comparisonSkiplist)
	// Pass through both the skiplist and error - the caller will decide if partial data is acceptable
	return scanSkiplist, err
}

// UpdateCacheIndexWithWorkflow implements the cache update workflow as specified
func (dc *DirectoryCache) updateCacheIndexWithWorkflow(shutdownChan <-chan struct{}) (*skiplistWrapper, error) {
	defer VerboseEnter()()
	// Step 1: Load main index
	mainSkiplist, err := dc.LoadMainIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to load main index: %w", err)
	}

	// Step 2: Load current cache index
	cacheSkiplist, err := dc.loadCacheIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to load cache index: %w", err)
	}

	// Step 3: Make a copy of the main index skiplist
	workingSkiplist := mainSkiplist.Copy()

	// Step 4: Merge the cache index skiplist
	if err := workingSkiplist.Merge(cacheSkiplist, MergeTheirs); err != nil {
		return nil, fmt.Errorf("failed to merge cache with main index: %w", err)
	}

	// Step 5: Create tmp index from scan using Hwang-Lin algorithm
	scanSkiplist, err := dc.createTmpIndexFromScan(shutdownChan, workingSkiplist)
	if err != nil && scanSkiplist == nil {
		// Only return error if we got no data at all
		return nil, fmt.Errorf("failed to create scan index: %w", err)
	}
	// If we have partial data due to interruption, continue with what we have
	if err != nil && IsDebugEnabled("scan") {
		fmt.Fprintf(os.Stderr, "[WORKFLOW] Scan interrupted, continuing with partial data (%d entries)\n", scanSkiplist.Length())
	}

	// Steps 6-8 are handled inside CreateTmpIndexFromScan (Hwang-Lin, hashing, waiting)

	// Step 9: Filter cache entries (entries not in main context)
	cacheOnlySkiplist := scanSkiplist.FilterNotByContext(MainContext)

	// If no cache entries, remove cache file
	if cacheOnlySkiplist.IsEmpty() {
		if IsDebugEnabled("scan") {
			fmt.Fprintf(os.Stderr, "[WORKFLOW] No cache entries found, removing cache file\n")
		}
		os.Remove(dc.CacheFile)
		return scanSkiplist, nil
	}
	
	if IsDebugEnabled("scan") {
		fmt.Fprintf(os.Stderr, "[WORKFLOW] Writing cache index with %d entries\n", cacheOnlySkiplist.Length())
	}

	// Step 10 & 11: Write cache index using vectorio with atomic rename
	tempCachePath := dc.generateTempFileName("cache")

	// Write cache using vectorio for efficient bulk writes (exclude MainContext entries)
	if err := dc.writeSkiplistWithVectorIO(cacheOnlySkiplist, tempCachePath, CacheContext); err != nil {
		os.Remove(tempCachePath)
		return nil, fmt.Errorf("failed to write cache index: %w", err)
	}

	// Note: We defer cleanup of scan index file until after Status completes
	// to avoid use-after-free when Status reads from scan skiplist

	// Atomic replace cache file
	if err := os.Rename(tempCachePath, dc.CacheFile); err != nil {
		os.Remove(tempCachePath) // Cleanup on failure
		return nil, fmt.Errorf("failed to rename cache file: %w", err)
	}

	return scanSkiplist, nil
}
