package dircachefilehash

import (
	"encoding/hex"
	"fmt"
	"unsafe"
)

// BEIndexFileMmapEntry implements BinaryEntryInterface for mmap access to index files with iterative skiplist building
//
// This implementation handles entries accessed via memory mapping with skiplist support:
// - Uses memory mapping for zero-copy access to index files
// - Supports iterative skiplist building during HwangLin operations
// - Provides binaryEntryRef for efficient skiplist coordination
// - Error handling for mmap failures (unmapping, remapping)
// - Thread-safe concurrent access with RWMutex coordination
//
// Safety features:
// - RWMutex cooperative locking with underlying index-level coordination
// - Error returns for mmap failures (munmap, mremap scenarios)
// - Bounds checking and offset validation
// - Skiplist building capability signaling
type BEIndexFileMmapEntry struct {
	BinaryEntryBase
	entryRef binaryEntryRef // Reference to mmap'd index entry
	context  string         // Context for this entry (e.g., MainContext, CacheContext)
}

// NewBEIndexFileMmapEntry creates a new BEIndexFileMmapEntry from a binaryEntryRef
// The reference should point to a valid entry in a memory-mapped index file
// context: the context for this entry (e.g., MainContext, CacheContext, ScanContext)
func NewBEIndexFileMmapEntry(entryRef binaryEntryRef, context string) *BEIndexFileMmapEntry {
	return &BEIndexFileMmapEntry{
		BinaryEntryBase: NewBinaryEntryBase(BEIndexFileMmap),
		entryRef:        entryRef,
		context:         context,
	}
}

// getBinaryEntry safely resolves the entry reference with proper locking
// Returns the entry pointer and nil error if successful
// Can fail if the underlying mmap is unmapped or remapped
func (ime *BEIndexFileMmapEntry) getBinaryEntry() (*binaryEntry, error) {
	// Acquire read lock on the underlying index for mremap safety
	if ime.entryRef.IndexFile != nil {
		ime.entryRef.IndexFile.mutex.RLock()
		defer ime.entryRef.IndexFile.mutex.RUnlock()
	}

	// Get the actual entry pointer
	entry := ime.entryRef.GetBinaryEntry()
	if entry == nil {
		return nil, ErrEntryInvalidated
	}

	return entry, nil
}

// IsValid performs a quick check if the entry is accessible
// For mmap entries, this checks the underlying mmap validity
func (ime *BEIndexFileMmapEntry) IsValid() bool {
	return ime.entryRef.IndexFile != nil &&
		ime.entryRef.IndexFile.Data != nil &&
		ime.entryRef.Offset >= 0 &&
		ime.entryRef.Offset < len(ime.entryRef.IndexFile.Data)
}

// Size returns the entry size field
func (ime *BEIndexFileMmapEntry) Size() (uint32, error) {
	ime.RLock()
	defer ime.RUnlock()

	entry, err := ime.getBinaryEntry()
	if err != nil {
		return 0, err
	}

	return entry.Size, nil
}

// CTimeWall returns the creation time wall clock value
func (ime *BEIndexFileMmapEntry) CTimeWall() (uint64, error) {
	ime.RLock()
	defer ime.RUnlock()

	entry, err := ime.getBinaryEntry()
	if err != nil {
		return 0, err
	}

	return entry.CTimeWall, nil
}

// MTimeWall returns the modification time wall clock value
func (ime *BEIndexFileMmapEntry) MTimeWall() (uint64, error) {
	ime.RLock()
	defer ime.RUnlock()

	entry, err := ime.getBinaryEntry()
	if err != nil {
		return 0, err
	}

	return entry.MTimeWall, nil
}

// Dev returns the device ID
func (ime *BEIndexFileMmapEntry) Dev() (uint32, error) {
	ime.RLock()
	defer ime.RUnlock()

	entry, err := ime.getBinaryEntry()
	if err != nil {
		return 0, err
	}

	return entry.Dev, nil
}

// Ino returns the inode number
func (ime *BEIndexFileMmapEntry) Ino() (uint32, error) {
	ime.RLock()
	defer ime.RUnlock()

	entry, err := ime.getBinaryEntry()
	if err != nil {
		return 0, err
	}

	return entry.Ino, nil
}

// Mode returns the file mode
func (ime *BEIndexFileMmapEntry) Mode() (uint32, error) {
	ime.RLock()
	defer ime.RUnlock()

	entry, err := ime.getBinaryEntry()
	if err != nil {
		return 0, err
	}

	return entry.Mode, nil
}

// UID returns the user ID
func (ime *BEIndexFileMmapEntry) UID() (uint32, error) {
	ime.RLock()
	defer ime.RUnlock()

	entry, err := ime.getBinaryEntry()
	if err != nil {
		return 0, err
	}

	return entry.UID, nil
}

// GID returns the group ID
func (ime *BEIndexFileMmapEntry) GID() (uint32, error) {
	ime.RLock()
	defer ime.RUnlock()

	entry, err := ime.getBinaryEntry()
	if err != nil {
		return 0, err
	}

	return entry.GID, nil
}

// FileSize returns the file size in bytes
func (ime *BEIndexFileMmapEntry) FileSize() (uint64, error) {
	ime.RLock()
	defer ime.RUnlock()

	entry, err := ime.getBinaryEntry()
	if err != nil {
		return 0, err
	}

	return entry.FileSize, nil
}

