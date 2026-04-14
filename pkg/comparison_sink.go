package dircachefilehash

import "fmt"

// updateComparisonSink implements ComparisonSink for the update pipeline.
// It classifies comparison results, determines which entries need hashing,
// and routes PipelineEntry values to either the hash channel (needs hash)
// or the bypass channel (no hash needed).
//
// This replaces the monolithic UpdateCallback.OnComparison method,
// separating classification from hash coordination and I/O.
type updateComparisonSink struct {
	dc       *DirectoryCache
	hashCh   chan<- *PipelineEntry
	bypassCh chan<- *PipelineEntry
	seqNum   uint64
}

// newUpdateComparisonSink creates a sink that routes entries into the update pipeline.
// The caller must close hashCh and bypassCh only after Close() returns.
func newUpdateComparisonSink(dc *DirectoryCache, hashCh, bypassCh chan<- *PipelineEntry) *updateComparisonSink {
	return &updateComparisonSink{
		dc:       dc,
		hashCh:   hashCh,
		bypassCh: bypassCh,
		seqNum:   0,
	}
}

// OnMatch handles entries present in both the existing index and the scan.
// No shouldIndex check needed — the scanner already filters by symlink mode
// and ignore patterns before entries reach this sink.
func (s *updateComparisonSink) OnMatch(left, right BinaryEntryInterface) error {
	// Skip already-deleted entries
	if isDeleted, err := left.IsDeleted(); err == nil && isDeleted {
		return nil
	}

	if needsHash(left, right) {
		// File changed — hash the scan entry, then write it
		s.emit(right, OpModified, true)
	} else {
		// File unchanged — use existing entry (already has hash)
		s.emit(left, OpUnchanged, false)
	}
	return nil
}

// OnLeftOnly handles entries only in the existing index (deleted from disk).
// For main.idx updates, deleted files are simply omitted (not written).
func (s *updateComparisonSink) OnLeftOnly(_ BinaryEntryInterface) error {
	// Deleted files are excluded from main.idx — do not emit
	return nil
}

// OnRightOnly handles entries only in the scan (new files).
// No shouldIndex check needed — the scanner already filters by symlink mode
// and ignore patterns before entries reach this sink.
func (s *updateComparisonSink) OnRightOnly(entry BinaryEntryInterface) error {
	// New file — needs hashing
	s.emit(entry, OpNewFile, true)
	return nil
}

// Close signals that no more entries will arrive. Closes both output channels.
func (s *updateComparisonSink) Close() error {
	close(s.hashCh)
	close(s.bypassCh)
	return nil
}

// emit creates a PipelineEntry and sends it to the appropriate channel.
func (s *updateComparisonSink) emit(entry BinaryEntryInterface, op PipelineOp, hash bool) {
	emitPipelineEntry(entry, op, hash, &s.seqNum, s.hashCh, s.bypassCh)
}

// statusComparisonSink implements ComparisonSink for the status pipeline.
// It compares main.idx (left) vs filesystem (right), using a pre-loaded
// merged cache as a hash lookup to avoid redundant hashing.
//
// Check order for each file on disk:
//  1. Check main — if metadata matches, file unchanged, skip entirely
//  2. Check merged cache — if metadata matches, use cached hash (no re-hash)
//  3. Neither matches — submit for hashing
//
// Only entries that differ from main are written to the new cache (sparse delta).
// The resulting cache file IS the status: its entries are the changes.
type statusComparisonSink struct {
	dc            *DirectoryCache
	cacheSkiplist *skiplistWrapper // pre-loaded merged cache for hash lookups
	hashCh        chan<- *PipelineEntry
	bypassCh      chan<- *PipelineEntry
	seqNum        uint64
}

// newStatusComparisonSink creates a sink that routes entries into the status pipeline.
// cacheSkiplist is the pre-loaded merge of all existing cache files, used to avoid
// re-hashing files whose metadata hasn't changed since the last status run.
func newStatusComparisonSink(dc *DirectoryCache, cacheSkiplist *skiplistWrapper, hashCh, bypassCh chan<- *PipelineEntry) *statusComparisonSink {
	return &statusComparisonSink{
		dc:            dc,
		cacheSkiplist: cacheSkiplist,
		hashCh:        hashCh,
		bypassCh:      bypassCh,
		seqNum:        0,
	}
}

// OnMatch handles entries present in both main.idx and the filesystem scan.
// No shouldIndex check needed — the scanner already filters by symlink mode
// and ignore patterns before entries reach this sink.
func (s *statusComparisonSink) OnMatch(left, right BinaryEntryInterface) error {
	// Skip already-deleted entries
	if isDeleted, err := left.IsDeleted(); err == nil && isDeleted {
		return nil
	}

	rightPath, err := right.RelativePath()
	if err != nil {
		return fmt.Errorf("failed to get right path: %w", err)
	}

	// Step 1: check main — if metadata matches, file unchanged, skip
	if !needsHash(left, right) {
		return nil
	}

	// File differs from main — check cache for a fresh entry
	if cached := s.cacheSkiplist.FindAsInterface(rightPath); cached != nil {
		if !needsHash(cached, right) {
			// Cache has a fresh entry — use it, no re-hash needed
			s.emit(cached, OpModified, false)
			return nil
		}
	}

	// Neither main nor cache is fresh — submit for hashing
	s.emit(right, OpModified, true)
	return nil
}

