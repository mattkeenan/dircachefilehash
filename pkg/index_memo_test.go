package dircachefilehash

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// setupMemoTestRepo creates a populated dcfh repo and closes the
// originating MetaStore, so the test can open a fresh one with an
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
	ms := NewMetaStore(testDir, testDir)
	if err := runUpdate(context.Background(), ms, ms.scanRun(), map[string]string{}); err != nil {
		_ = ms.Close()
		t.Fatalf("update: %v", err)
	}
	if err := ms.Close(); err != nil {
		t.Fatalf("close setup ms: %v", err)
	}
	return testDir
}

// TestLoadMainIndexSharesMmap verifies that the read-only mmap memo
// returns the same *mmapIndexFile across repeated LoadMainIndex calls
// when the on-disk file is unchanged.
func TestLoadMainIndexSharesMmap(t *testing.T) {
	testDir := setupMemoTestRepo(t)

	ms := NewMetaStore(testDir, testDir)
	defer func() { _ = ms.Close() }()

	sl1, err := ms.LoadMainIndex()
	if err != nil {
		t.Fatalf("load #1: %v", err)
	}
	idx1 := ms.mainIndex
	if idx1 == nil {
		t.Fatal("ms.mainIndex nil after load #1")
	}

	sl2, err := ms.LoadMainIndex()
	if err != nil {
		t.Fatalf("load #2: %v", err)
	}
	if ms.mainIndex != idx1 {
		t.Errorf("expected memo hit (same *mmapIndexFile); got idx1=%p idx2=%p", idx1, ms.mainIndex)
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

	ms := NewMetaStore(testDir, testDir)
	defer func() { _ = ms.Close() }()

	if _, err := ms.LoadMainIndex(); err != nil {
		t.Fatalf("load #1: %v", err)
	}
	idx1 := ms.mainIndex

	// Bump mtime forward; dev/inode/size unchanged but the memo's
	// cachedStat compares mtime too, so this triggers invalidation.
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(ms.IndexFile, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	if _, err := ms.LoadMainIndex(); err != nil {
		t.Fatalf("load #2: %v", err)
	}
	if ms.mainIndex == idx1 {
		t.Errorf("expected stat-mismatch invalidation; both loads returned %p", idx1)
	}

	ms.loadedMu.Lock()
	orphans := len(ms.orphanIndices)
	cached := len(ms.loadedIndices)
	ms.loadedMu.Unlock()
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
// ms.mainIndex on each call without DecRef, leaking mappings until
// process exit.
func TestLoadMainIndexNoLeak(t *testing.T) {
	testDir := setupMemoTestRepo(t)

	ms := NewMetaStore(testDir, testDir)
	defer func() { _ = ms.Close() }()

	for i := range 10 {
		if _, err := ms.LoadMainIndex(); err != nil {
			t.Fatalf("load #%d: %v", i, err)
		}
	}

	ms.loadedMu.Lock()
	cached := len(ms.loadedIndices)
	orphans := len(ms.orphanIndices)
	ms.loadedMu.Unlock()
	if cached != 1 {
		t.Errorf("expected 1 cached mapping, got %d", cached)
	}
	if orphans != 0 {
		t.Errorf("expected 0 orphans, got %d", orphans)
	}
	if ms.mainIndex == nil {
		t.Fatal("ms.mainIndex nil after 10 loads")
	}
	if rc := ms.mainIndex.RefCount(); rc != 1 {
		t.Errorf("expected refCount=1 (memo's single ref), got %d", rc)
	}
}

// TestStatusUsesSingleMainMapping is the headline correctness check:
// dcfh status's Diff(main, fs-scan) path used to load main.idx twice,
// once via OpenRef(RefTypeMain) and once via refreshFsScanCache. With
// the memo, both should hit the same *mmapIndexFile.
func TestStatusUsesSingleMainMapping(t *testing.T) {
	testDir := setupMemoTestRepo(t)

	ms := NewMetaStore(testDir, testDir)
	defer func() { _ = ms.Close() }()

	// Prime the memo so we observe the steady state.
	if _, err := ms.LoadMainIndex(); err != nil {
		t.Fatalf("prime: %v", err)
	}
	idxBefore := ms.mainIndex

	// Run Diff(main, fs-scan) — exactly the dcfh-status code path.
	if _, err := Diff(context.Background(), ms, ms.scanRun(), IndexRef{Type: RefTypeMain}, IndexRef{Type: RefTypeFsScan}, nil); err != nil {
		t.Fatalf("diff: %v", err)
	}

	// ms.mainIndex should still point at the same mapping as before:
	// both OpenRef(RefTypeMain) and refreshFsScanCache hit the memo.
	if ms.mainIndex != idxBefore {
		t.Errorf("Diff(main, fs-scan) replaced main mapping; expected memo hit on both sides")
	}

	// And there should be no orphans accumulated from the diff itself.
	ms.loadedMu.Lock()
	orphans := len(ms.orphanIndices)
	ms.loadedMu.Unlock()
	if orphans != 0 {
		t.Errorf("Diff(main, fs-scan) produced %d orphan mappings; expected 0", orphans)
	}
}
