package dircachefilehash

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"github.com/mattkeenan/dircachefilehash/pkg/format"
)

// Tests for the task 28.1 entry-writer correction (Approach A / design D3):
// the dcfhfix repair workflow now routes through the production single-writer
// path (TempIndexWriter + EntrySerialiser), so the produced index round-trips
// the full variable-length path and preserves the subject's checksum_type.

// validatedEntryFor builds a ValidatedEntry with the given path and per-entry
// hash type, plus distinct, deterministic metadata so a round-trip can be
// asserted field-by-field.
func validatedEntryFor(path string, hashType uint16) *ValidatedEntry {
	e := &binaryEntry{
		CTimeWall: 0x0123456789,
		MTimeWall: 0x9876543210,
		Dev:       0x1122334455667788,
		Ino:       0x8877665544332211,
		Mode:      0o100644,
		UID:       4242,
		GID:       2424,
		FileSize:  123456,
		HashType:  hashType,
	}
	for i := range e.Hash {
		e.Hash[i] = byte(i + 1)
	}
	return &ValidatedEntry{Entry: e, Path: path}
}

// headerOf reads the on-disk header of a produced index.
func headerOf(t *testing.T, path string) format.Header {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read produced index %q: %v", path, err)
	}
	if len(data) < format.HeaderSize {
		t.Fatalf("produced index %q too small: %d bytes", path, len(data))
	}
	return *(*format.Header)(unsafe.Pointer(&data[0]))
}

// writeSubjectInPlace writes entries to a fresh subject path via the corrected
// writer using --edit-in-place (no .pre-fix sibling, no pre-existing subject
// required — the rename creates it).
func writeSubjectInPlace(t *testing.T, path string, checksumType uint16, entries []*ValidatedEntry) {
	t.Helper()
	opts := FixEntryFlags{Quiet: true, EditInPlace: true, Force: true}
	if err := writeRepairedIndex(path, checksumType, entries, opts); err != nil {
		t.Fatalf("writeRepairedIndex: %v", err)
	}
}

// TC-2 — every entry's variable-length path round-trips byte-identically,
// including a multi-byte (CJK) path and a long path. This is the FR9 fix: the
// old O_APPEND writer dropped the path bytes entirely.
func TestWriter_VariableLengthPathRoundTrip(t *testing.T) {
	dir := t.TempDir()
	subject := filepath.Join(dir, "main.idx")

	// Index entries must be in sorted path order (the production layout).
	wantPaths := []string{
		strings.Repeat("deep/", 50) + "leaf.go", // long path
		"日本語/フォルダ/ファイル.txt",                     // multi-byte
		"simple.txt",
	}
	// Sort to match what a real index holds (byte order).
	sorted := append([]string(nil), wantPaths...)
	for i := range sorted {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j] < sorted[i] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	entries := make([]*ValidatedEntry, 0, len(sorted))
	for _, p := range sorted {
		entries = append(entries, validatedEntryFor(p, HashTypeSHA1))
	}
	writeSubjectInPlace(t, subject, HashTypeSHA1, entries)

	refs, err := validationMetaStore().LoadIndexFromFileForValidation(subject)
	if err != nil {
		t.Fatalf("re-read produced index: %v", err)
	}
	if len(refs) != len(sorted) {
		t.Fatalf("read %d entries, want %d", len(refs), len(sorted))
	}
	for i, ref := range refs {
		if got := ref.GetBinaryEntry().RelativePath(); got != sorted[i] {
			t.Errorf("entry %d path = %q, want %q (path not round-tripped)", i, got, sorted[i])
		}
	}
}

