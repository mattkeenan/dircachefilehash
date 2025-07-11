package dircachefilehash

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// EnhancedFilesystemScanIterator streams files directly from filesystem scanning
// with integrated hash coordination using algorithmHashManager.
// This provides memory-efficient iteration with asynchronous hashing that
// maintains strict sorted order required by the Hwang-Lin algorithm.
type EnhancedFilesystemScanIterator struct {
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
	pendingJobs       map[uint64]*binaryEntryRef     // JobID → entry waiting for hash
	currentJobID      uint64                         // Next JobID we're expecting
	scanIndexFileName string                         // Scan index file name
	
	// Current state
	currentScanned    *scannedPath
	nextScanned       *scannedPath
	scanStarted       bool
	hashingStarted    bool
	
	// Synchronization
	jobMutex          sync.Mutex                     // Protects pendingJobs and currentJobID
	completionWait    map[uint64]chan struct{}       // JobID → completion signal
	waitMutex         sync.Mutex                     // Protects completionWait
}

// NewEnhancedFilesystemScanIterator creates a new enhanced iterator that scans
// the specified paths with integrated hash coordination.
func NewEnhancedFilesystemScanIterator(dc *DirectoryCache, paths []string, name string, hashManager *algorithmHashManager) *EnhancedFilesystemScanIterator {
	if dc == nil {
		return &EnhancedFilesystemScanIterator{
			iteratorBase: iteratorBase{
				name:      name,
				exhausted: true,
			},
		}
	}
	
	iterator := &EnhancedFilesystemScanIterator{
		iteratorBase:   iteratorBase{name: name},
		dc:             dc,
		paths:          paths,
		scanChan:       make(chan *scannedPath, 100), // Buffered for performance
		shutdownChan:   make(chan struct{}),
		hashManager:    hashManager,
		completionChan: make(chan uint64, 100),
		pendingJobs:    make(map[uint64]*binaryEntryRef),
		currentJobID:   1, // JobIDs start at 1
		completionWait: make(map[uint64]chan struct{}),
	}
	
	// Register with hash manager for completion notifications
	if hashManager != nil {
		hashManager.RegisterIteratorNotification(iterator.completionChan)
	}
	
	return iterator
}

// Next returns the next file entry from the filesystem scan with valid hash
func (efsi *EnhancedFilesystemScanIterator) Next() (*binaryEntry, error) {
	if err := efsi.checkClosed(); err != nil {
		return nil, err
	}
	
	// Check if we're already exhausted (e.g., due to nil DirectoryCache)
	if efsi.exhausted {
		return nil, nil
	}
	
	// Start scanning if not already started
	if !efsi.scanStarted {
		if err := efsi.startScan(); err != nil {
			efsi.markExhausted()
			return nil, fmt.Errorf("failed to start filesystem scan: %w", err)
		}
	}
	
	// Start hash completion monitoring if not already started
	if !efsi.hashingStarted {
		efsi.startHashCompletion()
		efsi.hashingStarted = true
	}
	
	// Get the next scanned file
	scanned, err := efsi.getNextScannedFile()
	if err != nil {
		return nil, err
	}
	
	if scanned == nil {
		// No more files - exhausted
		efsi.markExhausted()
		return nil, nil
	}
	
	// Add to scan index and get entry reference
	entryRef, err := efsi.addToScanIndex(scanned)
	if err != nil {
		return nil, fmt.Errorf("failed to add to scan index: %w", err)
	}
	
	// Check if entry needs hashing
	if efsi.needsHashing(scanned) {
		// Submit hash job and wait for completion
		jobID := efsi.getNextJobID()
		if err := efsi.submitHashJob(jobID, scanned, entryRef); err != nil {
			return nil, fmt.Errorf("failed to submit hash job: %w", err)
		}
		
		// Wait for hash completion
		if err := efsi.waitForHashCompletion(jobID); err != nil {
			return nil, fmt.Errorf("failed to wait for hash completion: %w", err)
		}
	}
	
	// Get the final entry (now with valid hash if needed)
	entry := entryRef.GetBinaryEntry()
	if entry == nil {
		return nil, fmt.Errorf("failed to get binary entry after hashing")
	}
	
	efsi.updateCurrentPath(entry)
	return entry, nil
}

