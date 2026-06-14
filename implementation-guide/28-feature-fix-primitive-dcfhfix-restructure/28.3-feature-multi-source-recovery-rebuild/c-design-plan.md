# Multi-source recovery rebuild - Design
**Task**: 28.3 (feature)

## Task Reference
- **Task ID**: internal-28.3
- **Task URL**: N/A (internal task)
- **Parent Task**: 28
- **Branch**: feature/28.3-multi-source-recovery-rebuild
- **Template Version**: 2.1

## Goal
Architecture for the multi-source recovery rebuild: how `main.idx` is reconstructed
from a precedence-ordered merge of surviving index sources, driven through
`Repo.Fix`, reusing the 28.1/28.2 single-writer and fsck-read machinery so the
net-new surface is the merge orchestration plus one op-gated branch in `RunFix`.

## Design Priorities
Testability → Readability → Consistency → Simplicity → Reversibility

## Architecture Preferences
Composition over inheritance. Interfaces over singletons. Explicit over implicit.

## Key Decisions

### LD1 — Recovery is an op-gated *batch* branch in `RunFix`, not a per-subject command
- **Decision**: add `FixOpRecoveryRebuild FixOp = "recovery-rebuild"`. It is a write op **by
  omission** — `fixOpIsWrite` is fail-closed (`fix_run.go:49`), so leaving it out of
  `readOnlyFixOps` classifies it as a write with no extra code. It is **also kept out of
  `fixOpMutatesIndex`** (`fix_run.go:54`): the recovery branch takes its own pre-recovery
  snapshot (LD6), so it must not also trigger the per-subject `.pre-fix` backup path (no
  double-backup). When a `FixRequest`'s commands contain it, `RunFix` routes to a dedicated
  recovery branch **before** the per-subject command loop and lifts the `len(refs) != 1`
  guard (`pkg/fix_run.go:201`) for that branch only; the recovery op mixed with per-subject
  ops in one request is rejected. Every other op keeps the single-subject contract unchanged.
- **Rationale**: recovery is structurally different from the 10 existing ops — it reads
  **N sources** and writes **one destination** (`r.ms.IndexFile`) that is *not* one of the
  sources. The existing dispatch assumes `subject := refs[0].Path` is both the read and
  write target (`fix_run.go:204`); forcing recovery into that loop would corrupt the model.
- **Trade-offs**: two control-flow shapes inside `RunFix` (per-subject loop vs batch
  branch). Contained by gating strictly on the recovery op.

### LD2 — Reuse the fsck collect-all read for per-source extraction (truncation-tolerant already)
- **Decision**: read each source via the existing fsck workflow, not the mmap loader. A thin
  `collectAllEntries(indexFile, options) ([]*ValidatedEntry, checksumType, discarded, err)`
  wrapper calls the same path as `collectForEdit` with an **empty pathSet** (every entry is
  kept unchanged; `processSingleEntry` appends it — `fix_entry_workflow.go:274`).
- **Rationale**: `processAllEntriesWorkflow` (`fix_entry_workflow.go:234`) reads via
  `os.ReadFile`, loops under `offset < len(entryData)`, and on a malformed/truncated tail
  entry runs `trySkipToNextEntry`→`stop`. So a source with a readable header but truncated
  body already yields its **readable validated prefix** + counted discards. **This refines
  requirements FR1's "net-new tolerant read" note**: the tolerant reader exists in the fsck
  path; the intolerant `collectEntryRefs` (`index.go:402`) the requirements review flagged is
  the mmap *load* path used by Filter/status, which recovery does not touch.
- **Trade-offs**: `collectAllEntries` is `collectForEdit` with an empty pathSet (no edit, no
  `field`/`value`/`entriesFixed`). It adds **no new behaviour** — implementation may keep it as
  a one-line readability wrapper or inline the `collectForEdit(path, map[string]bool{}, "", "",
  options)` call at the single merge site. Not a second reader.

### LD3 — `mergeSourcesIntoEntries`: union by path, precedence-keep, sort, drop deleted
- **Decision**: `mergeSourcesIntoEntries(orderedPaths []string, options)` reads each source
  path via `collectAllEntries`, then folds them in **precedence order** into a `map[string]*ValidatedEntry`
  keyed by `ve.Path`, keeping the **first** occurrence (highest precedence) and counting each
  later occurrence as a conflict-loser discard. After the fold it (a) **drops entries whose
  `ve.Entry.IsDeleted()`** is set — `main.idx` excludes deleted entries (CLAUDE.md filtering
  rule; the higher-precedence cache tombstone wins the conflict, then is filtered out), and
  (b) **sorts the survivors by `ve.Path`** — sources are each individually sorted but their
  union is not, and the binary index / Hwang-Lin comparison require ascending path order (the
  writer does not enforce it).
