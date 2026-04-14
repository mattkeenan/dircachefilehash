package dircachefilehash

import (
	"fmt"

	zcsl "github.com/mattkeenan/zerocopyskiplist"
)

// BinaryEntrySkiplistIterator iterates through an existing skiplist using BinaryEntryInterface.
// Holds a cursor into the skiplist and advances it on each Next() call — O(1) per call.
type BinaryEntrySkiplistIterator struct {
	iteratorBase
	skiplist     *skiplistWrapper
	cursor       *zcsl.ItemPtr[binaryEntryRef, string, string]
	shutdownChan <-chan struct{}
	started      bool
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

	// Check for shutdown
	select {
	case <-bsi.shutdownChan:
		bsi.markExhausted()
		return nil, fmt.Errorf("iteration interrupted by shutdown signal")
	default:
	}

	// Advance cursor
	if !bsi.started {
		bsi.cursor = bsi.skiplist.skiplist.First()
		bsi.started = true
	} else {
		if bsi.cursor != nil {
			bsi.cursor = bsi.cursor.Next()
		}
	}

	if bsi.cursor == nil {
		bsi.markExhausted()
		return nil, nil
	}

	ref := bsi.cursor.Item()
	entry := NewBESkiplistEntry(*ref, bsi.skiplist)
	bsi.updateCurrentPathFromInterface(entry)
	return entry, nil
}

// Close releases any resources held by the iterator
func (bsi *BinaryEntrySkiplistIterator) Close() error {
	bsi.markClosed()
	return nil
}

// HasNext returns true if there are more entries available
func (bsi *BinaryEntrySkiplistIterator) HasNext() bool {
	return bsi.iteratorBase.HasNext()
}
