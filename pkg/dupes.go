package dircachefilehash

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"unsafe"
)

// DuplicateGroup represents a group of files with the same hash
type DuplicateGroup struct {
	Hash  string   `json:"hash"`
	Files []string `json:"files"`
	Count int      `json:"count"`
}

// DupeFilter narrows which index entries participate. Zero value is the
// whole-repo fast path. Paths are forward-slash, trailing "/"; Exclusive
// is ignored when Paths is empty. IgnoreHardlinks, when true, collapses
// entries sharing the same (Dev, Ino) pair inside a hash-group to a
// single representative path, so hardlinks to one inode are not reported
// as duplicates.
//
// Predicate is the per-entry filter (size / mtime / name / hash / …)
// shared with `dcfh status`, `dcfh update`, and `dcfhfind` — see
// pkg/flat_filter.go BuildFilter. Nil means "no per-entry filter", in
// which case entryKeeper returns a literal-true closure to keep the
// hot loop branch-light. Predicate runs *before* hash-bucketing so a
// group that loses members below the ≥2 threshold is never emitted.
type DupeFilter struct {
	Paths           []string   `json:"paths,omitempty"`
	Exclusive       bool       `json:"exclusive,omitempty"`
	IgnoreHardlinks bool       `json:"ignore_hardlinks,omitempty"`
	Predicate       FilterExpr `json:"-"`

	// Prints/Ignores/NoIgnoreFile carry the same scope-marker shape as
	// DiffRequest so a wire client (or a CLI that hasn't pre-built the
	// predicate) can ship the segments. localRepo.Groups composes them
	// into Predicate when Predicate is nil. NoIgnoreFile is accepted for
	// API symmetry but has no effect — dupes is index-only and never
	// triggers a scan, so .dcfh/ignore patterns aren't consulted.
	Prints       []FilterOptions `json:"prints,omitempty"`
	Ignores      []FilterOptions `json:"ignores,omitempty"`
	NoIgnoreFile bool            `json:"no_ignore_file,omitempty"`
}

// FindDuplicates returns groups of files with identical content hashes
// as recorded in the merged main+cache index.
//
// This is a one-pass, index-only operation — no filesystem scan runs.
// Files removed from disk since the last `dcfh update` will still appear
// here; run `dcfh update` first if on-disk truth matters.
//
// Algorithm: bucket every live index entry by the first eight bytes of
// its stored hash (reinterpreted as a uint64 — Go maps hash a single
// word, no allocations, no interface dispatch). After the pass, each
// bucket holding ≥2 entries is resolved into a concrete DuplicateGroup;
// prefix collisions (2⁻⁶⁴ per pair) are disambiguated by full-hash
// comparison inside the bucket. Path strings are materialised only for
// surviving duplicates, so a 1M-file index with 0.1 % duplicates does
// ~1 000 path allocations instead of 1M.
//
// Safety: the merged main+cache skiplist is backed by read-only mmaps
// that dcfh never mremap's, so `*binaryEntry` pointers obtained via
// ForEach stay valid for the call's duration. Scan indices (which do
// grow under mremap) are never in this skiplist.
//
// Size and date filters are applied before bucketing so a group that
// loses members below the ≥2 threshold is never emitted. Path
// filtering uses the same pre-bucket path only when Exclusive is true;
// Exclusive=false buckets the whole index and drops whole groups in
// appendBucketGroups so cross-prefix groups are preserved intact.
//
// IgnoreHardlinks is applied after hash-bucketing in appendBucketGroups:
// entries sharing (Dev, Ino) within a full-hash subgroup collapse to
// the first (path-sorted) occurrence, and the ≥2 threshold is re-checked
// so a group of pure hardlink siblings disappears.
func (dc *DirectoryCache) FindDuplicates(ctx context.Context, flags map[string]string, filter DupeFilter) ([]DuplicateGroup, error) {
	if err := dc.ApplyConfigOverrides(flags); err != nil {
		// No config loaded (fresh/partial repos): honour --symlinks
		// directly so the call still succeeds.
		if symlinkMode, ok := flags["symlinks"]; ok {
			dc.symlinkMode = symlinkMode
		}
	}

	skiplist, err := dc.LoadMergedMainCacheIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to load merged index: %w", err)
	}

	pathExclusive := filter.Exclusive && len(filter.Paths) > 0
	keep := entryKeeper(filter, pathExclusive)

	buckets := make(map[uint64][]*binaryEntry, max(skiplist.Length()/4, 16))

	var iterErr error
	skiplist.ForEach(func(entry *binaryEntry, _ string) bool {
		if err := ctx.Err(); err != nil {
			iterErr = err
			return false
		}
		if entry.IsDeleted() || !keep(entry) {
			return true
		}
		key := *(*uint64)(unsafe.Pointer(&entry.Hash[0]))
		buckets[key] = append(buckets[key], entry)
		return true
	})
	if iterErr != nil {
		return nil, iterErr
	}

	out := make([]DuplicateGroup, 0, len(buckets)/16+1)
	post := postBucketCtx{
		filter:      &filter,
		filterAfter: !filter.Exclusive && len(filter.Paths) > 0,
	}
	for _, entries := range buckets {
		if len(entries) < 2 {
			continue
		}
		appendBucketGroups(entries, &out, post)
	}
	slices.SortFunc(out, func(a, b DuplicateGroup) int {
		return strings.Compare(a.Hash, b.Hash)
	})
	return out, nil
}

