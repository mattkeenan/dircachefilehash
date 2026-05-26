package main

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"github.com/mattkeenan/dircachefilehash/pkg/format"
)

// goldenV3 is the genuine v3 index captured from the real v3 writer. From a
// cmd/dcfhfix test the working directory is cmd/dcfhfix, so the golden under
// pkg/format/testdata sits at this relative path.
const goldenV3 = "../../pkg/format/testdata/v3.idx"

// TC-10: dcfhfix is read-old / write-v4. createTempIndexWithHeader must stamp a
// CURRENT (v4) header even when repairing a legacy (v3) input — copying the
// source header verbatim would leave a v3 header over the v4-shaped (+8/entry)
// entries the repair appends, i.e. a corrupt, unloadable index. This asserts the
// v4 stamp directly on a genuine v3 golden.
func TestDcfhfixRepair_StampsV4Header(t *testing.T) {
	v3, err := os.ReadFile(goldenV3)
	if err != nil {
		t.Fatalf("read v3 golden: %v", err)
	}
	if (*format.Header)(unsafe.Pointer(&v3[0])).Version != 3 {
		t.Fatalf("fixture is not v3 (regenerate); got version %d",
			(*format.Header)(unsafe.Pointer(&v3[0])).Version)
	}

	tmp := filepath.Join(t.TempDir(), "repaired.idx")
	if err := createTempIndexWithHeader(v3, tmp); err != nil {
		t.Fatalf("createTempIndexWithHeader: %v", err)
	}

	out, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatalf("read repaired temp: %v", err)
	}
	if len(out) < format.HeaderSize {
		t.Fatalf("repaired header = %d bytes, want >= %d", len(out), format.HeaderSize)
	}
	h := (*format.Header)(unsafe.Pointer(&out[0]))
	if h.Version != format.CurrentIndexVersion {
		t.Errorf("repaired header Version = %d, want %d (v4 stamp; not the v3 source version)",
			h.Version, format.CurrentIndexVersion)
	}
	// Signature and checksum type are preserved from the source.
	if h.Signature != ([4]byte{'d', 'c', 'f', 'h'}) {
		t.Errorf("repaired header Signature = %q, want \"dcfh\"", string(h.Signature[:]))
	}
	if err := h.ValidateByteOrder(); err != nil {
		t.Errorf("repaired header byte order: %v", err)
	}
}
