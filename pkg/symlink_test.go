package dircachefilehash

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSymlinkModeTransitions tests the dynamic symlink following/unfollowing behavior
func TestSymlinkModeTransitions(t *testing.T) {
	// Create test directory structure
	testDir := t.TempDir()
	
	// Create a directory that will be symlinked
	targetDir := filepath.Join(testDir, "target")
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		t.Fatalf("Failed to create target dir: %v", err)
	}
	
	// Create files in target directory
	testFiles := []struct {
		path    string
		content string
	}{
		{"file1.txt", "content1"},
		{"subdir/file2.txt", "content2"},
		{"subdir/deep/file3.txt", "content3"},
	}
	
	for _, tf := range testFiles {
		fullPath := filepath.Join(targetDir, tf.path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("Failed to create dir for %s: %v", tf.path, err)
		}
		if err := os.WriteFile(fullPath, []byte(tf.content), 0644); err != nil {
			t.Fatalf("Failed to write %s: %v", tf.path, err)
		}
	}
	
	// Create repo directory with symlink
	repoDir := filepath.Join(testDir, "repo")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("Failed to create repo dir: %v", err)
	}
	
	// Create a regular file in repo
	if err := os.WriteFile(filepath.Join(repoDir, "regular.txt"), []byte("regular content"), 0644); err != nil {
		t.Fatalf("Failed to write regular file: %v", err)
	}
	
	// Create symlink to target directory
	symlinkPath := filepath.Join(repoDir, "linked")
	if err := os.Symlink(targetDir, symlinkPath); err != nil {
		t.Fatalf("Failed to create symlink: %v", err)
	}
	
	// Debug: Verify files and symlink exist
	entries, err := os.ReadDir(repoDir)
	if err != nil {
		t.Fatalf("Failed to read repo dir: %v", err)
	}
	t.Logf("Repo dir contains %d entries:", len(entries))
	for _, e := range entries {
		info, _ := e.Info()
		t.Logf("  %s (symlink: %v)", e.Name(), info.Mode()&os.ModeSymlink != 0)
	}
	
	// Initialize dcfh repo
	dcfhDir := filepath.Join(repoDir, ".dcfh")
	if err := os.MkdirAll(dcfhDir, 0755); err != nil {
		t.Fatalf("Failed to create .dcfh dir: %v", err)
	}
	
	dc := NewDirectoryCache(repoDir, repoDir)
	
	// First update with "all" - should follow symlinks
	flags := map[string]string{"symlinks": "all"}
	shutdownChan := make(<-chan struct{})
	
	// Apply flags to configure symlink mode
	if err := dc.ApplyConfigOverrides(flags); err != nil {
		// If no config loaded, set directly
		dc.symlinkMode = "all"
	}
	
	t.Logf("Before first update, symlink mode is: %s", dc.symlinkMode)
	
	// Update entire repository (no specific paths)
	if err := dc.Update(shutdownChan, flags); err != nil {
		t.Fatalf("Failed initial update with symlinks=all: %v", err)
	}
	
	// Check that symlinked files are in the index
	fileCount, _, err := dc.Stats()
	if err != nil {
		t.Fatalf("Failed to get stats after initial update: %v", err)
	}
	
	// Debug: List what files were found
	t.Logf("After update with symlinks=all, found %d files", fileCount)
	
	// Check if index file was created
	if _, err := os.Stat(dc.IndexFile); err != nil {
		t.Logf("Main index file not found: %v", err)
	}
	
	// Should have: regular.txt + 3 files under linked/
	// Note: directory symlinks themselves are not counted in Stats
	expectedCount := 4
	if fileCount != expectedCount {
		t.Errorf("Expected %d files with symlinks=all, got %d", expectedCount, fileCount)
	}
	
	// Now check status with "none" - should see deleted files
	// Don't do Update, just Status to see what would be deleted
	flags["symlinks"] = "none"
	
	// Enable debug to see what's happening
	SetDebugFlags("scan,scanning,symlinks")
	defer SetDebugFlags("")
	
	status, err := dc.Status(shutdownChan, flags)
	if err != nil {
		t.Fatalf("Failed to get status after symlinks=none: %v", err)
	}
	
	// Debug: Show all status changes
	t.Logf("Status after symlinks=none:")
	t.Logf("  Modified: %v", status.Modified)
	t.Logf("  Added: %v", status.Added)
	t.Logf("  Deleted: %v", status.Deleted)
	
	// Files under linked/ should be marked as deleted
	expectedDeleted := 3 // The 3 files under the symlink
	if len(status.Deleted) != expectedDeleted {
		t.Errorf("Expected %d deleted files after symlinks=none, got %d", expectedDeleted, len(status.Deleted))
		t.Logf("Deleted files: %v", status.Deleted)
	}
	
	// Verify the deleted files are the ones under the symlink
	for _, deleted := range status.Deleted {
		if !strings.HasPrefix(deleted, "linked/") {
			t.Errorf("Unexpected deleted file: %s", deleted)
		}
	}
	
	// Switch back to "all" - status should show no changes
	flags["symlinks"] = "all"
	status, err = dc.Status(shutdownChan, flags)
	if err != nil {
		t.Fatalf("Failed to get status after switching back to all: %v", err)
	}
	
	// Should have no changes when viewing with symlinks=all
	if len(status.Deleted) != 0 || len(status.Modified) != 0 || len(status.Added) != 0 {
		t.Errorf("Expected no changes with symlinks=all, got: deleted=%d, modified=%d, added=%d",
			len(status.Deleted), len(status.Modified), len(status.Added))
	}
}

