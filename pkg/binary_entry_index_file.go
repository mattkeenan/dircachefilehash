package dircachefilehash

import (
	"encoding/hex"
	"fmt"
	"os"
	"unsafe"
)

// BEIndexFileIOEntry implements BinaryEntryInterface for standard file I/O access to index files
//
// This implementation handles entries accessed via standard read/write operations:
// - Uses standard file I/O instead of memory mapping
// - Suitable for systems with limited mmap capabilities
// - Proper file position tracking for entry access
// - Error handling for file I/O failures
// - Support for both read and write operations
//
// Safety features:
// - File position validation and bounds checking
// - Thread-safe file access (each operation uses its own file handle)
// - Error returns for all I/O operations
// - Concurrent access safety through independent file handles
type BEIndexFileIOEntry struct {
	BinaryEntryBase
	filePath   string // Path to the index file
	fileOffset int64  // Absolute file offset to the entry
	entrySize  uint32 // Size of the entry for bounds checking
	context    string // Context for this entry (e.g., MainContext, CacheContext)
}

// NewBEIndexFileIOEntry creates a new BEIndexFileIOEntry for standard file I/O access
// filePath: path to the index file
// fileOffset: absolute position of the entry in the file
// entrySize: size of the entry for bounds checking
// context: the context for this entry (e.g., MainContext, CacheContext, ScanContext)
func NewBEIndexFileIOEntry(filePath string, fileOffset int64, entrySize uint32, context string) *BEIndexFileIOEntry {
	return &BEIndexFileIOEntry{
		BinaryEntryBase: NewBinaryEntryBase(BEIndexFileIO),
		filePath:        filePath,
		fileOffset:      fileOffset,
		entrySize:       entrySize,
		context:         context,
	}
}

// readEntryData reads the binary entry data from the file
// Each read operation uses its own file handle for thread safety
func (ife *BEIndexFileIOEntry) readEntryData() (*binaryEntry, error) {
	// Open file for this specific read operation
	file, err := os.Open(ife.filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open index file %s: %w", ife.filePath, err)
	}
	defer func() { _ = file.Close() }()

	// Seek to the entry position
	if _, err := file.Seek(ife.fileOffset, 0); err != nil {
		return nil, fmt.Errorf("failed to seek to entry position %d: %w", ife.fileOffset, err)
	}

	// Read the entry data
	entryData := make([]byte, ife.entrySize)
	n, err := file.Read(entryData)
	if err != nil {
		return nil, fmt.Errorf("failed to read entry data: %w", err)
	}
	if n != int(ife.entrySize) {
		return nil, fmt.Errorf("incomplete read: got %d bytes, expected %d", n, ife.entrySize)
	}

	// Cast to binaryEntry
	entry := (*binaryEntry)(unsafe.Pointer(&entryData[0]))

	// Validate entry size matches what we read
	if entry.Size != ife.entrySize {
		return nil, fmt.Errorf("entry size mismatch: entry reports %d, expected %d", entry.Size, ife.entrySize)
	}

	return entry, nil
}

// writeEntryData writes the binary entry data to the file
// Each write operation uses its own file handle for thread safety
func (ife *BEIndexFileIOEntry) writeEntryData(entry *binaryEntry) error {
	// Open file for this specific write operation
	file, err := os.OpenFile(ife.filePath, os.O_RDWR, 0644) //nolint:gosec // G302: .dcfh/ index file, non-secret (metadata + hashes)
	if err != nil {
		return fmt.Errorf("failed to open index file for writing %s: %w", ife.filePath, err)
	}
	defer func() { _ = file.Close() }()

	// Seek to the entry position
	if _, err := file.Seek(ife.fileOffset, 0); err != nil {
		return fmt.Errorf("failed to seek to entry position %d: %w", ife.fileOffset, err)
	}

	// Write the entry data
	entryData := (*[512]byte)(unsafe.Pointer(entry))[:ife.entrySize:ife.entrySize]
	n, err := file.Write(entryData)
	if err != nil {
		return fmt.Errorf("failed to write entry data: %w", err)
	}
	if n != int(ife.entrySize) {
		return fmt.Errorf("incomplete write: wrote %d bytes, expected %d", n, ife.entrySize)
	}

	// Sync to ensure data is written
	if err := file.Sync(); err != nil {
		return fmt.Errorf("failed to sync entry data: %w", err)
	}

	return nil
}

