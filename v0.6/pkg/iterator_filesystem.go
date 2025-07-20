//go:build exclude
package dircachefilehash

import (
	"fmt"
	"unsafe"
)

// FilesystemScanIterator streams files directly from filesystem scanning.
// This provides a memory-efficient way to iterate through files without
// loading them all into memory first.
//
// The iterator uses the existing scanPath infrastructure but adapts it
// to the PathEntryIterator interface, converting scannedPath to binaryEntry
// on-the-fly as needed by the unified Hwang-Lin algorithm.
type FilesystemScanIterator struct {
	iteratorBase
	
	// Filesystem scanning
	dc                *DirectoryCache
	paths             []string
	scanChan          chan *scannedPath
	shutdownChan      chan struct{}
	scanComplete      bool
	scanError         error
	
	// Current state
	currentScanned    *scannedPath
	nextScanned       *scannedPath
	scanStarted       bool
}

// NewFilesystemScanIterator creates a new iterator that scans the specified paths.
// If paths is empty, it scans the entire repository root directory.
func NewFilesystemScanIterator(dc *DirectoryCache, paths []string, name string) *FilesystemScanIterator {
	if dc == nil {
		return &FilesystemScanIterator{
			iteratorBase: iteratorBase{
				name:      name,
				exhausted: true,
			},
		}
	}
	
	return &FilesystemScanIterator{
		iteratorBase: iteratorBase{name: name},
		dc:           dc,
		paths:        paths,
		scanChan:     make(chan *scannedPath, 100), // Buffered for performance
		shutdownChan: make(chan struct{}),
	}
}

// Next returns the next file entry from the filesystem scan
func (fsi *FilesystemScanIterator) Next() (*binaryEntry, error) {
	if err := fsi.checkClosed(); err != nil {
		return nil, err
	}
	
	// Check if we're already exhausted (e.g., due to nil DirectoryCache)
	if fsi.exhausted {
		return nil, nil
	}
	
	// Start scanning if not already started
	if !fsi.scanStarted {
		if err := fsi.startScan(); err != nil {
			fsi.markExhausted()
			return nil, fmt.Errorf("failed to start filesystem scan: %w", err)
		}
	}
	
	// If we have a next entry cached, use it
	if fsi.nextScanned != nil {
		fsi.currentScanned = fsi.nextScanned
		fsi.nextScanned = nil
		
		entry, err := fsi.convertToEntry(fsi.currentScanned)
		if err != nil {
			return nil, fmt.Errorf("failed to convert scanned path to entry: %w", err)
		}
		
		fsi.updateCurrentPath(entry)
		return entry, nil
	}
	
	// Read next entry from scan channel
	select {
	case scanned, ok := <-fsi.scanChan:
		if !ok {
			// Channel closed - scan is complete
			fsi.markExhausted()
			if fsi.scanError != nil {
				return nil, fsi.scanError
			}
			return nil, nil
		}
		
		fsi.currentScanned = scanned
		entry, err := fsi.convertToEntry(scanned)
		if err != nil {
			return nil, fmt.Errorf("failed to convert scanned path to entry: %w", err)
		}
		
		fsi.updateCurrentPath(entry)
		return entry, nil
		
	default:
		// No entry available yet, but scan might still be running
		if fsi.scanComplete {
			fsi.markExhausted()
			if fsi.scanError != nil {
				return nil, fsi.scanError
			}
			return nil, nil
		}
		
		// Wait for next entry with blocking read
		select {
		case scanned, ok := <-fsi.scanChan:
			if !ok {
				fsi.markExhausted()
				if fsi.scanError != nil {
					return nil, fsi.scanError
				}
				return nil, nil
			}
			
			fsi.currentScanned = scanned
			entry, err := fsi.convertToEntry(scanned)
			if err != nil {
				return nil, fmt.Errorf("failed to convert scanned path to entry: %w", err)
			}
			
			fsi.updateCurrentPath(entry)
			return entry, nil
			
		case <-fsi.shutdownChan:
			fsi.markExhausted()
			return nil, fmt.Errorf("filesystem scan was shutdown")
		}
	}
}

