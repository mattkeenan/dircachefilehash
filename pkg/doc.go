// Package dircachefilehash maintains a sorted, mmap-backed binary index of a
// directory tree's metadata + content hashes, and serves status / update /
// duplicate-detection over that index. It is not a content-addressed store:
// dcfh tracks change, not content. Files are hashed only when their stat
// metadata diverges from the index, which is what makes it ~9× faster than
// `git status` on the same tree.
//
// # Primary entry points
//
// New callers should use the [Repo] interface. It is transport-neutral —
// the same surface drives a local repository or an SSH-attached remote.
//
//	ctx := context.Background()
//	repo, err := dircachefilehash.OpenRepo(ctx, "/path/to/.dcfh")
//	if err != nil { /* ... */ }
//	defer repo.Close()
//
//	result, err := repo.Diff(ctx, dircachefilehash.DiffRequest{})
//	groups, err := repo.Groups(ctx, dircachefilehash.GroupsRequest{})
//
// [CreateRepo] is the corresponding factory for `dcfh init`. The
// MetaStore type ([CreateMetaStore] returning a [*MetaStore]) remains
// public for low-level access to the on-disk index, but it no longer
// carries the verbs — Diff / Apply / Groups live on the [Repo] surface
// only.
//
// All public methods take a [context.Context] — long scans / hashes honour
// cancellation.
//
// # Architecture in five layers
//
// See ARCHITECTURE.md at the repo root for the canonical structure-and-
// metaphors document. In brief:
//
//   - Layer 1 — Foundation: index format, mmap loading, hashing, config.
//   - Layer 2 — Entry abstractions & algorithms: [BinaryEntryInterface],
//     skiplist, Hwang-Lin merge, iterators, hash pool, reorder buffer.
//   - Layer 3 — Pipelines: comparison → hash → reorder → write, plus the
//     callbacks and sinks that compose them.
//   - Layer 4 — Core operations: status, update, dupes, snapshot, recovery.
//   - Layer 5 — Wire / extension: [Repo], [Walker], [Hasher], the SSH
//     audit-mode protocol.
//
// # Load-bearing system metaphors
//
// A handful of mental models do most of the work. Library callers should
// know at least these:
//
//  1. [Repo] is transport-neutral. The same code path serves a local index
//     and an SSH-attached one; the difference is which [Walker] / [Hasher]
//     pair is plugged in.
//  2. Indices are sorted, mmap-backed, and updated via temp+rename. A
//     reader holding refs into a stale mapping is safe — superseded
//     mappings move to an orphan list and are unmapped at Close, not
//     when the on-disk file changes.
//  3. Status writes cache.idx as a side effect. It is not read-only:
//     hashing changed files and persisting them in the cache is what makes
//     subsequent operations O(stat) instead of O(hash).
//  4. The pipeline has four stages — comparison, hash, reorder, write —
//     connected by channels. A sequence number on each [PipelineEntry]
//     lets the reorder buffer restore sorted order after parallel hashing.
//  5. Skiplist entries carry a context tag (Main / Cache / Scan); merge
//     policy keys off it. Mixing contexts in one skiplist is intentional.
//
// ARCHITECTURE.md expands these and adds three more (Hwang-Lin merge,
// the RWMutex-on-mmap protecting against `mremap` SIGSEGV, the temp+
// rename atomicity contract).
//
// # Configuration
//
// Repository configuration is read from `.dcfh/config` and exposed via
// [Repo.Config] / [ConfigRepo.Get]. Process-wide diagnostics:
//
//	dircachefilehash.SetDebugFlags("scan,extravalidation")
//	dircachefilehash.SetVerboseLevel(2)
//
// # Note on the internal API
//
// Most types in this package are internal implementation details and may
// change without notice. External consumers should restrict themselves to:
//
//   - [Repo] / [SnapshotRepo] / [ConfigRepo] and their request/response
//     types ([DiffRequest], [ApplyRequest], [GroupsRequest], [FilterRequest],
//     [StatusResult], [UpdateResult], [DuplicateGroup], etc.).
//   - [MetaStore] and its public methods (Status, Update,
//     FindDuplicates, snapshot operations).
//   - [SetDebugFlags], [SetVerboseLevel].
//
// In-package contributors will also work with [BinaryEntryInterface] and
// the four implementations in `binary_entry_*.go` — that's the v0.7
// abstraction over the four entry storage modes (read-only mmap, ephemeral
// heap, temp-write, and a few others). It supersedes direct use of the old
// `binaryEntryRef` / `binaryEntry` plumbing for any new code.
package dircachefilehash
