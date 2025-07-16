package dircachefilehash

// No additional imports needed - using existing BESkiplistEntry

// SkiplistIterator iterates through an existing skiplist
// This is the simplest iterator implementation - it just walks through
// a skiplist that's already loaded in memory
type SkiplistIterator struct {
	iteratorBase
	skiplist *skiplistWrapper
}

// NewSkiplistIterator creates a new iterator for the given skiplist
func NewSkiplistIterator(sl *skiplistWrapper, name string) *SkiplistIterator {
	if sl == nil {
		return &SkiplistIterator{
			iteratorBase: iteratorBase{
				name:      name,
				exhausted: true, // Empty/nil skiplist is immediately exhausted
			},
		}
	}
	
	return &SkiplistIterator{
		iteratorBase: iteratorBase{name: name},
		skiplist:     sl,
	}
}

// Next returns the next entry from the skiplist
// Uses ForEachRef to get binaryEntryRef and create proper BESkiplistEntry
func (si *SkiplistIterator) Next() (BinaryEntryInterface, error) {
	if err := si.checkClosed(); err != nil {
		return nil, err
	}
	
	if si.skiplist == nil {
		si.markExhausted()
		return nil, nil
	}
	
	var foundEntry BinaryEntryInterface = nil
	
	// Use ForEachRef to find the next entry after our current position
	si.skiplist.ForEachRef(func(entryRef binaryEntryRef, context string) bool {
		// Get the path for comparison
		entry := entryRef.GetBinaryEntry()
		if entry == nil {
			return true // Skip invalid entries
		}
		entryPath := entry.RelativePath()
		
		// If we haven't started iterating yet (currentPath is empty), take the first entry
		if si.currentPath == "" {
			foundEntry = NewBESkiplistEntry(entryRef, si.skiplist)
			si.updateCurrentPathFromInterface(foundEntry)
			return false // Stop iteration
		}
		
		// If this path is lexicographically after our current position, take it
		if entryPath > si.currentPath {
			foundEntry = NewBESkiplistEntry(entryRef, si.skiplist)
			si.updateCurrentPathFromInterface(foundEntry)
			return false // Stop iteration
		}
		
		return true // Continue looking
	})
	
	if foundEntry == nil {
		// No more entries found
		si.markExhausted()
		return nil, nil
	}
	
	return foundEntry, nil
}

// Close releases any resources held by the iterator
// For SkiplistIterator, this just marks it as closed since we don't own the skiplist
func (si *SkiplistIterator) Close() error {
	si.markClosed()
	si.skiplist = nil
	return nil
}


// Length returns the number of entries in the skiplist (for convenience/debugging)
func (si *SkiplistIterator) Length() int {
	if si.skiplist == nil {
		return 0
	}
	return si.skiplist.Length()
}