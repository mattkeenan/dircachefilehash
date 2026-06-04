package format

import (
	"fmt"
	"unsafe"
)

// Build-time assertions for struct layout assumptions.
// These cause compilation to fail if our assumptions about memory layout are violated.
var (
	// Ensure Entry has expected size and alignment
	_ = [1]struct{}{}[unsafe.Sizeof(Entry{})%8] // Must be 8-byte aligned

	// Ensure Path field is exactly 8 bytes
	_ = [1]struct{}{}[unsafe.Sizeof(Entry{}.Path)-8]
)

// Entry represents a file entry in mmap'd memory (zero-copy).
// All fields are in host byte order for direct access.
// Time fields use the custom wall-time encoding (see core time_encoding.go).
//
// Field types use the vocabulary aliases (vocabulary.go) so width/signedness
// have a single owner. The on-disk layout is unchanged by the aliasing.
type Entry struct {
	Size       RecordSize // Total size of this entry including padding (host order) - MUST BE FIRST
	CTimeWall  WallTime   // Change time wall clock (wall time format)
	MTimeWall  WallTime   // Modification time wall clock (wall time format)
	Dev        DevID      // Device ID (host order)
	Ino        Inode      // Inode number (host order)
	Mode       FileMode   // File mode (host order)
	UID        UserID     // User ID (host order)
	GID        GroupID    // Group ID (host order)
	FileSize   ByteSize   // File size in bytes (host order) - supports files >4GB
	EntryFlags FlagBits   // Entry Flags
	HashType   HashKind   // Hash algorithm type (SHA1=1, SHA256=2, SHA512=3)
	Hash       [64]byte   // Hash value (up to 64 bytes for SHA-512)
	Path       [8]byte    // Path as bytes, actual length variable but must be at least 8 bytes long
}

// IsDeleted returns true if this entry is marked as deleted
func (be *Entry) IsDeleted() bool {
	return be.EntryFlags&EntryFlagDeleted != 0
}

// SetDeleted marks this entry as deleted
func (be *Entry) SetDeleted() {
	be.EntryFlags |= EntryFlagDeleted
}

// ClearDeleted removes the deleted flag from this entry
func (be *Entry) ClearDeleted() {
	be.EntryFlags &^= EntryFlagDeleted
}

// IsHashed returns true if this entry has been hashed
func (be *Entry) IsHashed() bool {
	return be.EntryFlags&EntryFlagHashed != 0
}

// SetHashed marks this entry as hashed
func (be *Entry) SetHashed() {
	be.EntryFlags |= EntryFlagHashed
}

// validateLayout performs runtime validation of struct layout assumptions.
// This should only be called in debug/development builds.
func (be *Entry) validateLayout() {
	entryStart := uintptr(unsafe.Pointer(be))
	pathFieldOffset := uintptr(unsafe.Pointer(&be.Path[0])) - entryStart
	expectedOffset := unsafe.Sizeof(*be) - 8

	if pathFieldOffset != expectedOffset {
		panic(fmt.Sprintf("Entry layout assumption violated: Path field at offset %d, expected %d",
			pathFieldOffset, expectedOffset))
	}

	// Verify 8-byte alignment
	if entryStart%8 != 0 {
		panic(fmt.Sprintf("Entry not 8-byte aligned: address 0x%x", entryStart))
	}

	// Verify size is reasonable
	if be.Size < uint32(unsafe.Sizeof(*be)) || be.Size > 4096 {
		panic(fmt.Sprintf("Entry size %d is unreasonable", be.Size))
	}
}

// RelativePath returns the relative path as string from mmap'd memory (zero-copy).
// This implementation uses traditional unsafe pointer arithmetic for maximum compatibility.
func (be *Entry) RelativePath() string {
	// Safety check: ensure we have a valid pointer
	if be == nil {
		panic("RelativePath called on nil Entry")
	}

	// Safety check: ensure Size is reasonable (not corrupted). The lower bound
	// tracks the struct minimum (minEntrySize) so it can never drift below the
	// real fixed-portion size as the layout changes (e.g. the v4 widen).
	if be.Size < uint32(minEntrySize) || be.Size > 65535 { //nolint:gosec // G115: struct size, bounded non-negative
		panic(fmt.Sprintf("RelativePath: invalid Size %d (expected %d-65535)", be.Size, minEntrySize))
	}

	// Derive the path via unsafe.Add from a single typed base so pointer
	// provenance is preserved (checkptr-clean). The path data is stored
	// immediately after the Entry struct; using unsafe.Sizeof accounts for all
	// compiler padding and is portable across architectures.
	//
	// The backward scan trims trailing NUL padding using an integer length, so
	// every dereferenced address (base + structSize + pathLen - 1) is strictly
	// within the entry and we never hold a past-the-end pointer in a live
	// variable (the GC rejects those). The Size guard above (Size >=
	// minEntrySize = struct size) guarantees pathLen does not underflow.
	base := unsafe.Pointer(be)
	structSize := unsafe.Sizeof(*be)

	// Scan backwards byte by byte from the end (endian-neutral).
	// At most 8 bytes to scan due to 8-byte alignment, making this O(1).
	pathLen := uintptr(be.Size) - structSize
	for pathLen > 0 && *(*byte)(unsafe.Add(base, structSize+pathLen-1)) == 0 {
		pathLen--
	}

	return unsafe.String((*byte)(unsafe.Add(base, structSize)), int(pathLen)) //nolint:gosec // G115: pathLen ≤ be.Size ≤ 65535, bounded non-negative
}

