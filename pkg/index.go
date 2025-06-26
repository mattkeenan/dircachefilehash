package dircachefilehash

import (
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"github.com/google/vectorio"
	"golang.org/x/sys/unix"
)

// IndexHeader represents the file header in host byte order (cast directly to mmap'd memory)
type IndexHeader struct {
	Signature    [4]byte  // "dcfh" signature
	ByteOrder    uint64   // Byte order detection magic (0x0102030405060708) - MUST be checked before other fields
	Version      uint32   // Index version (host order)
	EntryCount   uint32   // Number of entries (host order)
	Flags        uint16   // Index flags (host order) - matches binaryEntry.EntryFlags size
	ChecksumType uint16   // Checksum algorithm type (matches binaryEntry.HashType size)
	Checksum     [64]byte // Checksum of header+entries (up to 512-bit support)
}

// MmapIndex represents a memory-mapped index file
type MmapIndex struct {
	data    []byte
	file    *os.File
	entries []byte // Raw entry data after header
	size    int    // Current mapped size
	offset  int    // Current write offset
}


// Header returns a direct pointer to the header in mmap'd memory (zero-copy)
func (mi *MmapIndex) Header() *IndexHeader {
	return (*IndexHeader)(unsafe.Pointer(&mi.data[0]))
}

// ValidateSignature checks if the signature matches expected value
func (ih *IndexHeader) ValidateSignature(expected [4]byte) error {
	if ih.Signature != expected {
		return fmt.Errorf("invalid signature: got %q, expected %q",
			string(ih.Signature[:]), string(expected[:]))
	}
	return nil
}

// ValidateVersion checks if the version is supported
func (ih *IndexHeader) ValidateVersion(expected uint32) error {
	if ih.Version != expected {
		return fmt.Errorf("unsupported version: got %d, expected %d", ih.Version, expected)
	}
	return nil
}

// ValidateByteOrder checks if the byte order matches the host machine
func (ih *IndexHeader) ValidateByteOrder() error {
	if ih.ByteOrder != ByteOrderMagic {
		return fmt.Errorf("byte order mismatch: index file byte order 0x%016x does not match host byte order 0x%016x",
			ih.ByteOrder, ByteOrderMagic)
	}
	return nil
}

// SetHeader initializes the header fields in mmap'd memory
func (ih *IndexHeader) SetHeader(signature [4]byte, version uint32, entryCount uint32, flags uint16, checksumType uint16) {
	ih.Signature = signature
	ih.ByteOrder = ByteOrderMagic
	ih.Version = version
	ih.EntryCount = entryCount
	ih.Flags = flags
	ih.ChecksumType = checksumType
}

// SetHeaderForWritableIndex initializes the header for write operations (scan/temp indices)
// Automatically clears the Clean flag since we're opening for write
func (ih *IndexHeader) SetHeaderForWritableIndex(signature [4]byte, version uint32, entryCount uint32, baseFlags uint16, checksumType uint16) {
	// For writable indices, ensure Clean flag is cleared (not clean during write operations)
	flags := baseFlags &^ IndexFlagClean
	ih.SetHeader(signature, version, entryCount, flags, checksumType)
}

