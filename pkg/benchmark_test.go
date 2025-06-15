package dircachefilehash

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkHelper contains utilities for benchmark tests
type BenchmarkHelper struct {
	tempDir string
	cache   *DirectoryCache
}

// setupBenchmarkEnvironment creates a directory with many files for benchmarking
func setupBenchmarkEnvironment(b *testing.B, fileCount int) *BenchmarkHelper {
	tempDir, err := os.MkdirTemp("", "dircachefilehash_bench_*")
	if err != nil {
		b.Fatalf("Failed to create temp dir: %v", err)
	}

	// Create many test files with varying content
	for i := 0; i < fileCount; i++ {
		filename := filepath.Join(tempDir, fmt.Sprintf("file_%06d.txt", i))
		content := fmt.Sprintf("This is test file number %d with some content to hash", i)

		// Create some subdirectories
		if i%100 == 0 && i > 0 {
			subdir := filepath.Join(tempDir, fmt.Sprintf("subdir_%03d", i/100))
			if err := os.MkdirAll(subdir, 0755); err != nil {
				b.Fatalf("Failed to create subdir: %v", err)
			}
			filename = filepath.Join(subdir, fmt.Sprintf("file_%06d.txt", i))
		}

		if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
			b.Fatalf("Failed to create directory for %s: %v", filename, err)
		}

		if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
			b.Fatalf("Failed to create benchmark file %s: %v", filename, err)
		}
	}

	cache := NewDirectoryCache(tempDir, "")

	return &BenchmarkHelper{
		tempDir: tempDir,
		cache:   cache,
	}
}

// cleanup removes the temporary directory
func (bh *BenchmarkHelper) cleanup(b *testing.B) {
	if bh.cache != nil {
		bh.cache.Close()
	}
	if err := os.RemoveAll(bh.tempDir); err != nil {
		b.Logf("Warning: Failed to cleanup temp dir %s: %v", bh.tempDir, err)
	}
}

func BenchmarkScanDirectory(b *testing.B) {
	testSizes := []int{100, 1000, 5000}

	for _, size := range testSizes {
		b.Run(fmt.Sprintf("files_%d", size), func(b *testing.B) {
			bh := setupBenchmarkEnvironment(b, size)
			defer bh.cleanup(b)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				// Create a new cache for each iteration to avoid reusing state
				cache := NewDirectoryCache(bh.tempDir, "")
				err := cache.ScanDirectory()
				if err != nil {
					b.Fatalf("ScanDirectory failed: %v", err)
				}
				cache.Close()
			}
		})
	}
}

func BenchmarkLoadIndex(b *testing.B) {
	testSizes := []int{100, 1000, 5000}

	for _, size := range testSizes {
		b.Run(fmt.Sprintf("files_%d", size), func(b *testing.B) {
			bh := setupBenchmarkEnvironment(b, size)
			defer bh.cleanup(b)

			// Create the index once
			err := bh.cache.ScanDirectory()
			if err != nil {
				b.Fatalf("ScanDirectory failed: %v", err)
			}
			bh.cache.Close()

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				cache := NewDirectoryCache(bh.tempDir, "")
				err := cache.LoadIndex()
				if err != nil {
					b.Fatalf("LoadIndex failed: %v", err)
				}
				cache.Close()
			}
		})
	}
}

func BenchmarkGetEntries(b *testing.B) {
	testSizes := []int{100, 1000, 5000}

	for _, size := range testSizes {
		b.Run(fmt.Sprintf("files_%d", size), func(b *testing.B) {
			bh := setupBenchmarkEnvironment(b, size)
			defer bh.cleanup(b)

			// Setup the cache
			err := bh.cache.ScanDirectory()
			if err != nil {
				b.Fatalf("ScanDirectory failed: %v", err)
			}

			err = bh.cache.LoadIndex()
			if err != nil {
				b.Fatalf("LoadIndex failed: %v", err)
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				entries := bh.cache.GetEntries()
				if len(entries) != size {
					b.Fatalf("Expected %d entries, got %d", size, len(entries))
				}
			}
		})
	}
}

