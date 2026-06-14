package dircachefilehash

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"unsafe"
)

// Unit tests for the task 28.3 multi-source recovery merge core
// (mergeSourcesIntoEntries + orderedSourcePaths). These exercise the merge in
// isolation against crafted .idx fixtures: union by path, precedence keep-first,
// truncation tolerance, cross-source tombstone suppression, output ordering,
// mixed checksum-type skip, the empty/all-deleted cases, and read-source
// confinement. The recovery branch through Repo.Fix is covered in
// fix_run_test.go.

// recoveryMetaDir creates an empty .dcfh metadata directory and returns its path.
func recoveryMetaDir(t *testing.T) string {
	t.Helper()
	metaDir := filepath.Join(t.TempDir(), ".dcfh")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return metaDir
}

// writeSource writes entries (sorted by path) to metaDir/name under checksumType
// and returns the full path. The entries are sorted here so each source mirrors
// the production ascending-path layout.
func writeSource(t *testing.T, metaDir, name string, checksumType uint16, entries []*ValidatedEntry) string {
	t.Helper()
	sorted := append([]*ValidatedEntry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	path := filepath.Join(metaDir, name)
	writeSubjectInPlace(t, path, checksumType, sorted)
	return path
}

// deletedEntryFor builds a validated entry whose deleted flag is set.
func deletedEntryFor(path string) *ValidatedEntry {
	ve := validatedEntryFor(path, HashTypeSHA1)
	ve.Entry.SetDeleted()
	return ve
}

// entryWithSize builds a validated entry with a distinguishing FileSize so a
// precedence winner can be identified after the merge.
func entryWithSize(path string, hashType uint16, size int64) *ValidatedEntry {
	ve := validatedEntryFor(path, hashType)
	ve.Entry.FileSize = size
	return ve
}

// truncateLastEntry cuts the final entry of an index in half (removing the
// footer too), so the tolerant walk keeps the readable prefix and discards the
// truncated tail. Returns the surviving (readable) entry count.
func truncateLastEntry(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	header := (*indexHeader)(unsafe.Pointer(&data[0]))
	hdrSize := HeaderSizeForVersion(header.Version)
	count := int(header.EntryCount)
	if count < 1 {
		t.Fatalf("need >=1 entry to truncate, got %d", count)
	}
	off := hdrSize
	for i := 0; i < count-1; i++ {
		off += int(*(*uint32)(unsafe.Pointer(&data[off])))
	}
	lastSz := int(*(*uint32)(unsafe.Pointer(&data[off])))
	if err := os.Truncate(path, int64(off+lastSz/2)); err != nil {
		t.Fatal(err)
	}
	return count - 1
}

var mergeQuiet = FixEntryFlags{Quiet: true}

// pathsOf returns the merged entries' paths in order.
func pathsOf(entries []*ValidatedEntry) []string {
	out := make([]string, len(entries))
	for i, ve := range entries {
		out[i] = ve.Path
	}
	return out
}

// TC-1 — union by path: distinct paths across three sources merge to their union
// with no discards.
func TestMerge_UnionByPath(t *testing.T) {
	dir := recoveryMetaDir(t)
	m := writeSource(t, dir, MainIndex, HashTypeSHA256, []*ValidatedEntry{validatedEntryFor("m.txt", HashTypeSHA1)})
	c := writeSource(t, dir, CacheIndex, HashTypeSHA256, []*ValidatedEntry{validatedEntryFor("c.txt", HashTypeSHA1)})
	ts := writeSource(t, dir, "cache-20240101T000000Z.idx", HashTypeSHA256, []*ValidatedEntry{validatedEntryFor("t.txt", HashTypeSHA1)})

	merged, ct, discarded, contributing := mergeSourcesIntoEntries([]string{ts, c, m}, mergeQuiet)
	if got := pathsOf(merged); len(got) != 3 || got[0] != "c.txt" || got[1] != "m.txt" || got[2] != "t.txt" {
		t.Errorf("merged paths = %v, want [c.txt m.txt t.txt]", got)
	}
	if discarded != 0 {
		t.Errorf("discarded = %d, want 0", discarded)
	}
	if ct != HashTypeSHA256 {
		t.Errorf("checksumType = %d, want %d", ct, HashTypeSHA256)
	}
	if len(contributing) != 3 {
		t.Errorf("contributing = %d sources, want 3", len(contributing))
	}
}

// TC-2 — precedence keep-first + determinism: the same path in main and a
// higher-precedence timestamped cache resolves to the timestamped entry,
// regardless of the named-selector order; the main copy is a conflict-loser.
func TestMerge_PrecedenceAndDeterminism(t *testing.T) {
	dir := recoveryMetaDir(t)
	writeSource(t, dir, MainIndex, HashTypeSHA256, []*ValidatedEntry{entryWithSize("dup.txt", HashTypeSHA1, 111)})
	writeSource(t, dir, "cache-20240101T000000Z.idx", HashTypeSHA256, []*ValidatedEntry{entryWithSize("dup.txt", HashTypeSHA1, 999)})

	run := func(named []string) (int64, int) {
		ordered, err := orderedSourcePaths(dir, named)
		if err != nil {
			t.Fatalf("orderedSourcePaths: %v", err)
		}
		merged, _, discarded, _ := mergeSourcesIntoEntries(ordered, mergeQuiet)
		if len(merged) != 1 {
			t.Fatalf("merged = %d entries, want 1", len(merged))
		}
		return merged[0].Entry.FileSize, discarded
	}

	mainPath := filepath.Join(dir, MainIndex)
	size1, disc1 := run([]string{mainPath}) // timestamped auto-discovered
	size2, disc2 := run([]string{mainPath}) // identical inputs, second run
	size3, disc3 := run([]string{mainPath, mainPath})

	if size1 != 999 {
		t.Errorf("winner FileSize = %d, want 999 (timestamped cache outranks main)", size1)
	}
	if size1 != size2 || size2 != size3 {
		t.Errorf("non-deterministic winner: %d/%d/%d", size1, size2, size3)
	}
	if disc1 != 1 || disc1 != disc2 || disc2 != disc3 {
		t.Errorf("conflict-loser discard not deterministic/1: %d/%d/%d", disc1, disc2, disc3)
	}
}

// TC-3 — a truncated-body source keeps its readable validated prefix with a
// concrete asserted count; the truncated tail is one discard.
func TestMerge_TruncatedSourcePrefix(t *testing.T) {
	dir := recoveryMetaDir(t)
	src := writeSource(t, dir, CacheIndex, HashTypeSHA256, []*ValidatedEntry{
		validatedEntryFor("a.txt", HashTypeSHA1),
		validatedEntryFor("b.txt", HashTypeSHA1),
		validatedEntryFor("c.txt", HashTypeSHA1),
	})
	want := truncateLastEntry(t, src) // 2 survivors

	merged, _, discarded, _ := mergeSourcesIntoEntries([]string{src}, mergeQuiet)
	if len(merged) != want {
		t.Errorf("survivors = %d, want %d (readable prefix)", len(merged), want)
	}
	if discarded != 1 {
		t.Errorf("discarded = %d, want 1 (truncated tail entry)", discarded)
	}
}

// TC-4 — cross-source tombstone suppression: a path deleted in the higher-
// precedence source wins the conflict, then is filtered from the output; the
// live lower-precedence copy is a conflict-loser. The path is absent.
func TestMerge_TombstoneSuppression(t *testing.T) {
	dir := recoveryMetaDir(t)
	cache := writeSource(t, dir, CacheIndex, HashTypeSHA256, []*ValidatedEntry{deletedEntryFor("x.txt")})
	main := writeSource(t, dir, MainIndex, HashTypeSHA256, []*ValidatedEntry{
		validatedEntryFor("x.txt", HashTypeSHA1),
		validatedEntryFor("y.txt", HashTypeSHA1),
	})

	merged, _, discarded, _ := mergeSourcesIntoEntries([]string{cache, main}, mergeQuiet)
	if got := pathsOf(merged); len(got) != 1 || got[0] != "y.txt" {
		t.Errorf("merged paths = %v, want [y.txt] (x.txt suppressed by tombstone)", got)
	}
	if discarded != 2 {
		t.Errorf("discarded = %d, want 2 (live x.txt conflict-loser + deleted x.txt filter)", discarded)
	}
}

// TC-5 — survivors are sorted ascending by path even when the source union is
// not globally ordered.
func TestMerge_OutputSorted(t *testing.T) {
	dir := recoveryMetaDir(t)
	a := writeSource(t, dir, CacheIndex, HashTypeSHA256, []*ValidatedEntry{
		validatedEntryFor("b.txt", HashTypeSHA1),
		validatedEntryFor("d.txt", HashTypeSHA1),
	})
	b := writeSource(t, dir, MainIndex, HashTypeSHA256, []*ValidatedEntry{
		validatedEntryFor("a.txt", HashTypeSHA1),
		validatedEntryFor("c.txt", HashTypeSHA1),
	})

	merged, _, _, _ := mergeSourcesIntoEntries([]string{a, b}, mergeQuiet)
	got := pathsOf(merged)
	if len(got) != 4 || got[0] != "a.txt" || got[1] != "b.txt" || got[2] != "c.txt" || got[3] != "d.txt" {
		t.Errorf("merged paths = %v, want ascending [a b c d].txt", got)
	}
}

// TC-6 — a source whose header checksum_type differs from the established
// (highest-precedence contributing) type is skipped with its entries counted as
// discards; the merge still succeeds from the agreeing source and the output
// type is the established one. No re-hash, no abort.
func TestMerge_MixedChecksumTypeSkipped(t *testing.T) {
	dir := recoveryMetaDir(t)
	// cache.idx (SHA256) outranks main.idx (SHA512): cache establishes the type;
	// main disagrees and is skipped.
	cache := writeSource(t, dir, CacheIndex, HashTypeSHA256, []*ValidatedEntry{validatedEntryFor("keep.txt", HashTypeSHA1)})
	main := writeSource(t, dir, MainIndex, HashTypeSHA512, []*ValidatedEntry{
		validatedEntryFor("dropped1.txt", HashTypeSHA1),
		validatedEntryFor("dropped2.txt", HashTypeSHA1),
	})

	merged, ct, discarded, contributing := mergeSourcesIntoEntries([]string{cache, main}, mergeQuiet)
	if got := pathsOf(merged); len(got) != 1 || got[0] != "keep.txt" {
		t.Errorf("merged paths = %v, want [keep.txt]", got)
	}
	if ct != HashTypeSHA256 {
		t.Errorf("checksumType = %d, want %d (the agreeing, higher-precedence source)", ct, HashTypeSHA256)
	}
	if discarded != 2 {
		t.Errorf("discarded = %d, want 2 (the skipped SHA512 source's entries)", discarded)
	}
	if len(contributing) != 1 || filepath.Base(contributing[0]) != CacheIndex {
		t.Errorf("contributing = %v, want only cache.idx", contributing)
	}
}

// TC-7 — empty / all-deleted inputs merge to an empty set.
func TestMerge_EmptyAndAllDeleted(t *testing.T) {
	t.Run("no sources", func(t *testing.T) {
		merged, _, discarded, _ := mergeSourcesIntoEntries(nil, mergeQuiet)
		if len(merged) != 0 || discarded != 0 {
			t.Errorf("merged=%d discarded=%d, want 0/0", len(merged), discarded)
		}
	})
	t.Run("all deleted", func(t *testing.T) {
		dir := recoveryMetaDir(t)
		src := writeSource(t, dir, CacheIndex, HashTypeSHA256, []*ValidatedEntry{
			deletedEntryFor("a.txt"),
			deletedEntryFor("b.txt"),
		})
		merged, _, discarded, _ := mergeSourcesIntoEntries([]string{src}, mergeQuiet)
		if len(merged) != 0 {
			t.Errorf("merged = %d, want 0 (all tombstones filtered)", len(merged))
		}
		if discarded != 2 {
			t.Errorf("discarded = %d, want 2", discarded)
		}
	})
}

// TC-8 — a named source path resolving outside MetaDir is rejected by
// orderedSourcePaths before any read.
func TestOrderedSourcePaths_RejectsOutOfMetaDir(t *testing.T) {
	dir := recoveryMetaDir(t)
	outside := filepath.Join(t.TempDir(), "victim.idx")
	if err := os.WriteFile(outside, []byte("not an index"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := orderedSourcePaths(dir, []string{outside})
	if err == nil {
		t.Fatal("expected confinement rejection for out-of-MetaDir source, got nil")
	}
}
