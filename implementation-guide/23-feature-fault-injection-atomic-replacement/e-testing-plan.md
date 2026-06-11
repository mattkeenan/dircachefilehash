# Fault-injection tests for atomic replacement - Testing Plan
**Task**: 23 (feature)

## Task Reference
- **Task ID**: internal-23
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/23-fault-injection-atomic-replacement
- **Template Version**: 2.1

## Goal
Define the test matrix that proves the atomic-replacement integrity invariant under
injected I/O faults, the scan-churn tolerance, and the FR6 cancellation bugfix —
mapping every test to an FR/AC, with teeth checks guarding against vacuous passes.

## Test Strategy
### Test Levels
- **Unit (seam inertness)**: confirm the four seam vars default to the real funcs and
  the hook is nil in a production build (compile-time + a smoke assertion).
- **Integration (primary level)**: drive the real `update`/`status` pipelines against
  a `t.TempDir()` repo with a fault installed, asserting on-disk index state. This is
  where FR2-FR6 live — the integrity guarantee is a property of the whole pipeline,
  not an isolated unit.
- **Regression**: the full existing `./pkg/...` suite must stay green (seam inert on
  success paths; FR6 guard only affects the cancel path).
- **Static/security gate**: `golangci-lint run ./...` (gosec over new `io_seam.go`).

### Test Coverage Targets
- **Critical path (atomic replacement)**: every Production-Contract row exercised on
  both main and cache paths (open/write-via-sync/rename × {main, cache}).
- **Scan churn**: delete-before-hash and modify-before-hash.
- **Cancellation (FR6 fix)**: one promotion-blocked-on-cancel test + teeth check.
- **No fixed numeric % target** — this task adds *behavioural* fault coverage to a
  previously-uncovered path; success is "every contract row asserted", not a line %.
- **Teeth**: ≥1 new test must fail when the relevant production logic is reverted.

## Test Cases
### Functional Test Cases (integration unless noted)

- **TC-1 — Main rename fault preserves prior index (FR2/FR3/FR4)**
  - *Given*: a temp repo with a committed `main.idx` (one successful `update`), bytes
    captured; `withRenameFault(t, errInjected)` installed.
  - *When*: a second `update` runs (would promote a new temp).
  - *Then*: `update` returns nil (rename error swallowed by design — FR3 carve-out);
    `main.idx` bytes equal the capture (FR2); a `main-<ts>` temp remains and loads +
    validates clean (FR4 retained-and-valid).

- **TC-2 — Main open fault surfaces error, no residue (FR2/FR3/FR4)**
  - *Given*: committed `main.idx`, bytes captured; `withOpenFault(t, errInjected)`.
  - *When*: `update` runs.
  - *Then*: `update` returns non-nil error (FR3); `main.idx` unchanged (FR2); no temp
    residue — main `!ok` path removes it (FR4).

- **TC-3 — Main sync fault surfaces error, no residue (FR2/FR3/FR4)**
  - *Given*: committed `main.idx`, captured; `withSyncFault(t, errInjected)`.
  - *When*: `update` runs.
  - *Then*: non-nil error (FR3); `main.idx` unchanged (FR2); temp removed (main `!ok`).

- **TC-4 — Cache rename fault preserves prior cache, temp retained (FR2/FR3/FR4)**
  - *Given*: seeded `main.idx`; a committed `cache.idx` captured;
    `withRenameFault(t, errInjected)`.
  - *When*: `res, err := runStatus(ctx, ms, sr, map[string]string{}, nil)`.
  - *Then*: prior `cache.idx` bytes unchanged (FR2); the `cache-<ts>` temp is retained
    and loads clean (FR4 — retained for startup merge); rename error swallowed.

- **TC-5 — Cache open fault: error surfaced, temp retained (FR2/FR3/FR4)**
  - *Given*: seeded repo (no fs changes required — temp writer opens eagerly);
    `withOpenFault(t, errInjected)`.
  - *When*: `runStatus(...)`.
  - *Then*: non-nil error (FR3); prior `cache.idx` intact (FR2); per Production
    Contract the cache `!ok` path **retains** the temp for startup merge — assert it
    is present (open fault means nothing was written, so assert no *stale* partial is
    promoted over `cache.idx`; document if the temp is absent because open failed
    before creation).

- **TC-6 — Cache sync fault: error surfaced, retained temp loads clean (FR2/FR3/FR4)**
  - *Given*: seeded repo; `withSyncFault(t, errInjected)`.
  - *When*: `runStatus(...)`.
  - *Then*: non-nil error (FR3); prior `cache.idx` intact (FR2); the retained
    `cache-<ts>` temp loads + validates clean (FR4 — sync fault fires *after* header +
    body writes, so the file is structurally complete).

