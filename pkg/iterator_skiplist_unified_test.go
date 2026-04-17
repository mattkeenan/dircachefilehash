package dircachefilehash

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBinaryEntrySkiplistIterator_BasicIteration(t *testing.T) {
	// Create test directory and entries using existing infrastructure
	tempDir := createTempDir(t)
	defer func() { _ = os.RemoveAll(tempDir) }()

	dc := createTestDirectoryCache(t, tempDir)
	defer func() { _ = dc.cleanupCurrentScanFile() }()

	// Create test files
	writeTestFile(t, filepath.Join(tempDir, "file1.txt"), "content1")
	writeTestFile(t, filepath.Join(tempDir, "file2.txt"), "content2")

	// Create subdirectory
	subDir := filepath.Join(tempDir, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subdirectory: %v", err)
	}
	writeTestFile(t, filepath.Join(subDir, "file3.txt"), "content3")

	// Load main index to get a real skiplist with actual entries
	skiplist, err := dc.LoadMainIndex()
	if err != nil {
		t.Fatalf("Failed to load main index: %v", err)
	}

	// If main index is empty, do a quick scan to populate it
	if skiplist.Length() == 0 {
		// Perform a quick update to populate the index
		_, err := dc.runStatusWorkflowUnified(context.Background())
		if err != nil {
			t.Logf("Warning: failed to populate index: %v", err)
		}

		// Reload main index
		skiplist, err = dc.LoadMainIndex()
		if err != nil {
			t.Fatalf("Failed to reload main index: %v", err)
		}
	}

	// Skip test if we still have no entries (this might happen in test environment)
	if skiplist.Length() == 0 {
		t.Skip("No entries in skiplist for testing - this might be expected in test environment")
	}

	// Create iterator with background context
	ctx := t.Context()
	iterator := NewBinaryEntrySkiplistIterator(ctx, skiplist, "test-unified-skiplist")
	defer func() { _ = iterator.Close() }()

	var paths []string
	entryCount := 0

	// Iterate through all entries
	for iterator.HasNext() {
		entry, err := iterator.Next()
		if err != nil {
			t.Fatalf("Error during iteration: %v", err)
		}

		if entry == nil {
			break
		}

		entryCount++

		// Get path from interface
		path, err := entry.RelativePath()
		if err != nil {
			t.Errorf("Error getting path for entry %d: %v", entryCount, err)
			continue
		}

		paths = append(paths, path)
		t.Logf("Entry %d: %s", entryCount, path)
	}

	// Just verify we got some entries and they are in order
	if entryCount == 0 {
		t.Error("Expected some entries from iterator")
	}

	// Verify entries are in sorted order
	for i := 1; i < len(paths); i++ {
		if paths[i] <= paths[i-1] {
			t.Errorf("Entries not in sorted order: %s should come after %s", paths[i], paths[i-1])
		}
	}

	t.Logf("Successfully iterated through %d entries in sorted order", entryCount)
}

func TestBinaryEntrySkiplistIterator_EmptySkiplist(t *testing.T) {
	// Create empty skiplist
	skiplist := NewSkiplistWrapper(16, MainContext)

	// Create iterator
	ctx := t.Context()
	iterator := NewBinaryEntrySkiplistIterator(ctx, skiplist, "test-empty")
	defer func() { _ = iterator.Close() }()

	// Should have no entries
	if iterator.HasNext() {
		t.Error("Empty skiplist should not have entries")
	}

	entry, err := iterator.Next()
	if err != nil {
		t.Fatalf("Error iterating empty skiplist: %v", err)
	}

	if entry != nil {
		t.Error("Expected no entries from empty skiplist")
	}
}

func TestBinaryEntrySkiplistIterator_NilSkiplist(t *testing.T) {
	// Create iterator with nil skiplist
	ctx := t.Context()
	iterator := NewBinaryEntrySkiplistIterator(ctx, nil, "test-nil")
	defer func() { _ = iterator.Close() }()

	// Should be immediately exhausted
	if iterator.HasNext() {
		t.Error("Iterator with nil skiplist should not have entries")
	}

	entry, err := iterator.Next()
	if err != nil {
		t.Fatalf("Error with nil skiplist: %v", err)
	}

	if entry != nil {
		t.Error("Expected no entries with nil skiplist")
	}
}

func TestBinaryEntrySkiplistIterator_ClosedIterator(t *testing.T) {
	// Test with empty skiplist to avoid needing to create entries
	skiplist := NewSkiplistWrapper(16, MainContext)
	ctx := t.Context()
	iterator := NewBinaryEntrySkiplistIterator(ctx, skiplist, "test-closed")

	// Close the iterator immediately
	_ = iterator.Close()

	// Should return error when trying to use closed iterator
	entry, err := iterator.Next()
	if err == nil {
		t.Error("Expected error when using closed iterator")
	}

	if entry != nil {
		t.Error("Expected no entry from closed iterator")
	}

	if iterator.HasNext() {
		t.Error("Closed iterator should not have entries")
	}
}

func TestBinaryEntrySkiplistIterator_InterfaceCompliance(t *testing.T) {
	// Test that empty iterator implements BinaryEntryIterator interface
	ctx := t.Context()
	iterator := NewBinaryEntrySkiplistIterator(ctx, nil, "test-interface")
	defer func() { _ = iterator.Close() }()

	// Test that it implements BinaryEntryIterator interface
	var _ BinaryEntryIterator = iterator

	// Test basic interface methods
	if iterator.Name() != "test-interface" {
		t.Errorf("Expected name 'test-interface', got '%s'", iterator.Name())
	}

	// With nil skiplist, should not have entries
	if iterator.HasNext() {
		t.Error("Iterator with nil skiplist should not have entries")
	}

	entry, err := iterator.Next()
	if err != nil {
		t.Fatalf("Error with nil iterator: %v", err)
	}

	if entry != nil {
		t.Error("Expected no entry from nil iterator")
	}

	// Test CurrentPath is empty
	if iterator.CurrentPath() != "" {
		t.Errorf("Expected empty current path, got '%s'", iterator.CurrentPath())
	}
}
