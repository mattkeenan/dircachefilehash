# Multi-source recovery rebuild - Testing Plan
**Task**: 28.3 (feature)

## Task Reference
- **Task ID**: internal-28.3
- **Task URL**: N/A (internal task)
- **Parent Task**: 28
- **Branch**: feature/28.3-multi-source-recovery-rebuild
- **Template Version**: 2.1

## Goal
Test strategy for the multi-source recovery rebuild: unit coverage of the merge core,
integration coverage through `Repo.Fix`, and a fault-injection gate proving atomicity.
Covers requirements AC1–AC6 and the design edge cases (LD1–LD9).

## Test Strategy
### Test Levels
- **Unit** (`pkg/fix_recovery_test.go`): `mergeSourcesIntoEntries` + `orderedSourcePaths` in
  isolation against crafted `.idx` fixtures — precedence, truncation, tombstones, sort, checksum,
  confinement.
- **Integration** (`pkg/fix_run_test.go`): the recovery branch end-to-end through `Repo.Fix` —
  rebuild, dry-run, guards, confinement, snapshot abort, cancellation.
- **Reliability / fault-injection** (`pkg/fix_run_test.go`, Task 23 harness model): atomicity of
  the destructive write.
- **Regression**: full `go test ./...` — existing dcfhfix/recovery/filter tests stay green.

### Test Coverage Targets
- **Critical paths 100%**: `mergeSourcesIntoEntries`, `orderedSourcePaths`, `runRecoveryRebuild`
  (read-confinement reject, snapshot-readback gate, empty guard, dry-run branch, write).
- **Edge cases**: truncation, same-path conflict, tombstone suppression, mixed checksum-type,
  empty/all-deleted, out-of-MetaDir read source, symlinked snapshot copy, cancelled ctx.
- **Regression**: no behaviour change to the 10 existing fix ops or recovery validators.

## Test Cases
### Functional Test Cases (unit — merge core)
- **TC-1 (FR1/AC1)** union by path
  - **Given**: `main.idx`, `cache.idx`, one `cache-<ts>.idx`, each with distinct valid entries.
  - **When**: `mergeSourcesIntoEntries(ordered)`.
  - **Then**: merged set = union of all validated entries keyed by path; discards 0.
- **TC-2 (FR2/AC2)** precedence tie-break + determinism
  - **Given**: the same relative path present in `main.idx` and a `cache-<ts>.idx` with differing fields.
  - **When**: merge, run twice with the source list passed in two different orders.
  - **Then**: the timestamped-cache entry wins both times (timestamped > cache > main); identical result; the main copy counted as a conflict-loser discard.
- **TC-3 (FR1/AC3)** truncated-body source
  - **Given**: a source with a valid header but a body truncated mid-final-entry.
  - **When**: merge.
  - **Then**: the readable validated prefix is kept with a **concrete asserted count**; the truncated tail entry is counted as a discard (not a hard error, not a guess).
- **TC-4 (FR3 main-excludes-deleted)** cross-source tombstone suppression
  - **Given**: a path present-and-live in `main.idx` and present-and-**deleted** in a higher-precedence `cache.idx`.
  - **When**: merge.
  - **Then**: the cache tombstone wins the conflict, then is filtered by `IsDeleted()` — the path is **absent** from the output; the main copy is a discard.
- **TC-5** output ordering
  - **Given**: sources whose union is not globally path-sorted.
  - **When**: merge.
  - **Then**: survivors are ascending by `ve.Path`.
- **TC-6 (LD5 refinement)** mixed checksum-type source skipped
  - **Given**: agreeing-checksum `main`+`cache` plus one timestamped cache with a **different** header `checksumType`.
  - **When**: merge.
  - **Then**: the mismatched source is skipped with a counted discard; the merge still succeeds from the agreeing sources; output `checksumType` = highest-precedence agreeing source; **no** hard abort, **no** re-hash.
- **TC-7 (FR5)** empty / all-deleted → empty merged
  - **Given**: (a) no readable sources; (b) sources containing only deleted tombstones.
  - **When**: merge.
  - **Then**: merged set is empty in both cases; discards reported.
- **TC-8 (NFR4 read-confinement)** out-of-MetaDir source rejected
  - **Given**: an ordered source path resolving outside `MetaDir` (raw `RefTypeFile`).
  - **When**: `orderedSourcePaths` / merge.
  - **Then**: rejected before any `os.ReadFile`; error surfaced, nothing read.

