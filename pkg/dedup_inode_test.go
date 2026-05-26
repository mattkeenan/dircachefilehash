package dircachefilehash

import "testing"

// TC-8 (the reported bug): dedupByInode keys on the FULL 64-bit (Dev, Ino). Two
// distinct files whose inodes share their low 32 bits but differ above must NOT
// be collapsed as hardlinks. The pre-fix key was [2]uint32, which truncated Ino
// and dropped one of the pair from duplicate analysis — silent under-reporting on
// large-inode filesystems. This is the direct regression proof.
func TestDedupByInode_DistinguishesHighBitInodes(t *testing.T) {
	const dev = 0x10
	// Same Dev, same low-32 (0x0000_0005), different high-32 → genuinely distinct.
	a := &binaryEntry{Dev: dev, Ino: 0x1_0000_0005}
	b := &binaryEntry{Dev: dev, Ino: 0x2_0000_0005}

	got := dedupByInode([]*binaryEntry{a, b})
	if len(got) != 2 {
		t.Fatalf("dedupByInode collapsed two >32-bit-distinct inodes to %d entries; "+
			"want 2 (the pre-fix [2]uint32 key truncated Ino and dropped one)", len(got))
	}
}

// Control: genuine hardlinks (identical Dev AND Ino) still collapse to one, so the
// widened key did not break legitimate deduplication.
func TestDedupByInode_CollapsesGenuineHardlinks(t *testing.T) {
	const dev = 0x10
	a := &binaryEntry{Dev: dev, Ino: 0x3_0000_0007}
	b := &binaryEntry{Dev: dev, Ino: 0x3_0000_0007} // identical 64-bit Dev/Ino

	got := dedupByInode([]*binaryEntry{a, b})
	if len(got) != 1 {
		t.Fatalf("dedupByInode kept %d entries for identical Dev/Ino; want 1 (hardlink collapse)", len(got))
	}
}

// Different device, same inode number must NOT collapse (Dev is part of the key).
func TestDedupByInode_DistinguishesDevices(t *testing.T) {
	a := &binaryEntry{Dev: 0x10, Ino: 0x42}
	b := &binaryEntry{Dev: 0x20, Ino: 0x42}

	if got := dedupByInode([]*binaryEntry{a, b}); len(got) != 2 {
		t.Fatalf("dedupByInode collapsed same-inode entries on different devices to %d; want 2", len(got))
	}
}
