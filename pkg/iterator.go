package dircachefilehash

import (
	"fmt"
)

// BinaryEntryIterator abstracts the source of file entries using BinaryEntryInterface.
// All implementations MUST return entries in sorted path order for the Hwang-Lin algorithm.
type BinaryEntryIterator interface {
	// Next returns the next entry in sorted order as BinaryEntryInterface.
	// Returns (nil, nil) when exhausted.
	// Returns (nil, error) on error.
	Next() (BinaryEntryInterface, error)

	// CurrentPath returns the path of the last entry returned by Next().
	// Returns empty string if Next() hasn't been called or iterator is exhausted.
	CurrentPath() string

	// HasNext returns true if there are more entries available.
	HasNext() bool

	// Name returns a descriptive name for this iterator.
	Name() string

	// Close releases any resources held by the iterator.
	Close() error
}

// iteratorBase provides common functionality for iterator implementations
type iteratorBase struct {
	name        string
	currentPath string
	exhausted   bool
	closed      bool
}

// Name returns the iterator name
func (ib *iteratorBase) Name() string {
	return ib.name
}

// CurrentPath returns the current path
func (ib *iteratorBase) CurrentPath() string {
	return ib.currentPath
}

// HasNext returns true if not exhausted and not closed
func (ib *iteratorBase) HasNext() bool {
	return !ib.exhausted && !ib.closed
}

// markExhausted marks the iterator as exhausted
func (ib *iteratorBase) markExhausted() {
	ib.exhausted = true
	ib.currentPath = ""
}

// markClosed marks the iterator as closed
func (ib *iteratorBase) markClosed() {
	ib.closed = true
	ib.exhausted = true
	ib.currentPath = ""
}

// updateCurrentPath updates the current path from a binaryEntry
func (ib *iteratorBase) updateCurrentPath(entry *binaryEntry) {
	if entry != nil {
		ib.currentPath = entry.RelativePath()
	} else {
		ib.currentPath = ""
	}
}

// updateCurrentPathFromInterface updates the current path from a BinaryEntryInterface
func (ib *iteratorBase) updateCurrentPathFromInterface(entry BinaryEntryInterface) {
	if entry != nil {
		if path, err := entry.RelativePath(); err == nil {
			if IsDebugEnabled("load") {
				VerboseLog(3, "[ITERATOR-DEBUG] updateCurrentPathFromInterface: success, path='%s'", path)
			}
			ib.currentPath = path
		} else {
			if IsDebugEnabled("load") {
				VerboseLog(3, "[ITERATOR-DEBUG] updateCurrentPathFromInterface: RelativePath() error: %v", err)
			}
			ib.currentPath = ""
		}
	} else {
		if IsDebugEnabled("load") {
			VerboseLog(3, "[ITERATOR-DEBUG] updateCurrentPathFromInterface: entry is nil")
		}
		ib.currentPath = ""
	}
}

// checkClosed returns an error if the iterator is closed
func (ib *iteratorBase) checkClosed() error {
	if ib.closed {
		return fmt.Errorf("iterator %s is closed", ib.name)
	}
	return nil
}
