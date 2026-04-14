package dircachefilehash

import (
	"encoding/hex"
	"fmt"
)

// BESkiplistEntry implements BinaryEntryInterface for mmap-backed entries in skiplist
//
// This implementation handles entries that are already loaded in memory via skiplist:
// - Entries are stable (no ephemeral behavior like scan entries)
// - Backed by memory-mapped index files for zero-copy access
// - Most commonly used implementation for existing cached data
// - Read-only access pattern (no hash updates during normal operation)
//
// Safety features:
// - RWMutex locking for concurrent access protection
// - Underlying index-level RWMutex coordination for mremap safety
// - Error returns for consistency with interface (though failures rare)
type BESkiplistEntry struct {
	BinaryEntryBase
	entryRef binaryEntryRef   // Reference to mmap'd index entry via skiplist
	skiplist *skiplistWrapper // Reference to skiplist for context lookup
}

// NewBESkiplistEntry creates a new BESkiplistEntry from a binaryEntryRef and skiplist
// The reference should point to a valid entry in a memory-mapped index file
func NewBESkiplistEntry(entryRef binaryEntryRef, skiplist *skiplistWrapper) *BESkiplistEntry {
	return &BESkiplistEntry{
		BinaryEntryBase: NewBinaryEntryBase(BESkiplist),
		entryRef:        entryRef,
		skiplist:        skiplist,
	}
}

// getBinaryEntry safely resolves the entry reference with proper locking
// Returns the entry pointer and nil error if successful
// For skiplist entries, failures are rare since they're stable
func (sle *BESkiplistEntry) getBinaryEntry() (*binaryEntry, error) {
	// Acquire read lock on the underlying index for mremap safety
	if sle.entryRef.IndexFile != nil {
		sle.entryRef.IndexFile.mutex.RLock()
		defer sle.entryRef.IndexFile.mutex.RUnlock()
	}

	// Get the actual entry pointer
	entry := sle.entryRef.GetBinaryEntry()
	if entry == nil {
		return nil, ErrEntryInvalidated
	}

	return entry, nil
}

// IsValid performs a quick check if the entry is accessible
// For skiplist entries, this should almost always return true
func (sle *BESkiplistEntry) IsValid() bool {
	return sle.entryRef.IndexFile != nil &&
		sle.entryRef.IndexFile.Data != nil &&
		sle.entryRef.Offset >= 0 &&
		sle.entryRef.Offset < len(sle.entryRef.IndexFile.Data)
}

// Size returns the entry size field
func (sle *BESkiplistEntry) Size() (uint32, error) {
	sle.RLock()
	defer sle.RUnlock()

	entry, err := sle.getBinaryEntry()
	if err != nil {
		return 0, err
	}

	return entry.Size, nil
}

// CTimeWall returns the creation time wall clock value
func (sle *BESkiplistEntry) CTimeWall() (uint64, error) {
	sle.RLock()
	defer sle.RUnlock()

	entry, err := sle.getBinaryEntry()
	if err != nil {
		return 0, err
	}

	return entry.CTimeWall, nil
}

// MTimeWall returns the modification time wall clock value
func (sle *BESkiplistEntry) MTimeWall() (uint64, error) {
	sle.RLock()
	defer sle.RUnlock()

	entry, err := sle.getBinaryEntry()
	if err != nil {
		return 0, err
	}

	return entry.MTimeWall, nil
}

// Dev returns the device ID
func (sle *BESkiplistEntry) Dev() (uint32, error) {
	sle.RLock()
	defer sle.RUnlock()

	entry, err := sle.getBinaryEntry()
	if err != nil {
		return 0, err
	}

	return entry.Dev, nil
}

// Ino returns the inode number
func (sle *BESkiplistEntry) Ino() (uint32, error) {
	sle.RLock()
	defer sle.RUnlock()

	entry, err := sle.getBinaryEntry()
	if err != nil {
		return 0, err
	}

	return entry.Ino, nil
}

// Mode returns the file mode
func (sle *BESkiplistEntry) Mode() (uint32, error) {
	sle.RLock()
	defer sle.RUnlock()

	entry, err := sle.getBinaryEntry()
	if err != nil {
		return 0, err
	}

	return entry.Mode, nil
}

// UID returns the user ID
func (sle *BESkiplistEntry) UID() (uint32, error) {
	sle.RLock()
	defer sle.RUnlock()

	entry, err := sle.getBinaryEntry()
	if err != nil {
		return 0, err
	}

	return entry.UID, nil
}

// GID returns the group ID
func (sle *BESkiplistEntry) GID() (uint32, error) {
	sle.RLock()
	defer sle.RUnlock()

	entry, err := sle.getBinaryEntry()
	if err != nil {
		return 0, err
	}

	return entry.GID, nil
}

