package dircachefilehash

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// BinaryEntryTestSuite provides implementation-neutral testing for all BinaryEntryInterface implementations
type BinaryEntryTestSuite struct {
	Name        string
	CreateEntry func(t *testing.T, testData *TestEntryData) BinaryEntryInterface
	CleanupEntry func(t *testing.T, entry BinaryEntryInterface)
	SupportsSetHash bool // Whether this implementation supports hash updates
	SupportsSetDeleted bool // Whether this implementation supports deletion flag updates
	IsEphemeral bool // Whether entries can become invalid (for error testing)
}

// TestEntryData represents test data for creating binary entries
type TestEntryData struct {
	// Basic file metadata
	RelativePath string
	Size         uint32
	CTimeWall    uint64
	MTimeWall    uint64
	Dev          uint32
	Ino          uint32
	Mode         uint32
	UID          uint32
	GID          uint32
	FileSize     uint64
	
	// Hash data
	HashType     uint16
	Hash         [20]byte
	
	// Entry flags
	EntryFlags   uint32
	IsDeleted    bool
}

// CreateTestData creates standard test data for consistent testing
func CreateTestData() *TestEntryData {
	return &TestEntryData{
		RelativePath: "test/file.txt",
		Size:         128,
		CTimeWall:    encodeWallTime(1234567890, 0),
		MTimeWall:    encodeWallTime(1234567900, 0),
		Dev:          1,
		Ino:          12345,
		Mode:         0644,
		UID:          1000,
		GID:          1000,
		FileSize:     1024,
		HashType:     uint16(HashTypeSHA1),
		Hash:         [20]byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef, 0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef, 0x01, 0x23, 0x45, 0x67},
		EntryFlags:   0,
		IsDeleted:    false,
	}
}

// CreateDeletedTestData creates test data for a deleted entry
func CreateDeletedTestData() *TestEntryData {
	data := CreateTestData()
	data.RelativePath = "deleted/file.txt"
	data.IsDeleted = true
	data.EntryFlags = 1 // Assuming bit 0 is delete flag
	return data
}

// RunAllTests runs the complete test suite for a BinaryEntryInterface implementation
func (suite *BinaryEntryTestSuite) RunAllTests(t *testing.T) {
	t.Run(suite.Name, func(t *testing.T) {
		t.Run("BasicFieldAccess", suite.TestBasicFieldAccess)
		t.Run("DerivedMethods", suite.TestDerivedMethods)
		t.Run("Locking", suite.TestLocking)
		t.Run("ConcurrentAccess", suite.TestConcurrentAccess)
		
		if suite.SupportsSetHash {
			t.Run("HashUpdates", suite.TestHashUpdates)
		}
		
		if suite.SupportsSetDeleted {
			t.Run("DeletionUpdates", suite.TestDeletionUpdates)
		}
		
		if suite.IsEphemeral {
			t.Run("EphemeralBehavior", suite.TestEphemeralBehavior)
		}
		
		t.Run("ErrorHandling", suite.TestErrorHandling)
		t.Run("BatchOperations", suite.TestBatchOperations)
	})
}

