package dircachefilehash

import (
	"fmt"
	"os"
	"path/filepath"
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
	refs, indexFile, err := dc.loadIndexFromFileWithTracking(dc.IndexFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load main index: %w", err)
	}

	// Register the main index file for tracking
	if indexFile != nil {
		indexFile.Type = "main"
		dc.registerIndex("main", indexFile)
	}

	// Create skiplist and insert all entries with main context
	skiplist := NewSkiplistWrapper(16, MainContext)
	for _, ref := range refs {
		skiplist.Insert(ref, MainContext)
	}

	return skiplist, nil
}

// LoadMergedMainCacheIndex loads main index and merges cache index for unified architecture operations
// This provides a reusable pattern for operations that need complete existing file state without scanning
func (dc *DirectoryCache) LoadMergedMainCacheIndex() (*skiplistWrapper, error) {
	// Load main index as base
	mergedSkiplist, err := dc.LoadMainIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to load main index: %w", err)
	}
	
	// Load cache index and merge into the merged skiplist (avoid .Copy() - merge directly)
	cacheSkiplist, err := dc.loadCacheIndex()
	if err != nil {
		// Cache index might not exist, continue with just main index
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to load cache index: %w", err)
		}
	} else {
		// Merge cache into the merged skiplist (name reflects its actual purpose)
		if err := mergedSkiplist.Merge(cacheSkiplist, MergeTheirs); err != nil {
			return nil, fmt.Errorf("failed to merge cache index: %w", err)
		}
	}
	
	return mergedSkiplist, nil
}

// LoadCacheIndex loads the cache index file and merges timestamped cache files
func (dc *DirectoryCache) loadCacheIndex() (*skiplistWrapper, error) {
	// Create base skiplist for cache context
	skiplist := NewSkiplistWrapper(16, CacheContext)
	
	// Load main cache.idx if it exists
	if _, err := os.Stat(dc.CacheFile); err == nil {
		// Load entries from file as binaryEntryRef instances
		refs, indexFile, err := dc.loadIndexFromFileWithTracking(dc.CacheFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load cache index: %w", err)
		}

		// Register the cache index file for tracking
		if indexFile != nil {
			indexFile.Type = "cache"
			dc.registerIndex("cache", indexFile)
		}

		// Insert all entries with cache context
		for _, ref := range refs {
			skiplist.Insert(ref, CacheContext)
		}

		if IsDebugEnabled("load") {
			VerboseLog(3, "loadCacheIndex: loaded %d entries from cache.idx", len(refs))
		}
	}
	
	// Load and merge timestamped cache files in chronological order
	timestampedCaches, err := dc.ScanForTimestampedCacheFiles()
	if err != nil {
		return nil, fmt.Errorf("failed to scan for timestamped cache files: %w", err)
	}
	
	for _, cacheFile := range timestampedCaches {
		if IsDebugEnabled("load") {
			VerboseLog(3, "loadCacheIndex: merging timestamped cache file: %s", filepath.Base(cacheFile))
		}
		
		// Load timestamped cache file
		refs, indexFile, err := dc.loadIndexFromFileWithTracking(cacheFile)
		if err != nil {
			// Log warning and skip corrupted cache files
			if IsDebugEnabled("scan") {
				fmt.Fprintf(os.Stderr, "[CACHE] Warning: skipping corrupted cache file %s: %v\n", cacheFile, err)
			}
			continue
		}
		
		// Register the timestamped cache index file for tracking
		if indexFile != nil {
			indexFile.Type = "timestamped-cache"
			dc.registerIndex(fmt.Sprintf("timestamped-cache-%s", filepath.Base(cacheFile)), indexFile)
		}
		
		// Create temporary skiplist for this cache file
		timestampedSkiplist := NewSkiplistWrapper(16, CacheContext)
		for _, ref := range refs {
			timestampedSkiplist.Insert(ref, CacheContext)
		}
		
		// Merge into main cache skiplist (later timestamps take precedence)
		if err := skiplist.Merge(timestampedSkiplist, MergeTheirs); err != nil {
			return nil, fmt.Errorf("failed to merge timestamped cache file %s: %w", cacheFile, err)
		}
		
		if IsDebugEnabled("load") {
			VerboseLog(3, "loadCacheIndex: merged %d entries from %s", len(refs), filepath.Base(cacheFile))
		}
	}
	
	if IsDebugEnabled("load") && len(timestampedCaches) > 0 {
		VerboseLog(3, "loadCacheIndex: final merged cache has %d entries", skiplist.Length())
	}

	return skiplist, nil
}

// createUnifiedTmpIndexFromScan scans the directory using unified architecture
func (dc *DirectoryCache) createUnifiedTmpIndexFromScan(shutdownChan <-chan struct{}, comparisonSkiplist *skiplistWrapper) (*skiplistWrapper, error) {
	// Use the unified PerformUnifiedScanToSkiplist workflow  
	scanSkiplist, err := dc.performUnifiedScanToSkiplist(shutdownChan, []string{}, comparisonSkiplist)
	// Pass through both the skiplist and error - the caller will decide if partial data is acceptable
	return scanSkiplist, err
}

