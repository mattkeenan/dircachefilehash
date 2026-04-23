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
	hashJobManager *algorithmHashManager  // Existing hash manager (passed from caller)
	entryCounter   uint64                 // Internal counter for callback entries (used as cookie)
	pendingEntries []BinaryEntryInterface // Entries indexed by (cookie-1), nil = completed/ready
	nextFlushIndex uint64                 // Next counter position to check for flushing

	// Iterative cache index writing (following architecture-v0.7.md batched IoVec approach)
	cacheTempFileName string                 // Temp cache index filename for iterative writing
	backlog           []BinaryEntryInterface // Ready entries waiting to write (maintains path order)
	tempIndexWriter   any                    // IoVec writer for temp index output (TODO: implement TempIndexWriter)
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
		backlog:           make([]BinaryEntryInterface, 0),
		tempIndexWriter:   nil, // Will be initialized when first entry is written
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
		return sc.onMatch(leftEntry, rightEntry, rightPath)
	case ComparisonLeftFirst, ComparisonRightExhausted:
		sc.recordDeletion(leftEntry, leftPath)
	case ComparisonRightFirst, ComparisonLeftExhausted:
		if err := sc.recordAddition(rightEntry, rightPath, result == ComparisonLeftExhausted); err != nil {
			return false, err
		}
	}
	return true, nil
}

// onMatch handles ComparisonMatch: the file is in both the index and
// the filesystem. Classify as deleted (right-side tombstone),
// modified (metadata changed), or unchanged (write through to
// backlog). Expects sc.mutex to be held on entry; unlocks/re-locks
// around SubmitAndOrWriteHash to avoid deadlocks, matching the
// original contract.
func (sc *StatusCallback) onMatch(leftEntry, rightEntry BinaryEntryInterface, rightPath string) (bool, error) {
	if leftEntry == nil || rightEntry == nil {
		return true, nil
	}
	if isDeleted, err := rightEntry.IsDeleted(); err == nil && isDeleted {
		sc.result.Deleted = append(sc.result.Deleted, rightPath)
		return true, nil
	}
	reason := "unchanged"
	submitEntry := leftEntry
	if needsHash(leftEntry, rightEntry) {
		sc.result.Modified = append(sc.result.Modified, rightPath)
		reason = "modified"
		submitEntry = rightEntry
	}
	sc.mutex.Unlock()
	err := sc.SubmitAndOrWriteHash(submitEntry, reason)
	sc.mutex.Lock()
	if err != nil {
		return false, err
	}
	return true, nil
}

// recordDeletion appends path to result.Deleted if entry is non-nil
// and not already marked deleted.
func (sc *StatusCallback) recordDeletion(entry BinaryEntryInterface, path string) {
	if entry == nil {
		return
	}
	if isDeleted, err := entry.IsDeleted(); err == nil && !isDeleted {
		sc.result.Deleted = append(sc.result.Deleted, path)
	}
}

// recordAddition queues a hash request for a new file and appends
// it to result.Added. leftExhausted tags the verbose log line so
// debug output distinguishes the two call sites.
func (sc *StatusCallback) recordAddition(entry BinaryEntryInterface, path string, leftExhausted bool) error {
	if entry == nil {
		return nil
	}
	// Errors reading IsDeleted are treated as "skip", matching the
	// original inline logic (`err == nil && !isDeleted`).
	if isDeleted, _ := entry.IsDeleted(); isDeleted {
		return nil
	}
	logTag := ""
	if leftExhausted {
		logTag = " (left exhausted)"
	}
	if IsDebugEnabled("hash") {
		VerboseLog(3, "[STATUS-HASH] Requesting hash for new file%s: %s", logTag, path)
	}
	if err := entry.RequestHash(); err != nil {
		return err
	}
	sc.result.Added = append(sc.result.Added, path)
	if IsDebugEnabled("hash") {
		VerboseLog(3, "[STATUS-HASH] Successfully requested hash for new file%s: %s", logTag, path)
	}
	return nil
}

// OnLeftOnly handles remaining entries from the left iterator (deleted files)
func (sc *StatusCallback) OnLeftOnly(entry BinaryEntryInterface, path string) (bool, error) {
	if entry != nil {
		if isDeleted, err := entry.IsDeleted(); err == nil && !isDeleted {
			sc.mutex.Lock()
			sc.result.Deleted = append(sc.result.Deleted, path)
			sc.mutex.Unlock()

			// Deleted files don't need hashing, but DO get written to cache.idx
			// SubmitAndOrWriteHash will add to backlog for cache writing
			if err := sc.SubmitAndOrWriteHash(entry, "deleted"); err != nil {
				return false, err
			}
		}
	}
	return true, nil
}

