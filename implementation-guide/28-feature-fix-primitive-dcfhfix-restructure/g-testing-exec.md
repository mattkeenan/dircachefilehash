# Fix primitive + dcfhfix restructure - Testing Execution
**Task**: 28 (feature)

## Task Reference
- **Task ID**: internal-28
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/28-fix-primitive-dcfhfix-restructure
- **Template Version**: 2.1

## Goal
Execute the **integration** test plan (e-testing-plan.md): run the union of the
subtask suites as one regression gate on the assembled branch and verify each of
the five parent success criteria (IT-1…IT-5) holds on the integrated whole.

## Execution Checklist
- [x] Read e-testing-plan.md and d-implementation-plan.md thoroughly
- [x] Verify test environment ready (Go 1.26 toolchain, go.mod floor 1.25.0)
- [x] Execute test cases sequentially
- [x] Record pass/fail for each test
- [x] Document failures with reproduction steps (none)
- [x] Update status to "Finished" when all pass

## Test Results

### Functional Tests (integration — success criteria on the assembled whole)

| Test ID | Success criterion | Evidence (assembled branch) | Status |
|---------|-------------------|-----------------------------|--------|
| IT-1 | SC1 Fix primitive on the interface, single engine | `TestRepoFix_ThroughInterface`, `TestFixOpClassification_FailClosed` (pkg) — `Repo.Fix`→`repoCore.Fix`→single `RunFix` resolved end-to-end | PASS |
| IT-2 | SC2 dcfhfix a thin translator, no behaviour change | `TestHandleFixesCommand`, `TestSiblingPreFixPath_Shape` (cmd/dcfhfix) + live CLI round-trip (f Step 5); fsck helpers resolve from `pkg/` | PASS |
| IT-3 | SC3 multi-source recovery rebuild + negative guards | `TestRecovery_RebuildFromCache`, `TestMerge_*` (7), and guards `TestRecovery_OpMixingRejected` (mutual-exclusion), `TestRecovery_OutOfMetaDirSourceRejected` (confinement), `TestRecovery_EmptyGuardAborts`, `TestRecovery_DryRunWritesNothing`, `TestRecovery_ContextCancelledBeforeWrite` | PASS |
| IT-4 | SC4 both fsck modes; new-file write, no in-place mutation | `TestRunFix_ManualModeTypedError` (typed `ErrManualModeUnimplemented`, no write), `TestRunFix_EntryAppendProducesValidIndex` (single-writer + atomic rename), `TestRunFix_DryRunWritesNothing` | PASS |
| IT-5 | SC5 existing tests pass + new coverage in one run | `go test -count=1 ./pkg/... ./cmd/...` all `ok` — pre-existing dcfhfix/recovery suites and the new `fix_*`/`fix_recovery_*` suites pass together; no symbol collision, no import cycle (clean build) | PASS |

Full union run (fresh, `-count=1`): `pkg`, `pkg/format`, `pkg/fsdedupe`,
`cmd/dcfh`, `cmd/dcfh/internal/tui`, `cmd/dcfhfind`, `cmd/dcfhfix` — all `ok`.
460 `Test*`/`Example*` functions across `pkg` + `cmd`.

### Non-Functional Tests

| Dimension | Test | Result |
|-----------|------|--------|
| Reliability | `go test -race -gcflags=all=-d=checkptr=0 ./pkg/... ./cmd/...` | PASS (exit 0) |
| Reliability | No-partial-index invariant — `TestRecovery_FaultInjectionAtomicity` (sync fault → no `main.idx`, no `.fix.tmp`); `TestRecovery_EmptyGuardAborts`; `TestRunFix_DryRunWritesNothing` | PASS |
| Security | `golangci-lint run ./...` (gosec floor) | `0 issues.` |
| Security | `govulncheck ./...` | `No vulnerabilities found` (0 your-code / 0 imported; 8 in required modules, uncalled) |
| Security | CWF changeset semantic review of the assembled whole | **error: cap exceeded** — see Security Review (parent union > 500-line cap by construction; each subtask reviewed under-cap with `no findings`) |
| Performance (NFR1, informational) | No new index passes — parent adds 0 production lines; collect-once/write-once already asserted by subtasks | PASS (by construction) |
| Usability | dcfhfix CLI help/subcommand surface unchanged (`header`/`entry`/`fixes` families) — verified live (f Step 5) | PASS |

## Test Failures
None.

## Coverage Report
`go tool cover` on `pkg` for the integrated fix/recovery surface:
- `hasRecoveryOp` 100%, `verifyRecoverySnapshot` 100%, `mergeSourcesIntoEntries`
  93.3%, `orderedSourcePaths` 93.8%, `runRecoveryRebuild` 86.7%.
- `fixOpIsWrite` 100%, `fixOpMutatesIndex` 100%, `RunFix` 88.9%,
  `repoCore.Fix` 70.0%.
- Lowest figures are cosmetic backup-description helpers (`backupDescription`
  25%, `jsonDesc` 0%) — not load-bearing; the destructive/confinement/atomicity
  paths are the well-covered ones. Consistent with the per-subtask coverage
  recorded in 28.2/28.3 (no regression on integration).

## Status
**Status**: Finished
**Next Action**: /cwf-rollout
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Every IT case (IT-1…IT-5) PASS on the assembled branch; full union suite +
`-race` + lint(0) + govulncheck(0) green; recovery negative guards each
individually test-covered; no failures, no regressions. The only non-functional
non-PASS is the CWF changeset review's expected cap-exceeded error (parent union
diff), with each subtask already reviewed under-cap at `no findings`.

## Lessons Learned
The parent's testing-exec is a regression gate, not new authorship: the union
suite + targeted per-criterion runs + coverage on the assembled branch prove the
three subtasks compose without re-deriving their unit cases. The changeset
security review's line cap is a poor fit for a coordinating parent (the union
necessarily exceeds it) — the meaningful semantic review unit is the per-subtask
diff, which had full under-cap coverage.

## Security Review

**State**: error

error: cap exceeded: 3732 production lines > 500

> Expected by construction, not a skipped review (identical to the f-phase
> result). The parent is anchored at the pre-decomposition baseline `c385cb8`, so
> `security-review-changeset` diffs the **union** of all three subtasks (3732
> production lines) and trips the 500-line cap (exit 2 → record error, do not
> invoke the subagent). Each subtask was reviewed **under-cap** at its own f/g
> exec phases with verdict `no findings` (28.1/28.2/28.3). The integration parent
> adds **zero production lines** — no new surface for a semantic review. The
> confinement, single-writer, and atomicity invariants on the assembled whole are
> exercised by the green union suite (`-race`, `TestRecovery_*` guard + atomicity
> cases) and `golangci-lint`(0)/`govulncheck`(0) above.
