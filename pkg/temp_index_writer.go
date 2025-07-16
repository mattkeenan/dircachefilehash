package dircachefilehash

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"github.com/google/vectorio"
)

// TempIndexWriter handles iterative IoVec batch writing to temp index files
// Implements immediate batching - writes whatever entries are ready right now
type TempIndexWriter struct {
	file         *os.File
	tempPath     string
	headerWritten bool
	entryCount   uint32
	dc           *DirectoryCache
}

// NewTempIndexWriter creates a new temp index writer for the specified temp file
func NewTempIndexWriter(dc *DirectoryCache, tempPath string) (*TempIndexWriter, error) {
	// Create temp index file
	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp index file %s: %w", tempPath, err)
	}

	return &TempIndexWriter{
		file:         file,
		tempPath:     tempPath,
		headerWritten: false,
		entryCount:   0,
		dc:           dc,
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
	header.SetHeaderForWritableIndex(tiw.dc.signature, tiw.dc.version, 0, 0, HashTypeSHA1)

	// Create header IoVec
	headerIovec := syscall.Iovec{
		Base: (*byte)(unsafe.Pointer(&header)),
		Len:  uint64(HeaderSize),
	}

	// Write header using vectorio
	if nw, err := vectorio.WritevRaw(uintptr(tiw.file.Fd()), []syscall.Iovec{headerIovec}); err != nil {
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
	maxIovecs, err := getSystemIOVMax()
	if err != nil {
		return fmt.Errorf("failed to get system IOV_MAX: %w", err)
	}

	// Calculate expected total size for verification
	expectedTotal := 0
	for _, iovec := range entryIovecs {
		expectedTotal += int(iovec.Len)
	}

	totalWritten := 0

	// Write in chunks respecting IOV_MAX limit
	for offset := 0; offset < len(entryIovecs); offset += maxIovecs {
		end := offset + maxIovecs
		if end > len(entryIovecs) {
			end = len(entryIovecs)
		}

		// Use slice without copying to avoid allocation
		chunk := entryIovecs[offset:end]

		if nw, err := vectorio.WritevRaw(uintptr(tiw.file.Fd()), chunk); err != nil {
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
		tiw.file.Close()
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
	header.SetHeaderForWritableIndex(tiw.dc.signature, tiw.dc.version, tiw.entryCount, 0, HashTypeSHA1)

	// Mark header as clean
	header.setClean()

	// Calculate checksum of the entire file
	if err := tiw.calculateAndStoreFileChecksum(&header); err != nil {
		return fmt.Errorf("failed to calculate file checksum: %w", err)
	}

	// Rewrite header with correct count and checksum
	if _, err := tiw.file.Seek(0, 0); err != nil {
		return fmt.Errorf("failed to seek to beginning for final header: %w", err)
	}

	headerIovec := syscall.Iovec{
		Base: (*byte)(unsafe.Pointer(&header)),
		Len:  uint64(HeaderSize),
	}

	if nw, err := vectorio.WritevRaw(uintptr(tiw.file.Fd()), []syscall.Iovec{headerIovec}); err != nil {
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

// calculateAndStoreFileChecksum calculates checksum of header+entries and stores in header
func (tiw *TempIndexWriter) calculateAndStoreFileChecksum(header *indexHeader) error {
	// Get file size to read all data
	stat, err := tiw.file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat temp index file: %w", err)
	}

	fileSize := stat.Size()
	if fileSize < HeaderSize {
		return fmt.Errorf("temp index file too small: %d bytes", fileSize)
	}

	// Read entire file for checksum calculation
	if _, err := tiw.file.Seek(0, 0); err != nil {
		return fmt.Errorf("failed to seek to beginning for checksum: %w", err)
	}

	fileData := make([]byte, fileSize)
	if n, err := tiw.file.Read(fileData); err != nil {
		return fmt.Errorf("failed to read temp index for checksum: %w", err)
	} else if int64(n) != fileSize {
		return fmt.Errorf("incomplete read for checksum: read %d, expected %d", n, fileSize)
	}

	// Calculate checksum (exclude checksum field from header)
	tiw.dc.calculateAndStoreHeaderChecksum(header, fileData[HeaderSize:], int(fileSize-HeaderSize))

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