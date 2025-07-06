package dircachefilehash

import (
	"fmt"
	"os"
	"strconv"
	"strings"
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

// Status compares the current directory state with the loaded index using the new workflow
func (dc *DirectoryCache) Status(shutdownChan <-chan struct{}, flags map[string]string) (*StatusResult, error) {
	defer VerboseEnter()()
	// Use the new cache update workflow which implements steps 1-11 as specified
	// This returns the scan result which we can reuse to avoid duplicate scans
	currentSkiplist, err := dc.updateCacheIndexWithWorkflow(shutdownChan)
	if err != nil {
		return nil, fmt.Errorf("failed to update cache index: %w", err)
	}

	// Load both main and cache indices for comparison
	mainSkiplist, err := dc.LoadMainIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to load main index: %w", err)
	}
	if IsDebugEnabled("scan") {
		VerboseLog(3, "Status: mainSkiplist length = %d", mainSkiplist.Length())
	}

	cacheSkiplist, err := dc.loadCacheIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to load cache index: %w", err)
	}
	if IsDebugEnabled("scan") {
		VerboseLog(3, "Status: cacheSkiplist length = %d", cacheSkiplist.Length())
	}

	// Status compares main index (committed files) vs scan result (current disk state)

	result := &StatusResult{
		Modified: make([]string, 0),
		Added:    make([]string, 0),
		Deleted:  make([]string, 0),
	}

	// Check for verbose flag and include clean status if requested
	if verboseLevel, exists := flags["v"]; exists && verboseLevel != "" {
		if level, err := strconv.Atoi(verboseLevel); err == nil && level > 0 {
			result.CleanStatus = &CleanStatus{}

			// Check main index clean status
			if dc.mmapIndex != nil && dc.mmapIndex.Header() != nil {
				result.CleanStatus.MainIndex = dc.mmapIndex.Header().isClean()
			}

			// Check cache index clean status by loading it
			cacheSkiplist, err := dc.loadCacheIndex()
			if err == nil && cacheSkiplist != nil {
				// For cache index, we need to access the underlying mmap - this is a bit tricky
				// For now, we'll assume it's clean if it loaded successfully
				// TODO: Improve this to actually check the cache index header
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

	// Use Hwang-Lin merge algorithm to compare states
	if IsDebugEnabled("scan") {
		VerboseLog(3, "Status: mainSkiplist length = %d", mainSkiplist.Length())
		VerboseLog(3, "Status: currentSkiplist length = %d", currentSkiplist.Length())
	}
	dc.hwangLinStatus(mainSkiplist, currentSkiplist, func(status FileStatus, path string, indexEntry, diskEntry *binaryEntry) {
		if IsDebugEnabled("scan") {
			VerboseLog(3, "Status callback: %s -> %d", path, int(status))
		}
		switch status {
		case StatusModified:
			result.Modified = append(result.Modified, path)
		case StatusAdded:
			result.Added = append(result.Added, path)
		case StatusDeleted:
			result.Deleted = append(result.Deleted, path)
		}
	})

	// Now that Status comparison is complete, cleanup scan index file
	if err := dc.cleanupCurrentScanFile(); err != nil && !os.IsNotExist(err) {
		// Non-fatal, but log the error
		fmt.Fprintf(os.Stderr, "Warning: failed to cleanup scan file: %v\n", err)
	}

	return result, nil
}

// hwangLinStatus implements the Hwang-Lin merge algorithm using direct skiplist iteration (zero-copy)
func (dc *DirectoryCache) hwangLinStatus(mainSkiplist, scanSkiplist *skiplistWrapper,
	callback func(status FileStatus, path string, indexEntry, diskEntry *binaryEntry)) {

	// Use direct iteration instead of creating slices
	indexCurrent := mainSkiplist.skiplist.First()
	diskCurrent := scanSkiplist.skiplist.First()

	if IsDebugEnabled("scan") {
		VerboseLog(3, "hwangLinStatus: starting, indexCurrent=%v, diskCurrent=%v", indexCurrent != nil, diskCurrent != nil)
	}

	for indexCurrent != nil && diskCurrent != nil {
		indexRef := indexCurrent.Item()
		diskRef := diskCurrent.Item()

		indexEntry := indexRef.GetBinaryEntry()
		diskEntry := diskRef.GetBinaryEntry()

		if indexEntry == nil {
			// This should never happen - indicates a serious bug
			fmt.Fprintf(os.Stderr, "[ERROR] GetBinaryEntry returned nil for index entry - this should never happen\n")
			indexCurrent = indexCurrent.Next()
			continue
		}
		if diskEntry == nil {
			// This should never happen - indicates a serious bug
			fmt.Fprintf(os.Stderr, "[ERROR] GetBinaryEntry returned nil for disk entry - this should never happen\n")
			diskCurrent = diskCurrent.Next()
			continue
		}

		// Skip deleted entries from index
		if indexEntry.IsDeleted() {
			indexCurrent = indexCurrent.Next()
			continue
		}

		cmp := strings.Compare(indexEntry.RelativePath(), diskEntry.RelativePath())

		if cmp == 0 {
			// Same file - check if deleted or modified
			// Create string copy to avoid use-after-free when scan memory is unmapped
			pathCopy := string([]byte(indexEntry.RelativePath()))

			// Check if the disk/cache entry is marked as deleted
			if diskEntry.IsDeleted() {
				callback(StatusDeleted, pathCopy, indexEntry, diskEntry)
			} else if dc.isFileModified(indexEntry, diskEntry) {
				callback(StatusModified, pathCopy, indexEntry, diskEntry)
			} else {
				callback(StatusUnchanged, pathCopy, indexEntry, diskEntry)
			}
			indexCurrent = indexCurrent.Next()
			diskCurrent = diskCurrent.Next()
		} else if cmp < 0 {
			// File exists in index but not on disk - deleted
			// Create string copy to avoid use-after-free when scan memory is unmapped
			pathCopy := string([]byte(indexEntry.RelativePath()))
			callback(StatusDeleted, pathCopy, indexEntry, nil)
			indexCurrent = indexCurrent.Next()
		} else {
			// File exists on disk but not in index - added
			// Create string copy to avoid use-after-free when scan memory is unmapped
			pathCopy := string([]byte(diskEntry.RelativePath()))
			callback(StatusAdded, pathCopy, nil, diskEntry)
			diskCurrent = diskCurrent.Next()
		}
	}

	// Handle remaining entries from index (all deleted)
	for indexCurrent != nil {
		indexRef := indexCurrent.Item()
		indexEntry := indexRef.GetBinaryEntry()
		if indexEntry != nil && !indexEntry.IsDeleted() {
			// Create string copy to avoid use-after-free when scan memory is unmapped
			pathCopy := string([]byte(indexEntry.RelativePath()))
			callback(StatusDeleted, pathCopy, indexEntry, nil)
		}
		indexCurrent = indexCurrent.Next()
	}

	// Handle remaining entries from disk (all added)
	if IsDebugEnabled("scan") {
		VerboseLog(3, "hwangLinStatus: processing remaining disk entries, diskCurrent=%v", diskCurrent != nil)
	}
	for diskCurrent != nil {
		diskRef := diskCurrent.Item()
		diskEntry := diskRef.GetBinaryEntry()
		if IsDebugEnabled("scan") {
			if diskEntry != nil {
				// Don't access RelativePath() in debug log - it might be freed memory
				VerboseLog(3, "hwangLinStatus: processing disk entry")
			} else {
				VerboseLog(3, "hwangLinStatus: diskEntry is nil")
			}
		}
		if diskEntry != nil {
			// Create string copy to avoid use-after-free when scan memory is unmapped
			pathCopy := string([]byte(diskEntry.RelativePath()))
			callback(StatusAdded, pathCopy, nil, diskEntry)
		} else {
			// This should never happen - indicates a serious bug
			fmt.Fprintf(os.Stderr, "[ERROR] GetBinaryEntry returned nil for remaining disk entry - this should never happen\n")
		}
		diskCurrent = diskCurrent.Next()
	}
}

// isFileModified checks if a file has been modified using fast metadata comparison
func (dc *DirectoryCache) isFileModified(indexEntry, diskEntry *binaryEntry) bool {
	// Quick size check
	if indexEntry.FileSize != diskEntry.FileSize {
		return true
	}

	// Check ownership
	if indexEntry.UID != diskEntry.UID || indexEntry.GID != diskEntry.GID {
		return true
	}

	// Check timestamps using wall time
	indexCTime := timeFromWall(indexEntry.CTimeWall)
	diskCTime := timeFromWall(diskEntry.CTimeWall)
	if indexCTime.Unix() != diskCTime.Unix() || indexCTime.Nanosecond() != diskCTime.Nanosecond() {
		return true
	}

	indexMTime := timeFromWall(indexEntry.MTimeWall)
	diskMTime := timeFromWall(diskEntry.MTimeWall)
	if indexMTime.Unix() != diskMTime.Unix() || indexMTime.Nanosecond() != diskMTime.Nanosecond() {
		return true
	}

	return false
}

// HasChanges returns true if there are any changes
func (sr *StatusResult) HasChanges() bool {
	return len(sr.Modified) > 0 || len(sr.Added) > 0 || len(sr.Deleted) > 0
}

// TotalChanges returns the total number of changed files
func (sr *StatusResult) TotalChanges() int {
	return len(sr.Modified) + len(sr.Added) + len(sr.Deleted)
}
