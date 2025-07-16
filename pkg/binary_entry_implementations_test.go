package dircachefilehash

import (
	"testing"
)

// TestBinaryEntryImplementationsHashCoordination tests that all BinaryEntry implementations
// properly inherit the hash coordination methods from BinaryEntryBase
func TestBinaryEntryImplementationsHashCoordination(t *testing.T) {
	
	t.Run("BEScanEntry", func(t *testing.T) {
		// Create a mock binaryEntryRef for testing
		// Note: This is a minimal test - we're just checking that the methods exist and work
		mockRef := binaryEntryRef{
			IndexFile: nil,  // Will fail IsValid() which is fine for this test
			Offset:    0,
		}
		
		scanEntry := NewBEScanEntry(mockRef)
		
		// Test hash coordination methods (they should work even if the entry is invalid)
		testHashCoordinationMethods(t, scanEntry, "BEScanEntry")
	})
	
	t.Run("BESkiplistEntry", func(t *testing.T) {
		// Create a mock binaryEntryRef for testing
		mockRef := binaryEntryRef{
			IndexFile: nil,  // Will fail IsValid() which is fine for this test
			Offset:    0,
		}
		
		skipEntry := NewBESkiplistEntry(mockRef, nil)
		
		// Test hash coordination methods
		testHashCoordinationMethods(t, skipEntry, "BESkiplistEntry")
	})
	
	t.Run("BEIndexFileIOEntry", func(t *testing.T) {
		// Create a BEIndexFileIOEntry with fake file path
		ioEntry := NewBEIndexFileIOEntry("/fake/path", 0, 100, "test")
		
		// Test hash coordination methods (they should work even if the file doesn't exist)
		testHashCoordinationMethods(t, ioEntry, "BEIndexFileIOEntry")
	})
	
	t.Run("BEIndexFileMmapEntry", func(t *testing.T) {
		// Create a mock binaryEntryRef for testing
		mockRef := binaryEntryRef{
			IndexFile: nil,  // Will fail IsValid() which is fine for this test
			Offset:    0,
		}
		
		mmapEntry := NewBEIndexFileMmapEntry(mockRef, "test")
		
		// Test hash coordination methods
		testHashCoordinationMethods(t, mmapEntry, "BEIndexFileMmapEntry")
	})
}

// testHashCoordinationMethods tests the hash coordination methods on any BinaryEntryInterface
func testHashCoordinationMethods(t *testing.T, entry BinaryEntryInterface, entryType string) {
	// Initial state
	requested, err := entry.IsHashRequested()
	if err != nil {
		t.Fatalf("%s IsHashRequested failed: %v", entryType, err)
	}
	if requested {
		t.Errorf("%s: Expected hash not to be requested initially", entryType)
	}
	
	completed, err := entry.IsHashCompleted()
	if err != nil {
		t.Fatalf("%s IsHashCompleted failed: %v", entryType, err)
	}
	if completed {
		t.Errorf("%s: Expected hash not to be completed initially", entryType)
	}
	
	jobID := entry.GetHashJobID()
	if jobID != 0 {
		t.Errorf("%s: Expected job ID to be 0 initially, got %d", entryType, jobID)
	}
	
	// Request hash
	err = entry.RequestHash()
	if err != nil {
		t.Fatalf("%s RequestHash failed: %v", entryType, err)
	}
	
	// Check requested state
	requested, err = entry.IsHashRequested()
	if err != nil {
		t.Fatalf("%s IsHashRequested failed after request: %v", entryType, err)
	}
	if !requested {
		t.Errorf("%s: Expected hash to be requested after RequestHash", entryType)
	}
	
	// Set job ID
	testJobID := uint64(99999)
	entry.SetHashJobID(testJobID)
	
	retrievedJobID := entry.GetHashJobID()
	if retrievedJobID != testJobID {
		t.Errorf("%s: Expected job ID %d, got %d", entryType, testJobID, retrievedJobID)
	}
	
	// Mark completed
	entry.MarkHashCompleted()
	
	completed, err = entry.IsHashCompleted()
	if err != nil {
		t.Fatalf("%s IsHashCompleted failed after marking complete: %v", entryType, err)
	}
	if !completed {
		t.Errorf("%s: Expected hash to be completed after MarkHashCompleted", entryType)
	}
	
	// Verify request is still set (shouldn't be cleared by marking complete)
	requested, err = entry.IsHashRequested()
	if err != nil {
		t.Fatalf("%s IsHashRequested failed after completion: %v", entryType, err)
	}
	if !requested {
		t.Errorf("%s: Expected hash to still be requested after completion", entryType)
	}
}