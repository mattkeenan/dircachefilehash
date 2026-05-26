package format

import (
	"fmt"
	"unsafe"
)

// SafeEntry provides bounds-checked access to an Entry's fields inside an
// untrusted on-disk buffer. It is the single owner of typed reads/writes over
// index bytes — it replaces dcfhfix's hand-maintained struct duplicate and
// offset table.
//
// It enforces TWO tiers of bounds checking (do not weaken to a len(buf)-only
// check — a repair tool must assume corruption):
//  1. entry-level (in NewSafeEntry): the declared Size is read after a 4-byte
//     check, then validated non-zero / >= struct minimum / <= 4096 /
//     offset+Size <= len(data), yielding maxOffset (the entry's declared end);
//  2. field-level (validateFieldAccess): every field read/write is checked
//     against maxOffset — the entry's own end, NOT merely len(data) — so an
//     undersized Size cannot let a read spill into the next entry.
type SafeEntry struct {
	data      []byte
	entryIdx  int
	offset    int
	maxOffset int
	layout    entryLayout // version-selected field offsets (v4 or legacy)
}

// minEntrySize is the v4 (current) fixed-portion struct size. The per-version
// field offsets live in entryLayout (entry_layout.go + vN_layout.go); this is
// the current layout's minimum, used by RelativePath's corruption floor and
// MinEntrySize.
var minEntrySize = unsafe.Sizeof(Entry{})

// MinEntrySize returns the fixed-portion size of a current (v4) Entry.
func MinEntrySize() int { return int(minEntrySize) } //nolint:gosec // G115: struct size (unsafe.Sizeof), bounded non-negative

// MinEntrySizeForVersion returns the fixed-portion entry size for a version's
// layout. For an unknown/corrupt version it returns the smallest supported size
// (the most permissive floor) so corruption-recovery resync over an untrusted
// header version never over-rejects a legitimate entry; the per-version minimum
// is still enforced strictly downstream by NewSafeEntry.
func MinEntrySizeForVersion(version uint32) int {
	if lay, err := layoutForVersion(version); err == nil {
		return int(lay.minSize) //nolint:gosec // G115: struct size, bounded non-negative
	}
	return int(layoutV2.minSize) //nolint:gosec // G115: struct size, bounded non-negative (smallest supported layout)
}

// NewSafeEntry creates a bounds-checked accessor for an entry at the given
// offset (tier-1 validation). version selects the field layout (v4 vs legacy)
// — pass the validated header version, never a zeroed value, so a legacy file
// is read at its real offsets rather than v4-shaped ones.
func NewSafeEntry(data []byte, entryIdx int, offset int, version uint32) (*SafeEntry, error) {
	layout, err := layoutForVersion(version)
	if err != nil {
		return nil, fmt.Errorf("entry %d: %w", entryIdx, err)
	}

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

	if size < uint32(layout.minSize) { //nolint:gosec // G115: struct size, bounded non-negative
		return nil, fmt.Errorf("entry %d: size %d too small (minimum %d) at offset %d",
			entryIdx, size, layout.minSize, offset)
	}

	if size > 4096 { // Reasonable maximum
		return nil, fmt.Errorf("entry %d: size %d unreasonably large at offset %d", entryIdx, size, offset)
	}

	maxOffset := offset + int(size)
	if maxOffset > len(data) {
		return nil, fmt.Errorf("entry %d: size %d extends beyond data bounds at offset %d", entryIdx, size, offset)
	}

	return &SafeEntry{
		data:      data,
		entryIdx:  entryIdx,
		offset:    offset,
		maxOffset: maxOffset,
		layout:    layout,
	}, nil
}

// validateFieldAccess checks a field of given size at given offset stays within
// the entry's declared end (tier-2).
func (se *SafeEntry) validateFieldAccess(fieldOffset uintptr, fieldSize int, fieldName string) error {
	absoluteOffset := se.offset + int(fieldOffset) //nolint:gosec // G115: field offset, bounded non-negative
	if absoluteOffset+fieldSize > se.maxOffset {
		return fmt.Errorf("entry %d: %s field access would extend beyond entry bounds (offset %d, field size %d, entry max %d)",
			se.entryIdx, fieldName, absoluteOffset, fieldSize, se.maxOffset)
	}
	return nil
}

// readField is the single generic, bounds-checked field reader. Field width is
// derived from T (unsafe.Sizeof), so a future field-width change needs no edit
// here.
func readField[T any](se *SafeEntry, fieldOffset uintptr, fieldName string) (T, error) {
	var zero T
	size := int(unsafe.Sizeof(zero)) //nolint:gosec // G115: field width (unsafe.Sizeof), bounded non-negative
	if err := se.validateFieldAccess(fieldOffset, size, fieldName); err != nil {
		return zero, err
	}
	return *(*T)(unsafe.Pointer(&se.data[se.offset+int(fieldOffset)])), nil //nolint:gosec // G115: field offset, bounded non-negative
}

