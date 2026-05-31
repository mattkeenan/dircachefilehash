package dircachefilehash

import (
	"os"
	"syscall"
	"testing"
	"time"
)

// TestBEScan runs the implementation-neutral test suite for BEScan
func TestBEScan(t *testing.T) {
	suite := &BinaryEntryTestSuite{
		Name:               "BEScan",
		CreateEntry:        createBEScan,
		CleanupEntry:       cleanupBEScan,
		SupportsSetHash:    true,
		SupportsSetDeleted: true,
		IsEphemeral:        true,
	}

	suite.RunAllTests(t)
}

// createBEScan creates a heap-allocated BEScanEntry for testing.
func createBEScan(t *testing.T, testData *TestEntryData) BinaryEntryInterface {
	testData.Size = uint32(BESizeFromPathLen(len(testData.RelativePath)))

	mockInfo := &mockFileInfo{
		name:    "test_file.txt",
		size:    testData.FileSize,
		mode:    os.FileMode(testData.Mode),
		modTime: time.Unix(1234567900, 0),
	}

	mockStat := &syscall.Stat_t{
		Dev:  testData.Dev,
		Ino:  testData.Ino,
		Mode: testData.Mode,
		Uid:  testData.UID,
		Gid:  testData.GID,
		Ctim: syscall.Timespec{Sec: 1234567890, Nsec: 0},
		Mtim: syscall.Timespec{Sec: 1234567900, Nsec: 0},
	}

	scanEntry := NewBEScanEntry(testData.RelativePath, mockInfo, mockStat)

	// Post-process: apply test data that NewBEScanEntry doesn't set.
	// NewBEScanEntry is production code designed for lazy hashing — it only
	// sets metadata from fileInfo/statInfo. Tests need hash, CTimeWall from
	// stat (not modtime), and deleted flag to match CreateTestData().
	entry, _ := scanEntry.getBinaryEntry()
	entry.CTimeWall = testData.CTimeWall
	if testData.HashType != 0 {
		_ = scanEntry.SetHash(testData.Hash[:], testData.HashType)
	}
	if testData.IsDeleted {
		_ = scanEntry.SetDeleted(true)
	}

	return scanEntry
}

// cleanupBEScan releases resources created during testing.
// Heap-allocated entries need no cleanup; this is a no-op kept to satisfy
// the BinaryEntryTestSuite interface.
func cleanupBEScan(t *testing.T, entry BinaryEntryInterface) {}

// TestBEScanSpecific tests BEScan-specific functionality
func TestBEScanSpecific(t *testing.T) {
	t.Run("EphemeralBehavior", testBEScanEphemeralBehavior)
	t.Run("HashWorkerUpdates", testBEScanHashWorkerUpdates)
	t.Run("ConcurrentMremapSafety", testBEScanConcurrentMremapSafety)
	t.Run("InvalidEntryHandling", testBEScanInvalidHandling)
}

// testBEScanEphemeralBehavior tests ephemeral entry behavior
func testBEScanEphemeralBehavior(t *testing.T) {
	helper := &scanTestHelper{}
	entry, cleanup := helper.createTestEntry()
	defer cleanup()

	// Initially should be valid
	if !entry.IsValid() {
		t.Error("New scan entry should be valid")
	}

	// Should be able to access all fields
	if path, err := entry.RelativePath(); err != nil {
		t.Errorf("RelativePath() returned error: %v", err)
	} else if path != "test/file.txt" {
		t.Errorf("RelativePath() = %q, want %q", path, "test/file.txt")
	}

	// Test hash string access
	if hashStr, err := entry.HashString(); err != nil {
		t.Errorf("HashString() returned error: %v", err)
	} else if len(hashStr) != 40 { // 20 bytes * 2 hex chars
		t.Errorf("HashString() length = %d, want 40", len(hashStr))
	}
}

// testBEScanHashWorkerUpdates tests hash worker update functionality
func testBEScanHashWorkerUpdates(t *testing.T) {
	helper := &scanTestHelper{}
	entry, cleanup := helper.createTestEntry()
	defer cleanup()

	// Test hash update (simulating hash worker — 20-byte hash matches SHA1 type)
	newHash := [20]byte{0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11, 0x00, 0xff, 0xee, 0xdd, 0xcc}
	newHashType := HashTypeSHA1

	if err := entry.SetHash(newHash[:], newHashType); err != nil {
		t.Errorf("SetHash() returned error: %v", err)
	}

	// Verify update was successful
	if hash, err := entry.Hash(); err != nil {
		t.Errorf("Hash() after update returned error: %v", err)
	} else if hash != newHash {
		t.Errorf("Hash() after update = %x, want %x", hash, newHash)
	}

	if hashType, err := entry.HashType(); err != nil {
		t.Errorf("HashType() after update returned error: %v", err)
	} else if hashType != newHashType {
		t.Errorf("HashType() after update = %d, want %d", hashType, newHashType)
	}
}

