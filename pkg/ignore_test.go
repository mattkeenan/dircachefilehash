package dircachefilehash

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestIgnoreTransitions tests files transitioning from non-ignored to ignored status
func TestIgnoreTransitions(t *testing.T) {
	// Create test directory structure
	testDir := t.TempDir()
	repoDir := filepath.Join(testDir, "repo")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("Failed to create repo dir: %v", err)
	}

	// Create test files
	testFiles := []struct {
		path    string
		content string
	}{
		{"file1.txt", "content1"},
		{"file2.log", "content2"},
		{"subdir/file3.txt", "content3"},
		{"subdir/file4.log", "content4"},
		{"temp/file5.tmp", "content5"},
	}

	for _, tf := range testFiles {
		fullPath := filepath.Join(repoDir, tf.path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("Failed to create dir for %s: %v", tf.path, err)
		}
		if err := os.WriteFile(fullPath, []byte(tf.content), 0644); err != nil {
			t.Fatalf("Failed to write %s: %v", tf.path, err)
		}
	}

	// Initialize dcfh repo
	dcfhDir := filepath.Join(repoDir, ".dcfh")
	if err := os.MkdirAll(dcfhDir, 0755); err != nil {
		t.Fatalf("Failed to create .dcfh dir: %v", err)
	}

	ms := NewMetaStore(repoDir, repoDir)

	// Create initial ignore file with only .dcfh pattern (gitignore syntax)
	ignoreFile := filepath.Join(dcfhDir, "ignore")
	if err := os.WriteFile(ignoreFile, []byte(`# Initial ignore patterns
.dcfh/
`), 0644); err != nil {
		t.Fatalf("Failed to create ignore file: %v", err)
	}

	// Force reload of ignore patterns
	_ = ms.ignoreManager.Reload()

	// First update - all files should be indexed
	ctx := context.Background()
	flags := map[string]string{}

	if err := runUpdate(ctx, ms, ms.scanRun(), flags); err != nil {
		t.Fatalf("Failed initial update: %v", err)
	}

	// Check initial file count
	fileCount, _, err := ms.Stats()
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}

	expectedCount := 5 // All test files
	if fileCount != expectedCount {
		t.Errorf("Expected %d files initially, got %d", expectedCount, fileCount)
	}

	// Update ignore file to exclude .log files (gitignore syntax)
	if err := os.WriteFile(ignoreFile, []byte(`# Updated ignore patterns
.dcfh/
*.log
`), 0644); err != nil {
		t.Fatalf("Failed to update ignore file: %v", err)
	}

	// Force reload of ignore patterns
	_ = ms.ignoreManager.Reload()

	// Check status - should show .log files as deleted
	status, err := runStatus(ctx, ms, ms.scanRun(), flags, nil)
	if err != nil {
		t.Fatalf("Failed to get status after ignore update: %v", err)
	}

	// Log files should be marked as deleted
	expectedDeleted := 2 // file2.log and subdir/file4.log
	if len(status.Deleted) != expectedDeleted {
		t.Errorf("Expected %d deleted files after ignoring .log files, got %d", expectedDeleted, len(status.Deleted))
		t.Logf("Deleted files: %v", status.Deleted)
	}

	// Verify the deleted files are the .log files
	deletedMap := make(map[string]bool)
	for _, deleted := range status.Deleted {
		deletedMap[deleted] = true
	}

	if !deletedMap["file2.log"] {
		t.Error("Expected file2.log to be marked as deleted")
	}
	if !deletedMap["subdir/file4.log"] {
		t.Error("Expected subdir/file4.log to be marked as deleted")
	}

	// Update to apply the ignore changes
	if err := runUpdate(ctx, ms, ms.scanRun(), flags); err != nil {
		t.Fatalf("Failed update after ignore change: %v", err)
	}

	// Check final file count
	fileCount, _, err = ms.Stats()
	if err != nil {
		t.Fatalf("Failed to get final stats: %v", err)
	}

	expectedCount = 3 // Only non-.log files
	if fileCount != expectedCount {
		t.Errorf("Expected %d files after ignoring .log files, got %d", expectedCount, fileCount)
	}

	// Add another pattern to ignore .tmp files (gitignore syntax)
	if err := os.WriteFile(ignoreFile, []byte(`# Further updated ignore patterns
.dcfh/
*.log
*.tmp
`), 0644); err != nil {
		t.Fatalf("Failed to update ignore file again: %v", err)
	}

	// Force reload of ignore patterns
	_ = ms.ignoreManager.Reload()

	// Check status again
	status, err = runStatus(ctx, ms, ms.scanRun(), flags, nil)
	if err != nil {
		t.Fatalf("Failed to get status after second ignore update: %v", err)
	}

	// Only .tmp file should be marked as deleted (since .log files are already gone)
	expectedDeleted = 1 // temp/file5.tmp
	if len(status.Deleted) != expectedDeleted {
		t.Errorf("Expected %d deleted files after ignoring .tmp files, got %d", expectedDeleted, len(status.Deleted))
		t.Logf("Deleted files: %v", status.Deleted)
	}

	if len(status.Deleted) > 0 && status.Deleted[0] != "temp/file5.tmp" {
		t.Errorf("Expected temp/file5.tmp to be marked as deleted, got %v", status.Deleted)
	}
}