- **TC-7 — Delete-before-hash tolerated (FR5)**
  - *Given*: committed repo; a **new** file `Z` added (a new path always enters the
    hash stage, avoiding the `needsHash` mtime-granularity trap);
    `withHashPreReadHook` that `os.Remove`s `Z`'s abs path when relPath == `Z`.
  - *When*: `update` runs.
  - *Then*: `update` exits success; `Z`'s entry hash is empty (`IsHashEmpty`) — no
    torn/partial hash; the resulting `main.idx` loads + validates clean.

- **TC-8 — Modify-before-hash tolerated (FR5)**
  - *Given*: as TC-7 but the hook rewrites `Z` to different bytes instead of deleting.
  - *When*: `update` runs.
  - *Then*: success exit; `Z`'s entry hash empty (no corrupt value); index validates
    clean.

- **TC-9 — Mid-scan cancel does NOT promote a partial index (FR6 fix)**
  - *Given*: a committed repo with several files (so the pipeline has un-processed
    tail at cancel time), `main.idx` bytes captured; a `context.WithCancel` ctx;
    `withHashPreReadHook` that calls `cancel()` on its first invocation.
  - *When*: `update` runs under the cancellable ctx, after the production guard is
    applied.
  - *Then*: `update` returns a non-nil (`ctx.Err()`-derived) error; `main.idx` bytes
    equal the capture (no promotion); the temp was removed (`!ok` branch).

### Teeth checks (AC5 — anti-vacuous; run manually, revert, do NOT commit the break)
- **T-A**: revert the FR6 guard in `performPipelineScan` → TC-9 must fail (asserts the
  guard is load-bearing).
- **T-B**: make `finaliseMainIndex` skip the `!ok` `os.Remove` → TC-2/TC-3 residue
  assertion must fail.
- **T-C**: stub `withRenameFault` to delegate to the real `os.Rename` → TC-1 FR2
  assertion must fail (asserts the seam actually intercepts).
- Record each in g-testing-exec; restore immediately.

### Non-Functional Test Cases
- **Reliability/determinism (NFR5)**: TC-7/TC-8/TC-9 drive the mutation/cancel through
  the hook at a known stage — no wall-clock sleeps. Run the new tests under
  `go test -race -gcflags=all=-d=checkptr=0 ./pkg/...`; must be green and non-flaky
  (loop the scan-edge tests ~20× locally as a flake check).
- **Performance (NFR1)**: no benchmark required; assert by inspection that the
  production diff adds only one function-pointer indirection per primitive + one nil
  check per entry + one `ctx.Err()` check per update (off the hot byte path).
- **Security (NFR4)**: confirm seam vars are unexported and assigned only in
  `_test.go` (grep `fs(Rename|OpenFile|Sync)\s*=` / `hashPreReadHook\s*=` → matches in
  `*_test.go` + the `io_seam.go` initialisers only). `golangci-lint run ./...` clean.
- **Usability (NFR2)**: each failure assertion carries a message naming the invariant
  (e.g. `"prior main.idx must survive injected rename fault"`).

## Test Environment
### Setup Requirements
- `t.TempDir()` repos only; no production `.dcfh`, no real user data.
- Reuse the canonical setup/run idiom: `NewMetaStore(dir, dir)` +
  `runUpdate(ctx, ms, ms.scanRun(), map[string]string{})` (per
  `basic_integration_test.go`) and `runStatus(...)` for the cache path.
- Fault helpers from `fault_inject_test.go` (`swapFn` + four `with*` installers);
  shared `errInjected`. No new third-party deps.
- Tests must not call `t.Parallel()` (shared seam globals — NFR5).

### Automation
- New tests live in `pkg/atomic_index_test.go`, `pkg/scan_edge_cases_test.go`, helpers
  in `pkg/fault_inject_test.go`; picked up by the existing `go test ./pkg/...` CI job
  and the `.githooks/pre-commit` `-race` gate. No CI YAML change needed.

## Known Gaps (deliberate, recorded)
- **FR4-secondary (cleanup-failure self-heal)**: not covered — seaming
  `CleanupTimestampedCacheFiles` is a different mechanism for a non-integrity case
  (index already promoted before cleanup). Recorded per design Decision; revisit only
  if it bites in practice.
- **vectorio write-failure**: represented by the Sync fault (design Decision 2a); no
  dedicated `WritevRaw` injection.

## Validation Criteria
- [ ] TC-1…TC-9 all pass.
- [ ] Teeth checks T-A/T-B/T-C each fail on the deliberate break, pass after restore.
- [ ] `go test ./pkg/...` and `go test -race -gcflags=all=-d=checkptr=0 ./pkg/...` green.
- [ ] `golangci-lint run ./...` clean (gosec over `io_seam.go`).
- [ ] AC1-AC5 (b-requirements-plan.md) satisfied; production diff = 5 seam swaps +
      `io_seam.go` + FR6 guard, nothing else.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-exec 23
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
*To be filled upon completion*

## Lessons Learned
*To be captured during implementation*
