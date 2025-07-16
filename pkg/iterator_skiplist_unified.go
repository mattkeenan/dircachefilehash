package dircachefilehash

// BinaryEntrySkiplistIterator iterates through an existing skiplist using BinaryEntryInterface
// This is the unified version of SkiplistIterator that returns BinaryEntryInterface entries
type BinaryEntrySkiplistIterator struct {
	iteratorBase
	skiplist *skiplistWrapper
}

// NewBinaryEntrySkiplistIterator creates a new unified iterator for the given skiplist
func NewBinaryEntrySkiplistIterator(sl *skiplistWrapper, name string) *BinaryEntrySkiplistIterator {
	if sl == nil || sl.Length() == 0 {
		return &BinaryEntrySkiplistIterator{
			iteratorBase: iteratorBase{
				name:      name,
				exhausted: true, // Empty/nil skiplist is immediately exhausted
			},
		}
	}
	
	return &BinaryEntrySkiplistIterator{
		iteratorBase: iteratorBase{name: name},
		skiplist:     sl,
	}
}

// Next returns the next entry from the skiplist as BinaryEntryInterface
func (bsi *BinaryEntrySkiplistIterator) Next() (BinaryEntryInterface, error) {
	if err := bsi.checkClosed(); err != nil {
		return nil, err
	}
	
	if bsi.skiplist == nil {
		bsi.markExhausted()
		return nil, nil
	}
	
	var foundEntry BinaryEntryInterface = nil
	
	// Use ForEachRef to find the next entry after our current position
	bsi.skiplist.ForEachRef(func(entryRef binaryEntryRef, context string) bool {
		// Get the path for comparison
		entry := entryRef.GetBinaryEntry()
		if entry == nil {
			return true // Skip invalid entries
		}
		entryPath := entry.RelativePath()
		
		// If we haven't started iterating yet (currentPath is empty), take the first entry
		if bsi.currentPath == "" {
			foundEntry = NewBESkiplistEntry(entryRef, bsi.skiplist)
			bsi.updateCurrentPathFromInterface(foundEntry)
			return false // Stop iteration
		}
		
		// If this path is lexicographically after our current position, take it
		if entryPath > bsi.currentPath {
			foundEntry = NewBESkiplistEntry(entryRef, bsi.skiplist)
			bsi.updateCurrentPathFromInterface(foundEntry)
			return false // Stop iteration
		}
		
		return true // Continue looking
	})
	
	if foundEntry == nil {
		// No more entries found
		bsi.markExhausted()
		return nil, nil
	}
	
	return foundEntry, nil
}

// Close releases any resources held by the iterator
func (bsi *BinaryEntrySkiplistIterator) Close() error {
	bsi.markClosed()
	// Skiplist iterators don't need special cleanup since they reference existing data
	return nil
}

// HasNext returns true if there are more entries available
func (bsi *BinaryEntrySkiplistIterator) HasNext() bool {
	return bsi.iteratorBase.HasNext()
}