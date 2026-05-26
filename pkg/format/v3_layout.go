package format

import "unsafe"

// entryV3 is the on-disk entry layout for index format v3 (Dev/Ino are 32-bit).
// v3 is a frozen historical format: read for backward compatibility, never
// written. Its entry layout is byte-identical to v2 — v3 added only a header
// Timestamp (see header.go), not an entry-field change. The struct is kept
// separate (rather than aliased to entryV2) so v3 has its own conceptual home;
// the build assertion below pins the two layouts together so they cannot drift.
type entryV3 struct {
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

// Build-time assertions: v3 entry stays 8-byte aligned with an 8-byte Path
// field, AND is byte-identical to the v2 entry layout (same size, same Ino
// offset — the field most likely to drift). v2 and v3 are frozen formats that
// share an entry layout; if an edit ever breaks that, fail the build.
var (
	_ = [1]struct{}{}[unsafe.Sizeof(entryV3{})%8]
	_ = [1]struct{}{}[unsafe.Sizeof(entryV3{}.Path)-8]
	_ = [1]struct{}{}[unsafe.Sizeof(entryV3{})-unsafe.Sizeof(entryV2{})]
	_ = [1]struct{}{}[unsafe.Offsetof(entryV3{}.Ino)-unsafe.Offsetof(entryV2{}.Ino)]
)

// layoutV3 is the field offset set for v3 entries (identical to v2 today).
var layoutV3 = entryLayout{
	size:         0,
	cTimeWall:    unsafe.Offsetof((*entryV3)(nil).CTimeWall),
	mTimeWall:    unsafe.Offsetof((*entryV3)(nil).MTimeWall),
	dev:          unsafe.Offsetof((*entryV3)(nil).Dev),
	ino:          unsafe.Offsetof((*entryV3)(nil).Ino),
	mode:         unsafe.Offsetof((*entryV3)(nil).Mode),
	uid:          unsafe.Offsetof((*entryV3)(nil).UID),
	gid:          unsafe.Offsetof((*entryV3)(nil).GID),
	fileSize:     unsafe.Offsetof((*entryV3)(nil).FileSize),
	entryFlags:   unsafe.Offsetof((*entryV3)(nil).EntryFlags),
	hashType:     unsafe.Offsetof((*entryV3)(nil).HashType),
	hash:         unsafe.Offsetof((*entryV3)(nil).Hash),
	minSize:      unsafe.Sizeof(entryV3{}),
	narrowDevIno: true, // v3 Dev/Ino are 32-bit (identical entry layout to v2)
}