// OnLeftOnly handles entries only in main.idx (deleted from disk).
// Deleted entries are written to cache so the cache reflects the deletion.
func (s *statusComparisonSink) OnLeftOnly(entry BinaryEntryInterface) error {
	// Skip already-deleted entries
	if isDeleted, err := entry.IsDeleted(); err == nil && isDeleted {
		return nil
	}

	// Emit to bypass — cache retains deleted entries
	s.emit(entry, OpDeleted, false)
	return nil
}

// OnRightOnly handles entries only in the filesystem scan (new files, not in main).
// No shouldIndex check needed — the scanner already filters by symlink mode
// and ignore patterns before entries reach this sink.
func (s *statusComparisonSink) OnRightOnly(entry BinaryEntryInterface) error {
	rightPath, err := entry.RelativePath()
	if err != nil {
		return fmt.Errorf("failed to get right path: %w", err)
	}

	// Check cache for a fresh entry
	if cached := s.cacheSkiplist.FindAsInterface(rightPath); cached != nil {
		if !needsHash(cached, entry) {
			// Cache has a fresh entry — use it, no re-hash needed
			s.emit(cached, OpNewFile, false)
			return nil
		}
	}

	// No fresh cache entry — submit for hashing
	s.emit(entry, OpNewFile, true)
	return nil
}

// Close signals that no more entries will arrive. Closes both output channels.
func (s *statusComparisonSink) Close() error {
	close(s.hashCh)
	close(s.bypassCh)
	return nil
}

// emit creates a PipelineEntry and sends it to the appropriate channel.
func (s *statusComparisonSink) emit(entry BinaryEntryInterface, op PipelineOp, hash bool) {
	emitPipelineEntry(entry, op, hash, &s.seqNum, s.hashCh, s.bypassCh)
}

// emitPipelineEntry is the shared implementation used by both comparison sinks.
func emitPipelineEntry(entry BinaryEntryInterface, op PipelineOp, hash bool, seqNum *uint64, hashCh, bypassCh chan<- *PipelineEntry) {
	pe := &PipelineEntry{
		Entry:     entry,
		SeqNum:    *seqNum,
		Operation: op,
		NeedsHash: hash,
	}
	*seqNum++

	if hash {
		hashCh <- pe
	} else {
		bypassCh <- pe
	}
}

// sinkCallbackAdapter wraps a ComparisonSink as a HwangLinCallback so it can
// be used with the existing hwangLinUnified function without modifying it.
type sinkCallbackAdapter struct {
	CallbackBase
	sink ComparisonSink
}

// newSinkCallbackAdapter creates a HwangLinCallback that delegates to a ComparisonSink.
func newSinkCallbackAdapter(sink ComparisonSink) *sinkCallbackAdapter {
	return &sinkCallbackAdapter{
		CallbackBase: CallbackBase{name: "pipeline"},
		sink:         sink,
	}
}

func (a *sinkCallbackAdapter) OnComparison(
	result ComparisonResult,
	leftEntry, rightEntry BinaryEntryInterface,
	leftPath, rightPath string,
) (bool, error) {
	switch result {
	case ComparisonMatch:
		if err := a.sink.OnMatch(leftEntry, rightEntry); err != nil {
			return false, err
		}
	case ComparisonLeftFirst:
		if leftEntry != nil {
			if err := a.sink.OnLeftOnly(leftEntry); err != nil {
				return false, err
			}
		}
	case ComparisonRightFirst:
		if rightEntry != nil {
			if err := a.sink.OnRightOnly(rightEntry); err != nil {
				return false, err
			}
		}
	}
	return true, nil
}

func (a *sinkCallbackAdapter) OnLeftOnly(entry BinaryEntryInterface, path string) (bool, error) {
	if err := a.sink.OnLeftOnly(entry); err != nil {
		return false, err
	}
	return true, nil
}

func (a *sinkCallbackAdapter) OnRightOnly(entry BinaryEntryInterface, path string) (bool, error) {
	if err := a.sink.OnRightOnly(entry); err != nil {
		return false, err
	}
	return true, nil
}

func (a *sinkCallbackAdapter) OnComplete(err error) error {
	return a.sink.Close()
}

func (a *sinkCallbackAdapter) SubmitAndOrWriteHash(entry BinaryEntryInterface, operation string) error {
	return nil // Not used — sink handles routing internally
}
