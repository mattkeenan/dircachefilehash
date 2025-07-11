package dircachefilehash

import (
	"sync"
)

// DupesCallback implements HwangLinCallback to build a map of duplicate files
// during the unified Hwang-Lin algorithm execution. This enables duplicate detection
// in a single pass without needing to iterate through all entries separately.
type DupesCallback struct {
	CallbackBase
	
	// Hash map storing entries by their hash value
	hashMap map[string][]*binaryEntry
	
	// Mutex to protect concurrent access to hashMap
	mutex sync.Mutex
	
	// Results after processing is complete
	results []DuplicateGroup
}

// NewDupesCallback creates a new callback for duplicate detection
func NewDupesCallback(name string) *DupesCallback {
	return &DupesCallback{
		CallbackBase: CallbackBase{name: name},
		hashMap:      make(map[string][]*binaryEntry),
		results:      nil,
	}
}

// OnComparison processes each comparison result and adds entries to the hash map
func (dc *DupesCallback) OnComparison(
	result ComparisonResult,
	leftEntry, rightEntry *binaryEntry,
	leftPath, rightPath string,
) (bool, error) {
	dc.mutex.Lock()
	defer dc.mutex.Unlock()
	
	switch result {
	case ComparisonMatch:
		// Both entries exist - add the right entry (current filesystem state)
		// Skip the left entry as it represents older state
		if rightEntry != nil && !rightEntry.IsDeleted() {
			hashStr := rightEntry.HashString()
			dc.hashMap[hashStr] = append(dc.hashMap[hashStr], rightEntry)
		}
		
	case ComparisonLeftFirst:
		// Left entry exists but not on right - this is a deleted file
		// Don't add deleted files to duplicate detection
		
	case ComparisonRightFirst:
		// Right entry exists but not on left - this is a new/added file
		if rightEntry != nil && !rightEntry.IsDeleted() {
			hashStr := rightEntry.HashString()
			dc.hashMap[hashStr] = append(dc.hashMap[hashStr], rightEntry)
		}
		
	case ComparisonLeftExhausted:
		// Only right entries remain - these are all new/added files
		if rightEntry != nil && !rightEntry.IsDeleted() {
			hashStr := rightEntry.HashString()
			dc.hashMap[hashStr] = append(dc.hashMap[hashStr], rightEntry)
		}
		
	case ComparisonRightExhausted:
		// Only left entries remain - these are all deleted files
		// Don't add deleted files to duplicate detection
	}
	
	return true, nil // Continue processing
}

// OnLeftOnly handles remaining entries from the left iterator (deleted files)
func (dc *DupesCallback) OnLeftOnly(entry *binaryEntry, path string) (bool, error) {
	// Left-only entries represent deleted files, don't add to duplicates map
	return true, nil
}

// OnRightOnly handles remaining entries from the right iterator (new files)
func (dc *DupesCallback) OnRightOnly(entry *binaryEntry, path string) (bool, error) {
	if entry != nil && !entry.IsDeleted() {
		dc.mutex.Lock()
		hashStr := entry.HashString()
		dc.hashMap[hashStr] = append(dc.hashMap[hashStr], entry)
		dc.mutex.Unlock()
	}
	return true, nil
}

// OnComplete finalizes the duplicate detection by converting the hash map to DuplicateGroups
func (dc *DupesCallback) OnComplete(err error) error {
	dc.mutex.Lock()
	defer dc.mutex.Unlock()
	
	// Convert hash map to DuplicateGroup results
	// Only include hashes that have more than one file
	dc.results = make([]DuplicateGroup, 0)
	
	for hash, entries := range dc.hashMap {
		if len(entries) > 1 {
			files := make([]string, len(entries))
			for i, entry := range entries {
				files[i] = entry.RelativePath()
			}
			
			dc.results = append(dc.results, DuplicateGroup{
				Hash:  hash,
				Files: files,
				Count: len(files),
			})
		}
	}
	
	return nil
}

// GetResults returns the duplicate groups found during processing.
// This should only be called after OnComplete() has been called.
func (dc *DupesCallback) GetResults() []DuplicateGroup {
	dc.mutex.Lock()
	defer dc.mutex.Unlock()
	
	// Return a copy to prevent external modification
	results := make([]DuplicateGroup, len(dc.results))
	copy(results, dc.results)
	return results
}

// GetHashMapStats returns statistics about the hash map for debugging/testing
func (dc *DupesCallback) GetHashMapStats() (totalHashes int, totalEntries int, duplicateHashes int) {
	dc.mutex.Lock()
	defer dc.mutex.Unlock()
	
	totalHashes = len(dc.hashMap)
	duplicateHashes = 0
	
	for _, entries := range dc.hashMap {
		totalEntries += len(entries)
		if len(entries) > 1 {
			duplicateHashes++
		}
	}
	
	return totalHashes, totalEntries, duplicateHashes
}

// Clear resets the callback state for reuse
func (dc *DupesCallback) Clear() {
	dc.mutex.Lock()
	defer dc.mutex.Unlock()
	
	dc.hashMap = make(map[string][]*binaryEntry)
	dc.results = nil
}