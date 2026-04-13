package dircachefilehash

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// UpdateCallback implements HwangLinCallback for v0.7 direct temp index writing
// Writes entries directly to main.idx temp file during hwangLinUnified execution (no skiplist building)
type UpdateCallback struct {
	// v0.7 direct temp index writing
	dc                *DirectoryCache
	tempIndexFileName string // Temp main index filename for direct writing

	// Hash coordination with existing hashJobManager
	hashJobManager *algorithmHashManager // Existing hash manager (passed from caller)
	entryCounter   uint64                // Internal counter for callback entries (used as path order ID)

	// Simple atomic counter for completion detection
	jobsInFlight uint64 // Atomic counter: inc on submit, dec on complete

	// Path order preservation via retire skiplist
	retireSkiplist   *skiplistWrapper                // Entries ready to retire, ordered by path order ID as context
	nextRetireIndex  uint64                          // Next path order ID sequence number expected for retirement
	pathOrderToEntry map[uint64]BinaryEntryInterface // Track entries by path order ID for completion lookup
	// No mutex needed - UpdateCallback runs single-threaded via hwangLinUnified

	// Shutdown coordination and hash job synchronization
	shutdownChan <-chan struct{} // Shutdown signal from main
	hashJobWG    sync.WaitGroup  // Wait for all hash jobs to complete

	// Index writing - Iovec writer for temp index output
	tempIndexWriter *TempIndexWriter // Iovec writer for temp index output
}