// getNextScannedFile gets the next scanned file from the scan channel
func (efsi *EnhancedFilesystemScanIterator) getNextScannedFile() (*scannedPath, error) {
	// If we have a next entry cached, use it
	if efsi.nextScanned != nil {
		current := efsi.nextScanned
		efsi.nextScanned = nil
		return current, nil
	}
	
	// Read next entry from scan channel
	select {
	case scanned, ok := <-efsi.scanChan:
		if !ok {
			// Channel closed - scan is complete
			if efsi.scanError != nil {
				return nil, efsi.scanError
			}
			return nil, nil
		}
		return scanned, nil
		
	default:
		// No entry available yet, but scan might still be running
		if efsi.scanComplete {
			if efsi.scanError != nil {
				return nil, efsi.scanError
			}
			return nil, nil
		}
		
		// Wait for next entry with blocking read
		select {
		case scanned, ok := <-efsi.scanChan:
			if !ok {
				if efsi.scanError != nil {
					return nil, efsi.scanError
				}
				return nil, nil
			}
			return scanned, nil
			
		case <-efsi.shutdownChan:
			return nil, fmt.Errorf("filesystem scan was shutdown")
		}
	}
}

// addToScanIndex adds the scanned file to the scan index and returns entry reference
func (efsi *EnhancedFilesystemScanIterator) addToScanIndex(scanned *scannedPath) (binaryEntryRef, error) {
	// Initialize scan index if needed
	if efsi.scanIndexFileName == "" {
		var err error
		efsi.scanIndexFileName, err = efsi.createScanIndex()
		if err != nil {
			return binaryEntryRef{}, fmt.Errorf("failed to create scan index: %w", err)
		}
	}
	
	// Add entry to scan index (similar to appendEntryToScanIndex)
	binaryEntry, err := efsi.dc.appendEntryToScanIndex(efsi.scanIndexFileName, scanned)
	if err != nil {
		return binaryEntryRef{}, fmt.Errorf("failed to append entry to scan index: %w", err)
	}
	
	// Create entry reference from the binary entry
	entryRef := binaryEntryRef{
		// We need to create a proper reference from the binary entry
		// For now, we'll store the pointer directly
		// This is a simplified approach for the enhanced iterator
	}
	
	return entryRef, nil
}

// createScanIndex creates a new scan index for this iterator
func (efsi *EnhancedFilesystemScanIterator) createScanIndex() (string, error) {
	// Generate unique scan index name using DirectoryCache method
	scanFileName := efsi.dc.generateScanFileName()
	
	// Initialize scan index file
	if err := efsi.dc.initialiseScanIndex(scanFileName); err != nil {
		return "", fmt.Errorf("failed to initialize scan index %s: %w", scanFileName, err)
	}
	
	return scanFileName, nil
}

// needsHashing determines if the scanned file needs hashing
func (efsi *EnhancedFilesystemScanIterator) needsHashing(scanned *scannedPath) bool {
	// For now, assume all files need hashing
	// In a real implementation, this would check against existing indices
	// to determine if the file has changed and needs re-hashing
	return true
}

// getNextJobID returns the next JobID in sequence
func (efsi *EnhancedFilesystemScanIterator) getNextJobID() uint64 {
	efsi.jobMutex.Lock()
	defer efsi.jobMutex.Unlock()
	
	jobID := efsi.currentJobID
	efsi.currentJobID++
	return jobID
}

// submitHashJob submits a hash job to the algorithm hash manager
func (efsi *EnhancedFilesystemScanIterator) submitHashJob(jobID uint64, scanned *scannedPath, entryRef binaryEntryRef) error {
	if efsi.hashManager == nil {
		return fmt.Errorf("hash manager is nil")
	}
	
	// Create hash job
	job := &hashJobStart{
		JobID:       jobID,
		FilePath:    scanned.AbsPath,
		IndexEntry:  entryRef,
		ScannedPath: scanned,
	}
	
	// Track pending job
	efsi.jobMutex.Lock()
	efsi.pendingJobs[jobID] = &entryRef
	efsi.jobMutex.Unlock()
	
	// Create completion wait channel
	efsi.waitMutex.Lock()
	efsi.completionWait[jobID] = make(chan struct{})
	efsi.waitMutex.Unlock()
	
	// Submit job
	efsi.hashManager.SubmitHashJob(job)
	
	if IsDebugEnabled("enhanced-iterator") {
		fmt.Fprintf(os.Stderr, "[ENHANCED] Submitted hash job %d for file: %s\n", jobID, scanned.RelPath)
	}
	
	return nil
}

