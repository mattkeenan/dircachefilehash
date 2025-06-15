package dircachefilehash

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestSkiplistWrapper(t *testing.T) {
	// Create test environment
	th := setupTestEnvironment(t)
	defer th.cleanup(t)

	// Create and load index to get entries
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
		t.Fatal("No entries to test with")
	}

	t.Run("NewSkiplistWrapper", func(t *testing.T) {
		sw := NewSkiplistWrapper(16)
		if sw == nil {
			t.Fatal("NewSkiplistWrapper returned nil")
		}

		if !sw.IsEmpty() {
			t.Error("New skiplist should be empty")
		}

		if sw.Length() != 0 {
			t.Errorf("New skiplist length should be 0, got %d", sw.Length())
		}
	})

	t.Run("Insert", func(t *testing.T) {
		sw := NewSkiplistWrapper(16)

		// Insert first entry
		sw.Insert(entries[0])

		if sw.IsEmpty() {
			t.Error("Skiplist should not be empty after insert")
		}

		if sw.Length() != 1 {
			t.Errorf("Expected length 1, got %d", sw.Length())
		}

		// Insert more entries
		for i := 1; i < len(entries); i++ {
			sw.Insert(entries[i])
		}

		if sw.Length() != len(entries) {
			t.Errorf("Expected length %d, got %d", len(entries), sw.Length())
		}
	})

	t.Run("InsertBatch", func(t *testing.T) {
		sw := NewSkiplistWrapper(16)

		sw.InsertBatch(entries)

		if sw.Length() != len(entries) {
			t.Errorf("Expected length %d, got %d", len(entries), sw.Length())
		}
	})

	t.Run("GetSortedEntries", func(t *testing.T) {
		sw := NewSkiplistWrapper(16)
		sw.InsertBatch(entries)

		sortedEntries := sw.GetSortedEntries()

		if len(sortedEntries) != len(entries) {
			t.Errorf("Expected %d sorted entries, got %d", len(entries), len(sortedEntries))
		}

		// Verify entries are sorted by relative path
		for i := 1; i < len(sortedEntries); i++ {
			if sortedEntries[i-1].RelativePath() >= sortedEntries[i].RelativePath() {
				t.Errorf("Entries are not sorted: %s >= %s",
					sortedEntries[i-1].RelativePath(),
					sortedEntries[i].RelativePath())
			}
		}

		// Verify all original entries are present
		originalPaths := make(map[string]bool)
		for _, entry := range entries {
			originalPaths[entry.RelativePath()] = true
		}

		for _, entry := range sortedEntries {
			if !originalPaths[entry.RelativePath()] {
				t.Errorf("Unexpected entry in sorted results: %s", entry.RelativePath())
			}
		}
	})

	t.Run("ForEach", func(t *testing.T) {
		sw := NewSkiplistWrapper(16)
		sw.InsertBatch(entries)

		var visitedPaths []string
		sw.ForEach(func(entry *binaryEntry) bool {
			visitedPaths = append(visitedPaths, entry.RelativePath())
			return true // Continue iteration
		})

		if len(visitedPaths) != len(entries) {
			t.Errorf("Expected to visit %d entries, visited %d", len(entries), len(visitedPaths))
		}

		// Verify paths are in sorted order
		for i := 1; i < len(visitedPaths); i++ {
			if visitedPaths[i-1] >= visitedPaths[i] {
				t.Errorf("ForEach did not visit in sorted order: %s >= %s",
					visitedPaths[i-1], visitedPaths[i])
			}
		}
	})

	t.Run("ForEachEarlyStop", func(t *testing.T) {
		sw := NewSkiplistWrapper(16)
		sw.InsertBatch(entries)

		var visitedCount int
		stopAfter := 3

		sw.ForEach(func(entry *binaryEntry) bool {
			visitedCount++
			return visitedCount < stopAfter
		})

		if visitedCount != stopAfter {
			t.Errorf("Expected to visit %d entries before stopping, visited %d", stopAfter, visitedCount)
		}
	})

	t.Run("Find", func(t *testing.T) {
		sw := NewSkiplistWrapper(16)
		sw.InsertBatch(entries)

		// Test finding existing entry
		targetPath := entries[0].RelativePath()
		found := sw.Find(targetPath)
		if found == nil {
			t.Error("Failed to find existing entry")
		} else if found.RelativePath() != targetPath {
			t.Errorf("Found wrong entry: expected %s, got %s", targetPath, found.RelativePath())
		}

		// Test finding non-existent entry
		notFound := sw.Find("non/existent/path.txt")
		if notFound != nil {
			t.Error("Found non-existent entry")
		}
	})

	t.Run("Copy", func(t *testing.T) {
		sw := NewSkiplistWrapper(16)
		sw.InsertBatch(entries)

		copied := sw.Copy()
		if copied == nil {
			t.Fatal("Copy returned nil")
		}

		if copied.Length() != sw.Length() {
			t.Errorf("Copy has different length: original %d, copy %d", sw.Length(), copied.Length())
		}

		originalEntries := sw.GetSortedEntries()
		copiedEntries := copied.GetSortedEntries()

		if len(originalEntries) != len(copiedEntries) {
			t.Errorf("Copy has different number of entries")
		}

		for i := 0; i < len(originalEntries) && i < len(copiedEntries); i++ {
			if originalEntries[i].RelativePath() != copiedEntries[i].RelativePath() {
				t.Errorf("Copy entry mismatch at index %d: %s vs %s",
					i, originalEntries[i].RelativePath(), copiedEntries[i].RelativePath())
			}
		}
	})
}

