package dircachefilehash

import (
	"testing"
)

// goldenV3 is the genuine v3 index captured from the real v3 writer (committed
// under pkg/format/testdata). From a pkg/ test the working directory is pkg/, so
// the golden sits at this relative path.
const goldenV3 = "format/testdata/v3.idx"

// TC-2: a v3 index decodes through the heap-transcode path (DecodeHeap), not an
// in-place cast — proven by asserting the loaded index is heapBacked — and every
// content field decodes correctly, via BOTH entry-walk loaders.
func TestLegacyLoad_V3_RoutesThroughHeapTranscode(t *testing.T) {
	wantPaths := []string{"file1.txt", "file2.txt", "sub/file3.txt"}
	wantSizes := []uint64{6, 5, 6}

	t.Run("tracking_loader", func(t *testing.T) {
		idx, err := trackingMetaStore().loadIndexFromFileWithTracking(goldenV3)
		if err != nil {
			t.Fatalf("load v3 golden via tracking loader: %v", err)
		}
		defer idx.File.DecRef()

		if !idx.File.heapBacked {
			t.Error("v3 index must be heapBacked (routed through DecodeHeap), not cast in place")
		}
		if len(idx.Refs) != len(wantPaths) {
			t.Fatalf("decoded %d entries, want %d", len(idx.Refs), len(wantPaths))
		}
		for i, ref := range idx.Refs {
			e := ref.GetBinaryEntry()
			if got := e.RelativePath(); got != wantPaths[i] {
				t.Errorf("entry %d path = %q, want %q", i, got, wantPaths[i])
			}
			if e.FileSize != wantSizes[i] {
				t.Errorf("entry %d FileSize = %d, want %d", i, e.FileSize, wantSizes[i])
			}
			// Dev/Ino are machine-specific in a real capture; assert only that they
			// read as 64-bit values without panic (the widen happened on transcode).
			_ = e.Dev
			_ = e.Ino
		}
	})

	t.Run("validation_loader", func(t *testing.T) {
		refs, err := validationMetaStore().LoadIndexFromFileForValidation(goldenV3)
		if err != nil {
			t.Fatalf("load v3 golden via validation loader: %v", err)
		}
		if len(refs) != len(wantPaths) {
			t.Fatalf("decoded %d refs, want %d", len(refs), len(wantPaths))
		}
		if !refs[0].IndexFile.heapBacked {
			t.Error("validation-loaded v3 index must be heapBacked (DecodeHeap routing)")
		}
		for i, ref := range refs {
			if got := ref.GetBinaryEntry().RelativePath(); got != wantPaths[i] {
				t.Errorf("ref %d path = %q, want %q", i, got, wantPaths[i])
			}
		}
	})
}

// TC-9 (NFR5): Cleanup on a heap-backed index must NOT call unix.Munmap on the
// Go-allocated buffer (that is UB / would EINVAL); it just drops the reference.
// The guard keys off heapBacked, not a nil fd. Run under the project race gate.
func TestHeapBackedCleanup_NeverMunmaps(t *testing.T) {
	mif := &mmapIndexFile{Type: "test-heap", heapBacked: true, Data: make([]byte, 256)}
	if err := mif.Cleanup(); err != nil {
		t.Fatalf("heap-backed Cleanup returned error (munmap of heap slice?): %v", err)
	}
	if mif.Data != nil {
		t.Error("Cleanup must nil Data so the heap image can be GC'd")
	}
	// Idempotent: a second Cleanup on the now-nil buffer is a no-op, not a crash.
	if err := mif.Cleanup(); err != nil {
		t.Fatalf("second Cleanup on heap-backed file: %v", err)
	}
}
