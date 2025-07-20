//go:build exclude

package dircachefilehash

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestEnhancedFilesystemScanIterator(t *testing.T) {
	t.Run("BasicScanning", func(t *testing.T) {
		// Create test directory and DirectoryCache
		testDir, err := os.MkdirTemp("", "dcfh-enhanced-basic-test-*")
		if err != nil {
			t.Fatalf("Failed to create test directory: %v", err)
		}
		defer os.RemoveAll(testDir)
		
		dc := createTestDirectoryCacheForEnhanced(t, testDir)
		
		// Create test files
		testFiles := []string{"file1.txt", "file2.txt", "file3.txt"}
		for _, fileName := range testFiles {
			filePath := filepath.Join(testDir, fileName)
			if err := os.WriteFile(filePath, []byte("test content for "+fileName), 0644); err != nil {
				t.Fatalf("Failed to create test file %s: %v", fileName, err)
			}
		}
		
		// Create shutdown channel and hash manager
		shutdownChan := make(chan struct{})
		defer close(shutdownChan)
		
		hashManager := dc.newAlgorithmHashManager(2, shutdownChan)
		defer hashManager.Shutdown()
		
		// Create enhanced iterator
		iterator := NewEnhancedFilesystemScanIterator(dc, []string{testDir}, "test-enhanced", hashManager)
		defer iterator.Close()
		
		// Iterate through files
		var foundFiles []string
		for {
			entry, err := iterator.Next()
			if err != nil {
				t.Fatalf("Unexpected error during iteration: %v", err)
			}
			
			if entry == nil {
				break // End of iteration
			}
			
			// Get relative path
			relPath := entry.RelativePath()
			foundFiles = append(foundFiles, relPath)
		}
		
		// Verify we found the expected files
		if len(foundFiles) != len(testFiles) {
			t.Errorf("Expected %d files, got %d", len(testFiles), len(foundFiles))
		}
		
		// Verify files are in sorted order
		for i := 1; i < len(foundFiles); i++ {
			if foundFiles[i-1] > foundFiles[i] {
				t.Errorf("Files not in sorted order: %s > %s", foundFiles[i-1], foundFiles[i])
			}
		}
	})
	
	t.Run("HashCoordination", func(t *testing.T) {
		// Create test directory and DirectoryCache
		testDir, err := os.MkdirTemp("", "dcfh-enhanced-hash-test-*")
		if err != nil {
			t.Fatalf("Failed to create test directory: %v", err)
		}
		defer os.RemoveAll(testDir)
		
		dc := createTestDirectoryCacheForEnhanced(t, testDir)
		
		// Create test files with different content
		testFiles := []string{
			"alpha.txt",
			"beta.txt", 
			"gamma.txt",
		}
		
		for i, fileName := range testFiles {
			filePath := filepath.Join(testDir, fileName)
			content := fmt.Sprintf("test content %d for %s", i, fileName)
			if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
				t.Fatalf("Failed to create test file %s: %v", fileName, err)
			}
		}
		
		// Create shutdown channel and hash manager
		shutdownChan := make(chan struct{})
		defer close(shutdownChan)
		
		hashManager := dc.newAlgorithmHashManager(2, shutdownChan)
		defer hashManager.Shutdown()
		
		// Create enhanced iterator
		iterator := NewEnhancedFilesystemScanIterator(dc, []string{testDir}, "test-hash-coordination", hashManager)
		defer iterator.Close()
		
		// Iterate through files and verify hashes
		var hashedFiles []string
		for {
			entry, err := iterator.Next()
			if err != nil {
				t.Fatalf("Unexpected error during iteration: %v", err)
			}
			
			if entry == nil {
				break // End of iteration
			}
			
			// Verify entry has a valid hash
			hashString := entry.HashString()
			if hashString == "" {
				t.Errorf("Entry %s has empty hash", entry.RelativePath())
			}
			
			hashedFiles = append(hashedFiles, entry.RelativePath())
		}
		
		// Verify all files were processed
		if len(hashedFiles) != len(testFiles) {
			t.Errorf("Expected %d hashed files, got %d", len(testFiles), len(hashedFiles))
		}
		
		// Verify no pending jobs remain
		if pendingCount := iterator.GetPendingJobCount(); pendingCount > 0 {
			t.Errorf("Expected 0 pending jobs, got %d", pendingCount)
		}
	})
	
	t.Run("OutOfOrderCompletion", func(t *testing.T) {
		// Create test directory and DirectoryCache
		testDir, err := os.MkdirTemp("", "dcfh-enhanced-order-test-*")
		if err != nil {
			t.Fatalf("Failed to create test directory: %v", err)
		}
		defer os.RemoveAll(testDir)
		
		dc := createTestDirectoryCacheForEnhanced(t, testDir)
		
		// Create test files of different sizes (to encourage out-of-order completion)
		testFiles := []struct {
			name string
			size int
		}{
			{"small.txt", 100},
			{"large.txt", 100000},
			{"medium.txt", 1000},
		}
		
		for _, file := range testFiles {
			filePath := filepath.Join(testDir, file.name)
			content := make([]byte, file.size)
			for i := range content {
				content[i] = byte(i % 256)
			}
			if err := os.WriteFile(filePath, content, 0644); err != nil {
				t.Fatalf("Failed to create test file %s: %v", file.name, err)
			}
		}
		
		// Create shutdown channel and hash manager
		shutdownChan := make(chan struct{})
		defer close(shutdownChan)
		
		hashManager := dc.newAlgorithmHashManager(3, shutdownChan) // More workers for concurrency
		defer hashManager.Shutdown()
		
		// Create enhanced iterator
		iterator := NewEnhancedFilesystemScanIterator(dc, []string{testDir}, "test-out-of-order", hashManager)
		defer iterator.Close()
		
		// Iterate through files and verify order is maintained
		var processedFiles []string
		for {
			entry, err := iterator.Next()
			if err != nil {
				t.Fatalf("Unexpected error during iteration: %v", err)
			}
			
			if entry == nil {
				break // End of iteration
			}
			
			processedFiles = append(processedFiles, entry.RelativePath())
		}
		
		// Verify files are in sorted order despite potential out-of-order hash completion
		for i := 1; i < len(processedFiles); i++ {
			if processedFiles[i-1] > processedFiles[i] {
				t.Errorf("Files not in sorted order: %s > %s", processedFiles[i-1], processedFiles[i])
			}
		}
	})
	
	t.Run("EmptyDirectory", func(t *testing.T) {
		// Create empty test directory
		testDir, err := os.MkdirTemp("", "dcfh-enhanced-empty-test-*")
		if err != nil {
			t.Fatalf("Failed to create test directory: %v", err)
		}
		defer os.RemoveAll(testDir)
		
		dc := createTestDirectoryCacheForEnhanced(t, testDir)
		
		// Create shutdown channel and hash manager
		shutdownChan := make(chan struct{})
		defer close(shutdownChan)
		
		hashManager := dc.newAlgorithmHashManager(1, shutdownChan)
		defer hashManager.Shutdown()
		
		// Create enhanced iterator
		iterator := NewEnhancedFilesystemScanIterator(dc, []string{testDir}, "test-empty", hashManager)
		defer iterator.Close()
		
		// Should get no entries
		entry, err := iterator.Next()
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		
		if entry != nil {
			t.Errorf("Expected no entries in empty directory, got: %s", entry.RelativePath())
		}
	})
	
	t.Run("NilDirectoryCache", func(t *testing.T) {
		// Create enhanced iterator with nil DirectoryCache
		iterator := NewEnhancedFilesystemScanIterator(nil, []string{}, "test-nil-dc", nil)
		defer iterator.Close()
		
		// Should be immediately exhausted
		if !iterator.exhausted {
			t.Error("Expected iterator to be exhausted with nil DirectoryCache")
		}
		
		// Should return no entries
		entry, err := iterator.Next()
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		
		if entry != nil {
			t.Errorf("Expected no entries with nil DirectoryCache, got: %s", entry.RelativePath())
		}
	})
	
	t.Run("ClosedIterator", func(t *testing.T) {
		// Create test directory and DirectoryCache
		testDir, err := os.MkdirTemp("", "dcfh-enhanced-closed-test-*")
		if err != nil {
			t.Fatalf("Failed to create test directory: %v", err)
		}
		defer os.RemoveAll(testDir)
		
		dc := createTestDirectoryCacheForEnhanced(t, testDir)
		
		// Create shutdown channel and hash manager
		shutdownChan := make(chan struct{})
		defer close(shutdownChan)
		
		hashManager := dc.newAlgorithmHashManager(1, shutdownChan)
		defer hashManager.Shutdown()
		
		// Create enhanced iterator
		iterator := NewEnhancedFilesystemScanIterator(dc, []string{testDir}, "test-closed", hashManager)
		
		// Close iterator immediately
		iterator.Close()
		
		// Should return error when trying to iterate
		_, err = iterator.Next()
		if err == nil {
			t.Error("Expected error when iterating closed iterator")
		}
	})
	
	t.Run("LargeDirectory", func(t *testing.T) {
		// Create test directory and DirectoryCache
		testDir, err := os.MkdirTemp("", "dcfh-enhanced-large-test-*")
		if err != nil {
			t.Fatalf("Failed to create test directory: %v", err)
		}
		defer os.RemoveAll(testDir)
		
		dc := createTestDirectoryCacheForEnhanced(t, testDir)
		
		// Create many test files
		numFiles := 50
		for i := 0; i < numFiles; i++ {
			fileName := fmt.Sprintf("file_%03d.txt", i)
			filePath := filepath.Join(testDir, fileName)
			content := fmt.Sprintf("test content for file %d", i)
			if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
				t.Fatalf("Failed to create test file %s: %v", fileName, err)
			}
		}
		
		// Create shutdown channel and hash manager
		shutdownChan := make(chan struct{})
		defer close(shutdownChan)
		
		hashManager := dc.newAlgorithmHashManager(4, shutdownChan) // More workers
		defer hashManager.Shutdown()
		
		// Create enhanced iterator
		iterator := NewEnhancedFilesystemScanIterator(dc, []string{testDir}, "test-large", hashManager)
		defer iterator.Close()
		
		// Iterate through all files
		var foundFiles []string
		for {
			entry, err := iterator.Next()
			if err != nil {
				t.Fatalf("Unexpected error during iteration: %v", err)
			}
			
			if entry == nil {
				break // End of iteration
			}
			
			foundFiles = append(foundFiles, entry.RelativePath())
		}
		
		// Verify we found all files
		if len(foundFiles) != numFiles {
			t.Errorf("Expected %d files, got %d", numFiles, len(foundFiles))
		}
		
		// Verify files are in sorted order
		for i := 1; i < len(foundFiles); i++ {
			if foundFiles[i-1] > foundFiles[i] {
				t.Errorf("Files not in sorted order: %s > %s", foundFiles[i-1], foundFiles[i])
			}
		}
	})
	
	t.Run("ShutdownHandling", func(t *testing.T) {
		// Create test directory and DirectoryCache
		testDir, err := os.MkdirTemp("", "dcfh-enhanced-shutdown-test-*")
		if err != nil {
			t.Fatalf("Failed to create test directory: %v", err)
		}
		defer os.RemoveAll(testDir)
		
		dc := createTestDirectoryCacheForEnhanced(t, testDir)
		
		// Create some test files
		for i := 0; i < 10; i++ {
			fileName := fmt.Sprintf("file_%d.txt", i)
			filePath := filepath.Join(testDir, fileName)
			content := fmt.Sprintf("test content for file %d", i)
			if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
				t.Fatalf("Failed to create test file %s: %v", fileName, err)
			}
		}
		
		// Create shutdown channel and hash manager
		shutdownChan := make(chan struct{})
		
		hashManager := dc.newAlgorithmHashManager(2, shutdownChan)
		defer hashManager.Shutdown()
		
		// Create enhanced iterator
		iterator := NewEnhancedFilesystemScanIterator(dc, []string{testDir}, "test-shutdown", hashManager)
		defer iterator.Close()
		
		// Start iteration
		_, err = iterator.Next()
		if err != nil {
			t.Fatalf("Unexpected error during first iteration: %v", err)
		}
		
		// Signal shutdown
		close(shutdownChan)
		
		// Iterator should handle shutdown gracefully
		// (The next call might return an error or nil, both are acceptable)
		_, err = iterator.Next()
		// Don't check for specific error - shutdown behavior may vary
	})
}

