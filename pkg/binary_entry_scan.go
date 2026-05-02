package dircachefilehash

import (
	"encoding/hex"
	"fmt"
	"os"
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
// - No locking needed (pipeline channels provide synchronisation)
//
// Key benefits over v0.6 mmap approach:
// - Eliminates scan index file creation and cleanup
// - Reduces memory usage (no sparse index files)
// - Better performance (only hash files that will be indexed)
//
// Thread safety: BEScanEntry has single-owner semantics in the pipeline.
// Ownership transfers through channels (compare → hash → reorder → write).
// Channel send/receive provides the happens-before guarantee.
type BEScanEntry struct {
	BinaryEntryBase
	binaryData []byte // Single contiguous buffer containing binaryEntry + path data
	relPath    string // Relative path (cached for convenience)
}

// NewBEScanEntry creates a new heap-allocated BEScanEntry for filesystem scanning
// Creates entry with metadata but NO hash (lazy hashing approach)
func NewBEScanEntry(relPath string, fileInfo os.FileInfo, statInfo *syscall.Stat_t) *BEScanEntry {
	// Handle nil inputs — return an invalid entry that will fail IsValid() checks
	if fileInfo == nil || statInfo == nil {
		return &BEScanEntry{
			BinaryEntryBase: NewBinaryEntryBase(BEScan),
			binaryData:      nil,
			relPath:         relPath,
		}
	}

	// Calculate total size needed for binaryEntry + path + padding
	totalSize := BESizeFromPathLen(len(relPath))

	// Debug: Print size calculation
	if IsDebugEnabled("write") {
		fmt.Fprintf(os.Stderr, "[DEBUG-SIZE] NewBEScanEntry for %s: pathLen=%d, totalSize=%d\n", relPath, len(relPath), totalSize)
	}

	// Allocate single contiguous buffer for the complete binary entry
	binaryData := make([]byte, totalSize)

	// Cast the beginning of the buffer as a binaryEntry struct
	entry := (*binaryEntry)(unsafe.Pointer(&binaryData[0]))

	// Fill in metadata from file system scan
	entry.Size = uint32(totalSize)

	// Debug: Verify size was set correctly
	if IsDebugEnabled("write") {
		fmt.Fprintf(os.Stderr, "[DEBUG-SIZE] NewBEScanEntry entry.Size set to: %d\n", entry.Size)
	}
	entry.CTimeWall = encodeWallTime(statInfo.Ctim.Sec, statInfo.Ctim.Nsec)
	entry.MTimeWall = encodeWallTime(statInfo.Mtim.Sec, statInfo.Mtim.Nsec)
	entry.Dev = uint32(statInfo.Dev)
	entry.Ino = uint32(statInfo.Ino)
	entry.Mode = uint32(fileInfo.Mode())
	entry.UID = statInfo.Uid
	entry.GID = statInfo.Gid
	entry.FileSize = uint64(fileInfo.Size())
	entry.HashType = 0 // No hash type initially (lazy hashing)
	// entry.Hash remains zero-valued (no hash initially)
	entry.EntryFlags = 0 // Not deleted initially

	// Copy path starting after the struct (matching RelativePath)
	pathOffset := int(unsafe.Sizeof(*entry))
	pathBytes := []byte(relPath)
	copy(binaryData[pathOffset:], pathBytes)

	// Add null terminator
	if pathOffset+len(pathBytes) < len(binaryData) {
		binaryData[pathOffset+len(pathBytes)] = 0
	}

	return &BEScanEntry{
		BinaryEntryBase: NewBinaryEntryBase(BEScan),
		binaryData:      binaryData,
		relPath:         relPath,
	}
}

// getBinaryEntry safely returns the heap-allocated entry with proper locking
// Returns the entry pointer and nil error if successful
// v0.7: Much simpler than v0.6 - no mmap complexity
func (sbe *BEScanEntry) getBinaryEntry() (*binaryEntry, error) {
	// Quick validity check - binaryData should never be nil for heap allocation
	if sbe.binaryData == nil {
		return nil, ErrEntryInvalidated
	}

	// Return pointer to binaryEntry struct at start of buffer
	entry := (*binaryEntry)(unsafe.Pointer(&sbe.binaryData[0]))
	return entry, nil
}

// IsValid performs a quick check if the entry is still accessible
// v0.7: Much simpler for heap allocation - just check if binaryData exists
func (sbe *BEScanEntry) IsValid() bool {
	return sbe.binaryData != nil
}

// Size returns the entry size field
func (sbe *BEScanEntry) Size() (uint32, error) {
	entry, err := sbe.getBinaryEntry()
	if err != nil {
		return 0, err
	}

	return entry.Size, nil
}

// CTimeWall returns the creation time wall clock value
func (sbe *BEScanEntry) CTimeWall() (uint64, error) {
	entry, err := sbe.getBinaryEntry()
	if err != nil {
		return 0, err
	}

	return entry.CTimeWall, nil
}

// RelativePath returns the relative path for this entry
// v0.7: Stored separately from binaryEntry for heap allocation
func (sbe *BEScanEntry) RelativePath() (string, error) {
	if sbe.binaryData == nil {
		return "", ErrEntryInvalidated
	}
	return sbe.relPath, nil
}

// MTimeWall returns the modification time wall clock value
func (sbe *BEScanEntry) MTimeWall() (uint64, error) {
	entry, err := sbe.getBinaryEntry()
	if err != nil {
		return 0, err
	}

	return entry.MTimeWall, nil
}

// Dev returns the device ID
func (sbe *BEScanEntry) Dev() (uint32, error) {
	entry, err := sbe.getBinaryEntry()
	if err != nil {
		return 0, err
	}

	return entry.Dev, nil
}

// Ino returns the inode number
func (sbe *BEScanEntry) Ino() (uint32, error) {
	entry, err := sbe.getBinaryEntry()
	if err != nil {
		return 0, err
	}

	return entry.Ino, nil
}

// Mode returns the file mode
func (sbe *BEScanEntry) Mode() (uint32, error) {
	entry, err := sbe.getBinaryEntry()
	if err != nil {
		return 0, err
	}

	return entry.Mode, nil
}

// UID returns the user ID
func (sbe *BEScanEntry) UID() (uint32, error) {
	entry, err := sbe.getBinaryEntry()
	if err != nil {
		return 0, err
	}

	return entry.UID, nil
}

// GID returns the group ID
func (sbe *BEScanEntry) GID() (uint32, error) {
	entry, err := sbe.getBinaryEntry()
	if err != nil {
		return 0, err
	}

	return entry.GID, nil
}

// FileSize returns the file size in bytes
func (sbe *BEScanEntry) FileSize() (uint64, error) {
	entry, err := sbe.getBinaryEntry()
	if err != nil {
		return 0, err
	}

	return entry.FileSize, nil
}

// HashType returns the hash algorithm type
func (sbe *BEScanEntry) HashType() (uint16, error) {
	entry, err := sbe.getBinaryEntry()
	if err != nil {
		return 0, err
	}

	return entry.HashType, nil
}

// Hash returns the file hash as a byte array
func (sbe *BEScanEntry) Hash() ([20]byte, error) {
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
	entry, err := sbe.getBinaryEntry()
	if err != nil {
		return err
	}

	// Validate hash length based on hash type
	var expectedSize int
	switch hashType {
	case HashTypeSHA1:
		expectedSize = HashSizeSHA1 // 20 bytes
	case HashTypeSHA256:
		expectedSize = HashSizeSHA256 // 32 bytes
	case HashTypeSHA512:
		expectedSize = HashSizeSHA512 // 64 bytes
	default:
		return fmt.Errorf("unknown hash type: %d", hashType)
	}

	if len(hashBytes) != expectedSize {
		return fmt.Errorf("invalid hash length for type %s: got %d, expected %d", HashTypeName(hashType), len(hashBytes), expectedSize)
	}

	// Update hash and hash type in place
	copy(entry.Hash[:], hashBytes)
	entry.HashType = hashType

	return nil
}

// SetDeleted updates the entry's deletion flag
func (sbe *BEScanEntry) SetDeleted(deleted bool) error {
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

// RLock is a no-op for heap-allocated entries (single-owner pipeline semantics)
func (sbe *BEScanEntry) RLock() {}

// RUnlock is a no-op for heap-allocated entries
func (sbe *BEScanEntry) RUnlock() {}

// Lock is a no-op for heap-allocated entries (single-owner pipeline semantics)
func (sbe *BEScanEntry) Lock() {}

// Unlock is a no-op for heap-allocated entries
func (sbe *BEScanEntry) Unlock() {}

// GetBinaryData returns the heap-allocated binaryEntry as binary data for IoVec writing
// This returns the actual contiguous buffer - no copying needed
func (sbe *BEScanEntry) GetBinaryData() ([]byte, error) {
	if sbe.binaryData == nil {
		return nil, ErrEntryInvalidated
	}

	// Return the actual contiguous buffer containing binaryEntry + path data
	return sbe.binaryData, nil
}
