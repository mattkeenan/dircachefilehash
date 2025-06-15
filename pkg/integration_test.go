package dircachefilehash

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// IntegrationTestSuite tests full end-to-end workflows
func TestIntegrationWorkflows(t *testing.T) {
	t.Run("FullWorkflowNewProject", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "integration_new_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Step 1: Create a new project structure
		projectFiles := map[string]string{
			"README.md":            "# My Project\nThis is a test project",
			"main.go":              "package main\n\nfunc main() {\n\tprintln(\"Hello, World!\")\n}",
			"go.mod":               "module myproject\n\ngo 1.19",
			"src/utils.go":         "package src\n\nfunc Helper() string {\n\treturn \"helper\"\n}",
			"src/types.go":         "package src\n\ntype Config struct {\n\tName string\n}",
			"tests/main_test.go":   "package tests\n\nimport \"testing\"\n\nfunc TestMain(t *testing.T) {\n\t// test\n}",
			"docs/api.md":          "# API Documentation\n\n## Overview\n\nThis is the API docs",
			"config/settings.json": `{"name": "myproject", "version": "1.0.0"}`,
		}

		// Create project files
		for filepath, content := range projectFiles {
			fullPath := filepath.Join(tempDir, filepath)
			if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
				t.Fatalf("Failed to create directory for %s: %v", filepath, err)
			}
			if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
				t.Fatalf("Failed to create file %s: %v", filepath, err)
			}
		}

		// Step 2: Initialize cache
		cache := NewDirectoryCache(tempDir, "")
		defer cache.Close()

		// Step 3: Initial scan
		err = cache.ScanDirectory()
		if err != nil {
			t.Fatalf("Initial scan failed: %v", err)
		}

		err = cache.LoadIndex()
		if err != nil {
			t.Fatalf("LoadIndex failed: %v", err)
		}

		initialEntries := cache.GetEntries()
		if len(initialEntries) != len(projectFiles) {
			t.Errorf("Expected %d initial entries, got %d", len(projectFiles), len(initialEntries))
		}

		// Step 4: Verify initial status (no changes)
		status, err := cache.Status()
		if err != nil {
			t.Fatalf("Status check failed: %v", err)
		}

		if status.HasChanges() {
			t.Error("Initial status should show no changes")
		}

		// Step 5: Simulate development workflow
		// - Modify existing file
		mainGoPath := filepath.Join(tempDir, "main.go")
		newMainContent := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"Hello, Updated World!\")\n}"
		time.Sleep(time.Millisecond * 10) // Ensure different timestamp
		err = os.WriteFile(mainGoPath, []byte(newMainContent), 0644)
		if err != nil {
			t.Fatalf("Failed to modify main.go: %v", err)
		}

		// - Add new file
		newFilePath := filepath.Join(tempDir, "src/new_feature.go")
		newFileContent := "package src\n\nfunc NewFeature() {\n\t// new feature implementation\n}"
		err = os.WriteFile(newFilePath, []byte(newFileContent), 0644)
		if err != nil {
			t.Fatalf("Failed to create new file: %v", err)
		}

		// - Delete a file
		deletedFilePath := filepath.Join(tempDir, "docs/api.md")
		err = os.Remove(deletedFilePath)
		if err != nil {
			t.Fatalf("Failed to delete file: %v", err)
		}

		// Step 6: Check status after changes
		status, err = cache.Status()
		if err != nil {
			t.Fatalf("Status check after changes failed: %v", err)
		}

		if !status.HasChanges() {
			t.Error("Status should detect changes")
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

		// Step 7: Update index with changes
		err = cache.Update()
		if err != nil {
			t.Fatalf("Update failed: %v", err)
		}

		// Step 8: Verify status is clean after update
		status, err = cache.Status()
		if err != nil {
			t.Fatalf("Status check after update failed: %v", err)
		}

		if status.HasChanges() {
			t.Error("Status should show no changes after update")
		}

		// Step 9: Verify final state
		err = cache.LoadIndex()
		if err != nil {
			t.Fatalf("LoadIndex after update failed: %v", err)
		}

		finalEntries := cache.GetEntries()
		expectedFinalCount := len(projectFiles) // 1 deleted, 1 added = same count
		if len(finalEntries) != expectedFinalCount {
			t.Errorf("Expected %d final entries, got %d", expectedFinalCount, len(finalEntries))
		}
	})

	t.Run("FullWorkflowLargeProject", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "integration_large_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create a larger project structure
		numFiles := 500
		numDirs := 20

		// Create directory structure
		for i := 0; i < numDirs; i++ {
			dirPath := filepath.Join(tempDir, fmt.Sprintf("module_%03d", i))
			if err := os.MkdirAll(dirPath, 0755); err != nil {
				t.Fatalf("Failed to create directory: %v", err)
			}
		}

		// Create files across directories
		for i := 0; i < numFiles; i++ {
			dirIndex := i % numDirs
			dirPath := filepath.Join(tempDir, fmt.Sprintf("module_%03d", dirIndex))
			fileName := fmt.Sprintf("file_%05d.go", i)
			filePath := filepath.Join(dirPath, fileName)

			content := fmt.Sprintf("package module_%03d\n\n// File %d\nfunc Function_%05d() {\n\t// implementation\n}", dirIndex, i, i)

			if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
				t.Fatalf("Failed to create file %s: %v", filePath, err)
			}
		}

		// Initialize and scan
		cache := NewDirectoryCache(tempDir, "")
		defer cache.Close()

		startTime := time.Now()
		err = cache.ScanDirectory()
		scanDuration := time.Since(startTime)

		if err != nil {
			t.Fatalf("Large project scan failed: %v", err)
		}

		t.Logf("Scanned %d files in %v", numFiles, scanDuration)

		// Load and verify
		startTime = time.Now()
		err = cache.LoadIndex()
		loadDuration := time.Since(startTime)

		if err != nil {
			t.Fatalf("Large project load failed: %v", err)
		}

		t.Logf("Loaded index with %d files in %v", numFiles, loadDuration)

		entries := cache.GetEntries()
		if len(entries) != numFiles {
			t.Errorf("Expected %d entries, got %d", numFiles, len(entries))
		}

		// Test status performance
		startTime = time.Now()
		status, err := cache.Status()
		statusDuration := time.Since(startTime)

		if err != nil {
			t.Fatalf("Large project status failed: %v", err)
		}

		t.Logf("Status check for %d files completed in %v", numFiles, statusDuration)

		if status.HasChanges() {
			t.Error("Large project should have no initial changes")
		}

		// Test bulk modifications
		modifyCount := 50
		for i := 0; i < modifyCount; i++ {
			dirIndex := i % numDirs
			dirPath := filepath.Join(tempDir, fmt.Sprintf("module_%03d", dirIndex))
			fileName := fmt.Sprintf("file_%05d.go", i)
			filePath := filepath.Join(dirPath, fileName)

			modifiedContent := fmt.Sprintf("package module_%03d\n\n// Modified File %d\nfunc ModifiedFunction_%05d() {\n\t// modified implementation\n}", dirIndex, i, i)

			if err := os.WriteFile(filePath, []byte(modifiedContent), 0644); err != nil {
				t.Fatalf("Failed to modify file %s: %v", filePath, err)
			}
		}

		// Check status after bulk modifications
		startTime = time.Now()
		status, err = cache.Status()
		bulkStatusDuration := time.Since(startTime)

		if err != nil {
			t.Fatalf("Status after bulk modifications failed: %v", err)
		}

		t.Logf("Status check after %d modifications completed in %v", modifyCount, bulkStatusDuration)

		if len(status.Modified) != modifyCount {
			t.Errorf("Expected %d modified files, got %d", modifyCount, len(status.Modified))
		}
	})

	t.Run("WorkflowWithDuplicateDetection", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "integration_dupes_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create files with intentional duplicates
		uniqueContents := []string{
			"This is unique content 1",
			"This is unique content 2",
			"This is duplicated content",
			"Another unique content",
		}

		files := map[string]string{
			"unique1.txt":           uniqueContents[0],
			"unique2.txt":           uniqueContents[1],
			"duplicate1.txt":        uniqueContents[2],
			"subdir/duplicate2.txt": uniqueContents[2], // Same as duplicate1.txt
			"subdir/duplicate3.txt": uniqueContents[2], // Same as duplicate1.txt
			"unique3.txt":           uniqueContents[3],
			"backup/duplicate4.txt": uniqueContents[2], // Same as duplicate1.txt
		}

		// Create all files
		for filePath, content := range files {
			fullPath := filepath.Join(tempDir, filePath)
			if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
				t.Fatalf("Failed to create directory for %s: %v", filePath, err)
			}
			if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
				t.Fatalf("Failed to create file %s: %v", filePath, err)
			}
		}

		// Initialize cache and scan
		cache := NewDirectoryCache(tempDir, "")
		defer cache.Close()

		err = cache.ScanDirectory()
		if err != nil {
			t.Fatalf("Scan failed: %v", err)
		}

		err = cache.LoadIndex()
		if err != nil {
			t.Fatalf("Load failed: %v", err)
		}

		// Find duplicates
		duplicates := cache.FindDuplicates()

		// Should find one group of 4 duplicates (duplicate1.txt through duplicate4.txt)
		if len(duplicates) != 1 {
			t.Errorf("Expected 1 duplicate group, got %d", len(duplicates))
		}

		for hash, group := range duplicates {
			if len(group) != 4 {
				t.Errorf("Expected 4 files in duplicate group %s, got %d", hash, len(group))
			}

			// Verify all files in group have the expected content
			expectedContent := uniqueContents[2] // "This is duplicated content"
			expectedHash := calculateExpectedHash(expectedContent)

			if hash != expectedHash {
				t.Errorf("Duplicate group hash mismatch: expected %s, got %s", expectedHash, hash)
			}

			// Verify all expected duplicate files are present
			expectedFiles := []string{
				"backup/duplicate4.txt",
				"duplicate1.txt",
				"subdir/duplicate2.txt",
				"subdir/duplicate3.txt",
			}

			var actualFiles []string
			for _, entry := range group {
				actualFiles = append(actualFiles, entry.RelativePath())
			}
			sort.Strings(actualFiles)
			sort.Strings(expectedFiles)

			if len(actualFiles) != len(expectedFiles) {
				t.Errorf("Duplicate file count mismatch: expected %v, got %v", expectedFiles, actualFiles)
			} else {
				for i, expected := range expectedFiles {
					if actualFiles[i] != expected {
						t.Errorf("Duplicate file mismatch at index %d: expected %s, got %s", i, expected, actualFiles[i])
					}
				}
			}
		}

		// Test FindByHash
		duplicateContent := uniqueContents[2]
		duplicateHash := calculateExpectedHash(duplicateContent)
		hashMatches := cache.FindByHash(duplicateHash)

		if len(hashMatches) != 4 {
			t.Errorf("FindByHash expected 4 matches, got %d", len(hashMatches))
		}
	})

	t.Run("WorkflowWithIncrementalUpdates", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "integration_incremental_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create initial file set
		initialFiles := map[string]string{
			"stable1.txt": "This file won't change",
			"stable2.txt": "Another stable file",
			"modify1.txt": "This will be modified later",
			"modify2.txt": "This will also be modified",
			"delete1.txt": "This will be deleted",
		}

		for filePath, content := range initialFiles {
			fullPath := filepath.Join(tempDir, filePath)
			if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
				t.Fatalf("Failed to create initial file %s: %v", filePath, err)
			}
		}

		cache := NewDirectoryCache(tempDir, "")
		defer cache.Close()

		// Initial scan
		err = cache.ScanDirectory()
		if err != nil {
			t.Fatalf("Initial scan failed: %v", err)
		}

		err = cache.LoadIndex()
		if err != nil {
			t.Fatalf("Initial load failed: %v", err)
		}

		// Phase 1: Incremental update with specific paths
		time.Sleep(time.Millisecond * 10) // Ensure different timestamp

		// Modify one file
		modify1Path := filepath.Join(tempDir, "modify1.txt")
		err = os.WriteFile(modify1Path, []byte("Modified content for file 1"), 0644)
		if err != nil {
			t.Fatalf("Failed to modify file: %v", err)
		}

		// Use UpdatePaths to update only the modified file
		err = cache.UpdatePaths([]string{"modify1.txt"})
		if err != nil {
			t.Fatalf("UpdatePaths failed: %v", err)
		}

		// Verify incremental update worked
		status, err := cache.Status()
		if err != nil {
			t.Fatalf("Status after incremental update failed: %v", err)
		}

		if status.HasChanges() {
			t.Error("Should have no changes after incremental update")
		}

		// Phase 2: Multiple file incremental update
		time.Sleep(time.Millisecond * 10)

		// Modify another file and add a new one
		modify2Path := filepath.Join(tempDir, "modify2.txt")
		err = os.WriteFile(modify2Path, []byte("Modified content for file 2"), 0644)
		if err != nil {
			t.Fatalf("Failed to modify second file: %v", err)
		}

		newFilePath := filepath.Join(tempDir, "new1.txt")
		err = os.WriteFile(newFilePath, []byte("New file content"), 0644)
		if err != nil {
			t.Fatalf("Failed to create new file: %v", err)
		}

		// Update multiple paths
		err = cache.UpdatePaths([]string{"modify2.txt", "new1.txt"})
		if err != nil {
			t.Fatalf("UpdatePaths for multiple files failed: %v", err)
		}

		status, err = cache.Status()
		if err != nil {
			t.Fatalf("Status after multi-file update failed: %v", err)
		}

		if status.HasChanges() {
			t.Error("Should have no changes after multi-file incremental update")
		}

		// Phase 3: Directory-based incremental update
		subDir := filepath.Join(tempDir, "newsubdir")
		err = os.MkdirAll(subDir, 0755)
		if err != nil {
			t.Fatalf("Failed to create subdirectory: %v", err)
		}

		// Create files in subdirectory
		for i := 0; i < 3; i++ {
			subFilePath := filepath.Join(subDir, fmt.Sprintf("subfile%d.txt", i))
			content := fmt.Sprintf("Content for subfile %d", i)
			err = os.WriteFile(subFilePath, []byte(content), 0644)
			if err != nil {
				t.Fatalf("Failed to create subfile: %v", err)
			}
		}

		// Update entire subdirectory
		err = cache.UpdatePaths([]string{"newsubdir"})
		if err != nil {
			t.Fatalf("UpdatePaths for directory failed: %v", err)
		}

		status, err = cache.Status()
		if err != nil {
			t.Fatalf("Status after directory update failed: %v", err)
		}

		if status.HasChanges() {
			t.Error("Should have no changes after directory incremental update")
		}

		// Verify final state
		entries := cache.GetEntries()
		expectedFileCount := len(initialFiles) + 1 + 3 // +1 for new1.txt, +3 for subfiles
		if len(entries) != expectedFileCount {
			t.Errorf("Expected %d final entries, got %d", expectedFileCount, len(entries))
		}

		// Verify all expected files are present
		entryPaths := make(map[string]bool)
		for _, entry := range entries {
			entryPaths[entry.RelativePath()] = true
		}

		expectedPaths := []string{
			"stable1.txt", "stable2.txt", "modify1.txt", "modify2.txt", "delete1.txt",
			"new1.txt", "newsubdir/subfile0.txt", "newsubdir/subfile1.txt", "newsubdir/subfile2.txt",
		}

		for _, expectedPath := range expectedPaths {
			if !entryPaths[expectedPath] {
				t.Errorf("Expected path not found in final entries: %s", expectedPath)
			}
		}
	})

	t.Run("WorkflowWithErrorRecovery", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "integration_recovery_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create some initial files
		for i := 0; i < 5; i++ {
			filePath := filepath.Join(tempDir, fmt.Sprintf("file%d.txt", i))
			content := fmt.Sprintf("Content for file %d", i)
			err = os.WriteFile(filePath, []byte(content), 0644)
			if err != nil {
				t.Fatalf("Failed to create initial file: %v", err)
			}
		}

		cache := NewDirectoryCache(tempDir, "")
		defer cache.Close()

		// Create valid index
		err = cache.ScanDirectory()
		if err != nil {
			t.Fatalf("Initial scan failed: %v", err)
		}

		// Simulate index corruption by writing invalid data
		err = os.WriteFile(cache.IndexFile, []byte("invalid index data"), 0644)
		if err != nil {
			t.Fatalf("Failed to corrupt index: %v", err)
		}

		// Try to load corrupted index - should fail
		err = cache.LoadIndex()
		if err == nil {
			t.Error("Loading corrupted index should fail")
		}

		// Recovery: re-scan to rebuild index
		err = cache.ScanDirectory()
		if err != nil {
			t.Fatalf("Recovery scan failed: %v", err)
		}

		// Should now be able to load successfully
		err = cache.LoadIndex()
		if err != nil {
			t.Fatalf("Load after recovery failed: %v", err)
		}

		// Verify recovered state
		entries := cache.GetEntries()
		if len(entries) != 5 {
			t.Errorf("Expected 5 entries after recovery, got %d", len(entries))
		}

		status, err := cache.Status()
		if err != nil {
			t.Fatalf("Status after recovery failed: %v", err)
		}

		if status.HasChanges() {
			t.Error("Should have no changes after successful recovery")
		}
	})
}

func TestIntegrationPerformanceValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance validation in short mode")
	}

	t.Run("PerformanceBaseline", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "integration_perf_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create a moderate-sized project for performance validation
		numFiles := 1000

		// Create files with various sizes and content
		for i := 0; i < numFiles; i++ {
			// Create subdirectories every 100 files
			subDir := ""
			if i%100 == 0 && i > 0 {
				subDir = fmt.Sprintf("subdir_%03d", i/100)
				fullSubDir := filepath.Join(tempDir, subDir)
				if err := os.MkdirAll(fullSubDir, 0755); err != nil {
					t.Fatalf("Failed to create subdirectory: %v", err)
				}
			}

			fileName := fmt.Sprintf("file_%05d.txt", i)
			var filePath string
			if subDir != "" {
				filePath = filepath.Join(tempDir, subDir, fileName)
			} else {
				filePath = filepath.Join(tempDir, fileName)
			}

			// Create content of varying sizes
			contentSize := 100 + (i % 1000) // 100 to 1099 bytes
			content := strings.Repeat(fmt.Sprintf("Line %d content ", i), contentSize/20)

			err = os.WriteFile(filePath, []byte(content), 0644)
			if err != nil {
				t.Fatalf("Failed to create performance test file: %v", err)
			}
		}

		cache := NewDirectoryCache(tempDir, "")
		defer cache.Close()

		// Measure scan performance
		startTime := time.Now()
		err = cache.ScanDirectory()
		scanDuration := time.Since(startTime)

		if err != nil {
			t.Fatalf("Performance scan failed: %v", err)
		}

		t.Logf("Scanned %d files in %v (%.2f files/sec)",
			numFiles, scanDuration, float64(numFiles)/scanDuration.Seconds())

		// Measure load performance
		startTime = time.Now()
		err = cache.LoadIndex()
		loadDuration := time.Since(startTime)

		if err != nil {
			t.Fatalf("Performance load failed: %v", err)
		}

		t.Logf("Loaded %d files in %v (%.2f files/sec)",
			numFiles, loadDuration, float64(numFiles)/loadDuration.Seconds())

		// Measure status performance (no changes)
		startTime = time.Now()
		status, err := cache.Status()
		statusDuration := time.Since(startTime)

		if err != nil {
			t.Fatalf("Performance status failed: %v", err)
		}

		t.Logf("Status check for %d files in %v (%.2f files/sec)",
			numFiles, statusDuration, float64(numFiles)/statusDuration.Seconds())

		if status.HasChanges() {
			t.Error("Performance test should show no initial changes")
		}

		// Modify a subset of files and measure status performance with changes
		modifyCount := 100
		for i := 0; i < modifyCount; i++ {
			filePath := filepath.Join(tempDir, fmt.Sprintf("file_%05d.txt", i))
			modifiedContent := fmt.Sprintf("Modified content for performance test file %d", i)
			err = os.WriteFile(filePath, []byte(modifiedContent), 0644)
			if err != nil {
				t.Fatalf("Failed to modify performance test file: %v", err)
			}
		}

		startTime = time.Now()
		status, err = cache.Status()
		statusWithChangesDuration := time.Since(startTime)

		if err != nil {
			t.Fatalf("Performance status with changes failed: %v", err)
		}

		t.Logf("Status check with %d changes in %v (%.2f files/sec)",
			modifyCount, statusWithChangesDuration, float64(numFiles)/statusWithChangesDuration.Seconds())

		if len(status.Modified) != modifyCount {
			t.Errorf("Expected %d modified files, got %d", modifyCount, len(status.Modified))
		}

		// Performance assertions (these are rough guidelines, adjust based on system capabilities)
		maxScanTimePerFile := 5 * time.Millisecond
		if scanDuration > time.Duration(numFiles)*maxScanTimePerFile {
			t.Logf("Warning: Scan performance slower than expected: %v > %v",
				scanDuration, time.Duration(numFiles)*maxScanTimePerFile)
		}

		maxLoadTimePerFile := 1 * time.Millisecond
		if loadDuration > time.Duration(numFiles)*maxLoadTimePerFile {
			t.Logf("Warning: Load performance slower than expected: %v > %v",
				loadDuration, time.Duration(numFiles)*maxLoadTimePerFile)
		}

		maxStatusTimePerFile := 1 * time.Millisecond
		if statusDuration > time.Duration(numFiles)*maxStatusTimePerFile {
			t.Logf("Warning: Status performance slower than expected: %v > %v",
				statusDuration, time.Duration(numFiles)*maxStatusTimePerFile)
		}
	})
}

