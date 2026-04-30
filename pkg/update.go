package dircachefilehash

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
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
		return dc.updateFullRepositoryUnified(ctx)
	} else {
		// Specific paths: selective update - manage main vs cache indices
		return dc.updateSpecificPathsUnified(ctx, paths)
	}
}

// updateFullRepositoryUnified updates the entire repository using the unified hwangLinUnified architecture
func (dc *DirectoryCache) updateFullRepositoryUnified(ctx context.Context) error {
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

// updateFullRepository has been moved to v0.6/pkg/update.go as part of the v0.7 unified
// architecture migration. Use updateFullRepositoryUnified() instead.

// updateSpecificPathsUnified updates only specified paths using the unified hwangLinUnified architecture
func (dc *DirectoryCache) updateSpecificPathsUnified(ctx context.Context, paths []string) error {
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
	// Refresh cache.idx via the modern pipeline so it reflects the new main
	// index state (same path dcfh status takes).
	if _, err := dc.refreshFsScanCache(ctx); err != nil {
		return fmt.Errorf("failed to update cache: %w", err)
	}

	_ = dc.checkForOrphanedIndexFiles()
	return nil
}

// updateSpecificPaths has been moved to v0.6/pkg/update.go as part of the v0.7 unified
// architecture migration. Use updateSpecificPathsUnified() instead.

// performUnifiedScanToSkiplist performs scan using the old callback-driven architecture.
// This function is retained only for recovery.go which still depends on it.
//
// Deprecated: The update path now uses performPipelineScan.
func (dc *DirectoryCache) performUnifiedScanToSkiplist(ctx context.Context, paths []string, compareSkiplist *skiplistWrapper) (*skiplistWrapper, error) {
	defer VerboseEnter()()

	// Synchronise concurrent scans - only one scan per DirectoryCache at a time
	dc.scanMutex.Lock()
	defer dc.scanMutex.Unlock()

	// If a scan is already in progress, wait for it and return the same results
	if dc.scanInProgress {
		if dc.lastScanError != nil {
			return nil, dc.lastScanError
		}
		return dc.lastScanResult, nil
	}

	// Mark scan as in progress
	dc.scanInProgress = true
	defer func() {
		dc.scanInProgress = false
	}()

	// v0.7: Generate timestamped main index filename for persistent strategy
	tempMainIndexFileName := dc.GenerateTimestampedFileName("main")

	var operationSuccessful bool
	defer func() { finaliseMainIndex(dc, tempMainIndexFileName, "[UPDATE]", operationSuccessful) }()

	// v0.7: No scan index needed - UpdateCallback writes directly to temp main index

	// Create hash job manager for concurrent hashing (reuse existing infrastructure)
	hashJobManager := dc.newAlgorithmHashManager(ctx, dc.hashWorkers)
	defer hashJobManager.Shutdown()

	// Create iterators for unified algorithm
	existingIterator := NewBinaryEntrySkiplistIterator(ctx, compareSkiplist, "existing")
	scanIterator := NewUnifiedFilesystemScanIterator(ctx, dc, paths, "scan")

	// Create update callback for v0.7 direct temp index writing
	updateCallback := NewUpdateCallback(ctx, dc, tempMainIndexFileName, hashJobManager)

	// Run unified algorithm
	scanErr := hwangLinUnified(existingIterator, scanIterator, updateCallback, ctx)
	if scanErr != nil {
		// v0.7: Mark operation as failed for proper cleanup
		operationSuccessful = false
		dc.lastScanResult = nil
		dc.lastScanError = scanErr
		return nil, scanErr
	}

	// Signal that no more hash jobs will be submitted (same as original)
	hashJobManager.FinishSubmitting()

	// v0.7: UpdateCallback has written directly to temp main index file
	// Mark operation as successful for atomic rename
	operationSuccessful = true

	// For now, return empty skiplist to maintain compatibility
	emptySkiplist := NewSkiplistWrapper(16, ScanContext)
	dc.lastScanResult = emptySkiplist
	dc.lastScanError = nil

	return emptySkiplist, nil
}

// loadIndexWithProcessor loads an index file with processor and returns a skiplist
func (dc *DirectoryCache) loadIndexWithProcessor(filePath string, processor EntryProcessor) (*skiplistWrapper, error) {
	// Load entries using existing processor function
	entries, err := dc.LoadIndexFromFileWithProcessor(filePath, processor)
	if err != nil {
		return nil, err
	}

	// Create new skiplist
	skiplist := NewSkiplistWrapper(len(entries), CacheContext)

	// Add entries to skiplist
	for _, entryRef := range entries {
		skiplist.Insert(entryRef, CacheContext)
	}

	return skiplist, nil
}

// ScanFileInfo represents information about a scan index file
type ScanFileInfo struct {
	Path    string
	ModTime time.Time
	Size    int64
}

// findScanIndexFiles finds all scan index files and returns them sorted by modification time (newest first)
func (dc *DirectoryCache) findScanIndexFiles() ([]ScanFileInfo, error) {
	metaDir := dc.MetaDir
	entries, err := os.ReadDir(metaDir)
	if err != nil {
		return nil, err
	}

	var scanFiles []ScanFileInfo

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()

		// Check if it's a scan index file (scan-<pid>-<tid>.idx pattern)
		if filepath.Ext(name) == ".idx" &&
			(len(name) > 9 && name[:5] == "scan-") {
			filePath := filepath.Join(metaDir, name)

			// Get file info
			info, err := entry.Info()
			if err != nil {
				continue // Skip files we can't stat
			}

			scanFiles = append(scanFiles, ScanFileInfo{
				Path:    filePath,
				ModTime: info.ModTime(),
				Size:    info.Size(),
			})
		}
	}

	// Sort by modification time (newest first)
	sort.Slice(scanFiles, func(i, j int) bool {
		return scanFiles[i].ModTime.After(scanFiles[j].ModTime)
	})

	return scanFiles, nil
}