// entryKeeper returns a predicate that applies the pre-bucket filters
// (Predicate evaluation + path-exclusive). The zero-filter fast path
// returns a literal-true closure so the ForEach loop stays branch-light
// for the whole-repo `dcfh dupes` invocation.
func entryKeeper(filter DupeFilter, pathExclusive bool) func(*binaryEntry) bool {
	hasPred := filter.Predicate != nil
	if !pathExclusive && !hasPred {
		return func(*binaryEntry) bool { return true }
	}
	ctx := &FilterContext{IndexType: "dupes"}
	return func(entry *binaryEntry) bool {
		if pathExclusive && !pathMatchesPrefix(entry.RelativePath(), filter.Paths) {
			return false
		}
		if hasPred {
			ok, err := filter.Predicate.Evaluate(entry.asFilterEntry(), ctx)
			if err != nil || !ok {
				return false
			}
		}
		return true
	}
}

// pathMatchesPrefix reports whether rel falls under any of the given
// directory prefixes. prefixes are forward-slash paths ending in "/",
// so no trailing-slash edge cases arise.
func pathMatchesPrefix(rel string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(rel, p) {
			return true
		}
	}
	return false
}

// postBucketCtx carries the inputs appendBucketGroups needs to decide
// whether (and in what shape) to emit a hash-matched group. New
// post-bucket filters should add a field here rather than grow the
// appendBucketGroups signature; precomputed derivations (like
// filterAfter) live alongside the filter pointer.
type postBucketCtx struct {
	filter      *DupeFilter
	filterAfter bool // !filter.Exclusive && len(filter.Paths) > 0
}

// appendBucketGroups splits a prefix-collision bucket into per-full-hash
// subgroups, dropping singletons, and appends any group of ≥2 files to
// *out. Input is already in skiplist (path) order, so emitted groups
// have path-sorted Files slices for free — no per-group sort.
//
// Post-bucket filters live in emitHashGroup, in two flavours:
//   - group transformers (may shrink a group): e.g. IgnoreHardlinks via
//     dedupByInode. Applied before the ≥2 threshold check.
//   - group predicates (keep-or-drop): e.g. the ctx.filterAfter path
//     check for non-exclusive mode. Applied after the threshold check.
func appendBucketGroups(entries []*binaryEntry, out *[]DuplicateGroup, ctx postBucketCtx) {
	// Pair-fast-path: two entries sharing a uint64 prefix are almost
	// always a real duplicate. Compare full hashes to be sure.
	if len(entries) == 2 {
		if !sameHash(entries[0], entries[1]) {
			return
		}
		if g, ok := emitHashGroup(entries, ctx); ok {
			*out = append(*out, g)
		}
		return
	}

	// General case: partition by full hash array (64 bytes regardless
	// of algorithm — unused tail is zero-padded on write).
	sub := make(map[[64]byte][]*binaryEntry, len(entries))
	for _, e := range entries {
		sub[e.Hash] = append(sub[e.Hash], e)
	}
	for _, group := range sub {
		if g, ok := emitHashGroup(group, ctx); ok {
			*out = append(*out, g)
		}
	}
}

// emitHashGroup applies post-bucket filters to a same-hash group and
// returns the DuplicateGroup to emit, or ok=false to skip. Callers
// must guarantee every entry in group shares the same full hash.
func emitHashGroup(group []*binaryEntry, ctx postBucketCtx) (DuplicateGroup, bool) {
	if ctx.filter.IgnoreHardlinks {
		group = dedupByInode(group)
	}
	if len(group) < 2 {
		return DuplicateGroup{}, false
	}
	files := make([]string, len(group))
	for i, e := range group {
		files[i] = e.RelativePath()
	}
	if ctx.filterAfter && !anyPathMatchesPrefix(files, ctx.filter.Paths) {
		return DuplicateGroup{}, false
	}
	return DuplicateGroup{
		Hash:  group[0].HashString(),
		Files: files,
		Count: len(files),
	}, true
}

// anyPathMatchesPrefix reports whether any file falls under one of the
// given directory prefixes.
func anyPathMatchesPrefix(files []string, prefixes []string) bool {
	for _, f := range files {
		if pathMatchesPrefix(f, prefixes) {
			return true
		}
	}
	return false
}

// dedupByInode collapses entries sharing (Dev, Ino) to the first
// occurrence in the input slice. Input is already path-sorted (skiplist
// iteration order) so the kept representative is the lowest path per
// inode. Returns the input slice compacted in place when safe, else a
// fresh slice — either way the caller must use the returned slice.
func dedupByInode(entries []*binaryEntry) []*binaryEntry {
	if len(entries) < 2 {
		return entries
	}
	seen := make(map[[2]uint32]struct{}, len(entries))
	out := entries[:0]
	for _, e := range entries {
		key := [2]uint32{e.Dev, e.Ino}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, e)
	}
	return out
}

// sameHash reports whether two entries carry identical content hashes.
// Only HashSize bytes are significant; entries with different HashType
// are not duplicates even if their first bytes happen to match.
func sameHash(a, b *binaryEntry) bool {
	if a.HashType != b.HashType {
		return false
	}
	n := hashSizeForType(a.HashType)
	return bytes.Equal(a.Hash[:n], b.Hash[:n])
}

// hashSizeForType returns the number of meaningful bytes at the start
// of binaryEntry.Hash for the given algorithm. Centralises the switch
// repeated inline across pkg/util.go and pkg/binary_entry_scan.go.
func hashSizeForType(t uint16) int {
	switch t {
	case HashTypeSHA256:
		return HashSizeSHA256
	case HashTypeSHA512:
		return HashSizeSHA512
	default:
		return HashSizeSHA1
	}
}
