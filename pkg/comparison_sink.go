package dircachefilehash

import "fmt"

// scanWritePolicy selects the inclusion rules for scanWriteSink. The two
// callers — `dcfh update` (main.idx) and the cache-refresh half of
// `dcfh status` / fs-scan (cache.idx) — share most pipeline plumbing and
// differ only in what to write per case:
//
//	scanWriteCanonical → write every on-disk entry, no deletions.
//	                     Used for main.idx; the file IS the canonical
//	                     view of all known files with their hashes.
//
//	scanWriteDelta     → write only changes, keep deletions.
//	                     Used for cache.idx; the file IS the diff vs main.
type scanWritePolicy int

const (
	scanWriteCanonical scanWritePolicy = iota
	scanWriteDelta
)

// scanWriteSink is the unified pipeline-internal ComparisonSink. It
// classifies comparison results, decides hash-vs-bypass routing, and
// emits PipelineEntry values; the writer stage downstream decides which
// file to flush them to.
//
// hashLookup is an optional pre-loaded skiplist consulted to skip
// re-hashing when metadata indicates a change but a previous run already
// hashed the new bytes (cache.idx serves as that lookup for the Delta
// caller). Pass nil for the Canonical case.
type scanWriteSink struct {
	hashLookup *skiplistWrapper
	policy     scanWritePolicy
	hashCh     chan<- *PipelineEntry
	bypassCh   chan<- *PipelineEntry
	collector  *changeCollector
	seqNum     uint64
}

// newScanWriteSink creates the unified update/cache-refresh sink. The
// caller must close hashCh and bypassCh only after Close() returns.
//
// collector is the optional post-run change-set recorder. It must be
// supplied ONLY for the canonical update pass (main.idx); the
// cache-refresh delta pass passes nil, otherwise its second scan would
// double-record the change-set. nil disables recording entirely.
func newScanWriteSink(hashLookup *skiplistWrapper, policy scanWritePolicy, hashCh, bypassCh chan<- *PipelineEntry, collector *changeCollector) *scanWriteSink {
	return &scanWriteSink{
		hashLookup: hashLookup,
		policy:     policy,
		hashCh:     hashCh,
		bypassCh:   bypassCh,
		collector:  collector,
	}
}

// record appends an op-classified path to the collector (if any). On a
// RelativePath() error the path is dropped — the viewer pane is
// cosmetic and must never abort an otherwise-successful update.
func (s *scanWriteSink) record(op PipelineOp, entry BinaryEntryInterface) {
	if s.collector == nil {
		return
	}
	path, err := entry.RelativePath()
	if err != nil {
		return
	}
	s.collector.add(op, path)
}

// OnMatch handles entries present in both the existing index and the scan.
// No shouldIndex check needed — the scanner already filters by symlink mode
// and ignore patterns before entries reach this sink.
func (s *scanWriteSink) OnMatch(left, right BinaryEntryInterface) error {
	if isDeleted, err := left.IsDeleted(); err == nil && isDeleted {
		return nil
	}

	if !needsHash(left, right) {
		if s.policy == scanWriteCanonical {
			s.emit(left, OpUnchanged, false)
		}
		return nil
	}
	s.record(OpModified, right)
	return s.emitHashed(right, OpModified)
}

// OnLeftOnly handles entries only in the existing index (deleted from disk).
// Canonical drops them (main.idx omits deletions); Delta keeps them so the
// cache reflects what was removed.
func (s *scanWriteSink) OnLeftOnly(entry BinaryEntryInterface) error {
	// An already-tombstoned left entry is not a new deletion — skip it
	// for both the change-set and the emit (matches diffComparisonSink).
	if isDeleted, err := entry.IsDeleted(); err == nil && isDeleted {
		return nil
	}
	s.record(OpDeleted, entry)
	if s.policy == scanWriteCanonical {
		// Canonical (main.idx) omits deletions from the written index,
		// but the change-set still wants them — recorded above.
		return nil
	}
	s.emit(entry, OpDeleted, false)
	return nil
}

// OnRightOnly handles entries only in the scan (new files). Both policies
// write them; Delta consults the hash lookup first to skip re-hashing.
func (s *scanWriteSink) OnRightOnly(entry BinaryEntryInterface) error {
	s.record(OpNewFile, entry)
	return s.emitHashed(entry, OpNewFile)
}

// emitHashed routes a known-changed scan entry: try the optional hash
// lookup first (cache hit → bypass the hash workers), otherwise submit
// for hashing. Shared by OnMatch's modified branch and OnRightOnly.
func (s *scanWriteSink) emitHashed(scanned BinaryEntryInterface, op PipelineOp) error {
	if s.hashLookup != nil {
		path, err := scanned.RelativePath()
		if err != nil {
			return fmt.Errorf("failed to get scanned path: %w", err)
		}
		if cached := s.hashLookup.FindAsInterface(path); cached != nil {
			if !needsHash(cached, scanned) {
				s.emit(cached, op, false)
				return nil
			}
		}
	}
	s.emit(scanned, op, true)
	return nil
}

