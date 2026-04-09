package dircachefilehash

// BinaryEntrySkiplistIterator iterates through an existing skiplist using BinaryEntryInterface
// This is the unified version of SkiplistIterator that returns BinaryEntryInterface entries
type BinaryEntrySkiplistIterator struct {
	iteratorBase
	skiplist     *skiplistWrapper
	shutdownChan <-chan struct{}
}

// NewBinaryEntrySkiplistIterator creates a new unified iterator for the given skiplist
func NewBinaryEntrySkiplistIterator(sl *skiplistWrapper, name string, shutdownChan <-chan struct{}) *BinaryEntrySkiplistIterator {
	if sl == nil || sl.Length() == 0 {
		return &BinaryEntrySkiplistIterator{
			iteratorBase: iteratorBase{
				name:      name,
				exhausted: true, // Empty/nil skiplist is immediately exhausted
			},
			shutdownChan: shutdownChan,
		}
	}

	return &BinaryEntrySkiplistIterator{
		iteratorBase: iteratorBase{name: name},
		skiplist:     sl,
		shutdownChan: shutdownChan,
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
	iterationCount := 0
	err := bsi.skiplist.ForEachRef(func(entryRef binaryEntryRef, context string) bool {
		iterationCount++
		if IsDebugEnabled("load") {
			VerboseLog(3, "[ITERATOR-DEBUG] ForEachRef iteration %d: currentPath='%s'", iterationCount, bsi.currentPath)
		}

		// Get the path for comparison
		entry := entryRef.GetBinaryEntry()
		if entry == nil {
			if IsDebugEnabled("load") {
				VerboseLog(3, "[ITERATOR-DEBUG] Skipping invalid entry (nil)")
			}
			return true // Skip invalid entries
		}
		entryPath := entry.RelativePath()

		// Check if entry is deleted
		isDeleted := entry.IsDeleted()
		if IsDebugEnabled("load") {
			VerboseLog(3, "[ITERATOR-DEBUG] Entry path='%s', deleted=%v, context='%s'", entryPath, isDeleted, context)
		}

		// If we haven't started iterating yet (currentPath is empty), take the first entry
		if bsi.currentPath == "" {
			if IsDebugEnabled("load") {
				VerboseLog(3, "[ITERATOR-DEBUG] Taking first entry: path='%s', deleted=%v", entryPath, isDeleted)
			}
			foundEntry = NewBESkiplistEntry(entryRef, bsi.skiplist)
			bsi.updateCurrentPathFromInterface(foundEntry)
			if IsDebugEnabled("load") {
				VerboseLog(3, "[ITERATOR-DEBUG] Updated currentPath to: '%s'", bsi.currentPath)
			}
			return false // Stop iteration
		}

		// If this path is lexicographically after our current position, take it
		// (entryPath is already normalized, so this comparison will work correctly)
		pathComparison := entryPath > bsi.currentPath
		if IsDebugEnabled("load") {
			VerboseLog(3, "[ITERATOR-DEBUG] Path comparison: '%s' > '%s' = %v", entryPath, bsi.currentPath, pathComparison)
		}
		if pathComparison {
			if IsDebugEnabled("load") {
				VerboseLog(3, "[ITERATOR-DEBUG] Taking next entry: path='%s', deleted=%v", entryPath, isDeleted)
			}
			foundEntry = NewBESkiplistEntry(entryRef, bsi.skiplist)
			bsi.updateCurrentPathFromInterface(foundEntry)
			if IsDebugEnabled("load") {
				VerboseLog(3, "[ITERATOR-DEBUG] Updated currentPath to: '%s'", bsi.currentPath)
			}
			return false // Stop iteration
		}

		if IsDebugEnabled("load") {
			VerboseLog(3, "[ITERATOR-DEBUG] Continuing search: '%s' <= '%s'", entryPath, bsi.currentPath)
		}
		return true // Continue looking
	}, bsi.shutdownChan)

	if IsDebugEnabled("load") {
		VerboseLog(3, "[ITERATOR-DEBUG] ForEachRef completed after %d iterations, foundEntry=%v, err=%v", iterationCount, foundEntry != nil, err)
	}

	if err != nil {
		// Signal handling interruption
		bsi.markExhausted()
		return nil, err
	}

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
