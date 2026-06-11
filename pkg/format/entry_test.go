package format

import (
	"testing"
	"unsafe"
)

// TestEntry_PathLength_MatchesRelativePath pins calculatePathLength to the
// canonical RelativePath offset and pins validateLayout against panicking on a
// well-formed entry. Before task 24, calculatePathLength started its scan at
// &be.Path[0] (Offsetof(Path)=132) rather than the path-data offset Sizeof=144,
// over-counting by 12 bytes; and validateLayout asserted Offsetof(Path)==Sizeof-8
// (136 != 132) so it panicked on every valid entry — swallowed by ValidateEntry's
// recover, making ValidateEntry a no-op that always returned nil.
func TestEntry_PathLength_MatchesRelativePath(t *testing.T) {
	// Mixed mod-8 path lengths so the 12-byte over-count cannot be masked by the
	// expectedSize padding arithmetic.
	for _, path := range []string{"a", "abcdefg", "some/relative/path.go"} {
		buf := layEntry(path, 1, 2)
		e := (*Entry)(unsafe.Pointer(&buf[0]))

		if got := e.RelativePath(); got != path {
			t.Fatalf("RelativePath = %q, want %q", got, path)
		}
		if got := e.calculatePathLength(); got != len(path) { // primary regression pin
			t.Errorf("calculatePathLength(%q) = %d, want %d", path, got, len(path))
		}

		// validateLayout must not panic on a valid entry (Decision 4). Pin it
		// directly rather than via ValidateEntry, whose recover() would mask a panic.
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("validateLayout panicked on valid entry %q: %v", path, r)
				}
			}()
			e.validateLayout()
		}()

		if err := e.ValidateEntry(); err != nil { // now a genuine pass, not swallowed panic
			t.Errorf("ValidateEntry(%q) = %v, want nil", path, err)
		}
	}
}

// TestEntry_ValidateEntry_RejectsCorruptSize proves ValidateEntry is live again
// (rejects corruption) rather than the pre-fix no-op that always returned nil.
func TestEntry_ValidateEntry_RejectsCorruptSize(t *testing.T) {
	// Long path so Size-8 stays > minSize and the post-fix RelativePath scan stays
	// inside the buffer. Corrupt DOWNWARD only: inflating Size would make the scan
	// read past the exactly-sized layEntry buffer (OOB heap read → checkptr fatal
	// under `go test -race`).
	buf := layEntry("some/relative/path.go", 1, 2) // Size 168
	e := (*Entry)(unsafe.Pointer(&buf[0]))
	e.Size -= 8 // 160: in-bounds, 8-aligned, > minSize, inconsistent with the path

	if err := e.ValidateEntry(); err == nil {
		t.Fatal("ValidateEntry accepted a size-corrupted entry; the size check is dead")
	}
}
