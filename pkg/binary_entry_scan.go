package dircachefilehash

import (
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"syscall"
	"unsafe"
)

// BEScanEntry implements BinaryEntryInterface for ephemeral heap-allocated entries during scanning
//
// v0.7 Architecture: Uses heap allocation instead of mmap scan index files:
// - Entries are allocated on the heap with metadata but NO hash initially  
// - Lazy hashing: hash is computed only if entry is selected for writing to index
// - Standard Go garbage collection handles memory management
// - No mremap/munmap complexity or file cleanup required
// - Simpler locking model (per-entry mutex only)
//
// Key benefits over v0.6 mmap approach:
// - Eliminates scan index file creation and cleanup
// - Reduces memory usage (no sparse index files)
// - Better performance (only hash files that will be indexed)
type BEScanEntry struct {
	BinaryEntryBase
	entry   *binaryEntry // Heap-allocated entry data (no hash initially)
	relPath string       // Relative path (stored separately from binaryEntry)
	mutex   sync.RWMutex // Per-entry locking for hash coordination
}

// NewBEScanEntry creates a new heap-allocated BEScanEntry for filesystem scanning
// Creates entry with metadata but NO hash (lazy hashing approach)
func NewBEScanEntry(relPath string, fileInfo os.FileInfo, statInfo *syscall.Stat_t) *BEScanEntry {
	// Allocate binaryEntry on heap with metadata but empty hash
	entry := &binaryEntry{}
	
	// Fill in metadata from file system scan
	entry.Size = uint32(unsafe.Sizeof(binaryEntry{})) + uint32(len(relPath)) + 1 // +1 for null terminator
	modTime := fileInfo.ModTime()
	entry.CTimeWall = encodeWallTime(modTime.Unix(), int64(modTime.Nanosecond())) // Use ModTime for now, could enhance with ctime
	entry.MTimeWall = encodeWallTime(modTime.Unix(), int64(modTime.Nanosecond()))
	entry.Dev = uint32(statInfo.Dev)
	entry.Ino = uint32(statInfo.Ino)
	entry.Mode = uint32(fileInfo.Mode())
	entry.UID = statInfo.Uid
	entry.GID = statInfo.Gid
	entry.FileSize = uint64(fileInfo.Size())
	entry.HashType = 0    // No hash type initially (lazy hashing)
	// entry.Hash remains zero-valued (no hash initially)
	entry.EntryFlags = 0  // Not deleted initially
	
	return &BEScanEntry{
		BinaryEntryBase: NewBinaryEntryBase(BEScan),
		entry:          entry,
		relPath:        relPath,
		mutex:          sync.RWMutex{},
	}
}

// getBinaryEntry safely returns the heap-allocated entry with proper locking
// Returns the entry pointer and nil error if successful
// v0.7: Much simpler than v0.6 - no mmap complexity
func (sbe *BEScanEntry) getBinaryEntry() (*binaryEntry, error) {
	// Quick validity check - entry should never be nil for heap allocation
	if sbe.entry == nil {
		return nil, ErrEntryInvalidated
	}
	
	// Return heap-allocated entry (no mmap locking needed)
	return sbe.entry, nil
}

// IsValid performs a quick check if the entry is still accessible
// v0.7: Much simpler for heap allocation - just check if entry exists
func (sbe *BEScanEntry) IsValid() bool {
	return sbe.entry != nil
}

// Size returns the entry size field
func (sbe *BEScanEntry) Size() (uint32, error) {
	sbe.mutex.RLock()
	defer sbe.mutex.RUnlock()
	
	entry, err := sbe.getBinaryEntry()
	if err != nil {
		return 0, err
	}
	
	return entry.Size, nil
}

// CTimeWall returns the creation time wall clock value
func (sbe *BEScanEntry) CTimeWall() (uint64, error) {
	sbe.mutex.RLock()
	defer sbe.mutex.RUnlock()
	
	entry, err := sbe.getBinaryEntry()
	if err != nil {
		return 0, err
	}
	
	return entry.CTimeWall, nil
}

// RelativePath returns the relative path for this entry
// v0.7: Stored separately from binaryEntry for heap allocation
func (sbe *BEScanEntry) RelativePath() (string, error) {
	return sbe.relPath, nil
}

// MTimeWall returns the modification time wall clock value
func (sbe *BEScanEntry) MTimeWall() (uint64, error) {
	sbe.mutex.RLock()
	defer sbe.RUnlock()
	
	entry, err := sbe.getBinaryEntry()
	if err != nil {
		return 0, err
	}
	
	return entry.MTimeWall, nil
}

// Dev returns the device ID
func (sbe *BEScanEntry) Dev() (uint32, error) {
	sbe.RLock()
	defer sbe.RUnlock()
	
	entry, err := sbe.getBinaryEntry()
	if err != nil {
		return 0, err
	}
	
	return entry.Dev, nil
}

// Ino returns the inode number
func (sbe *BEScanEntry) Ino() (uint32, error) {
	sbe.RLock()
	defer sbe.RUnlock()
	
	entry, err := sbe.getBinaryEntry()
	if err != nil {
		return 0, err
	}
	
	return entry.Ino, nil
}

