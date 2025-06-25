package dircachefilehash

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"github.com/google/vectorio"
	"golang.org/x/sys/unix"
)

// IndexHeader represents the file header in host byte order (cast directly to mmap'd memory)
type IndexHeader struct {
	Signature  [4]byte // "dcfh" signature
	ByteOrder  uint64  // Byte order detection magic (0x0102030405060708) - MUST be checked before other fields
	Version    uint32  // Index version (host order)
	EntryCount uint32  // Number of entries (host order)
	Flags      uint32  // Index flags (host order) - includes sparse flag
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

// SetHeader initializes the header fields in mmap'd memory (defaults to unclean state)
func (ih *IndexHeader) SetHeader(signature [4]byte, version uint32, entryCount uint32, flags uint32) {
	ih.Signature = signature
	ih.ByteOrder = ByteOrderMagic
	ih.Version = version
	ih.EntryCount = entryCount
	ih.Flags = flags // By default, Clean flag is 0 (unclean)
}

// IsClean returns true if this index file is in a clean/complete state
func (ih *IndexHeader) IsClean() bool {
	return ih.Flags&IndexFlagClean != 0
}

// SetClean marks this index file as clean/complete (final operation)
func (ih *IndexHeader) SetClean() {
	ih.Flags |= IndexFlagClean
}

// ClearClean marks this index file as unclean/incomplete
func (ih *IndexHeader) ClearClean() {
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

	if stat.Size() < HeaderSize+ChecksumSize {
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

	// Verify checksum
	contentSize := int(stat.Size()) - ChecksumSize
	if err := dc.verifyChecksumMmap(data, contentSize); err != nil {
		return nil, fmt.Errorf("checksum verification failed: %w", err)
	}

	// Parse entries - create direct pointers to mmap'd binaryEntry structs
	var entries []*binaryEntry
	offset := 0
	entryData := mmapIndex.entries

	for i := uint32(0); i < header.EntryCount; i++ {
		if offset >= len(entryData)-ChecksumSize {
			return nil, fmt.Errorf("unexpected end of data at entry %d", i)
		}

		// Get direct pointer to binaryEntry in mmap'd memory
		entry := (*binaryEntry)(unsafe.Pointer(&entryData[offset]))
		entries = append(entries, entry)

		// Move to next entry using Size field
		offset += int(entry.Size)
	}

	return entries, nil
}

// verifyChecksumMmap verifies the SHA-1 checksum for mmap'd data
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
	totalSize := HeaderSize + ChecksumSize

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
	header.SetHeader(dc.signature, dc.version, 0, 0) // No flags for empty index

	// Write checksum
	checksum := dc.calculateChecksum(data[:HeaderSize])
	copy(data[HeaderSize:HeaderSize+ChecksumSize], checksum)

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
		initialSize := HeaderSize + entrySize + ChecksumSize
		if err := file.Truncate(int64(initialSize)); err != nil {
			return nil, fmt.Errorf("failed to truncate scan file: %w", err)
		}
		
		// Map and initialize header
		data, err := unix.Mmap(int(file.Fd()), 0, initialSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
		if err != nil {
			return nil, fmt.Errorf("failed to mmap scan file: %w", err)
		}
		
		// Initialize header (unclean state)
		header := (*IndexHeader)(unsafe.Pointer(&data[0]))
		header.SetHeader(dc.signature, dc.version, 1, 0) // Start with 1 entry, not clean
		
		// Write the entry
		entryData := data[HeaderSize:]
		actualSize := dc.writeBinaryEntryToMmap(entryData, relPath, make([]byte, HashSizeSHA1), HashTypeSHA1, scannedPath.Info, scannedPath.StatInfo, false)
		
		// Get pointer to the created entry
		entry := (*binaryEntry)(unsafe.Pointer(&entryData[0]))
		
		// Update checksum placeholder (will be updated when scan is complete)
		checksum := dc.calculateChecksum(data[:HeaderSize+actualSize])
		copy(data[HeaderSize+actualSize:], checksum)
		
		unix.Munmap(data)
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
	defer unix.Munmap(data)
	
	// Update entry count in header
	header := (*IndexHeader)(unsafe.Pointer(&data[0]))
	header.EntryCount++
	
	// Write new entry at end (before checksum)
	entryOffset := currentSize - ChecksumSize
	entryData := data[entryOffset:]
	actualSize := dc.writeBinaryEntryToMmap(entryData, relPath, make([]byte, HashSizeSHA1), HashTypeSHA1, scannedPath.Info, scannedPath.StatInfo, false)
	
	// Get pointer to the created entry
	entry := (*binaryEntry)(unsafe.Pointer(&entryData[0]))
	
	// Update checksum
	contentSize := entryOffset + actualSize
	checksum := dc.calculateChecksum(data[:contentSize])
	copy(data[contentSize:], checksum)
	
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

	// Create header in memory
	header := IndexHeader{}
	header.SetHeader(dc.signature, dc.version, uint32(entryCount), 0) // Default flags

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

	// Calculate checksum by reading back the written data
	if _, err := file.Seek(0, 0); err != nil {
		return fmt.Errorf("failed to seek to beginning for checksum: %w", err)
	}

	contentSize := HeaderSize + totalEntrySize
	contentData := make([]byte, contentSize)
	if n, err := file.Read(contentData); err != nil {
		return fmt.Errorf("failed to read back data for checksum: %w", err)
	} else if n != contentSize {
		return fmt.Errorf("read back incomplete: read %d bytes, expected %d", n, contentSize)
	}

	checksum := dc.calculateChecksum(contentData)

	// Create checksum IoVec
	checksumIovec := syscall.Iovec{
		Base: &checksum[0],
		Len:  uint64(ChecksumSize),
	}

	// Write checksum using vectorio
	if nw, err := vectorio.WritevRaw(uintptr(file.Fd()), []syscall.Iovec{checksumIovec}); err != nil {
		return fmt.Errorf("failed to write checksum with vectorio: %w", err)
	} else if nw != ChecksumSize {
		return fmt.Errorf("checksum write incomplete: wrote %d bytes, expected %d", nw, ChecksumSize)
	}

	// Mark header as clean and rewrite it
	header.SetClean()
	if _, err := file.Seek(0, 0); err != nil {
		return fmt.Errorf("failed to seek to beginning for clean flag: %w", err)
	}

	if nw, err := vectorio.WritevRaw(uintptr(file.Fd()), []syscall.Iovec{headerIovec}); err != nil {
		return fmt.Errorf("failed to write clean header with vectorio: %w", err)
	} else if nw != HeaderSize {
		return fmt.Errorf("clean header write incomplete: wrote %d bytes, expected %d", nw, HeaderSize)
	}

	// Sync to disk
	if err := file.Sync(); err != nil {
		return fmt.Errorf("failed to sync temp index: %w", err)
	}

	return nil
}

// WriteSkiplistToTmpIndex writes a skiplist to a temporary index file, then atomically renames it
func (dc *DirectoryCache) WriteSkiplistToTmpIndex(skiplist *SkiplistWrapper, finalPath string, context string) error {
	// Generate temporary file name
	tmpPath := dc.generateTmpIndexFileName()
	
	// Write to temporary file using vectorio
	if err := dc.WriteSkiplistWithVectorIO(skiplist, tmpPath, context); err != nil {
		os.Remove(tmpPath) // Cleanup on failure
		return fmt.Errorf("failed to write to temp file: %w", err)
	}

	// Atomic rename to final location
	if err := os.Rename(tmpPath, finalPath); err != nil {
		os.Remove(tmpPath) // Cleanup on failure
		return fmt.Errorf("failed to rename temp file: %w", err)
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
