package dircachefilehash

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// BenchmarkConfig defines the parameters for performance benchmarks
type BenchmarkConfig struct {
	TotalFiles    int   // Total number of files to generate
	LargeFiles    int   // Number of large files (>1MB)
	LargeFileSize int64 // Size of large files in bytes
	SmallFileSize int64 // Size of small files in bytes
	DirDepth      int   // Maximum directory depth
	FilesPerDir   int   // Average files per directory
}

// Standard benchmark configurations
var (
	// Small benchmark for development/CI
	SmallBenchConfig = BenchmarkConfig{
		TotalFiles:    1000,
		LargeFiles:    50,
		LargeFileSize: 2 * 1024 * 1024, // 2MB
		SmallFileSize: 4 * 1024,        // 4KB
		DirDepth:      3,
		FilesPerDir:   20,
	}

	// Medium benchmark for regular testing
	MediumBenchConfig = BenchmarkConfig{
		TotalFiles:    100000,
		LargeFiles:    1000,
		LargeFileSize: 5 * 1024 * 1024, // 5MB
		SmallFileSize: 8 * 1024,        // 8KB
		DirDepth:      5,
		FilesPerDir:   50,
	}

	// Large benchmark for performance validation
	LargeBenchConfig = BenchmarkConfig{
		TotalFiles:    1000000,
		LargeFiles:    10000,
		LargeFileSize: 10 * 1024 * 1024, // 10MB
		SmallFileSize: 16 * 1024,        // 16KB
		DirDepth:      8,
		FilesPerDir:   100,
	}
)

// generateDeterministicData creates deterministic file content based on seed
func generateDeterministicData(size int64, seed int64) []byte {
	data := make([]byte, size)

	// Use seed to create deterministic but varied content
	for i := int64(0); i < size; i++ {
		// Simple PRNG based on linear congruential generator
		seed = (seed*1103515245 + 12345) & 0x7fffffff
		data[i] = byte(seed >> 16)
	}

	return data
}

// createBenchmarkDataset generates a deterministic test dataset
func createBenchmarkDataset(rootDir string, config BenchmarkConfig) error {
	// Remove existing directory if it exists
	if err := os.RemoveAll(rootDir); err != nil {
		return fmt.Errorf("failed to clean root dir: %w", err)
	}

	// Create root directory
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		return fmt.Errorf("failed to create root dir: %w", err)
	}

	// Calculate directory structure
	totalDirs := int(math.Ceil(float64(config.TotalFiles) / float64(config.FilesPerDir)))
	dirsPerLevel := make([]int, config.DirDepth+1)

	// Distribute directories across levels
	remaining := totalDirs
	for level := 0; level <= config.DirDepth && remaining > 0; level++ {
		if level == config.DirDepth {
			dirsPerLevel[level] = remaining
		} else {
			dirsAtLevel := min(remaining/2, int(math.Pow(4, float64(level))))
			dirsPerLevel[level] = dirsAtLevel
			remaining -= dirsAtLevel
		}
	}

	fileIndex := 0
	largeFilesCreated := 0

	// Create directories and files
	for level := 0; level <= config.DirDepth; level++ {
		for dirNum := 0; dirNum < dirsPerLevel[level] && fileIndex < config.TotalFiles; dirNum++ {
			// Create directory path
			var dirPath string
			if level == 0 {
				dirPath = rootDir
			} else {
				// Create nested directory structure
				pathParts := []string{rootDir}
				for l := 1; l <= level; l++ {
					pathParts = append(pathParts, fmt.Sprintf("level%d_dir%d", l, dirNum%(int(math.Pow(2, float64(l))))))
				}
				dirPath = filepath.Join(pathParts...)
				if err := os.MkdirAll(dirPath, 0755); err != nil {
					return fmt.Errorf("failed to create dir %s: %w", dirPath, err)
				}
			}

			// Create files in this directory
			filesInThisDir := min(config.FilesPerDir, config.TotalFiles-fileIndex)
			for fileNum := 0; fileNum < filesInThisDir; fileNum++ {
				fileName := fmt.Sprintf("file_%06d.dat", fileIndex)
				filePath := filepath.Join(dirPath, fileName)

				// Determine file size (deterministic based on file index)
				var fileSize int64
				var isLarge bool

				// Use deterministic pattern to decide large vs small files
				if largeFilesCreated < config.LargeFiles {
					// Distribute large files evenly throughout the dataset
					largeFileInterval := config.TotalFiles / config.LargeFiles
					if fileIndex%largeFileInterval == 0 {
						fileSize = config.LargeFileSize
						isLarge = true
						largeFilesCreated++
					} else {
						fileSize = config.SmallFileSize
					}
				} else {
					fileSize = config.SmallFileSize
				}

				// Generate deterministic content
				seed := int64(fileIndex*12345 + 67890) // Deterministic seed
				if isLarge {
					seed += 999999 // Different seed space for large files
				}

				data := generateDeterministicData(fileSize, seed)

				// Write file
				if err := os.WriteFile(filePath, data, 0644); err != nil {
					return fmt.Errorf("failed to write file %s: %w", filePath, err)
				}

				fileIndex++
				if fileIndex >= config.TotalFiles {
					break
				}
			}
		}
	}

	return nil
}

