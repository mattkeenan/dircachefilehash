package dircachefilehash

import (
	"os"
	"sync"
	"testing"
	"time"
	"unsafe"
)

// TestBESkiplistEntry runs the implementation-neutral test suite for BESkiplistEntry
func TestBESkiplistEntry(t *testing.T) {
	suite := &BinaryEntryTestSuite{
		Name:               "BESkiplistEntry",
		CreateEntry:        createBESkiplistEntry,
		CleanupEntry:       cleanupBESkiplistEntry,
		SupportsSetHash:    true,
		SupportsSetDeleted: true,
		IsEphemeral:        false, // Skiplist entries are stable
	}
	
	suite.RunAllTests(t)
}

// testCleanupDataSkiplist stores cleanup information for skiplist test entries
var testCleanupDataSkiplist = make(map[BinaryEntryInterface]*skiplistTestCleanupInfo)

type skiplistTestCleanupInfo struct {
	testDir  string
	dc       *DirectoryCache
	skiplist *skiplistWrapper
}

// createMockBinaryEntryFromTestData creates a mock binaryEntry from TestEntryData
// Similar to createMockBinaryEntry but uses TestEntryData for full field population
func createMockBinaryEntryFromTestData(testData *TestEntryData) *binaryEntry {
	// Create a properly sized buffer like the real system would
	baseSize := int(unsafe.Sizeof(binaryEntry{}))
	totalSize := baseSize + len(testData.RelativePath) + 1
	padding := (8 - (totalSize % 8)) % 8
	entrySize := totalSize + padding
	
	data := make([]byte, entrySize)
	entry := (*binaryEntry)(unsafe.Pointer(&data[0]))
	
	// Populate all fields from testData
	entry.Size = uint32(entrySize)
	entry.CTimeWall = testData.CTimeWall
	entry.MTimeWall = testData.MTimeWall
	entry.Dev = testData.Dev
	entry.Ino = testData.Ino
	entry.Mode = testData.Mode
	entry.UID = testData.UID
	entry.GID = testData.GID
	entry.FileSize = testData.FileSize
	entry.HashType = testData.HashType
	copy(entry.Hash[:], testData.Hash[:])
	entry.EntryFlags = uint16(testData.EntryFlags)
	
	// Copy path after the struct
	pathStart := baseSize
	copy(data[pathStart:], testData.RelativePath)
	data[pathStart+len(testData.RelativePath)] = 0 // null terminator
	
	return entry
}

// createBESkiplistEntry creates a BESkiplistEntry for testing
// This creates a mock entry similar to how existing skiplist tests work
func createBESkiplistEntry(t *testing.T, testData *TestEntryData) BinaryEntryInterface {
	// Update the expected size to match what will be created
	testData.Size = uint32(BESizeFromPathLen(len(testData.RelativePath)))
	entrySize := int(testData.Size)
	
	// Create mock index file data with header + entry
	indexSize := HeaderSize + entrySize
	indexData := make([]byte, indexSize)
	
	// Create mock mmap index file
	mockIndexFile := &mmapIndexFile{
		Data:  indexData,
		Size:  indexSize,
		mutex: sync.RWMutex{},
	}
	
	// Create the entry within the index file data (after header)
	entryPtr := (*binaryEntry)(unsafe.Pointer(&indexData[HeaderSize]))
	
	// Populate the entry with test data
	entryPtr.Size = testData.Size
	entryPtr.CTimeWall = testData.CTimeWall
	entryPtr.MTimeWall = testData.MTimeWall
	entryPtr.Dev = testData.Dev
	entryPtr.Ino = testData.Ino
	entryPtr.Mode = testData.Mode
	entryPtr.UID = testData.UID
	entryPtr.GID = testData.GID
	entryPtr.FileSize = testData.FileSize
	entryPtr.HashType = testData.HashType
	copy(entryPtr.Hash[:], testData.Hash[:20])
	entryPtr.EntryFlags = uint16(testData.EntryFlags)
	
	// Copy path after the struct (within the index data)
	pathStart := HeaderSize + int(unsafe.Sizeof(*entryPtr))
	copy(indexData[pathStart:], testData.RelativePath)
	indexData[pathStart+len(testData.RelativePath)] = 0 // null terminator
	
	// Create binaryEntryRef with offset 0 (first entry after header)
	ref := binaryEntryRef{
		Offset:    0,
		IndexFile: mockIndexFile,
	}
	
	// Create BESkiplistEntry
	skiplistEntry := NewBESkiplistEntry(ref)
	
	// Store cleanup info (just need to track the allocated entry)
	testCleanupDataSkiplist[skiplistEntry] = &skiplistTestCleanupInfo{
		testDir:  "", // No directory to clean up
		dc:       nil,
		skiplist: nil,
	}
	
	return skiplistEntry
}

