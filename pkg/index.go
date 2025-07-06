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
	"syscall"
	"unsafe"

	"github.com/google/vectorio"
	"golang.org/x/sys/unix"
)

// indexHeader represents the file header in host byte order (cast directly to mmap'd memory)
type indexHeader struct {
	Signature    [4]byte  // "dcfh" signature
	ByteOrder    uint64   // Byte order detection magic (0x0102030405060708) - MUST be checked before other fields
	Version      uint32   // Index version (host order)
	EntryCount   uint32   // Number of entries (host order)
	Flags        uint16   // Index flags (host order) - matches binaryEntry.EntryFlags size
	ChecksumType uint16   // Checksum algorithm type (matches binaryEntry.HashType size)
	Checksum     [64]byte // Checksum of header+entries (up to 512-bit support)
}

// mmapIndex represents a memory-mapped index file
type mmapIndex struct {
	data    []byte
	file    *os.File
	entries []byte // Raw entry data after header
	size    int    // Current mapped size
	offset  int    // Current write offset
}

// mmapIndexFile represents a wrapper for index file lifecycle management
type mmapIndexFile struct {
	File     *os.File // File descriptor (nil for read-only main/cache indices)
	Data     []byte   // Memory-mapped data
	Size     int      // Current size of the mapping
	Offset   int      // Current write offset for scan indices
	Type     string   // Index type: "main", "cache", "scan"
	FilePath string   // File path for debugging/cleanup
	mutex    sync.RWMutex // Protects Data/Size during mremap operations
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

// ValidateVersion checks if the version is supported
func (ih *indexHeader) ValidateVersion(expected uint32) error {
	if ih.Version != expected {
		return fmt.Errorf("unsupported version: got %d, expected %d", ih.Version, expected)
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
	defer file.Close()
	
	// Get file size
	stat, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}
	
	if stat.Size() < HeaderSize {
		return nil, fmt.Errorf("file too small: %d bytes", stat.Size())
	}
	
	// Memory map just the header for reading
	data, err := unix.Mmap(int(file.Fd()), 0, HeaderSize, unix.PROT_READ, unix.MAP_PRIVATE)
	if err != nil {
		return nil, fmt.Errorf("failed to mmap file header: %w", err)
	}
	defer unix.Munmap(data)
	
	// Get direct pointer to header in mmap'd memory (zero-copy)
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
	// Calculate expected checksum
	hasher := sha1.New()
	
	// Hash header up to checksum field
	headerBytes := (*[HeaderSize]byte)(unsafe.Pointer(header))
	checksumOffset := unsafe.Offsetof(header.Checksum)
	hasher.Write(headerBytes[:checksumOffset])
	
	// If file has entry data, hash it too
	entryDataSize := fileSize - HeaderSize - ChecksumSize
	if entryDataSize > 0 {
		// Read entry data
		entryData := make([]byte, entryDataSize)
		if _, err := file.ReadAt(entryData, HeaderSize); err != nil {
			return fmt.Errorf("failed to read entry data for checksum validation: %w", err)
		}
		hasher.Write(entryData)
	}
	
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
	
	// Hash header up to checksum field
	headerBytes := (*[HeaderSize]byte)(unsafe.Pointer(header))
	checksumOffset := unsafe.Offsetof(header.Checksum)
	hasher.Write(headerBytes[:checksumOffset])
	
	// Hash entry data if any
	if entrySize > 0 {
		hasher.Write(entryData[:entrySize])
	}
	
	// Store checksum in header
	checksumBytes := hasher.Sum(nil)
	copy(header.Checksum[:], checksumBytes)
}

// calculateAndStoreHeaderChecksumFromIoVecs calculates checksum from IoVecs and stores it in header
func (dc *DirectoryCache) calculateAndStoreHeaderChecksumFromIoVecs(header *indexHeader, headerIovec syscall.Iovec, entryIovecs []syscall.Iovec) {
	hasher := dc.hasher
	hasher.Reset()
	
	// Hash header up to (but not including) checksum field
	headerBytes := unsafe.Slice((*byte)(headerIovec.Base), int(headerIovec.Len))
	checksumOffset := unsafe.Offsetof(header.Checksum)
	hasher.Write(headerBytes[:checksumOffset])
	
	// Hash entries
	for _, iovec := range entryIovecs {
		hasher.Write(unsafe.Slice((*byte)(iovec.Base), int(iovec.Len)))
	}
	
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
	for i := 0; i < padding; i++ {
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
		file.Close()
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	if stat.Size() < HeaderSize {
		file.Close()
		return nil, fmt.Errorf("file too small: %d bytes", stat.Size())
	}

	// Memory map the file for reading
	data, err := unix.Mmap(int(file.Fd()), 0, int(stat.Size()), unix.PROT_READ, unix.MAP_PRIVATE)
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to mmap file: %w", err)
	}

	// Create mmapIndexFile wrapper  
	indexFile := &mmapIndexFile{
		File:     file,
		Data:     data,
		Size:     int(stat.Size()),
		Type:     "loaded", // Generic type for loaded indices
		FilePath: filePath,
	}

	// Get direct pointer to header in mmap'd memory (zero-copy)
	header := (*indexHeader)(unsafe.Pointer(&data[0]))

	// Verify header using helper methods in logical order
	if err := header.ValidateSignature(dc.signature); err != nil {
		return nil, err
	}
	if err := header.ValidateByteOrder(); err != nil {
		return nil, err
	}
	if err := header.ValidateVersion(dc.version); err != nil {
		return nil, err
	}

	// Check Clean flag to determine if we should trust the header checksum
	isClean := (header.Flags & IndexFlagClean) != 0
	
	if !isClean {
		// File wasn't closed cleanly - header checksum is likely incorrect
		// Skip checksum validation for recovery purposes
		VerboseLog(2, "Skipping header checksum validation for unclean file: %s", filePath)
	} else {
		// File was closed cleanly - verify checksum from header
		if err := dc.verifyHeaderChecksum(data, header); err != nil {
			return nil, fmt.Errorf("checksum verification failed: %w", err)
		}
	}

	// Parse entries with callback processing
	var refs []binaryEntryRef
	offset := 0
	entryData := data[HeaderSize:]

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
				func() string { if entry.IsDeleted() { return " (DELETED)" } else { return "" } }())
			VerboseLog(3, "    HashType: %d (%s)", entry.HashType, HashTypeName(entry.HashType))
			VerboseLog(3, "    Hash: %s", entry.HashString())
			VerboseLog(3, "    Path: %s", entryPath)
		}
		
		return true, nil
	}
}

