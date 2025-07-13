package dircachefilehash

import (
	"fmt"
	"sync"
)

// BinaryEntryInterface abstracts access to binary entry data
// regardless of storage mechanism (mmap, read/write, ephemeral)
//
// This interface unifies access across four distinct data sources:
// 1. Skiplist (mmap-backed) - In-memory merged view using mmap'd data
// 2. Index file (read/write) - Direct file access without skiplist creation
// 3. Index file (mmap + iterative skiplist) - Mmap'd index with skiplist built during HwangLin
// 4. Scanning (mmap-backed, ephemeral) - Ephemeral entries in scan index
//
// Error Handling:
// - All accessor methods return (value, error) to handle ephemeral entry failures
// - Ephemeral entries can fail due to munmap or mremap operations
// - Non-ephemeral entries should only return errors in exceptional circumstances
//
// Locking Strategy:
// - Uses RWMutex-based cooperative locking
// - Read operations (accessors): Multiple readers allowed
// - Write operations (setters): Exclusive access
// - Manual locking available for batch operations
type BinaryEntryInterface interface {
	// Field accessors (acquire read lock, can return errors for ephemeral entries)
	Size() (uint32, error)
	CTimeWall() (uint64, error)
	MTimeWall() (uint64, error)
	Dev() (uint32, error)
	Ino() (uint32, error)
	Mode() (uint32, error)
	UID() (uint32, error)
	GID() (uint32, error)
	FileSize() (uint64, error)
	HashType() (uint16, error)
	Hash() ([20]byte, error)
	EntryFlags() (uint32, error)
	
	// Derived methods (acquire read lock, can return errors for ephemeral entries)
	RelativePath() (string, error)
	HashString() (string, error)
	IsDeleted() (bool, error)
	
	// Setters (acquire write lock, can return errors for ephemeral entries)
	SetHash(hashBytes []byte, hashType uint16) error
	SetDeleted(deleted bool) error
	
	// Manual locking for batch operations
	// These allow efficient multi-field access without re-acquiring locks
	RLock()
	RUnlock()
	Lock()
	Unlock()
	
	// Entry lifecycle
	IsValid() bool  // Quick check if entry is still accessible (for ephemeral entries)
	
	// Skiplist building capabilities
	SupportsSkiplistBuilding() bool                    // Can entries be used to build skiplist?
	GetBinaryEntryRef() (binaryEntryRef, bool)        // Get ref if available for skiplist building
}

// BinaryEntryImplementationType identifies the type of implementation
type BinaryEntryImplementationType int

const (
	// BESkiplist - mmap-backed entries in skiplist
	BESkiplist BinaryEntryImplementationType = iota
	
	// BEIndexFileIO - standard file I/O access
	BEIndexFileIO
	
	// BEIndexFileMmap - mmap with iterative skiplist building
	BEIndexFileMmap
	
	// BEScan - ephemeral mmap entries for hash coordination
	BEScan
)

// String returns the string representation of the implementation type
func (t BinaryEntryImplementationType) String() string {
	switch t {
	case BESkiplist:
		return "BESkiplist"
	case BEIndexFileIO:
		return "BEIndexFileIO"
	case BEIndexFileMmap:
		return "BEIndexFileMmap"
	case BEScan:
		return "BEScan"
	default:
		return "Unknown"
	}
}

// BinaryEntryBase provides common functionality for BinaryEntryInterface implementations
// This can be embedded in concrete implementations to provide standard locking behavior
type BinaryEntryBase struct {
	mutex              sync.RWMutex
	implementationType BinaryEntryImplementationType
	supportsSkiplist   bool  // Whether this implementation supports skiplist building
}

// NewBinaryEntryBase creates a new BinaryEntryBase with the specified implementation type
func NewBinaryEntryBase(implType BinaryEntryImplementationType) BinaryEntryBase {
	// Determine skiplist support based on implementation type
	supportsSkiplist := (implType == BESkiplist || implType == BEIndexFileMmap || implType == BEScan)
	
	return BinaryEntryBase{
		implementationType: implType,
		supportsSkiplist:   supportsSkiplist,
	}
}

