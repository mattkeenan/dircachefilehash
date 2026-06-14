# Multi-source recovery rebuild - Testing Execution
**Task**: 28.3 (feature)

## Task Reference
- **Task ID**: internal-28.3
- **Task URL**: N/A (internal task)
- **Parent Task**: 28
- **Branch**: feature/28.3-multi-source-recovery-rebuild
- **Template Version**: 2.1

## Goal
Execute the tests defined in e-testing-plan.md and verify implementation from d-implementation-plan.md.

## Execution Checklist
- [x] Read e-testing-plan.md and d-implementation-plan.md thoroughly
- [x] Verify test environment ready
- [x] Execute test cases sequentially
- [x] Record pass/fail for each test
- [x] Document failures with reproduction steps (none)
- [x] Update status to "Finished" when all pass

## Test Execution

Command:
`go test ./pkg/ -run 'TestMerge_|TestRecovery_|TestVerifyRecoverySnapshot_|TestOrderedSourcePaths_' -v -count=1`
All 16 named tests PASS. Full `go test ./pkg/... ./cmd/...` and the pre-commit
`-race -d=checkptr=0` gate are green (no regressions).

## Test Results

### Functional Tests (merge core — `pkg/fix_recovery_test.go`)

| TC | Test func | Expected | Status |
|----|-----------|----------|--------|
| TC-1 (FR1/AC1) | `TestMerge_UnionByPath` | union of distinct paths, 0 discards, type preserved | PASS |
| TC-2 (FR2/AC2) | `TestMerge_PrecedenceAndDeterminism` | timestamped>cache>main wins; deterministic; main copy = conflict-loser | PASS |
| TC-3 (FR1/AC3) | `TestMerge_TruncatedSourcePrefix` | readable prefix kept (asserted 2), truncated tail = 1 discard | PASS |
| TC-4 (FR3) | `TestMerge_TombstoneSuppression` | higher-precedence tombstone suppresses path; absent from output; 2 discards | PASS |
| TC-5 | `TestMerge_OutputSorted` | survivors ascending by path | PASS |
| TC-6 (LD5) | `TestMerge_MixedChecksumTypeSkipped` | mismatched source skipped w/ discards; merge succeeds; type = agreeing source | PASS |
| TC-7 (FR5) | `TestMerge_EmptyAndAllDeleted` | empty input → empty; all-deleted → empty + discards | PASS |
| TC-8 (NFR4) | `TestOrderedSourcePaths_RejectsOutOfMetaDir` | out-of-MetaDir source rejected before read | PASS |

### Functional Tests (integration through `Repo.Fix` — `pkg/fix_recovery_run_test.go`)

| TC | Test func | Expected | Status |
|----|-----------|----------|--------|
| TC-9 / TC-12b (AC1) | `TestRecovery_RebuildFromCache` | destroyed main + intact cache → re-readable checksum-valid main.idx at fixed dest; loads clean via production loader | PASS |
| TC-10 (FR6/LD9) | `TestRecovery_DryRunWritesNothing` | projected counts; no main.idx / recovery/ / .fix.tmp | PASS |
| TC-11 (LD1) | `TestRecovery_OpMixingRejected` | recovery + other op → error, no write | PASS |
| TC-12a (AC6/LD8) | `TestRecovery_OutOfMetaDirSourceRejected` | named out-of-MetaDir selector rejected before read/write | PASS |
| TC-13 (AC4/LD6) | `TestVerifyRecoverySnapshot_Gate` | missing/empty/symlinked snapshot copy aborts; valid passes | PASS |
| TC-14 (AC4/LD7) | `TestRecovery_EmptyGuardAborts` | zero-survivor merge aborts before rename; original intact; counts reported | PASS |
| TC-15 | `TestRecovery_ContextCancelledBeforeWrite` | cancelled ctx aborts without promoting | PASS |

### Non-Functional Tests

