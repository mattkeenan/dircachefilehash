package dircachefilehash

import (
	"context"
	"fmt"
	"path/filepath"
)

// IndexRef.Type vocabulary recognised by OpenRef.
//
// File-backed (set by ResolveIndexSelectors from on-disk paths):
//   - "main"       — the canonical main.idx
//   - "cache"      — the cache.idx (sparse delta over main)
//   - "scan"       — a scan-<id>.idx file (transient)
//   - "file"       — an arbitrary .idx path
//
// Virtual (no Path; materialised on Open):
//   - "cache+main" — cache deltas applied over main; cache wins
//   - "fs-scan"    — live filesystem state (refreshing cache.idx as a
//     side-effect; the result is exposed as cache+main)
//   - "snapshot"   — a snapshot's main.idx, identified by SnapshotID
const (
	RefTypeMain      = "main"
	RefTypeCache     = "cache"
	RefTypeScan      = "scan"
	RefTypeFile      = "file"
	RefTypeCacheMain = "cache+main"
	RefTypeFsScan    = "fs-scan"
	RefTypeSnapshot  = "snapshot"
)

// OpenRef dispatches on ref.Type and returns a path-sorted iterator backed
// by the resolved data, plus a closer that releases any resources held for
// the duration of the iteration.
//
// The returned iterator is safe to drive directly with hwangLin.
// Callers MUST invoke the closer when done — for in-memory skiplists owned
// by dc this is a no-op; for ad-hoc mmap'd files (snapshot/file/scan) the
// closer DecRefs the underlying mapping.
//
// fs-scan is a side-effecting case: opening it triggers a Status-style
// pipeline run that refreshes cache.idx, after which the iterator exposes
// cache+main. The user-visible cache write semantics are preserved by
// design — every fs-scan banks its hashing work, regardless of which Diff
// caller asked for it.
func OpenRef(ctx context.Context, dc *DirectoryCache, ref IndexRef) (BinaryEntryIterator, func() error, error) {
	switch ref.Type {
	case RefTypeMain:
		sl, err := dc.LoadMainIndex()
		if err != nil {
			return nil, nil, fmt.Errorf("OpenRef main: %w", err)
		}
		return NewBinaryEntrySkiplistIterator(ctx, sl, "main"), noopCloser, nil

	case RefTypeCache:
		sl, err := dc.loadCacheIndex()
		if err != nil {
			return nil, nil, fmt.Errorf("OpenRef cache: %w", err)
		}
		return NewBinaryEntrySkiplistIterator(ctx, sl, "cache"), noopCloser, nil

	case RefTypeCacheMain:
		sl, err := dc.LoadMergedMainCacheIndex()
		if err != nil {
			return nil, nil, fmt.Errorf("OpenRef cache+main: %w", err)
		}
		return NewBinaryEntrySkiplistIterator(ctx, sl, "cache+main"), noopCloser, nil

	case RefTypeFile, RefTypeScan:
		return openFileRef(ctx, dc, ref)

	case RefTypeSnapshot:
		return openSnapshotRef(ctx, dc, ref)

	case RefTypeFsScan:
		return openFsScanRef(ctx, dc)

	default:
		return nil, nil, fmt.Errorf("OpenRef: unknown ref type %q", ref.Type)
	}
}

// openFileRef opens an arbitrary .idx file (or a scan-<id>.idx) as an
// iterator. The returned closer DecRefs the mmap so the file descriptor
// and mapping are released when the caller finishes iterating.
func openFileRef(ctx context.Context, dc *DirectoryCache, ref IndexRef) (BinaryEntryIterator, func() error, error) {
	refs, indexFile, err := dc.loadIndexFromFileWithTracking(ref.Path)
	if err != nil {
		return nil, nil, fmt.Errorf("OpenRef %s %s: %w", ref.Type, ref.Path, err)
	}
	name := ref.Type
	if ref.ScanID != "" {
		name = "scan-" + ref.ScanID
	}
	sl := buildSkiplistFromRefs(refs, ScanContext)
	closer := func() error {
		if indexFile != nil {
			indexFile.DecRef()
		}
		return nil
	}
	return NewBinaryEntrySkiplistIterator(ctx, sl, name), closer, nil
}

// openSnapshotRef resolves a snapshot's main.idx file and opens it as an
// iterator. SnapshotID may be either an exact ID (timestamp form) or a tag;
// tag lookups return the most recent matching snapshot.
//
// Goes through the read-only mmap memo, so repeated `dcfh snapshot status`
// calls against the same snapshot in one process share a single mapping.
// The memo owns lifetime; the returned closer is a no-op.
func openSnapshotRef(ctx context.Context, dc *DirectoryCache, ref IndexRef) (BinaryEntryIterator, func() error, error) {
	if ref.SnapshotID == "" {
		return nil, nil, fmt.Errorf("OpenRef snapshot: SnapshotID is required")
	}
	id, err := ResolveSnapshotID(dc.MetaDir, ref.SnapshotID)
	if err != nil {
		return nil, nil, fmt.Errorf("OpenRef snapshot %q: %w", ref.SnapshotID, err)
	}
	path := filepath.Join(dc.MetaDir, "snapshots", id, "main.idx")
	_, refs, err := dc.loadIndexShared(path)
	if err != nil {
		return nil, nil, fmt.Errorf("OpenRef snapshot %s: %w", id, err)
	}
	sl := buildSkiplistFromRefs(refs, MainContext)
	return NewBinaryEntrySkiplistIterator(ctx, sl, "snapshot-"+id), noopCloser, nil
}

// openFsScanRef materialises the live filesystem as a cache+main iterator.
// The cache write is a structural property of opening fs-scan — every
// scan banks its hashing work into cache.idx — so callers driving Diff
// over fs-scan never have to think about cache lifecycle.
func openFsScanRef(ctx context.Context, dc *DirectoryCache) (BinaryEntryIterator, func() error, error) {
	merged, err := dc.refreshFsScanCache(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("OpenRef fs-scan: %w", err)
	}
	return NewBinaryEntrySkiplistIterator(ctx, merged, "fs-scan"), noopCloser, nil
}

func buildSkiplistFromRefs(refs []binaryEntryRef, ctx string) *skiplistWrapper {
	sl := NewSkiplistWrapper(16, ctx)
	for _, ref := range refs {
		sl.Insert(ref, ctx)
	}
	return sl
}

func noopCloser() error { return nil }