// Mode returns the file mode
func (sbe *BEScanEntry) Mode() (uint32, error) {
	sbe.RLock()
	defer sbe.RUnlock()
	
	entry, err := sbe.getBinaryEntry()
	if err != nil {
		return 0, err
	}
	
	return entry.Mode, nil
}

// UID returns the user ID
func (sbe *BEScanEntry) UID() (uint32, error) {
	sbe.RLock()
	defer sbe.RUnlock()
	
	entry, err := sbe.getBinaryEntry()
	if err != nil {
		return 0, err
	}
	
	return entry.UID, nil
}

// GID returns the group ID
func (sbe *BEScanEntry) GID() (uint32, error) {
	sbe.RLock()
	defer sbe.RUnlock()
	
	entry, err := sbe.getBinaryEntry()
	if err != nil {
		return 0, err
	}
	
	return entry.GID, nil
}

// FileSize returns the file size in bytes
func (sbe *BEScanEntry) FileSize() (uint64, error) {
	sbe.RLock()
	defer sbe.RUnlock()
	
	entry, err := sbe.getBinaryEntry()
	if err != nil {
		return 0, err
	}
	
	return entry.FileSize, nil
}

// HashType returns the hash algorithm type
func (sbe *BEScanEntry) HashType() (uint16, error) {
	sbe.RLock()
	defer sbe.RUnlock()
	
	entry, err := sbe.getBinaryEntry()
	if err != nil {
		return 0, err
	}
	
	return entry.HashType, nil
}

// Hash returns the file hash as a byte array
func (sbe *BEScanEntry) Hash() ([20]byte, error) {
	sbe.RLock()
	defer sbe.RUnlock()
	
	entry, err := sbe.getBinaryEntry()
	if err != nil {
		return [20]byte{}, err
	}
	
	// Convert from [64]byte to [20]byte (taking only first 20 bytes)
	var hash [20]byte
	copy(hash[:], entry.Hash[:20])
	return hash, nil
}

// EntryFlags returns the entry flags
func (sbe *BEScanEntry) EntryFlags() (uint32, error) {
	sbe.RLock()
	defer sbe.RUnlock()
	
	entry, err := sbe.getBinaryEntry()
	if err != nil {
		return 0, err
	}
	
	return uint32(entry.EntryFlags), nil
}


// HashString returns the hash as a hexadecimal string
func (sbe *BEScanEntry) HashString() (string, error) {
	hash, err := sbe.Hash()
	if err != nil {
		return "", err
	}
	
	return hex.EncodeToString(hash[:]), nil
}

// IsDeleted returns true if the entry is marked as deleted
func (sbe *BEScanEntry) IsDeleted() (bool, error) {
	flags, err := sbe.EntryFlags()
	if err != nil {
		return false, err
	}
	
	// Check bit 0 for deletion flag (matching existing implementation)
	return (flags & 1) != 0, nil
}

// SetHash updates the entry's hash and hash type
// This is used by hash workers to update entries in-place during scanning
func (sbe *BEScanEntry) SetHash(hashBytes []byte, hashType uint16) error {
	sbe.mutex.Lock()
	defer sbe.mutex.Unlock()
	
	entry, err := sbe.getBinaryEntry()
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
func (sbe *BEScanEntry) SetDeleted(deleted bool) error {
	sbe.Lock()
	defer sbe.Unlock()
	
	entry, err := sbe.getBinaryEntry()
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

// GetBinaryEntryRef returns false for heap-allocated entries
// v0.7: Heap entries don't have binaryEntryRef since they're not mmap-backed
func (sbe *BEScanEntry) GetBinaryEntryRef() (binaryEntryRef, bool) {
	return binaryEntryRef{}, false
}

// GetContext returns the context for this scan entry
// Scan entries are always created during filesystem scanning operations, so they have ScanContext
// TODO: If future core operations need context from non-skiplist sources, this may need
// to be enhanced to support context determination from other sources
func (sbe *BEScanEntry) GetContext() (string, error) {
	return ScanContext, nil
}

// RefCounted interface implementation for scan entries

// IncRef increments the reference count - no-op for heap entries
func (sbe *BEScanEntry) IncRef() {
	// Heap-allocated entries don't need reference counting
}

// DecRef decrements the reference count - no-op for heap entries  
func (sbe *BEScanEntry) DecRef() {
	// Heap-allocated entries don't need reference counting
}

// RefCount returns the reference count - for heap entries, always return 1
func (sbe *BEScanEntry) RefCount() int32 {
	// Heap-allocated entries don't need reference counting
	return 1
}

// RLock provides manual read locking for batch operations
func (sbe *BEScanEntry) RLock() {
	sbe.mutex.RLock()
}

// RUnlock releases the read lock
func (sbe *BEScanEntry) RUnlock() {
	sbe.mutex.RUnlock()
}

// Lock provides manual write locking for batch operations
func (sbe *BEScanEntry) Lock() {
	sbe.mutex.Lock()
}

// Unlock releases the write lock
func (sbe *BEScanEntry) Unlock() {
	sbe.mutex.Unlock()
}