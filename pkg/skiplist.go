package dircachefilehash

import (
	"strings"
	"sync"

	"github.com/mattkeenan/zerocopyskiplist"
)

// SkiplistWrapper wraps zerocopyskiplist to provide the same interface as BPlusTree
type SkiplistWrapper struct {
	skiplist *zerocopyskiplist.ZeroCopySkiplist[FileEntry, string]
	mutex    sync.RWMutex
}

// NewSkiplistWrapper creates a new skiplist wrapper with the specified max levels
func NewSkiplistWrapper(maxLevels int) *SkiplistWrapper {
	if maxLevels < 8 {
		maxLevels = 16 // reasonable default
	}

	// Key extractor function - extracts RelativePath as the key
	getKeyFromItem := func(entry *FileEntry) string {
		return entry.RelativePath
	}

	// Size function for serialization (not used in our current implementation)
	getItemSize := func(entry *FileEntry) int {
		// Rough estimate of FileEntry size including variable path length
		return 80 + len(entry.RelativePath) + len(entry.Hash)
	}

	// String comparator function
	cmpKey := func(a, b string) int {
		return strings.Compare(a, b)
	}

	skiplist := zerocopyskiplist.MakeZeroCopySkiplist(
		maxLevels,
		getKeyFromItem,
		getItemSize,
		cmpKey,
	)

	return &SkiplistWrapper{
		skiplist: skiplist,
	}
}

// Insert adds a file entry to the skiplist
func (sw *SkiplistWrapper) Insert(entry FileEntry) {
	sw.mutex.Lock()
	defer sw.mutex.Unlock()

	// Create a copy of the entry to store in the skiplist
	entryCopy := entry
	sw.skiplist.Insert(&entryCopy)
}

// GetSortedEntries returns all entries in sorted order by filename
func (sw *SkiplistWrapper) GetSortedEntries() []FileEntry {
	sw.mutex.RLock()
	defer sw.mutex.RUnlock()

	var entries []FileEntry

	// Iterate through the skiplist from first to last
	for current := sw.skiplist.First(); current != nil; current = current.Next() {
		entries = append(entries, *current.Item())
	}

	return entries
}

// Merge merges another skiplist into this skiplist
func (sw *SkiplistWrapper) Merge(other *SkiplistWrapper) {
	if other == nil {
		return
	}

	sw.mutex.Lock()
	defer sw.mutex.Unlock()
	other.mutex.RLock()
	defer other.mutex.RUnlock()

	// Use MergeTheirs strategy to prefer entries from the other skiplist (newer entries)
	err := sw.skiplist.Merge(other.skiplist, zerocopyskiplist.MergeTheirs)
	if err != nil {
		// In case of error, fall back to manual merge
		sw.manualMerge(other)
	}
}

// manualMerge performs manual merge as fallback
func (sw *SkiplistWrapper) manualMerge(other *SkiplistWrapper) {
	// Get all entries from the other skiplist
	otherEntries := other.getSortedEntriesUnsafe()

	// Insert all entries (duplicates will replace existing ones)
	for _, entry := range otherEntries {
		entryCopy := entry
		sw.skiplist.Insert(&entryCopy)
	}
}

// Delete removes entries from this skiplist that exist in the other skiplist
func (sw *SkiplistWrapper) Delete(other *SkiplistWrapper) {
	if other == nil {
		return
	}

	sw.mutex.Lock()
	defer sw.mutex.Unlock()
	other.mutex.RLock()
	defer other.mutex.RUnlock()

	// Get entries to delete
	entriesToDelete := other.getSortedEntriesUnsafe()

	// Delete each entry by its key (RelativePath)
	for _, entry := range entriesToDelete {
		sw.skiplist.Delete(entry.RelativePath)
	}
}

// getSortedEntriesUnsafe returns sorted entries without locking (internal use)
func (sw *SkiplistWrapper) getSortedEntriesUnsafe() []FileEntry {
	var entries []FileEntry

	for current := sw.skiplist.First(); current != nil; current = current.Next() {
		entries = append(entries, *current.Item())
	}

	return entries
}

// Find searches for an entry by its relative path
func (sw *SkiplistWrapper) Find(relativePath string) *FileEntry {
	sw.mutex.RLock()
	defer sw.mutex.RUnlock()

	if found := sw.skiplist.Find(relativePath); found != nil {
		return found.Item()
	}
	return nil
}

// Length returns the number of entries in the skiplist
func (sw *SkiplistWrapper) Length() int {
	sw.mutex.RLock()
	defer sw.mutex.RUnlock()

	return sw.skiplist.Length()
}

// IsEmpty returns true if the skiplist has no entries
func (sw *SkiplistWrapper) IsEmpty() bool {
	sw.mutex.RLock()
	defer sw.mutex.RUnlock()

	return sw.skiplist.IsEmpty()
}

// Copy creates a copy of the skiplist structure
func (sw *SkiplistWrapper) Copy() *SkiplistWrapper {
	sw.mutex.RLock()
	defer sw.mutex.RUnlock()

	newWrapper := &SkiplistWrapper{
		skiplist: sw.skiplist.Copy(),
	}

	return newWrapper
}
