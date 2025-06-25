package dircachefilehash

import (
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unsafe"
)

// DirectoryCache manages the file cache for a directory
// Note: skiplist management moved to higher-level files
type DirectoryCache struct {
	RootDir       string
	IndexFile     string
	CacheFile     string         // Path to index.cache file
	signature     [4]byte        // "dcfh" signature
	version       uint32         // Index version
	hasher        hash.Hash      // SHA-1 hasher for checksums
	mmapIndex     *MmapIndex     // Memory-mapped index file
	ignoreManager *IgnoreManager // Ignore pattern manager
}

// binaryEntry represents a file entry in mmap'd memory (zero-copy)
// All fields are in host byte order for direct access
// Time fields use Go's wall time format (uint64 encoding)
type binaryEntry struct {
	Size       uint32   // Total size of this entry including padding (host order) - MUST BE FIRST
	CTimeWall  uint64   // Change time wall clock (Go wall time format)
	MTimeWall  uint64   // Modification time wall clock (Go wall time format)
	Dev        uint32   // Device ID (host order)
	Ino        uint32   // Inode number (host order)
	Mode       uint32   // File mode (host order)
	UID        uint32   // User ID (host order)
	GID        uint32   // Group ID (host order)
	FileSize   uint64   // File size in bytes (host order) - supports files >4GB
	EntryFlags uint16   // Entry Flags
	HashType   uint16   // Hash algorithm type (SHA1=1, SHA256=2, SHA512=3)
	Hash       [64]byte // Hash value (up to 64 bytes for SHA-512)
	Path       [8]byte  // Path as bytes, actual length variable but must be at least 8 bytes long
}

// IsDeleted returns true if this entry is marked as deleted
func (be *binaryEntry) IsDeleted() bool {
	return be.EntryFlags&uint16(EntryFlagDeleted) != 0
}

// SetDeleted marks this entry as deleted
func (be *binaryEntry) SetDeleted() {
	be.EntryFlags |= uint16(EntryFlagDeleted)
}

// ClearDeleted removes the deleted flag from this entry
func (be *binaryEntry) ClearDeleted() {
	be.EntryFlags &^= uint16(EntryFlagDeleted)
}

// RelativePath returns the relative path as string from mmap'd memory (zero-copy)
func (be *binaryEntry) RelativePath() string {
	entryStart := uintptr(unsafe.Pointer(be))
	entryEnd := entryStart + uintptr(be.Size)
	pathStart := uintptr(unsafe.Pointer(&be.Path[0]))

	// Scan backwards byte by byte from the end (endian-neutral)
	// At most 8 bytes to scan due to 8-byte alignment, making this O(1)
	pathEnd := entryEnd
	for pathEnd > pathStart && *(*byte)(unsafe.Pointer(pathEnd - 1)) == 0 {
		pathEnd--
	}

	pathLen := int(pathEnd - pathStart)
	return unsafe.String((*byte)(unsafe.Pointer(pathStart)), pathLen)
}

// HashString returns the hash as a hex string
func (be *binaryEntry) HashString() string {
	// Determine hash size based on type
	var hashSize int
	switch be.HashType {
	case HashTypeSHA1:
		hashSize = HashSizeSHA1
	case HashTypeSHA256:
		hashSize = HashSizeSHA256
	case HashTypeSHA512:
		hashSize = HashSizeSHA512
	default:
		hashSize = HashSizeSHA1 // Default to SHA1 for compatibility
	}

	const hexChars = "0123456789abcdef"
	result := make([]byte, hashSize*2)
	for i := 0; i < hashSize; i++ {
		b := be.Hash[i]
		result[i*2] = hexChars[b>>4]
		result[i*2+1] = hexChars[b&0xf]
	}
	return unsafe.String(&result[0], len(result))
}

// EntrySize returns the total size of this entry including padding
func (be *binaryEntry) EntrySize() int {
	return int(be.Size)
}

// BESizeFromPathLen calculates the necessary size of a binaryEntry struct given pathname length
func BESizeFromPathLen(pathLen int) int {
	baseSize := int(unsafe.Sizeof(binaryEntry{}))
	totalSize := baseSize + pathLen + 1 // +1 for null terminator
	padding := (8 - (totalSize % 8)) % 8
	return totalSize + padding
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

// generateTempFileName generates a temporary filename with PID and timestamp
func (dc *DirectoryCache) generateTempFileName(prefix string) string {
	pid := os.Getpid()
	timestamp := time.Now().UnixNano()
	return filepath.Join(filepath.Dir(dc.IndexFile),
		fmt.Sprintf("%s-%d-%d.tmp", prefix, pid, timestamp))
}

// getGoroutineID extracts goroutine ID from runtime stack
func getGoroutineID() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	idField := strings.Fields(string(buf[:n]))[1]
	id, _ := strconv.ParseUint(idField, 10, 64)
	return id
}

// generateScanFileName generates a scan index filename with PID and goroutine ID
func (dc *DirectoryCache) generateScanFileName() string {
	pid := os.Getpid()
	tid := getGoroutineID()
	return filepath.Join(filepath.Dir(dc.IndexFile),
		fmt.Sprintf("scan-%d-%d.idx", pid, tid))
}

// generateTmpIndexFileName generates a tmp index filename with PID and goroutine ID
func (dc *DirectoryCache) generateTmpIndexFileName() string {
	pid := os.Getpid()
	tid := getGoroutineID()
	return filepath.Join(filepath.Dir(dc.IndexFile),
		fmt.Sprintf("tmp-%d-%d.idx", pid, tid))
}
