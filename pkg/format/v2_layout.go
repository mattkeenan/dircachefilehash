package format

import "unsafe"

// entryV2 is the on-disk entry layout for index format v2 (Dev/Ino are 32-bit).
// v2 is a frozen historical format: it is read for backward compatibility and
// never written. Its entry layout is byte-identical to v3 — the two versions
// differ only in the header (v2 omits the Timestamp tail; see header.go).
type entryV2 struct {
	Size       RecordSize // MUST BE FIRST (matches Entry)
	CTimeWall  WallTime
	MTimeWall  WallTime
	Dev        uint32 // legacy width (v4 widens to DevID=uint64)
	Ino        uint32 // legacy width (v4 widens to Inode=uint64)
	Mode       FileMode
	UID        UserID
	GID        GroupID
	FileSize   ByteSize
	EntryFlags FlagBits
	HashType   HashKind
	Hash       [64]byte
	Path       [8]byte
}

// Build-time assertions: v2 entry stays 8-byte aligned with an 8-byte Path field.
var (
	_ = [1]struct{}{}[unsafe.Sizeof(entryV2{})%8]
	_ = [1]struct{}{}[unsafe.Sizeof(entryV2{}.Path)-8]
)

// layoutV2 is the field offset set for v2 entries.
var layoutV2 = entryLayout{
	size:         0, // Size is the first field
	cTimeWall:    unsafe.Offsetof((*entryV2)(nil).CTimeWall),
	mTimeWall:    unsafe.Offsetof((*entryV2)(nil).MTimeWall),
	dev:          unsafe.Offsetof((*entryV2)(nil).Dev),
	ino:          unsafe.Offsetof((*entryV2)(nil).Ino),
	mode:         unsafe.Offsetof((*entryV2)(nil).Mode),
	uid:          unsafe.Offsetof((*entryV2)(nil).UID),
	gid:          unsafe.Offsetof((*entryV2)(nil).GID),
	fileSize:     unsafe.Offsetof((*entryV2)(nil).FileSize),
	entryFlags:   unsafe.Offsetof((*entryV2)(nil).EntryFlags),
	hashType:     unsafe.Offsetof((*entryV2)(nil).HashType),
	hash:         unsafe.Offsetof((*entryV2)(nil).Hash),
	minSize:      unsafe.Sizeof(entryV2{}),
	narrowDevIno: true, // v2 Dev/Ino are 32-bit
}
