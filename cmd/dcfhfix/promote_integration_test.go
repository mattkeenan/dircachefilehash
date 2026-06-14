package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dircachefilehash "github.com/mattkeenan/dircachefilehash/pkg"
)

// captureOutput redirects os.Stdout/os.Stderr around fn and returns what each
// received. Used for the message/quiet matrix (the promote helpers print
// directly to the process streams).
func captureOutput(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	origOut, origErr := os.Stdout, os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = wOut, wErr
	defer func() { os.Stdout, os.Stderr = origOut, origErr }()

	fn()

	_ = wOut.Close()
	_ = wErr.Close()
	var bufOut, bufErr bytes.Buffer
	_, _ = io.Copy(&bufOut, rOut)
	_, _ = io.Copy(&bufErr, rErr)
	return bufOut.String(), bufErr.String()
}

// fixtureEntryPath is the single entry laid down by writeFixtureIndex.
const fixtureEntryPath = "repo/file.go"

// writeFixtureIndex lays a valid one-entry index (header + entry) at path using
// the same wire-layout helpers as writepath_test.go.
func writeFixtureIndex(t *testing.T, path string) []byte {
	t.Helper()
	entry := layEntryBytes(fixtureEntryPath, 0x11223344, 0x55667788)
	data := append(layHeaderBytes(1), entry...)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	return data
}

// TC-I (entry remove): default mode preserves a byte-identical sibling and
// writes the repaired index to the canonical path; --edit-in-place suppresses
// the sibling. Backups are disabled to isolate the sibling behaviour.
func TestEntryRemove_DefaultPreservesSibling(t *testing.T) {
	dir := t.TempDir()
	idx := filepath.Join(dir, "main.idx")
	preRun := writeFixtureIndex(t, idx)

	if err := entryRemove(idx, []string{"repo/file.go"}, optsWith(t, "--backup=false", "--quiet")); err != nil {
		t.Fatalf("entryRemove: %v", err)
	}

	// Canonical index changed (the entry was removed → fewer bytes).
	repaired, err := os.ReadFile(idx)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(repaired, preRun) {
		t.Errorf("canonical index unchanged; expected the entry to be removed")
	}

	// Exactly one preserved sibling, byte-identical to the pre-run index.
	sib := findPreFixSibling(t, dir)
	got, err := os.ReadFile(sib)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, preRun) {
		t.Errorf("preserved sibling not byte-identical to the pre-run index")
	}
}

func TestEntryRemove_EditInPlaceSuppressesSibling(t *testing.T) {
	dir := t.TempDir()
	idx := filepath.Join(dir, "main.idx")
	writeFixtureIndex(t, idx)

	if err := entryRemove(idx, []string{"repo/file.go"}, optsWith(t, "--backup=false", "--quiet", "--edit-in-place", "--force")); err != nil {
		t.Fatalf("entryRemove: %v", err)
	}
	if matches := preFixSiblings(t, dir); len(matches) != 0 {
		t.Errorf("--edit-in-place must not create a .pre-fix sibling, found %v", matches)
	}
}

// TC-I5 (entry remove): --dry-run writes nothing — no canonical rewrite, no
// sibling — and still reports the intended preservation.
func TestEntryRemove_DryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	idx := filepath.Join(dir, "main.idx")
	preRun := writeFixtureIndex(t, idx)

	if err := entryRemove(idx, []string{"repo/file.go"}, optsWith(t, "--backup=false", "--dry-run")); err != nil {
		t.Fatalf("entryRemove dry-run: %v", err)
	}
	if got, _ := os.ReadFile(idx); !bytes.Equal(got, preRun) {
		t.Errorf("dry-run modified the canonical index")
	}
	if matches := preFixSiblings(t, dir); len(matches) != 0 {
		t.Errorf("dry-run created a sibling: %v", matches)
	}
}

// The dispatch chokepoint refuses a lone --edit-in-place (no --force) before any
// write path runs, leaving the filesystem untouched.
func TestDispatchCommand_GateRefusesLoneEditInPlace(t *testing.T) {
	dir := t.TempDir()
	idx := filepath.Join(dir, "main.idx")
	preRun := writeFixtureIndex(t, idx)

	err := dispatchCommand("entry", idx, []string{"remove", "repo/file.go"}, optsWith(t, "--backup=false", "--edit-in-place"))
	if err == nil {
		t.Fatal("expected gate to refuse lone --edit-in-place")
	}
	if got, _ := os.ReadFile(idx); !bytes.Equal(got, preRun) {
		t.Errorf("filesystem changed despite gate refusal")
	}
	if matches := preFixSiblings(t, dir); len(matches) != 0 {
		t.Errorf("gate refusal still created a sibling: %v", matches)
	}
}

