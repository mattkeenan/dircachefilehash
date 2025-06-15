package dircachefilehash

import (
	"hash"
	"os"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
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
	Size      uint32   // Total size of this entry including padding (host order) - MUST BE FIRST
	CTimeWall uint64   // Change time wall clock (Go wall time format)
	MTimeWall uint64   // Modification time wall clock (Go wall time format)
	Dev       uint32   // Device ID (host order)
	Ino       uint32   // Inode number (host order)
	Mode      uint32   // File mode (host order)
	UID       uint32   // User ID (host order)
	GID       uint32   // Group ID (host order)
	FileSize  uint64   // File size in bytes (host order) - supports files >4GB
	Flags     uint16   // Index flags (host order)
	Hash      [20]byte // SHA-1 hash (20 bytes, byte order irrelevant)
	// Variable-length path follows immediately after this struct
}

// RelativePath returns the relative path string from mmap'd memory (zero-copy)
func (be *binaryEntry) RelativePath() string {
	pathBytes := be.RelativePathBytes()
	if len(pathBytes) == 0 {
		return ""
	}
	return unsafe.String(&pathBytes[0], len(pathBytes))
}

// RelativePathBytes returns the relative path as byte slice from mmap'd memory (zero-copy)
func (be *binaryEntry) RelativePathBytes() []byte {
	entryStart := uintptr(unsafe.Pointer(be))
	entryEnd := entryStart + uintptr(be.Size)
	pathStart := entryStart + unsafe.Sizeof(*be)

	// Scan backwards byte by byte from the end (endian-neutral)
	// At most 8 bytes to scan due to 8-byte alignment, making this O(1)
	pathEnd := entryEnd
	for pathEnd > pathStart && *(*byte)(unsafe.Pointer(pathEnd - 1)) == 0 {
		pathEnd--
	}

	pathLen := int(pathEnd - pathStart)
	return unsafe.Slice((*byte)(unsafe.Pointer(pathStart)), pathLen)
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
	return int(be.Size)
}

// PathLenToSize calculates the necessary size of a binaryEntry struct given pathname length
func PathLenToSize(pathLen int) int {
	baseSize := int(unsafe.Sizeof(binaryEntry{}))
	totalSize := baseSize + pathLen + 1 // +1 for null terminator
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
		totalSize += int64(entry.FileSize)
	}
	return len(dc.entries), totalSize, nil
}

// IsMmapped returns true if the cache is using memory-mapped storage
func (dc *DirectoryCache) IsMmapped() bool {
	return dc.mmapIndex != nil
}

// sysUnusedOS hints to the OS that this memory region can be written to disk
func sysUnusedOS(ptr unsafe.Pointer, size int) {
	// Use madvise to hint that this memory is no longer needed in RAM
	unix.Madvise((*[1 << 30]byte)(ptr)[:size:size], unix.MADV_DONTNEED)
}

// timeWall extracts the wall field from time.Time using unsafe operations
func timeWall(t time.Time) uint64 {
	return *(*uint64)(unsafe.Pointer(&t))
}

// timeFromWall reconstructs a time.Time from wall time format
func timeFromWall(wall uint64) time.Time {
	var t time.Time
	*(*uint64)(unsafe.Pointer(&t)) = wall
	return t
}

// encodeWallTime directly encodes seconds and nanoseconds into Go's wall time format
func encodeWallTime(sec int64, nsec int64) uint64 {
	// Create time.Time with full nanosecond precision and extract wall time
	t := time.Unix(sec, nsec)
	return timeWall(t)
}
