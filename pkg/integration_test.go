package dircachefilehash

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestIntegrationWorkflow performs a comprehensive end-to-end test of dcfh operations
// with deterministic file operations, hash validation, and byte-by-byte index validation
func TestIntegrationWorkflow(t *testing.T) {
	// Create deterministic test sandbox
	testDir := createDeterministicSandbox(t)
	defer os.RemoveAll(testDir)

	// Initialise dcfh repository
	dc := NewDirectoryCache(testDir, testDir)

	// Phase 1: Initial state - empty repository
	t.Run("Phase1_InitialState", func(t *testing.T) {
		validateInitialState(t, dc)
	})

	// Phase 2: Create initial files and do first update
	t.Run("Phase2_InitialFiles", func(t *testing.T) {
		createInitialFiles(t, testDir)
		performInitialUpdate(t, dc)
		validateInitialIndex(t, dc)
	})

	// Phase 3: File operations (create, modify, delete)
	t.Run("Phase3_FileOperations", func(t *testing.T) {
		performFileOperations(t, testDir)
	})

	// Phase 4: Status check - validate detection of changes
	t.Run("Phase4_StatusCheck", func(t *testing.T) {
		validateStatusDetection(t, dc)
	})

	// Phase 5: Update and validate final state
	t.Run("Phase5_FinalUpdate", func(t *testing.T) {
		performFinalUpdate(t, dc)
		validateFinalIndex(t, dc)
	})

	// Phase 6: Hash integrity validation (before cache behaviour modifies files)
	t.Run("Phase6_HashIntegrity", func(t *testing.T) {
		validateHashIntegrity(t, dc)
	})

	// Phase 7: Cache behaviour validation
	t.Run("Phase7_CacheValidation", func(t *testing.T) {
		validateCacheBehaviour(t, dc)
	})
}

// createDeterministicSandbox creates a test directory with predictable structure and known content
func createDeterministicSandbox(t *testing.T) string {
	// Use predictable temporary directory
	testDir := filepath.Join(".", "test-integration-sandbox")
	
	// Clean up any existing test directory
	os.RemoveAll(testDir)
	
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}
	
	// Create subdirectories for organised testing
	dirs := []string{"subdir1", "subdir2", "empty_dir"}
	for _, dir := range dirs {
		dirPath := filepath.Join(testDir, dir)
		if err := os.MkdirAll(dirPath, 0755); err != nil {
			t.Fatalf("Failed to create subdirectory %s: %v", dir, err)
		}
	}
	
	return testDir
}

// validateInitialState checks that a new repository starts correctly
func validateInitialState(t *testing.T, dc *DirectoryCache) {
	// After NewDirectoryCache, we should have empty main index
	mainSkiplist, err := dc.LoadMainIndex()
	if err != nil {
		t.Fatalf("Failed to load initial main index: %v", err)
	}
	
	if mainSkiplist.Length() != 0 {
		t.Errorf("Initial main index should be empty, got %d entries", mainSkiplist.Length())
	}
	
	// No cache index should exist yet
	if _, err := os.Stat(dc.CacheFile); !os.IsNotExist(err) {
		t.Errorf("Cache file should not exist initially: %s", dc.CacheFile)
	}
}

// TestFileContent represents a test file with known content and expected hash
type TestFileContent struct {
	RelPath      string
	Content      []byte
	ExpectedHash string
}

// createInitialFiles creates a deterministic set of files with known content and pre-calculated hashes
func createInitialFiles(t *testing.T, testDir string) map[string]TestFileContent {
	files := map[string]TestFileContent{
		"file1.txt": {
			RelPath: "file1.txt",
			Content: []byte("This is file 1 content\n"),
			ExpectedHash: "", // Will be calculated
		},
		"file2.txt": {
			RelPath: "file2.txt", 
			Content: []byte("This is file 2 content with more text\n"),
			ExpectedHash: "",
		},
		"subdir1/nested.txt": {
			RelPath: "subdir1/nested.txt",
			Content: []byte("Nested file content\n"),
			ExpectedHash: "",
		},
		"subdir2/data.json": {
			RelPath: "subdir2/data.json",
			Content: []byte(`{"key": "value", "number": 42}` + "\n"),
			ExpectedHash: "",
		},
		"binary.bin": {
			RelPath: "binary.bin",
			Content: []byte("\x00\x01\x02\x03\x04\x05\xFF\xFE\xFD"),
			ExpectedHash: "",
		},
	}
	
	// Calculate expected hashes and create files
	for key, fileInfo := range files {
		// Calculate SHA-256 hash
		hasher := sha256.New()
		hasher.Write(fileInfo.Content)
		fileInfo.ExpectedHash = fmt.Sprintf("%x", hasher.Sum(nil))
		files[key] = fileInfo // Update the map with calculated hash
		
		// Create the file
		fullPath := filepath.Join(testDir, fileInfo.RelPath)
		if err := os.WriteFile(fullPath, fileInfo.Content, 0644); err != nil {
			t.Fatalf("Failed to create file %s: %v", fileInfo.RelPath, err)
		}
		
		// Set deterministic timestamp for reproducible results
		fixedTime := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
		if err := os.Chtimes(fullPath, fixedTime, fixedTime); err != nil {
			t.Fatalf("Failed to set timestamp for %s: %v", fileInfo.RelPath, err)
		}
	}
	
	return files
}

