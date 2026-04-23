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
	Modified      []string     `json:"modified"`
	Added         []string     `json:"added"`
	Deleted       []string     `json:"deleted"`
	ModifiedBytes int64        `json:"modified_bytes"`
	AddedBytes    int64        `json:"added_bytes"`
	DeletedBytes  int64        `json:"deleted_bytes"`
	CleanStatus   *CleanStatus `json:"clean_status,omitempty"` // Only included when verbose
}

// Status compares main.idx against the current filesystem, writing changes to cache.idx.
// The cache is a sparse delta — its entries ARE the status (modified, added, deleted).
func (dc *DirectoryCache) Status(ctx context.Context, flags map[string]string) (*StatusResult, error) {
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

	var operationSuccessful bool
	defer func() { finaliseStatusCache(dc, cacheTempFileName, operationSuccessful) }()

	// Create iterators: main.idx (left) vs filesystem (right)
	existingIterator := NewBinaryEntrySkiplistIterator(ctx, mainSkiplist, "existing")
	scanIterator := NewUnifiedFilesystemScanIterator(ctx, dc, []string{}, "scan")

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

	if verbose, exists := flags["v"]; exists && verbose != "" {
		if level, err := strconv.Atoi(verbose); err == nil && level > 0 {
			result.CleanStatus = collectCleanStatus(dc)
		}
	}

	return result, nil
}

// finaliseStatusCache handles the success/failure branches of the
// Status cache lifecycle: rename to cache.idx and cleanup on success,
// leave the timestamped file for startup merge on failure.
func finaliseStatusCache(dc *DirectoryCache, cacheTempFileName string, ok bool) {
	if !ok {
		if IsDebugEnabled("scan") {
			fmt.Fprintf(os.Stderr, "[STATUS] Operation incomplete - leaving %s for startup merge\n", filepath.Base(cacheTempFileName))
		}
		return
	}
	if _, err := os.Stat(cacheTempFileName); err != nil {
		return
	}
	if renameErr := os.Rename(cacheTempFileName, dc.CacheFile); renameErr != nil {
		if IsDebugEnabled("scan") {
			fmt.Fprintf(os.Stderr, "[STATUS] Warning: failed to rename %s to cache.idx: %v\n", cacheTempFileName, renameErr)
		}
		return
	}
	if cleanupErr := dc.CleanupTimestampedCacheFiles(); cleanupErr != nil && IsDebugEnabled("scan") {
		fmt.Fprintf(os.Stderr, "[STATUS] Warning: failed to cleanup timestamped cache files: %v\n", cleanupErr)
	}
}

// collectCleanStatus gathers the verbose --v clean-status snapshot:
// whether main/cache indices load cleanly and which temp indices are
// present. Stat failures collapse to "false" rather than propagating.
func collectCleanStatus(dc *DirectoryCache) *CleanStatus {
	cs := &CleanStatus{}
	if _, err := os.Stat(dc.IndexFile); err == nil {
		cs.MainIndex = true
	}
	if _, err := os.Stat(dc.CacheFile); err == nil {
		cs.CacheIndex = true
	}
	if tempFiles, err := dc.scanForTempIndices(); err == nil {
		cs.TempIndices = tempFiles
		cs.HasTempFiles = len(tempFiles) > 0
	}
	return cs
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

	refs, _, err := dc.loadIndexFromFileWithTracking(cachePath)
	if err != nil {
		// No cache file (pipeline failed or empty) — return empty result
		return result
	}

	// Iterate refs directly — they're already path-sorted from the pipeline's
	// reorder stage, so no skiplist needed.
	for _, ref := range refs {
		entry := ref.GetBinaryEntry()
		if entry == nil {
			continue
		}
		path := entry.RelativePath()
		size := int64(entry.FileSize)

		if entry.IsDeleted() {
			result.Deleted = append(result.Deleted, path)
			result.DeletedBytes += size
			continue
		}

		if mainEntry, _ := mainSkiplist.Find(path); mainEntry != nil {
			result.Modified = append(result.Modified, path)
			result.ModifiedBytes += size
		} else {
			result.Added = append(result.Added, path)
			result.AddedBytes += size
		}
	}

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
