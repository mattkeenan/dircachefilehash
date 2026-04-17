package dircachefilehash

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// indexHeader represents the file header in host byte order (cast directly to mmap'd memory).
//
// On-disk layout (64-bit little-endian):
//
//	Offset  Size  Field
//	0       4     Signature      "dcfh"
//	4       4     _Pad0          alignment padding for ByteOrder
//	8       8     ByteOrder      0x0102030405060708
//	16      4     Version        2 or 3
//	20      4     EntryCount
//	24      2     Flags
//	26      2     ChecksumType
//	28      64    Checksum       SHA-1/256/512 (v2 entry data starts at offset 88, inside unused tail)
//	92      4     _Pad1          alignment padding for Timestamp (v3 only)
//	96      8     Timestamp      Unix seconds (v3 only, not covered by checksum)
//	---     ---   ---
//	v2 total: 88 bytes used (entries start here; overlaps Checksum[60:64] + _Pad1)
//	v3 total: 104 bytes
type indexHeader struct {
	Signature    [4]byte  // offset 0:   "dcfh" signature
	_Pad0        [4]byte  // offset 4:   alignment padding for ByteOrder
	ByteOrder    uint64   // offset 8:   byte order detection magic - MUST be checked before other fields
	Version      uint32   // offset 16:  index version (host order)
	EntryCount   uint32   // offset 20:  number of entries (host order)
	Flags        uint16   // offset 24:  index flags (host order)
	ChecksumType uint16   // offset 26:  checksum algorithm type
	Checksum     [64]byte // offset 28:  checksum of header+entries (up to 512-bit)
	_Pad1        [4]byte  // offset 92:  alignment padding for Timestamp
	Timestamp    uint64   // offset 96:  unix timestamp of last write (v3+, not covered by checksum)
}

// headerSizeForVersion returns the header size for a given index version.
func headerSizeForVersion(version uint32) int {
	if version <= 2 {
		return V2HeaderSize
	}
	return HeaderSize
}

// HeaderSizeForVersion returns the header size for a given index version (exported for dcfhfix).
func HeaderSizeForVersion(version uint32) int {
	return headerSizeForVersion(version)
}

// mmapIndex represents a memory-mapped index file
type mmapIndex struct {
	data []byte
	file *os.File
}

// mmapIndexFile represents a wrapper for index file lifecycle management
type mmapIndexFile struct {
	File       *os.File     // File descriptor (nil for read-only main/cache indices)
	Data       []byte       // Memory-mapped data
	Size       int          // Current size of the mapping
	Offset     int          // Current write offset for scan indices
	Type       string       // Index type: "main", "cache", "scan"
	FilePath   string       // File path for debugging/cleanup
	headerSize int          // Version-dependent header size (V2HeaderSize or HeaderSize)
	mutex      sync.RWMutex // Protects Data/Size during mremap operations
	refCount   int32        // Atomic reference counter for safe cleanup
}

// Cleanup safely unmaps and closes the index file
func (mif *mmapIndexFile) Cleanup() error {
	mif.mutex.Lock()
	defer mif.mutex.Unlock()

	if mif.Data != nil {
		if err := unix.Munmap(mif.Data); err != nil {
			return fmt.Errorf("failed to unmap %s index: %w", mif.Type, err)
		}
		mif.Data = nil
	}

	if mif.File != nil {
		if err := mif.File.Close(); err != nil {
			return fmt.Errorf("failed to close %s index file: %w", mif.Type, err)
		}
		mif.File = nil
	}

	return nil
}

// IncRef atomically increments the reference count
func (mif *mmapIndexFile) IncRef() {
	atomic.AddInt32(&mif.refCount, 1)
}

// DecRef atomically decrements the reference count and cleans up if it reaches zero
func (mif *mmapIndexFile) DecRef() {
	if atomic.AddInt32(&mif.refCount, -1) == 0 {
		// Last reference released - safe to cleanup
		if err := mif.Cleanup(); err != nil {
			// Log error but don't return it since DecRef() should not fail
			if IsDebugEnabled("load") {
				VerboseLog(2, "Warning: cleanup failed during DecRef for %s: %v", mif.Type, err)
			}
		}
	}
}

// RefCount returns the current reference count (for debugging)
func (mif *mmapIndexFile) RefCount() int32 {
	return atomic.LoadInt32(&mif.refCount)
}

// Header returns a direct pointer to the header in mmap'd memory (zero-copy)
func (mi *mmapIndex) Header() *indexHeader {
	return (*indexHeader)(unsafe.Pointer(&mi.data[0]))
}

// ValidateSignature checks if the signature matches expected value
func (ih *indexHeader) ValidateSignature(expected [4]byte) error {
	if ih.Signature != expected {
		return fmt.Errorf("invalid signature: got %q, expected %q",
			string(ih.Signature[:]), string(expected[:]))
	}
	return nil
}

// ValidateVersion checks if the version is supported.
// Pass expected=0 to accept any version (used by read-only tools like dcfhfind).
// Otherwise accepts versions in range [MinIndexVersion, expected].
func (ih *indexHeader) ValidateVersion(expected uint32) error {
	if expected == 0 {
		return nil
	}
	if ih.Version < MinIndexVersion || ih.Version > expected {
		return fmt.Errorf("unsupported version: got %d, expected %d-%d", ih.Version, MinIndexVersion, expected)
	}
	return nil
}

// ValidateByteOrder checks if the byte order matches the host machine
func (ih *indexHeader) ValidateByteOrder() error {
	if ih.ByteOrder != ByteOrderMagic {
		return fmt.Errorf("byte order mismatch: index file byte order 0x%016x does not match host byte order 0x%016x",
			ih.ByteOrder, ByteOrderMagic)
	}
	return nil
}

// ValidateIndexHeader validates an index file header and returns a copy of the header struct
// This is a shared utility function that can be used across the codebase for header validation
func ValidateIndexHeader(indexPath string, validateVersion bool, expectedVersion uint32) (*indexHeader, error) {
	return ValidateIndexHeaderWithOptions(indexPath, validateVersion, expectedVersion, true)
}