// BenchmarkDirectoryScanSmall benchmarks directory scanning with small dataset
func BenchmarkDirectoryScanSmall(b *testing.B) {
	benchmarkDirectoryScan(b, SmallBenchConfig)
}

// BenchmarkDirectoryScanMedium benchmarks directory scanning with medium dataset
func BenchmarkDirectoryScanMedium(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping medium benchmark in short mode")
	}
	benchmarkDirectoryScan(b, MediumBenchConfig)
}

// BenchmarkDirectoryScanLarge benchmarks directory scanning with large dataset
func BenchmarkDirectoryScanLarge(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping large benchmark in short mode")
	}
	benchmarkDirectoryScan(b, LargeBenchConfig)
}

// benchmarkDirectoryScan performs the actual benchmark
func benchmarkDirectoryScan(b *testing.B, config BenchmarkConfig) {
	// Create temporary directory for benchmark
	tempDir := b.TempDir()
	datasetDir := filepath.Join(tempDir, "dataset")

	b.Logf("Creating benchmark dataset: %d files (%d large, %d small)",
		config.TotalFiles, config.LargeFiles, config.TotalFiles-config.LargeFiles)

	// Create dataset (this is not timed)
	b.StopTimer()
	start := time.Now()
	if err := createBenchmarkDataset(datasetDir, config); err != nil {
		b.Fatalf("Failed to create benchmark dataset: %v", err)
	}
	setupTime := time.Since(start)
	b.Logf("Dataset creation took: %v", setupTime)

	// Calculate expected data size
	expectedSize := int64(config.LargeFiles)*config.LargeFileSize +
		int64(config.TotalFiles-config.LargeFiles)*config.SmallFileSize
	b.Logf("Expected total data size: %.2f MB", float64(expectedSize)/(1024*1024))

	b.StartTimer()

	// Run the benchmark
	for i := 0; i < b.N; i++ {
		// Create directory cache
		dcfhDir := filepath.Join(tempDir, fmt.Sprintf("dcfh_%d", i))
		cache := NewDirectoryCache(datasetDir, dcfhDir)

		// Time the full update operation
		updateStart := time.Now()
		if err := cache.Update(nil, map[string]string{}); err != nil {
			b.Fatalf("Update failed: %v", err)
		}
		updateDuration := time.Since(updateStart)

		// Verify results
		fileCount, totalSize, err := cache.Stats()
		if err != nil {
			b.Fatalf("Stats failed: %v", err)
		}

		if fileCount != config.TotalFiles {
			b.Errorf("Expected %d files, got %d", config.TotalFiles, fileCount)
		}

		if totalSize != expectedSize {
			b.Errorf("Expected %d bytes, got %d", expectedSize, totalSize)
		}

		// Calculate performance metrics
		hashingRate := float64(totalSize) / updateDuration.Seconds() / (1024 * 1024) // MB/s
		fileRate := float64(fileCount) / updateDuration.Seconds()                    // files/s

		b.Logf("Performance: %.2f MB/s hashing rate, %.0f files/s, %v total time",
			hashingRate, fileRate, updateDuration)

		// Clean up for next iteration
		cache.Close()
		os.RemoveAll(dcfhDir)
	}

	// Report string copy statistics
	copies, accesses, rate := GetStringCopyStats()
	b.Logf("String copy stats: %d copies out of %d accesses (%.2f%% copy rate)", copies, accesses, rate)
}

// BenchmarkIndexOperationsSmall benchmarks index file operations
func BenchmarkIndexOperationsSmall(b *testing.B) {
	benchmarkIndexOperations(b, SmallBenchConfig)
}

// BenchmarkIndexOperationsMedium benchmarks index file operations
func BenchmarkIndexOperationsMedium(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping medium benchmark in short mode")
	}
	benchmarkIndexOperations(b, MediumBenchConfig)
}