// startScan begins the filesystem scanning in a separate goroutine
func (fsi *FilesystemScanIterator) startScan() error {
	if fsi.scanStarted {
		return nil
	}
	
	if fsi.dc == nil {
		return fmt.Errorf("DirectoryCache is nil")
	}
	
	fsi.scanStarted = true
	
	// Start scanning in background goroutine
	go func() {
		defer func() {
			fsi.scanComplete = true
			// scanPath already closes the channel, so we don't need to close it
		}()
		
		if err := fsi.dc.scanPath(fsi.paths, fsi.scanChan, fsi.shutdownChan); err != nil {
			fsi.scanError = err
		}
	}()
	
	return nil
}

// convertToEntry converts a scannedPath to a binaryEntry
// This mimics the logic in writeBinaryEntryToMmap but creates a standalone entry
func (fsi *FilesystemScanIterator) convertToEntry(scanned *scannedPath) (*binaryEntry, error) {
	if scanned == nil {
		return nil, fmt.Errorf("scanned path is nil")
	}
	
	// Calculate entry size (same logic as appendEntryToScanIndex)
	baseSize := int(unsafe.Sizeof(binaryEntry{}))
	totalSize := baseSize + len(scanned.RelPath) + 1 // +1 for null terminator
	padding := (8 - (totalSize % 8)) % 8
	entrySize := totalSize + padding
	
	// Allocate memory for the entry
	data := make([]byte, entrySize)
	entry := (*binaryEntry)(unsafe.Pointer(&data[0]))
	
	// Populate the entry (similar to writeBinaryEntryToMmap)
	entry.Size = uint32(entrySize)
	entry.CTimeWall = encodeWallTime(scanned.StatInfo.Ctim.Sec, scanned.StatInfo.Ctim.Nsec)
	entry.MTimeWall = encodeWallTime(scanned.StatInfo.Mtim.Sec, scanned.StatInfo.Mtim.Nsec)
	entry.Dev = uint32(scanned.StatInfo.Dev)
	entry.Ino = uint32(scanned.StatInfo.Ino)
	entry.Mode = uint32(scanned.Info.Mode())
	entry.UID = uint32(scanned.StatInfo.Uid)
	entry.GID = uint32(scanned.StatInfo.Gid)
	entry.FileSize = uint64(scanned.Info.Size())
	
	// Set hash type but leave hash empty (will be filled later if needed)
	entry.HashType = uint16(HashTypeSHA1) // Default hash type
	// entry.Hash remains zero-initialized
	
	// Set entry flags
	entry.EntryFlags = 0 // Not deleted by default
	
	// Copy the path after the struct
	pathOffset := baseSize
	copy(data[pathOffset:], scanned.RelPath)
	data[pathOffset+len(scanned.RelPath)] = 0 // null terminator
	
	return entry, nil
}

// Close stops the filesystem scan and releases resources
func (fsi *FilesystemScanIterator) Close() error {
	fsi.markClosed()
	
	// Signal shutdown to scanning goroutine (only if not already closed)
	if !fsi.scanComplete && fsi.shutdownChan != nil {
		select {
		case <-fsi.shutdownChan:
			// Already closed
		default:
			close(fsi.shutdownChan)
		}
	}
	
	// Drain any remaining entries from the channel
	if fsi.scanChan != nil {
		go func() {
			for range fsi.scanChan {
				// Drain the channel
			}
		}()
	}
	
	return nil
}

// HasNext returns true if there might be more entries available
// Note: This is a hint and may return true even when no more entries exist
func (fsi *FilesystemScanIterator) HasNext() bool {
	if fsi.exhausted || fsi.closed {
		return false
	}
	
	// If we have a cached next entry, we definitely have more
	if fsi.nextScanned != nil {
		return true
	}
	
	// If scan is complete and channel is empty, no more entries
	if fsi.scanComplete {
		select {
		case <-fsi.scanChan:
			// There was an entry available
			return true
		default:
			// No entries available and scan is complete
			return false
		}
	}
	
	// Scan is still running, so there might be more entries
	return true
}