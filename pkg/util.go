package dircachefilehash

import (
	"os"
	"time"
)

// DirectoryCache manages the file cache for a directory
type DirectoryCache struct {
	RootDir   string
	IndexFile string
	entries   []FileEntry
	signature [4]byte // "dcfh" signature
	version   uint32  // Index version
}

// FileEntry represents a file with its hash and metadata
// Fields are ordered to match git dircache index file format
type FileEntry struct {
	CTime        time.Time `json:"ctime"`         // Change time (metadata last changed, seconds since epoch)
	CTimeNano    int32     `json:"ctime_nano"`    // Change time nanoseconds
	MTime        time.Time `json:"mtime"`         // Modification time (content last modified, seconds since epoch)
	MTimeNano    int32     `json:"mtime_nano"`    // Modification time nanoseconds
	Dev          uint32    `json:"dev"`           // Device ID
	Ino          uint32    `json:"ino"`           // Inode number
	Mode         uint32    `json:"mode"`          // File mode (permissions and type)
	UID          uint32    `json:"uid"`           // User ID (owner)
	GID          uint32    `json:"gid"`           // Group ID
	Size         uint32    `json:"size"`          // File size in bytes
	Hash         string    `json:"hash"`          // SHA-1 hash (40 hex chars)
	Flags        uint16    `json:"flags"`         // Index flags
	PathLen      uint16    `json:"path_len"`      // Length of relative path (big-endian)
	RelativePath string    `json:"relative_path"` // Relative path from root directory
}

// fileJob represents a file hashing job
type fileJob struct {
	path    string
	info    os.FileInfo
	relPath string
	index   int // Original order for sorting
}

// fileResult represents the result of a file hashing job
type fileResult struct {
	entry *FileEntry
	err   error
	index int // Original order for sorting
}

// binaryEntry represents the fixed-size binary portion of a file entry
type binaryEntry struct {
	CTimeUnix uint32   // Change time seconds
	CTimeNano uint32   // Change time nanoseconds
	MTimeUnix uint32   // Modification time seconds
	MTimeNano uint32   // Modification time nanoseconds
	Dev       uint32   // Device ID
	Ino       uint32   // Inode number
	Mode      uint32   // File mode
	UID       uint32   // User ID
	GID       uint32   // Group ID
	Size      uint32   // File size
	Hash      [20]byte // SHA-1 hash (20 bytes)
	Flags     uint16   // Index flags
	PathLen   uint16   // Length of relative path
}

// GetEntries returns a copy of the current entries
func (dc *DirectoryCache) GetEntries() []FileEntry {
	entries := make([]FileEntry, len(dc.entries))
	copy(entries, dc.entries)
	return entries
}

// Stats returns statistics about the cache
func (dc *DirectoryCache) Stats() (int, int64, error) {
	var totalSize int64
	for _, entry := range dc.entries {
		totalSize += int64(entry.Size)
	}
	return len(dc.entries), totalSize, nil
}
