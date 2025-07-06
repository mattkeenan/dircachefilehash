package dircachefilehash

import (
	"testing"
)

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
	defer dc.Close()

	// Create empty index
	if err := dc.createEmptyIndex(); err != nil {
		t.Fatalf("Failed to create empty index: %v", err)
	}

	// Test FindDuplicates with empty flags
	flags := map[string]string{}
	duplicates, err := dc.FindDuplicates(nil, flags)
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
	defer dc.Close()

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
			duplicates, err := dc.FindDuplicates(nil, flags)
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
