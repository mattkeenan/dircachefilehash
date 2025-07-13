package dircachefilehash

import (
	"testing"
	"time"
)

// TestStatusCallbackHashRequests tests that StatusCallback properly requests hashing
// when needsHash() returns true
func TestStatusCallbackHashRequests(t *testing.T) {
	// Create a test directory cache
	dc := &DirectoryCache{}
	
	// Create status callback
	callback := NewStatusCallback("test-status", dc, nil)
	
	t.Run("ModifiedFileRequestsHash", func(t *testing.T) {
		// Create a modified file scenario
		existingEntry := &mockBinaryEntry{
			relPath:   "file.txt",
			size:      1000,
			mtime:     time.Now().Add(-time.Hour), // Old time
			hashValue: [20]byte{1, 2, 3, 4, 5},
		}
		
		modifiedEntry := &mockBinaryEntry{
			relPath:   "file.txt",
			size:      1000,
			mtime:     time.Now(), // New time - file modified
			hashValue: [20]byte{}, // No hash yet
		}
		
		// Process comparison - this should request hash because times differ
		continueProcessing, err := callback.OnComparison(
			ComparisonMatch,
			existingEntry,
			modifiedEntry,
			"file.txt",
			"file.txt",
		)
		
		if err != nil {
			t.Fatalf("OnComparison failed: %v", err)
		}
		if !continueProcessing {
			t.Error("Expected to continue processing")
		}
		
		// Verify hash was requested on the modified entry
		requested, err := modifiedEntry.IsHashRequested()
		if err != nil {
			t.Fatalf("IsHashRequested failed: %v", err)
		}
		if !requested {
			t.Error("Expected hash to be requested for modified file")
		}
		
		// Verify result contains the modified file
		result := callback.GetResult()
		if len(result.Modified) != 1 {
			t.Errorf("Expected 1 modified file, got %d", len(result.Modified))
		}
		if len(result.Modified) > 0 && result.Modified[0] != "file.txt" {
			t.Errorf("Expected modified file 'file.txt', got '%s'", result.Modified[0])
		}
	})
	
	t.Run("NewFileRequestsHash", func(t *testing.T) {
		// Clear callback state
		callback.Clear()
		
		newEntry := &mockBinaryEntry{
			relPath:   "newfile.txt",
			size:      2000,
			mtime:     time.Now(),
			hashValue: [20]byte{}, // No hash yet
		}
		
		// Process new file scenario
		continueProcessing, err := callback.OnComparison(
			ComparisonRightFirst,
			nil,
			newEntry,
			"",
			"newfile.txt",
		)
		
		if err != nil {
			t.Fatalf("OnComparison failed: %v", err)
		}
		if !continueProcessing {
			t.Error("Expected to continue processing")
		}
		
		// Verify hash was requested on the new entry
		requested, err := newEntry.IsHashRequested()
		if err != nil {
			t.Fatalf("IsHashRequested failed: %v", err)
		}
		if !requested {
			t.Error("Expected hash to be requested for new file")
		}
		
		// Verify result contains the added file
		result := callback.GetResult()
		if len(result.Added) != 1 {
			t.Errorf("Expected 1 added file, got %d", len(result.Added))
		}
		if len(result.Added) > 0 && result.Added[0] != "newfile.txt" {
			t.Errorf("Expected added file 'newfile.txt', got '%s'", result.Added[0])
		}
	})
	
	t.Run("UnchangedFileNoHashRequest", func(t *testing.T) {
		// Clear callback state
		callback.Clear()
		
		// Create identical files (no change)
		unchangedEntry1 := &mockBinaryEntry{
			relPath:   "unchanged.txt",
			size:      500,
			mtime:     time.Now(),
			hashValue: [20]byte{1, 2, 3, 4, 5},
		}
		
		unchangedEntry2 := &mockBinaryEntry{
			relPath:   "unchanged.txt",
			size:      500,
			mtime:     unchangedEntry1.mtime, // Same time
			hashValue: [20]byte{1, 2, 3, 4, 5},
		}
		
		// Set all metadata to be identical for needsHash() to return false
		// Copy the time from entry1 to entry2 to ensure they're identical
		unchangedEntry2.mtime = unchangedEntry1.mtime
		
		// Process unchanged file scenario
		continueProcessing, err := callback.OnComparison(
			ComparisonMatch,
			unchangedEntry1,
			unchangedEntry2,
			"unchanged.txt",
			"unchanged.txt",
		)
		
		if err != nil {
			t.Fatalf("OnComparison failed: %v", err)
		}
		if !continueProcessing {
			t.Error("Expected to continue processing")
		}
		
		// Verify hash was NOT requested (file unchanged)
		requested1, err := unchangedEntry1.IsHashRequested()
		if err != nil {
			t.Fatalf("IsHashRequested failed on entry1: %v", err)
		}
		requested2, err := unchangedEntry2.IsHashRequested()
		if err != nil {
			t.Fatalf("IsHashRequested failed on entry2: %v", err)
		}
		
		if requested1 || requested2 {
			t.Error("Expected hash NOT to be requested for unchanged file")
		}
		
		// Verify result has no changes (no modified/added/deleted files)
		result := callback.GetResult()
		if len(result.Modified) > 0 || len(result.Added) > 0 || len(result.Deleted) > 0 {
			t.Errorf("Expected no changes for unchanged file, got Modified=%d, Added=%d, Deleted=%d",
				len(result.Modified), len(result.Added), len(result.Deleted))
		}
	})
	
	t.Run("OnRightOnlyRequestsHash", func(t *testing.T) {
		// Clear callback state
		callback.Clear()
		
		newEntry := &mockBinaryEntry{
			relPath:   "rightonly.txt",
			size:      3000,
			mtime:     time.Now(),
			hashValue: [20]byte{}, // No hash yet
		}
		
		// Process via OnRightOnly (remaining new files)
		continueProcessing, err := callback.OnRightOnly(newEntry, "rightonly.txt")
		
		if err != nil {
			t.Fatalf("OnRightOnly failed: %v", err)
		}
		if !continueProcessing {
			t.Error("Expected to continue processing")
		}
		
		// Verify hash was requested
		requested, err := newEntry.IsHashRequested()
		if err != nil {
			t.Fatalf("IsHashRequested failed: %v", err)
		}
		if !requested {
			t.Error("Expected hash to be requested for right-only file")
		}
		
		// Verify result contains the added file
		result := callback.GetResult()
		if len(result.Added) != 1 {
			t.Errorf("Expected 1 added file, got %d", len(result.Added))
		}
	})
}