func TestSkiplistMergeAndDelete(t *testing.T) {
	// Create two test environments
	th1 := setupTestEnvironment(t)
	defer th1.cleanup(t)

	// Create second test environment with different files
	tempDir2, err := os.MkdirTemp("", "dircachefilehash_test2_*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir2)

	testFiles2 := map[string]string{
		"file_a.txt":      "Content A",
		"file_b.txt":      "Content B",
		"dir2/file_c.txt": "Content C",
	}

	// Create test files for second environment
	for filename, content := range testFiles2 {
		fullPath := filepath.Join(tempDir2, filename)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("Failed to create directory for %s: %v", filename, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file %s: %v", filename, err)
		}
	}

	cache2 := NewDirectoryCache(tempDir2, "")
	defer cache2.Close()

	// Setup both caches
	err = th1.cache.ScanDirectory()
	if err != nil {
		t.Fatalf("ScanDirectory failed for cache1: %v", err)
	}
	err = th1.cache.LoadIndex()
	if err != nil {
		t.Fatalf("LoadIndex failed for cache1: %v", err)
	}

	err = cache2.ScanDirectory()
	if err != nil {
		t.Fatalf("ScanDirectory failed for cache2: %v", err)
	}
	err = cache2.LoadIndex()
	if err != nil {
		t.Fatalf("LoadIndex failed for cache2: %v", err)
	}

	entries1 := th1.cache.GetEntries()
	entries2 := cache2.GetEntries()

	t.Run("Merge", func(t *testing.T) {
		sw1 := NewSkiplistWrapper(16)
		sw2 := NewSkiplistWrapper(16)

		sw1.InsertBatch(entries1)
		sw2.InsertBatch(entries2)

		originalLen1 := sw1.Length()
		originalLen2 := sw2.Length()

		// Merge sw2 into sw1
		sw1.Merge(sw2)

		// sw1 should now contain entries from both
		expectedLen := originalLen1 + originalLen2
		if sw1.Length() != expectedLen {
			t.Errorf("After merge, expected length %d, got %d", expectedLen, sw1.Length())
		}

		// Verify all entries are present and sorted
		mergedEntries := sw1.GetSortedEntries()
		for i := 1; i < len(mergedEntries); i++ {
			if mergedEntries[i-1].RelativePath() >= mergedEntries[i].RelativePath() {
				t.Error("Merged entries are not sorted")
				break
			}
		}

		// Verify entries from both original skiplists are present
		allPaths := make(map[string]bool)
		for _, entry := range entries1 {
			allPaths[entry.RelativePath()] = true
		}
		for _, entry := range entries2 {
			allPaths[entry.RelativePath()] = true
		}

		for _, entry := range mergedEntries {
			if !allPaths[entry.RelativePath()] {
				t.Errorf("Unexpected entry after merge: %s", entry.RelativePath())
			}
		}
	})

	t.Run("Delete", func(t *testing.T) {
		sw1 := NewSkiplistWrapper(16)
		sw2 := NewSkiplistWrapper(16)

		// Insert some overlapping entries
		sw1.InsertBatch(entries1)

		// Insert only first few entries from entries1 into sw2
		if len(entries1) > 2 {
			sw2.InsertBatch(entries1[:2])
		}

		originalLen1 := sw1.Length()
		deleteLen := sw2.Length()

		// Delete entries in sw2 from sw1
		sw1.Delete(sw2)

		// sw1 should have fewer entries
		expectedLen := originalLen1 - deleteLen
		if sw1.Length() != expectedLen {
			t.Errorf("After delete, expected length %d, got %d", expectedLen, sw1.Length())
		}

		// Verify deleted entries are not present
		remainingEntries := sw1.GetSortedEntries()
		deletedPaths := make(map[string]bool)
		if len(entries1) > 2 {
			for _, entry := range entries1[:2] {
				deletedPaths[entry.RelativePath()] = true
			}
		}

		for _, entry := range remainingEntries {
			if deletedPaths[entry.RelativePath()] {
				t.Errorf("Deleted entry still present: %s", entry.RelativePath())
			}
		}
	})

	t.Run("MergeNil", func(t *testing.T) {
		sw := NewSkiplistWrapper(16)
		sw.InsertBatch(entries1)

		originalLen := sw.Length()

		// Merge with nil should not change anything
		sw.Merge(nil)

		if sw.Length() != originalLen {
			t.Errorf("Merge with nil changed length: expected %d, got %d", originalLen, sw.Length())
		}
	})

	t.Run("DeleteNil", func(t *testing.T) {
		sw := NewSkiplistWrapper(16)
		sw.InsertBatch(entries1)

		originalLen := sw.Length()

		// Delete with nil should not change anything
		sw.Delete(nil)

		if sw.Length() != originalLen {
			t.Errorf("Delete with nil changed length: expected %d, got %d", originalLen, sw.Length())
		}
	})
}

func TestSkiplistWithSmallMaxLevels(t *testing.T) {
	// Test with small max levels to ensure default is applied
	sw := NewSkiplistWrapper(4) // Less than minimum of 8

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
	sw.InsertBatch(entries)

	// Should still work correctly
	if sw.Length() != len(entries) {
		t.Errorf("Expected length %d, got %d", len(entries), sw.Length())
	}

	sortedEntries := sw.GetSortedEntries()
	if len(sortedEntries) != len(entries) {
		t.Errorf("Expected %d sorted entries, got %d", len(entries), len(sortedEntries))
	}
}

func BenchmarkSkiplistWrapper(b *testing.B) {
	// Setup test environment
	th := setupTestEnvironment(&testing.T{})
	defer th.cleanup(&testing.T{})

	// Create entries
	err := th.cache.ScanDirectory()
	if err != nil {
		b.Fatalf("ScanDirectory failed: %v", err)
	}

	err = th.cache.LoadIndex()
	if err != nil {
		b.Fatalf("LoadIndex failed: %v", err)
	}

	entries := th.cache.GetEntries()
	if len(entries) == 0 {
		b.Fatal("No entries for benchmarking")
	}

	b.Run("Insert", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			sw := NewSkiplistWrapper(16)
			for _, entry := range entries {
				sw.Insert(entry)
			}
		}
	})

	b.Run("InsertBatch", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			sw := NewSkiplistWrapper(16)
			sw.InsertBatch(entries)
		}
	})

	b.Run("GetSortedEntries", func(b *testing.B) {
		sw := NewSkiplistWrapper(16)
		sw.InsertBatch(entries)

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			sorted := sw.GetSortedEntries()
			if len(sorted) != len(entries) {
				b.Fatal("Wrong number of sorted entries")
			}
		}
	})

	b.Run("Find", func(b *testing.B) {
		sw := NewSkiplistWrapper(16)
		sw.InsertBatch(entries)

		targetPath := entries[len(entries)/2].RelativePath()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			found := sw.Find(targetPath)
			if found == nil {
				b.Fatal("Entry not found")
			}
		}
	})

	b.Run("ForEach", func(b *testing.B) {
		sw := NewSkiplistWrapper(16)
		sw.InsertBatch(entries)

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			count := 0
			sw.ForEach(func(entry *binaryEntry) bool {
				count++
				return true
			})
			if count != len(entries) {
				b.Fatal("Wrong count in ForEach")
			}
		}
	})

	b.Run("Copy", func(b *testing.B) {
		sw := NewSkiplistWrapper(16)
		sw.InsertBatch(entries)

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			copied := sw.Copy()
			if copied.Length() != sw.Length() {
				b.Fatal("Copy has wrong length")
			}
		}
	})
}