// TC-3 — the produced index preserves the subject's checksum_type, for each
// supported algorithm. A successful validation load also proves the footer
// checksum was finalised under that algorithm (TC-6).
func TestWriter_ChecksumTypePreserved(t *testing.T) {
	for _, ct := range []uint16{HashTypeSHA1, HashTypeSHA256, HashTypeSHA512} {
		t.Run(HashTypeName(ct), func(t *testing.T) {
			dir := t.TempDir()
			subject := filepath.Join(dir, "main.idx")
			entries := []*ValidatedEntry{validatedEntryFor("repo/file.go", ct)}
			writeSubjectInPlace(t, subject, ct, entries)

			h := headerOf(t, subject)
			if h.ChecksumType != ct {
				t.Errorf("produced ChecksumType = %d, want %d", h.ChecksumType, ct)
			}
			if h.Version != format.CurrentIndexVersion {
				t.Errorf("produced Version = %d, want %d", h.Version, format.CurrentIndexVersion)
			}
			// Footer checksum is verified on load; success proves it validates
			// under the preserved algorithm.
			if _, err := validationMetaStore().LoadIndexFromFileForValidation(subject); err != nil {
				t.Errorf("produced index failed validation load: %v", err)
			}
		})
	}
}

// TC-4 — newFixMetaStore refuses an unsupported checksum type before any write
// (guards against re-hashing under the wrong algorithm), and round-trips a
// supported one to the matching hash type.
func TestNewFixMetaStore_ChecksumTypeAssertion(t *testing.T) {
	if _, err := newFixMetaStore(t.TempDir(), 0xBEEF); err == nil {
		t.Error("expected an error for an unsupported checksum type")
	}
	for _, ct := range []uint16{HashTypeSHA1, HashTypeSHA256, HashTypeSHA512} {
		ms, err := newFixMetaStore(t.TempDir(), ct)
		if err != nil {
			t.Fatalf("newFixMetaStore(%d): %v", ct, err)
		}
		if got := ms.GetCurrentHashType(); got != ct {
			t.Errorf("synthesised hash type = %d, want %d", got, ct)
		}
	}
}

// TC-5 — a legacy v3 subject is deliberately upgraded to the current (v4)
// layout (read-old / write-new), preserving checksum_type, with every entry's
// metadata and path intact at the v4 offsets (Dev/Ino widened, no field shift).
func TestWriter_LegacyV3UpgradesToV4(t *testing.T) {
	dir := t.TempDir()
	subject := filepath.Join(dir, "main.idx")

	src, err := os.ReadFile(goldenV3)
	if err != nil {
		t.Fatalf("read v3 golden: %v", err)
	}
	srcHeader := *(*format.Header)(unsafe.Pointer(&src[0]))
	if srcHeader.Version != 3 {
		t.Fatalf("golden is not v3 (regenerate); got version %d", srcHeader.Version)
	}
	if err := os.WriteFile(subject, src, 0o644); err != nil {
		t.Fatal(err)
	}

	// Keep every entry (empty path set, no field edit); upgrade in place.
	opts := FixEntryFlags{Quiet: true, EditInPlace: true, Force: true}
	if _, _, err := ProcessEntriesWithWorkflow(subject, map[string]bool{}, "", "", opts); err != nil {
		t.Fatalf("repair v3 subject: %v", err)
	}

	h := headerOf(t, subject)
	if h.Version != format.CurrentIndexVersion {
		t.Errorf("upgraded Version = %d, want %d (v4)", h.Version, format.CurrentIndexVersion)
	}
	if h.ChecksumType != srcHeader.ChecksumType {
		t.Errorf("upgraded ChecksumType = %d, want %d (preserved from v3)", h.ChecksumType, srcHeader.ChecksumType)
	}

	// All three known entries re-read with paths intact at v4 offsets.
	wantPaths := []string{"file1.txt", "file2.txt", "sub/file3.txt"}
	refs, err := validationMetaStore().LoadIndexFromFileForValidation(subject)
	if err != nil {
		t.Fatalf("re-read upgraded index: %v", err)
	}
	if len(refs) != len(wantPaths) {
		t.Fatalf("read %d entries, want %d", len(refs), len(wantPaths))
	}
	for i, ref := range refs {
		if got := ref.GetBinaryEntry().RelativePath(); got != wantPaths[i] {
			t.Errorf("entry %d path = %q, want %q", i, got, wantPaths[i])
		}
	}
}

