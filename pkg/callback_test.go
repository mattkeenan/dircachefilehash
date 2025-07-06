package dircachefilehash

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCallbackArchitecture verifies that the callback architecture doesn't break basic functionality
func TestCallbackArchitecture(t *testing.T) {
	// Create a temporary test directory
	testDir, err := os.MkdirTemp("", "callback-test-")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(testDir)

	// Create test files
	testFiles := map[string]string{
		"file1.txt": "Content of file 1",
		"file2.txt": "Content of file 2",
	}

	for filename, content := range testFiles {
		filePath := filepath.Join(testDir, filename)
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", filename, err)
		}
	}

	// Initialise DirectoryCache
	cache := NewDirectoryCache(testDir, testDir)
	defer cache.Close()

	// Configure to use SHA-256
	flags := map[string]string{
		"filehash.default": "sha256",
	}
	if err := cache.ApplyConfigOverrides(flags); err != nil {
		t.Fatalf("Failed to apply config: %v", err)
	}

	// Perform initial update to create index
	if err := cache.Update(nil, flags); err != nil {
		t.Fatalf("Failed to update cache: %v", err)
	}
	
	// Check if index file was created
	if _, err := os.Stat(cache.IndexFile); os.IsNotExist(err) {
		t.Fatalf("Index file was not created: %s", cache.IndexFile)
	}
	t.Logf("Index file created: %s", cache.IndexFile)

	// Test 1: Verify LoadMainIndex works with callback architecture
	mainSkiplist, err := cache.LoadMainIndex()
	if err != nil {
		t.Fatalf("Failed to load main index: %v", err)
	}

	// Count entries using ForEach
	entryCount := 0
	mainSkiplist.ForEach(func(entry *binaryEntry, context string) bool {
		entryCount++
		t.Logf("Entry: %s", entry.RelativePath())
		return true
	})

	if entryCount != len(testFiles) {
		t.Errorf("Expected %d entries, got %d", len(testFiles), entryCount)
	}

	// Test 2: Test direct loadIndexFromFile 
	refs, err := cache.loadIndexFromFile(cache.IndexFile)
	if err != nil {
		t.Fatalf("Failed to load index directly: %v", err)
	}

	t.Logf("loadIndexFromFile returned %d refs", len(refs))
	if len(refs) != len(testFiles) {
		t.Errorf("Expected %d refs, got %d", len(testFiles), len(refs))
	}

	// Test 3: Test Status detection 
	status, err := cache.Status(nil, flags)
	if err != nil {
		t.Fatalf("Failed to get status: %v", err)
	}

	// Should have no changes since we just updated
	if status.HasChanges() {
		t.Errorf("Expected no changes, but got: Added=%v, Modified=%v, Deleted=%v", 
			status.Added, status.Modified, status.Deleted)
	}

	// Test 4: Modify a file and test status detection
	modifiedFile := filepath.Join(testDir, "file1.txt")
	if err := os.WriteFile(modifiedFile, []byte("Modified content"), 0644); err != nil {
		t.Fatalf("Failed to modify file: %v", err)
	}

	status, err = cache.Status(nil, flags)
	if err != nil {
		t.Fatalf("Failed to get status after modification: %v", err)
	}

	// Should detect the modification
	if len(status.Modified) != 1 || status.Modified[0] != "file1.txt" {
		t.Errorf("Expected file1.txt to be modified, got Modified=%v", status.Modified)
	}

	// Test 5: Delete a file and test status detection
	deletedFile := filepath.Join(testDir, "file2.txt")
	if err := os.Remove(deletedFile); err != nil {
		t.Fatalf("Failed to delete file: %v", err)
	}
	
	// Verify file is actually deleted
	if _, err := os.Stat(deletedFile); !os.IsNotExist(err) {
		t.Fatalf("File %s should be deleted but still exists", deletedFile)
	}
	t.Logf("File %s successfully deleted", deletedFile)

	status, err = cache.Status(nil, flags)
	if err != nil {
		t.Fatalf("Failed to get status after deletion: %v", err)
	}

	t.Logf("Status after deletion: Added=%v, Modified=%v, Deleted=%v", 
		status.Added, status.Modified, status.Deleted)

	// Should detect the deletion
	if len(status.Deleted) != 1 || status.Deleted[0] != "file2.txt" {
		t.Errorf("Expected file2.txt to be deleted, got Deleted=%v", status.Deleted)
	}
}