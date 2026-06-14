package dircachefilehash

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Integration + reliability tests for the task 28.3 recovery rebuild driven
// through the Repo.Fix batch branch (runRecoveryRebuild): rebuild from a
// surviving source, dry-run zero-artefact, op-mixing rejection, read/write
// confinement, the snapshot-readback gate, the empty guard, context
// cancellation, and fault-injection atomicity. The merge core itself is unit-
// tested in fix_recovery_test.go.

// seedRecoveryRepo builds a repo whose cache.idx holds the given paths and whose
// main.idx is absent (the destroyed-main recovery scenario). Returns the repo
// and its MetaStore.
func seedRecoveryRepo(t *testing.T, paths ...string) (Repo, *MetaStore) {
	t.Helper()
	root := t.TempDir()
	ms := NewMetaStore(root, root)
	if err := os.MkdirAll(ms.MetaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	entries := make([]*ValidatedEntry, 0, len(paths))
	for _, p := range paths {
		entries = append(entries, validatedEntryFor(p, HashTypeSHA1))
	}
	writeSource(t, ms.MetaDir, CacheIndex, HashTypeSHA256, entries)
	// Model a destroyed main.idx: CreateMetaStore seeds an empty one, so remove
	// it to leave cache.idx the only surviving source.
	if err := os.Remove(ms.IndexFile); err != nil {
		t.Fatal(err)
	}
	repo := newLocalRepo(ms)
	t.Cleanup(func() { _ = repo.Close() })
	return repo, ms
}

func recoveryRequest(selectors []string) FixRequest {
	return FixRequest{
		IndexSelectors: selectors,
		Mode:           FixModeAuto,
		Commands:       []FixCommand{{Op: FixOpRecoveryRebuild}},
		Flags:          FixEntryFlags{Quiet: true},
	}
}

// TC-9 / TC-12b — a destroyed main.idx is rebuilt from the intact cache.idx via
// one Repo.Fix call; the result re-loads clean and the write target is the fixed
// in-MetaDir main.idx.
func TestRecovery_RebuildFromCache(t *testing.T) {
	repo, ms := seedRecoveryRepo(t, "a.txt", "b.txt")

	res, err := repo.Fix(context.Background(), recoveryRequest([]string{"cache", "main"}))
	if err != nil {
		t.Fatalf("recovery Fix: %v", err)
	}
	if res.RepairsApplied != 2 {
		t.Errorf("RepairsApplied = %d, want 2", res.RepairsApplied)
	}
	if res.EntriesDiscarded != 0 {
		t.Errorf("EntriesDiscarded = %d, want 0", res.EntriesDiscarded)
	}
	if _, err := os.Stat(ms.IndexFile); err != nil {
		t.Fatalf("main.idx not produced at the fixed destination: %v", err)
	}
	assertLoadsClean(t, ms, ms.IndexFile)
}

// TC-10 / LD9 — a dry run reports the projected counts but writes nothing: no
// main.idx, no recovery/ snapshot, no .fix.tmp.
func TestRecovery_DryRunWritesNothing(t *testing.T) {
	repo, ms := seedRecoveryRepo(t, "a.txt", "b.txt")

	req := recoveryRequest([]string{"cache", "main"})
	req.DryRun = true
	res, err := repo.Fix(context.Background(), req)
	if err != nil {
		t.Fatalf("dry-run Fix: %v", err)
	}
	if res.RepairsApplied != 2 {
		t.Errorf("projected RepairsApplied = %d, want 2", res.RepairsApplied)
	}
	if _, err := os.Stat(ms.IndexFile); !os.IsNotExist(err) {
		t.Error("dry-run created main.idx")
	}
	if _, err := os.Stat(filepath.Join(ms.MetaDir, "recovery")); !os.IsNotExist(err) {
		t.Error("dry-run created a recovery/ snapshot")
	}
	if _, err := os.Stat(ms.IndexFile + ".fix.tmp"); !os.IsNotExist(err) {
		t.Error("dry-run left a .fix.tmp")
	}
}

// TC-11 / LD1 — recovery-rebuild mixed with another op in one request is
// rejected before any write.
func TestRecovery_OpMixingRejected(t *testing.T) {
	repo, ms := seedRecoveryRepo(t, "a.txt")

	req := recoveryRequest([]string{"cache"})
	req.Commands = append(req.Commands, FixCommand{Op: FixOpHeaderShow})
	if _, err := repo.Fix(context.Background(), req); err == nil {
		t.Fatal("expected op-mixing rejection, got nil")
	}
	if _, err := os.Stat(ms.IndexFile); !os.IsNotExist(err) {
		t.Error("rejected op-mixing still wrote main.idx")
	}
}

// TC-12a / NFR4 — a named source resolving outside MetaDir is rejected before
// any read or write.
func TestRecovery_OutOfMetaDirSourceRejected(t *testing.T) {
	repo, ms := seedRecoveryRepo(t, "a.txt")
	outside := filepath.Join(t.TempDir(), "victim.idx")
	if err := os.WriteFile(outside, []byte("not an index"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := repo.Fix(context.Background(), recoveryRequest([]string{"cache", outside})); err == nil {
		t.Fatal("expected out-of-MetaDir source rejection, got nil")
	}
	if _, err := os.Stat(ms.IndexFile); !os.IsNotExist(err) {
		t.Error("rejected request still wrote main.idx")
	}
}

// TC-13 / LD6 — verifyRecoverySnapshot is the fatal readback: a missing, empty,
// or symlinked snapshot copy of a contributing source aborts; a valid copy
// passes.
func TestVerifyRecoverySnapshot_Gate(t *testing.T) {
	metaDir := recoveryMetaDir(t)
	recoveryDir := filepath.Join(metaDir, "recovery")
	if err := os.MkdirAll(recoveryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	contributing := []string{filepath.Join(metaDir, CacheIndex)}
	copyPath := filepath.Join(recoveryDir, CacheIndex)

	// Missing copy.
	if err := verifyRecoverySnapshot(metaDir, contributing); err == nil {
		t.Error("missing snapshot copy should abort")
	}
	// Empty copy.
	if err := os.WriteFile(copyPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyRecoverySnapshot(metaDir, contributing); err == nil {
		t.Error("empty snapshot copy should abort")
	}
	// Symlinked copy (not followed).
	if err := os.Remove(copyPath); err != nil {
		t.Fatal(err)
	}
	realTarget := filepath.Join(metaDir, "real-target")
	if err := os.WriteFile(realTarget, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realTarget, copyPath); err != nil {
		t.Fatal(err)
	}
	if err := verifyRecoverySnapshot(metaDir, contributing); err == nil {
		t.Error("symlinked snapshot copy should abort (not be followed)")
	}
	// Valid non-empty regular copy passes.
	if err := os.Remove(copyPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(copyPath, []byte("real backup bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyRecoverySnapshot(metaDir, contributing); err != nil {
		t.Errorf("valid snapshot copy should pass: %v", err)
	}
}

// TC-14 / LD7 — sources that merge to zero survivors abort before the write,
// leaving no main.idx behind.
func TestRecovery_EmptyGuardAborts(t *testing.T) {
	root := t.TempDir()
	ms := NewMetaStore(root, root)
	if err := os.MkdirAll(ms.MetaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// cache.idx with only a tombstone → merges to zero survivors.
	writeSource(t, ms.MetaDir, CacheIndex, HashTypeSHA256, []*ValidatedEntry{deletedEntryFor("gone.txt")})
	if err := os.Remove(ms.IndexFile); err != nil { // destroyed main.idx
		t.Fatal(err)
	}
	repo := newLocalRepo(ms)
	t.Cleanup(func() { _ = repo.Close() })

	res, err := repo.Fix(context.Background(), recoveryRequest([]string{"cache"}))
	if err == nil {
		t.Fatal("empty merge should abort with an error")
	}
	if res.EntriesDiscarded != 1 {
		t.Errorf("EntriesDiscarded = %d, want 1 (the filtered tombstone)", res.EntriesDiscarded)
	}
	if _, statErr := os.Stat(ms.IndexFile); !os.IsNotExist(statErr) {
		t.Error("empty guard still wrote main.idx")
	}
}

// TC-15 — a context cancelled before the write aborts the rebuild without
// producing main.idx.
func TestRecovery_ContextCancelledBeforeWrite(t *testing.T) {
	repo, ms := seedRecoveryRepo(t, "a.txt", "b.txt")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := repo.Fix(ctx, recoveryRequest([]string{"cache", "main"})); err == nil {
		t.Fatal("cancelled context should abort the rebuild")
	}
	if _, err := os.Stat(ms.IndexFile); !os.IsNotExist(err) {
		t.Error("cancelled rebuild still wrote main.idx")
	}
}

// TC-16 / NFR5 — fault-injection atomicity: a failure during the temp-index
// write leaves no partial/corrupt main.idx and no stray temp; once the fault
// clears, a clean rebuild still succeeds.
func TestRecovery_FaultInjectionAtomicity(t *testing.T) {
	repo, ms := seedRecoveryRepo(t, "a.txt", "b.txt")

	t.Run("sync fault leaves no partial index", func(t *testing.T) {
		// Force the pre-rename Sync (inside writeRepairedIndex) to fail — it
		// fires after the temp body is written but before the promote. swapFn
		// restores fsSync when this subtest ends.
		withSyncFault(t, errInjected)

		if _, err := repo.Fix(context.Background(), recoveryRequest([]string{"cache", "main"})); err == nil {
			t.Fatal("injected sync fault should fail the rebuild")
		}
		if _, err := os.Stat(ms.IndexFile); !os.IsNotExist(err) {
			t.Error("faulted rebuild left a main.idx behind")
		}
		if _, err := os.Stat(ms.IndexFile + ".fix.tmp"); !os.IsNotExist(err) {
			t.Error("faulted rebuild left a .fix.tmp behind")
		}
	})

	t.Run("clean rebuild after the fault clears", func(t *testing.T) {
		res, err := repo.Fix(context.Background(), recoveryRequest([]string{"cache", "main"}))
		if err != nil {
			t.Fatalf("post-fault rebuild: %v", err)
		}
		if res.RepairsApplied != 2 {
			t.Errorf("RepairsApplied = %d, want 2", res.RepairsApplied)
		}
		assertLoadsClean(t, ms, ms.IndexFile)
	})
}
