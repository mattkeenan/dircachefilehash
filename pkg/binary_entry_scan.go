package dircachefilehash

import (
	"encoding/hex"
	"fmt"
	"unsafe"
)

// ScanBinaryEntry implements BinaryEntryInterface for ephemeral mmap entries in scan indices
//
// This is the most complex implementation because scan entries are ephemeral:
// - They exist in memory-mapped scan index files that can be remapped (mremap)
// - The underlying memory can be unmapped when scan completes
// - Hash workers update entries in-place during concurrent processing
// - Memory addresses can change during mremap operations
//
// Safety is provided by:
// - RWMutex locking to coordinate with mremap operations
// - Quick validity checks to detect unmapped memory
// - Error returns for all operations that can fail due to ephemeral nature
type ScanBinaryEntry struct {
	BinaryEntryBase
	entryRef binaryEntryRef // Reference to mmap'd scan index entry
}

// NewScanBinaryEntry creates a new ScanBinaryEntry from a binaryEntryRef
// The reference must point to a valid entry in a scan index file
func NewScanBinaryEntry(entryRef binaryEntryRef) *ScanBinaryEntry {
	return &ScanBinaryEntry{
		BinaryEntryBase: NewBinaryEntryBase(ScanImplementation),
		entryRef:        entryRef,
	}
}

// getBinaryEntry safely resolves the entry reference with proper locking
// Returns the entry pointer and nil error if successful
// Returns nil and error if the entry has been invalidated
func (sbe *ScanBinaryEntry) getBinaryEntry() (*binaryEntry, error) {
	// Quick validity check without acquiring locks
	if !sbe.IsValid() {
		return nil, ErrEntryInvalidated
	}
	
	// Acquire read lock on the underlying index
	// This prevents mremap operations while we're accessing memory
	if sbe.entryRef.IndexFile != nil {
		sbe.entryRef.IndexFile.mutex.RLock()
		defer sbe.entryRef.IndexFile.mutex.RUnlock()
	}
	
	// Get the actual entry pointer
	entry := sbe.entryRef.GetBinaryEntry()
	if entry == nil {
		return nil, ErrEntryInvalidated
	}
	
	return entry, nil
}

// IsValid performs a quick check if the entry is still accessible
// This doesn't guarantee the entry won't become invalid immediately after,
// but provides a fast path for obviously invalid entries
func (sbe *ScanBinaryEntry) IsValid() bool {
	return sbe.entryRef.IndexFile != nil && 
		   sbe.entryRef.IndexFile.Data != nil &&
		   sbe.entryRef.Offset >= 0 &&
		   sbe.entryRef.Offset < len(sbe.entryRef.IndexFile.Data)
}

// Size returns the entry size field
func (sbe *ScanBinaryEntry) Size() (uint32, error) {
	sbe.RLock()
	defer sbe.RUnlock()
	
	entry, err := sbe.getBinaryEntry()
	if err != nil {
		return 0, err
	}
	
	return entry.Size, nil
}

// CTimeWall returns the creation time wall clock value
func (sbe *ScanBinaryEntry) CTimeWall() (uint64, error) {
	sbe.RLock()
	defer sbe.RUnlock()
	
	entry, err := sbe.getBinaryEntry()
	if err != nil {
		return 0, err
	}
	
	return entry.CTimeWall, nil
}

// MTimeWall returns the modification time wall clock value
func (sbe *ScanBinaryEntry) MTimeWall() (uint64, error) {
	sbe.RLock()
	defer sbe.RUnlock()
	
	entry, err := sbe.getBinaryEntry()
	if err != nil {
		return 0, err
	}
	
	return entry.MTimeWall, nil
}

// Dev returns the device ID
func (sbe *ScanBinaryEntry) Dev() (uint32, error) {
	sbe.RLock()
	defer sbe.RUnlock()
	
	entry, err := sbe.getBinaryEntry()
	if err != nil {
		return 0, err
	}
	
	return entry.Dev, nil
}

