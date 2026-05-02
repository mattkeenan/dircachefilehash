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

## 1. ~~`AppendEntryToScanIndex` is documented as the only write path; it's effectively dead~~ — **resolved**

- **Status**: closed. `AppendEntryToScanIndex`, `appendEntryToNamedIndex`, `writeBinaryEntryToMmap`, `initialiseScanIndex`, `cleanupCurrentScanFile`, the unused `AppendEntryToFixIndex` / `InitializeFixIndex` / `CleanupFixIndex` siblings, and the `mremap()` call in `pkg/index.go` have all been deleted. The orphaned recovery cascade (`loadIndexWithCleanCopyingAndFixes`, `processRecoveryEntry`, `applyFixesToEntry`, etc.) that fed them has been deleted too. CLAUDE.md's §"Memory Protection and Locking Mechanism" was rewritten — the locks are now defensive rather than load-bearing.
- **Note**: `RecoverFromIndexWithFixes` and `RecoverWithStatePreservation` remain as stubs returning "not yet implemented" — designing the v0.7 recovery write path is item 4.

## 2. ~~`BEScanEntry` is documented as mmap-backed; it's heap-allocated~~ — **resolved**

- **Status**: closed. `pkg/binary_entry_interface.go:23` now reads "Scanning (heap-allocated, ephemeral)" and the surrounding error-handling note no longer references mremap.

## 3. ~~Two rename sites for `main.idx`~~ — **resolved**

- **Status**: closed. The legacy callback path (`UpdateCallback`, `performScanToSkiplist`, `pkg/callback_update.go`) has been deleted; `finaliseMainIndex` (`pkg/pipeline_update.go:174`) is now the single owner of the `main.idx` temp+rename contract.
- **Note**: deleting the legacy path also removed the FATAL `os.Exit(1)` scaffolding inside `RecoverFromIndexWithFixes` and `RecoverWithStatePreservation`; those wrappers now return clean "v0.7 pipeline-based recovery not yet implemented" errors so `AutoRecover`'s strategy chain can fall through. See item 4.

## 4. ~~`recovery.go` is half-migrated to v0.7~~ — **resolved (cleanup); rewrite deferred**

