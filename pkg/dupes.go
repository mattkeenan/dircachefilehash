package dircachefilehash

// FindDuplicates returns groups of files with identical hashes using skiplist iteration (zero-copy)
func (dc *DirectoryCache) FindDuplicates() map[string][]*binaryEntry {
	duplicates := make(map[string][]*binaryEntry)

	if dc.skiplist == nil {
		return duplicates
	}

	// Use skiplist iteration to collect duplicates
	dc.skiplist.ForEach(func(entry *binaryEntry) bool {
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

	return duplicates
}

// FindByHash finds entries with the specified hash using skiplist iteration (zero-copy)
func (dc *DirectoryCache) FindByHash(hash string) []*binaryEntry {
	var matches []*binaryEntry

	if dc.skiplist == nil {
		return matches
	}

	// Use skiplist iteration to find matching hashes
	dc.skiplist.ForEach(func(entry *binaryEntry) bool {
		if entry.HashString() == hash {
			matches = append(matches, entry)
		}
		return true // Continue iteration
	})

	return matches
}