// ValidateIndexHeaderWithOptions validates index header with configurable checksum validation
func ValidateIndexHeaderWithOptions(indexPath string, validateVersion bool, expectedVersion uint32, validateChecksum bool) (*indexHeader, error) {
	file, err := os.Open(indexPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	// Get file size
	stat, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	if stat.Size() < int64(V2HeaderSize) {
		return nil, fmt.Errorf("file too small: %d bytes", stat.Size())
	}

	// Mmap enough for a v3 header. For v2 files smaller than HeaderSize,
	// mmap the file size — POSIX zero-fills beyond EOF for MAP_PRIVATE.
	mmapSize := HeaderSize
	if stat.Size() < int64(mmapSize) {
		mmapSize = int(stat.Size())
	}

	// Memory map the header for reading
	data, err := unix.Mmap(int(file.Fd()), 0, mmapSize, unix.PROT_READ, unix.MAP_PRIVATE)
	if err != nil {
		return nil, fmt.Errorf("failed to mmap file header: %w", err)
	}
	defer func() { _ = unix.Munmap(data) }()

	// Get direct pointer to header in mmap'd memory (zero-copy).
	// For v2 files where mmapSize < sizeof(indexHeader), the Timestamp field
	// at the end of the struct may read beyond the mmap. This is safe because
	// mmap rounds up to page size, and those bytes read as zero.
	header := (*indexHeader)(unsafe.Pointer(&data[0]))

	// Verify header using the standard validation methods
	signature := [4]byte{'d', 'c', 'f', 'h'}
	if err := header.ValidateSignature(signature); err != nil {
		return nil, err
	}
	if err := header.ValidateByteOrder(); err != nil {
		return nil, err
	}
	if validateVersion {
		if err := header.ValidateVersion(expectedVersion); err != nil {
			return nil, err
		}
	}

	// Check Clean flag to determine if we should trust the header checksum
	isClean := (header.Flags & IndexFlagClean) != 0

	if validateChecksum && !isClean {
		// File wasn't closed cleanly - header checksum is likely incorrect
		// Skip checksum validation for recovery purposes
		VerboseLog(2, "Skipping header checksum validation for unclean file: %s", indexPath)
	} else if validateChecksum && isClean {
		// File was closed cleanly - validate the header checksum
		if err := validateHeaderChecksum(file, header, stat.Size()); err != nil {
			return nil, fmt.Errorf("header checksum validation failed: %w", err)
		}
	}

	// Create a copy of the header since we're unmapping the memory
	headerCopy := *header
	return &headerCopy, nil
}

// validateHeaderChecksum validates the header checksum against the file contents
func validateHeaderChecksum(file *os.File, header *indexHeader, fileSize int64) error {
	// Calculate expected checksum using same order as TempIndexWriter iterative approach
	hasher := sha1.New()

	// Entry data starts after the version-specific header
	hdrSize := int64(headerSizeForVersion(header.Version))

	// First: Hash entry data (matches TempIndexWriter.WriteIoVecBatch order)
	entryDataSize := fileSize - hdrSize
	if entryDataSize > 0 {
		// Read entry data
		entryData := make([]byte, entryDataSize)
		if _, err := file.ReadAt(entryData, hdrSize); err != nil {
			return fmt.Errorf("failed to read entry data for checksum validation: %w", err)
		}
		hasher.Write(entryData)
	}

	// Second: Hash header up to checksum field (matches TempIndexWriter.Close order).
	// checksumOffset is the same for v2 and v3 (Timestamp is after Checksum).
	headerBytes := (*[HeaderSize]byte)(unsafe.Pointer(header))
	checksumOffset := unsafe.Offsetof(header.Checksum)
	hasher.Write(headerBytes[:checksumOffset])

	// Compare with stored checksum
	expectedChecksum := hasher.Sum(nil)
	if !bytes.Equal(expectedChecksum, header.Checksum[:len(expectedChecksum)]) {
		return fmt.Errorf("checksum mismatch: expected %x, got %x", expectedChecksum, header.Checksum[:len(expectedChecksum)])
	}

	return nil
}

// SetHeader initialises the header fields in mmap'd memory
func (ih *indexHeader) SetHeader(signature [4]byte, version uint32, entryCount uint32, flags uint16, checksumType uint16) {
	ih.Signature = signature
	ih.ByteOrder = ByteOrderMagic
	ih.Version = version
	ih.EntryCount = entryCount
	ih.Flags = flags
	ih.ChecksumType = checksumType
	ih.Timestamp = uint64(time.Now().Unix())
}

// SetHeaderForWritableIndex initialises the header for write operations (scan/temp indices)
// Automatically clears the Clean flag since we're opening for write
func (ih *indexHeader) SetHeaderForWritableIndex(signature [4]byte, version uint32, entryCount uint32, baseFlags uint16, checksumType uint16) {
	// For writable indices, ensure Clean flag is cleared (not clean during write operations)
	flags := baseFlags &^ IndexFlagClean
	ih.SetHeader(signature, version, entryCount, flags, checksumType)
}

// calculateAndStoreHeaderChecksum calculates checksum and stores it in header
func (dc *DirectoryCache) calculateAndStoreHeaderChecksum(header *indexHeader, entryData []byte, entrySize int) {
	hasher := dc.hasher
	hasher.Reset()

	// IMPORTANT: Use same order as TempIndexWriter: entry data first, then header fields

	// First: Hash entry data if any (matches TempIndexWriter.WriteIoVecBatch order)
	if entrySize > 0 {
		hasher.Write(entryData[:entrySize])
	}

	// Second: Hash header up to checksum field (matches TempIndexWriter.addHeaderToChecksum order)
	headerBytes := (*[HeaderSize]byte)(unsafe.Pointer(header))
	checksumOffset := unsafe.Offsetof(header.Checksum)
	hasher.Write(headerBytes[:checksumOffset])

	// Store checksum in header
	checksumBytes := hasher.Sum(nil)
	copy(header.Checksum[:], checksumBytes)
}

// isClean returns true if this index file is in a clean/complete state
func (ih *indexHeader) isClean() bool {
	return ih.Flags&IndexFlagClean != 0
}

// setClean marks this index file as clean/complete (final operation)
func (ih *indexHeader) setClean() {
	ih.Flags |= IndexFlagClean
}

// clearClean marks this index file as unclean/incomplete
func (ih *indexHeader) clearClean() {
	ih.Flags &^= IndexFlagClean
}

// writeBinaryEntryToMmap writes a binaryEntry directly to mmap'd memory (PRIVATE - only for scan index)
func (dc *DirectoryCache) writeBinaryEntryToMmap(data []byte, relPath string, hash []byte, hashType uint16, info os.FileInfo, stat *syscall.Stat_t, isDeleted bool) {
	// Calculate total entry size first
	baseSize := int(unsafe.Sizeof(binaryEntry{}))
	totalSize := baseSize + len(relPath) + 1 // +1 for null terminator
	padding := (8 - (totalSize % 8)) % 8
	entrySize := totalSize + padding

	// Write binaryEntry directly to mmap'd memory
	entry := (*binaryEntry)(unsafe.Pointer(&data[0]))

	entry.Size = uint32(entrySize) // Total size of this entry
	entry.CTimeWall = encodeWallTime(stat.Ctim.Sec, stat.Ctim.Nsec)
	entry.MTimeWall = encodeWallTime(stat.Mtim.Sec, stat.Mtim.Nsec)
	entry.Dev = uint32(stat.Dev)
	entry.Ino = uint32(stat.Ino)
	entry.Mode = uint32(info.Mode())
	entry.UID = stat.Uid
	entry.GID = stat.Gid
	entry.FileSize = uint64(info.Size()) // File content size
	entry.HashType = hashType
	entry.EntryFlags = 0

	// Set deleted flag if needed
	if isDeleted {
		entry.SetDeleted()
	}

	// Clear hash field and copy hash data
	for i := range entry.Hash {
		entry.Hash[i] = 0
	}
	copy(entry.Hash[:], hash)

	// Write variable-size path directly after struct
	pathOffset := int(unsafe.Sizeof(*entry))
	copy(data[pathOffset:pathOffset+len(relPath)], relPath)

	// Add null terminator
	data[pathOffset+len(relPath)] = 0

	// Zero out padding
	for i := range padding {
		data[totalSize+i] = 0
	}
}

// EntryProcessor defines a callback function for processing entries during index loading
// Parameters: entry (the binaryEntry), entryIndex (0-based), filePath (source file)
// Returns: shouldInclude (whether to include in result), error (if processing failed)
type EntryProcessor func(entry *binaryEntry, entryIndex uint32, filePath string) (shouldInclude bool, err error)

// LoadIndexFromFileForValidation is a public wrapper for loadIndexFromFile used by dcfh index commands
func (dc *DirectoryCache) LoadIndexFromFileForValidation(filePath string) ([]binaryEntryRef, error) {
	// Use verbose processor for validation operations to maintain existing behaviour
	return dc.loadIndexFromFileWithProcessor(filePath, VerboseEntryProcessor())
}

// LoadIndexFromFileWithProcessor loads an index file with custom entry processing
func (dc *DirectoryCache) LoadIndexFromFileWithProcessor(filePath string, processor EntryProcessor) ([]binaryEntryRef, error) {
	return dc.loadIndexFromFileWithProcessor(filePath, processor)
}

// loadIndexFromFileWithProcessor is the internal implementation with callback support
func (dc *DirectoryCache) loadIndexFromFileWithProcessor(filePath string, processor EntryProcessor) ([]binaryEntryRef, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open index file %s: %w", filePath, err)
	}

	// Get file size
	stat, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	if stat.Size() < int64(V2HeaderSize) {
		_ = file.Close()
		return nil, fmt.Errorf("file too small: %d bytes", stat.Size())
	}

	// Memory map the file for reading
	data, err := unix.Mmap(int(file.Fd()), 0, int(stat.Size()), unix.PROT_READ, unix.MAP_PRIVATE)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("failed to mmap file: %w", err)
	}

	// Get direct pointer to header in mmap'd memory (zero-copy)
	header := (*indexHeader)(unsafe.Pointer(&data[0]))

	// Verify header; on failure, clean up both mmap and file
	if err := header.ValidateSignature(dc.signature); err != nil {
		_ = unix.Munmap(data)
		_ = file.Close()
		return nil, err
	}
	if err := header.ValidateByteOrder(); err != nil {
		_ = unix.Munmap(data)
		_ = file.Close()
		return nil, err
	}
	if err := header.ValidateVersion(dc.version); err != nil {
		_ = unix.Munmap(data)
		_ = file.Close()
		return nil, err
	}

	hdrSize := headerSizeForVersion(header.Version)
	indexFile := &mmapIndexFile{
		File:       file,
		Data:       data,
		Size:       int(stat.Size()),
		Type:       "loaded",
		FilePath:   filePath,
		headerSize: hdrSize,
	}

	isClean := (header.Flags & IndexFlagClean) != 0

	if !isClean {
		VerboseLog(2, "Skipping header checksum validation for unclean file: %s", filePath)
	} else {
		if err := dc.verifyHeaderChecksum(data, header); err != nil {
			return nil, fmt.Errorf("checksum verification failed: %w", err)
		}
	}

	// Parse entries with callback processing
	var refs []binaryEntryRef
	offset := 0
	entryData := data[hdrSize:]

	for i := uint32(0); i < header.EntryCount; i++ {
		if offset >= len(entryData) {
			return nil, fmt.Errorf("unexpected end of data at entry %d", i)
		}

		// Get direct pointer to binaryEntry in mmap'd memory
		entry := (*binaryEntry)(unsafe.Pointer(&entryData[offset]))

		// Validate binaryEntry chaining consistency
		if err := dc.validateEntryChaining(entry, offset, entryData, int(i)); err != nil {
			return nil, fmt.Errorf("entry %d validation failed: %w", i, err)
		}

		// Perform extra validation if debug flag is enabled
		if IsDebugEnabled("extravalidation") {
			if err := entry.ValidateEntry(); err != nil {
				return nil, fmt.Errorf("entry %d extra validation failed: %w", i, err)
			}
		}

		// Call the processor callback
		shouldInclude := true
		if processor != nil {
			include, err := processor(entry, i, filePath)
			if err != nil {
				return nil, fmt.Errorf("entry processor failed at entry %d: %w", i, err)
			}
			shouldInclude = include
		}

		// Only include entry if processor says so
		if shouldInclude {
			// Create binaryEntryRef instead of storing pointer
			ref := binaryEntryRef{
				Offset:    offset, // Offset from start of entry data
				IndexFile: indexFile,
			}
			refs = append(refs, ref)
		}

		// Move to next entry using Size field
		nextOffset := offset + int(entry.Size)

		// Validate chaining consistency: current entry + Size = next entry
		if IsDebugEnabled("indexchaining") && i < header.EntryCount-1 {
			if nextOffset >= len(entryData) {
				return nil, fmt.Errorf("entry %d size %d would exceed data bounds (offset %d + size = %d, max %d)",
					i, entry.Size, offset, nextOffset, len(entryData))
			}
		}

		offset = nextOffset
	}

	// Final validation: ensure we consumed exactly the expected amount of data
	if offset != len(entryData) {
		return nil, fmt.Errorf("data size mismatch: consumed %d bytes, expected %d bytes", offset, len(entryData))
	}

	return refs, nil
}

