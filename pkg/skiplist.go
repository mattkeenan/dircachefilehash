package dircachefilehash

import (
	"strings"
	"sync/atomic"
	"syscall"
	"unsafe"

	zcsl "github.com/mattkeenan/zerocopyskiplist"
)

// Instrumentation counters for string copy performance analysis
var (
	stringCopyCount    int64 // Total string copies performed
	stringAccessCount  int64 // Total string accesses attempted
)

// GetStringCopyStats returns instrumentation statistics
func GetStringCopyStats() (copies, accesses int64, copyRate float64) {
	c := atomic.LoadInt64(&stringCopyCount)
	a := atomic.LoadInt64(&stringAccessCount)
	rate := 0.0
	if a > 0 {
		rate = float64(c) / float64(a) * 100
	}
	return c, a, rate
}

// ResetStringCopyStats resets the instrumentation counters
func ResetStringCopyStats() {
	atomic.StoreInt64(&stringCopyCount, 0)
	atomic.StoreInt64(&stringAccessCount, 0)
}

// SkiplistWrapper wraps the new generic zerocopyskiplist with context support
type SkiplistWrapper struct {
	skiplist *zcsl.ZeroCopySkiplist[BinaryEntryRef, string, string]
}

// NewSkiplistWrapper creates a new skiplist wrapper with context tracking
func NewSkiplistWrapper(maxLevels int, defaultContext string) *SkiplistWrapper {
	if maxLevels < 8 {
		maxLevels = 16 // reasonable default
	}

	// Key extractor function - extracts RelativePath as the key
	// CRITICAL: Must copy string data out of mmap memory (PIC-style)
	getKeyFromItem := func(ref *BinaryEntryRef) string {
		atomic.AddInt64(&stringAccessCount, 1)
		entry := ref.GetBinaryEntry()
		if entry == nil {
			return ""
		}
		// Copy the string to avoid mremap invalidation (like PIC/GOT)
		path := entry.RelativePath()
		atomic.AddInt64(&stringCopyCount, 1)
		return string([]byte(path)) // Force copy to heap
	}

	// Size function for serialization
	getItemSize := func(ref *BinaryEntryRef) int {
		entry := ref.GetBinaryEntry()
		if entry == nil {
			return 0
		}
		return int(entry.Size)
	}

	// String comparator function
	cmpKey := func(a, b string) int {
		return strings.Compare(a, b)
	}

	skiplist := zcsl.MakeZeroCopySkiplist[BinaryEntryRef, string, string](
		maxLevels,
		getKeyFromItem,
		getItemSize,
		cmpKey,
	)

	return &SkiplistWrapper{
		skiplist: skiplist,
	}
}

// Insert adds a BinaryEntryRef with specific context
func (sw *SkiplistWrapper) Insert(ref BinaryEntryRef, context string) bool {
	return sw.skiplist.Insert(&ref, context)
}

// Find searches for an entry by its relative path and returns entry with context
func (sw *SkiplistWrapper) Find(relativePath string) (*binaryEntry, string) {
	itemPtr, context := sw.skiplist.Find(relativePath)
	if itemPtr != nil {
		ref := itemPtr.Item()
		entry := ref.GetBinaryEntry()
		return entry, context
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
		ref := current.Item()
		entry := ref.GetBinaryEntry()
		if entry != nil {
			if !callback(entry, context) {
				break
			}
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
		ref := first.Item()
		return ref.GetBinaryEntry()
	}
	return nil
}

// Last returns the last entry in the skiplist
func (sw *SkiplistWrapper) Last() *binaryEntry {
	last := sw.skiplist.Last()
	if last != nil {
		ref := last.Item()
		return ref.GetBinaryEntry()
	}
	return nil
}

// ToIovecSlice generates Iovec slices for all items
func (sw *SkiplistWrapper) ToIovecSlice() []syscall.Iovec {
	// Use CallbackToIovecSlice to ensure proper BinaryEntryRef resolution
	return sw.CallbackToIovecSlice(func(entry *binaryEntry, context string) bool {
		return true // Include all entries
	})
}

// ToContextIovecSlice generates Iovec slices for items matching the context
func (sw *SkiplistWrapper) ToContextIovecSlice(context string) []syscall.Iovec {
	// Use CallbackToIovecSlice to ensure proper BinaryEntryRef resolution
	return sw.CallbackToIovecSlice(func(entry *binaryEntry, entryContext string) bool {
		return entryContext == context
	})
}

// ToNotContextIovecSlice generates Iovec slices for items not matching the context
func (sw *SkiplistWrapper) ToNotContextIovecSlice(context string) []syscall.Iovec {
	// Use CallbackToIovecSlice to ensure proper BinaryEntryRef resolution
	return sw.CallbackToIovecSlice(func(entry *binaryEntry, entryContext string) bool {
		return entryContext != context
	})
}

// CallbackToIovecSlice generates Iovec slices for items that match the callback filter
func (sw *SkiplistWrapper) CallbackToIovecSlice(callback func(*binaryEntry, string) bool) []syscall.Iovec {
	var iovecs []syscall.Iovec
	
	// Iterate through all items and create IoVec entries for resolved binaryEntry pointers
	sw.ForEach(func(entry *binaryEntry, context string) bool {
		if callback(entry, context) {
			// Create IoVec pointing to the resolved binaryEntry
			iovec := syscall.Iovec{
				Base: (*byte)(unsafe.Pointer(entry)),
				Len:  uint64(entry.Size),
			}
			iovecs = append(iovecs, iovec)
		}
		return true // Continue iteration
	})
	
	return iovecs
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
	for current := sw.skiplist.First(); current != nil; current = current.Next() {
		entryContext := current.Context()
		if entryContext != context {
			ref := *current.Item()
			result.Insert(ref, entryContext)
		}
	}
	return result
}
