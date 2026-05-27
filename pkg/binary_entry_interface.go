package dircachefilehash

import (
	"fmt"
	"sync"
)

// RefCounted interface for hierarchical reference counting
// Types that hold references to mmapIndexFile should implement this
type RefCounted interface {
	IncRef()
	DecRef()
	RefCount() int32 // for debugging
}

// BinaryEntryInterface abstracts access to binary entry data
// regardless of storage mechanism (mmap, read/write, ephemeral)
//
// This interface unifies access across four distinct data sources:
// 1. Skiplist (mmap-backed) - In-memory merged view using mmap'd data
// 2. Index file (read/write) - Direct file access without skiplist creation
// 3. Index file (mmap + iterative skiplist) - Mmap'd index with skiplist built during HwangLin
// 4. Scanning (heap-allocated, ephemeral) - Ephemeral entries created during scan
//
// Error Handling:
// - All accessor methods return (value, error) for uniform handling across storage modes
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
	Dev() (uint64, error)
	Ino() (uint64, error)
	Mode() (uint32, error)
	UID() (uint32, error)
	GID() (uint32, error)
	FileSize() (int64, error)
	HashType() (uint16, error)
	Hash() ([20]byte, error)
	EntryFlags() (uint32, error)

	// Derived methods (acquire read lock, can return errors for ephemeral entries)
	RelativePath() (string, error)
	HashString() (string, error)
	IsDeleted() (bool, error)
	GetContext() (string, error)

	// Setters (acquire write lock, can return errors for ephemeral entries)
	SetHash(hashBytes []byte, hashType uint16) error
	SetDeleted(deleted bool) error

	// Hash coordination for two-phase processing
	RequestHash() error             // Request that this entry be hashed (callback → iterator communication)
	IsHashRequested() (bool, error) // Check if hashing has been requested
	IsHashCompleted() (bool, error) // Check if hashing has been completed

	// Internal hash coordination methods (for use by hash managers)
	SetHashJobID(jobID uint64) // Set the job ID when submitting to hash manager
	GetHashJobID() uint64      // Get the job ID for completion tracking
	MarkHashCompleted()        // Mark hashing as completed

	// Manual locking for batch operations
	// These allow efficient multi-field access without re-acquiring locks
	RLock()
	RUnlock()
	Lock()
	Unlock()

	// Entry lifecycle
	IsValid() bool // Quick check if entry is still accessible (for ephemeral entries)

	// Skiplist building capabilities
	SupportsSkiplistBuilding() bool            // Can entries be used to build skiplist?
	GetBinaryEntryRef() (binaryEntryRef, bool) // Get ref if available for skiplist building
}

// BinaryEntryImplementationType identifies the type of implementation
type BinaryEntryImplementationType int

const (
	// BESkiplist - mmap-backed entries in skiplist
	BESkiplist BinaryEntryImplementationType = iota

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
	supportsSkiplist   bool // Whether this implementation supports skiplist building

	// Hash coordination state for two-phase processing
	hashRequested bool   // Whether hashing has been requested
	hashCompleted bool   // Whether hashing has been completed
	hashJobID     uint64 // Job ID if hash has been requested (0 = not requested)
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

// RequestHash requests that this entry be hashed (default implementation)
// This sets the hashRequested flag and assigns a job ID for coordination
// The actual hashing should be handled by the calling code
func (base *BinaryEntryBase) RequestHash() error {
	base.mutex.Lock()
	defer base.mutex.Unlock()

	// Already requested or completed
	if base.hashRequested || base.hashCompleted {
		if IsDebugEnabled("hash") {
			VerboseLog(3, "[HASH-REQUEST] Hash already requested or completed for entry")
		}
		return nil
	}

	// b) Hash request tracing (but this is just flag setting!)
	if IsDebugEnabled("hash") {
		VerboseLog(3, "[HASH-REQUEST] Setting hashRequested flag (but NO actual job submission to manager)")
	}

	// Set request flag - job ID should be assigned by the hash manager
	base.hashRequested = true
	// Note: hashJobID will be set by the hash manager when the job is actually submitted

	return nil
}

// IsHashRequested checks if hashing has been requested (default implementation)
func (base *BinaryEntryBase) IsHashRequested() (bool, error) {
	base.mutex.RLock()
	defer base.mutex.RUnlock()

	return base.hashRequested, nil
}

// IsHashCompleted checks if hashing has been completed (default implementation)
func (base *BinaryEntryBase) IsHashCompleted() (bool, error) {
	base.mutex.RLock()
	defer base.mutex.RUnlock()

	return base.hashCompleted, nil
}

// SetHashJobID sets the job ID for hash coordination (internal use)
// This is called by the hash manager when submitting the job
func (base *BinaryEntryBase) SetHashJobID(jobID uint64) {
	base.mutex.Lock()
	defer base.mutex.Unlock()

	base.hashJobID = jobID
}

// GetHashJobID returns the job ID for hash coordination (internal use)
func (base *BinaryEntryBase) GetHashJobID() uint64 {
	base.mutex.RLock()
	defer base.mutex.RUnlock()

	return base.hashJobID
}

// MarkHashCompleted marks hashing as completed (internal use)
// This is called by the hash manager when the job completes
func (base *BinaryEntryBase) MarkHashCompleted() {
	base.mutex.Lock()
	defer base.mutex.Unlock()

	base.hashCompleted = true
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
	if existingEntry == nil || scannedEntry == nil {
		return true
	}

	// FileSize is int64 (signed, off_t-style); compare it directly rather than
	// folding it into the uint64-typed slice below — an int64->uint64 round-trip
	// would re-introduce the very G115 conversion this task removes. The loop is
	// equality-only, so an inline check is equivalent.
	ef, err := existingEntry.FileSize()
	if err != nil {
		return true
	}
	sf, err := scannedEntry.FileSize()
	if err != nil || ef != sf {
		return true
	}

	fields := []func(BinaryEntryInterface) (uint64, error){
		func(e BinaryEntryInterface) (uint64, error) { v, err := e.UID(); return uint64(v), err },
		func(e BinaryEntryInterface) (uint64, error) { v, err := e.GID(); return uint64(v), err },
		func(e BinaryEntryInterface) (uint64, error) { v, err := e.Mode(); return uint64(v), err },
		func(e BinaryEntryInterface) (uint64, error) { return e.CTimeWall() },
		func(e BinaryEntryInterface) (uint64, error) { return e.MTimeWall() },
	}

	for _, f := range fields {
		ev, err := f(existingEntry)
		if err != nil {
			return true
		}
		sv, err := f(scannedEntry)
		if err != nil || ev != sv {
			return true
		}
	}

	return false
}
