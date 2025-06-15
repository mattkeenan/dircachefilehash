package dircachefilehash

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestHelper contains common test utilities
type TestHelper struct {
	tempDir   string
	cacheDir  string
	cache     *DirectoryCache
	testFiles map[string]string // filename -> content
}

// setupTestEnvironment creates a temporary directory with test files
func setupTestEnvironment(t *testing.T) *TestHelper {
	tempDir, err := os.MkdirTemp("", "dircachefilehash_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	cacheDir := filepath.Join(tempDir, "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("Failed to create cache dir: %v", err)
	}

	testFiles := map[string]string{
		"file1.txt":        "Hello, World!",
		"file2.txt":        "Another test file",
		"subdir/file3.txt": "File in subdirectory",
		"subdir/file4.txt": "Another file in subdirectory",
		"duplicate1.txt":   "This content is duplicated",
		"duplicate2.txt":   "This content is duplicated", // Same content as duplicate1.txt
		"empty.txt":        "",
	}

	// Create test files
	for filename, content := range testFiles {
		fullPath := filepath.Join(tempDir, filename)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("Failed to create directory for %s: %v", filename, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", filename, err)
		}
	}

	cache := NewDirectoryCache(tempDir, cacheDir)

	return &TestHelper{
		tempDir:   tempDir,
		cacheDir:  cacheDir,
		cache:     cache,
		testFiles: testFiles,
	}
}

// cleanup removes the temporary directory
func (th *TestHelper) cleanup(t *testing.T) {
	if th.cache != nil {
		th.cache.Close()
	}
	if err := os.RemoveAll(th.tempDir); err != nil {
		t.Logf("Warning: Failed to cleanup temp dir %s: %v", th.tempDir, err)
	}
}

// calculateExpectedHash calculates the expected SHA-1 hash for content
func calculateExpectedHash(content string) string {
	hasher := sha1.New()
	hasher.Write([]byte(content))
	return hex.EncodeToString(hasher.Sum(nil))
}

func TestNewDirectoryCache(t *testing.T) {
	th := setupTestEnvironment(t)
	defer th.cleanup(t)

	// Test basic creation
	if th.cache == nil {
		t.Fatal("NewDirectoryCache returned nil")
	}

	if th.cache.RootDir != th.tempDir {
		t.Errorf("Expected RootDir %s, got %s", th.tempDir, th.cache.RootDir)
	}

	expectedIndexFile := filepath.Join(th.cacheDir, ".dcfh", "index")
	if th.cache.IndexFile != expectedIndexFile {
		t.Errorf("Expected IndexFile %s, got %s", expectedIndexFile, th.cache.IndexFile)
	}

	// Test that .dcfh directory was created
	dcfhDir := filepath.Join(th.cacheDir, ".dcfh")
	if _, err := os.Stat(dcfhDir); os.IsNotExist(err) {
		t.Error(".dcfh directory was not created")
	}

	// Test with empty dcfhDir (should use rootDir)
	cache2 := NewDirectoryCache(th.tempDir, "")
	expectedIndexFile2 := filepath.Join(th.tempDir, ".dcfh", "index")
	if cache2.IndexFile != expectedIndexFile2 {
		t.Errorf("Expected IndexFile %s, got %s", expectedIndexFile2, cache2.IndexFile)
	}
	cache2.Close()
}

func TestScanDirectoryAndUpdate(t *testing.T) {
	th := setupTestEnvironment(t)
	defer th.cleanup(t)

	// Test ScanDirectory
	err := th.cache.ScanDirectory()
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	// Verify index file was created
	if _, err := os.Stat(th.cache.IndexFile); os.IsNotExist(err) {
		t.Error("Index file was not created")
	}

	// Test Update with no arguments (should scan entire directory)
	err = th.cache.Update()
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Test Update with specific paths
	err = th.cache.Update("file1.txt", "subdir")
	if err != nil {
		t.Fatalf("Update with specific paths failed: %v", err)
	}
}

