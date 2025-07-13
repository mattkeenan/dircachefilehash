package dircachefilehash

import (
	"sync"
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
	
	// Cache index writing - entries to be written to cache.idx
	hashingEntries   []BinaryEntryInterface  // Entries that need hashing and caching
}

// NewStatusCallback creates a new callback for status checking
func NewStatusCallback(name string, dc *DirectoryCache, hashManager *algorithmHashManager) *StatusCallback {
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
		hashingEntries: make([]BinaryEntryInterface, 0),
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
					// Submit hash job for the changed file (rightEntry is the current filesystem state)
					if err := sc.submitHashJobToManager(rightEntry); err != nil {
						return false, err
					}
					// File needs hashing - categorize as modified
					sc.result.Modified = append(sc.result.Modified, rightPath)
				}
				// If no hashing needed, file is unchanged (not added to any list)
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
	
	// Check completion queue from hashJobManager and process completed entries
	sc.processCompletedHashJobs()
	
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
	sc.hashingEntries = make([]BinaryEntryInterface, 0)
}

// submitHashJobToManager submits a hash job using the cookie-based tracking system
func (sc *StatusCallback) submitHashJobToManager(entry BinaryEntryInterface) error {
	// Get the binaryEntryRef for hash job submission
	// For now, request hash through the existing interface
	if err := entry.RequestHash(); err != nil {
		return err
	}
	
	// Add entry to the list of entries that need to be cached
	sc.hashingEntries = append(sc.hashingEntries, entry)
	
	// TODO: Implement direct hash job submission with cookie when GetBinaryEntryRef() is available
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

// GetHashingEntries returns the entries that need to be cached (for writing to cache.idx)
func (sc *StatusCallback) GetHashingEntries() []BinaryEntryInterface {
	return sc.hashingEntries
}