// NewUpdateCallback creates a new UpdateCallback for v0.7 direct temp index writing
func NewUpdateCallback(dc *DirectoryCache, tempIndexFileName string, hashManager *algorithmHashManager, shutdownChan <-chan struct{}) *UpdateCallback {
	return &UpdateCallback{
		// v0.7 direct temp index writing
		dc:                dc,
		tempIndexFileName: tempIndexFileName,

		// Hash coordination
		hashJobManager: hashManager,
		entryCounter:   0,

		// Path order preservation
		retireSkiplist:   NewSkiplistWrapper(16, "retire"),
		nextRetireIndex:  1, // Start retiring from path order ID 1
		pathOrderToEntry: make(map[uint64]BinaryEntryInterface),

		// Shutdown coordination
		shutdownChan: shutdownChan,

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

	// Try to retire contiguous entries (non-blocking)
	if err := uc.retireContiguousEntries(); err != nil {
		return false, err
	}

	return true, nil // Continue processing
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
	fmt.Fprintf(os.Stderr, "[UPDATE-DEBUG] OnComplete() called with error: %v\n", err)
	if IsDebugEnabled("scanning") {
		if err != nil {
			fmt.Fprintf(os.Stderr, "[UPDATE] Update completed with error: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "[UPDATE] Update completed successfully, entries written to temp index\n")
		}
	}

	// Wait for all hash jobs to complete with graceful shutdown handling
	fmt.Fprintf(os.Stderr, "[UPDATE-COMPLETE] OnComplete() state analysis:\n")

	fmt.Fprintf(os.Stderr, "[UPDATE-COMPLETE] Getting jobs in flight...\n")
	remainingJobs := atomic.LoadUint64(&uc.jobsInFlight)
	fmt.Fprintf(os.Stderr, "  - Jobs in flight: %d\n", remainingJobs)

	fmt.Fprintf(os.Stderr, "[UPDATE-COMPLETE] Skipping pathOrderToEntry map size (avoiding potential deadlock)...\n")

	fmt.Fprintf(os.Stderr, "  - Next retire index: %d\n", uc.nextRetireIndex)
	fmt.Fprintf(os.Stderr, "  - Entry counter (total submitted): %d\n", uc.entryCounter)

	if remainingJobs > 0 {
		fmt.Fprintf(os.Stderr, "  - About to wait for WaitGroup with %d outstanding jobs\n", remainingJobs)
	} else {
		fmt.Fprintf(os.Stderr, "  - No jobs in flight, WaitGroup should complete immediately\n")
	}

	// Wait for hash jobs with cancellation support
	done := make(chan struct{})
	go func() {
		uc.hashJobWG.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All jobs completed normally
		if IsDebugEnabled("hash") {
			VerboseLog(3, "[UPDATE-COMPLETE] All hash jobs completed successfully")
		}
	case <-uc.shutdownChan:
		// Interrupted - cleanup what we have
		if IsDebugEnabled("hash") {
			VerboseLog(3, "[UPDATE-COMPLETE] Shutdown signal received, proceeding with available entries")
		}
	}

	// Final cleanup regardless of completion vs interruption
	fmt.Fprintf(os.Stderr, "[UPDATE-COMPLETE] Processing final completions and retiring remaining entries\n")

	// Drain any remaining completions (non-blocking)
	fmt.Fprintf(os.Stderr, "[UPDATE-COMPLETE] Step 1: Calling processCompletedHashJobs...\n")
	uc.processCompletedHashJobs()
	fmt.Fprintf(os.Stderr, "[UPDATE-COMPLETE] Step 1: processCompletedHashJobs completed\n")

	// Write any remaining entries from retireSkiplist
	fmt.Fprintf(os.Stderr, "[UPDATE-COMPLETE] Step 2: Calling retireContiguousEntries...\n")
	if retireErr := uc.retireContiguousEntries(); retireErr != nil {
		fmt.Fprintf(os.Stderr, "[UPDATE-COMPLETE] Step 2: retireContiguousEntries returned error: %v\n", retireErr)
		if err == nil {
			err = fmt.Errorf("failed to retire remaining entries: %w", retireErr)
		} else {
			fmt.Fprintf(os.Stderr, "[UPDATE] Warning: failed to retire remaining entries: %v\n", retireErr)
		}
	} else {
		fmt.Fprintf(os.Stderr, "[UPDATE-COMPLETE] Step 2: retireContiguousEntries completed successfully\n")
	}

	// v0.7: Close temp index writer to finalize the temp index file
	fmt.Fprintf(os.Stderr, "[UPDATE-COMPLETE] Step 3: Closing temp index writer...\n")
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

	// Increment wait group BEFORE submitting job
	uc.hashJobWG.Add(1)

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
	// No locking needed - UpdateCallback is single-threaded
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
	fmt.Fprintf(os.Stderr, "[UPDATE-COMPLETE] processCompletedHashJobs: Starting...\n")
	if uc.hashJobManager == nil {
		fmt.Fprintf(os.Stderr, "[UPDATE-COMPLETE] processCompletedHashJobs: No hash manager, returning\n")
		if IsDebugEnabled("hash") {
			VerboseLog(3, "[HASH-COMPLETE] No hash manager available for completion processing")
		}
		return
	}

	fmt.Fprintf(os.Stderr, "[UPDATE-COMPLETE] processCompletedHashJobs: Entering completion loop...\n")
	completionCount := 0
	// Non-blocking read of completion channel (entries arrive in JobID order)
	for {
		select {
		case completion := <-uc.hashJobManager.CompletionChannel():
			completionCount++
			fmt.Fprintf(os.Stderr, "[UPDATE-COMPLETE] processCompletedHashJobs: Processing completion %d, JobID=%d, Cookie=%d\n", completionCount, completion.JobID, completion.Cookie)

			// Handle termination/sentinel completion (JobID=0, Cookie=0)
			if completion.JobID == 0 && completion.Cookie == 0 {
				fmt.Fprintf(os.Stderr, "[UPDATE-COMPLETE] processCompletedHashJobs: Received termination signal, ignoring\n")
				continue
			}

			// Completions arrive in JobID order thanks to algorithmHashManager
			// Add completed entry to retireSkiplist with path order ID as context for ordering
			pathOrderID := completion.Cookie

			if IsDebugEnabled("hash") {
				VerboseLog(3, "[HASH-COMPLETE] Received completion message: JobID=%d, Cookie=%d", completion.JobID, pathOrderID)
			}

			// Signal wait group that this job is done (ALWAYS - even if entry lookup fails)
			fmt.Fprintf(os.Stderr, "[UPDATE-COMPLETE] processCompletedHashJobs: About to call hashJobWG.Done()\n")
			uc.hashJobWG.Done()
			fmt.Fprintf(os.Stderr, "[UPDATE-COMPLETE] processCompletedHashJobs: hashJobWG.Done() completed\n")

			// Decrement atomic counter when job completes (guard against underflow)
			if atomic.LoadUint64(&uc.jobsInFlight) > 0 {
				atomic.AddUint64(&uc.jobsInFlight, ^uint64(0)) // Atomic decrement
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

				// NOTE: Do NOT delete from pathOrderToEntry here!
				// The entry must remain until retireContiguousEntries() processes it

				if IsDebugEnabled("hash") {
					remainingJobs := atomic.LoadUint64(&uc.jobsInFlight)
					VerboseLog(3, "[HASH-COMPLETE] Entry added to retireSkiplist, remaining jobs: %d", remainingJobs)
				}
			} else {
				if IsDebugEnabled("hash") {
					VerboseLog(1, "[HASH-COMPLETE] WARNING: Received completion for unknown pathOrderID: %d (job counted as complete)", pathOrderID)
				}
			}
			fmt.Fprintf(os.Stderr, "[UPDATE-COMPLETE] processCompletedHashJobs: Completed processing completion %d, looping back...\n", completionCount)
		default:
			// No more completions available (non-blocking)
			fmt.Fprintf(os.Stderr, "[UPDATE-COMPLETE] processCompletedHashJobs: No more completions, processed %d total, returning\n", completionCount)
			return
		}
	}
}

// createEntryIovec creates an Iovec for zero-copy writing when possible
// Implementation from architecture document lines 688-709
func (uc *UpdateCallback) createEntryIovec(entry BinaryEntryInterface) (syscall.Iovec, error) {
	// For mmap'd entries: Reference underlying mmap'd binaryEntry directly
	if binaryEntryRef, ok := entry.GetBinaryEntryRef(); ok {
		underlyingEntry := binaryEntryRef.GetBinaryEntry()
		return syscall.Iovec{
			Base: (*byte)(unsafe.Pointer(underlyingEntry)),
			Len:  uint64(underlyingEntry.Size),
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
	// Path starts after the struct (matching writeBinaryEntryToMmap and RelativePath)
	pathOffset := int(unsafe.Sizeof(*binaryEntryPtr))
	pathSpace := data[pathOffset:]

	// Copy path with null terminator
	copy(pathSpace, pathBytes)
	if len(pathSpace) > len(pathBytes) {
		pathSpace[len(pathBytes)] = 0 // Null terminator
	}

	return nil
}

// findEntryByPathOrderID finds the entry that corresponds to a given path order ID
// Safe without locking - UpdateCallback runs single-threaded via hwangLinUnified algorithm
func (uc *UpdateCallback) findEntryByPathOrderID(pathOrderID uint64) BinaryEntryInterface {
	fmt.Fprintf(os.Stderr, "[UPDATE-COMPLETE] findEntryByPathOrderID: Looking up pathOrderID=%d\n", pathOrderID)
	entry, exists := uc.pathOrderToEntry[pathOrderID]
	fmt.Fprintf(os.Stderr, "[UPDATE-COMPLETE] findEntryByPathOrderID: Lookup complete, exists=%v\n", exists)
	if !exists {
		return nil
	}
	return entry
}

// retireContiguousEntries processes retire entries in path order ID sequence for writing
func (uc *UpdateCallback) retireContiguousEntries() error {
	var readyIoVecs []syscall.Iovec
	// Keep entry references alive until after WriteIoVecBatch completes.
	// Iovec.Base points into memory owned by these entries (BEScanEntry.binaryData
	// or mmap'd binaryEntry). Deleting from pathOrderToEntry before the write
	// would allow GC to collect BEScanEntry backing memory, causing dangling pointers.
	var retiredEntries []BinaryEntryInterface

	// Retire entries in strict path order ID sequence (no gaps allowed)
	for {
		// O(1) lookup by path order ID (no locking - single-threaded)
		entry := uc.pathOrderToEntry[uc.nextRetireIndex]
		if entry == nil {
			break // Gap found - cannot retire until this path order ID arrives
		}

		// Found next contiguous entry - create Iovec for writing
		ioVec, err := uc.createEntryIovec(entry)
		if err != nil {
			return fmt.Errorf("failed to create Iovec for path order ID %d: %w", uc.nextRetireIndex, err)
		}

		if IsDebugEnabled("write") {
			if path, err := entry.RelativePath(); err == nil {
				VerboseLog(3, "[UPDATE-RETIRE] Retiring entry %s (pathOrderID=%d)", path, uc.nextRetireIndex)
				// Remove from skiplist by path (not context)
				uc.retireSkiplist.Delete(path)
			}
		}

		readyIoVecs = append(readyIoVecs, ioVec)
		retiredEntries = append(retiredEntries, entry)
		uc.nextRetireIndex++
	}

	// Write contiguous batch to temp index via single Iovec operation
	if len(readyIoVecs) > 0 {
		// Initialize temp index writer if needed
		if uc.tempIndexWriter == nil {
			if IsDebugEnabled("write") {
				VerboseLog(3, "[UPDATE-RETIRE] Creating new TempIndexWriter for: %s", uc.tempIndexFileName)
			}
			writer, err := NewTempIndexWriter(uc.dc, uc.tempIndexFileName)
			if err != nil {
				return fmt.Errorf("failed to create temp index writer: %w", err)
			}
			uc.tempIndexWriter = writer
			if IsDebugEnabled("write") {
				VerboseLog(3, "[UPDATE-RETIRE] TempIndexWriter created successfully")
			}
		}

		if IsDebugEnabled("write") {
			totalBytes := uint64(0)
			for _, iovec := range readyIoVecs {
				totalBytes += iovec.Len
			}
			VerboseLog(3, "[UPDATE-RETIRE] Writing batch of %d Iovecs, total %d bytes to temp index", len(readyIoVecs), totalBytes)
		}

		if err := uc.tempIndexWriter.WriteIoVecBatch(readyIoVecs); err != nil {
			if IsDebugEnabled("write") {
				VerboseLog(3, "[UPDATE-RETIRE] Failed to write Iovec batch: %v", err)
			}
			return fmt.Errorf("failed to write Iovec batch: %w", err)
		}

		if IsDebugEnabled("write") {
			VerboseLog(3, "[UPDATE-RETIRE] Successfully wrote batch of %d entries to temp index", len(readyIoVecs))
		}

		// Now safe to release entry references — writev() has completed.
		// Clean up path order mapping for all retired entries.
		startIndex := uc.nextRetireIndex - uint64(len(retiredEntries))
		for i := range retiredEntries {
			delete(uc.pathOrderToEntry, startIndex+uint64(i))
		}
		// retiredEntries goes out of scope here, allowing GC
	} else {
		if IsDebugEnabled("write") {
			VerboseLog(3, "[UPDATE-RETIRE] No entries ready for writing this cycle")
		}
	}

	return nil
}
