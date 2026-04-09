package dircachefilehash

import (
	"fmt"
	"os"
	"sync"
)

// UnifiedFilesystemScanIterator streams files directly from filesystem scanning
// using BinaryEntryInterface. This provides memory-efficient iteration that
// maintains strict sorted order required by the Hwang-Lin algorithm.
// Hash coordination is handled by callbacks, not the iterator.
type UnifiedFilesystemScanIterator struct {
	iteratorBase
	
	// Filesystem scanning
	dc                *DirectoryCache
	paths             []string
	scanChan          chan *scannedPath
	shutdownChan      chan struct{}
	shutdownOnce      sync.Once
	scanComplete      bool
	scanError         error
	scanIndexFileName string                         // Scan index file name
	
	// Current state
	nextScanned       *scannedPath
	scanStarted       bool
}

// NewUnifiedFilesystemScanIterator creates a new iterator that scans
// the specified paths using BinaryEntryInterface.
func NewUnifiedFilesystemScanIterator(dc *DirectoryCache, paths []string, name string) *UnifiedFilesystemScanIterator {
	if dc == nil {
		return &UnifiedFilesystemScanIterator{
			iteratorBase: iteratorBase{
				name:      name,
				exhausted: true,
			},
		}
	}
	
	iterator := &UnifiedFilesystemScanIterator{
		iteratorBase:   iteratorBase{name: name},
		dc:             dc,
		paths:          paths,
		scanChan:       make(chan *scannedPath, 100), // Buffered for performance
		shutdownChan:   make(chan struct{}),
	}
	
	return iterator
}

// Next returns the next file entry from the filesystem scan as BinaryEntryInterface
func (ufsi *UnifiedFilesystemScanIterator) Next() (BinaryEntryInterface, error) {
	if err := ufsi.checkClosed(); err != nil {
		return nil, err
	}
	
	// Check if we're already exhausted (e.g., due to nil DirectoryCache)
	if ufsi.exhausted {
		return nil, nil
	}
	
	// Start scanning if not already started
	if !ufsi.scanStarted {
		if err := ufsi.startScan(); err != nil {
			ufsi.markExhausted()
			return nil, fmt.Errorf("failed to start filesystem scan: %w", err)
		}
	}
	
	
	// Get the next scanned file
	scanned, err := ufsi.getNextScannedFile()
	if err != nil {
		return nil, err
	}
	
	if scanned == nil {
		// No more files - exhausted
		ufsi.markExhausted()
		return nil, nil
	}
	
	// Create BEScanEntry from scanned file
	scanEntry, err := ufsi.createScanEntry(scanned)
	if err != nil {
		return nil, fmt.Errorf("failed to create scan entry: %w", err)
	}
	
	// Iterator is synchronous: just creates entries with metadata
	// Hash coordination happens in callbacks using CallbackHashCoordinator pattern
	
	// Update current path and return the interface
	ufsi.updateCurrentPathFromInterface(scanEntry)
	return scanEntry, nil
}

// getNextScannedFile gets the next scanned file from the scan channel
func (ufsi *UnifiedFilesystemScanIterator) getNextScannedFile() (*scannedPath, error) {
	// If we have a next entry cached, use it
	if ufsi.nextScanned != nil {
		current := ufsi.nextScanned
		ufsi.nextScanned = nil
		return current, nil
	}
	
	// Read next entry from scan channel
	select {
	case scanned, ok := <-ufsi.scanChan:
		if !ok {
			// Channel closed - scan is complete
			if ufsi.scanError != nil {
				return nil, ufsi.scanError
			}
			return nil, nil
		}
		return scanned, nil
		
	default:
		// No entry available yet, but scan might still be running
		if ufsi.scanComplete {
			if ufsi.scanError != nil {
				return nil, ufsi.scanError
			}
			return nil, nil
		}
		
		// Wait for next entry with blocking read
		select {
		case scanned, ok := <-ufsi.scanChan:
			if !ok {
				if ufsi.scanError != nil {
					return nil, ufsi.scanError
				}
				return nil, nil
			}
			return scanned, nil
			
		case <-ufsi.shutdownChan:
			return nil, fmt.Errorf("filesystem scan was shutdown")
		}
	}
}