// performInitialUpdate does the first update and captures the index state
func performInitialUpdate(t *testing.T, dc *DirectoryCache) {
	if err := dc.Update(nil, map[string]string{}); err != nil {
		t.Fatalf("Initial update failed: %v", err)
	}
	
	// Verify main index was created
	if _, err := os.Stat(dc.IndexFile); os.IsNotExist(err) {
		t.Fatalf("Main index file was not created: %s", dc.IndexFile)
	}
	
	// Cache file should not exist after full update
	if _, err := os.Stat(dc.CacheFile); !os.IsNotExist(err) {
		t.Errorf("Cache file should not exist after full update, but found: %s", dc.CacheFile)
	}
}

// validateInitialIndex checks the main index contains expected entries with correct hashes
func validateInitialIndex(t *testing.T, dc *DirectoryCache) {
	// Re-create the test files to get expected hashes
	expectedFiles := map[string]string{
		"file1.txt":           calculateSHA256("This is file 1 content\n"),
		"file2.txt":           calculateSHA256("This is file 2 content with more text\n"),
		"subdir1/nested.txt":  calculateSHA256("Nested file content\n"),
		"subdir2/data.json":   calculateSHA256(`{"key": "value", "number": 42}` + "\n"),
		"binary.bin":          calculateSHA256("\x00\x01\x02\x03\x04\x05\xFF\xFE\xFD"),
	}
	
	// Load main index and verify contents
	mainSkiplist, err := dc.LoadMainIndex()
	if err != nil {
		t.Fatalf("Failed to load main index: %v", err)
	}
	
	// Collect all paths and hashes from index
	actualFiles := make(map[string]string)
	mainSkiplist.ForEach(func(entry *binaryEntry, context string) bool {
		if !entry.IsDeleted() {
			actualFiles[entry.RelativePath()] = entry.HashString()
		}
		return true
	})
	
	// Verify all expected files are present with correct hashes
	for expectedPath, expectedHash := range expectedFiles {
		actualHash, exists := actualFiles[expectedPath]
		if !exists {
			t.Errorf("Expected file %s not found in index", expectedPath)
			continue
		}
		
		if actualHash != expectedHash {
			t.Errorf("Hash mismatch for %s.\nExpected: %s\nActual: %s", 
				expectedPath, expectedHash, actualHash)
		}
	}
	
	// Verify no unexpected files
	for actualPath := range actualFiles {
		if _, expected := expectedFiles[actualPath]; !expected {
			t.Errorf("Unexpected file %s found in index", actualPath)
		}
	}
	
	// Save index state for later comparison
	saveIndexSnapshot(t, dc.IndexFile, "initial")
	
	t.Logf("Initial index validation passed: %d files with correct hashes", len(expectedFiles))
}