// TestBasicFieldAccess tests all basic field accessor methods
func (suite *BinaryEntryTestSuite) TestBasicFieldAccess(t *testing.T) {
	testData := CreateTestData()
	entry := suite.CreateEntry(t, testData)
	defer suite.CleanupEntry(t, entry)
	
	// Test Size
	if size, err := entry.Size(); err != nil {
		t.Errorf("Size() returned error: %v", err)
	} else if size != testData.Size {
		t.Errorf("Size() = %d, want %d", size, testData.Size)
	}
	
	// Test CTimeWall
	if ctime, err := entry.CTimeWall(); err != nil {
		t.Errorf("CTimeWall() returned error: %v", err)
	} else if ctime != testData.CTimeWall {
		t.Errorf("CTimeWall() = %d, want %d", ctime, testData.CTimeWall)
	}
	
	// Test MTimeWall
	if mtime, err := entry.MTimeWall(); err != nil {
		t.Errorf("MTimeWall() returned error: %v", err)
	} else if mtime != testData.MTimeWall {
		t.Errorf("MTimeWall() = %d, want %d", mtime, testData.MTimeWall)
	}
	
	// Test Dev
	if dev, err := entry.Dev(); err != nil {
		t.Errorf("Dev() returned error: %v", err)
	} else if dev != testData.Dev {
		t.Errorf("Dev() = %d, want %d", dev, testData.Dev)
	}
	
	// Test Ino
	if ino, err := entry.Ino(); err != nil {
		t.Errorf("Ino() returned error: %v", err)
	} else if ino != testData.Ino {
		t.Errorf("Ino() = %d, want %d", ino, testData.Ino)
	}
	
	// Test Mode
	if mode, err := entry.Mode(); err != nil {
		t.Errorf("Mode() returned error: %v", err)
	} else if mode != testData.Mode {
		t.Errorf("Mode() = %d, want %d", mode, testData.Mode)
	}
	
	// Test UID
	if uid, err := entry.UID(); err != nil {
		t.Errorf("UID() returned error: %v", err)
	} else if uid != testData.UID {
		t.Errorf("UID() = %d, want %d", uid, testData.UID)
	}
	
	// Test GID
	if gid, err := entry.GID(); err != nil {
		t.Errorf("GID() returned error: %v", err)
	} else if gid != testData.GID {
		t.Errorf("GID() = %d, want %d", gid, testData.GID)
	}
	
	// Test FileSize
	if fileSize, err := entry.FileSize(); err != nil {
		t.Errorf("FileSize() returned error: %v", err)
	} else if fileSize != testData.FileSize {
		t.Errorf("FileSize() = %d, want %d", fileSize, testData.FileSize)
	}
	
	// Test HashType
	if hashType, err := entry.HashType(); err != nil {
		t.Errorf("HashType() returned error: %v", err)
	} else if hashType != testData.HashType {
		t.Errorf("HashType() = %d, want %d", hashType, testData.HashType)
	}
	
	// Test Hash
	if hash, err := entry.Hash(); err != nil {
		t.Errorf("Hash() returned error: %v", err)
	} else if hash != testData.Hash {
		t.Errorf("Hash() = %x, want %x", hash, testData.Hash)
	}
	
	// Test EntryFlags
	if flags, err := entry.EntryFlags(); err != nil {
		t.Errorf("EntryFlags() returned error: %v", err)
	} else if flags != testData.EntryFlags {
		t.Errorf("EntryFlags() = %d, want %d", flags, testData.EntryFlags)
	}
}

// TestDerivedMethods tests derived methods like RelativePath, HashString, IsDeleted
func (suite *BinaryEntryTestSuite) TestDerivedMethods(t *testing.T) {
	testData := CreateTestData()
	entry := suite.CreateEntry(t, testData)
	defer suite.CleanupEntry(t, entry)
	
	// Test RelativePath
	if path, err := entry.RelativePath(); err != nil {
		t.Errorf("RelativePath() returned error: %v", err)
	} else if path != testData.RelativePath {
		t.Errorf("RelativePath() = %q, want %q", path, testData.RelativePath)
	}
	
	// Test HashString
	expectedHashString := fmt.Sprintf("%x", testData.Hash)
	if hashStr, err := entry.HashString(); err != nil {
		t.Errorf("HashString() returned error: %v", err)
	} else if hashStr != expectedHashString {
		t.Errorf("HashString() = %q, want %q", hashStr, expectedHashString)
	}
	
	// Test IsDeleted
	if deleted, err := entry.IsDeleted(); err != nil {
		t.Errorf("IsDeleted() returned error: %v", err)
	} else if deleted != testData.IsDeleted {
		t.Errorf("IsDeleted() = %t, want %t", deleted, testData.IsDeleted)
	}
	
	// Test with deleted entry
	deletedData := CreateDeletedTestData()
	deletedEntry := suite.CreateEntry(t, deletedData)
	defer suite.CleanupEntry(t, deletedEntry)
	
	if deleted, err := deletedEntry.IsDeleted(); err != nil {
		t.Errorf("IsDeleted() on deleted entry returned error: %v", err)
	} else if !deleted {
		t.Errorf("IsDeleted() on deleted entry = false, want true")
	}
}

// TestLocking tests manual locking functionality
func (suite *BinaryEntryTestSuite) TestLocking(t *testing.T) {
	testData := CreateTestData()
	entry := suite.CreateEntry(t, testData)
	defer suite.CleanupEntry(t, entry)
	
	// Test that locking doesn't panic or hang
	entry.RLock()
	entry.RUnlock()
	
	entry.Lock()
	entry.Unlock()
	
	// Test nested read locks (should work with RWMutex)
	entry.RLock()
	entry.RLock()
	entry.RUnlock()
	entry.RUnlock()
}

