package format

import (
	"encoding/hex"
	"os"
	"testing"
	"unsafe"
)

// readV4Entries walks a v4 image's entry region and returns one SafeEntry per
// entry (version = CurrentIndexVersion). Fails the test on any decode error.
func readV4Entries(t *testing.T, image []byte) []*SafeEntry {
	t.Helper()
	h := (*Header)(unsafe.Pointer(&image[0]))
	if h.Version != CurrentIndexVersion {
		t.Fatalf("transcoded image version = %d, want %d", h.Version, CurrentIndexVersion)
	}
	var out []*SafeEntry
	offset := HeaderSizeForVersion(CurrentIndexVersion)
	for i := uint32(0); i < h.EntryCount; i++ {
		se, err := NewSafeEntry(image, int(i), offset, CurrentIndexVersion)
		if err != nil {
			t.Fatalf("entry %d: NewSafeEntry at %d: %v", i, offset, err)
		}
		out = append(out, se)
		sz, _ := se.GetSize()
		offset += int(sz)
	}
	return out
}

// TC-5: TranscodeLegacyIndex turns a v2 or v3 legacy image into a v4 image whose
// every field matches the laid legacy values, with Dev/Ino widened 32→64. The
// legacy entry layout is identical for v2 and v3 (only the header differs), so
// both versions are exercised through the same fixture body.
func TestTranscodeLegacyIndex_Positive(t *testing.T) {
	for _, version := range []uint32{2, 3} {
		t.Run(HeaderTagForVersion(version), func(t *testing.T) {
			e0 := layLegacyEntry("src/main.go", 0xAABBCCDD, 0x11223344)
			e1 := layLegacyEntry("docs/readme.md", 0x0a0b0c0d, 0xFFEEDDCC)
			legacy := layIndex(version, e0, e1)

			image, err := TranscodeLegacyIndex(legacy)
			if err != nil {
				t.Fatalf("TranscodeLegacyIndex(v%d): %v", version, err)
			}

			h := (*Header)(unsafe.Pointer(&image[0]))
			if h.Version != CurrentIndexVersion {
				t.Errorf("image version = %d, want %d", h.Version, CurrentIndexVersion)
			}
			if h.EntryCount != 2 {
				t.Errorf("image EntryCount = %d, want 2", h.EntryCount)
			}

			entries := readV4Entries(t, image)
			if len(entries) != 2 {
				t.Fatalf("decoded %d entries, want 2", len(entries))
			}
			want := []struct {
				path string
				dev  DevID
				ino  Inode
			}{
				{"src/main.go", 0xAABBCCDD, 0x11223344},
				{"docs/readme.md", 0x0a0b0c0d, 0xFFEEDDCC},
			}
			for i, se := range entries {
				if got, _ := se.GetDev(); got != want[i].dev {
					t.Errorf("entry %d Dev = %#x, want %#x (widened, high bits zero)", i, got, want[i].dev)
				}
				if got, _ := se.GetIno(); got != want[i].ino {
					t.Errorf("entry %d Ino = %#x, want %#x", i, got, want[i].ino)
				}
				if got, err := se.GetPath(); err != nil || got != want[i].path {
					t.Errorf("entry %d Path = %q, %v; want %q", i, got, err, want[i].path)
				}
				if got, _ := se.GetMode(); got != 0o100644 {
					t.Errorf("entry %d Mode = %#o, want %#o", i, got, 0o100644)
				}
				if got, _ := se.GetFileSize(); got != 0xdeadbeef {
					t.Errorf("entry %d FileSize = %#x, want 0xdeadbeef", i, got)
				}
				if got, _ := se.GetHashType(); got != HashTypeSHA1 {
					t.Errorf("entry %d HashType = %d, want %d", i, got, HashTypeSHA1)
				}
			}
		})
	}
}

