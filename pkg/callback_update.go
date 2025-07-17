package dircachefilehash

import (
	"fmt"
	"os"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// UpdateCallback implements HwangLinCallback for v0.7 direct temp index writing
// Writes entries directly to main.idx temp file during hwangLinUnified execution (no skiplist building)
type UpdateCallback struct {
	// v0.7 direct temp index writing
	dc                *DirectoryCache
	tempIndexFileName string                  // Temp main index filename for direct writing
	
	// Hash coordination with existing hashJobManager
	hashJobManager   *algorithmHashManager   // Existing hash manager (passed from caller)
	entryCounter     uint64                  // Internal counter for callback entries (used as cookie)
	pendingEntries   []BinaryEntryInterface  // Entries indexed by (cookie-1), nil = completed/ready
	nextFlushIndex   uint64                  // Next counter position to check for flushing
	
	// Simple atomic counter for completion detection
	jobsInFlight     uint64                  // Atomic counter: inc on submit, dec on complete
	
	// Index writing - in-order entry processing (v0.7: direct temp index writing)
	backlog          []BinaryEntryInterface  // Ready entries waiting to write (maintains path order)
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
		pendingEntries: make([]BinaryEntryInterface, 0),
		nextFlushIndex: 0,
		
		// Direct temp index writing
		backlog:         make([]BinaryEntryInterface, 0),
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
			
			if IsDebugEnabled("write") {
				VerboseLog(3, "[UPDATE-WRITE] Atomically renaming temp index: %s -> %s", tempPath, mainIndexPath)
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
	
	// Update callback writes to main.idx temp file for complete repository state
	if entry == nil {
		return nil // Nothing to process
	}
	
	// Check if this entry needs hashing and writing
	needsHashing := (operation == "new_file") || (operation == "modified")
	
	// CRITICAL: Main index excludes deleted entries (clean repository state)
	isDeleted, _ := entry.IsDeleted()
	needsWriting := !isDeleted // Only write non-deleted entries to main.idx
	
	if needsHashing && needsWriting {
		// Submit hash job for files that need hashing and will be written to main.idx
		if IsDebugEnabled("hash") {
			if path, err := entry.RelativePath(); err == nil {
				VerboseLog(3, "[UPDATE-HASH] Submitting hash job for: %s", path)
			}
		}
		if err := uc.submitHashJobToManager(entry); err != nil {
			return err
		}
		// Also add to backlog immediately for now (async completion handling is TODO)
		uc.appendToBacklog(entry)
	} else if needsWriting {
		// File unchanged - add directly to backlog for writing to main.idx
		if IsDebugEnabled("write") {
			if path, err := entry.RelativePath(); err == nil {
				VerboseLog(3, "[UPDATE-WRITE] Adding unchanged file to backlog: %s", path)
			}
		}
		uc.appendToBacklog(entry)
	}
	// Deleted entries or files that don't need writing: no action needed
	
	// Process any completed hash jobs and write in-order entries
	uc.processCompletedHashJobs()
	
	// Flush any ready entries to temp index
	if err := uc.flushInOrderEntries(); err != nil {
		return fmt.Errorf("failed to flush entries during hash coordination: %w", err)
	}
	
	return nil
}


// submitHashJobToManager submits a hash job using the cookie-based tracking system
func (uc *UpdateCallback) submitHashJobToManager(entry BinaryEntryInterface) error {
	// RequestHash() does the actual job submission AND housekeeping (sets flags, prevents duplicates)
	if err := entry.RequestHash(); err != nil {
		return err
	}
	
	// Only increment counters after successful submission
	atomic.AddUint64(&uc.jobsInFlight, 1)
	uc.entryCounter++
	cookie := uc.entryCounter
	
	// Store entry at cookie position for completion tracking
	if int(cookie) > len(uc.pendingEntries) {
		// Expand slice to accommodate new cookie position
		newSlice := make([]BinaryEntryInterface, cookie)
		copy(newSlice, uc.pendingEntries)
		uc.pendingEntries = newSlice
	}
	uc.pendingEntries[cookie-1] = entry // Store at (cookie-1) since cookies start at 1
	
	return nil
}

// processCompletedHashJobs checks for completed jobs and marks them as ready
func (uc *UpdateCallback) processCompletedHashJobs() {
	if uc.hashJobManager == nil {
		return
	}
	
	// Non-blocking check for completed jobs from existing hashJobManager
	for {
		select {
		case completion := <-uc.hashJobManager.CompletionChannel():
			// completion contains both JobID and Cookie
			cookie := completion.Cookie
			
			if cookie > 0 && int(cookie) <= len(uc.pendingEntries) {
				// Mark entry as completed by setting to nil (ready for flush)
				uc.pendingEntries[cookie-1] = nil
				// Decrement atomic counter when job completes
				atomic.AddUint64(&uc.jobsInFlight, ^uint64(0)) // Atomic decrement
			}
		default:
			return // No more completed jobs available
		}
	}
}

// appendToBacklog adds an entry to the ready-to-write backlog
func (uc *UpdateCallback) appendToBacklog(entry BinaryEntryInterface) {
	uc.backlog = append(uc.backlog, entry)
}

// flushInOrderEntries processes backlog and pending entries for ordered writing
// Implements architecture-specified immediate IoVec batching
func (uc *UpdateCallback) flushInOrderEntries() error {
	if IsDebugEnabled("write") {
		VerboseLog(3, "[UPDATE-WRITE] Flushing entries: backlog=%d pending=%d", len(uc.backlog), len(uc.pendingEntries))
	}
	
	// Initialize temp index writer if needed
	if uc.tempIndexWriter == nil {
		writer, err := NewTempIndexWriter(uc.dc, uc.tempIndexFileName)
		if err != nil {
			return fmt.Errorf("failed to create temp index writer: %w", err)
		}
		uc.tempIndexWriter = writer
	}
	
	// Use counter to check for contiguous completed entries (no gaps)
	var readyIoVecs []syscall.Iovec
	
	// Process backlog entries that can be written in order
	for len(uc.backlog) > 0 {
		entry := uc.backlog[0]
		
		if IsDebugEnabled("write") {
			if path, err := entry.RelativePath(); err == nil {
				VerboseLog(3, "[UPDATE-WRITE] Processing backlog entry: %s", path)
			}
		}
		
		// Create zero-copy IoVec when possible  
		ioVec, err := uc.createEntryIoVec(entry)
		if err != nil {
			return fmt.Errorf("failed to create IoVec for entry: %w", err)
		}
		
		readyIoVecs = append(readyIoVecs, ioVec)
		uc.backlog = uc.backlog[1:] // Remove from backlog
	}
	
	// Check pending entries from nextFlushIndex for contiguous completions (nil = ready)
	for int(uc.nextFlushIndex) < len(uc.pendingEntries) {
		if uc.pendingEntries[uc.nextFlushIndex] != nil {
			// Hit a non-completed entry - stop to maintain order
			break
		}
		// Entry is nil (completed) - can skip it in flush sequence
		uc.nextFlushIndex++
	}
	
	// Write batch with single vectorio call to temp index
	if len(readyIoVecs) > 0 {
		if err := uc.tempIndexWriter.WriteIoVecBatch(readyIoVecs); err != nil {
			return fmt.Errorf("failed to write IoVec batch: %w", err)
		}
		
		if IsDebugEnabled("write") {
			VerboseLog(3, "[UPDATE-WRITE] Wrote batch of %d entries to temp index", len(readyIoVecs))
		}
	}
	
	if IsDebugEnabled("write") {
		VerboseLog(3, "[UPDATE-WRITE] Flush complete: entries written to temp index")
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
	
	// For read/write entries: Must copy data (unavoidable for non-mmap'd entries)
	// TODO: Implement GetBinaryData() method for BinaryEntryInterface if needed
	// For now, handle heap-allocated entries by creating binary data
	
	// For heap-allocated BEScanEntry, we need to create binary data
	var binaryData [256]byte // Size of binaryEntry struct
	
	// Fill binary data from BinaryEntryInterface methods
	if err := uc.fillBinaryDataFromInterface(entry, &binaryData); err != nil {
		return syscall.Iovec{}, fmt.Errorf("failed to fill binary data: %w", err)
	}
	
	return syscall.Iovec{
		Base: (*byte)(unsafe.Pointer(&binaryData[0])),
		Len:  uint64(len(binaryData)),
	}, nil
}

// fillBinaryDataFromInterface fills binary data structure from BinaryEntryInterface
func (uc *UpdateCallback) fillBinaryDataFromInterface(entry BinaryEntryInterface, data *[256]byte) error {
	// Create a binaryEntry struct in the data array
	binaryEntryPtr := (*binaryEntry)(unsafe.Pointer(&data[0]))
	
	// Fill in all fields from the interface
	var err error
	
	if binaryEntryPtr.Size, err = entry.Size(); err != nil {
		return err
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
	
	// Calculate total size including path
	pathBytes := []byte(relPath)
	totalSize := uint32(unsafe.Sizeof(binaryEntry{})) + uint32(len(pathBytes)) + 1 // +1 for null terminator
	binaryEntryPtr.Size = totalSize
	
	// Copy path after the binaryEntry struct
	pathStart := uintptr(unsafe.Pointer(binaryEntryPtr)) + unsafe.Sizeof(binaryEntry{})
	pathDest := (*[256]byte)(unsafe.Pointer(pathStart))
	copy(pathDest[:], pathBytes)
	pathDest[len(pathBytes)] = 0 // Null terminator
	
	return nil
}