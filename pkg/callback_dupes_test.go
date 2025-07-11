package dircachefilehash

import (
	"encoding/hex"
	"testing"
	"unsafe"
)

func TestDupesCallback(t *testing.T) {
	t.Run("BasicDuplicateDetection", func(t *testing.T) {
		callback := NewDupesCallback("test-dupes")
		
		// Create test entries with same hash (simulating duplicates)
		// Use realistic SHA1-like hex strings
		entry1 := createTestBinaryEntry("file1.txt", "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2")
		entry2 := createTestBinaryEntry("file2.txt", "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2") // Same hash
		entry3 := createTestBinaryEntry("file3.txt", "f1e2d3c4b5a6f1e2d3c4b5a6f1e2d3c4b5a6f1e2") // Different hash
		
		// Simulate unified algorithm calls
		err := callback.OnStart("left", "right")
		if err != nil {
			t.Fatalf("OnStart failed: %v", err)
		}
		
		// Add entries through comparison results
		_, err = callback.OnComparison(ComparisonRightFirst, nil, entry1, "", "file1.txt")
		if err != nil {
			t.Fatalf("OnComparison failed for entry1: %v", err)
		}
		
		_, err = callback.OnComparison(ComparisonRightFirst, nil, entry2, "", "file2.txt")
		if err != nil {
			t.Fatalf("OnComparison failed for entry2: %v", err)
		}
		
		_, err = callback.OnComparison(ComparisonRightFirst, nil, entry3, "", "file3.txt")
		if err != nil {
			t.Fatalf("OnComparison failed for entry3: %v", err)
		}
		
		// Complete processing
		err = callback.OnComplete(nil)
		if err != nil {
			t.Fatalf("OnComplete failed: %v", err)
		}
		
		// Verify results
		results := callback.GetResults()
		if len(results) != 1 {
			t.Errorf("Expected 1 duplicate group, got %d", len(results))
		}
		
		if len(results) > 0 {
			group := results[0]
			expectedHash := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
			if group.Hash != expectedHash {
				t.Errorf("Expected hash '%s', got '%s'", expectedHash, group.Hash)
			}
			if group.Count != 2 {
				t.Errorf("Expected count 2, got %d", group.Count)
			}
			if len(group.Files) != 2 {
				t.Errorf("Expected 2 files, got %d", len(group.Files))
			}
			
			// Verify file names are present
			foundFile1, foundFile2 := false, false
			for _, file := range group.Files {
				if file == "file1.txt" {
					foundFile1 = true
				} else if file == "file2.txt" {
					foundFile2 = true
				}
			}
			if !foundFile1 || !foundFile2 {
				t.Errorf("Expected to find both file1.txt and file2.txt in results: %v", group.Files)
			}
		}
	})
	
	t.Run("NoDuplicates", func(t *testing.T) {
		callback := NewDupesCallback("no-dupes")
		
		// Create test entries with different hashes
		entry1 := createTestBinaryEntry("file1.txt", "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2")
		entry2 := createTestBinaryEntry("file2.txt", "f1e2d3c4b5a6f1e2d3c4b5a6f1e2d3c4b5a6f1e2")
		entry3 := createTestBinaryEntry("file3.txt", "1a2b3c4d5e6f1a2b3c4d5e6f1a2b3c4d5e6f1a2b")
		
		err := callback.OnStart("left", "right")
		if err != nil {
			t.Fatalf("OnStart failed: %v", err)
		}
		
		// Add entries
		_, err = callback.OnComparison(ComparisonRightFirst, nil, entry1, "", "file1.txt")
		if err != nil {
			t.Fatalf("OnComparison failed: %v", err)
		}
		
		_, err = callback.OnComparison(ComparisonRightFirst, nil, entry2, "", "file2.txt")
		if err != nil {
			t.Fatalf("OnComparison failed: %v", err)
		}
		
		_, err = callback.OnComparison(ComparisonRightFirst, nil, entry3, "", "file3.txt")
		if err != nil {
			t.Fatalf("OnComparison failed: %v", err)
		}
		
		err = callback.OnComplete(nil)
		if err != nil {
			t.Fatalf("OnComplete failed: %v", err)
		}
		
		// Should have no duplicate groups
		results := callback.GetResults()
		if len(results) != 0 {
			t.Errorf("Expected 0 duplicate groups with different hashes, got %d", len(results))
		}
	})
	
	t.Run("SkipDeletedEntries", func(t *testing.T) {
		callback := NewDupesCallback("skip-deleted")
		
		// Create test entries, including deleted ones
		entry1 := createTestBinaryEntry("file1.txt", "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2")
		entry2 := createTestBinaryEntry("file2.txt", "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2") // Same hash
		entry3 := createTestBinaryEntry("deleted.txt", "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2") // Same hash but deleted
		entry3.SetDeleted() // Mark as deleted
		
		err := callback.OnStart("left", "right")
		if err != nil {
			t.Fatalf("OnStart failed: %v", err)
		}
		
		// Add entries
		_, err = callback.OnComparison(ComparisonRightFirst, nil, entry1, "", "file1.txt")
		if err != nil {
			t.Fatalf("OnComparison failed: %v", err)
		}
		
		_, err = callback.OnComparison(ComparisonRightFirst, nil, entry2, "", "file2.txt")
		if err != nil {
			t.Fatalf("OnComparison failed: %v", err)
		}
		
		_, err = callback.OnComparison(ComparisonRightFirst, nil, entry3, "", "deleted.txt")
		if err != nil {
			t.Fatalf("OnComparison failed: %v", err)
		}
		
		err = callback.OnComplete(nil)
		if err != nil {
			t.Fatalf("OnComplete failed: %v", err)
		}
		
		// Should still have duplicates, but deleted file should be excluded
		results := callback.GetResults()
		if len(results) != 1 {
			t.Errorf("Expected 1 duplicate group, got %d", len(results))
		}
		
		if len(results) > 0 {
			group := results[0]
			if group.Count != 2 {
				t.Errorf("Expected count 2 (excluding deleted), got %d", group.Count)
			}
			
			// Should not contain deleted.txt
			for _, file := range group.Files {
				if file == "deleted.txt" {
					t.Errorf("Deleted file should not appear in duplicates: %v", group.Files)
				}
			}
		}
	})
	
	t.Run("ComparisonMatch", func(t *testing.T) {
		callback := NewDupesCallback("match-test")
		
		// Create entries for match scenario (left = old state, right = current state)
		leftEntry := createTestBinaryEntry("file1.txt", "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2")
		rightEntry := createTestBinaryEntry("file1.txt", "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2")
		
		err := callback.OnStart("left", "right")
		if err != nil {
			t.Fatalf("OnStart failed: %v", err)
		}
		
		// On match, should use right entry (current state)
		_, err = callback.OnComparison(ComparisonMatch, leftEntry, rightEntry, "file1.txt", "file1.txt")
		if err != nil {
			t.Fatalf("OnComparison failed: %v", err)
		}
		
		err = callback.OnComplete(nil)
		if err != nil {
			t.Fatalf("OnComplete failed: %v", err)
		}
		
		// Verify stats - should have 1 hash with 1 entry (no duplicates)
		totalHashes, totalEntries, duplicateHashes := callback.GetHashMapStats()
		if totalHashes != 1 {
			t.Errorf("Expected 1 hash, got %d", totalHashes)
		}
		if totalEntries != 1 {
			t.Errorf("Expected 1 entry, got %d", totalEntries)
		}
		if duplicateHashes != 0 {
			t.Errorf("Expected 0 duplicate hashes, got %d", duplicateHashes)
		}
	})
	
	t.Run("LeftOnlyIgnored", func(t *testing.T) {
		callback := NewDupesCallback("left-only")
		
		// Create left-only entry (deleted file)
		leftEntry := createTestBinaryEntry("deleted.txt", "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2")
		
		err := callback.OnStart("left", "right")
		if err != nil {
			t.Fatalf("OnStart failed: %v", err)
		}
		
		// Left-only entries should be ignored (they represent deleted files)
		_, err = callback.OnComparison(ComparisonLeftFirst, leftEntry, nil, "deleted.txt", "")
		if err != nil {
			t.Fatalf("OnComparison failed: %v", err)
		}
		
		// Also test OnLeftOnly directly
		_, err = callback.OnLeftOnly(leftEntry, "deleted.txt")
		if err != nil {
			t.Fatalf("OnLeftOnly failed: %v", err)
		}
		
		err = callback.OnComplete(nil)
		if err != nil {
			t.Fatalf("OnComplete failed: %v", err)
		}
		
		// Should have no entries in hash map
		totalHashes, totalEntries, duplicateHashes := callback.GetHashMapStats()
		if totalHashes != 0 {
			t.Errorf("Expected 0 hashes, got %d", totalHashes)
		}
		if totalEntries != 0 {
			t.Errorf("Expected 0 entries, got %d", totalEntries)
		}
		if duplicateHashes != 0 {
			t.Errorf("Expected 0 duplicate hashes, got %d", duplicateHashes)
		}
	})
	
	t.Run("OnRightOnly", func(t *testing.T) {
		callback := NewDupesCallback("right-only")
		
		// Create right-only entries (new files)
		entry1 := createTestBinaryEntry("new1.txt", "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2")
		entry2 := createTestBinaryEntry("new2.txt", "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2") // Same hash
		
		err := callback.OnStart("left", "right")
		if err != nil {
			t.Fatalf("OnStart failed: %v", err)
		}
		
		// Test OnRightOnly directly
		_, err = callback.OnRightOnly(entry1, "new1.txt")
		if err != nil {
			t.Fatalf("OnRightOnly failed for entry1: %v", err)
		}
		
		_, err = callback.OnRightOnly(entry2, "new2.txt")
		if err != nil {
			t.Fatalf("OnRightOnly failed for entry2: %v", err)
		}
		
		err = callback.OnComplete(nil)
		if err != nil {
			t.Fatalf("OnComplete failed: %v", err)
		}
		
		// Should detect duplicates
		results := callback.GetResults()
		if len(results) != 1 {
			t.Errorf("Expected 1 duplicate group, got %d", len(results))
		}
		
		if len(results) > 0 {
			group := results[0]
			if group.Count != 2 {
				t.Errorf("Expected count 2, got %d", group.Count)
			}
		}
	})
	
	t.Run("MultipleDuplicateGroups", func(t *testing.T) {
		callback := NewDupesCallback("multiple-groups")
		
		// Create multiple groups of duplicates
		// Group 1: hash "aaaa..."
		hash1 := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		entry1a := createTestBinaryEntry("group1a.txt", hash1)
		entry1b := createTestBinaryEntry("group1b.txt", hash1)
		entry1c := createTestBinaryEntry("group1c.txt", hash1)
		
		// Group 2: hash "bbbb..."
		hash2 := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		entry2a := createTestBinaryEntry("group2a.txt", hash2)
		entry2b := createTestBinaryEntry("group2b.txt", hash2)
		
		// Unique file
		hash3 := "cccccccccccccccccccccccccccccccccccccccc"
		entry3 := createTestBinaryEntry("unique.txt", hash3)
		
		err := callback.OnStart("left", "right")
		if err != nil {
			t.Fatalf("OnStart failed: %v", err)
		}
		
		// Add all entries
		entries := []*binaryEntry{entry1a, entry1b, entry1c, entry2a, entry2b, entry3}
		for _, entry := range entries {
			_, err = callback.OnComparison(ComparisonRightFirst, nil, entry, "", entry.RelativePath())
			if err != nil {
				t.Fatalf("OnComparison failed for entry %s: %v", entry.RelativePath(), err)
			}
		}
		
		err = callback.OnComplete(nil)
		if err != nil {
			t.Fatalf("OnComplete failed: %v", err)
		}
		
		// Should have 2 duplicate groups
		results := callback.GetResults()
		if len(results) != 2 {
			t.Errorf("Expected 2 duplicate groups, got %d", len(results))
		}
		
		// Verify group sizes
		groupSizes := make(map[string]int)
		for _, group := range results {
			groupSizes[group.Hash] = group.Count
		}
		
		if groupSizes[hash1] != 3 {
			t.Errorf("Expected group '%s' to have 3 files, got %d", hash1, groupSizes[hash1])
		}
		if groupSizes[hash2] != 2 {
			t.Errorf("Expected group '%s' to have 2 files, got %d", hash2, groupSizes[hash2])
		}
		
		// Unique file should not appear in results
		if groupSizes[hash3] != 0 {
			t.Errorf("Unique file with hash '%s' should not appear in results", hash3)
		}
	})
	
	t.Run("CallbackName", func(t *testing.T) {
		callback := NewDupesCallback("test-name")
		if callback.Name() != "test-name" {
			t.Errorf("Expected name 'test-name', got '%s'", callback.Name())
		}
	})
	
	t.Run("Clear", func(t *testing.T) {
		callback := NewDupesCallback("clear-test")
		
		// Add some data
		entry := createTestBinaryEntry("file.txt", "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2")
		_, err := callback.OnComparison(ComparisonRightFirst, nil, entry, "", "file.txt")
		if err != nil {
			t.Fatalf("OnComparison failed: %v", err)
		}
		
		// Verify data exists
		totalHashes, _, _ := callback.GetHashMapStats()
		if totalHashes == 0 {
			t.Error("Expected hash map to have data before clear")
		}
		
		// Clear and verify
		callback.Clear()
		totalHashes, totalEntries, duplicateHashes := callback.GetHashMapStats()
		if totalHashes != 0 || totalEntries != 0 || duplicateHashes != 0 {
			t.Errorf("Expected empty hash map after clear, got hashes=%d, entries=%d, duplicates=%d",
				totalHashes, totalEntries, duplicateHashes)
		}
		
		results := callback.GetResults()
		if len(results) != 0 {
			t.Errorf("Expected empty results after clear, got %d results", len(results))
		}
	})
	
	t.Run("ConcurrentAccess", func(t *testing.T) {
		// This test verifies that concurrent access is safe
		// In practice, the unified algorithm is single-threaded, but the mutex protects against races
		callback := NewDupesCallback("concurrent-test")
		
		err := callback.OnStart("left", "right")
		if err != nil {
			t.Fatalf("OnStart failed: %v", err)
		}
		
		// Simulate concurrent calls (though in practice this won't happen)
		entry1 := createTestBinaryEntry("file1.txt", "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2")
		entry2 := createTestBinaryEntry("file2.txt", "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2")
		
		done := make(chan bool, 2)
		
		go func() {
			_, err := callback.OnComparison(ComparisonRightFirst, nil, entry1, "", "file1.txt")
			if err != nil {
				t.Errorf("Concurrent OnComparison failed: %v", err)
			}
			done <- true
		}()
		
		go func() {
			_, err := callback.OnComparison(ComparisonRightFirst, nil, entry2, "", "file2.txt")
			if err != nil {
				t.Errorf("Concurrent OnComparison failed: %v", err)
			}
			done <- true
		}()
		
		// Wait for both goroutines
		<-done
		<-done
		
		err = callback.OnComplete(nil)
		if err != nil {
			t.Fatalf("OnComplete failed: %v", err)
		}
		
		// Verify results
		results := callback.GetResults()
		if len(results) != 1 {
			t.Errorf("Expected 1 duplicate group after concurrent access, got %d", len(results))
		}
	})
}

