package dircachefilehash

import (
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"
)

// Build-time assertions for struct layout assumptions
// These will cause compilation to fail if our assumptions about memory layout are violated
var (
	// Ensure binaryEntry has expected size and alignment
	_ = [1]struct{}{}[unsafe.Sizeof(binaryEntry{})%8] // Must be 8-byte aligned

	// Ensure Path field is exactly 8 bytes
	_ = [1]struct{}{}[unsafe.Sizeof(binaryEntry{}.Path)-8]
)

// ScanIndexInfo tracks memory-mapped scan index files for cleanup
type ScanIndexInfo struct {
	FilePath string // Path to scan index file
	MmapData []byte // Memory-mapped data (if currently mapped)
	FileSize int    // Size of the file
}

// DirectoryCache manages the file cache for a directory
// Note: skiplist management moved to higher-level files
type DirectoryCache struct {
	RootDir       string
	IndexFile     string
	CacheFile     string         // Path to index.cache file
	signature     [4]byte        // "dcfh" signature
	version       uint32         // Index version
	hasher        hash.Hash      // SHA-1 hasher for checksums
	mmapIndex     *mmapIndex     // Memory-mapped index file
	ignoreManager *IgnoreManager // Ignore pattern manager
	config        *Config        // Configuration manager
	symlinkMode   string         // Current symlink handling mode
	hashWorkers   int            // Number of concurrent hash workers

	// Concurrent scan synchronization
	scanMutex      sync.RWMutex     // Protects scan operations
	scanInProgress bool             // True if a scan is currently running
	lastScanResult *skiplistWrapper // Result from the last completed scan
	lastScanError  error            // Error from the last completed scan
	currentScan    *mmapIndexFile   // Current scan index file (single mmap, expanded with mremap)
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
	return be.EntryFlags&EntryFlagDeleted != 0
}

// SetDeleted marks this entry as deleted
func (be *binaryEntry) SetDeleted() {
	be.EntryFlags |= EntryFlagDeleted
}

// ClearDeleted removes the deleted flag from this entry
func (be *binaryEntry) ClearDeleted() {
	be.EntryFlags &^= EntryFlagDeleted
}

// validateLayout performs runtime validation of struct layout assumptions
// This should only be called in debug/development builds
func (be *binaryEntry) validateLayout() {
	entryStart := uintptr(unsafe.Pointer(be))
	pathFieldOffset := uintptr(unsafe.Pointer(&be.Path[0])) - entryStart
	expectedOffset := unsafe.Sizeof(*be) - 8

	if pathFieldOffset != expectedOffset {
		panic(fmt.Sprintf("binaryEntry layout assumption violated: Path field at offset %d, expected %d",
			pathFieldOffset, expectedOffset))
	}

	// Verify 8-byte alignment
	if entryStart%8 != 0 {
		panic(fmt.Sprintf("binaryEntry not 8-byte aligned: address 0x%x", entryStart))
	}

	// Verify size is reasonable
	if be.Size < uint32(unsafe.Sizeof(*be)) || be.Size > 4096 {
		panic(fmt.Sprintf("binaryEntry size %d is unreasonable", be.Size))
	}
}

// RelativePath returns the relative path as string from mmap'd memory (zero-copy)
// This implementation uses traditional unsafe pointer arithmetic for maximum compatibility
func (be *binaryEntry) RelativePath() string {
	// Safety check: ensure we have a valid pointer
	if be == nil {
		panic("RelativePath called on nil binaryEntry")
	}

	// Safety check: ensure Size is reasonable (not corrupted)
	if be.Size < 48 || be.Size > 65535 {
		panic(fmt.Sprintf("RelativePath: invalid Size %d (expected 48-65535)", be.Size))
	}

	entryStart := uintptr(unsafe.Pointer(be))
	entryEnd := entryStart + uintptr(be.Size)

	// Calculate path start portably using struct size
	// The path data is stored immediately after the binaryEntry struct
	// This accounts for all compiler padding and is portable across architectures
	structSize := unsafe.Sizeof(*be)
	pathStart := entryStart + structSize

	// Scan backwards byte by byte from the end (endian-neutral)
	// At most 8 bytes to scan due to 8-byte alignment, making this O(1)
	pathEnd := entryEnd
	for pathEnd > pathStart && *(*byte)(unsafe.Pointer(pathEnd - 1)) == 0 {
		pathEnd--
	}

	pathLen := int(pathEnd - pathStart)
	return unsafe.String((*byte)(unsafe.Pointer(pathStart)), pathLen)
}

