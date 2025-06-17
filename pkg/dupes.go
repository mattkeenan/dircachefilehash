package dircachefilehash

import "fmt"

// FindDuplicates returns groups of files with identical hashes using context-aware cache management (read-only)
func (dc *DirectoryCache) FindDuplicates() (map[string][]*binaryEntry, error) {
	// Load main index with context
	if dc.skiplist.IsEmpty() {
		if err := dc.LoadIndex(dc.IndexFile, "main"); err != nil {
			return nil, fmt.Errorf("failed to load index: %w", err)
		}
	}

	// Update cache index using context-aware logic (dupes operation - read-only for main index)
	if err := dc.UpdateCacheIndex(); err != nil {
		return nil, fmt.Errorf("failed to update cache index: %w", err)
	}

	// Load cache index with context
	cacheSkiplist, err := dc.LoadCacheIndex("cache")
	if err != nil {
		return nil, fmt.Errorf("failed to load cache index: %w", err)
	}

	// Create combined view: main index + cache for complete current state (all context-aware)
	workingSkiplist := dc.skiplist.Copy("main")
	if err := workingSkiplist.Merge(cacheSkiplist); err != nil {
		return nil, fmt.Errorf("failed to merge cache with main index: %w", err)
	}

	duplicates := make(map[string][]*binaryEntry)

	// Use skiplist iteration to collect duplicates (all entries with context tracking)
	workingSkiplist.ForEach(func(entry *binaryEntry) bool {
		// Skip deleted entries
		if entry.IsDeleted() {
			return true // Continue iteration
		}

		hashStr := entry.HashString()
		duplicates[hashStr] = append(duplicates[hashStr], entry)
		return true // Continue iteration
	})

	// Remove entries with only one file
	for hash, entries := range duplicates {
		if len(entries) <= 1 {
			delete(duplicates, hash)
		}
	}

	return duplicates, nil
}

// FindByHash finds entries with the specified hash using context-aware cache management (read-only)
func (dc *DirectoryCache) FindByHash(hash string) ([]*binaryEntry, error) {
	// Load main index with context
	if dc.skiplist.IsEmpty() {
		if err := dc.LoadIndex(dc.IndexFile, "main"); err != nil {
			return nil, fmt.Errorf("failed to load index: %w", err)
		}
	}

	// Update cache index using context-aware logic (dupes operation - read-only for main index)
	if err := dc.UpdateCacheIndex(); err != nil {
		return nil, fmt.Errorf("failed to update cache index: %w", err)
	}

	// Load cache index with context
	cacheSkiplist, err := dc.LoadCacheIndex("cache")
	if err != nil {
		return nil, fmt.Errorf("failed to load cache index: %w", err)
	}

	// Create combined view: main index + cache for complete current state (all context-aware)
	workingSkiplist := dc.skiplist.Copy("main")
	if err := workingSkiplist.Merge(cacheSkiplist); err != nil {
		return nil, fmt.Errorf("failed to merge cache with main index: %w", err)
	}

	var matches []*binaryEntry

	// Use skiplist iteration to find matching hashes (all entries with context tracking)
	workingSkiplist.ForEach(func(entry *binaryEntry) bool {
		// Skip deleted entries
		if entry.IsDeleted() {
			return true // Continue iteration
		}

		if entry.HashString() == hash {
			matches = append(matches, entry)
		}
		return true // Continue iteration
	})

	return matches, nil
}
