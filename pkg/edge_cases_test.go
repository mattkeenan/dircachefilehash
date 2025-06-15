package dircachefilehash

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"
)

func TestEdgeCasesAndErrorConditions(t *testing.T) {
	t.Run("NonExistentDirectory", func(t *testing.T) {
		cache := NewDirectoryCache("/non/existent/directory", "")
		defer cache.Close()

		err := cache.ScanDirectory()
		if err == nil {
			t.Error("Expected error when scanning non-existent directory")
		}

		err = cache.Update()
		if err == nil {
			t.Error("Expected error when updating non-existent directory")
		}

		err = cache.LoadIndex()
		if err == nil {
			t.Error("Expected error when loading index from non-existent directory")
		}
	})

	t.Run("ReadOnlyDirectory", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("Skipping read-only test when running as root")
		}

		tempDir, err := os.MkdirTemp("", "readonly_test_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create a file in the directory
		testFile := filepath.Join(tempDir, "test.txt")
		err = os.WriteFile(testFile, []byte("test content"), 0644)
		if err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		// Make directory read-only
		err = os.Chmod(tempDir, 0555)
		if err != nil {
			t.Fatalf("Failed to make directory read-only: %v", err)
		}

		cache := NewDirectoryCache(tempDir, "")
		defer cache.Close()

		// Should be able to scan
		err = cache.ScanDirectory()
		if err != nil {
			t.Errorf("Unexpected error scanning read-only directory: %v", err)
		}

		// Restore write permissions for cleanup
		os.Chmod(tempDir, 0755)
	})

	t.Run("VeryLongFilenames", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "long_filename_test_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create files with very long names (but within filesystem limits)
		longName := strings.Repeat("a", 200) + ".txt"
		longPath := filepath.Join(tempDir, longName)

		err = os.WriteFile(longPath, []byte("content"), 0644)
		if err != nil {
			t.Fatalf("Failed to create file with long name: %v", err)
		}

		cache := NewDirectoryCache(tempDir, "")
		defer cache.Close()

		err = cache.ScanDirectory()
		if err != nil {
			t.Fatalf("ScanDirectory failed with long filename: %v", err)
		}

		err = cache.LoadIndex()
		if err != nil {
			t.Fatalf("LoadIndex failed with long filename: %v", err)
		}

		entries := cache.GetEntries()
		if len(entries) != 1 {
			t.Errorf("Expected 1 entry, got %d", len(entries))
		}

		if len(entries) > 0 {
			if entries[0].RelativePath() != longName {
				t.Errorf("Wrong relative path for long filename: expected %s, got %s",
					longName, entries[0].RelativePath())
			}
		}
	})

	t.Run("SpecialCharactersInFilenames", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "special_chars_test_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Test various special characters in filenames
		specialFiles := []string{
			"file with spaces.txt",
			"file-with-dashes.txt",
			"file_with_underscores.txt",
			"file.with.dots.txt",
			"file[with]brackets.txt",
			"file(with)parens.txt",
		}

		for _, filename := range specialFiles {
			filePath := filepath.Join(tempDir, filename)
			err = os.WriteFile(filePath, []byte("content for "+filename), 0644)
			if err != nil {
				t.Fatalf("Failed to create special character file %s: %v", filename, err)
			}
		}

		cache := NewDirectoryCache(tempDir, "")
		defer cache.Close()

		err = cache.ScanDirectory()
		if err != nil {
			t.Fatalf("ScanDirectory failed with special character filenames: %v", err)
		}

		err = cache.LoadIndex()
		if err != nil {
			t.Fatalf("LoadIndex failed with special character filenames: %v", err)
		}

		entries := cache.GetEntries()
		if len(entries) != len(specialFiles) {
			t.Errorf("Expected %d entries, got %d", len(specialFiles), len(entries))
		}

		// Verify all special files are present
		foundFiles := make(map[string]bool)
		for _, entry := range entries {
			foundFiles[entry.RelativePath()] = true
		}

		for _, filename := range specialFiles {
			if !foundFiles[filename] {
				t.Errorf("Special character file not found: %s", filename)
			}
		}
	})

	t.Run("EmptyFiles", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "empty_files_test_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create multiple empty files
		emptyFiles := []string{"empty1.txt", "empty2.txt", "empty3.txt"}
		for _, filename := range emptyFiles {
			filePath := filepath.Join(tempDir, filename)
			err = os.WriteFile(filePath, []byte{}, 0644)
			if err != nil {
				t.Fatalf("Failed to create empty file %s: %v", filename, err)
			}
		}

		cache := NewDirectoryCache(tempDir, "")
		defer cache.Close()

		err = cache.ScanDirectory()
		if err != nil {
			t.Fatalf("ScanDirectory failed with empty files: %v", err)
		}

		err = cache.LoadIndex()
		if err != nil {
			t.Fatalf("LoadIndex failed with empty files: %v", err)
		}

		entries := cache.GetEntries()
		if len(entries) != len(emptyFiles) {
			t.Errorf("Expected %d entries for empty files, got %d", len(emptyFiles), len(entries))
		}

		// Verify empty files have zero file size
		for _, entry := range entries {
			if entry.FileSize != 0 {
				t.Errorf("Empty file has non-zero size: %s has size %d",
					entry.RelativePath(), entry.FileSize)
			}
		}

		// Verify hash is hash of empty content
		expectedEmptyHash := "da39a3ee5e6b4b0d3255bfef95601890afd80709" // SHA-1 of empty string
		for _, entry := range entries {
			if entry.HashString() != expectedEmptyHash {
				t.Errorf("Empty file has wrong hash: expected %s, got %s",
					expectedEmptyHash, entry.HashString())
			}
		}
	})

	t.Run("VeryLargeFiles", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "large_files_test_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create a moderately large file (1MB)
		largeFilePath := filepath.Join(tempDir, "large_file.txt")
		largeContent := make([]byte, 1024*1024) // 1MB
		for i := range largeContent {
			largeContent[i] = byte(i % 256)
		}

		err = os.WriteFile(largeFilePath, largeContent, 0644)
		if err != nil {
			t.Fatalf("Failed to create large file: %v", err)
		}

		cache := NewDirectoryCache(tempDir, "")
		defer cache.Close()

		err = cache.ScanDirectory()
		if err != nil {
			t.Fatalf("ScanDirectory failed with large file: %v", err)
		}

		err = cache.LoadIndex()
		if err != nil {
			t.Fatalf("LoadIndex failed with large file: %v", err)
		}

		entries := cache.GetEntries()
		if len(entries) != 1 {
			t.Errorf("Expected 1 entry for large file, got %d", len(entries))
		}

		if len(entries) > 0 {
			if entries[0].FileSize != uint64(len(largeContent)) {
				t.Errorf("Large file has wrong size: expected %d, got %d",
					len(largeContent), entries[0].FileSize)
			}
		}
	})

	t.Run("DeepDirectoryStructure", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "deep_dirs_test_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create a deep directory structure
		deepPath := tempDir
		for i := 0; i < 10; i++ {
			deepPath = filepath.Join(deepPath, "level"+string(rune('0'+i)))
		}

		err = os.MkdirAll(deepPath, 0755)
		if err != nil {
			t.Fatalf("Failed to create deep directory structure: %v", err)
		}

		// Create a file in the deep directory
		deepFile := filepath.Join(deepPath, "deep_file.txt")
		err = os.WriteFile(deepFile, []byte("deep content"), 0644)
		if err != nil {
			t.Fatalf("Failed to create file in deep directory: %v", err)
		}

		cache := NewDirectoryCache(tempDir, "")
		defer cache.Close()

		err = cache.ScanDirectory()
		if err != nil {
			t.Fatalf("ScanDirectory failed with deep directory: %v", err)
		}

		err = cache.LoadIndex()
		if err != nil {
			t.Fatalf("LoadIndex failed with deep directory: %v", err)
		}

		entries := cache.GetEntries()
		if len(entries) != 1 {
			t.Errorf("Expected 1 entry in deep directory, got %d", len(entries))
		}

		if len(entries) > 0 {
			expectedPath := "level0/level1/level2/level3/level4/level5/level6/level7/level8/level9/deep_file.txt"
			if entries[0].RelativePath() != expectedPath {
				t.Errorf("Wrong relative path for deep file: expected %s, got %s",
					expectedPath, entries[0].RelativePath())
			}
		}
	})

	t.Run("SymlinksAndSpecialFiles", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "symlinks_test_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create a regular file
		regularFile := filepath.Join(tempDir, "regular.txt")
		err = os.WriteFile(regularFile, []byte("regular content"), 0644)
		if err != nil {
			t.Fatalf("Failed to create regular file: %v", err)
		}

		// Create a symlink
		symlinkPath := filepath.Join(tempDir, "symlink.txt")
		err = os.Symlink(regularFile, symlinkPath)
		if err != nil {
			t.Logf("Warning: Failed to create symlink (may not be supported): %v", err)
		}

		cache := NewDirectoryCache(tempDir, "")
		defer cache.Close()

		err = cache.ScanDirectory()
		if err != nil {
			t.Fatalf("ScanDirectory failed with symlinks: %v", err)
		}

		err = cache.LoadIndex()
		if err != nil {
			t.Fatalf("LoadIndex failed with symlinks: %v", err)
		}

		entries := cache.GetEntries()
		// Should only include regular files, not symlinks
		if len(entries) != 1 {
			t.Errorf("Expected 1 entry (regular file only), got %d", len(entries))
		}

		if len(entries) > 0 {
			if entries[0].RelativePath() != "regular.txt" {
				t.Errorf("Expected regular.txt, got %s", entries[0].RelativePath())
			}
		}
	})

	t.Run("ConcurrentAccess", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "concurrent_test_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create some test files
		for i := 0; i < 10; i++ {
			filename := filepath.Join(tempDir, "file"+string(rune('0'+i))+".txt")
			content := "content " + string(rune('0'+i))
			err = os.WriteFile(filename, []byte(content), 0644)
			if err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}
		}

		cache := NewDirectoryCache(tempDir, "")
		defer cache.Close()

		err = cache.ScanDirectory()
		if err != nil {
			t.Fatalf("ScanDirectory failed: %v", err)
		}

		err = cache.LoadIndex()
		if err != nil {
			t.Fatalf("LoadIndex failed: %v", err)
		}

		// Test concurrent access to entry methods
		entries := cache.GetEntries()
		if len(entries) == 0 {
			t.Fatal("No entries to test concurrent access")
		}

		// Run multiple goroutines accessing the same entry
		done := make(chan bool, 10)
		for i := 0; i < 10; i++ {
			go func() {
				defer func() { done <- true }()
				for j := 0; j < 100; j++ {
					_ = entries[0].RelativePath()
					_ = entries[0].HashString()
					_ = entries[0].EntrySize()
					_ = entries[0].RelativePathBytes()
				}
			}()
		}

		// Wait for all goroutines to complete
		for i := 0; i < 10; i++ {
			<-done
		}
	})

	t.Run("FileModificationDuringScanning", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "modification_test_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create initial file
		testFile := filepath.Join(tempDir, "modifiable.txt")
		err = os.WriteFile(testFile, []byte("initial content"), 0644)
		if err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		cache := NewDirectoryCache(tempDir, "")
		defer cache.Close()

		// Create initial index
		err = cache.ScanDirectory()
		if err != nil {
			t.Fatalf("ScanDirectory failed: %v", err)
		}

		// Modify file with different timestamps
		time.Sleep(time.Millisecond * 10) // Ensure different timestamp
		err = os.WriteFile(testFile, []byte("modified content"), 0644)
		if err != nil {
			t.Fatalf("Failed to modify test file: %v", err)
		}

		// Load the original index
		err = cache.LoadIndex()
		if err != nil {
			t.Fatalf("LoadIndex failed: %v", err)
		}

		// Check status - should detect modification
		status, err := cache.Status()
		if err != nil {
			t.Fatalf("Status failed: %v", err)
		}

		if len(status.Modified) != 1 {
			t.Errorf("Expected 1 modified file, got %d", len(status.Modified))
		}
	})

	t.Run("CorruptedIndexFile", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "corrupted_index_test_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		cache := NewDirectoryCache(tempDir, "")
		defer cache.Close()

		// Create a valid index first
		testFile := filepath.Join(tempDir, "test.txt")
		err = os.WriteFile(testFile, []byte("test content"), 0644)
		if err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		err = cache.ScanDirectory()
		if err != nil {
			t.Fatalf("ScanDirectory failed: %v", err)
		}

		// Corrupt the index file
		err = os.WriteFile(cache.IndexFile, []byte("corrupted data"), 0644)
		if err != nil {
			t.Fatalf("Failed to corrupt index file: %v", err)
		}

		// Loading the corrupted index should fail
		err = cache.LoadIndex()
		if err == nil {
			t.Error("Expected error when loading corrupted index file")
		}
	})

	t.Run("MultipleCacheInstances", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "multiple_cache_test_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create test file
		testFile := filepath.Join(tempDir, "test.txt")
		err = os.WriteFile(testFile, []byte("test content"), 0644)
		if err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		// Create first cache and build index
		cache1 := NewDirectoryCache(tempDir, "")
		defer cache1.Close()

		err = cache1.ScanDirectory()
		if err != nil {
			t.Fatalf("ScanDirectory failed: %v", err)
		}

		// Create second cache instance pointing to same directory
		cache2 := NewDirectoryCache(tempDir, "")
		defer cache2.Close()

		// Second cache should be able to load the index created by first cache
		err = cache2.LoadIndex()
		if err != nil {
			t.Fatalf("LoadIndex failed on second cache: %v", err)
		}

		// Both caches should have the same entries
		entries1 := cache1.GetEntries()
		entries2 := cache2.GetEntries()

		if len(entries1) != len(entries2) {
			t.Errorf("Caches have different number of entries: %d vs %d", len(entries1), len(entries2))
		}

		// Verify entries are the same
		for i := 0; i < len(entries1) && i < len(entries2); i++ {
			if entries1[i].RelativePath() != entries2[i].RelativePath() {
				t.Errorf("Entry mismatch at index %d: %s vs %s",
					i, entries1[i].RelativePath(), entries2[i].RelativePath())
			}
		}
	})
}

