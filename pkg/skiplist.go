package dircachefilehash

import (
	"strings"

	zcsl "github.com/mattkeenan/zerocopyskiplist"
)

// SkiplistWrapper wraps zerocopyskiplist for zero-copy access to mmap'd binaryEntry pointers
type SkiplistWrapper struct {
	skiplist     *zcsl.ZeroCopySkiplist[binaryEntry, string]
	context      string
	entryContext map[string]string // Maps relative path to context
}

// NewSkiplistWrapper creates a new skiplist wrapper with context tracking
func NewSkiplistWrapper(maxLevels int, context string) *SkiplistWrapper {
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
		skiplist:     skiplist,
		context:      context,
		entryContext: make(map[string]string),
	}
}

// Insert adds a binaryEntry pointer with specific context
func (sw *SkiplistWrapper) Insert(entry *binaryEntry, context string) {
	sw.skiplist.Insert(entry)
	sw.entryContext[entry.RelativePath()] = context
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

// Merge merges another skiplist into this skiplist with context tracking
func (sw *SkiplistWrapper) Merge(other *SkiplistWrapper) error {
	if other == nil {
		return nil
	}

	err := sw.skiplist.Merge(other.skiplist, zcsl.MergeTheirs)
	if err != nil {
		// Fallback to manual merge if the built-in merge fails
		sw.manualMerge(other)
	}

	// Merge context mappings
	for path, context := range other.entryContext {
		sw.entryContext[path] = context
	}

	return nil
}

// manualMerge performs manual merge as fallback
func (sw *SkiplistWrapper) manualMerge(other *SkiplistWrapper) {
	otherEntries := other.GetSortedEntries()
	for _, entry := range otherEntries {
		sw.skiplist.Insert(entry)
		if context, exists := other.entryContext[entry.RelativePath()]; exists {
			sw.entryContext[entry.RelativePath()] = context
		}
	}
}

// Delete removes entries from this skiplist that exist in the other skiplist
func (sw *SkiplistWrapper) Delete(other *SkiplistWrapper) {
	if other == nil {
		return
	}

	entriesToDelete := other.GetSortedEntries()
	for _, entry := range entriesToDelete {
		relativePath := entry.RelativePath()
		sw.skiplist.Delete(relativePath)
		delete(sw.entryContext, relativePath)
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
	newWrapper := &SkiplistWrapper{
		skiplist:     sw.skiplist.Copy(),
		context:      sw.context,
		entryContext: make(map[string]string),
	}

	// Copy context mappings
	for path, context := range sw.entryContext {
		newWrapper.entryContext[path] = context
	}

	return newWrapper
}

// CopyWithContext creates a copy with a specific context
func (sw *SkiplistWrapper) CopyWithContext(context string) *SkiplistWrapper {
	newWrapper := &SkiplistWrapper{
		skiplist:     sw.skiplist.Copy(),
		context:      context,
		entryContext: make(map[string]string),
	}

	// Copy context mappings
	for path, entryContext := range sw.entryContext {
		newWrapper.entryContext[path] = entryContext
	}

	return newWrapper
}

// GetContext returns the default context for this skiplist
func (sw *SkiplistWrapper) GetContext() string {
	return sw.context
}

// GetEntry returns the context for a specific entry
func (sw *SkiplistWrapper) GetEntry(relativePath string) string {
	if context, exists := sw.entryContext[relativePath]; exists {
		return context
	}
	return sw.context
}

// SetContext sets the default context for this skiplist
func (sw *SkiplistWrapper) SetContext(context string) {
	sw.context = context
}

// FilterExcluding returns a new skiplist excluding entries with specified context
func (sw *SkiplistWrapper) FilterExcluding(excludeContext string) *SkiplistWrapper {
	result := NewSkiplistWrapper(16, sw.context)

	sw.ForEach(func(entry *binaryEntry) bool {
		relativePath := entry.RelativePath()
		entryContext := sw.GetEntry(relativePath)
		if entryContext != excludeContext {
			result.Insert(entry, entryContext)
		}
		return true
	})

	return result
}

// FilterDeleted returns a new skiplist without deleted entries
func (sw *SkiplistWrapper) FilterDeleted() *SkiplistWrapper {
	result := NewSkiplistWrapper(16, sw.context)

	sw.ForEach(func(entry *binaryEntry) bool {
		if !entry.IsDeleted() {
			relativePath := entry.RelativePath()
			entryContext := sw.GetEntry(relativePath)
			result.Insert(entry, entryContext)
		}
		return true
	})

	return result
}

// Stats returns statistics about the skiplist entries
func (sw *SkiplistWrapper) Stats() (total, deleted, active int) {
	sw.ForEach(func(entry *binaryEntry) bool {
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
