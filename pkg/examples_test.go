package dircachefilehash

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// ExampleNewDirectoryCache demonstrates basic usage
func ExampleNewDirectoryCache() {
	// Create a temporary directory for this example
	tempDir, err := os.MkdirTemp("", "example_*")
	if err != nil {
		fmt.Printf("Error creating temp dir: %v\n", err)
		return
	}
	defer os.RemoveAll(tempDir)

	// Create some example files
	files := map[string]string{
		"readme.txt":    "This is a readme file",
		"src/main.go":   "package main\n\nfunc main() {}",
		"src/utils.go":  "package main\n\nfunc helper() {}",
		"docs/guide.md": "# User Guide\n\nWelcome to our project",
	}

	for filepath, content := range files {
		fullPath := filepath.Join(tempDir, filepath)
		os.MkdirAll(filepath.Dir(fullPath), 0755)
		os.WriteFile(fullPath, []byte(content), 0644)
	}

	// Create directory cache
	cache := NewDirectoryCache(tempDir, "")
	defer cache.Close()

	// Scan directory and create index
	err = cache.ScanDirectory()
	if err != nil {
		fmt.Printf("Scan failed: %v\n", err)
		return
	}

	// Load the index
	err = cache.LoadIndex()
	if err != nil {
		fmt.Printf("Load failed: %v\n", err)
		return
	}

	// Get statistics
	count, totalSize, err := cache.Stats()
	if err != nil {
		fmt.Printf("Stats failed: %v\n", err)
		return
	}

	fmt.Printf("Indexed %d files with total size %d bytes\n", count, totalSize)
	// Output: Indexed 4 files with total size 84 bytes
}

// ExampleDirectoryCache_Status demonstrates status checking
func ExampleDirectoryCache_Status() {
	// Setup temporary directory with files
	tempDir, err := os.MkdirTemp("", "status_example_*")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	defer os.RemoveAll(tempDir)

	// Create initial files
	initialFiles := map[string]string{
		"stable.txt": "This file won't change",
		"modify.txt": "This will be modified",
		"delete.txt": "This will be deleted",
	}

	for filepath, content := range initialFiles {
		fullPath := filepath.Join(tempDir, filepath)
		os.WriteFile(fullPath, []byte(content), 0644)
	}

	// Create and scan cache
	cache := NewDirectoryCache(tempDir, "")
	defer cache.Close()

	cache.ScanDirectory()
	cache.LoadIndex()

	// Initial status should show no changes
	status, _ := cache.Status()
	fmt.Printf("Initial changes: %t\n", status.HasChanges())

	// Make some changes
	os.WriteFile(filepath.Join(tempDir, "modify.txt"), []byte("Modified content"), 0644)
	os.WriteFile(filepath.Join(tempDir, "new.txt"), []byte("New file"), 0644)
	os.Remove(filepath.Join(tempDir, "delete.txt"))

	// Check status after changes
	status, _ = cache.Status()
	fmt.Printf("After changes: %d modified, %d added, %d deleted\n",
		len(status.Modified), len(status.Added), len(status.Deleted))

	// Output:
	// Initial changes: false
	// After changes: 1 modified, 1 added, 1 deleted
}

// ExampleDirectoryCache_FindDuplicates demonstrates duplicate detection
func ExampleDirectoryCache_FindDuplicates() {
	// Setup temporary directory
	tempDir, err := os.MkdirTemp("", "duplicates_example_*")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	defer os.RemoveAll(tempDir)

	// Create files with some duplicates
	files := map[string]string{
		"original.txt":       "This content is duplicated",
		"copy1.txt":          "This content is duplicated",
		"copy2.txt":          "This content is duplicated",
		"unique.txt":         "This is unique content",
		"subdir/another.txt": "This content is duplicated",
	}

	for filepath, content := range files {
		fullPath := filepath.Join(tempDir, filepath)
		os.MkdirAll(filepath.Dir(fullPath), 0755)
		os.WriteFile(fullPath, []byte(content), 0644)
	}

	// Create and scan cache
	cache := NewDirectoryCache(tempDir, "")
	defer cache.Close()

	cache.ScanDirectory()
	cache.LoadIndex()

	// Find duplicates
	duplicates := cache.FindDuplicates()

	for hash, group := range duplicates {
		fmt.Printf("Hash %s has %d duplicates:\n", hash[:8], len(group))

		var paths []string
		for _, entry := range group {
			paths = append(paths, entry.RelativePath())
		}
		sort.Strings(paths) // Sort for consistent output

		for _, path := range paths {
			fmt.Printf("  %s\n", path)
		}
	}

	// Output:
	// Hash 4b05b1d5 has 4 duplicates:
	//   copy1.txt
	//   copy2.txt
	//   original.txt
	//   subdir/another.txt
}

