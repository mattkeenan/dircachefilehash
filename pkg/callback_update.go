package dircachefilehash

import (
	"fmt"
	"os"
	"sync/atomic"
	"syscall"
	"unsafe"
	"github.com/google/vectorio"
)

// UpdateCallback implements HwangLinCallback for v0.7 direct temp index writing
// Writes entries directly to main.idx temp file during hwangLinUnified execution (no skiplist building)
type UpdateCallback struct {
	// v0.7 direct temp index writing
	dc                *DirectoryCache
	tempIndexFileName string                  // Temp main index filename for direct writing
	
	// Hash coordination with existing hashJobManager
	hashJobManager   *algorithmHashManager   // Existing hash manager (passed from caller)
	entryCounter     uint64                  // Internal counter for callback entries (used as path order ID)
	
	// Simple atomic counter for completion detection
	jobsInFlight     uint64                  // Atomic counter: inc on submit, dec on complete
	
	// Path order preservation via retire skiplist
	retireSkiplist   *skiplistWrapper        // Entries ready to retire, ordered by path order ID as context
	nextRetireIndex  uint64                  // Next path order ID sequence number expected for retirement
	pathOrderToEntry map[uint64]BinaryEntryInterface // Track entries by path order ID for completion lookup
	
	// Index writing - IoVec writer for temp index output
	tempIndexWriter  *TempIndexWriter        // IoVec writer for temp index output
}

// NewUpdateCallback creates a new UpdateCallback for v0.7 direct temp index writing
func NewUpdateCallback(dc *DirectoryCache, tempIndexFileName string, hashManager *algorithmHashManager) *UpdateCallback {
	return &UpdateCallback{
		// v0.7 direct temp index writing
		dc:                dc,
		tempIndexFileName: tempIndexFileName,
		
		// Hash coordination
		hashJobManager: hashManager,
		entryCounter:   0,
		
		// Path order preservation
		retireSkiplist:  NewSkiplistWrapper(16, "retire"),
		nextRetireIndex: 1, // Start retiring from path order ID 1
		pathOrderToEntry: make(map[uint64]BinaryEntryInterface),
		
		// Index writing
		tempIndexWriter: nil, // Will be initialized when first entry is written
	}
}

