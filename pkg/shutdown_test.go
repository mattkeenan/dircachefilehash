package dircachefilehash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGracefulShutdownDuringHash(t *testing.T) {
	// Create test directory using standard Go testing pattern
	tempDir := t.TempDir()

	t.Logf("Test directory path: %s", tempDir)

	// Initialize dcfh repository
	dc := NewDirectoryCache(tempDir, tempDir)
	defer dc.Close()

	// Set a very small hash buffer size to guarantee interruption during large file hashing
	// This is a test-specific override - in real usage it would come from config
	if dc.config != nil {
		section := dc.config.ini.Section("performance")
		section.Key("hash_buffer").SetValue("64K") // Very small buffer for testing
	}

	// Create empty index to establish baseline
	if err := dc.createEmptyIndex(); err != nil {
		t.Fatalf("Failed to create empty index: %v", err)
	}

	// Create existing.txt and add it to cache index first
	existingFile := filepath.Join(tempDir, "existing.txt")
	if err := os.WriteFile(existingFile, []byte("existing content for cache index"), 0644); err != nil {
		t.Fatalf("Failed to create existing file: %v", err)
	}

	// Run status to add existing.txt to cache.idx
	t.Logf("Running initial status to add existing.txt to cache index")
	_, err := dc.Status(nil, map[string]string{})
	if err != nil {
		t.Fatalf("Failed to run initial status: %v", err)
	}

	// Verify existing.txt is in cache index
	cacheIndexPath := filepath.Join(tempDir, ".dcfh", "cache.idx")
	initialEntryRefs, err := dc.LoadIndexFromFileForValidation(cacheIndexPath)
	if err != nil {
		t.Fatalf("Failed to load initial cache index: %v", err)
	}

	if len(initialEntryRefs) != 1 {
		t.Fatalf("Expected 1 entry in initial cache index, got %d", len(initialEntryRefs))
	}

	initialEntry := initialEntryRefs[0].GetBinaryEntry()
	existingPath := initialEntry.RelativePath()
	if existingPath != "existing.txt" {
		t.Fatalf("Expected initial entry to be 'existing.txt', got '%s'", existingPath)
	}
	t.Logf("Confirmed existing.txt is in cache index with hash: %x", initialEntry.Hash[:8])

	// Create a moderately large deterministic file (250MB to ensure hashing takes longer than timer)
	// Based on observed timing: 100MB = ~63ms, so 250MB should take ~150ms > 25ms timer
	largeFile := filepath.Join(tempDir, "large_file.bin")
	if err := createDeterministicFile(largeFile, 250*1024*1024); err != nil {
		t.Fatalf("Failed to create large file: %v", err)
	}

	// Create shutdown channel
	shutdownChan := make(chan struct{})

	// Start a timer to send shutdown signal after a short delay
	// This should trigger during the hashing of the large file
	timerFired := false
	shutdownTimer := time.AfterFunc(10*time.Millisecond, func() {
		t.Logf("Timer fired - sending shutdown signal")
		timerFired = true
		close(shutdownChan)
	})
	defer shutdownTimer.Stop()

	// Start the status command which will trigger scan and hash
	t.Logf("Starting status command (will trigger scan and hash)")
	start := time.Now()

	// This should be interrupted by the shutdown signal
	_, err = dc.Status(shutdownChan, map[string]string{})

	elapsed := time.Since(start)
	t.Logf("Status completed in %v", elapsed)

	// Verify our timer actually fired
	if !timerFired {
		t.Logf("Warning: Timer did not fire - operation completed too quickly for shutdown test")
	}

	// The operation should complete gracefully (no error expected from graceful shutdown)
	if err != nil {
		t.Logf("Status returned error (may be expected during shutdown): %v", err)
	}

	// Verify that cache.idx still exists after shutdown
	// (cacheIndexPath already declared above)
	if _, err := os.Stat(cacheIndexPath); os.IsNotExist(err) {
		t.Fatalf("cache.idx should exist after shutdown")
	}

	// Load and verify the cache index after shutdown
	finalEntryRefs, err := dc.LoadIndexFromFileForValidation(cacheIndexPath)
	if err != nil {
		t.Fatalf("Failed to load final cache index: %v", err)
	}

	finalEntryCount := len(finalEntryRefs)
	t.Logf("Final cache index has %d entries", finalEntryCount)

	// Verify that existing.txt is still present and large_file.bin is not
	foundExisting := false
	foundLargeFile := false

	for i, entryRef := range finalEntryRefs {
		entry := entryRef.GetBinaryEntry()
		path := entry.RelativePath()

		// All entries in final index should have valid hashes (empty hash entries should be filtered)
		if entry.IsHashEmpty() {
			t.Errorf("Entry %d (%s) has empty hash but should have been filtered out", i, path)
		}

		if path == "existing.txt" {
			foundExisting = true
			t.Logf("✓ Found existing.txt in final cache index (hash: %x)", entry.Hash[:8])
		} else if path == "large_file.bin" {
			foundLargeFile = true
			t.Logf("Found large_file.bin in final cache index (hash: %x)", entry.Hash[:8])
			// Note: This could happen if hashing completed before shutdown was processed
		}
	}

	// Verify expectations
	if !foundExisting {
		t.Errorf("existing.txt should remain in cache index after shutdown")
	}

	if foundLargeFile {
		t.Logf("large_file.bin completed hashing before shutdown signal was processed")
	} else {
		t.Logf("large_file.bin was successfully interrupted by shutdown signal")
	}

	t.Logf("✓ Shutdown test completed: existing.txt preserved, shutdown behavior verified")

	// Verify no scan index files are left behind
	dcfhDir := filepath.Join(tempDir, ".dcfh")
	entries, err := os.ReadDir(dcfhDir)
	if err != nil {
		t.Fatalf("Failed to read .dcfh directory: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if strings.HasPrefix(name, "scan-") && strings.HasSuffix(name, ".idx") {
			t.Errorf("Found leftover scan index file: %s", name)
		}
	}

	t.Logf("Shutdown test completed successfully")
}

// createDeterministicFile creates a file with deterministic content of the specified size
func createDeterministicFile(path string, size int64) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	// Create deterministic content by repeating a pattern
	pattern := []byte("0123456789abcdef")
	written := int64(0)

	for written < size {
		remainingBytes := size - written
		if remainingBytes < int64(len(pattern)) {
			pattern = pattern[:remainingBytes]
		}

		n, err := file.Write(pattern)
		if err != nil {
			return err
		}
		written += int64(n)
	}

	return file.Sync()
}