// RLock acquires a read lock
func (base *BinaryEntryBase) RLock() {
	base.mutex.RLock()
}

// RUnlock releases a read lock
func (base *BinaryEntryBase) RUnlock() {
	base.mutex.RUnlock()
}

// Lock acquires a write lock
func (base *BinaryEntryBase) Lock() {
	base.mutex.Lock()
}

// Unlock releases a write lock
func (base *BinaryEntryBase) Unlock() {
	base.mutex.Unlock()
}

// SupportsSkiplistBuilding returns whether this implementation supports skiplist building
func (base *BinaryEntryBase) SupportsSkiplistBuilding() bool {
	return base.supportsSkiplist
}

// GetBinaryEntryRef returns a binaryEntryRef if available (default: not supported)
// Implementations that support this should override this method
func (base *BinaryEntryBase) GetBinaryEntryRef() (binaryEntryRef, bool) {
	return binaryEntryRef{}, false
}

// ImplementationType returns the implementation type
func (base *BinaryEntryBase) ImplementationType() BinaryEntryImplementationType {
	return base.implementationType
}

// IsEphemeral returns true if this implementation type can have ephemeral entries
func (base *BinaryEntryBase) IsEphemeral() bool {
	return base.implementationType == BEScan
}

// Common error types for BinaryEntryInterface implementations
var (
	// ErrEntryInvalidated is returned when an ephemeral entry has been invalidated
	ErrEntryInvalidated = fmt.Errorf("binary entry has been invalidated (munmap/mremap)")
	
	// ErrEntryNotWritable is returned when trying to modify a read-only entry
	ErrEntryNotWritable = fmt.Errorf("binary entry is read-only")
	
	// ErrEntryCorrupted is returned when entry data appears corrupted
	ErrEntryCorrupted = fmt.Errorf("binary entry data is corrupted")
)

// needsHash determines if a file needs hashing by comparing metadata (mremap-safe).
// This mirrors the logic of isFileChangedFromScanned but uses safe interface methods.
// Returns true if the file has changed and needs hashing.
func needsHash(existingEntry, scannedEntry BinaryEntryInterface) bool {
	// If no existing entry, file is new and needs hashing
	if existingEntry == nil {
		return true
	}
	
	// If no scanned entry, assume needs hashing (shouldn't happen in normal flow)
	if scannedEntry == nil {
		return true
	}
	
	// Quick size check
	existingSize, err := existingEntry.FileSize()
	if err != nil {
		return true // Assume needs hashing if we can't read existing size
	}
	scannedSize, err := scannedEntry.FileSize()
	if err != nil {
		return true // Assume needs hashing if we can't read scanned size
	}
	if existingSize != scannedSize {
		return true
	}
	
	// Check ownership
	existingUID, err := existingEntry.UID()
	if err != nil {
		return true
	}
	scannedUID, err := scannedEntry.UID()
	if err != nil {
		return true
	}
	if existingUID != scannedUID {
		return true
	}
	
	existingGID, err := existingEntry.GID()
	if err != nil {
		return true
	}
	scannedGID, err := scannedEntry.GID()
	if err != nil {
		return true
	}
	if existingGID != scannedGID {
		return true
	}
	
	// Check mode
	existingMode, err := existingEntry.Mode()
	if err != nil {
		return true
	}
	scannedMode, err := scannedEntry.Mode()
	if err != nil {
		return true
	}
	if existingMode != scannedMode {
		return true
	}
	
	// Check timestamps using wall time
	existingCTime, err := existingEntry.CTimeWall()
	if err != nil {
		return true
	}
	scannedCTime, err := scannedEntry.CTimeWall()
	if err != nil {
		return true
	}
	if existingCTime != scannedCTime {
		return true
	}
	
	existingMTime, err := existingEntry.MTimeWall()
	if err != nil {
		return true
	}
	scannedMTime, err := scannedEntry.MTimeWall()
	if err != nil {
		return true
	}
	if existingMTime != scannedMTime {
		return true
	}
	
	return false // No changes detected, no hashing needed
}