// OnComparison processes each comparison result following hwangLinCompareToSkiplist logic
func (uc *UpdateCallback) OnComparison(
	result ComparisonResult,
	leftEntry, rightEntry BinaryEntryInterface,
	leftPath, rightPath string,
) (bool, error) {
	if IsDebugEnabled("verbose-3") {
		fmt.Fprintf(os.Stderr, "[VERBOSE-3] UpdateCallback.OnComparison: result=%s, leftPath=%s, rightPath=%s, leftEntry!=nil=%t, rightEntry!=nil=%t\n", 
			result, leftPath, rightPath, leftEntry != nil, rightEntry != nil)
	}
	
	switch result {
	case ComparisonMatch:
		// Files exist in both - check if they differ (matches hwangLinCompareToSkiplist cmp == 0 case)
		if leftEntry != nil && rightEntry != nil {
			// Skip deleted entries in the index (left side)
			if isDeleted, err := leftEntry.IsDeleted(); err == nil && isDeleted {
				return true, nil // Continue processing, skip this entry
			}
			
			// Check if this file should still be indexed
			if !uc.dc.shouldIndex(rightPath) {
				// File exists but should no longer be indexed - skip (main.idx excludes deleted entries)
				return true, nil
			}

			// Check if the file needs hashing (has changed)
			if needsHash(leftEntry, rightEntry) {
				// File changed - use unified hash coordination
				if err := uc.SubmitAndOrWriteHash(rightEntry, "modified"); err != nil {
					return false, err
				}
			} else {
				// File unchanged - use unified hash coordination
				if err := uc.SubmitAndOrWriteHash(leftEntry, "unchanged"); err != nil {
					return false, err
				}
			}
		}
		
	case ComparisonRightFirst:
		// File only in scan (right side) - new file (matches hwangLinCompareToSkiplist cmp < 0 case)
		if rightEntry != nil {
			if IsDebugEnabled("verbose-3") {
				fmt.Fprintf(os.Stderr, "[VERBOSE-3] UpdateCallback: ComparisonRightFirst for %s - processing new file\n", rightPath)
			}
			// Check if this file should be indexed
			if !uc.dc.shouldIndex(rightPath) {
				if IsDebugEnabled("verbose-3") {
					fmt.Fprintf(os.Stderr, "[VERBOSE-3] UpdateCallback: Skipping %s - shouldIndex returned false\n", rightPath)
				}
				// File should not be indexed - skip without creating entry
				return true, nil
			}
			
			if IsDebugEnabled("verbose-3") {
				fmt.Fprintf(os.Stderr, "[VERBOSE-3] UpdateCallback: Creating scan entry for new file %s\n", rightPath)
			}
			// Use unified hash coordination for new file
			if err := uc.SubmitAndOrWriteHash(rightEntry, "new_file"); err != nil {
				return false, err
			}
		}
		
	case ComparisonLeftFirst:
		// File only in index (left side) - deleted file (matches hwangLinCompareToSkiplist cmp > 0 case)
		if leftEntry != nil {
			// Check if this file should still be indexed based on symlink and ignore rules
			if !uc.dc.shouldIndex(leftPath) {
				// File should not be indexed - skip without creating deleted entry
				return true, nil
			}

			// Skip already deleted entries
			if isDeleted, err := leftEntry.IsDeleted(); err == nil && isDeleted {
				return true, nil // Continue processing
			}

			// Skip deleted entries (main.idx excludes deleted entries per v0.7 architecture)
			return true, nil
		}
	}
	
	// Check completion queue from hashJobManager and merge completed entries to backlog
	uc.processCompletedHashJobs()
	
	// Create IoVec array from in-order entries (no gaps) and call writeIoVec to output temp index
	if err := uc.flushInOrderEntries(); err != nil {
		return false, err
	}
	
	return true, nil // Continue processing
}



// writeEntryToTempIndex writes a single entry to the temp index file using IoVec
func (uc *UpdateCallback) writeEntryToTempIndex(entry BinaryEntryInterface) error {
	if IsDebugEnabled("write") {
		if path, err := entry.RelativePath(); err == nil {
			VerboseLog(3, "[UPDATE-WRITE] writeEntryToTempIndex called for: %s", path)
		}
	}
	
	// TODO: Implement proper IoVec batch writing to temp index
	// For now, just log that writing would happen here - this will be implemented when TempIndexWriter is available
	if IsDebugEnabled("write") {
		VerboseLog(3, "[UPDATE-WRITE] WARNING: writeEntryToTempIndex is not implemented - no entries written!")
	}
	return nil
}

// OnLeftOnly handles remaining entries from left iterator (when right is exhausted)
func (uc *UpdateCallback) OnLeftOnly(entry BinaryEntryInterface, path string) (bool, error) {
	// Left entry exists but no right entry - this is a deleted file
	// Skip deleted entries (main.idx excludes deleted entries per v0.7 architecture)
	return true, nil
}

// OnRightOnly handles remaining entries from right iterator (when left is exhausted)  
func (uc *UpdateCallback) OnRightOnly(entry BinaryEntryInterface, path string) (bool, error) {
	// Right entry exists but no left entry - this is a new file
	// Check if this file should be indexed
	if !uc.dc.shouldIndex(path) {
		// File should not be indexed - skip without creating entry
		return true, nil
	}
	
	// Use unified hash coordination for new file
	if err := uc.SubmitAndOrWriteHash(entry, "new_file"); err != nil {
		return false, err
	}
	return true, nil
}