func TestLoadIndex(t *testing.T) {
	th := setupTestEnvironment(t)
	defer th.cleanup(t)

	// First create an index
	err := th.cache.ScanDirectory()
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	// Test loading the index
	err = th.cache.LoadIndex()
	if err != nil {
		t.Fatalf("LoadIndex failed: %v", err)
	}

	// Verify entries were loaded
	entries := th.cache.GetEntries()
	if len(entries) == 0 {
		t.Error("No entries loaded from index")
	}

	// Verify we can access entry data
	for _, entry := range entries {
		if entry.RelativePath() == "" {
			t.Error("Entry has empty relative path")
		}
		if entry.HashString() == "" {
			t.Error("Entry has empty hash")
		}
		if entry.EntrySize() == 0 {
			t.Error("Entry has zero size")
		}
	}
}

func TestGetEntries(t *testing.T) {
	th := setupTestEnvironment(t)
	defer th.cleanup(t)

	// Create and load index
	err := th.cache.ScanDirectory()
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	err = th.cache.LoadIndex()
	if err != nil {
		t.Fatalf("LoadIndex failed: %v", err)
	}

	entries := th.cache.GetEntries()

	// Should have entries for all test files (excluding directories)
	expectedCount := len(th.testFiles)
	if len(entries) != expectedCount {
		t.Errorf("Expected %d entries, got %d", expectedCount, len(entries))
	}

	// Verify entries are sorted by path
	for i := 1; i < len(entries); i++ {
		if entries[i-1].RelativePath() >= entries[i].RelativePath() {
			t.Error("Entries are not sorted by relative path")
			break
		}
	}
}

func TestStats(t *testing.T) {
	th := setupTestEnvironment(t)
	defer th.cleanup(t)

	// Create and load index
	err := th.cache.ScanDirectory()
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	err = th.cache.LoadIndex()
	if err != nil {
		t.Fatalf("LoadIndex failed: %v", err)
	}

	count, totalSize, err := th.cache.Stats()
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}

	expectedCount := len(th.testFiles)
	if count != expectedCount {
		t.Errorf("Expected count %d, got %d", expectedCount, count)
	}

	// Calculate expected total size
	var expectedSize int64
	for _, content := range th.testFiles {
		expectedSize += int64(len(content))
	}

	if totalSize != expectedSize {
		t.Errorf("Expected total size %d, got %d", expectedSize, totalSize)
	}
}

func TestIsMmapped(t *testing.T) {
	th := setupTestEnvironment(t)
	defer th.cleanup(t)

	// Before loading index, should not be mmapped
	if th.cache.IsMmapped() {
		t.Error("Cache should not be mmapped before loading index")
	}

	// Create and load index
	err := th.cache.ScanDirectory()
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	err = th.cache.LoadIndex()
	if err != nil {
		t.Fatalf("LoadIndex failed: %v", err)
	}

	// After loading index, should be mmapped
	if !th.cache.IsMmapped() {
		t.Error("Cache should be mmapped after loading index")
	}
}

func TestBinaryEntryMethods(t *testing.T) {
	th := setupTestEnvironment(t)
	defer th.cleanup(t)

	// Create and load index
	err := th.cache.ScanDirectory()
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	err = th.cache.LoadIndex()
	if err != nil {
		t.Fatalf("LoadIndex failed: %v", err)
	}

	entries := th.cache.GetEntries()
	if len(entries) == 0 {
		t.Fatal("No entries to test")
	}

	for _, entry := range entries {
		// Test RelativePath
		path := entry.RelativePath()
		if path == "" {
			t.Error("RelativePath returned empty string")
		}

		// Test RelativePathBytes
		pathBytes := entry.RelativePathBytes()
		if len(pathBytes) == 0 {
			t.Error("RelativePathBytes returned empty slice")
		}

		// Verify path and pathBytes match
		if string(pathBytes) != path {
			t.Errorf("RelativePath and RelativePathBytes don't match: %s vs %s", path, string(pathBytes))
		}

		// Test HashString
		hashStr := entry.HashString()
		if hashStr == "" {
			t.Error("HashString returned empty string")
		}

		// Verify hash format (should be hex)
		if len(hashStr) != 40 { // SHA-1 is 40 hex characters
			t.Errorf("HashString has wrong length: expected 40, got %d", len(hashStr))
		}

		// Test EntrySize
		size := entry.EntrySize()
		if size <= 0 {
			t.Error("EntrySize returned non-positive value")
		}

		// Verify hash matches expected content
		if content, exists := th.testFiles[path]; exists {
			expectedHash := calculateExpectedHash(content)
			if hashStr != expectedHash {
				t.Errorf("Hash mismatch for %s: expected %s, got %s", path, expectedHash, hashStr)
			}
		}
	}
}