// Processor factory functions for different use cases

// DefaultEntryProcessor returns a processor that includes all entries (normal loading behaviour)
func DefaultEntryProcessor() EntryProcessor {
	return func(entry *binaryEntry, entryIndex uint32, filePath string) (bool, error) {
		return true, nil
	}
}

// VerboseEntryProcessor returns a processor that outputs verbose information based on global verbose level
func VerboseEntryProcessor() EntryProcessor {
	return func(entry *binaryEntry, entryIndex uint32, filePath string) (bool, error) {
		entryPath := entry.RelativePath()

		if GetVerboseLevel() >= 1 {
			VerboseLog(1, "%s", entryPath) // Level 1: filename only (like 'ls')
		}
		if GetVerboseLevel() >= 2 {
			// Level 2: ls -l style output (mode, index filename, mtime, path)
			mtime := timeFromWall(entry.MTimeWall)
			VerboseLog(2, "  %04o %8d %s %s (%s)", entry.Mode&0o7777, entry.FileSize,
				mtime.Format("2006-01-02 15:04:05"), entryPath, filepath.Base(filePath))
		}
		if GetVerboseLevel() >= 3 {
			// Level 3: complete breakdown of each field in binaryEntry
			VerboseLog(3, "  Entry %d details:", entryIndex)
			VerboseLog(3, "    Size: %d bytes", entry.Size)
			VerboseLog(3, "    CTimeWall: %d (%s)", entry.CTimeWall, timeFromWall(entry.CTimeWall))
			VerboseLog(3, "    MTimeWall: %d (%s)", entry.MTimeWall, timeFromWall(entry.MTimeWall))
			VerboseLog(3, "    Dev: %d", entry.Dev)
			VerboseLog(3, "    Ino: %d", entry.Ino)
			VerboseLog(3, "    Mode: 0o%o", entry.Mode)
			VerboseLog(3, "    UID: %d", entry.UID)
			VerboseLog(3, "    GID: %d", entry.GID)
			VerboseLog(3, "    FileSize: %d", entry.FileSize)
			VerboseLog(3, "    EntryFlags: 0x%04x%s", entry.EntryFlags,
				func() string {
					if entry.IsDeleted() {
						return " (DELETED)"
					} else {
						return ""
					}
				}())
			VerboseLog(3, "    HashType: %d (%s)", entry.HashType, HashTypeName(entry.HashType))
			VerboseLog(3, "    Hash: %s", entry.HashString())
			VerboseLog(3, "    Path: %s", entryPath)
		}

		return true, nil
	}
}

