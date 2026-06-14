package dircachefilehash

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
	"unsafe"
)

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

func TestRecoveryValidationProcessor(t *testing.T) {
	// Create a properly formatted binary entry for testing
	// We need to allocate memory for the entry + path data
	pathStr := "test.txt"
	structSize := int(unsafe.Sizeof(binaryEntry{}))
	totalSize := ((structSize + len(pathStr) + 7) / 8) * 8 // 8-byte aligned

	// Allocate aligned memory
	data := make([]byte, totalSize)
	entry := (*binaryEntry)(unsafe.Pointer(&data[0]))

	// Initialise the entry
	*entry = binaryEntry{
		Size:      uint32(totalSize),
		CTimeWall: timeWall(time.Now()),
		MTimeWall: timeWall(time.Now()),
		Dev:       1,
		Ino:       123456,
		Mode:      0644,
		UID:       1000,
		GID:       1000,
		FileSize:  1024,
		HashType:  HashTypeSHA256,
	}
	copy(entry.Hash[:], []byte("abcd1234567890123456789012345678")) // 32 bytes for SHA256

	// Copy path data after the struct
	copy(data[structSize:], []byte(pathStr))

	processor := RecoveryValidationProcessor(2)

	t.Run("ValidEntry", func(t *testing.T) {
		shouldInclude, err := processor(entry, 0, "test.txt")
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if !shouldInclude {
			t.Error("Valid entry should be included")
		}
	})

	t.Run("NilEntry", func(t *testing.T) {
		shouldInclude, err := processor(nil, 0, "test.txt")
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if shouldInclude {
			t.Error("Nil entry should not be included")
		}
	})

	t.Run("EmptyPath", func(t *testing.T) {
		// Create entry with empty path data
		emptyData := make([]byte, structSize+8) // Just struct + padding
		emptyEntry := (*binaryEntry)(unsafe.Pointer(&emptyData[0]))
		*emptyEntry = *entry
		emptyEntry.Size = uint32(len(emptyData))
		// No path data copied - should result in empty path

		shouldInclude, err := processor(emptyEntry, 0, "")
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if shouldInclude {
			t.Error("Entry with empty path should not be included")
		}
	})

	t.Run("AllZeroHash", func(t *testing.T) {
		// Create a copy with all-zero hash
		zeroHashData := make([]byte, totalSize)
		copy(zeroHashData, data)
		zeroHashEntry := (*binaryEntry)(unsafe.Pointer(&zeroHashData[0]))

		// Zero out the hash
		for i := range zeroHashEntry.Hash {
			zeroHashEntry.Hash[i] = 0
		}

		shouldInclude, err := processor(zeroHashEntry, 0, "test.txt")
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if shouldInclude {
			t.Error("Entry with all-zero hash should not be included")
		}
	})

	t.Run("ExcessiveFileSize", func(t *testing.T) {
		// Create a copy with excessive file size. (1<<62)+1 is just above the
		// MaxFileSize ceiling (1<<62) and still fits a signed int64 — 1<<63
		// would overflow int64 now that FileSize is signed.
		largeSizeData := make([]byte, totalSize)
		copy(largeSizeData, data)
		largeSizeEntry := (*binaryEntry)(unsafe.Pointer(&largeSizeData[0]))
		largeSizeEntry.FileSize = (1 << 62) + 1 // Just over the 4-exabyte ceiling

		shouldInclude, err := processor(largeSizeEntry, 0, "test.txt")
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if shouldInclude {
			t.Error("Entry with excessive file size should not be included")
		}
	})

	t.Run("NegativeFileSize", func(t *testing.T) {
		// A corrupt or over-2^63 legacy size reinterprets as a negative int64
		// after the signedness flip; the validator must reject it fail-closed
		// (SC3 corruption floor), not silently propagate it.
		negSizeData := make([]byte, totalSize)
		copy(negSizeData, data)
		negSizeEntry := (*binaryEntry)(unsafe.Pointer(&negSizeData[0]))
		negSizeEntry.FileSize = -1 // Negative = corrupt

		shouldInclude, err := processor(negSizeEntry, 0, "test.txt")
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if shouldInclude {
			t.Error("Entry with negative file size should not be included")
		}
	})
}

func TestPreRecoverySnapshot(t *testing.T) {
	// Create temporary test directory
	tempDir := t.TempDir()

	// Create MetaStore instance
	ms := NewMetaStore(tempDir, tempDir)

	// Ensure the .dcfh directory exists
	dcfhDir := ms.MetaDir
	if err := os.MkdirAll(dcfhDir, 0755); err != nil {
		t.Fatalf("Failed to create dcfh directory: %v", err)
	}

	// Create test files and build index
	testFile1 := filepath.Join(tempDir, "file1.txt")
	if err := os.WriteFile(testFile1, []byte("content1"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Build a proper main index first
	if err := runUpdate(context.Background(), ms, ms.scanRun(), nil); err != nil {
		t.Fatalf("Failed to create initial index: %v", err)
	}

	// Create cache file and scan file for testing
	if err := copyFile(ms.IndexFile, ms.CacheFile); err != nil {
		t.Fatalf("Failed to create cache file: %v", err)
	}

	scanFile := filepath.Join(dcfhDir, "scan-123-456.idx")
	if err := copyFile(ms.IndexFile, scanFile); err != nil {
		t.Fatalf("Failed to create scan file: %v", err)
	}

	// Test the pre-recovery snapshot function directly
	err := ms.createPreRecoverySnapshot(2)
	if err != nil {
		t.Fatalf("createPreRecoverySnapshot failed: %v", err)
	}

	// Verify pre-recovery snapshot was created
	recoveryDir := filepath.Join(dcfhDir, "recovery")
	if _, err := os.Stat(recoveryDir); os.IsNotExist(err) {
		t.Error("Recovery snapshot directory was not created")
	}

	// Verify files were backed up to recovery directory
	recoveryFiles, err := filepath.Glob(filepath.Join(recoveryDir, "*.idx"))
	if err != nil {
		t.Fatalf("Failed to check for recovery snapshot files: %v", err)
	}
	if len(recoveryFiles) < 3 { // Should have main.idx, cache.idx, and scan-123-456.idx
		t.Errorf("Expected at least 3 files in recovery directory, got %d", len(recoveryFiles))
	}

	// Verify specific files were backed up
	expectedFiles := []string{"main.idx", "cache.idx", "scan-123-456.idx"}
	for _, expectedFile := range expectedFiles {
		expectedPath := filepath.Join(recoveryDir, expectedFile)
		if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
			t.Errorf("Expected file %s was not backed up to recovery directory", expectedFile)
		}
	}

	// Verify file metadata preservation by checking one file
	originalMainStat, err := os.Stat(ms.IndexFile)
	if err != nil {
		t.Fatalf("Failed to stat original main.idx: %v", err)
	}

	backupMainPath := filepath.Join(recoveryDir, "main.idx")
	backupMainStat, err := os.Stat(backupMainPath)
	if err != nil {
		t.Fatalf("Failed to stat backup main.idx: %v", err)
	}

	// Check that modification times are preserved (within a small margin)
	if originalMainStat.ModTime().Sub(backupMainStat.ModTime()).Abs() > time.Second {
		t.Errorf("Modification time not preserved: original %v, backup %v",
			originalMainStat.ModTime(), backupMainStat.ModTime())
	}

	// Check that file sizes match
	if originalMainStat.Size() != backupMainStat.Size() {
		t.Errorf("File size mismatch: original %d, backup %d",
			originalMainStat.Size(), backupMainStat.Size())
	}
}
