package dircachefilehash

import (
	"fmt"
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

// StatusResult represents the result of a status check
type StatusResult struct {
	Modified []string
	Added    []string
	Deleted  []string
}

// Status compares the current directory state with the loaded index using Hwang-Lin algorithm on skiplists
func (dc *DirectoryCache) Status() (*StatusResult, error) {
	if dc.skiplist.IsEmpty() {
		if err := dc.LoadIndex(); err != nil {
			return nil, fmt.Errorf("failed to load index: %w", err)
		}
	}

	// Scan current state into a new cache
	currentCache := NewDirectoryCache(dc.RootDir, "")
	if err := currentCache.ScanDirectory(); err != nil {
		return nil, fmt.Errorf("failed to scan directory: %w", err)
	}
	defer currentCache.Close()

	result := &StatusResult{
		Modified: make([]string, 0),
		Added:    make([]string, 0),
		Deleted:  make([]string, 0),
	}

	// Hwang-Lin merge algorithm using skiplist iterators
	dc.hwangLinStatus(dc.skiplist, currentCache.skiplist, func(status FileStatus, path string, indexEntry, diskEntry *binaryEntry) {
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

// StatusWithCallback compares directory state using Hwang-Lin algorithm with callback for zero-copy operation
func (dc *DirectoryCache) StatusWithCallback(callback func(status FileStatus, path string, indexEntry, diskEntry *binaryEntry)) error {
	if dc.skiplist.IsEmpty() {
		if err := dc.LoadIndex(); err != nil {
			return fmt.Errorf("failed to load index: %w", err)
		}
	}

	currentCache := NewDirectoryCache(dc.RootDir, "")
	if err := currentCache.ScanDirectory(); err != nil {
		return fmt.Errorf("failed to scan directory: %w", err)
	}
	defer currentCache.Close()

	// Use Hwang-Lin algorithm directly
	dc.hwangLinStatus(dc.skiplist, currentCache.skiplist, callback)
	return nil
}

// hwangLinStatus implements the Hwang-Lin merge algorithm for comparing two sorted skiplists
func (dc *DirectoryCache) hwangLinStatus(indexSkiplist, diskSkiplist *SkiplistWrapper,
	callback func(status FileStatus, path string, indexEntry, diskEntry *binaryEntry)) {

	// Get sorted entries from both skiplists (zero-copy)
	indexEntries := indexSkiplist.GetSortedEntries()
	diskEntries := diskSkiplist.GetSortedEntries()

	i, j := 0, 0

	// Hwang-Lin merge
	for i < len(indexEntries) && j < len(diskEntries) {
		indexEntry := indexEntries[i]
		diskEntry := diskEntries[j]

		cmp := strings.Compare(indexEntry.RelativePath(), diskEntry.RelativePath())

		if cmp == 0 {
			// Same file - check if modified
			if dc.isFileModified(indexEntry, diskEntry) {
				callback(StatusModified, indexEntry.RelativePath(), indexEntry, diskEntry)
			} else {
				callback(StatusUnchanged, indexEntry.RelativePath(), indexEntry, diskEntry)
			}
			i++
			j++
		} else if cmp < 0 {
			// File exists in index but not on disk - deleted
			callback(StatusDeleted, indexEntry.RelativePath(), indexEntry, nil)
			i++
		} else {
			// File exists on disk but not in index - added
			callback(StatusAdded, diskEntry.RelativePath(), nil, diskEntry)
			j++
		}
	}

	// Handle remaining entries from index (all deleted)
	for i < len(indexEntries) {
		callback(StatusDeleted, indexEntries[i].RelativePath(), indexEntries[i], nil)
		i++
	}

	// Handle remaining entries from disk (all added)
	for j < len(diskEntries) {
		callback(StatusAdded, diskEntries[j].RelativePath(), nil, diskEntries[j])
		j++
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

// GetModifiedFiles returns only the paths of modified files using Hwang-Lin algorithm
func (dc *DirectoryCache) GetModifiedFiles() ([]string, error) {
	var modified []string

	err := dc.StatusWithCallback(func(status FileStatus, path string, indexEntry, diskEntry *binaryEntry) {
		if status == StatusModified {
			modified = append(modified, path)
		}
	})

	return modified, err
}

// GetAddedFiles returns only the paths of added files using Hwang-Lin algorithm
func (dc *DirectoryCache) GetAddedFiles() ([]string, error) {
	var added []string

	err := dc.StatusWithCallback(func(status FileStatus, path string, indexEntry, diskEntry *binaryEntry) {
		if status == StatusAdded {
			added = append(added, path)
		}
	})

	return added, err
}

// GetDeletedFiles returns only the paths of deleted files using Hwang-Lin algorithm
func (dc *DirectoryCache) GetDeletedFiles() ([]string, error) {
	var deleted []string

	err := dc.StatusWithCallback(func(status FileStatus, path string, indexEntry, diskEntry *binaryEntry) {
		if status == StatusDeleted {
			deleted = append(deleted, path)
		}
	})

	return deleted, err
}

// HasChanges returns true if there are any changes
func (sr *StatusResult) HasChanges() bool {
	return len(sr.Modified) > 0 || len(sr.Added) > 0 || len(sr.Deleted) > 0
}

// TotalChanges returns the total number of changed files
func (sr *StatusResult) TotalChanges() int {
	return len(sr.Modified) + len(sr.Added) + len(sr.Deleted)
}

// HasChangesQuick performs a quick check for any changes using Hwang-Lin algorithm without collecting all results
func (dc *DirectoryCache) HasChangesQuick() (bool, error) {
	hasChanges := false

	err := dc.StatusWithCallback(func(status FileStatus, path string, indexEntry, diskEntry *binaryEntry) {
		if status != StatusUnchanged {
			hasChanges = true
		}
	})

	return hasChanges, err
}
