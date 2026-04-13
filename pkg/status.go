package dircachefilehash

import (
	"context"
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

// Status compares main.idx against the current filesystem, writing changes to cache.idx.
// The cache is a sparse delta — its entries ARE the status (modified, added, deleted).
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

	// Load and merge all cache indices (cache.idx + timestamped cache files)
	// Used as a hash lookup to avoid re-hashing files already cached
	cacheSkiplist, err := dc.loadCacheIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to load cache index: %w", err)
	}
	if IsDebugEnabled("scan") {
		VerboseLog(3, "Status: cacheSkiplist length = %d", cacheSkiplist.Length())
	}

	// Generate timestamped cache index filename for pipeline output
	cacheTempFileName := dc.GenerateTimestampedFileName("cache")

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

	// Create iterators: main.idx (left) vs filesystem (right)
	existingIterator := NewBinaryEntrySkiplistIterator(mainSkiplist, "existing", shutdownChan)
	scanIterator := NewUnifiedFilesystemScanIterator(dc, []string{}, "scan")

	// Convert shutdown channel to context for the pipeline
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-shutdownChan:
			cancel()
		case <-ctx.Done():
		}
	}()

	// Run the 4-stage pipeline: Compare → Hash → Reorder → Write
	// The pipeline writes only changes (vs main) to the cache file.
	scanErr := RunStatusPipeline(ctx, dc, cacheSkiplist, existingIterator, scanIterator, cacheTempFileName)
	if scanErr != nil {
		if IsDebugEnabled("scan") {
			fmt.Fprintf(os.Stderr, "[STATUS] Pipeline error: %v\n", scanErr)
		}
	}

	// CRITICAL: Status command MUST write hashed results to cache for performance optimisation
	operationSuccessful = (scanErr == nil)

	if IsDebugEnabled("scan") {
		if operationSuccessful {
			fmt.Fprintf(os.Stderr, "[STATUS] Operation completed successfully - will rename to cache.idx and cleanup\n")
		} else {
			fmt.Fprintf(os.Stderr, "[STATUS] Operation interrupted - will preserve timestamped cache file\n")
		}
	}

	// Derive StatusResult from the cache file — its entries ARE the status
	result := deriveStatusFromCache(dc, mainSkiplist, cacheTempFileName)

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

// deriveStatusFromCache reads the cache file and categorises each entry
// by comparing against main.idx. The cache is a sparse delta — every entry
// in it represents a change from main.
func deriveStatusFromCache(dc *DirectoryCache, mainSkiplist *skiplistWrapper, cachePath string) *StatusResult {
	result := &StatusResult{
		Modified: make([]string, 0),
		Added:    make([]string, 0),
		Deleted:  make([]string, 0),
	}

	// Load the cache file that was just written
	if _, err := os.Stat(cachePath); err != nil {
		return result // No cache file (e.g. pipeline failed) — empty result
	}

	refs, indexFile, err := dc.loadIndexFromFileWithTracking(cachePath)
	if err != nil {
		if IsDebugEnabled("scan") {
			fmt.Fprintf(os.Stderr, "[STATUS] Warning: failed to load cache for status derivation: %v\n", err)
		}
		return result
	}

	// Build a temporary skiplist from cache entries for iteration
	cacheResult := NewSkiplistWrapper(16, CacheContext)
	if indexFile != nil {
		cacheResult.AddIndexReference(indexFile)
	}
	for _, ref := range refs {
		cacheResult.Insert(ref, CacheContext)
	}

	// Each cache entry is a change — categorise it
	cacheResult.ForEach(func(entry *binaryEntry, _ string) bool {
		path := entry.RelativePath()

		if entry.IsDeleted() {
			result.Deleted = append(result.Deleted, path)
			return true
		}

		// Check if this path exists in main
		if mainEntry, _ := mainSkiplist.Find(path); mainEntry != nil {
			result.Modified = append(result.Modified, path)
		} else {
			result.Added = append(result.Added, path)
		}
		return true
	})

	return result
}

// HasChanges returns true if there are any changes
func (sr *StatusResult) HasChanges() bool {
	return len(sr.Modified) > 0 || len(sr.Added) > 0 || len(sr.Deleted) > 0
}

// TotalChanges returns the total number of changed files
func (sr *StatusResult) TotalChanges() int {
	return len(sr.Modified) + len(sr.Added) + len(sr.Deleted)
}
