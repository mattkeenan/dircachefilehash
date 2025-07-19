package dircachefilehash

import (
	"fmt"
	"os"
	"path/filepath"
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

	// Initialize timestamped cache index for iterative writing during Status
	cacheTempFileName := dc.GenerateTimestampedFileName("cache")
	if err := dc.initialiseScanIndex(cacheTempFileName); err != nil {
		return nil, fmt.Errorf("failed to initialise cache temp index: %w", err)
	}
	
	// Track operation success for proper cleanup strategy
	var operationSuccessful bool
	defer func() {
		if operationSuccessful {
			// Success: atomic rename to cache.idx and cleanup timestamped files
			if _, err := os.Stat(cacheTempFileName); err == nil {
				if renameErr := os.Rename(cacheTempFileName, dc.CacheFile); renameErr != nil {
					if IsDebugEnabled("scan") {
						fmt.Fprintf(os.Stderr, "[STATUS] Warning: failed to rename %s to cache.idx: %v\n", cacheTempFileName, renameErr)
					}
				} else {
					// Success - cleanup all timestamped cache files
					if cleanupErr := dc.CleanupTimestampedCacheFiles(); cleanupErr != nil && IsDebugEnabled("scan") {
						fmt.Fprintf(os.Stderr, "[STATUS] Warning: failed to cleanup timestamped cache files: %v\n", cleanupErr)
					}
				}
			}
		} else {
			// Interruption/Error: leave timestamped cache file for startup merge
			if IsDebugEnabled("scan") {
				fmt.Fprintf(os.Stderr, "[STATUS] Operation incomplete - leaving %s for startup merge\n", filepath.Base(cacheTempFileName))
			}
		}
	}()

	// Create hash manager for filesystem scanning
	hashManager := dc.newAlgorithmHashManager(dc.hashWorkers, shutdownChan)
	defer hashManager.Shutdown()

	// Create iterators for unified algorithm
	existingIterator := NewBinaryEntrySkiplistIterator(comparisonSkiplist, "existing", shutdownChan)
	scanIterator := NewUnifiedFilesystemScanIterator(dc, []string{}, "scan")

	// Create status callback for iterative cache writing during hwangLinUnified execution
	statusCallback := NewStatusCallback("status", dc, hashManager, cacheTempFileName)

	// Run unified algorithm to compare existing vs current filesystem state
	scanErr := hwangLinUnified(existingIterator, scanIterator, statusCallback, shutdownChan)
	if scanErr != nil {
		// Even if interrupted, return partial results
		if IsDebugEnabled("scan") {
			fmt.Fprintf(os.Stderr, "[STATUS] Scan interrupted, returning partial status results\n")
		}
	}

	// Signal completion of hash job submission
	hashManager.FinishSubmitting()

	// CRITICAL: Status command MUST write hashed results to cache for performance optimization
	// Mark operation as successful only if scan completed without interruption
	operationSuccessful = (scanErr == nil)
	
	if IsDebugEnabled("scan") {
		if operationSuccessful {
			fmt.Fprintf(os.Stderr, "[STATUS] Operation completed successfully - will rename to cache.idx and cleanup\n")
		} else {
			fmt.Fprintf(os.Stderr, "[STATUS] Operation interrupted - will preserve timestamped cache file\n")
		}
	}

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