// testBEScanConcurrentMremapSafety tests concurrent access during potential mremap
func testBEScanConcurrentMremapSafety(t *testing.T) {
	helper := &scanTestHelper{}
	entry, cleanup := helper.createTestEntry()
	defer cleanup()

	// This test verifies that our locking prevents races during mremap
	// In practice, this would be very hard to reproduce, but we can at least
	// verify that concurrent access doesn't panic

	const numReaders = 5
	const numOperations = 50

	done := make(chan bool, numReaders)

	// Start concurrent readers
	for range numReaders {
		go func() {
			for range numOperations {
				// Try to access the entry concurrently
				if path, err := entry.RelativePath(); err != nil {
					// Errors are acceptable for ephemeral entries
					t.Logf("Concurrent access error (acceptable): %v", err)
				} else if path != "test/file.txt" {
					t.Errorf("Concurrent RelativePath() = %q, want %q", path, "test/file.txt")
				}

				// Small delay to encourage race conditions
				time.Sleep(time.Microsecond)
			}
			done <- true
		}()
	}

	// Wait for all readers to complete
	for range numReaders {
		<-done
	}
}

// testBEScanInvalidHandling tests handling of invalid entries
func testBEScanInvalidHandling(t *testing.T) {
	// Create an invalid entry with minimal data
	// (BEScanEntry doesn't directly depend on IndexFile like the ref-based constructors)
	invalidEntry := NewBEScanEntry("", nil, nil)

	// Should not be valid
	if invalidEntry.IsValid() {
		t.Error("Entry with nil IndexFile should not be valid")
	}

	// All operations should return ErrEntryInvalidated
	if _, err := invalidEntry.RelativePath(); err != ErrEntryInvalidated {
		t.Errorf("RelativePath() on invalid entry = %v, want %v", err, ErrEntryInvalidated)
	}

	if _, err := invalidEntry.Hash(); err != ErrEntryInvalidated {
		t.Errorf("Hash() on invalid entry = %v, want %v", err, ErrEntryInvalidated)
	}

	if err := invalidEntry.SetHash(make([]byte, 20), 1); err != ErrEntryInvalidated {
		t.Errorf("SetHash() on invalid entry = %v, want %v", err, ErrEntryInvalidated)
	}
}

// scanTestHelper helps create heap-allocated scan entries for testing.
type scanTestHelper struct{}

// createTestEntry creates a heap-allocated BEScanEntry for testing.
// The cleanup function is a no-op kept for caller compatibility.
func (h *scanTestHelper) createTestEntry() (*BEScanEntry, func()) {
	testData := CreateTestData()
	testData.Size = uint32(BESizeFromPathLen(len(testData.RelativePath)))

	mockInfo := &mockFileInfo{
		name:    "test_file.txt",
		size:    testData.FileSize,
		mode:    os.FileMode(testData.Mode),
		modTime: time.Unix(1234567900, 0),
	}

	mockStat := &syscall.Stat_t{
		Dev:  testData.Dev,
		Ino:  testData.Ino,
		Mode: testData.Mode,
		Uid:  testData.UID,
		Gid:  testData.GID,
		Ctim: syscall.Timespec{Sec: 1234567890, Nsec: 0},
		Mtim: syscall.Timespec{Sec: 1234567900, Nsec: 0},
	}

	return NewBEScanEntry(testData.RelativePath, mockInfo, mockStat), func() {}
}

// Benchmark tests for BEScan
func BenchmarkBEScan(b *testing.B) {
	helper := &scanTestHelper{}

	createFn := func() BinaryEntryInterface {
		entry, _ := helper.createTestEntry()
		return entry
	}

	cleanupFn := func(entry BinaryEntryInterface) {}

	suite := &BinaryEntryTestSuite{}

	b.Run("FieldAccess", func(b *testing.B) {
		suite.BenchmarkFieldAccess(b, createFn, cleanupFn)
	})

	b.Run("ConcurrentAccess", func(b *testing.B) {
		suite.BenchmarkConcurrentAccess(b, createFn, cleanupFn)
	})
}
