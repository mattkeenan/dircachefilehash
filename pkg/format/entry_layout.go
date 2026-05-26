package format

import "fmt"

// entryLayout is the per-version field offset set the codec and transcoder use
// to read an entry's fields out of on-disk bytes. Offsets are derived from each
// version's struct via unsafe.Offsetof (see vN_layout.go), so a struct edit
// updates them automatically and a build-time assertion guards layout drift.
//
// One on-disk version → one layout. v2 and v3 share an identical entry layout
// (they differ only in the header), but each version keeps its own struct and
// layout var so a future version can diverge in its own file without disturbing
// the others.
type entryLayout struct {
	size       uintptr
	cTimeWall  uintptr
	mTimeWall  uintptr
	dev        uintptr
	ino        uintptr
	mode       uintptr
	uid        uintptr
	gid        uintptr
	fileSize   uintptr
	entryFlags uintptr
	hashType   uintptr
	hash       uintptr
	minSize    uintptr // fixed-portion (struct) size for this layout
	// narrowDevIno marks a layout whose Dev/Ino are 32-bit (legacy v2/v3). The
	// codec must read them as uint32 and widen, not as the 64-bit DevID/Inode —
	// otherwise an 8-byte read at the legacy Dev offset spills into Ino.
	narrowDevIno bool
}

// layoutForVersion selects the entry layout for a (validated) header version.
// It fails closed: a version outside the supported range returns an error rather
// than defaulting to the current (v4) offsets, so an attacker-controlled version
// byte can never drive a v4-shaped read over legacy or garbage bytes. One arm
// per on-disk version — adding a future version is a localised edit here plus a
// new vN_layout.go.
func layoutForVersion(version uint32) (entryLayout, error) {
	switch version {
	case 2:
		return layoutV2, nil
	case 3:
		return layoutV3, nil
	case CurrentIndexVersion: // v4
		return layoutV4, nil
	default:
		return entryLayout{}, fmt.Errorf("no entry layout for index version %d (supported %d-%d)",
			version, MinIndexVersion, CurrentIndexVersion)
	}
}