func BenchmarkStatus(b *testing.B) {
	testSizes := []int{100, 1000, 5000}

	for _, size := range testSizes {
		b.Run(fmt.Sprintf("files_%d", size), func(b *testing.B) {
			bh := setupBenchmarkEnvironment(b, size)
			defer bh.cleanup(b)

			// Setup the cache
			err := bh.cache.ScanDirectory()
			if err != nil {
				b.Fatalf("ScanDirectory failed: %v", err)
			}

			err = bh.cache.LoadIndex()
			if err != nil {
				b.Fatalf("LoadIndex failed: %v", err)
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				status, err := bh.cache.Status()
				if err != nil {
					b.Fatalf("Status failed: %v", err)
				}
				if status.HasChanges() {
					b.Fatal("Unexpected changes detected")
				}
			}
		})
	}
}

func BenchmarkStatusWithChanges(b *testing.B) {
	bh := setupBenchmarkEnvironment(b, 1000)
	defer bh.cleanup(b)

	// Setup the cache
	err := bh.cache.ScanDirectory()
	if err != nil {
		b.Fatalf("ScanDirectory failed: %v", err)
	}

	err = bh.cache.LoadIndex()
	if err != nil {
		b.Fatalf("LoadIndex failed: %v", err)
	}

	// Modify some files to create changes
	for i := 0; i < 10; i++ {
		filename := filepath.Join(bh.tempDir, fmt.Sprintf("file_%06d.txt", i))
		content := fmt.Sprintf("Modified content for file %d", i)
		if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
			b.Fatalf("Failed to modify file: %v", err)
		}
	}

	// Add some new files
	for i := 1000; i < 1010; i++ {
		filename := filepath.Join(bh.tempDir, fmt.Sprintf("file_%06d.txt", i))
		content := fmt.Sprintf("New file content %d", i)
		if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
			b.Fatalf("Failed to create new file: %v", err)
		}
	}

	// Delete some files
	for i := 990; i < 995; i++ {
		filename := filepath.Join(bh.tempDir, fmt.Sprintf("file_%06d.txt", i))
		if err := os.Remove(filename); err != nil {
			b.Fatalf("Failed to delete file: %v", err)
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		status, err := bh.cache.Status()
		if err != nil {
			b.Fatalf("Status failed: %v", err)
		}
		if !status.HasChanges() {
			b.Fatal("Expected changes but none detected")
		}
	}
}

func BenchmarkHasChangesQuick(b *testing.B) {
	bh := setupBenchmarkEnvironment(b, 1000)
	defer bh.cleanup(b)

	// Setup the cache
	err := bh.cache.ScanDirectory()
	if err != nil {
		b.Fatalf("ScanDirectory failed: %v", err)
	}

	err = bh.cache.LoadIndex()
	if err != nil {
		b.Fatalf("LoadIndex failed: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		hasChanges, err := bh.cache.HasChangesQuick()
		if err != nil {
			b.Fatalf("HasChangesQuick failed: %v", err)
		}
		if hasChanges {
			b.Fatal("Unexpected changes detected")
		}
	}
}

func BenchmarkFindDuplicates(b *testing.B) {
	bh := setupBenchmarkEnvironment(b, 1000)
	defer bh.cleanup(b)

	// Create some duplicate files
	duplicateContent := "This is duplicate content for benchmarking"
	for i := 0; i < 50; i++ {
		filename := filepath.Join(bh.tempDir, fmt.Sprintf("duplicate_%03d.txt", i))
		if err := os.WriteFile(filename, []byte(duplicateContent), 0644); err != nil {
			b.Fatalf("Failed to create duplicate file: %v", err)
		}
	}

	// Setup the cache
	err := bh.cache.ScanDirectory()
	if err != nil {
		b.Fatalf("ScanDirectory failed: %v", err)
	}

	err = bh.cache.LoadIndex()
	if err != nil {
		b.Fatalf("LoadIndex failed: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		duplicates := bh.cache.FindDuplicates()
		if len(duplicates) == 0 {
			b.Fatal("Expected to find duplicates but none found")
		}
	}
}

func BenchmarkBinaryEntryMethods(b *testing.B) {
	bh := setupBenchmarkEnvironment(b, 1000)
	defer bh.cleanup(b)

	// Setup the cache
	err := bh.cache.ScanDirectory()
	if err != nil {
		b.Fatalf("ScanDirectory failed: %v", err)
	}

	err = bh.cache.LoadIndex()
	if err != nil {
		b.Fatalf("LoadIndex failed: %v", err)
	}

	entries := bh.cache.GetEntries()
	if len(entries) == 0 {
		b.Fatal("No entries to benchmark")
	}

	entry := entries[0]

	b.Run("RelativePath", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = entry.RelativePath()
		}
	})

	b.Run("RelativePathBytes", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = entry.RelativePathBytes()
		}
	})

	b.Run("HashString", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = entry.HashString()
		}
	})

	b.Run("EntrySize", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = entry.EntrySize()
		}
	})
}