// Ino returns the inode number
func (sbe *ScanBinaryEntry) Ino() (uint32, error) {
	sbe.RLock()
	defer sbe.RUnlock()
	
	entry, err := sbe.getBinaryEntry()
	if err != nil {
		return 0, err
	}
	
	return entry.Ino, nil
}

// Mode returns the file mode
func (sbe *ScanBinaryEntry) Mode() (uint32, error) {
	sbe.RLock()
	defer sbe.RUnlock()
	
	entry, err := sbe.getBinaryEntry()
	if err != nil {
		return 0, err
	}
	
	return entry.Mode, nil
}

// UID returns the user ID
func (sbe *ScanBinaryEntry) UID() (uint32, error) {
	sbe.RLock()
	defer sbe.RUnlock()
	
	entry, err := sbe.getBinaryEntry()
	if err != nil {
		return 0, err
	}
	
	return entry.UID, nil
}

// GID returns the group ID
func (sbe *ScanBinaryEntry) GID() (uint32, error) {
	sbe.RLock()
	defer sbe.RUnlock()
	
	entry, err := sbe.getBinaryEntry()
	if err != nil {
		return 0, err
	}
	
	return entry.GID, nil
}

// FileSize returns the file size in bytes
func (sbe *ScanBinaryEntry) FileSize() (uint64, error) {
	sbe.RLock()
	defer sbe.RUnlock()
	
	entry, err := sbe.getBinaryEntry()
	if err != nil {
		return 0, err
	}
	
	return entry.FileSize, nil
}

// HashType returns the hash algorithm type
func (sbe *ScanBinaryEntry) HashType() (uint16, error) {
	sbe.RLock()
	defer sbe.RUnlock()
	
	entry, err := sbe.getBinaryEntry()
	if err != nil {
		return 0, err
	}
	
	return entry.HashType, nil
}

// Hash returns the file hash as a byte array
func (sbe *ScanBinaryEntry) Hash() ([20]byte, error) {
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
func (sbe *ScanBinaryEntry) EntryFlags() (uint32, error) {
	sbe.RLock()
	defer sbe.RUnlock()
	
	entry, err := sbe.getBinaryEntry()
	if err != nil {
		return 0, err
	}
	
	return uint32(entry.EntryFlags), nil
}

// RelativePath returns the relative file path
func (sbe *ScanBinaryEntry) RelativePath() (string, error) {
	sbe.RLock()
	defer sbe.RUnlock()
	
	entry, err := sbe.getBinaryEntry()
	if err != nil {
		return "", err
	}
	
	// Calculate path location after the fixed-size binaryEntry struct
	pathPtr := unsafe.Pointer(uintptr(unsafe.Pointer(entry)) + unsafe.Sizeof(*entry))
	pathBytes := (*[256]byte)(pathPtr) // Max reasonable path length for bounds checking
	
	// Find null terminator to determine actual path length
	pathLen := 0
	for i := 0; i < len(pathBytes); i++ {
		if pathBytes[i] == 0 {
			pathLen = i
			break
		}
	}
	
	if pathLen == 0 {
		return "", fmt.Errorf("invalid path in scan entry")
	}
	
	return string(pathBytes[:pathLen]), nil
}

// HashString returns the hash as a hexadecimal string
func (sbe *ScanBinaryEntry) HashString() (string, error) {
	hash, err := sbe.Hash()
	if err != nil {
		return "", err
	}
	
	return hex.EncodeToString(hash[:]), nil
}

// IsDeleted returns true if the entry is marked as deleted
func (sbe *ScanBinaryEntry) IsDeleted() (bool, error) {
	flags, err := sbe.EntryFlags()
	if err != nil {
		return false, err
	}
	
	// Check bit 0 for deletion flag (matching existing implementation)
	return (flags & 1) != 0, nil
}

// SetHash updates the entry's hash and hash type
// This is used by hash workers to update entries in-place during scanning
func (sbe *ScanBinaryEntry) SetHash(hashBytes []byte, hashType uint16) error {
	sbe.Lock()
	defer sbe.Unlock()
	
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
func (sbe *ScanBinaryEntry) SetDeleted(deleted bool) error {
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