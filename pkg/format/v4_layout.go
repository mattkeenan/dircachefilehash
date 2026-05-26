package format

import "unsafe"

// entryV4 names the v4 on-disk entry layout per the per-version convention. v4
// is the CURRENT format — the one dcfh writes — so its layout is the canonical
// Entry (entry.go), which carries the field definitions and all entry behaviour.
// This is an alias, not a copy: there is exactly one live write struct. v2/v3
// have standalone read-only structs because they are frozen; v4 does not, so it
// cannot drift from the type the writer actually serialises.
type entryV4 = Entry

// Build-time assertion: the v4 entry is exactly 8 bytes larger than the legacy
// (v2/v3) entry. Widening Dev (uint32→uint64) and Ino (uint32→uint64) adds 8
// bytes total and shifts every field after Ino by 8. If this drifts, the
// transcoder and codec offsets are out of sync — fail the build rather than
// misread on disk.
var _ = [1]struct{}{}[unsafe.Sizeof(entryV4{})-unsafe.Sizeof(entryV2{})-8]

// layoutV4 is the field offset set for v4 entries (the current Entry).
var layoutV4 = entryLayout{
	size:         0, // Size is the first field
	cTimeWall:    unsafe.Offsetof((*entryV4)(nil).CTimeWall),
	mTimeWall:    unsafe.Offsetof((*entryV4)(nil).MTimeWall),
	dev:          unsafe.Offsetof((*entryV4)(nil).Dev),
	ino:          unsafe.Offsetof((*entryV4)(nil).Ino),
	mode:         unsafe.Offsetof((*entryV4)(nil).Mode),
	uid:          unsafe.Offsetof((*entryV4)(nil).UID),
	gid:          unsafe.Offsetof((*entryV4)(nil).GID),
	fileSize:     unsafe.Offsetof((*entryV4)(nil).FileSize),
	entryFlags:   unsafe.Offsetof((*entryV4)(nil).EntryFlags),
	hashType:     unsafe.Offsetof((*entryV4)(nil).HashType),
	hash:         unsafe.Offsetof((*entryV4)(nil).Hash),
	minSize:      unsafe.Sizeof(entryV4{}),
	narrowDevIno: false, // v4 Dev/Ino are 64-bit
}