// HashType returns the hash algorithm type
func (ime *BEIndexFileMmapEntry) HashType() (uint16, error) {
	ime.RLock()
	defer ime.RUnlock()

	entry, err := ime.getBinaryEntry()
	if err != nil {
		return 0, err
	}

	return entry.HashType, nil
}

// Hash returns the file hash as a byte array
func (ime *BEIndexFileMmapEntry) Hash() ([20]byte, error) {
	ime.RLock()
	defer ime.RUnlock()

	entry, err := ime.getBinaryEntry()
	if err != nil {
		return [20]byte{}, err
	}

	// Convert from [64]byte to [20]byte (taking only first 20 bytes)
	var hash [20]byte
	copy(hash[:], entry.Hash[:20])
	return hash, nil
}

// EntryFlags returns the entry flags
func (ime *BEIndexFileMmapEntry) EntryFlags() (uint32, error) {
	ime.RLock()
	defer ime.RUnlock()

	entry, err := ime.getBinaryEntry()
	if err != nil {
		return 0, err
	}

	return uint32(entry.EntryFlags), nil
}

// RelativePath returns the relative file path
func (ime *BEIndexFileMmapEntry) RelativePath() (string, error) {
	ime.RLock()
	defer ime.RUnlock()

	entry, err := ime.getBinaryEntry()
	if err != nil {
		return "", err
	}

	// Calculate path location after the fixed-size binaryEntry struct
	pathPtr := unsafe.Add(unsafe.Pointer(entry), unsafe.Sizeof(*entry))
	pathBytes := (*[256]byte)(pathPtr) // Max reasonable path length for bounds checking

	// Find null terminator to determine actual path length
	pathLen := 0
	for i := range len(pathBytes) {
		if pathBytes[i] == 0 {
			pathLen = i
			break
		}
	}

	if pathLen == 0 {
		// Empty path represents current directory - normalize to "." like ls -al
		return ".", nil
	}

	return string(pathBytes[:pathLen]), nil
}

// HashString returns the hash as a hexadecimal string
func (ime *BEIndexFileMmapEntry) HashString() (string, error) {
	hash, err := ime.Hash()
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(hash[:]), nil
}

// IsDeleted returns true if the entry is marked as deleted
func (ime *BEIndexFileMmapEntry) IsDeleted() (bool, error) {
	flags, err := ime.EntryFlags()
	if err != nil {
		return false, err
	}

	// Check bit 0 for deletion flag (matching existing implementation)
	return (flags & 1) != 0, nil
}

// SetHash updates the entry's hash and hash type
// For mmap entries, this allows in-place updates during iterative processing
func (ime *BEIndexFileMmapEntry) SetHash(hashBytes []byte, hashType uint16) error {
	ime.Lock()
	defer ime.Unlock()

	entry, err := ime.getBinaryEntry()
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
// For mmap entries, this allows in-place updates during iterative processing
func (ime *BEIndexFileMmapEntry) SetDeleted(deleted bool) error {
	ime.Lock()
	defer ime.Unlock()

	entry, err := ime.getBinaryEntry()
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
func (ime *BEIndexFileMmapEntry) GetBinaryEntryRef() (binaryEntryRef, bool) {
	return ime.entryRef, true
}

// GetContext returns the context for this mmap index entry
// Context is provided during creation and identifies the source/purpose of the entry
func (ime *BEIndexFileMmapEntry) GetContext() (string, error) {
	return ime.context, nil
}

// RefCounted interface implementation for mmap index file entries

// IncRef increments the reference count on the underlying mmapIndexFile
func (ime *BEIndexFileMmapEntry) IncRef() {
	if ime.entryRef.IndexFile != nil {
		ime.entryRef.IndexFile.IncRef()
	}
}

// DecRef decrements the reference count on the underlying mmapIndexFile
func (ime *BEIndexFileMmapEntry) DecRef() {
	if ime.entryRef.IndexFile != nil {
		ime.entryRef.IndexFile.DecRef()
	}
}

// RefCount returns the reference count of the underlying mmapIndexFile
func (ime *BEIndexFileMmapEntry) RefCount() int32 {
	if ime.entryRef.IndexFile != nil {
		return ime.entryRef.IndexFile.RefCount()
	}
	return 0
}

// LoadIndexFileMmap loads an index file via mmap and creates a factory for creating entries
// This is a helper function for creating BEIndexFileMmapEntry instances from an index file
func LoadIndexFileMmap(filePath string, ms *MetaStore) (*mmapIndexFile, error) {
	// Use the existing infrastructure to load the index file
	_, indexFile, err := ms.loadIndexFromFileWithTracking(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to load index file %s: %w", filePath, err)
	}

	return indexFile, nil
}

// CreateEntryFromOffset creates a BEIndexFileMmapEntry from an offset within the loaded index
// offset should be relative to the entries section (after header)
func CreateEntryFromOffset(indexFile *mmapIndexFile, entryOffset int) *BEIndexFileMmapEntry {
	entryRef := binaryEntryRef{
		Offset:    entryOffset,
		IndexFile: indexFile,
	}

	return NewBEIndexFileMmapEntry(entryRef, "mmap")
}
