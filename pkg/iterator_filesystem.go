package dircachefilehash

import (
	"context"
	"fmt"
	"sync/atomic"
)

// FilesystemScanIterator streams files directly from filesystem scanning
// using BinaryEntryInterface. This provides memory-efficient iteration that
// maintains strict sorted order required by the Hwang-Lin algorithm.
// Hash coordination is handled by callbacks, not the iterator.
type FilesystemScanIterator struct {
	iteratorBase

	// Filesystem scanning
	sr           *ScanRun
	paths        []string
	scanChan     chan *scannedPath
	ctx          context.Context
	cancel       context.CancelFunc
	scanComplete atomic.Bool
	scanError    error

	// Current state
	nextScanned *scannedPath
	scanStarted bool
}

// NewFilesystemScanIterator creates a new iterator that scans
// the specified paths using BinaryEntryInterface. sr carries the
// walker, ignore predicate, and territory back-reference (sr.Store).
func NewFilesystemScanIterator(ctx context.Context, sr *ScanRun, paths []string, name string) *FilesystemScanIterator {
	if sr == nil || sr.Store == nil {
		return &FilesystemScanIterator{
			iteratorBase: iteratorBase{
				name:      name,
				exhausted: true,
			},
		}
	}

	childCtx, cancel := context.WithCancel(ctx)

	iterator := &FilesystemScanIterator{
		iteratorBase: iteratorBase{name: name},
		sr:           sr,
		paths:        paths,
		scanChan:     make(chan *scannedPath, 100), // Buffered for performance
		ctx:          childCtx,
		cancel:       cancel,
	}

	return iterator
}

// Next returns the next file entry from the filesystem scan as BinaryEntryInterface
func (ufsi *FilesystemScanIterator) Next() (BinaryEntryInterface, error) {
	if err := ufsi.checkClosed(); err != nil {
		return nil, err
	}

	// Check if we're already exhausted (e.g., due to nil MetaStore)
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
	scanEntry := ufsi.createScanEntry(scanned)

	// Iterator is synchronous: just creates entries with metadata
	// Hash coordination happens in callbacks using CallbackHashCoordinator pattern

	// Update current path and return the interface
	ufsi.updateCurrentPathFromInterface(scanEntry)
	return scanEntry, nil
}

// getNextScannedFile gets the next scanned file from the scan channel
func (ufsi *FilesystemScanIterator) getNextScannedFile() (*scannedPath, error) {
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
		if ufsi.scanComplete.Load() {
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

		case <-ufsi.ctx.Done():
			return nil, fmt.Errorf("filesystem scan interrupted: %w", ufsi.ctx.Err())
		}
	}
}

// createScanEntry creates a heap-allocated BEScanEntry from scannedPath
// v0.7: No scan index file needed - direct heap allocation with lazy hashing
func (ufsi *FilesystemScanIterator) createScanEntry(scanned *scannedPath) BinaryEntryInterface {
	// v0.7: Create heap-allocated entry directly (no scan index file)
	// Entry will have metadata but no hash initially (lazy hashing)
	return NewBEScanEntry(scanned.RelPath, scanned.Info, scanned.StatInfo)
}

// startScan begins the filesystem scanning in a separate goroutine
func (ufsi *FilesystemScanIterator) startScan() error {
	if ufsi.scanStarted {
		return nil
	}

	if ufsi.sr == nil || ufsi.sr.Store == nil {
		return fmt.Errorf("ScanRun or its Store is nil")
	}

	ufsi.scanStarted = true

	// Start scanning in background goroutine
	go func() {
		defer func() {
			ufsi.scanComplete.Store(true)
			// scanPath already closes the channel, so we don't need to close it
		}()

		if err := ufsi.sr.Walker.Walk(ufsi.ctx, ufsi.paths, ufsi.sr, ufsi.scanChan); err != nil {
			ufsi.scanError = err
		}
	}()

	return nil
}

// Close stops the filesystem scan and releases resources
func (ufsi *FilesystemScanIterator) Close() error {
	// Check if already closed to prevent double-close
	if err := ufsi.checkClosed(); err != nil {
		return nil //nolint:nilerr // intentional: double-close is a no-op, not an error
	}

	ufsi.markClosed()

	// Signal shutdown to scanning goroutine
	if ufsi.cancel != nil {
		ufsi.cancel()
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
func (ufsi *FilesystemScanIterator) HasNext() bool {
	if ufsi.exhausted || ufsi.closed {
		return false
	}

	// If we have a cached next entry, we definitely have more
	if ufsi.nextScanned != nil {
		return true
	}

	// If scan is complete and channel is empty, no more entries
	if ufsi.scanComplete.Load() {
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
