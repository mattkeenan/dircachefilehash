package dircachefilehash

import (
	"os"
	"path/filepath"
	"testing"
)

// TestScanIgnoreStatAndFilter asserts the scan-walker chokepoint drops
// paths matching the per-call ScanIgnore even when IgnoreManager wouldn't.
func TestScanIgnoreStatAndFilter(t *testing.T) {
	tempDir := t.TempDir()

	keep := filepath.Join(tempDir, "keep.go")
	drop := filepath.Join(tempDir, "drop.tmp")
	for _, p := range []string{keep, drop} {
		if err := os.WriteFile(p, []byte("x"), 0644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	dc := NewDirectoryCache(tempDir, tempDir)
	defer func() { _ = dc.Close() }()

	// Without ScanIgnore set, both files survive statAndFilter.
	sr := dc.scanRun()
	if _, _, ok := dc.statAndFilter(sr, keep); !ok {
		t.Errorf("keep should pass with nil ScanIgnore")
	}
	if _, _, ok := dc.statAndFilter(sr, drop); !ok {
		t.Errorf("drop should pass with nil ScanIgnore")
	}

	// Wire up an --ignore predicate matching *.tmp.
	expr, err := BuildScanIgnore([]FilterOptions{{Names: []string{"*.tmp"}}})
	if err != nil {
		t.Fatalf("BuildScanIgnore: %v", err)
	}
	sr.ScanIgnore = expr

	if _, _, ok := dc.statAndFilter(sr, keep); !ok {
		t.Errorf("keep should still pass with ScanIgnore = *.tmp")
	}
	if _, _, ok := dc.statAndFilter(sr, drop); ok {
		t.Errorf("drop should be filtered by ScanIgnore = *.tmp")
	}
}

// TestScanIgnoreShouldIndex covers the legacy callback chokepoint:
// only a relPath is available, so path-based predicates fire and
// stat-using ones silently no-op.
func TestScanIgnoreShouldIndex(t *testing.T) {
	tempDir := t.TempDir()
	dc := NewDirectoryCache(tempDir, tempDir)
	defer func() { _ = dc.Close() }()

	sr := dc.scanRun()
	// Without ScanIgnore, shouldIndex returns true for an unfiltered path.
	if !dc.shouldIndex(sr, "foo.tmp") {
		t.Fatal("baseline: shouldIndex should accept foo.tmp before ScanIgnore is set")
	}

	expr, err := BuildScanIgnore([]FilterOptions{{Names: []string{"*.tmp"}}})
	if err != nil {
		t.Fatalf("BuildScanIgnore: %v", err)
	}
	sr.ScanIgnore = expr

	if dc.shouldIndex(sr, "foo.tmp") {
		t.Errorf("shouldIndex should drop foo.tmp under --ignore --name *.tmp")
	}
	if !dc.shouldIndex(sr, "foo.go") {
		t.Errorf("shouldIndex should keep foo.go under --ignore --name *.tmp")
	}
}

// TestScanFilterEntryUnavailableData asserts that predicates whose
// inputs aren't reachable at scan-time (--ignore --hash X always;
// --ignore --min-size N when info is nil) do NOT drop entries — the
// error → no-match swallow keeps the entry alive for output-time
// evaluation.
func TestScanFilterEntryUnavailableData(t *testing.T) {
	tempDir := t.TempDir()
	dc := NewDirectoryCache(tempDir, tempDir)
	defer func() { _ = dc.Close() }()

	sr := dc.scanRun()
	hashExpr, err := BuildScanIgnore([]FilterOptions{{Hashes: []string{"deadbeef"}}})
	if err != nil {
		t.Fatalf("BuildScanIgnore: %v", err)
	}
	sr.ScanIgnore = hashExpr
	if sr.scanIgnoreDrops("anything", nil, "test") {
		t.Errorf("hash predicate at scan-time must not match (data unavailable)")
	}

	min := uint64(1024)
	sizeExpr, err := BuildScanIgnore([]FilterOptions{{MinSize: &min}})
	if err != nil {
		t.Fatalf("BuildScanIgnore: %v", err)
	}
	sr.ScanIgnore = sizeExpr
	if sr.scanIgnoreDrops("anything", nil, "test") {
		t.Errorf("size predicate without FileInfo must not match")
	}
}
