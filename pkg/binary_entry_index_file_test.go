package dircachefilehash

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
	"unsafe"
)

// TestBEIndexFileIO runs the implementation-neutral test suite for BEIndexFileIO
func TestBEIndexFileIO(t *testing.T) {
	suite := &BinaryEntryTestSuite{
		Name:               "BEIndexFileIO",
		CreateEntry:        createBEIndexFileIO,
		CleanupEntry:       cleanupBEIndexFileIO,
		SupportsSetHash:    true,
		SupportsSetDeleted: true,
		IsEphemeral:        false, // File-based entries are stable
	}
	
	suite.RunAllTests(t)
}

// testCleanupDataIndexFile stores cleanup information for index file test entries
var testCleanupDataIndexFile = make(map[BinaryEntryInterface]*indexFileTestCleanupInfo)
var cleanupMutex sync.Mutex

type indexFileTestCleanupInfo struct {
	testDir    string
	indexFile  string
	fileHandle *os.File
}

// createBEIndexFileIO creates a BEIndexFileIOEntry for testing
// This creates a temporary index file with the test entry data
func createBEIndexFileIO(t *testing.T, testData *TestEntryData) BinaryEntryInterface {
	// Create temporary directory for test
	testDir, err := os.MkdirTemp("", "dcfh-index-file-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	
	// Update the expected size to match what will be created
	testData.Size = uint32(BESizeFromPathLen(len(testData.RelativePath)))
	entrySize := int(testData.Size)
	
	// Create temporary index file
	indexFilePath := filepath.Join(testDir, "test.idx")
	indexFile, err := os.Create(indexFilePath)
	if err != nil {
		os.RemoveAll(testDir)
		t.Fatalf("Failed to create temp index file: %v", err)
	}
	
	// Write minimal header (64 bytes)
	header := make([]byte, HeaderSize)
	copy(header[0:4], "dcfh") // signature
	if _, err := indexFile.Write(header); err != nil {
		indexFile.Close()
		os.RemoveAll(testDir)
		t.Fatalf("Failed to write header: %v", err)
	}
	
	// Create entry data
	entryData := make([]byte, entrySize)
	entry := (*binaryEntry)(unsafe.Pointer(&entryData[0]))
	
	// Populate the entry with test data
	entry.Size = testData.Size
	entry.CTimeWall = testData.CTimeWall
	entry.MTimeWall = testData.MTimeWall
	entry.Dev = testData.Dev
	entry.Ino = testData.Ino
	entry.Mode = testData.Mode
	entry.UID = testData.UID
	entry.GID = testData.GID
	entry.FileSize = testData.FileSize
	entry.HashType = testData.HashType
	copy(entry.Hash[:], testData.Hash[:20])
	entry.EntryFlags = uint16(testData.EntryFlags)
	
	// Copy path after the struct
	pathStart := int(unsafe.Sizeof(*entry))
	copy(entryData[pathStart:], testData.RelativePath)
	entryData[pathStart+len(testData.RelativePath)] = 0 // null terminator
	
	// Write entry data to file
	if _, err := indexFile.Write(entryData); err != nil {
		indexFile.Close()
		os.RemoveAll(testDir)
		t.Fatalf("Failed to write entry data: %v", err)
	}
	
	// Close and reopen for reading
	indexFile.Close()
	
	// Create BEIndexFileIOEntry pointing to the entry (after header)
	fileOffset := int64(HeaderSize)
	indexFileEntry := NewBEIndexFileIOEntry(indexFilePath, fileOffset, testData.Size, "test")
	
	// Store cleanup info with thread safety
	cleanupMutex.Lock()
	testCleanupDataIndexFile[indexFileEntry] = &indexFileTestCleanupInfo{
		testDir:   testDir,
		indexFile: indexFilePath,
	}
	cleanupMutex.Unlock()
	
	return indexFileEntry
}

// cleanupBEIndexFileIO cleans up resources created during testing
func cleanupBEIndexFileIO(t *testing.T, entry BinaryEntryInterface) {
	cleanupMutex.Lock()
	defer cleanupMutex.Unlock()
	
	// Look up cleanup info from global map
	if cleanupInfo, exists := testCleanupDataIndexFile[entry]; exists {
		// Clean up test directory (no file handles to close since each operation uses its own)
		if cleanupInfo.testDir != "" {
			os.RemoveAll(cleanupInfo.testDir)
		}
		
		// Remove from map
		delete(testCleanupDataIndexFile, entry)
	}
}

// TestBEIndexFileIOSpecific tests BEIndexFile-specific functionality
func TestBEIndexFileIOSpecific(t *testing.T) {
	t.Run("FileIOAccess", testBEIndexFileIOEntryFileIOAccess)
	t.Run("FileHandleManagement", testBEIndexFileIOEntryFileHandleManagement)
	t.Run("WriteOperations", testBEIndexFileIOEntryWriteOperations)
	t.Run("ErrorHandling", testBEIndexFileIOEntryErrorHandling)
	t.Run("ConcurrentFileAccess", testBEIndexFileIOEntryConcurrentFileAccess)
}

// testBEIndexFileIOEntryFileIOAccess tests file I/O access patterns
func testBEIndexFileIOEntryFileIOAccess(t *testing.T) {
	helper := &indexFileTestHelper{}
	entry, cleanup := helper.createTestEntry(t)
	defer cleanup()
	
	// Test multiple read operations
	for i := 0; i < 5; i++ {
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

// testBEIndexFileIOEntryFileHandleManagement tests file handle lifecycle
func testBEIndexFileIOEntryFileHandleManagement(t *testing.T) {
	helper := &indexFileTestHelper{}
	entry, cleanup := helper.createTestEntry(t)
	defer cleanup()
	
	// Test that file access works correctly
	if _, err := entry.RelativePath(); err != nil {
		t.Errorf("RelativePath() returned error: %v", err)
	}
	
	// Test multiple accesses (each should open/close its own handle)
	for i := 0; i < 3; i++ {
		if _, err := entry.RelativePath(); err != nil {
			t.Errorf("RelativePath() iteration %d returned error: %v", i, err)
		}
	}
	
	// Test that concurrent operations don't interfere
	// (each uses its own file handle)
}

// testBEIndexFileIOEntryWriteOperations tests write operations
func testBEIndexFileIOEntryWriteOperations(t *testing.T) {
	helper := &indexFileTestHelper{}
	entry, cleanup := helper.createTestEntry(t)
	defer cleanup()
	
	// Test hash update
	newHash := [20]byte{0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11, 0x00, 0xff, 0xee, 0xdd, 0xcc}
	newHashType := uint16(HashTypeSHA256)
	
	if err := entry.SetHash(newHash[:], newHashType); err != nil {
		t.Errorf("SetHash() returned error: %v", err)
	}
	
	// Verify update was successful (read from file)
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
	
	// Test deletion flag update
	if err := entry.SetDeleted(true); err != nil {
		t.Errorf("SetDeleted(true) returned error: %v", err)
	}
	
	if deleted, err := entry.IsDeleted(); err != nil {
		t.Errorf("IsDeleted() after SetDeleted(true) returned error: %v", err)
	} else if !deleted {
		t.Errorf("IsDeleted() after SetDeleted(true) = false, want true")
	}
	
	// Test unsetting deletion flag
	if err := entry.SetDeleted(false); err != nil {
		t.Errorf("SetDeleted(false) returned error: %v", err)
	}
	
	if deleted, err := entry.IsDeleted(); err != nil {
		t.Errorf("IsDeleted() after SetDeleted(false) returned error: %v", err)
	} else if deleted {
		t.Errorf("IsDeleted() after SetDeleted(false) = true, want false")
	}
}

// testBEIndexFileIOEntryErrorHandling tests error handling
func testBEIndexFileIOEntryErrorHandling(t *testing.T) {
	// Test with non-existent file
	invalidEntry := NewBEIndexFileIOEntry("/nonexistent/file.idx", HeaderSize, 128, "test")
	
	// Should not be valid
	if invalidEntry.IsValid() {
		t.Error("Entry with non-existent file should not be valid")
	}
	
	// All operations should return errors
	if _, err := invalidEntry.RelativePath(); err == nil {
		t.Error("RelativePath() on invalid entry should return error")
	}
	
	if _, err := invalidEntry.Hash(); err == nil {
		t.Error("Hash() on invalid entry should return error")
	}
	
	if err := invalidEntry.SetHash(make([]byte, 20), 1); err == nil {
		t.Error("SetHash() on invalid entry should return error")
	}
	
	// Test with invalid hash length
	helper := &indexFileTestHelper{}
	validEntry, cleanup := helper.createTestEntry(t)
	defer cleanup()
	
	if err := validEntry.SetHash(make([]byte, 10), 1); err == nil {
		t.Error("SetHash() with invalid hash length should return error")
	}
}

// testBEIndexFileIOEntryConcurrentFileAccess tests concurrent file access
func testBEIndexFileIOEntryConcurrentFileAccess(t *testing.T) {
	helper := &indexFileTestHelper{}
	entry, cleanup := helper.createTestEntry(t)
	defer cleanup()
	
	// Test concurrent read access
	const numReaders = 10
	const numOperations = 50
	
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

// indexFileTestHelper helps create index file entries for testing
type indexFileTestHelper struct {
	testDir   string
	indexFile string
}

// createTestEntry creates a test index file entry and returns it with a cleanup function
func (h *indexFileTestHelper) createTestEntry(t *testing.T) (*BEIndexFileIOEntry, func()) {
	// Create test data
	testData := CreateTestData()
	testData.Size = uint32(BESizeFromPathLen(len(testData.RelativePath)))
	
	// Use the createBEIndexFileIO helper to set up the full infrastructure
	entry := createBEIndexFileIO(t, testData).(*BEIndexFileIOEntry)
	
	// Return entry and cleanup function
	cleanup := func() {
		cleanupBEIndexFileIO(t, entry)
	}
	
	return entry, cleanup
}

// Benchmark tests for BEIndexFile
func BenchmarkBEIndexFileIO(b *testing.B) {
	// Create test entry once for all benchmarks
	testData := CreateTestData()
	testData.Size = uint32(BESizeFromPathLen(len(testData.RelativePath)))
	entry := createBEIndexFileIO(&testing.T{}, testData) // This is a hack, but works for benchmarks
	
	defer func() {
		// Cleanup
		cleanupMutex.Lock()
		if cleanupInfo, exists := testCleanupDataIndexFile[entry]; exists {
			if cleanupInfo.testDir != "" {
				os.RemoveAll(cleanupInfo.testDir)
			}
			delete(testCleanupDataIndexFile, entry)
		}
		cleanupMutex.Unlock()
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
	
	b.Run("SetHash", func(b *testing.B) {
		hash := [20]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}
		for i := 0; i < b.N; i++ {
			entry.SetHash(hash[:], uint16(HashTypeSHA1))
		}
	})
}