// benchmarkIndexOperations benchmarks index file read/write performance
func benchmarkIndexOperations(b *testing.B, config BenchmarkConfig) {
	// Create dataset once
	tempDir := b.TempDir()
	datasetDir := filepath.Join(tempDir, "dataset")

	b.StopTimer()
	if err := createBenchmarkDataset(datasetDir, config); err != nil {
		b.Fatalf("Failed to create benchmark dataset: %v", err)
	}

	// Create initial index
	dcfhDir := filepath.Join(tempDir, "dcfh")
	cache := NewDirectoryCache(datasetDir, dcfhDir)
	if err := cache.Update(nil, map[string]string{}); err != nil {
		b.Fatalf("Initial update failed: %v", err)
	}
	cache.Close()
	b.StartTimer()

	// Benchmark index loading operations
	b.Run("LoadIndex", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			cache := NewDirectoryCache(datasetDir, dcfhDir)
			if _, err := cache.LoadMainIndex(); err != nil {
				b.Fatalf("LoadMainIndex failed: %v", err)
			}
			cache.Close()
		}
	})

	// Benchmark status operations
	b.Run("Status", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			cache := NewDirectoryCache(datasetDir, dcfhDir)
			if _, err := cache.Status(nil, map[string]string{}); err != nil {
				b.Fatalf("Status failed: %v", err)
			}
			cache.Close()
		}
	})

	// Benchmark duplicate detection
	b.Run("FindDuplicates", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			cache := NewDirectoryCache(datasetDir, dcfhDir)
			if _, err := cache.LoadMainIndex(); err != nil {
				b.Fatalf("LoadMainIndex failed: %v", err)
			}
			if _, err := cache.FindDuplicates(nil, map[string]string{}); err != nil {
				b.Fatalf("FindDuplicates failed: %v", err)
			}
			cache.Close()
		}
	})
}

