package dircachefilehash

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Helper function to create a temporary test repository for filesystem iterator tests
func createTestRepositoryForFS(t *testing.T, files map[string]string) (*DirectoryCache, string) {
	// Create temporary directory
	testDir, err := os.MkdirTemp("", "dcfh-filesystem-test-*")
	if err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}
	
	// Create .dcfh directory
	dcfhDir := filepath.Join(testDir, ".dcfh")
	if err := os.MkdirAll(dcfhDir, 0755); err != nil {
		t.Fatalf("Failed to create .dcfh directory: %v", err)
	}
	
	// Create test files
	for path, content := range files {
		fullPath := filepath.Join(testDir, path)
		
		// Create parent directories if needed
		parentDir := filepath.Dir(fullPath)
		if err := os.MkdirAll(parentDir, 0755); err != nil {
			t.Fatalf("Failed to create parent directory %s: %v", parentDir, err)
		}
		
		// Write file content
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", path, err)
		}
	}
	
	// Create DirectoryCache
	dc := NewDirectoryCache(testDir, testDir)
	
	return dc, testDir
}

func TestFilesystemScanIterator(t *testing.T) {
	t.Run("BasicScanning", func(t *testing.T) {
		// Create test repository
		files := map[string]string{
			"file1.txt":         "content1",
			"file2.txt":         "content2",
			"subdir/file3.txt":  "content3",
			"subdir/file4.txt":  "content4",
		}
		dc, testDir := createTestRepositoryForFS(t, files)
		defer os.RemoveAll(testDir)
		
		// Create filesystem scan iterator
		iter := NewFilesystemScanIterator(dc, []string{}, "test-fs-scan")
		defer iter.Close()
		
		// Check initial state
		if iter.Name() != "test-fs-scan" {
			t.Errorf("Expected name 'test-fs-scan', got '%s'", iter.Name())
		}
		
		if !iter.HasNext() {
			t.Error("Expected HasNext() to be true initially")
		}
		
		// Collect all scanned files
		var scannedPaths []string
		for {
			entry, err := iter.Next()
			if err != nil {
				t.Fatalf("Unexpected error during iteration: %v", err)
			}
			
			if entry == nil {
				break // End of iteration
			}
			
			path := entry.RelativePath()
			scannedPaths = append(scannedPaths, path)
			
			// Verify entry has reasonable values
			if entry.FileSize == 0 {
				t.Errorf("Entry for %s has zero file size", path)
			}
			
			if entry.Size == 0 {
				t.Errorf("Entry for %s has zero entry size", path)
			}
			
			// Check that the path exists in our test files
			expectedContent, exists := files[path]
			if !exists {
				t.Errorf("Scanned unexpected file: %s", path)
			} else {
				// Verify file size matches content length
				if entry.FileSize != uint64(len(expectedContent)) {
					t.Errorf("File %s: expected size %d, got %d", 
						path, len(expectedContent), entry.FileSize)
				}
			}
		}
		
		// Verify we got all expected files
		if len(scannedPaths) < len(files) {
			t.Errorf("Expected at least %d files, got %d", len(files), len(scannedPaths))
		}
		
		// Verify paths are sorted (filesystem scanner should provide sorted output)
		for i := 1; i < len(scannedPaths); i++ {
			if scannedPaths[i-1] >= scannedPaths[i] {
				t.Errorf("Paths not in sorted order: '%s' >= '%s'", 
					scannedPaths[i-1], scannedPaths[i])
			}
		}
		
		// Iterator should be exhausted
		if iter.HasNext() {
			t.Error("Expected HasNext() to be false after exhaustion")
		}
	})
	
	t.Run("SpecificPaths", func(t *testing.T) {
		// Create test repository
		files := map[string]string{
			"file1.txt":         "content1",
			"file2.txt":         "content2", 
			"subdir/file3.txt":  "content3",
			"subdir/file4.txt":  "content4",
		}
		dc, testDir := createTestRepositoryForFS(t, files)
		defer os.RemoveAll(testDir)
		
		// Scan only specific file
		targetFile := filepath.Join(testDir, "file1.txt")
		iter := NewFilesystemScanIterator(dc, []string{targetFile}, "specific-file-scan")
		defer iter.Close()
		
		// Should get exactly one file
		entry, err := iter.Next()
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		
		if entry == nil {
			t.Fatal("Expected to get one entry")
		}
		
		if entry.RelativePath() != "file1.txt" {
			t.Errorf("Expected path 'file1.txt', got '%s'", entry.RelativePath())
		}
		
		// Should not get any more entries
		entry2, err := iter.Next()
		if err != nil {
			t.Fatalf("Unexpected error on second call: %v", err)
		}
		
		if entry2 != nil {
			t.Error("Expected no more entries")
		}
	})
	
	t.Run("EmptyDirectory", func(t *testing.T) {
		// Create empty test repository
		dc, testDir := createTestRepositoryForFS(t, map[string]string{})
		defer os.RemoveAll(testDir)
		
		iter := NewFilesystemScanIterator(dc, []string{}, "empty-scan")
		defer iter.Close()
		
		// Should get no entries
		entry, err := iter.Next()
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		
		if entry != nil {
			t.Errorf("Expected no entries, got: %s", entry.RelativePath())
		}
		
		if iter.HasNext() {
			t.Error("Expected HasNext() to be false with empty directory")
		}
	})
	
	t.Run("NilDirectoryCache", func(t *testing.T) {
		iter := NewFilesystemScanIterator(nil, []string{}, "nil-dc")
		defer iter.Close()
		
		// Should be immediately exhausted
		if iter.HasNext() {
			t.Error("Expected HasNext() to be false with nil DirectoryCache")
		}
		
		entry, err := iter.Next()
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		
		if entry != nil {
			t.Error("Expected nil entry with nil DirectoryCache")
		}
	})
	
	t.Run("ClosedIterator", func(t *testing.T) {
		// Create test repository
		files := map[string]string{
			"file1.txt": "content1",
			"file2.txt": "content2",
		}
		dc, testDir := createTestRepositoryForFS(t, files)
		defer os.RemoveAll(testDir)
		
		iter := NewFilesystemScanIterator(dc, []string{}, "closed-iter")
		
		// Close the iterator
		if err := iter.Close(); err != nil {
			t.Fatalf("Unexpected error closing iterator: %v", err)
		}
		
		// Attempting to iterate should return error
		entry, err := iter.Next()
		if err == nil {
			t.Error("Expected error when calling Next() on closed iterator")
		}
		
		if entry != nil {
			t.Error("Expected nil entry when calling Next() on closed iterator")
		}
		
		if iter.HasNext() {
			t.Error("Expected HasNext() to be false on closed iterator")
		}
		
		// Closing again should be safe
		if err := iter.Close(); err != nil {
			t.Errorf("Unexpected error closing iterator again: %v", err)
		}
	})
	
	t.Run("LargeDirectory", func(t *testing.T) {
		// Create test repository with many files
		files := make(map[string]string)
		for i := 0; i < 50; i++ {
			files[fmt.Sprintf("file%03d.txt", i)] = fmt.Sprintf("content%d", i)
		}
		
		dc, testDir := createTestRepositoryForFS(t, files)
		defer os.RemoveAll(testDir)
		
		iter := NewFilesystemScanIterator(dc, []string{}, "large-scan")
		defer iter.Close()
		
		// Count entries
		count := 0
		var lastPath string
		
		for {
			entry, err := iter.Next()
			if err != nil {
				t.Fatalf("Unexpected error during iteration: %v", err)
			}
			
			if entry == nil {
				break
			}
			
			count++
			currentPath := entry.RelativePath()
			
			// Verify sorting
			if lastPath != "" && currentPath <= lastPath {
				t.Errorf("Paths not in sorted order: '%s' <= '%s'", currentPath, lastPath)
			}
			lastPath = currentPath
		}
		
		// Should have found all files (or at least a reasonable number)
		if count < len(files) {
			t.Errorf("Expected at least %d files, got %d", len(files), count)
		}
	})
	
	t.Run("InvalidPath", func(t *testing.T) {
		// Create test repository
		dc, testDir := createTestRepositoryForFS(t, map[string]string{})
		defer os.RemoveAll(testDir)
		
		// Try to scan non-existent path
		nonExistentPath := filepath.Join(testDir, "does-not-exist")
		iter := NewFilesystemScanIterator(dc, []string{nonExistentPath}, "invalid-path")
		defer iter.Close()
		
		// Should handle gracefully (might get error or just no results)
		entry, err := iter.Next()
		if err != nil {
			// Error is acceptable for invalid paths
			t.Logf("Got expected error for invalid path: %v", err)
		} else if entry != nil {
			t.Errorf("Unexpected entry for invalid path: %s", entry.RelativePath())
		}
	})
}

// Integration tests removed - deprecated v0.6 FilesystemScanIterator integration 
// with v0.7 hwangLinUnified algorithm. Equivalent functionality is tested
// by UnifiedFilesystemScanIterator tests which use the proper BinaryEntryIterator interface.

