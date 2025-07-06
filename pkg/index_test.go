package dircachefilehash

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"
)

func TestIndexHeader_SetHeader(t *testing.T) {
	var header indexHeader
	signature := [4]byte{'d', 'c', 'f', 'h'}
	version := uint32(CurrentIndexVersion)
	entryCount := uint32(10)
	flags := uint16(0)
	checksumType := uint16(HashTypeSHA1)

	header.SetHeader(signature, version, entryCount, flags, checksumType)

	if header.Signature != signature {
		t.Errorf("Expected signature %v, got %v", signature, header.Signature)
	}
	if header.Version != version {
		t.Errorf("Expected version %d, got %d", version, header.Version)
	}
	if header.EntryCount != entryCount {
		t.Errorf("Expected entry count %d, got %d", entryCount, header.EntryCount)
	}
	if header.Flags != flags {
		t.Errorf("Expected flags %d, got %d", flags, header.Flags)
	}
	if header.ChecksumType != checksumType {
		t.Errorf("Expected checksum type %d, got %d", checksumType, header.ChecksumType)
	}
	if header.ByteOrder != ByteOrderMagic {
		t.Errorf("Expected byte order %x, got %x", ByteOrderMagic, header.ByteOrder)
	}
}

func TestIndexHeader_CleanMethods(t *testing.T) {
	var header indexHeader
	
	// Test initial state (not clean)
	if header.isClean() {
		t.Error("Expected header to be initially not clean")
	}

	// Test setClean
	header.setClean()
	if !header.isClean() {
		t.Error("Expected header to be clean after setClean()")
	}

	// Test clearClean
	header.clearClean()
	if header.isClean() {
		t.Error("Expected header to be not clean after clearClean()")
	}
}

func TestIndexHeader_ValidateSignature(t *testing.T) {
	var header indexHeader
	signature := [4]byte{'d', 'c', 'f', 'h'}
	header.Signature = signature

	// Test valid signature
	if err := header.ValidateSignature(signature); err != nil {
		t.Errorf("Expected no error for valid signature, got %v", err)
	}

	// Test invalid signature
	wrongSig := [4]byte{'t', 'e', 's', 't'}
	if err := header.ValidateSignature(wrongSig); err == nil {
		t.Error("Expected error for invalid signature")
	}
}

func TestIndexHeader_ValidateByteOrder(t *testing.T) {
	var header indexHeader
	header.ByteOrder = ByteOrderMagic

	// Test valid byte order
	if err := header.ValidateByteOrder(); err != nil {
		t.Errorf("Expected no error for valid byte order, got %v", err)
	}

	// Test invalid byte order
	header.ByteOrder = 0x1234567890abcdef
	if err := header.ValidateByteOrder(); err == nil {
		t.Error("Expected error for invalid byte order")
	}
}

func TestIndexHeader_ValidateVersion(t *testing.T) {
	var header indexHeader
	version := uint32(1)
	header.Version = version

	// Test valid version
	if err := header.ValidateVersion(version); err != nil {
		t.Errorf("Expected no error for valid version, got %v", err)
	}

	// Test invalid version
	if err := header.ValidateVersion(version + 1); err == nil {
		t.Error("Expected error for invalid version")
	}
}

func TestDirectoryCache_scanForTempIndices(t *testing.T) {
	// Create temporary directory structure
	tempDir := t.TempDir()
	dcfhDir := filepath.Join(tempDir, ".dcfh")
	if err := os.MkdirAll(dcfhDir, 0755); err != nil {
		t.Fatalf("Failed to create test directory: %v", err)
	}

	// Create test files
	testFiles := []string{
		"main.idx",           // Not a temp file
		"cache.idx",          // Not a temp file
		"scan-1234-5678.idx", // Temp file
		"tmp-9999-1111.idx",  // Temp file
		"scan-abc-def.idx",   // Temp file (different format but should match)
		"other.txt",          // Not an index file
	}

	for _, file := range testFiles {
		if err := os.WriteFile(filepath.Join(dcfhDir, file), []byte("test"), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", file, err)
		}
	}

	// Create DirectoryCache instance
	dc := &DirectoryCache{
		IndexFile: filepath.Join(dcfhDir, "main.idx"),
	}

	// Test scanForTempIndices
	tempFiles, err := dc.scanForTempIndices()
	if err != nil {
		t.Fatalf("scanForTempIndices failed: %v", err)
	}

	expectedTempFiles := []string{
		"scan-1234-5678.idx",
		"tmp-9999-1111.idx",
		"scan-abc-def.idx",
	}

	if len(tempFiles) != len(expectedTempFiles) {
		t.Errorf("Expected %d temp files, got %d", len(expectedTempFiles), len(tempFiles))
	}

	// Convert to map for easier checking
	foundFiles := make(map[string]bool)
	for _, file := range tempFiles {
		foundFiles[file] = true
	}

	for _, expected := range expectedTempFiles {
		if !foundFiles[expected] {
			t.Errorf("Expected to find temp file %s", expected)
		}
	}
}

func TestBinaryEntry_Size(t *testing.T) {
	// Test that binaryEntry size calculation is correct
	expectedSize := int(unsafe.Sizeof(binaryEntry{}))
	
	// This is more of a documentation test to ensure we know the size
	if expectedSize == 0 {
		t.Error("binaryEntry size should not be zero")
	}
	
	// The exact size may vary by platform, but should be reasonable
	if expectedSize > 200 || expectedSize < 50 {
		t.Errorf("binaryEntry size %d seems unreasonable", expectedSize)
	}
}