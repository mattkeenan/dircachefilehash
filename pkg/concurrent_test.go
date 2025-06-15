package dircachefilehash

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestConcurrentOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrent tests in short mode")
	}

	t.Run("ConcurrentRead", func(t *testing.T) {
		th := setupTestEnvironment(t)
		defer th.cleanup(t)

		// Create and load index
		err := th.cache.ScanDirectory()
		if err != nil {
			t.Fatalf("ScanDirectory failed: %v", err)
		}

		err = th.cache.LoadIndex()
		if err != nil {
			t.Fatalf("LoadIndex failed: %v", err)
		}

		entries := th.cache.GetEntries()
		if len(entries) == 0 {
			t.Fatal("No entries for concurrent testing")
		}

		// Run multiple goroutines reading from the same cache
		numGoroutines := runtime.NumCPU() * 2
		iterationsPerGoroutine := 100

		var wg sync.WaitGroup
		errors := make(chan error, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(goroutineID int) {
				defer wg.Done()

				for j := 0; j < iterationsPerGoroutine; j++ {
					// Test GetEntries
					currentEntries := th.cache.GetEntries()
					if len(currentEntries) != len(entries) {
						errors <- fmt.Errorf("goroutine %d: GetEntries returned %d entries, expected %d",
							goroutineID, len(currentEntries), len(entries))
						return
					}

					// Test accessing entry data
					for _, entry := range currentEntries {
						_ = entry.RelativePath()
						_ = entry.HashString()
						_ = entry.EntrySize()
						_ = entry.RelativePathBytes()
					}

					// Test Stats
					count, totalSize, err := th.cache.Stats()
					if err != nil {
						errors <- fmt.Errorf("goroutine %d: Stats failed: %v", goroutineID, err)
						return
					}
					if count != len(entries) {
						errors <- fmt.Errorf("goroutine %d: Stats count mismatch", goroutineID)
						return
					}
					if totalSize == 0 {
						errors <- fmt.Errorf("goroutine %d: Stats total size is zero", goroutineID)
						return
					}

					// Test IsMmapped
					if !th.cache.IsMmapped() {
						errors <- fmt.Errorf("goroutine %d: Cache should be mmapped", goroutineID)
						return
					}
				}
			}(i)
		}

		wg.Wait()
		close(errors)

		// Check for any errors
		for err := range errors {
			t.Error(err)
		}
	})

	t.Run("ConcurrentStatus", func(t *testing.T) {
		th := setupTestEnvironment(t)
		defer th.cleanup(t)

		// Create and load index
		err := th.cache.ScanDirectory()
		if err != nil {
			t.Fatalf("ScanDirectory failed: %v", err)
		}

		err = th.cache.LoadIndex()
		if err != nil {
			t.Fatalf("LoadIndex failed: %v", err)
		}

		// Run multiple status checks concurrently
		numGoroutines := runtime.NumCPU()
		iterationsPerGoroutine := 10 // Status is more expensive

		var wg sync.WaitGroup
		errors := make(chan error, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(goroutineID int) {
				defer wg.Done()

				for j := 0; j < iterationsPerGoroutine; j++ {
					// Test Status
					status, err := th.cache.Status()
					if err != nil {
						errors <- fmt.Errorf("goroutine %d: Status failed: %v", goroutineID, err)
						return
					}

					if status.HasChanges() {
						errors <- fmt.Errorf("goroutine %d: Unexpected changes detected", goroutineID)
						return
					}

					// Test individual status methods
					modified, err := th.cache.GetModifiedFiles()
					if err != nil {
						errors <- fmt.Errorf("goroutine %d: GetModifiedFiles failed: %v", goroutineID, err)
						return
					}
					if len(modified) != 0 {
						errors <- fmt.Errorf("goroutine %d: Unexpected modified files", goroutineID)
						return
					}

					added, err := th.cache.GetAddedFiles()
					if err != nil {
						errors <- fmt.Errorf("goroutine %d: GetAddedFiles failed: %v", goroutineID, err)
						return
					}
					if len(added) != 0 {
						errors <- fmt.Errorf("goroutine %d: Unexpected added files", goroutineID)
						return
					}

					deleted, err := th.cache.GetDeletedFiles()
					if err != nil {
						errors <- fmt.Errorf("goroutine %d: GetDeletedFiles failed: %v", goroutineID, err)
						return
					}
					if len(deleted) != 0 {
						errors <- fmt.Errorf("goroutine %d: Unexpected deleted files", goroutineID)
						return
					}

					hasChanges, err := th.cache.HasChangesQuick()
					if err != nil {
						errors <- fmt.Errorf("goroutine %d: HasChangesQuick failed: %v", goroutineID, err)
						return
					}
					if hasChanges {
						errors <- fmt.Errorf("goroutine %d: HasChangesQuick detected unexpected changes", goroutineID)
						return
					}
				}
			}(i)
		}

		wg.Wait()
		close(errors)

		// Check for any errors
		for err := range errors {
			t.Error(err)
		}
	})

	t.Run("ConcurrentDuplicateDetection", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "concurrent_dupes_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create files with some duplicates
		duplicateContent := "This content is duplicated for concurrent testing"
		files := map[string]string{
			"unique1.txt":     "Unique content 1",
			"unique2.txt":     "Unique content 2",
			"dup1.txt":        duplicateContent,
			"subdir/dup2.txt": duplicateContent,
			"subdir/dup3.txt": duplicateContent,
			"unique3.txt":     "Unique content 3",
		}

		for filePath, content := range files {
			fullPath := filepath.Join(tempDir, filePath)
			if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
				t.Fatalf("Failed to create directory: %v", err)
			}
			if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
				t.Fatalf("Failed to create file: %v", err)
			}
		}

		cache := NewDirectoryCache(tempDir, "")
		defer cache.Close()

		err = cache.ScanDirectory()
		if err != nil {
			t.Fatalf("ScanDirectory failed: %v", err)
		}

		err = cache.LoadIndex()
		if err != nil {
			t.Fatalf("LoadIndex failed: %v", err)
		}

		// Run concurrent duplicate detection
		numGoroutines := runtime.NumCPU()
		iterationsPerGoroutine := 20

		var wg sync.WaitGroup
		errors := make(chan error, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(goroutineID int) {
				defer wg.Done()

				for j := 0; j < iterationsPerGoroutine; j++ {
					// Test FindDuplicates
					duplicates := cache.FindDuplicates()
					if len(duplicates) != 1 {
						errors <- fmt.Errorf("goroutine %d: Expected 1 duplicate group, got %d",
							goroutineID, len(duplicates))
						return
					}

					for hash, group := range duplicates {
						if len(group) != 3 {
							errors <- fmt.Errorf("goroutine %d: Expected 3 duplicates for hash %s, got %d",
								goroutineID, hash, len(group))
							return
						}

						// Test FindByHash with the same hash
						matches := cache.FindByHash(hash)
						if len(matches) != 3 {
							errors <- fmt.Errorf("goroutine %d: FindByHash returned %d matches, expected 3",
								goroutineID, len(matches))
							return
						}
					}
				}
			}(i)
		}

		wg.Wait()
		close(errors)

		// Check for any errors
		for err := range errors {
			t.Error(err)
		}
	})

	t.Run("ConcurrentMultipleCaches", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "concurrent_multi_cache_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create test files
		for i := 0; i < 20; i++ {
			filename := fmt.Sprintf("file_%03d.txt", i)
			content := fmt.Sprintf("Content for file %d", i)
			filePath := filepath.Join(tempDir, filename)

			if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
				t.Fatalf("Failed to create file: %v", err)
			}
		}

		// Create initial index with first cache
		cache1 := NewDirectoryCache(tempDir, "")
		err = cache1.ScanDirectory()
		if err != nil {
			t.Fatalf("Initial scan failed: %v", err)
		}
		cache1.Close()

		// Run multiple cache instances concurrently
		numGoroutines := runtime.NumCPU()
		iterationsPerGoroutine := 5

		var wg sync.WaitGroup
		errors := make(chan error, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(goroutineID int) {
				defer wg.Done()

				for j := 0; j < iterationsPerGoroutine; j++ {
					// Create a new cache instance
					cache := NewDirectoryCache(tempDir, "")
					defer cache.Close()

					// Load the existing index
					err := cache.LoadIndex()
					if err != nil {
						errors <- fmt.Errorf("goroutine %d: LoadIndex failed: %v", goroutineID, err)
						return
					}

					// Verify entries
					entries := cache.GetEntries()
					if len(entries) != 20 {
						errors <- fmt.Errorf("goroutine %d: Expected 20 entries, got %d",
							goroutineID, len(entries))
						return
					}

					// Test status
					status, err := cache.Status()
					if err != nil {
						errors <- fmt.Errorf("goroutine %d: Status failed: %v", goroutineID, err)
						return
					}

					if status.HasChanges() {
						errors <- fmt.Errorf("goroutine %d: Unexpected changes in concurrent cache", goroutineID)
						return
					}
				}
			}(i)
		}

		wg.Wait()
		close(errors)

		// Check for any errors
		for err := range errors {
			t.Error(err)
		}
	})
}