// writeField is the single generic, bounds-checked field writer.
func writeField[T any](se *SafeEntry, fieldOffset uintptr, fieldName string, value T) error {
	size := int(unsafe.Sizeof(value)) //nolint:gosec // G115: field width (unsafe.Sizeof), bounded non-negative
	if err := se.validateFieldAccess(fieldOffset, size, fieldName); err != nil {
		return err
	}
	*(*T)(unsafe.Pointer(&se.data[se.offset+int(fieldOffset)])) = value //nolint:gosec // G115: field offset, bounded non-negative
	return nil
}

// Safe field readers (typed wrappers over readField). Offsets come from the
// version-selected layout, so a legacy entry is read at its real field offsets.
func (se *SafeEntry) GetSize() (RecordSize, error) {
	return readField[RecordSize](se, se.layout.size, "size")
}
func (se *SafeEntry) GetCTimeWall() (WallTime, error) {
	return readField[WallTime](se, se.layout.cTimeWall, "ctime")
}
func (se *SafeEntry) GetMTimeWall() (WallTime, error) {
	return readField[WallTime](se, se.layout.mTimeWall, "mtime")
}

// GetDev/GetIno honour the layout's field width: legacy (v2/v3) Dev/Ino are
// 32-bit on disk, so they are read as uint32 and widened to the 64-bit DevID/
// Inode; the current (v4) layout reads the full 64 bits. Reading the wide type
// at a legacy offset would spill into the adjacent field.
func (se *SafeEntry) GetDev() (DevID, error) {
	if se.layout.narrowDevIno {
		v, err := readField[uint32](se, se.layout.dev, "dev")
		return DevID(v), err
	}
	return readField[DevID](se, se.layout.dev, "dev")
}
func (se *SafeEntry) GetIno() (Inode, error) {
	if se.layout.narrowDevIno {
		v, err := readField[uint32](se, se.layout.ino, "ino")
		return Inode(v), err
	}
	return readField[Inode](se, se.layout.ino, "ino")
}
func (se *SafeEntry) GetMode() (FileMode, error) {
	return readField[FileMode](se, se.layout.mode, "mode")
}
func (se *SafeEntry) GetUID() (UserID, error)  { return readField[UserID](se, se.layout.uid, "uid") }
func (se *SafeEntry) GetGID() (GroupID, error) { return readField[GroupID](se, se.layout.gid, "gid") }
func (se *SafeEntry) GetFileSize() (ByteSize, error) {
	return readField[ByteSize](se, se.layout.fileSize, "file_size")
}
func (se *SafeEntry) GetEntryFlags() (FlagBits, error) {
	return readField[FlagBits](se, se.layout.entryFlags, "entry_flags")
}
func (se *SafeEntry) GetHashType() (HashKind, error) {
	return readField[HashKind](se, se.layout.hashType, "hash_type")
}

// GetHash safely copies the 64-byte hash field.
func (se *SafeEntry) GetHash() ([64]byte, error) {
	if err := se.validateFieldAccess(se.layout.hash, 64, "hash"); err != nil {
		return [64]byte{}, err
	}
	var hash [64]byte
	copy(hash[:], se.data[se.offset+int(se.layout.hash):se.offset+int(se.layout.hash)+64]) //nolint:gosec // G115: field offset, bounded non-negative
	return hash, nil
}

// Safe field writers (typed wrappers over writeField).
func (se *SafeEntry) SetCTimeWall(value WallTime) error {
	return writeField(se, se.layout.cTimeWall, "ctime", value)
}
func (se *SafeEntry) SetMTimeWall(value WallTime) error {
	return writeField(se, se.layout.mTimeWall, "mtime", value)
}
func (se *SafeEntry) SetMode(value FileMode) error {
	return writeField(se, se.layout.mode, "mode", value)
}
func (se *SafeEntry) SetUID(value UserID) error { return writeField(se, se.layout.uid, "uid", value) }
func (se *SafeEntry) SetGID(value GroupID) error {
	return writeField(se, se.layout.gid, "gid", value)
}
func (se *SafeEntry) SetFileSize(value ByteSize) error {
	return writeField(se, se.layout.fileSize, "file_size", value)
}

// GetPath safely extracts the null-terminated path from the entry.
//
// The variable-length path is appended AFTER the fixed entry struct (at the
// layout's struct size), matching the authoritative writer (EntrySerialiser) and
// Entry.RelativePath. Uses the version-selected layout's minSize so a legacy
// entry's path is found after the legacy struct, not the (larger) v4 one. (The
// dcfhfix duplicate this replaces read from the unused trailing Path[8] field
// instead — a latent bug that yielded an empty path.)
// Bounded by the entry's declared end (maxOffset), never len(data).
func (se *SafeEntry) GetPath() (string, error) {
	pathStart := se.offset + int(se.layout.minSize) //nolint:gosec // G115: struct size offset, bounded non-negative
	if pathStart >= se.maxOffset {
		return "", nil // entry declares no appended path
	}
	pathData := se.data[pathStart:se.maxOffset]

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
