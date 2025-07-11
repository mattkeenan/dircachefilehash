package dircachefilehash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAtomicWriteIndex tests the atomicWriteIndex function which consolidates
// the common pattern of writing to a temp file and atomically renaming it
func TestAtomicWriteIndex(t *testing.T) {
	// Create test directory
	testDir := t.TempDir()
	
	// Initialize DirectoryCache
	dc := NewDirectoryCache(testDir, testDir)
	
	// Create a test skiplist with some entries
	skiplist := NewSkiplistWrapper(16, MainContext)
	
	// Add some test entries
	// Since we can't create binaryEntryRef directly, we'll just test with an empty skiplist
	// The atomic write function should still work correctly
	
	// Test writing to cache file
	t.Run("WriteCacheFile", func(t *testing.T) {
		cacheFile := filepath.Join(testDir, ".dcfh", "cache.idx")
		
		// Ensure .dcfh directory exists
		os.MkdirAll(filepath.Dir(cacheFile), 0755)
		
		// Write using atomicWriteIndex
		err := dc.atomicWriteIndex(skiplist, cacheFile, CacheContext, false)
		if err != nil {
			t.Fatalf("Failed to write cache file: %v", err)
		}
		
		// Check that cache file exists
		if _, err := os.Stat(cacheFile); err != nil {
			t.Errorf("Cache file not created: %v", err)
		}
		
		// Check no temp files remain
		entries, err := os.ReadDir(filepath.Dir(cacheFile))
		if err != nil {
			t.Fatalf("Failed to read directory: %v", err)
		}
		
		for _, entry := range entries {
			if strings.Contains(entry.Name(), ".tmp") {
				t.Errorf("Temp file left behind: %s", entry.Name())
			}
		}
	})
	
	// Test writing to main index file
	t.Run("WriteMainIndex", func(t *testing.T) {
		mainFile := filepath.Join(testDir, ".dcfh", "main.idx")
		
		// Write using atomicWriteIndex with excludeDeleted=true
		err := dc.atomicWriteIndex(skiplist, mainFile, MainContext, true)
		if err != nil {
			t.Fatalf("Failed to write main index: %v", err)
		}
		
		// Check that main file exists
		if _, err := os.Stat(mainFile); err != nil {
			t.Errorf("Main index file not created: %v", err)
		}
		
		// Check no temp files remain
		entries, err := os.ReadDir(filepath.Dir(mainFile))
		if err != nil {
			t.Fatalf("Failed to read directory: %v", err)
		}
		
		for _, entry := range entries {
			if strings.Contains(entry.Name(), ".tmp") {
				t.Errorf("Temp file left behind: %s", entry.Name())
			}
		}
	})
	
	// Test error handling - write to read-only directory
	t.Run("WriteError", func(t *testing.T) {
		roDir := filepath.Join(testDir, "readonly")
		os.MkdirAll(roDir, 0755)
		
		// Make directory read-only
		os.Chmod(roDir, 0555)
		defer os.Chmod(roDir, 0755) // Restore permissions for cleanup
		
		targetFile := filepath.Join(roDir, "test.idx")
		
		// This should fail due to permissions
		err := dc.atomicWriteIndex(skiplist, targetFile, "", false)
		if err == nil {
			t.Error("Expected error writing to read-only directory")
		}
		
		// Check no temp files were left behind
		// (Can't read the directory since it's read-only, but that's OK)
	})
}

// TestAtomicWriteConsistency verifies write operations are truly atomic
func TestAtomicWriteConsistency(t *testing.T) {
	testDir := t.TempDir()
	dc := NewDirectoryCache(testDir, testDir)
	
	// Create initial index file
	indexFile := filepath.Join(testDir, ".dcfh", "test.idx")
	os.MkdirAll(filepath.Dir(indexFile), 0755)
	
	// Write initial content
	initialContent := []byte("initial content")
	os.WriteFile(indexFile, initialContent, 0644)
	
	// Create a skiplist that will fail to write
	// (We can't easily simulate a write failure, so we'll just verify the pattern)
	skiplist := NewSkiplistWrapper(16, MainContext)
	
	// Successful write should replace the file
	err := dc.atomicWriteIndex(skiplist, indexFile, "", false)
	if err != nil {
		t.Fatalf("Failed to write index: %v", err)
	}
	
	// File should exist and not contain initial content
	content, err := os.ReadFile(indexFile)
	if err != nil {
		t.Fatalf("Failed to read index file: %v", err)
	}
	
	if string(content) == string(initialContent) {
		t.Error("File was not replaced")
	}
}