// SearchEntryProcessor returns a processor that searches for matching entries
type SearchOptions struct {
	Pattern      string  // Filename pattern (glob)
	PathPrefix   string  // Path prefix filter
	HashPrefix   string  // Hash prefix filter
	ExactSize    *uint64 // Exact file size filter
	ShowDeleted  bool    // Show only deleted entries
	SearchCount  *int    // Pointer to counter for matches
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

// LoadIndexFromFile loads and maps the specified index file, returns array of entry pointers
// loadIndexFromFile loads an index file and returns binaryEntryRef instances (backward compatibility wrapper)
func (dc *DirectoryCache) loadIndexFromFile(filePath string) ([]binaryEntryRef, error) {
	// Use default processor for normal loading operations (no verbose output)
	return dc.loadIndexFromFileWithProcessor(filePath, DefaultEntryProcessor())
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
	hasher.Reset()
	
	// Hash header fields before checksum field
	headerBytes := (*[HeaderSize]byte)(unsafe.Pointer(header))
	checksumOffset := unsafe.Offsetof(header.Checksum)
	hasher.Write(headerBytes[:checksumOffset])
	
	// Hash entry data (everything after header)
	entryData := data[HeaderSize:]
	hasher.Write(entryData)
	
	calculatedChecksum := hasher.Sum(nil)
	
	// Compare checksums
	for i := 0; i < expectedSize; i++ {
		if storedChecksum[i] != calculatedChecksum[i] {
			return fmt.Errorf("checksum mismatch at byte %d", i)
		}
	}
	return nil
}

// verifyChecksumMmap verifies the SHA-1 checksum for mmap'd data (legacy function)
func (dc *DirectoryCache) verifyChecksumMmap(data []byte, contentSize int) error {
	if len(data) < contentSize+ChecksumSize {
		return fmt.Errorf("insufficient data for checksum")
	}

	storedChecksum := data[contentSize : contentSize+ChecksumSize]
	calculatedChecksum := dc.calculateChecksum(data[:contentSize])

	for i := 0; i < ChecksumSize; i++ {
		if storedChecksum[i] != calculatedChecksum[i] {
			return fmt.Errorf("checksum mismatch at byte %d", i)
		}
	}
	return nil
}

// calculateChecksum calculates SHA-1 checksum of data
func (dc *DirectoryCache) calculateChecksum(data []byte) []byte {
	dc.hasher.Reset()
	dc.hasher.Write(data)
	return dc.hasher.Sum(nil)
}

// Close cleans up mmap'd resources and checks for orphaned index files
func (dc *DirectoryCache) Close() error {
	// Check for orphaned index files first (ignore errors during check)
	dc.checkForOrphanedIndexFiles()
	
	if dc.mmapIndex != nil {
		if err := unix.Munmap(dc.mmapIndex.data); err != nil {
			return fmt.Errorf("failed to unmap: %w", err)
		}
		if err := dc.mmapIndex.file.Close(); err != nil {
			return fmt.Errorf("failed to close file: %w", err)
		}
		dc.mmapIndex = nil
	}
	return nil
}


func (dc *DirectoryCache) createEmptyIndex() error {
	totalSize := HeaderSize

	file, err := os.Create(dc.IndexFile)
	if err != nil {
		return fmt.Errorf("failed to create index file %s: %w", dc.IndexFile, err)
	}
	defer file.Close()

	if err := file.Truncate(int64(totalSize)); err != nil {
		return fmt.Errorf("failed to truncate file: %w", err)
	}

	data, err := unix.Mmap(int(file.Fd()), 0, totalSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return fmt.Errorf("failed to mmap file: %w", err)
	}
	defer unix.Munmap(data)

	// Zero out the entire memory region first
	for i := range data {
		data[i] = 0
	}

	// Write header directly to mmap'd memory (zero-copy)
	header := (*indexHeader)(unsafe.Pointer(&data[0]))
	header.SetHeader(dc.signature, dc.version, 0, 0, HashTypeSHA1) // No flags for empty index

	// Calculate and store checksum (no entries for empty index)
	dc.calculateAndStoreHeaderChecksum(header, nil, 0)

	if err := unix.Msync(data, unix.MS_SYNC); err != nil {
		return fmt.Errorf("failed to sync mmap: %w", err)
	}

	return nil
}

// appendEntryToScanIndex appends a binaryEntry to the existing scan index mmap
// Uses single mmap with mremap for efficient memory usage
func (dc *DirectoryCache) appendEntryToScanIndex(scanFileName string, scannedPath *scannedPath) (*binaryEntry, error) {
	// Verify we have an active scan index
	if dc.currentScan == nil || dc.currentScan.FilePath != scanFileName {
		return nil, fmt.Errorf("scan index not initialised for file %s", scanFileName)
	}

	// Calculate entry size
	baseSize := int(unsafe.Sizeof(binaryEntry{}))
	totalSize := baseSize + len(scannedPath.RelPath) + 1 // +1 for null terminator
	padding := (8 - (totalSize % 8)) % 8
	entrySize := totalSize + padding

	// Calculate required new size
	newSize := dc.currentScan.Offset + entrySize

	// Expand file and mmap if necessary
	if newSize > dc.currentScan.Size {
		// Lock for mremap operation (write lock)
		dc.currentScan.mutex.Lock()
		
		// Expand the file using existing file descriptor
		if err := dc.currentScan.File.Truncate(int64(newSize)); err != nil {
			dc.currentScan.mutex.Unlock()
			return nil, fmt.Errorf("failed to expand scan file: %w", err)
		}

		// Expand the mmap using mremap
		newMmap, err := unix.Mremap(dc.currentScan.Data, newSize, unix.MREMAP_MAYMOVE)
		if err != nil {
			dc.currentScan.mutex.Unlock()
			return nil, fmt.Errorf("failed to mremap scan file: %w", err)
		}

		// Update stored mmap info
		dc.currentScan.Data = newMmap
		dc.currentScan.Size = newSize
		
		dc.currentScan.mutex.Unlock()
	}

	// Get header and update entry count
	header := (*indexHeader)(unsafe.Pointer(&dc.currentScan.Data[0]))
	entryOffset := dc.currentScan.Offset  // Write at current offset
	header.EntryCount++

	// Write the new entry
	entryData := dc.currentScan.Data[entryOffset:]
	currentHashType := dc.GetCurrentHashType()
	currentHashSize := GetHashSize(currentHashType)
	dc.writeBinaryEntryToMmap(entryData, scannedPath.RelPath, make([]byte, currentHashSize), currentHashType, scannedPath.Info, scannedPath.StatInfo, false)

	// Get pointer to the created entry
	entry := (*binaryEntry)(unsafe.Pointer(&entryData[0]))


	// Update offset for next entry
	dc.currentScan.Offset += entrySize

	// Note: We don't update checksum here since it's only needed once at the end
	// when all entries and hashes are complete

	return entry, nil
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
	entryOffset := (*indexInfo).Offset  // Write at current offset
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
		file.Close()
		return fmt.Errorf("failed to truncate scan file: %w", err)
	}

	// Create initial mmap
	data, err := unix.Mmap(int(file.Fd()), 0, initialSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		file.Close()
		return fmt.Errorf("failed to mmap scan file: %w", err)
	}

	// Initialise header for writable index (automatically clears Clean flag)
	header := (*indexHeader)(unsafe.Pointer(&data[0]))
	header.SetHeaderForWritableIndex(dc.signature, dc.version, 0, 0, HashTypeSHA1) // Start with 0 entries

	// Create scan index wrapper (keep file open)
	dc.currentScan = &mmapIndexFile{
		File:     file,
		Data:     data,
		Size:     initialSize,
		Offset:   HeaderSize, // Start writing entries after header
		Type:     "scan",
		FilePath: scanFileName,
	}

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
	defer file.Close() // Close immediately after setup since recovery doesn't need persistent handle

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
	defer unix.Munmap(data)

	// Initialise header for writable index (automatically clears Clean flag)
	header := (*indexHeader)(unsafe.Pointer(&data[0]))
	header.SetHeaderForWritableIndex(dc.signature, dc.version, 0, 0, HashTypeSHA1) // Start with 0 entries

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
		file.Close()
		return nil, fmt.Errorf("failed to truncate fix file: %w", err)
	}

	// Create initial mmap
	data, err := unix.Mmap(int(file.Fd()), 0, initialSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to mmap fix file: %w", err)
	}

	// Initialize header for writable index (automatically clears Clean flag)
	header := (*indexHeader)(unsafe.Pointer(&data[0]))
	header.SetHeaderForWritableIndex(dc.signature, dc.version, 0, 0, HashTypeSHA1) // Start with 0 entries

	// Create mmapIndexFile for fix index
	fixInfo := &mmapIndexFile{
		FilePath: fixFileName,
		File:     file,
		Data:     data,
		Size:     initialSize,
		Offset:   HeaderSize, // Start writing after header
		Type:     "fix",
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
	
	// Step 2 - Cleanup mmap and file descriptor
	if err := dc.currentScan.Cleanup(); err != nil {
		return fmt.Errorf("failed to cleanup scan index: %w", err)
	}
	
	// Step 3 - Remove the scan index file
	err := os.Remove(filePath)
	dc.currentScan = nil
	
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove scan file: %w", err)
	}
	
	return nil
}