- **Status**: cleanup pass complete. The original "dead helpers" list was already mostly resolved by the item-1 cleanup (`loadIndexWithCleanCopyingAndFixes`, `processRecoveryEntry`, `analyzeEntryForFixes`, etc. were deleted then; `isValidHashType` is in fact live, called from `validateEntryLogical`). What remained was a v0.6-shaped orchestration shell with no production caller — `AutoRecover`, `RecoverFromIndex`, `RecoverFromIndexWithFixes` (stub), `RecoverFromScanFiles` (referenced v0.6 `scan-*.idx` files that don't exist), `RecoverWithStatePreservation` (stub), `CreateEmptyMainIndex` (had an `os.Exit(1)` landmine), the strategy-chain helpers (`tryRecoveryStrategy`, `tryRecoverSource`, `anyIndexExists`), and the supporting `loadIndexWithProcessor` / `findScanIndexFiles` / `ScanFileInfo` in `pkg/update.go`. All of that has been deleted. `pkg/recovery.go` is now just the validation/processor framework plus the snapshot helpers used by idxck (down from 626 to 431 lines).
- **Note**: the actual v0.7 recovery write path is intentionally **deferred** — it lands alongside the Fix primitive in BACKLOG Phase 1b-2. Recovery (rebuild `main.idx` from available indices) is conceptually a Fix batch, and `dcfhfix` is the v0.7 user-facing repair surface; designing them together is the right shape. See the BACKLOG entry for scope, including the auto-fix mode that becomes safe-by-default once `dcfhfix` writes to a new index file rather than overwriting.

## 5. ~~`scan.go` identity crisis (711 lines)~~ — **resolved**

- **Status**: closed. `scan.go` now owns only the walk machinery (~424 lines). Foundation types live in `pkg/scan_types.go`; symlink policy lives in `pkg/scan_symlinks.go`; `hashJobStart` moved to `pkg/algorithm_hash_manager.go` next to its only consumer; the dead `hwangLinType` enum was deleted outright.

## 6. ~~`util.go` is an overloaded grab-bag (655 lines)~~ — **resolved**

- **Status**: closed. `pkg/util.go` (and `pkg/util_test.go`) have been deleted. The contents were split along their real seams: `DirectoryCache` + `ScanIndexInfo` + `loadedIndex` + `cachedStat` moved to `pkg/dircache.go` (where its methods already live); `binaryEntry` struct + methods + `binaryEntryRef` + build-time layout assertions moved to a new `pkg/binary_entry.go` (matching the existing `binary_entry_*.go` naming pattern); the three time helpers moved to `pkg/time_encoding.go`; `GenerateTimestampedFileName` / `ScanForTimestampedCacheFiles` / `CleanupTimestampedCacheFiles` plus `PathToSlug` moved to `pkg/filenames.go`; `ParseHumanSize` / `FormatHumanSize` / `FormatHumanRate` moved to `pkg/human_size.go`; `getGoroutineID` moved to `pkg/recovery.go` next to its only caller (`createPreRecoverySnapshot` at `pkg/recovery.go:406`). The dead `generateTempFileName` (zero production callers) was deleted outright. CLAUDE.md and ARCHITECTURE.md were updated to reflect the new file inventory.

## 7. ~~`binary_entry_interface_test_framework.go` ships in production builds~~ — **resolved**

- **Status**: closed. Renamed to `pkg/binary_entry_interface_test_framework_test.go` so the Go toolchain only compiles it into test binaries. No code or package changes were needed — every consumer was already a `_test.go` file in the same `package dircachefilehash`. `BinaryEntryTestSuite`, `TestEntryData`, `CreateTestData`, and `CreateDeletedTestData` no longer appear in `dcfh`, `dcfhfind`, or `dcfhfix` production binaries.

## 8. `PathEntryIterator` is dead but still exported

- **Site**: `pkg/iterator.go:7` defines `PathEntryIterator` (returns `*binaryEntry`); the v0.7 surface is `BinaryEntryIterator` at `:36` of the same file.
- **Reality**: No callers in production code (`grep -rn 'PathEntryIterator' pkg/ cmd/ --include='*.go'` returns only the type definition itself).
- **Why it matters**: An exported, undocumented "use which?" choice in the godoc surface is the wrong impression to give a library consumer. Either delete it or mark it deprecated and route the type alias to `BinaryEntryIterator`.

## 9. Context dispatch is duplicated across nine files

- **Sites**: `MainContext` / `CacheContext` / `ScanContext` literals appear in `pkg/index_loading.go:87,128`, `pkg/openref.go:95,125`, `pkg/update.go:155,171,175`, `pkg/dcfhfind_support.go:71,73`, `pkg/recovery.go:632,720`, `pkg/binary_entry_scan.go:329`, `pkg/binary_entry_index_file.go:29`, `pkg/binary_entry_index_file_mmap.go:26`.
- **Reality**: Every site decides independently which context tag a skiplist or entry should carry. There is no central dispatch table mapping "I'm loading X" to "tag with Y."
- **Why it matters**: The context tag drives merge policy. A contributor who tags wrong introduces silent merge bugs that show up only when two contexts collide. Centralising the mapping would make it impossible to typo.

## 10. ~~`hashJobStart` carries v0.6 and v0.7 fields concurrently~~ — **resolved**

- **Status**: closed. The deprecated `IndexEntry binaryEntryRef` field has been removed; `hashJobStart` now holds only `Entry BinaryEntryInterface` (plus the `ScannedPath` fallback used for symlink-mode detection). The struct has moved to `pkg/algorithm_hash_manager.go` next to its consumer.

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

**Items 1, 2, 5, 10 share a root cause** — all four were remnants of the
v0.6 mmap-scan subsystem that v0.7 superseded but didn't remove.
**Resolved together** in a single coherent removal pass: deleted the
v0.6 write path and its supporting machinery, dropped the deprecated
`IndexEntry` field on `hashJobStart`, fixed the stale interface
comment, split `scan.go` along its real seams, and rewrote
CLAUDE.md's locking and lifecycle sections to match v0.7 reality.
