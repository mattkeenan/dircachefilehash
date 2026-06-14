# Multi-source recovery rebuild - Implementation Plan
**Task**: 28.3 (feature)

## Task Reference
- **Task ID**: internal-28.3
- **Task URL**: N/A (internal task)
- **Parent Task**: 28
- **Branch**: feature/28.3-multi-source-recovery-rebuild
- **Template Version**: 2.1

## Goal
Implement the multi-source recovery rebuild per c-design-plan (LD1–LD9): a net-new
merge core plus one op-gated recovery branch in `RunFix`, reusing `writeRepairedIndex`,
the fsck tolerant read, and `createPreRecoverySnapshot`.

## Workflow
Patterns first → Test → Minimal impl → Refactor green → Commit message explains "why".
Three milestones, each its own commit + gate (`go test ./...` + `golangci-lint run ./...`
+ pre-commit `-race`).

## Files to Modify
### Primary Changes
- `pkg/fix_recovery.go` (**new**) — `mergeSourcesIntoEntries(orderedPaths []string, options)`
  (LD3); the precedence source-ordering helper `orderedSourcePaths(metaDir, namedRefPaths)`
  (LD4 — confirmed net-new: `recovery.go` has only validators + `createPreRecoverySnapshot`,
  no existing ordered enumerator); the snapshot-readback gate (LD6); the per-source read-confinement
  check (see Security below).
- `pkg/fix_run.go` — add `FixOpRecoveryRebuild` const (LD1); `runRecoveryRebuild(...)` batch
  branch invoked from `RunFix` (lift single-ref guard, reject op-mixing, dry-run short-circuit,
  empty guard, confine + `writeRepairedIndex` to `ms.IndexFile`); keep the op **out of**
  `readOnlyFixOps` and `fixOpMutatesIndex`.
- **No new `collectAllEntries` helper** — the merge calls `collectForEdit(path,
  map[string]bool{}, "", "", options)` directly per source (single call site; `entriesFixed` is
  always 0 and discarded). Avoids a one-caller wrapper (Rule of Three).

### Supporting Changes
- `pkg/fix_recovery_test.go` (**new**) — unit tests for merge/precedence/sort/deleted/discards.
- `pkg/fix_run_test.go` — recovery-branch tests through `Repo.Fix` + fault-injection atomicity.

### Security: read-source confinement (closes the review's read/write asymmetry)
A raw-path selector resolves to `RefTypeFile` with the caller's path **verbatim**
(`filter_run.go:143`) — unconfined. The design confined only the write dest (LD8). Decision:
**confine read-sources to `MetaDir` too**. Each ordered source path is canonicalised and asserted
within `MetaDir` (reuse `hasPathPrefix` / the `confineWriteDest` canonicalisation) **before**
`collectForEdit` opens it; an out-of-`MetaDir` source is rejected. This matches parent FR8
("from any readable combination of main/cache/timestamped-cache files" — all repo-internal) and
makes AC6 honest for **both** read and write. gosec rationales on the new `os.ReadFile` sites
then truthfully cite the MetaDir confinement.

## Implementation Steps

### Milestone 1 — Merge core (LD2/LD3/LD4) — commit 1
- [ ] Per-source read = `collectForEdit(path, map[string]bool{}, "", "", options)` (no wrapper).
  Confirm an empty pathSet keeps every entry unchanged including **deleted** entries
  (`processSingleEntry` append branch; `NewValidatedEntry` does not reject the deleted flag —
  tombstones must survive the read for cross-source suppression), and that a truncated tail is
  dropped via `trySkipToNextEntry`→`stop` with the discard counted. Discard `entriesFixed` (0).
- [ ] Precedence ordering helper `orderedSourcePaths(metaDir, namedRefPaths) []string` =
  named ref `.Path`s ∪ `ScanForTimestampedCacheFiles` (reversed, newest-first), de-duped,
  ordered **timestamped(newest→oldest) > cache.idx > main.idx**; **each path confinement-checked
  within `metaDir`** (reject out-of-`MetaDir`) and skipped if non-readable.
