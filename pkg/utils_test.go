package dircachefilehash

import (
	"testing"
	"time"
	"unsafe"
)

func TestTimeWallFunctions(t *testing.T) {
	t.Run("timeWallAndTimeFromWall", func(t *testing.T) {
		// Test with current time
		now := time.Now()
		wall := timeWall(now)
		reconstructed := timeFromWall(wall)

		// The reconstructed time should be very close to the original
		// Note: There might be some precision loss, so we allow for small differences
		diff := now.Sub(reconstructed)
		if diff < 0 {
			diff = -diff
		}

		// Allow up to 1 microsecond difference due to precision
		if diff > time.Microsecond {
			t.Errorf("Time reconstruction failed: original=%v, reconstructed=%v, diff=%v",
				now, reconstructed, diff)
		}
	})

	t.Run("timeWallWithSpecificTimes", func(t *testing.T) {
		testTimes := []time.Time{
			time.Unix(0, 0),                                          // Unix epoch
			time.Unix(1000000000, 0),                                 // Year 2001
			time.Unix(1000000000, 123456789),                         // With nanoseconds
			time.Unix(2000000000, 987654321),                         // Year 2033 with nanoseconds
			time.Date(2023, 12, 25, 15, 30, 45, 123456789, time.UTC), // Specific date
		}

		for i, testTime := range testTimes {
			wall := timeWall(testTime)
			reconstructed := timeFromWall(wall)

			// Check if times are equal or very close
			diff := testTime.Sub(reconstructed)
			if diff < 0 {
				diff = -diff
			}

			if diff > time.Microsecond {
				t.Errorf("Test %d failed: original=%v, reconstructed=%v, diff=%v",
					i, testTime, reconstructed, diff)
			}
		}
	})

	t.Run("encodeWallTime", func(t *testing.T) {
		testCases := []struct {
			sec  int64
			nsec int64
		}{
			{0, 0},
			{1000000000, 0},
			{1000000000, 123456789},
			{2000000000, 987654321},
			{-1, 0}, // Test negative time
		}

		for i, tc := range testCases {
			encoded := encodeWallTime(tc.sec, tc.nsec)

			// Create a time.Time from the same sec/nsec and compare
			expected := time.Unix(tc.sec, tc.nsec)
			expectedWall := timeWall(expected)

			if encoded != expectedWall {
				t.Errorf("Test %d failed: encodeWallTime(%d, %d) = %d, expected %d",
					i, tc.sec, tc.nsec, encoded, expectedWall)
			}

			// Also test round-trip
			reconstructed := timeFromWall(encoded)
			recreatedTime := time.Unix(tc.sec, tc.nsec)

			diff := recreatedTime.Sub(reconstructed)
			if diff < 0 {
				diff = -diff
			}

			if diff > time.Microsecond {
				t.Errorf("Test %d round-trip failed: diff=%v", i, diff)
			}
		}
	})

	t.Run("timeWallConsistency", func(t *testing.T) {
		// Test that our time functions are consistent with Go's internal time representation
		baseTime := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

		// Add various durations and test consistency
		durations := []time.Duration{
			0,
			time.Nanosecond,
			time.Microsecond,
			time.Millisecond,
			time.Second,
			time.Minute,
			time.Hour,
			24 * time.Hour,
		}

		for _, duration := range durations {
			testTime := baseTime.Add(duration)
			wall := timeWall(testTime)
			reconstructed := timeFromWall(wall)

			// Should be able to reconstruct the time accurately
			if !testTime.Equal(reconstructed) {
				// Allow for small precision differences
				diff := testTime.Sub(reconstructed)
				if diff < 0 {
					diff = -diff
				}

				if diff > time.Microsecond {
					t.Errorf("Consistency test failed for duration %v: original=%v, reconstructed=%v, diff=%v",
						duration, testTime, reconstructed, diff)
				}
			}
		}
	})
}

func TestHashTypeFunctions(t *testing.T) {
	t.Run("HashTypeConstants", func(t *testing.T) {
		// Verify constants have expected values
		if HashTypeSHA1 != 1 {
			t.Errorf("HashTypeSHA1 should be 1, got %d", HashTypeSHA1)
		}
		if HashTypeSHA256 != 2 {
			t.Errorf("HashTypeSHA256 should be 2, got %d", HashTypeSHA256)
		}
		if HashTypeSHA512 != 3 {
			t.Errorf("HashTypeSHA512 should be 3, got %d", HashTypeSHA512)
		}
	})

	t.Run("HashSizeConstants", func(t *testing.T) {
		// Verify size constants
		if HashSizeSHA1 != 20 {
			t.Errorf("HashSizeSHA1 should be 20, got %d", HashSizeSHA1)
		}
		if HashSizeSHA256 != 32 {
			t.Errorf("HashSizeSHA256 should be 32, got %d", HashSizeSHA256)
		}
		if HashSizeSHA512 != 64 {
			t.Errorf("HashSizeSHA512 should be 64, got %d", HashSizeSHA512)
		}
	})
}