- **Rationale**: one in-memory pass over the union; reuses `ValidatedEntry` end-to-end so no
  field re-decode. Discard accounting is disjoint (FR6): an entry is attributed to exactly one
  of truncation/validation (counted in `collectAllEntries`) **or** conflict-loser (counted in
  the fold) **or** deleted-filter.
- **Trade-offs**: holds the merged entry set in memory; bounded by total readable index size
  (NFR1), same envelope as the existing repair path.

### LD4 — Source set = named refs + auto-discovered timestamped caches; precedence op-derived
- **Decision**: the recovery branch assembles its ordered source-**path** list as the union of
  (a) the resolved `refs` the caller named (their `.Path` values) and (b) auto-discovered
  timestamped caches from `ScanForTimestampedCacheFiles` (`filenames.go:22`, returns
  `cache-{ISO8601}.idx` chronologically sorted). The combined set is de-duplicated by path and
  ordered by the fixed precedence **timestamped caches (newest→oldest) > `cache.idx` >
  `main.idx`** (reverse the chronological scan for newest-first), computed from filename, so the
  result is identical regardless of how the caller listed selectors (FR2 determinism).
- **Why auto-discover (resolves the requirements-review ambiguity)**: the standard selector
  vocabulary **cannot name** timestamped caches — verified: `ResolveIndexSelectors` "all"
  expands to `main`+`cache`+`scan` only (`filter_run.go:105`), and a raw path becomes a single
  `RefTypeFile`. A recovering user therefore cannot enumerate cache lineage by hand, so the op
  folds it in. This does **not** weaken confinement: D2/NFR4 governs the **write destination**
  (fixed `r.ms.IndexFile`, LD8), not which repo-internal files are *read*; reading more
  surviving sources never escapes `MetaDir` and maximises recovered state (recovery's purpose).
  The named `refs` remain the explicit floor (and are confinement-checked); they are not used
  to *subset* the discovered cache lineage.
- **Trade-offs**: a deliberately narrow selector (e.g. only `main`) is still augmented with
  on-disk timestamped caches. Accepted — they are repo-internal cache history and the write is
  confined; a future explicit "strict-sources" flag could disable augmentation if a real need
  appears (not built now).

### LD5 — Reuse `writeRepairedIndex` for the atomic write
- **Decision**: the merged, sorted survivors are written via
  `writeRepairedIndex(r.ms.IndexFile, checksumType, merged, options)`
  (`fix_entry_workflow.go:113`) — temp `.fix.tmp` beside the destination → `TempIndexWriter`
  + `EntrySerialiser` → `Close` (stamps count + checksum) → `PromoteRepairedIndex` (atomic
  rename, `.pre-fix` preservation). `checksumType` is taken from the sources, asserting they
  agree (all belong to one repo); a mismatch is an error, never a silent re-hash (mirrors
  `newFixMetaStore`'s round-trip assertion).
- **Rationale**: this is the *same* no-partial-index single-writer path 28.1 built; recovery
  inherits temp-discard-on-error, `.pre-fix`, and the FR9 variable-length-path round-trip for
  free. No second writer.
- **Trade-offs**: none material — it already takes `(indexFile, checksumType, []*ValidatedEntry,
  options)`, exactly the merge's output shape.

### LD6 — Snapshot precondition wraps the best-effort helper with a fatal readback
- **Decision**: before any write, call `createPreRecoverySnapshot` (`recovery.go:350`), then
  **verify** the sources being rebuilt are present and **non-empty** in `.dcfh/recovery/`
  (`os.Stat` + size > 0 per required source). If a required source was not snapshotted, abort
  with `r.ms.IndexFile` byte-untouched.
- **Rationale**: `createPreRecoverySnapshot` is best-effort — it swallows per-file copy errors
  (`continue // Non-fatal`, `recovery.go:383`) and returns `nil` even if `copiedCount == 0`,
  exposing no count. Trusting its `error` return would make FR4's "snapshot failure aborts" a
  no-op. The readback is the fatal gate the requirement needs; the helper itself is reused
  unmodified.
- **Trade-offs / residual risk**: the readback verifies **existence + non-emptiness**, not full
  byte-integrity of the copy, and there is a small TOCTOU window between snapshot, readback, and
  the rebuild write. Accepted for a local repair tool operating inside the user's own `MetaDir`
  (no privilege boundary crossed); the implementation must not widen it (e.g. by following a
  symlink in `recovery/`). One extra `Stat` per required source; negligible cost.

### LD7 — Empty guard (the floor is deliberately out of scope)
- **Decision**: if the merged survivor count is **0**, abort **before** `PromoteRepairedIndex`,
  leave originals intact, return the discard counts. The optional under-floor ratio guard
  (FR5's second clause) is **not** built in 28.3.
- **Rationale**: the empty case is the one that actually destroys data — overwriting a
  recoverable index with a header-only/empty one. A non-zero merged set is, by construction,
  recovered valid state worth promoting. A ratio floor would key off source-header `EntryCount`,
  which a *truncated* source can report as a stale/large value — spuriously blocking a
  legitimate rebuild. Carrying a deferred heuristic invites re-litigation at implementation;
  per "the best part is no part" it is dropped until a concrete over-aggressive-rebuild case
  appears (then a tested threshold, not a hidden knob).
- **Trade-offs**: a rebuild that recovers very few of many original entries still promotes; the
  pre-recovery snapshot (LD6) is the safety net for that case, not a floor.

### LD9 — DryRun short-circuits before snapshot and write
- **Decision**: under `req.DryRun`, the recovery branch runs source ordering (LD4) + the merge
  (LD3) to compute would-be counts, then returns `FixResult{RepairsApplied, EntriesDiscarded}`
  **without** calling `createPreRecoverySnapshot` or `writeRepairedIndex` — no snapshot, no
  temp, no `.pre-fix`, no rename.
- **Rationale**: every existing write op gates on `DryRun` (`fix_run.go:273` ff.); recovery must
  match so a preview never mutates the repo. Computing the merge is read-only and gives the user
  an accurate projected count.
- **Trade-offs**: dry-run still pays the read/merge cost — acceptable and necessary to report
  real counts.

### LD8 — Confinement: recovery reachable only via `Repo.Fix` (writeRoot = MetaDir)
- **Decision**: the recovery write target is always `r.ms.IndexFile`, structurally inside
  `MetaDir`; `RunFix` still runs `confineWriteDest(r.ms.IndexFile, writeRoot)`. The op must
  **never** be exposed through the `writeRoot==""` CLI explicit-subject exemption
  (`fix_run.go:221`) — there is no dcfhfix CLI surface for recovery in this task, and the
  library path always passes `MetaDir` (`repo_local.go:387`).
- **Rationale**: keeps the D2/NFR4 invariant — a selector can never steer the rebuild's write
  outside the repo, because the destination is not selector-derived at all.
- **Trade-offs**: none; this is the existing confinement contract applied to a fixed destination.

## System Design

### Component Overview
- **`pkg/fix_run.go`**: new `FixOpRecoveryRebuild`; recovery-branch detection + dispatch in
  `RunFix` (LD1); the snapshot precondition (LD6), empty/floor guard (LD7), and the
  `writeRepairedIndex` call (LD5).
- **`pkg/fix_recovery.go`** (new, sibling to `recovery.go`): `mergeSourcesIntoEntries` (LD3)
  and the precedence-ordering helper (LD4); reuses the existing validators +
  `createPreRecoverySnapshot`.
- **`pkg/fix_entry_workflow.go`**: small `collectAllEntries` wrapper (LD2).
- **`repoCore.Fix`** (`pkg/repo_local.go`): unchanged in shape — already resolves selectors and
  passes `MetaDir`; the recovery op flows through it without a signature change.

### Data Flow (recovery rebuild)
1. `repoCore.Fix` resolves `IndexSelectors` → `refs`, passes `MetaDir` as `writeRoot`, calls `RunFix`.
2. `RunFix` detects `FixOpRecoveryRebuild` → recovery branch (bypasses the single-ref guard, LD1).
3. Build precedence-ordered source **path** list (LD4): named `refs` ∪ reverse(`ScanForTimestampedCacheFiles`), de-duped, ordered timestamped(newest→oldest) > cache > main, readable ones only.
4. `mergeSourcesIntoEntries(orderedPaths)` (LD3): per-source `collectAllEntries` → union by path (precedence-keep) → drop deleted → sort by path; accumulate discards.
5. **Empty guard** (LD7): merged count 0 → abort, report counts (no snapshot/write reached).
6. **DryRun** (LD9): if set, return projected `FixResult` here — no snapshot, no write.
7. `createPreRecoverySnapshot` + **fatal readback** (presence + non-empty) of required sources (LD6); abort untouched on failure.
8. `confineWriteDest(r.ms.IndexFile, MetaDir)` (LD8) → `writeRepairedIndex(r.ms.IndexFile, checksumType, merged, options)` → atomic promote (LD5).
9. Return `FixResult{RepairsApplied: len(merged), EntriesDiscarded: total}`.

## Interface Design

### Data Models
```go
// New op in the existing FixOp set (pkg/fix_run.go); a write op, not read-only,
// and NOT a per-subject mutating op — handled by the batch branch.
const FixOpRecoveryRebuild FixOp = "recovery-rebuild"

// Net-new orchestration (pkg/fix_recovery.go). Takes ordered source PATHS — only
// .Path is consumed, and timestamped caches are not a distinct IndexRef type, so
// []string is the honest shape (composes with collectAllEntries' string arg):
func mergeSourcesIntoEntries(orderedPaths []string, options FixEntryFlags) (
    merged []*ValidatedEntry, checksumType uint16, discarded int, err error)

// Net-new read wrapper (pkg/fix_entry_workflow.go) — collectForEdit with an empty
// pathSet; may be inlined at the single call site (LD2):
func collectAllEntries(indexFile string, options FixEntryFlags) (
    collected []*ValidatedEntry, checksumType uint16, discarded int, err error)
```
- **`FixResult`**: reuse the shipped two counters — `RepairsApplied` = entries in the rebuilt
  `main.idx`, `EntriesDiscarded` = total dropped (truncation + validation + conflict + deleted).
  No struct field is added unless a test needs a sources-processed count (requirements NFR:
  "no struct expansion without a tested consumer").

### API
```go
// Unchanged interface (pkg/repo.go) — recovery is expressed as a command, not a new method:
Fix(ctx context.Context, req FixRequest) (*FixResult, error)
// Caller: req.Commands = []FixCommand{{Op: FixOpRecoveryRebuild}}, req.IndexSelectors = surviving sources.
```

## Constraints
- Single writer (`TempIndexWriter`/`EntrySerialiser`); main/cache read-only; temp pure-vectorio — preserved via `writeRepairedIndex`.
- Write destination is `r.ms.IndexFile`, never selector-derived (LD8); confinement enforced.
- No on-disk format change; merged `main.idx` satisfies the existing header/checksum/layout contract.
- No new third-party dependencies; British spelling in prose/comments.

## Decomposition Check
- [ ] **Time**: >1 week? No — ~2–3 days; the heavy reuse (LD2/LD5) shrinks the build.
- [ ] **People**: >2? No.
- [ ] **Complexity**: 3+ concerns? No — one path: order → snapshot → merge → guard → write.
- [x] **Risk**: data-destructive write needing isolation? Yes — already the isolated leaf; the
  fault-injection gate + snapshot readback + empty guard are its containment.
- [ ] **Independence**: separable? No — single dependency chain.

**Result: 1 of 5 → no further decomposition.**

## Validation
- [ ] Design review (4-agent map/reduce) — pending this phase's Step 8.
- [ ] Architecture approved by user.
- [x] Integration points verified against source (`writeRepairedIndex`, `collectForEdit`/
  `processAllEntriesWorkflow`, `createPreRecoverySnapshot`, `ScanForTimestampedCacheFiles`,
  `repoCore.Fix`, `ms.IndexFile`, `ResolveIndexSelectors`, `RunFix` single-ref guard).

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
LD1–LD9 followed; LD5 checksum policy made concrete at exec (first contributing
source sets the type; disagreeing sources skipped-with-discard); LD7 under-floor
guard stayed descoped by design. See j-retrospective.md.

## Lessons Learned
The batch-level `RunFix` branch (not a 10th per-subject op) was the right seam —
one reads-N-writes-one op slotted in cleanly without touching `repoCore.Fix`. See
j-retrospective.md.
</content>