func TestStatus(t *testing.T) {
	th := setupTestEnvironment(t)
	defer th.cleanup(t)

	// Create initial index
	err := th.cache.ScanDirectory()
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	err = th.cache.LoadIndex()
	if err != nil {
		t.Fatalf("LoadIndex failed: %v", err)
	}

	// Test status with no changes
	status, err := th.cache.Status()
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}

	if status.HasChanges() {
		t.Error("Status should show no changes initially")
	}

	if status.TotalChanges() != 0 {
		t.Errorf("Expected 0 total changes, got %d", status.TotalChanges())
	}

	// Modify a file
	modifiedFile := filepath.Join(th.tempDir, "file1.txt")
	err = os.WriteFile(modifiedFile, []byte("Modified content"), 0644)
	if err != nil {
		t.Fatalf("Failed to modify file: %v", err)
	}

	// Add a new file
	newFile := filepath.Join(th.tempDir, "newfile.txt")
	err = os.WriteFile(newFile, []byte("New file content"), 0644)
	if err != nil {
		t.Fatalf("Failed to create new file: %v", err)
	}

	// Delete a file
	deletedFile := filepath.Join(th.tempDir, "file2.txt")
	err = os.Remove(deletedFile)
	if err != nil {
		t.Fatalf("Failed to delete file: %v", err)
	}

	// Test status with changes
	status, err = th.cache.Status()
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}

	if !status.HasChanges() {
		t.Error("Status should show changes")
	}

	if len(status.Modified) != 1 {
		t.Errorf("Expected 1 modified file, got %d", len(status.Modified))
	}

	if len(status.Added) != 1 {
		t.Errorf("Expected 1 added file, got %d", len(status.Added))
	}

	if len(status.Deleted) != 1 {
		t.Errorf("Expected 1 deleted file, got %d", len(status.Deleted))
	}

	expectedTotal := 3
	if status.TotalChanges() != expectedTotal {
		t.Errorf("Expected %d total changes, got %d", expectedTotal, status.TotalChanges())
	}
}

func TestStatusMethods(t *testing.T) {
	th := setupTestEnvironment(t)
	defer th.cleanup(t)

	// Create initial index
	err := th.cache.ScanDirectory()
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	err = th.cache.LoadIndex()
	if err != nil {
		t.Fatalf("LoadIndex failed: %v", err)
	}

	// Modify files for testing
	modifiedFile := filepath.Join(th.tempDir, "file1.txt")
	err = os.WriteFile(modifiedFile, []byte("Modified content"), 0644)
	if err != nil {
		t.Fatalf("Failed to modify file: %v", err)
	}

	newFile := filepath.Join(th.tempDir, "newfile.txt")
	err = os.WriteFile(newFile, []byte("New file content"), 0644)
	if err != nil {
		t.Fatalf("Failed to create new file: %v", err)
	}

	deletedFile := filepath.Join(th.tempDir, "file2.txt")
	err = os.Remove(deletedFile)
	if err != nil {
		t.Fatalf("Failed to delete file: %v", err)
	}

	// Test GetModifiedFiles
	modified, err := th.cache.GetModifiedFiles()
	if err != nil {
		t.Fatalf("GetModifiedFiles failed: %v", err)
	}
	if len(modified) != 1 {
		t.Errorf("Expected 1 modified file, got %d", len(modified))
	}

	// Test GetAddedFiles
	added, err := th.cache.GetAddedFiles()
	if err != nil {
		t.Fatalf("GetAddedFiles failed: %v", err)
	}
	if len(added) != 1 {
		t.Errorf("Expected 1 added file, got %d", len(added))
	}

	// Test GetDeletedFiles
	deleted, err := th.cache.GetDeletedFiles()
	if err != nil {
		t.Fatalf("GetDeletedFiles failed: %v", err)
	}
	if len(deleted) != 1 {
		t.Errorf("Expected 1 deleted file, got %d", len(deleted))
	}

	// Test HasChangesQuick
	hasChanges, err := th.cache.HasChangesQuick()
	if err != nil {
		t.Fatalf("HasChangesQuick failed: %v", err)
	}
	if !hasChanges {
		t.Error("HasChangesQuick should return true when there are changes")
	}
}

