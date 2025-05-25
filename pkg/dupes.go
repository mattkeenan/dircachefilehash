package dircachefilehash

// FindDuplicates returns groups of files with identical hashes
func (dc *DirectoryCache) FindDuplicates() map[string][]FileEntry {
	duplicates := make(map[string][]FileEntry)

	for _, entry := range dc.entries {
		if _, exists := duplicates[entry.Hash]; !exists {
			duplicates[entry.Hash] = make([]FileEntry, 0)
		}
		duplicates[entry.Hash] = append(duplicates[entry.Hash], entry)
	}

	// Remove entries with only one file
	for hash, entries := range duplicates {
		if len(entries) <= 1 {
			delete(duplicates, hash)
		}
	}

	return duplicates
}

// FindByHash finds entries with the specified hash
func (dc *DirectoryCache) FindByHash(hash string) []FileEntry {
	var matches []FileEntry

	// Since entries are now sorted by filename, we need to do a linear search
	for _, entry := range dc.entries {
		if entry.Hash == hash {
			matches = append(matches, entry)
		}
	}

	return matches
}
