package dircachefilehash

import (
	"os"
	"path/filepath"
	"testing"
)

// TestScanIgnoreStatAndFilter verifies that a non-nil dc.scanIgnore at
// the scan-walker chokepoint drops matching paths even when the
// IgnoreManager has nothing to say about them. Tests the
// statAndFilter:432 hook.
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

	// Without scanIgnore set, both files survive statAndFilter.
	if _, _, ok := dc.statAndFilter(keep); !ok {
		t.Errorf("keep should pass with nil scanIgnore")
	}
	if _, _, ok := dc.statAndFilter(drop); !ok {
		t.Errorf("drop should pass with nil scanIgnore")
	}

	// Wire up an --ignore predicate matching *.tmp.
	expr, err := BuildScanIgnore([]FilterOptions{{Names: []string{"*.tmp"}}})
	if err != nil {
		t.Fatalf("BuildScanIgnore: %v", err)
	}
	dc.scanIgnore = expr

	if _, _, ok := dc.statAndFilter(keep); !ok {
		t.Errorf("keep should still pass with scanIgnore = *.tmp")
	}
	if _, _, ok := dc.statAndFilter(drop); ok {
		t.Errorf("drop should be filtered by scanIgnore = *.tmp")
	}
}

// TestScanIgnoreShouldIndex covers the second chokepoint (legacy
// callback pipeline). shouldIndex receives only a relative path so the
// adapter has no FileInfo; path-based predicates still fire.
func TestScanIgnoreShouldIndex(t *testing.T) {
	tempDir := t.TempDir()
	dc := NewDirectoryCache(tempDir, tempDir)
	defer func() { _ = dc.Close() }()

	// Without scanIgnore, shouldIndex returns true for an unfiltered path.
	if !dc.shouldIndex("foo.tmp") {
		t.Fatal("baseline: shouldIndex should accept foo.tmp before scanIgnore is set")
	}

	expr, err := BuildScanIgnore([]FilterOptions{{Names: []string{"*.tmp"}}})
	if err != nil {
		t.Fatalf("BuildScanIgnore: %v", err)
	}
	dc.scanIgnore = expr

	if dc.shouldIndex("foo.tmp") {
		t.Errorf("shouldIndex should drop foo.tmp under --ignore --name *.tmp")
	}
	if !dc.shouldIndex("foo.go") {
		t.Errorf("shouldIndex should keep foo.go under --ignore --name *.tmp")
	}
}

// TestScanFilterEntryUnavailableData verifies that predicates needing
// data not available at scan-time silently no-op (return error → scan
// hook treats as "no match" → entry survives) rather than dropping
// entries on uncertainty. A --ignore --hash X with no FileInfo must
// not filter anything.
func TestScanFilterEntryUnavailableData(t *testing.T) {
	// Hash predicate at the no-info shouldIndex chokepoint: error path.
	expr, err := BuildScanIgnore([]FilterOptions{{Hashes: []string{"deadbeef"}}})
	if err != nil {
		t.Fatalf("BuildScanIgnore: %v", err)
	}
	if scanIgnoreMatches(expr, "anything", nil) {
		t.Errorf("hash predicate at scan-time must not match (data unavailable)")
	}

	// Stat predicate at the no-info chokepoint also no-ops.
	min := uint64(1024)
	expr2, err := BuildScanIgnore([]FilterOptions{{MinSize: &min}})
	if err != nil {
		t.Fatalf("BuildScanIgnore: %v", err)
	}
	if scanIgnoreMatches(expr2, "anything", nil) {
		t.Errorf("size predicate without FileInfo must not match")
	}
}
