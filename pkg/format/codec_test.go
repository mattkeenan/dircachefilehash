package format

import (
	"testing"
	"unsafe"
)

// buildValidEntry lays a valid Entry (with the given path) into a fresh buffer
// of exactly the entry's size and returns the bytes.
func buildValidEntry(path string) []byte {
	size := BESizeFromPathLen(len(path))
	buf := make([]byte, size)
	e := (*Entry)(unsafe.Pointer(&buf[0]))
	e.Size = RecordSize(size)
	e.HashType = HashTypeSHA1
	e.Dev = 0x11223344
	e.Ino = 0x55667788
	e.Mode = 0o100644
	e.EntryFlags = EntryFlagHashed
	e.Hash[0] = 0xAB
	e.Hash[HashSizeSHA1-1] = 0xCD
	copy(buf[MinEntrySize():], path) // variable-length path follows the fixed struct
	return buf
}

func setSize(buf []byte, n uint32) {
	*(*uint32)(unsafe.Pointer(&buf[0])) = n
}

func TestSafeEntry_HappyPath(t *testing.T) {
	buf := buildValidEntry("foo/bar.go")
	se, err := NewSafeEntry(buf, 0, 0, CurrentIndexVersion)
	if err != nil {
		t.Fatalf("NewSafeEntry: unexpected error: %v", err)
	}
	if got, _ := se.GetDev(); got != 0x11223344 {
		t.Errorf("GetDev = %#x, want 0x11223344", got)
	}
	if got, _ := se.GetIno(); got != 0x55667788 {
		t.Errorf("GetIno = %#x, want 0x55667788", got)
	}
	if got, _ := se.GetHashType(); got != HashTypeSHA1 {
		t.Errorf("GetHashType = %d, want %d", got, HashTypeSHA1)
	}
	if got, _ := se.GetEntryFlags(); got != EntryFlagHashed {
		t.Errorf("GetEntryFlags = %#x, want %#x", got, EntryFlagHashed)
	}
	if got, err := se.GetHash(); err != nil || got[0] != 0xAB || got[HashSizeSHA1-1] != 0xCD {
		t.Errorf("GetHash = %x (err %v); want [0]=0xAB [%d]=0xCD", got, err, HashSizeSHA1-1)
	}
	if got, err := se.GetPath(); err != nil || got != "foo/bar.go" {
		t.Errorf("GetPath = %q, %v; want \"foo/bar.go\", nil", got, err)
	}
}

// Tier-1: entry-level Size validation in NewSafeEntry.
func TestSafeEntry_Tier1_SizeValidation(t *testing.T) {
	tests := []struct {
		name string
		size uint32 // value written into the Size field
		buf  int    // backing buffer length
	}{
		{"zero size", 0, 200},
		{"size below struct minimum", 10, 200},
		{"size exceeds buffer", 300, 200},
		{"size unreasonably large", 5000, 6000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf := make([]byte, tc.buf)
			setSize(buf, tc.size)
			if _, err := NewSafeEntry(buf, 0, 0, CurrentIndexVersion); err == nil {
				t.Fatalf("NewSafeEntry(%s): expected error, got nil", tc.name)
			}
		})
	}
}

// Tier-1: a buffer too short to even hold the Size field must error, not panic.
func TestSafeEntry_Tier1_TruncatedBuffer(t *testing.T) {
	if _, err := NewSafeEntry([]byte{0x01, 0x02}, 0, 0, CurrentIndexVersion); err == nil {
		t.Fatal("expected error for 2-byte buffer, got nil")
	}
}

// Tier-1: an out-of-range start offset (e.g. driven by a corrupt entry count
// walking past the buffer) must error, not index out of bounds.
func TestSafeEntry_Tier1_OffsetOutOfRange(t *testing.T) {
	buf := buildValidEntry("x")
	if _, err := NewSafeEntry(buf, 5, len(buf), CurrentIndexVersion); err == nil {
		t.Fatal("expected error for offset == len(buf), got nil")
	}
	if _, err := NewSafeEntry(buf, 5, len(buf)+100, CurrentIndexVersion); err == nil {
		t.Fatal("expected error for offset beyond buffer, got nil")
	}
}

// Tier-2: field/path reads are bounded by the entry's declared Size (maxOffset),
// NOT by len(buf). This is the regression the design's weaker "len(buf)" phrasing
// risked: a large buffer whose entry Size ends early must not let a read spill
// past the entry.
func TestSafeEntry_Tier2_PathBoundedBySizeNotBuffer(t *testing.T) {
	min := MinEntrySize()
	// Declare an entry of size min+8 (room for an 8-byte path slot) but back it
	// with a much larger buffer containing a long string after the struct.
	declared := min + 8
	buf := make([]byte, min+64)
	setSize(buf, uint32(declared))
	long := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" // 32 bytes, no NUL
	copy(buf[min:], long)

	se, err := NewSafeEntry(buf, 0, 0, CurrentIndexVersion)
	if err != nil {
		t.Fatalf("NewSafeEntry: %v", err)
	}
	got, err := se.GetPath()
	if err != nil {
		t.Fatalf("GetPath: %v", err)
	}
	if len(got) > declared-min {
		t.Fatalf("GetPath read %d bytes (%q); must be bounded by declared Size (%d), not the buffer",
			len(got), got, declared-min)
	}
}

// Tier-2: an entry whose declared Size is exactly the struct minimum has no
// appended path. GetPath must return "" bounded by Size — and must NOT leak the
// trailing bytes that exist in the larger backing buffer.
func TestSafeEntry_Tier2_NoPathBoundedBySize(t *testing.T) {
	min := MinEntrySize()
	buf := make([]byte, min+32)
	setSize(buf, uint32(min))
	copy(buf[min:], "LEAKLEAKLEAKLEAK") // bytes beyond the declared entry
	se, err := NewSafeEntry(buf, 0, 0, CurrentIndexVersion)
	if err != nil {
		t.Fatalf("NewSafeEntry: %v", err)
	}
	got, err := se.GetPath()
	if err != nil {
		t.Fatalf("GetPath: unexpected error: %v", err)
	}
	if got != "" {
		t.Fatalf("GetPath = %q; want \"\" (Size leaves no room for a path; buffer tail must not leak)", got)
	}
}