func TestPathLenToSize(t *testing.T) {
	testCases := []struct {
		pathLen  int
		expected int
	}{
		{0, int(unsafe.Sizeof(binaryEntry{})) + 1 + 7},     // +1 for null terminator, +7 for 8-byte alignment
		{1, int(unsafe.Sizeof(binaryEntry{})) + 2 + 6},     // +2 for char+null, +6 for alignment
		{7, int(unsafe.Sizeof(binaryEntry{})) + 8},         // Exactly aligned
		{8, int(unsafe.Sizeof(binaryEntry{})) + 9 + 7},     // +9 for chars+null, +7 for alignment
		{15, int(unsafe.Sizeof(binaryEntry{})) + 16},       // Exactly aligned
		{100, int(unsafe.Sizeof(binaryEntry{})) + 101 + 3}, // +101 for chars+null, +3 for alignment
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("pathLen_%d", tc.pathLen), func(t *testing.T) {
			result := PathLenToSize(tc.pathLen)
			if result != tc.expected {
				t.Errorf("PathLenToSize(%d) = %d, expected %d", tc.pathLen, result, tc.expected)
			}

			// Verify result is 8-byte aligned
			if result%8 != 0 {
				t.Errorf("PathLenToSize(%d) = %d, which is not 8-byte aligned", tc.pathLen, result)
			}
		})
	}
}