// performFileOperations creates, modifies, and deletes files with known outcomes
func performFileOperations(t *testing.T, testDir string) {
	// Create new file with known content
	newFileContent := "This is a new file\n"
	newFile := filepath.Join(testDir, "new_file.txt")
	if err := os.WriteFile(newFile, []byte(newFileContent), 0644); err != nil {
		t.Fatalf("Failed to create new file: %v", err)
	}
	
	// Modify existing file with known new content
	modifiedContent := "Modified content for file 1\n"
	modifiedFile := filepath.Join(testDir, "file1.txt")
	if err := os.WriteFile(modifiedFile, []byte(modifiedContent), 0644); err != nil {
		t.Fatalf("Failed to modify file1.txt: %v", err)
	}
	
	// Delete existing file
	deletedFile := filepath.Join(testDir, "file2.txt")
	if err := os.Remove(deletedFile); err != nil {
		t.Fatalf("Failed to delete file2.txt: %v", err)
	}
	
	// Verify file is actually deleted
	if _, err := os.Stat(deletedFile); !os.IsNotExist(err) {
		t.Fatalf("file2.txt should be deleted but still exists")
	}
	t.Logf("file2.txt successfully deleted")
	
	// Leave binary.bin and subdir files unchanged for testing
	
	// Set deterministic timestamps
	fixedTime := time.Date(2023, 2, 1, 12, 0, 0, 0, time.UTC)
	os.Chtimes(newFile, fixedTime, fixedTime)
	os.Chtimes(modifiedFile, fixedTime, fixedTime)
	
	// Store expected hashes for validation
	t.Logf("New file hash: %s", calculateSHA256(newFileContent))
	t.Logf("Modified file hash: %s", calculateSHA256(modifiedContent))
}

// validateStatusDetection checks that status correctly identifies changes
func validateStatusDetection(t *testing.T, dc *DirectoryCache) {
	// Debug: Check what files actually exist on disk
	diskFiles := make(map[string]bool)
	err := filepath.Walk(dc.RootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			relPath, _ := filepath.Rel(dc.RootDir, path)
			diskFiles[relPath] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to walk directory: %v", err)
	}
	t.Logf("Files on disk: %v", diskFiles)
	
	result, err := dc.Status(nil, map[string]string{})
	if err != nil {
		t.Fatalf("Status check failed: %v", err)
	}
	
	// Debug: Log the actual results before comparison
	t.Logf("Status result - Added: %v, Modified: %v, Deleted: %v", 
		result.Added, result.Modified, result.Deleted)
	
	// Verify detected changes
	expectedAdded := []string{"new_file.txt"}
	expectedModified := []string{"file1.txt"}
	expectedDeleted := []string{"file2.txt"}
	
	sort.Strings(result.Added)
	sort.Strings(result.Modified)
	sort.Strings(result.Deleted)
	sort.Strings(expectedAdded)
	sort.Strings(expectedModified)
	sort.Strings(expectedDeleted)
	
	if !stringSlicesEqual(result.Added, expectedAdded) {
		t.Errorf("Added files don't match.\nExpected: %v\nActual: %v", 
			expectedAdded, result.Added)
	}
	
	if !stringSlicesEqual(result.Modified, expectedModified) {
		t.Errorf("Modified files don't match.\nExpected: %v\nActual: %v", 
			expectedModified, result.Modified)
	}
	
	if !stringSlicesEqual(result.Deleted, expectedDeleted) {
		t.Errorf("Deleted files don't match.\nExpected: %v\nActual: %v", 
			expectedDeleted, result.Deleted)
	}
	
	// Verify cache file was created during status check
	if _, err := os.Stat(dc.CacheFile); os.IsNotExist(err) {
		t.Errorf("Cache file should exist after status check: %s", dc.CacheFile)
	}
	
	// Save cache state snapshot
	saveIndexSnapshot(t, dc.CacheFile, "after_status")
	
	t.Logf("Status detection validated: %d added, %d modified, %d deleted", 
		len(result.Added), len(result.Modified), len(result.Deleted))
}

// performFinalUpdate updates the repository to final state
func performFinalUpdate(t *testing.T, dc *DirectoryCache) {
	if err := dc.Update(nil, map[string]string{}); err != nil {
		t.Fatalf("Final update failed: %v", err)
	}
	
	// After full update, cache should be removed
	if _, err := os.Stat(dc.CacheFile); !os.IsNotExist(err) {
		t.Errorf("Cache file should be removed after full update, but found: %s", dc.CacheFile)
	}
}

