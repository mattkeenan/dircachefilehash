package dircachefilehash

import (
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
	// Create empty skiplist for comparison (full scan)
	emptySkiplist := NewSkiplistWrapper(16, "empty")

	// Use new scan workflow to get all files
	scanSkiplist, err := dc.PerformHwangLinScanToSkiplist([]string{}, emptySkiplist)
	if err != nil {
		return fmt.Errorf("failed to scan repository: %w", err)
	}

	// Write everything to main index using vectorio
	tempIndexPath := dc.generateTempFileName("index")
	if err := dc.WriteSkiplistWithVectorIO(scanSkiplist, tempIndexPath, ""); err != nil {
		os.Remove(tempIndexPath)
		return fmt.Errorf("failed to write new index: %w", err)
	}

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
	// Load main index to use as comparison base
	mainSkiplist, err := dc.LoadMainIndex()
	if err != nil {
		return fmt.Errorf("failed to load main index: %w", err)
	}

	// Use new scan workflow with main index as comparison to get only changes in specified paths
	scanSkiplist, err := dc.PerformHwangLinScanToSkiplist(paths, mainSkiplist)
	if err != nil {
		return fmt.Errorf("failed to scan specified paths: %w", err)
	}

	// Merge scan results with main index (scan results take precedence)
	updatedMainSkiplist := mainSkiplist.Copy()
	if err := updatedMainSkiplist.Merge(scanSkiplist, MergeTheirs); err != nil {
		return fmt.Errorf("failed to merge scan results with main index: %w", err)
	}

	// Write new main index using vectorio
	tempIndexPath := dc.generateTempFileName("index")
	if err := dc.WriteSkiplistWithVectorIO(updatedMainSkiplist, tempIndexPath, MainContext); err != nil {
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
