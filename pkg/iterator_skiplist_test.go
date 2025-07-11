package dircachefilehash

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"unsafe"
)

// Helper function to create a skiplist with test entries
func createTestSkiplist(paths []string) *skiplistWrapper {
	skiplist := NewSkiplistWrapper(16, MainContext)
	
	for _, path := range paths {
		entry := createMockBinaryEntry(path)
		
		// Create a proper mock index file for testing
		entrySize := int(entry.Size)
		entryBytes := (*[256]byte)(unsafe.Pointer(entry))[:entrySize:entrySize]
		
		mockIndexFile := &mmapIndexFile{
			Data:  entryBytes,
			Size:  entrySize,
			mutex: sync.RWMutex{},
		}
		
		ref := createBinaryEntryRef(entry, mockIndexFile)
		inserted := skiplist.Insert(ref, MainContext)
		if !inserted {
			panic("Failed to insert entry for path: " + path)
		}
	}
	
	// Debug: Check that entries were actually inserted
	actualLength := skiplist.Length()
	if actualLength != len(paths) {
		panic(fmt.Sprintf("Expected %d entries, got %d", len(paths), actualLength))
	}
	
	return skiplist
}

func TestSkiplistIterator(t *testing.T) {
	testPaths := []string{
		"file1.txt",
		"file2.txt", 
		"subdir/file3.txt",
		"subdir/nested/file4.txt",
	}
	
	t.Run("NormalIteration", func(t *testing.T) {
		skiplist := createTestSkiplist(testPaths)
		iter := NewSkiplistIterator(skiplist, "test-skiplist")
		defer iter.Close()
		
		// Check initial state
		if iter.Name() != "test-skiplist" {
			t.Errorf("Expected name 'test-skiplist', got '%s'", iter.Name())
		}
		
		if !iter.HasNext() {
			t.Error("Expected HasNext() to be true initially")
		}
		
		if iter.Length() != len(testPaths) {
			t.Errorf("Expected length %d, got %d", len(testPaths), iter.Length())
		}
		
		// Collect all paths from iteration
		var iteratedPaths []string
		for {
			entry, err := iter.Next()
			if err != nil {
				t.Fatalf("Unexpected error during iteration: %v", err)
			}
			
			if entry == nil {
				break // End of iteration
			}
			
			iteratedPaths = append(iteratedPaths, entry.RelativePath())
		}
		
		// Check that we got all paths in sorted order
		if len(iteratedPaths) != len(testPaths) {
			t.Errorf("Expected %d paths, got %d", len(testPaths), len(iteratedPaths))
		}
		
		// Verify paths are sorted (skiplist should maintain order)
		for i := 1; i < len(iteratedPaths); i++ {
			if strings.Compare(iteratedPaths[i-1], iteratedPaths[i]) >= 0 {
				t.Errorf("Paths not in sorted order: '%s' >= '%s'", 
					iteratedPaths[i-1], iteratedPaths[i])
			}
		}
		
		// Verify we got the expected paths (sorted)
		expectedSorted := make([]string, len(testPaths))
		copy(expectedSorted, testPaths)
		// Note: testPaths should already be sorted for this test
		
		for i, expected := range expectedSorted {
			if i >= len(iteratedPaths) || iteratedPaths[i] != expected {
				t.Errorf("Expected path '%s' at position %d, got '%s'", 
					expected, i, getStringAtIndex(iteratedPaths, i))
			}
		}
		
		// Iterator should be exhausted
		if iter.HasNext() {
			t.Error("Expected HasNext() to be false after exhaustion")
		}
		
		if iter.CurrentPath() != "" {
			t.Errorf("Expected empty current path after exhaustion, got '%s'", iter.CurrentPath())
		}
	})
	
	t.Run("EmptySkiplist", func(t *testing.T) {
		skiplist := createTestSkiplist([]string{})
		iter := NewSkiplistIterator(skiplist, "empty-skiplist")
		defer iter.Close()
		
		if iter.Length() != 0 {
			t.Errorf("Expected length 0 for empty skiplist, got %d", iter.Length())
		}
		
		// Should be immediately exhausted
		entry, err := iter.Next()
		if err != nil {
			t.Fatalf("Unexpected error with empty skiplist: %v", err)
		}
		
		if entry != nil {
			t.Error("Expected nil entry with empty skiplist")
		}
		
		if iter.HasNext() {
			t.Error("Expected HasNext() to be false with empty skiplist")
		}
	})
	
	t.Run("NilSkiplist", func(t *testing.T) {
		iter := NewSkiplistIterator(nil, "nil-skiplist")
		defer iter.Close()
		
		if iter.Length() != 0 {
			t.Errorf("Expected length 0 for nil skiplist, got %d", iter.Length())
		}
		
		// Should be immediately exhausted
		if iter.HasNext() {
			t.Error("Expected HasNext() to be false with nil skiplist")
		}
		
		entry, err := iter.Next()
		if err != nil {
			t.Fatalf("Unexpected error with nil skiplist: %v", err)
		}
		
		if entry != nil {
			t.Error("Expected nil entry with nil skiplist")
		}
	})
	
	t.Run("ClosedIterator", func(t *testing.T) {
		skiplist := createTestSkiplist(testPaths)
		iter := NewSkiplistIterator(skiplist, "closed-skiplist")
		
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
		
		// Should return 0 length when closed
		if iter.Length() != 0 {
			t.Errorf("Expected length 0 for closed iterator, got %d", iter.Length())
		}
		
		// Closing again should be safe
		if err := iter.Close(); err != nil {
			t.Errorf("Unexpected error closing iterator again: %v", err)
		}
	})
	
	t.Run("SingleEntry", func(t *testing.T) {
		skiplist := createTestSkiplist([]string{"single.txt"})
		iter := NewSkiplistIterator(skiplist, "single-entry")
		defer iter.Close()
		
		if iter.Length() != 1 {
			t.Errorf("Expected length 1, got %d", iter.Length())
		}
		
		// Get the single entry
		entry, err := iter.Next()
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		
		if entry == nil {
			t.Fatal("Expected non-nil entry")
		}
		
		if entry.RelativePath() != "single.txt" {
			t.Errorf("Expected path 'single.txt', got '%s'", entry.RelativePath())
		}
		
		if iter.CurrentPath() != "single.txt" {
			t.Errorf("Expected current path 'single.txt', got '%s'", iter.CurrentPath())
		}
		
		// Second call should return nil
		entry2, err := iter.Next()
		if err != nil {
			t.Fatalf("Unexpected error on second call: %v", err)
		}
		
		if entry2 != nil {
			t.Error("Expected nil entry on second call")
		}
		
		if iter.HasNext() {
			t.Error("Expected HasNext() to be false after single entry")
		}
	})
}

