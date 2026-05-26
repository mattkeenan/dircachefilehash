package dircachefilehash

import (
	"testing"
)

// TestBinaryEntryHashCoordination tests the hash coordination methods across all implementations
func TestBinaryEntryHashCoordination(t *testing.T) {
	// Test using the mock binary entry from callback_update_test.go
	mockEntry := &mockBinaryEntry{
		relPath:   "test/file.txt",
		size:      1024,
		deleted:   false,
		hashValue: [20]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20},
	}

	t.Run("InitialState", func(t *testing.T) {
		// Initially, hash should not be requested or completed
		requested, err := mockEntry.IsHashRequested()
		if err != nil {
			t.Fatalf("IsHashRequested failed: %v", err)
		}
		if requested {
			t.Error("Expected hash not to be requested initially")
		}

		completed, err := mockEntry.IsHashCompleted()
		if err != nil {
			t.Fatalf("IsHashCompleted failed: %v", err)
		}
		if completed {
			t.Error("Expected hash not to be completed initially")
		}

		jobID := mockEntry.GetHashJobID()
		if jobID != 0 {
			t.Errorf("Expected job ID to be 0 initially, got %d", jobID)
		}
	})

	t.Run("RequestHash", func(t *testing.T) {
		// Request hashing
		err := mockEntry.RequestHash()
		if err != nil {
			t.Fatalf("RequestHash failed: %v", err)
		}

		// Should now be requested
		requested, err := mockEntry.IsHashRequested()
		if err != nil {
			t.Fatalf("IsHashRequested failed: %v", err)
		}
		if !requested {
			t.Error("Expected hash to be requested after RequestHash")
		}

		// Should still not be completed
		completed, err := mockEntry.IsHashCompleted()
		if err != nil {
			t.Fatalf("IsHashCompleted failed: %v", err)
		}
		if completed {
			t.Error("Expected hash not to be completed yet")
		}
	})

	t.Run("SetJobID", func(t *testing.T) {
		// Set a job ID
		testJobID := uint64(12345)
		mockEntry.SetHashJobID(testJobID)

		jobID := mockEntry.GetHashJobID()
		if jobID != testJobID {
			t.Errorf("Expected job ID %d, got %d", testJobID, jobID)
		}
	})

	t.Run("MarkCompleted", func(t *testing.T) {
		// Mark as completed
		mockEntry.MarkHashCompleted()

		// Should now be completed
		completed, err := mockEntry.IsHashCompleted()
		if err != nil {
			t.Fatalf("IsHashCompleted failed: %v", err)
		}
		if !completed {
			t.Error("Expected hash to be completed after MarkHashCompleted")
		}

		// Should still be requested
		requested, err := mockEntry.IsHashRequested()
		if err != nil {
			t.Fatalf("IsHashRequested failed: %v", err)
		}
		if !requested {
			t.Error("Expected hash to still be requested after completion")
		}
	})

	t.Run("DuplicateRequest", func(t *testing.T) {
		// Create a fresh mock entry
		freshMock := &mockBinaryEntry{
			relPath: "test/file2.txt",
			size:    2048,
		}

		// Request hash twice
		err1 := freshMock.RequestHash()
		err2 := freshMock.RequestHash()

		if err1 != nil {
			t.Fatalf("First RequestHash failed: %v", err1)
		}
		if err2 != nil {
			t.Fatalf("Second RequestHash failed: %v", err2)
		}

		// Should be requested
		requested, err := freshMock.IsHashRequested()
		if err != nil {
			t.Fatalf("IsHashRequested failed: %v", err)
		}
		if !requested {
			t.Error("Expected hash to be requested")
		}
	})
}

// TestBinaryEntryBaseHashCoordination tests the default implementation in BinaryEntryBase
func TestBinaryEntryBaseHashCoordination(t *testing.T) {
	// Create a BinaryEntryBase directly to test the default implementations
	base := NewBinaryEntryBase(BESkiplist)

	t.Run("InitialState", func(t *testing.T) {
		requested, err := base.IsHashRequested()
		if err != nil {
			t.Fatalf("IsHashRequested failed: %v", err)
		}
		if requested {
			t.Error("Expected hash not to be requested initially")
		}

		completed, err := base.IsHashCompleted()
		if err != nil {
			t.Fatalf("IsHashCompleted failed: %v", err)
		}
		if completed {
			t.Error("Expected hash not to be completed initially")
		}

		jobID := base.GetHashJobID()
		if jobID != 0 {
			t.Errorf("Expected job ID to be 0 initially, got %d", jobID)
		}
	})

	t.Run("RequestHashFlow", func(t *testing.T) {
		// Request hash
		err := base.RequestHash()
		if err != nil {
			t.Fatalf("RequestHash failed: %v", err)
		}

		// Check state
		requested, err := base.IsHashRequested()
		if err != nil {
			t.Fatalf("IsHashRequested failed: %v", err)
		}
		if !requested {
			t.Error("Expected hash to be requested")
		}

		// Set job ID
		testJobID := uint64(54321)
		base.SetHashJobID(testJobID)

		retrievedJobID := base.GetHashJobID()
		if retrievedJobID != testJobID {
			t.Errorf("Expected job ID %d, got %d", testJobID, retrievedJobID)
		}

		// Mark completed
		base.MarkHashCompleted()

		completed, err := base.IsHashCompleted()
		if err != nil {
			t.Fatalf("IsHashCompleted failed: %v", err)
		}
		if !completed {
			t.Error("Expected hash to be completed")
		}
	})
}
