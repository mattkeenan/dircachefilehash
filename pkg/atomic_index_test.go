package dircachefilehash

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// --- shared scaffolding for the fault-injection tests ---------------------
// (writeFile lives in treeview_enrichment_test.go)

// readBytes returns the on-disk bytes of path, failing the test on error.
func readBytes(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // G304: test-controlled temp path
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

// mainTempFiles returns the retained timestamped main-*.idx temp files.
func mainTempFiles(t *testing.T, ms *MetaStore) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(ms.MetaDir, "main-*.idx"))
	if err != nil {
		t.Fatalf("glob main temps: %v", err)
	}
	return matches
}

// cacheTempFiles returns the retained timestamped cache-*.idx temp files.
func cacheTempFiles(t *testing.T, ms *MetaStore) []string {
	t.Helper()
	matches, err := ms.ScanForTimestampedCacheFiles()
	if err != nil {
		t.Fatalf("scan cache temps: %v", err)
	}
	return matches
}

// assertLoadsClean asserts the index at path loads cleanly through the
// production loader — signature/version checks, the clean-flag header checksum
// (verifyHeaderChecksum), and structural per-entry validation. This is the path
// production uses for main/cache; the dcfhfind/repair ValidateIndexHeader uses a
// different checksum routine that does not match the writer, so it is not the
// right "loads clean" oracle here. A retained temp is the recovery input, so it
// must be a valid, loadable index.
func assertLoadsClean(t *testing.T, ms *MetaStore, path string) {
	t.Helper()
	idx, err := ms.loadIndexFromFileWithTracking(path)
	if err != nil {
		t.Fatalf("retained index %s must load + validate clean: %v", filepath.Base(path), err)
	}
	idx.release()
}

// seedMainRepo creates a temp repo with one committed main.idx (one successful
// full update) and returns the MetaStore. No cache.idx survives a full update.
func seedMainRepo(t *testing.T) *MetaStore {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "alpha\n")
	writeFile(t, dir, "sub/b.txt", "bravo\n")
	ms := NewMetaStore(dir, dir)
	if err := runUpdate(context.Background(), ms, ms.scanRun(), map[string]string{}); err != nil {
		t.Fatalf("seed update: %v", err)
	}
	return ms
}

// seedCacheRepo creates a temp repo with one committed cache.idx (one clean
// status run) and returns the MetaStore.
func seedCacheRepo(t *testing.T) *MetaStore {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "alpha\n")
	writeFile(t, dir, "sub/b.txt", "bravo\n")
	ms := NewMetaStore(dir, dir)
	if _, err := runStatus(context.Background(), ms, ms.scanRun(), map[string]string{}, nil); err != nil {
		t.Fatalf("seed status: %v", err)
	}
	if _, err := os.Stat(ms.CacheFile); err != nil {
		t.Fatalf("seed status did not produce cache.idx: %v", err)
	}
	return ms
}

// --- FR2/FR3/FR4: main path ----------------------------------------------

// TC-1 — Main rename fault preserves prior index (FR2/FR3/FR4).
func TestAtomic_MainRenameFault_PreservesIndex(t *testing.T) {
	ms := seedMainRepo(t)
	prior := readBytes(t, ms.IndexFile)
	writeFile(t, ms.RootDir, "c.txt", "charlie\n") // ensure the faulted run has work

	withRenameFault(t, errInjected)
	// Rename failure is swallowed by the deferred finalise (FR3 carve-out).
	if err := runUpdate(context.Background(), ms, ms.scanRun(), map[string]string{}); err != nil {
		t.Fatalf("rename fault should be swallowed, update returned: %v", err)
	}

	if got := readBytes(t, ms.IndexFile); string(got) != string(prior) {
		t.Errorf("prior main.idx must survive injected rename fault (bytes changed)")
	}
	temps := mainTempFiles(t, ms)
	if len(temps) == 0 {
		t.Fatalf("rename fault must leave the main temp retained for recovery")
	}
	for _, tp := range temps {
		assertLoadsClean(t, ms, tp)
	}
}