// ExampleDirectoryCache_UpdatePaths demonstrates incremental updates
func ExampleDirectoryCache_UpdatePaths() {
	// Setup temporary directory
	tempDir, err := os.MkdirTemp("", "update_example_*")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	defer os.RemoveAll(tempDir)

	// Create initial files
	for i := 0; i < 5; i++ {
		filename := fmt.Sprintf("file%d.txt", i)
		content := fmt.Sprintf("Content for file %d", i)
		os.WriteFile(filepath.Join(tempDir, filename), []byte(content), 0644)
	}

	// Create cache and initial scan
	cache := NewDirectoryCache(tempDir, "")
	defer cache.Close()

	cache.ScanDirectory()
	cache.LoadIndex()

	fmt.Printf("Initial scan complete\n")

	// Modify specific files
	os.WriteFile(filepath.Join(tempDir, "file1.txt"), []byte("Modified content"), 0644)
	os.WriteFile(filepath.Join(tempDir, "file3.txt"), []byte("Another modification"), 0644)

	// Update only the modified files
	err = cache.UpdatePaths([]string{"file1.txt", "file3.txt"})
	if err != nil {
		fmt.Printf("Update failed: %v\n", err)
		return
	}

	// Check status - should show no changes after targeted update
	status, _ := cache.Status()
	fmt.Printf("Changes after targeted update: %t\n", status.HasChanges())

	// Output:
	// Initial scan complete
	// Changes after targeted update: false
}

