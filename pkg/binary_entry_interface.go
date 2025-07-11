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
}

// BinaryEntryImplementationType identifies the type of implementation
type BinaryEntryImplementationType int

const (
	// SkiplistImplementation - mmap-backed entries in skiplist
	SkiplistImplementation BinaryEntryImplementationType = iota
	
	// ReadWriteImplementation - standard file I/O access
	ReadWriteImplementation
	
	// IterativeSkiplistImplementation - mmap with iterative skiplist building
	IterativeSkiplistImplementation
	
	// ScanImplementation - ephemeral mmap entries for hash coordination
	ScanImplementation
)

// String returns the string representation of the implementation type
func (t BinaryEntryImplementationType) String() string {
	switch t {
	case SkiplistImplementation:
		return "Skiplist"
	case ReadWriteImplementation:
		return "ReadWrite"
	case IterativeSkiplistImplementation:
		return "IterativeSkiplist"
	case ScanImplementation:
		return "Scan"
	default:
		return "Unknown"
	}
}

// BinaryEntryBase provides common functionality for BinaryEntryInterface implementations
// This can be embedded in concrete implementations to provide standard locking behavior
type BinaryEntryBase struct {
	mutex sync.RWMutex
	implementationType BinaryEntryImplementationType
}

// NewBinaryEntryBase creates a new BinaryEntryBase with the specified implementation type
func NewBinaryEntryBase(implType BinaryEntryImplementationType) BinaryEntryBase {
	return BinaryEntryBase{
		implementationType: implType,
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

// ImplementationType returns the implementation type
func (base *BinaryEntryBase) ImplementationType() BinaryEntryImplementationType {
	return base.implementationType
}

// IsEphemeral returns true if this implementation type can have ephemeral entries
func (base *BinaryEntryBase) IsEphemeral() bool {
	return base.implementationType == ScanImplementation
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