// TestConcurrentAccess tests concurrent access patterns
func (suite *BinaryEntryTestSuite) TestConcurrentAccess(t *testing.T) {
	testData := CreateTestData()
	entry := suite.CreateEntry(t, testData)
	defer suite.CleanupEntry(t, entry)
	
	const numReaders = 10
	const numOperations = 100
	
	var wg sync.WaitGroup
	errors := make(chan error, numReaders*numOperations)
	
	// Start multiple readers
	for i := 0; i < numReaders; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				// Test concurrent read access
				if path, err := entry.RelativePath(); err != nil {
					errors <- fmt.Errorf("concurrent RelativePath() error: %v", err)
					return
				} else if path != testData.RelativePath {
					errors <- fmt.Errorf("concurrent RelativePath() = %q, want %q", path, testData.RelativePath)
					return
				}
				
				// Brief pause to encourage race conditions
				time.Sleep(time.Microsecond)
			}
		}()
	}
	
	wg.Wait()
	close(errors)
	
	// Check for errors
	for err := range errors {
		t.Error(err)
	}
}

// TestHashUpdates tests hash update functionality (if supported)
func (suite *BinaryEntryTestSuite) TestHashUpdates(t *testing.T) {
	if !suite.SupportsSetHash {
		t.Skip("Implementation does not support hash updates")
	}
	
	testData := CreateTestData()
	entry := suite.CreateEntry(t, testData)
	defer suite.CleanupEntry(t, entry)
	
	// Test hash update
	newHash := [20]byte{0xff, 0xee, 0xdd, 0xcc, 0xbb, 0xaa, 0x99, 0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11, 0x00, 0xff, 0xee, 0xdd, 0xcc}
	newHashType := uint16(HashTypeSHA256)
	
	if err := entry.SetHash(newHash[:], newHashType); err != nil {
		t.Errorf("SetHash() returned error: %v", err)
	}
	
	// Verify hash was updated
	if hash, err := entry.Hash(); err != nil {
		t.Errorf("Hash() after update returned error: %v", err)
	} else if hash != newHash {
		t.Errorf("Hash() after update = %x, want %x", hash, newHash)
	}
	
	// Verify hash type was updated
	if hashType, err := entry.HashType(); err != nil {
		t.Errorf("HashType() after update returned error: %v", err)
	} else if hashType != newHashType {
		t.Errorf("HashType() after update = %d, want %d", hashType, newHashType)
	}
}

// TestDeletionUpdates tests deletion flag update functionality (if supported)
func (suite *BinaryEntryTestSuite) TestDeletionUpdates(t *testing.T) {
	if !suite.SupportsSetDeleted {
		t.Skip("Implementation does not support deletion updates")
	}
	
	testData := CreateTestData()
	entry := suite.CreateEntry(t, testData)
	defer suite.CleanupEntry(t, entry)
	
	// Initially should not be deleted
	if deleted, err := entry.IsDeleted(); err != nil {
		t.Errorf("Initial IsDeleted() returned error: %v", err)
	} else if deleted {
		t.Errorf("Initial IsDeleted() = true, want false")
	}
	
	// Set as deleted
	if err := entry.SetDeleted(true); err != nil {
		t.Errorf("SetDeleted(true) returned error: %v", err)
	}
	
	// Verify deletion flag was set
	if deleted, err := entry.IsDeleted(); err != nil {
		t.Errorf("IsDeleted() after SetDeleted(true) returned error: %v", err)
	} else if !deleted {
		t.Errorf("IsDeleted() after SetDeleted(true) = false, want true")
	}
	
	// Clear deletion flag
	if err := entry.SetDeleted(false); err != nil {
		t.Errorf("SetDeleted(false) returned error: %v", err)
	}
	
	// Verify deletion flag was cleared
	if deleted, err := entry.IsDeleted(); err != nil {
		t.Errorf("IsDeleted() after SetDeleted(false) returned error: %v", err)
	} else if deleted {
		t.Errorf("IsDeleted() after SetDeleted(false) = true, want false")
	}
}