// TestIgnoreDeindexConfig tests the ignore_is_deindex configuration option
func TestIgnoreDeindexConfig(t *testing.T) {
	// Create test directory
	testDir := t.TempDir()
	repoDir := filepath.Join(testDir, "repo")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("Failed to create repo dir: %v", err)
	}

	// Create test files
	testFiles := []string{"file1.txt", "file2.log", "file3.tmp"}
	for _, file := range testFiles {
		if err := os.WriteFile(filepath.Join(repoDir, file), []byte("content"), 0644); err != nil {
			t.Fatalf("Failed to write %s: %v", file, err)
		}
	}

	// Initialize dcfh repo
	dcfhDir := filepath.Join(repoDir, ".dcfh")
	if err := os.MkdirAll(dcfhDir, 0755); err != nil {
		t.Fatalf("Failed to create .dcfh dir: %v", err)
	}

	// Create config file with ignore_is_deindex = false
	configFile := filepath.Join(dcfhDir, "config")
	if err := os.WriteFile(configFile, []byte(`[ignore]
ignore_is_deindex = false

[symlink]
mode = none
`), 0644); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	// Create a new MetaStore which will load the config automatically
	ms := NewMetaStore(repoDir, repoDir)

	// Apply config
	if _, err := ms.ApplyConfigOverrides(map[string]string{}); err != nil {
		t.Fatalf("Failed to apply config: %v", err)
	}

	// Verify ignore_is_deindex is false
	if ms.ignoreIsDeindex {
		t.Error("Expected ignoreIsDeindex to be false based on config")
	}

	// Create ignore file (gitignore syntax)
	ignoreFile := filepath.Join(dcfhDir, "ignore")
	if err := os.WriteFile(ignoreFile, []byte(`.dcfh/
*.log
`), 0644); err != nil {
		t.Fatalf("Failed to create ignore file: %v", err)
	}

	// Initial update
	ctx := context.Background()
	if err := runUpdate(ctx, ms, ms.scanRun(), map[string]string{}); err != nil {
		t.Fatalf("Failed initial update: %v", err)
	}

	// All files should be indexed (even .log file because ignore_is_deindex is false)
	fileCount, _, err := ms.Stats()
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}

	if fileCount != 3 {
		t.Errorf("Expected 3 files with ignore_is_deindex=false, got %d", fileCount)
	}

	// Now change config to enable deindexing
	if err := os.WriteFile(configFile, []byte(`[ignore]
ignore_is_deindex = true

[symlink]
mode = none
`), 0644); err != nil {
		t.Fatalf("Failed to update config file: %v", err)
	}

	// Reload config by recreating MetaStore
	ms = NewMetaStore(repoDir, repoDir)

	// Apply config
	if _, err := ms.ApplyConfigOverrides(map[string]string{}); err != nil {
		t.Fatalf("Failed to apply updated config: %v", err)
	}

	// Check status - .log file should now be marked for deletion
	status, err := runStatus(ctx, ms, ms.scanRun(), map[string]string{}, nil)
	if err != nil {
		t.Fatalf("Failed to get status: %v", err)
	}

	if len(status.Deleted) != 1 || status.Deleted[0] != "file2.log" {
		t.Errorf("Expected file2.log to be marked for deletion, got %v", status.Deleted)
	}
}

// TestIgnoreManagerSuppressFile asserts that SetSuppressFile makes
// LoadIgnorePatterns a no-op even when an .dcfh/ignore file exists with
// real patterns.
func TestIgnoreManagerSuppressFile(t *testing.T) {
	tempDir := t.TempDir()
	metaDir := filepath.Join(tempDir, ".dcfh")
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		t.Fatalf("mkdir meta: %v", err)
	}
	ignorePath := filepath.Join(metaDir, "ignore")
	if err := os.WriteFile(ignorePath, []byte("*.tmp\n"), 0644); err != nil {
		t.Fatalf("write ignore: %v", err)
	}

	// Baseline: pattern is honoured.
	im := NewIgnoreManager(metaDir)
	if err := im.LoadIgnorePatterns(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if !im.ShouldIgnore("foo.tmp") {
		t.Fatal("baseline: *.tmp should match foo.tmp")
	}

	// With suppressFile set before load, patterns are not read.
	im2 := NewIgnoreManager(metaDir)
	im2.SetSuppressFile(true)
	if err := im2.LoadIgnorePatterns(); err != nil {
		t.Fatalf("load suppressed: %v", err)
	}
	if im2.HasPatterns() {
		t.Errorf("suppressed manager should report no patterns; got %d", len(im2.GetPatterns()))
	}
	if im2.ShouldIgnore("foo.tmp") {
		t.Errorf("suppressed manager should not match foo.tmp")
	}
}