func TestBinaryEntryStructure(t *testing.T) {
	t.Run("BinaryEntryAlignment", func(t *testing.T) {
		// Test that binaryEntry structure has expected size and alignment
		entrySize := unsafe.Sizeof(binaryEntry{})

		// Verify Size field is first (critical for mmap operations)
		entry := &binaryEntry{}
		sizeFieldOffset := unsafe.Offsetof(entry.Size)

		if sizeFieldOffset != 0 {
			t.Errorf("Size field should be at offset 0, got %d", sizeFieldOffset)
		}

		// Verify the structure is properly aligned
		if entrySize%8 != 0 {
			t.Errorf("binaryEntry size %d is not 8-byte aligned", entrySize)
		}

		t.Logf("binaryEntry size: %d bytes", entrySize)
	})

	t.Run("BinaryEntryFieldOffsets", func(t *testing.T) {
		entry := &binaryEntry{}

		// Test that critical fields are at expected positions
		offsets := map[string]uintptr{
			"Size":      unsafe.Offsetof(entry.Size),
			"CTimeWall": unsafe.Offsetof(entry.CTimeWall),
			"MTimeWall": unsafe.Offsetof(entry.MTimeWall),
			"Dev":       unsafe.Offsetof(entry.Dev),
			"Ino":       unsafe.Offsetof(entry.Ino),
			"Mode":      unsafe.Offsetof(entry.Mode),
			"UID":       unsafe.Offsetof(entry.UID),
			"GID":       unsafe.Offsetof(entry.GID),
			"FileSize":  unsafe.Offsetof(entry.FileSize),
			"Flags":     unsafe.Offsetof(entry.Flags),
			"HashType":  unsafe.Offsetof(entry.HashType),
			"Hash":      unsafe.Offsetof(entry.Hash),
		}

		t.Logf("binaryEntry field offsets:")
		for field, offset := range offsets {
			t.Logf("  %s: %d", field, offset)
		}

		// Size field must be first
		if offsets["Size"] != 0 {
			t.Errorf("Size field must be at offset 0, got %d", offsets["Size"])
		}

		// Verify fields are in ascending order (good for cache locality)
		if offsets["CTimeWall"] <= offsets["Size"] {
			t.Error("CTimeWall should come after Size")
		}
		if offsets["Hash"] <= offsets["HashType"] {
			t.Error("Hash should come after HashType")
		}
	})
}

func TestPathLenToSizeFunction(t *testing.T) {
	t.Run("PathLenToSizeCalculation", func(t *testing.T) {
		baseSize := int(unsafe.Sizeof(binaryEntry{}))

		testCases := []struct {
			pathLen     int
			description string
		}{
			{0, "empty path"},
			{1, "single character"},
			{7, "7 characters (alignment boundary test)"},
			{8, "8 characters"},
			{15, "15 characters (alignment boundary test)"},
			{16, "16 characters"},
			{31, "31 characters"},
			{32, "32 characters"},
			{63, "63 characters"},
			{64, "64 characters"},
			{100, "100 characters"},
			{255, "255 characters (max typical filename)"},
		}

		for _, tc := range testCases {
			t.Run(tc.description, func(t *testing.T) {
				result := PathLenToSize(tc.pathLen)

				// Calculate expected size manually
				totalSize := baseSize + tc.pathLen + 1 // +1 for null terminator
				padding := (8 - (totalSize % 8)) % 8
				expected := totalSize + padding

				if result != expected {
					t.Errorf("PathLenToSize(%d) = %d, expected %d", tc.pathLen, result, expected)
				}

				// Verify result is 8-byte aligned
				if result%8 != 0 {
					t.Errorf("PathLenToSize(%d) = %d, not 8-byte aligned", tc.pathLen, result)
				}

				// Verify result is large enough for the path
				minRequired := baseSize + tc.pathLen + 1
				if result < minRequired {
					t.Errorf("PathLenToSize(%d) = %d, less than minimum required %d",
						tc.pathLen, result, minRequired)
				}

				// Verify we don't waste too much space (padding should be < 8 bytes)
				wastedSpace := result - minRequired
				if wastedSpace >= 8 {
					t.Errorf("PathLenToSize(%d) wastes %d bytes (should be < 8)", tc.pathLen, wastedSpace)
				}
			})
		}
	})

	t.Run("PathLenToSizeProperties", func(t *testing.T) {
		// Test mathematical properties of the function

		// Monotonicity: longer paths should require same or more space
		for i := 0; i < 100; i++ {
			size1 := PathLenToSize(i)
			size2 := PathLenToSize(i + 1)

			if size2 < size1 {
				t.Errorf("PathLenToSize is not monotonic: PathLenToSize(%d)=%d > PathLenToSize(%d)=%d",
					i, size1, i+1, size2)
			}
		}

		// Test that alignment works for various base sizes
		baseSize := int(unsafe.Sizeof(binaryEntry{}))
		for pathLen := 0; pathLen < 50; pathLen++ {
			size := PathLenToSize(pathLen)

			// Should always be aligned
			if size%8 != 0 {
				t.Errorf("PathLenToSize(%d) = %d is not 8-byte aligned", pathLen, size)
			}

			// Should contain enough space for base + path + null terminator
			minSpace := baseSize + pathLen + 1
			if size < minSpace {
				t.Errorf("PathLenToSize(%d) = %d is less than required space %d",
					pathLen, size, minSpace)
			}
		}
	})
}

