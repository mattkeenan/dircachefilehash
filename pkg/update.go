package dircachefilehash

import (
	"context"
	"fmt"
	"os"
)

// Update scans the directory and updates the index file using the new workflow
func (dc *DirectoryCache) Update(ctx context.Context, flags map[string]string, paths ...string) error {
	// Apply flags before scanning
	if err := dc.ApplyConfigOverrides(flags); err != nil {
		// If no config loaded, apply symlink mode directly if provided
		if symlinkMode, exists := flags["symlinks"]; exists {
			dc.symlinkMode = symlinkMode
		}
	}

	if len(paths) == 0 {
		// No specific paths: update entire repository - put everything in main index
		return dc.updateFullRepository(ctx)
	} else {
		// Specific paths: selective update - manage main vs cache indices
		return dc.updateSpecificPaths(ctx, paths)
	}
}

// updateFullRepository updates the entire repository: everything goes into
// the main index, and the cache index is removed.
func (dc *DirectoryCache) updateFullRepository(ctx context.Context) error {
	// Load main index to use as comparison base (avoid re-hashing unchanged files)
	comparisonSkiplist, err := dc.LoadMainIndex()
	if err != nil {
		// If main index doesn't exist or can't be loaded, use empty skiplist
		comparisonSkiplist = NewSkiplistWrapper(16, "empty")
	}

	// Load cache index and merge with main for comparison
	// This ensures we don't re-hash files already tracked in cache
	cacheSkiplist, err := dc.loadCacheIndex()
	if err == nil && !cacheSkiplist.IsEmpty() {
		// Merge cache into main (cache entries take precedence)
		if err := comparisonSkiplist.Merge(cacheSkiplist, MergeTheirs); err != nil {
			return fmt.Errorf("failed to merge cache index for comparison: %w", err)
		}
	}

	// Pipeline: comparison → hash → reorder → write, with atomic rename on success
	err = dc.performPipelineScan(ctx, []string{}, comparisonSkiplist)
	if err != nil {
		return fmt.Errorf("pipeline scan failed: %w", err)
	}

	// Remove cache file since everything is now in main index
	_ = os.Remove(dc.CacheFile) // Non-fatal if it fails
	_ = dc.checkForOrphanedIndexFiles()

	return nil
}

// updateSpecificPaths updates only specified paths: changed entries land in
// the main index and the cache index is refreshed afterwards.
func (dc *DirectoryCache) updateSpecificPaths(ctx context.Context, paths []string) error {
	// Load main index for comparison (avoid re-hashing unchanged files)
	comparisonSkiplist, err := dc.LoadMainIndex()
	if err != nil {
		return fmt.Errorf("failed to load main index: %w", err)
	}

	// Load cache index and merge for comparison to avoid re-hashing
	cacheSkiplist, err := dc.loadCacheIndex()
	if err == nil && !cacheSkiplist.IsEmpty() {
		// Merge cache into comparison skiplist (cache entries take precedence)
		if err := comparisonSkiplist.Merge(cacheSkiplist, MergeTheirs); err != nil {
			return fmt.Errorf("failed to merge cache index for comparison: %w", err)
		}
	}

	// Pipeline: comparison → hash → reorder → write, with atomic rename on success
	err = dc.performPipelineScan(ctx, paths, comparisonSkiplist)
	if err != nil {
		return fmt.Errorf("update interrupted: %w", err)
	}
	// Refresh cache.idx so it reflects the new main index state.
	if _, err := dc.refreshFsScanCache(ctx); err != nil {
		return fmt.Errorf("failed to update cache: %w", err)
	}

	_ = dc.checkForOrphanedIndexFiles()
	return nil
}
