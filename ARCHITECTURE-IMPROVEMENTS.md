# Architecture improvements

This is a finding-grade list of rough edges in the architecture described
in `ARCHITECTURE.md`. It is **not** a plan. Each item names a specific
mismatch between the metaphors we tell ourselves and what the code
actually does, plus enough evidence to act on.

How to use this doc: these are candidate seeds for `BACKLOG.md`. Pick one,
investigate, decide whether to fix / accept / re-document, then either
open a backlog entry or leave a `### Notes` against the existing one.

Items are ordered by observed risk, not effort.

---

## 1. `AppendEntryToScanIndex` is documented as the only write path; it's effectively dead

- **Claim**: CLAUDE.md:559 — "`AppendEntryToScanIndex()` is the ONLY function that writes binaryEntries to index files."
- **Reality**: `pkg/index.go:1008` defines it; the only remaining callers are inside `pkg/recovery.go`. The pipeline write path is `TempIndexWriter` → `vectorio.WritevRaw`.
- **Why it matters**: The "single entry writing path" is the load-bearing constraint that the locking design rests on (see CLAUDE.md §"Memory Protection and Locking Mechanism"). If the constraint isn't true any more, neither is the safety argument that flows from it. Either restate the constraint correctly for v0.7 or remove the dead path.

## 2. `BEScanEntry` is documented as mmap-backed; it's heap-allocated

- **Claim**: `pkg/binary_entry_interface.go:25` lists "Scanning (mmap-backed, ephemeral)" as one of the four storage modes.
- **Reality**: `pkg/binary_entry_scan.go:11` and `:28` describe it as heap-allocated; the per-type comment in the same file explicitly contrasts the v0.7 heap design with the v0.6 mmap design.
- **Why it matters**: A reader who trusts the interface comment will think entries can disappear under them when the mapping is remapped. They can't. The metaphor "scan = mmap" is the foundation of the locking story; it doesn't apply.

## 3. Two rename sites for `main.idx`

- **Sites**:
  - `pkg/pipeline_update.go:193` — `os.Rename(tempName, dc.IndexFile)` at the tail of the update pipeline.
  - `pkg/callback_update.go:298` — `os.Rename(tempPath, mainIndexPath)` inside `UpdateCallback.finaliseTempIndexWriter`.
- **Reality**: Either one is dead code or there's a double-rename / race. The two paths look like they came from different stages of the v0.7 migration and were never reconciled.
- **Why it matters**: `main.idx` is the durable state. A subtly broken atomicity contract here means a crash mid-update produces a partially-written index that nothing detects.

## 4. `recovery.go` is half-migrated to v0.7

- **Evidence**: `pkg/recovery.go` (~1200 lines) has multiple commented-out write call sites where the v0.7 pipeline equivalent hasn't been wired in. Several recovery modes still call into the v0.6 mmap-scan path (item 1) because no replacement has been written.
- **Why it matters**: Recovery is the path that runs *when things are already wrong*. Half-migrated code there means failure modes that work in tests don't work in the field, and we won't notice because nobody hits the recovery path under controlled conditions. This is the strongest argument for finishing the deferred Fix primitive.

## 5. `scan.go` identity crisis (711 lines)

- **History**: Hwang-Lin moved out to `hwang_lin.go`. Filesystem walk and symlink traversal stayed.
- **What's still in there**: filesystem walk + symlink resolution + types that are foundation-level (`scannedPath`, `hwangLinType`, `hashJobStart` at `pkg/scan.go:20-47`).
- **Why it matters**: The file's name no longer tells you what's in it, so contributors can't predict where to look. Splitting it (e.g. `scan_walk.go` for the filesystem walk, `scan_types.go` for the foundation types) would restore the layering the rest of the package follows.

## 6. `util.go` is an overloaded grab-bag (655 lines)

- **What's in it**: the `DirectoryCache` struct definition (`pkg/util.go:39`), time encoding, file naming helpers, goroutine ID extractor, orphaned-file detector, build-time assertions about `binaryEntry`.
- **Why it matters**: `DirectoryCache` is the cross-layer aggregate (it contains `Walker`, `Hasher`, scanner state, mmap memo, config — fields that span Layers 1–4 in `ARCHITECTURE.md`). Its definition lives in a foundation file alongside time-encoding utilities, so the boundary between "type" and "utilities" disappears. The file's naming ("things that don't belong elsewhere") is honest about the problem but doesn't fix it.

## 7. `binary_entry_interface_test_framework.go` ships in production builds

