package dircachefilehash

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"syscall"
	"time"
	"unsafe"
)

// WriteIndex writes the sorted index to the specified file in binary format
func (dc *DirectoryCache) WriteIndex() error {
	file, err := os.Create(dc.IndexFile)
	if err != nil {
		return fmt.Errorf("failed to create index file %s: %w", dc.IndexFile, err)
	}
	defer file.Close()

	// Write header: signature (4 bytes) + version (4 bytes) + entry count (4 bytes)
	if err := binary.Write(file, binary.BigEndian, dc.signature); err != nil {
		return fmt.Errorf("failed to write signature: %w", err)
	}
	if err := binary.Write(file, binary.BigEndian, dc.version); err != nil {
		return fmt.Errorf("failed to write version: %w", err)
	}
	if err := binary.Write(file, binary.BigEndian, uint32(len(dc.entries))); err != nil {
		return fmt.Errorf("failed to write entry count: %w", err)
	}

	// Write entries
	for _, entry := range dc.entries {
		if err := dc.writeEntry(file, &entry); err != nil {
			return fmt.Errorf("failed to write entry %s: %w", entry.RelativePath, err)
		}
	}

	// Write checksum of the entire file (excluding the checksum itself)
	if err := dc.writeChecksum(file); err != nil {
		return fmt.Errorf("failed to write checksum: %w", err)
	}

	return nil
}

// writeEntry writes a single file entry in binary format
func (dc *DirectoryCache) writeEntry(w io.Writer, entry *FileEntry) error {
	// Convert hash string to bytes
	hashBytes, err := hex.DecodeString(entry.Hash)
	if err != nil {
		return fmt.Errorf("invalid hash %s: %w", entry.Hash, err)
	}
	if len(hashBytes) != 20 {
		return fmt.Errorf("hash must be 20 bytes, got %d", len(hashBytes))
	}

	// Create binary entry struct
	binEntry := binaryEntry{
		CTimeUnix: uint32(entry.CTime.Unix()),
		CTimeNano: uint32(entry.CTimeNano),
		MTimeUnix: uint32(entry.MTime.Unix()),
		MTimeNano: uint32(entry.MTimeNano),
		Dev:       entry.Dev,
		Ino:       entry.Ino,
		Mode:      entry.Mode,
		UID:       entry.UID,
		GID:       entry.GID,
		Size:      entry.Size,
		Flags:     entry.Flags,
		PathLen:   entry.PathLen,
	}
	copy(binEntry.Hash[:], hashBytes)

	// Write fixed-size portion with single binary.Write
	if err := binary.Write(w, binary.BigEndian, binEntry); err != nil {
		return err
	}

	// Write variable-size path
	pathBytes := []byte(entry.RelativePath)
	if _, err := w.Write(pathBytes); err != nil {
		return err
	}

	// Add null terminator
	if err := binary.Write(w, binary.BigEndian, byte(0)); err != nil {
		return err
	}

	// Pad to 8-byte boundary
	totalLen := int(unsafe.Sizeof(binEntry)) + int(entry.PathLen) + 1
	padding := (8 - (totalLen % 8)) % 8
	if padding > 0 {
		paddingBytes := make([]byte, padding)
		if _, err := w.Write(paddingBytes); err != nil {
			return err
		}
	}

	return nil
}

// writeChecksum writes SHA-1 checksum of the entire file content
func (dc *DirectoryCache) writeChecksum(file *os.File) error {
	// Seek to beginning to read entire file content
	if _, err := file.Seek(0, 0); err != nil {
		return err
	}

	hasher := sha1.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return err
	}

	// Seek to end to append checksum
	if _, err := file.Seek(0, 2); err != nil {
		return err
	}

	checksum := hasher.Sum(nil)
	if _, err := file.Write(checksum); err != nil {
		return err
	}

	return nil
}

// LoadIndex loads an existing binary index file
func (dc *DirectoryCache) LoadIndex() error {
	file, err := os.Open(dc.IndexFile)
	if err != nil {
		return fmt.Errorf("failed to open index file %s: %w", dc.IndexFile, err)
	}
	defer file.Close()

	// Read and verify header
	var signature [4]byte
	var version, entryCount uint32

	if err := binary.Read(file, binary.BigEndian, &signature); err != nil {
		return fmt.Errorf("failed to read signature: %w", err)
	}
	if signature != dc.signature {
		return fmt.Errorf("invalid signature: expected %s, got %s",
			string(dc.signature[:]), string(signature[:]))
	}

	if err := binary.Read(file, binary.BigEndian, &version); err != nil {
		return fmt.Errorf("failed to read version: %w", err)
	}
	if version != dc.version {
		return fmt.Errorf("unsupported version: expected %d, got %d", dc.version, version)
	}

	if err := binary.Read(file, binary.BigEndian, &entryCount); err != nil {
		return fmt.Errorf("failed to read entry count: %w", err)
	}

	// Read entries
	dc.entries = make([]FileEntry, entryCount)
	for i := uint32(0); i < entryCount; i++ {
		entry, err := dc.readEntry(file)
		if err != nil {
			return fmt.Errorf("failed to read entry %d: %w", i, err)
		}
		dc.entries[i] = *entry
	}

	// Verify checksum
	if err := dc.verifyChecksum(file); err != nil {
		return fmt.Errorf("checksum verification failed: %w", err)
	}

	return nil
}

