package main

import (
	"unsafe"

	"github.com/mattkeenan/dircachefilehash/pkg/format"
)

// Wire-layout fixture builders for the cmd/dcfhfix tests. The surgical header
// writer they once exercised relocated to pkg in task 28.2 (see
// pkg/fix_header_test.go), but the integration/promote tests still build raw
// index bytes here.

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

// layHeaderBytes lays a current-version Header into a HeaderSize buffer.
func layHeaderBytes(entryCount uint32) []byte {
	buf := make([]byte, format.HeaderSize)
	h := (*format.Header)(unsafe.Pointer(&buf[0]))
	h.SetHeader([4]byte{'d', 'c', 'f', 'h'}, format.CurrentIndexVersion, entryCount, format.IndexFlagClean, format.HashTypeSHA1)
	return buf
}