func TestStatusWithCallback(t *testing.T) {
	th := setupTestEnvironment(t)
	defer th.cleanup(t)

	// Create initial index
	err := th.cache.ScanDirectory()
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	err = th.cache.LoadIndex()
	if err != nil {
		t.Fatalf("LoadIndex failed: %v", err)
	}

	// Modify a file
	modifiedFile := filepath.Join(th.tempDir, "file1.txt")
	err = os.WriteFile(modifiedFile, []byte("Modified content"), 0644)
	if err != nil {
		t.Fatalf("Failed to modify file: %v", err)
	}

	// Track callback results
	var callbackResults []FileStatus
	var callbackPaths []string

	err = th.cache.StatusWithCallback(func(status FileStatus, path string, indexEntry, diskEntry *binaryEntry) {
		callbackResults = append(callbackResults, status)
		callbackPaths = append(callbackPaths, path)

		// Verify entry pointers
		switch status {
		case StatusModified:
			if indexEntry == nil || diskEntry == nil {
				t.Error("Modified status should have both indexEntry and diskEntry")
			}
		case StatusAdded:
			if indexEntry != nil || diskEntry == nil {
				t.Error("Added status should have only diskEntry")
			}
		case StatusDeleted:
			if indexEntry == nil || diskEntry != nil {
				t.Error("Deleted status should have only indexEntry")
			}
		case StatusUnchanged:
			if indexEntry == nil || diskEntry == nil {
				t.Error("Unchanged status should have both indexEntry and diskEntry")
			}
		}
	})

	if err != nil {
		t.Fatalf("StatusWithCallback failed: %v", err)
	}

	// Should have one modified file and several unchanged files
	modifiedCount := 0
	unchangedCount := 0
	for _, status := range callbackResults {
		switch status {
		case StatusModified:
			modifiedCount++
		case StatusUnchanged:
			unchangedCount++
		}
	}

	if modifiedCount != 1 {
		t.Errorf("Expected 1 modified file, got %d", modifiedCount)
	}

	if unchangedCount != len(th.testFiles)-1 {
		t.Errorf("Expected %d unchanged files, got %d", len(th.testFiles)-1, unchangedCount)
	}
}

func TestFindDuplicates(t *testing.T) {
	th := setupTestEnvironment(t)
	defer th.cleanup(t)

	// Create and load index
	err := th.cache.ScanDirectory()
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	err = th.cache.LoadIndex()
	if err != nil {
		t.Fatalf("LoadIndex failed: %v", err)
	}

	duplicates := th.cache.FindDuplicates()

	// Should find duplicates for files with identical content
	duplicateContent := "This content is duplicated"
	expectedHash := calculateExpectedHash(duplicateContent)

	if dupEntries, exists := duplicates[expectedHash]; exists {
		if len(dupEntries) != 2 {
			t.Errorf("Expected 2 duplicate entries, got %d", len(dupEntries))
		}

		// Verify the duplicate files are the expected ones
		var paths []string
		for _, entry := range dupEntries {
			paths = append(paths, entry.RelativePath())
		}
		sort.Strings(paths)

		expectedPaths := []string{"duplicate1.txt", "duplicate2.txt"}
		sort.Strings(expectedPaths)

		if len(paths) != len(expectedPaths) {
			t.Errorf("Duplicate paths mismatch: expected %v, got %v", expectedPaths, paths)
		} else {
			for i, path := range paths {
				if path != expectedPaths[i] {
					t.Errorf("Duplicate path mismatch at index %d: expected %s, got %s", i, expectedPaths[i], path)
				}
			}
		}
	} else {
		t.Error("No duplicates found for identical content")
	}
}

