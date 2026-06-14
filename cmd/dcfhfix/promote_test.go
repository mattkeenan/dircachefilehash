package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dircachefilehash "github.com/mattkeenan/dircachefilehash/pkg"
)

// optsWith builds a ParsedOptions with the flags promote.go reads, parsed from
// the given argv-style tokens (e.g. "--edit-in-place", "--force").
func optsWith(t *testing.T, flags ...string) *ParsedOptions {
	t.Helper()
	o := NewParsedOptions()
	o.DefineOption("edit-in-place", "", OptionTypeBool, "false", "")
	o.DefineOption("force", "f", OptionTypeBool, "false", "")
	o.DefineOption("quiet", "q", OptionTypeBool, "false", "")
	o.DefineOption("dry-run", "n", OptionTypeBool, "false", "")
	o.DefineOption("backup", "b", OptionTypeBool, "true", "")
	if err := o.Parse(flags); err != nil {
		t.Fatalf("parse opts %v: %v", flags, err)
	}
	return o
}

// currentStamp mirrors siblingPreFixPath's second-resolution UTC stamp.
func currentStamp() string { return time.Now().UTC().Format("20060102T150405Z") }

// withStableSecond runs body until it completes entirely within a single UTC
// second. siblingPreFixPath is second-resolution, so a fixture planted at a
// pre-computed sibling path only matches the path the code under test derives
// when no second boundary is crossed mid-test. body returns false to signal it
// straddled a boundary (retry); true once it ran meaningfully and asserted.
func withStableSecond(t *testing.T, body func() bool) {
	t.Helper()
	for range 50 {
		if body() {
			return
		}
	}
	t.Fatal("could not obtain a stable UTC second after 50 attempts")
}

// TC-U1 — sibling naming shape.
func TestSiblingPreFixPath_Shape(t *testing.T) {
	got := dircachefilehash.SiblingPreFixPath("/x/main.idx")
	const wantPrefix = "/x/main.idx.pre-fix-"
	if !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("siblingPreFixPath = %q, want prefix %q", got, wantPrefix)
	}
	if dir := filepath.Dir(got); dir != "/x" {
		t.Errorf("sibling dir = %q, want /x", dir)
	}
	stamp := strings.TrimPrefix(got, wantPrefix)
	if _, err := time.Parse("20060102T150405Z", stamp); err != nil {
		t.Errorf("stamp %q not in expected compact-UTC format: %v", stamp, err)
	}
}

// TC-U2 — preserve creates byte-identical sibling, original intact.
func TestPreserveOriginal_ByteIdenticalOriginalIntact(t *testing.T) {
	dir := t.TempDir()
	idx := filepath.Join(dir, "main.idx")
	content := []byte("dcfh-index\x00\x01\x02payload")
	if err := os.WriteFile(idx, content, 0644); err != nil {
		t.Fatal(err)
	}

	sib, err := dircachefilehash.PreserveOriginal(idx)
	if err != nil {
		t.Fatalf("preserveOriginal: %v", err)
	}
	if sib == idx || filepath.Dir(sib) != dir {
		t.Fatalf("sibling %q must be a distinct path in the same dir as %q", sib, idx)
	}

	gotSib, err := os.ReadFile(sib)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotSib, content) {
		t.Errorf("sibling not byte-identical to source")
	}
	// Original still present and unchanged (copy-before-rename ordering — AC6).
	gotOrig, err := os.ReadFile(idx)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotOrig, content) {
		t.Errorf("original modified by preservation")
	}
}

// TC-U3 — refuse symlink destination, symlink target untouched.
func TestPreserveOriginal_RefusesSymlink(t *testing.T) {
	withStableSecond(t, func() bool {
		s1 := currentStamp()
		dir := t.TempDir()
		idx := filepath.Join(dir, "main.idx")
		if err := os.WriteFile(idx, []byte("orig"), 0644); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(dir, "sentinel")
		if err := os.WriteFile(target, []byte("SECRET"), 0644); err != nil {
			t.Fatal(err)
		}
		base := dircachefilehash.SiblingPreFixPath(idx)
		if err := os.Symlink(target, base); err != nil {
			t.Fatal(err)
		}

		_, err := dircachefilehash.PreserveOriginal(idx)
		if currentStamp() != s1 {
			return false // straddled a second boundary; retry
		}
		if err == nil {
			t.Fatal("expected refusal when sibling pre-exists as a symlink")
		}
		// No write traversed the link: the target file is unchanged.
		got, rerr := os.ReadFile(target)
		if rerr != nil {
			t.Fatal(rerr)
		}
		if string(got) != "SECRET" {
			t.Errorf("symlink target modified: got %q", got)
		}
		return true
	})
}