// TC-5 (empty): a zero-entry legacy index transcodes to a header-only v4 image.
func TestTranscodeLegacyIndex_Empty(t *testing.T) {
	image, err := TranscodeLegacyIndex(layIndex(3))
	if err != nil {
		t.Fatalf("empty index: %v", err)
	}
	if len(image) != HeaderSize {
		t.Fatalf("empty index image = %d bytes, want header-only %d", len(image), HeaderSize)
	}
	if h := (*Header)(unsafe.Pointer(&image[0])); h.Version != CurrentIndexVersion || h.EntryCount != 0 {
		t.Errorf("empty image header: version %d, count %d; want %d, 0", h.Version, h.EntryCount, CurrentIndexVersion)
	}
}

// TC-5 (fail-closed): malformed legacy input errors cleanly with no over-read and
// no allocation sized from the untrusted EntryCount.
func TestTranscodeLegacyIndex_FailClosed(t *testing.T) {
	good := layIndex(3, layLegacyEntry("a/file.txt", 1, 2))

	t.Run("too small for header", func(t *testing.T) {
		if _, err := TranscodeLegacyIndex(make([]byte, V2HeaderSize-1)); err == nil {
			t.Fatal("expected error for sub-header buffer")
		}
	})

	t.Run("non-legacy version", func(t *testing.T) {
		img := append([]byte(nil), good...)
		(*Header)(unsafe.Pointer(&img[0])).Version = CurrentIndexVersion // v4 is not legacy
		if _, err := TranscodeLegacyIndex(img); err == nil {
			t.Fatal("expected error transcoding a current-version image")
		}
	})

	t.Run("truncated mid-entry", func(t *testing.T) {
		if _, err := TranscodeLegacyIndex(good[:len(good)-8]); err == nil {
			t.Fatal("expected error for entry region truncated mid-entry")
		}
	})

	t.Run("bogus per-entry size", func(t *testing.T) {
		img := append([]byte(nil), good...)
		*(*uint32)(unsafe.Pointer(&img[HeaderSize])) = 5 // first entry Size below legacy minimum
		if _, err := TranscodeLegacyIndex(img); err == nil {
			t.Fatal("expected error for sub-minimum entry size")
		}
	})

	t.Run("oversized EntryCount against tiny file", func(t *testing.T) {
		img := append([]byte(nil), good...)
		(*Header)(unsafe.Pointer(&img[0])).EntryCount = 0xFFFFFFFF // crafted huge count
		// Must error (region exhausted) without attempting a multi-GB allocation;
		// the test simply completing without OOM proves the incremental-grow design.
		if _, err := TranscodeLegacyIndex(img); err == nil {
			t.Fatal("expected error for EntryCount exceeding the entry region")
		}
	})
}

// TC-6: layoutForVersion returns a layout for every supported version and a
// non-nil error for everything else — the fail-closed boundary that stops an
// attacker-controlled version byte selecting v4 offsets over garbage.
func TestLayoutForVersion(t *testing.T) {
	for _, v := range []uint32{2, 3, CurrentIndexVersion} {
		if _, err := layoutForVersion(v); err != nil {
			t.Errorf("layoutForVersion(%d) unexpected error: %v", v, err)
		}
	}
	// v2/v3 are narrow (32-bit Dev/Ino); v4 is wide.
	if l2, _ := layoutForVersion(2); !l2.narrowDevIno {
		t.Error("v2 layout should be narrowDevIno")
	}
	if l4, _ := layoutForVersion(CurrentIndexVersion); l4.narrowDevIno {
		t.Error("v4 layout should not be narrowDevIno")
	}
	for _, v := range []uint32{0, 1, CurrentIndexVersion + 1, 0xFFFFFFFF} {
		if _, err := layoutForVersion(v); err == nil {
			t.Errorf("layoutForVersion(%d): expected error, got nil", v)
		}
	}
}

// HeaderTagForVersion is a tiny subtest-name helper ("v2"/"v3"/...).
func HeaderTagForVersion(v uint32) string {
	return "v" + string(rune('0'+v))
}