// OnRightOnly handles remaining entries from the right iterator (new files)
func (sc *StatusCallback) OnRightOnly(entry BinaryEntryInterface, path string) (bool, error) {
	if entry != nil {
		if isDeleted, err := entry.IsDeleted(); err == nil && !isDeleted {
			// Add to result list
			sc.mutex.Lock()
			sc.result.Added = append(sc.result.Added, path)
			sc.mutex.Unlock()

			// Use unified hash coordination for new file
			if err := sc.SubmitAndOrWriteHash(entry, "new_file"); err != nil {
				return false, err
			}
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
	if IsDebugEnabled("write") {
		if path, err := entry.RelativePath(); err == nil {
			VerboseLog(3, "[STATUS-WRITE] Adding entry to backlog: %s", path)
		}
	}
	sc.backlog = append(sc.backlog, entry)
	if IsDebugEnabled("write") {
		VerboseLog(3, "[STATUS-WRITE] Backlog now contains %d entries", len(sc.backlog))
	}
}

// flushInOrderEntries processes backlog and pending entries, creating IoVecs for temp index writing
func (sc *StatusCallback) flushInOrderEntries() error {
	if IsDebugEnabled("write") {
		VerboseLog(3, "[STATUS-WRITE] Flushing entries: backlog=%d pending=%d", len(sc.backlog), len(sc.pendingEntries))
	}

	// Use counter to check for contiguous completed entries (no gaps)
	var readyIoVecs []syscall.Iovec

	// Process backlog entries that can be written in order
	for len(sc.backlog) > 0 {
		entry := sc.backlog[0]

		if IsDebugEnabled("write") {
			if path, err := entry.RelativePath(); err == nil {
				VerboseLog(3, "[STATUS-WRITE] Creating IoVec for backlog entry: %s", path)
			}
		}

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
		if IsDebugEnabled("write") {
			VerboseLog(3, "[STATUS-WRITE] Writing %d IoVecs to temp index", len(readyIoVecs))
		}
		return sc.writeIoVecBatchToTempIndex(readyIoVecs)
	}

	if IsDebugEnabled("write") {
		VerboseLog(3, "[STATUS-WRITE] No IoVecs ready to write")
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
	if IsDebugEnabled("write") {
		VerboseLog(3, "[STATUS-WRITE] writeIoVecBatchToTempIndex called with %d iovecs - BUT NOT IMPLEMENTED", len(iovecs))
	}
	// TODO: Implement proper IoVec batch writing to temp index
	// For now, just skip writing - this will be implemented when TempIndexWriter is available
	// The architecture framework is in place for iterative writing
	if IsDebugEnabled("write") {
		VerboseLog(3, "[STATUS-WRITE] WARNING: writeIoVecBatchToTempIndex is not implemented - no entries written!")
	}
	return nil
}

// SubmitAndOrWriteHash handles unified hash coordination and cache writing for Status command
func (sc *StatusCallback) SubmitAndOrWriteHash(entry BinaryEntryInterface, operation string) error {
	if IsDebugEnabled("hash") || IsDebugEnabled("write") {
		if path, err := entry.RelativePath(); err == nil {
			VerboseLog(3, "[STATUS-HASH] SubmitAndOrWriteHash called for %s: %s", operation, path)
		}
	}

	// Status callback always writes to cache temp index for performance optimization
	if entry == nil {
		return nil // Nothing to process
	}

	// Check if this entry needs hashing and writing
	needsHashing := (operation == "new_file") || (operation == "modified")

	// CRITICAL: Cache index excludes MainContext entries (no duplication with main.idx)
	entryContext, _ := entry.GetContext()
	needsWriting := (entryContext != MainContext)

	if needsHashing {
		// Submit hash job using the existing infrastructure
		if err := sc.submitHashJobToManager(entry); err != nil {
			return err
		}
	} else if needsWriting {
		// File unchanged or deleted - add directly to backlog for writing to cache
		// CRITICAL: cache.idx includes deleted entries but excludes MainContext entries
		sc.appendToBacklog(entry)
	}
	// MainContext entries: no writing to cache (already in main.idx)

	// Process any completed hash jobs and write in-order entries
	sc.processCompletedHashJobs()

	// Flush any ready entries to temp index
	if err := sc.flushInOrderEntries(); err != nil {
		return fmt.Errorf("failed to flush entries during hash coordination: %w", err)
	}

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