// TestExampleUsagePatterns tests common usage patterns
func TestExampleUsagePatterns(t *testing.T) {
	t.Run("BasicWorkflow", func(t *testing.T) {
		// This test demonstrates the most common usage pattern
		tempDir, err := os.MkdirTemp("", "basic_workflow_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create a project structure
		projectStructure := map[string]string{
			"README.md":                 "# My Project",
			"main.go":                   "package main\n\nfunc main() {}",
			"go.mod":                    "module myproject\n\ngo 1.19",
			"internal/config.go":        "package internal\n\ntype Config struct{}",
			"internal/handler.go":       "package internal\n\nfunc Handle() {}",
			"cmd/server/main.go":        "package main\n\nfunc main() {}",
			"pkg/utils/strings.go":      "package utils\n\nfunc Reverse(s string) string { return s }",
			"docs/api.md":               "# API Documentation",
			"tests/integration_test.go": "package tests\n\nimport \"testing\"",
		}

		// Create all files
		for filePath, content := range projectStructure {
			fullPath := filepath.Join(tempDir, filePath)
			if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
				t.Fatalf("Failed to create directory: %v", err)
			}
			if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
				t.Fatalf("Failed to create file %s: %v", filePath, err)
			}
		}

		// Step 1: Initialize cache
		cache := NewDirectoryCache(tempDir, "")
		defer cache.Close()

		// Step 2: Initial scan
		if err := cache.ScanDirectory(); err != nil {
			t.Fatalf("Initial scan failed: %v", err)
		}

		// Step 3: Load index
		if err := cache.LoadIndex(); err != nil {
			t.Fatalf("Load index failed: %v", err)
		}

		// Step 4: Verify initial state
		entries := cache.GetEntries()
		if len(entries) != len(projectStructure) {
			t.Errorf("Expected %d entries, got %d", len(projectStructure), len(entries))
		}

		// Step 5: Check that all expected files are present
		entryPaths := make(map[string]bool)
		for _, entry := range entries {
			entryPaths[entry.RelativePath()] = true
		}

		for expectedPath := range projectStructure {
			if !entryPaths[expectedPath] {
				t.Errorf("Expected file not found: %s", expectedPath)
			}
		}

		// Step 6: Verify no initial changes
		status, err := cache.Status()
		if err != nil {
			t.Fatalf("Status check failed: %v", err)
		}
		if status.HasChanges() {
			t.Error("Should have no initial changes")
		}

		// Step 7: Simulate development workflow
		// Modify existing file
		modifiedContent := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"Hello, World!\")\n}"
		if err := os.WriteFile(filepath.Join(tempDir, "main.go"), []byte(modifiedContent), 0644); err != nil {
			t.Fatalf("Failed to modify main.go: %v", err)
		}

		// Add new file
		newFileContent := "package utils\n\nfunc Capitalize(s string) string { return s }"
		newFilePath := filepath.Join(tempDir, "pkg/utils/format.go")
		if err := os.WriteFile(newFilePath, []byte(newFileContent), 0644); err != nil {
			t.Fatalf("Failed to create new file: %v", err)
		}

		// Delete a file
		if err := os.Remove(filepath.Join(tempDir, "docs/api.md")); err != nil {
			t.Fatalf("Failed to delete file: %v", err)
		}

		// Step 8: Check status after changes
		status, err = cache.Status()
		if err != nil {
			t.Fatalf("Status after changes failed: %v", err)
		}

		if len(status.Modified) != 1 || status.Modified[0] != "main.go" {
			t.Errorf("Expected 1 modified file (main.go), got %v", status.Modified)
		}

		if len(status.Added) != 1 || status.Added[0] != "pkg/utils/format.go" {
			t.Errorf("Expected 1 added file (pkg/utils/format.go), got %v", status.Added)
		}

		if len(status.Deleted) != 1 || status.Deleted[0] != "docs/api.md" {
			t.Errorf("Expected 1 deleted file (docs/api.md), got %v", status.Deleted)
		}

		// Step 9: Update the index
		if err := cache.Update(); err != nil {
			t.Fatalf("Update failed: %v", err)
		}

		// Step 10: Verify clean state after update
		status, err = cache.Status()
		if err != nil {
			t.Fatalf("Status after update failed: %v", err)
		}
		if status.HasChanges() {
			t.Error("Should have no changes after update")
		}
	})

	t.Run("MonitoringWorkflow", func(t *testing.T) {
		// This test demonstrates using the cache for monitoring file changes
		tempDir, err := os.MkdirTemp("", "monitoring_workflow_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create watched files
		watchedFiles := []string{"config.json", "data.csv", "log.txt"}
		for _, filename := range watchedFiles {
			content := fmt.Sprintf("Initial content for %s", filename)
			if err := os.WriteFile(filepath.Join(tempDir, filename), []byte(content), 0644); err != nil {
				t.Fatalf("Failed to create watched file: %v", err)
			}
		}

		cache := NewDirectoryCache(tempDir, "")
		defer cache.Close()

		// Establish baseline
		cache.ScanDirectory()
		cache.LoadIndex()

		// Simulate monitoring loop
		for iteration := 0; iteration < 3; iteration++ {
			// Modify a file each iteration
			filename := watchedFiles[iteration%len(watchedFiles)]
			newContent := fmt.Sprintf("Updated content for %s at iteration %d", filename, iteration)
			filePath := filepath.Join(tempDir, filename)

			if err := os.WriteFile(filePath, []byte(newContent), 0644); err != nil {
				t.Fatalf("Failed to modify watched file: %v", err)
			}

			// Quick check for changes
			hasChanges, err := cache.HasChangesQuick()
			if err != nil {
				t.Fatalf("HasChangesQuick failed: %v", err)
			}
			if !hasChanges {
				t.Errorf("Iteration %d: Should detect changes", iteration)
			}

			// Get detailed status
			status, err := cache.Status()
			if err != nil {
				t.Fatalf("Status failed: %v", err)
			}

			if len(status.Modified) != 1 {
				t.Errorf("Iteration %d: Expected 1 modified file, got %d", iteration, len(status.Modified))
			}

			if status.Modified[0] != filename {
				t.Errorf("Iteration %d: Expected modified file %s, got %s", iteration, filename, status.Modified[0])
			}

			// Update tracking
			if err := cache.UpdatePaths([]string{filename}); err != nil {
				t.Fatalf("UpdatePaths failed: %v", err)
			}

			// Verify clean state
			hasChanges, err = cache.HasChangesQuick()
			if err != nil {
				t.Fatalf("HasChangesQuick after update failed: %v", err)
			}
			if hasChanges {
				t.Errorf("Iteration %d: Should have no changes after update", iteration)
			}
		}
	})

	t.Run("DuplicateCleanupWorkflow", func(t *testing.T) {
		// This test demonstrates using the cache for duplicate file detection and cleanup
		tempDir, err := os.MkdirTemp("", "duplicate_cleanup_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create files with intentional duplicates
		uniqueContents := map[string][]string{
			"Content A": {"file1.txt", "backup/file1_backup.txt", "archive/old_file1.txt"},
			"Content B": {"file2.txt", "backup/file2_backup.txt"},
			"Content C": {"unique_file.txt"}, // No duplicates
			"Content D": {"data.txt", "data_copy.txt", "redundant/data_old.txt", "temp/data_temp.txt"},
		}

		// Create all files
		for content, paths := range uniqueContents {
			for _, relPath := range paths {
				fullPath := filepath.Join(tempDir, relPath)
				if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
					t.Fatalf("Failed to create directory: %v", err)
				}
				if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
					t.Fatalf("Failed to create file %s: %v", relPath, err)
				}
			}
		}

		cache := NewDirectoryCache(tempDir, "")
		defer cache.Close()

		// Scan for duplicates
		cache.ScanDirectory()
		cache.LoadIndex()

		// Find duplicate groups
		duplicates := cache.FindDuplicates()

		// Verify we found the expected number of duplicate groups
		expectedGroups := 0
		for _, paths := range uniqueContents {
			if len(paths) > 1 {
				expectedGroups++
			}
		}

		if len(duplicates) != expectedGroups {
			t.Errorf("Expected %d duplicate groups, found %d", expectedGroups, len(duplicates))
		}

		// Analyze each duplicate group
		for hash, group := range duplicates {
			if len(group) < 2 {
				t.Errorf("Duplicate group %s has only %d files", hash, len(group))
				continue
			}

			// Get the content of the first file to verify they're actually duplicates
			firstEntry := group[0]
			firstFilePath := filepath.Join(tempDir, firstEntry.RelativePath())
			expectedContent, err := os.ReadFile(firstFilePath)
			if err != nil {
				t.Fatalf("Failed to read first file in group: %v", err)
			}

			// Verify all files in the group have the same content
			for _, entry := range group {
				filePath := filepath.Join(tempDir, entry.RelativePath())
				content, err := os.ReadFile(filePath)
				if err != nil {
					t.Fatalf("Failed to read file %s: %v", entry.RelativePath(), err)
				}
				if string(content) != string(expectedContent) {
					t.Errorf("File %s content doesn't match group", entry.RelativePath())
				}

				// Verify hash matches
				expectedHash := calculateExpectedHash(string(content))
				if entry.HashString() != expectedHash {
					t.Errorf("File %s hash mismatch: expected %s, got %s",
						entry.RelativePath(), expectedHash, entry.HashString())
				}
			}
		}

		// Test finding by specific hash
		if len(duplicates) > 0 {
			// Pick the first duplicate group
			for hash, group := range duplicates {
				matches := cache.FindByHash(hash)
				if len(matches) != len(group) {
					t.Errorf("FindByHash returned %d matches, expected %d", len(matches), len(group))
				}

				// Verify all matches are in the original group
				groupPaths := make(map[string]bool)
				for _, entry := range group {
					groupPaths[entry.RelativePath()] = true
				}

				for _, match := range matches {
					if !groupPaths[match.RelativePath()] {
						t.Errorf("FindByHash returned unexpected file: %s", match.RelativePath())
					}
				}
				break // Only test the first group
			}
		}
	})

	t.Run("IncrementalBackupWorkflow", func(t *testing.T) {
		// This test demonstrates using the cache for incremental backup scenarios
		tempDir, err := os.MkdirTemp("", "backup_workflow_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create initial file set (simulate source directory)
		initialFiles := map[string]string{
			"documents/report.docx": "Important report content",
			"documents/notes.txt":   "Meeting notes",
			"projects/code.go":      "package main\n\nfunc main() {}",
			"projects/readme.md":    "# Project README",
			"media/photo.jpg":       "JPEG image data",
			"config/settings.json":  `{"theme": "dark", "language": "en"}`,
		}

		for filePath, content := range initialFiles {
			fullPath := filepath.Join(tempDir, filePath)
			if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
				t.Fatalf("Failed to create directory: %v", err)
			}
			if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
				t.Fatalf("Failed to create file %s: %v", filePath, err)
			}
		}

		cache := NewDirectoryCache(tempDir, "")
		defer cache.Close()

		// Initial backup (full scan)
		cache.ScanDirectory()
		cache.LoadIndex()

		initialCount, initialSize, err := cache.Stats()
		if err != nil {
			t.Fatalf("Initial stats failed: %v", err)
		}

		t.Logf("Initial backup: %d files, %d bytes", initialCount, initialSize)

		// Simulate passage of time and file modifications
		// Day 1: Modify some files
		modifications := map[string]string{
			"documents/report.docx": "Updated report content with new data",
			"config/settings.json":  `{"theme": "light", "language": "en", "version": "1.1"}`,
		}

		for filePath, newContent := range modifications {
			fullPath := filepath.Join(tempDir, filePath)
			if err := os.WriteFile(fullPath, []byte(newContent), 0644); err != nil {
				t.Fatalf("Failed to modify file %s: %v", filePath, err)
			}
		}

		// Add new files
		newFiles := map[string]string{
			"documents/presentation.pptx": "New presentation content",
			"projects/utils.go":           "package main\n\nfunc utility() {}",
		}

		for filePath, content := range newFiles {
			fullPath := filepath.Join(tempDir, filePath)
			if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
				t.Fatalf("Failed to create new file %s: %v", filePath, err)
			}
		}

		// Delete a file
		deletedFile := "media/photo.jpg"
		if err := os.Remove(filepath.Join(tempDir, deletedFile)); err != nil {
			t.Fatalf("Failed to delete file: %v", err)
		}

		// Incremental backup check
		status, err := cache.Status()
		if err != nil {
			t.Fatalf("Incremental status failed: %v", err)
		}

		// Verify detected changes
		if len(status.Modified) != len(modifications) {
			t.Errorf("Expected %d modified files, got %d", len(modifications), len(status.Modified))
		}

		if len(status.Added) != len(newFiles) {
			t.Errorf("Expected %d added files, got %d", len(newFiles), len(status.Added))
		}

		if len(status.Deleted) != 1 {
			t.Errorf("Expected 1 deleted file, got %d", len(status.Deleted))
		}

		// Verify specific files are correctly identified
		for filePath := range modifications {
			found := false
			for _, modifiedPath := range status.Modified {
				if modifiedPath == filePath {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Modified file %s not detected", filePath)
			}
		}

		// Update backup index
		if err := cache.Update(); err != nil {
			t.Fatalf("Backup update failed: %v", err)
		}

		// Verify backup is up to date
		status, err = cache.Status()
		if err != nil {
			t.Fatalf("Post-update status failed: %v", err)
		}

		if status.HasChanges() {
			t.Error("Backup should be up to date after update")
		}

		// Final statistics
		finalCount, finalSize, err := cache.Stats()
		if err != nil {
			t.Fatalf("Final stats failed: %v", err)
		}

		t.Logf("Final backup: %d files, %d bytes", finalCount, finalSize)

		expectedFinalCount := initialCount + len(newFiles) - 1 // +new files, -deleted file
		if finalCount != expectedFinalCount {
			t.Errorf("Expected final count %d, got %d", expectedFinalCount, finalCount)
		}
	})
}