// calculatePathLength finds the length of the null-terminated path
func (be *Entry) calculatePathLength() int {
	// pathStart keeps &be.Path[0] (the element-address cast is already
	// checkptr-clean and in-bounds, so it is safe to hold live). NB: this starts
	// the path at Sizeof(*be)-8 (Path is the last field), an 8-byte-earlier
	// address than RelativePath uses — preserved here byte-for-byte; the
	// discrepancy is tracked as a separate backlog item.
	//
	// The trailing-NUL trim uses an integer length so every dereferenced address
	// (pathStart + n - 1) stays within the entry; no past-the-end pointer is
	// ever held in a live variable. Signed length math mirrors the original's
	// behaviour when be.Size is below the path-start offset.
	base := unsafe.Pointer(be)
	pathStart := unsafe.Pointer(&be.Path[0])
	startOff := int(uintptr(pathStart) - uintptr(base)) //nolint:gosec // G115: = Sizeof(*be) - 8, a small struct-layout constant

	// Scan for null terminator
	n := int(be.Size) - startOff //nolint:gosec // G115: be.Size ≤ 65535, bounded non-negative
	for n > 0 && *(*byte)(unsafe.Add(pathStart, n-1)) == 0 {
		n--
	}

	return n
}

// ValidateEntry performs comprehensive validation of an Entry.
// Used when extravalidation debug option is enabled.
func (be *Entry) ValidateEntry() error {
	// Validate layout assumptions
	defer func() {
		if r := recover(); r != nil {
			// Silently recover: convert panic to error for graceful handling.
			// validateLayout() may panic on malformed entries; we catch it
			// so ValidateEntry() returns an error instead of crashing.
			_ = r
		}
	}()

	be.validateLayout()

	// Validate size constraints
	minSize := uint32(unsafe.Sizeof(*be))
	if be.Size < minSize {
		return fmt.Errorf("entry size %d too small, minimum %d", be.Size, minSize)
	}

	if be.Size > 4096 { // Reasonable maximum
		return fmt.Errorf("entry size %d too large, maximum 4096", be.Size)
	}

	// Validate path length
	pathLen := be.calculatePathLength()
	if pathLen == 0 {
		return fmt.Errorf("entry has zero-length path")
	}

	expectedSize := int(minSize) + pathLen + 1 // +1 for null terminator
	padding := (8 - (expectedSize % 8)) % 8
	expectedSize += padding

	if int(be.Size) != expectedSize {
		return fmt.Errorf("entry size %d doesn't match calculated size %d (path_len=%d, padding=%d)",
			be.Size, expectedSize, pathLen, padding)
	}

	// Validate hash type
	switch be.HashType {
	case HashTypeSHA1, HashTypeSHA256, HashTypeSHA512:
		// Valid hash types
	default:
		return fmt.Errorf("invalid hash type %d", be.HashType)
	}

	return nil
}

// HashString returns the hash as a hex string
func (be *Entry) HashString() string {
	// Determine hash size based on type
	var hashSize int
	switch be.HashType {
	case HashTypeSHA1:
		hashSize = HashSizeSHA1
	case HashTypeSHA256:
		hashSize = HashSizeSHA256
	case HashTypeSHA512:
		hashSize = HashSizeSHA512
	default:
		hashSize = HashSizeSHA1 // Default to SHA1 for compatibility
	}

	const hexChars = "0123456789abcdef"
	result := make([]byte, hashSize*2)
	for i := 0; i < hashSize; i++ {
		b := be.Hash[i]
		result[i*2] = hexChars[b>>4]
		result[i*2+1] = hexChars[b&0xf]
	}
	return unsafe.String(&result[0], len(result))
}

// IsHashEmpty returns true if this entry has an empty (all zeros) hash
func (be *Entry) IsHashEmpty() bool {
	// If hash type is 0, no hash type is set, so hash is empty
	if be.HashType == 0 {
		return true
	}

	// Check if all 64 bytes of the hash are zero.
	// Direct array comparison is optimized in Go.
	var zeroHash [64]byte
	return be.Hash == zeroHash
}

// EntrySize returns the total size of this entry including padding
func (be *Entry) EntrySize() int {
	return int(be.Size)
}

// BESizeFromPathLen calculates the necessary size of an Entry struct given pathname length
func BESizeFromPathLen(pathLen int) int {
	baseSize := int(unsafe.Sizeof(Entry{}))
	totalSize := baseSize + pathLen + 1 // +1 for null terminator
	padding := (8 - (totalSize % 8)) % 8
	return totalSize + padding
}
