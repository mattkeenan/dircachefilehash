# Architecture

This is the one-stop "how does this codebase fit together" document. It is
written for contributors and library callers who want a working mental
model in a single sitting. For specific subsystems and migration history,
see the deep-dive docs listed in §5.

For known rough edges in this architecture, see
`ARCHITECTURE-IMPROVEMENTS.md`.

---

## §1 What dcfh is

**dcfh tracks change, not content.** It walks a directory tree, records
each file's metadata + content hash in a sorted binary index, and on
subsequent runs compares the live tree against that index in O(stat)
unless a file's metadata says it has changed.

The unit of measurement is the *index file*: a sorted, mmap-friendly
binary log of `(path, stat, hash)` triples. Daily operations
(`status`, `update`, `dupes`) consume and produce these files.

dcfh is not a content-addressed store. There is no blob storage; if you
delete the file, the hash is just bookkeeping. The benefit is speed —
typically ~9× faster than `git status` on the same tree, because the
hashing path runs only when stat metadata diverges. The cost is that
recovery means rescanning, not recovering blobs.

---

## §2 Layered architecture

Five layers, foundation upward. Most files live in `pkg/`; tests are
elided for brevity.

### Layer 1 — Foundation

| File | Role |
|------|------|
| `pkg/constants.go` | Index format constants, context tag constants, file naming patterns. |
| `pkg/hash.go` | Hash algorithm registry, `HashFile` / `HashFileInterruptible`, symlink-target hashing. |
| `pkg/index.go` | Binary index format, `mmapIndexFile`, header read/write, mmap lifecycle, vectorio writes. |
| `pkg/index_loading.go` | Memo'd shared mmap loading (see §3 metaphor 5). |
| `pkg/binary_entry.go` | `binaryEntry` struct, methods, `binaryEntryRef`, build-time layout assertions, `BESizeFromPathLen`. |
| `pkg/time_encoding.go` | `timeWall` / `timeFromWall` / `encodeWallTime` — custom 1885-epoch wall-time format. |
| `pkg/filenames.go` | `DirectoryCache.GenerateTimestampedFileName` / `ScanForTimestampedCacheFiles` / `CleanupTimestampedCacheFiles`, plus `PathToSlug`. |
| `pkg/human_size.go` | `ParseHumanSize`, `FormatHumanSize`, `FormatHumanRate`. |
| `pkg/verbose.go` | Debug flags / verbose level. |
| `pkg/config.go` | `.dcfh/config` parsing and the validators (`ValidateHashAlgorithm` etc., `pkg/config.go:469`). |
| `pkg/ignore.go` | `.dcfhignore` matching. |

### Layer 2 — Entry abstractions & algorithms

| File | Role |
|------|------|
| `pkg/binary_entry_interface.go` | `BinaryEntryInterface`: the v0.7 abstraction over the four entry storage modes. Full per-mode comment at `pkg/binary_entry_interface.go:16`. |
| `pkg/binary_entry_skiplist.go` | `BESkiplistEntry` — entries living inside a loaded mmap'd index. |
| `pkg/binary_entry_scan.go` | `BEScanEntry` — heap-allocated entries created during a filesystem scan (`pkg/binary_entry_scan.go:28`). |
| `pkg/binary_entry_index_file.go` | `BEIndexFileIOEntry` — entries read via plain I/O. |
| `pkg/binary_entry_index_file_mmap.go` | `BEIndexFileMmapEntry` — entries read via mmap, used by `dcfhfind` and recovery. |
| `pkg/skiplist.go` | `skiplistWrapper`: the zerocopyskiplist wrapper with context-tagged entries and vectorio integration (`pkg/skiplist.go:39`). |
| `pkg/iterator.go` | `BinaryEntryIterator` interface (the v0.7 surface; `PathEntryIterator` in the same file is a v0.6 holdover). |
| `pkg/iterator_skiplist.go`, `pkg/iterator_filesystem.go` | Iterator implementations for skiplists and live filesystem walks. |
| `pkg/hwang_lin.go` | The Hwang-Lin merge of two sorted iterators (`pkg/hwang_lin.go:10`). |
| `pkg/reorder_buffer.go` | Restores sorted order after parallel hashing — keys on `PipelineEntry.SeqNum`. |
| `pkg/hash_pool.go` | Bounded worker pool that drains hash jobs onto entries. |
| `pkg/entry_serialiser.go` | Serialises entries onto disk via vectorio. |

