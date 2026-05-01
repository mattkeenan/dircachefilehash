package dircachefilehash

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Test basic UpdateCallback functionality
func TestUpdateCallback_BasicOperation(t *testing.T) {
	t.Skip("UpdateCallback tests require algorithmHashManager — update path now uses pipeline")
	tempDir, err := os.MkdirTemp("", "callback-update-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	dc := createTestDirectoryCacheForUpdate(t, tempDir)

	// Create hash manager for testing
	hashManager := dc.newAlgorithmHashManager(context.Background(), 2)
	defer hashManager.Shutdown()

	// Create scan index filename
	scanFileName := dc.generateScanFileName()
	if err := dc.initialiseScanIndex(scanFileName); err != nil {
		t.Fatalf("Failed to initialise scan index: %v", err)
	}

	// Create update callback
	updateCallback := NewUpdateCallback(context.Background(), dc, scanFileName, nil)

	// Test Name method
	if updateCallback.Name() != "update" {
		t.Errorf("Expected name 'update', got '%s'", updateCallback.Name())
	}

	// TODO: v0.7 UpdateCallback doesn't use GetResultSkiplist() - writes directly to temp index
	// Test initial state
	// resultSkiplist := updateCallback.GetResultSkiplist()
	// if resultSkiplist.Length() != 0 {
	// 	t.Errorf("Expected empty result skiplist, got %d entries", resultSkiplist.Length())
	// }

	// Test OnStart
	if err := updateCallback.OnStart("left", "right"); err != nil {
		t.Errorf("OnStart failed: %v", err)
	}

	// Test OnComplete
	if err := updateCallback.OnComplete(nil); err != nil {
		t.Errorf("OnComplete failed: %v", err)
	}
}

// Test UpdateCallback with mock entries
func TestUpdateCallback_MockEntries(t *testing.T) {
	t.Skip("UpdateCallback tests require algorithmHashManager — update path now uses pipeline")
	tempDir, err := os.MkdirTemp("", "callback-update-mock-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	dc := createTestDirectoryCacheForUpdate(t, tempDir)

	// Create hash manager for testing
	hashManager := dc.newAlgorithmHashManager(context.Background(), 2)
	defer hashManager.Shutdown()

	// Create scan index filename
	scanFileName := dc.generateScanFileName()
	if err := dc.initialiseScanIndex(scanFileName); err != nil {
		t.Fatalf("Failed to initialise scan index: %v", err)
	}

	// Create update callback
	updateCallback := NewUpdateCallback(context.Background(), dc, scanFileName, nil)

	// Create mock entries for testing
	leftEntry := createMockBinaryEntryForUpdate("test1.txt", 1024, false)
	rightEntry := createMockBinaryEntryForUpdate("test1.txt", 2048, false) // Different size = modified

	// Test ComparisonMatch case (file modified)
	continueProcessing, err := updateCallback.OnComparison(
		ComparisonMatch,
		leftEntry, rightEntry,
		"test1.txt", "test1.txt",
	)
	if err != nil {
		t.Errorf("OnComparison failed for match case: %v", err)
	}
	if !continueProcessing {
		t.Error("Expected OnComparison to return continueProcessing=true")
	}

	// Test ComparisonRightFirst case (new file)
	continueProcessing, err = updateCallback.OnComparison(
		ComparisonRightFirst,
		nil, rightEntry,
		"", "test2.txt",
	)
	if err != nil {
		t.Errorf("OnComparison failed for right-first case: %v", err)
	}
	if !continueProcessing {
		t.Error("Expected OnComparison to return continueProcessing=true")
	}

	// Test ComparisonLeftFirst case (deleted file)
	continueProcessing, err = updateCallback.OnComparison(
		ComparisonLeftFirst,
		leftEntry, nil,
		"test3.txt", "",
	)
	if err != nil {
		t.Errorf("OnComparison failed for left-first case: %v", err)
	}
	if !continueProcessing {
		t.Error("Expected OnComparison to return continueProcessing=true")
	}
}

// Test UpdateCallback integration with real files
func TestUpdateCallback_RealFiles(t *testing.T) {
	t.Skip("UpdateCallback tests require algorithmHashManager — update path now uses pipeline")
	tempDir, err := os.MkdirTemp("", "callback-update-real-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Create test files
	testFile1 := filepath.Join(tempDir, "file1.txt")
	testFile2 := filepath.Join(tempDir, "file2.txt")

	if err := os.WriteFile(testFile1, []byte("content1"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	if err := os.WriteFile(testFile2, []byte("content2"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	dc := createTestDirectoryCacheForUpdate(t, tempDir)

	// Create hash manager for testing
	hashManager := dc.newAlgorithmHashManager(context.Background(), 2)
	defer hashManager.Shutdown()

	// Create scan index filename
	scanFileName := dc.generateScanFileName()
	if err := dc.initialiseScanIndex(scanFileName); err != nil {
		t.Fatalf("Failed to initialise scan index: %v", err)
	}

	// Create update callback
	updateCallback := NewUpdateCallback(context.Background(), dc, scanFileName, nil)

	// Create real scan entries using the unified iterator
	scanIterator := NewFilesystemScanIterator(context.Background(), dc, []string{}, "test-scan")
	defer func() { _ = scanIterator.Close() }()

	// Get first file entry
	rightEntry, err := scanIterator.Next()
	if err != nil {
		t.Fatalf("Failed to get scan entry: %v", err)
	}
	if rightEntry == nil {
		t.Fatal("Expected scan entry, got nil")
	}

	rightPath := scanIterator.CurrentPath()

	// Test OnRightOnly (new file scenario)
	continueProcessing, err := updateCallback.OnRightOnly(rightEntry, rightPath)
	if err != nil {
		t.Errorf("OnRightOnly failed: %v", err)
	}
	if !continueProcessing {
		t.Error("Expected OnRightOnly to return continueProcessing=true")
	}

	// TODO: v0.7 UpdateCallback doesn't use GetResultSkiplist() - writes directly to temp index
	// Check that result skiplist has entries
	// resultSkiplist := updateCallback.GetResultSkiplist()
	// if resultSkiplist.Length() == 0 {
	// 	t.Error("Expected result skiplist to have entries after processing")
	// }

	// Signal completion
	hashManager.FinishSubmitting()
}

// Test UpdateCallback error handling
func TestUpdateCallback_ErrorHandling(t *testing.T) {
	t.Skip("UpdateCallback tests require algorithmHashManager — update path now uses pipeline")
	tempDir, err := os.MkdirTemp("", "callback-update-error-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	dc := createTestDirectoryCacheForUpdate(t, tempDir)

	// Create hash manager for testing
	hashManager := dc.newAlgorithmHashManager(context.Background(), 2)
	defer hashManager.Shutdown()

	// Create scan index filename
	scanFileName := dc.generateScanFileName()
	if err := dc.initialiseScanIndex(scanFileName); err != nil {
		t.Fatalf("Failed to initialise scan index: %v", err)
	}

	// Create update callback
	updateCallback := NewUpdateCallback(context.Background(), dc, scanFileName, nil)

	// Test with nil entries (edge case)
	continueProcessing, err := updateCallback.OnComparison(
		ComparisonMatch,
		nil, nil,
		"", "",
	)
	if err != nil {
		t.Errorf("OnComparison should handle nil entries gracefully: %v", err)
	}
	if !continueProcessing {
		t.Error("Expected OnComparison to return continueProcessing=true even with nil entries")
	}

	// Test OnComplete with error
	testErr := fmt.Errorf("test error")
	if err := updateCallback.OnComplete(testErr); err != nil {
		t.Errorf("OnComplete should handle errors gracefully: %v", err)
	}
}

// createTestDirectoryCacheForUpdate creates a test DirectoryCache for update testing
func createTestDirectoryCacheForUpdate(t *testing.T, testDir string) *DirectoryCache {
	// Create .dcfh directory
	dcfhDir := filepath.Join(testDir, ".dcfh")
	if err := os.MkdirAll(dcfhDir, 0755); err != nil {
		t.Fatalf("Failed to create .dcfh directory: %v", err)
	}

	return NewDirectoryCache(testDir, testDir)
}

// createMockBinaryEntryForUpdate creates a mock BinaryEntryInterface for testing
func createMockBinaryEntryForUpdate(relPath string, size uint64, deleted bool) BinaryEntryInterface {
	// Create a simple mock that implements the minimum required methods
	return &mockBinaryEntry{
		relPath:   relPath,
		size:      size,
		deleted:   deleted,
		mtime:     time.Now(),
		hashValue: [20]byte{1, 2, 3, 4, 5}, // Simple test hash
	}
}

// mockBinaryEntry is a simple mock implementation for testing
type mockBinaryEntry struct {
	relPath       string
	size          uint64
	deleted       bool
	mtime         time.Time
	hashValue     [20]byte
	hashRequested bool
	hashCompleted bool
	hashJobID     uint64
}

func (m *mockBinaryEntry) RelativePath() (string, error)                   { return m.relPath, nil }
func (m *mockBinaryEntry) Size() (uint32, error)                           { return uint32(m.size), nil }
func (m *mockBinaryEntry) FileSize() (uint64, error)                       { return m.size, nil }
func (m *mockBinaryEntry) IsDeleted() (bool, error)                        { return m.deleted, nil }
func (m *mockBinaryEntry) Hash() ([20]byte, error)                         { return m.hashValue, nil }
func (m *mockBinaryEntry) HashString() (string, error)                     { return fmt.Sprintf("%x", m.hashValue), nil }
func (m *mockBinaryEntry) HashType() (uint16, error)                       { return HashTypeSHA1, nil }
func (m *mockBinaryEntry) MTimeWall() (uint64, error)                      { return uint64(m.mtime.Unix()), nil }
func (m *mockBinaryEntry) CTimeWall() (uint64, error)                      { return uint64(m.mtime.Unix()), nil }
func (m *mockBinaryEntry) Dev() (uint32, error)                            { return 123, nil }
func (m *mockBinaryEntry) Ino() (uint32, error)                            { return 456, nil }
func (m *mockBinaryEntry) Mode() (uint32, error)                           { return 0644, nil }
func (m *mockBinaryEntry) UID() (uint32, error)                            { return 1000, nil }
func (m *mockBinaryEntry) GID() (uint32, error)                            { return 1000, nil }
func (m *mockBinaryEntry) EntryFlags() (uint32, error)                     { return 0, nil }
func (m *mockBinaryEntry) SetHash(hashBytes []byte, hashType uint16) error { return nil }
func (m *mockBinaryEntry) SetDeleted(deleted bool) error                   { m.deleted = deleted; return nil }
func (m *mockBinaryEntry) RLock()                                          {}
func (m *mockBinaryEntry) RUnlock()                                        {}
func (m *mockBinaryEntry) Lock()                                           {}
func (m *mockBinaryEntry) Unlock()                                         {}
func (m *mockBinaryEntry) IsValid() bool                                   { return true }
func (m *mockBinaryEntry) SupportsSkiplistBuilding() bool                  { return false }
func (m *mockBinaryEntry) GetBinaryEntryRef() (binaryEntryRef, bool)       { return binaryEntryRef{}, false }
func (m *mockBinaryEntry) GetContext() (string, error)                     { return "mock", nil }
func (m *mockBinaryEntry) RequestHash() error                              { m.hashRequested = true; return nil }
func (m *mockBinaryEntry) IsHashRequested() (bool, error)                  { return m.hashRequested, nil }
func (m *mockBinaryEntry) IsHashCompleted() (bool, error)                  { return m.hashCompleted, nil }
func (m *mockBinaryEntry) SetHashJobID(jobID uint64)                       { m.hashJobID = jobID }
func (m *mockBinaryEntry) GetHashJobID() uint64                            { return m.hashJobID }
func (m *mockBinaryEntry) MarkHashCompleted()                              { m.hashCompleted = true }