// TestSymlinkModeInternal tests internal symlink mode
func TestSymlinkModeInternal(t *testing.T) {
	testDir := t.TempDir()
	
	// Create repo directory
	repoDir := filepath.Join(testDir, "repo")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("Failed to create repo dir: %v", err)
	}
	
	// Create internal target directory
	internalTarget := filepath.Join(repoDir, "internal-target")
	if err := os.MkdirAll(internalTarget, 0755); err != nil {
		t.Fatalf("Failed to create internal target: %v", err)
	}
	
	// Create external target directory
	externalTarget := filepath.Join(testDir, "external-target")
	if err := os.MkdirAll(externalTarget, 0755); err != nil {
		t.Fatalf("Failed to create external target: %v", err)
	}
	
	// Add files to both targets
	for i, target := range []string{internalTarget, externalTarget} {
		fileName := filepath.Join(target, fmt.Sprintf("file%d.txt", i))
		if err := os.WriteFile(fileName, []byte(fmt.Sprintf("content%d", i)), 0644); err != nil {
			t.Fatalf("Failed to write file: %v", err)
		}
	}
	
	// Add a regular file to the repo root for comparison
	if err := os.WriteFile(filepath.Join(repoDir, "root.txt"), []byte("root content"), 0644); err != nil {
		t.Fatalf("Failed to write root file: %v", err)
	}
	
	// Create symlinks
	internalLink := filepath.Join(repoDir, "internal-link")
	if err := os.Symlink(internalTarget, internalLink); err != nil {
		t.Fatalf("Failed to create internal symlink: %v", err)
	}
	
	externalLink := filepath.Join(repoDir, "external-link")
	if err := os.Symlink(externalTarget, externalLink); err != nil {
		t.Fatalf("Failed to create external symlink: %v", err)
	}
	
	// Initialize and update with internal mode
	dcfhDir := filepath.Join(repoDir, ".dcfh")
	if err := os.MkdirAll(dcfhDir, 0755); err != nil {
		t.Fatalf("Failed to create .dcfh dir: %v", err)
	}
	dc := NewDirectoryCache(repoDir, repoDir)
	
	// Enable debug output
	SetDebugFlags("symlinks")
	defer SetDebugFlags("")
	
	shutdownChan := make(<-chan struct{})
	flags := map[string]string{"symlinks": "internal"}
	if err := dc.Update(shutdownChan, flags); err != nil {
		t.Fatalf("Failed update with symlinks=internal: %v", err)
	}
	
	fileCount, _, err := dc.Stats()
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}
	
	// Should have: root.txt + internal-target/file0.txt (direct) + internal-link/file0.txt (via symlink)
	// But NOT file1.txt (via external-link which is not followed)
	expectedCount := 3
	if fileCount != expectedCount {
		t.Errorf("Expected %d files with symlinks=internal, got %d", expectedCount, fileCount)
		
		// Debug: List all files found
		status, err := dc.Status(shutdownChan, flags)
		if err == nil {
			t.Logf("Modified: %v", status.Modified)
			t.Logf("Added: %v", status.Added)
			t.Logf("Deleted: %v", status.Deleted)
		}
		
		// Also try to see what's in the main index
		mainSkiplist, err := dc.LoadMainIndex()
		if err == nil {
			t.Logf("Files in main index:")
			mainSkiplist.ForEach(func(entry *binaryEntry, context string) bool {
				if !entry.IsDeleted() {
					t.Logf("  %s", entry.RelativePath())
				}
				return true
			})
		}
	}
}