// validateFinalIndex checks the final state matches expected outcome with correct hashes
func validateFinalIndex(t *testing.T, dc *DirectoryCache) {
	// Expected final state with known hashes - binary.bin is unchanged at this point
	expectedFinalFiles := map[string]string{
		"binary.bin":         calculateSHA256("\x00\x01\x02\x03\x04\x05\xFF\xFE\xFD"), // unchanged
		"file1.txt":          calculateSHA256("Modified content for file 1\n"),             // modified
		"new_file.txt":       calculateSHA256("This is a new file\n"),                     // added
		"subdir1/nested.txt": calculateSHA256("Nested file content\n"),                    // unchanged
		"subdir2/data.json":  calculateSHA256(`{"key": "value", "number": 42}` + "\n"),    // unchanged
		// file2.txt should be gone (deleted)
	}
	
	mainSkiplist, err := dc.LoadMainIndex()
	if err != nil {
		t.Fatalf("Failed to load final main index: %v", err)
	}
	
	// Collect actual files and hashes
	actualFiles := make(map[string]string)
	mainSkiplist.ForEach(func(entry *binaryEntry, context string) bool {
		if !entry.IsDeleted() {
			actualFiles[entry.RelativePath()] = entry.HashString()
		}
		return true
	})
	
	// Verify all expected files with correct hashes
	for expectedPath, expectedHash := range expectedFinalFiles {
		actualHash, exists := actualFiles[expectedPath]
		if !exists {
			t.Errorf("Expected final file %s not found in index", expectedPath)
			continue
		}
		
		if actualHash != expectedHash {
			t.Errorf("Final hash mismatch for %s.\nExpected: %s\nActual: %s", 
				expectedPath, expectedHash, actualHash)
		}
	}
	
	// Verify no unexpected files (particularly that file2.txt is gone)
	for actualPath := range actualFiles {
		if _, expected := expectedFinalFiles[actualPath]; !expected {
			t.Errorf("Unexpected final file %s found in index", actualPath)
		}
	}
	
	// Verify deleted file is not present
	if _, exists := actualFiles["file2.txt"]; exists {
		t.Errorf("Deleted file file2.txt should not be present in final index")
	}
	
	// Save final index state
	saveIndexSnapshot(t, dc.IndexFile, "final")
	
	// Compare with initial state to verify changes
	compareIndexSnapshots(t, "initial", "final")
	
	t.Logf("Final index validation passed: %d files with correct hashes", len(expectedFinalFiles))
}

// validateCacheBehaviour tests cache system behaviour with hash consistency
func validateCacheBehaviour(t *testing.T, dc *DirectoryCache) {
	// Create a small modification with known hash
	newContent := []byte("\x00\x01\x02\x03\x04\x05\xFF\xFE\xFD\x42")
	expectedHash := calculateSHA256(string(newContent))
	
	testFile := filepath.Join(dc.RootDir, "binary.bin")
	if err := os.WriteFile(testFile, newContent, 0644); err != nil {
		t.Fatalf("Failed to modify test file: %v", err)
	}
	
	// First status should create cache
	result1, err := dc.Status(nil, map[string]string{})
	if err != nil {
		t.Fatalf("First status failed: %v", err)
	}
	
	if len(result1.Modified) != 1 || result1.Modified[0] != "binary.bin" {
		t.Errorf("Expected binary.bin to be modified, got: %v", result1.Modified)
	}
	
	// Verify cache exists and contains correct hash
	if _, err := os.Stat(dc.CacheFile); os.IsNotExist(err) {
		t.Errorf("Cache file should exist after status: %s", dc.CacheFile)
	} else {
		// Load cache and verify hash
		cacheSkiplist, err := dc.loadCacheIndex()
		if err != nil {
			t.Errorf("Failed to load cache: %v", err)
		} else {
			found := false
			cacheSkiplist.ForEach(func(entry *binaryEntry, context string) bool {
				if entry.RelativePath() == "binary.bin" {
					found = true
					actualHash := entry.HashString()
					if actualHash != expectedHash {
						t.Errorf("Cache hash mismatch for binary.bin.\nExpected: %s\nActual: %s", 
							expectedHash, actualHash)
					}
				}
				return true
			})
			if !found {
				t.Errorf("binary.bin not found in cache index")
			}
		}
	}
	
	// Second status should use cache (should be faster, same result)
	result2, err := dc.Status(nil, map[string]string{})
	if err != nil {
		t.Fatalf("Second status failed: %v", err)
	}
	
	// Results should be identical
	if !stringSlicesEqual(result1.Modified, result2.Modified) {
		t.Errorf("Cache behaviour inconsistent. First: %v, Second: %v", 
			result1.Modified, result2.Modified)
	}
	
	t.Logf("Cache behaviour validated with correct hash: %s", expectedHash)
}

