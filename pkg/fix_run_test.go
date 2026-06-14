package dircachefilehash

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Tests for the task 28.2 Repo.Fix primitive and its RunFix core: command
// classification (fail-closed), write-destination confinement (D2/NFR4),
// dry-run zero-artefact (FR6/LD5), Manual-mode typed error (FR5), the shared
// cap boundary (LD4/AC6), and an end-to-end edit through the Repo interface
// (FR1). The corrupt-fixture cap walk and CLI parity land in g-testing-exec.

// buildSubject writes a valid index with the given paths to path, returning it.
func buildSubject(t *testing.T, path string, paths ...string) {
	t.Helper()
	entries := make([]*ValidatedEntry, 0, len(paths))
	for _, p := range paths {
		entries = append(entries, validatedEntryFor(p, HashTypeSHA1))
	}
	writeSubjectInPlace(t, path, HashTypeSHA256, entries)
}

func TestFixOpClassification_FailClosed(t *testing.T) {
	readOnly := []FixOp{FixOpHeaderShow, FixOpEntryShow, FixOpFixesList}
	for _, op := range readOnly {
		if fixOpIsWrite(op) {
			t.Errorf("%q should be classified read-only", op)
		}
	}
	writes := []FixOp{
		FixOpHeaderEdit, FixOpEntryEdit, FixOpEntryAppend, FixOpEntryRemove,
		FixOpFixesPop, FixOpFixesDiscard, FixOpFixesClear,
	}
	for _, op := range writes {
		if !fixOpIsWrite(op) {
			t.Errorf("%q should be classified write", op)
		}
	}
	// Fail-closed: an unclassified op is treated as a write, not read-only.
	if !fixOpIsWrite(FixOp("future-unknown-op")) {
		t.Error("unknown op must default to write (fail-closed), got read-only")
	}
}

func TestCapExceeded_Boundary(t *testing.T) {
	if capExceeded(unfixableEntryCap) {
		t.Errorf("capExceeded(%d) = true, want false (cap is > not >=)", unfixableEntryCap)
	}
	if !capExceeded(unfixableEntryCap + 1) {
		t.Errorf("capExceeded(%d) = false, want true (the 101st unfixable entry trips it)", unfixableEntryCap+1)
	}
}

