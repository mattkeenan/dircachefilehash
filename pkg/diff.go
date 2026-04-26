package dircachefilehash

import (
	"context"
	"fmt"
)

// Diff compares two index references and returns a StatusResult describing
// the differences. Either side may be a virtual ref (cache+main, fs-scan,
// snapshot) or a concrete file/skiplist ref.
//
// Result semantics, in terms of right vs left:
//   - Modified — paths present in both, with differing content hashes
//   - Added    — paths present only on the right
//   - Deleted  — paths present only on the left
//
// Internal dispatch by participation of fs-scan:
//
//  1. Neither side is fs-scan — open both refs and walk them with
//     hwangLinUnified driving a diffComparisonSink. Both sides must already
//     carry hashes for live entries; the dispatcher guarantees this.
//
//  2. main vs fs-scan — delegated to dc.Status to preserve byte-equivalence
//     with the long-standing dcfh status semantics. Once Phase 2 lands the
//     cache-write moves into the fs-scan open-path and this special-case
//     collapses into case 1.
//
//  3. X vs fs-scan (or fs-scan vs X) where X != main — refresh cache via
//     dc.Status (banking the hashing work), substitute fs-scan with
//     cache+main, then dispatch to case 1.
func Diff(ctx context.Context, dc *DirectoryCache, leftRef, rightRef IndexRef) (*StatusResult, error) {
	// Case 2: byte-equivalent to dcfh status. Avoids running diff
	// classification twice (once via the pipeline, once via the sink) and
	// keeps Phase 1 free of behavioural drift on the most-trodden path.
	if leftRef.Type == RefTypeMain && rightRef.Type == RefTypeFsScan {
		return dc.Status(ctx, nil)
	}

	// Case 3: refresh cache so fs-scan can be replayed as cache+main. Only
	// run dc.Status once even when both sides reference fs-scan — a second
	// scan would be wasted work over the same filesystem state.
	if leftRef.Type == RefTypeFsScan || rightRef.Type == RefTypeFsScan {
		if _, err := dc.Status(ctx, nil); err != nil {
			return nil, fmt.Errorf("diff: refresh cache for fs-scan: %w", err)
		}
		if leftRef.Type == RefTypeFsScan {
			leftRef = IndexRef{Type: RefTypeCacheMain}
		}
		if rightRef.Type == RefTypeFsScan {
			rightRef = IndexRef{Type: RefTypeCacheMain}
		}
	}

	return diffOpenRefs(ctx, dc, leftRef, rightRef)
}

// diffOpenRefs is case 1 of Diff: open both refs, walk them with the
// generic comparison sink, return the accumulated StatusResult. Both refs
// must have already been resolved away from fs-scan by the caller.
func diffOpenRefs(ctx context.Context, dc *DirectoryCache, leftRef, rightRef IndexRef) (*StatusResult, error) {
	leftIter, leftClose, err := OpenRef(ctx, dc, leftRef)
	if err != nil {
		return nil, fmt.Errorf("diff: open left %s: %w", leftRef.Type, err)
	}
	defer func() { _ = leftClose() }()

	rightIter, rightClose, err := OpenRef(ctx, dc, rightRef)
	if err != nil {
		return nil, fmt.Errorf("diff: open right %s: %w", rightRef.Type, err)
	}
	defer func() { _ = rightClose() }()

	sink := newDiffComparisonSink()
	adapter := newSinkCallbackAdapter(sink)
	if err := hwangLinUnified(leftIter, rightIter, adapter, ctx); err != nil {
		return nil, fmt.Errorf("diff: %w", err)
	}
	return sink.result, nil
}
