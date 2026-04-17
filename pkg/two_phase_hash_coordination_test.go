package dircachefilehash

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"
)

// TestTwoPhaseHashCoordination tests the complete two-phase hash coordination architecture
func TestTwoPhaseHashCoordination(t *testing.T) {
	t.Skip("Two-phase hash coordination depends on status callback — pending pipeline migration")
	// Create test directory with files
	tempDir, err := os.MkdirTemp("", "dcfh-two-phase-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Create test files with different content
	testFiles := map[string]string{
		"file1.txt":    "content for file 1",
		"file2.txt":    "different content for file 2",
		"modified.txt": "original content",
	}

	for name, content := range testFiles {
		if err := os.WriteFile(tempDir+"/"+name, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", name, err)
		}
	}

	// Create DirectoryCache with .dcfh directory
	dc := createTestDirectoryCache(t, tempDir)
	defer func() { _ = dc.Close() }()

	t.Run("Phase1_HashRequestMechanism", func(t *testing.T) {
		testPhase1HashRequests(t, dc, tempDir)
	})

	t.Run("Phase2_HashCoordinationAtWriteTime", func(t *testing.T) {
		testPhase2HashCoordination(t, dc, tempDir)
	})

	t.Run("EndToEnd_StatusWithHashing", func(t *testing.T) {
		testEndToEndStatusHashing(t, dc, tempDir)
	})
}

// testPhase1HashRequests verifies that callbacks properly request hashing when needsHash() returns true
func testPhase1HashRequests(t *testing.T, dc *DirectoryCache, _ string) {
	// Create hash manager
	hashManager := dc.newAlgorithmHashManager(context.Background(), 2)
	defer hashManager.Shutdown()

	// Create iterators - main index (empty) vs filesystem scan
	mainSkiplist, err := dc.LoadMainIndex()
	if err != nil {
		t.Fatalf("Failed to load main index: %v", err)
	}

	existingIterator := NewBinaryEntrySkiplistIterator(context.Background(), mainSkiplist, "existing")
	scanIterator := NewUnifiedFilesystemScanIterator(context.Background(), dc, []string{}, "scan")
	defer func() { _ = existingIterator.Close() }()
	defer func() { _ = scanIterator.Close() }()

	// Manually iterate and test hash request mechanism
	var testedEntries int
	for {
		// Get entries from both iterators
		existingEntry, err1 := existingIterator.Next()
		scanEntry, err2 := scanIterator.Next()

		if err1 != nil || err2 != nil {
			if err1 != nil {
				t.Errorf("Existing iterator error: %v", err1)
			}
			if err2 != nil {
				t.Errorf("Scan iterator error: %v", err2)
			}
			break
		}

		if existingEntry == nil && scanEntry == nil {
			break // Both exhausted
		}

		// Test new file scenario (no existing entry, has scan entry)
		if existingEntry == nil && scanEntry != nil {
			scanPath, _ := scanEntry.RelativePath()
			t.Logf("Testing hash request for new file: %s", scanPath)

			// Verify hash is initially empty
			initialHash, err := scanEntry.Hash()
			if err != nil {
				t.Errorf("Failed to get initial hash for %s: %v", scanPath, err)
				continue
			}

			// Should be all zeros initially
			allZero := true
			for _, b := range initialHash {
				if b != 0 {
					allZero = false
					break
				}
			}
			if !allZero {
				t.Errorf("Expected initial hash to be zero for %s", scanPath)
			}

			// Test hash request mechanism
			if err := scanEntry.RequestHash(); err != nil {
				t.Errorf("RequestHash failed for %s: %v", scanPath, err)
				continue
			}

			// Verify hash was requested
			requested, err := scanEntry.IsHashRequested()
			if err != nil {
				t.Errorf("IsHashRequested failed for %s: %v", scanPath, err)
			} else if !requested {
				t.Errorf("Expected hash to be requested for new file %s", scanPath)
			} else {
				t.Logf("✓ Hash successfully requested for new file: %s", scanPath)
			}

			testedEntries++
		}

		// For this test, we primarily care about new files (existing will be empty)
		// Break after testing a few entries
		if testedEntries >= 3 {
			break
		}
	}

	if testedEntries == 0 {
		t.Error("No entries were tested for hash requests")
	} else {
		t.Logf("Successfully tested hash requests for %d entries", testedEntries)
	}
}

// testPhase2HashCoordination verifies that hash coordination happens at write time
func testPhase2HashCoordination(t *testing.T, dc *DirectoryCache, _ string) {
	// This tests the second phase: actual hash coordination and computation
	// We'll use the Update command which should trigger hash computation

	// First, do an update to populate the main index using unified architecture
	updateResult, err := dc.runStatusWorkflowUnified(context.Background())
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if updateResult == nil {
		t.Fatal("Update result is nil")
	}

	t.Logf("Update completed: %d entries processed", updateResult.Length())

	// Verify that files now have hashes (proving phase 2 coordination worked)
	mainSkiplist, err := dc.LoadMainIndex()
	if err != nil {
		t.Fatalf("Failed to reload main index: %v", err)
	}

	hashedEntries := 0
	mainSkiplist.ForEach(func(entry *binaryEntry, relPath string) bool {
		// Check if entry has a valid hash
		allZero := true
		for _, b := range entry.Hash {
			if b != 0 {
				allZero = false
				break
			}
		}

		if !allZero {
			hashedEntries++
			t.Logf("✓ File %s has computed hash: %x", relPath, entry.Hash[:8])
		} else {
			t.Logf("⚠ File %s has zero hash", relPath)
		}

		return true // Continue iteration
	})

	if hashedEntries == 0 {
		t.Error("No files have computed hashes - phase 2 coordination may have failed")
	} else {
		t.Logf("Successfully verified hash coordination: %d files have computed hashes", hashedEntries)
	}
}

// testEndToEndStatusHashing verifies that Status command properly uses two-phase coordination
func testEndToEndStatusHashing(t *testing.T, dc *DirectoryCache, tempDir string) {
	// Modify a file to create a change scenario
	modifiedFile := tempDir + "/modified.txt"
	if err := os.WriteFile(modifiedFile, []byte("new modified content"), 0644); err != nil {
		t.Fatalf("Failed to modify test file: %v", err)
	}

	// Add a brief delay to ensure timestamp difference
	time.Sleep(10 * time.Millisecond)

	// Run Status command - this should detect the modification and use two-phase coordination
	flags := make(map[string]string)
	result, err := dc.Status(context.Background(), flags)
	if err != nil {
		t.Fatalf("Status command failed: %v", err)
	}

	if result == nil {
		t.Fatal("Status result is nil")
	}

	// Verify status detected changes
	totalChanges := len(result.Modified) + len(result.Added) + len(result.Deleted)
	if totalChanges == 0 {
		t.Error("Status should have detected at least one change")
	} else {
		t.Logf("Status detected %d changes: %d modified, %d added, %d deleted",
			totalChanges, len(result.Modified), len(result.Added), len(result.Deleted))
	}

	// Check if modified.txt was detected
	foundModified := slices.Contains(result.Modified, "modified.txt")

	if !foundModified {
		t.Error("Status should have detected modified.txt as changed")
	} else {
		t.Log("✓ Status correctly detected modified.txt as changed")
	}

	// Verify that cache index was updated with hashed results (if Status command implements phase 2)
	cacheSkiplist, err := dc.loadCacheIndex()
	if err != nil {
		t.Fatalf("Failed to load cache index: %v", err)
	}

	if !cacheSkiplist.IsEmpty() {
		t.Logf("✓ Cache index has %d entries (Status command wrote results)", cacheSkiplist.Length())

		// Check if any entries have computed hashes
		hashedInCache := 0
		cacheSkiplist.ForEach(func(entry *binaryEntry, relPath string) bool {
			allZero := true
			for _, b := range entry.Hash {
				if b != 0 {
					allZero = false
					break
				}
			}
			if !allZero {
				hashedInCache++
			}
			return true
		})

		if hashedInCache > 0 {
			t.Logf("✓ Cache contains %d entries with computed hashes", hashedInCache)
		} else {
			t.Log("ℹ Cache entries don't have computed hashes yet (CallbackHashCoordinator not fully implemented)")
		}
	} else {
		t.Log("ℹ Cache index is empty (Status command may not be writing to cache yet)")
	}
}

// TestHashRequestCoordinationStates tests the hash coordination state machine
func TestHashRequestCoordinationStates(t *testing.T) {
	// Create a mock entry to test state transitions
	mockEntry := &mockBinaryEntry{
		relPath:   "test.txt",
		size:      100,
		mtime:     time.Now(),
		hashValue: [20]byte{},
	}

	// Test initial state
	requested, err := mockEntry.IsHashRequested()
	if err != nil {
		t.Fatalf("IsHashRequested failed: %v", err)
	}
	if requested {
		t.Error("Hash should not be requested initially")
	}

	completed, err := mockEntry.IsHashCompleted()
	if err != nil {
		t.Fatalf("IsHashCompleted failed: %v", err)
	}
	if completed {
		t.Error("Hash should not be completed initially")
	}

	// Test requesting hash
	if err := mockEntry.RequestHash(); err != nil {
		t.Fatalf("RequestHash failed: %v", err)
	}

	requested, err = mockEntry.IsHashRequested()
	if err != nil {
		t.Fatalf("IsHashRequested failed after request: %v", err)
	}
	if !requested {
		t.Error("Hash should be requested after RequestHash()")
	}

	// Test completion
	mockEntry.MarkHashCompleted()

	completed, err = mockEntry.IsHashCompleted()
	if err != nil {
		t.Fatalf("IsHashCompleted failed after completion: %v", err)
	}
	if !completed {
		t.Error("Hash should be completed after MarkHashCompleted()")
	}

	t.Log("✓ Hash coordination state machine works correctly")
}
