# Fix primitive + dcfhfix restructure - Implementation Execution
**Task**: 28 (feature)

## Task Reference
- **Task ID**: internal-28
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/28-fix-primitive-dcfhfix-restructure
- **Template Version**: 2.1

## Goal
Execute the **integration** of the three landed subtasks (28.1/28.2/28.3) per
d-implementation-plan.md: assemble onto one branch, verify the whole builds and
tests green, and map the five parent success criteria to landed surfaces. No new
production code — any gap is a new subtask, not an inline patch.

## Execution Checklist
- [x] Read d-implementation-plan.md and e-testing-plan.md thoroughly
- [x] Verify all prerequisites met (three subtask squashes ff-merged)
- [x] Execute integration steps sequentially
- [x] Update "Actual Results" for each step
- [x] Document any deviations from plan
- [x] Update status to "Finished" when complete

## Actual Results

### Step 1: Confirm assembly
- **Planned**: enumerate the three squashes `a0bcf0e0`/`61efe765`/`752cde35`;
  `git log --no-merges a0bcf0e0~1..752cde35` shows them consecutive with no merge
  commits; LSP symbol check the three surfaces coexist.
- **Actual**: `git log --no-merges a0bcf0e0~1..752cde35` returns exactly the three
  squashes in order (28.1 → 28.2 → 28.3). `git log --merges a0bcf0e0~1..HEAD`
  empty — linear history, ff-only as required. LSP `workspaceSymbol` confirms
  `RunFix` (fix_run.go:204), `runRecoveryRebuild` (fix_recovery.go:28), and both
  the 28.2 `TestRunFix_*` and 28.3 `TestMerge_*`/`TestRecovery_*` suites resolve in
  one workspace — no symbol collision.
- **Deviations**: none.

### Step 2: Build the whole
- **Planned**: `go build ./...` — all three binaries compile.
- **Actual**: `go build ./...` exit 0; `dcfh`, `dcfhfind`, `dcfhfix` all produced.
  Clean build proves no import cycle across the three subtask surfaces.
- **Deviations**: none.

### Step 3: Full-suite verification
- **Planned**: `go test ./pkg/... ./cmd/...` + `-race -d=checkptr=0` +
  `golangci-lint run ./...` (0) + `govulncheck` (0).
- **Actual**: all green —
  - `go test ./pkg/... ./cmd/...`: ok (pkg, pkg/format, pkg/fsdedupe, cmd/dcfh,
    cmd/dcfh/internal/tui, cmd/dcfhfind, cmd/dcfhfix).
  - `go test -race -gcflags=all=-d=checkptr=0 ./pkg/... ./cmd/...`: ok (exit 0).
  - `golangci-lint run ./...`: `0 issues.`
  - `govulncheck ./...`: `No vulnerabilities found` (0 your-code, 0 imported; 8 in
    required modules not called).
- **Deviations**: none. (Toolchain is go-1.26 locally; go.mod floor stays 1.25.0.)

### Step 4: Map parent success criteria to the landed surface
- **SC1 (Repo.Fix + FixRequest/FixResult, single engine)** → `Repo.Fix`
  (repo.go:191), `repoCore.Fix` (repo_local.go:373) delegating to the single
  `RunFix` engine (fix_run.go:204); `FixRequest`/`FixResult` structs present;
  `TestRepoFix_ThroughInterface` (fix_run_test.go:437). ✓
- **SC2 (fsck helpers in pkg/, dcfhfix a thin translator)** → helpers resolve in
  `pkg/` (`ValidatedEntry.ApplyFieldFix`, `newFixMetaStore`, `SiblingPreFixPath`,
  backup stack); `cmd/dcfhfix` dispatches via `runFixWrite` → `RunFix`. CLI surface
  unchanged (verified live, Step 5). ✓
- **SC3 (multi-source recovery rebuild)** → `runRecoveryRebuild`
  (fix_recovery.go:28), `mergeSourcesIntoEntries` (fix_recovery.go:191),
  `hasRecoveryOp` (fix_recovery.go:13), `FixOpRecoveryRebuild` (fix_run.go:43).
  Negative guards verified by reading the code: **mutual-exclusion** —
  `recovery-rebuild cannot be combined with other fix ops` (fix_run.go:215-216),
  test `TestRecovery_OpMixingRejected`; **library-only confinement** —
  `writeRoot == ""` ⇒ error before any read/write (fix_recovery.go:32-33). ✓