// Helper function to create test directory cache
func createTestDirectoryCacheForEnhanced(t *testing.T, testDir string) *DirectoryCache {
	// Create .dcfh directory
	dcfhDir := filepath.Join(testDir, ".dcfh")
	if err := os.MkdirAll(dcfhDir, 0755); err != nil {
		t.Fatalf("Failed to create .dcfh directory: %v", err)
	}
	
	return NewDirectoryCache(testDir, testDir)
}

// Integration tests removed - deprecated v0.6 EnhancedFilesystemScanIterator integration
// with v0.7 hwangLinUnified algorithm. Equivalent functionality is tested
// by UnifiedFilesystemScanIterator tests which use the proper BinaryEntryIterator interface.

// Benchmark enhanced filesystem scan iterator
func BenchmarkEnhancedFilesystemScanIterator(b *testing.B) {
	// Create test directory and DirectoryCache
	testDir, err := os.MkdirTemp("", "dcfh-enhanced-bench-*")
	if err != nil {
		b.Fatalf("Failed to create test directory: %v", err)
	}
	defer os.RemoveAll(testDir)
	
	dc := createTestDirectoryCacheForEnhancedBench(b, testDir)
	
	// Create test files
	numFiles := 100
	for i := 0; i < numFiles; i++ {
		fileName := fmt.Sprintf("file_%03d.txt", i)
		filePath := filepath.Join(testDir, fileName)
		content := fmt.Sprintf("test content for file %d", i)
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			b.Fatalf("Failed to create test file %s: %v", fileName, err)
		}
	}
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		// Create shutdown channel and hash manager
		shutdownChan := make(chan struct{})
		hashManager := dc.newAlgorithmHashManager(4, shutdownChan)
		
		// Create enhanced iterator
		iterator := NewEnhancedFilesystemScanIterator(dc, []string{testDir}, "bench", hashManager)
		
		// Iterate through all files
		count := 0
		for {
			entry, err := iterator.Next()
			if err != nil {
				b.Fatalf("Unexpected error during iteration: %v", err)
			}
			
			if entry == nil {
				break // End of iteration
			}
			
			count++
		}
		
		// Cleanup
		iterator.Close()
		hashManager.Shutdown()
		close(shutdownChan)
		
		if count != numFiles {
			b.Errorf("Expected %d files, got %d", numFiles, count)
		}
	}
}

// Helper function for benchmark
func createTestDirectoryCacheForEnhancedBench(b *testing.B, testDir string) *DirectoryCache {
	// Create .dcfh directory
	dcfhDir := filepath.Join(testDir, ".dcfh")
	if err := os.MkdirAll(dcfhDir, 0755); err != nil {
		b.Fatalf("Failed to create .dcfh directory: %v", err)
	}
	
	return NewDirectoryCache(testDir, testDir)
}