package dircachefilehash

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBasicIntegration validates core functionality without complex status semantics
func TestBasicIntegration(t *testing.T) {
	// Create test sandbox
	testDir := filepath.Join(".", "test-basic-sandbox")
	os.RemoveAll(testDir)
	defer os.RemoveAll(testDir)

	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	// Initialise dcfh repository
	dc := NewDirectoryCache(testDir, testDir)

	t.Run("EmptyRepository", func(t *testing.T) {
		// Test empty repository initialization
		mainSkiplist, err := dc.LoadMainIndex()
		if err != nil {
			t.Fatalf("Failed to load main index: %v", err)
		}

		if mainSkiplist.Length() != 0 {
			t.Errorf("Expected empty main index, got %d entries", mainSkiplist.Length())
		}
	})

	t.Run("CreateAndUpdateFiles", func(t *testing.T) {
		// Create test files with known content
		testFiles := map[string]string{
			"test1.txt":      "Hello, World!\n",
			"test2.txt":      "Another test file\n",
			"dir/nested.txt": "Nested file content\n",
		}

		for relPath, content := range testFiles {
			fullPath := filepath.Join(testDir, relPath)
			os.MkdirAll(filepath.Dir(fullPath), 0755)
			if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
				t.Fatalf("Failed to create %s: %v", relPath, err)
			}
		}

		// Update index
		if err := dc.Update(nil, map[string]string{}); err != nil {
			t.Fatalf("Update failed: %v", err)
		}

		// Verify index contains correct files with correct hashes
		mainSkiplist, err := dc.LoadMainIndex()
		if err != nil {
			t.Fatalf("Failed to load main index: %v", err)
		}

		if mainSkiplist.Length() != len(testFiles) {
			t.Errorf("Expected %d files in index, got %d", len(testFiles), mainSkiplist.Length())
		}

		// Verify each file has correct hash
		for expectedPath, expectedContent := range testFiles {
			found := false
			expectedHash := calculateFileSHA256(expectedContent)

			mainSkiplist.ForEach(func(entry *binaryEntry, context string) bool {
				if entry.RelativePath() == expectedPath {
					found = true
					actualHash := entry.HashString()
					if actualHash != expectedHash {
						t.Errorf("Hash mismatch for %s.\nExpected: %s\nActual: %s",
							expectedPath, expectedHash, actualHash)
					}
				}
				return true
			})

			if !found {
				t.Errorf("File %s not found in index", expectedPath)
			}
		}
	})

	t.Run("ModifyFiles", func(t *testing.T) {
		// Modify an existing file
		modifiedContent := "Modified content\n"
		modifiedFile := filepath.Join(testDir, "test1.txt")
		if err := os.WriteFile(modifiedFile, []byte(modifiedContent), 0644); err != nil {
			t.Fatalf("Failed to modify test1.txt: %v", err)
		}

		// Update index
		if err := dc.Update(nil, map[string]string{}); err != nil {
			t.Fatalf("Update after modification failed: %v", err)
		}

		// Verify the modified file has the new hash
		mainSkiplist, err := dc.LoadMainIndex()
		if err != nil {
			t.Fatalf("Failed to load main index after modification: %v", err)
		}

		expectedHash := calculateFileSHA256(modifiedContent)
		found := false

		mainSkiplist.ForEach(func(entry *binaryEntry, context string) bool {
			if entry.RelativePath() == "test1.txt" {
				found = true
				actualHash := entry.HashString()
				if actualHash != expectedHash {
					t.Errorf("Modified file hash mismatch.\nExpected: %s\nActual: %s",
						expectedHash, actualHash)
				}
			}
			return true
		})

		if !found {
			t.Errorf("Modified file test1.txt not found in index")
		}
	})

	t.Run("CacheSystem", func(t *testing.T) {
		// Modify a file to trigger cache creation
		testFile := filepath.Join(testDir, "test2.txt")
		newContent := "Cache test content\n"
		if err := os.WriteFile(testFile, []byte(newContent), 0644); err != nil {
			t.Fatalf("Failed to modify test2.txt for cache test: %v", err)
		}

		// Run status to create cache
		_, err := dc.Status(nil, map[string]string{})
		if err != nil {
			t.Fatalf("Status failed: %v", err)
		}

		// Verify cache file exists
		if _, err := os.Stat(dc.CacheFile); os.IsNotExist(err) {
			t.Errorf("Cache file should exist after status: %s", dc.CacheFile)
		}

		// Load cache and verify it contains the modification
		cacheSkiplist, err := dc.loadCacheIndex()
		if err != nil {
			t.Fatalf("Failed to load cache index: %v", err)
		}

		expectedHash := calculateFileSHA256(newContent)
		found := false

		cacheSkiplist.ForEach(func(entry *binaryEntry, context string) bool {
			if entry.RelativePath() == "test2.txt" {
				found = true
				actualHash := entry.HashString()
				if actualHash != expectedHash {
					t.Errorf("Cache hash mismatch for test2.txt.\nExpected: %s\nActual: %s",
						expectedHash, actualHash)
				}
			}
			return true
		})

		if !found {
			t.Errorf("Modified file test2.txt not found in cache")
		}

		// Update main index to include the cache changes for integrity test
		if err := dc.Update(nil, map[string]string{}); err != nil {
			t.Fatalf("Failed to update main index after cache test: %v", err)
		}
	})

	t.Run("IndexIntegrity", func(t *testing.T) {
		// Verify all files in index have correct hashes matching disk
		mainSkiplist, err := dc.LoadMainIndex()
		if err != nil {
			t.Fatalf("Failed to load main index for integrity check: %v", err)
		}

		hashMismatches := 0
		mainSkiplist.ForEach(func(entry *binaryEntry, context string) bool {
			if entry.IsDeleted() {
				return true // Skip deleted entries
			}

			filePath := filepath.Join(dc.RootDir, entry.RelativePath())
			if _, err := os.Stat(filePath); os.IsNotExist(err) {
				// File doesn't exist on disk - this might be expected
				return true
			}

			// Calculate actual file hash
			actualHash, err := calculateDiskFileHash(filePath)
			if err != nil {
				t.Errorf("Failed to calculate hash for %s: %v", entry.RelativePath(), err)
				return true
			}

			storedHash := entry.HashString()
			if actualHash != storedHash {
				t.Errorf("Index integrity violation for %s.\nStored: %s\nActual: %s",
					entry.RelativePath(), storedHash, actualHash)
				hashMismatches++
			}

			return true
		})

		if hashMismatches > 0 {
			t.Errorf("Found %d hash integrity violations", hashMismatches)
		} else {
			t.Logf("Index integrity check passed: all hashes match disk content")
		}
	})

	t.Run("CleanupValidation", func(t *testing.T) {
		// Verify no scan index files are left behind
		dcfhDir := filepath.Join(testDir, ".dcfh")
		entries, err := os.ReadDir(dcfhDir)
		if err != nil {
			t.Fatalf("Failed to read .dcfh directory: %v", err)
		}

		scanFiles := 0
		for _, entry := range entries {
			if strings.Contains(entry.Name(), "scan-") && strings.HasSuffix(entry.Name(), ".idx") {
				scanFiles++
				t.Errorf("Found leftover scan index file: %s", entry.Name())
			}
		}

		if scanFiles == 0 {
			t.Logf("Cleanup validation passed: no scan index files left behind")
		}
	})
}

// Helper functions

func calculateFileSHA256(content string) string {
	hasher := sha256.New()
	hasher.Write([]byte(content))
	return fmt.Sprintf("%x", hasher.Sum(nil))
}

func calculateDiskFileHash(filePath string) (string, error) {
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
