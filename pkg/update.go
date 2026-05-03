package dircachefilehash

import (
	"context"
	"fmt"
	"os"
)

// runUpdate scans the directory and updates the index file using the
// pipeline.
func runUpdate(ctx context.Context, ms *MetaStore, sr *ScanRun, flags map[string]string, paths ...string) error {
	ms.applyOverridesToScanRun(sr, flags)

	if len(paths) == 0 {
		// No specific paths: update entire repository - put everything in main index
		return ms.updateFullRepository(ctx, sr)
	}
	// Specific paths: selective update - manage main vs cache indices
	return ms.updateSpecificPaths(ctx, sr, paths)
}

// updateFullRepository updates the entire repository: everything goes into
// the main index, and the cache index is removed.
func (ms *MetaStore) updateFullRepository(ctx context.Context, sr *ScanRun) error {
	// Load main index to use as comparison base (avoid re-hashing unchanged files)
	comparisonSkiplist, err := ms.LoadMainIndex()
	if err != nil {
		// If main index doesn't exist or can't be loaded, use empty skiplist
		comparisonSkiplist = NewSkiplistWrapper(16, "empty")
	}

	// Load cache index and merge with main for comparison
	// This ensures we don't re-hash files already tracked in cache
	cacheSkiplist, err := ms.loadCacheIndex()
	if err == nil && !cacheSkiplist.IsEmpty() {
		// Merge cache into main (cache entries take precedence)
		if err := comparisonSkiplist.Merge(cacheSkiplist, MergeTheirs); err != nil {
			return fmt.Errorf("failed to merge cache index for comparison: %w", err)
		}
	}

	// Pipeline: comparison → hash → reorder → write, with atomic rename on success
	err = ms.performPipelineScan(ctx, sr, []string{}, comparisonSkiplist)
	if err != nil {
		return fmt.Errorf("pipeline scan failed: %w", err)
	}

	// Remove cache file since everything is now in main index
	_ = os.Remove(ms.CacheFile) // Non-fatal if it fails
	_ = ms.checkForOrphanedIndexFiles()

	return nil
}

// updateSpecificPaths updates only specified paths: changed entries land in
// the main index and the cache index is refreshed afterwards.
func (ms *MetaStore) updateSpecificPaths(ctx context.Context, sr *ScanRun, paths []string) error {
	// Load main index for comparison (avoid re-hashing unchanged files)
	comparisonSkiplist, err := ms.LoadMainIndex()
	if err != nil {
		return fmt.Errorf("failed to load main index: %w", err)
	}

	// Load cache index and merge for comparison to avoid re-hashing
	cacheSkiplist, err := ms.loadCacheIndex()
	if err == nil && !cacheSkiplist.IsEmpty() {
		// Merge cache into comparison skiplist (cache entries take precedence)
		if err := comparisonSkiplist.Merge(cacheSkiplist, MergeTheirs); err != nil {
			return fmt.Errorf("failed to merge cache index for comparison: %w", err)
		}
	}

	// Pipeline: comparison → hash → reorder → write, with atomic rename on success
	err = ms.performPipelineScan(ctx, sr, paths, comparisonSkiplist)
	if err != nil {
		return fmt.Errorf("update interrupted: %w", err)
	}
	// Refresh cache.idx so it reflects the new main index state.
	if _, err := ms.refreshFsScanCache(ctx, sr); err != nil {
		return fmt.Errorf("failed to update cache: %w", err)
	}

	_ = ms.checkForOrphanedIndexFiles()
	return nil
}
