package dircachefilehash

import (
	"fmt"
	"sync"
	"syscall"
	"unsafe"
)

// StatusCallback implements HwangLinCallback to collect file status changes
// during the unified Hwang-Lin algorithm execution. This enables status checking
// in a single pass without needing to iterate through all entries separately.
// CRITICAL: Status command MUST hash files and cache results to cache.idx for performance.
type StatusCallback struct {
	CallbackBase
	
	// Results collected during processing
	result *StatusResult
	
	// Mutex to protect concurrent access to result
	mutex sync.Mutex
	
	// Directory cache reference for modification checking
	dc *DirectoryCache
	
	// Hash coordination with existing hashJobManager (avoid maps where simple counter works)
	hashJobManager   *algorithmHashManager   // Existing hash manager (passed from caller)
	entryCounter     uint64                  // Internal counter for callback entries (used as cookie)
	pendingEntries   []BinaryEntryInterface  // Entries indexed by (cookie-1), nil = completed/ready
	nextFlushIndex   uint64                  // Next counter position to check for flushing
	
	// Iterative cache index writing (following architecture-v0.7.md batched IoVec approach)
	cacheTempFileName string                  // Temp cache index filename for iterative writing
	backlog          []BinaryEntryInterface  // Ready entries waiting to write (maintains path order)
	tempIndexWriter  interface{}             // IoVec writer for temp index output (TODO: implement TempIndexWriter)
}

// NewStatusCallback creates a new callback for status checking and cache writing
func NewStatusCallback(name string, dc *DirectoryCache, hashManager *algorithmHashManager, cacheTempFileName string) *StatusCallback {
	return &StatusCallback{
		CallbackBase: CallbackBase{name: name},
		result: &StatusResult{
			Modified: make([]string, 0),
			Added:    make([]string, 0),
			Deleted:  make([]string, 0),
		},
		dc: dc,
		
		// Hash coordination
		hashJobManager: hashManager,
		entryCounter:   0,
		pendingEntries: make([]BinaryEntryInterface, 0),
		nextFlushIndex: 0,
		
		// Iterative cache writing
		cacheTempFileName: cacheTempFileName,
		backlog:          make([]BinaryEntryInterface, 0),
		tempIndexWriter:  nil, // Will be initialized when first entry is written
	}
}

// OnComparison processes each comparison result and categorizes file status
func (sc *StatusCallback) OnComparison(
	result ComparisonResult,
	leftEntry, rightEntry BinaryEntryInterface,
	leftPath, rightPath string,
) (bool, error) {
	sc.mutex.Lock()
	
	switch result {
	case ComparisonMatch:
		// Both entries exist - check if file was modified or deleted
		if leftEntry != nil && rightEntry != nil {
			// Check if the right entry (current filesystem state) is marked as deleted
			if isDeleted, err := rightEntry.IsDeleted(); err == nil && isDeleted {
				sc.result.Deleted = append(sc.result.Deleted, rightPath)
			} else {
				// CRITICAL: Status command MUST hash files that need hashing
				if needsHash(leftEntry, rightEntry) {
					// File changed - submit hash job for current state
					if err := sc.submitHashJobToManager(rightEntry); err != nil {
						return false, err
					}
					// File needs hashing - categorize as modified
					sc.result.Modified = append(sc.result.Modified, rightPath)
				} else {
					// File unchanged - append existing entry to backlog immediately
					sc.appendToBacklog(leftEntry)
				}
			}
		}
		
	case ComparisonLeftFirst:
		// Left entry exists but not on right - this is a deleted file
		if leftEntry != nil {
			// Only count as deleted if the left entry is not already marked as deleted
			if isDeleted, err := leftEntry.IsDeleted(); err == nil && !isDeleted {
				sc.result.Deleted = append(sc.result.Deleted, leftPath)
			}
		}
		
	case ComparisonRightFirst:
		// Right entry exists but not on left - this is a new/added file
		if rightEntry != nil {
			// Only count as added if the right entry is not marked as deleted
			if isDeleted, err := rightEntry.IsDeleted(); err == nil && !isDeleted {
				// CRITICAL: Status command MUST hash new files (always needs hashing)
				if err := rightEntry.RequestHash(); err != nil {
					return false, err
				}
				sc.result.Added = append(sc.result.Added, rightPath)
			}
		}
		
	case ComparisonLeftExhausted:
		// Only right entries remain - these are all new/added files
		if rightEntry != nil {
			if isDeleted, err := rightEntry.IsDeleted(); err == nil && !isDeleted {
				// CRITICAL: Status command MUST hash new files (always needs hashing)
				if err := rightEntry.RequestHash(); err != nil {
					return false, err
				}
				sc.result.Added = append(sc.result.Added, rightPath)
			}
		}
		
	case ComparisonRightExhausted:
		// Only left entries remain - these are all deleted files
		if leftEntry != nil {
			if isDeleted, err := leftEntry.IsDeleted(); err == nil && !isDeleted {
				sc.result.Deleted = append(sc.result.Deleted, leftPath)
			}
		}
	}
	
	// Release mutex before hash coordination to avoid deadlocks
	sc.mutex.Unlock()
	
	// Check completion queue from hashJobManager and merge completed entries to backlog
	sc.processCompletedHashJobs()
	
	// Create IoVec array from in-order entries (no gaps) and call writeIoVec to output temp index
	if err := sc.flushInOrderEntries(); err != nil {
		return false, fmt.Errorf("failed to flush entries: %w", err)
	}
	
	return true, nil // Continue processing
}

