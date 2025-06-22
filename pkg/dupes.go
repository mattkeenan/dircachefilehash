package dircachefilehash

import (
	"fmt"

	zcsl "github.com/mattkeenan/zerocopyskiplist"
)

// DuplicateGroup represents a group of files with the same hash
type DuplicateGroup struct {
	Hash  string
	Files []string
}

// FindDuplicates returns groups of files with identical hashes using the new workflow
func (dc *DirectoryCache) FindDuplicates() ([]DuplicateGroup, error) {
	// Use the new cache update workflow to ensure we have current data
	if err := dc.UpdateCacheIndexWithWorkflow(); err != nil {
		return nil, fmt.Errorf("failed to update cache index: %w", err)
	}

	// Load both main and cache indices
	mainSkiplist, err := dc.LoadMainIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to load main index: %w", err)
	}

	cacheSkiplist, err := dc.LoadCacheIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to load cache index: %w", err)
	}

	// Create combined view: main index + cache for complete current state
	workingSkiplist := mainSkiplist.Copy()
	if err := workingSkiplist.Merge(cacheSkiplist, zcsl.MergeTheirs); err != nil {
		return nil, fmt.Errorf("failed to merge cache with main index: %w", err)
	}

	duplicates := make(map[string][]*binaryEntry)

	// Use skiplist iteration to collect duplicates
	workingSkiplist.ForEach(func(entry *binaryEntry, context string) bool {
		// Skip deleted entries
		if entry.IsDeleted() {
			return true // Continue iteration
		}

		hashStr := entry.HashString()
		duplicates[hashStr] = append(duplicates[hashStr], entry)
		return true // Continue iteration
	})

	// Convert to exported type and remove entries with only one file
	var result []DuplicateGroup
	for hash, entries := range duplicates {
		if len(entries) > 1 {
			var files []string
			for _, entry := range entries {
				files = append(files, entry.RelativePath())
			}
			result = append(result, DuplicateGroup{
				Hash:  hash,
				Files: files,
			})
		}
	}

	return result, nil
}