func TestConstants(t *testing.T) {
	t.Run("HeaderAndChecksumSizes", func(t *testing.T) {
		// Verify HeaderSize matches actual header struct
		if HeaderSize != 12 {
			t.Errorf("HeaderSize should be 12, got %d", HeaderSize)
		}

		// Verify ChecksumSize is correct for SHA-1
		if ChecksumSize != 20 {
			t.Errorf("ChecksumSize should be 20 (SHA-1), got %d", ChecksumSize)
		}

		// Test that HeaderSize matches IndexHeader struct size
		headerStructSize := int(unsafe.Sizeof(IndexHeader{}))
		if HeaderSize != headerStructSize {
			t.Errorf("HeaderSize constant (%d) doesn't match IndexHeader struct size (%d)",
				HeaderSize, headerStructSize)
		}
	})

	t.Run("IndexHeaderStructure", func(t *testing.T) {
		header := &IndexHeader{}

		// Test field sizes and offsets
		signatureOffset := unsafe.Offsetof(header.Signature)
		versionOffset := unsafe.Offsetof(header.Version)
		entryCountOffset := unsafe.Offsetof(header.EntryCount)

		if signatureOffset != 0 {
			t.Errorf("Signature should be at offset 0, got %d", signatureOffset)
		}

		expectedVersionOffset := uintptr(4) // 4 bytes for signature
		if versionOffset != expectedVersionOffset {
			t.Errorf("Version should be at offset %d, got %d", expectedVersionOffset, versionOffset)
		}

		expectedEntryCountOffset := uintptr(8) // 4 bytes signature + 4 bytes version
		if entryCountOffset != expectedEntryCountOffset {
			t.Errorf("EntryCount should be at offset %d, got %d", expectedEntryCountOffset, entryCountOffset)
		}

		// Test field sizes
		signatureSize := unsafe.Sizeof(header.Signature)
		versionSize := unsafe.Sizeof(header.Version)
		entryCountSize := unsafe.Sizeof(header.EntryCount)

		if signatureSize != 4 {
			t.Errorf("Signature size should be 4, got %d", signatureSize)
		}
		if versionSize != 4 {
			t.Errorf("Version size should be 4, got %d", versionSize)
		}
		if entryCountSize != 4 {
			t.Errorf("EntryCount size should be 4, got %d", entryCountSize)
		}

		t.Logf("IndexHeader layout: Signature@%d[%d], Version@%d[%d], EntryCount@%d[%d], Total=%d",
			signatureOffset, signatureSize, versionOffset, versionSize,
			entryCountOffset, entryCountSize, unsafe.Sizeof(*header))
	})
}

func TestFileJobAndResult(t *testing.T) {
	t.Run("FileJobStructure", func(t *testing.T) {
		job := fileJob{}

		// Verify all fields are present and accessible
		job.path = "test/path"
		job.relPath = "relative/path"
		job.index = 42
		job.info = nil // This would normally be an os.FileInfo

		if job.path != "test/path" {
			t.Error("fileJob.path assignment failed")
		}
		if job.relPath != "relative/path" {
			t.Error("fileJob.relPath assignment failed")
		}
		if job.index != 42 {
			t.Error("fileJob.index assignment failed")
		}
	})

	t.Run("FileResultStructure", func(t *testing.T) {
		result := fileResult{}

		// Verify all fields are present and accessible
		result.entry = nil // This would normally be a *binaryEntry
		result.err = nil
		result.index = 123

		if result.index != 123 {
			t.Error("fileResult.index assignment failed")
		}
	})
}