func TestFindByHash(t *testing.T) {
	th := setupTestEnvironment(t)
	defer th.cleanup(t)

	// Create and load index
	err := th.cache.ScanDirectory()
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	err = th.cache.LoadIndex()
	if err != nil {
		t.Fatalf("LoadIndex failed: %v", err)
	}

	// Test finding by hash
	content := "Hello, World!"
	expectedHash := calculateExpectedHash(content)

	matches := th.cache.FindByHash(expectedHash)
	if len(matches) != 1 {
		t.Errorf("Expected 1 match for hash, got %d", len(matches))
	}

	if len(matches) > 0 && matches[0].RelativePath() != "file1.txt" {
		t.Errorf("Expected match for file1.txt, got %s", matches[0].RelativePath())
	}

	// Test finding by non-existent hash
	nonExistentHash := "0000000000000000000000000000000000000000"
	matches = th.cache.FindByHash(nonExistentHash)
	if len(matches) != 0 {
		t.Errorf("Expected 0 matches for non-existent hash, got %d", len(matches))
	}
}

func TestUpdatePaths(t *testing.T) {
	th := setupTestEnvironment(t)
	defer th.cleanup(t)

	// Create initial index
	err := th.cache.ScanDirectory()
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	// Test UpdatePaths with specific files
	paths := []string{"file1.txt", "subdir/file3.txt"}
	err = th.cache.UpdatePaths(paths)
	if err != nil {
		t.Fatalf("UpdatePaths failed: %v", err)
	}

	// Verify index was updated
	err = th.cache.LoadIndex()
	if err != nil {
		t.Fatalf("LoadIndex failed: %v", err)
	}

	entries := th.cache.GetEntries()
	if len(entries) == 0 {
		t.Error("No entries found after UpdatePaths")
	}
}

func TestClose(t *testing.T) {
	th := setupTestEnvironment(t)
	defer th.cleanup(t)

	// Create and load index
	err := th.cache.ScanDirectory()
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	err = th.cache.LoadIndex()
	if err != nil {
		t.Fatalf("LoadIndex failed: %v", err)
	}

	// Verify it's mmapped
	if !th.cache.IsMmapped() {
		t.Error("Cache should be mmapped before close")
	}

	// Test Close
	err = th.cache.Close()
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Verify it's no longer mmapped
	if th.cache.IsMmapped() {
		t.Error("Cache should not be mmapped after close")
	}

	// Test multiple calls to Close (should not fail)
	err = th.cache.Close()
	if err != nil {
		t.Fatalf("Second Close failed: %v", err)
	}
}

func TestEmptyDirectory(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "dircachefilehash_empty_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cache := NewDirectoryCache(tempDir, "")
	defer cache.Close()

	// Test scanning empty directory
	err = cache.ScanDirectory()
	if err != nil {
		t.Fatalf("ScanDirectory failed on empty directory: %v", err)
	}

	err = cache.LoadIndex()
	if err != nil {
		t.Fatalf("LoadIndex failed on empty directory: %v", err)
	}

	entries := cache.GetEntries()
	if len(entries) != 0 {
		t.Errorf("Expected 0 entries in empty directory, got %d", len(entries))
	}

	count, totalSize, err := cache.Stats()
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}

	if count != 0 {
		t.Errorf("Expected count 0, got %d", count)
	}

	if totalSize != 0 {
		t.Errorf("Expected total size 0, got %d", totalSize)
	}
}

func TestErrorCases(t *testing.T) {
	// Test with non-existent directory
	cache := NewDirectoryCache("/non/existent/directory", "")
	defer cache.Close()

	err := cache.ScanDirectory()
	if err == nil {
		t.Error("Expected error when scanning non-existent directory")
	}

	// Test loading non-existent index
	err = cache.LoadIndex()
	if err == nil {
		t.Error("Expected error when loading non-existent index")
	}
}