// IsValid performs a quick check if the entry is accessible
func (ife *BEIndexFileIOEntry) IsValid() bool {
	// Check if file exists and is accessible
	if _, err := os.Stat(ife.filePath); err != nil {
		return false
	}

	// Check if offset and size are reasonable
	if ife.fileOffset < HeaderSize || ife.entrySize == 0 {
		return false
	}

	return true
}

// Size returns the entry size field
func (ife *BEIndexFileIOEntry) Size() (uint32, error) {
	ife.RLock()
	defer ife.RUnlock()

	entry, err := ife.readEntryData()
	if err != nil {
		return 0, err
	}

	return entry.Size, nil
}

// CTimeWall returns the creation time wall clock value
func (ife *BEIndexFileIOEntry) CTimeWall() (uint64, error) {
	ife.RLock()
	defer ife.RUnlock()

	entry, err := ife.readEntryData()
	if err != nil {
		return 0, err
	}

	return entry.CTimeWall, nil
}

// MTimeWall returns the modification time wall clock value
func (ife *BEIndexFileIOEntry) MTimeWall() (uint64, error) {
	ife.RLock()
	defer ife.RUnlock()

	entry, err := ife.readEntryData()
	if err != nil {
		return 0, err
	}

	return entry.MTimeWall, nil
}

// Dev returns the device ID
func (ife *BEIndexFileIOEntry) Dev() (uint32, error) {
	ife.RLock()
	defer ife.RUnlock()

	entry, err := ife.readEntryData()
	if err != nil {
		return 0, err
	}

	return entry.Dev, nil
}

// Ino returns the inode number
func (ife *BEIndexFileIOEntry) Ino() (uint32, error) {
	ife.RLock()
	defer ife.RUnlock()

	entry, err := ife.readEntryData()
	if err != nil {
		return 0, err
	}

	return entry.Ino, nil
}

// Mode returns the file mode
func (ife *BEIndexFileIOEntry) Mode() (uint32, error) {
	ife.RLock()
	defer ife.RUnlock()

	entry, err := ife.readEntryData()
	if err != nil {
		return 0, err
	}

	return entry.Mode, nil
}

// UID returns the user ID
func (ife *BEIndexFileIOEntry) UID() (uint32, error) {
	ife.RLock()
	defer ife.RUnlock()

	entry, err := ife.readEntryData()
	if err != nil {
		return 0, err
	}

	return entry.UID, nil
}

// GID returns the group ID
func (ife *BEIndexFileIOEntry) GID() (uint32, error) {
	ife.RLock()
	defer ife.RUnlock()

	entry, err := ife.readEntryData()
	if err != nil {
		return 0, err
	}

	return entry.GID, nil
}

// FileSize returns the file size in bytes
func (ife *BEIndexFileIOEntry) FileSize() (uint64, error) {
	ife.RLock()
	defer ife.RUnlock()

	entry, err := ife.readEntryData()
	if err != nil {
		return 0, err
	}

	return entry.FileSize, nil
}

// HashType returns the hash algorithm type
func (ife *BEIndexFileIOEntry) HashType() (uint16, error) {
	ife.RLock()
	defer ife.RUnlock()

	entry, err := ife.readEntryData()
	if err != nil {
		return 0, err
	}

	return entry.HashType, nil
}

// Hash returns the file hash as a byte array
func (ife *BEIndexFileIOEntry) Hash() ([20]byte, error) {
	ife.RLock()
	defer ife.RUnlock()

	entry, err := ife.readEntryData()
	if err != nil {
		return [20]byte{}, err
	}

	// Convert from [64]byte to [20]byte (taking only first 20 bytes)
	var hash [20]byte
	copy(hash[:], entry.Hash[:20])
	return hash, nil
}