// WriteSkiplistWithVectorIO writes a skiplist to an index file using vectorio for efficient bulk writes
func (dc *DirectoryCache) writeSkiplistWithVectorIO(skiplist *skiplistWrapper, outputPath string, context string) error {
	return dc.writeSkiplistWithVectorIOFiltered(skiplist, outputPath, context, false)
}

// writeMainIndexWithVectorIO writes a main index file excluding deleted entries using vectorio
func (dc *DirectoryCache) writeMainIndexWithVectorIO(skiplist *skiplistWrapper, outputPath string, context string) error {
	return dc.writeSkiplistWithVectorIOFiltered(skiplist, outputPath, context, true)
}

// writeSkiplistWithVectorIOFiltered writes a skiplist to temp index using pure vectorio (no mmap)
func (dc *DirectoryCache) writeSkiplistWithVectorIOFiltered(skiplist *skiplistWrapper, outputPath string, context string, excludeDeleted bool) error {
	// Generate IoVec slices for the specified context
	var entryIovecs []syscall.Iovec
	
	if excludeDeleted {
		// Use callback to filter out deleted entries for main index
		entryIovecs = skiplist.CallbackToIovecSlice(func(entry *binaryEntry, entryContext string) bool {
			// Include entry if it matches context (or no context filter), is not deleted, and has a valid hash
			contextMatch := (context == "" || entryContext == context)
			return contextMatch && !entry.IsDeleted() && !entry.IsHashEmpty()
		})
	} else {
		// Include all entries for cache index (including deleted ones) but exclude entries with empty hashes
		entryIovecs = skiplist.CallbackToIovecSlice(func(entry *binaryEntry, entryContext string) bool {
			// For cache index, include if has valid hash and either no context filter or matches context
			if entry.IsHashEmpty() {
				return false
			}
			if context == "" {
				return true
			} else {
				// For cache index, exclude MainContext entries (keep CacheContext + ScanContext)
				return entryContext != MainContext
			}
		})
	}

	// Calculate entry data size
	totalEntrySize := 0
	entryCount := len(entryIovecs)
	for _, iovec := range entryIovecs {
		totalEntrySize += int(iovec.Len)
	}
	

	// Create output file (O_CREAT|O_WRONLY)
	file, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("failed to create temp index file %s: %w", outputPath, err)
	}
	defer file.Close()

	// Create header in memory for temp index (writable, so Clear flag cleared)
	header := indexHeader{}
	header.SetHeaderForWritableIndex(dc.signature, dc.version, uint32(entryCount), 0, HashTypeSHA1)

	// Create header IoVec
	headerIovec := syscall.Iovec{
		Base: (*byte)(unsafe.Pointer(&header)),
		Len:  uint64(HeaderSize),
	}

	// Write header using vectorio
	if nw, err := vectorio.WritevRaw(uintptr(file.Fd()), []syscall.Iovec{headerIovec}); err != nil {
		return fmt.Errorf("failed to write header with vectorio: %w", err)
	} else if nw != HeaderSize {
		return fmt.Errorf("header write incomplete: wrote %d bytes, expected %d", nw, HeaderSize)
	}

	// Write entries using vectorio (if any) - chunk to respect IOV_MAX limit
	if len(entryIovecs) > 0 {
		maxIovecs, err := getSystemIOVMax()
		if err != nil {
			return fmt.Errorf("failed to get system IOV_MAX: %w", err)
		}
		totalWritten := 0
		
		for offset := 0; offset < len(entryIovecs); offset += maxIovecs {
			end := offset + maxIovecs
			if end > len(entryIovecs) {
				end = len(entryIovecs)
			}
			
			// Use slice without copying to avoid allocation
			chunk := entryIovecs[offset:end]
			
			if nw, err := vectorio.WritevRaw(uintptr(file.Fd()), chunk); err != nil {
				return fmt.Errorf("failed to write entries chunk with vectorio: %w", err)
			} else {
				totalWritten += nw
			}
		}
		
		if totalWritten != totalEntrySize {
			return fmt.Errorf("entries write incomplete: wrote %d bytes, expected %d", totalWritten, totalEntrySize)
		}
	}

	// Mark header as clean first (before calculating checksum)
	header.setClean()
	
	// Calculate checksum from IoVecs and store in header
	dc.calculateAndStoreHeaderChecksumFromIoVecs(&header, headerIovec, entryIovecs)
	
	// Rewrite the complete header with clean flag and checksum
	if _, err := file.Seek(0, 0); err != nil {
		return fmt.Errorf("failed to seek to beginning for final header: %w", err)
	}

	if nw, err := vectorio.WritevRaw(uintptr(file.Fd()), []syscall.Iovec{headerIovec}); err != nil {
		return fmt.Errorf("failed to write final header with vectorio: %w", err)
	} else if nw != HeaderSize {
		return fmt.Errorf("final header write incomplete: wrote %d bytes, expected %d", nw, HeaderSize)
	}

	// Sync to disk
	if err := file.Sync(); err != nil {
		return fmt.Errorf("failed to sync temp index: %w", err)
	}

	return nil
}