// TC-I3 — `--force` alone (no --edit-in-place) still preserves the sibling.
func TestEntryRemove_ForceAlonePreservesSibling(t *testing.T) {
	dir := t.TempDir()
	idx := filepath.Join(dir, "main.idx")
	preRun := writeFixtureIndex(t, idx)

	if err := entryRemove(idx, []string{fixtureEntryPath}, optsWith(t, "--backup=false", "--quiet", "--force")); err != nil {
		t.Fatalf("entryRemove: %v", err)
	}
	if got, _ := os.ReadFile(idx); bytes.Equal(got, preRun) {
		t.Errorf("canonical index unchanged; expected the entry to be removed")
	}
	sib := findPreFixSibling(t, dir)
	if got, _ := os.ReadFile(sib); !bytes.Equal(got, preRun) {
		t.Errorf("--force alone must still preserve a byte-identical sibling")
	}
}

// TC-I7 (FR5): with backups enabled (the default), a default-mode repair still
// populates the FIFO `fixes` stack *and* writes the new .pre-fix sibling — the
// two safety mechanisms coexist.
func TestEntryRemove_BackupStackCoexistsWithSibling(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".dcfh"), 0755); err != nil {
		t.Fatal(err)
	}
	idx := filepath.Join(dir, "main.idx")
	writeFixtureIndex(t, idx)

	// No --backup flag => backups enabled (dcfhfix default).
	if err := entryRemove(idx, []string{fixtureEntryPath}, optsWith(t, "--quiet")); err != nil {
		t.Fatalf("entryRemove: %v", err)
	}

	_ = findPreFixSibling(t, dir) // sibling present
	backups, err := filepath.Glob(filepath.Join(dir, ".dcfh", "fixes", "main", "*.idx"))
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) == 0 {
		t.Errorf("expected a fixes-stack backup alongside the sibling (FR5)")
	}
}

// TC-I1 (header edit path): default mode preserves the sibling. The header path
// is the one site whose options param was previously discarded (`_`), so this
// exercises the un-blanked wiring end-to-end.
func TestHeaderEdit_DefaultPreservesSibling(t *testing.T) {
	dir := t.TempDir()
	idx := filepath.Join(dir, "main.idx")
	preRun := writeFixtureIndex(t, idx)

	if err := headerEdit(idx, "flags", "0", optsWith(t, "--backup=false", "--quiet")); err != nil {
		t.Fatalf("headerEdit: %v", err)
	}
	if got, _ := os.ReadFile(idx); bytes.Equal(got, preRun) {
		t.Errorf("canonical index unchanged; expected the flags edit")
	}
	sib := findPreFixSibling(t, dir)
	if got, _ := os.ReadFile(sib); !bytes.Equal(got, preRun) {
		t.Errorf("preserved sibling not byte-identical to the pre-run index")
	}
}

func TestHeaderEdit_EditInPlaceSuppressesSibling(t *testing.T) {
	dir := t.TempDir()
	idx := filepath.Join(dir, "main.idx")
	writeFixtureIndex(t, idx)

	if err := headerEdit(idx, "flags", "0", optsWith(t, "--backup=false", "--quiet", "--edit-in-place", "--force")); err != nil {
		t.Fatalf("headerEdit: %v", err)
	}
	if matches := preFixSiblings(t, dir); len(matches) != 0 {
		t.Errorf("--edit-in-place must not create a .pre-fix sibling, found %v", matches)
	}
}

// TC-I1 (entry edit path): default mode preserves the sibling.
func TestEntryEdit_DefaultPreservesSibling(t *testing.T) {
	dir := t.TempDir()
	idx := filepath.Join(dir, "main.idx")
	preRun := writeFixtureIndex(t, idx)

	if err := entryEdit(idx, "uid", "4242", []string{fixtureEntryPath}, optsWith(t, "--backup=false", "--quiet")); err != nil {
		t.Fatalf("entryEdit: %v", err)
	}
	if got, _ := os.ReadFile(idx); bytes.Equal(got, preRun) {
		t.Errorf("canonical index unchanged; expected the uid edit")
	}
	sib := findPreFixSibling(t, dir)
	if got, _ := os.ReadFile(sib); !bytes.Equal(got, preRun) {
		t.Errorf("preserved sibling not byte-identical to the pre-run index")
	}
}

