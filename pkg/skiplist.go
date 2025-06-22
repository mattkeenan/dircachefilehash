package dircachefilehash

import (
	"strings"
	"syscall"

	zcsl "github.com/mattkeenan/zerocopyskiplist"
)

// SkiplistWrapper wraps the new generic zerocopyskiplist with context support
type SkiplistWrapper struct {
	skiplist *zcsl.ZeroCopySkiplist[binaryEntry, string, string]
}

// NewSkiplistWrapper creates a new skiplist wrapper with context tracking
func NewSkiplistWrapper(maxLevels int, defaultContext string) *SkiplistWrapper {
	if maxLevels < 8 {
		maxLevels = 16 // reasonable default
	}

	// Key extractor function - extracts RelativePath as the key
	getKeyFromItem := func(entry *binaryEntry) string {
		return entry.RelativePath()
	}

	// Size function for serialization
	getItemSize := func(entry *binaryEntry) int {
		return int(entry.Size)
	}

	// String comparator function
	cmpKey := func(a, b string) int {
		return strings.Compare(a, b)
	}

	skiplist := zcsl.MakeZeroCopySkiplist[binaryEntry, string, string](
		maxLevels,
		getKeyFromItem,
		getItemSize,
		cmpKey,
	)

	return &SkiplistWrapper{
		skiplist: skiplist,
	}
}

// Insert adds a binaryEntry pointer with specific context
func (sw *SkiplistWrapper) Insert(entry *binaryEntry, context string) bool {
	return sw.skiplist.Insert(entry, context)
}

// Find searches for an entry by its relative path and returns entry with context
func (sw *SkiplistWrapper) Find(relativePath string) (*binaryEntry, string) {
	itemPtr, context := sw.skiplist.Find(relativePath)
	if itemPtr != nil {
		return itemPtr.Item(), context
	}
	return nil, ""
}

// Delete removes an entry by its relative path
func (sw *SkiplistWrapper) Delete(relativePath string) bool {
	return sw.skiplist.Delete(relativePath)
}

// ForEach iterates through all entries in sorted order with a callback (zero-copy)
func (sw *SkiplistWrapper) ForEach(callback func(*binaryEntry, string) bool) {
	for current := sw.skiplist.First(); current != nil; current = current.Next() {
		context := current.Context()
		if !callback(current.Item(), context) {
			break
		}
	}
}

// ForEachContext iterates through entries matching a specific context
func (sw *SkiplistWrapper) ForEachContext(context string, callback func(*binaryEntry) bool) {
	sw.ForEach(func(entry *binaryEntry, entryContext string) bool {
		if entryContext == context {
			return callback(entry)
		}
		return true // Continue iteration
	})
}

// Merge merges another skiplist into this skiplist
func (sw *SkiplistWrapper) Merge(other *SkiplistWrapper, strategy zcsl.MergeStrategy) error {
	if other == nil {
		return nil
	}

	return sw.skiplist.Merge(other.skiplist, strategy)
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
	newWrapper := &SkiplistWrapper{
		skiplist: sw.skiplist.Copy(),
	}
	return newWrapper
}

// First returns the first entry in the skiplist
func (sw *SkiplistWrapper) First() *binaryEntry {
	first := sw.skiplist.First()
	if first != nil {
		return first.Item()
	}
	return nil
}

// Last returns the last entry in the skiplist
func (sw *SkiplistWrapper) Last() *binaryEntry {
	last := sw.skiplist.Last()
	if last != nil {
		return last.Item()
	}
	return nil
}

// ToIovecSlice generates Iovec slices for all items
func (sw *SkiplistWrapper) ToIovecSlice() []syscall.Iovec {
	return sw.skiplist.ToIovecSlice("")
}

// ToContextIovecSlice generates Iovec slices for items matching the context
func (sw *SkiplistWrapper) ToContextIovecSlice(context string) []syscall.Iovec {
	return sw.skiplist.ToContextIovecSlice(context)
}

// ToNotContextIovecSlice generates Iovec slices for items not matching the context
func (sw *SkiplistWrapper) ToNotContextIovecSlice(context string) []syscall.Iovec {
	return sw.skiplist.ToNotContextIovecSlice(context)
}

// CallbackToIovecSlice generates Iovec slices for items that match the callback filter
func (sw *SkiplistWrapper) CallbackToIovecSlice(callback func(*binaryEntry, string) bool) []syscall.Iovec {
	return sw.skiplist.CallbackToIovecSlice(func(item *zcsl.ItemPtr[binaryEntry, string, string]) bool {
		context := item.Context()
		return callback(item.Item(), context)
	})
}

// Stats returns statistics about the skiplist entries
func (sw *SkiplistWrapper) Stats() (total, deleted, active int) {
	sw.ForEach(func(entry *binaryEntry, context string) bool {
		total++
		if entry.IsDeleted() {
			deleted++
		} else {
			active++
		}
		return true
	})
	return total, deleted, active
}

// UpdateContext updates the context for an existing entry
func (sw *SkiplistWrapper) UpdateContext(relativePath string, newContext string) bool {
	return sw.skiplist.UpdateContext(relativePath, newContext)
}

// FilterNotByContext returns a new skiplist with entries not matching the given context
func (sw *SkiplistWrapper) FilterNotByContext(context string) *SkiplistWrapper {
	result := NewSkiplistWrapper(16, "")
	sw.ForEach(func(entry *binaryEntry, entryContext string) bool {
		if entryContext != context {
			result.Insert(entry, entryContext)
		}
		return true
	})
	return result
}
