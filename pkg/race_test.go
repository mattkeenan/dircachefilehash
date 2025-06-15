//go:build race
// +build race

package dircachefilehash

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// These tests are specifically designed to detect race conditions
// Run with: go test -race -tags=race

func TestRaceConditionsInConcurrentAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping race condition tests in short mode")
	}

	t.Run("ConcurrentEntryAccess", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "race_entry_access_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create test files
		numFiles := 100
		for i := 0; i < numFiles; i++ {
			filename := fmt.Sprintf("racefile_%04d.txt", i)
			content := fmt.Sprintf("Race test content for file %d", i)
			filePath := filepath.Join(tempDir, filename)

			if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
				t.Fatalf("Failed to create race test file: %v", err)
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

		entries := cache.GetEntries()
		if len(entries) == 0 {
			t.Fatal("No entries for race testing")
		}

		// Launch many goroutines that access entry data concurrently
		numGoroutines := runtime.NumCPU() * 4
		iterations := 1000

		var wg sync.WaitGroup

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(goroutineID int) {
				defer wg.Done()

				for j := 0; j < iterations; j++ {
					// Access different entries to test concurrent read safety
					entryIndex := (goroutineID + j) % len(entries)
					entry := entries[entryIndex]

					// These operations should be safe for concurrent access
					_ = entry.RelativePath()
					_ = entry.RelativePathBytes()
					_ = entry.HashString()
					_ = entry.EntrySize()

					// Brief yield to increase chance of race detection
					if j%10 == 0 {
						runtime.Gosched()
					}
				}
			}(i)
		}

		wg.Wait()
	})

	t.Run("ConcurrentStatusChecks", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "race_status_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create test files
		for i := 0; i < 50; i++ {
			filename := fmt.Sprintf("statusfile_%03d.txt", i)
			content := fmt.Sprintf("Status test content %d", i)
			filePath := filepath.Join(tempDir, filename)

			if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
				t.Fatalf("Failed to create status test file: %v", err)
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

		// Run concurrent status checks
		numGoroutines := runtime.NumCPU() * 2
		iterations := 50

		var wg sync.WaitGroup
		errorChan := make(chan error, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(goroutineID int) {
				defer wg.Done()

				for j := 0; j < iterations; j++ {
					// Different status operations
					switch j % 5 {
					case 0:
						_, err := cache.Status()
						if err != nil {
							errorChan <- fmt.Errorf("goroutine %d: Status failed: %v", goroutineID, err)
							return
						}
					case 1:
						_, err := cache.GetModifiedFiles()
						if err != nil {
							errorChan <- fmt.Errorf("goroutine %d: GetModifiedFiles failed: %v", goroutineID, err)
							return
						}
					case 2:
						_, err := cache.GetAddedFiles()
						if err != nil {
							errorChan <- fmt.Errorf("goroutine %d: GetAddedFiles failed: %v", goroutineID, err)
							return
						}
					case 3:
						_, err := cache.GetDeletedFiles()
						if err != nil {
							errorChan <- fmt.Errorf("goroutine %d: GetDeletedFiles failed: %v", goroutineID, err)
							return
						}
					case 4:
						_, err := cache.HasChangesQuick()
						if err != nil {
							errorChan <- fmt.Errorf("goroutine %d: HasChangesQuick failed: %v", goroutineID, err)
							return
						}
					}

					// Occasional yield
					if j%10 == 0 {
						runtime.Gosched()
					}
				}
			}(i)
		}

		wg.Wait()
		close(errorChan)

		// Check for errors
		for err := range errorChan {
			t.Error(err)
		}
	})

	t.Run("ConcurrentCacheOperations", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "race_cache_ops_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create test files
		for i := 0; i < 30; i++ {
			filename := fmt.Sprintf("cacheop_%03d.txt", i)
			content := fmt.Sprintf("Cache operation test %d", i)
			filePath := filepath.Join(tempDir, filename)

			if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
				t.Fatalf("Failed to create cache test file: %v", err)
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

		// Run concurrent cache operations
		numGoroutines := runtime.NumCPU()
		iterations := 100

		var wg sync.WaitGroup
		errorChan := make(chan error, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(goroutineID int) {
				defer wg.Done()

				for j := 0; j < iterations; j++ {
					// Different cache operations
					switch j % 6 {
					case 0:
						entries := cache.GetEntries()
						if len(entries) == 0 {
							errorChan <- fmt.Errorf("goroutine %d: GetEntries returned empty", goroutineID)
							return
						}
					case 1:
						_, _, err := cache.Stats()
						if err != nil {
							errorChan <- fmt.Errorf("goroutine %d: Stats failed: %v", goroutineID, err)
							return
						}
					case 2:
						if !cache.IsMmapped() {
							errorChan <- fmt.Errorf("goroutine %d: Cache should be mmapped", goroutineID)
							return
						}
					case 3:
						duplicates := cache.FindDuplicates()
						_ = duplicates // Just access the result
					case 4:
						// Find by a hash that should exist
						entries := cache.GetEntries()
						if len(entries) > 0 {
							hash := entries[0].HashString()
							matches := cache.FindByHash(hash)
							if len(matches) == 0 {
								errorChan <- fmt.Errorf("goroutine %d: FindByHash found no matches", goroutineID)
								return
							}
						}
					case 5:
						// StatusWithCallback
						callbackCalled := false
						err := cache.StatusWithCallback(func(status FileStatus, path string, indexEntry, diskEntry *binaryEntry) {
							callbackCalled = true
						})
						if err != nil {
							errorChan <- fmt.Errorf("goroutine %d: StatusWithCallback failed: %v", goroutineID, err)
							return
						}
						if !callbackCalled {
							errorChan <- fmt.Errorf("goroutine %d: StatusWithCallback didn't call callback", goroutineID)
							return
						}
					}

					// Yield occasionally
					if j%20 == 0 {
						runtime.Gosched()
					}
				}
			}(i)
		}

		wg.Wait()
		close(errorChan)

		// Check for errors
		for err := range errorChan {
			t.Error(err)
		}
	})

	t.Run("ConcurrentSkiplistOperations", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "race_skiplist_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create test files
		for i := 0; i < 50; i++ {
			filename := fmt.Sprintf("skiplist_%03d.txt", i)
			content := fmt.Sprintf("Skiplist test %d", i)
			filePath := filepath.Join(tempDir, filename)

			if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
				t.Fatalf("Failed to create skiplist test file: %v", err)
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

		entries := cache.GetEntries()
		if len(entries) == 0 {
			t.Fatal("No entries for skiplist race testing")
		}

		// Create skiplist and populate it
		skiplist := NewSkiplistWrapper(16)
		skiplist.InsertBatch(entries)

		// Run concurrent skiplist operations
		numGoroutines := runtime.NumCPU()
		iterations := 200

		var wg sync.WaitGroup
		errorChan := make(chan error, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(goroutineID int) {
				defer wg.Done()

				for j := 0; j < iterations; j++ {
					// Different skiplist operations
					switch j % 7 {
					case 0:
						sorted := skiplist.GetSortedEntries()
						if len(sorted) != len(entries) {
							errorChan <- fmt.Errorf("goroutine %d: GetSortedEntries length mismatch", goroutineID)
							return
						}
					case 1:
						if skiplist.Length() != len(entries) {
							errorChan <- fmt.Errorf("goroutine %d: Length mismatch", goroutineID)
							return
						}
					case 2:
						if skiplist.IsEmpty() {
							errorChan <- fmt.Errorf("goroutine %d: Skiplist should not be empty", goroutineID)
							return
						}
					case 3:
						// Find existing entry
						targetPath := entries[j%len(entries)].RelativePath()
						found := skiplist.Find(targetPath)
						if found == nil {
							errorChan <- fmt.Errorf("goroutine %d: Failed to find entry %s", goroutineID, targetPath)
							return
						}
					case 4:
						// ForEach operation
						count := 0
						skiplist.ForEach(func(entry *binaryEntry) bool {
							count++
							return count < 10 // Stop after 10 for performance
						})
					case 5:
						// Copy operation
						copied := skiplist.Copy()
						if copied.Length() != skiplist.Length() {
							errorChan <- fmt.Errorf("goroutine %d: Copy length mismatch", goroutineID)
							return
						}
					case 6:
						// Insert operation (safe due to mutex in skiplist)
						if len(entries) > 0 {
							skiplist.Insert(entries[j%len(entries)])
						}
					}

					// Yield occasionally
					if j%25 == 0 {
						runtime.Gosched()
					}
				}
			}(i)
		}

		wg.Wait()
		close(errorChan)

		// Check for errors
		for err := range errorChan {
			t.Error(err)
		}
	})

	t.Run("ConcurrentMemoryAccess", func(t *testing.T) {
		// Test concurrent access to memory-mapped data
		tempDir, err := os.MkdirTemp("", "race_memory_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create test files with longer paths to test memory boundary conditions
		for i := 0; i < 30; i++ {
			subdir := fmt.Sprintf("very_long_subdirectory_name_level_%03d", i/10)
			filename := fmt.Sprintf("very_long_filename_for_memory_testing_%04d.txt", i)

			fullSubdir := filepath.Join(tempDir, subdir)
			if err := os.MkdirAll(fullSubdir, 0755); err != nil {
				t.Fatalf("Failed to create long subdir: %v", err)
			}

			filePath := filepath.Join(fullSubdir, filename)
			content := fmt.Sprintf("Memory access test content for file %d with some additional text to make it longer", i)

			if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
				t.Fatalf("Failed to create memory test file: %v", err)
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

		entries := cache.GetEntries()
		if len(entries) == 0 {
			t.Fatal("No entries for memory race testing")
		}

		// Test concurrent access to memory-mapped entry data
		numGoroutines := runtime.NumCPU() * 2
		iterations := 500

		var wg sync.WaitGroup
		errorChan := make(chan error, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(goroutineID int) {
				defer wg.Done()

				for j := 0; j < iterations; j++ {
					// Access different entries to test memory boundaries
					entryIndex := (goroutineID*iterations + j) % len(entries)
					entry := entries[entryIndex]

					// These operations access memory-mapped data
					path := entry.RelativePath()
					pathBytes := entry.RelativePathBytes()
					hash := entry.HashString()
					size := entry.EntrySize()

					// Validate the data
					if len(path) == 0 {
						errorChan <- fmt.Errorf("goroutine %d: Empty path", goroutineID)
						return
					}

					if len(pathBytes) == 0 {
						errorChan <- fmt.Errorf("goroutine %d: Empty path bytes", goroutineID)
						return
					}

					if string(pathBytes) != path {
						errorChan <- fmt.Errorf("goroutine %d: Path mismatch", goroutineID)
						return
					}

					if len(hash) != 40 { // SHA-1 hash length
						errorChan <- fmt.Errorf("goroutine %d: Invalid hash length", goroutineID)
						return
					}

					if size == 0 {
						errorChan <- fmt.Errorf("goroutine %d: Zero entry size", goroutineID)
						return
					}

					// Brief yield
					if j%50 == 0 {
						runtime.Gosched()
					}
				}
			}(i)
		}

		wg.Wait()
		close(errorChan)

		// Check for errors
		for err := range errorChan {
			t.Error(err)
		}
	})
}

func TestRaceConditionsInStateModification(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping race condition tests in short mode")
	}

	t.Run("ConcurrentFileModificationAndStatus", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "race_modification_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create initial files
		numFiles := 20
		for i := 0; i < numFiles; i++ {
			filename := fmt.Sprintf("modfile_%03d.txt", i)
			content := fmt.Sprintf("Initial content %d", i)
			filePath := filepath.Join(tempDir, filename)

			if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
				t.Fatalf("Failed to create modification test file: %v", err)
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

		// Run concurrent file modifications and status checks
		var wg sync.WaitGroup
		stopChan := make(chan struct{})
		errorChan := make(chan error, 10)

		// Goroutine that modifies files
		wg.Add(1)
		go func() {
			defer wg.Done()
			counter := 0

			for {
				select {
				case <-stopChan:
					return
				default:
					// Modify a file
					fileIndex := counter % numFiles
					filename := fmt.Sprintf("modfile_%03d.txt", fileIndex)
					filePath := filepath.Join(tempDir, filename)

					newContent := fmt.Sprintf("Modified content %d at iteration %d", fileIndex, counter)

					// Add small delay to ensure timestamp differences
					time.Sleep(time.Millisecond)

					err := os.WriteFile(filePath, []byte(newContent), 0644)
					if err != nil {
						errorChan <- fmt.Errorf("file modifier: Failed to modify file: %v", err)
						return
					}

					counter++

					// Brief pause between modifications
					time.Sleep(time.Millisecond * 5)
				}
			}
		}()

		// Multiple goroutines checking status
		numStatusCheckers := 3
		for i := 0; i < numStatusCheckers; i++ {
			wg.Add(1)
			go func(checkerID int) {
				defer wg.Done()

				for {
					select {
					case <-stopChan:
						return
					default:
						// Check status
						_, err := cache.Status()
						if err != nil {
							errorChan <- fmt.Errorf("status checker %d: Status failed: %v", checkerID, err)
							return
						}

						// Brief pause between checks
						time.Sleep(time.Millisecond * 2)
					}
				}
			}(i)
		}

		// Let the race run for a short time
		time.Sleep(time.Millisecond * 100)

		// Stop all goroutines
		close(stopChan)
		wg.Wait()
		close(errorChan)

		// Check for errors
		for err := range errorChan {
			t.Error(err)
		}
	})

	t.Run("ConcurrentCacheInstancesOnSameDirectory", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "race_multi_cache_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create test files
		for i := 0; i < 15; i++ {
			filename := fmt.Sprintf("multicache_%03d.txt", i)
			content := fmt.Sprintf("Multi-cache test %d", i)
			filePath := filepath.Join(tempDir, filename)

			if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
				t.Fatalf("Failed to create multi-cache test file: %v", err)
			}
		}

		// Create initial index with one cache
		initialCache := NewDirectoryCache(tempDir, "")
		err = initialCache.ScanDirectory()
		if err != nil {
			t.Fatalf("Initial scan failed: %v", err)
		}
		initialCache.Close()

		// Run multiple cache instances concurrently
		numCaches := runtime.NumCPU()
		var wg sync.WaitGroup
		errorChan := make(chan error, numCaches)

		for i := 0; i < numCaches; i++ {
			wg.Add(1)
			go func(cacheID int) {
				defer wg.Done()

				// Create cache instance
				cache := NewDirectoryCache(tempDir, "")
				defer cache.Close()

				// Load existing index
				err := cache.LoadIndex()
				if err != nil {
					errorChan <- fmt.Errorf("cache %d: LoadIndex failed: %v", cacheID, err)
					return
				}

				// Perform various operations
				for j := 0; j < 50; j++ {
					switch j % 4 {
					case 0:
						entries := cache.GetEntries()
						if len(entries) == 0 {
							errorChan <- fmt.Errorf("cache %d: No entries found", cacheID)
							return
						}
					case 1:
						_, _, err := cache.Stats()
						if err != nil {
							errorChan <- fmt.Errorf("cache %d: Stats failed: %v", cacheID, err)
							return
						}
					case 2:
						_, err := cache.Status()
						if err != nil {
							errorChan <- fmt.Errorf("cache %d: Status failed: %v", cacheID, err)
							return
						}
					case 3:
						_ = cache.FindDuplicates()
					}

					// Brief yield
					if j%10 == 0 {
						runtime.Gosched()
					}
				}
			}(i)
		}

		wg.Wait()
		close(errorChan)

		// Check for errors
		for err := range errorChan {
			t.Error(err)
		}
	})
}