// TestEphemeralBehavior tests ephemeral entry behavior (if applicable)
func (suite *BinaryEntryTestSuite) TestEphemeralBehavior(t *testing.T) {
	if !suite.IsEphemeral {
		t.Skip("Implementation is not ephemeral")
	}
	
	testData := CreateTestData()
	entry := suite.CreateEntry(t, testData)
	defer suite.CleanupEntry(t, entry)
	
	// Test IsValid() method
	if !entry.IsValid() {
		t.Error("IsValid() = false for new entry, want true")
	}
	
	// Note: Testing actual ephemeral failures (munmap, mremap) would require
	// implementation-specific setup. This is a basic validation that the
	// interface supports ephemeral behavior.
}

// TestErrorHandling tests error handling in various scenarios
func (suite *BinaryEntryTestSuite) TestErrorHandling(t *testing.T) {
	testData := CreateTestData()
	entry := suite.CreateEntry(t, testData)
	defer suite.CleanupEntry(t, entry)
	
	// For non-ephemeral implementations, all operations should succeed
	// For ephemeral implementations, we test what we can without actually
	// invalidating the entry (which would be implementation-specific)
	
	methods := []struct {
		name string
		test func() error
	}{
		{"Size", func() error { _, err := entry.Size(); return err }},
		{"CTimeWall", func() error { _, err := entry.CTimeWall(); return err }},
		{"MTimeWall", func() error { _, err := entry.MTimeWall(); return err }},
		{"Dev", func() error { _, err := entry.Dev(); return err }},
		{"Ino", func() error { _, err := entry.Ino(); return err }},
		{"Mode", func() error { _, err := entry.Mode(); return err }},
		{"UID", func() error { _, err := entry.UID(); return err }},
		{"GID", func() error { _, err := entry.GID(); return err }},
		{"FileSize", func() error { _, err := entry.FileSize(); return err }},
		{"HashType", func() error { _, err := entry.HashType(); return err }},
		{"Hash", func() error { _, err := entry.Hash(); return err }},
		{"EntryFlags", func() error { _, err := entry.EntryFlags(); return err }},
		{"RelativePath", func() error { _, err := entry.RelativePath(); return err }},
		{"HashString", func() error { _, err := entry.HashString(); return err }},
		{"IsDeleted", func() error { _, err := entry.IsDeleted(); return err }},
	}
	
	for _, method := range methods {
		if err := method.test(); err != nil && !suite.IsEphemeral {
			t.Errorf("%s() returned unexpected error: %v", method.name, err)
		}
	}
}

// TestBatchOperations tests batch operations with manual locking
func (suite *BinaryEntryTestSuite) TestBatchOperations(t *testing.T) {
	testData := CreateTestData()
	entry := suite.CreateEntry(t, testData)
	defer suite.CleanupEntry(t, entry)
	
	// Test batch read operations under single lock
	entry.RLock()
	
	path, err1 := entry.RelativePath()
	size, err2 := entry.Size()
	hash, err3 := entry.HashString()
	
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
	
	// Verify results are correct
	if path != testData.RelativePath {
		t.Errorf("Batch RelativePath() = %q, want %q", path, testData.RelativePath)
	}
	if size != testData.Size {
		t.Errorf("Batch Size() = %d, want %d", size, testData.Size)
	}
	expectedHashString := fmt.Sprintf("%x", testData.Hash)
	if hash != expectedHashString {
		t.Errorf("Batch HashString() = %q, want %q", hash, expectedHashString)
	}
}

// BenchmarkFieldAccess benchmarks field access performance
func (suite *BinaryEntryTestSuite) BenchmarkFieldAccess(b *testing.B, createFn func() BinaryEntryInterface, cleanupFn func(BinaryEntryInterface)) {
	entry := createFn()
	defer cleanupFn(entry)
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		// Benchmark commonly accessed fields
		entry.RelativePath()
		entry.Size()
		entry.HashString()
		entry.IsDeleted()
	}
}

// BenchmarkConcurrentAccess benchmarks concurrent access performance
func (suite *BinaryEntryTestSuite) BenchmarkConcurrentAccess(b *testing.B, createFn func() BinaryEntryInterface, cleanupFn func(BinaryEntryInterface)) {
	entry := createFn()
	defer cleanupFn(entry)
	
	b.ResetTimer()
	
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// Test concurrent read access
			entry.RelativePath()
			entry.HashString()
		}
	})
}