// TC-2 — Main open fault surfaces error, no residue (FR2/FR3/FR4).
func TestAtomic_MainOpenFault_SurfacesErrorNoResidue(t *testing.T) {
	ms := seedMainRepo(t)
	prior := readBytes(t, ms.IndexFile)
	writeFile(t, ms.RootDir, "c.txt", "charlie\n")

	withOpenFault(t, errInjected)
	if err := runUpdate(context.Background(), ms, ms.scanRun(), map[string]string{}); err == nil {
		t.Fatalf("open fault must surface a non-nil error")
	}

	if got := readBytes(t, ms.IndexFile); string(got) != string(prior) {
		t.Errorf("prior main.idx must be intact after injected open fault")
	}
	if temps := mainTempFiles(t, ms); len(temps) != 0 {
		t.Errorf("main open fault (!ok) must leave no temp residue, found: %v", temps)
	}
}

// TC-3 — Main sync fault surfaces error, no residue (FR2/FR3/FR4).
func TestAtomic_MainSyncFault_SurfacesErrorNoResidue(t *testing.T) {
	ms := seedMainRepo(t)
	prior := readBytes(t, ms.IndexFile)
	writeFile(t, ms.RootDir, "c.txt", "charlie\n")

	withSyncFault(t, errInjected)
	if err := runUpdate(context.Background(), ms, ms.scanRun(), map[string]string{}); err == nil {
		t.Fatalf("sync fault must surface a non-nil error")
	}

	if got := readBytes(t, ms.IndexFile); string(got) != string(prior) {
		t.Errorf("prior main.idx must be intact after injected sync fault")
	}
	if temps := mainTempFiles(t, ms); len(temps) != 0 {
		t.Errorf("main sync fault (!ok) must remove the temp, found residue: %v", temps)
	}
}

// --- FR2/FR3/FR4: cache path ---------------------------------------------

// TC-4 — Cache rename fault preserves prior cache, temp retained (FR2/FR3/FR4).
func TestAtomic_CacheRenameFault_PreservesCacheTempRetained(t *testing.T) {
	ms := seedCacheRepo(t)
	prior := readBytes(t, ms.CacheFile)

	withRenameFault(t, errInjected)
	// Cache rename failure is swallowed; status may return nil for that path.
	_, _ = runStatus(context.Background(), ms, ms.scanRun(), map[string]string{}, nil)

	if got := readBytes(t, ms.CacheFile); string(got) != string(prior) {
		t.Errorf("prior cache.idx must survive injected rename fault")
	}
	temps := cacheTempFiles(t, ms)
	if len(temps) == 0 {
		t.Fatalf("cache rename fault must retain the temp for startup merge")
	}
	for _, tp := range temps {
		assertLoadsClean(t, ms, tp)
	}
}

// TC-5 — Cache open fault: error surfaced, no stale partial promoted (FR2/FR3/FR4).
func TestAtomic_CacheOpenFault_SurfacesErrorNoStalePromotion(t *testing.T) {
	ms := seedCacheRepo(t)
	prior := readBytes(t, ms.CacheFile)

	withOpenFault(t, errInjected)
	if _, err := runStatus(context.Background(), ms, ms.scanRun(), map[string]string{}, nil); err == nil {
		t.Fatalf("cache open fault must surface a non-nil error")
	}

	if got := readBytes(t, ms.CacheFile); string(got) != string(prior) {
		t.Errorf("prior cache.idx must be intact after injected open fault")
	}
	// Open failed before the temp was created, so no cache temp should exist —
	// in particular no stale/partial index is promoted over cache.idx.
	if temps := cacheTempFiles(t, ms); len(temps) != 0 {
		t.Errorf("cache open fault failed before temp creation; expected none, found: %v", temps)
	}
}

// TC-6 — Cache sync fault: error surfaced, retained temp loads clean (FR2/FR3/FR4).
func TestAtomic_CacheSyncFault_RetainedTempLoadsClean(t *testing.T) {
	ms := seedCacheRepo(t)
	prior := readBytes(t, ms.CacheFile)

	withSyncFault(t, errInjected)
	if _, err := runStatus(context.Background(), ms, ms.scanRun(), map[string]string{}, nil); err == nil {
		t.Fatalf("cache sync fault must surface a non-nil error")
	}

	if got := readBytes(t, ms.CacheFile); string(got) != string(prior) {
		t.Errorf("prior cache.idx must be intact after injected sync fault")
	}
	temps := cacheTempFiles(t, ms)
	if len(temps) == 0 {
		t.Fatalf("cache sync fault (!ok) must retain the temp for startup merge")
	}
	// Sync fires after header + body writes, so the retained file is complete.
	for _, tp := range temps {
		assertLoadsClean(t, ms, tp)
	}
}