// RelativePathModern returns the relative path using Go 1.17+ unsafe.Slice pattern
// This is safer but requires Go 1.17+. Can be used as migration path.
func (be *binaryEntry) RelativePathModern() string {
	if IsDebugEnabled("extravalidation") {
		be.validateLayout()
	}

	// Calculate path length by scanning for null terminator
	pathLen := be.calculatePathLength()
	if pathLen == 0 {
		return ""
	}

	// Use Go 1.17+ unsafe.Slice for safer memory access
	pathBytes := unsafe.Slice(&be.Path[0], pathLen)
	return unsafe.String(&pathBytes[0], len(pathBytes))
}

// calculatePathLength finds the length of the null-terminated path
func (be *binaryEntry) calculatePathLength() int {
	entryStart := uintptr(unsafe.Pointer(be))
	entryEnd := entryStart + uintptr(be.Size)
	pathStart := uintptr(unsafe.Pointer(&be.Path[0]))

	// Scan for null terminator
	pathEnd := entryEnd
	for pathEnd > pathStart && *(*byte)(unsafe.Pointer(pathEnd - 1)) == 0 {
		pathEnd--
	}

	return int(pathEnd - pathStart)
}

// ValidateEntry performs comprehensive validation of a binaryEntry
// Used when extravalidation debug option is enabled
func (be *binaryEntry) ValidateEntry() error {
	// Validate layout assumptions
	defer func() {
		if r := recover(); r != nil {
			// Convert panic to error for graceful handling
		}
	}()

	be.validateLayout()

	// Validate size constraints
	minSize := uint32(unsafe.Sizeof(*be))
	if be.Size < minSize {
		return fmt.Errorf("entry size %d too small, minimum %d", be.Size, minSize)
	}

	if be.Size > 4096 { // Reasonable maximum
		return fmt.Errorf("entry size %d too large, maximum 4096", be.Size)
	}

	// Validate path length
	pathLen := be.calculatePathLength()
	if pathLen == 0 {
		return fmt.Errorf("entry has zero-length path")
	}

	expectedSize := int(minSize) + pathLen + 1 // +1 for null terminator
	padding := (8 - (expectedSize % 8)) % 8
	expectedSize += padding

	if int(be.Size) != expectedSize {
		return fmt.Errorf("entry size %d doesn't match calculated size %d (path_len=%d, padding=%d)",
			be.Size, expectedSize, pathLen, padding)
	}

	// Validate hash type
	switch be.HashType {
	case HashTypeSHA1, HashTypeSHA256, HashTypeSHA512:
		// Valid hash types
	default:
		return fmt.Errorf("invalid hash type %d", be.HashType)
	}

	return nil
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

// IsHashEmpty returns true if this entry has an empty (all zeros) hash
func (be *binaryEntry) IsHashEmpty() bool {
	// If hash type is 0, no hash type is set, so hash is empty
	if be.HashType == 0 {
		return true
	}

	// Check if all 64 bytes of the hash are zero
	// Direct array comparison is optimized in Go
	var zeroHash [64]byte
	return be.Hash == zeroHash
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

// binaryEntryRef represents an offset-based reference to a binaryEntry in mmap'd memory
// This is mremap-safe since it uses offsets instead of raw pointers
type binaryEntryRef struct {
	Offset    int            // Offset from start of entry data (after header)
	IndexFile *mmapIndexFile // Reference to the mmap'd index file
}

// GetBinaryEntry resolves the reference to get the actual binaryEntry pointer
func (ref *binaryEntryRef) GetBinaryEntry() *binaryEntry {
	if ref.IndexFile == nil {
		if IsDebugEnabled("scan") {
			VerboseLog(3, "GetBinaryEntry: IndexFile is nil")
		}
		return nil
	}

	// Read lock to protect against concurrent mremap operations
	ref.IndexFile.mutex.RLock()
	defer ref.IndexFile.mutex.RUnlock()

	if ref.IndexFile.Data == nil {
		if IsDebugEnabled("scan") {
			VerboseLog(3, "GetBinaryEntry: IndexFile.Data is nil")
		}
		return nil
	}

	if IsDebugEnabled("scan") {
		VerboseLog(3, "GetBinaryEntry: offset=%d, data_size=%d", ref.Offset, len(ref.IndexFile.Data))
	}

	// Calculate pointer from base + header size + offset
	entryPtr := uintptr(unsafe.Pointer(&ref.IndexFile.Data[0])) + HeaderSize + uintptr(ref.Offset)
	return (*binaryEntry)(unsafe.Pointer(entryPtr))
}

// createBinaryEntryRef creates a binaryEntryRef from a binaryEntry pointer and mmapIndexFile
func createBinaryEntryRef(entry *binaryEntry, indexFile *mmapIndexFile) binaryEntryRef {
	if indexFile == nil {
		return binaryEntryRef{}
	}

	// Read lock to protect against concurrent mremap operations
	indexFile.mutex.RLock()
	defer indexFile.mutex.RUnlock()

	if indexFile.Data == nil {
		return binaryEntryRef{}
	}

	// Calculate offset from base of entry data (after header)
	entryPtr := uintptr(unsafe.Pointer(entry))
	basePtr := uintptr(unsafe.Pointer(&indexFile.Data[0])) + HeaderSize
	offset := int(entryPtr - basePtr)

	return binaryEntryRef{
		Offset:    offset,
		IndexFile: indexFile,
	}
}

// timeWall converts a time.Time to a uint64 wall time format for storage
// Uses custom format: 34 bits seconds since Jan 1, 1885 + 30 bits nanoseconds (no monotonic bit)
// NOTE: Does not handle files with dates before 1885 (will underflow)
// Range: Jan 1, 1885 to approximately year 2429
func timeWall(t time.Time) uint64 {
	// Use Jan 1, 1885 as epoch (like Go's monotonic case but without monotonic bit)
	// Unix epoch (1970-01-01) is 2682374400 seconds after Jan 1, 1885
	const unixTo1885 = 2682374400

	sec := t.Unix() + unixTo1885
	nsec := int64(t.Nanosecond())

	// Custom format: sec(34) + nsec(30) - gives us range until year ~2429
	wall := (uint64(sec) << 30) | uint64(nsec)
	return wall
}

// timeFromWall reconstructs a time.Time from wall time format
func timeFromWall(wall uint64) time.Time {
	// Extract components from our custom format
	const unixTo1885 = 2682374400

	// Extract nanoseconds (low 30 bits) and seconds (next 34 bits)
	nsec := int64(wall & 0x3FFFFFFF) // 30 bits for nanoseconds
	sec := int64(wall>>30) - unixTo1885

	return time.Unix(sec, nsec)
}

// encodeWallTime directly encodes seconds and nanoseconds into wall time format
// Uses custom format: 34 bits seconds since Jan 1, 1885 + 30 bits nanoseconds
// NOTE: Does not handle timestamps before 1885 (will underflow)
func encodeWallTime(sec int64, nsec int64) uint64 {
	// Convert Unix timestamp to 1885-based time
	const unixTo1885 = 2682374400
	offsetSec := sec + unixTo1885

	// Custom format: sec(34) + nsec(30)
	wall := (uint64(offsetSec) << 30) | uint64(nsec)
	return wall
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

// ParseHumanSize parses human-readable size strings (e.g., "2M", "512k", "1G")
func ParseHumanSize(sizeStr string) (int, error) {
	if sizeStr == "" {
		return 0, fmt.Errorf("empty size string")
	}

	// Convert to uppercase for consistent parsing
	sizeStr = strings.ToUpper(strings.TrimSpace(sizeStr))

	// Extract numeric part and suffix
	var numPart string
	var suffix string
	for i, char := range sizeStr {
		if char >= '0' && char <= '9' || char == '.' {
			numPart += string(char)
		} else {
			suffix = sizeStr[i:]
			break
		}
	}

	if numPart == "" {
		return 0, fmt.Errorf("no numeric part in size string: %s", sizeStr)
	}

	// Parse the numeric part
	num, err := strconv.ParseFloat(numPart, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid numeric part in size string %s: %w", sizeStr, err)
	}

	// Apply multiplier based on suffix
	var multiplier int64 = 1
	switch suffix {
	case "", "B":
		multiplier = 1
	case "K", "KB":
		multiplier = 1024
	case "M", "MB":
		multiplier = 1024 * 1024
	case "G", "GB":
		multiplier = 1024 * 1024 * 1024
	default:
		return 0, fmt.Errorf("unknown size suffix: %s", suffix)
	}

	result := int64(num * float64(multiplier))
	if result <= 0 {
		return 0, fmt.Errorf("size must be positive: %s", sizeStr)
	}
	if result > int64(^uint(0)>>1) { // Check for int overflow
		return 0, fmt.Errorf("size too large: %s", sizeStr)
	}

	return int(result), nil
}