- **Evidence**: `pkg/binary_entry_interface_test_framework.go` is a `.go` file (not `_test.go`); contains test scaffolding / mock types.
- **Why it matters**: It compiles into any binary that links the package, adding dead code and polluting the public namespace with test-only symbols. Renaming to `*_test.go` (or moving to `pkg/internal/testutil/`) is mechanical.

## 8. `PathEntryIterator` is dead but still exported

- **Site**: `pkg/iterator.go:7` defines `PathEntryIterator` (returns `*binaryEntry`); the v0.7 surface is `BinaryEntryIterator` at `:36` of the same file.
- **Reality**: No callers in production code (`grep -rn 'PathEntryIterator' pkg/ cmd/ --include='*.go'` returns only the type definition itself).
- **Why it matters**: An exported, undocumented "use which?" choice in the godoc surface is the wrong impression to give a library consumer. Either delete it or mark it deprecated and route the type alias to `BinaryEntryIterator`.

## 9. Context dispatch is duplicated across nine files

- **Sites**: `MainContext` / `CacheContext` / `ScanContext` literals appear in `pkg/index_loading.go:87,128`, `pkg/openref.go:95,125`, `pkg/update.go:155,171,175`, `pkg/dcfhfind_support.go:71,73`, `pkg/recovery.go:632,720`, `pkg/binary_entry_scan.go:329`, `pkg/binary_entry_index_file.go:29`, `pkg/binary_entry_index_file_mmap.go:26`.
- **Reality**: Every site decides independently which context tag a skiplist or entry should carry. There is no central dispatch table mapping "I'm loading X" to "tag with Y."
- **Why it matters**: The context tag drives merge policy. A contributor who tags wrong introduces silent merge bugs that show up only when two contexts collide. Centralising the mapping would make it impossible to typo.

## 10. `hashJobStart` carries v0.6 and v0.7 fields concurrently

- **Site**: `pkg/scan.go:38` — the struct holds both `IndexEntry binaryEntryRef` (v0.6, deprecated) and `Entry BinaryEntryInterface` (v0.7, current).
- **Why it matters**: Two parallel hash submission paths coexist in the same type. Either is sufficient; both is a future bug. Picking one and removing the other is a self-contained cleanup.

## 11. `Repo` is a thin wrapper, not a replacement

- **Reality**: `localRepo.Diff` calls `dc.Status` (`pkg/repo_local.go`); `localRepo.Apply` calls `dc.Update`. Both `DirectoryCache.Status` and `Repo.Diff` are public — same logic, two doors.
- **Why it matters**: We've roughly doubled the public surface area and have no enforcement that new callers go through `Repo`. A library consumer who reads `pkg/doc.go` will reasonably take either path; in practice we want them on `Repo` because that's the path that survives the Phase 3 colocated-repo work. Either narrow `DirectoryCache` to in-package use (move it to `internal/`) or commit to keeping both paths first-class indefinitely.

## 12. No `Repo.Fix` primitive

- **Reality**: `dcfhfix` and recovery still go through `DirectoryCache` directly. The deferred Fix primitive is the only currently-tracked path to closure (see the Phase 1b-2 entry in `BACKLOG.md`).
- **Why it matters**: As long as recovery isn't behind `Repo`, an SSH-attached repo can't be repaired remotely — the wire protocol has no Fix verb. That's a footgun for the audit-mode story (Phase 2).

---

## Broader observations

**CLAUDE.md's layer model is missing Layer 5.** The wire / extension
subsystem (`pkg/repo*.go`, `pkg/walker*.go`, `pkg/wire*.go`,
`pkg/ssh_auth.go` — ~13 files) doesn't appear in CLAUDE.md's five-layer
description at all. Contributors reading CLAUDE.md as the architectural
reference will not realise that subsystem exists. Updating CLAUDE.md is
out of scope here, but worth doing.

**Doc sprawl.** With this doc plus `ARCHITECTURE.md`, the package now
has at least five "where do I learn the system" entry points: this
file, `ARCHITECTURE.md`, `pkg/doc.go`, `architecture-v0.7.md`,
`streaming-iterator-architecture.md`, plus `design.md`. The first two
are the canonical going-forward; the older three are migration / design
history. After this lands, a prune pass to mark or archive the older
docs is overdue.

**Items 1, 2, 5, 10 share a root cause.** All four are remnants of the
v0.6 mmap-scan subsystem that v0.7 superseded but didn't remove. There
is a coherent removal pass to be planned — delete `AppendEntryToScanIndex`
and its callers in recovery, drop the `IndexEntry` field on `hashJobStart`,
clean up the comment in `binary_entry_interface.go`, split `scan.go`.
Doing them together avoids four half-finished cleanups.
