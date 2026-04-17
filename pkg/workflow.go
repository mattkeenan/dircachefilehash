package dircachefilehash

import (
	"context"
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

// runStatusWorkflowUnified implements the Status command workflow using unified architecture
// This follows the v0.7 pattern: write to cache-{timestamp}.idx, rename to cache.idx on success,
// leave timestamped file on interruption for startup merge.
func (dc *DirectoryCache) runStatusWorkflowUnified(ctx context.Context) (*skiplistWrapper, error) {
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

	// v0.7 unified: Use performUnifiedStatusScan which handles iterative writing via StatusCallback
	// This writes directly to temp cache index during Hwang-Lin iteration - no skiplist handling needed
	resultSkiplist, scanErr := dc.performUnifiedStatusScan(ctx, cacheTempFileName, workingSkiplist)
	if scanErr != nil {
		// v0.7: On interruption, StatusCallback handles cleanup and partial results preserved in timestamped file
		// operationSuccessful remains false, so defer will leave timestamped file for startup merge
		return resultSkiplist, fmt.Errorf("status scan interrupted: %w", scanErr)
	}

	// v0.7: performUnifiedStatusScan has already written cache entries to cacheTempFileName
	// Mark operation as successful so defer will rename to cache.idx and cleanup timestamped files
	operationSuccessful = true

	return resultSkiplist, nil
}

// performUnifiedStatusScan performs status scan using StatusCallback for v0.7 cache writing
// This follows the same pattern as performUnifiedScanToSkiplist but uses StatusCallback
// to filter and write only cache entries (not in main context) during iteration
func (dc *DirectoryCache) performUnifiedStatusScan(ctx context.Context, cacheFileName string, compareSkiplist *skiplistWrapper) (*skiplistWrapper, error) {
	defer VerboseEnter()()

	// Synchronise concurrent scans - only one scan per DirectoryCache at a time
	dc.scanMutex.Lock()
	defer dc.scanMutex.Unlock()

	// If a scan is already in progress, wait for it and return the same results
	if dc.scanInProgress {
		if dc.lastScanError != nil {
			return nil, dc.lastScanError
		}
		return dc.lastScanResult, nil
	}

	// Mark scan as in progress
	dc.scanInProgress = true
	defer func() {
		dc.scanInProgress = false
	}()

	// Create hash job manager for concurrent hashing
	hashJobManager := dc.newAlgorithmHashManager(ctx, dc.hashWorkers)
	defer hashJobManager.Shutdown()

	// Create iterators for unified algorithm
	existingIterator := NewBinaryEntrySkiplistIterator(ctx, compareSkiplist, "existing")
	scanIterator := NewUnifiedFilesystemScanIterator(ctx, dc, []string{}, "scan")

	// Create status callback for v0.7 direct cache index writing
	statusCallback := NewStatusCallback("status", dc, hashJobManager, cacheFileName)

	// Run unified algorithm with StatusCallback
	scanErr := hwangLinUnified(existingIterator, scanIterator, statusCallback, ctx)
	if scanErr != nil {
		dc.lastScanResult = compareSkiplist // Return original skiplist on error
		dc.lastScanError = scanErr
		return compareSkiplist, scanErr
	}

	// Signal that no more hash jobs will be submitted
	hashJobManager.FinishSubmitting()

	// v0.7: StatusCallback has written cache entries to temp cache index file
	// Return the original comparison skiplist (represents complete file state)
	dc.lastScanResult = compareSkiplist
	dc.lastScanError = nil

	return compareSkiplist, nil
}

// updateCacheIndexWithWorkflow has been moved to v0.6/pkg/workflow.go as part of the v0.7 unified
// architecture migration. Use runStatusWorkflowUnified() instead.