// calculateAndStoreHeaderChecksum calculates checksum and stores it in header
func (dc *DirectoryCache) calculateAndStoreHeaderChecksum(header *IndexHeader, entryData []byte, entrySize int) {
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
func (dc *DirectoryCache) calculateAndStoreHeaderChecksumFromIoVecs(header *IndexHeader, headerIovec syscall.Iovec, entryIovecs []syscall.Iovec) {
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
func (ih *IndexHeader) isClean() bool {
	return ih.Flags&IndexFlagClean != 0
}

// setClean marks this index file as clean/complete (final operation)
func (ih *IndexHeader) setClean() {
	ih.Flags |= IndexFlagClean
}

// clearClean marks this index file as unclean/incomplete
func (ih *IndexHeader) clearClean() {
	ih.Flags &^= IndexFlagClean
}

// writeBinaryEntryToMmap writes a binaryEntry directly to mmap'd memory (PRIVATE - only for scan index)
func (dc *DirectoryCache) writeBinaryEntryToMmap(data []byte, relPath string, hash []byte, hashType uint16, info os.FileInfo, stat *syscall.Stat_t, isDeleted bool) int {
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

	return entrySize
}


// LoadIndexFromFile loads and maps the specified index file, returns array of entry pointers
func (dc *DirectoryCache) LoadIndexFromFile(filePath string) ([]*binaryEntry, error) {
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

	// Create MmapIndex with direct header access
	mmapIndex := &MmapIndex{
		data:    data,
		file:    file,
		entries: data[HeaderSize:],
		size:    int(stat.Size()),
		offset:  HeaderSize,
	}
	dc.mmapIndex = mmapIndex

	// Get direct pointer to header in mmap'd memory (zero-copy)
	header := mmapIndex.Header()

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

	// Verify checksum from header
	if err := dc.verifyHeaderChecksum(data, header); err != nil {
		return nil, fmt.Errorf("checksum verification failed: %w", err)
	}

	// Parse entries - create direct pointers to mmap'd binaryEntry structs
	var entries []*binaryEntry
	offset := 0
	entryData := mmapIndex.entries

	for i := uint32(0); i < header.EntryCount; i++ {
		if offset >= len(entryData) {
			return nil, fmt.Errorf("unexpected end of data at entry %d", i)
		}

		// Get direct pointer to binaryEntry in mmap'd memory
		entry := (*binaryEntry)(unsafe.Pointer(&entryData[offset]))
		
		// Validate binaryEntry chaining consistency
		if err := dc.validateEntryChaining(entry, offset, entryData); err != nil {
			return nil, fmt.Errorf("entry %d validation failed: %w", i, err)
		}
		
		// Perform extra validation if debug flag is enabled
		if IsExtraValidationEnabled() {
			if err := entry.ValidateEntry(); err != nil {
				return nil, fmt.Errorf("entry %d extra validation failed: %w", i, err)
			}
		}
		
		entries = append(entries, entry)

		// Move to next entry using Size field
		nextOffset := offset + int(entry.Size)
		
		// Validate chaining consistency: current entry + Size = next entry
		if IsIndexChainingEnabled() && i < header.EntryCount-1 {
			if nextOffset >= len(entryData) {
				return nil, fmt.Errorf("entry %d size %d would exceed data bounds (offset %d + size = %d, max %d)",
					i, entry.Size, offset, nextOffset, len(entryData))
			}
		}
		
		offset = nextOffset
	}
	
	// Final validation: ensure we consumed exactly the expected amount of data
	expectedEndOffset := len(entryData)
	if offset != expectedEndOffset {
		return nil, fmt.Errorf("entry chaining inconsistent: final offset %d, expected %d (gap of %d bytes)",
			offset, expectedEndOffset, expectedEndOffset-offset)
	}

	return entries, nil
}

// verifyHeaderChecksum verifies the checksum stored in the header
func (dc *DirectoryCache) verifyHeaderChecksum(data []byte, header *IndexHeader) error {
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

// Close cleans up mmap'd resources
func (dc *DirectoryCache) Close() error {
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
	header := (*IndexHeader)(unsafe.Pointer(&data[0]))
	header.SetHeader(dc.signature, dc.version, 0, 0, HashTypeSHA1) // No flags for empty index

	// Calculate and store checksum (no entries for empty index)
	dc.calculateAndStoreHeaderChecksum(header, nil, 0)

	if err := unix.Msync(data, unix.MS_SYNC); err != nil {
		return fmt.Errorf("failed to sync mmap: %w", err)
	}

	return nil
}

// AppendEntryToScanIndex creates a binaryEntry in a scan index file for concurrent hash processing
func (dc *DirectoryCache) AppendEntryToScanIndex(scanFileName string, relPath string, scannedPath *ScannedPath) (*binaryEntry, error) {
	// Calculate entry size
	baseSize := int(unsafe.Sizeof(binaryEntry{}))
	totalSize := baseSize + len(relPath) + 1 // +1 for null terminator
	padding := (8 - (totalSize % 8)) % 8
	entrySize := totalSize + padding

	// Open or create scan index file
	file, err := os.OpenFile(scanFileName, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open scan file %s: %w", scanFileName, err)
	}
	defer file.Close()

	// Get current file size
	stat, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat scan file: %w", err)
	}

	currentSize := int(stat.Size())
	
	// For new files, initialize with header
	if currentSize == 0 {
		initialSize := HeaderSize + entrySize
		if err := file.Truncate(int64(initialSize)); err != nil {
			return nil, fmt.Errorf("failed to truncate scan file: %w", err)
		}
		
		// Map and initialize header
		data, err := unix.Mmap(int(file.Fd()), 0, initialSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
		if err != nil {
			return nil, fmt.Errorf("failed to mmap scan file: %w", err)
		}
		
		// Initialize header for writable index (automatically clears Clean flag)
		header := (*IndexHeader)(unsafe.Pointer(&data[0]))
		header.SetHeaderForWritableIndex(dc.signature, dc.version, 1, 0, HashTypeSHA1) // Start with 1 entry, not clean
		
		// Write the entry
		entryData := data[HeaderSize:]
		actualSize := dc.writeBinaryEntryToMmap(entryData, relPath, make([]byte, HashSizeSHA1), HashTypeSHA1, scannedPath.Info, scannedPath.StatInfo, false)
		
		// Get pointer to the created entry
		entry := (*binaryEntry)(unsafe.Pointer(&entryData[0]))
		
		// Calculate and store checksum in header
		dc.calculateAndStoreHeaderChecksum(header, entryData, actualSize)
		
		return entry, nil
	}
	
	// For existing files, expand and append entry
	newSize := currentSize + entrySize
	if err := file.Truncate(int64(newSize)); err != nil {
		return nil, fmt.Errorf("failed to expand scan file: %w", err)
	}
	
	// Map the file
	data, err := unix.Mmap(int(file.Fd()), 0, newSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return nil, fmt.Errorf("failed to mmap expanded scan file: %w", err)
	}
	
	// Update entry count in header
	header := (*IndexHeader)(unsafe.Pointer(&data[0]))
	header.EntryCount++
	
	// Write new entry at end
	entryOffset := currentSize
	entryData := data[entryOffset:]
	actualSize := dc.writeBinaryEntryToMmap(entryData, relPath, make([]byte, HashSizeSHA1), HashTypeSHA1, scannedPath.Info, scannedPath.StatInfo, false)
	
	// Get pointer to the created entry
	entry := (*binaryEntry)(unsafe.Pointer(&entryData[0]))
	
	// Recalculate and update checksum in header
	allEntryData := data[HeaderSize:]
	totalEntrySize := entryOffset - HeaderSize + actualSize
	dc.calculateAndStoreHeaderChecksum(header, allEntryData, totalEntrySize)
	
	return entry, nil
}

// WriteSkiplistWithVectorIO writes a skiplist to an index file using vectorio for efficient bulk writes
func (dc *DirectoryCache) WriteSkiplistWithVectorIO(skiplist *SkiplistWrapper, outputPath string, context string) error {
	return dc.writeSkiplistWithVectorIOFiltered(skiplist, outputPath, context, false)
}

// WriteMainIndexWithVectorIO writes a main index file excluding deleted entries using vectorio
func (dc *DirectoryCache) WriteMainIndexWithVectorIO(skiplist *SkiplistWrapper, outputPath string, context string) error {
	return dc.writeSkiplistWithVectorIOFiltered(skiplist, outputPath, context, true)
}

// writeSkiplistWithVectorIOFiltered writes a skiplist to temp index using pure vectorio (no mmap)
func (dc *DirectoryCache) writeSkiplistWithVectorIOFiltered(skiplist *SkiplistWrapper, outputPath string, context string, excludeDeleted bool) error {
	// Generate IoVec slices for the specified context
	var entryIovecs []syscall.Iovec
	
	if excludeDeleted {
		// Use callback to filter out deleted entries for main index
		entryIovecs = skiplist.CallbackToIovecSlice(func(entry *binaryEntry, entryContext string) bool {
			// Include entry if it matches context (or no context filter) and is not deleted
			contextMatch := (context == "" || entryContext == context)
			return contextMatch && !entry.IsDeleted()
		})
	} else {
		// Include all entries for cache index (including deleted ones)
		if context == "" {
			entryIovecs = skiplist.ToIovecSlice()
		} else {
			entryIovecs = skiplist.ToContextIovecSlice(context)
		}
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
	header := IndexHeader{}
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

	// Write entries using vectorio (if any)
	if len(entryIovecs) > 0 {
		if nw, err := vectorio.WritevRaw(uintptr(file.Fd()), entryIovecs); err != nil {
			return fmt.Errorf("failed to write entries with vectorio: %w", err)
		} else if nw != totalEntrySize {
			return fmt.Errorf("entries write incomplete: wrote %d bytes, expected %d", nw, totalEntrySize)
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
func (dc *DirectoryCache) MergeScanSkiplistsWithVectorIO(baseSkiplist *SkiplistWrapper, scanSkiplist *SkiplistWrapper, outputPath string) error {
	// Create merged skiplist
	mergedSkiplist := baseSkiplist.Copy()
	
	// Merge scan results into base skiplist
	if err := mergedSkiplist.Merge(scanSkiplist, MergeTheirs); err != nil {
		return fmt.Errorf("failed to merge skiplists: %w", err)
	}

	// Write merged result using vectorio
	return dc.WriteSkiplistWithVectorIO(mergedSkiplist, outputPath, "")
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
func (dc *DirectoryCache) validateEntryChaining(entry *binaryEntry, offset int, entryData []byte) error {
	// Basic size validation
	if entry.Size == 0 {
		return fmt.Errorf("entry has zero size at offset %d", offset)
	}
	
	minSize := uint32(unsafe.Sizeof(*entry))
	if entry.Size < minSize {
		return fmt.Errorf("entry size %d too small (minimum %d) at offset %d", 
			entry.Size, minSize, offset)
	}
	
	maxReasonableSize := uint32(4096) // Reasonable maximum for path + padding
	if entry.Size > maxReasonableSize {
		return fmt.Errorf("entry size %d unreasonably large (maximum %d) at offset %d", 
			entry.Size, maxReasonableSize, offset)
	}
	
	// Validate that the entry doesn't extend beyond available data
	if offset+int(entry.Size) > len(entryData) {
		return fmt.Errorf("entry size %d at offset %d would extend beyond data bounds (available: %d)",
			entry.Size, offset, len(entryData)-offset)
	}
	
	// Validate 8-byte alignment
	if entry.Size%8 != 0 {
		return fmt.Errorf("entry size %d not 8-byte aligned at offset %d", entry.Size, offset)
	}
	
	// Validate that the entry pointer is 8-byte aligned
	entryPtr := uintptr(unsafe.Pointer(entry))
	if entryPtr%8 != 0 {
		return fmt.Errorf("entry pointer 0x%x not 8-byte aligned at offset %d", entryPtr, offset)
	}
	
	// If memory layout debugging is enabled, log layout information
	if IsMemoryLayoutEnabled() {
		pathFieldOffset := uintptr(unsafe.Pointer(&entry.Path[0])) - entryPtr
		os.Stderr.WriteString(fmt.Sprintf("Entry %d: size=%d, ptr=0x%x, path_offset=%d\n", 
			offset/int(minSize), entry.Size, entryPtr, pathFieldOffset))
	}
	
	return nil
}
