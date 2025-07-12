package dircachefilehash

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// UnifiedFilesystemScanIterator streams files directly from filesystem scanning
// using BinaryEntryInterface with integrated hash coordination via algorithmHashManager.
// This provides memory-efficient iteration with asynchronous hashing that
// maintains strict sorted order required by the Hwang-Lin algorithm.
type UnifiedFilesystemScanIterator struct {
	iteratorBase
	
	// Filesystem scanning
	dc                *DirectoryCache
	paths             []string
	scanChan          chan *scannedPath
	shutdownChan      chan struct{}
	scanComplete      bool
	scanError         error
	
	// Hash coordination
	hashManager       *algorithmHashManager
	completionChan    chan uint64                    // Receives completion notifications
	pendingJobs       map[uint64]BinaryEntryInterface // JobID → entry waiting for hash
	currentJobID      uint64                         // Next JobID we're expecting
	scanIndexFileName string                         // Scan index file name
	
	// Current state
	nextScanned       *scannedPath
	scanStarted       bool
	hashingStarted    bool
	
	// Synchronization
	jobMutex          sync.Mutex                     // Protects pendingJobs and currentJobID
	completionWait    map[uint64]chan struct{}       // JobID → completion signal
	waitMutex         sync.Mutex                     // Protects completionWait
}

