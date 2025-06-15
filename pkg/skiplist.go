package dircachefilehash

import (
	"strings"

	zcsl "github.com/mattkeenan/zerocopyskiplist"
)

// SkiplistWrapper wraps zerocopyskiplist for zero-copy access to mmap'd binaryEntry pointers
type SkiplistWrapper struct {
	skiplist *zcsl.ZeroCopySkiplist[binaryEntry, string]
}

// NewSkiplistWrapper creates a new skiplist wrapper with the specified max levels
func NewSkiplistWrapper(maxLevels int) *SkiplistWrapper {
	if maxLevels < 8 {
		maxLevels = 16 // reasonable default
	}

	// Key extractor function - extracts RelativePath as the key
	getKeyFromItem := func(entry *binaryEntry) string {
		return entry.RelativePath()
	}

	// Size function for serialization
	getItemSize := func(entry *binaryEntry) int {
		return entry.EntrySize()
	}

	// String comparator function
	cmpKey := func(a, b string) int {
		return strings.Compare(a, b)
	}

	skiplist := zcsl.MakeZeroCopySkiplist(
		maxLevels,
		getKeyFromItem,
		getItemSize,
		cmpKey,
	)

	return &SkiplistWrapper{
		skiplist: skiplist,
	}
}

// Insert adds a binaryEntry pointer to the skiplist (zero-copy)
func (sw *SkiplistWrapper) Insert(entry *binaryEntry) {
	sw.skiplist.Insert(entry)
}

// GetSortedEntries returns pointers to all entries in sorted order (zero-copy)
func (sw *SkiplistWrapper) GetSortedEntries() []*binaryEntry {
	var entries []*binaryEntry
	for current := sw.skiplist.First(); current != nil; current = current.Next() {
		entries = append(entries, current.Item())
	}
	return entries
}

// ForEach iterates through all entries in sorted order with a callback (zero-copy)
func (sw *SkiplistWrapper) ForEach(callback func(*binaryEntry) bool) {
	for current := sw.skiplist.First(); current != nil; current = current.Next() {
		if !callback(current.Item()) {
			break
		}
	}
}

// Merge merges another skiplist into this skiplist
func (sw *SkiplistWrapper) Merge(other *SkiplistWrapper) {
	if other == nil {
		return
	}

	err := sw.skiplist.Merge(other.skiplist, zcsl.MergeTheirs)
	if err != nil {
		// Fallback to manual merge if the built-in merge fails
		sw.manualMerge(other)
	}
}

// manualMerge performs manual merge as fallback
func (sw *SkiplistWrapper) manualMerge(other *SkiplistWrapper) {
	otherEntries := other.GetSortedEntries()
	for _, entry := range otherEntries {
		sw.skiplist.Insert(entry)
	}
}

// Delete removes entries from this skiplist that exist in the other skiplist
func (sw *SkiplistWrapper) Delete(other *SkiplistWrapper) {
	if other == nil {
		return
	}

	entriesToDelete := other.GetSortedEntries()
	for _, entry := range entriesToDelete {
		sw.skiplist.Delete(entry.RelativePath())
	}
}

// Find searches for an entry by its relative path
func (sw *SkiplistWrapper) Find(relativePath string) *binaryEntry {
	if found := sw.skiplist.Find(relativePath); found != nil {
		return found.Item()
	}
	return nil
}

// Length returns the number of entries in the skiplist
func (sw *SkiplistWrapper) Length() int {
	return sw.skiplist.Length()
}

// IsEmpty returns true if the skiplist has no entries
func (sw *SkiplistWrapper) IsEmpty() bool {
	return sw.skiplist.IsEmpty()
}

// Copy creates a copy of the skiplist structure
func (sw *SkiplistWrapper) Copy() *SkiplistWrapper {
	return &SkiplistWrapper{
		skiplist: sw.skiplist.Copy(),
	}
}
