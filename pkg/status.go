package dircachefilehash

import (
	"fmt"
	"os"
	"strconv"
)

// FileStatus represents the status of a file
type FileStatus int

const (
	StatusUnchanged FileStatus = iota
	StatusModified
	StatusAdded
	StatusDeleted
)

// CleanStatus represents the clean status of index files
type CleanStatus struct {
	MainIndex    bool     `json:"main_index"`
	CacheIndex   bool     `json:"cache_index"`
	TempIndices  []string `json:"temp_indices,omitempty"` // List of temporary index files found
	HasTempFiles bool     `json:"has_temp_files"`         // True if any temp files exist
}

// StatusResult represents the result of a status check
type StatusResult struct {
	Modified    []string     `json:"modified"`
	Added       []string     `json:"added"`
	Deleted     []string     `json:"deleted"`
	CleanStatus *CleanStatus `json:"clean_status,omitempty"` // Only included when verbose
}

// Status compares the current directory state with the loaded index using the unified architecture
func (dc *DirectoryCache) Status(shutdownChan <-chan struct{}, flags map[string]string) (*StatusResult, error) {
	defer VerboseEnter()()
	
	// Apply flags before scanning
	if err := dc.ApplyConfigOverrides(flags); err != nil {
		// If no config loaded, apply symlink mode directly if provided
		if symlinkMode, exists := flags["symlinks"]; exists {
			dc.symlinkMode = symlinkMode
		}
	}
	
	// Load main index as base for comparison
	mainSkiplist, err := dc.LoadMainIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to load main index: %w", err)
	}
	if IsDebugEnabled("scan") {
		VerboseLog(3, "Status: mainSkiplist length = %d", mainSkiplist.Length())
	}

	// Load cache index and merge with main for complete existing state
	cacheSkiplist, err := dc.loadCacheIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to load cache index: %w", err)
	}
	if IsDebugEnabled("scan") {
		VerboseLog(3, "Status: cacheSkiplist length = %d", cacheSkiplist.Length())
	}

	// Create comparison skiplist by merging main + cache (cache takes precedence)
	comparisonSkiplist := mainSkiplist.Copy()
	if !cacheSkiplist.IsEmpty() {
		if err := comparisonSkiplist.Merge(cacheSkiplist, MergeTheirs); err != nil {
			return nil, fmt.Errorf("failed to merge cache with main index: %w", err)
		}
	}
	if IsDebugEnabled("scan") {
		VerboseLog(3, "Status: comparisonSkiplist length = %d", comparisonSkiplist.Length())
	}

	// Create hash manager for filesystem scanning
	hashManager := dc.newAlgorithmHashManager(dc.hashWorkers, shutdownChan)
	defer hashManager.Shutdown()

	// Create iterators for unified algorithm
	existingIterator := NewBinaryEntrySkiplistIterator(comparisonSkiplist, "existing")
	scanIterator := NewUnifiedFilesystemScanIterator(dc, []string{}, "scan", hashManager)

	// Create status callback to collect status changes
	statusCallback := NewStatusCallback("status", dc)

	// Run unified algorithm to compare existing vs current filesystem state
	if err := hwangLinUnified(existingIterator, scanIterator, statusCallback); err != nil {
		// Even if interrupted, return partial results
		if IsDebugEnabled("scan") {
			fmt.Fprintf(os.Stderr, "[STATUS] Scan interrupted, returning partial status results\n")
		}
	}

	// Signal completion of hash job submission
	hashManager.FinishSubmitting()

	// Get the final status result from the callback
	result := statusCallback.GetResult()

	// Check for verbose flag and include clean status if requested
	if verboseLevel, exists := flags["v"]; exists && verboseLevel != "" {
		if level, err := strconv.Atoi(verboseLevel); err == nil && level > 0 {
			result.CleanStatus = &CleanStatus{}

			// Check main index clean status
			if _, err := os.Stat(dc.IndexFile); err == nil {
				// Main index exists - for now assume clean if it loads
				result.CleanStatus.MainIndex = true
			} else {
				result.CleanStatus.MainIndex = false
			}

			// Check cache index clean status  
			if _, err := os.Stat(dc.CacheFile); err == nil {
				// Cache index exists - for now assume clean if it exists
				result.CleanStatus.CacheIndex = true
			} else {
				result.CleanStatus.CacheIndex = false
			}

			// Scan for temporary index files in the .dcfh directory
			tempFiles, err := dc.scanForTempIndices()
			if err == nil {
				result.CleanStatus.TempIndices = tempFiles
				result.CleanStatus.HasTempFiles = len(tempFiles) > 0
			} else {
				result.CleanStatus.HasTempFiles = false
			}
		}
	}

	return result, nil
}

// hwangLinStatus function removed - migrated to use hwangLinUnified with StatusCallback
// This eliminates the duplicate Hwang-Lin implementation in favor of the unified architecture

// isFileModified function removed - migrated to use isFileModifiedInterface in StatusCallback
// This eliminates duplicate file modification checking logic

// HasChanges returns true if there are any changes
func (sr *StatusResult) HasChanges() bool {
	return len(sr.Modified) > 0 || len(sr.Added) > 0 || len(sr.Deleted) > 0
}

// TotalChanges returns the total number of changed files
func (sr *StatusResult) TotalChanges() int {
	return len(sr.Modified) + len(sr.Added) + len(sr.Deleted)
}