func BenchmarkIterateEntries(b *testing.B) {
	testSizes := []int{100, 1000, 5000}

	for _, size := range testSizes {
		b.Run(fmt.Sprintf("files_%d", size), func(b *testing.B) {
			bh := setupBenchmarkEnvironment(b, size)
			defer bh.cleanup(b)

			// Setup the cache
			err := bh.cache.ScanDirectory()
			if err != nil {
				b.Fatalf("ScanDirectory failed: %v", err)
			}

			err = bh.cache.LoadIndex()
			if err != nil {
				b.Fatalf("LoadIndex failed: %v", err)
			}

			entries := bh.cache.GetEntries()

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				var totalSize int64
				for _, entry := range entries {
					totalSize += int64(entry.FileSize)
					_ = entry.RelativePath() // Access the path
					_ = entry.HashString()   // Access the hash
				}
				if totalSize == 0 {
					b.Fatal("Total size should not be zero")
				}
			}
		})
	}
}

func BenchmarkStats(b *testing.B) {
	testSizes := []int{100, 1000, 5000}

	for _, size := range testSizes {
		b.Run(fmt.Sprintf("files_%d", size), func(b *testing.B) {
			bh := setupBenchmarkEnvironment(b, size)
			defer bh.cleanup(b)

			// Setup the cache
			err := bh.cache.ScanDirectory()
			if err != nil {
				b.Fatalf("ScanDirectory failed: %v", err)
			}

			err = bh.cache.LoadIndex()
			if err != nil {
				b.Fatalf("LoadIndex failed: %v", err)
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				count, totalSize, err := bh.cache.Stats()
				if err != nil {
					b.Fatalf("Stats failed: %v", err)
				}
				if count != size {
					b.Fatalf("Expected count %d, got %d", size, count)
				}
				if totalSize == 0 {
					b.Fatal("Total size should not be zero")
				}
			}
		})
	}
}

func BenchmarkUpdate(b *testing.B) {
	bh := setupBenchmarkEnvironment(b, 100)
	defer bh.cleanup(b)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Create a fresh cache for each iteration
		cache := NewDirectoryCache(bh.tempDir, "")
		err := cache.Update()
		if err != nil {
			b.Fatalf("Update failed: %v", err)
		}
		cache.Close()
	}
}

func BenchmarkUpdatePaths(b *testing.B) {
	bh := setupBenchmarkEnvironment(b, 1000)
	defer bh.cleanup(b)

	// Setup initial index
	err := bh.cache.ScanDirectory()
	if err != nil {
		b.Fatalf("ScanDirectory failed: %v", err)
	}

	// Select some paths to update
	paths := []string{
		"file_000001.txt",
		"file_000010.txt",
		"file_000100.txt",
		"subdir_001/file_000200.txt",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		err := bh.cache.UpdatePaths(paths)
		if err != nil {
			b.Fatalf("UpdatePaths failed: %v", err)
		}
	}
}
