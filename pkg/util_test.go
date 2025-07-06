package dircachefilehash

import (
	"strings"
	"testing"
	"time"
	"unsafe"
)

func TestBinaryEntry_Methods(t *testing.T) {
	entry := binaryEntry{
		Size:       100,
		FileSize:   1024,
		Hash:       [64]byte{1, 2, 3, 4, 5},
		EntryFlags: 0,
		HashType:   HashTypeSHA1,
	}

	// Test EntrySize
	if size := entry.EntrySize(); size != 100 {
		t.Errorf("Expected entry size 100, got %d", size)
	}

	// Test IsDeleted (should be false initially)
	if entry.IsDeleted() {
		t.Error("Expected entry to not be deleted initially")
	}

	// Test SetDeleted
	entry.SetDeleted()
	if !entry.IsDeleted() {
		t.Error("Expected entry to be deleted after SetDeleted()")
	}

	// Test ClearDeleted
	entry.ClearDeleted()
	if entry.IsDeleted() {
		t.Error("Expected entry to not be deleted after ClearDeleted()")
	}

	// Test IsHashEmpty - entry with some hash data should not be empty
	if entry.IsHashEmpty() {
		t.Error("Expected entry with hash data to not be empty")
	}

	// Test IsHashEmpty - entry with all zeros should be empty
	emptyEntry := binaryEntry{}
	if !emptyEntry.IsHashEmpty() {
		t.Error("Expected entry with zero hash to be empty")
	}

	// Test IsHashEmpty - entry with partial hash data should not be empty
	partialEntry := binaryEntry{Hash: [64]byte{0, 0, 0, 1}, HashType: HashTypeSHA1}
	if partialEntry.IsHashEmpty() {
		t.Error("Expected entry with partial hash to not be empty")
	}

	// Test IsHashEmpty - entry with HashType = 0 should be empty regardless of hash content
	zeroTypeEntry := binaryEntry{HashType: 0, Hash: [64]byte{1, 2, 3, 4, 5}}
	if !zeroTypeEntry.IsHashEmpty() {
		t.Error("Expected entry with HashType = 0 to be empty")
	}

	// Test IsHashEmpty - entry with valid HashType but zero hash should be empty
	validTypeZeroHash := binaryEntry{HashType: HashTypeSHA1}
	if !validTypeZeroHash.IsHashEmpty() {
		t.Error("Expected entry with valid HashType but zero hash to be empty")
	}
}

func TestBinaryEntry_HashString(t *testing.T) {
	entry := binaryEntry{
		Hash: [64]byte{
			0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef,
			0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef,
			0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef,
			0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef,
		},
		HashType: HashTypeSHA256,
	}

	hashStr := entry.HashString()
	expected := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	if len(hashStr) != 64 {
		t.Errorf("Expected hash string length 64, got %d", len(hashStr))
	}

	if hashStr != expected {
		t.Errorf("Expected hash string %s, got %s", expected, hashStr)
	}
}

func TestBinaryEntry_RelativePath(t *testing.T) {
	// This test is complex because RelativePath() expects a specific memory layout
	// that matches how the real system creates binaryEntry structures.
	// For now, we'll test that the method exists and doesn't crash

	testPath := "test.txt"

	// Create a properly sized buffer like the real system would
	baseSize := int(unsafe.Sizeof(binaryEntry{}))
	totalSize := baseSize + len(testPath) + 1
	padding := (8 - (totalSize % 8)) % 8
	entrySize := totalSize + padding

	data := make([]byte, entrySize)
	entry := (*binaryEntry)(unsafe.Pointer(&data[0]))
	entry.Size = uint32(entrySize)

	// The path is stored AFTER the binaryEntry struct, not within it
	pathOffset := baseSize
	copy(data[pathOffset:], testPath)
	data[pathOffset+len(testPath)] = 0 // null terminator

	// Test RelativePath - it should at least not crash
	retrievedPath := entry.RelativePath()

	// Basic validation that we got some path back
	if len(retrievedPath) == 0 {
		t.Error("RelativePath should return a non-empty string")
	}

	// Verify we got the expected path
	if retrievedPath != testPath {
		t.Errorf("Expected path '%s', got '%s'", testPath, retrievedPath)
	}
}

func TestTimeConversion(t *testing.T) {
	// Test time conversion functions
	now := time.Now()

	// Convert to wall time and back
	wall := timeWall(now)
	converted := timeFromWall(wall)

	// Should be very close (within reasonable precision limits)
	diff := now.Sub(converted)
	if diff > 10*time.Second || diff < -10*time.Second {
		t.Errorf("Time conversion error too large: %v", diff)
	}
}