// MergeScanSkiplistsWithVectorIO merges scan skiplists and writes final index using vectorio
func (dc *DirectoryCache) mergeScanSkiplistsWithVectorIO(baseSkiplist *skiplistWrapper, scanSkiplist *skiplistWrapper, outputPath string) error {
	// Create merged skiplist
	mergedSkiplist := baseSkiplist.Copy()
	
	// Merge scan results into base skiplist
	if err := mergedSkiplist.Merge(scanSkiplist, MergeTheirs); err != nil {
		return fmt.Errorf("failed to merge skiplists: %w", err)
	}

	// Write merged result using vectorio
	return dc.writeSkiplistWithVectorIO(mergedSkiplist, outputPath, "")
}

// getSystemIOVMax returns the system's IOV_MAX limit using sysconf(_SC_IOV_MAX)
// Falls back to conservative default if sysconf fails
func getSystemIOVMax() (int, error) {
	// _SC_IOV_MAX constant for sysconf() - platform specific
	const SC_IOV_MAX = 60 // Linux value, may vary on other platforms
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
	
	// Get the .dcfh directory from the IndexFile path
	dcfhDir := filepath.Dir(dc.IndexFile)
	
	// Read the .dcfh directory
	entries, err := os.ReadDir(dcfhDir)
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
		os.Stderr.WriteString(fmt.Sprintf("Entry %d: size=%d, ptr=0x%x, path_offset=%d\n", 
			offset/int(minSize), entry.Size, entryPtr, pathFieldOffset))
	}
	
	return nil
}
