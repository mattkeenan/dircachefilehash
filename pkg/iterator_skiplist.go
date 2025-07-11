package dircachefilehash

// No imports needed for this implementation

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
// Uses an internal iterator pattern similar to hwangLinStatus
func (si *SkiplistIterator) Next() (*binaryEntry, error) {
	if err := si.checkClosed(); err != nil {
		return nil, err
	}
	
	if si.skiplist == nil {
		si.markExhausted()
		return nil, nil
	}
	
	// Get the first entry (or continue from where we left off)
	// For simplicity, we'll use the ForEach pattern with early termination
	var foundEntry *binaryEntry
	found := false
	
	si.skiplist.ForEach(func(entry *binaryEntry, context string) bool {
		entryPath := entry.RelativePath()
		
		
		// If this is the first call (currentPath is empty), take the first entry
		if si.currentPath == "" {
			foundEntry = entry
			found = true
			return false // Stop iteration
		}
		
		// Skip entries we've already seen (strictly less than)
		if entryPath <= si.currentPath {
			return true // Continue looking
		}
		
		// This is the next entry (first one > currentPath)
		foundEntry = entry
		found = true
		return false // Stop iteration
	})
	
	if !found {
		si.markExhausted()
		return nil, nil
	}
	
	// Update current path
	si.updateCurrentPath(foundEntry)
	
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