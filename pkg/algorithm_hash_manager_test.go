package dircachefilehash

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestAlgorithmHashManager(t *testing.T) {
	t.Run("BasicOperation", func(t *testing.T) {
		// Create test directory and DirectoryCache
		testDir, err := os.MkdirTemp("", "dcfh-algorithm-test-*")
		if err != nil {
			t.Fatalf("Failed to create test directory: %v", err)
		}
		defer func() { _ = os.RemoveAll(testDir) }()

		dc := createTestDirectoryCache(t, testDir)

		// Create context for hash manager
		ctx := t.Context()

		// Create algorithm hash manager
		manager := dc.newAlgorithmHashManager(ctx, 2) // 2 workers
		defer manager.Shutdown()

		// Create notification channel
		notifyChan := make(chan uint64, 10)
		manager.RegisterIteratorNotification(notifyChan)
		defer manager.UnregisterIteratorNotification(notifyChan)

		// Create test files
		testFile1 := filepath.Join(testDir, "test1.txt")
		testFile2 := filepath.Join(testDir, "test2.txt")

		if err := os.WriteFile(testFile1, []byte("test content 1"), 0644); err != nil {
			t.Fatalf("Failed to create test file 1: %v", err)
		}
		if err := os.WriteFile(testFile2, []byte("test content 2"), 0644); err != nil {
			t.Fatalf("Failed to create test file 2: %v", err)
		}

		// Create hash jobs
		jobs := createTestHashJobs(t, dc, []string{testFile1, testFile2})

		// Submit jobs
		for _, job := range jobs {
			manager.SubmitHashJob(job)
		}

		// Finish submitting
		manager.FinishSubmitting()

		// Wait for completions
		var completions []uint64
		timeout := time.After(5 * time.Second)

		for len(completions) < len(jobs) {
			select {
			case jobID := <-notifyChan:
				completions = append(completions, jobID)
			case <-timeout:
				t.Fatalf("Timeout waiting for completions, got %d of %d", len(completions), len(jobs))
			}
		}

		// Verify completions are in order
		for i, jobID := range completions {
			expectedJobID := uint64(i + 1)
			if jobID != expectedJobID {
				t.Errorf("Expected JobID %d, got %d", expectedJobID, jobID)
			}
		}
	})

	t.Run("OutOfOrderCompletion", func(t *testing.T) {
		// Create test directory and DirectoryCache
		testDir, err := os.MkdirTemp("", "dcfh-algorithm-order-test-*")
		if err != nil {
			t.Fatalf("Failed to create test directory: %v", err)
		}
		defer func() { _ = os.RemoveAll(testDir) }()

		dc := createTestDirectoryCache(t, testDir)

		// Create context for hash manager
		ctx := t.Context()

		// Create algorithm hash manager with 1 worker to control execution order
		manager := dc.newAlgorithmHashManager(ctx, 1)
		defer manager.Shutdown()

		// Create notification channel
		notifyChan := make(chan uint64, 10)
		manager.RegisterIteratorNotification(notifyChan)
		defer manager.UnregisterIteratorNotification(notifyChan)

		// Test the completion processor directly by simulating out-of-order completions
		// We'll send completions in order 3, 1, 4, 2

		// Send JobID 3 (should be queued)
		manager.completionChan <- 3

		// Send JobID 1 (should be signaled immediately, then check queue)
		manager.completionChan <- 1

		// Send JobID 4 (should be queued)
		manager.completionChan <- 4

		// Send JobID 2 (should be signaled, then flush 3 and 4)
		manager.completionChan <- 2

		// Wait for all completions
		var completions []uint64
		timeout := time.After(2 * time.Second)

		for len(completions) < 4 {
			select {
			case jobID := <-notifyChan:
				completions = append(completions, jobID)
			case <-timeout:
				t.Fatalf("Timeout waiting for completions, got %d of 4", len(completions))
			}
		}

		// Verify completions are in order: 1, 2, 3, 4
		expectedOrder := []uint64{1, 2, 3, 4}
		for i, jobID := range completions {
			if jobID != expectedOrder[i] {
				t.Errorf("Expected JobID %d at position %d, got %d", expectedOrder[i], i, jobID)
			}
		}

		// Verify queue is empty
		queueSize, nextExpected, _ := manager.GetQueueStats()
		if queueSize != 0 {
			t.Errorf("Expected empty queue, got size %d", queueSize)
		}
		if nextExpected != 5 {
			t.Errorf("Expected next expected JobID 5, got %d", nextExpected)
		}
	})

	t.Run("MultipleIterators", func(t *testing.T) {
		// Create test directory and DirectoryCache
		testDir, err := os.MkdirTemp("", "dcfh-algorithm-multi-test-*")
		if err != nil {
			t.Fatalf("Failed to create test directory: %v", err)
		}
		defer func() { _ = os.RemoveAll(testDir) }()

		dc := createTestDirectoryCache(t, testDir)

		// Create context for hash manager
		ctx := t.Context()

		// Create algorithm hash manager
		manager := dc.newAlgorithmHashManager(ctx, 2)
		defer manager.Shutdown()

		// Create multiple notification channels
		notifyChan1 := make(chan uint64, 10)
		notifyChan2 := make(chan uint64, 10)
		notifyChan3 := make(chan uint64, 10)

		manager.RegisterIteratorNotification(notifyChan1)
		manager.RegisterIteratorNotification(notifyChan2)
		manager.RegisterIteratorNotification(notifyChan3)

		defer manager.UnregisterIteratorNotification(notifyChan1)
		defer manager.UnregisterIteratorNotification(notifyChan2)
		defer manager.UnregisterIteratorNotification(notifyChan3)

		// Send some completions
		manager.completionChan <- 1
		manager.completionChan <- 2
		manager.completionChan <- 3

		// Wait for all iterators to receive all completions
		timeout := time.After(2 * time.Second)

		for _, ch := range []chan uint64{notifyChan1, notifyChan2, notifyChan3} {
			var completions []uint64
			for len(completions) < 3 {
				select {
				case jobID := <-ch:
					completions = append(completions, jobID)
				case <-timeout:
					t.Fatalf("Timeout waiting for completions from iterator")
				}
			}

			// Verify all got completions in order
			expectedOrder := []uint64{1, 2, 3}
			for i, jobID := range completions {
				if jobID != expectedOrder[i] {
					t.Errorf("Expected JobID %d at position %d, got %d", expectedOrder[i], i, jobID)
				}
			}
		}
	})

	t.Run("RegistrationAndUnregistration", func(t *testing.T) {
		// Create test directory and DirectoryCache
		testDir, err := os.MkdirTemp("", "dcfh-algorithm-reg-test-*")
		if err != nil {
			t.Fatalf("Failed to create test directory: %v", err)
		}
		defer func() { _ = os.RemoveAll(testDir) }()

		dc := createTestDirectoryCache(t, testDir)

		// Create context for hash manager
		ctx := t.Context()

		// Create algorithm hash manager
		manager := dc.newAlgorithmHashManager(ctx, 1)
		defer manager.Shutdown()

		// Test registration
		notifyChan1 := make(chan uint64, 10)
		notifyChan2 := make(chan uint64, 10)

		manager.RegisterIteratorNotification(notifyChan1)
		manager.RegisterIteratorNotification(notifyChan2)

		// Check stats
		_, _, registeredCount := manager.GetQueueStats()
		if registeredCount != 2 {
			t.Errorf("Expected 2 registered iterators, got %d", registeredCount)
		}

		// Test unregistration
		manager.UnregisterIteratorNotification(notifyChan1)

		// Check stats
		_, _, registeredCount = manager.GetQueueStats()
		if registeredCount != 1 {
			t.Errorf("Expected 1 registered iterator after unregistration, got %d", registeredCount)
		}

		// Send completion - only notifyChan2 should receive it
		manager.completionChan <- 1

		timeout := time.After(1 * time.Second)

		// notifyChan2 should receive completion
		select {
		case jobID := <-notifyChan2:
			if jobID != 1 {
				t.Errorf("Expected JobID 1, got %d", jobID)
			}
		case <-timeout:
			t.Error("Timeout waiting for completion on notifyChan2")
		}

		// notifyChan1 should NOT receive completion
		select {
		case jobID := <-notifyChan1:
			t.Errorf("Unexpected completion on unregistered channel: %d", jobID)
		case <-time.After(100 * time.Millisecond):
			// Expected - no completion should arrive
		}
	})

	t.Run("ShutdownHandling", func(t *testing.T) {
		// Create test directory and DirectoryCache
		testDir, err := os.MkdirTemp("", "dcfh-algorithm-shutdown-test-*")
		if err != nil {
			t.Fatalf("Failed to create test directory: %v", err)
		}
		defer func() { _ = os.RemoveAll(testDir) }()

		dc := createTestDirectoryCache(t, testDir)

		// Create context for hash manager
		ctx, cancel := context.WithCancel(context.Background())

		// Create algorithm hash manager
		manager := dc.newAlgorithmHashManager(ctx, 2)

		// Test that IsShuttingDown returns false initially
		if manager.IsShuttingDown() {
			t.Error("Expected IsShuttingDown to return false initially")
		}

		// Cancel context
		cancel()

		// Test that IsShuttingDown returns true after shutdown
		if !manager.IsShuttingDown() {
			t.Error("Expected IsShuttingDown to return true after shutdown")
		}

		// Shutdown should complete without hanging
		done := make(chan struct{})
		go func() {
			manager.Shutdown()
			close(done)
		}()

		select {
		case <-done:
			// Expected - shutdown completed
		case <-time.After(5 * time.Second):
			t.Error("Timeout waiting for shutdown to complete")
		}
	})

	t.Run("LargeQueue", func(t *testing.T) {
		// Test with a larger number of out-of-order completions
		testDir, err := os.MkdirTemp("", "dcfh-algorithm-large-test-*")
		if err != nil {
			t.Fatalf("Failed to create test directory: %v", err)
		}
		defer func() { _ = os.RemoveAll(testDir) }()

		dc := createTestDirectoryCache(t, testDir)

		// Create context for hash manager
		ctx := t.Context()

		// Create algorithm hash manager
		manager := dc.newAlgorithmHashManager(ctx, 1)
		defer manager.Shutdown()

		// Create notification channel
		notifyChan := make(chan uint64, 100)
		manager.RegisterIteratorNotification(notifyChan)
		defer manager.UnregisterIteratorNotification(notifyChan)

		// Send completions in reverse order: 10, 9, 8, ..., 1
		for i := 10; i >= 1; i-- {
			manager.completionChan <- uint64(i)
		}

		// Wait for all completions
		var completions []uint64
		timeout := time.After(5 * time.Second)

		for len(completions) < 10 {
			select {
			case jobID := <-notifyChan:
				completions = append(completions, jobID)
			case <-timeout:
				t.Fatalf("Timeout waiting for completions, got %d of 10", len(completions))
			}
		}

		// Verify completions are in order: 1, 2, 3, ..., 10
		for i, jobID := range completions {
			expectedJobID := uint64(i + 1)
			if jobID != expectedJobID {
				t.Errorf("Expected JobID %d at position %d, got %d", expectedJobID, i, jobID)
			}
		}

		// Verify queue is empty
		queueSize, nextExpected, _ := manager.GetQueueStats()
		if queueSize != 0 {
			t.Errorf("Expected empty queue, got size %d", queueSize)
		}
		if nextExpected != 11 {
			t.Errorf("Expected next expected JobID 11, got %d", nextExpected)
		}
	})
}