func TestStressConditions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress tests in short mode")
	}

	t.Run("LargeNumberOfFiles", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "stress_large_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create a large number of files
		numFiles := 5000
		numDirs := 50

		// Create directory structure
		for i := 0; i < numDirs; i++ {
			dirPath := filepath.Join(tempDir, fmt.Sprintf("dir_%04d", i))
			if err := os.MkdirAll(dirPath, 0755); err != nil {
				t.Fatalf("Failed to create directory: %v", err)
			}
		}

		// Create files
		for i := 0; i < numFiles; i++ {
			dirIndex := i % numDirs
			dirPath := filepath.Join(tempDir, fmt.Sprintf("dir_%04d", dirIndex))
			filename := fmt.Sprintf("file_%08d.txt", i)
			filePath := filepath.Join(dirPath, filename)

			content := fmt.Sprintf("File %d content: %s", i, strings.Repeat("x", i%100+10))

			if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
				t.Fatalf("Failed to create file %d: %v", i, err)
			}

			// Progress indication for long-running test
			if i%1000 == 0 && i > 0 {
				t.Logf("Created %d/%d files", i, numFiles)
			}
		}

		t.Logf("Created %d files in %d directories", numFiles, numDirs)

		cache := NewDirectoryCache(tempDir, "")
		defer cache.Close()

		// Test scanning performance
		startTime := time.Now()
		err = cache.ScanDirectory()
		scanDuration := time.Since(startTime)

		if err != nil {
			t.Fatalf("Scan of %d files failed: %v", numFiles, err)
		}

		t.Logf("Scanned %d files in %v (%.1f files/sec)",
			numFiles, scanDuration, float64(numFiles)/scanDuration.Seconds())

		// Test loading performance
		startTime = time.Now()
		err = cache.LoadIndex()
		loadDuration := time.Since(startTime)

		if err != nil {
			t.Fatalf("Load of %d files failed: %v", numFiles, err)
		}

		t.Logf("Loaded %d files in %v (%.1f files/sec)",
			numFiles, loadDuration, float64(numFiles)/loadDuration.Seconds())

		// Verify all files are present
		entries := cache.GetEntries()
		if len(entries) != numFiles {
			t.Errorf("Expected %d entries, got %d", numFiles, len(entries))
		}

		// Test status performance
		startTime = time.Now()
		status, err := cache.Status()
		statusDuration := time.Since(startTime)

		if err != nil {
			t.Fatalf("Status of %d files failed: %v", numFiles, err)
		}

		t.Logf("Status check for %d files in %v (%.1f files/sec)",
			numFiles, statusDuration, float64(numFiles)/statusDuration.Seconds())

		if status.HasChanges() {
			t.Error("Large file set should have no initial changes")
		}

		// Test memory efficiency
		count, totalSize, err := cache.Stats()
		if err != nil {
			t.Fatalf("Stats failed: %v", err)
		}

		t.Logf("Stats: %d files, %d total bytes", count, totalSize)

		if count != numFiles {
			t.Errorf("Stats count mismatch: expected %d, got %d", numFiles, count)
		}
	})

	t.Run("RapidFileChanges", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "stress_rapid_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create initial files
		numFiles := 100
		for i := 0; i < numFiles; i++ {
			filename := fmt.Sprintf("file_%03d.txt", i)
			content := fmt.Sprintf("Initial content %d", i)
			filePath := filepath.Join(tempDir, filename)

			if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
				t.Fatalf("Failed to create initial file: %v", err)
			}
		}

		cache := NewDirectoryCache(tempDir, "")
		defer cache.Close()

		// Initial scan
		err = cache.ScanDirectory()
		if err != nil {
			t.Fatalf("Initial scan failed: %v", err)
		}

		err = cache.LoadIndex()
		if err != nil {
			t.Fatalf("Initial load failed: %v", err)
		}

		// Perform rapid changes and status checks
		numIterations := 20
		for iteration := 0; iteration < numIterations; iteration++ {
			// Modify a subset of files
			modifyCount := 20
			for i := 0; i < modifyCount; i++ {
				fileIndex := (iteration*modifyCount + i) % numFiles
				filename := fmt.Sprintf("file_%03d.txt", fileIndex)
				filePath := filepath.Join(tempDir, filename)

				content := fmt.Sprintf("Modified content %d iteration %d", fileIndex, iteration)

				// Add small delay to ensure different timestamps
				time.Sleep(time.Millisecond)

				if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
					t.Fatalf("Failed to modify file in iteration %d: %v", iteration, err)
				}
			}

			// Check status
			status, err := cache.Status()
			if err != nil {
				t.Fatalf("Status failed in iteration %d: %v", iteration, err)
			}

			if len(status.Modified) != modifyCount {
				t.Errorf("Iteration %d: Expected %d modified files, got %d",
					iteration, modifyCount, len(status.Modified))
			}

			// Update index
			err = cache.Update()
			if err != nil {
				t.Fatalf("Update failed in iteration %d: %v", iteration, err)
			}

			// Verify clean status after update
			status, err = cache.Status()
			if err != nil {
				t.Fatalf("Status after update failed in iteration %d: %v", iteration, err)
			}

			if status.HasChanges() {
				t.Errorf("Iteration %d: Should have no changes after update", iteration)
			}

			if iteration%5 == 0 {
				t.Logf("Completed iteration %d/%d", iteration, numIterations)
			}
		}
	})

	t.Run("DeepDirectoryNesting", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "stress_deep_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create deeply nested directory structure
		maxDepth := 20
		filesPerLevel := 5

		var createdFiles []string

		for depth := 0; depth < maxDepth; depth++ {
			// Build path for this depth
			pathParts := make([]string, depth+1)
			pathParts[0] = tempDir
			for i := 1; i <= depth; i++ {
				pathParts[i] = fmt.Sprintf("level_%02d", i)
			}
			dirPath := filepath.Join(pathParts...)

			// Create directory
			if err := os.MkdirAll(dirPath, 0755); err != nil {
				t.Fatalf("Failed to create directory at depth %d: %v", depth, err)
			}

			// Create files in this directory
			for fileIndex := 0; fileIndex < filesPerLevel; fileIndex++ {
				filename := fmt.Sprintf("file_d%02d_f%02d.txt", depth, fileIndex)
				filePath := filepath.Join(dirPath, filename)
				content := fmt.Sprintf("File at depth %d, index %d\nPath: %s", depth, fileIndex, filePath)

				if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
					t.Fatalf("Failed to create file at depth %d: %v", depth, err)
				}

				// Store relative path for verification
				relPath, _ := filepath.Rel(tempDir, filePath)
				createdFiles = append(createdFiles, relPath)
			}
		}

		totalFiles := maxDepth * filesPerLevel
		t.Logf("Created %d files across %d directory levels", totalFiles, maxDepth)

		cache := NewDirectoryCache(tempDir, "")
		defer cache.Close()

		// Test scanning deep structure
		startTime := time.Now()
		err = cache.ScanDirectory()
		scanDuration := time.Since(startTime)

		if err != nil {
			t.Fatalf("Scan of deep structure failed: %v", err)
		}

		t.Logf("Scanned deep structure in %v", scanDuration)

		err = cache.LoadIndex()
		if err != nil {
			t.Fatalf("Load of deep structure failed: %v", err)
		}

		// Verify all files are found
		entries := cache.GetEntries()
		if len(entries) != totalFiles {
			t.Errorf("Expected %d entries for deep structure, got %d", totalFiles, len(entries))
		}

		// Verify file paths
		foundPaths := make(map[string]bool)
		for _, entry := range entries {
			foundPaths[entry.RelativePath()] = true
		}

		for _, expectedPath := range createdFiles {
			if !foundPaths[expectedPath] {
				t.Errorf("Expected file not found in deep structure: %s", expectedPath)
			}
		}

		// Test status performance on deep structure
		startTime = time.Now()
		status, err := cache.Status()
		statusDuration := time.Since(startTime)

		if err != nil {
			t.Fatalf("Status of deep structure failed: %v", err)
		}

		t.Logf("Status check of deep structure in %v", statusDuration)

		if status.HasChanges() {
			t.Error("Deep structure should have no initial changes")
		}
	})
}