- [ ] `mergeSourcesIntoEntries(orderedPaths, options)`: fold sources in order into
  `map[string]*ValidatedEntry` keyed by `ve.Path`, **keep first** (highest precedence), count
  later duplicates as conflict-loser discards; after the fold drop `ve.Entry.IsDeleted()`
  entries (tombstone wins conflict → then filtered); `sort.Slice` survivors by `ve.Path`.
  **Checksum policy (refines LD5)**: the output `checksumType` = the highest-precedence readable
  source's type; a source whose header `checksumType` **differs** is **skipped with a counted
  discard** (logged), **not** a hard abort — so one stale auto-discovered cache under an old
  hash type cannot block all recovery (the design's "never re-hash under the wrong algo" intent
  is preserved by skipping, not re-hashing). Return `(merged, checksumType, totalDiscards, err)`;
  discards disjoint per FR6 (truncation/validation in the read; conflict in the fold; deleted in
  the filter; checksum-mismatch in the source skip).
- [ ] **Tests (write first)** in `fix_recovery_test.go`: union by path; precedence tie-break
  (same path in two sources → higher wins); **cross-source tombstone suppression** (deleted entry
  in higher-precedence source suppresses the path from a lower source, then is filtered from
  output); truncated source → asserted prefix count + discard; output sorted; **mixed
  checksum-type source skipped with discard** (recovery still succeeds); **all-deleted input →
  empty merged**; empty input → empty merged; out-of-`MetaDir` source path rejected.
- [ ] Gate green.

### Milestone 2 — RunFix recovery branch (LD1/LD5/LD6/LD7/LD8/LD9) — commit 2
- [ ] Add `FixOpRecoveryRebuild FixOp = "recovery-rebuild"`. Verify (test) it classifies as a
  write via `fixOpIsWrite` and is **not** in `fixOpMutatesIndex`.
- [ ] In `RunFix`, before the single-ref guard / per-subject loop: detect a recovery command.
  If present with any other op in the same request → error. Otherwise call `runRecoveryRebuild`
  (does not hit `len(refs) != 1`).
