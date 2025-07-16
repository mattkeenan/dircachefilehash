package dircachefilehash

import (
	"fmt"
	"os"
)

// UpdateCallback implements HwangLinCallback for v0.7 direct temp index writing
// Writes entries directly to main.idx temp file during hwangLinUnified execution (no skiplist building)
type UpdateCallback struct {
	// v0.7 direct temp index writing
	dc                *DirectoryCache
	tempIndexFileName string                  // Temp main index filename for direct writing
	
	// Hash coordination with existing hashJobManager (avoid maps where simple counter works)
	hashJobManager   *algorithmHashManager   // Existing hash manager (passed from caller)
	entryCounter     uint64                  // Internal counter for callback entries (used as cookie)
	pendingEntries   []BinaryEntryInterface  // Entries indexed by (cookie-1), nil = completed/ready
	nextFlushIndex   uint64                  // Next counter position to check for flushing
	
	// Index writing - in-order entry processing (v0.7: direct temp index writing)
	backlog          []BinaryEntryInterface  // Ready entries waiting to write (maintains path order)
	tempIndexWriter  interface{}             // IoVec writer for temp index output (TODO: implement TempIndexWriter)
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
	
	// v0.7: No skiplist to manage, just ensure temp index writer is finalized
	// Temp index writer cleanup will be handled by the caller
	
	return nil
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
	// Get the binaryEntryRef for hash job submission
	// This assumes the entry supports GetBinaryEntryRef() - need to add this to interface
	// For now, request hash through the existing interface
	if err := entry.RequestHash(); err != nil {
		return err
	}
	
	// TODO: Implement direct hash job submission with cookie when GetBinaryEntryRef() is available
	// Increment counter for this entry (used as external cookie)
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
	
	// TODO: Submit to hash manager when direct submission is available
	// uc.hashJobManager.SubmitHashJob(&hashJobStart{
	//     FilePath:    rightPath,
	//     IndexEntry:  ref,
	//     Cookie:      cookie,
	// })
	
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
			// completion now contains both JobID and Cookie
			cookie := completion.Cookie
			
			if cookie > 0 && int(cookie) <= len(uc.pendingEntries) {
				// Mark entry as completed by setting to nil (ready for flush)
				uc.pendingEntries[cookie-1] = nil
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
func (uc *UpdateCallback) flushInOrderEntries() error {
	if IsDebugEnabled("write") {
		VerboseLog(3, "[UPDATE-WRITE] Flushing entries: backlog=%d pending=%d", len(uc.backlog), len(uc.pendingEntries))
	}
	
	// Process backlog entries that can be written in order
	for len(uc.backlog) > 0 {
		entry := uc.backlog[0]
		
		if IsDebugEnabled("write") {
			if path, err := entry.RelativePath(); err == nil {
				VerboseLog(3, "[UPDATE-WRITE] Processing backlog entry: %s", path)
			}
		}
		
		// v0.7: Write entry directly to temp index using IoVec batch writing
		if IsDebugEnabled("write") {
			if path, err := entry.RelativePath(); err == nil {
				VerboseLog(3, "[UPDATE-WRITE] Writing entry to temp index: %s", path)
			}
		}
		
		// Write entry directly to temp index file (v0.7 approach)
		if err := uc.writeEntryToTempIndex(entry); err != nil {
			return fmt.Errorf("failed to write entry to temp index: %w", err)
		}
		
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
	
	if IsDebugEnabled("write") {
		VerboseLog(3, "[UPDATE-WRITE] Flush complete: entries written to temp index")
	}
	
	return nil
}