// Helper function to safely get string at index
func getStringAtIndex(slice []string, index int) string {
	if index >= 0 && index < len(slice) {
		return slice[index]
	}
	return "<out of bounds>"
}

func TestSkiplistIteratorIntegration(t *testing.T) {
	// Test with actual DirectoryCache skiplists (if we can create them)
	// This tests integration with the real skiplist implementation
	
	t.Run("WithRealSkiplist", func(t *testing.T) {
		// Create a real skiplist using the existing infrastructure
		skiplist := NewSkiplistWrapper(16, MainContext)
		
		// Add some entries in unsorted order to test sorting
		unsortedPaths := []string{
			"zzz.txt",
			"aaa.txt", 
			"mmm.txt",
			"bbb/ccc.txt",
		}
		
		for _, path := range unsortedPaths {
			entry := createMockBinaryEntry(path)
			
			// Create a proper mock index file for testing
			entrySize := int(entry.Size)
			entryBytes := (*[256]byte)(unsafe.Pointer(entry))[:entrySize:entrySize]
			
			mockIndexFile := &mmapIndexFile{
				Data:  entryBytes,
				Size:  entrySize,
				mutex: sync.RWMutex{},
			}
			
			ref := createBinaryEntryRef(entry, mockIndexFile)
			skiplist.Insert(ref, MainContext)
		}
		
		iter := NewSkiplistIterator(skiplist, "integration-test")
		defer iter.Close()
		
		// Collect all paths
		var paths []string
		for {
			entry, err := iter.Next()
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			
			if entry == nil {
				break
			}
			
			paths = append(paths, entry.RelativePath())
		}
		
		// Should have all entries
		if len(paths) != len(unsortedPaths) {
			t.Errorf("Expected %d paths, got %d", len(unsortedPaths), len(paths))
		}
		
		// Should be in sorted order
		expectedOrder := []string{
			"aaa.txt",
			"bbb/ccc.txt", 
			"mmm.txt",
			"zzz.txt",
		}
		
		for i, expected := range expectedOrder {
			if i >= len(paths) || paths[i] != expected {
				t.Errorf("Expected path '%s' at position %d, got '%s'", 
					expected, i, getStringAtIndex(paths, i))
			}
		}
	})
}