package dircachefilehash

import (
	"fmt"
	"testing"
)

// mockIterator implements BinaryEntryIterator for testing
type mockIterator struct {
	iteratorBase
	entries    []BinaryEntryInterface
	index      int
	closeError error
}

// newMockIterator creates a mock iterator with predefined entries
func newMockIterator(name string, entries []BinaryEntryInterface) *mockIterator {
	return &mockIterator{
		iteratorBase: iteratorBase{name: name},
		entries:      entries,
		index:        0,
	}
}

// Next returns the next entry
func (mi *mockIterator) Next() (BinaryEntryInterface, error) {
	if err := mi.checkClosed(); err != nil {
		return nil, err
	}

	if mi.index >= len(mi.entries) {
		mi.markExhausted()
		return nil, nil
	}

	entry := mi.entries[mi.index]
	mi.index++
	mi.updateCurrentPathFromInterface(entry)

	return entry, nil
}

// Close closes the iterator
func (mi *mockIterator) Close() error {
	mi.markClosed()
	return mi.closeError
}

// Helper function to create a BinaryEntryInterface with a specific path
// This creates a proper memory layout that RelativePath() can handle
func createMockBinaryEntry(path string) BinaryEntryInterface {
	// Use the existing test infrastructure
	testData := &TestEntryData{
		RelativePath: path,
		Size:         128,
		CTimeWall:    encodeWallTime(1234567890, 0),
		MTimeWall:    encodeWallTime(1234567900, 0),
		Dev:          1,
		Ino:          123,
		Mode:         0644,
		UID:          1000,
		GID:          1000,
		FileSize:     100,
		HashType:     HashTypeSHA1,
		EntryFlags:   0,
		IsDeleted:    false,
	}

	// Set a simple hash pattern
	for i := range 20 {
		testData.Hash[i] = byte(i + len(path)%20)
	}

	return createBESkiplist(&testing.T{}, testData)
}

func TestIteratorBase(t *testing.T) {
	t.Run("InitialState", func(t *testing.T) {
		iter := &iteratorBase{name: "test-iterator"}

		if iter.Name() != "test-iterator" {
			t.Errorf("Expected name 'test-iterator', got '%s'", iter.Name())
		}

		if iter.CurrentPath() != "" {
			t.Errorf("Expected empty current path, got '%s'", iter.CurrentPath())
		}

		if !iter.HasNext() {
			t.Error("Expected HasNext() to be true initially")
		}
	})

	t.Run("MarkExhausted", func(t *testing.T) {
		iter := &iteratorBase{name: "test-iterator"}

		// Set some initial state
		iter.currentPath = "some/path"

		// Mark as exhausted
		iter.markExhausted()

		if iter.HasNext() {
			t.Error("Expected HasNext() to be false after markExhausted()")
		}

		if iter.CurrentPath() != "" {
			t.Errorf("Expected empty current path after exhausted, got '%s'", iter.CurrentPath())
		}
	})

	t.Run("MarkClosed", func(t *testing.T) {
		iter := &iteratorBase{name: "test-iterator"}

		// Set some initial state
		iter.currentPath = "some/path"

		// Mark as closed
		iter.markClosed()

		if iter.HasNext() {
			t.Error("Expected HasNext() to be false after markClosed()")
		}

		if iter.CurrentPath() != "" {
			t.Errorf("Expected empty current path after closed, got '%s'", iter.CurrentPath())
		}

		if err := iter.checkClosed(); err == nil {
			t.Error("Expected checkClosed() to return error after markClosed()")
		}
	})

	t.Run("UpdateCurrentPath", func(t *testing.T) {
		iter := &iteratorBase{name: "test-iterator"}

		// Test with valid entry
		entry := createMockBinaryEntry("test/path.txt")
		iter.updateCurrentPathFromInterface(entry)

		if iter.CurrentPath() != "test/path.txt" {
			t.Errorf("Expected current path 'test/path.txt', got '%s'", iter.CurrentPath())
		}

		// Test with nil entry
		iter.updateCurrentPath(nil)

		if iter.CurrentPath() != "" {
			t.Errorf("Expected empty current path with nil entry, got '%s'", iter.CurrentPath())
		}
	})
}