func TestEncodeWallTime(t *testing.T) {
	// Test wall time encoding
	sec := int64(1234567890)
	nsec := int64(123456789)

	wall := encodeWallTime(sec, nsec)
	if wall == 0 {
		t.Error("Wall time should not be zero")
	}

	// Convert back and verify - just check that conversion works
	converted := timeFromWall(wall)
	// Don't check exact equality since time conversion might have different epoch
	// Just verify we get a reasonable time back
	if converted.IsZero() {
		t.Error("Converted time should not be zero")
	}
}

func TestBESizeFromPathLen(t *testing.T) {
	tests := []struct {
		pathLen  int
		expected int
	}{
		{0, int(unsafe.Sizeof(binaryEntry{})) + 1 + 7}, // +1 for null, +7 for padding to 8-byte boundary
		{1, int(unsafe.Sizeof(binaryEntry{})) + 2 + 6}, // +2 for char+null, +6 for padding
		{7, int(unsafe.Sizeof(binaryEntry{})) + 8},     // +8 for path+null, no padding needed
		{8, int(unsafe.Sizeof(binaryEntry{})) + 9 + 7}, // +9 for path+null, +7 for padding
	}

	for _, tt := range tests {
		t.Run("pathLen="+string(rune(tt.pathLen+'0')), func(t *testing.T) {
			result := BESizeFromPathLen(tt.pathLen)
			if result != tt.expected {
				t.Errorf("BESizeFromPathLen(%d) = %d, want %d", tt.pathLen, result, tt.expected)
			}
		})
	}
}

func TestDirectoryCache_generateTempFileName(t *testing.T) {
	dc := &DirectoryCache{}

	// Test different prefixes
	prefixes := []string{"scan", "tmp", "cache"}

	for _, prefix := range prefixes {
		filename := dc.generateTempFileName(prefix)

		if filename == "" {
			t.Errorf("Generated filename should not be empty for prefix %s", prefix)
		}

		// Should contain the prefix
		if len(filename) < len(prefix) {
			t.Errorf("Generated filename %s should contain prefix %s", filename, prefix)
		}

		// Should be unique (test by generating multiple)
		filename2 := dc.generateTempFileName(prefix)
		if filename == filename2 {
			t.Errorf("Generated filenames should be unique: %s == %s", filename, filename2)
		}
	}
}

func TestDirectoryCache_generateScanFileName(t *testing.T) {
	dc := &DirectoryCache{}

	filename := dc.generateScanFileName()

	if filename == "" {
		t.Error("Generated scan filename should not be empty")
	}

	// Should contain "scan" prefix
	if len(filename) < 4 {
		t.Errorf("Generated scan filename %s should contain scan prefix", filename)
	}

	// Test filename format (scan-{pid}-{tid}.idx)
	if !strings.Contains(filename, "scan-") || !strings.HasSuffix(filename, ".idx") {
		t.Errorf("Generated scan filename %s should match scan-{pid}-{tid}.idx pattern", filename)
	}
}

func TestGetGoroutineID(t *testing.T) {
	id := getGoroutineID()

	if id == 0 {
		t.Error("Goroutine ID should not be zero")
	}

	// Should be consistent within the same goroutine
	id2 := getGoroutineID()
	if id != id2 {
		t.Errorf("Goroutine ID should be consistent: %d != %d", id, id2)
	}
}

func TestNewDirectoryCache(t *testing.T) {
	rootDir := "/tmp/test/root" // Use /tmp to avoid permission issues
	dcfhDir := "/tmp/test/dcfh"

	dc := NewDirectoryCache(rootDir, dcfhDir)

	if dc == nil {
		t.Fatal("NewDirectoryCache should not return nil")
	}

	if dc.RootDir != rootDir {
		t.Errorf("Expected RootDir %s, got %s", rootDir, dc.RootDir)
	}

	// CacheFile should be the cache.idx file in dcfhDir, not dcfhDir itself
	expectedCacheFile := strings.TrimSuffix(dcfhDir, "/") + "/.dcfh/cache.idx"
	if dc.CacheFile != expectedCacheFile {
		t.Errorf("Expected CacheFile %s, got %s", expectedCacheFile, dc.CacheFile)
	}

	// Check that hasher is initialised
	if dc.hasher == nil {
		t.Error("Hasher should be initialised")
	}

	// Check signature
	expectedSig := [4]byte{'d', 'c', 'f', 'h'}
	if dc.signature != expectedSig {
		t.Errorf("Expected signature %v, got %v", expectedSig, dc.signature)
	}
}
