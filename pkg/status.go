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

// Status compares main.idx against the current filesystem, writing changes
// to cache.idx, and returns a structured StatusResult describing the diff.
// It is a thin wrapper over Diff(main, fs-scan); the cache write is the
// side-effect of opening fs-scan. The --v clean-status snapshot remains
// a Status-only feature, attached to the result here.
//
// filter, when non-nil, narrows the reported result without affecting
// the cache write — the cache always reflects on-disk truth so a future
// status without the filter sees the same state.
func (ms *MetaStore) Status(ctx context.Context, sr *ScanRun, flags map[string]string, filter FilterExpr) (*StatusResult, error) {
	defer VerboseEnter()()

	res, _ := ms.ApplyConfigOverrides(flags)
	// Authoritative post-override values flow into sr; the caller may
	// have built sr before flags were applied.
	if sr != nil {
		sr.SymlinkMode = res.SymlinkMode
		sr.HashWorkers = res.HashWorkers
	}

	result, err := Diff(ctx, ms, sr, IndexRef{Type: RefTypeMain}, IndexRef{Type: RefTypeFsScan}, filter)
	if err != nil {
		return result, err
	}

	if verbose, exists := flags["v"]; exists && verbose != "" {
		if level, atoiErr := strconv.Atoi(verbose); atoiErr == nil && level > 0 {
			result.CleanStatus = collectCleanStatus(ms)
		}
	}

	return result, nil
}

// refreshFsScanCache runs the cache-refreshing scan pipeline that used to
// live inline inside ms.Status. It loads main + cache, runs the 4-stage
// pipeline (writing changes to a fresh cache-{ts}.idx, renamed to cache.idx
// on success), then re-loads the post-rename cache and merges it over the
// in-memory main. The returned skiplist IS the cache+main view callers
// need next — no Copy and no second cache load required at the call site.
//
// On error the skiplist is nil; the cache may still have been partially
// written (the timestamped file is left in place for startup merge).
func (ms *MetaStore) refreshFsScanCache(ctx context.Context, sr *ScanRun) (*skiplistWrapper, error) {
	mainSkiplist, err := ms.LoadMainIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to load main index: %w", err)
	}
	if IsDebugEnabled("scan") {
		VerboseLog(3, "refreshFsScanCache: mainSkiplist length = %d", mainSkiplist.Length())
	}

	cacheSkiplistPre, err := ms.loadCacheIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to load cache index: %w", err)
	}
	if IsDebugEnabled("scan") {
		VerboseLog(3, "refreshFsScanCache: cacheSkiplist length = %d", cacheSkiplistPre.Length())
	}

	cacheTempFileName := ms.GenerateTimestampedFileName("cache")

	existingIterator := NewBinaryEntrySkiplistIterator(ctx, mainSkiplist, "existing")
	scanIterator := NewFilesystemScanIterator(ctx, sr, []string{}, "scan")

	scanErr := RunStatusPipeline(ctx, ms, sr, cacheSkiplistPre, existingIterator, scanIterator, cacheTempFileName)
	finaliseStatusCache(ms, cacheTempFileName, scanErr == nil)

	if scanErr != nil {
		if IsDebugEnabled("scan") {
			fmt.Fprintf(os.Stderr, "[STATUS] Pipeline error: %v\n", scanErr)
		}
		return nil, scanErr
	}
	if IsDebugEnabled("scan") {
		fmt.Fprintf(os.Stderr, "[STATUS] Pipeline completed; cache.idx refreshed\n")
	}

	// The pipeline renamed cache-{ts}.idx → cache.idx, so cacheSkiplistPre is
	// stale. Load the post-rename cache and overlay it on mainSkiplist.
	// mainSkiplist has no other consumers at this point — mutate in place,
	// no Copy needed.
	cacheSkiplistPost, err := ms.loadCacheIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to reload cache index: %w", err)
	}
	if err := mainSkiplist.Merge(cacheSkiplistPost, MergeTheirs); err != nil {
		return nil, fmt.Errorf("failed to merge cache over main: %w", err)
	}
	return mainSkiplist, nil
}

// finaliseStatusCache handles the success/failure branches of the
// Status cache lifecycle: rename to cache.idx and cleanup on success,
// leave the timestamped file for startup merge on failure.
func finaliseStatusCache(ms *MetaStore, cacheTempFileName string, ok bool) {
	if !ok {
		if IsDebugEnabled("scan") {
			fmt.Fprintf(os.Stderr, "[STATUS] Operation incomplete - leaving %s for startup merge\n", filepath.Base(cacheTempFileName))
		}
		return
	}
	if _, err := os.Stat(cacheTempFileName); err != nil {
		return
	}
	if renameErr := os.Rename(cacheTempFileName, ms.CacheFile); renameErr != nil {
		if IsDebugEnabled("scan") {
			fmt.Fprintf(os.Stderr, "[STATUS] Warning: failed to rename %s to cache.idx: %v\n", cacheTempFileName, renameErr)
		}
		return
	}
	if cleanupErr := ms.CleanupTimestampedCacheFiles(); cleanupErr != nil && IsDebugEnabled("scan") {
		fmt.Fprintf(os.Stderr, "[STATUS] Warning: failed to cleanup timestamped cache files: %v\n", cleanupErr)
	}
}

// collectCleanStatus gathers the verbose --v clean-status snapshot:
// whether main/cache indices load cleanly and which temp indices are
// present. Stat failures collapse to "false" rather than propagating.
func collectCleanStatus(ms *MetaStore) *CleanStatus {
	cs := &CleanStatus{}
	if _, err := os.Stat(ms.IndexFile); err == nil {
		cs.MainIndex = true
	}
	if _, err := os.Stat(ms.CacheFile); err == nil {
		cs.CacheIndex = true
	}
	if tempFiles, err := ms.scanForTempIndices(); err == nil {
		cs.TempIndices = tempFiles
		cs.HasTempFiles = len(tempFiles) > 0
	}
	return cs
}

// HasChanges returns true if there are any changes
func (sr *StatusResult) HasChanges() bool {
	return len(sr.Modified) > 0 || len(sr.Added) > 0 || len(sr.Deleted) > 0
}

// TotalChanges returns the total number of changed files
func (sr *StatusResult) TotalChanges() int {
	return len(sr.Modified) + len(sr.Added) + len(sr.Deleted)
}