// TC-I1 (entry append path): default mode preserves the sibling.
func TestEntryAppend_DefaultPreservesSibling(t *testing.T) {
	dir := t.TempDir()
	idx := filepath.Join(dir, "main.idx")
	preRun := writeFixtureIndex(t, idx)

	entryJSON := `{"path":"new/added.go","hash":"0000000000000000000000000000000000000000","mtime":"0","ctime":"0","hash_type":1}`
	if err := entryAppend(idx, entryJSON, optsWith(t, "--backup=false", "--quiet")); err != nil {
		t.Fatalf("entryAppend: %v", err)
	}
	if got, _ := os.ReadFile(idx); bytes.Equal(got, preRun) {
		t.Errorf("canonical index unchanged; expected the appended entry")
	}
	sib := findPreFixSibling(t, dir)
	if got, _ := os.ReadFile(sib); !bytes.Equal(got, preRun) {
		t.Errorf("preserved sibling not byte-identical to the pre-run index")
	}
}

// TC-A1 — message/quiet matrix. promoteRepairedIndex owns both messages: the
// routine preservation notice obeys --quiet; the destructive in-place warning
// does not.
func TestPromoteRepairedIndex_MessageQuietMatrix(t *testing.T) {
	const preserved = "Original preserved at"
	const destructive = "--edit-in-place overwrites"

	cases := []struct {
		name          string
		flags         []string
		wantStdoutHas string // "" => assert absent
		wantStderrHas string // "" => assert absent
	}{
		{"default notice on stdout", []string{}, preserved, ""},
		{"default notice suppressed by --quiet", []string{"--quiet"}, "", ""},
		{"in-place warning on stderr", []string{"--edit-in-place", "--force"}, "", destructive},
		{"in-place warning survives --quiet", []string{"--edit-in-place", "--force", "--quiet"}, "", destructive},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			idx := filepath.Join(dir, "main.idx")
			tmp := idx + ".tmp"
			if err := os.WriteFile(idx, []byte("ORIG"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(tmp, []byte("NEW"), 0644); err != nil {
				t.Fatal(err)
			}

			var perr error
			stdout, stderr := captureOutput(t, func() {
				perr = dircachefilehash.PromoteRepairedIndex(tmp, idx, fixFlags(optsWith(t, tc.flags...)))
			})
			if perr != nil {
				t.Fatalf("promoteRepairedIndex: %v", perr)
			}

			assertContains(t, "stdout", stdout, tc.wantStdoutHas)
			assertContains(t, "stderr", stderr, tc.wantStderrHas)
			// The preservation notice must never leak onto stderr, nor the
			// destructive warning onto stdout.
			if strings.Contains(stdout, destructive) {
				t.Errorf("destructive warning leaked onto stdout: %q", stdout)
			}
			if strings.Contains(stderr, preserved) {
				t.Errorf("preservation notice leaked onto stderr: %q", stderr)
			}
		})
	}
}

// TC-I6 — dry-run + destructive preview: reportDryRunPreservation warns on
// stderr and emits no "would preserve" line; nothing is written.
func TestEntryRemove_DryRunDestructivePreview(t *testing.T) {
	dir := t.TempDir()
	idx := filepath.Join(dir, "main.idx")
	preRun := writeFixtureIndex(t, idx)

	var rerr error
	stdout, stderr := captureOutput(t, func() {
		rerr = entryRemove(idx, []string{fixtureEntryPath},
			optsWith(t, "--backup=false", "--dry-run", "--edit-in-place", "--force"))
	})
	if rerr != nil {
		t.Fatalf("entryRemove dry-run: %v", rerr)
	}
	if !strings.Contains(stderr, "would overwrite") {
		t.Errorf("expected destructive preview on stderr, got %q", stderr)
	}
	if strings.Contains(stdout, "Would preserve") {
		t.Errorf("destructive dry-run must not print a 'Would preserve' line: %q", stdout)
	}
	if got, _ := os.ReadFile(idx); !bytes.Equal(got, preRun) {
		t.Errorf("dry-run modified the canonical index")
	}
	if matches := preFixSiblings(t, dir); len(matches) != 0 {
		t.Errorf("dry-run created a sibling: %v", matches)
	}
}

// assertContains asserts want is present in got (want=="" => assert absent).
func assertContains(t *testing.T, stream, got, want string) {
	t.Helper()
	if want == "" {
		return
	}
	if !strings.Contains(got, want) {
		t.Errorf("%s = %q, want it to contain %q", stream, got, want)
	}
}