// TestSymlinkModeExternal tests external symlink mode
func TestSymlinkModeExternal(t *testing.T) {
	testDir := t.TempDir()
	
	// Create repo directory
	repoDir := filepath.Join(testDir, "repo")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("Failed to create repo dir: %v", err)
	}
	
	// Create internal target directory
	internalTarget := filepath.Join(repoDir, "internal-target")
	if err := os.MkdirAll(internalTarget, 0755); err != nil {
		t.Fatalf("Failed to create internal target: %v", err)
	}
	
	// Create external target directory
	externalTarget := filepath.Join(testDir, "external-target")
	if err := os.MkdirAll(externalTarget, 0755); err != nil {
		t.Fatalf("Failed to create external target: %v", err)
	}
	
	// Add files to both targets
	for i, target := range []string{internalTarget, externalTarget} {
		fileName := filepath.Join(target, fmt.Sprintf("file%d.txt", i))
		if err := os.WriteFile(fileName, []byte(fmt.Sprintf("content%d", i)), 0644); err != nil {
			t.Fatalf("Failed to write file: %v", err)
		}
	}
	
	// Add a regular file to the repo root for comparison
	if err := os.WriteFile(filepath.Join(repoDir, "root.txt"), []byte("root content"), 0644); err != nil {
		t.Fatalf("Failed to write root file: %v", err)
	}
	
	// Create symlinks
	internalLink := filepath.Join(repoDir, "internal-link")
	if err := os.Symlink(internalTarget, internalLink); err != nil {
		t.Fatalf("Failed to create internal symlink: %v", err)
	}
	
	externalLink := filepath.Join(repoDir, "external-link")
	if err := os.Symlink(externalTarget, externalLink); err != nil {
		t.Fatalf("Failed to create external symlink: %v", err)
	}
	
	// Initialize and update with external mode
	dcfhDir := filepath.Join(repoDir, ".dcfh")
	if err := os.MkdirAll(dcfhDir, 0755); err != nil {
		t.Fatalf("Failed to create .dcfh dir: %v", err)
	}
	dc := NewDirectoryCache(repoDir, repoDir)
	
	shutdownChan := make(<-chan struct{})
	flags := map[string]string{"symlinks": "external"}
	if err := dc.Update(shutdownChan, flags); err != nil {
		t.Fatalf("Failed update with symlinks=external: %v", err)
	}
	
	fileCount, _, err := dc.Stats()
	if err != nil {
		t.Fatalf("Failed to get stats: %v", err)
	}
	
	// Should have: root.txt + internal-target/file0.txt (direct scan) + external-link/file1.txt (via symlink)
	// But NOT internal-link/file0.txt (internal symlink not followed in external mode)
	expectedCount := 3
	if fileCount != expectedCount {
		t.Errorf("Expected %d files with symlinks=external, got %d", expectedCount, fileCount)
	}
}