| TC | Test func | Expected | Status |
|----|-----------|----------|--------|
| TC-16 (NFR5/AC5) | `TestRecovery_FaultInjectionAtomicity` | sync fault leaves no partial main.idx / no .fix.tmp; clean rebuild after fault succeeds + loads clean | PASS |

- **Security**: `golangci-lint run ./...` = 0 issues (gosec floor); `govulncheck` 0
  applicable vulnerabilities. Confinement asserted by TC-8/TC-12; CWF changeset
  verdict recorded below.
- **Reliability**: TC-13/TC-14/TC-16 are the no-partial-index gates; pre-commit
  `-race` (`-d=checkptr=0`) green.
- **Regression**: full `go test ./pkg/... ./cmd/...` green; the 10 existing fix ops
  and recovery validators unchanged.

## Test Failures

None. (During implementation, the destroyed-main fixtures were corrected to
`os.Remove` the empty `main.idx` that `CreateMetaStore` seeds — recorded in
f-implementation-exec.md, not a test-execution failure.)

## Coverage Report

`pkg/fix_recovery.go` per-function (recovery test set):
- `verifyRecoverySnapshot` 100%
- `orderedSourcePaths` 93.8%
- `mergeSourcesIntoEntries` 93.3%
- `runRecoveryRebuild` 86.7%
- `hasRecoveryOp` 75.0%

Uncovered lines are defensive error returns (e.g. `ScanForTimestampedCacheFiles`
I/O error, `createPreRecoverySnapshot` failure) — critical paths (confinement
reject, snapshot-readback gate, empty guard, dry-run, write) are exercised.

## Status
**Status**: Finished
**Next Action**: /cwf-rollout
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Security Review

**State**: no findings

This confirms the gosec G304 rationale on the `os.ReadFile` at `collectForEdit` (line 195): the path is "the RunFix-confined subject (confineWriteDest against MetaDir) for the library path." The new recovery caller honours this — every source path passes through `confineWriteDest(c, metaDir)` in `orderedSourcePaths` (line 1220 of the diff) before reaching `collectForEdit`. So the rationale stays truthful for the new caller.

I have everything needed to form a verdict. Let me reason through the threat categories.

## Security review of the 28.3 testing-exec changeset

**Reviewable production surface.** Two production files: the net-new `pkg/fix_recovery.go` (recovery orchestration: `hasRecoveryOp`, `runRecoveryRebuild`, `orderedSourcePaths`, `mergeSourcesIntoEntries`, `verifyRecoverySnapshot`) and a small addition to `pkg/fix_run.go` (the `FixOpRecoveryRebuild` constant plus the batch-branch dispatch in `RunFix`). The remainder of the diff is CWF process markdown (a–f plan/exec files) and two test files (`pkg/fix_recovery_test.go`, `pkg/fix_recovery_run_test.go`). I read the production code in full and cross-checked its load-bearing reuse claims against the actual sources: `confineWriteDest` (`pkg/fix_run.go:118`), `createPreRecoverySnapshot` (`pkg/recovery.go:350`), and `collectForEdit` (`pkg/fix_entry_workflow.go:194`).

**(a) Bash injection / unsafe command construction** — Not applicable. This is Go; there is no `os/exec`, `system`, shell, or command-string construction anywhere in the diff. No finding.

**(b) Perl / git-output parsing** — Not applicable. No Perl and no git-porcelain parsing in this changeset. No finding.

**(c) Prompt injection** — Not applicable. The recovery path consumes index *selectors* (file paths) and binary `.idx` content; none flows into LLM context. The plan markdown files are CWF process docs authored by the workflow, not untrusted external input reaching a downstream model. No finding.

**(d) Environment-variable handling** — No env vars are read in the diff. The write destination is the fixed `ms.IndexFile` (structurally inside `MetaDir`), never env- or selector-derived. No finding.

**(e) Pattern-based / path-handling risks** — This is the category that matters for a data-destructive, path-driven rebuild, and I verified the invariants hold rather than taking the exec note on trust:

- **Read-source confinement is enforced before any open.** Every candidate (named selectors *and* auto-discovered timestamped caches) passes through `confineWriteDest(c, metaDir)` in `orderedSourcePaths` (diff line 1220). I confirmed `confineWriteDest` (`fix_run.go:118`) is fail-closed: it `filepath.Abs`+`Clean`s, resolves the parent dir via `EvalSymlinks`, recombines the leaf, resolves the root, and rejects via `hasPathPrefix` — an out-of-`MetaDir` source is a hard error, not a silent skip (asserted by `TestOrderedSourcePaths_RejectsOutOfMetaDir` and `TestRecovery_OutOfMetaDirSourceRejected`). This is precisely what keeps the reused `collectForEdit` `os.ReadFile` G304 rationale (`fix_entry_workflow.go:195` — "never a raw selector") truthful for the new caller.
- **Symlink handling.** `orderedSourcePaths` (diff line 1230) `Lstat`s the leaf and requires `Mode().IsRegular()`, so a source that is itself a symlink is skipped, not followed. `verifyRecoverySnapshot` (diff line 1319) likewise `Lstat`s each snapshot copy and rejects non-regular entries — a planted symlink in `recovery/` aborts rather than being traversed (asserted by `TestVerifyRecoverySnapshot_Gate`).
- **Destructive-write guards.** The empty-merge guard aborts before any snapshot or rename (diff line 1120); the best-effort `createPreRecoverySnapshot` (which I confirmed swallows per-file copy errors and returns `nil` regardless, `recovery.go:383`) is converted into a hard precondition by the net-new `verifyRecoverySnapshot` readback; the `writeRoot == ""` library-only assertion (diff line 1098) keeps the destructive op off the unconfined CLI exemption; the final write goes through the single-writer `writeRepairedIndex` (temp + atomic rename). All are present and test-covered, including fault-injection atomicity (`TestRecovery_FaultInjectionAtomicity`).

One pattern recorded with the required framing, not as a defect:

1. **`verifyRecoverySnapshot` validates existence + non-emptiness, not byte-integrity, with an acknowledged TOCTOU window** (`pkg/fix_recovery.go:1315`). It `Lstat`s each contributing source's snapshot copy and checks regular-file + non-zero size, but does not verify the copy's content matches the source, and there is a window between snapshot, readback, and the rebuild write. *Safe here because* the operation runs entirely inside the user's own `MetaDir` with no privilege boundary crossed, the destination is the fixed in-`MetaDir` `main.idx`, and the pre-recovery snapshot plus atomic rename bound the blast radius — this exact residual is called out and accepted in design LD6. *Audit future uses* where this presence-only readback pattern might be relied on as an integrity guarantee, or where the recovery op could become reachable across a privilege boundary or with an attacker-writable `recovery/` directory — there the existence check would be insufficient and content/atomicity verification would be required. This matches the verdict the implementation-exec reviewer already recorded; the testing-exec diff adds test code that exercises this gate but does not change the residual.

No actionable security defects. The path-confinement, symlink-rejection, and destructive-write invariants the threat model cares about are enforced before the read and before the write, verified against the real reused sources, and the one residual pattern is correctly scoped and documented.

```cwf-review
state: no findings
summary: Recovery rebuild confines read sources and the fixed write dest to MetaDir (verified fail-closed confineWriteDest), Lstat-rejects symlinks on both source and snapshot copies, and gates the destructive write behind empty-merge + fatal snapshot-readback + atomic single-writer; tests add coverage without changing the one documented presence-only/TOCTOU pattern (safe-here per LD6). No actionable defect.
```

## Lessons Learned
The destroyed-main fixtures had to `os.Remove` the empty `main.idx` that
`CreateMetaStore` seeds — a non-obvious setup detail that initially failed 8
integration tests. See j-retrospective.md.
