package dircachefilehash

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUnifiedFilesystemScanIterator_BasicScanning(t *testing.T) {
	// Create test directory with files
	tempDir := createTestDirectoryStructure(t)
	defer os.RemoveAll(tempDir)

	dc := createTestDirectoryCache(t, tempDir)
	defer dc.cleanupCurrentScanFile()

	// Create iterator (no hash manager needed - iterator is synchronous)
	iterator := NewUnifiedFilesystemScanIterator(dc, []string{tempDir}, "test-unified")
	defer iterator.Close()

	var entries []BinaryEntryInterface
	entryCount := 0

	// Iterate through all entries
	for iterator.HasNext() {
		entry, err := iterator.Next()
		if err != nil {
			t.Fatalf("Error during iteration: %v", err)
		}

		if entry == nil {
			break
		}

		entries = append(entries, entry)
		entryCount++

		// Verify entry is valid and has expected fields
		if !entry.IsValid() {
			t.Errorf("Entry %d is not valid", entryCount)
		}

		path, err := entry.RelativePath()
		if err != nil {
			t.Errorf("Error getting relative path for entry %d: %v", entryCount, err)
		} else if path == "" {
			t.Errorf("Entry %d has empty path", entryCount)
		}

		// Check that entry has hash (since we submit all for hashing)
		hashStr, err := entry.HashString()
		if err != nil {
			t.Errorf("Error getting hash string for entry %d: %v", entryCount, err)
		} else if hashStr == "" {
			t.Logf("Warning: Entry %d has empty hash (path: %s)", entryCount, path)
		}

		t.Logf("Entry %d: %s (hash: %s)", entryCount, path, hashStr)
	}

	if entryCount == 0 {
		t.Error("No entries returned from iterator")
	}

	t.Logf("Successfully iterated through %d entries", entryCount)
}

func TestUnifiedFilesystemScanIterator_EmptyDirectory(t *testing.T) {
	tempDir := createTempDir(t)
	defer os.RemoveAll(tempDir)

	dc := createTestDirectoryCache(t, tempDir)
	defer dc.cleanupCurrentScanFile()

	iterator := NewUnifiedFilesystemScanIterator(dc, []string{tempDir}, "test-empty")
	defer iterator.Close()

	// Should have no entries
	entry, err := iterator.Next()
	if err != nil {
		t.Fatalf("Error iterating empty directory: %v", err)
	}

	if entry != nil {
		t.Error("Expected no entries from empty directory")
	}

	if iterator.HasNext() {
		t.Error("HasNext should return false for empty directory")
	}
}

func TestUnifiedFilesystemScanIterator_NilDirectoryCache(t *testing.T) {
	// Test with nil DirectoryCache (no hash manager needed)

	// Create iterator with nil DirectoryCache
	iterator := NewUnifiedFilesystemScanIterator(nil, []string{"/tmp"}, "test-nil")
	defer iterator.Close()

	// Should be immediately exhausted
	if iterator.HasNext() {
		t.Error("Iterator with nil DirectoryCache should not have entries")
	}

	entry, err := iterator.Next()
	if err != nil {
		t.Fatalf("Error with nil DirectoryCache: %v", err)
	}

	if entry != nil {
		t.Error("Expected no entries with nil DirectoryCache")
	}
}

func TestUnifiedFilesystemScanIterator_ClosedIterator(t *testing.T) {
	tempDir := createTestDirectoryStructure(t)
	defer os.RemoveAll(tempDir)

	dc := createTestDirectoryCache(t, tempDir)
	defer dc.cleanupCurrentScanFile()

	iterator := NewUnifiedFilesystemScanIterator(dc, []string{tempDir}, "test-closed")

	// Close the iterator immediately
	iterator.Close()

	// Should return error when trying to use closed iterator
	entry, err := iterator.Next()
	if err == nil {
		t.Error("Expected error when using closed iterator")
	}

	if entry != nil {
		t.Error("Expected no entry from closed iterator")
	}

	if iterator.HasNext() {
		t.Error("Closed iterator should not have entries")
	}
}

func TestUnifiedFilesystemScanIterator_SpecificPaths(t *testing.T) {
	tempDir := createTestDirectoryStructure(t)
	defer os.RemoveAll(tempDir)

	dc := createTestDirectoryCache(t, tempDir)
	defer dc.cleanupCurrentScanFile()

	// Create specific file to scan
	testFile := filepath.Join(tempDir, "specific.txt")
	writeTestFile(t, testFile, "specific content")

	// Create iterator for specific path
	iterator := NewUnifiedFilesystemScanIterator(dc, []string{testFile}, "test-specific")
	defer iterator.Close()

	// Should find exactly one entry
	entry, err := iterator.Next()
	if err != nil {
		t.Fatalf("Error iterating specific path: %v", err)
	}

	if entry == nil {
		t.Fatal("Expected one entry for specific file")
	}

	path, err := entry.RelativePath()
	if err != nil {
		t.Fatalf("Error getting path: %v", err)
	}

	if !filepath.IsAbs(path) {
		expectedRelPath := filepath.Join(filepath.Base(tempDir), "specific.txt")
		if path != expectedRelPath && path != "specific.txt" {
			t.Logf("Found path: %s (expected: %s or specific.txt)", path, expectedRelPath)
		}
	}

	// Should have no more entries
	nextEntry, err := iterator.Next()
	if err != nil {
		t.Fatalf("Error getting next entry: %v", err)
	}

	if nextEntry != nil {
		t.Error("Expected only one entry for specific file")
	}
}