// Close signals that no more entries will arrive. Closes both output channels.
func (s *scanWriteSink) Close() error {
	close(s.hashCh)
	close(s.bypassCh)
	return nil
}

// emit creates a PipelineEntry and sends it to the appropriate channel.
func (s *scanWriteSink) emit(entry BinaryEntryInterface, op PipelineOp, hash bool) {
	emitPipelineEntry(entry, op, hash, &s.seqNum, s.hashCh, s.bypassCh)
}

// diffComparisonSink implements ComparisonSink for the generic Diff engine.
// It accumulates a *StatusResult directly during the Hwang-Lin walk by
// classifying every comparison purely on hash equality — no metadata
// fast-path, no cache lookup, no hashing.
//
// Pre-condition: both iterators must already carry hashes for every live
// entry. The Diff engine guarantees this by routing fs-scan refs through
// a cache-refreshing materialiser before opening them.
//
// Deleted entries are treated as "not present on that side": a deleted left
// + live right collapses to OnRightOnly (added); the symmetric case
// collapses to OnLeftOnly (deleted); both-deleted is silently dropped.
//
// filter, when non-nil, gates whether an entry contributes to the result
// (path slice + byte counter). The right entry is evaluated for OnMatch
// — that's the post-change shape users filter on. Failed predicate
// evaluations are silently dropped (treated as non-match) to keep status
// useful in the face of e.g. a transient mmap error on one entry.
type diffComparisonSink struct {
	result *StatusResult
	filter FilterExpr
	ctx    *FilterContext
}

func newDiffComparisonSink(filter FilterExpr) *diffComparisonSink {
	return &diffComparisonSink{
		result: &StatusResult{
			Modified: make([]string, 0),
			Added:    make([]string, 0),
			Deleted:  make([]string, 0),
		},
		filter: filter,
		ctx:    &FilterContext{IndexType: "diff"},
	}
}

// keep returns true when entry passes the filter (or no filter is set).
// Errors collapse to "skip this entry" — see the type comment for why.
func (s *diffComparisonSink) keep(entry BinaryEntryInterface) bool {
	if s.filter == nil {
		return true
	}
	ok, err := s.filter.Evaluate(entry, s.ctx)
	return err == nil && ok
}

func (s *diffComparisonSink) OnMatch(left, right BinaryEntryInterface) error {
	leftDel, _ := left.IsDeleted()
	rightDel, _ := right.IsDeleted()

	if leftDel && rightDel {
		return nil
	}
	if leftDel {
		// Already know rightDel==false; avoid re-checking inside the helper.
		if !s.keep(right) {
			return nil
		}
		return s.recordAdded(right)
	}
	if rightDel {
		if !s.keep(left) {
			return nil
		}
		return s.recordDeleted(left)
	}

	leftHash, err := left.Hash()
	if err != nil {
		return fmt.Errorf("diff: left hash: %w", err)
	}
	rightHash, err := right.Hash()
	if err != nil {
		return fmt.Errorf("diff: right hash: %w", err)
	}
	if leftHash == rightHash {
		return nil
	}

	if !s.keep(right) {
		return nil
	}
	path, err := right.RelativePath()
	if err != nil {
		return fmt.Errorf("diff: right path: %w", err)
	}
	size, _ := right.FileSize()
	s.result.Modified = append(s.result.Modified, path)
	s.result.ModifiedBytes += size
	return nil
}

func (s *diffComparisonSink) OnLeftOnly(entry BinaryEntryInterface) error {
	if d, _ := entry.IsDeleted(); d {
		return nil
	}
	if !s.keep(entry) {
		return nil
	}
	return s.recordDeleted(entry)
}

func (s *diffComparisonSink) OnRightOnly(entry BinaryEntryInterface) error {
	if d, _ := entry.IsDeleted(); d {
		return nil
	}
	if !s.keep(entry) {
		return nil
	}
	return s.recordAdded(entry)
}

// recordAdded / recordDeleted append to the result without re-checking
// the deletion bit — callers (OnMatch, OnLeftOnly, OnRightOnly) have
// already settled it.
func (s *diffComparisonSink) recordAdded(entry BinaryEntryInterface) error {
	path, err := entry.RelativePath()
	if err != nil {
		return fmt.Errorf("diff: right path: %w", err)
	}
	size, _ := entry.FileSize()
	s.result.Added = append(s.result.Added, path)
	s.result.AddedBytes += size
	return nil
}

func (s *diffComparisonSink) recordDeleted(entry BinaryEntryInterface) error {
	path, err := entry.RelativePath()
	if err != nil {
		return fmt.Errorf("diff: left path: %w", err)
	}
	size, _ := entry.FileSize()
	s.result.Deleted = append(s.result.Deleted, path)
	s.result.DeletedBytes += size
	return nil
}

func (s *diffComparisonSink) Close() error { return nil }

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
// be used with the existing hwangLin function without modifying it.
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
