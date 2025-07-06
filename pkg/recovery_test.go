package dircachefilehash

import (
	"os"
	"path/filepath"
	"testing"
	"time"
	"unsafe"
)

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
		// Create a copy with excessive file size
		largeSizeData := make([]byte, totalSize)
		copy(largeSizeData, data)
		largeSizeEntry := (*binaryEntry)(unsafe.Pointer(&largeSizeData[0]))
		largeSizeEntry.FileSize = 1 << 63 // Very large size
		
		shouldInclude, err := processor(largeSizeEntry, 0, "test.txt")
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		if shouldInclude {
			t.Error("Entry with excessive file size should not be included")
		}
	})
}

func TestRecoveryWorkflow(t *testing.T) {
	// Create temporary test directory
	tempDir := t.TempDir()
	
	// Create DirectoryCache instance (it will create .dcfh directory structure)
	dc := NewDirectoryCache(tempDir, tempDir)
	
	t.Run("RecoverFromNonExistentFile", func(t *testing.T) {
		dcfhDir := filepath.Dir(dc.IndexFile)
		nonExistentPath := filepath.Join(dcfhDir, "nonexistent.idx")
		err := dc.RecoverFromIndex(nonExistentPath, 1)
		if err == nil {
			t.Error("Expected error when recovering from non-existent file")
		}
	})
	
	t.Run("RecoverFromScanFilesEmpty", func(t *testing.T) {
		err := dc.RecoverFromScanFiles(1)
		if err == nil {
			t.Error("Expected error when no scan files exist")
		}
	})
	
	t.Run("AutoRecoverEmpty", func(t *testing.T) {
		err := dc.AutoRecover(1)
		if err == nil {
			t.Error("Expected error when no recovery sources available")
		}
	})
}

func TestScanFileInfoSorting(t *testing.T) {
	// Create temporary test directory
	tempDir := t.TempDir()
	
	// Create DirectoryCache instance (it will create .dcfh directory structure)
	dc := NewDirectoryCache(tempDir, tempDir)
	
	// Get the actual .dcfh directory path from the IndexFile
	dcfhDir := filepath.Dir(dc.IndexFile)
	
	// Ensure the .dcfh directory exists
	if err := os.MkdirAll(dcfhDir, 0755); err != nil {
		t.Fatalf("Failed to create dcfh directory: %v", err)
	}
	
	// Create test scan files with different timestamps in the correct .dcfh directory
	now := time.Now()
	testFiles := []struct {
		name    string
		modTime time.Time
	}{
		{"scan-123-456.idx", now.Add(-time.Hour)},   // Oldest
		{"scan-789-012.idx", now.Add(-time.Minute)}, // Newest
		{"scan-345-678.idx", now.Add(-time.Hour/2)}, // Middle
	}
	
	for _, tf := range testFiles {
		filePath := filepath.Join(dcfhDir, tf.name)
		if err := os.WriteFile(filePath, []byte("test"), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", tf.name, err)
		}
		if err := os.Chtimes(filePath, tf.modTime, tf.modTime); err != nil {
			t.Fatalf("Failed to set modification time for %s: %v", tf.name, err)
		}
	}
	
	// Find scan files
	scanFiles, err := dc.findScanIndexFiles()
	if err != nil {
		t.Fatalf("Failed to find scan files: %v", err)
	}
	
	if len(scanFiles) != 3 {
		t.Errorf("Expected 3 scan files, got %d", len(scanFiles))
	}
	
	// Verify they're sorted by modification time (newest first)
	if len(scanFiles) >= 2 {
		if scanFiles[0].ModTime.Before(scanFiles[1].ModTime) {
			t.Error("Scan files should be sorted by modification time (newest first)")
		}
	}
	
	// Verify the newest file is first
	if len(scanFiles) > 0 {
		expectedNewest := "scan-789-012.idx"
		if filepath.Base(scanFiles[0].Path) != expectedNewest {
			t.Errorf("Expected newest file %s to be first, got %s", 
				expectedNewest, filepath.Base(scanFiles[0].Path))
		}
	}
}

func TestCreateEmptyMainIndex(t *testing.T) {
	// Create temporary test directory
	tempDir := t.TempDir()
	
	// Create DirectoryCache instance
	dc := NewDirectoryCache(tempDir, tempDir)
	
	// Ensure the .dcfh directory exists
	dcfhDir := filepath.Dir(dc.IndexFile)
	if err := os.MkdirAll(dcfhDir, 0755); err != nil {
		t.Fatalf("Failed to create dcfh directory: %v", err)
	}
	
	// Test creating empty main index
	err := dc.CreateEmptyMainIndex()
	if err != nil {
		t.Fatalf("CreateEmptyMainIndex failed: %v", err)
	}
	
	// Verify the index file was created
	if _, err := os.Stat(dc.IndexFile); os.IsNotExist(err) {
		t.Error("Main index file was not created")
	}
	
	// Verify the index is valid by loading it
	mainSkiplist, err := dc.LoadMainIndex()
	if err != nil {
		t.Fatalf("Failed to load created main index: %v", err)
	}
	
	// Verify it's empty
	if mainSkiplist.Length() != 0 {
		t.Errorf("Expected empty index, but got %d entries", mainSkiplist.Length())
	}
	
	// Verify cache file was removed
	if _, err := os.Stat(dc.CacheFile); !os.IsNotExist(err) {
		t.Error("Cache file should have been removed")
	}
}

func TestPreRecoverySnapshot(t *testing.T) {
	// Create temporary test directory
	tempDir := t.TempDir()
	
	// Create DirectoryCache instance
	dc := NewDirectoryCache(tempDir, tempDir)
	
	// Ensure the .dcfh directory exists
	dcfhDir := filepath.Dir(dc.IndexFile)
	if err := os.MkdirAll(dcfhDir, 0755); err != nil {
		t.Fatalf("Failed to create dcfh directory: %v", err)
	}
	
	// Create test files and build index
	testFile1 := filepath.Join(tempDir, "file1.txt")
	if err := os.WriteFile(testFile1, []byte("content1"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	
	// Build a proper main index first
	if err := dc.Update(nil, nil); err != nil {
		t.Fatalf("Failed to create initial index: %v", err)
	}
	
	// Create cache file and scan file for testing
	if err := copyFile(dc.IndexFile, dc.CacheFile); err != nil {
		t.Fatalf("Failed to create cache file: %v", err)
	}
	
	scanFile := filepath.Join(dcfhDir, "scan-123-456.idx")
	if err := copyFile(dc.IndexFile, scanFile); err != nil {
		t.Fatalf("Failed to create scan file: %v", err)
	}
	
	// Test the pre-recovery snapshot function directly
	err := dc.createPreRecoverySnapshot(2)
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
	originalMainStat, err := os.Stat(dc.IndexFile)
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

// Helper function for copying files in tests
func copyFile(src, dst string) error {
	sourceData, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, sourceData, 0644)
}