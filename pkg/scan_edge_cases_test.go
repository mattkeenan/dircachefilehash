package dircachefilehash

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// freshFind opens a brand-new MetaStore over the same dir (bypassing any
// memoised index on ms) and returns the entry for rel from the on-disk main
// index, or nil if absent. rel is kept general (all current edge-case tests
// happen to target "z.txt") so future cases can look up other paths.
func freshFind(t *testing.T, ms *MetaStore, rel string) *binaryEntry { //nolint:unparam // rel kept general for future edge-case tests
	t.Helper()
	fresh := NewMetaStore(ms.RootDir, ms.RootDir)
	sl, err := fresh.LoadMainIndex()
	if err != nil {
		t.Fatalf("reload main index: %v", err)
	}
	entry, _ := sl.Find(rel)
	return entry
}

// --- FR5: concurrent file churn between discovery and hash ----------------

// TC-7 — Delete-before-hash tolerated (FR5). A new file is removed by the
// pre-hash hook; the run must still succeed and produce a clean index with the
// affected entry carrying an empty hash (the read failed, non-fatally).
func TestScanEdge_DeleteBeforeHash_Tolerated(t *testing.T) {
	ms := seedMainRepo(t)
	writeFile(t, ms.RootDir, "z.txt", "zulu\n") // new path → always hashed
	zAbs := filepath.Join(ms.RootDir, "z.txt")

	withHashPreReadHook(t, func(relPath string) {
		if relPath == "z.txt" {
			_ = os.Remove(zAbs)
		}
	})

	if err := runUpdate(context.Background(), ms, ms.scanRun(), map[string]string{}); err != nil {
		t.Fatalf("delete-before-hash must complete with success, got: %v", err)
	}
	assertLoadsClean(t, ms, ms.IndexFile)
	entry := freshFind(t, ms, "z.txt")
	if entry == nil {
		t.Fatalf("z.txt entry expected present (per-entry tolerance), got absent")
	}
	if !entry.IsHashEmpty() {
		t.Errorf("z.txt deleted before hash must have an empty hash, got %s", entry.HashString())
	}
}

// TC-8 — Modify-before-hash tolerated (FR5). The pre-hash hook rewrites the new
// file's contents; the re-read succeeds, so the entry carries a coherent
// (non-empty) hash of whatever bytes were read. The FR5 acceptance is "success
// exit + index validates clean"; we additionally assert the entry is coherent
// (present, non-corrupt) rather than the e-testing-plan's stricter "hash empty"
// wording, which only holds when the read itself fails (the delete case).
func TestScanEdge_ModifyBeforeHash_Tolerated(t *testing.T) {
	ms := seedMainRepo(t)
	writeFile(t, ms.RootDir, "z.txt", "zulu\n")
	zAbs := filepath.Join(ms.RootDir, "z.txt")

	withHashPreReadHook(t, func(relPath string) {
		if relPath == "z.txt" {
			_ = os.WriteFile(zAbs, []byte("ZULU-REWRITTEN-LONGER\n"), 0o644) //nolint:gosec // G306: test temp file
		}
	})

	if err := runUpdate(context.Background(), ms, ms.scanRun(), map[string]string{}); err != nil {
		t.Fatalf("modify-before-hash must complete with success, got: %v", err)
	}
	assertLoadsClean(t, ms, ms.IndexFile)
	entry := freshFind(t, ms, "z.txt")
	if entry == nil {
		t.Fatalf("z.txt entry expected present after modify-before-hash, got absent")
	}
	if entry.IsHashEmpty() {
		t.Errorf("z.txt re-read should yield a coherent (non-empty) hash")
	}
}

// TC-10 — Grow-before-hash tolerated (FR5). The pre-hash hook rewrites the new
// file with strictly longer contents between the scan-time stat and the read.
// The read over the grown bytes succeeds, so the entry carries a coherent
// (non-empty) hash. We assert coherence (success + clean load + present entry +
// non-empty hash), NOT that the recorded size equals the grown size: the
// stamped scan-time size and the hashed bytes legitimately diverge, so a
// size-equality assertion would be brittle (the TC-8 philosophy).
func TestScanEdge_GrowBeforeHash_Tolerated(t *testing.T) {
	ms := seedMainRepo(t)
	writeFile(t, ms.RootDir, "z.txt", "zulu\n") // new path → always hashed
	zAbs := filepath.Join(ms.RootDir, "z.txt")

	withHashPreReadHook(t, func(relPath string) {
		if relPath == "z.txt" {
			_ = os.WriteFile(zAbs, []byte("ZULU-GROWN-MUCH-LONGER-THAN-BEFORE\n"), 0o644) //nolint:gosec // G306: test temp file
		}
	})

	if err := runUpdate(context.Background(), ms, ms.scanRun(), map[string]string{}); err != nil {
		t.Fatalf("grow-before-hash must complete with success, got: %v", err)
	}
	assertLoadsClean(t, ms, ms.IndexFile)
	entry := freshFind(t, ms, "z.txt")
	if entry == nil {
		t.Fatalf("z.txt entry expected present after grow-before-hash, got absent")
	}
	if entry.IsHashEmpty() {
		t.Errorf("z.txt read over grown bytes should yield a coherent (non-empty) hash")
	}
}

