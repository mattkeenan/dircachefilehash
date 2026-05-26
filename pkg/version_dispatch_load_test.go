package dircachefilehash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"
)

// layIndexFile writes a zero-entry index image to a temp file and returns its
// path. The header is laid in a full HeaderSize buffer (so the version-aware
// header size is honoured), then the file is padded or truncated to totalBytes.
// Fixtures are written UNCLEAN so the loaders skip checksum verification — this
// isolates the version-dispatch and header-size gates under test from the
// (separate) checksum path, mirroring the raw-byte style of pkg/format's codec
// tests. Entry count is 0: every gate under test fires before the entry walk.
func layIndexFile(t *testing.T, version uint32, totalBytes int) string {
	t.Helper()
	buf := make([]byte, HeaderSize)
	h := (*indexHeader)(unsafe.Pointer(&buf[0]))
	h.SetHeader([4]byte{'d', 'c', 'f', 'h'}, version, 0, 0, HashTypeSHA1) // entryCount=0, flags=0 → unclean

	out := buf[:HeaderSizeForVersion(version)]
	if totalBytes > len(out) {
		out = append(out, make([]byte, totalBytes-len(out))...)
	} else {
		out = out[:totalBytes]
	}

	path := filepath.Join(t.TempDir(), "test.idx")
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func validationMetaStore() *MetaStore {
	// Mirrors dcfhfind_support.go: version 0 makes ValidateVersion a no-op, so
	// the resolver is the only real version gate on this path.
	return &MetaStore{signature: [4]byte{'d', 'c', 'f', 'h'}, version: 0}
}

func trackingMetaStore() *MetaStore {
	return &MetaStore{signature: [4]byte{'d', 'c', 'f', 'h'}, version: CurrentIndexVersion}
}

// TC-2 / TC-3: an out-of-range version is rejected cleanly by BOTH entry-walk
// loaders — no panic, no over-read, no entries. The dcfhfind path (version:0) is
// the one the resolver primarily protects; the tracking path is belt-and-braces.
func TestLoad_RejectsOutOfRangeVersion(t *testing.T) {
	for _, version := range []uint32{CurrentIndexVersion + 1, 0xFFFFFFFF} {
		t.Run("dcfhfind_validation", func(t *testing.T) {
			path := layIndexFile(t, version, HeaderSize)
			refs, err := validationMetaStore().LoadIndexFromFileForValidation(path)
			if err == nil {
				t.Fatalf("version %d: expected error, got nil (refs=%d)", version, len(refs))
			}
			if refs != nil {
				t.Errorf("version %d: expected no refs on rejection, got %d", version, len(refs))
			}
			if !strings.Contains(err.Error(), "version") {
				t.Errorf("version %d: error %q should mention the version", version, err)
			}
		})
		t.Run("tracking", func(t *testing.T) {
			path := layIndexFile(t, version, HeaderSize)
			idx, err := trackingMetaStore().loadIndexFromFileWithTracking(path)
			if err == nil {
				if idx != nil {
					idx.File.DecRef()
				}
				t.Fatalf("version %d: expected error, got nil", version)
			}
		})
	}
}

// TC-4: a v3 header on an 88..103-byte file passes the V2HeaderSize size gate but
// must be rejected by the header-size guard BEFORE slicing data[104:] — a clean
// error, never a slice-bounds panic. Boundary: a full 104-byte v3 header with
// zero entries must instead load successfully (the guard is `>`, not `>=`).
func TestLoad_V3HeaderTruncation(t *testing.T) {
	for _, size := range []int{V2HeaderSize, 90, HeaderSize - 1} { // 88, 90, 103
		t.Run("dcfhfind_validation", func(t *testing.T) {
			path := layIndexFile(t, CurrentIndexVersion, size)
			_, err := validationMetaStore().LoadIndexFromFileForValidation(path)
			if err == nil {
				t.Fatalf("size %d: expected truncation error, got nil", size)
			}
			if !strings.Contains(err.Error(), "too small") {
				t.Errorf("size %d: error %q should report a too-small header", size, err)
			}
		})
		t.Run("tracking", func(t *testing.T) {
			path := layIndexFile(t, CurrentIndexVersion, size)
			idx, err := trackingMetaStore().loadIndexFromFileWithTracking(path)
			if err == nil {
				if idx != nil {
					idx.File.DecRef()
				}
				t.Fatalf("size %d: expected truncation error, got nil", size)
			}
		})
	}

	// Boundary: exactly HeaderSize with zero entries loads cleanly (empty index).
	t.Run("full_header_empty_ok", func(t *testing.T) {
		path := layIndexFile(t, CurrentIndexVersion, HeaderSize)
		refs, err := validationMetaStore().LoadIndexFromFileForValidation(path)
		if err != nil {
			t.Fatalf("full 104-byte v3 header should load: %v", err)
		}
		if len(refs) != 0 {
			t.Errorf("empty index should yield 0 refs, got %d", len(refs))
		}
	})
}
