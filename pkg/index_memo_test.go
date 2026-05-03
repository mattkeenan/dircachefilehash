package dircachefilehash

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// setupMemoTestRepo creates a populated dcfh repo and closes the
// originating DirectoryCache, so the test can open a fresh one with an
// empty memo against the on-disk state.
func setupMemoTestRepo(t *testing.T) string {
	t.Helper()
	testDir := filepath.Join(t.TempDir(), "memo")
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(testDir, name), []byte(name), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	dc := NewDirectoryCache(testDir, testDir)
	if err := dc.Update(context.Background(), dc.scanRun(), map[string]string{}); err != nil {
		_ = dc.Close()
		t.Fatalf("update: %v", err)
	}
	if err := dc.Close(); err != nil {
		t.Fatalf("close setup dc: %v", err)
	}
	return testDir
}

// TestLoadMainIndexSharesMmap verifies that the read-only mmap memo
// returns the same *mmapIndexFile across repeated LoadMainIndex calls
// when the on-disk file is unchanged.
func TestLoadMainIndexSharesMmap(t *testing.T) {
	testDir := setupMemoTestRepo(t)

	dc := NewDirectoryCache(testDir, testDir)
	defer func() { _ = dc.Close() }()

	sl1, err := dc.LoadMainIndex()
	if err != nil {
		t.Fatalf("load #1: %v", err)
	}
	idx1 := dc.mainIndex
	if idx1 == nil {
		t.Fatal("dc.mainIndex nil after load #1")
	}

	sl2, err := dc.LoadMainIndex()
	if err != nil {
		t.Fatalf("load #2: %v", err)
	}
	if dc.mainIndex != idx1 {
		t.Errorf("expected memo hit (same *mmapIndexFile); got idx1=%p idx2=%p", idx1, dc.mainIndex)
	}
	if sl1.Length() != sl2.Length() || sl1.Length() == 0 {
		t.Errorf("skiplist lengths: sl1=%d sl2=%d", sl1.Length(), sl2.Length())
	}
}

// TestLoadMainIndexInvalidatesOnMtimeChange verifies that a stat change
// on main.idx (mtime touch) causes the next LoadMainIndex to load fresh
// and orphan the previous mapping. This isolates the memo's invalidation
// path from Update's side-effects.
func TestLoadMainIndexInvalidatesOnMtimeChange(t *testing.T) {
	testDir := setupMemoTestRepo(t)

	dc := NewDirectoryCache(testDir, testDir)
	defer func() { _ = dc.Close() }()

	if _, err := dc.LoadMainIndex(); err != nil {
		t.Fatalf("load #1: %v", err)
	}
	idx1 := dc.mainIndex

	// Bump mtime forward; dev/inode/size unchanged but the memo's
	// cachedStat compares mtime too, so this triggers invalidation.
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(dc.IndexFile, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	if _, err := dc.LoadMainIndex(); err != nil {
		t.Fatalf("load #2: %v", err)
	}
	if dc.mainIndex == idx1 {
		t.Errorf("expected stat-mismatch invalidation; both loads returned %p", idx1)
	}

	dc.loadedMu.Lock()
	orphans := len(dc.orphanIndices)
	cached := len(dc.loadedIndices)
	dc.loadedMu.Unlock()
	if orphans != 1 {
		t.Errorf("expected exactly 1 orphan after invalidation, got %d", orphans)
	}
	if cached != 1 {
		t.Errorf("expected 1 cached mapping after invalidation, got %d", cached)
	}
}

// TestLoadMainIndexNoLeak verifies that repeated LoadMainIndex calls
// (no on-disk change between them) leave a single cached mapping with
// refCount=1 and no orphans. The pre-memo behaviour overwrote
// dc.mainIndex on each call without DecRef, leaking mappings until
// process exit.
func TestLoadMainIndexNoLeak(t *testing.T) {
	testDir := setupMemoTestRepo(t)

	dc := NewDirectoryCache(testDir, testDir)
	defer func() { _ = dc.Close() }()

	for i := range 10 {
		if _, err := dc.LoadMainIndex(); err != nil {
			t.Fatalf("load #%d: %v", i, err)
		}
	}

	dc.loadedMu.Lock()
	cached := len(dc.loadedIndices)
	orphans := len(dc.orphanIndices)
	dc.loadedMu.Unlock()
	if cached != 1 {
		t.Errorf("expected 1 cached mapping, got %d", cached)
	}
	if orphans != 0 {
		t.Errorf("expected 0 orphans, got %d", orphans)
	}
	if dc.mainIndex == nil {
		t.Fatal("dc.mainIndex nil after 10 loads")
	}
	if rc := dc.mainIndex.RefCount(); rc != 1 {
		t.Errorf("expected refCount=1 (memo's single ref), got %d", rc)
	}
}

// TestStatusUsesSingleMainMapping is the headline correctness check:
// dcfh status's Diff(main, fs-scan) path used to load main.idx twice,
// once via OpenRef(RefTypeMain) and once via refreshFsScanCache. With
// the memo, both should hit the same *mmapIndexFile.
func TestStatusUsesSingleMainMapping(t *testing.T) {
	testDir := setupMemoTestRepo(t)

	dc := NewDirectoryCache(testDir, testDir)
	defer func() { _ = dc.Close() }()

	// Prime the memo so we observe the steady state.
	if _, err := dc.LoadMainIndex(); err != nil {
		t.Fatalf("prime: %v", err)
	}
	idxBefore := dc.mainIndex

	// Run Diff(main, fs-scan) — exactly the dcfh-status code path.
	if _, err := Diff(context.Background(), dc, dc.scanRun(), IndexRef{Type: RefTypeMain}, IndexRef{Type: RefTypeFsScan}, nil); err != nil {
		t.Fatalf("diff: %v", err)
	}

	// dc.mainIndex should still point at the same mapping as before:
	// both OpenRef(RefTypeMain) and refreshFsScanCache hit the memo.
	if dc.mainIndex != idxBefore {
		t.Errorf("Diff(main, fs-scan) replaced main mapping; expected memo hit on both sides")
	}

	// And there should be no orphans accumulated from the diff itself.
	dc.loadedMu.Lock()
	orphans := len(dc.orphanIndices)
	dc.loadedMu.Unlock()
	if orphans != 0 {
		t.Errorf("Diff(main, fs-scan) produced %d orphan mappings; expected 0", orphans)
	}
}