func TestMockIterator(t *testing.T) {
	// Create test entries
	entries := []BinaryEntryInterface{
		createMockBinaryEntry("file1.txt"),
		createMockBinaryEntry("file2.txt"),
		createMockBinaryEntry("subdir/file3.txt"),
	}

	t.Run("NormalIteration", func(t *testing.T) {
		iter := newMockIterator("test-mock", entries)
		defer func() { _ = iter.Close() }()

		// Check initial state
		if iter.Name() != "test-mock" {
			t.Errorf("Expected name 'test-mock', got '%s'", iter.Name())
		}

		if !iter.HasNext() {
			t.Error("Expected HasNext() to be true initially")
		}

		if iter.CurrentPath() != "" {
			t.Errorf("Expected empty current path initially, got '%s'", iter.CurrentPath())
		}

		// Iterate through entries
		expectedPaths := []string{"file1.txt", "file2.txt", "subdir/file3.txt"}

		for i, expectedPath := range expectedPaths {
			entry, err := iter.Next()
			if err != nil {
				t.Fatalf("Unexpected error on iteration %d: %v", i, err)
			}

			if entry == nil {
				t.Fatalf("Expected entry on iteration %d, got nil", i)
			}

			if iter.CurrentPath() != expectedPath {
				t.Errorf("Expected current path '%s' on iteration %d, got '%s'",
					expectedPath, i, iter.CurrentPath())
			}
		}

		// Check exhaustion
		entry, err := iter.Next()
		if err != nil {
			t.Fatalf("Unexpected error when exhausted: %v", err)
		}

		if entry != nil {
			t.Error("Expected nil entry when exhausted")
		}

		if iter.HasNext() {
			t.Error("Expected HasNext() to be false when exhausted")
		}

		if iter.CurrentPath() != "" {
			t.Errorf("Expected empty current path when exhausted, got '%s'", iter.CurrentPath())
		}
	})

	t.Run("EmptyIterator", func(t *testing.T) {
		iter := newMockIterator("empty-mock", []BinaryEntryInterface{})
		defer func() { _ = iter.Close() }()

		// Should be immediately exhausted
		entry, err := iter.Next()
		if err != nil {
			t.Fatalf("Unexpected error with empty iterator: %v", err)
		}

		if entry != nil {
			t.Error("Expected nil entry with empty iterator")
		}

		if iter.HasNext() {
			t.Error("Expected HasNext() to be false with empty iterator")
		}
	})

	t.Run("ClosedIterator", func(t *testing.T) {
		iter := newMockIterator("closed-mock", entries)

		// Close the iterator
		if err := iter.Close(); err != nil {
			t.Fatalf("Unexpected error closing iterator: %v", err)
		}

		// Attempting to iterate should return error
		entry, err := iter.Next()
		if err == nil {
			t.Error("Expected error when calling Next() on closed iterator")
		}

		if entry != nil {
			t.Error("Expected nil entry when calling Next() on closed iterator")
		}

		if iter.HasNext() {
			t.Error("Expected HasNext() to be false on closed iterator")
		}

		// Closing again should be safe
		if err := iter.Close(); err != nil {
			t.Errorf("Unexpected error closing iterator again: %v", err)
		}
	})

	t.Run("CloseError", func(t *testing.T) {
		iter := newMockIterator("error-mock", entries)
		iter.closeError = fmt.Errorf("mock close error")

		// Close should return the error
		if err := iter.Close(); err == nil {
			t.Error("Expected close error")
		} else if err.Error() != "mock close error" {
			t.Errorf("Expected 'mock close error', got '%s'", err.Error())
		}

		// Iterator should still be marked as closed
		if iter.HasNext() {
			t.Error("Expected HasNext() to be false even with close error")
		}
	})
}

func TestBinaryEntryPath(t *testing.T) {
	// Test that our mock binaryEntry path handling works correctly
	testPaths := []string{
		"simple.txt",
		"with spaces.txt",
		"subdir/nested.txt",
		"deep/nested/path/file.txt",
	}

	for _, path := range testPaths {
		entry := createMockBinaryEntry(path)
		actualPath, err := entry.RelativePath()
		if err != nil {
			t.Fatalf("Failed to get relative path: %v", err)
		}
		if actualPath != path {
			t.Errorf("Expected path '%s', got '%s'", path, actualPath)
		}
	}
}
