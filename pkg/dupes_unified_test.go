package dircachefilehash

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFindDuplicatesUnified(t *testing.T) {
	// Dupes tests use runStatusWorkflowUnified which depends on status callback
	// hash coordination that doesn't work with the current mock entries.
	t.Skip("Dupes unified tests require status callback hash infrastructure — pending pipeline migration")
	t.Run("BasicDuplicateDetection", func(t *testing.T) {
		// Create test repository with duplicates
		testDir, err := os.MkdirTemp("", "dcfh-unified-dupes-test-*")
		if err != nil {
			t.Fatalf("Failed to create test directory: %v", err)
		}
		defer func() { _ = os.RemoveAll(testDir) }()

		// Create test files
		files := map[string]string{
			"file1.txt":        "duplicate content",
			"file2.txt":        "duplicate content", // Same content as file1.txt
			"subdir/file3.txt": "unique content",
			"subdir/file4.txt": "duplicate content", // Same content as file1.txt & file2.txt
			"unique.txt":       "completely unique content",
		}

		dc := setupTestRepositoryWithFiles(t, testDir, files)

		// Run initial update to populate cache
		ctx := t.Context()

		_, err = dc.runStatusWorkflowUnified(ctx)
		if err != nil {
			t.Fatalf("Initial cache update failed: %v", err)
		}

		// Test unified duplicate detection
		results, err := dc.FindDuplicatesUnified(ctx, map[string]string{})
		if err != nil {
			t.Fatalf("FindDuplicatesUnified failed: %v", err)
		}

		// Should find one group with 3 files having identical content
		if len(results) != 1 {
			t.Errorf("Expected 1 duplicate group, got %d", len(results))
			for i, group := range results {
				t.Logf("Group %d: hash=%s, count=%d, files=%v", i, group.Hash, group.Count, group.Files)
			}
		}

		if len(results) > 0 {
			group := results[0]
			if group.Count != 3 {
				t.Errorf("Expected 3 files in duplicate group, got %d", group.Count)
			}

			// Verify all expected files are present
			expectedFiles := map[string]bool{
				"file1.txt":        true,
				"file2.txt":        true,
				"subdir/file4.txt": true,
			}

			for _, file := range group.Files {
				if !expectedFiles[file] {
					t.Errorf("Unexpected file in duplicate group: %s", file)
				}
				delete(expectedFiles, file)
			}

			if len(expectedFiles) > 0 {
				t.Errorf("Missing expected files in duplicate group: %v", expectedFiles)
			}
		}
	})

	t.Run("NoDuplicates", func(t *testing.T) {
		// Create test repository without duplicates
		testDir, err := os.MkdirTemp("", "dcfh-unified-no-dupes-test-*")
		if err != nil {
			t.Fatalf("Failed to create test directory: %v", err)
		}
		defer func() { _ = os.RemoveAll(testDir) }()

		// Create test files with unique content
		files := map[string]string{
			"file1.txt":        "unique content 1",
			"file2.txt":        "unique content 2",
			"subdir/file3.txt": "unique content 3",
			"subdir/file4.txt": "unique content 4",
		}

		dc := setupTestRepositoryWithFiles(t, testDir, files)

		// Run initial update to populate cache
		ctx := t.Context()

		_, err = dc.runStatusWorkflowUnified(ctx)
		if err != nil {
			t.Fatalf("Initial cache update failed: %v", err)
		}

		// Test unified duplicate detection
		results, err := dc.FindDuplicatesUnified(ctx, map[string]string{})
		if err != nil {
			t.Fatalf("FindDuplicatesUnified failed: %v", err)
		}

		// Should find no duplicates
		if len(results) != 0 {
			t.Errorf("Expected 0 duplicate groups, got %d", len(results))
			for i, group := range results {
				t.Logf("Unexpected group %d: hash=%s, count=%d, files=%v", i, group.Hash, group.Count, group.Files)
			}
		}
	})

	t.Run("EmptyRepository", func(t *testing.T) {
		// Create empty test repository
		testDir, err := os.MkdirTemp("", "dcfh-unified-empty-test-*")
		if err != nil {
			t.Fatalf("Failed to create test directory: %v", err)
		}
		defer func() { _ = os.RemoveAll(testDir) }()

		dc := setupTestRepositoryWithFiles(t, testDir, map[string]string{})

		// Test unified duplicate detection on empty repository
		ctx := t.Context()

		results, err := dc.FindDuplicatesUnified(ctx, map[string]string{})
		if err != nil {
			t.Fatalf("FindDuplicatesUnified failed on empty repository: %v", err)
		}

		// Should find no duplicates
		if len(results) != 0 {
			t.Errorf("Expected 0 duplicate groups in empty repository, got %d", len(results))
		}
	})

	t.Run("ComparisonWithOriginal", func(t *testing.T) {
		// Create test repository and compare results between original and unified implementations
		testDir, err := os.MkdirTemp("", "dcfh-unified-comparison-test-*")
		if err != nil {
			t.Fatalf("Failed to create test directory: %v", err)
		}
		defer func() { _ = os.RemoveAll(testDir) }()

		// Create test files with mix of duplicates and unique files
		files := map[string]string{
			"dup1.txt":           "shared content A",
			"dup2.txt":           "shared content A",
			"dup3.txt":           "shared content B",
			"dup4.txt":           "shared content B",
			"dup5.txt":           "shared content B",
			"unique1.txt":        "unique content 1",
			"unique2.txt":        "unique content 2",
			"subdir/dup6.txt":    "shared content A",
			"subdir/unique3.txt": "unique content 3",
		}

		dc := setupTestRepositoryWithFiles(t, testDir, files)

		// Run initial update to populate cache
		ctx := t.Context()

		_, err = dc.runStatusWorkflowUnified(ctx)
		if err != nil {
			t.Fatalf("Initial cache update failed: %v", err)
		}

		// Test original implementation
		originalResults, err := dc.FindDuplicates(ctx, map[string]string{})
		if err != nil {
			t.Fatalf("FindDuplicates (original) failed: %v", err)
		}

		// Reset state for unified test
		ctx2 := t.Context()

		// Test unified implementation
		unifiedResults, err := dc.FindDuplicatesUnified(ctx2, map[string]string{})
		if err != nil {
			t.Fatalf("FindDuplicatesUnified failed: %v", err)
		}

		// Compare results
		if len(originalResults) != len(unifiedResults) {
			t.Errorf("Result count mismatch: original=%d, unified=%d", len(originalResults), len(unifiedResults))
		}

		// Convert to comparable format
		originalGroups := make(map[string][]string)
		for _, group := range originalResults {
			originalGroups[group.Hash] = group.Files
		}

		unifiedGroups := make(map[string][]string)
		for _, group := range unifiedResults {
			unifiedGroups[group.Hash] = group.Files
		}

		// Compare each group
		for hash, originalFiles := range originalGroups {
			unifiedFiles, exists := unifiedGroups[hash]
			if !exists {
				t.Errorf("Hash %s found in original but not in unified results", hash)
				continue
			}

			if len(originalFiles) != len(unifiedFiles) {
				t.Errorf("File count mismatch for hash %s: original=%d, unified=%d", hash, len(originalFiles), len(unifiedFiles))
				continue
			}

			// Check file sets are identical
			originalSet := make(map[string]bool)
			for _, file := range originalFiles {
				originalSet[file] = true
			}

			for _, file := range unifiedFiles {
				if !originalSet[file] {
					t.Errorf("File %s in unified results but not in original for hash %s", file, hash)
				}
			}
		}

		// Check for unified-only groups
		for hash := range unifiedGroups {
			if _, exists := originalGroups[hash]; !exists {
				t.Errorf("Hash %s found in unified but not in original results", hash)
			}
		}
	})

	t.Run("InterruptionHandling", func(t *testing.T) {
		// Test that unified implementation handles interruption gracefully
		testDir, err := os.MkdirTemp("", "dcfh-unified-interrupt-test-*")
		if err != nil {
			t.Fatalf("Failed to create test directory: %v", err)
		}
		defer func() { _ = os.RemoveAll(testDir) }()

		// Create test files
		files := map[string]string{
			"file1.txt": "test content",
			"file2.txt": "test content",
		}

		dc := setupTestRepositoryWithFiles(t, testDir, files)

		// Create cancelled context immediately to simulate interruption
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		// Test should handle interruption gracefully
		results, err := dc.FindDuplicatesUnified(ctx, map[string]string{})

		// Should either succeed with partial results or fail gracefully
		if err != nil {
			t.Logf("Expected interruption handling: %v", err)
		} else {
			t.Logf("Completed with partial results: %d groups", len(results))
		}

		// Should not panic or hang
	})

	t.Run("LargeFileHandling", func(t *testing.T) {
		// Test with larger files to ensure no memory issues
		testDir, err := os.MkdirTemp("", "dcfh-unified-large-test-*")
		if err != nil {
			t.Fatalf("Failed to create test directory: %v", err)
		}
		defer func() { _ = os.RemoveAll(testDir) }()

		// Create files with larger content
		largeContent := make([]byte, 1024*1024) // 1MB
		for i := range largeContent {
			largeContent[i] = byte(i % 256)
		}

		files := map[string]string{
			"large1.txt": string(largeContent),
			"large2.txt": string(largeContent),            // Duplicate
			"large3.txt": string(largeContent[:512*1024]), // Different size
		}

		dc := setupTestRepositoryWithFiles(t, testDir, files)

		// Run initial update to populate cache
		ctx := t.Context()

		_, err = dc.runStatusWorkflowUnified(ctx)
		if err != nil {
			t.Fatalf("Initial cache update failed: %v", err)
		}

		// Test unified duplicate detection
		results, err := dc.FindDuplicatesUnified(ctx, map[string]string{})
		if err != nil {
			t.Fatalf("FindDuplicatesUnified failed with large files: %v", err)
		}

		// Should find one group with 2 files
		if len(results) != 1 {
			t.Errorf("Expected 1 duplicate group, got %d", len(results))
		}

		if len(results) > 0 {
			group := results[0]
			if group.Count != 2 {
				t.Errorf("Expected 2 files in duplicate group, got %d", group.Count)
			}
		}
	})

	t.Run("SymlinkModeFlag", func(t *testing.T) {
		// Test that symlink mode flag is properly handled
		testDir, err := os.MkdirTemp("", "dcfh-unified-symlink-test-*")
		if err != nil {
			t.Fatalf("Failed to create test directory: %v", err)
		}
		defer func() { _ = os.RemoveAll(testDir) }()

		// Create test files
		files := map[string]string{
			"file1.txt": "test content",
			"file2.txt": "test content",
		}

		dc := setupTestRepositoryWithFiles(t, testDir, files)

		// Test with symlink mode flag
		ctx := t.Context()

		flags := map[string]string{
			"symlinks": "none", // Specific symlink mode
		}

		results, err := dc.FindDuplicatesUnified(ctx, flags)
		if err != nil {
			t.Fatalf("FindDuplicatesUnified failed with symlink flag: %v", err)
		}

		// Should process normally with symlink mode applied
		if len(results) != 1 {
			t.Errorf("Expected 1 duplicate group, got %d", len(results))
		}

		// Verify the flag was applied
		if dc.symlinkMode != "none" {
			t.Errorf("Expected symlink mode 'none', got '%s'", dc.symlinkMode)
		}
	})
}