// OnLeftOnly handles remaining entries from the left iterator (deleted files)
func (sc *StatusCallback) OnLeftOnly(entry BinaryEntryInterface, path string) (bool, error) {
	if entry != nil {
		if isDeleted, err := entry.IsDeleted(); err == nil && !isDeleted {
			sc.mutex.Lock()
			sc.result.Deleted = append(sc.result.Deleted, path)
			sc.mutex.Unlock()
		}
	}
	return true, nil
}

// OnRightOnly handles remaining entries from the right iterator (new files)
func (sc *StatusCallback) OnRightOnly(entry BinaryEntryInterface, path string) (bool, error) {
	if entry != nil {
		if isDeleted, err := entry.IsDeleted(); err == nil && !isDeleted {
			// CRITICAL: Status command MUST hash new files (always needs hashing)
			if err := entry.RequestHash(); err != nil {
				return false, err
			}
			sc.mutex.Lock()
			sc.result.Added = append(sc.result.Added, path)
			sc.mutex.Unlock()
		}
	}
	return true, nil
}

// GetResult returns the status result collected during processing.
// This should only be called after OnComplete() has been called.
func (sc *StatusCallback) GetResult() *StatusResult {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()
	
	// Return the result directly since StatusResult contains slices that are safe to share
	return sc.result
}


// Clear resets the callback state for reuse
func (sc *StatusCallback) Clear() {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()
	
	sc.result = &StatusResult{
		Modified: make([]string, 0),
		Added:    make([]string, 0),
		Deleted:  make([]string, 0),
	}
	
	// Reset hash coordination state
	sc.entryCounter = 0
	sc.pendingEntries = make([]BinaryEntryInterface, 0)
	sc.nextFlushIndex = 0
}