func TestMmapIndexStructure(t *testing.T) {
	t.Run("MmapIndexFields", func(t *testing.T) {
		mmapIdx := &MmapIndex{}

		// Test that all fields are accessible
		mmapIdx.data = make([]byte, 100)
		mmapIdx.file = nil
		mmapIdx.header = nil
		mmapIdx.entries = make([]byte, 50)
		mmapIdx.size = 100
		mmapIdx.offset = 20

		if len(mmapIdx.data) != 100 {
			t.Error("MmapIndex.data assignment failed")
		}
		if len(mmapIdx.entries) != 50 {
			t.Error("MmapIndex.entries assignment failed")
		}
		if mmapIdx.size != 100 {
			t.Error("MmapIndex.size assignment failed")
		}
		if mmapIdx.offset != 20 {
			t.Error("MmapIndex.offset assignment failed")
		}
	})
}

func TestDirectoryCacheStructure(t *testing.T) {
	t.Run("DirectoryCacheFields", func(t *testing.T) {
		// Test that DirectoryCache has all expected fields
		cache := &DirectoryCache{}

		cache.RootDir = "/test/root"
		cache.IndexFile = "/test/index"
		cache.entries = make([]*binaryEntry, 0)
		cache.signature = [4]byte{'t', 'e', 's', 't'}
		cache.version = 1
		cache.hasher = nil
		cache.mmapIndex = nil

		if cache.RootDir != "/test/root" {
			t.Error("DirectoryCache.RootDir assignment failed")
		}
		if cache.IndexFile != "/test/index" {
			t.Error("DirectoryCache.IndexFile assignment failed")
		}
		if cache.version != 1 {
			t.Error("DirectoryCache.version assignment failed")
		}
		if cache.signature != [4]byte{'t', 'e', 's', 't'} {
			t.Error("DirectoryCache.signature assignment failed")
		}
	})
}

func TestHashJobAndResult(t *testing.T) {
	t.Run("HashJobStructure", func(t *testing.T) {
		job := hashJob{}

		job.entry = nil
		job.filePath = "/test/file"
		job.deviceID = 12345

		if job.filePath != "/test/file" {
			t.Error("hashJob.filePath assignment failed")
		}
		if job.deviceID != 12345 {
			t.Error("hashJob.deviceID assignment failed")
		}
	})

	t.Run("HashResultStructure", func(t *testing.T) {
		result := hashResult{}

		result.entry = nil
		result.hash = []byte{1, 2, 3, 4}
		result.hashType = HashTypeSHA1
		result.err = nil

		if len(result.hash) != 4 {
			t.Error("hashResult.hash assignment failed")
		}
		if result.hashType != HashTypeSHA1 {
			t.Error("hashResult.hashType assignment failed")
		}
	})
}

func TestUtilityFunctionProperties(t *testing.T) {
	t.Run("MemoryAlignment", func(t *testing.T) {
		// Test that our structures are properly aligned for performance
		structures := map[string]uintptr{
			"binaryEntry": unsafe.Sizeof(binaryEntry{}),
			"IndexHeader": unsafe.Sizeof(IndexHeader{}),
			"fileJob":     unsafe.Sizeof(fileJob{}),
			"fileResult":  unsafe.Sizeof(fileResult{}),
			"hashJob":     unsafe.Sizeof(hashJob{}),
			"hashResult":  unsafe.Sizeof(hashResult{}),
		}

		for name, size := range structures {
			t.Logf("%s size: %d bytes", name, size)

			// Check if structure size is reasonable (not too large)
			maxReasonableSize := uintptr(1024) // 1KB should be plenty for any of these
			if size > maxReasonableSize {
				t.Errorf("%s size %d is unusually large (>%d)", name, size, maxReasonableSize)
			}
		}
	})

	t.Run("ZeroCopyProperties", func(t *testing.T) {
		// Test properties that enable zero-copy operations

		// binaryEntry should be suitable for direct memory mapping
		entry := &binaryEntry{}
		entrySize := unsafe.Sizeof(*entry)

		// Should be aligned for efficient memory access
		if entrySize%8 != 0 {
			t.Logf("Note: binaryEntry size %d is not 8-byte aligned", entrySize)
		}

		// Test that we can safely cast between pointer types for zero-copy
		data := make([]byte, entrySize)
		entryPtr := (*binaryEntry)(unsafe.Pointer(&data[0]))

		// Should be able to set and read fields
		entryPtr.Size = 123
		entryPtr.FileSize = 456

		if entryPtr.Size != 123 {
			t.Error("Failed to set Size field via pointer cast")
		}
		if entryPtr.FileSize != 456 {
			t.Error("Failed to set FileSize field via pointer cast")
		}
	})
}