// SearchEntryProcessor returns a processor that searches for matching entries
type SearchOptions struct {
	Pattern     string  // Filename pattern (glob)
	PathPrefix  string  // Path prefix filter
	HashPrefix  string  // Hash prefix filter
	ExactSize   *uint64 // Exact file size filter
	ShowDeleted bool    // Show only deleted entries
	SearchCount *int    // Pointer to counter for matches
}

func SearchEntryProcessor(opts SearchOptions) EntryProcessor {
	return func(entry *binaryEntry, entryIndex uint32, filePath string) (bool, error) {
		// Skip deleted entries unless specifically requested
		if entry.IsDeleted() && !opts.ShowDeleted {
			return false, nil
		}

		// Skip non-deleted entries if only deleted requested
		if !entry.IsDeleted() && opts.ShowDeleted {
			return false, nil
		}

		entryPath := entry.RelativePath()

		// Apply filters
		if opts.Pattern != "" {
			matched, err := filepath.Match(opts.Pattern, filepath.Base(entryPath))
			if err != nil {
				return false, fmt.Errorf("invalid pattern %s: %w", opts.Pattern, err)
			}
			if !matched {
				return false, nil
			}
		}

		if opts.PathPrefix != "" && !strings.HasPrefix(entryPath, opts.PathPrefix) {
			return false, nil
		}

		if opts.HashPrefix != "" {
			hashStr := entry.HashString()
			if !strings.HasPrefix(strings.ToLower(hashStr), strings.ToLower(opts.HashPrefix)) {
				return false, nil
			}
		}

		if opts.ExactSize != nil && entry.FileSize != *opts.ExactSize {
			return false, nil
		}

		// Entry matches - output it
		if opts.SearchCount != nil {
			*opts.SearchCount++
		}

		// Output the match based on verbose level
		VerboseLog(0, "%s", entryPath)
		if GetVerboseLevel() >= 1 {
			mtime := timeFromWall(entry.MTimeWall)
			deletedFlag := ""
			if entry.IsDeleted() {
				deletedFlag = " (DELETED)"
			}
			VerboseLog(1, "  %04o %8d %s %s%s",
				entry.Mode&0o7777, entry.FileSize,
				mtime.Format("2006-01-02 15:04:05"),
				filepath.Base(filePath), deletedFlag)
		}
		if GetVerboseLevel() >= 2 {
			VerboseLog(2, "  Hash: %s (%s)", entry.HashString(), HashTypeName(entry.HashType))
		}

		return false, nil // Don't include in skiplist, just process for output
	}
}

