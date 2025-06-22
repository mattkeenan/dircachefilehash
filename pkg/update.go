package dircachefilehash

import (
	"context"
	"fmt"
	"os"
)

// Update scans the directory and updates the index file using the new workflow
func (dc *DirectoryCache) Update(paths ...string) error {
	if len(paths) == 0 {
		// No specific paths: update entire repository - put everything in main index
		return dc.updateFullRepository()
	} else {
		// Specific paths: selective update - manage main vs cache indices
		return dc.updateSpecificPaths(paths)
	}
}

// updateFullRepository updates the entire repository and puts everything in main index
func (dc *DirectoryCache) updateFullRepository() error {
	// Use scan.go to perform full directory scan
	ctx := context.Background()

	// Create empty skiplist for comparison (full scan)
	emptySkiplist := NewSkiplistWrapper(16, "empty")

	// Use Hwang-Lin scan to get all files
	results, err := dc.PerformHwangLinScan(ctx, []string{}, emptySkiplist)
	if err != nil {
		return fmt.Errorf("failed to scan repository: %w", err)
	}

	// Process results into entries for writing
	processedEntries, err := dc.ProcessHwangLinResults(results)
	if err != nil {
		return fmt.Errorf("failed to process scan results: %w", err)
	}

	// Write everything to main index using ProcessedEntry writing
	tempIndexPath := dc.generateTempFileName("index")
	oldIndexFile := dc.IndexFile
	dc.IndexFile = tempIndexPath

	if err := dc.WriteProcessedEntries(processedEntries, 0); err != nil {
		dc.IndexFile = oldIndexFile
		os.Remove(tempIndexPath)
		return fmt.Errorf("failed to write new index: %w", err)
	}
	dc.IndexFile = oldIndexFile

	// Atomic replace main index
	if err := os.Rename(tempIndexPath, dc.IndexFile); err != nil {
		os.Remove(tempIndexPath) // Cleanup on failure
		return fmt.Errorf("failed to rename index file: %w", err)
	}

	// Remove cache file since everything is now in main index
	os.Remove(dc.CacheFile) // Non-fatal if it fails
	dc.CleanupTempFiles()

	return nil
}

// updateSpecificPaths updates only specified paths and manages main index vs cache
func (dc *DirectoryCache) updateSpecificPaths(paths []string) error {
	ctx := context.Background()

	// Load main index to use as comparison base
	mainSkiplist, err := dc.LoadMainIndex()
	if err != nil {
		return fmt.Errorf("failed to load main index: %w", err)
	}

	// Use Hwang-Lin scan with main index as comparison to get only changes in specified paths
	results, err := dc.PerformHwangLinScan(ctx, paths, mainSkiplist)
	if err != nil {
		return fmt.Errorf("failed to scan specified paths: %w", err)
	}

	// Process results into entries
	processedEntries, err := dc.ProcessHwangLinResults(results)
	if err != nil {
		return fmt.Errorf("failed to process scan results: %w", err)
	}

	// Create new main index by merging old main with updated entries
	// First, write processed entries to a temp file to get binaryEntry pointers
	tempScanPath := dc.generateTempFileName("scan")
	oldIndexFile := dc.IndexFile
	dc.IndexFile = tempScanPath

	if err := dc.WriteProcessedEntries(processedEntries, 0); err != nil {
		dc.IndexFile = oldIndexFile
		os.Remove(tempScanPath)
		return fmt.Errorf("failed to write temp scan results: %w", err)
	}

	// Load the temp scan results as binaryEntry pointers
	scanEntries, err := dc.LoadIndexFromFile(tempScanPath)
	dc.IndexFile = oldIndexFile
	os.Remove(tempScanPath)

	if err != nil {
		return fmt.Errorf("failed to load temp scan results: %w", err)
	}

	// Create updated main index
	updatedMainSkiplist := mainSkiplist.Copy()

	// Remove old entries for the updated paths and add new ones
	updatedPaths := make(map[string]bool)
	for _, entry := range scanEntries {
		path := entry.RelativePath()
		updatedPaths[path] = true

		// Remove old version if it exists
		updatedMainSkiplist.Delete(path)

		// Add new version
		updatedMainSkiplist.Insert(entry, MainContext)
	}

	// Write new main index
	tempIndexPath := dc.generateTempFileName("index")
	if err := dc.WriteSkiplistToTmpIndex(updatedMainSkiplist, tempIndexPath, MainContext); err != nil {
		return fmt.Errorf("failed to write new index: %w", err)
	}

	// Atomic replace main index
	if err := os.Rename(tempIndexPath, dc.IndexFile); err != nil {
		os.Remove(tempIndexPath) // Cleanup on failure
		return fmt.Errorf("failed to rename index file: %w", err)
	}

	// Update cache using the new workflow
	if err := dc.UpdateCacheIndexWithWorkflow(); err != nil {
		return fmt.Errorf("failed to update cache: %w", err)
	}

	dc.CleanupTempFiles()
	return nil
}

// fileJob represents a file hashing job (kept for compatibility with index.go)
type fileJob struct {
	path    string
	info    os.FileInfo
	relPath string
	index   int
}
