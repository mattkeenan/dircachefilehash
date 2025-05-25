// Package dircachefilehash provides functionality to scan directories,
// hash file contents, and maintain a sorted index file for file integrity
// checking and change detection.
package dircachefilehash

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"
	"unsafe"
)

// binaryEntry represents the fixed-size binary portion of a file entry
type binaryEntry struct {
	CTimeUnix uint32   // Change time seconds
	CTimeNano uint32   // Change time nanoseconds
	MTimeUnix uint32   // Modification time seconds
	MTimeNano uint32   // Modification time nanoseconds
	Dev       uint32   // Device ID
	Ino       uint32   // Inode number
	Mode      uint32   // File mode
	UID       uint32   // User ID
	GID       uint32   // Group ID
	Size      uint32   // File size
	Hash      [20]byte // SHA-1 hash (20 bytes)
	Flags     uint16   // Index flags
	PathLen   uint16   // Length of relative path
}

// FileEntry represents a file with its hash and metadata
// Fields are ordered to match git dircache index file format
type FileEntry struct {
	CTime        time.Time `json:"ctime"`         // Change time (metadata last changed, seconds since epoch)
	CTimeNano    int32     `json:"ctime_nano"`    // Change time nanoseconds
	MTime        time.Time `json:"mtime"`         // Modification time (content last modified, seconds since epoch)
	MTimeNano    int32     `json:"mtime_nano"`    // Modification time nanoseconds
	Dev          uint32    `json:"dev"`           // Device ID
	Ino          uint32    `json:"ino"`           // Inode number
	Mode         uint32    `json:"mode"`          // File mode (permissions and type)
	UID          uint32    `json:"uid"`           // User ID (owner)
	GID          uint32    `json:"gid"`           // Group ID
	Size         uint32    `json:"size"`          // File size in bytes
	Hash         string    `json:"hash"`          // SHA-1 hash (40 hex chars)
	Flags        uint16    `json:"flags"`         // Index flags
	PathLen      uint16    `json:"path_len"`      // Length of relative path (big-endian)
	RelativePath string    `json:"relative_path"` // Relative path from root directory
}

// DirectoryCache manages the file cache for a directory
type DirectoryCache struct {
	RootDir   string
	IndexFile string
	entries   []FileEntry
	signature [4]byte // "dcfh" signature
	version   uint32  // Index version
}

// NewDirectoryCache creates a new directory cache instance
func NewDirectoryCache(rootDir, indexFile string) *DirectoryCache {
	// If indexFile is empty, use default location under rootDir
	if indexFile == "" {
		indexFile = filepath.Join(rootDir, ".dcfh", "index")
	}

	return &DirectoryCache{
		RootDir:   rootDir,
		IndexFile: indexFile,
		entries:   make([]FileEntry, 0),
		signature: [4]byte{'d', 'c', 'f', 'h'}, // "dcfh" signature
		version:   1,                           // Version 1 format
	}
}

// hashFile calculates SHA-1 hash of a file's contents (matching git)
func (dc *DirectoryCache) hashFile(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file %s: %w", filePath, err)
	}
	defer file.Close()

	hasher := sha1.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("failed to hash file %s: %w", filePath, err)
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// ScanDirectory scans the directory and creates file entries with hashes
func (dc *DirectoryCache) ScanDirectory() error {
	dc.entries = make([]FileEntry, 0)

	// FIFO slice for file paths - push to end, pop from front
	pathQueue := []string{dc.RootDir}

	// Process paths until queue is empty
	for len(pathQueue) > 0 {
		// Pop the first entry from the FIFO slice
		currentPath := pathQueue[0]
		pathQueue = pathQueue[1:]

		// Get file info
		info, err := os.Lstat(currentPath) // Use Lstat to handle symlinks properly
		if err != nil {
			// Skip files we can't access
			continue
		}

		// If it's a directory, read its contents and add to queue
		if info.IsDir() {
			// Skip the index directory if it's inside the scan directory
			indexDir := filepath.Dir(dc.IndexFile)
			if currentPath == indexDir {
				continue
			}

			entries, err := os.ReadDir(currentPath)
			if err != nil {
				// Skip directories we can't read
				continue
			}

			// Sort entries by name using bytewise comparison
			sort.Slice(entries, func(i, j int) bool {
				return entries[i].Name() < entries[j].Name()
			})

			// Add all entries to the FIFO queue
			for _, entry := range entries {
				fullPath := filepath.Join(currentPath, entry.Name())
				pathQueue = append(pathQueue, fullPath)
			}
		} else if info.Mode().IsRegular() {
			// Skip the index file itself
			if currentPath == dc.IndexFile {
				continue
			}

			// Process regular files - hash contents and create entry
			if err := dc.processRegularFile(currentPath, info); err != nil {
				// Log error but continue processing
				fmt.Fprintf(os.Stderr, "Warning: failed to process file %s: %v\n", currentPath, err)
				continue
			}
		}
		// For other file types (symlinks, device files, etc.), we just skip them
		// since we only want to index regular files with content hashes
	}

	// Sort entries by hash for byte comparison order
	sort.Slice(dc.entries, func(i, j int) bool {
		return dc.entries[i].Hash < dc.entries[j].Hash
	})

	return nil
}