// --- Golden fixtures (committed under testdata/) -----------------------------

// TC (golden): testdata/v3.idx is a genuine v3 index from the real v3 writer
// (captured at baseline before the widen). It must decode through the transcode
// path to a v4 image whose CONTENT-derived stable fields (path/mode/filesize/
// hash) match what dcfh wrote. Dev/Ino are NOT asserted (machine-specific), and
// being uint32-truncated on disk they prove decode-compat only, not the >2^32
// fix (that is TC-8's in-memory fixture).
func TestGolden_V3_DecodesToV4(t *testing.T) {
	raw, err := os.ReadFile("testdata/v3.idx")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if h := (*Header)(unsafe.Pointer(&raw[0])); h.Version != 3 {
		t.Fatalf("golden v3.idx version = %d, want 3 (regenerate from a v3 writer)", h.Version)
	}

	image, err := TranscodeLegacyIndex(raw)
	if err != nil {
		t.Fatalf("transcode golden v3: %v", err)
	}
	entries := readV4Entries(t, image)

	type want struct {
		path, hash string
		size       ByteSize
	}
	// Hashes are dcfh's content-derived SHA-1 for the file bodies, read back from
	// the committed golden (the oracle). They are deterministic (content-only), so
	// asserting them proves the decode reads the Hash field correctly and that the
	// golden is genuine v3 output.
	wants := []want{
		{"file1.txt", "b6a98d9ce9a2d9149288fa3df42d377c3e42737a", 6},     // "alpha\n"
		{"file2.txt", "f2c82decdd7181cf98945929a62598db7e6b477e", 5},     // "beta\n"
		{"sub/file3.txt", "ae9a6306a205417afddd14316cc1d0d5e04a98f1", 6}, // "gamma\n"
	}
	if len(entries) != len(wants) {
		t.Fatalf("golden decoded %d entries, want %d", len(entries), len(wants))
	}
	for i, w := range wants {
		se := entries[i]
		if got, err := se.GetPath(); err != nil || got != w.path {
			t.Errorf("entry %d Path = %q, %v; want %q", i, got, err, w.path)
		}
		if got, _ := se.GetFileSize(); got != w.size {
			t.Errorf("entry %d FileSize = %d, want %d", i, got, w.size)
		}
		if got, _ := se.GetMode(); got&0o777 != 0o644 {
			t.Errorf("entry %d Mode lower bits = %#o, want 0644", i, got&0o777)
		}
		h, _ := se.GetHash()
		if got := hex.EncodeToString(h[:HashSizeSHA1]); got != w.hash {
			t.Errorf("entry %d Hash = %s, want %s", i, got, w.hash)
		}
	}
}

// TC (golden): testdata/v4.idx is the frozen v4 layout anchor. It loads natively
// (no transcode) and its first entry sits at the v4 header size with the v4
// stride — a future struct edit that shifts the v4 layout breaks this.
func TestGolden_V4_LayoutAnchor(t *testing.T) {
	raw, err := os.ReadFile("testdata/v4.idx")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	h := (*Header)(unsafe.Pointer(&raw[0]))
	if h.Version != CurrentIndexVersion {
		t.Fatalf("golden v4.idx version = %d, want %d", h.Version, CurrentIndexVersion)
	}
	// First entry at the v4 header offset, sized for its path by the v4 stride.
	se, err := NewSafeEntry(raw, 0, HeaderSize, CurrentIndexVersion)
	if err != nil {
		t.Fatalf("v4 golden first entry: %v", err)
	}
	if got, err := se.GetPath(); err != nil || got != "file1.txt" {
		t.Errorf("v4 golden entry 0 Path = %q, %v; want file1.txt", got, err)
	}
	sz, _ := se.GetSize()
	if want := RecordSize(BESizeFromPathLen(len("file1.txt"))); sz != want {
		t.Errorf("v4 golden entry 0 Size = %d, want %d (v4 stride drift?)", sz, want)
	}
}