func TestRaceConditionsDetection(t *testing.T) {
	// This test is designed to maximize the chance of detecting race conditions
	// by creating high contention scenarios

	if testing.Short() {
		t.Skip("Skipping intensive race detection tests in short mode")
	}

	t.Run("HighContentionDataAccess", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "race_high_contention_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create files with varying path lengths to test different memory layouts
		pathLengths := []int{10, 50, 100, 200}
		for i, pathLen := range pathLengths {
			for j := 0; j < 10; j++ {
				// Create path of specific length
				subdir := fmt.Sprintf("dir_%d", i)
				basename := fmt.Sprintf("file_%d_", j)
				padding := pathLen - len(subdir) - len(basename) - 5 // Account for separators and extension
				if padding < 0 {
					padding = 0
				}
				filename := basename + fmt.Sprintf("%0*d.txt", padding, j)

				fullSubdir := filepath.Join(tempDir, subdir)
				if err := os.MkdirAll(fullSubdir, 0755); err != nil {
					t.Fatalf("Failed to create subdir: %v", err)
				}

				filePath := filepath.Join(fullSubdir, filename)
				content := fmt.Sprintf("High contention test content for file %d-%d", i, j)

				if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
					t.Fatalf("Failed to create contention test file: %v", err)
				}
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

		entries := cache.GetEntries()
		if len(entries) == 0 {
			t.Fatal("No entries for high contention testing")
		}

		// Create very high contention scenario
		numGoroutines := runtime.NumCPU() * 8 // More goroutines than CPUs
		iterations := 2000                    // Many iterations

		var wg sync.WaitGroup

		// All goroutines access the same data simultaneously
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(goroutineID int) {
				defer wg.Done()

				for j := 0; j < iterations; j++ {
					// Access the same entries from all goroutines to maximize contention
					for k := 0; k < len(entries) && k < 5; k++ { // Limit to first 5 entries
						entry := entries[k]

						// Rapid successive access to the same memory locations
						_ = entry.RelativePath()
						_ = entry.RelativePathBytes()
						_ = entry.HashString()
						_ = entry.EntrySize()
					}

					// No yielding to maximize contention
				}
			}(i)
		}

		wg.Wait()
	})

	t.Run("RapidStateChanges", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "race_rapid_state_*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		// Create a small set of files for rapid modification
		numFiles := 5
		for i := 0; i < numFiles; i++ {
			filename := fmt.Sprintf("rapid_%02d.txt", i)
			content := fmt.Sprintf("Rapid test %d", i)
			filePath := filepath.Join(tempDir, filename)

			if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
				t.Fatalf("Failed to create rapid test file: %v", err)
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

		// Rapid state changes with concurrent access
		duration := time.Millisecond * 200 // Short duration, high intensity
		stopTime := time.Now().Add(duration)

		var wg sync.WaitGroup

		// Rapid file modifier
		wg.Add(1)
		go func() {
			defer wg.Done()
			counter := 0

			for time.Now().Before(stopTime) {
				fileIndex := counter % numFiles
				filename := fmt.Sprintf("rapid_%02d.txt", fileIndex)
				filePath := filepath.Join(tempDir, filename)

				newContent := fmt.Sprintf("Rapid modified %d-%d", fileIndex, counter)
				os.WriteFile(filePath, []byte(newContent), 0644)

				counter++
			}
		}()

		// Multiple concurrent status checkers
		for i := 0; i < runtime.NumCPU(); i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				for time.Now().Before(stopTime) {
					cache.Status()
					cache.HasChangesQuick()
					cache.GetEntries()
				}
			}()
		}

		wg.Wait()
	})
}

