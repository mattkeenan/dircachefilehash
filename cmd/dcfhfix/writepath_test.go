package main

import (
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"github.com/mattkeenan/dircachefilehash/pkg/format"
)

// layEntryBytes lays a fixed Entry struct + variable path into a buffer of
// exactly the entry's on-disk size (the production wire layout).
func layEntryBytes(path string, dev format.DevID, ino format.Inode) []byte {
	size := format.BESizeFromPathLen(len(path))
	buf := make([]byte, size)
	e := (*format.Entry)(unsafe.Pointer(&buf[0]))
	e.Size = format.RecordSize(size)
	e.Dev = dev
	e.Ino = ino
	e.Mode = 0o100644
	e.HashType = format.HashTypeSHA1
	copy(buf[format.MinEntrySize():], path)
	return buf
}

// layHeaderBytes lays a v3 Header into a HeaderSize buffer.
func layHeaderBytes(entryCount uint32) []byte {
	buf := make([]byte, format.HeaderSize)
	h := (*format.Header)(unsafe.Pointer(&buf[0]))
	h.SetHeader([4]byte{'d', 'c', 'f', 'h'}, format.CurrentIndexVersion, entryCount, format.IndexFlagClean, format.HashTypeSHA1)
	return buf
}

// TC-3: dcfhfix's header write path uses format.Header (104 bytes). The write
// cast (*[HeaderSize]byte)(unsafe.Pointer(customHeader)) therefore reads exactly
// the struct — the prior 8-byte over-read (from the deleted 96-byte indexHeader
// duplicate) is gone. The written file must have a correct 104-byte v3 header
// (Timestamp preserved) with the original entry intact at offset 104.
func TestWritePath_CustomHeader_NoOverRead(t *testing.T) {
	entry := layEntryBytes("repo/file.go", 0x11223344, 0x55667788)
	orig := append(layHeaderBytes(1), entry...)

	entryData := &EntryData{
		IndexFile:    "in-memory",
		OriginalData: orig,
		EntryCount:   1,
	}

	// Custom header carrying a distinctive Timestamp that must survive the
	// full-struct write cast untouched.
	chBuf := make([]byte, format.HeaderSize)
	ch := (*format.Header)(unsafe.Pointer(&chBuf[0]))
	ch.SetHeader([4]byte{'d', 'c', 'f', 'h'}, format.CurrentIndexVersion, 1, format.IndexFlagClean, format.HashTypeSHA1)
	ch.Timestamp = 0x0123456789ABCDEF

	out := filepath.Join(t.TempDir(), "out.idx")
	if err := writeIndexWithCustomHeader(entryData, out, ch); err != nil {
		t.Fatalf("writeIndexWithCustomHeader: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read written index: %v", err)
	}
	if len(data) != len(orig) {
		t.Fatalf("written length = %d, want %d (header %d + entry %d)", len(data), len(orig), format.HeaderSize, len(entry))
	}

	// Header: 104 bytes, current version, byte-order magic, distinctive Timestamp intact.
	h := (*format.Header)(unsafe.Pointer(&data[0]))
	if err := h.ValidateByteOrder(); err != nil {
		t.Errorf("written header ByteOrder: %v", err)
	}
	if h.Version != format.CurrentIndexVersion {
		t.Errorf("written Version = %d, want %d", h.Version, format.CurrentIndexVersion)
	}
	if h.EntryCount != 1 {
		t.Errorf("written EntryCount = %d, want 1", h.EntryCount)
	}
	if h.Timestamp != 0x0123456789ABCDEF {
		t.Errorf("written Timestamp = %#x, want 0x0123456789ABCDEF (over/under-read of header)", h.Timestamp)
	}

	// Entry intact at offset HeaderSize (104), not shifted by an over/under-read.
	se, err := format.NewSafeEntry(data, 0, format.HeaderSize, format.CurrentIndexVersion)
	if err != nil {
		t.Fatalf("NewSafeEntry at offset %d: %v", format.HeaderSize, err)
	}
	if got, _ := se.GetDev(); got != 0x11223344 {
		t.Errorf("entry GetDev = %#x, want 0x11223344", got)
	}
	if got, err := se.GetPath(); err != nil || got != "repo/file.go" {
		t.Errorf("entry GetPath = %q, %v; want \"repo/file.go\"", got, err)
	}
}