// TC-U4 — refuse directory destination.
func TestPreserveOriginal_RefusesDirectory(t *testing.T) {
	withStableSecond(t, func() bool {
		s1 := currentStamp()
		dir := t.TempDir()
		idx := filepath.Join(dir, "main.idx")
		if err := os.WriteFile(idx, []byte("orig"), 0644); err != nil {
			t.Fatal(err)
		}
		base := dircachefilehash.SiblingPreFixPath(idx)
		if err := os.Mkdir(base, 0755); err != nil {
			t.Fatal(err)
		}

		_, err := dircachefilehash.PreserveOriginal(idx)
		if currentStamp() != s1 {
			return false
		}
		if err == nil {
			t.Fatal("expected refusal when sibling pre-exists as a directory")
		}
		return true
	})
}

// TC-U5 — EEXIST on a regular file advances the counter.
func TestPreserveOriginal_EEXISTAdvancesCounter(t *testing.T) {
	withStableSecond(t, func() bool {
		s1 := currentStamp()
		dir := t.TempDir()
		idx := filepath.Join(dir, "main.idx")
		if err := os.WriteFile(idx, []byte("ORIGINAL"), 0644); err != nil {
			t.Fatal(err)
		}
		base := dircachefilehash.SiblingPreFixPath(idx)
		if err := os.WriteFile(base, []byte("PRIOR"), 0644); err != nil {
			t.Fatal(err)
		}

		sib, err := dircachefilehash.PreserveOriginal(idx)
		if currentStamp() != s1 {
			return false
		}
		if err != nil {
			t.Fatalf("preserveOriginal: %v", err)
		}
		if sib != base+"-1" {
			t.Errorf("advanced sibling = %q, want %q", sib, base+"-1")
		}
		// The pre-existing copy is left intact.
		if prior, _ := os.ReadFile(base); string(prior) != "PRIOR" {
			t.Errorf("pre-existing preserved copy was overwritten: %q", prior)
		}
		if got, _ := os.ReadFile(sib); string(got) != "ORIGINAL" {
			t.Errorf("new sibling content = %q, want ORIGINAL", got)
		}
		return true
	})
}

// TC-U6 — exhausting the collision bound is a hard refusal.
func TestPreserveOriginal_BoundExhaustion(t *testing.T) {
	withStableSecond(t, func() bool {
		s1 := currentStamp()
		dir := t.TempDir()
		idx := filepath.Join(dir, "main.idx")
		if err := os.WriteFile(idx, []byte("orig"), 0644); err != nil {
			t.Fatal(err)
		}
		base := dircachefilehash.SiblingPreFixPath(idx)
		// Occupy base and base-1 .. base-<max> with regular files.
		if err := os.WriteFile(base, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		for n := 1; n <= dircachefilehash.MaxPreFixCollisionSuffix; n++ {
			cand := fmt.Sprintf("%s-%d", base, n)
			if err := os.WriteFile(cand, []byte("x"), 0644); err != nil {
				t.Fatal(err)
			}
		}

		_, err := dircachefilehash.PreserveOriginal(idx)
		if currentStamp() != s1 {
			return false
		}
		if err == nil {
			t.Fatal("expected error when all collision candidates exist")
		}
		// No candidate beyond the bound was created.
		if _, serr := os.Stat(fmt.Sprintf("%s-%d", base, dircachefilehash.MaxPreFixCollisionSuffix+1)); serr == nil {
			t.Errorf("a sibling past the bound was created")
		}
		return true
	})
}

// TC-U7 — gate logic.
func TestValidateEditInPlaceGate(t *testing.T) {
	if err := dircachefilehash.ValidateEditInPlaceGate(fixFlags(optsWith(t, "--edit-in-place"))); err == nil {
		t.Error("--edit-in-place without --force should error")
	}
	if err := dircachefilehash.ValidateEditInPlaceGate(fixFlags(optsWith(t, "--edit-in-place", "--force"))); err != nil {
		t.Errorf("--edit-in-place --force should pass: %v", err)
	}
	if err := dircachefilehash.ValidateEditInPlaceGate(fixFlags(optsWith(t))); err != nil {
		t.Errorf("neither flag should pass: %v", err)
	}
	if err := dircachefilehash.ValidateEditInPlaceGate(fixFlags(optsWith(t, "--force"))); err != nil {
		t.Errorf("--force alone should pass: %v", err)
	}
}

// promoteRepairedIndex: default mode preserves a byte-identical sibling then
// renames the temp into place (the preserve-before-rename boundary, NFR5).
func TestPromoteRepairedIndex_DefaultPreserves(t *testing.T) {
	dir := t.TempDir()
	idx := filepath.Join(dir, "main.idx")
	tmp := filepath.Join(dir, "main.idx.tmp")
	original := []byte("ORIGINAL-INDEX")
	repaired := []byte("REPAIRED-INDEX")
	if err := os.WriteFile(idx, original, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, repaired, 0644); err != nil {
		t.Fatal(err)
	}

	if err := dircachefilehash.PromoteRepairedIndex(tmp, idx, fixFlags(optsWith(t))); err != nil {
		t.Fatalf("promoteRepairedIndex: %v", err)
	}
	// Canonical now holds the repaired bytes.
	if got, _ := os.ReadFile(idx); !bytes.Equal(got, repaired) {
		t.Errorf("canonical = %q, want repaired", got)
	}
	// Temp consumed by the rename.
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Errorf("temp file should be gone after rename")
	}
	// Exactly one .pre-fix sibling, byte-identical to the pre-repair original.
	sib := findPreFixSibling(t, dir)
	if got, _ := os.ReadFile(sib); !bytes.Equal(got, original) {
		t.Errorf("preserved sibling = %q, want original", got)
	}
}