func BenchmarkRaceConditions(b *testing.B) {
	// Benchmark concurrent access patterns to help identify race conditions

	tempDir, err := os.MkdirTemp("", "bench_race_*")
	if err != nil {
		b.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test files
	for i := 0; i < 100; i++ {
		filename := fmt.Sprintf("bench_%04d.txt", i)
		content := fmt.Sprintf("Benchmark content %d", i)
		filePath := filepath.Join(tempDir, filename)

		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			b.Fatalf("Failed to create benchmark file: %v", err)
		}
	}

	cache := NewDirectoryCache(tempDir, "")
	defer cache.Close()

	cache.ScanDirectory()
	cache.LoadIndex()

	entries := cache.GetEntries()

	b.Run("ConcurrentEntryAccess", func(b *testing.B) {
		b.SetParallelism(runtime.NumCPU() * 2)
		b.ResetTimer()

		b.RunParallel(func(pb *testing.PB) {
			entryIndex := 0
			for pb.Next() {
				entry := entries[entryIndex%len(entries)]
				_ = entry.RelativePath()
				_ = entry.HashString()
				entryIndex++
			}
		})
	})

	b.Run("ConcurrentStatusChecks", func(b *testing.B) {
		b.SetParallelism(runtime.NumCPU())
		b.ResetTimer()

		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				cache.HasChangesQuick()
			}
		})
	})
}
