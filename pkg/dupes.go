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
func (dc *DirectoryCache) FindDuplicates(ctx context.Context, flags map[string]string) ([]DuplicateGroup, error) {
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

	buckets := make(map[uint64][]*binaryEntry, max(skiplist.Length()/4, 16))

	var iterErr error
	skiplist.ForEach(func(entry *binaryEntry, _ string) bool {
		if err := ctx.Err(); err != nil {
			iterErr = err
			return false
		}
		if entry.IsDeleted() {
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
	for _, entries := range buckets {
		if len(entries) < 2 {
			continue
		}
		appendBucketGroups(entries, &out)
	}
	slices.SortFunc(out, func(a, b DuplicateGroup) int {
		return strings.Compare(a.Hash, b.Hash)
	})
	return out, nil
}

// FindDuplicatesUnified is the legacy name for FindDuplicates. It
// remains callable so the Repo interface and existing benchmarks don't
// need churn; new callers should use FindDuplicates directly.
func (dc *DirectoryCache) FindDuplicatesUnified(ctx context.Context, flags map[string]string) ([]DuplicateGroup, error) {
	return dc.FindDuplicates(ctx, flags)
}

// appendBucketGroups splits a prefix-collision bucket into per-full-hash
// subgroups, dropping singletons, and appends any group of ≥2 files to
// *out. Input is already in skiplist (path) order, so emitted groups
// have path-sorted Files slices for free — no per-group sort.
func appendBucketGroups(entries []*binaryEntry, out *[]DuplicateGroup) {
	// Pair-fast-path: two entries sharing a uint64 prefix are almost
	// always a real duplicate. Compare full hashes to be sure.
	if len(entries) == 2 {
		if sameHash(entries[0], entries[1]) {
			*out = append(*out, DuplicateGroup{
				Hash:  entries[0].HashString(),
				Files: []string{entries[0].RelativePath(), entries[1].RelativePath()},
				Count: 2,
			})
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
		if len(group) < 2 {
			continue
		}
		files := make([]string, len(group))
		for i, e := range group {
			files[i] = e.RelativePath()
		}
		*out = append(*out, DuplicateGroup{
			Hash:  group[0].HashString(),
			Files: files,
			Count: len(files),
		})
	}
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
