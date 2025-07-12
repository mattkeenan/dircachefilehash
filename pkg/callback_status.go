package dircachefilehash

import (
	"sync"
)

// StatusCallback implements HwangLinCallback to collect file status changes
// during the unified Hwang-Lin algorithm execution. This enables status checking
// in a single pass without needing to iterate through all entries separately.
type StatusCallback struct {
	CallbackBase
	
	// Results collected during processing
	result *StatusResult
	
	// Mutex to protect concurrent access to result
	mutex sync.Mutex
	
	// Directory cache reference for modification checking
	dc *DirectoryCache
}

// NewStatusCallback creates a new callback for status checking
func NewStatusCallback(name string, dc *DirectoryCache) *StatusCallback {
	return &StatusCallback{
		CallbackBase: CallbackBase{name: name},
		result: &StatusResult{
			Modified: make([]string, 0),
			Added:    make([]string, 0),
			Deleted:  make([]string, 0),
		},
		dc: dc,
	}
}

// OnComparison processes each comparison result and categorizes file status
func (sc *StatusCallback) OnComparison(
	result ComparisonResult,
	leftEntry, rightEntry BinaryEntryInterface,
	leftPath, rightPath string,
) (bool, error) {
	sc.mutex.Lock()
	defer sc.mutex.Unlock()
	
	switch result {
	case ComparisonMatch:
		// Both entries exist - check if file was modified or deleted
		if leftEntry != nil && rightEntry != nil {
			// Check if the right entry (current filesystem state) is marked as deleted
			if isDeleted, err := rightEntry.IsDeleted(); err == nil && isDeleted {
				sc.result.Deleted = append(sc.result.Deleted, rightPath)
			} else if sc.isFileModifiedInterface(leftEntry, rightEntry) {
				sc.result.Modified = append(sc.result.Modified, rightPath)
			}
			// If unchanged, we don't add to any list (StatusUnchanged is implicit)
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
				sc.result.Added = append(sc.result.Added, rightPath)
			}
		}
		
	case ComparisonLeftExhausted:
		// Only right entries remain - these are all new/added files
		if rightEntry != nil {
			if isDeleted, err := rightEntry.IsDeleted(); err == nil && !isDeleted {
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

// isFileModifiedInterface checks if a file has been modified using BinaryEntryInterface
func (sc *StatusCallback) isFileModifiedInterface(leftEntry, rightEntry BinaryEntryInterface) bool {
	// Quick size check
	leftSize, err := leftEntry.FileSize()
	if err != nil {
		return true // Assume modified if we can't read size
	}
	rightSize, err := rightEntry.FileSize()
	if err != nil {
		return true // Assume modified if we can't read size
	}
	if leftSize != rightSize {
		return true
	}

	// Check ownership
	leftUID, err := leftEntry.UID()
	if err != nil {
		return true
	}
	rightUID, err := rightEntry.UID()
	if err != nil {
		return true
	}
	if leftUID != rightUID {
		return true
	}

	leftGID, err := leftEntry.GID()
	if err != nil {
		return true
	}
	rightGID, err := rightEntry.GID()
	if err != nil {
		return true
	}
	if leftGID != rightGID {
		return true
	}

	// Check timestamps using wall time
	leftCTime, err := leftEntry.CTimeWall()
	if err != nil {
		return true
	}
	rightCTime, err := rightEntry.CTimeWall()
	if err != nil {
		return true
	}
	if leftCTime != rightCTime {
		return true
	}

	leftMTime, err := leftEntry.MTimeWall()
	if err != nil {
		return true
	}
	rightMTime, err := rightEntry.MTimeWall()
	if err != nil {
		return true
	}
	if leftMTime != rightMTime {
		return true
	}

	return false
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
}