// createScanEntry creates a heap-allocated BEScanEntry from scannedPath
// v0.7: No scan index file needed - direct heap allocation with lazy hashing
func (ufsi *UnifiedFilesystemScanIterator) createScanEntry(scanned *scannedPath) (BinaryEntryInterface, error) {
	// v0.7: Create heap-allocated entry directly (no scan index file)
	// Entry will have metadata but no hash initially (lazy hashing)
	bescanEntry := NewBEScanEntry(scanned.RelPath, scanned.Info, scanned.StatInfo)
	return bescanEntry, nil
}

// createScanIndex creates a new scan index for this iterator
func (ufsi *UnifiedFilesystemScanIterator) createScanIndex() (string, error) {
	// Generate unique scan index name using DirectoryCache method
	scanFileName := ufsi.dc.generateScanFileName()
	
	// Initialize scan index file
	if err := ufsi.dc.initialiseScanIndex(scanFileName); err != nil {
		return "", fmt.Errorf("failed to initialize scan index %s: %w", scanFileName, err)
	}
	
	return scanFileName, nil
}




// startScan begins the filesystem scanning in a separate goroutine
func (ufsi *UnifiedFilesystemScanIterator) startScan() error {
	if ufsi.scanStarted {
		return nil
	}
	
	if ufsi.dc == nil {
		return fmt.Errorf("DirectoryCache is nil")
	}
	
	ufsi.scanStarted = true
	
	// Start scanning in background goroutine
	go func() {
		defer func() {
			ufsi.scanComplete = true
			// scanPath already closes the channel, so we don't need to close it
		}()
		
		if err := ufsi.dc.scanPath(ufsi.paths, ufsi.scanChan, ufsi.shutdownChan); err != nil {
			ufsi.scanError = err
		}
	}()
	
	return nil
}

// Close stops the filesystem scan and releases resources
func (ufsi *UnifiedFilesystemScanIterator) Close() error {
	// Check if already closed to prevent double-close
	if err := ufsi.checkClosed(); err != nil {
		return nil // Already closed, nothing to do
	}

	ufsi.markClosed()

	// Signal shutdown to scanning goroutine (sync.Once prevents double-close panic)
	if !ufsi.scanComplete && ufsi.shutdownChan != nil {
		ufsi.shutdownOnce.Do(func() {
			close(ufsi.shutdownChan)
		})
	}

	// Clean up scan index
	if ufsi.scanIndexFileName != "" {
		if err := ufsi.dc.cleanupCurrentScanFile(); err != nil && !os.IsNotExist(err) {
			// Non-fatal, but log the error
			fmt.Fprintf(os.Stderr, "Warning: failed to cleanup scan file: %v\n", err)
		}
		ufsi.scanIndexFileName = ""
	}
	
	// Drain any remaining entries from the channel (non-blocking)
	if ufsi.scanChan != nil {
		// Non-blocking drain - just empty what's currently buffered.
		// Must check ok to stop when channel is closed (closed channels
		// return zero values on every read, causing infinite loops).
	drainLoop:
		for {
			select {
			case _, ok := <-ufsi.scanChan:
				if !ok {
					break drainLoop // channel closed
				}
			default:
				break drainLoop // nothing buffered
			}
		}
	}

	return nil
}

// HasNext returns true if there might be more entries available
func (ufsi *UnifiedFilesystemScanIterator) HasNext() bool {
	if ufsi.exhausted || ufsi.closed {
		return false
	}
	
	// If we have a cached next entry, we definitely have more
	if ufsi.nextScanned != nil {
		return true
	}
	
	// If scan is complete and channel is empty, no more entries
	if ufsi.scanComplete {
		select {
		case <-ufsi.scanChan:
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