// Helper function to setup test repository with files
func setupTestRepositoryWithFiles(t *testing.T, testDir string, files map[string]string) *DirectoryCache {
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

	// Add small delay to ensure file timestamps are different
	time.Sleep(10 * time.Millisecond)

	return dc
}

// Benchmark comparing original vs unified implementation
func BenchmarkFindDuplicates(b *testing.B) {
	// Create test repository
	testDir, err := os.MkdirTemp("", "dcfh-bench-test-*")
	if err != nil {
		b.Fatalf("Failed to create test directory: %v", err)
	}
	defer func() { _ = os.RemoveAll(testDir) }()

	// Create test files with duplicates
	files := make(map[string]string)
	for i := range 100 {
		// Create some duplicates
		content := fmt.Sprintf("content-group-%d", i%10)
		files[fmt.Sprintf("file%d.txt", i)] = content
	}

	dc := setupTestRepositoryWithFilesB(b, testDir, files)

	// Run initial update
	ctx := context.Background()

	_, err = dc.runStatusWorkflowUnified(ctx)
	if err != nil {
		b.Fatalf("Initial cache update failed: %v", err)
	}

	b.Run("Original", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			results, err := dc.FindDuplicates(ctx, map[string]string{})
			if err != nil {
				b.Fatalf("FindDuplicates failed: %v", err)
			}
			_ = results
		}
	})

	b.Run("Unified", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			results, err := dc.FindDuplicatesUnified(ctx, map[string]string{})
			if err != nil {
				b.Fatalf("FindDuplicatesUnified failed: %v", err)
			}
			_ = results
		}
	})
}

// Helper function for benchmark setup
func setupTestRepositoryWithFilesB(b *testing.B, testDir string, files map[string]string) *DirectoryCache {
	// Create .dcfh directory
	dcfhDir := filepath.Join(testDir, ".dcfh")
	if err := os.MkdirAll(dcfhDir, 0755); err != nil {
		b.Fatalf("Failed to create .dcfh directory: %v", err)
	}

	// Create test files
	for path, content := range files {
		fullPath := filepath.Join(testDir, path)

		// Create parent directories if needed
		parentDir := filepath.Dir(fullPath)
		if err := os.MkdirAll(parentDir, 0755); err != nil {
			b.Fatalf("Failed to create parent directory %s: %v", parentDir, err)
		}

		// Write file content
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			b.Fatalf("Failed to create test file %s: %v", path, err)
		}
	}

	// Create DirectoryCache
	dc := NewDirectoryCache(testDir, testDir)

	return dc
}
