package dircachefilehash

// FindDuplicates returns groups of files with identical hashes (zero-copy)
func (dc *DirectoryCache) FindDuplicates() map[string][]*binaryEntry {
	duplicates := make(map[string][]*binaryEntry)

	for _, entry := range dc.entries {
		hashStr := entry.HashString()
		duplicates[hashStr] = append(duplicates[hashStr], entry)
	}

	// Remove entries with only one file
	for hash, entries := range duplicates {
		if len(entries) <= 1 {
			delete(duplicates, hash)
		}
	}

	return duplicates
}

// FindByHash finds entries with the specified hash (zero-copy)
func (dc *DirectoryCache) FindByHash(hash string) []*binaryEntry {
	var matches []*binaryEntry

	for _, entry := range dc.entries {
		if entry.HashString() == hash {
			matches = append(matches, entry)
		}
	}

	return matches
}
