# Fault-injection tests for atomic replacement - Maintenance
**Task**: 23 (feature)

## Task Reference
- **Task ID**: internal-23
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/23-fault-injection-atomic-replacement
- **Template Version**: 2.1

## Goal
Keep the task 23 fault-injection suite and the FR6 cancellation guard healthy
over time: the tests are permanent regression sentinels for the
atomic-replacement, scan-edge, and mid-scan-cancel invariants, and the test-only
`io_seam.go` indirection carries an invariant that must not drift into production.

## Monitoring Requirements
### System Health
- **Uptime / Performance / Resource Usage**: N/A — this is a library/CLI test
  suite, not a running service. The only "health" signal is the CI status of the
  `pkg` test package on `main`.

### Application Metrics
- **CI signal**: `go test ./pkg/...` and `go test -race -gcflags=all=-d=checkptr=0
  ./pkg/...` must stay green. TC-1..TC-9 failing is the regression indicator.
- **Flakiness**: the fault hooks run on multiple hash-worker goroutines; any
  reintroduced shared mutable state would surface under `-race`. Treat a flaky
  TC-7/TC-8/TC-9 as a real concurrency defect, not a re-run-until-green nuisance
  (TC-9 already needed a `sync.Once` fix for exactly this reason — see D3).
- **Error Rates**: N/A.

### Alerting Rules
- **Critical**: any TC-1..TC-9 failure on `main` — the atomic-replacement or FR6
  integrity guarantee has regressed; block the offending change.
- **Warning**: `golangci-lint`/gosec flags the seam (e.g. a new G302/G304 site
  without rationale) — investigate before merge.
- **Info**: routine — no standing alert.

## Maintenance Tasks
### Regular Maintenance Schedule
- **On every change touching the write path** (`pipeline_update.go`,
  `status.go`, `temp_index_writer.go`, `hash_pool.go`, `io_seam.go`): re-run the
  `pkg` suite incl. `-race`; the fault tests are the proof the path still fails
  safely.
- **Periodic** (with the repo's normal dead-code / dependency sweeps): confirm
  the seam vars still have exactly one assignment site (their `io_seam.go`
  initialiser) outside `_test.go`. Command of record:
  `rg 'fs(Rename|OpenFile|Sync)\s*=' pkg/` and
  `rg 'hashPreReadHook\s*=' pkg/` → only the initialisers and `_test.go` swaps.

### Preventive Maintenance
- **Guard the seam INVARIANT**: `fsRename`/`fsOpenFile`/`fsSync`/`hashPreReadHook`
  must never be assigned from non-test code, and must never be wired to an
  env var / config key / CLI flag. Doing so converts a test seam into a runtime
  index-write-failure / pre-read-injection override vector. The invariant is
  documented inline in `io_seam.go`; both security reviews flagged this as the
  one pattern (category e) to audit on future change.
- **Teeth stay sharp**: if the FR6 guard or the `finaliseMainIndex` `!ok` cleanup
  is ever refactored, re-run the teeth checks (T-A/T-B/T-C in g-testing-exec.md)
  to confirm the tests still fail loudly when the guard is removed.
- Dead-code audit (see `.cwf/docs/dead-code-audit.md`) — the seam vars will read
  as "only assigned, helpers only test-referenced"; that is by design, not dead
  code. Note it so a future sweep does not delete them.

## Incident Response
### Common Issues
- **TC-9 flakes under `-race`**: a newly-added hook or shared counter in the hash
  pipeline is racing. Resolution: make the shared write one-shot/atomic (the
  existing fix wraps `cancel()` in `sync.Once`); do not paper over with retries.
- **FR4 "loads clean" assertion fails on a freshly promoted index**: do **not**
  reach for `ValidateIndexHeader` — its `validateHeaderChecksum` diverges from
  what the writer produces and fails even a normal `main.idx` (deviation D1). Use
  the production loader `loadIndexFromFileWithTracking` via `assertLoadsClean`.
- **A new write primitive isn't intercepted by a fault test**: it was added
  outside the seam. Route it through `io_seam.go` (or justify why it stays on the
  trusted-only path, as the wire handler write deliberately does).

### Troubleshooting Guide
- **Symptom**: atomic-replacement test fails after a write-path change.
- **Diagnosis**: read the failing assertion message — each names the invariant it
  guards (e.g. "prior main.idx must survive injected rename fault"). Map it back
  to the FR row in `e-testing-plan.md`.
- **Resolution**: restore the guarded behaviour; re-run `pkg` + `-race`; re-run
  the relevant teeth check to confirm the test is still load-bearing.

### Escalation Procedures
- N/A — single-repo developer workflow. "Escalation" is opening a follow-up CWF
  task (e.g. the D1 checksum-validator reconciliation noted below).

## Performance Optimisation
### Optimisation Areas
- None outstanding. The production change adds no hot-path cost (NFR1): one
  pointer indirection per primitive, one nil-check per hashed entry, one
  `ctx.Err()` per update.

### Scaling Strategy
- N/A.

## Documentation
### Runbooks
- Re-running the suite: `go test ./pkg/ -run 'TestAtomic_|TestScanEdge_' -v`.
- Full safety gate: `go test -race -gcflags=all=-d=checkptr=0 ./pkg/...` then
  `golangci-lint run ./...`.
- Seam-invariant audit: the two `rg` commands under Preventive Maintenance.

### Knowledge Base
- Architecture decision: production write primitives are reached through a
  package-internal function-pointer seam (`io_seam.go`) so tests can inject
  `os.Rename`/`os.OpenFile`/`*os.File.Sync` failures without a real failing disk;
  the seam is inert in production (defaults to the real calls, hook nil).
- The two checksum validators (production `verifyHeaderChecksum` vs repair-path
  `validateHeaderChecksum`) disagree; D1 captured this. Follow-up below.

## Follow-up Items
- **Backlog (low)**: reconcile `ValidateIndexHeader`'s `validateHeaderChecksum`
  with the production writer/loader so the repair/dcfhfind path can validate a
  normally-promoted index. Surfaced by D1; out of scope for task 23.

## Success Criteria
- [x] Monitoring defined — CI green on `pkg` incl. `-race` is the single signal.
- [x] Maintenance procedures documented — seam-invariant audit commands + teeth
      re-run procedure recorded.
- [x] Common issues documented with resolutions — D1 oracle trap, TC-9 race,
      unseamed-primitive cases captured.
- [x] Follow-up captured — D1 checksum-validator reconciliation noted for backlog.

## Status
**Status**: Finished
**Next Action**: /cwf-retrospective 23
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Maintenance for a test-and-guard change is regression-sentinel upkeep, not
service ops: keep TC-1..TC-9 green (treating any flake as a real concurrency
bug), and protect the `io_seam.go` invariant from drifting into production or
becoming externally settable. One follow-up (D1 checksum-validator reconciliation)
is recorded for the backlog.

## Lessons Learned
The maintenance template assumes a deployed service (SLAs, on-call, scaling). For
a library test suite the durable maintenance artefacts are: (1) the named audit
commands that keep the test-only seam test-only, and (2) the teeth checks that
keep the tests from silently going vacuous after a refactor. Recording those is
worth more than placeholder uptime targets.
