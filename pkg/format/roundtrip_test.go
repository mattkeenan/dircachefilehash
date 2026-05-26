package format

import (
	"bytes"
	"testing"
	"unsafe"
)

// layEntry writes a fully-populated Entry (fixed struct + variable path) into a
// fresh buffer of exactly the entry's on-disk size, mirroring what the
// production EntrySerialiser lays down. Returns the bytes.
func layEntry(path string, dev DevID, ino Inode) []byte {
	size := BESizeFromPathLen(len(path))
	buf := make([]byte, size)
	e := (*Entry)(unsafe.Pointer(&buf[0]))
	e.Size = RecordSize(size)
	e.CTimeWall = 0x0000000111111111
	e.MTimeWall = 0x0000000222222222
	e.Dev = dev
	e.Ino = ino
	e.Mode = 0o100644
	e.UID = 1000
	e.GID = 1000
	e.FileSize = 0xdeadbeef
	e.HashType = HashTypeSHA1
	for i := range HashSizeSHA1 {
		e.Hash[i] = byte(i + 1)
	}
	copy(buf[MinEntrySize():], path) // variable path follows the fixed struct
	return buf
}

// layLegacyEntry writes a pre-v4 (v2/v3) entry — 32-bit Dev/Ino — into a fresh
// buffer of exactly the legacy on-disk size, via the frozen entryV2 layout. It
// is the read-old test seam: v2/v3 fixtures are byte-accurate legacy images
// rather than v4 bytes mislabelled as legacy. Dev/Ino are uint32 here (the
// legacy width) so the widening on decode is observable.
func layLegacyEntry(path string, dev, ino uint32) []byte {
	structSize := int(unsafe.Sizeof(entryV2{}))
	size := structSize + len(path) + 1
	size += (8 - size%8) % 8 // 8-byte align, matching BESizeFromPathLen
	buf := make([]byte, size)
	e := (*entryV2)(unsafe.Pointer(&buf[0]))
	e.Size = RecordSize(size)
	e.CTimeWall = 0x0000000111111111
	e.MTimeWall = 0x0000000222222222
	e.Dev = dev
	e.Ino = ino
	e.Mode = 0o100644
	e.UID = 1000
	e.GID = 1000
	e.FileSize = 0xdeadbeef
	e.HashType = HashTypeSHA1
	for i := range HashSizeSHA1 {
		e.Hash[i] = byte(i + 1)
	}
	copy(buf[structSize:], path) // variable path follows the legacy fixed struct
	return buf
}

// layIndex builds an index image: a header of the given version followed by the
// supplied entry byte blobs. The header struct is always HeaderSize bytes; for
// v2 only the first V2HeaderSize bytes are retained so entries begin at offset
// 88 (the real v2 on-disk position), exactly as the loader expects.
func layIndex(version uint32, entries ...[]byte) []byte {
	hbuf := make([]byte, HeaderSize)
	h := (*Header)(unsafe.Pointer(&hbuf[0]))
	h.SetHeader([4]byte{'d', 'c', 'f', 'h'}, version, uint32(len(entries)), IndexFlagClean, HashTypeSHA1)

	hdrSize := HeaderSizeForVersion(version)
	out := append([]byte(nil), hbuf[:hdrSize]...)
	for _, e := range entries {
		out = append(out, e...)
	}
	return out
}

// TC-1: a v4 (current) index round-trips through the codec without byte drift.
// Every getter returns the laid value (read offsets match the canonical Entry
// layout), and re-writing each value through its setter leaves the buffer
// byte-for-byte identical (setters write exactly where getters read — no offset
// drift, no neighbouring-field corruption). Byte-identity round-trip is a
// current-version-only property: we only ever write CurrentIndexVersion.
func TestRoundTrip_V4_ByteIdentical(t *testing.T) {
	e0 := layEntry("src/main.go", 0x11223344, 0x55667788)
	e1 := layEntry("docs/readme.md", 0x0a0b0c0d, 0x0e0f1011)
	orig := layIndex(CurrentIndexVersion, e0, e1)

	// Header parses at offset 0.
	h := (*Header)(unsafe.Pointer(&orig[0]))
	if h.Version != CurrentIndexVersion {
		t.Fatalf("header Version = %d, want %d", h.Version, CurrentIndexVersion)
	}
	if h.EntryCount != 2 {
		t.Fatalf("header EntryCount = %d, want 2", h.EntryCount)
	}

	work := append([]byte(nil), orig...)

	offset := HeaderSizeForVersion(CurrentIndexVersion)
	for i, want := range []struct {
		path string
		dev  DevID
		ino  Inode
	}{
		{"src/main.go", 0x11223344, 0x55667788},
		{"docs/readme.md", 0x0a0b0c0d, 0x0e0f1011},
	} {
		se, err := NewSafeEntry(work, i, offset, CurrentIndexVersion)
		if err != nil {
			t.Fatalf("entry %d: NewSafeEntry at offset %d: %v", i, offset, err)
		}

		if got, _ := se.GetDev(); got != want.dev {
			t.Errorf("entry %d: GetDev = %#x, want %#x", i, got, want.dev)
		}
		if got, _ := se.GetIno(); got != want.ino {
			t.Errorf("entry %d: GetIno = %#x, want %#x", i, got, want.ino)
		}
		if got, err := se.GetPath(); err != nil || got != want.path {
			t.Errorf("entry %d: GetPath = %q, %v; want %q", i, got, err, want.path)
		}

		// Idempotent setter check: write every value back unchanged via the
		// codec setters; the buffer must not change a single byte.
		ct, _ := se.GetCTimeWall()
		mt, _ := se.GetMTimeWall()
		mode, _ := se.GetMode()
		uid, _ := se.GetUID()
		gid, _ := se.GetGID()
		fsz, _ := se.GetFileSize()
		mustSet(t, se.SetCTimeWall(ct))
		mustSet(t, se.SetMTimeWall(mt))
		mustSet(t, se.SetMode(mode))
		mustSet(t, se.SetUID(uid))
		mustSet(t, se.SetGID(gid))
		mustSet(t, se.SetFileSize(fsz))

		sz, _ := se.GetSize()
		offset += int(sz)
	}

	if !bytes.Equal(work, orig) {
		t.Fatal("buffer changed after idempotent setter round-trip; a setter writes at a different offset than the matching getter reads")
	}
}

