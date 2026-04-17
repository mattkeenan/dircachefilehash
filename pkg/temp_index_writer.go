package dircachefilehash

import (
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"hash"
	"os"
	"syscall"
	"unsafe"

	"github.com/google/vectorio"
)

// TempIndexWriter handles iterative IoVec batch writing to temp index files
// Implements immediate batching - writes whatever entries are ready right now
type TempIndexWriter struct {
	file           *os.File
	tempPath       string
	headerWritten  bool
	entryCount     uint32
	dc             *DirectoryCache
	checksumWriter hash.Hash // Incremental checksum calculation
}

// NewTempIndexWriter creates a new temp index writer for the specified temp file
func NewTempIndexWriter(dc *DirectoryCache, tempPath string) (*TempIndexWriter, error) {
	// Create temp index file
	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp index file %s: %w", tempPath, err)
	}

	// Create a new hasher instance based on the configured hash type
	var checksumWriter hash.Hash
	currentHashType := dc.GetCurrentHashType()
	switch currentHashType {
	case HashTypeSHA1:
		checksumWriter = sha1.New()
	case HashTypeSHA256:
		checksumWriter = sha256.New()
	case HashTypeSHA512:
		checksumWriter = sha512.New()
	default:
		checksumWriter = sha256.New() // Fallback to SHA-256 (default)
	}

	return &TempIndexWriter{
		file:           file,
		tempPath:       tempPath,
		headerWritten:  false,
		entryCount:     0,
		dc:             dc,
		checksumWriter: checksumWriter, // Use new hasher instance
	}, nil
}

// WriteIoVecBatch writes a batch of entries immediately using vectorio
// Implements immediate batching - writes whatever entries are provided right now
func (tiw *TempIndexWriter) WriteIoVecBatch(readyIoVecs []syscall.Iovec) error {
	if tiw.file == nil {
		return fmt.Errorf("temp index writer is closed")
	}

	// Write placeholder header if not already written
	if !tiw.headerWritten {
		if err := tiw.writePlaceholderHeader(); err != nil {
			return fmt.Errorf("failed to write placeholder header: %w", err)
		}
		tiw.headerWritten = true
	}

	// Write entries using vectorio (even empty batches or single entries)
	if len(readyIoVecs) > 0 {
		// CRITICAL: Add each entry to checksum BEFORE writing to file
		for _, iovec := range readyIoVecs {
			// Convert IoVec to []byte for checksum calculation
			entryBytes := unsafe.Slice((*byte)(unsafe.Pointer(iovec.Base)), int(iovec.Len))
			tiw.checksumWriter.Write(entryBytes)
		}

		if err := tiw.writeEntriesWithVectorIO(readyIoVecs); err != nil {
			return fmt.Errorf("failed to write entries batch: %w", err)
		}

		// Update entry count
		tiw.entryCount += uint32(len(readyIoVecs))
	}

	return nil
}

// writePlaceholderHeader writes initial header with zero entry count
// Final header with correct count and checksum will be written in Close()
func (tiw *TempIndexWriter) writePlaceholderHeader() error {
	// Create placeholder header (will be rewritten in Close() with correct count/checksum)
	header := indexHeader{}
	header.SetHeaderForWritableIndex(tiw.dc.signature, tiw.dc.version, 0, 0, tiw.dc.GetCurrentHashType())

	// Create header IoVec
	headerIovec := syscall.Iovec{
		Base: (*byte)(unsafe.Pointer(&header)),
		Len:  uint64(HeaderSize),
	}

	// Write header using vectorio
	if nw, err := vectorio.WritevRaw(tiw.file.Fd(), []syscall.Iovec{headerIovec}); err != nil {
		return fmt.Errorf("failed to write placeholder header with vectorio: %w", err)
	} else if nw != HeaderSize {
		return fmt.Errorf("placeholder header write incomplete: wrote %d bytes, expected %d", nw, HeaderSize)
	}

	return nil
}

// writeEntriesWithVectorIO writes entries using vectorio with chunking for IOV_MAX
func (tiw *TempIndexWriter) writeEntriesWithVectorIO(entryIovecs []syscall.Iovec) error {
	if len(entryIovecs) == 0 {
		return nil // Empty batch - nothing to write
	}

	// Get system IOV_MAX limit for chunking
	maxIovecs := getSystemIOVMax()

	// Calculate expected total size for verification
	expectedTotal := 0
	for _, iovec := range entryIovecs {
		expectedTotal += int(iovec.Len)
	}

	totalWritten := 0

	// Write in chunks respecting IOV_MAX limit
	for offset := 0; offset < len(entryIovecs); offset += maxIovecs {
		end := min(offset+maxIovecs, len(entryIovecs))

		// Use slice without copying to avoid allocation
		chunk := entryIovecs[offset:end]

		if nw, err := vectorio.WritevRaw(tiw.file.Fd(), chunk); err != nil {
			return fmt.Errorf("failed to write entries chunk with vectorio: %w", err)
		} else {
			totalWritten += nw
		}
	}

	// Verify complete write
	if totalWritten != expectedTotal {
		return fmt.Errorf("entries write incomplete: wrote %d bytes, expected %d", totalWritten, expectedTotal)
	}

	return nil
}