// processRegularFile processes a regular file and adds it to the entries
func (dc *DirectoryCache) processRegularFile(filePath string, info os.FileInfo) error {
	// Calculate relative path from root directory
	relPath, err := filepath.Rel(dc.RootDir, filePath)
	if err != nil {
		return fmt.Errorf("failed to get relative path for %s: %w", filePath, err)
	}

	// Hash the file contents
	hash, err := dc.hashFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to hash file %s: %w", filePath, err)
	}

	// Get system-specific file information
	stat := info.Sys().(*syscall.Stat_t)

	entry := FileEntry{
		CTime:        time.Unix(stat.Ctim.Sec, stat.Ctim.Nsec),
		CTimeNano:    int32(stat.Ctim.Nsec),
		MTime:        info.ModTime(),
		MTimeNano:    int32(info.ModTime().Nanosecond()),
		Dev:          uint32(stat.Dev),
		Ino:          uint32(stat.Ino),
		Mode:         uint32(info.Mode()),
		UID:          stat.Uid,
		GID:          stat.Gid,
		Size:         uint32(info.Size()),
		Hash:         hash,
		Flags:        uint16(len(relPath)), // Use path length as flags (git convention)
		PathLen:      uint16(len(relPath)), // Length of relative path
		RelativePath: relPath,
	}

	dc.entries = append(dc.entries, entry)
	return nil
}

// WriteIndex writes the sorted index to the specified file in binary format
func (dc *DirectoryCache) WriteIndex() error {
	// Ensure the directory for the index file exists
	indexDir := filepath.Dir(dc.IndexFile)
	if err := os.MkdirAll(indexDir, 0755); err != nil {
		return fmt.Errorf("failed to create index directory %s: %w", indexDir, err)
	}

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

// GetEntries returns a copy of the current entries
func (dc *DirectoryCache) GetEntries() []FileEntry {
	entries := make([]FileEntry, len(dc.entries))
	copy(entries, dc.entries)
	return entries
}

// FindByHash finds entries with the specified hash
func (dc *DirectoryCache) FindByHash(hash string) []FileEntry {
	var matches []FileEntry

	// Use binary search since entries are sorted by hash
	idx := sort.Search(len(dc.entries), func(i int) bool {
		return dc.entries[i].Hash >= hash
	})

	// Collect all entries with matching hash
	for i := idx; i < len(dc.entries) && dc.entries[i].Hash == hash; i++ {
		matches = append(matches, dc.entries[i])
	}

	return matches
}

// Update scans the directory and updates the index file
func (dc *DirectoryCache) Update() error {
	if err := dc.ScanDirectory(); err != nil {
		return fmt.Errorf("failed to scan directory: %w", err)
	}

	if err := dc.WriteIndex(); err != nil {
		return fmt.Errorf("failed to write index: %w", err)
	}

	return nil
}

// Stats returns statistics about the cache
func (dc *DirectoryCache) Stats() (int, int64, error) {
	var totalSize int64
	for _, entry := range dc.entries {
		totalSize += int64(entry.Size)
	}
	return len(dc.entries), totalSize, nil
}

// FindDuplicates returns groups of files with identical hashes
func (dc *DirectoryCache) FindDuplicates() map[string][]FileEntry {
	duplicates := make(map[string][]FileEntry)

	for _, entry := range dc.entries {
		if _, exists := duplicates[entry.Hash]; !exists {
			duplicates[entry.Hash] = make([]FileEntry, 0)
		}
		duplicates[entry.Hash] = append(duplicates[entry.Hash], entry)
	}

	// Remove entries with only one file
	for hash, entries := range duplicates {
		if len(entries) <= 1 {
			delete(duplicates, hash)
		}
	}

	return duplicates
}
