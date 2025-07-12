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

// Status compares the current directory state with the loaded index using the new workflow
func (dc *DirectoryCache) Status(shutdownChan <-chan struct{}, flags map[string]string) (*StatusResult, error) {
	defer VerboseEnter()()
	
	// Apply flags before scanning
	if err := dc.ApplyConfigOverrides(flags); err != nil {
		// If no config loaded, apply symlink mode directly if provided
		if symlinkMode, exists := flags["symlinks"]; exists {
			dc.symlinkMode = symlinkMode
		}
	}
	
	// Use the new cache update workflow which implements steps 1-11 as specified
	// This returns the scan result which we can reuse to avoid duplicate scans
	currentSkiplist, err := dc.updateCacheIndexWithWorkflow(shutdownChan)
	if err != nil && currentSkiplist == nil {
		// Only return error if we got no data at all
		return nil, fmt.Errorf("failed to update cache index: %w", err)
	}
	// If we have partial data due to interruption, continue with what we have
	if err != nil {
		// Log the interruption but continue with partial data
		if IsDebugEnabled("scan") {
			fmt.Fprintf(os.Stderr, "[STATUS] Scan interrupted but cache saved, continuing with partial data (%d entries)\n", currentSkiplist.Length())
		}
		// Note: We continue with status reporting even though scan was interrupted
		// The cache has been updated with partial results for next time
	}

	// Load both main and cache indices for comparison
	mainSkiplist, err := dc.LoadMainIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to load main index: %w", err)
	}
	if IsDebugEnabled("scan") {
		VerboseLog(3, "Status: mainSkiplist length = %d", mainSkiplist.Length())
	}

	cacheSkiplist, err := dc.loadCacheIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to load cache index: %w", err)
	}
	if IsDebugEnabled("scan") {
		VerboseLog(3, "Status: cacheSkiplist length = %d", cacheSkiplist.Length())
	}

	// Status compares main index (committed files) vs scan result (current disk state)

	result := &StatusResult{
		Modified: make([]string, 0),
		Added:    make([]string, 0),
		Deleted:  make([]string, 0),
	}

	// Check for verbose flag and include clean status if requested
	if verboseLevel, exists := flags["v"]; exists && verboseLevel != "" {
		if level, err := strconv.Atoi(verboseLevel); err == nil && level > 0 {
			result.CleanStatus = &CleanStatus{}

			// Check main index clean status
			if dc.mmapIndex != nil && dc.mmapIndex.Header() != nil {
				result.CleanStatus.MainIndex = dc.mmapIndex.Header().isClean()
			}

			// Check cache index clean status by loading it
			cacheSkiplist, err := dc.loadCacheIndex()
			if err == nil && cacheSkiplist != nil {
				// For cache index, we need to access the underlying mmap - this is a bit tricky
				// For now, we'll assume it's clean if it loaded successfully
				// TODO: Improve this to actually check the cache index header
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

	// Use unified Hwang-Lin algorithm with StatusCallback
	if IsDebugEnabled("scan") {
		VerboseLog(3, "Status: mainSkiplist length = %d", mainSkiplist.Length())
		VerboseLog(3, "Status: currentSkiplist length = %d", currentSkiplist.Length())
	}
	
	// Create iterators for both skiplists
	mainIterator := NewBinaryEntrySkiplistIterator(mainSkiplist, "main-index")
	defer mainIterator.Close()
	
	currentIterator := NewBinaryEntrySkiplistIterator(currentSkiplist, "current-scan")
	defer currentIterator.Close()
	
	// Create status callback
	statusCallback := NewStatusCallback("status-check", dc)
	
	// Execute unified algorithm
	if err := hwangLinUnified(mainIterator, currentIterator, statusCallback); err != nil {
		return nil, fmt.Errorf("failed to compare main and current state: %w", err)
	}
	
	// Get results from callback and merge with existing result
	statusResult := statusCallback.GetResult()
	result.Modified = statusResult.Modified
	result.Added = statusResult.Added
	result.Deleted = statusResult.Deleted

	// Now that Status comparison is complete, cleanup scan index file
	if err := dc.cleanupCurrentScanFile(); err != nil && !os.IsNotExist(err) {
		// Non-fatal, but log the error
		fmt.Fprintf(os.Stderr, "Warning: failed to cleanup scan file: %v\n", err)
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