// Helper function to create test directory cache
func createTestDirectoryCache(t *testing.T, testDir string) *DirectoryCache {
	// Create .dcfh directory
	dcfhDir := filepath.Join(testDir, ".dcfh")
	if err := os.MkdirAll(dcfhDir, 0755); err != nil {
		t.Fatalf("Failed to create .dcfh directory: %v", err)
	}

	return NewDirectoryCache(testDir, testDir)
}

// Helper function to create test hash jobs
func createTestHashJobs(t *testing.T, _ *DirectoryCache, filePaths []string) []*hashJobStart {
	var jobs []*hashJobStart

	for i, filePath := range filePaths {
		// Get file info for the test file
		fileInfo, err := os.Stat(filePath)
		if err != nil {
			t.Fatalf("Failed to stat test file %s: %v", filePath, err)
		}

		// Create a simple scannedPath for testing
		scannedPath := &scannedPath{
			AbsPath: filePath,
			RelPath: filepath.Base(filePath),
			Info:    fileInfo,
		}

		// Create a mock binaryEntryRef for testing
		entryRef := binaryEntryRef{
			// In a real implementation, this would point to actual mmap'd memory
			// For testing, we'll use a simplified approach
		}

		// Create a test BEScanEntry for v0.7
		// Get syscall.Stat_t for the file
		var stat syscall.Stat_t
		if err := syscall.Stat(filePath, &stat); err != nil {
			t.Fatalf("Failed to get stat for test file %s: %v", filePath, err)
		}

		testEntry := NewBEScanEntry(filepath.Base(filePath), fileInfo, &stat)

		job := &hashJobStart{
			JobID:       uint64(i + 1),
			FilePath:    filePath,
			IndexEntry:  entryRef,
			ScannedPath: scannedPath,
			Entry:       testEntry, // v0.7 unified entry
		}

		jobs = append(jobs, job)
	}

	return jobs
}