func TestDupesCallbackIntegration(t *testing.T) {
	t.Run("WithUnifiedAlgorithm", func(t *testing.T) {
		// Create mock iterators with duplicate files
		leftIter := newMockIterator("left-iter", []*binaryEntry{
			createTestBinaryEntry("old1.txt", "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"),
			createTestBinaryEntry("old2.txt", "f1e2d3c4b5a6f1e2d3c4b5a6f1e2d3c4b5a6f1e2"),
		})
		
		rightIter := newMockIterator("right-iter", []*binaryEntry{
			createTestBinaryEntry("new1.txt", "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"), // Same hash as old1.txt
			createTestBinaryEntry("new2.txt", "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"), // Same hash as old1.txt & new1.txt
			createTestBinaryEntry("new3.txt", "1a2b3c4d5e6f1a2b3c4d5e6f1a2b3c4d5e6f1a2b"), // Different hash
		})
		
		// Create DupesCallback
		dupesCallback := NewDupesCallback("integration-test")
		
		// Run unified algorithm
		err := hwangLinUnified(leftIter, rightIter, dupesCallback)
		if err != nil {
			t.Fatalf("hwangLinUnified failed: %v", err)
		}
		
		// Verify results
		results := dupesCallback.GetResults()
		if len(results) != 1 {
			t.Errorf("Expected 1 duplicate group, got %d", len(results))
		}
		
		if len(results) > 0 {
			group := results[0]
			expectedHash := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
			if group.Hash != expectedHash {
				t.Errorf("Expected hash '%s', got '%s'", expectedHash, group.Hash)
			}
			
			// Should have 2 files (new1.txt, new2.txt) - old1.txt is left-only (deleted)
			if group.Count != 2 {
				t.Errorf("Expected 2 files in duplicate group, got %d", group.Count)
			}
			
			// Verify file names
			expectedFiles := map[string]bool{"new1.txt": true, "new2.txt": true}
			for _, file := range group.Files {
				if !expectedFiles[file] {
					t.Errorf("Unexpected file in duplicate group: %s", file)
				}
				delete(expectedFiles, file)
			}
			
			if len(expectedFiles) > 0 {
				t.Errorf("Missing files in duplicate group: %v", expectedFiles)
			}
		}
		
		// Verify statistics
		totalHashes, totalEntries, duplicateHashes := dupesCallback.GetHashMapStats()
		if totalHashes != 2 {
			t.Errorf("Expected 2 total hashes, got %d", totalHashes)
		}
		if totalEntries != 3 {
			t.Errorf("Expected 3 total entries, got %d", totalEntries)
		}
		if duplicateHashes != 1 {
			t.Errorf("Expected 1 duplicate hash, got %d", duplicateHashes)
		}
	})
}

