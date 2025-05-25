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

// Status compares the current directory state with the loaded index using Hwang-Lin merge algorithm
func (dc *DirectoryCache) Status() (*StatusResult, error) {
	// Load existing index if not already loaded
	if len(dc.entries) == 0 {
		if err := dc.LoadIndex(); err != nil {
			return nil, fmt.Errorf("failed to load index: %w", err)
		}
	}

	// Scan current state
	currentCache := NewDirectoryCache(dc.RootDir, "")
	if err := currentCache.ScanDirectory(); err != nil {
		return nil, fmt.Errorf("failed to scan directory: %w", err)
	}

	// Get sorted entries (both are already sorted by RelativePath)
	indexEntries := dc.GetEntries()
	diskEntries := currentCache.GetEntries()

	result := &StatusResult{
		Modified: make([]string, 0),
		Added:    make([]string, 0),
		Deleted:  make([]string, 0),
	}

	// Hwang-Lin merge algorithm to compare two sorted lists
	i, j := 0, 0

	for i < len(indexEntries) && j < len(diskEntries) {
		indexEntry := indexEntries[i]
		diskEntry := diskEntries[j]

		// Compare filenames (both lists are sorted by RelativePath)
		cmp := strings.Compare(indexEntry.RelativePath, diskEntry.RelativePath)

		if cmp == 0 {
			// Same filename - check if file is modified using fast comparison
			if dc.isFileModified(&indexEntry, &diskEntry) {
				result.Modified = append(result.Modified, indexEntry.RelativePath)
			}
			// Advance both pointers
			i++
			j++
		} else if cmp < 0 {
			// File exists in index but not on disk (deleted)
			result.Deleted = append(result.Deleted, indexEntry.RelativePath)
			i++
		} else {
			// File exists on disk but not in index (added)
			result.Added = append(result.Added, diskEntry.RelativePath)
			j++
		}
	}

	// Handle remaining entries in index (all deleted)
	for i < len(indexEntries) {
		result.Deleted = append(result.Deleted, indexEntries[i].RelativePath)
		i++
	}

	// Handle remaining entries on disk (all added)
	for j < len(diskEntries) {
		result.Added = append(result.Added, diskEntries[j].RelativePath)
		j++
	}

	return result, nil
}

// isFileModified checks if a file has been modified using fast metadata comparison
func (dc *DirectoryCache) isFileModified(indexEntry, diskEntry *FileEntry) bool {
	// Quick checks first - if these differ, file is definitely modified
	if indexEntry.Size != diskEntry.Size {
		return true
	}

	// Check UID and GID
	if indexEntry.UID != diskEntry.UID || indexEntry.GID != diskEntry.GID {
		return true
	}

	// Check change time (ctime) - metadata modification time
	if indexEntry.CTime.Unix() != diskEntry.CTime.Unix() ||
		indexEntry.CTimeNano != diskEntry.CTimeNano {
		return true
	}

	// Check modification time (mtime) - content modification time
	if indexEntry.MTime.Unix() != diskEntry.MTime.Unix() ||
		indexEntry.MTimeNano != diskEntry.MTimeNano {
		return true
	}

	// If size, ownership, and both timestamps are identical, assume content is unchanged
	// This avoids expensive hash computation for most files
	return false
}

// HasChanges returns true if there are any changes (modified, added, or deleted files)
func (sr *StatusResult) HasChanges() bool {
	return len(sr.Modified) > 0 || len(sr.Added) > 0 || len(sr.Deleted) > 0
}

// TotalChanges returns the total number of changed files
func (sr *StatusResult) TotalChanges() int {
	return len(sr.Modified) + len(sr.Added) + len(sr.Deleted)
}