// promoteRepairedIndex: --edit-in-place suppresses the sibling.
func TestPromoteRepairedIndex_EditInPlaceSuppresses(t *testing.T) {
	dir := t.TempDir()
	idx := filepath.Join(dir, "main.idx")
	tmp := filepath.Join(dir, "main.idx.tmp")
	if err := os.WriteFile(idx, []byte("ORIGINAL"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, []byte("REPAIRED"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := dircachefilehash.PromoteRepairedIndex(tmp, idx, fixFlags(optsWith(t, "--edit-in-place", "--force"))); err != nil {
		t.Fatalf("promoteRepairedIndex: %v", err)
	}
	if got, _ := os.ReadFile(idx); string(got) != "REPAIRED" {
		t.Errorf("canonical = %q, want REPAIRED", got)
	}
	if matches := preFixSiblings(t, dir); len(matches) != 0 {
		t.Errorf("--edit-in-place must not create a .pre-fix sibling, found %v", matches)
	}
}

// preserveOriginal cleans up the partial sibling and skips the rename when the
// copy fails (here the "index" is a directory, so io.Copy from it errors) —
// the load-bearing NFR5 safety branch: a preservation failure leaves nothing
// half-written and never reaches the rename.
func TestPreserveOriginal_CopyFailureRemovesPartial(t *testing.T) {
	withStableSecond(t, func() bool {
		s1 := currentStamp()
		dir := t.TempDir()
		idxDir := filepath.Join(dir, "main.idx")
		if err := os.Mkdir(idxDir, 0755); err != nil {
			t.Fatal(err)
		}

		_, err := dircachefilehash.PreserveOriginal(idxDir)
		if currentStamp() != s1 {
			return false
		}
		if err == nil {
			t.Fatal("expected a copy failure when the source is a directory")
		}
		if matches := preFixSiblings(t, dir); len(matches) != 0 {
			t.Errorf("partial sibling not cleaned up after copy failure: %v", matches)
		}
		return true
	})
}

// promoteRepairedIndex surfaces a rename failure rather than masking it. In
// --edit-in-place mode preservation is skipped, so a missing temp source makes
// the rename the only fallible step.
func TestPromoteRepairedIndex_RenameFailure(t *testing.T) {
	dir := t.TempDir()
	idx := filepath.Join(dir, "main.idx")
	if err := os.WriteFile(idx, []byte("ORIG"), 0644); err != nil {
		t.Fatal(err)
	}
	missingTmp := filepath.Join(dir, "does-not-exist.tmp")

	if err := dircachefilehash.PromoteRepairedIndex(missingTmp, idx, fixFlags(optsWith(t, "--edit-in-place", "--force"))); err == nil {
		t.Fatal("expected a rename failure for a missing temp file")
	}
}

// promoteRepairedIndex returns the preservation error and does NOT rename when
// preservation fails in default mode — the canonical index is left intact, and
// the temp source survives because no rename ran (the NFR5 invariant at the
// helper boundary).
func TestPromoteRepairedIndex_PreserveFailureSkipsRename(t *testing.T) {
	withStableSecond(t, func() bool {
		s1 := currentStamp()
		dir := t.TempDir()
		idxDir := filepath.Join(dir, "main.idx") // a directory → the copy fails
		if err := os.Mkdir(idxDir, 0755); err != nil {
			t.Fatal(err)
		}
		tmp := filepath.Join(dir, "src.tmp")
		if err := os.WriteFile(tmp, []byte("NEW"), 0644); err != nil {
			t.Fatal(err)
		}

		err := dircachefilehash.PromoteRepairedIndex(tmp, idxDir, fixFlags(optsWith(t)))
		if currentStamp() != s1 {
			return false
		}
		if err == nil {
			t.Fatal("expected the preservation failure to propagate")
		}
		if _, serr := os.Stat(tmp); serr != nil {
			t.Errorf("temp file should remain — no rename may run after a failed preservation")
		}
		if info, serr := os.Stat(idxDir); serr != nil || !info.IsDir() {
			t.Errorf("canonical index must be left intact after a failed preservation")
		}
		return true
	})
}

// preFixSiblings returns all main.idx.pre-fix-* siblings in dir (the fixtures
// all name their index "main.idx").
func preFixSiblings(t *testing.T, dir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "main.idx.pre-fix-*"))
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

// findPreFixSibling asserts exactly one main.idx.pre-fix-* sibling exists in dir
// and returns it.
func findPreFixSibling(t *testing.T, dir string) string {
	t.Helper()
	matches := preFixSiblings(t, dir)
	if len(matches) != 1 {
		t.Fatalf("want exactly one .pre-fix sibling, found %d: %v", len(matches), matches)
	}
	return matches[0]
}
