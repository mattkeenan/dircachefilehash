package dircachefilehash

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	HeaderSize   = 12 // signature(4) + version(4) + entry_count(4)
	ChecksumSize = 20 // SHA-1 checksum size
)

// MmapIndex represents a memory-mapped index file
type MmapIndex struct {
	data    []byte
	file    *os.File
	header  *IndexHeader
	entries []byte // Raw entry data after header
	size    int    // Current mapped size
	offset  int    // Current write offset
}

// IndexHeader represents the file header in host byte order
type IndexHeader struct {
	Signature  [4]byte
	Version    uint32
	EntryCount uint32
}

// makeSpaceForEntry ensures space for an entry, expands mapping if needed, and returns pointer
func (dc *DirectoryCache) makeSpaceForEntry(mmapIdx *MmapIndex, entrySize int) (*binaryEntry, error) {
	// Check if we need more space (including checksum space)
	requiredSpace := mmapIdx.offset + entrySize + ChecksumSize
	if requiredSpace > mmapIdx.size {
		// Need to expand the mapping
		newSize := mmapIdx.size * 2
		if newSize < requiredSpace {
			newSize = requiredSpace + (1024 * 1024) // Add 1MB buffer
		}

		// Unmap current mapping
		if err := unix.Munmap(mmapIdx.data); err != nil {
			return nil, fmt.Errorf("failed to unmap: %w", err)
		}

		// Expand file
		if err := mmapIdx.file.Truncate(int64(newSize)); err != nil {
			return nil, fmt.Errorf("failed to expand file: %w", err)
		}

		// Remap with new size
		newData, err := unix.Mmap(int(mmapIdx.file.Fd()), 0, newSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
		if err != nil {
			return nil, fmt.Errorf("failed to remap file: %w", err)
		}

		// Update mapping
		mmapIdx.data = newData
		mmapIdx.size = newSize
		mmapIdx.entries = newData[HeaderSize:]
		mmapIdx.header = (*IndexHeader)(unsafe.Pointer(&newData[0]))
	}

	// Get pointer to entry location
	entryPtr := (*binaryEntry)(unsafe.Pointer(&mmapIdx.data[mmapIdx.offset]))

	// Zero out the entire entry space
	entryBytes := (*[1 << 20]byte)(unsafe.Pointer(entryPtr))[:entrySize:entrySize]
	for i := range entryBytes {
		entryBytes[i] = 0
	}

	// Set only the Size field
	entryPtr.Size = uint32(entrySize)

	// Update offset for next entry
	mmapIdx.offset += entrySize

	return entryPtr, nil
}

// writeEntryToMmap writes a binaryEntry directly to mmap'd memory
func (dc *DirectoryCache) writeEntryToMmap(data []byte, relPath string, hash []byte, hashType uint16, info os.FileInfo, stat *syscall.Stat_t) int {
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
	entry.Flags = uint16(len(relPath))
	entry.HashType = hashType

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

// WriteIndex writes entries directly to mmap'd index file using skiplist
func (dc *DirectoryCache) WriteIndex(jobs []fileJob) error {
	// Calculate total file size needed
	totalSize := HeaderSize + ChecksumSize
	for _, job := range jobs {
		totalSize += PathLenToSize(len(job.relPath))
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

	// Write header
	header := (*IndexHeader)(unsafe.Pointer(&data[0]))
	header.Signature = dc.signature
	header.Version = dc.version
	header.EntryCount = uint32(len(jobs))

	// Clear and recreate skiplist for new entries
	dc.skiplist = NewSkiplistWrapper(16)

	// Write entries directly to mmap'd memory and add to skiplist iteratively
	offset := HeaderSize
	for _, job := range jobs {
		// Process file and get hash
		hashBytes, hashType, stat, err := dc.processFileJob(job)
		if err != nil {
			return fmt.Errorf("failed to process file %s: %w", job.path, err)
		}

		// Write entry directly to mmap'd memory
		entrySize := dc.writeEntryToMmap(data[offset:], job.relPath, hashBytes, hashType, job.info, stat)

		// Get pointer to the entry we just wrote and add to skiplist immediately
		entryPtr := (*binaryEntry)(unsafe.Pointer(&data[offset]))
		dc.skiplist.Insert(entryPtr)

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

// LoadIndex loads and maps the index file, populating skiplist with direct pointers to entries
func (dc *DirectoryCache) LoadIndex() error {
	file, err := os.Open(dc.IndexFile)
	if err != nil {
		return fmt.Errorf("failed to open index file %s: %w", dc.IndexFile, err)
	}

	// Get file size
	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return fmt.Errorf("failed to stat file: %w", err)
	}

	if stat.Size() < HeaderSize+ChecksumSize {
		file.Close()
		return fmt.Errorf("file too small: %d bytes", stat.Size())
	}

	// Memory map the file for reading
	data, err := unix.Mmap(int(file.Fd()), 0, int(stat.Size()), unix.PROT_READ, unix.MAP_PRIVATE)
	if err != nil {
		file.Close()
		return fmt.Errorf("failed to mmap file: %w", err)
	}

	// Create MmapIndex
	mmapIndex := &MmapIndex{
		data:    data,
		file:    file,
		header:  (*IndexHeader)(unsafe.Pointer(&data[0])),
		entries: data[HeaderSize:],
		size:    int(stat.Size()),
		offset:  HeaderSize,
	}
	dc.mmapIndex = mmapIndex

	// Verify header
	if mmapIndex.header.Signature != dc.signature {
		return fmt.Errorf("invalid signature")
	}
	if mmapIndex.header.Version != dc.version {
		return fmt.Errorf("unsupported version: %d", mmapIndex.header.Version)
	}

	// Verify checksum
	contentSize := int(stat.Size()) - ChecksumSize
	if err := dc.verifyChecksumMmap(data, contentSize); err != nil {
		return fmt.Errorf("checksum verification failed: %w", err)
	}

	// Clear and recreate skiplist
	dc.skiplist = NewSkiplistWrapper(16)

	// Parse entries - create direct pointers to mmap'd binaryEntry structs and add to skiplist
	offset := 0
	entryData := mmapIndex.entries

	for i := uint32(0); i < mmapIndex.header.EntryCount; i++ {
		if offset >= len(entryData)-ChecksumSize {
			return fmt.Errorf("unexpected end of data at entry %d", i)
		}

		// Get direct pointer to binaryEntry in mmap'd memory
		entry := (*binaryEntry)(unsafe.Pointer(&entryData[offset]))

		// Insert immediately while parsing for better cache locality and memory efficiency
		dc.skiplist.Insert(entry)

		// Move to next entry using Size field
		offset += int(entry.Size)
	}

	return nil
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

// createEmptyIndex creates an empty index file
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

	// Write header
	header := (*IndexHeader)(unsafe.Pointer(&data[0]))
	header.Signature = dc.signature
	header.Version = dc.version
	header.EntryCount = 0

	// Write checksum
	checksum := dc.calculateChecksum(data[:HeaderSize])
	copy(data[HeaderSize:HeaderSize+ChecksumSize], checksum)

	if err := unix.Msync(data, unix.MS_SYNC); err != nil {
		return fmt.Errorf("failed to sync mmap: %w", err)
	}

	return nil
}
