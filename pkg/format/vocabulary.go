// Package format is the single source of truth for the dcfh on-disk index
// format: the entry/header layout, the type vocabulary that fixes each field's
// width and signedness, the layout constants, version handling, and the
// bounds-checked codec used to read fields from untrusted on-disk bytes.
//
// This package owns the format so that a width or layout change is a
// single-sited edit rather than a cross-codebase sweep. It deliberately depends
// on nothing in the core package (a cycle would defeat the purpose): everything
// here is foundational layout, host-order and zero-copy by design.
package format

// Type vocabulary: one named alias per on-disk concept. Width and signedness of
// each concept live here and nowhere else. These are Go *aliases* (`=`), so they
// are interchangeable with their underlying types — switching a field or
// accessor to a vocabulary type is transparent at every call site. A future
// width change (e.g. widening Dev/Ino to 64-bit) is a one-line edit here that
// the compiler propagates.
//
// NOTE (task 3.3): DevID/Inode were widened from uint32 to uint64 here — the
// single-sited edit that bumps the on-disk format to v4 (see constants.go) and
// removes the ingest truncation that made `dcfh dupes` under-report on
// large-inode filesystems. The compiler propagates the width through every
// field and accessor typed with these aliases.
type (
	DevID      = uint64 // st_dev
	Inode      = uint64 // st_ino
	FileMode   = uint32 // st_mode
	UserID     = uint32 // st_uid
	GroupID    = uint32 // st_gid
	WallTime   = uint64 // encoded wall-clock (see time_encoding.go in core)
	ByteSize   = uint64 // file size in bytes
	RecordSize = uint32 // an entry's own total on-disk size (incl. padding)
	FlagBits   = uint16 // entry/index flag bitset
	HashKind   = uint16 // hash algorithm identifier (HashTypeSHA1/256/512)
)