// Parse-offset / header-size check (NOT a round-trip — v2 is never written).
// Legacy (v2) entry data sits at V2HeaderSize (88), v3+ at HeaderSize (104).
// Reading a v2 index with the legacy layout at offset 88 decodes; at the stray
// "96" it must NOT silently reproduce the entry — the version-aware header size
// and field layout are load-bearing. Uses genuine legacy bytes (layLegacyEntry),
// since after the v4 widen a v4-laid entry is not a valid v2 image.
func TestParseOffset_V2HeaderSize(t *testing.T) {
	if HeaderSizeForVersion(2) != V2HeaderSize || V2HeaderSize != 88 {
		t.Fatalf("HeaderSizeForVersion(2) = %d, want 88", HeaderSizeForVersion(2))
	}
	if HeaderSizeForVersion(3) != HeaderSize || HeaderSize != 104 {
		t.Fatalf("HeaderSizeForVersion(3) = %d, want 104", HeaderSizeForVersion(3))
	}

	entry := layLegacyEntry("v2/file.txt", 0xAABBCCDD, 0x99887766)
	idx := layIndex(2, entry)

	// Correct, version-aware offset + legacy layout: decodes cleanly.
	se, err := NewSafeEntry(idx, 0, V2HeaderSize, 2)
	if err != nil {
		t.Fatalf("NewSafeEntry at V2HeaderSize(88): %v", err)
	}
	if got, _ := se.GetDev(); got != 0xAABBCCDD {
		t.Errorf("v2 GetDev = %#x, want 0xAABBCCDD", got)
	}
	if got, err := se.GetPath(); err != nil || got != "v2/file.txt" {
		t.Errorf("v2 GetPath = %q, %v; want \"v2/file.txt\"", got, err)
	}

	// Wrong offset 96: must not silently decode the same entry. (Here it reads
	// into the entry's CTimeWall bytes, yielding a bogus Size that tier-1
	// rejects.)
	if _, err := NewSafeEntry(idx, 0, 96, 2); err == nil {
		t.Fatal("reading v2 entry at offset 96 unexpectedly succeeded; entry offset must be version-aware (88)")
	}
}

// TC-3 (format layer): the Header struct is exactly HeaderSize bytes, so
// dcfhfix's (*[HeaderSize]byte)(unsafe.Pointer(header)) write cast reads the
// whole struct and nothing more — the prior 8-byte over-read (from a 96-byte
// duplicate) cannot recur. Header fields also round-trip, Timestamp included.
func TestRoundTrip_HeaderSizeInvariant(t *testing.T) {
	if got := int(unsafe.Sizeof(Header{})); got != HeaderSize {
		t.Fatalf("unsafe.Sizeof(Header{}) = %d, want HeaderSize=%d (write-cast would over/under-read)", got, HeaderSize)
	}

	buf := make([]byte, HeaderSize)
	h := (*Header)(unsafe.Pointer(&buf[0]))
	h.SetHeader([4]byte{'d', 'c', 'f', 'h'}, CurrentIndexVersion, 7, IndexFlagClean, HashTypeSHA1)
	h.Timestamp = 0x0123456789ABCDEF // distinctive; survives the full-struct write cast

	// Re-read the same bytes as a fresh Header (what a reader/loader does).
	r := (*Header)(unsafe.Pointer(&buf[0]))
	if err := r.ValidateSignature([4]byte{'d', 'c', 'f', 'h'}); err != nil {
		t.Errorf("ValidateSignature: %v", err)
	}
	if err := r.ValidateByteOrder(); err != nil {
		t.Errorf("ValidateByteOrder: %v", err)
	}
	if r.Version != CurrentIndexVersion {
		t.Errorf("Version = %d, want %d", r.Version, CurrentIndexVersion)
	}
	if r.EntryCount != 7 {
		t.Errorf("EntryCount = %d, want 7", r.EntryCount)
	}
	if !r.IsClean() {
		t.Error("IsClean = false, want true")
	}
	if r.Timestamp != 0x0123456789ABCDEF {
		t.Errorf("Timestamp = %#x, want 0x0123456789ABCDEF", r.Timestamp)
	}
}

func mustSet(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("setter returned error: %v", err)
	}
}
