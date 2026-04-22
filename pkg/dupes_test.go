package dircachefilehash

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// setupDupesRepo writes each (relpath → content) pair under a fresh
// temp dir, runs Update to populate main.idx, and returns an open
// DirectoryCache ready for FindDuplicates. Tests close it themselves.
func setupDupesRepo(t *testing.T, files map[string]string) *DirectoryCache {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
	dc := NewDirectoryCache(root, filepath.Join(root, ".dcfh"))
	if err := dc.Update(context.Background(), map[string]string{}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	return dc
}

func TestFindDuplicates_RealDuplicates(t *testing.T) {
	dc := setupDupesRepo(t, map[string]string{
		"a.txt":     "shared A",
		"b.txt":     "shared A",
		"sub/c.txt": "shared A",
		"d.txt":     "shared B",
		"sub/e.txt": "shared B",
		"f.txt":     "unique",
		"sub/g.txt": "another unique",
	})
	defer func() { _ = dc.Close() }()

	groups, err := dc.FindDuplicates(context.Background(), map[string]string{}, nil, true)
	if err != nil {
		t.Fatalf("FindDuplicates: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("want 2 duplicate groups, got %d: %+v", len(groups), groups)
	}
	// Groups must be sorted by hash for determinism.
	if groups[0].Hash > groups[1].Hash {
		t.Errorf("groups not sorted by hash: %q then %q", groups[0].Hash, groups[1].Hash)
	}
	byContent := map[string][]string{
		"shared A": {"a.txt", "b.txt", "sub/c.txt"},
		"shared B": {"d.txt", "sub/e.txt"},
	}
	matched := 0
	for _, g := range groups {
		for _, want := range byContent {
			if len(g.Files) == len(want) && slices.Equal(g.Files, want) {
				matched++
				break
			}
		}
		// Files within a group must be in path-sorted order
		// (skiplist iteration is path-sorted, so no per-group sort).
		if !slices.IsSorted(g.Files) {
			t.Errorf("group %q files not sorted: %v", g.Hash, g.Files)
		}
		if g.Count != len(g.Files) {
			t.Errorf("Count %d != len(Files) %d", g.Count, len(g.Files))
		}
	}
	if matched != 2 {
		t.Errorf("group contents mismatch; groups=%+v", groups)
	}
}

func TestFindDuplicates_NoDuplicates(t *testing.T) {
	dc := setupDupesRepo(t, map[string]string{
		"a": "one",
		"b": "two",
		"c": "three",
	})
	defer func() { _ = dc.Close() }()

	groups, err := dc.FindDuplicates(context.Background(), map[string]string{}, nil, true)
	if err != nil {
		t.Fatalf("FindDuplicates: %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("want 0 groups, got %d: %+v", len(groups), groups)
	}
}

func TestFindDuplicates_ContextCancellation(t *testing.T) {
	dc := setupDupesRepo(t, map[string]string{
		"a": "x", "b": "x", "c": "x",
	})
	defer func() { _ = dc.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before the call
	_, err := dc.FindDuplicates(ctx, map[string]string{}, nil, true)
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
}

// dupesPathFilterFixture seeds a repo with three dupe groups spread
// across a/, b/, c/ so each test can pick which prefixes to pass.
//
//	group1: a/x, a/y, b/x    (cross-dir)
//	group2: b/y, c/x         (cross-dir, no member in a/)
//	group3: c/y, c/z         (entirely inside c/)
func dupesPathFilterFixture(t *testing.T) *DirectoryCache {
	t.Helper()
	return setupDupesRepo(t, map[string]string{
		"a/x": "g1", "a/y": "g1", "b/x": "g1",
		"b/y": "g2", "c/x": "g2",
		"c/y": "g3", "c/z": "g3",
		"a/solo": "unique-a",
		"b/solo": "unique-b",
	})
}

func groupFiles(groups []DuplicateGroup) [][]string {
	out := make([][]string, len(groups))
	for i, g := range groups {
		out[i] = g.Files
	}
	return out
}

func TestFindDuplicates_PathFilter_ZeroPaths(t *testing.T) {
	dc := dupesPathFilterFixture(t)
	defer func() { _ = dc.Close() }()

	groups, err := dc.FindDuplicates(context.Background(), map[string]string{}, nil, true)
	if err != nil {
		t.Fatalf("FindDuplicates: %v", err)
	}
	if len(groups) != 3 {
		t.Fatalf("want 3 groups (whole repo), got %d: %+v", len(groups), groups)
	}
}

func TestFindDuplicates_PathFilter_ExclusiveOneDir(t *testing.T) {
	dc := dupesPathFilterFixture(t)
	defer func() { _ = dc.Close() }()

	// Only c/ — group3 is fully inside, group1 has members outside c/
	// so its in-c/ count drops to 0, group2 drops to a singleton.
	groups, err := dc.FindDuplicates(context.Background(), map[string]string{}, []string{"c/"}, true)
	if err != nil {
		t.Fatalf("FindDuplicates: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("want 1 group, got %d: %v", len(groups), groupFiles(groups))
	}
	if !slices.Equal(groups[0].Files, []string{"c/y", "c/z"}) {
		t.Errorf("want [c/y c/z], got %v", groups[0].Files)
	}
}

func TestFindDuplicates_PathFilter_ExclusiveTwoDirs(t *testing.T) {
	dc := dupesPathFilterFixture(t)
	defer func() { _ = dc.Close() }()

	// a/ ∪ c/. group1 loses its b/x member → still dup (a/x,a/y).
	// group2 loses a/… (none), keeps c/x only → singleton, dropped.
	// group3 stays.
	groups, err := dc.FindDuplicates(context.Background(), map[string]string{}, []string{"a/", "c/"}, true)
	if err != nil {
		t.Fatalf("FindDuplicates: %v", err)
	}
	got := groupFiles(groups)
	want := [][]string{{"a/x", "a/y"}, {"c/y", "c/z"}}
	// Order by Hash is stable but test-independent; match as set.
	if len(got) != len(want) {
		t.Fatalf("want %d groups, got %d: %v", len(want), len(got), got)
	}
	for _, w := range want {
		found := false
		for _, g := range got {
			if slices.Equal(g, w) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing group %v in %v", w, got)
		}
	}
}

func TestFindDuplicates_PathFilter_NonExclusive(t *testing.T) {
	dc := dupesPathFilterFixture(t)
	defer func() { _ = dc.Close() }()

	// --exclusive=no with a/: cross-dir group1 (has a/x,a/y,b/x) is
	// reported in full; group2 has no member in a/ so it's dropped;
	// group3 has no member in a/ so it's dropped.
	groups, err := dc.FindDuplicates(context.Background(), map[string]string{}, []string{"a/"}, false)
	if err != nil {
		t.Fatalf("FindDuplicates: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("want 1 group, got %d: %v", len(groups), groupFiles(groups))
	}
	if !slices.Equal(groups[0].Files, []string{"a/x", "a/y", "b/x"}) {
		t.Errorf("want [a/x a/y b/x], got %v", groups[0].Files)
	}
}

func TestDuplicateGroup_Fields(t *testing.T) {
	group := DuplicateGroup{
		Hash:  "abc123def456",
		Files: []string{"file1.txt", "file2.txt", "dir/file3.txt"},
		Count: 3,
	}

	if group.Hash != "abc123def456" {
		t.Errorf("Expected hash 'abc123def456', got '%s'", group.Hash)
	}

	if len(group.Files) != 3 {
		t.Errorf("Expected 3 files, got %d", len(group.Files))
	}

	if group.Count != 3 {
		t.Errorf("Expected count 3, got %d", group.Count)
	}

	expectedFiles := []string{"file1.txt", "file2.txt", "dir/file3.txt"}
	for i, expected := range expectedFiles {
		if group.Files[i] != expected {
			t.Errorf("Expected file[%d] '%s', got '%s'", i, expected, group.Files[i])
		}
	}
}

func TestDirectoryCache_FindDuplicates_EmptyIndex(t *testing.T) {
	// Create temporary directory for testing
	tempDir := t.TempDir()

	// Create DirectoryCache instance
	dc := NewDirectoryCache(tempDir, tempDir)
	defer func() { _ = dc.Close() }()

	// Create empty index
	if err := dc.createEmptyIndex(); err != nil {
		t.Fatalf("Failed to create empty index: %v", err)
	}

	// Test FindDuplicates with empty flags
	flags := map[string]string{}
	duplicates, err := dc.FindDuplicates(context.Background(), flags, nil, true)
	if err != nil {
		t.Fatalf("FindDuplicates failed: %v", err)
	}

	if len(duplicates) != 0 {
		t.Errorf("Expected no duplicates in empty index, got %d", len(duplicates))
	}

	// Report string copy stats
	copies, accesses, rate := GetStringCopyStats()
	t.Logf("String copy stats: %d copies out of %d accesses (%.2f%% copy rate)", copies, accesses, rate)
}

func TestDirectoryCache_FindDuplicates_WithFlags(t *testing.T) {
	// Create temporary directory for testing
	tempDir := t.TempDir()

	// Create DirectoryCache instance
	dc := NewDirectoryCache(tempDir, tempDir)
	defer func() { _ = dc.Close() }()

	// Create empty index
	if err := dc.createEmptyIndex(); err != nil {
		t.Fatalf("Failed to create empty index: %v", err)
	}

	// Test different flag combinations
	testFlags := []map[string]string{
		{},                 // No flags
		{"v": "1"},         // Verbose level 1
		{"v": "2"},         // Verbose level 2
		{"other": "value"}, // Other flags
	}

	for i, flags := range testFlags {
		t.Run("flags_test_"+string(rune(i+'0')), func(t *testing.T) {
			duplicates, err := dc.FindDuplicates(context.Background(), flags, nil, true)
			if err != nil {
				t.Fatalf("FindDuplicates failed with flags %v: %v", flags, err)
			}

			// With empty index, should always return no duplicates
			if len(duplicates) != 0 {
				t.Errorf("Expected no duplicates with flags %v, got %d", flags, len(duplicates))
			}
		})
	}
}

// Mock test for duplicate detection logic (would need more complex setup for real testing)
func TestDuplicateGroup_CreationAndValidation(t *testing.T) {
	// Test creating a duplicate group
	files := []string{
		"documents/file1.txt",
		"backup/file1_copy.txt",
		"archive/old_file1.txt",
	}

	group := DuplicateGroup{
		Hash:  "sha256:abcdef123456789",
		Files: files,
		Count: len(files),
	}

	// Validate the group
	if group.Count != len(group.Files) {
		t.Errorf("Count mismatch: expected %d, got %d", len(group.Files), group.Count)
	}

	if len(group.Hash) == 0 {
		t.Error("Hash should not be empty")
	}

	if len(group.Files) < 2 {
		t.Error("Duplicate group should have at least 2 files")
	}

	// Test that files are properly stored
	for i, expectedFile := range files {
		if group.Files[i] != expectedFile {
			t.Errorf("File[%d]: expected '%s', got '%s'", i, expectedFile, group.Files[i])
		}
	}
}

func TestDuplicateGroup_EmptyGroup(t *testing.T) {
	// Test handling of empty group
	group := DuplicateGroup{}

	if group.Hash != "" {
		t.Errorf("Empty group hash should be empty, got '%s'", group.Hash)
	}

	if len(group.Files) != 0 {
		t.Errorf("Empty group should have 0 files, got %d", len(group.Files))
	}

	if group.Count != 0 {
		t.Errorf("Empty group count should be 0, got %d", group.Count)
	}
}

func TestDuplicateGroup_SingleFile(t *testing.T) {
	// Test group with single file (not really a duplicate, but test data structure)
	group := DuplicateGroup{
		Hash:  "single_file_hash",
		Files: []string{"single_file.txt"},
		Count: 1,
	}

	if group.Count != 1 {
		t.Errorf("Single file group count should be 1, got %d", group.Count)
	}

	if len(group.Files) != 1 {
		t.Errorf("Single file group should have 1 file, got %d", len(group.Files))
	}

	if group.Files[0] != "single_file.txt" {
		t.Errorf("Expected file 'single_file.txt', got '%s'", group.Files[0])
	}
}

// Test that duplicate groups maintain consistency
func TestDuplicateGroup_Consistency(t *testing.T) {
	testCases := []struct {
		name    string
		hash    string
		files   []string
		count   int
		isValid bool
	}{
		{
			name:    "valid group",
			hash:    "valid_hash",
			files:   []string{"file1.txt", "file2.txt"},
			count:   2,
			isValid: true,
		},
		{
			name:    "count mismatch",
			hash:    "hash",
			files:   []string{"file1.txt", "file2.txt"},
			count:   3,
			isValid: false,
		},
		{
			name:    "empty hash",
			hash:    "",
			files:   []string{"file1.txt", "file2.txt"},
			count:   2,
			isValid: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			group := DuplicateGroup{
				Hash:  tc.hash,
				Files: tc.files,
				Count: tc.count,
			}

			// Check basic consistency
			isValid := group.Count == len(group.Files) && group.Hash != ""

			if isValid != tc.isValid {
				t.Errorf("Expected validity %v, got %v", tc.isValid, isValid)
			}
		})
	}
}