// CompositeEntryProcessor combines multiple processors (all must return true to include entry)
func CompositeEntryProcessor(processors ...EntryProcessor) EntryProcessor {
	return func(entry *binaryEntry, entryIndex uint32, filePath string) (bool, error) {
		for _, processor := range processors {
			if processor != nil {
				shouldInclude, err := processor(entry, entryIndex, filePath)
				if err != nil {
					return false, err
				}
				if !shouldInclude {
					return false, nil
				}
			}
		}
		return true, nil
	}
}

// loadIndexFromFileWithTracking loads an index file and returns both entries and the mmapIndexFile for tracking
func (dc *DirectoryCache) loadIndexFromFileWithTracking(filePath string) ([]binaryEntryRef, *mmapIndexFile, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open index file %s: %w", filePath, err)
	}

	// Get file size
	stat, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, fmt.Errorf("failed to stat file: %w", err)
	}

	if stat.Size() < int64(V2HeaderSize) {
		_ = file.Close()
		return nil, nil, fmt.Errorf("file too small: %d bytes", stat.Size())
	}

	// Memory map the file for reading
	data, err := unix.Mmap(int(file.Fd()), 0, int(stat.Size()), unix.PROT_READ, unix.MAP_PRIVATE)
	if err != nil {
		_ = file.Close()
		return nil, nil, fmt.Errorf("failed to mmap file: %w", err)
	}

	header := (*indexHeader)(unsafe.Pointer(&data[0]))

	// Verify header; on failure, clean up both mmap and file
	if err := header.ValidateSignature(dc.signature); err != nil {
		_ = unix.Munmap(data)
		_ = file.Close()
		return nil, nil, err
	}
	if err := header.ValidateByteOrder(); err != nil {
		_ = unix.Munmap(data)
		_ = file.Close()
		return nil, nil, err
	}
	if err := header.ValidateVersion(dc.version); err != nil {
		_ = unix.Munmap(data)
		_ = file.Close()
		return nil, nil, err
	}

	hdrSize := headerSizeForVersion(header.Version)
	indexFile := &mmapIndexFile{
		File:       file,
		Data:       data,
		Size:       int(stat.Size()),
		Type:       "loaded",
		FilePath:   filePath,
		headerSize: hdrSize,
	}

	isClean := (header.Flags & IndexFlagClean) != 0

	if !isClean {
		VerboseLog(2, "Skipping header checksum validation for unclean file: %s", filePath)
	} else {
		if err := dc.verifyHeaderChecksum(data, header); err != nil {
			indexFile.DecRef()
			return nil, nil, fmt.Errorf("checksum verification failed: %w", err)
		}
	}

	// Parse entries
	var refs []binaryEntryRef
	offset := 0
	entryData := data[hdrSize:]

	for i := uint32(0); i < header.EntryCount; i++ {
		if offset >= len(entryData) {
			indexFile.DecRef()
			return nil, nil, fmt.Errorf("unexpected end of data at entry %d", i)
		}

		// Get direct pointer to binaryEntry in mmap'd memory
		entry := (*binaryEntry)(unsafe.Pointer(&entryData[offset]))

		// Validate binaryEntry chaining consistency
		if err := dc.validateEntryChaining(entry, offset, entryData, int(i)); err != nil {
			indexFile.DecRef()
			return nil, nil, fmt.Errorf("entry %d validation failed: %w", i, err)
		}

		// Perform extra validation if debug flag is enabled
		if IsDebugEnabled("extravalidation") {
			if err := entry.ValidateEntry(); err != nil {
				indexFile.DecRef()
				return nil, nil, fmt.Errorf("entry %d extra validation failed: %w", i, err)
			}
		}

		// Create binaryEntryRef wrapper using createBinaryEntryRef helper
		ref := createBinaryEntryRef(entry, indexFile)
		refs = append(refs, ref)

		// Advance to next entry
		nextOffset := offset + int(entry.Size)

		// Validate that we're not going backwards or stuck
		if nextOffset <= offset {
			indexFile.DecRef()
			return nil, nil, fmt.Errorf("entry %d has invalid size %d (would not advance)", i, entry.Size)
		}

		// Debug output for entry chaining if requested
		if IsDebugEnabled("indexchaining") && i < header.EntryCount-1 {
			if nextOffset >= len(entryData) {
				indexFile.DecRef()
				return nil, nil, fmt.Errorf("entry %d size %d would exceed data bounds (offset %d + size = %d, max %d)",
					i, entry.Size, offset, nextOffset, len(entryData))
			}
		}

		offset = nextOffset
	}

	// Final validation: ensure we consumed exactly the expected amount of data
	if offset != len(entryData) {
		indexFile.DecRef()
		return nil, nil, fmt.Errorf("data size mismatch: consumed %d bytes, expected %d bytes", offset, len(entryData))
	}

	return refs, indexFile, nil
}

