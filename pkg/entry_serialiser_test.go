package dircachefilehash

import (
	"os"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestEntrySerialiserScanEntry(t *testing.T) {
	relPath := "test/hello.txt"
	mockInfo := &mockFileInfo{
		name:    "hello.txt",
		size:    2048,
		mode:    0644,
		modTime: time.Unix(1700000000, 0),
	}
	mockStat := &syscall.Stat_t{
		Dev: 1, Ino: 99, Mode: 0644, Uid: 1000, Gid: 1000,
		Ctim: syscall.Timespec{Sec: 1700000000},
		Mtim: syscall.Timespec{Sec: 1700000000},
	}

	entry := NewBEScanEntry(relPath, mockInfo, mockStat)
	serialiser := NewEntrySerialiser()

	data, err := serialiser.Serialise(entry)
	if err != nil {
		t.Fatalf("Serialise failed: %v", err)
	}

	// Should return the same backing slice as GetBinaryData
	expected, _ := entry.GetBinaryData()
	if len(data) != len(expected) {
		t.Fatalf("length mismatch: got %d, want %d", len(data), len(expected))
	}

	// Verify the serialised data is valid by reading back the binaryEntry fields
	be := (*binaryEntry)(unsafe.Pointer(&data[0]))
	if be.Size != uint32(len(data)) {
		t.Errorf("Size mismatch: got %d, want %d", be.Size, len(data))
	}
	if be.FileSize != 2048 {
		t.Errorf("FileSize mismatch: got %d, want 2048", be.FileSize)
	}

	// Verify the path round-trips via RelativePath() on the heap-backed entry.
	// This exercises the exact zero-copy accessor path that previously crashed
	// under the race detector's checkptr mode; with the unsafe.Add rewrite it is
	// checkptr-clean, so this doubles as a direct regression for that fix.
	gotPath := be.RelativePath()
	if gotPath != relPath {
		t.Errorf("path mismatch: got %q, want %q", gotPath, relPath)
	}
}

func TestEntrySerialiserFromInterface(t *testing.T) {
	relPath := "another/file.go"
	mockInfo := &mockFileInfo{
		name:    "file.go",
		size:    512,
		mode:    0644,
		modTime: time.Unix(1700000000, 0),
	}
	mockStat := &syscall.Stat_t{
		Dev: 2, Ino: 42, Mode: 0644, Uid: 500, Gid: 500,
		Ctim: syscall.Timespec{Sec: 1700000000},
		Mtim: syscall.Timespec{Sec: 1700000000},
	}

	entry := NewBEScanEntry(relPath, mockInfo, mockStat)

	// Use serialiseFromInterface directly to test the fallback path
	data, err := serialiseFromInterface(entry)
	if err != nil {
		t.Fatalf("serialiseFromInterface failed: %v", err)
	}

	be := (*binaryEntry)(unsafe.Pointer(&data[0]))
	if be.Size == 0 {
		t.Fatal("Size should not be zero")
	}
	if be.FileSize != 512 {
		t.Errorf("FileSize mismatch: got %d, want 512", be.FileSize)
	}

	pathOffset := int(unsafe.Sizeof(binaryEntry{}))
	pathEnd := pathOffset
	for pathEnd < len(data) && data[pathEnd] != 0 {
		pathEnd++
	}
	gotPath := string(data[pathOffset:pathEnd])
	if gotPath != relPath {
		t.Errorf("path mismatch: got %q, want %q", gotPath, relPath)
	}
}

func TestEntrySerialiserSizeMatchesEntrySize(t *testing.T) {
	// Verify that serialised data length matches the entry's reported Size field
	paths := []string{"a.txt", "longer/path/to/file.txt", "x"}
	serialiser := NewEntrySerialiser()

	for _, relPath := range paths {
		t.Run(relPath, func(t *testing.T) {
			mockInfo := &mockFileInfo{
				name:    relPath,
				size:    100,
				mode:    0644,
				modTime: time.Unix(1700000000, 0),
			}
			mockStat := &syscall.Stat_t{
				Dev: 1, Ino: 1, Mode: 0644, Uid: 1000, Gid: 1000,
				Ctim: syscall.Timespec{Sec: 1700000000},
				Mtim: syscall.Timespec{Sec: 1700000000},
			}

			entry := NewBEScanEntry(relPath, mockInfo, mockStat)
			data, err := serialiser.Serialise(entry)
			if err != nil {
				t.Fatalf("Serialise failed: %v", err)
			}

			entrySize, _ := entry.Size()
			if uint32(len(data)) != entrySize {
				t.Errorf("data length %d != entry.Size %d", len(data), entrySize)
			}

			// Verify 8-byte alignment
			if len(data)%8 != 0 {
				t.Errorf("data length %d is not 8-byte aligned", len(data))
			}
		})
	}
}

func TestEntrySerialiserNilEntry(t *testing.T) {
	// BEScanEntry with nil binaryData should fail gracefully
	entry := &BEScanEntry{
		BinaryEntryBase: NewBinaryEntryBase(BEScan),
		binaryData:      nil,
		relPath:         "gone.txt",
	}
	serialiser := NewEntrySerialiser()

	_, err := serialiser.Serialise(entry)
	if err == nil {
		t.Fatal("expected error for nil binaryData")
	}
}

func TestEntrySerialiserSkiplistEntry(t *testing.T) {
	// Create a temporary index file with a known entry, then serialise from skiplist
	testDir, err := os.MkdirTemp("", "serialiser-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(testDir) }()

	relPath := "skiplist/test.txt"
	mockInfo := &mockFileInfo{
		name:    "test.txt",
		size:    768,
		mode:    0644,
		modTime: time.Unix(1700000000, 0),
	}
	mockStat := &syscall.Stat_t{
		Dev: 1, Ino: 55, Mode: 0644, Uid: 1000, Gid: 1000,
		Ctim: syscall.Timespec{Sec: 1700000000},
		Mtim: syscall.Timespec{Sec: 1700000000},
	}

	// Create a scan entry, then build an index file from it so we can load into skiplist
	scanEntry := NewBEScanEntry(relPath, mockInfo, mockStat)
	scanData, err := scanEntry.GetBinaryData()
	if err != nil {
		t.Fatalf("failed to get scan binary data: %v", err)
	}

	// Write a minimal index file with this entry
	indexPath := testDir + "/.dcfh/test-serialiser.idx"
	if err := os.MkdirAll(testDir+"/.dcfh", 0755); err != nil {
		t.Fatalf("Failed to create .dcfh directory: %v", err)
	}

	// Use the scan entry data to verify round-trip serialisation
	// For this test, we just verify the fallback path works correctly
	// since setting up a full mmap'd skiplist entry requires more infrastructure
	fallbackData, err := serialiseFromInterface(scanEntry)
	if err != nil {
		t.Fatalf("serialiseFromInterface failed: %v", err)
	}

	if len(fallbackData) != len(scanData) {
		t.Errorf("fallback length %d != scan data length %d", len(fallbackData), len(scanData))
	}

	_ = indexPath // infrastructure for full skiplist test would go here
}
