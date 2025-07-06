package dircachefilehash

import (
	"os"
	"path/filepath"
	"testing"
	"time"
	"unsafe"
)

func TestCacheSystem(t *testing.T) {
	// Set TMPDIR to local directory for testing compatibility
	origTmpDir := os.Getenv("TMPDIR")
	defer func() {
		if origTmpDir == "" {
			os.Unsetenv("TMPDIR")
		} else {
			os.Setenv("TMPDIR", origTmpDir)
		}
	}()

	// Create temporary directory under project
	localTmpDir := filepath.Join(".", "test-tmp")
	if err := os.MkdirAll(localTmpDir, 0755); err != nil {
		t.Fatalf("Failed to create local tmp dir: %v", err)
	}
	defer os.RemoveAll(localTmpDir)

	os.Setenv("TMPDIR", localTmpDir)

	// Create test directory
	testDir, err := os.MkdirTemp("", "dcfh-cache-test-*")
	if err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}
	defer os.RemoveAll(testDir)

	// Create deterministic test files
	testFiles := map[string]string{
		"file1.txt":        "content of file 1",
		"file2.txt":        "content of file 2",
		"file3.txt":        "content of file 3",
		"subdir/file4.txt": "content of file 4",
	}

	for relPath, content := range testFiles {
		fullPath := filepath.Join(testDir, relPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("Failed to create directory for %s: %v", relPath, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", relPath, err)
		}
	}

	// Initialise dcfh repository
	dc := NewDirectoryCache(testDir, "")

	// Enable debug logging for this test
	SetVerboseLevel(3)
	SetDebugFlags("scan")

	// Test 1: Initial status (should populate cache)
	t.Run("InitialStatus", func(t *testing.T) {
		result, err := dc.Status(nil, map[string]string{})
		if err != nil {
			t.Fatalf("Initial status failed: %v", err)
		}

		// Should show all files as new (not in main index yet)
		expectedFiles := len(testFiles)
		if len(result.Added) != expectedFiles {
			t.Errorf("Expected %d added files, got %d", expectedFiles, len(result.Added))
		}

		// Check cache file exists and has content
		cacheFile := filepath.Join(testDir, ".dcfh", "cache.idx")
		stat, err := os.Stat(cacheFile)
		if err != nil {
			t.Errorf("Cache file should exist after status: %v", err)
		} else if stat.Size() <= 88 { // Header is 88 bytes, should have more content
			t.Errorf("Cache file too small: %d bytes (should be > 88)", stat.Size())
		}
	})

	// Test 2: Second status (should be fast and use cache)
	t.Run("SecondStatus", func(t *testing.T) {
		start := time.Now()
		result, err := dc.Status(nil, map[string]string{})
		duration := time.Since(start)

		if err != nil {
			t.Fatalf("Second status failed: %v", err)
		}

		// Should still show files as added (cache working, no new scan needed)
		expectedFiles := len(testFiles)
		if len(result.Added) != expectedFiles {
			t.Errorf("Expected %d added files, got %d", expectedFiles, len(result.Added))
		}

		// Should be fast (< 100ms for small dataset)
		if duration > 100*time.Millisecond {
			t.Errorf("Second status too slow: %v (should be < 100ms)", duration)
		} else {
			t.Logf("Second status completed in %v (cache working!)", duration)
		}
	})

	// Test 3: Update index (move files from cache to main)
	t.Run("UpdateIndex", func(t *testing.T) {
		err := dc.Update(nil, map[string]string{})
		if err != nil {
			t.Fatalf("Update failed: %v", err)
		}

		// Check main index exists and has content
		mainFile := filepath.Join(testDir, ".dcfh", "main.idx")
		stat, err := os.Stat(mainFile)
		if err != nil {
			t.Errorf("Main index should exist after update: %v", err)
		} else if stat.Size() <= 88 {
			t.Errorf("Main index too small: %d bytes", stat.Size())
		}
	})

	// Test 4: Status after update (should show clean)
	t.Run("StatusAfterUpdate", func(t *testing.T) {
		start := time.Now()
		result, err := dc.Status(nil, map[string]string{})
		duration := time.Since(start)

		if err != nil {
			t.Fatalf("Status after update failed: %v", err)
		}

		// Should show no changes (all files now in main index)
		if result.HasChanges() {
			t.Errorf("Expected no changes after update, got: %d modified, %d added, %d deleted",
				len(result.Modified), len(result.Added), len(result.Deleted))
		}

		// Should still be fast
		if duration > 100*time.Millisecond {
			t.Errorf("Status after update too slow: %v", duration)
		}

		t.Logf("Status after update: clean repository in %v", duration)
	})

	// Test 5: Modify a file and test cache behaviour
	t.Run("ModifyFile", func(t *testing.T) {
		// Modify one file
		modifiedFile := filepath.Join(testDir, "file1.txt")
		if err := os.WriteFile(modifiedFile, []byte("MODIFIED CONTENT"), 0644); err != nil {
			t.Fatalf("Failed to modify test file: %v", err)
		}

		// Status should detect the change
		result, err := dc.Status(nil, map[string]string{})
		if err != nil {
			t.Fatalf("Status after modification failed: %v", err)
		}

		// Should show 1 modified file
		if len(result.Modified) != 1 {
			t.Errorf("Expected 1 modified file, got %d", len(result.Modified))
		}
		if len(result.Added) != 0 || len(result.Deleted) != 0 {
			t.Errorf("Expected only modifications, got %d added, %d deleted",
				len(result.Added), len(result.Deleted))
		}

		// Second status should still be fast (cache working for unchanged files)
		start := time.Now()
		result2, err := dc.Status(nil, map[string]string{})
		duration := time.Since(start)

		if err != nil {
			t.Fatalf("Second status after modification failed: %v", err)
		}

		// Results should be identical
		if len(result2.Modified) != 1 {
			t.Errorf("Second status results differ: expected 1 modified, got %d", len(result2.Modified))
		}

		if duration > 50*time.Millisecond {
			t.Errorf("Second status after modification too slow: %v", duration)
		}

		t.Logf("Cache working correctly for mixed changed/unchanged files in %v", duration)
	})
}

func TestCachePortability(t *testing.T) {
	// Test that struct size calculation is consistent
	var be binaryEntry
	structSize := uintptr(136) // Expected size from structlayout
	actualSize := uintptr(unsafe.Sizeof(be))

	if actualSize != structSize {
		t.Errorf("Binary entry struct size changed: expected %d, got %d", structSize, actualSize)
		t.Errorf("This may indicate a portability issue - check struct layout with 'structlayout'")
	}

	t.Logf("Binary entry struct size: %d bytes (portable path offset)", actualSize)
}