// readEntry reads a single file entry from binary format
func (dc *DirectoryCache) readEntry(r io.Reader) (*FileEntry, error) {
	// Read fixed-size portion with single binary.Read
	var binEntry binaryEntry
	if err := binary.Read(r, binary.BigEndian, &binEntry); err != nil {
		return nil, err
	}

	// Create FileEntry from binary data
	entry := &FileEntry{
		CTime:     time.Unix(int64(binEntry.CTimeUnix), int64(binEntry.CTimeNano)),
		CTimeNano: int32(binEntry.CTimeNano),
		MTime:     time.Unix(int64(binEntry.MTimeUnix), int64(binEntry.MTimeNano)),
		MTimeNano: int32(binEntry.MTimeNano),
		Dev:       binEntry.Dev,
		Ino:       binEntry.Ino,
		Mode:      binEntry.Mode,
		UID:       binEntry.UID,
		GID:       binEntry.GID,
		Size:      binEntry.Size,
		Hash:      hex.EncodeToString(binEntry.Hash[:]),
		Flags:     binEntry.Flags,
		PathLen:   binEntry.PathLen,
	}

	// Read variable-size path
	pathBytes := make([]byte, entry.PathLen)
	if _, err := io.ReadFull(r, pathBytes); err != nil {
		return nil, err
	}
	entry.RelativePath = string(pathBytes)

	// Read null terminator
	var nullByte byte
	if err := binary.Read(r, binary.BigEndian, &nullByte); err != nil {
		return nil, err
	}

	// Read padding to 8-byte boundary
	totalLen := int(unsafe.Sizeof(binEntry)) + int(entry.PathLen) + 1
	padding := (8 - (totalLen % 8)) % 8
	if padding > 0 {
		paddingBytes := make([]byte, padding)
		if _, err := io.ReadFull(r, paddingBytes); err != nil {
			return nil, err
		}
	}

	return entry, nil
}

// verifyChecksum verifies the SHA-1 checksum at the end of the file
func (dc *DirectoryCache) verifyChecksum(file *os.File) error {
	// Get current position (should be at end of entries)
	currentPos, err := file.Seek(0, 1)
	if err != nil {
		return err
	}

	// Read the stored checksum (last 20 bytes)
	storedChecksum := make([]byte, 20)
	if _, err := io.ReadFull(file, storedChecksum); err != nil {
		return err
	}

	// Calculate checksum of file content (excluding the checksum itself)
	if _, err := file.Seek(0, 0); err != nil {
		return err
	}

	hasher := sha1.New()
	if _, err := io.CopyN(hasher, file, currentPos); err != nil {
		return err
	}

	calculatedChecksum := hasher.Sum(nil)

	// Compare checksums
	if !bytes.Equal(storedChecksum, calculatedChecksum) {
		return fmt.Errorf("checksum mismatch: stored=%x, calculated=%x",
			storedChecksum, calculatedChecksum)
	}

	return nil
}

// processFileJob processes a single file job and returns a FileEntry
func (dc *DirectoryCache) processFileJob(job fileJob) (*FileEntry, error) {
	// Hash the file contents
	hash, err := dc.hashFile(job.path)
	if err != nil {
		return nil, fmt.Errorf("failed to hash file %s: %w", job.path, err)
	}

	// Get system-specific file information
	stat := job.info.Sys().(*syscall.Stat_t)

	entry := &FileEntry{
		CTime:        time.Unix(stat.Ctim.Sec, stat.Ctim.Nsec),
		CTimeNano:    int32(stat.Ctim.Nsec),
		MTime:        job.info.ModTime(),
		MTimeNano:    int32(job.info.ModTime().Nanosecond()),
		Dev:          uint32(stat.Dev),
		Ino:          uint32(stat.Ino),
		Mode:         uint32(job.info.Mode()),
		UID:          stat.Uid,
		GID:          stat.Gid,
		Size:         uint32(job.info.Size()),
		Hash:         hash,
		Flags:        uint16(len(job.relPath)), // Use path length as flags (git convention)
		PathLen:      uint16(len(job.relPath)), // Length of relative path
		RelativePath: job.relPath,
	}

	return entry, nil
}
