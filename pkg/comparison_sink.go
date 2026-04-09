package dircachefilehash

import (
	"fmt"
	"os"
)

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
func (s *updateComparisonSink) OnMatch(left, right BinaryEntryInterface) error {
	// Skip already-deleted entries
	if isDeleted, err := left.IsDeleted(); err == nil && isDeleted {
		return nil
	}

	// Check if file should still be indexed
	rightPath, err := right.RelativePath()
	if err != nil {
		return fmt.Errorf("failed to get right path: %w", err)
	}
	if !s.dc.shouldIndex(rightPath) {
		return nil // skip, main.idx excludes unindexed files
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
func (s *updateComparisonSink) OnLeftOnly(entry BinaryEntryInterface) error {
	// Skip already-deleted entries
	if isDeleted, err := entry.IsDeleted(); err == nil && isDeleted {
		return nil
	}

	leftPath, err := entry.RelativePath()
	if err != nil {
		return fmt.Errorf("failed to get left path: %w", err)
	}
	if !s.dc.shouldIndex(leftPath) {
		return nil
	}

	// Deleted files are excluded from main.idx — do not emit
	return nil
}

// OnRightOnly handles entries only in the scan (new files).
func (s *updateComparisonSink) OnRightOnly(entry BinaryEntryInterface) error {
	rightPath, err := entry.RelativePath()
	if err != nil {
		return fmt.Errorf("failed to get right path: %w", err)
	}

	if !s.dc.shouldIndex(rightPath) {
		if IsDebugEnabled("verbose-3") {
			fmt.Fprintf(os.Stderr, "[VERBOSE-3] ComparisonSink: Skipping %s - shouldIndex returned false\n", rightPath)
		}
		return nil
	}

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
	pe := &PipelineEntry{
		Entry:     entry,
		SeqNum:    s.seqNum,
		Operation: op,
		NeedsHash: hash,
	}
	s.seqNum++

	if hash {
		s.hashCh <- pe
	} else {
		s.bypassCh <- pe
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
