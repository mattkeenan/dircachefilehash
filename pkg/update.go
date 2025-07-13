package dircachefilehash

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Update scans the directory and updates the index file using the new workflow
func (dc *DirectoryCache) Update(shutdownChan <-chan struct{}, flags map[string]string, paths ...string) error {
	// Apply flags before scanning
	if err := dc.ApplyConfigOverrides(flags); err != nil {
		// If no config loaded, apply symlink mode directly if provided
		if symlinkMode, exists := flags["symlinks"]; exists {
			dc.symlinkMode = symlinkMode
		}
	}
	
	if len(paths) == 0 {
		// No specific paths: update entire repository - put everything in main index
		return dc.updateFullRepositoryUnified(shutdownChan)
	} else {
		// Specific paths: selective update - manage main vs cache indices
		return dc.updateSpecificPathsUnified(shutdownChan, paths)
	}
}

// updateFullRepositoryUnified updates the entire repository using the unified hwangLinUnified architecture
func (dc *DirectoryCache) updateFullRepositoryUnified(shutdownChan <-chan struct{}) error {
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

	// Use unified scan workflow to get all files
	scanSkiplist, err := dc.performUnifiedScanToSkiplist(shutdownChan, []string{}, comparisonSkiplist)
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

// updateFullRepository updates the entire repository and puts everything in main index
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

// updateSpecificPathsUnified updates only specified paths using the unified hwangLinUnified architecture
func (dc *DirectoryCache) updateSpecificPathsUnified(shutdownChan <-chan struct{}, paths []string) error {
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

	// Use unified scan workflow with merged index as comparison to get only changes in specified paths
	scanSkiplist, err := dc.performUnifiedScanToSkiplist(shutdownChan, paths, comparisonSkiplist)
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

	// Update cache using the unified workflow
	if _, err := dc.updateCacheIndexWithWorkflowUnified(shutdownChan); err != nil {
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

// updateSpecificPaths updates only specified paths and manages main index vs cache
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

	// Update cache using the new workflow
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

// performUnifiedScanToSkiplist performs scan using the unified hwangLinUnified architecture
// This replaces the complex performHwangLinScanToSkiplist function (300+ lines) with the unified infrastructure
func (dc *DirectoryCache) performUnifiedScanToSkiplist(shutdownChan <-chan struct{}, paths []string, compareSkiplist *skiplistWrapper) (*skiplistWrapper, error) {
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

	// Generate scan index filename for this operation
	scanFileName := dc.generateScanFileName()

	// Initialise scan index with mmap
	if err := dc.initialiseScanIndex(scanFileName); err != nil {
		return nil, fmt.Errorf("failed to initialise scan index: %w", err)
	}

	// Create hash job manager for concurrent hashing (reuse existing infrastructure)
	hashJobManager := dc.newAlgorithmHashManager(dc.hashWorkers, shutdownChan)
	defer hashJobManager.Shutdown()

	// Create iterators for unified algorithm
	existingIterator := NewBinaryEntrySkiplistIterator(compareSkiplist, "existing")
	scanIterator := NewUnifiedFilesystemScanIterator(dc, paths, "scan")

	// Create update callback that replicates hwangLinCompareToSkiplist logic
	updateCallback := NewUpdateCallback(dc, scanFileName, hashJobManager)

	// Run unified algorithm (replaces the complex internal logic)
	if err := hwangLinUnified(existingIterator, scanIterator, updateCallback); err != nil {
		// Return partial results if available (same interruption handling as before)
		scanSkiplist := updateCallback.GetResultSkiplist()
		dc.lastScanResult = scanSkiplist
		dc.lastScanError = err
		return scanSkiplist, err
	}

	// Signal that no more hash jobs will be submitted (same as original)
	hashJobManager.FinishSubmitting()

	// Get final result skiplist  
	scanSkiplist := updateCallback.GetResultSkiplist()
	dc.lastScanResult = scanSkiplist
	dc.lastScanError = nil

	return scanSkiplist, nil
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
	// Get the .dcfh directory from the IndexFile path
	dcfhDir := filepath.Dir(dc.IndexFile)

	// Read the .dcfh directory
	entries, err := os.ReadDir(dcfhDir)
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
			filePath := filepath.Join(dcfhDir, name)

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