// BenchmarkFullWorkflowMedium tests complete workflow: init, update, modify files, status, update
func BenchmarkFullWorkflowMedium(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping full workflow benchmark in short mode")
	}

	config := MediumBenchConfig
	tempDir := b.TempDir()
	datasetDir := filepath.Join(tempDir, "dataset")

	b.Logf("Creating medium benchmark dataset: %d files (%d large, %d small)",
		config.TotalFiles, config.LargeFiles, config.TotalFiles-config.LargeFiles)

	// Create dataset (this is not timed)
	b.StopTimer()
	start := time.Now()
	if err := createBenchmarkDataset(datasetDir, config); err != nil {
		b.Fatalf("Failed to create benchmark dataset: %v", err)
	}
	setupTime := time.Since(start)
	b.Logf("Dataset creation took: %v", setupTime)

	// Calculate expected data size
	expectedSize := int64(config.LargeFiles)*config.LargeFileSize +
		int64(config.TotalFiles-config.LargeFiles)*config.SmallFileSize
	b.Logf("Expected total data size: %.2f MB", float64(expectedSize)/(1024*1024))

	b.StartTimer()

	// Run the full workflow benchmark
	for i := 0; i < b.N; i++ {
		dcfhDir := filepath.Join(tempDir, fmt.Sprintf("dcfh_%d", i))

		// Step 1: Initialise repository from scratch
		initStart := time.Now()
		cache := NewDirectoryCache(datasetDir, dcfhDir)
		if err := cache.createEmptyIndex(); err != nil {
			b.Fatalf("Failed to initialise repository: %v", err)
		}
		initDuration := time.Since(initStart)

		// Step 2: Initial update (index all files)
		updateStart := time.Now()
		if err := cache.Update(nil, map[string]string{}); err != nil {
			b.Fatalf("Initial update failed: %v", err)
		}
		initialUpdateDuration := time.Since(updateStart)

		// Verify initial state
		fileCount, totalSize, err := cache.Stats()
		if err != nil {
			b.Fatalf("Stats failed: %v", err)
		}
		if fileCount != config.TotalFiles {
			b.Errorf("Expected %d files, got %d", config.TotalFiles, fileCount)
		}

		// Step 3: Modify some files, add new files, delete some files
		modifyStart := time.Now()

		// Modify 10% of existing files
		modifyCount := config.TotalFiles / 10
		for j := 0; j < modifyCount; j++ {
			filePath := filepath.Join(datasetDir, fmt.Sprintf("file_%06d.dat", j))
			if err := os.WriteFile(filePath, []byte("modified content"), 0644); err != nil {
				b.Fatalf("Failed to modify file: %v", err)
			}
		}

		// Add 5% new files
		newCount := config.TotalFiles / 20
		for j := 0; j < newCount; j++ {
			fileName := fmt.Sprintf("new_file_%d_%06d.dat", i, j)
			filePath := filepath.Join(datasetDir, fileName)
			content := generateDeterministicData(config.SmallFileSize, int64(j+config.TotalFiles))
			if err := os.WriteFile(filePath, content, 0644); err != nil {
				b.Fatalf("Failed to create new file: %v", err)
			}
		}

		// Delete 2% of existing files
		deleteCount := config.TotalFiles / 50
		for j := config.TotalFiles - deleteCount; j < config.TotalFiles; j++ {
			filePath := filepath.Join(datasetDir, fmt.Sprintf("file_%06d.dat", j))
			os.Remove(filePath) // Ignore errors if file doesn't exist
		}

		modifyDuration := time.Since(modifyStart)

		// Step 4: Run status to detect changes
		statusStart := time.Now()
		statusResult, err := cache.Status(nil, map[string]string{"v": "1"})
		if err != nil {
			b.Fatalf("Status failed: %v", err)
		}
		statusDuration := time.Since(statusStart)

		// Step 5: Update again to incorporate changes
		finalUpdateStart := time.Now()
		if err := cache.Update(nil, map[string]string{}); err != nil {
			b.Fatalf("Final update failed: %v", err)
		}
		finalUpdateDuration := time.Since(finalUpdateStart)

		// Calculate performance metrics
		totalDuration := initDuration + initialUpdateDuration + modifyDuration + statusDuration + finalUpdateDuration
		hashingRate := float64(totalSize) / initialUpdateDuration.Seconds() / (1024 * 1024) // MB/s for initial scan
		fileRate := float64(fileCount) / initialUpdateDuration.Seconds()                    // files/s for initial scan

		changesDetected := len(statusResult.Modified) + len(statusResult.Added) + len(statusResult.Deleted)

		b.Logf("Workflow Performance:")
		b.Logf("  Init: %v", initDuration)
		b.Logf("  Initial Update: %v (%.2f MB/s, %.0f files/s)", initialUpdateDuration, hashingRate, fileRate)
		b.Logf("  File Modifications: %v (%d modified, %d added, %d deleted)", modifyDuration, modifyCount, newCount, deleteCount)
		b.Logf("  Status Detection: %v (%d changes detected)", statusDuration, changesDetected)
		b.Logf("  Final Update: %v", finalUpdateDuration)
		b.Logf("  Total Workflow Time: %v", totalDuration)

		// Clean up for next iteration
		cache.Close()

		// Clean up modified files for next iteration
		for j := 0; j < newCount; j++ {
			fileName := fmt.Sprintf("new_file_%d_%06d.dat", i, j)
			filePath := filepath.Join(datasetDir, fileName)
			os.Remove(filePath)
		}

		os.RemoveAll(dcfhDir)
	}

	// Report string copy statistics
	copies, accesses, rate := GetStringCopyStats()
	b.Logf("String copy stats: %d copies out of %d accesses (%.2f%% copy rate)", copies, accesses, rate)
}

// BenchmarkMemoryUsage benchmarks memory usage during operations
func BenchmarkMemoryUsage(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping memory benchmark in short mode")
	}

	config := MediumBenchConfig
	tempDir := b.TempDir()
	datasetDir := filepath.Join(tempDir, "dataset")

	b.StopTimer()
	if err := createBenchmarkDataset(datasetDir, config); err != nil {
		b.Fatalf("Failed to create benchmark dataset: %v", err)
	}
	b.StartTimer()

	b.Run("MemoryEfficiency", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			dcfhDir := filepath.Join(tempDir, fmt.Sprintf("dcfh_%d", i))
			cache := NewDirectoryCache(datasetDir, dcfhDir)

			// Force GC before measurement
			b.StopTimer()
			runtime.GC()
			var m1 runtime.MemStats
			runtime.ReadMemStats(&m1)
			b.StartTimer()

			// Perform operation
			if err := cache.Update(nil, map[string]string{}); err != nil {
				b.Fatalf("Update failed: %v", err)
			}

			// Measure memory after operation
			b.StopTimer()
			runtime.GC()
			var m2 runtime.MemStats
			runtime.ReadMemStats(&m2)

			memoryUsed := m2.Alloc - m1.Alloc
			b.ReportMetric(float64(memoryUsed)/(1024*1024), "MB_used")
			b.ReportMetric(float64(memoryUsed)/float64(config.TotalFiles), "bytes_per_file")

			cache.Close()
			os.RemoveAll(dcfhDir)
			b.StartTimer()
		}
	})
}

// Helper function for min
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