### Layer 3 — Pipelines

| File | Role |
|------|------|
| `pkg/pipeline.go` | `PipelineEntry` and the channel scaffolding shared by both pipelines. |
| `pkg/pipeline_status.go` | The four-stage status pipeline (`pkg/pipeline_status.go:9`). |
| `pkg/pipeline_update.go` | The four-stage update pipeline (`pkg/pipeline_update.go:9`); main-index rename at `pkg/pipeline_update.go:193`. |
| `pkg/comparison_sink.go` | The two sinks: `scanWriteSink` (canonical write) and `diffComparisonSink` (delta capture). Distinction documented at `pkg/comparison_sink.go:1`. |
| `pkg/callback.go` | `HwangLinCallback` — the per-row hook interface the merge fires for added / modified / deleted paths. The active implementation is `sinkCallbackAdapter` in `pkg/comparison_sink.go`. |
| `pkg/diff.go` | `Diff()` — the function `Repo.Diff` and `dc.Status` both delegate to (`pkg/diff.go:8`). |
| `pkg/openref.go` | `OpenRef()` and the `IndexRef` vocabulary (Main / Cache / Merged / FsScan). |
| `pkg/temp_index_writer.go` | `TempIndexWriter` — the write-only target the pipeline drains into before the atomic rename. |
| `pkg/algorithm_hash_manager.go` | Cookie-based hash submission + completion ordering. |
| `pkg/dircache.go` | The `DirectoryCache` struct definition, constructors, and high-level helpers (`CreateDirectoryCache` at `pkg/dircache.go:282`). |
| `pkg/scan.go` | Filesystem walk + symlink traversal + scan-supporting types (`scannedPath`, `hashJobStart`). |

### Layer 4 — Core operations

| File | Role |
|------|------|
| `pkg/status.go` | `Status(ctx, flags, filter)` — `pkg/status.go:49`. |
| `pkg/update.go` | `Update(ctx, flags, paths...)` — `pkg/update.go:13`. |
| `pkg/dupes.go` | `FindDuplicates`. |
| `pkg/snapshot.go` | Snapshot create / list / prune / delete. |
| `pkg/recovery.go` | Index recovery and validation (1200+ lines; mid-migration to v0.7 — see improvements doc). |
| `pkg/filter.go`, `pkg/filter_run.go`, `pkg/flat_filter.go` | The filter predicate tree used by `Repo.Filter` and `dcfhfind`. |
| `pkg/dcfhfind_support.go` | Support functions used by `cmd/dcfhfind`. |

### Layer 5 — Wire / extension

This layer is what makes the same code drive both local and SSH-attached
repositories. CLAUDE.md's older five-layer description does not name it.

| File | Role |
|------|------|
| `pkg/repo.go` | The `Repo` interface (`pkg/repo.go:154`), `OpenRepo` (`pkg/repo.go:322`), `CreateRepo` (`pkg/repo.go:345`), and the request/response types. |
| `pkg/repo_local.go` | `localRepo` — the local implementation; currently delegates to `DirectoryCache.Status` / `.Update`. |
| `pkg/walker.go` | `Walker` and `Hasher` interfaces — the swap point that turns a local pipeline into a remote one. |
| `pkg/walker_local.go` | Local filesystem implementations of `Walker` / `Hasher`. |
| `pkg/walker_wire.go` | RPC-backed implementations that hash on the remote host. |
| `pkg/wire.go`, `pkg/wire_client.go`, `pkg/wire_client_shell.go`, `pkg/wire_handler.go`, `pkg/wire_server.go`, `pkg/wire_transport.go` | The framed JSON-RPC protocol behind `ssh://` repo URIs, plus a fallback shell-pipeline transport. |
| `pkg/ssh_auth.go` | SSH connection setup. |

