package main

import (
	"fmt"
	"unsafe"
)

// binaryEntry matches the struct in pkg/util.go exactly
// This local definition is needed since the original is not exported
type binaryEntry struct {
	Size       uint32   // Total size of this entry including padding (host order) - MUST BE FIRST
	CTimeWall  uint64   // Change time wall clock (Go wall time format)
	MTimeWall  uint64   // Modification time wall clock (Go wall time format)
	Dev        uint32   // Device ID (host order)
	Ino        uint32   // Inode number (host order)
	Mode       uint32   // File mode (host order)
	UID        uint32   // User ID (host order)
	GID        uint32   // Group ID (host order)
	FileSize   uint64   // File size in bytes (host order) - supports files >4GB
	EntryFlags uint16   // Entry Flags
	HashType   uint16   // Hash algorithm type (SHA1=1, SHA256=2, SHA512=3)
	Hash       [64]byte // Hash value (up to 64 bytes for SHA-512)
	Path       [8]byte  // Path as bytes, actual length variable but must be at least 8 bytes long
}

// SafeEntryAccessor provides safe, bounds-checked access to binaryEntry fields
// This is critical for a repair tool that must handle potentially corrupted data
type SafeEntryAccessor struct {
	data      []byte
	entryIdx  int
	offset    int
	maxOffset int
}

// Field offsets calculated at compile time - never hardcode these!
var (
	offsetSize       = uintptr(0)                                      // Size is first field
	offsetCTimeWall  = unsafe.Offsetof((*binaryEntry)(nil).CTimeWall)  // Will be 4
	offsetMTimeWall  = unsafe.Offsetof((*binaryEntry)(nil).MTimeWall)  // Will be 12
	offsetDev        = unsafe.Offsetof((*binaryEntry)(nil).Dev)        // Will be 20
	offsetIno        = unsafe.Offsetof((*binaryEntry)(nil).Ino)        // Will be 24
	offsetMode       = unsafe.Offsetof((*binaryEntry)(nil).Mode)       // Will be 28
	offsetUID        = unsafe.Offsetof((*binaryEntry)(nil).UID)        // Will be 32
	offsetGID        = unsafe.Offsetof((*binaryEntry)(nil).GID)        // Will be 36
	offsetFileSize   = unsafe.Offsetof((*binaryEntry)(nil).FileSize)   // Will be 40
	offsetEntryFlags = unsafe.Offsetof((*binaryEntry)(nil).EntryFlags) // Will be 48
	offsetHashType   = unsafe.Offsetof((*binaryEntry)(nil).HashType)   // Will be 50
	offsetHash       = unsafe.Offsetof((*binaryEntry)(nil).Hash)       // Will be 52
	offsetPath       = unsafe.Offsetof((*binaryEntry)(nil).Path)       // Will be 116
	minEntrySize     = unsafe.Sizeof(binaryEntry{})
)

// NewSafeEntryAccessor creates a new safe accessor for an entry at the given offset
func NewSafeEntryAccessor(data []byte, entryIdx int, offset int) (*SafeEntryAccessor, error) {
	if offset < 0 || offset >= len(data) {
		return nil, fmt.Errorf("entry %d: invalid offset %d (data length: %d)", entryIdx, offset, len(data))
	}

	// We need at least 4 bytes to read the size field
	if offset+4 > len(data) {
		return nil, fmt.Errorf("entry %d: insufficient data to read size field at offset %d", entryIdx, offset)
	}

	// Read the size field (first 4 bytes)
	size := *(*uint32)(unsafe.Pointer(&data[offset]))

	// Validate size field
	if size == 0 {
		return nil, fmt.Errorf("entry %d: zero size at offset %d", entryIdx, offset)
	}

	if size < uint32(minEntrySize) {
		return nil, fmt.Errorf("entry %d: size %d too small (minimum %d) at offset %d",
			entryIdx, size, minEntrySize, offset)
	}

	if size > 4096 { // Reasonable maximum
		return nil, fmt.Errorf("entry %d: size %d unreasonably large at offset %d", entryIdx, size, offset)
	}

	maxOffset := offset + int(size)
	if maxOffset > len(data) {
		return nil, fmt.Errorf("entry %d: size %d extends beyond data bounds at offset %d", entryIdx, size, offset)
	}

	return &SafeEntryAccessor{
		data:      data,
		entryIdx:  entryIdx,
		offset:    offset,
		maxOffset: maxOffset,
	}, nil
}

// validateFieldAccess checks if we can safely access a field of given size at given offset
func (sea *SafeEntryAccessor) validateFieldAccess(fieldOffset uintptr, fieldSize int, fieldName string) error {
	absoluteOffset := sea.offset + int(fieldOffset)
	if absoluteOffset+fieldSize > sea.maxOffset {
		return fmt.Errorf("entry %d: %s field access would extend beyond entry bounds (offset %d, field size %d, entry max %d)",
			sea.entryIdx, fieldName, absoluteOffset, fieldSize, sea.maxOffset)
	}
	return nil
}

// Safe field readers
func (sea *SafeEntryAccessor) GetSize() (uint32, error) {
	if err := sea.validateFieldAccess(offsetSize, 4, "size"); err != nil {
		return 0, err
	}
	return *(*uint32)(unsafe.Pointer(&sea.data[sea.offset])), nil
}

func (sea *SafeEntryAccessor) GetCTimeWall() (uint64, error) {
	if err := sea.validateFieldAccess(offsetCTimeWall, 8, "ctime"); err != nil {
		return 0, err
	}
	return *(*uint64)(unsafe.Pointer(&sea.data[sea.offset+int(offsetCTimeWall)])), nil
}

