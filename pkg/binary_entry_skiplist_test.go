package dircachefilehash

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"
	"unsafe"
)

// TestBESkiplist runs the implementation-neutral test suite for BESkiplist
func TestBESkiplist(t *testing.T) {
	suite := &BinaryEntryTestSuite{
		Name:               "BESkiplist",
		CreateEntry:        createBESkiplist,
		CleanupEntry:       cleanupBESkiplist,
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

// createBESkiplist creates a BESkiplistEntry for testing
// This creates a mock entry similar to how existing skiplist tests work
func createBESkiplist(t *testing.T, testData *TestEntryData) BinaryEntryInterface {
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

	// Create BESkiplistEntry (test doesn't need real skiplist for basic functionality)
	skiplistEntry := NewBESkiplistEntry(ref, nil)

	// Store cleanup info (just need to track the allocated entry)
	testCleanupDataSkiplist[skiplistEntry] = &skiplistTestCleanupInfo{
		testDir:  "", // No directory to clean up
		dc:       nil,
		skiplist: nil,
	}

	return skiplistEntry
}

// cleanupBESkiplist cleans up resources created during testing
func cleanupBESkiplist(t *testing.T, entry BinaryEntryInterface) {
	// Look up cleanup info from global map
	if cleanupInfo, exists := testCleanupDataSkiplist[entry]; exists {
		// Clean up directory if any was created
		if cleanupInfo.testDir != "" {
			_ = os.RemoveAll(cleanupInfo.testDir)
		}
		// Note: skiplist doesn't need explicit cleanup for mock entries
		// Remove from map
		delete(testCleanupDataSkiplist, entry)
	}
}

// TestBESkiplistSpecific tests BESkiplist-specific functionality
func TestBESkiplistSpecific(t *testing.T) {
	t.Run("StableBehavior", testBESkiplistEntryStableBehavior)
	t.Run("ReadOnlyPattern", testBESkiplistEntryReadOnlyPattern)
	t.Run("ConcurrentAccess", testBESkiplistEntryConcurrentAccess)
	t.Run("MmapSafety", testBESkiplistEntryMmapSafety)
	t.Run("IterationVisitsAllPathLengths", testSkiplistIterationAllPathLengths)
}

// testSkiplistIterationAllPathLengths verifies that skiplist iteration via
// First()/Next() visits every inserted entry, regardless of path length.
// Reproducer for a bug where files with filenames at NAME_MAX (255 bytes)
// were found by Find() but skipped by sequential iteration.
func testSkiplistIterationAllPathLengths(t *testing.T) {
	const dirPrefix = "Desktop/Agent Anderson Scott – Posts _ Facebook_files/"
	const maxNameLen = 255

	// Build mock index data containing entries with filename lengths 1..255
	// All entries go into one contiguous buffer (header + entries)
	type entryInfo struct {
		path   string
		offset int // offset from start of entry data (after header)
	}

	var entries []entryInfo
	totalEntryBytes := 0

	for nameLen := 1; nameLen <= maxNameLen; nameLen++ {
		// Generate a filename of exactly nameLen bytes using printable ASCII
		name := make([]byte, nameLen)
		for i := range name {
			// Use letters that produce varied sort positions
			name[i] = byte('A' + (i+nameLen)%26)
		}
		// Ensure .js extension for realism
		if nameLen >= 4 {
			copy(name[nameLen-3:], ".js")
		}

		path := dirPrefix + string(name)
		entrySize := BESizeFromPathLen(len(path))

		entries = append(entries, entryInfo{
			path:   path,
			offset: totalEntryBytes,
		})
		totalEntryBytes += entrySize
	}

	// Allocate contiguous buffer: header + all entries
	indexSize := HeaderSize + totalEntryBytes
	indexData := make([]byte, indexSize)

	// Create mock mmap index file
	mockIndexFile := &mmapIndexFile{
		Data:  indexData,
		Size:  indexSize,
		mutex: sync.RWMutex{},
	}

	// Populate each entry in the buffer
	for _, ei := range entries {
		entrySize := BESizeFromPathLen(len(ei.path))
		entryPtr := (*binaryEntry)(unsafe.Pointer(&indexData[HeaderSize+ei.offset]))
		entryPtr.Size = uint32(entrySize)
		entryPtr.Mode = 0100644
		entryPtr.FileSize = 1000
		entryPtr.HashType = HashTypeSHA1

		// Copy path after the struct
		pathStart := HeaderSize + ei.offset + int(unsafe.Sizeof(*entryPtr))
		copy(indexData[pathStart:], ei.path)
		indexData[pathStart+len(ei.path)] = 0 // null terminator
	}

	// Insert all entries into a skiplist
	skiplist := NewSkiplistWrapper(16, MainContext)
	for _, ei := range entries {
		ref := binaryEntryRef{
			Offset:    ei.offset,
			IndexFile: mockIndexFile,
		}
		if !skiplist.Insert(ref, MainContext) {
			t.Fatalf("Failed to insert entry with path length %d: %s", len(ei.path), ei.path)
		}
	}

	// Verify skiplist has all entries
	if skiplist.Length() != maxNameLen {
		t.Fatalf("Skiplist length = %d, want %d", skiplist.Length(), maxNameLen)
	}

	// Verify all entries are reachable by Find()
	for _, ei := range entries {
		if entry, _ := skiplist.Find(ei.path); entry == nil {
			t.Errorf("Find() failed for path length %d: %s", len(ei.path), ei.path)
		}
	}

	// Verify all entries are reachable by ForEach iteration
	visitedByForEach := make(map[string]bool)
	skiplist.ForEach(func(entry *binaryEntry, context string) bool {
		path := entry.RelativePath()
		visitedByForEach[path] = true
		return true
	})

	if len(visitedByForEach) != maxNameLen {
		t.Errorf("ForEach visited %d entries, want %d", len(visitedByForEach), maxNameLen)
	}

	// Verify all entries are reachable by BinaryEntrySkiplistIterator (First/Next cursor)
	// This is the iterator used by the status pipeline via Hwang-Lin
	ctx := context.Background()
	iter := NewBinaryEntrySkiplistIterator(ctx, skiplist, "test")
	defer func() { _ = iter.Close() }()

	visitedByIterator := make(map[string]bool)
	for {
		entry, err := iter.Next()
		if err != nil {
			t.Fatalf("Iterator error: %v", err)
		}
		if entry == nil {
			break
		}
		path, err := entry.RelativePath()
		if err != nil {
			t.Fatalf("RelativePath error: %v", err)
		}
		if path == "." || path == "" {
			t.Errorf("Iterator returned empty/dot path")
		}
		visitedByIterator[path] = true
	}

	if len(visitedByIterator) != maxNameLen {
		t.Errorf("Iterator visited %d unique paths, want %d", len(visitedByIterator), maxNameLen)
	}

	// Report which entries were missed by each method
	for _, ei := range entries {
		nameLen := len(ei.path) - len(dirPrefix)
		if !visitedByForEach[ei.path] {
			t.Errorf("ForEach missed entry with filename length %d", nameLen)
		}
		if !visitedByIterator[ei.path] {
			t.Errorf("Iterator missed entry with filename length %d", nameLen)
		}
	}
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
	for i := range 10 {
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
	for range numReaders {
		go func() {
			for range numOperations {
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
	for range numReaders {
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
	for range 5 {
		go func() {
			entry.RLock()
			defer entry.RUnlock()

			_, _ = entry.RelativePath()
			_, _ = entry.Size()
			_, _ = entry.HashString()
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
	testDir string
	dc      *DirectoryCache
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

	// Use the createBESkiplist helper to set up the full infrastructure
	entry := createBESkiplist(t, testData).(*BESkiplistEntry)

	// Return entry and cleanup function
	cleanup := func() {
		cleanupBESkiplist(t, entry)
	}

	return entry, cleanup
}

// Benchmark tests for BESkiplist
func BenchmarkBESkiplist(b *testing.B) {
	// Create test entry once for all benchmarks
	testData := CreateTestData()
	testData.Size = uint32(BESizeFromPathLen(len(testData.RelativePath)))
	entry := createBESkiplist(&testing.T{}, testData) // This is a hack, but works for benchmarks

	defer func() {
		// Cleanup
		if cleanupInfo, exists := testCleanupDataSkiplist[entry]; exists {
			if cleanupInfo.testDir != "" {
				_ = os.RemoveAll(cleanupInfo.testDir)
			}
			// Note: skiplist doesn't need explicit cleanup for mock entries
			delete(testCleanupDataSkiplist, entry)
		}
	}()

	b.Run("RelativePath", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = entry.RelativePath()
		}
	})

	b.Run("HashString", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = entry.HashString()
		}
	})

	b.Run("Size", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = entry.Size()
		}
	})
}