// Close finalizes the temp index by writing correct header and syncing
func (tiw *TempIndexWriter) Close() error {
	if tiw.file == nil {
		return nil // Already closed
	}

	defer func() {
		_ = tiw.file.Close()
		tiw.file = nil
	}()

	// If no header was written, write empty index
	if !tiw.headerWritten {
		if err := tiw.writePlaceholderHeader(); err != nil {
			return fmt.Errorf("failed to write empty index header: %w", err)
		}
	}

	// Create final header with correct entry count
	header := indexHeader{}
	header.SetHeaderForWritableIndex(tiw.dc.signature, tiw.dc.version, tiw.entryCount, 0, tiw.dc.GetCurrentHashType())

	// Mark header as clean
	header.setClean()

	// Add header fields (excluding checksum) to running checksum
	tiw.addHeaderToChecksum(&header)

	// Finalize checksum and store in header
	finalChecksum := tiw.checksumWriter.Sum(nil)
	copy(header.Checksum[:], finalChecksum)

	// Rewrite header with correct count and checksum
	if _, err := tiw.file.Seek(0, 0); err != nil {
		return fmt.Errorf("failed to seek to beginning for final header: %w", err)
	}

	headerIovec := syscall.Iovec{
		Base: (*byte)(unsafe.Pointer(&header)),
		Len:  uint64(HeaderSize),
	}

	if nw, err := vectorio.WritevRaw(tiw.file.Fd(), []syscall.Iovec{headerIovec}); err != nil {
		return fmt.Errorf("failed to write final header with vectorio: %w", err)
	} else if nw != HeaderSize {
		return fmt.Errorf("final header write incomplete: wrote %d bytes, expected %d", nw, HeaderSize)
	}

	// Sync to disk
	if err := tiw.file.Sync(); err != nil {
		return fmt.Errorf("failed to sync temp index: %w", err)
	}

	return nil
}

// addHeaderToChecksum adds header fields (excluding checksum field) to running checksum
func (tiw *TempIndexWriter) addHeaderToChecksum(header *indexHeader) {
	// Serialize header WITHOUT checksum field (following existing pattern from index.go)
	headerBytes := (*[HeaderSize]byte)(unsafe.Pointer(header))
	checksumOffset := unsafe.Offsetof(header.Checksum)

	// Add header fields (up to but not including checksum) to running checksum
	tiw.checksumWriter.Write(headerBytes[:checksumOffset])
}

// WriteSerialised writes pre-serialised entry data to the temp index.
// The data slices are kept alive internally until writev() completes,
// preventing the dangling-pointer class of bugs that afflicts raw Iovec usage.
// Each element of entries must be a complete wire-format binaryEntry (as
// produced by EntrySerialiser.Serialise).
func (tiw *TempIndexWriter) WriteSerialised(entries [][]byte) error {
	if tiw.file == nil {
		return fmt.Errorf("temp index writer is closed")
	}
	if len(entries) == 0 {
		return nil
	}

	// Write placeholder header if not already written
	if !tiw.headerWritten {
		if err := tiw.writePlaceholderHeader(); err != nil {
			return fmt.Errorf("failed to write placeholder header: %w", err)
		}
		tiw.headerWritten = true
	}

	// Build Iovecs from the provided data slices.
	// The entries slice (and its []byte elements) stay on the stack frame
	// of this function until after writev completes.
	iovecs := make([]syscall.Iovec, len(entries))
	for i, data := range entries {
		// Update checksum before writing
		tiw.checksumWriter.Write(data)

		iovecs[i] = syscall.Iovec{
			Base: &data[0],
			Len:  uint64(len(data)),
		}
	}

	if err := tiw.writeEntriesWithVectorIO(iovecs); err != nil {
		return fmt.Errorf("failed to write serialised entries: %w", err)
	}

	tiw.entryCount += uint32(len(entries))
	return nil
}

// GetTempPath returns the temp file path
func (tiw *TempIndexWriter) GetTempPath() string {
	return tiw.tempPath
}

// GetEntryCount returns the number of entries written
func (tiw *TempIndexWriter) GetEntryCount() uint32 {
	return tiw.entryCount
}