func BenchmarkSkiplistLargeDataset(b *testing.B) {
	// Create a larger dataset for benchmarking
	tempDir, err := os.MkdirTemp("", "skiplist_bench_*")
	if err != nil {
		b.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create many files
	numFiles := 10000
	for i := 0; i < numFiles; i++ {
		filename := filepath.Join(tempDir, fmt.Sprintf("file_%08d.txt", i))
		content := fmt.Sprintf("Content for file %d", i)

		if i%1000 == 0 {
			subdir := filepath.Join(tempDir, fmt.Sprintf("subdir_%04d", i/1000))
			if err := os.MkdirAll(subdir, 0755); err != nil {
				b.Fatalf("Failed to create subdir: %v", err)
			}
			filename = filepath.Join(subdir, fmt.Sprintf("file_%08d.txt", i))
		}

		if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
			b.Fatalf("Failed to create dir: %v", err)
		}
		if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
			b.Fatalf("Failed to create file: %v", err)
		}
	}

	cache := NewDirectoryCache(tempDir, "")
	defer cache.Close()

	err = cache.ScanDirectory()
	if err != nil {
		b.Fatalf("ScanDirectory failed: %v", err)
	}

	err = cache.LoadIndex()
	if err != nil {
		b.Fatalf("LoadIndex failed: %v", err)
	}

	entries := cache.GetEntries()

	b.Run(fmt.Sprintf("InsertBatch_%d_files", numFiles), func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			sw := NewSkiplistWrapper(20) // Higher max levels for large dataset
			sw.InsertBatch(entries)
		}
	})

	sw := NewSkiplistWrapper(20)
	sw.InsertBatch(entries)

	b.Run(fmt.Sprintf("GetSortedEntries_%d_files", numFiles), func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			sorted := sw.GetSortedEntries()
			if len(sorted) != len(entries) {
				b.Fatal("Wrong number of entries")
			}
		}
	})

	// Test finding entries at different positions
	testPaths := []string{
		entries[0].RelativePath(),                // First
		entries[len(entries)/4].RelativePath(),   // Quarter
		entries[len(entries)/2].RelativePath(),   // Middle
		entries[len(entries)*3/4].RelativePath(), // Three quarters
		entries[len(entries)-1].RelativePath(),   // Last
	}

	for i, path := range testPaths {
		b.Run(fmt.Sprintf("Find_position_%d", i), func(b *testing.B) {
			b.ReportAllocs()
			for j := 0; j < b.N; j++ {
				found := sw.Find(path)
				if found == nil {
					b.Fatal("Entry not found")
				}
			}
		})
	}
}
