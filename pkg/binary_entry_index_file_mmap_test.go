package dircachefilehash

import (
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

// TestBEIndexFileMmap runs the implementation-neutral test suite for BEIndexFileMmap.
// SetHash/SetDeleted are not exercised: the v0.7 production load path
// (loadIndexShared) maps PROT_READ only.
func TestBEIndexFileMmap(t *testing.T) {
	suite := &BinaryEntryTestSuite{
		Name:               "BEIndexFileMmap",
		CreateEntry:        createBEIndexFileMmap,
		CleanupEntry:       cleanupBEIndexFileMmap,
		SupportsSetHash:    false,
		SupportsSetDeleted: false,
		IsEphemeral:        false,
	}

	suite.RunAllTests(t)
}

// testCleanupDataIndexFileMmap stores cleanup information for mmap index file test entries
var testCleanupDataIndexFileMmap = make(map[BinaryEntryInterface]*indexFileMmapTestCleanupInfo)
var cleanupMutexMmap sync.Mutex

type indexFileMmapTestCleanupInfo struct {
	testDir string
	dc      *DirectoryCache
}

// createBEIndexFileMmap creates a BEIndexFileMmapEntry backed by a real
// mmap'd index file written via the production path
// (TempIndexWriter.WriteSerialised + loadIndexShared).
func createBEIndexFileMmap(t *testing.T, testData *TestEntryData) BinaryEntryInterface {
	testDir, err := os.MkdirTemp("", "dcfh-index-file-mmap-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	dc := NewDirectoryCache(testDir, testDir)

	testData.Size = uint32(BESizeFromPathLen(len(testData.RelativePath)))

	mockInfo := &mockFileInfo{
		name:    "test_file.txt",
		size:    int64(testData.FileSize),
		mode:    os.FileMode(testData.Mode),
		modTime: time.Unix(1234567900, 0),
	}
	mockStat := &syscall.Stat_t{
		Dev:  uint64(testData.Dev),
		Ino:  uint64(testData.Ino),
		Mode: testData.Mode,
		Uid:  testData.UID,
		Gid:  testData.GID,
		Ctim: syscall.Timespec{Sec: 1234567890, Nsec: 0},
		Mtim: syscall.Timespec{Sec: 1234567900, Nsec: 0},
	}

	// Build a heap BEScanEntry to capture the test fields the suite expects,
	// then serialise it through the production EntrySerialiser path.
	scanEntry := NewBEScanEntry(testData.RelativePath, mockInfo, mockStat)
	be, _ := scanEntry.getBinaryEntry()
	be.CTimeWall = testData.CTimeWall
	if testData.HashType != 0 {
		_ = scanEntry.SetHash(testData.Hash[:], testData.HashType)
	}
	if (testData.EntryFlags & 1) != 0 {
		_ = scanEntry.SetDeleted(true)
	}

	serialised, err := NewEntrySerialiser().Serialise(scanEntry)
	if err != nil {
		_ = os.RemoveAll(testDir)
		t.Fatalf("Failed to serialise test entry: %v", err)
	}

	// Write the entry to a temp index file using the production writer.
	indexPath := filepath.Join(testDir, "test.idx")
	writer, err := NewTempIndexWriter(dc, indexPath)
	if err != nil {
		_ = os.RemoveAll(testDir)
		t.Fatalf("Failed to create temp index writer: %v", err)
	}
	if err := writer.WriteSerialised([][]byte{serialised}); err != nil {
		_ = os.RemoveAll(testDir)
		t.Fatalf("Failed to write serialised entry: %v", err)
	}
	if err := writer.Close(); err != nil {
		_ = os.RemoveAll(testDir)
		t.Fatalf("Failed to close temp index writer: %v", err)
	}

	// Mmap the index back through the shared loading path.
	_, refs, err := dc.loadIndexShared(indexPath)
	if err != nil {
		_ = os.RemoveAll(testDir)
		t.Fatalf("Failed to load index: %v", err)
	}
	if len(refs) == 0 {
		_ = os.RemoveAll(testDir)
		t.Fatalf("loadIndexShared returned no refs")
	}

	mmapEntry := NewBEIndexFileMmapEntry(refs[0], "test")

	cleanupMutexMmap.Lock()
	testCleanupDataIndexFileMmap[mmapEntry] = &indexFileMmapTestCleanupInfo{
		testDir: testDir,
		dc:      dc,
	}
	cleanupMutexMmap.Unlock()

	return mmapEntry
}

// cleanupBEIndexFileMmap cleans up resources created during testing
func cleanupBEIndexFileMmap(t *testing.T, entry BinaryEntryInterface) {
	cleanupMutexMmap.Lock()
	defer cleanupMutexMmap.Unlock()

	if cleanupInfo, exists := testCleanupDataIndexFileMmap[entry]; exists {
		if cleanupInfo.dc != nil {
			_ = cleanupInfo.dc.Close()
		}
		if cleanupInfo.testDir != "" {
			_ = os.RemoveAll(cleanupInfo.testDir)
		}
		delete(testCleanupDataIndexFileMmap, entry)
	}
}

// TestBEIndexFileMmapSpecific tests BEIndexFileMmap-specific functionality
func TestBEIndexFileMmapSpecific(t *testing.T) {
	t.Run("MmapAccess", testBEIndexFileMmapMmapAccess)
	t.Run("SkiplistSupport", testBEIndexFileMmapSkiplistSupport)
	t.Run("MmapSafety", testBEIndexFileMmapMmapSafety)
	t.Run("WriteOperations", testBEIndexFileMmapWriteOperations)
	t.Run("ConcurrentMmapAccess", testBEIndexFileMmapConcurrentMmapAccess)
}

// testBEIndexFileMmapMmapAccess tests memory-mapped access patterns
func testBEIndexFileMmapMmapAccess(t *testing.T) {
	helper := &indexFileMmapTestHelper{}
	entry, cleanup := helper.createTestEntry(t)
	defer cleanup()

	// Test multiple read operations (should be fast with mmap)
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

		if hash, err := entry.HashString(); err != nil {
			t.Errorf("HashString() iteration %d returned error: %v", i, err)
		} else if len(hash) != 40 {
			t.Errorf("HashString() iteration %d length = %d, want 40", i, len(hash))
		}
	}
}

// testBEIndexFileMmapSkiplistSupport tests skiplist building support
func testBEIndexFileMmapSkiplistSupport(t *testing.T) {
	helper := &indexFileMmapTestHelper{}
	entry, cleanup := helper.createTestEntry(t)
	defer cleanup()

	// Should support skiplist building
	if !entry.SupportsSkiplistBuilding() {
		t.Error("BEIndexFileMmapEntry should support skiplist building")
	}

	// Should provide binaryEntryRef
	ref, ok := entry.GetBinaryEntryRef()
	if !ok {
		t.Error("BEIndexFileMmapEntry should provide binaryEntryRef")
	}

	// Verify the ref is valid
	if ref.IndexFile == nil {
		t.Error("binaryEntryRef should have valid IndexFile")
	}

	if ref.Offset < 0 {
		t.Error("binaryEntryRef should have valid offset")
	}

	// Verify we can access the entry through the ref
	actualEntry := ref.GetBinaryEntry()
	if actualEntry == nil {
		t.Error("binaryEntryRef should provide valid entry")
	}
}

// testBEIndexFileMmapMmapSafety tests mmap coordination safety
func testBEIndexFileMmapMmapSafety(t *testing.T) {
	helper := &indexFileMmapTestHelper{}
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

// testBEIndexFileMmapWriteOperations is a no-op in v0.7: the production load
// path maps PROT_READ only, so in-place writes are not exercised.
func testBEIndexFileMmapWriteOperations(t *testing.T) {
	t.Skip("v0.7: BEIndexFileMmapEntry backing mmap is read-only; writes are not supported")
}

// testBEIndexFileMmapConcurrentMmapAccess tests concurrent mmap access
func testBEIndexFileMmapConcurrentMmapAccess(t *testing.T) {
	helper := &indexFileMmapTestHelper{}
	entry, cleanup := helper.createTestEntry(t)
	defer cleanup()

	// Test concurrent read access (typical pattern for mmap entries)
	const numReaders = 10
	const numOperations = 50

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

// indexFileMmapTestHelper helps create mmap index file entries for testing
type indexFileMmapTestHelper struct {
}

// createTestEntry creates a test mmap index file entry and returns it with a cleanup function
func (h *indexFileMmapTestHelper) createTestEntry(t *testing.T) (*BEIndexFileMmapEntry, func()) {
	// Create test data
	testData := CreateTestData()
	testData.Size = uint32(BESizeFromPathLen(len(testData.RelativePath)))

	// Use the createBEIndexFileMmap helper to set up the full infrastructure
	entry := createBEIndexFileMmap(t, testData).(*BEIndexFileMmapEntry)

	// Return entry and cleanup function
	cleanup := func() {
		cleanupBEIndexFileMmap(t, entry)
	}

	return entry, cleanup
}

// Benchmark tests for BEIndexFileMmap
func BenchmarkBEIndexFileMmap(b *testing.B) {
	// Create test entry once for all benchmarks
	testData := CreateTestData()
	testData.Size = uint32(BESizeFromPathLen(len(testData.RelativePath)))
	entry := createBEIndexFileMmap(&testing.T{}, testData) // This is a hack, but works for benchmarks

	defer func() {
		// Cleanup
		cleanupMutexMmap.Lock()
		if cleanupInfo, exists := testCleanupDataIndexFileMmap[entry]; exists {
			if cleanupInfo.testDir != "" {
				_ = os.RemoveAll(cleanupInfo.testDir)
			}
			delete(testCleanupDataIndexFileMmap, entry)
		}
		cleanupMutexMmap.Unlock()
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

	b.Run("SetHash", func(b *testing.B) {
		hash := [20]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}
		for i := 0; i < b.N; i++ {
			_ = entry.SetHash(hash[:], HashTypeSHA1)
		}
	})

	b.Run("GetBinaryEntryRef", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			entry.GetBinaryEntryRef()
		}
	})
}
