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
	if len(paths) == 0 {
		// No specific paths: update entire repository - put everything in main index
		return dc.updateFullRepository(shutdownChan)
	} else {
		// Specific paths: selective update - manage main vs cache indices
		return dc.updateSpecificPaths(shutdownChan, paths)
	}
}

// updateFullRepository updates the entire repository and puts everything in main index
func (dc *DirectoryCache) updateFullRepository(shutdownChan <-chan struct{}) error {
	// Create empty skiplist for comparison (full scan)
	emptySkiplist := NewSkiplistWrapper(16, "empty")

	// Use new scan workflow to get all files
	scanSkiplist, err := dc.performHwangLinScanToSkiplist(shutdownChan, []string{}, emptySkiplist)
	if err != nil {
		return fmt.Errorf("failed to scan repository: %w", err)
	}

	// Write everything to main index using vectorio (exclude deleted entries)
	tempIndexPath := dc.generateTempFileName("index")
	if err := dc.writeMainIndexWithVectorIO(scanSkiplist, tempIndexPath, ""); err != nil {
		os.Remove(tempIndexPath)
		return fmt.Errorf("failed to write new index: %w", err)
	}

	// Cleanup scan index file now that temp index is written
	if err := dc.cleanupCurrentScanFile(); err != nil && !os.IsNotExist(err) {
		// Non-fatal, but log the error
		fmt.Fprintf(os.Stderr, "Warning: failed to cleanup scan file: %v\n", err)
	}

	// Atomic replace main index
	if err := os.Rename(tempIndexPath, dc.IndexFile); err != nil {
		os.Remove(tempIndexPath) // Cleanup on failure
		return fmt.Errorf("failed to rename index file: %w", err)
	}

	// Remove cache file since everything is now in main index
	os.Remove(dc.CacheFile) // Non-fatal if it fails
	dc.checkForOrphanedIndexFiles()

	return nil
}

// updateSpecificPaths updates only specified paths and manages main index vs cache
func (dc *DirectoryCache) updateSpecificPaths(shutdownChan <-chan struct{}, paths []string) error {
	// Load main index to use as comparison base
	mainSkiplist, err := dc.LoadMainIndex()
	if err != nil {
		return fmt.Errorf("failed to load main index: %w", err)
	}

	// Use new scan workflow with main index as comparison to get only changes in specified paths
	scanSkiplist, err := dc.performHwangLinScanToSkiplist(shutdownChan, paths, mainSkiplist)
	if err != nil {
		return fmt.Errorf("failed to scan specified paths: %w", err)
	}

	// Merge scan results with main index (scan results take precedence)
	updatedMainSkiplist := mainSkiplist.Copy()
	if err := updatedMainSkiplist.Merge(scanSkiplist, MergeTheirs); err != nil {
		return fmt.Errorf("failed to merge scan results with main index: %w", err)
	}

	// Write new main index using vectorio (exclude deleted entries)
	tempIndexPath := dc.generateTempFileName("index")
	if err := dc.writeMainIndexWithVectorIO(updatedMainSkiplist, tempIndexPath, MainContext); err != nil {
		return fmt.Errorf("failed to write new index: %w", err)
	}

	// Cleanup scan index file now that temp index is written
	if err := dc.cleanupCurrentScanFile(); err != nil && !os.IsNotExist(err) {
		// Non-fatal, but log the error
		fmt.Fprintf(os.Stderr, "Warning: failed to cleanup scan file: %v\n", err)
	}

	// Atomic replace main index
	if err := os.Rename(tempIndexPath, dc.IndexFile); err != nil {
		os.Remove(tempIndexPath) // Cleanup on failure
		return fmt.Errorf("failed to rename index file: %w", err)
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