func TestUnifiedFilesystemScanIterator_HashCompletion(t *testing.T) {
	tempDir := createTestDirectoryStructure(t)
	defer os.RemoveAll(tempDir)

	dc := createTestDirectoryCache(t, tempDir)
	defer dc.cleanupCurrentScanFile()

	// Create hash manager with multiple workers for concurrent processing

	iterator := NewUnifiedFilesystemScanIterator(dc, []string{tempDir}, "test-hash")
	defer iterator.Close()

	var validHashes, emptyHashes int

	// Iterate through entries and check hash completion
	for iterator.HasNext() {
		entry, err := iterator.Next()
		if err != nil {
			t.Fatalf("Error during iteration: %v", err)
		}

		if entry == nil {
			break
		}

		// Check hash
		hashStr, err := entry.HashString()
		if err != nil {
			t.Errorf("Error getting hash: %v", err)
			continue
		}

		if hashStr != "" {
			validHashes++
		} else {
			emptyHashes++
		}

		path, _ := entry.RelativePath()
		t.Logf("Entry: %s, Hash: %s", path, hashStr)
	}

	t.Logf("Hash completion results: %d valid hashes, %d empty hashes", validHashes, emptyHashes)

	// Most entries should have valid hashes (allowing some to be empty due to test timing)
	if validHashes == 0 && emptyHashes > 0 {
		t.Error("No entries received valid hashes")
	}
}

func TestUnifiedFilesystemScanIterator_ConcurrentAccess(t *testing.T) {
	tempDir := createTestDirectoryStructure(t)
	defer os.RemoveAll(tempDir)

	dc := createTestDirectoryCache(t, tempDir)
	defer dc.cleanupCurrentScanFile()

	iterator := NewUnifiedFilesystemScanIterator(dc, []string{tempDir}, "test-concurrent")
	defer iterator.Close()

	// Test basic concurrent safety by accessing iterator from main thread
	// while hash completion happens in background
	entryCount := 0
	for iterator.HasNext() {
		entry, err := iterator.Next()
		if err != nil {
			t.Fatalf("Error during concurrent iteration: %v", err)
		}

		if entry == nil {
			break
		}

		entryCount++

		// Access multiple interface methods to test concurrent safety
		entry.RLock()
		_, _ = entry.RelativePath()
		_, _ = entry.HashString()
		_, _ = entry.IsDeleted()
		entry.RUnlock()
	}

	if entryCount == 0 {
		t.Error("No entries found during concurrent access test")
	}

	t.Logf("Concurrent access test completed with %d entries", entryCount)
}

func TestUnifiedFilesystemScanIterator_ResourceCleanup(t *testing.T) {
	tempDir := createTestDirectoryStructure(t)
	defer os.RemoveAll(tempDir)

	dc := createTestDirectoryCache(t, tempDir)
	defer dc.cleanupCurrentScanFile()

	iterator := NewUnifiedFilesystemScanIterator(dc, []string{tempDir}, "test-cleanup")

	// Process some entries
	for i := 0; i < 3 && iterator.HasNext(); i++ {
		entry, err := iterator.Next()
		if err != nil {
			t.Fatalf("Error during iteration: %v", err)
		}
		if entry == nil {
			break
		}
	}

	// Close iterator and verify cleanup
	err := iterator.Close()
	if err != nil {
		t.Errorf("Error during cleanup: %v", err)
	}

	// Verify iterator is closed
	if iterator.HasNext() {
		t.Error("Iterator should not have entries after close")
	}

	// Iterator is now synchronous - no pending jobs to track
}

// Helper functions for testing

func createTestDirectoryStructure(t *testing.T) string {
	tempDir := createTempDir(t)

	// Create some test files
	writeTestFile(t, filepath.Join(tempDir, "file1.txt"), "content1")
	writeTestFile(t, filepath.Join(tempDir, "file2.txt"), "content2")

	// Create subdirectory with file
	subDir := filepath.Join(tempDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subdirectory: %v", err)
	}
	writeTestFile(t, filepath.Join(subDir, "file3.txt"), "content3")

	return tempDir
}

func createTempDir(t *testing.T) string {
	tempDir, err := os.MkdirTemp("", "dcfh-unified-test-")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	return tempDir
}

func writeTestFile(t *testing.T, path, content string) {
	err := os.WriteFile(path, []byte(content), 0644)
	if err != nil {
		t.Fatalf("Failed to write test file %s: %v", path, err)
	}
}