---

## §3 System metaphors

These are the load-bearing mental models. If you understand only this
section you can read most of the code.

### 1. Three index file types, three lifecycles

dcfh distinguishes three on-disk index roles. Each has its own access
mode and lifetime:

| Type | Access | Lifetime |
|------|--------|----------|
| **Main** (`main.idx`) | Read-only mmap | Persistent. |
| **Cache** (`cache.idx`, `cache-{ts}.idx`) | Read-only mmap (post-write) | Persistent; rewritten as a side-effect of `status`. |
| **Scan** (in v0.7) | Heap-allocated `BEScanEntry` | In-memory only; never hits disk. |
| **Temp** (`*.tmp.{pid}.idx`) | Write-only via vectorio, no mmap | Created during a write, atomically renamed in place. |

Canonical references: the per-mode comment block at
`pkg/binary_entry_interface.go:16` and the index lifecycle in
`pkg/index.go:66`.

The v0.7 scan path is heap-allocated. The older mmap-backed scan path
(`AppendEntryToScanIndex` in `pkg/index.go:1008`) is retained for
recovery only — see the improvements doc.

### 2. Hwang-Lin merge of sorted streams

Most operations reduce to "merge two sorted streams, one row at a time,
fire a callback per (left, right, both) row." That's `hwangLin` at
`pkg/hwang_lin.go:10`. The three direct callers are `Status`, `Update`,
and `FindDuplicates`. The algorithm gives O(n+m) instead of the naive
O(n×m) and is the reason status on million-file trees is sub-second.

### 3. Skiplist entries carry a context tag

The `skiplistWrapper` at `pkg/skiplist.go:39` is more than a sorted set —
each entry carries a context tag (`MainContext` / `CacheContext` /
`ScanContext`, defined at `pkg/constants.go:11`). Merge policy keys off
the tag: `MergeOurs` / `MergeTheirs` mean different things depending on
which contexts are colliding. Mixing contexts in a single skiplist is
the normal case, not an error: a "merged main+cache" view is one
skiplist with both tags present, and the tag tells the writer which
entries to propagate to disk.

### 4. The pipeline is four channel-connected stages

```
  comparison  ───►  hash  ───►  reorder  ───►  write
   (Hwang-Lin)    (workers)    (SeqNum)     (vectorio + rename)
```

Both `pipeline_status.go` and `pipeline_update.go` (`:9` in each) build
this graph. The hash stage runs concurrently — typically 2 workers
saturate commodity NVMe; more thrashes the IO scheduler. Because hash
order isn't deterministic, every `PipelineEntry` carries a `SeqNum`
that the reorder buffer (`pkg/reorder_buffer.go`) uses to restore
sorted order before the write stage. The write stage drains into a
`TempIndexWriter` that is atomically renamed at the end (metaphor 7).

### 5. Memo'd mmap loading

`loadIndexShared` at `pkg/index_loading.go:33` returns the same
`*mmapIndexFile` for repeated calls on the same path, keyed on a stat
tuple. If the on-disk file changes (size, inode, mtime), the cache
entry is *moved* to an orphan list rather than unmapped immediately —
because outstanding skiplist entries may still hold pointers into the
old mapping. The orphan list is drained at `DirectoryCache.Close()`.

This is the only correct way to load an index from inside the package.
Direct mmap callers are a bug.

### 6. RWMutex on every mmap