func TestConfineWriteDest_AcceptAndReject(t *testing.T) {
	root := t.TempDir()
	// Positive control: a leaf inside root is accepted (need not exist yet).
	in := filepath.Join(root, "sub")
	if err := os.MkdirAll(in, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := confineWriteDest(filepath.Join(in, "main.idx"), root); err != nil {
		t.Errorf("in-root dest rejected: %v", err)
	}
	// Reject: a sibling directory outside root.
	outside := t.TempDir()
	if _, err := confineWriteDest(filepath.Join(outside, "main.idx"), root); err == nil {
		t.Error("out-of-root dest accepted, want rejection")
	}
	// Reject: a path that lexically sits under root but traverses a symlinked
	// parent pointing outside.
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err := confineWriteDest(filepath.Join(link, "main.idx"), root); err == nil {
		t.Error("symlinked-parent escape accepted, want rejection")
	}
}

func TestConfineWriteDir_AncestorOutsideRoot(t *testing.T) {
	root := t.TempDir()
	// A not-yet-created multi-component dir under root is accepted (deepest
	// existing ancestor resolved, remainder recombined).
	if err := confineWriteDir(filepath.Join(root, ".dcfh", "fixes", "main"), root); err != nil {
		t.Errorf("in-root multi-component dir rejected: %v", err)
	}
	// A dir whose existing ancestor resolves outside root is rejected.
	outside := t.TempDir()
	if err := confineWriteDir(filepath.Join(outside, "fixes", "main"), root); err == nil {
		t.Error("out-of-root dir accepted, want rejection")
	}
}

func TestRunFix_ManualModeTypedError(t *testing.T) {
	dir := t.TempDir()
	subject := filepath.Join(dir, "main.idx")
	buildSubject(t, subject, "a.txt")

	refs := []IndexRef{{Path: subject, Type: RefTypeFile}}
	req := FixRequest{
		Mode:     FixModeManual,
		Commands: []FixCommand{{Op: FixOpEntryRemove, Paths: []string{"a.txt"}}},
		Flags:    FixEntryFlags{Quiet: true, EditInPlace: true, Force: true},
	}
	_, err := RunFix(context.Background(), refs, req, "", os.Stderr)
	if !errors.Is(err, ErrManualModeUnimplemented) {
		t.Fatalf("expected ErrManualModeUnimplemented, got %v", err)
	}
}

func TestRunFix_DryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	subject := filepath.Join(dir, "main.idx")
	buildSubject(t, subject, "a.txt", "b.txt")
	before, err := os.ReadFile(subject)
	if err != nil {
		t.Fatal(err)
	}

	refs := []IndexRef{{Path: subject, Type: RefTypeFile}}
	req := FixRequest{
		Mode:     FixModeAuto,
		DryRun:   true,
		Backup:   true, // backup must also be skipped under dry-run
		Commands: []FixCommand{{Op: FixOpEntryRemove, Paths: []string{"a.txt"}}},
		Flags:    FixEntryFlags{Quiet: true},
	}
	res, err := RunFix(context.Background(), refs, req, "", os.Stderr)
	if err != nil {
		t.Fatalf("dry-run RunFix: %v", err)
	}
	if res.RepairsApplied != 1 {
		t.Errorf("dry-run RepairsApplied = %d, want 1 (would-be count)", res.RepairsApplied)
	}
	// No artefact written: subject byte-identical, no .fix.tmp / .pre-fix / fixes dir.
	after, err := os.ReadFile(subject)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("dry-run mutated the subject index")
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, "*"))
	for _, f := range leftovers {
		base := filepath.Base(f)
		if base != "main.idx" {
			t.Errorf("dry-run left an artefact: %s", base)
		}
	}
}

func TestRunFix_EntryRemoveWritesAndCounts(t *testing.T) {
	dir := t.TempDir()
	subject := filepath.Join(dir, "main.idx")
	buildSubject(t, subject, "a.txt", "b.txt", "c.txt")

	refs := []IndexRef{{Path: subject, Type: RefTypeFile}}
	req := FixRequest{
		Mode:     FixModeAuto,
		Commands: []FixCommand{{Op: FixOpEntryRemove, Paths: []string{"b.txt"}}},
		Flags:    FixEntryFlags{Quiet: true, EditInPlace: true, Force: true},
	}
	res, err := RunFix(context.Background(), refs, req, "", os.Stderr)
	if err != nil {
		t.Fatalf("RunFix: %v", err)
	}
	if res.RepairsApplied != 1 {
		t.Errorf("RepairsApplied = %d, want 1", res.RepairsApplied)
	}
	// b.txt gone, a.txt and c.txt remain.
	found, notFound, err := FindEntries(subject, []string{"a.txt", "b.txt", "c.txt"})
	if err != nil {
		t.Fatalf("FindEntries: %v", err)
	}
	if len(found) != 2 {
		t.Errorf("found %d entries, want 2 (a,c)", len(found))
	}
	if len(notFound) != 1 || notFound[0] != "b.txt" {
		t.Errorf("notFound = %v, want [b.txt]", notFound)
	}
}

