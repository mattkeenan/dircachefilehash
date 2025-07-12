package dircachefilehash

import (
	"encoding/hex"
	"sync"
)

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
func (si *SkiplistIterator) Next() (BinaryEntryInterface, error) {
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
	
	// Wrap the entry in a BinaryEntryInterface
	// Since this is from a skiplist, we create a minimal wrapper
	// TODO: Integrate with proper BESkiplistEntry once binaryEntryRef migration is complete
	wrapper := &legacyBinaryEntryWrapper{entry: foundEntry}
	
	return wrapper, nil
}

// Close releases any resources held by the iterator
// For SkiplistIterator, this just marks it as closed since we don't own the skiplist
func (si *SkiplistIterator) Close() error {
	si.markClosed()
	si.skiplist = nil
	return nil
}

// legacyBinaryEntryWrapper provides a minimal BinaryEntryInterface wrapper for *binaryEntry
// This is a temporary bridge until full BESkiplistEntry integration is complete
type legacyBinaryEntryWrapper struct {
	entry *binaryEntry
	mutex sync.RWMutex
}

// Size returns the entry size
func (w *legacyBinaryEntryWrapper) Size() (uint32, error) {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	return w.entry.Size, nil
}

// CTimeWall returns the creation time wall clock
func (w *legacyBinaryEntryWrapper) CTimeWall() (uint64, error) {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	return w.entry.CTimeWall, nil
}

// MTimeWall returns the modification time wall clock
func (w *legacyBinaryEntryWrapper) MTimeWall() (uint64, error) {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	return w.entry.MTimeWall, nil
}

// Dev returns the device ID
func (w *legacyBinaryEntryWrapper) Dev() (uint32, error) {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	return w.entry.Dev, nil
}

// Ino returns the inode number
func (w *legacyBinaryEntryWrapper) Ino() (uint32, error) {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	return w.entry.Ino, nil
}

// Mode returns the file mode
func (w *legacyBinaryEntryWrapper) Mode() (uint32, error) {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	return w.entry.Mode, nil
}

// UID returns the user ID
func (w *legacyBinaryEntryWrapper) UID() (uint32, error) {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	return w.entry.UID, nil
}

// GID returns the group ID
func (w *legacyBinaryEntryWrapper) GID() (uint32, error) {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	return w.entry.GID, nil
}

// FileSize returns the file size
func (w *legacyBinaryEntryWrapper) FileSize() (uint64, error) {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	return w.entry.FileSize, nil
}

// HashType returns the hash type
func (w *legacyBinaryEntryWrapper) HashType() (uint16, error) {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	return w.entry.HashType, nil
}

// Hash returns the file hash
func (w *legacyBinaryEntryWrapper) Hash() ([20]byte, error) {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	var hash [20]byte
	copy(hash[:], w.entry.Hash[:20])
	return hash, nil
}

// EntryFlags returns the entry flags
func (w *legacyBinaryEntryWrapper) EntryFlags() (uint32, error) {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	return uint32(w.entry.EntryFlags), nil
}

// RelativePath returns the relative path
func (w *legacyBinaryEntryWrapper) RelativePath() (string, error) {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	return w.entry.RelativePath(), nil
}

// HashString returns the hash as a hex string
func (w *legacyBinaryEntryWrapper) HashString() (string, error) {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	return hex.EncodeToString(w.entry.Hash[:]), nil
}

// IsDeleted returns whether the entry is marked as deleted
func (w *legacyBinaryEntryWrapper) IsDeleted() (bool, error) {
	w.mutex.RLock()
	defer w.mutex.RUnlock()
	return w.entry.IsDeleted(), nil
}

// SetHash updates the file hash (not supported for legacy wrapper)
func (w *legacyBinaryEntryWrapper) SetHash(hashBytes []byte, hashType uint16) error {
	return ErrEntryNotWritable
}

// SetDeleted updates the deletion flag (not supported for legacy wrapper)
func (w *legacyBinaryEntryWrapper) SetDeleted(deleted bool) error {
	return ErrEntryNotWritable
}

// RLock acquires a read lock
func (w *legacyBinaryEntryWrapper) RLock() {
	w.mutex.RLock()
}

// RUnlock releases a read lock
func (w *legacyBinaryEntryWrapper) RUnlock() {
	w.mutex.RUnlock()
}

// Lock acquires a write lock
func (w *legacyBinaryEntryWrapper) Lock() {
	w.mutex.Lock()
}

// Unlock releases a write lock
func (w *legacyBinaryEntryWrapper) Unlock() {
	w.mutex.Unlock()
}

// IsValid returns true (legacy entries are always valid)
func (w *legacyBinaryEntryWrapper) IsValid() bool {
	return w.entry != nil
}

// SupportsSkiplistBuilding returns false (legacy wrapper doesn't support this)
func (w *legacyBinaryEntryWrapper) SupportsSkiplistBuilding() bool {
	return false
}

// GetBinaryEntryRef returns false (legacy wrapper doesn't have refs)
func (w *legacyBinaryEntryWrapper) GetBinaryEntryRef() (binaryEntryRef, bool) {
	return binaryEntryRef{}, false
}

// Length returns the number of entries in the skiplist (for convenience/debugging)
func (si *SkiplistIterator) Length() int {
	if si.skiplist == nil {
		return 0
	}
	return si.skiplist.Length()
}