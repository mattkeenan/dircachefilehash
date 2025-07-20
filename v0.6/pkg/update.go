//go:build exclude
package dircachefilehash

import (
	"fmt"
	"os"
)

// ============================================================================
// DEPRECATED v0.6 UPDATE FUNCTIONS - MOVED TO v0.6/
// ============================================================================
// 
// These functions are part of the old v0.6 architecture and have been
// replaced by the unified v0.7 architecture. They are preserved here for 
// reference and potential recovery scenarios but should not be used in new code.
//
// Replacement in v0.7:
// - updateFullRepository() → updateFullRepositoryUnified()  
// - updateSpecificPaths() → updateSpecificPathsUnified()
// - Functions that use performHwangLinScanToSkiplist() → use hwangLinUnified() with callbacks
// ============================================================================

// updateFullRepository updates the entire repository and puts everything in main index
// DEPRECATED: This function is part of the v0.6 architecture. Use updateFullRepositoryUnified() 
// in v0.7 instead.
func (dc *DirectoryCache) updateFullRepository(shutdownChan <-chan struct{}) error {
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

	// Use new scan workflow to get all files
	scanSkiplist, err := dc.performHwangLinScanToSkiplist(shutdownChan, []string{}, comparisonSkiplist)
	if err != nil {
		// Handle interruption by saving partial work to cache
		if scanSkiplist != nil && !scanSkiplist.IsEmpty() {
			// Merge partial scan results into comparison skiplist
			if mergeErr := comparisonSkiplist.Merge(scanSkiplist, MergeTheirs); mergeErr != nil {
				return fmt.Errorf("failed to merge partial scan results: %w", mergeErr)
			}
			
			// Write to cache index atomically (CacheContext here means "create a cache index file"
			// which excludes MainContext entries but keeps CacheContext + ScanContext entries)
			if writeErr := dc.atomicWriteIndex(comparisonSkiplist, dc.CacheFile, CacheContext, false); writeErr != nil {
				return fmt.Errorf("failed to save partial results to cache: %w", writeErr)
			}
			
			// Cleanup scan index file after successful write
			if cleanupErr := dc.cleanupCurrentScanFile(); cleanupErr != nil && !os.IsNotExist(cleanupErr) {
				fmt.Fprintf(os.Stderr, "Warning: failed to cleanup scan file: %v\n", cleanupErr)
			}
		}
		return fmt.Errorf("update interrupted: %w", err)
	}

	// For full repository update, merge scan results back into comparison skiplist
	if scanSkiplist != nil && !scanSkiplist.IsEmpty() {
		if err := comparisonSkiplist.Merge(scanSkiplist, MergeTheirs); err != nil {
			return fmt.Errorf("failed to merge scan results: %w", err)
		}
	}

	// Write the complete merged skiplist to main index atomically (exclude deleted entries)
	if err := dc.atomicWriteIndex(comparisonSkiplist, dc.IndexFile, "", true); err != nil {
		return fmt.Errorf("failed to write new main index: %w", err)
	}

	// Cleanup scan index file now that main index is written
	if err := dc.cleanupCurrentScanFile(); err != nil && !os.IsNotExist(err) {
		// Non-fatal, but log the error
		fmt.Fprintf(os.Stderr, "Warning: failed to cleanup scan file: %v\n", err)
	}

	// Remove cache file since everything is now in main index
	os.Remove(dc.CacheFile) // Non-fatal if it fails
	dc.checkForOrphanedIndexFiles()

	return nil
}

// updateSpecificPaths updates only specified paths and manages main index vs cache
// DEPRECATED: This function is part of the v0.6 architecture. Use updateSpecificPathsUnified() 
// in v0.7 instead.
func (dc *DirectoryCache) updateSpecificPaths(shutdownChan <-chan struct{}, paths []string) error {
	// Load main index for final output
	mainSkiplist, err := dc.LoadMainIndex()
	if err != nil {
		return fmt.Errorf("failed to load main index: %w", err)
	}

	// Create comparison skiplist starting with main index
	comparisonSkiplist := mainSkiplist.Copy()
	
	// Load cache index and merge for comparison to avoid re-hashing
	cacheSkiplist, err := dc.loadCacheIndex()
	if err == nil && !cacheSkiplist.IsEmpty() {
		// Merge cache into comparison skiplist (cache entries take precedence)
		if err := comparisonSkiplist.Merge(cacheSkiplist, MergeTheirs); err != nil {
			return fmt.Errorf("failed to merge cache index for comparison: %w", err)
		}
	}

	// Use new scan workflow with merged index as comparison to get only changes in specified paths
	scanSkiplist, err := dc.performHwangLinScanToSkiplist(shutdownChan, paths, comparisonSkiplist)
	if err != nil {
		// Handle interruption by saving partial work to cache
		if scanSkiplist != nil && !scanSkiplist.IsEmpty() {
			// Merge partial scan results into comparison skiplist (which already has cache data)
			if mergeErr := comparisonSkiplist.Merge(scanSkiplist, MergeTheirs); mergeErr != nil {
				return fmt.Errorf("failed to merge partial scan results: %w", mergeErr)
			}
			
			// Write to cache index atomically (CacheContext here means "create a cache index file"
			// which excludes MainContext entries but keeps CacheContext + ScanContext entries)
			if writeErr := dc.atomicWriteIndex(comparisonSkiplist, dc.CacheFile, CacheContext, false); writeErr != nil {
				return fmt.Errorf("failed to save partial results to cache: %w", writeErr)
			}
			
			// Cleanup scan index file after successful write
			if cleanupErr := dc.cleanupCurrentScanFile(); cleanupErr != nil && !os.IsNotExist(cleanupErr) {
				fmt.Fprintf(os.Stderr, "Warning: failed to cleanup scan file: %v\n", cleanupErr)
			}
		}
		return fmt.Errorf("update interrupted: %w", err)
	}

	// Merge scan results with main index (scan results take precedence)
	if err := mainSkiplist.Merge(scanSkiplist, MergeTheirs); err != nil {
		return fmt.Errorf("failed to merge scan results with main index: %w", err)
	}

	// Write new main index atomically (exclude deleted entries)
	if err := dc.atomicWriteIndex(mainSkiplist, dc.IndexFile, MainContext, true); err != nil {
		return fmt.Errorf("failed to write new main index: %w", err)
	}

	// Cleanup scan index file now that main index is written
	if err := dc.cleanupCurrentScanFile(); err != nil && !os.IsNotExist(err) {
		// Non-fatal, but log the error
		fmt.Fprintf(os.Stderr, "Warning: failed to cleanup scan file: %v\n", err)
	}

	// Update cache using the v0.6 workflow (deprecated)
	if _, err := dc.updateCacheIndexWithWorkflow(shutdownChan); err != nil {
		return fmt.Errorf("failed to update cache: %w", err)
	}

	// Cleanup scan index file from cache workflow
	if err := dc.cleanupCurrentScanFile(); err != nil && !os.IsNotExist(err) {
		// Non-fatal, but log the error
		fmt.Fprintf(os.Stderr, "Warning: failed to cleanup scan file: %v\n", err)
	}

	dc.checkForOrphanedIndexFiles()
	return nil
}