// verifyHeaderChecksum verifies the checksum stored in the header
func (dc *DirectoryCache) verifyHeaderChecksum(data []byte, header *indexHeader) error {
	// Get the stored checksum from header
	storedChecksum := header.Checksum[:]

	// Determine checksum algorithm from header
	var hasher hash.Hash
	var expectedSize int
	switch header.ChecksumType {
	case HashTypeSHA1:
		hasher = sha1.New()
		expectedSize = HashSizeSHA1
	case HashTypeSHA256:
		hasher = sha256.New()
		expectedSize = HashSizeSHA256
	case HashTypeSHA512:
		hasher = sha512.New()
		expectedSize = HashSizeSHA512
	default:
		return fmt.Errorf("unsupported checksum type: %d", header.ChecksumType)
	}

	// Calculate checksum of header (excluding checksum field) + entries
	// IMPORTANT: Must match TempIndexWriter order: entry data first, then header fields
	hasher.Reset()

	// Hash entry data first (matches TempIndexWriter.WriteIoVecBatch)
	hdrSize := headerSizeForVersion(header.Version)
	entryData := data[hdrSize:]
	hasher.Write(entryData)

	// Hash header fields before checksum field (matches TempIndexWriter.addHeaderToChecksum)
	headerBytes := (*[HeaderSize]byte)(unsafe.Pointer(header))
	checksumOffset := unsafe.Offsetof(header.Checksum)
	hasher.Write(headerBytes[:checksumOffset])

	calculatedChecksum := hasher.Sum(nil)

	// Compare checksums
	for i := 0; i < expectedSize; i++ {
		if storedChecksum[i] != calculatedChecksum[i] {
			return fmt.Errorf("checksum mismatch at byte %d", i)
		}
	}
	return nil
}

// Close cleans up mmap'd resources and checks for orphaned index files
func (dc *DirectoryCache) Close() error {
	// Check for orphaned index files first (ignore errors during check)
	_ = dc.checkForOrphanedIndexFiles()

	// Clean up old mmapIndex if still present
	if dc.mmapIndex != nil {
		if err := unix.Munmap(dc.mmapIndex.data); err != nil {
			return fmt.Errorf("failed to unmap: %w", err)
		}
		if err := dc.mmapIndex.file.Close(); err != nil {
			return fmt.Errorf("failed to close file: %w", err)
		}
		dc.mmapIndex = nil
	}

	// Clean up tracked index files using DecRef() for proper reference counting
	if dc.mainIndex != nil {
		dc.mainIndex.DecRef()
		dc.mainIndex = nil
	}

	if dc.cacheIndex != nil {
		dc.cacheIndex.DecRef()
		dc.cacheIndex = nil
	}

	if dc.currentScan != nil {
		dc.currentScan.DecRef()
		dc.currentScan = nil
	}

	// Clean up all scan indices
	for _, scanIndex := range dc.scanIndices {
		if scanIndex != nil {
			scanIndex.DecRef()
		}
	}
	dc.scanIndices = nil

	return nil
}

func (dc *DirectoryCache) createEmptyIndex() error {
	totalSize := HeaderSize

	file, err := os.Create(dc.IndexFile)
	if err != nil {
		return fmt.Errorf("failed to create index file %s: %w", dc.IndexFile, err)
	}
	defer func() { _ = file.Close() }()

	if err := file.Truncate(int64(totalSize)); err != nil {
		return fmt.Errorf("failed to truncate file: %w", err)
	}

	data, err := unix.Mmap(int(file.Fd()), 0, totalSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return fmt.Errorf("failed to mmap file: %w", err)
	}
	defer func() { _ = unix.Munmap(data) }()

	// Zero out the entire memory region first
	for i := range data {
		data[i] = 0
	}

	// Write header directly to mmap'd memory (zero-copy)
	header := (*indexHeader)(unsafe.Pointer(&data[0]))
	header.SetHeader(dc.signature, dc.version, 0, 0, dc.GetCurrentHashType()) // No flags for empty index

	// Calculate and store checksum (no entries for empty index)
	dc.calculateAndStoreHeaderChecksum(header, nil, 0)

	if err := unix.Msync(data, unix.MS_SYNC); err != nil {
		return fmt.Errorf("failed to sync mmap: %w", err)
	}

	return nil
}

// appendEntryToNamedIndex is a generic function that appends a binaryEntry to any named index file
// This supports both scan indices and fix indices with proper mmap management
func (dc *DirectoryCache) appendEntryToNamedIndex(indexFileName string, indexInfo **mmapIndexFile, relPath string, hash []byte, hashType uint16, info os.FileInfo, stat *syscall.Stat_t, isDeleted bool) (*binaryEntry, error) {
	// Calculate entry size requirements
	entrySize := int(unsafe.Sizeof(binaryEntry{})) + len(relPath) + 1 // +1 for null terminator
	padding := (8 - (entrySize % 8)) % 8
	entrySize += padding

	// Ensure index is initialized
	if *indexInfo == nil {
		return nil, fmt.Errorf("index not initialized for file %s", indexFileName)
	}

	// Check if we need to expand the file
	requiredSize := (*indexInfo).Offset + entrySize
	newSize := (*indexInfo).Size
	for newSize < requiredSize {
		newSize = newSize * 2
		if newSize > 1<<30 { // Cap at 1GB
			newSize = requiredSize + (1 << 20) // Add 1MB at a time
		}
	}

	// Expand file and mmap if necessary
	if newSize > (*indexInfo).Size {
		// Lock for mremap operation (write lock)
		(*indexInfo).mutex.Lock()

		// Expand the file using existing file descriptor
		if err := (*indexInfo).File.Truncate(int64(newSize)); err != nil {
			(*indexInfo).mutex.Unlock()
			return nil, fmt.Errorf("failed to expand index file: %w", err)
		}

		// Expand the mmap using mremap
		newMmap, err := unix.Mremap((*indexInfo).Data, newSize, unix.MREMAP_MAYMOVE)
		if err != nil {
			(*indexInfo).mutex.Unlock()
			return nil, fmt.Errorf("failed to mremap index file: %w", err)
		}

		// Update stored mmap info
		(*indexInfo).Data = newMmap
		(*indexInfo).Size = newSize

		(*indexInfo).mutex.Unlock()
	}

	// Get header and update entry count
	header := (*indexHeader)(unsafe.Pointer(&(*indexInfo).Data[0]))
	entryOffset := (*indexInfo).Offset // Write at current offset
	header.EntryCount++

	// Write the new entry
	entryData := (*indexInfo).Data[entryOffset:]
	dc.writeBinaryEntryToMmap(entryData, relPath, hash, hashType, info, stat, isDeleted)

	// Get pointer to the created entry
	entry := (*binaryEntry)(unsafe.Pointer(&entryData[0]))

	// Update offset for next entry
	(*indexInfo).Offset += entrySize

	return entry, nil
}

