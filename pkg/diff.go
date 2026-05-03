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
// OpenRef materialises both sides into hashed iterators (fs-scan opens
// itself by side-effecting a cache refresh, so callers don't have to
// think about cache lifecycle) and hwangLin drives a
// diffComparisonSink to accumulate the result. A non-nil filter narrows
// the result without affecting cache writes — entries filtered out are
// excluded from Modified/Added/Deleted slices and from byte counts.
func Diff(ctx context.Context, dc *DirectoryCache, sr *ScanRun, leftRef, rightRef IndexRef, filter FilterExpr) (*StatusResult, error) {
	leftIter, leftClose, err := OpenRef(ctx, dc, sr, leftRef)
	if err != nil {
		return nil, fmt.Errorf("diff: open left %s: %w", leftRef.Type, err)
	}
	defer func() { _ = leftClose() }()

	rightIter, rightClose, err := OpenRef(ctx, dc, sr, rightRef)
	if err != nil {
		return nil, fmt.Errorf("diff: open right %s: %w", rightRef.Type, err)
	}
	defer func() { _ = rightClose() }()

	sink := newDiffComparisonSink(filter)
	adapter := newSinkCallbackAdapter(sink)
	if err := hwangLin(leftIter, rightIter, adapter, ctx); err != nil {
		return nil, fmt.Errorf("diff: %w", err)
	}
	return sink.result, nil
}