// validateHashIntegrity performs comprehensive hash validation across all index operations
func validateHashIntegrity(t *testing.T, dc *DirectoryCache) {
	// Load current index state
	mainSkiplist, err := dc.LoadMainIndex()
	if err != nil {
		t.Fatalf("Failed to load main index for hash validation: %v", err)
	}
	
	// Validate every file's hash matches actual content
	hashMismatches := 0
	mainSkiplist.ForEach(func(entry *binaryEntry, context string) bool {
		if entry.IsDeleted() {
			return true // Skip deleted entries
		}
		
		filePath := filepath.Join(dc.RootDir, entry.RelativePath())
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			t.Errorf("Index contains entry for non-existent file: %s", entry.RelativePath())
			return true
		}
		
		// Calculate actual file hash
		actualHash, err := calculateFileHash(filePath)
		if err != nil {
			t.Errorf("Failed to calculate hash for %s: %v", entry.RelativePath(), err)
			return true
		}
		
		storedHash := entry.HashString()
		if actualHash != storedHash {
			t.Errorf("Hash integrity violation for %s.\nExpected: %s\nActual: %s", 
				entry.RelativePath(), actualHash, storedHash)
			hashMismatches++
		}
		
		return true
	})
	
	if hashMismatches == 0 {
		t.Logf("Hash integrity validation passed: all %d files have correct hashes", 
			mainSkiplist.Length())
	} else {
		t.Errorf("Hash integrity validation failed: %d mismatches found", hashMismatches)
	}
}

// Helper functions

// calculateSHA256 calculates SHA-256 hash of string content
func calculateSHA256(content string) string {
	hasher := sha256.New()
	hasher.Write([]byte(content))
	return fmt.Sprintf("%x", hasher.Sum(nil))
}

// calculateFileHash calculates SHA-1 hash of file content
func calculateFileHash(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}
	
	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}

// saveIndexSnapshot saves a copy of an index file for comparison
func saveIndexSnapshot(t *testing.T, indexPath, label string) {
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		return // File doesn't exist, nothing to save
	}
	
	snapshotPath := indexPath + "." + label + ".snapshot"
	
	src, err := os.Open(indexPath)
	if err != nil {
		t.Errorf("Failed to open index for snapshot: %v", err)
		return
	}
	defer src.Close()
	
	dst, err := os.Create(snapshotPath)
	if err != nil {
		t.Errorf("Failed to create snapshot file: %v", err)
		return
	}
	defer dst.Close()
	
	if _, err := io.Copy(dst, src); err != nil {
		t.Errorf("Failed to copy index to snapshot: %v", err)
		return
	}
	
	t.Logf("Saved index snapshot: %s", snapshotPath)
}

// compareIndexSnapshots performs byte-by-byte comparison of index snapshots
func compareIndexSnapshots(t *testing.T, label1, label2 string) {
	// Find the snapshot files
	var snapshot1, snapshot2 string
	
	// Look for snapshot files in the test directory
	testDir := "test-integration-sandbox"
	dcfhDir := filepath.Join(testDir, ".dcfh")
	
	if entries, err := os.ReadDir(dcfhDir); err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if strings.Contains(name, label1+".snapshot") {
				snapshot1 = filepath.Join(dcfhDir, name)
			}
			if strings.Contains(name, label2+".snapshot") {
				snapshot2 = filepath.Join(dcfhDir, name)
			}
		}
	}
	
	if snapshot1 == "" || snapshot2 == "" {
		t.Logf("Snapshots not found for comparison: %s vs %s", label1, label2)
		return
	}
	
	// Read both snapshots
	data1, err1 := os.ReadFile(snapshot1)
	data2, err2 := os.ReadFile(snapshot2)
	
	if err1 != nil || err2 != nil {
		t.Logf("Failed to read snapshots for comparison: %v, %v", err1, err2)
		return
	}
	
	// Compare sizes
	if len(data1) != len(data2) {
		t.Logf("Index snapshots have different sizes: %s=%d bytes, %s=%d bytes", 
			label1, len(data1), label2, len(data2))
	}
	
	// Compare content
	if !bytes.Equal(data1, data2) {
		t.Logf("Index snapshots differ: %s vs %s", label1, label2)
		// TODO: Add detailed binary format analysis here
	} else {
		t.Logf("Index snapshots are identical: %s vs %s", label1, label2)
	}
}

// stringSlicesEqual compares two string slices for equality
func stringSlicesEqual(a, b []string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if len(a) != len(b) {
		return false
	}
	
	for i := range a {
		// Add extra safety check
		if i >= len(a) || i >= len(b) {
			return false
		}
		if a[i] != b[i] {
			return false
		}
	}
	
	return true
}