// CreateTmpIndexFromScan scans the directory and creates a temporary index using the unified v0.7 architecture
func (dc *DirectoryCache) createTmpIndexFromScan(shutdownChan <-chan struct{}, comparisonSkiplist *skiplistWrapper) (*skiplistWrapper, error) {
	// Use the unified v0.7 architecture with hwangLinUnified
	scanSkiplist, err := dc.createUnifiedTmpIndexFromScan(shutdownChan, comparisonSkiplist)
	// Pass through both the skiplist and error - the caller will decide if partial data is acceptable
	return scanSkiplist, err
}

// runStatusWorkflowUnified implements the Status command workflow using unified architecture
// This follows the v0.7 pattern: write to cache-{timestamp}.idx, rename to cache.idx on success,
// leave timestamped file on interruption for startup merge.
func (dc *DirectoryCache) runStatusWorkflowUnified(shutdownChan <-chan struct{}) (*skiplistWrapper, error) {
	defer VerboseEnter()()
	
	// Generate timestamped cache filename following v0.7 architecture
	cacheTempFileName := dc.GenerateTimestampedFileName("cache")
	
	// Track operation success for proper v0.7 cleanup strategy
	var operationSuccessful bool
	defer func() {
		if operationSuccessful {
			// Success: atomic rename to cache.idx and cleanup timestamped files
			if _, err := os.Stat(cacheTempFileName); err == nil {
				if renameErr := os.Rename(cacheTempFileName, dc.CacheFile); renameErr != nil {
					if IsDebugEnabled("scan") {
						fmt.Fprintf(os.Stderr, "[WORKFLOW] Warning: failed to rename %s to cache.idx: %v\n", cacheTempFileName, renameErr)
					}
				} else {
					// Success - cleanup all timestamped cache files
					if cleanupErr := dc.CleanupTimestampedCacheFiles(); cleanupErr != nil && IsDebugEnabled("scan") {
						fmt.Fprintf(os.Stderr, "[WORKFLOW] Warning: failed to cleanup timestamped cache files: %v\n", cleanupErr)
					}
				}
			}
		} else {
			// Interruption/Error: Leave cache-{timestamp}.idx for startup merge (v0.7 pattern)
			if IsDebugEnabled("scan") {
				fmt.Fprintf(os.Stderr, "[WORKFLOW] Operation incomplete, leaving %s for startup merge\n", cacheTempFileName)
			}
		}
	}()
	
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

	// Step 5: Create tmp index from scan using unified Hwang-Lin algorithm
	scanSkiplist, scanErr := dc.createUnifiedTmpIndexFromScan(shutdownChan, workingSkiplist)
	
	// If we got absolutely no data, return error immediately
	if scanErr != nil && scanSkiplist == nil {
		return nil, fmt.Errorf("failed to create scan index: %w", scanErr)
	}
	
	// If we have partial data due to interruption, continue to save it
	if scanErr != nil && IsDebugEnabled("scan") {
		fmt.Fprintf(os.Stderr, "[WORKFLOW] Scan interrupted, continuing with partial data (%d entries)\n", scanSkiplist.Length())
	}

	// Merge scan results back into workingSkiplist to create complete state
	// This ensures the returned skiplist contains all files (unchanged + changed)
	if scanSkiplist != nil && !scanSkiplist.IsEmpty() {
		if err := workingSkiplist.Merge(scanSkiplist, MergeTheirs); err != nil {
			return nil, fmt.Errorf("failed to merge scan results: %w", err)
		}
	}

	// Steps 6-8 are handled inside createUnifiedTmpIndexFromScan (Hwang-Lin, hashing, waiting)

	// Step 9: Filter cache entries (entries not in main context)
	cacheOnlySkiplist := workingSkiplist.FilterNotByContext(MainContext)

	// If no cache entries, remove cache file and mark success
	if cacheOnlySkiplist.IsEmpty() {
		if IsDebugEnabled("scan") {
			fmt.Fprintf(os.Stderr, "[WORKFLOW] No cache entries found, removing cache file\n")
		}
		os.Remove(dc.CacheFile)
		operationSuccessful = true // No cache file to write, so this is success
		// Return the complete working skiplist and the original scan error (if any)
		return workingSkiplist, scanErr
	}
	
	if IsDebugEnabled("scan") {
		fmt.Fprintf(os.Stderr, "[WORKFLOW] Writing cache index to timestamped file %s with %d entries\n", cacheTempFileName, cacheOnlySkiplist.Length())
	}

	// Step 10 & 11: Write cache index to timestamped file (v0.7 pattern)
	// Note: We defer cleanup of scan index file until after Status completes
	// to avoid use-after-free when Status reads from scan skiplist
	if writeErr := dc.atomicWriteIndex(cacheOnlySkiplist, cacheTempFileName, CacheContext, false); writeErr != nil {
		// If we can't write the cache, at least return the complete working data we have
		return workingSkiplist, fmt.Errorf("scan error: %v, cache write error: %w", scanErr, writeErr)
	}

	// If we got here without interruption, mark operation as successful
	if scanErr == nil {
		operationSuccessful = true
	}
	// Note: If scanErr != nil, we still wrote partial results but don't mark as successful
	// This leaves the timestamped file for startup merge (correct v0.7 behavior)

	// Return the complete working skiplist and propagate the scan error (if any) so caller knows scan was interrupted
	return workingSkiplist, scanErr
}

// updateCacheIndexWithWorkflow has been moved to v0.6/pkg/workflow.go as part of the v0.7 unified
// architecture migration. Use runStatusWorkflowUnified() instead.