func TestHashTypeConstants(t *testing.T) {
	// Verify hash type constants have expected values
	if HashTypeSHA1 != 1 {
		t.Errorf("HashTypeSHA1 = %d, expected 1", HashTypeSHA1)
	}
	if HashTypeSHA256 != 2 {
		t.Errorf("HashTypeSHA256 = %d, expected 2", HashTypeSHA256)
	}
	if HashTypeSHA512 != 3 {
		t.Errorf("HashTypeSHA512 = %d, expected 3", HashTypeSHA512)
	}

	// Verify hash size constants
	if HashSizeSHA1 != 20 {
		t.Errorf("HashSizeSHA1 = %d, expected 20", HashSizeSHA1)
	}
	if HashSizeSHA256 != 32 {
		t.Errorf("HashSizeSHA256 = %d, expected 32", HashSizeSHA256)
	}
	if HashSizeSHA512 != 64 {
		t.Errorf("HashSizeSHA512 = %d, expected 64", HashSizeSHA512)
	}
}

func TestStatusResultMethods(t *testing.T) {
	// Test empty result
	emptyResult := &StatusResult{}
	if emptyResult.HasChanges() {
		t.Error("Empty StatusResult should not have changes")
	}
	if emptyResult.TotalChanges() != 0 {
		t.Errorf("Empty StatusResult TotalChanges should be 0, got %d", emptyResult.TotalChanges())
	}

	// Test result with changes
	result := &StatusResult{
		Modified: []string{"file1.txt", "file2.txt"},
		Added:    []string{"file3.txt"},
		Deleted:  []string{"file4.txt", "file5.txt", "file6.txt"},
	}

	if !result.HasChanges() {
		t.Error("StatusResult with changes should return true for HasChanges")
	}

	expectedTotal := 6
	if result.TotalChanges() != expectedTotal {
		t.Errorf("StatusResult TotalChanges should be %d, got %d", expectedTotal, result.TotalChanges())
	}
}

func TestFileStatusConstants(t *testing.T) {
	// Verify FileStatus constants have expected values
	if StatusUnchanged != 0 {
		t.Errorf("StatusUnchanged = %d, expected 0", StatusUnchanged)
	}
	if StatusModified != 1 {
		t.Errorf("StatusModified = %d, expected 1", StatusModified)
	}
	if StatusAdded != 2 {
		t.Errorf("StatusAdded = %d, expected 2", StatusAdded)
	}
	if StatusDeleted != 3 {
		t.Errorf("StatusDeleted = %d, expected 3", StatusDeleted)
	}
}