// Helper function to create test binary entries with specific paths and hashes
func createTestBinaryEntry(relativePath, hashStr string) *binaryEntry {
	// Create a minimal test entry with the specified path and hash
	pathBytes := []byte(relativePath)
	
	// Calculate entry size with 8-byte alignment
	baseSize := int(unsafe.Sizeof(binaryEntry{}))
	totalSize := baseSize + len(pathBytes) + 1 // +1 for null terminator
	padding := (8 - (totalSize % 8)) % 8
	entrySize := totalSize + padding
	
	// Allocate memory for the entry
	data := make([]byte, entrySize)
	entry := (*binaryEntry)(unsafe.Pointer(&data[0]))
	
	// Set basic fields
	entry.Size = uint32(entrySize)
	entry.HashType = uint16(HashTypeSHA1)
	entry.FileSize = 100 // Arbitrary test file size
	entry.EntryFlags = 0 // Not deleted
	
	// Set hash - decode hex string to binary
	for i := range entry.Hash {
		entry.Hash[i] = 0 // Clear all bytes first
	}
	
	// If hashStr looks like a hex string, decode it
	if len(hashStr) > 0 {
		if len(hashStr)%2 == 0 && len(hashStr) <= 128 { // Valid hex length, max 64 bytes = 128 hex chars
			hashBytes, err := hex.DecodeString(hashStr)
			if err == nil && len(hashBytes) <= 64 {
				// Successfully decoded hex string
				copy(entry.Hash[:], hashBytes)
			} else {
				// Fallback: create pattern from string
				for i := 0; i < 20 && i < len(hashStr); i++ {
					entry.Hash[i] = hashStr[i%len(hashStr)]
				}
			}
		} else {
			// Create pattern from string for non-hex input
			for i := 0; i < 20 && i < len(hashStr); i++ {
				entry.Hash[i] = hashStr[i%len(hashStr)]
			}
		}
	}
	
	// Copy path after the struct
	pathOffset := baseSize
	copy(data[pathOffset:], pathBytes)
	data[pathOffset+len(pathBytes)] = 0 // null terminator
	
	return entry
}