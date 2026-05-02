package dircachefilehash

import (
	"os"
	"syscall"
	"time"
)

// scannedPath represents a file found during filesystem scanning.
// It flows from the walk through the Walker abstraction into the
// FilesystemScanIterator, Hwang-Lin compare, and hash pipeline.
type scannedPath struct {
	AbsPath  string
	RelPath  string
	Info     os.FileInfo
	StatInfo *syscall.Stat_t
}

// mockFileInfo implements os.FileInfo for synthesised entries
// (e.g. deleted entries reconstructed from index data when no
// underlying file exists on disk).
type mockFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
}

func (m *mockFileInfo) Name() string       { return m.name }
func (m *mockFileInfo) Size() int64        { return m.size }
func (m *mockFileInfo) Mode() os.FileMode  { return m.mode }
func (m *mockFileInfo) ModTime() time.Time { return m.modTime }
func (m *mockFileInfo) IsDir() bool        { return m.mode.IsDir() }
func (m *mockFileInfo) Sys() any           { return nil }
