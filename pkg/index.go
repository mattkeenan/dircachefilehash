package dircachefilehash

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"

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

// IsSparse returns true if this is a sparse index
func (ih *IndexHeader) IsSparse() bool {
	return ih.Flags&IndexFlagSparse != 0
}

// SetSparse marks this index as sparse
func (ih *IndexHeader) SetSparse() {
	ih.Flags |= IndexFlagSparse
}

// ClearSparse removes the sparse flag from this index
func (ih *IndexHeader) ClearSparse() {
	ih.Flags &^= IndexFlagSparse
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
func (ih *IndexHeader) SetHeader(signature [4]byte, version uint32, entryCount uint32, flags uint32) {
	ih.Signature = signature
	ih.ByteOrder = ByteOrderMagic
	ih.Version = version
	ih.EntryCount = entryCount
	ih.Flags = flags
}

// EntryDataOffset returns the offset where entry data begins
func (ih *IndexHeader) EntryDataOffset() int {
	return HeaderSize
}

// writeEntryToMmap writes a binaryEntry directly to mmap'd memory
func (dc *DirectoryCache) writeEntryToMmap(data []byte, relPath string, hash []byte, hashType uint16, info os.FileInfo, stat *syscall.Stat_t, isDeleted bool) int {
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

// WriteIndex writes entries directly to mmap'd index file (pure file I/O)
func (dc *DirectoryCache) WriteIndex(jobs []fileJob) error {
	return dc.writeIndexWithFlags(jobs, 0) // Default: not sparse
}

// writeIndexWithFlags writes entries directly to mmap'd index file with specified flags (pure file I/O)
func (dc *DirectoryCache) writeIndexWithFlags(jobs []fileJob, flags uint32) error {
	// Calculate total file size needed
	totalSize := HeaderSize + ChecksumSize
	for _, job := range jobs {
		totalSize += BESizeFromPathLen(len(job.relPath))
	}

	// Create the file
	file, err := os.Create(dc.IndexFile)
	if err != nil {
		return fmt.Errorf("failed to create index file %s: %w", dc.IndexFile, err)
	}
	defer file.Close()

	// Truncate to exact size
	if err := file.Truncate(int64(totalSize)); err != nil {
		return fmt.Errorf("failed to truncate file: %w", err)
	}

	// Memory map the file
	data, err := unix.Mmap(int(file.Fd()), 0, totalSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return fmt.Errorf("failed to mmap file: %w", err)
	}
	defer unix.Munmap(data)

	// Write header directly to mmap'd memory (zero-copy)
	header := (*IndexHeader)(unsafe.Pointer(&data[0]))
	header.SetHeader(dc.signature, dc.version, uint32(len(jobs)), flags)

	// Write entries directly to mmap'd memory
	offset := HeaderSize
	for _, job := range jobs {
		// Process file and get hash
		hashBytes, hashType, stat, err := dc.processFileJob(job)
		if err != nil {
			return fmt.Errorf("failed to process file %s: %w", job.path, err)
		}

		// Write entry directly to mmap'd memory
		entrySize := dc.writeEntryToMmap(data[offset:], job.relPath, hashBytes, hashType, job.info, stat, false)
		offset += entrySize
	}

	// Calculate and write checksum
	checksum := dc.calculateChecksum(data[:offset])
	copy(data[offset:offset+ChecksumSize], checksum)

	// Sync to disk
	if err := unix.Msync(data, unix.MS_SYNC); err != nil {
		return fmt.Errorf("failed to sync mmap: %w", err)
	}

	return nil
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

// WriteEntries writes existing binaryEntry pointers directly to index file (pure file I/O)
func (dc *DirectoryCache) WriteEntries(entries []*binaryEntry, flags uint32) error {
	// Calculate total file size needed
	totalSize := HeaderSize + ChecksumSize
	for _, entry := range entries {
		totalSize += entry.EntrySize()
	}

	// Create the file
	file, err := os.Create(dc.IndexFile)
	if err != nil {
		return fmt.Errorf("failed to create index file %s: %w", dc.IndexFile, err)
	}
	defer file.Close()

	// Truncate to exact size
	if err := file.Truncate(int64(totalSize)); err != nil {
		return fmt.Errorf("failed to truncate file: %w", err)
	}

	// Memory map the file
	data, err := unix.Mmap(int(file.Fd()), 0, totalSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return fmt.Errorf("failed to mmap file: %w", err)
	}
	defer unix.Munmap(data)

	// Write header directly to mmap'd memory (zero-copy)
	header := (*IndexHeader)(unsafe.Pointer(&data[0]))
	header.SetHeader(dc.signature, dc.version, uint32(len(entries)), flags)

	// Copy entries directly to mmap'd memory
	offset := HeaderSize
	for _, entry := range entries {
		entrySize := entry.EntrySize()

		// Copy entry data directly
		copy(data[offset:offset+entrySize],
			(*[1 << 20]byte)(unsafe.Pointer(entry))[:entrySize:entrySize])

		offset += entrySize
	}

	// Calculate and write checksum
	checksum := dc.calculateChecksum(data[:offset])
	copy(data[offset:offset+ChecksumSize], checksum)

	// Sync to disk
	if err := unix.Msync(data, unix.MS_SYNC); err != nil {
		return fmt.Errorf("failed to sync mmap: %w", err)
	}

	return nil
}

// WriteSparseEntries writes existing binaryEntry pointers as a sparse index file
func (dc *DirectoryCache) WriteSparseEntries(entries []*binaryEntry, filePath string) error {
	oldIndexFile := dc.IndexFile
	dc.IndexFile = filePath
	defer func() { dc.IndexFile = oldIndexFile }()
	return dc.WriteEntries(entries, IndexFlagSparse)
}

// createEmptyIndexAt creates an empty index file at the specified path
func (dc *DirectoryCache) createEmptyIndexAt(filePath string) error {
	oldIndexFile := dc.IndexFile
	dc.IndexFile = filePath
	defer func() { dc.IndexFile = oldIndexFile }()
	return dc.createEmptyIndex()
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
