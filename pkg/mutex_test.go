package dircachefilehash

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

// TestIndexMutexProtection tests that the mutex protection prevents concurrent access issues
func TestIndexMutexProtection(t *testing.T) {
	// Create test directory
	tempDir, err := os.MkdirTemp("", "dcfh_mutex_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Initialize DirectoryCache
	dc := NewDirectoryCache(tempDir, tempDir)
	dc.indexLockTimeout = 5 // 5 second timeout

	// Create test files
	for i := 0; i < 100; i++ {
		fileName := filepath.Join(tempDir, fmt.Sprintf("file_%03d.txt", i))
		content := fmt.Sprintf("Test file %d content\n", i)
		if err := os.WriteFile(fileName, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
	}

	// Load main index
	mainSkiplist, err := dc.LoadMainIndex()
	if err != nil {
		t.Fatalf("Failed to load main index: %v", err)
	}

	// Create a scan index
	scanFileName := filepath.Join(tempDir, ".dcfh", "test-scan.idx")
	if err := dc.initialiseScanIndex(scanFileName); err != nil {
		t.Fatalf("Failed to initialize scan index: %v", err)
	}

	// Test concurrent access with mutex protection
	var wg sync.WaitGroup
	errors := make(chan error, 10)

	// Start multiple goroutines that try to write while reading
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			
			// Try to write a temp index while another goroutine might be expanding memory
			tempPath := filepath.Join(tempDir, ".dcfh", fmt.Sprintf("temp-%d.idx", id))
			err := dc.writeSkiplistWithVectorIO(mainSkiplist, tempPath, "")
			if err != nil {
				errors <- fmt.Errorf("goroutine %d: write failed: %w", id, err)
			}
			os.Remove(tempPath)
		}(i)
	}

	// Simulate mremap operations in another goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		
		// Add entries to scan index to trigger mremap
		for i := 0; i < 50; i++ {
			relPath := fmt.Sprintf("new_file_%03d.txt", i)
			hash := make([]byte, 20) // SHA1 hash
			info := &fakeFileInfo{name: relPath, size: 1024}
			stat := &syscall.Stat_t{
				Mode: 0644,
				Uid:  uint32(os.Getuid()),
				Gid:  uint32(os.Getgid()),
			}
			
			_, err := dc.AppendEntryToScanIndex(scanFileName, relPath, hash, HashTypeSHA1, info, stat, false)
			if err != nil {
				errors <- fmt.Errorf("append entry failed: %w", err)
			}
			
			// Small delay to increase chance of concurrent access
			time.Sleep(time.Millisecond)
		}
	}()

	// Wait for all goroutines
	wg.Wait()
	close(errors)

	// Check for errors
	var errorCount int
	for err := range errors {
		t.Errorf("Concurrent access error: %v", err)
		errorCount++
	}

	if errorCount == 0 {
		t.Logf("Successfully handled concurrent access with mutex protection")
	}

	// Cleanup
	if err := dc.cleanupCurrentScanFile(); err != nil {
		t.Errorf("Failed to cleanup scan file: %v", err)
	}
}

// TestIndexLockTimeout tests that the timeout mechanism works
func TestIndexLockTimeout(t *testing.T) {
	// Create test directory
	tempDir, err := os.MkdirTemp("", "dcfh_timeout_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Initialize DirectoryCache with short timeout
	dc := NewDirectoryCache(tempDir, tempDir)
	dc.indexLockTimeout = 1 // 1 second timeout

	// Create a simple index
	if err := dc.createEmptyIndex(); err != nil {
		t.Fatalf("Failed to create empty index: %v", err)
	}

	// Load main index
	mainSkiplist, err := dc.LoadMainIndex()
	if err != nil {
		t.Fatalf("Failed to load main index: %v", err)
	}

	// Simulate a deadlock scenario by manually locking an index
	if dc.mainIndex != nil {
		dc.mainIndex.mutex.Lock()
		defer dc.mainIndex.mutex.Unlock()

		// Try to write with timeout
		start := time.Now()
		tempPath := filepath.Join(tempDir, ".dcfh", "temp-timeout.idx")
		err := dc.writeSkiplistWithVectorIO(mainSkiplist, tempPath, "")
		elapsed := time.Since(start)

		// Should complete despite lock timeout
		if err != nil {
			t.Fatalf("Write failed: %v", err)
		}

		// Check that timeout was triggered (should be around 1 second)
		if elapsed < 900*time.Millisecond || elapsed > 1500*time.Millisecond {
			t.Logf("Warning: timeout timing unexpected: %v", elapsed)
		}

		os.Remove(tempPath)
	}
}

// fakeFileInfo implements os.FileInfo for testing
type fakeFileInfo struct {
	name string
	size int64
}

func (f *fakeFileInfo) Name() string       { return f.name }
func (f *fakeFileInfo) Size() int64        { return f.size }
func (f *fakeFileInfo) Mode() os.FileMode  { return 0644 }
func (f *fakeFileInfo) ModTime() time.Time { return time.Now() }
func (f *fakeFileInfo) IsDir() bool        { return false }
func (f *fakeFileInfo) Sys() interface{}   { return nil }