// submitHashJobToManager submits hash job and stores entry for pending completion tracking
func (sc *StatusCallback) submitHashJobToManager(entry BinaryEntryInterface) error {
	// Submit to existing hash manager using callback's own counter as cookie
	_, ok := entry.GetBinaryEntryRef()
	if !ok {
		return fmt.Errorf("entry doesn't support hash job submission")
	}
	
	// Increment counter for this entry (used as external cookie)
	sc.entryCounter++
	cookie := sc.entryCounter
	
	// Store entry at cookie position for completion tracking
	if int(cookie) > len(sc.pendingEntries) {
		// Expand slice to accommodate new cookie position
		newSlice := make([]BinaryEntryInterface, cookie)
		copy(newSlice, sc.pendingEntries)
		sc.pendingEntries = newSlice
	}
	sc.pendingEntries[cookie-1] = entry // Store at (cookie-1) since cookies start at 1
	
	// Request hash through the existing interface
	if err := entry.RequestHash(); err != nil {
		return err
	}
	
	// TODO: Implement direct hash job submission with cookie to hashJobManager
	// sc.hashJobManager.SubmitHashJob(&hashJobStart{
	//     FilePath:    entry.AbsolutePath(),
	//     IndexEntry:  ref,
	//     Cookie:      cookie,
	// })
	
	return nil
}

// appendToBacklog adds entry to backlog for immediate writing (file unchanged case)
func (sc *StatusCallback) appendToBacklog(entry BinaryEntryInterface) {
	sc.backlog = append(sc.backlog, entry)
}

// flushInOrderEntries processes backlog and pending entries, creating IoVecs for temp index writing
func (sc *StatusCallback) flushInOrderEntries() error {
	// Use counter to check for contiguous completed entries (no gaps)
	var readyIoVecs []syscall.Iovec
	
	// Process backlog entries that can be written in order
	for len(sc.backlog) > 0 {
		entry := sc.backlog[0]
		
		// Create zero-copy IoVec when possible  
		ioVec, err := sc.createEntryIoVec(entry)
		if err != nil {
			return err
		}
		
		readyIoVecs = append(readyIoVecs, ioVec)
		sc.backlog = sc.backlog[1:] // Remove from backlog
	}
	
	// Check pending entries from nextFlushIndex for contiguous completions (nil = ready)
	for int(sc.nextFlushIndex) < len(sc.pendingEntries) {
		if sc.pendingEntries[sc.nextFlushIndex] != nil {
			// Hit a non-completed entry - stop to maintain order
			break
		}
		// Entry is nil (completed) - can skip it in flush sequence
		sc.nextFlushIndex++
	}
	
	// Write batch with single vectorio call to temp index
	if len(readyIoVecs) > 0 {
		return sc.writeIoVecBatchToTempIndex(readyIoVecs)
	}
	
	return nil
}

// createEntryIoVec creates zero-copy IoVec from BinaryEntryInterface
func (sc *StatusCallback) createEntryIoVec(entry BinaryEntryInterface) (syscall.Iovec, error) {
	// For mmap'd entries: Reference underlying mmap'd binaryEntry directly
	if ref, ok := entry.GetBinaryEntryRef(); ok {
		underlyingEntry := ref.GetBinaryEntry()
		return syscall.Iovec{
			Base: (*byte)(unsafe.Pointer(underlyingEntry)),
			Len:  uint64(unsafe.Sizeof(binaryEntry{})),
		}, nil
	}
	
	return syscall.Iovec{}, fmt.Errorf("entry doesn't support binary entry reference")
}

// writeIoVecBatchToTempIndex writes IoVec batch to cache temp index using vectorio
func (sc *StatusCallback) writeIoVecBatchToTempIndex(iovecs []syscall.Iovec) error {
	// TODO: Implement proper IoVec batch writing to temp index
	// For now, just skip writing - this will be implemented when TempIndexWriter is available
	// The architecture framework is in place for iterative writing
	return nil
}

// processCompletedHashJobs checks for completed jobs and marks them as ready
func (sc *StatusCallback) processCompletedHashJobs() {
	if sc.hashJobManager == nil {
		return
	}
	
	// Non-blocking check for completed jobs from existing hashJobManager
	for {
		select {
		case completion := <-sc.hashJobManager.CompletionChannel():
			// completion now contains both JobID and Cookie
			cookie := completion.Cookie
			
			if cookie > 0 && int(cookie) <= len(sc.pendingEntries) {
				// Mark entry as completed by setting to nil (ready for flush)
				sc.pendingEntries[cookie-1] = nil
			}
		default:
			return // No more completed jobs available
		}
	}
}