// FileSize returns the file size in bytes
func (sle *BESkiplistEntry) FileSize() (uint64, error) {
	sle.RLock()
	defer sle.RUnlock()

	entry, err := sle.getBinaryEntry()
	if err != nil {
		return 0, err
	}

	return entry.FileSize, nil
}

// HashType returns the hash algorithm type
func (sle *BESkiplistEntry) HashType() (uint16, error) {
	sle.RLock()
	defer sle.RUnlock()

	entry, err := sle.getBinaryEntry()
	if err != nil {
		return 0, err
	}

	return entry.HashType, nil
}

// Hash returns the file hash as a byte array
func (sle *BESkiplistEntry) Hash() ([20]byte, error) {
	sle.RLock()
	defer sle.RUnlock()

	entry, err := sle.getBinaryEntry()
	if err != nil {
		return [20]byte{}, err
	}

	// Convert from [64]byte to [20]byte (taking only first 20 bytes)
	var hash [20]byte
	copy(hash[:], entry.Hash[:20])
	return hash, nil
}

// EntryFlags returns the entry flags
func (sle *BESkiplistEntry) EntryFlags() (uint32, error) {
	sle.RLock()
	defer sle.RUnlock()

	entry, err := sle.getBinaryEntry()
	if err != nil {
		return 0, err
	}

	return uint32(entry.EntryFlags), nil
}

// RelativePath returns the relative file path.
// Delegates to binaryEntry.RelativePath() which uses the entry's Size field to
// determine path bounds. This is safe without additional bounds checking because
// (non-obvious at first glance) every entry has already passed validateEntryChaining
// during index loading, which enforces Size <= 4096 and validates alignment. The
// Size field is therefore guaranteed to be reasonable before an entry reaches a skiplist.
func (sle *BESkiplistEntry) RelativePath() (string, error) {
	sle.RLock()
	defer sle.RUnlock()

	entry, err := sle.getBinaryEntry()
	if err != nil {
		return "", err
	}

	return entry.RelativePath(), nil
}

// HashString returns the hash as a hexadecimal string
func (sle *BESkiplistEntry) HashString() (string, error) {
	hash, err := sle.Hash()
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(hash[:]), nil
}

// IsDeleted returns true if the entry is marked as deleted
func (sle *BESkiplistEntry) IsDeleted() (bool, error) {
	flags, err := sle.EntryFlags()
	if err != nil {
		return false, err
	}

	// Check bit 0 for deletion flag (matching existing implementation)
	return (flags & 1) != 0, nil
}

// SetHash updates the entry's hash and hash type
// For skiplist entries, this is typically read-only, but supported for interface consistency
func (sle *BESkiplistEntry) SetHash(hashBytes []byte, hashType uint16) error {
	sle.Lock()
	defer sle.Unlock()

	entry, err := sle.getBinaryEntry()
	if err != nil {
		return err
	}

	// Validate hash length
	if len(hashBytes) != 20 {
		return fmt.Errorf("invalid hash length: got %d, expected 20", len(hashBytes))
	}

	// Update hash and hash type in place
	copy(entry.Hash[:], hashBytes)
	entry.HashType = hashType

	return nil
}

// SetDeleted updates the entry's deletion flag
// For skiplist entries, this is typically read-only, but supported for interface consistency
func (sle *BESkiplistEntry) SetDeleted(deleted bool) error {
	sle.Lock()
	defer sle.Unlock()

	entry, err := sle.getBinaryEntry()
	if err != nil {
		return err
	}

	// Update deletion flag (bit 0)
	if deleted {
		entry.EntryFlags |= 1
	} else {
		entry.EntryFlags &^= 1
	}

	return nil
}

// GetBinaryEntryRef returns the underlying binaryEntryRef for skiplist building
func (sle *BESkiplistEntry) GetBinaryEntryRef() (binaryEntryRef, bool) {
	return sle.entryRef, true
}

// GetContext returns the context for this skiplist entry
// Context is retrieved from the skiplist by looking up this entry's path
// (MainContext, CacheContext, ScanContext, etc.)
func (sle *BESkiplistEntry) GetContext() (string, error) {
	if sle.skiplist == nil {
		return "", fmt.Errorf("skiplist reference is nil")
	}

	// Get the path for this entry to look up its context
	path, err := sle.RelativePath()
	if err != nil {
		return "", fmt.Errorf("failed to get relative path for context lookup: %v", err)
	}

	// Find the entry in the skiplist to get its context
	_, context := sle.skiplist.Find(path)
	return context, nil
}
