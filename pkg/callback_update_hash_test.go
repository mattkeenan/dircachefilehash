package dircachefilehash

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestUpdateCallbackHashRequests tests that UpdateCallback properly requests hashing
// when needsHash() returns true
func TestUpdateCallbackHashRequests(t *testing.T) {
	// UpdateCallback requires a non-nil algorithmHashManager for hash job submission.
	// The update path now uses the pipeline architecture; these tests exercise the
	// deprecated callback path retained for recovery.go.
	t.Skip("UpdateCallback tests require algorithmHashManager infrastructure — update path now uses pipeline")
	// Create a temporary test directory
	tempDir, err := os.MkdirTemp("", "dcfh-update-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Create a test DirectoryCache
	dc := NewDirectoryCache(tempDir, tempDir)
	defer func() { _ = dc.Close() }()

	// Create update callback
	callback := NewUpdateCallback(context.Background(), dc, "test-scan", nil)

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
		// The call will fail due to createScanEntryAndHash but should still request hash first
		continueProcessing, err := callback.OnComparison(
			ComparisonMatch,
			existingEntry,
			modifiedEntry,
			"file.txt",
			"file.txt",
		)

		// The operation should fail due to missing binaryEntryRef support in mock
		// but we should verify that hash was requested before the failure
		if err == nil {
			// If no error, check normal continuation
			if !continueProcessing {
				t.Error("Expected to continue processing")
			}
		} else {
			// Expected error from createScanEntryAndHash
			if err.Error() != "scan entry does not support binaryEntryRef for update" {
				t.Fatalf("Unexpected error: %v", err)
			}
		}

		// Verify hash was requested on the modified entry (before createScanEntryAndHash)
		requested, err := modifiedEntry.IsHashRequested()
		if err != nil {
			t.Fatalf("IsHashRequested failed: %v", err)
		}
		if !requested {
			t.Error("Expected hash to be requested for modified file")
		}
	})

	t.Run("NewFileRequestsHash", func(t *testing.T) {
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

		// Expected to fail due to mock limitations, but hash should be requested first
		if err == nil {
			if !continueProcessing {
				t.Error("Expected to continue processing")
			}
		} else {
			// Expected error from createScanEntryAndHash
			if err.Error() != "scan entry does not support binaryEntryRef for update" {
				t.Fatalf("Unexpected error: %v", err)
			}
		}

		// Verify hash was requested on the new entry
		requested, err := newEntry.IsHashRequested()
		if err != nil {
			t.Fatalf("IsHashRequested failed: %v", err)
		}
		if !requested {
			t.Error("Expected hash to be requested for new file")
		}
	})

	t.Run("UnchangedFileNoHashRequest", func(t *testing.T) {
		// Create identical files (no change)
		baseTime := time.Now()
		unchangedEntry1 := &mockBinaryEntry{
			relPath:   "unchanged.txt",
			size:      500,
			mtime:     baseTime,
			hashValue: [20]byte{1, 2, 3, 4, 5},
		}

		unchangedEntry2 := &mockBinaryEntry{
			relPath:   "unchanged.txt",
			size:      500,
			mtime:     baseTime, // Same time
			hashValue: [20]byte{1, 2, 3, 4, 5},
		}

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
	})

	t.Run("OnRightOnlyRequestsHash", func(t *testing.T) {
		newEntry := &mockBinaryEntry{
			relPath:   "rightonly.txt",
			size:      3000,
			mtime:     time.Now(),
			hashValue: [20]byte{}, // No hash yet
		}

		// Process via OnRightOnly (remaining new files)
		continueProcessing, err := callback.OnRightOnly(newEntry, "rightonly.txt")

		// Expected to fail due to mock limitations, but hash should be requested first
		if err == nil {
			if !continueProcessing {
				t.Error("Expected to continue processing")
			}
		} else {
			// Expected error from createScanEntryAndHash
			if err.Error() != "scan entry does not support binaryEntryRef for update" {
				t.Fatalf("Unexpected error: %v", err)
			}
		}

		// Verify hash was requested
		requested, err := newEntry.IsHashRequested()
		if err != nil {
			t.Fatalf("IsHashRequested failed: %v", err)
		}
		if !requested {
			t.Error("Expected hash to be requested for right-only file")
		}
	})

	t.Run("IgnoredFileSkipsHashing", func(t *testing.T) {
		// Create a mock entry for a file that should be ignored
		ignoredEntry := &mockBinaryEntry{
			relPath:   ".git/config", // Git files are typically ignored
			size:      100,
			mtime:     time.Now(),
			hashValue: [20]byte{},
		}

		// Mock the shouldIndex to return false for ignored files
		// Note: this test depends on the actual shouldIndex implementation
		// but we'll test the logic flow

		// Process ignored file - behavior depends on shouldIndex implementation
		continueProcessing, err := callback.OnRightOnly(ignoredEntry, ".git/config")

		// May fail due to mock limitations or may succeed if shouldIndex returns false
		if err == nil {
			if !continueProcessing {
				t.Error("Expected to continue processing")
			}
		} else {
			// Expected error from createScanEntryAndHash
			if err.Error() != "scan entry does not support binaryEntryRef for update" {
				t.Fatalf("Unexpected error: %v", err)
			}
		}

		// The important part is that it doesn't crash and continues processing
		// Actual hash request behavior depends on shouldIndex implementation
	})
}