### Functional Test Cases (integration — through `Repo.Fix`)
- **TC-9 (AC1)** rebuild from surviving source
  - **Given**: a repo with `main.idx` destroyed (removed/zeroed) but `cache.idx` intact.
  - **When**: `repo.Fix(ctx, FixRequest{Commands:[{Op:recovery-rebuild}], IndexSelectors:[...]})`.
  - **Then**: a re-readable, checksum-valid `main.idx` is produced; `FixResult` reports the entry/discard counts; the index re-loads via the normal loader.
- **TC-10 (FR6/LD9)** dry-run writes nothing
  - **Given**: the TC-9 repo, `DryRun: true`.
  - **When**: `Fix`.
  - **Then**: no `.fix.tmp` / `.pre-fix` / `recovery/` snapshot / rename occurs; `FixResult` still reports the would-be counts.
- **TC-11 (LD1)** op-mixing rejected
  - **Given**: a request mixing `recovery-rebuild` with another op.
  - **When**: `Fix`.
  - **Then**: error; no write.
- **TC-12 (AC6/LD8)** confinement, both sides
  - **Given**: (a) a named source selector outside `MetaDir`; (b) the fixed write dest.
  - **When**: `Fix`.
  - **Then**: (a) rejected before any read; (b) the write target stays `r.ms.IndexFile` inside `MetaDir`; neither can be steered outside.
- **TC-13 (AC4/LD6)** snapshot-readback failure aborts
  - **Given**: the snapshot of a contributing source is made missing / zero-length / a symlink in `recovery/` (forced).
  - **When**: `Fix`.
  - **Then**: abort before promote; `main.idx` byte-unchanged; typed error.
- **TC-14 (AC4/LD7)** empty/all-deleted guard
  - **Given**: sources that merge to zero survivors.
  - **When**: `Fix`.
  - **Then**: abort before rename; originals intact; counts reported (no header-only/empty `main.idx`).
- **TC-15** context cancellation
  - **Given**: a `ctx` cancelled before the write step.
  - **When**: `Fix`.
  - **Then**: abort without promoting; `main.idx` unchanged.

### Non-Functional Test Cases
- **TC-16 (NFR5/AC5)** fault-injection atomicity — **the data-integrity gate**
  - **Given**: the TC-9 repo; failure injected (modelled on `pkg/fault_inject_test.go` /
    `pkg/atomic_index_test.go`) at: mid-serialise, and between temp-write and `PromoteRepairedIndex`.
  - **When**: `Fix`.
  - **Then**: no partial/corrupt `main.idx` is ever left behind — the temp is removed and the
    original `main.idx` (or its absence) is unchanged; a subsequent valid rebuild still succeeds.
- **Security**: `golangci-lint run ./...` (gosec floor) 0 issues; new `os.ReadFile`/write sites
  carry per-line rationales citing the MetaDir confinement (read **and** write). CWF
  `cwf-security-reviewer-changeset` verdict recorded in f-/g-. Confinement asserted by TC-8/TC-12.
- **Reliability**: TC-13/TC-14/TC-16 are the no-partial-index gates; pre-commit `-race`
  (`-d=checkptr=0`) green.
- **Performance**: single pass bounded by total readable index size (NFR1) — informational; no
  dedicated benchmark, but assert no source is read more than once.
- **Regression**: full suite green; the 10 existing fix ops and recovery validators unchanged.

## Test Environment
### Setup Requirements
- Go toolchain; Unix filesystem with **symlink support** (TC-8/TC-12 confinement, TC-13 symlink
  readback) and temp dirs.
- Fixtures: crafted `.idx` files — valid, truncated-body, same-path-conflict, deleted-tombstone,
  mixed-checksum-type — reusing the 28.2 fixture helpers (`cmd/dcfhfix/testhelpers_test.go`
  patterns / `pkg` fix test builders) where they apply.
- Fault-injection seam from the Task 23 harness (`fault_inject_test.go`).

### Automation
- `go test ./pkg/...` (+ `-race` via pre-commit); `golangci-lint run ./...`. No external services.

## Validation Criteria
- [ ] TC-1…TC-16 pass; AC1–AC6 each covered by ≥1 case.
- [ ] Critical-path coverage 100% (merge, ordering, recovery branch guards).
- [ ] `golangci-lint run ./...` 0 issues; CWF changeset verdict recorded.
- [ ] Full `go test ./...` + pre-commit `-race` green (no regressions).

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All 16 planned TCs executed and passed (g-testing-exec.md). The fault-injection
TC-16 was restructured into two subtests so the post-fault clean rebuild runs
within the test. See j-retrospective.md.

## Lessons Learned
The Task-23 `withSyncFault` seam isolates cleanly to `writeRepairedIndex` because
the snapshot copy uses `os.WriteFile` — a no-collateral atomicity probe. See
j-retrospective.md.
</content>
