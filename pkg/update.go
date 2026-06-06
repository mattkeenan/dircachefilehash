package dircachefilehash

import (
	"context"
	"fmt"
	"os"
)

// changeCollector accumulates op-classified relative paths during an
// update so the post-run interactive-tree viewer can label each path
// Added/Modified/Deleted. It is written ONLY by the single comparison
// goroutine (via the canonical scanWriteSink — see comparison_sink.go)
// and read ONLY after RunUpdatePipeline returns (wg.Wait complete), so
// no lock is needed. A nil *changeCollector means "don't collect" — the
// default, non-interactive path.
type changeCollector struct {
	added    []string
	modified []string
	deleted  []string
}

// add records one op-classified path. OpUnchanged is ignored.
func (c *changeCollector) add(op PipelineOp, path string) {
	if c == nil {
		return
	}
	switch op {
	case OpNewFile:
		c.added = append(c.added, path)
	case OpModified:
		c.modified = append(c.modified, path)
	case OpDeleted:
		c.deleted = append(c.deleted, path)
	}
}

// runUpdate scans the whole repository and updates the index using the
// pipeline. It is the non-collecting, whole-repo entry point used
// throughout the codebase and tests; path-scoped and/or change-set
// collecting updates go through runUpdateCollecting (used by Apply).
func runUpdate(ctx context.Context, ms *MetaStore, sr *ScanRun, flags map[string]string) error {
	return runUpdateCollecting(ctx, ms, sr, flags, nil)
}

// runUpdateCollecting is runUpdate with an optional change collector
// threaded to the canonical update pass only (never the cache-refresh
// delta pass — see updateSpecificPaths). collector may be nil.
func runUpdateCollecting(ctx context.Context, ms *MetaStore, sr *ScanRun, flags map[string]string, collector *changeCollector, paths ...string) error {
	ms.applyOverridesToScanRun(sr, flags)

	if len(paths) == 0 {
		// No specific paths: update entire repository - put everything in main index
		return ms.updateFullRepository(ctx, sr, collector)
	}
	// Specific paths: selective update - manage main vs cache indices
	return ms.updateSpecificPaths(ctx, sr, paths, collector)
}

// updateFullRepository updates the entire repository: everything goes into
// the main index, and the cache index is removed.
func (ms *MetaStore) updateFullRepository(ctx context.Context, sr *ScanRun, collector *changeCollector) error {
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
	err = ms.performPipelineScan(ctx, sr, []string{}, comparisonSkiplist, collector)
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
func (ms *MetaStore) updateSpecificPaths(ctx context.Context, sr *ScanRun, paths []string, collector *changeCollector) error {
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

	// Pipeline: comparison → hash → reorder → write, with atomic rename on
	// success. The collector attaches to THIS canonical pass only.
	err = ms.performPipelineScan(ctx, sr, paths, comparisonSkiplist, collector)
	if err != nil {
		return fmt.Errorf("update interrupted: %w", err)
	}
	// Refresh cache.idx so it reflects the new main index state. The
	// collector is deliberately NOT passed here — this delta pass would
	// otherwise re-record the same paths and corrupt the change-set.
	if _, err := ms.refreshFsScanCache(ctx, sr); err != nil {
		return fmt.Errorf("failed to update cache: %w", err)
	}

	_ = ms.checkForOrphanedIndexFiles()
	return nil
}