- **SC4 (both fsck modes; new-file write)** → auto-fix routes through the
  single-writer path; manual mode returns the typed `ErrManualModeUnimplemented`
  (fix_run.go:111) **before any write** (fix_run.go:207-208); tests
  `TestRunFix_ManualModeTypedError`, `TestFixOpClassification_FailClosed`. ✓
- **SC5 (existing tests pass + new coverage)** → the 28.2 `TestRunFix_*` and 28.3
  `TestRecovery_*`/`TestMerge_*` suites run green in one pass alongside the
  pre-existing dcfhfix/recovery suites (Step 3). ✓

### Step 5: End-to-end coherence smoke (throwaway temp repos)
- **Planned**: dcfhfix subcommand smoke + library recovery-rebuild smoke on seeded
  temp repos.
- **Actual**: on a `mktemp -d` repo (2 files, `dcfh init`+`update` → 2-entry
  `main.idx`), the assembled `dcfhfix` translator drove a full mutating round-trip:
  `entry show data/a.txt` (renders by relative-path key), `header edit flags 0`
  (writes via single-writer + `.pre-fix` backup sibling), `fixes list` (shows the
  relocated 28.1 backup stack), `fixes pop` (restores), post-pop `header show`
  loads clean with entry count intact. This exercises cmd/dcfhfix → `RunFix` →
  moved helpers as one whole. The recovery-rebuild library path is **library-only
  by design** (the confinement guard makes it CLI-unreachable) and is covered by
  the green `TestRecovery_*` suite (destroyed-main + intact-cache fixtures) rather
  than a CLI invocation — consistent with SC3's negative guard.
- **Deviations**: the smoke used the actual restructured dcfhfix surface
  (`header|entry|fixes` families). CLAUDE.md's older `header`/`scan` example syntax
  is stale aspirational doc; the shipped surface is the 28.2 translator. No
  production gap — a doc-refresh nit only.

## Blockers Encountered
None. (One smoke-harness slip: a `cd "$TMP"` persisted the shell cwd into a temp
dir that was then removed; re-run from repo root with absolute paths — no bearing
on the integration result.)

## Deferral Check
- [x] All steps from d-implementation-plan.md executed (5 integration steps)
- [x] All success criteria from a-task-plan.md met (SC1–SC5 mapped, Step 4)
- [x] All requirements from b-requirements-plan.md addressed (via the subtasks)
- [x] All design guidance in c-design-plan.md followed (the decomposition itself)
- [x] No planned work deferred without user approval
- [x] No integration gap found — no new subtask required

## Status
**Status**: Finished
**Next Action**: /cwf-testing-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Integration verified end-to-end: linear assembly of `a0bcf0e0`/`61efe765`/
`752cde35`; clean build of all three binaries; full suite + `-race` + lint(0) +
govulncheck(0) green; SC1–SC5 each mapped to a landed, tested symbol with the
recovery negative guards read in-code; dcfhfix translator round-trip smoke green.

## Lessons Learned
The coordinating-parent exec is verification, not coding: LSP symbol resolution +
the union test suite + a live CLI round-trip together prove the three subtasks
compose without re-reading every diff. The only finding was documentation drift
(CLAUDE.md's dcfhfix example syntax predates the 28.2 restructure) — a doc nit, not
an integration defect.

## Security Review

**State**: error

error: cap exceeded: 3732 production lines > 500

> Expected by construction, not a skipped review. The parent is anchored at the
> pre-decomposition baseline `c385cb8`, so `security-review-changeset` diffs the
> **union** of all three subtasks (3732 production lines) and trips the 500-line
> cap (exit 2 → record error, do not invoke the subagent). Each subtask was
> already reviewed **under-cap** at its own f/g exec phases with verdict
> `no findings`: 28.1 (helper relocation, behaviour-preserving), 28.2 (Fix
> primitive + CLI translator), 28.3 (recovery rebuild, 288 production lines — the
> destructive path, scoped to one file precisely to stay under-cap). The
> integration parent adds **zero production lines**, so there is no new code
> surface for a semantic review to assess; the changeset review's unit of work is
> the per-subtask diff, which has full coverage. Confinement/single-writer
> invariants on the assembled whole are exercised by the green union test suite
> (Step 3) and read in-code (Step 4).
