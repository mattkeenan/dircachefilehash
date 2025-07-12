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

// testCleanupData stores cleanup information for test entries
var testCleanupData = make(map[BinaryEntryInterface]*scanTestCleanupInfo)

type scanTestCleanupInfo struct {
	testDir string
	dc      *DirectoryCache
}

// createBEScan creates a BEScan for testing
// This sets up a temporary scan index and adds a test entry
func createBEScan(t *testing.T, testData *TestEntryData) BinaryEntryInterface {
	// Create temporary directory for test
	testDir, err := os.MkdirTemp("", "dcfh-scan-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	
	// Create DirectoryCache for the test
	dc := NewDirectoryCache(testDir, testDir)
	
	// Update the expected size to match what AppendEntryToScanIndex actually creates
	testData.Size = uint32(BESizeFromPathLen(len(testData.RelativePath)))
	
	// Generate scan file name
	scanFileName := dc.generateScanFileName()
	
	// Initialize scan index
	if err := dc.initialiseScanIndex(scanFileName); err != nil {
		os.RemoveAll(testDir)
		t.Fatalf("Failed to initialize scan index: %v", err)
	}
	
	// Create mock file info and stat for the test entry
	mockInfo := &mockFileInfo{
		name:    "test_file.txt",
		size:    int64(testData.FileSize),
		mode:    os.FileMode(testData.Mode),
		modTime: time.Unix(1234567900, 0),
	}
	
	mockStat := &syscall.Stat_t{
		Dev:   uint64(testData.Dev),
		Ino:   uint64(testData.Ino),
		Mode:  testData.Mode,
		Uid:   testData.UID,
		Gid:   testData.GID,
		Ctim:  syscall.Timespec{Sec: 1234567890, Nsec: 0},
		Mtim:  syscall.Timespec{Sec: 1234567900, Nsec: 0},
	}
	
	// Append entry to scan index
	_, err = dc.AppendEntryToScanIndex(
		scanFileName,
		testData.RelativePath,
		testData.Hash[:],
		testData.HashType,
		mockInfo,
		mockStat,
		testData.IsDeleted,
	)
	if err != nil {
		// Cleanup on error
		os.RemoveAll(testDir)
		t.Fatalf("Failed to create scan entry: %v", err)
	}
	
	// Get the scan index file to create binaryEntryRef
	if dc.currentScan == nil {
		os.RemoveAll(testDir)
		t.Fatalf("No current scan index after AppendEntryToScanIndex")
	}
	
	// Calculate offset of the entry in the scan index
	// The binaryEntryRef.Offset is relative to the start of entries section (after header)
	// The first entry has offset 0 relative to entries section
	entryOffset := 0
	
	// Create binaryEntryRef pointing to our entry
	entryRef := binaryEntryRef{
		Offset:    entryOffset,
		IndexFile: dc.currentScan,
	}
	
	// Create and return BEScanEntry
	scanEntry := NewBEScanEntry(entryRef)
	
	// Store cleanup info in global map
	testCleanupData[scanEntry] = &scanTestCleanupInfo{
		testDir: testDir,
		dc:      dc,
	}
	
	return scanEntry
}

// cleanupBEScan cleans up resources created during testing
func cleanupBEScan(t *testing.T, entry BinaryEntryInterface) {
	// Look up cleanup info from global map
	if cleanupInfo, exists := testCleanupData[entry]; exists {
		// Clean up test directory
		if cleanupInfo.dc != nil {
			cleanupInfo.dc.cleanupCurrentScanFile()
		}
		if cleanupInfo.testDir != "" {
			os.RemoveAll(cleanupInfo.testDir)
		}
		// Remove from map
		delete(testCleanupData, entry)
	}
}


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
	entry, cleanup := helper.createTestEntry(t)
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
	entry, cleanup := helper.createTestEntry(t)
	defer cleanup()
	
	// Test hash update (simulating hash worker)
	newHash := [20]byte{0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11, 0x00, 0xff, 0xee, 0xdd, 0xcc}
	newHashType := uint16(HashTypeSHA256)
	
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
	entry, cleanup := helper.createTestEntry(t)
	defer cleanup()
	
	// This test verifies that our locking prevents races during mremap
	// In practice, this would be very hard to reproduce, but we can at least
	// verify that concurrent access doesn't panic
	
	const numReaders = 5
	const numOperations = 50
	
	done := make(chan bool, numReaders)
	
	// Start concurrent readers
	for i := 0; i < numReaders; i++ {
		go func() {
			for j := 0; j < numOperations; j++ {
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
	for i := 0; i < numReaders; i++ {
		<-done
	}
}

// testBEScanInvalidHandling tests handling of invalid entries
func testBEScanInvalidHandling(t *testing.T) {
	// Create an invalid entry (nil IndexFile)
	invalidRef := binaryEntryRef{
		Offset:    0,
		IndexFile: nil,
	}
	
	invalidEntry := NewBEScanEntry(invalidRef)
	
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

// scanTestHelper helps create scan entries for testing
type scanTestHelper struct {
	testDir string
	dc      *DirectoryCache
}

// createTestEntry creates a test scan entry and returns it with a cleanup function
func (h *scanTestHelper) createTestEntry(t *testing.T) (*BEScanEntry, func()) {
	// Create temporary directory
	testDir, err := os.MkdirTemp("", "dcfh-scan-specific-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	h.testDir = testDir
	
	// Create DirectoryCache
	h.dc = NewDirectoryCache(testDir, testDir)
	
	// Create test data
	testData := CreateTestData()
	// Update the expected size to match what AppendEntryToScanIndex actually creates
	testData.Size = uint32(BESizeFromPathLen(len(testData.RelativePath)))
	
	// Generate scan file name
	scanFileName := h.dc.generateScanFileName()
	
	// Initialize scan index
	if err := h.dc.initialiseScanIndex(scanFileName); err != nil {
		t.Fatalf("Failed to initialize scan index: %v", err)
	}
	
	// Create mock file info and stat
	mockInfo := &mockFileInfo{
		name:    "test_file.txt",
		size:    int64(testData.FileSize),
		mode:    os.FileMode(testData.Mode),
		modTime: time.Unix(1234567900, 0),
	}
	
	mockStat := &syscall.Stat_t{
		Dev:   uint64(testData.Dev),
		Ino:   uint64(testData.Ino),
		Mode:  testData.Mode,
		Uid:   testData.UID,
		Gid:   testData.GID,
		Ctim:  syscall.Timespec{Sec: 1234567890, Nsec: 0},
		Mtim:  syscall.Timespec{Sec: 1234567900, Nsec: 0},
	}
	
	// Append entry to scan index
	_, err = h.dc.AppendEntryToScanIndex(
		scanFileName,
		testData.RelativePath,
		testData.Hash[:],
		testData.HashType,
		mockInfo,
		mockStat,
		testData.IsDeleted,
	)
	if err != nil {
		t.Fatalf("Failed to create scan entry: %v", err)
	}
	
	// Create binaryEntryRef
	// The first entry has offset 0 relative to entries section (after header)
	entryRef := binaryEntryRef{
		Offset:    0,
		IndexFile: h.dc.currentScan,
	}
	
	// Create BEScanEntry
	scanEntry := NewBEScanEntry(entryRef)
	
	// Return entry and cleanup function
	cleanup := func() {
		if h.dc != nil {
			h.dc.cleanupCurrentScanFile()
		}
		if h.testDir != "" {
			os.RemoveAll(h.testDir)
		}
	}
	
	return scanEntry, cleanup
}


// Benchmark tests for BEScan
func BenchmarkBEScan(b *testing.B) {
	helper := &scanTestHelper{}
	
	createFn := func() BinaryEntryInterface {
		entry, _ := helper.createTestEntry(&testing.T{}) // This is a hack, but works for benchmarks
		return entry
	}
	
	cleanupFn := func(entry BinaryEntryInterface) {
		if _, ok := entry.(*BEScanEntry); ok {
			// Manually cleanup - this is a limitation of the benchmark approach
			if helper.dc != nil {
				helper.dc.cleanupCurrentScanFile()
			}
			if helper.testDir != "" {
				os.RemoveAll(helper.testDir)
			}
		}
	}
	
	suite := &BinaryEntryTestSuite{}
	
	b.Run("FieldAccess", func(b *testing.B) {
		suite.BenchmarkFieldAccess(b, createFn, cleanupFn)
	})
	
	b.Run("ConcurrentAccess", func(b *testing.B) {
		suite.BenchmarkConcurrentAccess(b, createFn, cleanupFn)
	})
}