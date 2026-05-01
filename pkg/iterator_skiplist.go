package dircachefilehash

import (
	"context"
	"fmt"

	zcsl "github.com/mattkeenan/zerocopyskiplist"
)

// BinaryEntrySkiplistIterator iterates through an existing skiplist using BinaryEntryInterface.
// Holds a cursor into the skiplist and advances it on each Next() call — O(1) per call.
type BinaryEntrySkiplistIterator struct {
	iteratorBase
	skiplist *skiplistWrapper
	cursor   *zcsl.ItemPtr[binaryEntryRef, string, string]
	ctx      context.Context
	started  bool
}

// NewBinaryEntrySkiplistIterator creates a new iterator for the given skiplist.
func NewBinaryEntrySkiplistIterator(ctx context.Context, sl *skiplistWrapper, name string) *BinaryEntrySkiplistIterator {
	if sl == nil || sl.Length() == 0 {
		return &BinaryEntrySkiplistIterator{
			iteratorBase: iteratorBase{
				name:      name,
				exhausted: true, // Empty/nil skiplist is immediately exhausted
			},
			ctx: ctx,
		}
	}

	return &BinaryEntrySkiplistIterator{
		iteratorBase: iteratorBase{name: name},
		skiplist:     sl,
		ctx:          ctx,
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
	case <-bsi.ctx.Done():
		bsi.markExhausted()
		return nil, fmt.Errorf("iteration interrupted: %w", bsi.ctx.Err())
	default:
	}

	// Advance cursor
	if !bsi.started {
		bsi.cursor = bsi.skiplist.skiplist.First()
		bsi.started = true
	} else if bsi.cursor != nil {
		bsi.cursor = bsi.cursor.Next()
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