// buildCorruptSubject writes a subject with n entries (sorted, equal-length,
// unique paths) then nulls every path's bytes — leaving Size intact so
// NewValidatedEntry fails ("path is empty") while trySkipToNextEntry advances
// cleanly. Every entry is therefore an unfixable discard.
func buildCorruptSubject(t *testing.T, path string, n int) {
	t.Helper()
	paths := make([]string, n)
	entries := make([]*ValidatedEntry, n)
	for i := range n {
		paths[i] = fmt.Sprintf("p%05d.txt", i) // equal length, sorted, unique
		entries[i] = validatedEntryFor(paths[i], HashTypeSHA1)
	}
	writeSubjectInPlace(t, path, HashTypeSHA256, entries)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		idx := bytes.Index(data, []byte(p))
		if idx < 0 {
			t.Fatalf("could not locate path %q to corrupt", p)
		}
		for i := 0; i < len(p); i++ {
			data[idx+i] = 0
		}
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestRunFix_CapTripsOnAllThreeLoops drives the edit, append, and removal walk
// loops through 101 unfixable entries. The shared cap predicate (capExceeded,
// >100) must trip on the 101st on every loop, RunFix must return an error, and
// FixResult.EntriesDiscarded must reflect the discards (AC6/NFR5/LD4).
func TestRunFix_CapTripsOnAllThreeLoops(t *testing.T) {
	cases := []struct {
		name string
		cmd  FixCommand
	}{
		{"edit", FixCommand{Op: FixOpEntryEdit, Field: "uid", Value: "7", Paths: []string{"none"}}},
		{"append", FixCommand{Op: FixOpEntryAppend, Value: `{"path":"new.txt","hash":"0000000000000000000000000000000000000000","hash_type":1,"mtime":"0","ctime":"0"}`}},
		{"remove", FixCommand{Op: FixOpEntryRemove, Paths: []string{"none"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			subject := filepath.Join(dir, "main.idx")
			buildCorruptSubject(t, subject, unfixableEntryCap+1) // 101 corrupt entries

			refs := []IndexRef{{Path: subject, Type: RefTypeFile}}
			req := FixRequest{
				Mode:     FixModeAuto,
				Commands: []FixCommand{tc.cmd},
				Flags:    FixEntryFlags{Quiet: true, EditInPlace: true, Force: true},
			}
			res, err := RunFix(context.Background(), refs, req, "", os.Stderr)
			if err == nil {
				t.Fatalf("%s: expected cap-trip error, got nil", tc.name)
			}
			if !strings.Contains(err.Error(), "too many unfixable entries") {
				t.Errorf("%s: error = %q, want 'too many unfixable entries'", tc.name, err)
			}
			if res.EntriesDiscarded != unfixableEntryCap+1 {
				t.Errorf("%s: EntriesDiscarded = %d, want %d (discards surfaced on cap error)", tc.name, res.EntriesDiscarded, unfixableEntryCap+1)
			}
		})
	}
}

// TestRunFix_HeaderEditChangesField exercises the header-edit family
// (runHeaderEdit → ApplyHeaderEdit surgical writer) and confirms the field
// changes while the produced index stays loadable.
func TestRunFix_HeaderEditChangesField(t *testing.T) {
	dir := t.TempDir()
	subject := filepath.Join(dir, "main.idx")
	buildSubject(t, subject, "a.txt", "b.txt")

	refs := []IndexRef{{Path: subject, Type: RefTypeFile}}
	req := FixRequest{
		Mode:     FixModeAuto,
		Commands: []FixCommand{{Op: FixOpHeaderEdit, Field: "flags", Value: "5"}},
		Flags:    FixEntryFlags{Quiet: true, EditInPlace: true, Force: true},
	}
	res, err := RunFix(context.Background(), refs, req, "", os.Stderr)
	if err != nil {
		t.Fatalf("header edit: %v", err)
	}
	if res.RepairsApplied != 1 {
		t.Errorf("RepairsApplied = %d, want 1", res.RepairsApplied)
	}
	if h := headerOf(t, subject); h.Flags != 5 {
		t.Errorf("written header Flags = %d, want 5", h.Flags)
	}

	// An unknown field is rejected (validation), no write.
	_, err = RunFix(context.Background(), refs, FixRequest{
		Mode:     FixModeAuto,
		Commands: []FixCommand{{Op: FixOpHeaderEdit, Field: "bogus", Value: "1"}},
		Flags:    FixEntryFlags{Quiet: true, EditInPlace: true, Force: true},
	}, "", os.Stderr)
	if err == nil {
		t.Error("expected error for unknown header field, got nil")
	}
}

// TestRunFix_EntryAppendProducesValidIndex covers the append family and TC-3:
// the produced index loads and contains the appended entry.
func TestRunFix_EntryAppendProducesValidIndex(t *testing.T) {
	dir := t.TempDir()
	subject := filepath.Join(dir, "main.idx")
	buildSubject(t, subject, "a.txt")

	newJSON := `{"path":"appended.txt","hash":"1111111111111111111111111111111111111111","hash_type":1,"mtime":"0","ctime":"0"}`
	refs := []IndexRef{{Path: subject, Type: RefTypeFile}}
	res, err := RunFix(context.Background(), refs, FixRequest{
		Mode:     FixModeAuto,
		Commands: []FixCommand{{Op: FixOpEntryAppend, Value: newJSON}},
		Flags:    FixEntryFlags{Quiet: true, EditInPlace: true, Force: true},
	}, "", os.Stderr)
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if res.RepairsApplied != 1 {
		t.Errorf("RepairsApplied = %d, want 1", res.RepairsApplied)
	}
	found, _, err := FindEntries(subject, []string{"appended.txt"})
	if err != nil {
		t.Fatalf("FindEntries: %v", err)
	}
	if len(found) != 1 {
		t.Errorf("appended entry not found in produced index (%d matches)", len(found))
	}
}

// TestRunFix_BackupControlAndPop covers TC-6: with Backup on, a write op stacks
// a recoverable backup (confineBackupDir + CreateBackup), discoverable via
// ListBackups and restorable via the fixes-pop family; with Backup off, none.
func TestRunFix_BackupControlAndPop(t *testing.T) {
	root := t.TempDir()
	metaDir := filepath.Join(root, ".dcfh")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	subject := filepath.Join(metaDir, "main.idx")
	buildSubject(t, subject, "keep.txt", "edit.txt")

	refs := []IndexRef{{Path: subject, Type: RefTypeFile}}
	// Backup ON (library path: writeRoot = MetaDir, confinement engaged).
	_, err := RunFix(context.Background(), refs, FixRequest{
		Mode:     FixModeAuto,
		Backup:   true,
		Commands: []FixCommand{{Op: FixOpEntryEdit, Field: "uid", Value: "9", Paths: []string{"edit.txt"}}},
		Flags:    FixEntryFlags{Quiet: true, EditInPlace: true, Force: true},
	}, metaDir, os.Stderr)
	if err != nil {
		t.Fatalf("edit with backup: %v", err)
	}
	backups, err := ListBackups(subject)
	if err != nil {
		t.Fatalf("ListBackups: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("backup stack = %d, want 1", len(backups))
	}
	if backups[0].Operation != "entry-edit" {
		t.Errorf("backup operation = %q, want entry-edit", backups[0].Operation)
	}

	// fixes-pop via RunFix restores and clears the stack.
	if _, err := RunFix(context.Background(), refs, FixRequest{
		Mode:     FixModeAuto,
		Commands: []FixCommand{{Op: FixOpFixesPop}},
		Flags:    FixEntryFlags{Quiet: true},
	}, metaDir, os.Stderr); err != nil {
		t.Fatalf("fixes-pop: %v", err)
	}
	if after, _ := ListBackups(subject); len(after) != 0 {
		t.Errorf("backup stack after pop = %d, want 0", len(after))
	}

	// Backup OFF: no new backup artefact.
	if _, err := RunFix(context.Background(), refs, FixRequest{
		Mode:     FixModeAuto,
		Backup:   false,
		Commands: []FixCommand{{Op: FixOpEntryEdit, Field: "uid", Value: "8", Paths: []string{"edit.txt"}}},
		Flags:    FixEntryFlags{Quiet: true, EditInPlace: true, Force: true},
	}, metaDir, os.Stderr); err != nil {
		t.Fatalf("edit without backup: %v", err)
	}
	if after, _ := ListBackups(subject); len(after) != 0 {
		t.Errorf("backup stack with --backup=false = %d, want 0", len(after))
	}
}

// TestRunFix_FixesDiscardAndClear covers the remaining backup-stack write ops
// through RunFix (DiscardBackup, ClearBackups), completing TC-1's coverage of
// every FixOp variant.
func TestRunFix_FixesDiscardAndClear(t *testing.T) {
	root := t.TempDir()
	metaDir := filepath.Join(root, ".dcfh")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	subject := filepath.Join(metaDir, "main.idx")
	buildSubject(t, subject, "x.txt", "y.txt")
	refs := []IndexRef{{Path: subject, Type: RefTypeFile}}

	edit := func(uid string) {
		if _, err := RunFix(context.Background(), refs, FixRequest{
			Mode:     FixModeAuto,
			Backup:   true,
			Commands: []FixCommand{{Op: FixOpEntryEdit, Field: "uid", Value: uid, Paths: []string{"x.txt"}}},
			Flags:    FixEntryFlags{Quiet: true, EditInPlace: true, Force: true},
		}, metaDir, os.Stderr); err != nil {
			t.Fatalf("edit: %v", err)
		}
	}
	stack := func(op FixOp) {
		if _, err := RunFix(context.Background(), refs, FixRequest{
			Mode: FixModeAuto, Commands: []FixCommand{{Op: op}}, Flags: FixEntryFlags{Quiet: true},
		}, metaDir, os.Stderr); err != nil {
			t.Fatalf("%s: %v", op, err)
		}
	}

	// Backup filenames are second-granularity, so rapid same-second edits
	// collide into one stack entry — exercise discard/clear one backup at a time.
	edit("1")
	if b, _ := ListBackups(subject); len(b) != 1 {
		t.Fatalf("stack after edit = %d, want 1", len(b))
	}
	stack(FixOpFixesDiscard)
	if b, _ := ListBackups(subject); len(b) != 0 {
		t.Errorf("stack after discard = %d, want 0", len(b))
	}
	edit("2")
	stack(FixOpFixesClear)
	if b, _ := ListBackups(subject); len(b) != 0 {
		t.Errorf("stack after clear = %d, want 0", len(b))
	}
}

// TestRepoFix_ThroughInterface exercises Fix via the Repo interface (FR1) and
// confirms MetaDir confinement: an in-MetaDir subject edits, an out-of-MetaDir
// selector is rejected before any write.
func TestRepoFix_ThroughInterface(t *testing.T) {
	root := t.TempDir()
	ms := NewMetaStore(root, root)
	metaDir := ms.MetaDir
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	subject := filepath.Join(metaDir, MainIndex)
	buildSubject(t, subject, "keep.txt", "drop.txt")

	var repo Repo = newLocalRepo(ms)
	t.Cleanup(func() { _ = repo.Close() })

	// In-MetaDir edit via the interface.
	res, err := repo.Fix(context.Background(), FixRequest{
		IndexSelectors: []string{"main"},
		Mode:           FixModeAuto,
		Commands:       []FixCommand{{Op: FixOpEntryRemove, Paths: []string{"drop.txt"}}},
		Flags:          FixEntryFlags{Quiet: true, EditInPlace: true, Force: true},
	})
	if err != nil {
		t.Fatalf("repo.Fix: %v", err)
	}
	if res.RepairsApplied != 1 {
		t.Errorf("RepairsApplied = %d, want 1", res.RepairsApplied)
	}

	// Out-of-MetaDir subject: a selector resolving to an existing file outside
	// MetaDir must be rejected before any write (D2/NFR4/AC7).
	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "victim.idx")
	buildSubject(t, outside, "x.txt")
	outsideBefore, _ := os.ReadFile(outside)

	_, err = repo.Fix(context.Background(), FixRequest{
		IndexSelectors: []string{outside},
		Mode:           FixModeAuto,
		Commands:       []FixCommand{{Op: FixOpEntryRemove, Paths: []string{"x.txt"}}},
		Flags:          FixEntryFlags{Quiet: true, EditInPlace: true, Force: true},
	})
	if err == nil {
		t.Fatal("expected confinement rejection for out-of-MetaDir selector, got nil")
	}
	outsideAfter, _ := os.ReadFile(outside)
	if string(outsideBefore) != string(outsideAfter) {
		t.Error("out-of-MetaDir subject was mutated despite confinement")
	}
}
