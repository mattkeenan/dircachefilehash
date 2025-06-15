package dircachefilehash

import (
	"fmt"
	"os"
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
}

// IndexHeader represents the file header in host byte order
type IndexHeader struct {
	Signature  [4]byte
	Version    uint32
	EntryCount uint32
}

// WriteIndex writes entries directly to mmap'd index file
func (dc *DirectoryCache) WriteIndex(jobs []fileJob) error {
	// Calculate total file size needed
	totalSize := HeaderSize + ChecksumSize
	for _, job := range jobs {
		entrySize := int(unsafe.Sizeof(binaryEntry{})) + len(job.relPath) + 1
		padding := (8 - (entrySize % 8)) % 8
		totalSize += entrySize + padding
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

	// Write entries directly to mmap'd memory
	offset := HeaderSize
	for _, job := range jobs {
		// Process file and get hash
		hash, stat, err := dc.processFileJob(job)
		if err != nil {
			return fmt.Errorf("failed to process file %s: %w", job.path, err)
		}

		// Write entry directly to mmap'd memory
		entrySize := dc.writeEntryToMmap(data[offset:], job.relPath, hash, job.info, stat)
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

// LoadIndex loads and maps the index file, populating direct pointers to entries
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

	// Parse entries - create direct pointers to mmap'd binaryEntry structs
	dc.entries = make([]*binaryEntry, 0, mmapIndex.header.EntryCount)
	offset := 0
	entryData := mmapIndex.entries

	for i := uint32(0); i < mmapIndex.header.EntryCount; i++ {
		if offset >= len(entryData)-ChecksumSize {
			return fmt.Errorf("unexpected end of data at entry %d", i)
		}

		// Get direct pointer to binaryEntry in mmap'd memory
		entry := (*binaryEntry)(unsafe.Pointer(&entryData[offset]))
		dc.entries = append(dc.entries, entry)

		// Move to next entry
		offset += entry.EntrySize()
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