// AppendEntryToScanIndex is an exported wrapper for appending entries to scan index files
func (dc *DirectoryCache) AppendEntryToScanIndex(scanFileName string, relPath string, hash []byte, hashType uint16, info os.FileInfo, stat *syscall.Stat_t, isDeleted bool) (*binaryEntry, error) {
	if dc.currentScan == nil || dc.currentScan.FilePath != scanFileName {
		return nil, fmt.Errorf("scan index not initialized for file %s", scanFileName)
	}
	return dc.appendEntryToNamedIndex(scanFileName, &dc.currentScan, relPath, hash, hashType, info, stat, isDeleted)
}

// AppendEntryToFixIndex is an exported wrapper for appending entries to fix index files
func (dc *DirectoryCache) AppendEntryToFixIndex(fixFileName string, fixIndex **mmapIndexFile, relPath string, hash []byte, hashType uint16, info os.FileInfo, stat *syscall.Stat_t, isDeleted bool) (*binaryEntry, error) {
	return dc.appendEntryToNamedIndex(fixFileName, fixIndex, relPath, hash, hashType, info, stat, isDeleted)
}

// initialiseScanIndex creates and initialises a new scan index file with mmap
func (dc *DirectoryCache) initialiseScanIndex(scanFileName string) error {
	// Create the scan index file (use 0666, let umask control final permissions)
	file, err := os.OpenFile(scanFileName, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		return fmt.Errorf("failed to create scan file %s: %w", scanFileName, err)
	}
	// Keep file open throughout scan process

	// Initial size is just the header
	initialSize := HeaderSize
	if err := file.Truncate(int64(initialSize)); err != nil {
		_ = file.Close()
		return fmt.Errorf("failed to truncate scan file: %w", err)
	}

	// Create initial mmap
	data, err := unix.Mmap(int(file.Fd()), 0, initialSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("failed to mmap scan file: %w", err)
	}

	// Initialise header for writable index (automatically clears Clean flag)
	header := (*indexHeader)(unsafe.Pointer(&data[0]))
	header.SetHeaderForWritableIndex(dc.signature, dc.version, 0, 0, dc.GetCurrentHashType()) // Start with 0 entries

	// Create scan index wrapper (keep file open)
	dc.currentScan = &mmapIndexFile{
		File:       file,
		Data:       data,
		Size:       initialSize,
		Offset:     HeaderSize, // Start writing entries after header
		Type:       "scan",
		FilePath:   scanFileName,
		headerSize: HeaderSize,
		refCount:   1, // Start with ref count = 1 to prevent premature cleanup
	}

	// Register the scan index for tracking
	dc.registerIndex("scan", dc.currentScan)

	return nil
}

// createEmptyScanIndex creates an empty scan index file for recovery operations
// Unlike initialiseScanIndex, this creates a standalone file without setting dc.currentScan
func (dc *DirectoryCache) createEmptyScanIndex(scanFileName string) error {
	// Create the scan index file (use 0666, let umask control final permissions)
	file, err := os.OpenFile(scanFileName, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		return fmt.Errorf("failed to create scan file %s: %w", scanFileName, err)
	}
	defer func() { _ = file.Close() }() // Close immediately after setup since recovery doesn't need persistent handle

	// Initial size is just the header
	initialSize := HeaderSize
	if err := file.Truncate(int64(initialSize)); err != nil {
		return fmt.Errorf("failed to truncate scan file: %w", err)
	}

	// Create initial mmap for header initialization
	data, err := unix.Mmap(int(file.Fd()), 0, initialSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return fmt.Errorf("failed to mmap scan file: %w", err)
	}
	defer func() { _ = unix.Munmap(data) }()

	// Initialise header for writable index (automatically clears Clean flag)
	header := (*indexHeader)(unsafe.Pointer(&data[0]))
	header.SetHeaderForWritableIndex(dc.signature, dc.version, 0, 0, dc.GetCurrentHashType()) // Start with 0 entries

	// Sync to disk
	if err := unix.Msync(data, unix.MS_SYNC); err != nil {
		return fmt.Errorf("failed to sync mmap: %w", err)
	}

	return nil
}

// InitializeFixIndex creates and initializes a new fix index file with mmap
// Similar to scan indices but for dcfhfix operations
func (dc *DirectoryCache) InitializeFixIndex(fixFileName string) (*mmapIndexFile, error) {
	// Create the fix index file
	file, err := os.OpenFile(fixFileName, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0666)
	if err != nil {
		return nil, fmt.Errorf("failed to create fix file %s: %w", fixFileName, err)
	}
	// Keep file open throughout fix process

	// Initial size is just the header
	initialSize := HeaderSize
	if err := file.Truncate(int64(initialSize)); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("failed to truncate fix file: %w", err)
	}

	// Create initial mmap
	data, err := unix.Mmap(int(file.Fd()), 0, initialSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("failed to mmap fix file: %w", err)
	}

	// Initialize header for writable index (automatically clears Clean flag)
	header := (*indexHeader)(unsafe.Pointer(&data[0]))
	header.SetHeaderForWritableIndex(dc.signature, dc.version, 0, 0, dc.GetCurrentHashType()) // Start with 0 entries

	// Create mmapIndexFile for fix index
	fixInfo := &mmapIndexFile{
		FilePath:   fixFileName,
		File:       file,
		Data:       data,
		Size:       initialSize,
		Offset:     HeaderSize, // Start writing after header
		Type:       "fix",
		headerSize: HeaderSize,
	}

	return fixInfo, nil
}

