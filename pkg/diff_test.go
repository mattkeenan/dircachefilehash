package dircachefilehash

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

// TestParseIndexRef covers the new selector strings added in Phase 1.
func TestParseIndexRef(t *testing.T) {
	cases := []struct {
		sel      string
		wantType string
		wantSnap string
	}{
		{"main", RefTypeMain, ""},
		{"cache", RefTypeCache, ""},
		{"cache+main", RefTypeCacheMain, ""},
		{"fs-scan", RefTypeFsScan, ""},
		{"snapshot:monthly", RefTypeSnapshot, "monthly"},
		{"snapshot:20260101T120000.000000000Z", RefTypeSnapshot, "20260101T120000.000000000Z"},
	}
	for _, c := range cases {
		ref, err := ParseIndexRef("/tmp", c.sel)
		if err != nil {
			t.Errorf("ParseIndexRef(%q) err: %v", c.sel, err)
			continue
		}
		if ref.Type != c.wantType {
			t.Errorf("ParseIndexRef(%q): type=%q, want %q", c.sel, ref.Type, c.wantType)
		}
		if ref.SnapshotID != c.wantSnap {
			t.Errorf("ParseIndexRef(%q): snapshotID=%q, want %q", c.sel, ref.SnapshotID, c.wantSnap)
		}
	}

	if _, err := ParseIndexRef("/tmp", "snapshot:"); err == nil {
		t.Errorf("ParseIndexRef(snapshot:) expected error")
	}
}

// TestDiff_MainVsFsScan_MatchesStatus pins the canonical case: Diff(main,
// fs-scan) must produce exactly the same StatusResult as ms.Status. Phase 1
// implements this via delegation; Phase 2 will reroute through the generic
// engine, and this test guards equivalence across that change.
func TestDiff_MainVsFsScan_MatchesStatus(t *testing.T) {
	ms := setupDiffRepo(t)
	defer func() { _ = ms.Close() }()

	// Mutate state so there's actually something to diff.
	root := ms.RootDir
	if err := os.WriteFile(filepath.Join(root, "added.txt"), []byte("new"), 0o644); err != nil {
		t.Fatalf("write added.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("modified content"), 0o644); err != nil {
		t.Fatalf("modify a.txt: %v", err)
	}
	if err := os.Remove(filepath.Join(root, "b.txt")); err != nil {
		t.Fatalf("remove b.txt: %v", err)
	}

	ctx := context.Background()

	// Reference path: ms.Status.
	wantSR, err := ms.Status(ctx, ms.scanRun(), nil, nil)
	if err != nil {
		t.Fatalf("ms.Status: %v", err)
	}

	// Generic engine path: Diff(main, fs-scan).
	gotSR, err := Diff(ctx, ms, ms.scanRun(), IndexRef{Type: RefTypeMain}, IndexRef{Type: RefTypeFsScan}, nil)
	if err != nil {
		t.Fatalf("Diff(main, fs-scan): %v", err)
	}

	assertStatusResultsEqual(t, wantSR, gotSR)
}

// TestDiff_CacheMainVsMain exercises the case-1 path of the engine (no
// fs-scan). After a Status run, cache+main carries the deltas; diffing it
// against bare main should reproduce the same changes the Status reported.
func TestDiff_CacheMainVsMain(t *testing.T) {
	ms := setupDiffRepo(t)
	defer func() { _ = ms.Close() }()

	// Mutate, then run Status to populate cache.idx.
	root := ms.RootDir
	if err := os.WriteFile(filepath.Join(root, "added.txt"), []byte("new"), 0o644); err != nil {
		t.Fatalf("write added.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("modified"), 0o644); err != nil {
		t.Fatalf("modify a.txt: %v", err)
	}

	ctx := context.Background()
	statusSR, err := ms.Status(ctx, ms.scanRun(), nil, nil)
	if err != nil {
		t.Fatalf("ms.Status: %v", err)
	}

	// Diff(cache+main, main) — left is the post-mutation view, right is the
	// pre-mutation view, so the symmetry is: status's Added → diff's Deleted,
	// status's Deleted → diff's Added, Modified is invariant.
	diffSR, err := Diff(ctx, ms, ms.scanRun(), IndexRef{Type: RefTypeCacheMain}, IndexRef{Type: RefTypeMain}, nil)
	if err != nil {
		t.Fatalf("Diff(cache+main, main): %v", err)
	}

	gotAdded := slices.Clone(diffSR.Added)
	gotDeleted := slices.Clone(diffSR.Deleted)
	slices.Sort(gotAdded)
	slices.Sort(gotDeleted)

	wantAdded := slices.Clone(statusSR.Deleted)
	wantDeleted := slices.Clone(statusSR.Added)
	slices.Sort(wantAdded)
	slices.Sort(wantDeleted)

	if !reflect.DeepEqual(gotAdded, wantAdded) {
		t.Errorf("Added: got %v, want %v (mirror of status.Deleted)", gotAdded, wantAdded)
	}
	if !reflect.DeepEqual(gotDeleted, wantDeleted) {
		t.Errorf("Deleted: got %v, want %v (mirror of status.Added)", gotDeleted, wantDeleted)
	}
	if len(diffSR.Modified) != len(statusSR.Modified) {
		t.Errorf("Modified count: got %d, want %d", len(diffSR.Modified), len(statusSR.Modified))
	}
}

func setupDiffRepo(t *testing.T) *MetaStore {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"a.txt":     "alpha",
		"b.txt":     "beta",
		"sub/c.txt": "gamma",
	}
	for rel, body := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	ms := NewMetaStore(root, filepath.Join(root, ".dcfh"))
	if err := ms.Update(context.Background(), ms.scanRun(), map[string]string{}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	return ms
}

func assertStatusResultsEqual(t *testing.T, want, got *StatusResult) {
	t.Helper()
	wantMod := slices.Clone(want.Modified)
	gotMod := slices.Clone(got.Modified)
	wantAdd := slices.Clone(want.Added)
	gotAdd := slices.Clone(got.Added)
	wantDel := slices.Clone(want.Deleted)
	gotDel := slices.Clone(got.Deleted)
	slices.Sort(wantMod)
	slices.Sort(gotMod)
	slices.Sort(wantAdd)
	slices.Sort(gotAdd)
	slices.Sort(wantDel)
	slices.Sort(gotDel)
	if !reflect.DeepEqual(wantMod, gotMod) {
		t.Errorf("Modified: got %v, want %v", gotMod, wantMod)
	}
	if !reflect.DeepEqual(wantAdd, gotAdd) {
		t.Errorf("Added: got %v, want %v", gotAdd, wantAdd)
	}
	if !reflect.DeepEqual(wantDel, gotDel) {
		t.Errorf("Deleted: got %v, want %v", gotDel, wantDel)
	}
	if want.ModifiedBytes != got.ModifiedBytes {
		t.Errorf("ModifiedBytes: got %d, want %d", got.ModifiedBytes, want.ModifiedBytes)
	}
	if want.AddedBytes != got.AddedBytes {
		t.Errorf("AddedBytes: got %d, want %d", got.AddedBytes, want.AddedBytes)
	}
	if want.DeletedBytes != got.DeletedBytes {
		t.Errorf("DeletedBytes: got %d, want %d", got.DeletedBytes, want.DeletedBytes)
	}
}