// TC-8 — forward-progress past a mid-stream corrupt entry is preserved by the
// relocated workflow: a recoverable corruption (the entry's Size field stays
// intact, so trySkipToNextEntry resyncs by size) is discarded and counted,
// while the valid entries before AND after it survive into the repaired index.
// This guards the fsck "assume corrupt, make forward progress" contract against
// regression from the relocation + single-writer rewrite.
func TestWorkflow_ForwardProgressPastCorruptEntry(t *testing.T) {
	dir := t.TempDir()
	subject := filepath.Join(dir, "main.idx")

	// Equal-length, sorted, unique paths so the middle path is unambiguous to
	// locate and corrupt by content.
	wantBefore, corrupt, wantAfter := "aaa.txt", "mmm.txt", "zzz.txt"
	entries := []*ValidatedEntry{
		validatedEntryFor(wantBefore, HashTypeSHA1),
		validatedEntryFor(corrupt, HashTypeSHA1),
		validatedEntryFor(wantAfter, HashTypeSHA1),
	}
	writeSubjectInPlace(t, subject, HashTypeSHA1, entries)

	// Corrupt ONLY the middle entry's path bytes (null them out). The Size field
	// is untouched, so NewValidatedEntry fails with "path is empty" but
	// trySkipToNextEntry advances cleanly by Size onto the next entry.
	data, err := os.ReadFile(subject)
	if err != nil {
		t.Fatal(err)
	}
	idx := bytes.Index(data, []byte(corrupt))
	if idx < 0 {
		t.Fatalf("could not locate middle path %q to corrupt", corrupt)
	}
	for i := 0; i < len(corrupt); i++ {
		data[i+idx] = 0
	}
	if err := os.WriteFile(subject, data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Repair with no field edits — the corrupt entry is discarded, survivors kept.
	opts := FixEntryFlags{Quiet: true, EditInPlace: true, Force: true}
	fixed, discarded, err := ProcessEntriesWithWorkflow(subject, map[string]bool{}, "", "", opts)
	if err != nil {
		t.Fatalf("repair with mid-stream corruption: %v", err)
	}
	if fixed != 0 {
		t.Errorf("entriesFixed = %d, want 0 (no field edits requested)", fixed)
	}
	if discarded != 1 {
		t.Errorf("entriesDiscarded = %d, want 1 (the corrupt middle entry)", discarded)
	}

	refs, err := validationMetaStore().LoadIndexFromFileForValidation(subject)
	if err != nil {
		t.Fatalf("re-read repaired index: %v", err)
	}
	got := make([]string, len(refs))
	for i, ref := range refs {
		got[i] = ref.GetBinaryEntry().RelativePath()
	}
	want := []string{wantBefore, wantAfter}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("survivors = %v, want %v (corrupt entry discarded, flanking entries kept)", got, want)
	}
}

// TC-7 — a failure before the rename leaves no partial index: the temp is
// removed and the subject is untouched. Here default-mode preservation fails
// (the subject is a directory, so the copy errors), exercising the
// no-partial-index cleanup in writeRepairedIndex.
func TestWriteRepairedIndex_AbortRemovesTemp(t *testing.T) {
	dir := t.TempDir()
	subjectDir := filepath.Join(dir, "main.idx") // a directory → PreserveOriginal copy fails
	if err := os.Mkdir(subjectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	entries := []*ValidatedEntry{validatedEntryFor("f.txt", HashTypeSHA1)}

	// Default mode (no EditInPlace) → PreserveOriginal runs and fails.
	err := writeRepairedIndex(subjectDir, HashTypeSHA1, entries, FixEntryFlags{Quiet: true})
	if err == nil {
		t.Fatal("expected writeRepairedIndex to fail when preservation fails")
	}
	if _, serr := os.Stat(subjectDir + ".fix.tmp"); !os.IsNotExist(serr) {
		t.Errorf("temp index was not removed after abort")
	}
	if info, serr := os.Stat(subjectDir); serr != nil || !info.IsDir() {
		t.Errorf("subject must be untouched after a failed repair")
	}
}