// TC-11 — Shrink-before-hash tolerated (FR5). Mirror of TC-10, but the hook
// rewrites z.txt with strictly shorter (still non-empty) contents. The read of
// the shorter file still succeeds, so the same coherence-only oracle applies.
func TestScanEdge_ShrinkBeforeHash_Tolerated(t *testing.T) {
	ms := seedMainRepo(t)
	writeFile(t, ms.RootDir, "z.txt", "zulu-original-longer\n")
	zAbs := filepath.Join(ms.RootDir, "z.txt")

	withHashPreReadHook(t, func(relPath string) {
		if relPath == "z.txt" {
			_ = os.WriteFile(zAbs, []byte("z\n"), 0o644) //nolint:gosec // G306: test temp file
		}
	})

	if err := runUpdate(context.Background(), ms, ms.scanRun(), map[string]string{}); err != nil {
		t.Fatalf("shrink-before-hash must complete with success, got: %v", err)
	}
	assertLoadsClean(t, ms, ms.IndexFile)
	entry := freshFind(t, ms, "z.txt")
	if entry == nil {
		t.Fatalf("z.txt entry expected present after shrink-before-hash, got absent")
	}
	if entry.IsHashEmpty() {
		t.Errorf("z.txt read over shrunk bytes should yield a coherent (non-empty) hash")
	}
}

// TC-12 — File-replaced-by-directory-before-hash tolerated (FR5). The pre-hash
// hook removes z.txt and recreates it as a directory. entry.Mode() still
// reports the scan-time regular file, so the entry stays on the non-symlink
// HashOne branch; the read of a directory fails (EISDIR) and is swallowed,
// mirroring TC-7's delete tolerance (empty hash, run still succeeds). The
// Remove+Mkdir pair is safe because z.txt is a single new entry hashed by
// exactly one worker, so the hook fires once; it would be non-idempotent under
// concurrent hook invocation if generalised to many files.
func TestScanEdge_FileToDirBeforeHash_Tolerated(t *testing.T) {
	ms := seedMainRepo(t)
	writeFile(t, ms.RootDir, "z.txt", "zulu\n")
	zAbs := filepath.Join(ms.RootDir, "z.txt")

	withHashPreReadHook(t, func(relPath string) {
		if relPath == "z.txt" {
			_ = os.Remove(zAbs)
			_ = os.Mkdir(zAbs, 0o755) //nolint:gosec // G301: test temp dir
		}
	})

	if err := runUpdate(context.Background(), ms, ms.scanRun(), map[string]string{}); err != nil {
		t.Fatalf("file-to-dir before hash must complete with success, got: %v", err)
	}
	assertLoadsClean(t, ms, ms.IndexFile)
	entry := freshFind(t, ms, "z.txt")
	if entry == nil {
		t.Fatalf("z.txt entry expected present (per-entry tolerance), got absent")
	}
	if !entry.IsHashEmpty() {
		t.Errorf("z.txt replaced by directory must have an empty hash, got %s", entry.HashString())
	}
}

// --- FR6: mid-scan interrupt promotes no partial index --------------------

// TC-9 — Mid-scan cancel does NOT promote a partial index (FR6 fix). The
// pre-hash hook cancels the context on its first invocation; the cancelled-
// context guard in performPipelineScan must take the !ok branch so main.idx is
// preserved and no temp is promoted.
func TestScanEdge_MidScanCancel_NoPartialPromotion(t *testing.T) {
	ms := seedMainRepo(t)
	prior := readBytes(t, ms.IndexFile)

	// Several new files so the pipeline has an unprocessed tail at cancel time.
	for _, name := range []string{"c.txt", "d.txt", "e.txt", "f.txt", "g.txt"} {
		writeFile(t, ms.RootDir, name, "payload-"+name+"\n")
	}

	// The hook runs on multiple hash-worker goroutines, so guard the one-shot
	// cancel with sync.Once (a plain bool would be a data race under -race).
	ctx, cancel := context.WithCancel(context.Background())
	var once sync.Once
	withHashPreReadHook(t, func(_ string) {
		once.Do(cancel)
	})

	err := runUpdate(ctx, ms, ms.scanRun(), map[string]string{})
	if err == nil {
		t.Fatalf("mid-scan cancel must surface a non-nil error (no silent partial promotion)")
	}
	if got := readBytes(t, ms.IndexFile); string(got) != string(prior) {
		t.Errorf("cancelled update must NOT promote a partial index over main.idx")
	}
	if temps := mainTempFiles(t, ms); len(temps) != 0 {
		t.Errorf("cancelled update must remove its temp (!ok branch), found: %v", temps)
	}
}