// cleanupBESkiplistEntry cleans up resources created during testing
func cleanupBESkiplistEntry(t *testing.T, entry BinaryEntryInterface) {
	// Look up cleanup info from global map
	if cleanupInfo, exists := testCleanupDataSkiplist[entry]; exists {
		// Clean up directory if any was created
		if cleanupInfo.testDir != "" {
			os.RemoveAll(cleanupInfo.testDir)
		}
		// Note: skiplist doesn't need explicit cleanup for mock entries
		// Remove from map
		delete(testCleanupDataSkiplist, entry)
	}
}

// TestBESkiplistEntrySpecific tests BESkiplistEntry-specific functionality
func TestBESkiplistEntrySpecific(t *testing.T) {
	t.Run("StableBehavior", testBESkiplistEntryStableBehavior)
	t.Run("ReadOnlyPattern", testBESkiplistEntryReadOnlyPattern)
	t.Run("ConcurrentAccess", testBESkiplistEntryConcurrentAccess)
	t.Run("MmapSafety", testBESkiplistEntryMmapSafety)
}

// testBESkiplistEntryStableBehavior tests stable (non-ephemeral) behavior
func testBESkiplistEntryStableBehavior(t *testing.T) {
	helper := &skiplistTestHelper{}
	entry, cleanup := helper.createTestEntry(t)
	defer cleanup()
	
	// Should always be valid for stable entries
	if !entry.IsValid() {
		t.Error("Skiplist entry should always be valid")
	}
	
	// Should be able to access all fields reliably
	for i := 0; i < 10; i++ {
		if path, err := entry.RelativePath(); err != nil {
			t.Errorf("RelativePath() iteration %d returned error: %v", i, err)
		} else if path != "test/file.txt" {
			t.Errorf("RelativePath() iteration %d = %q, want %q", i, path, "test/file.txt")
		}
		
		if size, err := entry.Size(); err != nil {
			t.Errorf("Size() iteration %d returned error: %v", i, err)
		} else if size == 0 {
			t.Errorf("Size() iteration %d = 0, should be non-zero", i)
		}
	}
}

// testBESkiplistEntryReadOnlyPattern tests typical read-only usage pattern
func testBESkiplistEntryReadOnlyPattern(t *testing.T) {
	helper := &skiplistTestHelper{}
	entry, cleanup := helper.createTestEntry(t)
	defer cleanup()
	
	// Test batch read operations (typical skiplist usage)
	entry.RLock()
	
	path, err1 := entry.RelativePath()
	size, err2 := entry.Size()
	hash, err3 := entry.HashString()
	deleted, err4 := entry.IsDeleted()
	
	entry.RUnlock()
	
	// Verify all operations succeeded
	if err1 != nil {
		t.Errorf("RelativePath() in batch operation returned error: %v", err1)
	}
	if err2 != nil {
		t.Errorf("Size() in batch operation returned error: %v", err2)
	}
	if err3 != nil {
		t.Errorf("HashString() in batch operation returned error: %v", err3)
	}
	if err4 != nil {
		t.Errorf("IsDeleted() in batch operation returned error: %v", err4)
	}
	
	// Verify results are sensible
	if path != "test/file.txt" {
		t.Errorf("Batch RelativePath() = %q, want %q", path, "test/file.txt")
	}
	if size == 0 {
		t.Errorf("Batch Size() = 0, should be non-zero")
	}
	if len(hash) != 40 { // 20 bytes * 2 hex chars
		t.Errorf("Batch HashString() length = %d, want 40", len(hash))
	}
	if deleted {
		t.Errorf("Batch IsDeleted() = true, want false for test entry")
	}
}