// Benchmark the algorithm hash manager
func BenchmarkAlgorithmHashManager(b *testing.B) {
	// Create test directory and DirectoryCache
	testDir, err := os.MkdirTemp("", "dcfh-algorithm-bench-*")
	if err != nil {
		b.Fatalf("Failed to create test directory: %v", err)
	}
	defer func() { _ = os.RemoveAll(testDir) }()

	dc := createTestDirectoryCacheForBench(b, testDir)

	b.Run("OrderedCompletions", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			// Create context for hash manager
			ctx, cancel := context.WithCancel(context.Background())

			// Create algorithm hash manager
			manager := dc.newAlgorithmHashManager(ctx, 1)

			// Create notification channel
			notifyChan := make(chan uint64, 100)
			manager.RegisterIteratorNotification(notifyChan)

			// Send completions in order
			for j := 1; j <= 100; j++ {
				manager.completionChan <- uint64(j)
			}

			// Wait for all completions
			for range 100 {
				<-notifyChan
			}

			// Cleanup
			cancel()
			manager.Shutdown()
		}
	})

	b.Run("ReverseOrderCompletions", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			// Create context for hash manager
			ctx, cancel := context.WithCancel(context.Background())

			// Create algorithm hash manager
			manager := dc.newAlgorithmHashManager(ctx, 1)

			// Create notification channel
			notifyChan := make(chan uint64, 100)
			manager.RegisterIteratorNotification(notifyChan)

			// Send completions in reverse order
			for j := 100; j >= 1; j-- {
				manager.completionChan <- uint64(j)
			}

			// Wait for all completions
			for range 100 {
				<-notifyChan
			}

			// Cleanup
			cancel()
			manager.Shutdown()
		}
	})
}

// Helper function for benchmark
func createTestDirectoryCacheForBench(b *testing.B, testDir string) *DirectoryCache {
	// Create .dcfh directory
	dcfhDir := filepath.Join(testDir, ".dcfh")
	if err := os.MkdirAll(dcfhDir, 0755); err != nil {
		b.Fatalf("Failed to create .dcfh directory: %v", err)
	}

	return NewDirectoryCache(testDir, testDir)
}