func TestMemoryPressure(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory pressure tests in short mode")
	}

	t.Run("MemoryEfficiencyTest", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "stress_memory_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create many files to test memory efficiency
		numFiles := 10000

		// Create files with varying content sizes
		for i := 0; i < numFiles; i++ {
			// Organize into subdirectories to test directory traversal
			subDir := fmt.Sprintf("subdir_%04d", i/1000)
			subDirPath := filepath.Join(tempDir, subDir)
			if err := os.MkdirAll(subDirPath, 0755); err != nil {
				t.Fatalf("Failed to create subdirectory: %v", err)
			}

			filename := fmt.Sprintf("memfile_%08d.txt", i)
			filePath := filepath.Join(subDirPath, filename)

			// Create content of varying sizes to test memory handling
			contentSize := 500 + (i % 2000) // 500 to 2499 bytes
			content := fmt.Sprintf("File %d\n", i) + strings.Repeat(fmt.Sprintf("line %d ", i%100), contentSize/10)

			if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
				t.Fatalf("Failed to create memory test file %d: %v", i, err)
			}

			if i%2000 == 0 && i > 0 {
				t.Logf("Created %d/%d files for memory test", i, numFiles)
			}
		}

		// Force garbage collection before testing
		runtime.GC()
		runtime.GC()

		var m1, m2 runtime.MemStats
		runtime.ReadMemStats(&m1)

		cache := NewDirectoryCache(tempDir, "")
		defer cache.Close()

		// Test memory usage during scan
		err = cache.ScanDirectory()
		if err != nil {
			t.Fatalf("Memory test scan failed: %v", err)
		}

		runtime.ReadMemStats(&m2)
		scanMemoryIncrease := m2.Alloc - m1.Alloc
		t.Logf("Memory increase during scan: %d bytes (%.2f MB)",
			scanMemoryIncrease, float64(scanMemoryIncrease)/(1024*1024))

		// Test memory usage during load (should be minimal due to mmap)
		runtime.ReadMemStats(&m1)

		err = cache.LoadIndex()
		if err != nil {
			t.Fatalf("Memory test load failed: %v", err)
		}

		runtime.ReadMemStats(&m2)
		loadMemoryIncrease := m2.Alloc - m1.Alloc
		t.Logf("Memory increase during load: %d bytes (%.2f MB)",
			loadMemoryIncrease, float64(loadMemoryIncrease)/(1024*1024))

		// Verify mmap is being used for memory efficiency
		if !cache.IsMmapped() {
			t.Error("Cache should be using mmap for memory efficiency")
		}

		// Test accessing all entries (should use minimal additional memory due to zero-copy)
		runtime.ReadMemStats(&m1)

		entries := cache.GetEntries()
		if len(entries) != numFiles {
			t.Errorf("Expected %d entries, got %d", numFiles, len(entries))
		}

		// Access all entry data
		var totalSize uint64
		var longestPath string
		for _, entry := range entries {
			totalSize += entry.FileSize
			path := entry.RelativePath()
			if len(path) > len(longestPath) {
				longestPath = path
			}
			_ = entry.HashString()
			_ = entry.EntrySize()
		}

		runtime.ReadMemStats(&m2)
		accessMemoryIncrease := m2.Alloc - m1.Alloc
		t.Logf("Memory increase during entry access: %d bytes (%.2f MB)",
			accessMemoryIncrease, float64(accessMemoryIncrease)/(1024*1024))

		t.Logf("Processed %d files, total size %d bytes, longest path: %s",
			len(entries), totalSize, longestPath)

		// Memory usage should be reasonable compared to data size
		bytesPerFile := float64(scanMemoryIncrease) / float64(numFiles)
		t.Logf("Average memory per file during scan: %.2f bytes", bytesPerFile)

		// Load should use much less memory than scan due to mmap
		if loadMemoryIncrease > scanMemoryIncrease/2 {
			t.Logf("Warning: Load memory usage (%d) is high compared to scan (%d)",
				loadMemoryIncrease, scanMemoryIncrease)
		}

		// Access should use minimal additional memory due to zero-copy
		maxReasonableAccessMemory := uint64(numFiles * 100) // 100 bytes per file seems reasonable
		if accessMemoryIncrease > maxReasonableAccessMemory {
			t.Logf("Warning: Entry access memory usage (%d) is higher than expected (max %d)",
				accessMemoryIncrease, maxReasonableAccessMemory)
		}
	})

	t.Run("RepeatedOperations", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "stress_repeated_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create moderate number of files
		numFiles := 1000
		for i := 0; i < numFiles; i++ {
			filename := fmt.Sprintf("repeat_%04d.txt", i)
			content := fmt.Sprintf("Repeated test file %d content", i)
			filePath := filepath.Join(tempDir, filename)

			if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
				t.Fatalf("Failed to create repeated test file: %v", err)
			}
		}

		// Perform many repeated operations to test for memory leaks
		numIterations := 50
		for iteration := 0; iteration < numIterations; iteration++ {
			cache := NewDirectoryCache(tempDir, "")

			// Scan and load
			err = cache.ScanDirectory()
			if err != nil {
				t.Fatalf("Repeated scan %d failed: %v", iteration, err)
			}

			err = cache.LoadIndex()
			if err != nil {
				t.Fatalf("Repeated load %d failed: %v", iteration, err)
			}

			// Access entries
			entries := cache.GetEntries()
			if len(entries) != numFiles {
				t.Errorf("Repeated iteration %d: Expected %d entries, got %d",
					iteration, numFiles, len(entries))
			}

			// Perform various operations
			_, _, err = cache.Stats()
			if err != nil {
				t.Fatalf("Repeated stats %d failed: %v", iteration, err)
			}

			status, err := cache.Status()
			if err != nil {
				t.Fatalf("Repeated status %d failed: %v", iteration, err)
			}

			if status.HasChanges() {
				t.Errorf("Repeated iteration %d: Unexpected changes", iteration)
			}

			// Clean up
			cache.Close()

			// Periodic garbage collection and memory check
			if iteration%10 == 0 {
				runtime.GC()
				runtime.GC()

				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				t.Logf("Iteration %d/%d: Current memory usage: %.2f MB",
					iteration, numIterations, float64(m.Alloc)/(1024*1024))
			}
		}

		t.Logf("Completed %d repeated operations successfully", numIterations)
	})
}