// testBESkiplistEntryConcurrentAccess tests concurrent access patterns
func testBESkiplistEntryConcurrentAccess(t *testing.T) {
	helper := &skiplistTestHelper{}
	entry, cleanup := helper.createTestEntry(t)
	defer cleanup()
	
	// This test verifies that concurrent read access works correctly
	// (typical pattern for skiplist entries in production)
	
	const numReaders = 10
	const numOperations = 100
	
	done := make(chan bool, numReaders)
	
	// Start concurrent readers
	for i := 0; i < numReaders; i++ {
		go func() {
			for j := 0; j < numOperations; j++ {
				// Test concurrent read access
				if path, err := entry.RelativePath(); err != nil {
					t.Errorf("Concurrent RelativePath() error: %v", err)
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

// testBESkiplistEntryMmapSafety tests mmap coordination safety
func testBESkiplistEntryMmapSafety(t *testing.T) {
	helper := &skiplistTestHelper{}
	entry, cleanup := helper.createTestEntry(t)
	defer cleanup()
	
	// Test that the entry properly coordinates with underlying mmap
	// This primarily tests that the locking doesn't deadlock or panic
	
	// Multiple readers should work fine
	for i := 0; i < 5; i++ {
		go func() {
			entry.RLock()
			defer entry.RUnlock()
			
			entry.RelativePath()
			entry.Size()
			entry.HashString()
		}()
	}
	
	// Brief pause to let goroutines run
	time.Sleep(10 * time.Millisecond)
	
	// Should still be accessible after concurrent access
	if path, err := entry.RelativePath(); err != nil {
		t.Errorf("RelativePath() after concurrent access returned error: %v", err)
	} else if path != "test/file.txt" {
		t.Errorf("RelativePath() after concurrent access = %q, want %q", path, "test/file.txt")
	}
}

// skiplistTestHelper helps create skiplist entries for testing
type skiplistTestHelper struct {
	testDir  string
	dc       *DirectoryCache
	skiplist *skiplistWrapper
}

// createTestEntry creates a test skiplist entry and returns it with a cleanup function
func (h *skiplistTestHelper) createTestEntry(t *testing.T) (*BESkiplistEntry, func()) {
	// Create temporary directory
	testDir, err := os.MkdirTemp("", "dcfh-skiplist-specific-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	h.testDir = testDir
	
	// Create DirectoryCache
	h.dc = NewDirectoryCache(testDir, testDir)
	
	// Create test data
	testData := CreateTestData()
	testData.Size = uint32(BESizeFromPathLen(len(testData.RelativePath)))
	
	// Use the createBESkiplistEntry helper to set up the full infrastructure
	entry := createBESkiplistEntry(t, testData).(*BESkiplistEntry)
	
	// Return entry and cleanup function
	cleanup := func() {
		cleanupBESkiplistEntry(t, entry)
	}
	
	return entry, cleanup
}

// Benchmark tests for BESkiplistEntry
func BenchmarkBESkiplistEntry(b *testing.B) {
	// Create test entry once for all benchmarks
	testData := CreateTestData()
	testData.Size = uint32(BESizeFromPathLen(len(testData.RelativePath)))
	entry := createBESkiplistEntry(&testing.T{}, testData) // This is a hack, but works for benchmarks
	
	defer func() {
		// Cleanup
		if cleanupInfo, exists := testCleanupDataSkiplist[entry]; exists {
			if cleanupInfo.testDir != "" {
				os.RemoveAll(cleanupInfo.testDir)
			}
			// Note: skiplist doesn't need explicit cleanup for mock entries
			delete(testCleanupDataSkiplist, entry)
		}
	}()
	
	b.Run("RelativePath", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			entry.RelativePath()
		}
	})
	
	b.Run("HashString", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			entry.HashString()
		}
	})
	
	b.Run("Size", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			entry.Size()
		}
	})
}