// OnStart is called before the algorithm begins processing
func (uc *UpdateCallback) OnStart(leftName, rightName string) error {
	if IsDebugEnabled("scanning") {
		fmt.Fprintf(os.Stderr, "[UPDATE] Starting unified update: left=%s, right=%s\n", leftName, rightName)
	}
	
	// v0.7: No skiplist to manage, just initialize temp index writer if needed
	// Temp index writer will be initialized on first write
	
	return nil
}

// OnComplete is called after the algorithm finishes processing
func (uc *UpdateCallback) OnComplete(err error) error {
	if IsDebugEnabled("scanning") {
		if err != nil {
			fmt.Fprintf(os.Stderr, "[UPDATE] Update completed with error: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "[UPDATE] Update completed successfully, entries written to temp index\n")
		}
	}
	
	// v0.7: Close temp index writer to finalize the temp index file
	if uc.tempIndexWriter != nil {
		tempPath := uc.tempIndexWriter.GetTempPath() // Hoist - call once
		
		if closeErr := uc.tempIndexWriter.Close(); closeErr != nil {
			if err == nil {
				// No previous error, return the close error
				return fmt.Errorf("failed to close temp index writer: %w", closeErr)
			} else {
				// Previous error exists, log close error but return original error
				fmt.Fprintf(os.Stderr, "[UPDATE] Warning: failed to close temp index writer: %v\n", closeErr)
			}
		}
		
		if IsDebugEnabled("write") {
			VerboseLog(3, "[UPDATE-WRITE] Temp index writer closed: %s (%d entries)", 
				tempPath, uc.tempIndexWriter.GetEntryCount())
		}
		
		// v0.7: Atomic rename temp index to main.idx after successful completion
		if err == nil {
			mainIndexPath := uc.dc.IndexFile
			
			// Check the size of the temp file before rename
			var tempFileSize int64 = -1
			if stat, statErr := os.Stat(tempPath); statErr == nil {
				tempFileSize = stat.Size()
			}
			
			if IsDebugEnabled("write") {
				VerboseLog(3, "[UPDATE-WRITE] Atomically renaming temp index: %s (%d bytes) -> %s", tempPath, tempFileSize, mainIndexPath)
			}
			
			if renameErr := os.Rename(tempPath, mainIndexPath); renameErr != nil {
				return fmt.Errorf("failed to atomically rename temp index to main index: %w", renameErr)
			}
			
			if IsDebugEnabled("write") {
				VerboseLog(3, "[UPDATE-WRITE] Successfully updated main index: %s", mainIndexPath)
			}
		} else {
			// Error occurred - clean up temp file
			if cleanupErr := os.Remove(tempPath); cleanupErr != nil {
				fmt.Fprintf(os.Stderr, "[UPDATE] Warning: failed to cleanup temp index file %s: %v\n", tempPath, cleanupErr)
			}
		}
	}
	
	return err // Return original error if any
}

// Name returns the name of this callback for debugging
func (uc *UpdateCallback) Name() string {
	return "update"
}

// SubmitAndOrWriteHash handles unified hash coordination for Update command
// This method coordinates hash requests and iterative writing to main.idx temp file
func (uc *UpdateCallback) SubmitAndOrWriteHash(entry BinaryEntryInterface, operation string) error {
	if IsDebugEnabled("hash") || IsDebugEnabled("write") {
		if path, err := entry.RelativePath(); err == nil {
			VerboseLog(3, "[UPDATE-HASH] SubmitAndOrWriteHash called for %s: %s", operation, path)
		}
	}
	
	// Process any completed hash jobs first (non-blocking)
	uc.processCompletedHashJobs()
	
	// Update callback writes to main.idx temp file for complete repository state
	if entry == nil {
		return nil // Nothing to process
	}
	
	// Check if this entry needs hashing and writing
	needsHashing := (operation == "new_file") || (operation == "modified")
	
	// CRITICAL: Main index excludes deleted entries (clean repository state)
	isDeleted, _ := entry.IsDeleted()
	needsWriting := !isDeleted // Only write non-deleted entries to main.idx
	
	if !needsWriting {
		// Deleted entries: no action needed for main.idx
		return nil
	}
	
	// Assign sequential path order ID to maintain callback order
	uc.entryCounter++
	pathOrderID := uc.entryCounter
	
	if needsHashing {
		// Submit hash job (parallel processing - don't park here)
		if IsDebugEnabled("hash") {
			if path, err := entry.RelativePath(); err == nil {
				VerboseLog(3, "[UPDATE-HASH] Submitting hash job for: %s (pathOrderID=%d)", path, pathOrderID)
			}
		}
		if err := uc.submitHashJobToManager(entry, pathOrderID); err != nil {
			return err
		}
		// Entry will be added to parkedSkiplist when hash completion arrives
	} else {
		// Already hashed - add directly to retireSkiplist with path order ID as context
		if IsDebugEnabled("write") {
			if path, err := entry.RelativePath(); err == nil {
				VerboseLog(3, "[UPDATE-WRITE] Adding unchanged file to retireSkiplist: %s (pathOrderID=%d)", path, pathOrderID)
			}
		}
		pathOrderStr := fmt.Sprintf("%d", pathOrderID)
		if entryRef, ok := entry.GetBinaryEntryRef(); ok {
			uc.retireSkiplist.Insert(entryRef, pathOrderStr)
		}
	}
	
	// Try to retire contiguous sequence from retireSkiplist
	return uc.retireContiguousEntries()
}


// submitHashJobToManager submits a hash job using the path order ID tracking system
func (uc *UpdateCallback) submitHashJobToManager(entry BinaryEntryInterface, pathOrderID uint64) error {
	// a) Hash job submission tracing
	if IsDebugEnabled("hash") {
		if path, err := entry.RelativePath(); err == nil {
			VerboseLog(3, "[HASH-SUBMIT] Submitting hash job for entry: %s", path)
		}
	}
	
	// Get the next job ID from hash manager
	jobID := uc.hashJobManager.GetNextJobID()
	
	// Set the job ID on the entry for tracking
	entry.SetHashJobID(jobID)
	
	// RequestHash() does housekeeping (sets flags, prevents duplicates)
	if err := entry.RequestHash(); err != nil {
		if IsDebugEnabled("hash") {
			VerboseLog(3, "[HASH-SUBMIT] Failed to set hash request flag: %v", err)
		}
		return err
	}
	
	// Get file path for hash job
	filePath, err := entry.RelativePath()
	if err != nil {
		if IsDebugEnabled("hash") {
			VerboseLog(3, "[HASH-SUBMIT] Failed to get relative path: %v", err)
		}
		return err
	}
	
	// Create hash job start structure using unified BinaryEntryInterface
	hashJob := &hashJobStart{
		JobID:    jobID,
		Cookie:   pathOrderID,
		FilePath: filePath,
		Entry:    entry, // Unified interface works for both mmap and heap entries
	}
	
	// Submit hash job to manager
	uc.hashJobManager.SubmitHashJob(hashJob)
	
	if IsDebugEnabled("hash") {
		VerboseLog(3, "[HASH-SUBMIT] Hash job submitted to manager: JobID=%d, Cookie=%d", jobID, pathOrderID)
	}
	
	// Store path order ID to entry mapping for completion lookup
	uc.pathOrderToEntry[pathOrderID] = entry
	
	// Only increment jobs in flight counter after successful submission
	atomic.AddUint64(&uc.jobsInFlight, 1)
	
	if IsDebugEnabled("hash") {
		VerboseLog(3, "[HASH-SUBMIT] Hash job submitted successfully, pathOrderID=%d, jobsInFlight=%d", pathOrderID, atomic.LoadUint64(&uc.jobsInFlight))
	}
	
	return nil
}

// processCompletedHashJobs checks for completed jobs and adds them to parking skiplist
func (uc *UpdateCallback) processCompletedHashJobs() {
	if uc.hashJobManager == nil {
		if IsDebugEnabled("hash") {
			VerboseLog(3, "[HASH-COMPLETE] No hash manager available for completion processing")
		}
		return
	}
	
	// Non-blocking read of completion channel (entries arrive in JobID order)
	for {
		select {
		case completion := <-uc.hashJobManager.CompletionChannel():
			// Completions arrive in JobID order thanks to algorithmHashManager
			// Add completed entry to retireSkiplist with path order ID as context for ordering
			pathOrderID := completion.Cookie
			
			if IsDebugEnabled("hash") {
				VerboseLog(3, "[HASH-COMPLETE] Received completion message: JobID=%d, Cookie=%d", completion.JobID, pathOrderID)
			}
			
			// Find the entry that this completion belongs to
			entry := uc.findEntryByPathOrderID(pathOrderID)
			if entry != nil {
				if path, err := entry.RelativePath(); err == nil {
					if IsDebugEnabled("hash") {
						VerboseLog(3, "[HASH-COMPLETE] Adding completed entry to retireSkiplist: %s (pathOrderID=%d)", path, pathOrderID)
					}
				}
				
				// Add to retire skiplist with path order ID as context
				pathOrderStr := fmt.Sprintf("%d", pathOrderID)
				if entryRef, ok := entry.GetBinaryEntryRef(); ok {
					uc.retireSkiplist.Insert(entryRef, pathOrderStr)
				}
				
				// Clean up path order ID to entry mapping since job is complete
				delete(uc.pathOrderToEntry, pathOrderID)
				
				// Decrement atomic counter when job completes
				atomic.AddUint64(&uc.jobsInFlight, ^uint64(0)) // Atomic decrement
				
				if IsDebugEnabled("hash") {
					remainingJobs := atomic.LoadUint64(&uc.jobsInFlight)
					VerboseLog(3, "[HASH-COMPLETE] Entry added to parkedSkiplist, remaining jobs: %d", remainingJobs)
				}
			} else {
				if IsDebugEnabled("hash") {
					VerboseLog(3, "[HASH-COMPLETE] Could not find entry for pathOrderID %d", pathOrderID)
				}
			}
		default:
			// No more completions available (non-blocking)
			return
		}
	}
}

// appendToBacklog adds an entry to the ready-to-write backlog  
func (uc *UpdateCallback) appendToBacklog(entry BinaryEntryInterface) {
	if IsDebugEnabled("hash") {
		if path, err := entry.RelativePath(); err == nil {
			VerboseLog(3, "[HASH-BACKLOG] Adding entry to backlog: %s (backlog size before: %d)", path, len(uc.backlog))
		}
	}
	
	// f) Hashed entries put in completion backlog queue
	uc.backlog = append(uc.backlog, entry)
	
	if IsDebugEnabled("hash") {
		VerboseLog(3, "[HASH-BACKLOG] Entry added to backlog, new backlog size: %d", len(uc.backlog))
	}
}

// flushInOrderEntries processes backlog and pending entries for ordered writing
// Implements architecture-specified immediate IoVec batching
func (uc *UpdateCallback) flushInOrderEntries() error {
	// g) In-order tests and IoVec creation tracing
	if IsDebugEnabled("hash") || IsDebugEnabled("write") {
		VerboseLog(3, "[UPDATE-FLUSH] Starting flush: backlog=%d pending=%d nextFlushIndex=%d", len(uc.backlog), len(uc.pendingEntries), uc.nextFlushIndex)
	}
	
	// Initialize temp index writer if needed
	if uc.tempIndexWriter == nil {
		if IsDebugEnabled("hash") {
			VerboseLog(3, "[UPDATE-FLUSH] Creating new TempIndexWriter for: %s", uc.tempIndexFileName)
		}
		writer, err := NewTempIndexWriter(uc.dc, uc.tempIndexFileName)
		if err != nil {
			return fmt.Errorf("failed to create temp index writer: %w", err)
		}
		uc.tempIndexWriter = writer
		if IsDebugEnabled("hash") {
			VerboseLog(3, "[UPDATE-FLUSH] TempIndexWriter created successfully")
		}
	}
	
	// Use counter to check for contiguous completed entries (no gaps)
	var readyIoVecs []syscall.Iovec
	backlogProcessed := 0
	
	// Process backlog entries that can be written in order
	for len(uc.backlog) > 0 {
		entry := uc.backlog[0]
		backlogProcessed++
		
		if IsDebugEnabled("hash") || IsDebugEnabled("write") {
			if path, err := entry.RelativePath(); err == nil {
				VerboseLog(3, "[UPDATE-FLUSH] Processing backlog entry %d: %s", backlogProcessed, path)
			}
		}
		
		// h) IoVec creation from completed hashes
		// Create zero-copy IoVec when possible  
		ioVec, err := uc.createEntryIoVec(entry)
		if err != nil {
			if IsDebugEnabled("hash") {
				VerboseLog(3, "[UPDATE-FLUSH] Failed to create IoVec for backlog entry: %v", err)
			}
			return fmt.Errorf("failed to create IoVec for entry: %w", err)
		}
		
		if IsDebugEnabled("hash") {
			VerboseLog(3, "[UPDATE-FLUSH] Created IoVec for backlog entry, size: %d bytes", ioVec.Len)
		}
		
		readyIoVecs = append(readyIoVecs, ioVec)
		uc.backlog = uc.backlog[1:] // Remove from backlog
	}
	
	// g) In-order test for pending entries
	pendingSkipped := 0
	// Check pending entries from nextFlushIndex for contiguous completions (nil = ready)
	for int(uc.nextFlushIndex) < len(uc.pendingEntries) {
		if uc.pendingEntries[uc.nextFlushIndex] != nil {
			// Hit a non-completed entry - stop to maintain order
			if IsDebugEnabled("hash") {
				VerboseLog(3, "[UPDATE-FLUSH] Hit non-completed entry at index %d, stopping in-order check", uc.nextFlushIndex)
			}
			break
		}
		// Entry is nil (completed) - can skip it in flush sequence
		pendingSkipped++
		uc.nextFlushIndex++
	}
	
	if IsDebugEnabled("hash") && pendingSkipped > 0 {
		VerboseLog(3, "[UPDATE-FLUSH] Skipped %d completed pending entries, nextFlushIndex now: %d", pendingSkipped, uc.nextFlushIndex)
	}
	
	// i) IoVec entries writing tracing
	// Write batch with single vectorio call to temp index
	if len(readyIoVecs) > 0 {
		if IsDebugEnabled("hash") || IsDebugEnabled("write") {
			totalBytes := uint64(0)
			for _, iovec := range readyIoVecs {
				totalBytes += iovec.Len
			}
			VerboseLog(3, "[UPDATE-WRITE] Writing batch of %d IoVecs, total %d bytes to temp index", len(readyIoVecs), totalBytes)
		}
		
		if err := uc.tempIndexWriter.WriteIoVecBatch(readyIoVecs); err != nil {
			if IsDebugEnabled("hash") {
				VerboseLog(3, "[UPDATE-WRITE] Failed to write IoVec batch: %v", err)
			}
			return fmt.Errorf("failed to write IoVec batch: %w", err)
		}
		
		if IsDebugEnabled("hash") || IsDebugEnabled("write") {
			VerboseLog(3, "[UPDATE-WRITE] Successfully wrote batch of %d entries to temp index", len(readyIoVecs))
		}
	} else {
		if IsDebugEnabled("hash") {
			VerboseLog(3, "[UPDATE-WRITE] No entries ready for writing this flush cycle")
		}
	}
	
	if IsDebugEnabled("hash") || IsDebugEnabled("write") {
		VerboseLog(3, "[UPDATE-FLUSH] Flush complete: processed %d backlog entries, skipped %d pending entries", backlogProcessed, pendingSkipped)
	}
	
	return nil
}

// createEntryIoVec creates an IoVec for zero-copy writing when possible
// Implementation from architecture document lines 688-709
func (uc *UpdateCallback) createEntryIoVec(entry BinaryEntryInterface) (syscall.Iovec, error) {
	// For mmap'd entries: Reference underlying mmap'd binaryEntry directly
	if binaryEntryRef, ok := entry.GetBinaryEntryRef(); ok {
		underlyingEntry := binaryEntryRef.GetBinaryEntry()
		return syscall.Iovec{
			Base: (*byte)(unsafe.Pointer(underlyingEntry)),
			Len:  uint64(unsafe.Sizeof(binaryEntry{})),
		}, nil
	}
	
	// For heap-allocated BEScanEntry: Use GetBinaryData() to avoid copying
	if scanEntry, ok := entry.(*BEScanEntry); ok {
		binaryData, err := scanEntry.GetBinaryData()
		if err != nil {
			return syscall.Iovec{}, fmt.Errorf("failed to get binary data from scan entry: %w", err)
		}
		
		return syscall.Iovec{
			Base: (*byte)(unsafe.Pointer(&binaryData[0])),
			Len:  uint64(len(binaryData)),
		}, nil
	}
	
	// For other entry types: Fall back to copying approach
	entrySize, err := entry.Size()
	if err != nil {
		return syscall.Iovec{}, fmt.Errorf("failed to get entry size: %w", err)
	}
	
	// Allocate buffer with entry's actual size
	binaryData := make([]byte, entrySize)
	
	// Fill binary data from BinaryEntryInterface methods
	if err := uc.fillBinaryDataFromInterface(entry, binaryData); err != nil {
		return syscall.Iovec{}, fmt.Errorf("failed to fill binary data: %w", err)
	}
	
	return syscall.Iovec{
		Base: (*byte)(unsafe.Pointer(&binaryData[0])),
		Len:  uint64(entrySize),
	}, nil
}

// fillBinaryDataFromInterface fills binary data structure from BinaryEntryInterface
func (uc *UpdateCallback) fillBinaryDataFromInterface(entry BinaryEntryInterface, data []byte) error {
	// Create a binaryEntry struct in the data slice
	binaryEntryPtr := (*binaryEntry)(unsafe.Pointer(&data[0]))
	
	// Fill in all fields from the interface
	var err error
	
	if binaryEntryPtr.Size, err = entry.Size(); err != nil {
		return err
	}
	
	// Verify size is not zero (debug)
	if binaryEntryPtr.Size == 0 {
		if path, pathErr := entry.RelativePath(); pathErr == nil {
			return fmt.Errorf("BUG: entry for %s has zero size from interface", path)
		}
		return fmt.Errorf("BUG: entry has zero size from interface")
	}
	if binaryEntryPtr.CTimeWall, err = entry.CTimeWall(); err != nil {
		return err
	}
	if binaryEntryPtr.MTimeWall, err = entry.MTimeWall(); err != nil {
		return err
	}
	if binaryEntryPtr.Dev, err = entry.Dev(); err != nil {
		return err
	}
	if binaryEntryPtr.Ino, err = entry.Ino(); err != nil {
		return err
	}
	if binaryEntryPtr.Mode, err = entry.Mode(); err != nil {
		return err
	}
	if binaryEntryPtr.UID, err = entry.UID(); err != nil {
		return err
	}
	if binaryEntryPtr.GID, err = entry.GID(); err != nil {
		return err
	}
	if binaryEntryPtr.FileSize, err = entry.FileSize(); err != nil {
		return err
	}
	if binaryEntryPtr.HashType, err = entry.HashType(); err != nil {
		return err
	}
	
	// Fill hash
	hash, err := entry.Hash()
	if err != nil {
		return err
	}
	copy(binaryEntryPtr.Hash[:], hash[:])
	
	// Fill entry flags
	flags, err := entry.EntryFlags()
	if err != nil {
		return err
	}
	binaryEntryPtr.EntryFlags = uint16(flags)
	
	// Add relative path at the end of the binaryEntry
	relPath, err := entry.RelativePath()
	if err != nil {
		return err
	}
	
	// Size should already be calculated correctly in constructor - don't override it
	// The Size field should come from the BinaryEntryInterface implementation
	
	// Copy path starting at the Path field (8-byte placeholder that can extend beyond)
	pathBytes := []byte(relPath)
	
	// Path starts at the Path field offset within binaryEntry
	pathOffset := int(unsafe.Offsetof(binaryEntryPtr.Path))
	pathSpace := data[pathOffset:]
	
	// Copy path with null terminator
	copy(pathSpace, pathBytes)
	if len(pathSpace) > len(pathBytes) {
		pathSpace[len(pathBytes)] = 0 // Null terminator
	}
	
	return nil
}

// findEntryByPathOrderID finds the entry that corresponds to a given path order ID
func (uc *UpdateCallback) findEntryByPathOrderID(pathOrderID uint64) BinaryEntryInterface {
	entry, exists := uc.pathOrderToEntry[pathOrderID]
	if !exists {
		return nil
	}
	return entry
}

// retireContiguousEntries processes retire entries in path order ID sequence for writing
func (uc *UpdateCallback) retireContiguousEntries() error {
	var readyIoVecs []IoVec
	
	// Retire entries in strict path order ID sequence (no gaps allowed)
	for {
		pathOrderStr := fmt.Sprintf("%d", uc.nextRetireIndex)
		entry := uc.retireSkiplist.FindByContext(pathOrderStr)
		if entry == nil {
			break // Gap found - cannot retire until this path order ID arrives
		}
		
		// Found next contiguous entry - create IoVec for writing
		ioVec, err := uc.createEntryIoVec(entry)
		if err != nil {
			return fmt.Errorf("failed to create IoVec for path order ID %d: %w", uc.nextRetireIndex, err)
		}
		
		if IsDebugEnabled("write") {
			if path, err := entry.RelativePath(); err == nil {
				VerboseLog(3, "[UPDATE-RETIRE] Retiring entry %s (pathOrderID=%d)", path, uc.nextRetireIndex)
			}
		}
		
		readyIoVecs = append(readyIoVecs, ioVec)
		uc.retireSkiplist.RemoveByContext(pathOrderStr)
		uc.nextRetireIndex++
	}
	
	// Write contiguous batch to temp index via single IoVec operation
	if len(readyIoVecs) > 0 {
		if IsDebugEnabled("write") {
			VerboseLog(3, "[UPDATE-RETIRE] Writing batch of %d entries to temp index", len(readyIoVecs))
		}
		
		// Ensure temp index writer is initialized
		if err := uc.ensureTempIndexWriter(); err != nil {
			return err
		}
		
		return uc.tempIndexWriter.WriteIoVecBatch(readyIoVecs)
	}
	
	return nil
}

// createEntryIoVec creates an IoVec for writing an entry to the temp index
func (uc *UpdateCallback) createEntryIoVec(entry BinaryEntryInterface) (IoVec, error) {
	// For now, use the existing createIoVecFromEntry method
	// This will need to be updated based on the actual BinaryEntryInterface implementation
	return uc.createIoVecFromEntry(entry)
}

// ensureTempIndexWriter ensures the temp index writer is initialized
func (uc *UpdateCallback) ensureTempIndexWriter() error {
	if uc.tempIndexWriter == nil {
		var err error
		uc.tempIndexWriter, err = NewTempIndexWriter(uc.tempIndexFileName)
		if err != nil {
			return fmt.Errorf("failed to create temp index writer: %w", err)
		}
	}
	return nil
}