// EntryFlags returns the entry flags
func (ife *BEIndexFileIOEntry) EntryFlags() (uint32, error) {
	ife.RLock()
	defer ife.RUnlock()

	entry, err := ife.readEntryData()
	if err != nil {
		return 0, err
	}

	return uint32(entry.EntryFlags), nil
}

// RelativePath returns the relative file path
func (ife *BEIndexFileIOEntry) RelativePath() (string, error) {
	ife.RLock()
	defer ife.RUnlock()

	entry, err := ife.readEntryData()
	if err != nil {
		return "", err
	}

	// Calculate path location after the fixed-size binaryEntry struct
	pathPtr := unsafe.Add(unsafe.Pointer(entry), unsafe.Sizeof(*entry))
	pathBytes := (*[256]byte)(pathPtr) // Max reasonable path length for bounds checking

	// Find null terminator to determine actual path length
	pathLen := 0
	maxPathLen := min(int(ife.entrySize)-int(unsafe.Sizeof(*entry)), 256)

	for i := range maxPathLen {
		if pathBytes[i] == 0 {
			pathLen = i
			break
		}
	}

	if pathLen == 0 {
		// Empty path represents current directory - normalize to "." like ls -al
		return ".", nil
	}

	return string(pathBytes[:pathLen]), nil
}

// HashString returns the hash as a hexadecimal string
func (ife *BEIndexFileIOEntry) HashString() (string, error) {
	hash, err := ife.Hash()
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(hash[:]), nil
}

// IsDeleted returns true if the entry is marked as deleted
func (ife *BEIndexFileIOEntry) IsDeleted() (bool, error) {
	flags, err := ife.EntryFlags()
	if err != nil {
		return false, err
	}

	// Check bit 0 for deletion flag (matching existing implementation)
	return (flags & 1) != 0, nil
}

// SetHash updates the entry's hash and hash type
func (ife *BEIndexFileIOEntry) SetHash(hashBytes []byte, hashType uint16) error {
	ife.Lock()
	defer ife.Unlock()

	// Read current entry data
	entry, err := ife.readEntryData()
	if err != nil {
		return err
	}

	// Validate hash length
	if len(hashBytes) != 20 {
		return fmt.Errorf("invalid hash length: got %d, expected 20", len(hashBytes))
	}

	// Update hash and hash type
	copy(entry.Hash[:], hashBytes)
	entry.HashType = hashType

	// Write back to file
	return ife.writeEntryData(entry)
}

// SetDeleted updates the entry's deletion flag
func (ife *BEIndexFileIOEntry) SetDeleted(deleted bool) error {
	ife.Lock()
	defer ife.Unlock()

	// Read current entry data
	entry, err := ife.readEntryData()
	if err != nil {
		return err
	}

	// Update deletion flag (bit 0)
	if deleted {
		entry.EntryFlags |= 1
	} else {
		entry.EntryFlags &^= 1
	}

	// Write back to file
	return ife.writeEntryData(entry)
}

// GetContext returns the context for this index file entry
// Context is provided during creation and identifies the source/purpose of the entry
func (ife *BEIndexFileIOEntry) GetContext() (string, error) {
	return ife.context, nil
}

// RefCounted interface implementation (no-op for file I/O entries)

// IncRef is a no-op for file I/O entries since they don't hold mmap references
func (ife *BEIndexFileIOEntry) IncRef() {
	// No-op: File I/O entries don't need ref counting since they use standard file operations
}

// DecRef is a no-op for file I/O entries since they don't hold mmap references
func (ife *BEIndexFileIOEntry) DecRef() {
	// No-op: File I/O entries don't need ref counting since they use standard file operations
}

// RefCount always returns 0 for file I/O entries
func (ife *BEIndexFileIOEntry) RefCount() int32 {
	return 0 // No references to track for file I/O entries
}
