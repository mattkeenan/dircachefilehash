//go:build exclude
package dircachefilehash

import (
	"fmt"
	"os"
)

// ============================================================================
// DEPRECATED v0.6 WORKFLOW FUNCTIONS - MOVED TO v0.6/
// ============================================================================
// 
// These functions are part of the old v0.6 architecture and have been
// replaced by the unified v0.7 architecture. They are preserved here for 
// reference and potential recovery scenarios but should not be used in new code.
//
// Replacement in v0.7:
// - updateCacheIndexWithWorkflow() → runStatusWorkflowUnified() (for Status commands)
// ============================================================================

// updateCacheIndexWithWorkflow implements the cache update workflow as specified
// DEPRECATED: This function is part of the v0.6 architecture. Use runStatusWorkflowUnified() 
// in v0.7 instead.
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
	scanSkiplist, scanErr := dc.createTmpIndexFromScan(shutdownChan, workingSkiplist)
	
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

	// Steps 6-8 are handled inside CreateTmpIndexFromScan (Hwang-Lin, hashing, waiting)

	// Step 9: Filter cache entries (entries not in main context)
	cacheOnlySkiplist := workingSkiplist.FilterNotByContext(MainContext)

	// If no cache entries, remove cache file
	if cacheOnlySkiplist.IsEmpty() {
		if IsDebugEnabled("scan") {
			fmt.Fprintf(os.Stderr, "[WORKFLOW] No cache entries found, removing cache file\n")
		}
		os.Remove(dc.CacheFile)
		// Return the complete working skiplist and the original scan error (if any)
		return workingSkiplist, scanErr
	}
	
	if IsDebugEnabled("scan") {
		fmt.Fprintf(os.Stderr, "[WORKFLOW] Writing cache index with %d entries\n", cacheOnlySkiplist.Length())
	}

	// Step 10 & 11: Write cache index atomically using vectorio
	// Note: We defer cleanup of scan index file until after Status completes
	// to avoid use-after-free when Status reads from scan skiplist
	if writeErr := dc.atomicWriteIndex(cacheOnlySkiplist, dc.CacheFile, CacheContext, false); writeErr != nil {
		// If we can't write the cache, at least return the complete working data we have
		return workingSkiplist, fmt.Errorf("scan error: %v, cache write error: %w", scanErr, writeErr)
	}

	// Return the complete working skiplist and propagate the scan error (if any) so caller knows scan was interrupted
	return workingSkiplist, scanErr
}