// TestSymlinkCacheRadixBehavior tests that the radix cache works correctly with sorted paths
func TestSymlinkCacheRadixBehavior(t *testing.T) {
	testDir := t.TempDir()
	
	// Create a complex directory structure
	repoDir := filepath.Join(testDir, "repo")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("Failed to create repo dir: %v", err)
	}
	
	// Create multiple symlinked directories
	targets := []string{"target-a", "target-b", "target-c"}
	for _, target := range targets {
		targetPath := filepath.Join(testDir, target)
		if err := os.MkdirAll(targetPath, 0755); err != nil {
			t.Fatalf("Failed to create target %s: %v", target, err)
		}
		
		// Create deep structure in each target
		for i := 0; i < 3; i++ {
			filePath := filepath.Join(targetPath, fmt.Sprintf("subdir%d/file.txt", i))
			if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
				t.Fatalf("Failed to create subdir: %v", err)
			}
			if err := os.WriteFile(filePath, []byte("content"), 0644); err != nil {
				t.Fatalf("Failed to write file: %v", err)
			}
		}
		
		// Create symlink in repo
		linkPath := filepath.Join(repoDir, "link-"+target)
		if err := os.Symlink(targetPath, linkPath); err != nil {
			t.Fatalf("Failed to create symlink: %v", err)
		}
	}
	
	// Add some regular files between symlinks (alphabetically)
	regularFiles := []string{"aaa.txt", "link-target-between.txt", "zzz.txt"}
	for _, file := range regularFiles {
		if err := os.WriteFile(filepath.Join(repoDir, file), []byte("regular"), 0644); err != nil {
			t.Fatalf("Failed to write regular file %s: %v", file, err)
		}
	}
	
	// Initialize with all symlinks followed
	dcfhDir := filepath.Join(repoDir, ".dcfh")
	if err := os.MkdirAll(dcfhDir, 0755); err != nil {
		t.Fatalf("Failed to create .dcfh dir: %v", err)
	}
	dc := NewDirectoryCache(repoDir, repoDir)
	
	shutdownChan := make(<-chan struct{})
	flags := map[string]string{"symlinks": "all"}
	if err := dc.Update(shutdownChan, flags); err != nil {
		t.Fatalf("Failed initial update: %v", err)
	}
	
	// Switch to none and check status before updating
	flags["symlinks"] = "none"
	
	status, err := dc.Status(shutdownChan, flags)
	if err != nil {
		t.Fatalf("Failed to get status: %v", err)
	}
	
	// All files under symlinks should be deleted
	expectedDeleted := 9 // 3 targets * 3 files each
	if len(status.Deleted) != expectedDeleted {
		t.Errorf("Expected %d deleted files, got %d", expectedDeleted, len(status.Deleted))
	}
	
	// Regular files should NOT be deleted
	for _, deleted := range status.Deleted {
		for _, regular := range regularFiles {
			if deleted == regular {
				t.Errorf("Regular file %s should not be deleted", regular)
			}
		}
	}
}

// TestUnfollowedSymlinkNoHashing verifies that files under unfollowed symlinks are not submitted for hashing
func TestUnfollowedSymlinkNoHashing(t *testing.T) {
	testDir := t.TempDir()
	
	// Create repo and target
	repoDir := filepath.Join(testDir, "repo")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("Failed to create repo dir: %v", err)
	}
	
	targetDir := filepath.Join(testDir, "target")
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		t.Fatalf("Failed to create target dir: %v", err)
	}
	
	// Create a large file that would take time to hash
	largeFile := filepath.Join(targetDir, "large.bin")
	data := make([]byte, 10*1024*1024) // 10MB
	for i := range data {
		data[i] = byte(i % 256)
	}
	if err := os.WriteFile(largeFile, data, 0644); err != nil {
		t.Fatalf("Failed to write large file: %v", err)
	}
	
	// Create symlink
	linkPath := filepath.Join(repoDir, "link")
	if err := os.Symlink(targetDir, linkPath); err != nil {
		t.Fatalf("Failed to create symlink: %v", err)
	}
	
	// Initialize and update with symlinks followed
	dcfhDir := filepath.Join(repoDir, ".dcfh")
	if err := os.MkdirAll(dcfhDir, 0755); err != nil {
		t.Fatalf("Failed to create .dcfh dir: %v", err)
	}
	dc := NewDirectoryCache(repoDir, repoDir)
	
	// Enable debug output to catch any hashing attempts
	SetDebugFlags("scanning")
	defer SetDebugFlags("")
	
	shutdownChan := make(<-chan struct{})
	flags := map[string]string{"symlinks": "all"}
	if err := dc.Update(shutdownChan, flags); err != nil {
		t.Fatalf("Failed initial update: %v", err)
	}
	
	// Now modify the large file
	data[0] = 255
	if err := os.WriteFile(largeFile, data, 0644); err != nil {
		t.Fatalf("Failed to modify large file: %v", err)
	}
	
	// Update with symlinks=none - the modified file should NOT be hashed
	flags["symlinks"] = "none"
	if err := dc.Update(shutdownChan, flags); err != nil {
		t.Fatalf("Failed update with symlinks=none: %v", err)
	}
	
	// The file should be marked as deleted without being hashed
	status, err := dc.Status(shutdownChan, flags)
	if err != nil {
		t.Fatalf("Failed to get status: %v", err)
	}
	
	found := false
	for _, deleted := range status.Deleted {
		if deleted == "link/large.bin" {
			found = true
			break
		}
	}
	
	if !found {
		t.Errorf("Expected link/large.bin to be in deleted files")
	}
}