When a scan-mode index grows, the mapping is `mremap`ped, which can
move the memory region. Hash workers reading the same memory at that
moment would SIGSEGV. The fix is an RWMutex on `mmapIndexFile`:
readers (hash workers) take read locks for entry access, the writer
(`appendEntryToNamedIndex` at `pkg/index.go:967`) takes the write lock
around the mremap. Read locks are held only over the offset-to-pointer
conversion — the pattern is "acquire under lock, dereference under
lock, release before slow work."

In v0.7 this matters less for the scan path (entries are heap-allocated
and don't grow under hash workers), but it still protects the main /
cache mmap'd indices from being remapped during a concurrent read.

### 7. Atomic index replacement via temp + rename

Writes never mutate `main.idx` or `cache.idx` in place. The pipeline
drains into a `*.tmp.*` file via `TempIndexWriter`, and the last action
before the operation completes is a single `os.Rename`. POSIX
guarantees that's atomic on the same filesystem, so a reader either
sees the old file in full or the new file in full. Canonical sites:
`pkg/pipeline_update.go:193` (main), `pkg/status.go:144` (cache).

### 8. Status writes cache.idx

This deserves its own metaphor because it surprises everyone. `Status`
is *not* read-only. It is `Diff(main, fs-scan)`, and materialising the
fs-scan side requires hashing files whose metadata changed. Those
hashes are too expensive to throw away, so they get written to
`cache.idx`. A subsequent `status` finds the same entries in cache and
skips re-hashing. The cache is *the* mechanism that makes dcfh fast.

The comment at `pkg/pipeline_status.go:9` says it explicitly: "the
resulting cache file IS the status." Anyone who edits the status path
under the assumption that it's read-only will subtly break the cache
guarantee.

---

## §4 Repo, Walker, Hasher

`Repo` (`pkg/repo.go:154`) is the transport-neutral surface. Every CLI
command goes through it. There are two implementations:

- `localRepo` (`pkg/repo_local.go`) — wraps a `DirectoryCache`. Its
  methods currently delegate back to `dc.Status` / `dc.Update` etc.
  rather than implementing fresh logic, so for now `Repo` and
  `DirectoryCache` are two public faces of the same code.
- A future colocated wrapper (Phase 3 in the migration plan) will proxy
  the full interface for SSH-attached repositories without dragging the
  whole `DirectoryCache` across the wire.

The swap point that turns a local pipeline into a remote one is the
`Walker` / `Hasher` pair at `pkg/walker.go`. A `Walker` produces
sorted (path, stat) tuples; a `Hasher` produces content hashes. The
local implementations (`walker_local.go`) read the filesystem directly;
the wire implementations (`walker_wire.go`) RPC out to a `dcfh remote`
process running on the server. The pipeline doesn't know which it has.

The Phase 1b Filter primitive (`Repo.Filter`) has landed. The
symmetric Fix primitive (recovery / dcfhfix migration) is deferred and
tracked in `BACKLOG.md`.

---

## §5 Pointers to deeper docs

| Doc | When to read it |
|-----|-----------------|
| `architecture-v0.7.md` | The v0.6→v0.7 migration plan: what changed, why, what's still in flight. |
| `streaming-iterator-architecture.md` | How `BinaryEntryInterface` and the iterator hierarchy were designed. |
| `design.md` | Earlier design notes; superseded by this doc + `architecture-v0.7.md` for current behaviour. |
| `cmd/dcfhfind/DESIGN.md` | The find(1)-style predicate + action surface, including the filter expression grammar. |
| `cmd/dcfhfix/DESIGN.md` | Recovery-tool spec: header / entry / scan repair workflows, backup stack semantics. |
| `CLAUDE.md` | Operational guide for AI assistants and contributors. Authoritative on conventions, anti-patterns, and the locking design. |
| `BACKLOG.md` | Open work items. |
| `ARCHITECTURE-IMPROVEMENTS.md` | Known rough edges in the architecture described here. |