- [ ] `runRecoveryRebuild` sequence (design Data Flow 3–9): **assert `writeRoot != "" first**
  (LD8); `ms := initMetaStoreBase("", writeRoot)` → `orderedSourcePaths` (LD4) →
  `mergeSourcesIntoEntries` → empty guard (count 0 ⇒ return counts, no write) → if `req.DryRun`
  return projected counts (no snapshot/write) → `ms.createPreRecoverySnapshot(req.Verbose)` +
  **fatal readback** of the contributing sources (see below) → `ctx.Err()` check →
  `confineWriteDest(ms.IndexFile, writeRoot)` → `writeRepairedIndex(ms.IndexFile, checksumType,
  merged, options)` → return `FixResult{RepairsApplied: len(merged), EntriesDiscarded: total}`.
- [ ] **Readback set (LD6, resolves AC1↔AC4 tension)**: verify only the **sources that actually
  contributed entries to the merge** were snapshotted — **not** the destroyed write target
  `main.idx`. Use `os.Lstat` (**not** `os.Stat`) on each contributing source's copy under
  `recovery/`, require a **regular file, size > 0**, and reject a symlinked entry (the design's
  "don't follow a symlink in `recovery/`" constraint, LD6). Abort `main.idx`-untouched on failure.
- [ ] **Plumbing (no `RunFix` signature change)**: the branch derives `MetaDir` from `writeRoot`
  (the library path always passes `r.ms.MetaDir`; recovery is never reached via the
  `writeRoot==""` CLI exemption — hence the first-line assert). `ms := initMetaStoreBase("",
  writeRoot)` — confirmed to set `MetaDir` + `IndexFile = join(MetaDir, "main.idx")`, needs **no**
  checksum type, and **no `configureMetaStore`/`LoadConfig`** (base store only, avoids config
  I/O). `writeRepairedIndex` synthesises its own writer `MetaStore` from `checksumType`
  internally. `repoCore.Fix` itself needs no change — the recovery command flows through unaltered.
- [ ] **Tests (write first)** in `fix_run_test.go` through `Repo.Fix`: destroyed `main.idx` +
  intact `cache.idx` → valid re-readable `main.idx` (AC1); dry-run writes nothing but reports
  counts (LD9); op-mixing rejected; **named source path outside MetaDir rejected before any read**
  (AC6 read-side) and the fixed write dest stays confined (AC6 write-side); forced
  snapshot-readback failure (missing/empty/symlinked copy) aborts with original byte-unchanged
  (AC4/LD6); empty + all-deleted merged set leaves original intact (AC4/LD7); cancelled `ctx`
  before write aborts without promoting.
- [ ] Gate green.

### Milestone 3 — Fault-injection + edge coverage (NFR5/AC5) — commit 3
- [ ] Atomicity test on the rebuild modelled on `pkg/fault_inject_test.go` /
  `pkg/atomic_index_test.go`: inject failure between temp write and promote (and during
  serialise) → assert no partial/corrupt `main.idx`, temp removed, original intact.
- [ ] Round out edge coverage: multi-source precedence end-to-end through `Repo.Fix`;
  truncated-body source through the full rebuild; conflict-loser discard count surfaced.
- [ ] `golangci-lint run ./...` clean (gosec floor; re-anchor any new `os.ReadFile`/write
  rationale to the confinement/MetaDir invariant). CWF changeset security verdict recorded in
  f-/g-.
- [ ] Gate green.

## Code Changes
### Before (single-source only — `pkg/fix_run.go:201`)
```go
if len(refs) != 1 {
    return result, fmt.Errorf("fix operates on a single index file (got %d); 28.2 is single-source", len(refs))
}
subject := refs[0].Path
for _, cmd := range req.Commands { /* per-subject dispatch */ }
```
### After (recovery branch precedes the single-subject path)
```go
if hasRecoveryOp(req.Commands) { // single-op membership check over req.Commands
    if len(req.Commands) != 1 {
        return result, fmt.Errorf("recovery-rebuild cannot be mixed with other fix ops")
    }
    // runRecoveryRebuild asserts writeRoot != "" first, then builds
    // ms := initMetaStoreBase("", writeRoot) internally (RunFix holds no *MetaStore).
    return runRecoveryRebuild(ctx, refs, req, writeRoot, result) // LD1–LD9
}
if len(refs) != 1 { /* unchanged single-source guard */ }
subject := refs[0].Path
// ... unchanged per-subject loop
```

## Test Coverage
**See e-testing-plan.md for the complete test plan** (TC mapping for AC1–AC6, fault injection).

## Validation Criteria
**See e-testing-plan.md.** Production gate per milestone: `go test ./...` + `golangci-lint run
./...` + pre-commit `-race` all green; no on-disk format change; `cwf-manage validate` OK.

## Scope Completion
**IMPORTANT**: Complete all three milestones before marking Finished. The optional under-floor
guard (FR5 second clause) is **descoped by design** (LD7) — not deferred work, a recorded
decision. No other deferrals planned. If a deferral becomes necessary, get user approval, update
success criteria, and file a follow-up immediately.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Executed as planned across 3 milestones (single commit `53b8f70a`): merge core,
RunFix recovery branch, fault-injection. Two signature simplifications vs plan
(no merge `err`; `+contributing`). See f-implementation-exec.md / j-retrospective.md.

## Lessons Learned
Reusing `collectForEdit` with an empty pathSet (keep-all + truncation-tolerant)
removed the need for a recovery-specific reader — net-new code stayed at 288
production lines. See j-retrospective.md.
</content>
