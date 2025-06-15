package dircachefilehash

import (
	"hash"
	"os"
	"unsafe"
)

// DirectoryCache manages the file cache for a directory
type DirectoryCache struct {
	RootDir   string
	IndexFile string
	entries   []*binaryEntry // Direct pointers to mmap'd entries
	signature [4]byte        // "dcfh" signature
	version   uint32         // Index version
	hasher    hash.Hash      // SHA-1 hasher for checksums
	mmapIndex *MmapIndex     // Memory-mapped index file
}

// binaryEntry represents a file entry in mmap'd memory (zero-copy)
// All fields are in host byte order for direct access
// Time fields use Go's wall time format (uint64 encoding)
type binaryEntry struct {
	CTimeWall uint64   // Change time wall clock (Go wall time format)
	MTimeWall uint64   // Modification time wall clock (Go wall time format)
	Dev       uint32   // Device ID (host order)
	Ino       uint32   // Inode number (host order)
	Mode      uint32   // File mode (host order)
	UID       uint32   // User ID (host order)
	GID       uint32   // Group ID (host order)
	Size      uint32   // File size (host order)
	Hash      [20]byte // SHA-1 hash (20 bytes, byte order irrelevant)
	Flags     uint16   // Index flags (host order)
	PathLen   uint16   // Length of relative path (host order)
	// Variable-length path follows immediately after this struct
}

// RelativePath returns the relative path string from mmap'd memory (zero-copy)
func (be *binaryEntry) RelativePath() string {
	pathPtr := unsafe.Pointer(uintptr(unsafe.Pointer(be)) + unsafe.Sizeof(*be))
	pathBytes := unsafe.Slice((*byte)(pathPtr), be.PathLen)
	return unsafe.String((*byte)(pathPtr), be.PathLen)
}

// RelativePathBytes returns the relative path as byte slice from mmap'd memory (zero-copy)
func (be *binaryEntry) RelativePathBytes() []byte {
	pathPtr := unsafe.Pointer(uintptr(unsafe.Pointer(be)) + unsafe.Sizeof(*be))
	return unsafe.Slice((*byte)(pathPtr), be.PathLen)
}

// HashString returns the hash as a hex string
func (be *binaryEntry) HashString() string {
	const hexChars = "0123456789abcdef"
	var result [40]byte
	for i, b := range be.Hash {
		result[i*2] = hexChars[b>>4]
		result[i*2+1] = hexChars[b&0xf]
	}
	return unsafe.String(&result[0], 40)
}

// EntrySize returns the total size of this entry including padding
func (be *binaryEntry) EntrySize() int {
	baseSize := int(unsafe.Sizeof(*be))
	totalSize := baseSize + int(be.PathLen) + 1 // +1 for null terminator
	padding := (8 - (totalSize % 8)) % 8
	return totalSize + padding
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
	entry *binaryEntry
	err   error
	index int // Original order for sorting
}

// GetEntries returns direct pointers to mmap'd entries (zero-copy)
func (dc *DirectoryCache) GetEntries() []*binaryEntry {
	return dc.entries
}

// Stats returns statistics about the cache
func (dc *DirectoryCache) Stats() (int, int64, error) {
	var totalSize int64
	for _, entry := range dc.entries {
		totalSize += int64(entry.Size)
	}
	return len(dc.entries), totalSize, nil
}

// IsMmapped returns true if the cache is using memory-mapped storage
func (dc *DirectoryCache) IsMmapped() bool {
	return dc.mmapIndex != nil
}