func (sea *SafeEntryAccessor) GetMTimeWall() (uint64, error) {
	if err := sea.validateFieldAccess(offsetMTimeWall, 8, "mtime"); err != nil {
		return 0, err
	}
	return *(*uint64)(unsafe.Pointer(&sea.data[sea.offset+int(offsetMTimeWall)])), nil
}

func (sea *SafeEntryAccessor) GetMode() (uint32, error) {
	if err := sea.validateFieldAccess(offsetMode, 4, "mode"); err != nil {
		return 0, err
	}
	return *(*uint32)(unsafe.Pointer(&sea.data[sea.offset+int(offsetMode)])), nil
}

func (sea *SafeEntryAccessor) GetUID() (uint32, error) {
	if err := sea.validateFieldAccess(offsetUID, 4, "uid"); err != nil {
		return 0, err
	}
	return *(*uint32)(unsafe.Pointer(&sea.data[sea.offset+int(offsetUID)])), nil
}

func (sea *SafeEntryAccessor) GetGID() (uint32, error) {
	if err := sea.validateFieldAccess(offsetGID, 4, "gid"); err != nil {
		return 0, err
	}
	return *(*uint32)(unsafe.Pointer(&sea.data[sea.offset+int(offsetGID)])), nil
}

func (sea *SafeEntryAccessor) GetFileSize() (uint64, error) {
	if err := sea.validateFieldAccess(offsetFileSize, 8, "file_size"); err != nil {
		return 0, err
	}
	return *(*uint64)(unsafe.Pointer(&sea.data[sea.offset+int(offsetFileSize)])), nil
}

func (sea *SafeEntryAccessor) GetDev() (uint32, error) {
	if err := sea.validateFieldAccess(offsetDev, 4, "dev"); err != nil {
		return 0, err
	}
	return *(*uint32)(unsafe.Pointer(&sea.data[sea.offset+int(offsetDev)])), nil
}

func (sea *SafeEntryAccessor) GetIno() (uint32, error) {
	if err := sea.validateFieldAccess(offsetIno, 4, "ino"); err != nil {
		return 0, err
	}
	return *(*uint32)(unsafe.Pointer(&sea.data[sea.offset+int(offsetIno)])), nil
}

func (sea *SafeEntryAccessor) GetEntryFlags() (uint16, error) {
	if err := sea.validateFieldAccess(offsetEntryFlags, 2, "entry_flags"); err != nil {
		return 0, err
	}
	return *(*uint16)(unsafe.Pointer(&sea.data[sea.offset+int(offsetEntryFlags)])), nil
}

func (sea *SafeEntryAccessor) GetHashType() (uint16, error) {
	if err := sea.validateFieldAccess(offsetHashType, 2, "hash_type"); err != nil {
		return 0, err
	}
	return *(*uint16)(unsafe.Pointer(&sea.data[sea.offset+int(offsetHashType)])), nil
}

func (sea *SafeEntryAccessor) GetHash() ([64]byte, error) {
	if err := sea.validateFieldAccess(offsetHash, 64, "hash"); err != nil {
		return [64]byte{}, err
	}
	var hash [64]byte
	copy(hash[:], sea.data[sea.offset+int(offsetHash):sea.offset+int(offsetHash)+64])
	return hash, nil
}

// Safe field writers
func (sea *SafeEntryAccessor) SetCTimeWall(value uint64) error {
	if err := sea.validateFieldAccess(offsetCTimeWall, 8, "ctime"); err != nil {
		return err
	}
	*(*uint64)(unsafe.Pointer(&sea.data[sea.offset+int(offsetCTimeWall)])) = value
	return nil
}

func (sea *SafeEntryAccessor) SetMTimeWall(value uint64) error {
	if err := sea.validateFieldAccess(offsetMTimeWall, 8, "mtime"); err != nil {
		return err
	}
	*(*uint64)(unsafe.Pointer(&sea.data[sea.offset+int(offsetMTimeWall)])) = value
	return nil
}

func (sea *SafeEntryAccessor) SetMode(value uint32) error {
	if err := sea.validateFieldAccess(offsetMode, 4, "mode"); err != nil {
		return err
	}
	*(*uint32)(unsafe.Pointer(&sea.data[sea.offset+int(offsetMode)])) = value
	return nil
}

func (sea *SafeEntryAccessor) SetUID(value uint32) error {
	if err := sea.validateFieldAccess(offsetUID, 4, "uid"); err != nil {
		return err
	}
	*(*uint32)(unsafe.Pointer(&sea.data[sea.offset+int(offsetUID)])) = value
	return nil
}

func (sea *SafeEntryAccessor) SetGID(value uint32) error {
	if err := sea.validateFieldAccess(offsetGID, 4, "gid"); err != nil {
		return err
	}
	*(*uint32)(unsafe.Pointer(&sea.data[sea.offset+int(offsetGID)])) = value
	return nil
}

func (sea *SafeEntryAccessor) SetFileSize(value uint64) error {
	if err := sea.validateFieldAccess(offsetFileSize, 8, "file_size"); err != nil {
		return err
	}
	*(*uint64)(unsafe.Pointer(&sea.data[sea.offset+int(offsetFileSize)])) = value
	return nil
}

// GetPath safely extracts the path from the entry
func (sea *SafeEntryAccessor) GetPath() (string, error) {
	if err := sea.validateFieldAccess(offsetPath, 1, "path"); err != nil {
		return "", err
	}

	pathStart := sea.offset + int(offsetPath)
	pathData := sea.data[pathStart:sea.maxOffset]

	// Find null terminator or use all remaining data
	pathEnd := len(pathData)
	for i, b := range pathData {
		if b == 0 {
			pathEnd = i
			break
		}
	}

	return string(pathData[:pathEnd]), nil
}