// NewUnifiedFilesystemScanIterator creates a new enhanced iterator that scans
// the specified paths with integrated hash coordination using BinaryEntryInterface.
func NewUnifiedFilesystemScanIterator(dc *DirectoryCache, paths []string, name string, hashManager *algorithmHashManager) *UnifiedFilesystemScanIterator {
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
		hashManager:    hashManager,
		completionChan: make(chan uint64, 100),
		pendingJobs:    make(map[uint64]BinaryEntryInterface),
		currentJobID:   1, // JobIDs start at 1
		completionWait: make(map[uint64]chan struct{}),
	}
	
	// Register with hash manager for completion notifications
	if hashManager != nil {
		hashManager.RegisterIteratorNotification(iterator.completionChan)
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
	
	// Start hash completion monitoring if not already started
	if !ufsi.hashingStarted {
		ufsi.startHashCompletion()
		ufsi.hashingStarted = true
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
	
	// Phase 1: Iterator just creates entries with metadata (no hashing decisions here)
	// The Hwang-Lin callback will decide whether to hash based on comparison with existing entries
	
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

// createScanEntry creates a BEScanEntry from scannedPath by adding to scan index
func (ufsi *UnifiedFilesystemScanIterator) createScanEntry(scanned *scannedPath) (BinaryEntryInterface, error) {
	// Initialize scan index if needed
	if ufsi.scanIndexFileName == "" {
		var err error
		ufsi.scanIndexFileName, err = ufsi.createScanIndex()
		if err != nil {
			return nil, fmt.Errorf("failed to create scan index: %w", err)
		}
	}
	
	// Add entry to scan index and get the binary entry
	scanEntry, err := ufsi.dc.appendEntryToScanIndex(ufsi.scanIndexFileName, scanned)
	if err != nil {
		return nil, fmt.Errorf("failed to append entry to scan index: %w", err)
	}
	
	// Create binaryEntryRef from the binary entry and current scan
	entryRef := createBinaryEntryRef(scanEntry, ufsi.dc.currentScan)
	
	// Create BEScanEntry using the entry reference
	bescanEntry := NewBEScanEntry(entryRef)
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

// needsHash determines if the scanned file needs hashing by comparing with existing entry
// Uses the same proven logic as isFileChangedFromScanned but with proper null checks
func needsHash(existingEntry *binaryEntry, scanned *scannedPath) bool {
	// If no existing entry, file is new and needs hashing
	if existingEntry == nil {
		return true
	}
	
	// If no scanned info, assume needs hashing
	if scanned == nil || scanned.StatInfo == nil {
		return true
	}
	
	stat := scanned.StatInfo
	
	// Quick size check
	if existingEntry.FileSize != uint64(scanned.Info.Size()) {
		return true
	}
	
	// Check ownership
	if existingEntry.UID != stat.Uid || existingEntry.GID != stat.Gid {
		return true
	}
	
	// Check mode
	if existingEntry.Mode != uint32(scanned.Info.Mode()) {
		return true
	}
	
	// Check timestamps using wall time encoding
	currentCTime := encodeWallTime(stat.Ctim.Sec, stat.Ctim.Nsec)
	currentMTime := encodeWallTime(stat.Mtim.Sec, stat.Mtim.Nsec)
	
	return existingEntry.CTimeWall != currentCTime || existingEntry.MTimeWall != currentMTime
}

// getNextJobID returns the next JobID in sequence
func (ufsi *UnifiedFilesystemScanIterator) getNextJobID() uint64 {
	ufsi.jobMutex.Lock()
	defer ufsi.jobMutex.Unlock()
	
	jobID := ufsi.currentJobID
	ufsi.currentJobID++
	return jobID
}

// submitHashJob submits a hash job to the algorithm hash manager
func (ufsi *UnifiedFilesystemScanIterator) submitHashJob(jobID uint64, scanned *scannedPath, scanEntry BinaryEntryInterface) error {
	if ufsi.hashManager == nil {
		return fmt.Errorf("hash manager is nil")
	}
	
	// Get the underlying binaryEntryRef for hash job
	ref, hasRef := scanEntry.GetBinaryEntryRef()
	if !hasRef {
		return fmt.Errorf("scan entry does not support hash job submission")
	}
	
	// Create hash job
	job := &hashJobStart{
		JobID:       jobID,
		FilePath:    scanned.AbsPath,
		IndexEntry:  ref,
		ScannedPath: scanned,
	}
	
	// Track pending job
	ufsi.jobMutex.Lock()
	ufsi.pendingJobs[jobID] = scanEntry
	ufsi.jobMutex.Unlock()
	
	// Create completion wait channel
	ufsi.waitMutex.Lock()
	ufsi.completionWait[jobID] = make(chan struct{})
	ufsi.waitMutex.Unlock()
	
	// Submit job
	ufsi.hashManager.SubmitHashJob(job)
	
	if IsDebugEnabled("unified-iterator") {
		fmt.Fprintf(os.Stderr, "[UNIFIED] Submitted hash job %d for file: %s\n", jobID, scanned.RelPath)
	}
	
	return nil
}

// waitForHashCompletion waits for the specified job to complete
func (ufsi *UnifiedFilesystemScanIterator) waitForHashCompletion(jobID uint64) error {
	ufsi.waitMutex.Lock()
	waitChan, exists := ufsi.completionWait[jobID]
	ufsi.waitMutex.Unlock()
	
	if !exists {
		return fmt.Errorf("no completion wait channel for job %d", jobID)
	}
	
	// Wait for completion or timeout
	select {
	case <-waitChan:
		// Job completed
		if IsDebugEnabled("unified-iterator") {
			fmt.Fprintf(os.Stderr, "[UNIFIED] Hash job %d completed\n", jobID)
		}
		return nil
		
	case <-time.After(30 * time.Second):
		return fmt.Errorf("timeout waiting for hash job %d completion", jobID)
		
	case <-ufsi.shutdownChan:
		return fmt.Errorf("iterator shutdown while waiting for hash job %d", jobID)
	}
}

// startHashCompletion starts the hash completion monitoring goroutine
func (ufsi *UnifiedFilesystemScanIterator) startHashCompletion() {
	go func() {
		for {
			select {
			case jobID, ok := <-ufsi.completionChan:
				if !ok {
					// Completion channel closed
					return
				}
				
				if IsDebugEnabled("unified-iterator") {
					fmt.Fprintf(os.Stderr, "[UNIFIED] Received completion notification for job %d\n", jobID)
				}
				
				// Signal completion to waiting goroutine
				ufsi.waitMutex.Lock()
				if waitChan, exists := ufsi.completionWait[jobID]; exists {
					close(waitChan)
					delete(ufsi.completionWait, jobID)
				}
				ufsi.waitMutex.Unlock()
				
				// Remove from pending jobs
				ufsi.jobMutex.Lock()
				delete(ufsi.pendingJobs, jobID)
				ufsi.jobMutex.Unlock()
				
			case <-ufsi.shutdownChan:
				return
			}
		}
	}()
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
	ufsi.markClosed()
	
	// Unregister from hash manager
	if ufsi.hashManager != nil {
		ufsi.hashManager.UnregisterIteratorNotification(ufsi.completionChan)
	}
	
	// Signal shutdown to scanning goroutine (only if not already closed)
	if !ufsi.scanComplete && ufsi.shutdownChan != nil {
		select {
		case <-ufsi.shutdownChan:
			// Already closed
		default:
			close(ufsi.shutdownChan)
		}
	}
	
	// Close completion channel
	if ufsi.completionChan != nil {
		close(ufsi.completionChan)
	}
	
	// Signal any waiting goroutines
	ufsi.waitMutex.Lock()
	for _, waitChan := range ufsi.completionWait {
		close(waitChan)
	}
	ufsi.completionWait = make(map[uint64]chan struct{})
	ufsi.waitMutex.Unlock()
	
	// Clean up scan index
	if ufsi.scanIndexFileName != "" {
		if err := ufsi.dc.cleanupCurrentScanFile(); err != nil && !os.IsNotExist(err) {
			// Non-fatal, but log the error
			fmt.Fprintf(os.Stderr, "Warning: failed to cleanup scan file: %v\n", err)
		}
		ufsi.scanIndexFileName = ""
	}
	
	// Drain any remaining entries from the channel
	if ufsi.scanChan != nil {
		go func() {
			for range ufsi.scanChan {
				// Drain the channel
			}
		}()
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

// GetPendingJobCount returns the number of pending hash jobs (for debugging)
func (ufsi *UnifiedFilesystemScanIterator) GetPendingJobCount() int {
	ufsi.jobMutex.Lock()
	defer ufsi.jobMutex.Unlock()
	return len(ufsi.pendingJobs)
}

// GetCurrentJobID returns the current job ID (for debugging)
func (ufsi *UnifiedFilesystemScanIterator) GetCurrentJobID() uint64 {
	ufsi.jobMutex.Lock()
	defer ufsi.jobMutex.Unlock()
	return ufsi.currentJobID
}