// waitForHashCompletion waits for the specified job to complete
func (efsi *EnhancedFilesystemScanIterator) waitForHashCompletion(jobID uint64) error {
	efsi.waitMutex.Lock()
	waitChan, exists := efsi.completionWait[jobID]
	efsi.waitMutex.Unlock()
	
	if !exists {
		return fmt.Errorf("no completion wait channel for job %d", jobID)
	}
	
	// Wait for completion or timeout
	select {
	case <-waitChan:
		// Job completed
		if IsDebugEnabled("enhanced-iterator") {
			fmt.Fprintf(os.Stderr, "[ENHANCED] Hash job %d completed\n", jobID)
		}
		return nil
		
	case <-time.After(30 * time.Second):
		return fmt.Errorf("timeout waiting for hash job %d completion", jobID)
		
	case <-efsi.shutdownChan:
		return fmt.Errorf("iterator shutdown while waiting for hash job %d", jobID)
	}
}

// startHashCompletion starts the hash completion monitoring goroutine
func (efsi *EnhancedFilesystemScanIterator) startHashCompletion() {
	go func() {
		for {
			select {
			case jobID, ok := <-efsi.completionChan:
				if !ok {
					// Completion channel closed
					return
				}
				
				if IsDebugEnabled("enhanced-iterator") {
					fmt.Fprintf(os.Stderr, "[ENHANCED] Received completion notification for job %d\n", jobID)
				}
				
				// Signal completion to waiting goroutine
				efsi.waitMutex.Lock()
				if waitChan, exists := efsi.completionWait[jobID]; exists {
					close(waitChan)
					delete(efsi.completionWait, jobID)
				}
				efsi.waitMutex.Unlock()
				
				// Remove from pending jobs
				efsi.jobMutex.Lock()
				delete(efsi.pendingJobs, jobID)
				efsi.jobMutex.Unlock()
				
			case <-efsi.shutdownChan:
				return
			}
		}
	}()
}

// startScan begins the filesystem scanning in a separate goroutine
func (efsi *EnhancedFilesystemScanIterator) startScan() error {
	if efsi.scanStarted {
		return nil
	}
	
	if efsi.dc == nil {
		return fmt.Errorf("DirectoryCache is nil")
	}
	
	efsi.scanStarted = true
	
	// Start scanning in background goroutine
	go func() {
		defer func() {
			efsi.scanComplete = true
			// scanPath already closes the channel, so we don't need to close it
		}()
		
		if err := efsi.dc.scanPath(efsi.paths, efsi.scanChan, efsi.shutdownChan); err != nil {
			efsi.scanError = err
		}
	}()
	
	return nil
}

// Close stops the filesystem scan and releases resources
func (efsi *EnhancedFilesystemScanIterator) Close() error {
	efsi.markClosed()
	
	// Unregister from hash manager
	if efsi.hashManager != nil {
		efsi.hashManager.UnregisterIteratorNotification(efsi.completionChan)
	}
	
	// Signal shutdown to scanning goroutine (only if not already closed)
	if !efsi.scanComplete && efsi.shutdownChan != nil {
		select {
		case <-efsi.shutdownChan:
			// Already closed
		default:
			close(efsi.shutdownChan)
		}
	}
	
	// Close completion channel
	if efsi.completionChan != nil {
		close(efsi.completionChan)
	}
	
	// Signal any waiting goroutines
	efsi.waitMutex.Lock()
	for _, waitChan := range efsi.completionWait {
		close(waitChan)
	}
	efsi.completionWait = make(map[uint64]chan struct{})
	efsi.waitMutex.Unlock()
	
	// Clean up scan index
	if efsi.scanIndexFileName != "" {
		if err := efsi.dc.cleanupCurrentScanFile(); err != nil && !os.IsNotExist(err) {
			// Non-fatal, but log the error
			fmt.Fprintf(os.Stderr, "Warning: failed to cleanup scan file: %v\n", err)
		}
		efsi.scanIndexFileName = ""
	}
	
	// Drain any remaining entries from the channel
	if efsi.scanChan != nil {
		go func() {
			for range efsi.scanChan {
				// Drain the channel
			}
		}()
	}
	
	return nil
}

// HasNext returns true if there might be more entries available
func (efsi *EnhancedFilesystemScanIterator) HasNext() bool {
	if efsi.exhausted || efsi.closed {
		return false
	}
	
	// If we have a cached next entry, we definitely have more
	if efsi.nextScanned != nil {
		return true
	}
	
	// If scan is complete and channel is empty, no more entries
	if efsi.scanComplete {
		select {
		case <-efsi.scanChan:
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
func (efsi *EnhancedFilesystemScanIterator) GetPendingJobCount() int {
	efsi.jobMutex.Lock()
	defer efsi.jobMutex.Unlock()
	return len(efsi.pendingJobs)
}

// GetCurrentJobID returns the current job ID (for debugging)
func (efsi *EnhancedFilesystemScanIterator) GetCurrentJobID() uint64 {
	efsi.jobMutex.Lock()
	defer efsi.jobMutex.Unlock()
	return efsi.currentJobID
}