// CleanupFixIndex cleans up fix index resources after completion
func (dc *DirectoryCache) CleanupFixIndex(fixInfo *mmapIndexFile) error {
	if fixInfo == nil {
		return fmt.Errorf("can't clean up nil fix index")
	}

	// Munmap
	if fixInfo.Data != nil {
		if err := unix.Munmap(fixInfo.Data); err != nil {
			return fmt.Errorf("failed to munmap fix index: %w", err)
		}
	}

	// Close file
	if fixInfo.File != nil {
		if err := fixInfo.File.Close(); err != nil {
			return fmt.Errorf("failed to close fix index file: %w", err)
		}
	}

	// Delete the file
	if err := os.Remove(fixInfo.FilePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove fix index file: %w", err)
	}

	return nil
}

// CleanupCurrentScanFile cleans up scan index resources after temp index is written
// This should be called after temp index writing but before rename operations
//
// CRITICAL ORDER to prevent use-after-free:
// 1. Caller must "forget" scan skiplist (allow GC) - done by caller
// 2. Munmap the scan index file - done here
// 3. Delete the scan index file - done here
func (dc *DirectoryCache) cleanupCurrentScanFile() error {
	if dc.currentScan == nil {
		return fmt.Errorf("can't clean up missing scan index file: %w", os.ErrNotExist)
	}

	// Get file path for deletion
	filePath := dc.currentScan.FilePath

	// Unregister from tracking before cleanup
	dc.unregisterIndex("scan", dc.currentScan)

	// Step 2 - Decrement reference count, cleanup happens automatically when count reaches 0
	dc.currentScan.DecRef()

	// Step 3 - Remove the scan index file
	err := os.Remove(filePath)
	dc.currentScan = nil

	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove scan file: %w", err)
	}

	return nil
}

// getSystemIOVMax returns the system's IOV_MAX limit using sysconf(_SC_IOV_MAX)
// Falls back to conservative default if sysconf fails
func getSystemIOVMax() (int, error) {
	// _SC_IOV_MAX constant for sysconf() - platform specific
	const SC_IOV_MAX = 60       // Linux value, may vary on other platforms
	const fallbackIOVMax = 1024 // Conservative default per golang/go#58623

	// Call sysconf directly using unix.Syscall (syscall 99 on Linux)
	r1, _, errno := unix.Syscall(99, uintptr(SC_IOV_MAX), 0, 0)
	if errno != 0 {
		// Fall back to conservative default if sysconf fails
		return fallbackIOVMax, nil
	}

	iovMax := int(r1)

	// Validate the result is reasonable, fall back if not
	if iovMax <= 0 || iovMax > 1<<20 { // Sanity check: between 1 and 1M
		return fallbackIOVMax, nil
	}

	return iovMax, nil
}

// scanForTempIndices scans the .dcfh directory for temporary index files
func (dc *DirectoryCache) scanForTempIndices() ([]string, error) {
	var tempFiles []string

	entries, err := os.ReadDir(dc.DcfhDir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()

		// Look for temporary index files with patterns:
		// - scan-{pid}-{tid}.idx (scan indices)
		// - tmp-{pid}-{tid}.idx (temp indices)
		if strings.HasPrefix(name, "scan-") && strings.HasSuffix(name, ".idx") ||
			strings.HasPrefix(name, "tmp-") && strings.HasSuffix(name, ".idx") {
			tempFiles = append(tempFiles, name)
		}
	}

	return tempFiles, nil
}

// validateEntryChaining validates the consistency of a binaryEntry's internal structure
// and its position within the mmap'd data
func (dc *DirectoryCache) validateEntryChaining(entry *binaryEntry, offset int, entryData []byte, entryIndex int) error {
	// Basic size validation
	if entry.Size == 0 {
		return fmt.Errorf("entry has zero size at offset %d (entry index %d)", offset, entryIndex)
	}

	minSize := uint32(unsafe.Sizeof(*entry))
	if entry.Size < minSize {
		return fmt.Errorf("entry size %d too small (minimum %d) at offset %d (entry index %d)",
			entry.Size, minSize, offset, entryIndex)
	}

	maxReasonableSize := uint32(4096) // Reasonable maximum for path + padding
	if entry.Size > maxReasonableSize {
		return fmt.Errorf("entry size %d unreasonably large (maximum %d) at offset %d (entry index %d)",
			entry.Size, maxReasonableSize, offset, entryIndex)
	}

	// Validate that the entry doesn't extend beyond available data
	if offset+int(entry.Size) > len(entryData) {
		return fmt.Errorf("entry size %d at offset %d would extend beyond data bounds (available: %d) (entry index %d)",
			entry.Size, offset, len(entryData)-offset, entryIndex)
	}

	// Validate 8-byte alignment
	if entry.Size%8 != 0 {
		return fmt.Errorf("entry size %d not 8-byte aligned at offset %d (entry index %d)", entry.Size, offset, entryIndex)
	}

	// Validate that the entry pointer is 8-byte aligned
	entryPtr := uintptr(unsafe.Pointer(entry))
	if entryPtr%8 != 0 {
		return fmt.Errorf("entry pointer 0x%x not 8-byte aligned at offset %d", entryPtr, offset)
	}

	// If memory layout debugging is enabled, log layout information
	if IsDebugEnabled("memorylayout") {
		pathFieldOffset := uintptr(unsafe.Pointer(&entry.Path[0])) - entryPtr
		fmt.Fprintf(os.Stderr, "Entry %d: size=%d, ptr=0x%x, path_offset=%d\n",
			offset/int(minSize), entry.Size, entryPtr, pathFieldOffset)
	}

	return nil
}
