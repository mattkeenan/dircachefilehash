package dircachefilehash

import (
	"fmt"
	"strconv"
	"strings"

	zcsl "github.com/mattkeenan/zerocopyskiplist"
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
	MainIndex     bool     `json:"main_index"`
	CacheIndex    bool     `json:"cache_index"`
	TempIndices   []string `json:"temp_indices,omitempty"`   // List of temporary index files found
	HasTempFiles  bool     `json:"has_temp_files"`           // True if any temp files exist
}

// StatusResult represents the result of a status check
type StatusResult struct {
	Modified    []string     `json:"modified"`
	Added       []string     `json:"added"`
	Deleted     []string     `json:"deleted"`
	CleanStatus *CleanStatus `json:"clean_status,omitempty"` // Only included when verbose
}

// Status compares the current directory state with the loaded index using the new workflow
func (dc *DirectoryCache) Status(flags map[string]string) (*StatusResult, error) {
	// Use the new cache update workflow which implements steps 1-11 as specified
	if err := dc.UpdateCacheIndexWithWorkflow(); err != nil {
		return nil, fmt.Errorf("failed to update cache index: %w", err)
	}

	// Load both main and cache indices for comparison
	mainSkiplist, err := dc.LoadMainIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to load main index: %w", err)
	}

	cacheSkiplist, err := dc.LoadCacheIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to load cache index: %w", err)
	}

	// Create combined view: main index + cache index for complete indexed state
	indexedSkiplist := mainSkiplist.Copy()
	if err := indexedSkiplist.Merge(cacheSkiplist, zcsl.MergeTheirs); err != nil {
		return nil, fmt.Errorf("failed to merge cache with main index: %w", err)
	}

	// Scan current directory state for comparison
	currentSkiplist, err := dc.CreateTmpIndexFromScan(NewSkiplistWrapper(16, "empty"))
	if err != nil {
		return nil, fmt.Errorf("failed to scan directory: %w", err)
	}

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
			cacheSkiplist, err := dc.LoadCacheIndex()
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
	dc.hwangLinStatus(indexedSkiplist, currentSkiplist, func(status FileStatus, path string, indexEntry, diskEntry *binaryEntry) {
		switch status {
		case StatusModified:
			result.Modified = append(result.Modified, path)
		case StatusAdded:
			result.Added = append(result.Added, path)
		case StatusDeleted:
			result.Deleted = append(result.Deleted, path)
		}
	})

	return result, nil
}

// hwangLinStatus implements the Hwang-Lin merge algorithm using direct skiplist iteration (zero-copy)
func (dc *DirectoryCache) hwangLinStatus(indexSkiplist, diskSkiplist *SkiplistWrapper,
	callback func(status FileStatus, path string, indexEntry, diskEntry *binaryEntry)) {

	// Use direct iteration instead of creating slices
	indexCurrent := indexSkiplist.skiplist.First()
	diskCurrent := diskSkiplist.skiplist.First()

	for indexCurrent != nil && diskCurrent != nil {
		indexRef := indexCurrent.Item()
		diskRef := diskCurrent.Item()
		
		indexEntry := indexRef.GetBinaryEntry()
		diskEntry := diskRef.GetBinaryEntry()
		
		if indexEntry == nil {
			indexCurrent = indexCurrent.Next()
			continue
		}
		if diskEntry == nil {
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
			// Same file - check if modified
			if dc.isFileModified(indexEntry, diskEntry) {
				callback(StatusModified, indexEntry.RelativePath(), indexEntry, diskEntry)
			} else {
				callback(StatusUnchanged, indexEntry.RelativePath(), indexEntry, diskEntry)
			}
			indexCurrent = indexCurrent.Next()
			diskCurrent = diskCurrent.Next()
		} else if cmp < 0 {
			// File exists in index but not on disk - deleted
			callback(StatusDeleted, indexEntry.RelativePath(), indexEntry, nil)
			indexCurrent = indexCurrent.Next()
		} else {
			// File exists on disk but not in index - added
			callback(StatusAdded, diskEntry.RelativePath(), nil, diskEntry)
			diskCurrent = diskCurrent.Next()
		}
	}

	// Handle remaining entries from index (all deleted)
	for indexCurrent != nil {
		indexRef := indexCurrent.Item()
		indexEntry := indexRef.GetBinaryEntry()
		if indexEntry != nil && !indexEntry.IsDeleted() {
			callback(StatusDeleted, indexEntry.RelativePath(), indexEntry, nil)
		}
		indexCurrent = indexCurrent.Next()
	}

	// Handle remaining entries from disk (all added)
	for diskCurrent != nil {
		diskRef := diskCurrent.Item()
		diskEntry := diskRef.GetBinaryEntry()
		if diskEntry != nil {
			callback(StatusAdded, diskEntry.RelativePath(), nil, diskEntry)
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