func TestIntegrationMemoryUsage(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory usage test in short mode")
	}

	t.Run("MemoryUsageValidation", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "integration_memory_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create files for memory testing
		numFiles := 2000
		for i := 0; i < numFiles; i++ {
			fileName := fmt.Sprintf("memtest_%06d.txt", i)
			filePath := filepath.Join(tempDir, fileName)

			// Create files with significant content to test memory efficiency
			content := fmt.Sprintf("Memory test file %d\n", i) + strings.Repeat("x", 1000)

			err = os.WriteFile(filePath, []byte(content), 0644)
			if err != nil {
				t.Fatalf("Failed to create memory test file: %v", err)
			}
		}

		cache := NewDirectoryCache(tempDir, "")
		defer cache.Close()

		// Test memory efficiency of scanning
		err = cache.ScanDirectory()
		if err != nil {
			t.Fatalf("Memory test scan failed: %v", err)
		}

		// Test memory efficiency of loading (mmap should use minimal memory)
		err = cache.LoadIndex()
		if err != nil {
			t.Fatalf("Memory test load failed: %v", err)
		}

		// Verify mmap is being used
		if !cache.IsMmapped() {
			t.Error("Cache should be using mmap for memory efficiency")
		}

		// Test that we can access all entries without excessive memory usage
		entries := cache.GetEntries()
		if len(entries) != numFiles {
			t.Errorf("Expected %d entries for memory test, got %d", numFiles, len(entries))
		}

		// Access all entry data to verify zero-copy operations work
		var totalSize uint64
		for _, entry := range entries {
			totalSize += entry.FileSize
			_ = entry.RelativePath() // Test zero-copy path access
			_ = entry.HashString()   // Test hash access
			_ = entry.EntrySize()    // Test size access
		}

		if totalSize == 0 {
			t.Error("Total size should not be zero for memory test")
		}

		t.Logf("Memory test: processed %d files with total size %d bytes", numFiles, totalSize)

		// Test memory efficiency of status operations
		status, err := cache.Status()
		if err != nil {
			t.Fatalf("Memory test status failed: %v", err)
		}

		if status.HasChanges() {
			t.Error("Memory test should show no changes initially")
		}
	})
}