// Demonstrates error handling patterns
func TestExampleErrorHandling(t *testing.T) {
	t.Run("GracefulErrorHandling", func(t *testing.T) {
		// Test handling of various error conditions

		// Non-existent directory
		cache := NewDirectoryCache("/nonexistent/directory", "")
		defer cache.Close()

		err := cache.ScanDirectory()
		if err == nil {
			t.Error("Expected error for non-existent directory")
		}

		err = cache.LoadIndex()
		if err == nil {
			t.Error("Expected error for non-existent index")
		}

		// Create a valid directory for further testing
		tempDir, err := os.MkdirTemp("", "error_handling_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create a file
		testFile := filepath.Join(tempDir, "test.txt")
		if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}

		validCache := NewDirectoryCache(tempDir, "")
		defer validCache.Close()

		// Create valid index
		if err := validCache.ScanDirectory(); err != nil {
			t.Fatalf("Scan should succeed: %v", err)
		}

		// Corrupt the index file
		corruptData := []byte("invalid index data")
		if err := os.WriteFile(validCache.IndexFile, corruptData, 0644); err != nil {
			t.Fatalf("Failed to corrupt index: %v", err)
		}

		// Loading corrupted index should fail gracefully
		err = validCache.LoadIndex()
		if err == nil {
			t.Error("Expected error for corrupted index")
		}

		// Recovery: re-scan should work
		if err := validCache.ScanDirectory(); err != nil {
			t.Fatalf("Recovery scan should succeed: %v", err)
		}

		if err := validCache.LoadIndex(); err != nil {
			t.Fatalf("Load after recovery should succeed: %v", err)
		}
	})
}
