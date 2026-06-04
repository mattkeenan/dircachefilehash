# Re-enable checkptr in the race gate - Testing Plan
**Task**: 10 (bugfix)

## Task Reference
- **Task ID**: internal-10
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: bugfix/10-reenable-checkptr-race-gate
- **Template Version**: 2.1

## Goal
Validate that the `unsafe.Add` rewrite is checkptr-clean at **runtime**, changes no
behaviour, adds no allocations, and that the re-armed gate stays green.

## Test Strategy
This is a behaviour-preserving internal refactor of unsafe accessors. The decisive
signal is the **race detector with checkptr ON** exercising the heap-backed read
paths — the exact configuration that previously crashed. No new production code
paths are added, so the strategy is regression-led, not new-feature-led.

### Test Levels
- **Regression (primary)**: existing `pkg` + repo suites under `go test -race ./...`
  with checkptr **ON** (no `-d=checkptr=0`). Previously crashed → must now pass.
- **Targeted unit**: `pkg/entry_serialiser_test.go` converted to call
  `RelativePath()` on a heap-backed buffer — a direct, named regression for C2.
- **Static/gate**: `go vet ./...`, `golangci-lint run ./...`, and the actual
  `.githooks/pre-commit` invocation as edited.
- **Non-functional**: allocation check on the accessor path (`-benchmem`).

### Test Coverage Targets
- **Critical paths (must be exercised under checkptr-ON `-race`)**: `GetBinaryEntry`
  (C1), `RelativePath` (C2), `calculatePathLength` (C3) on **heap-backed** indices
  (the `heapBacked` path — where `checkptrBase` is non-zero and checkptr fires).
- **Regression**: all existing tests pass; no behaviour/format change.
- **No coverage-% target** — refactor of existing lines, not new surface.

## Test Cases
### Functional Test Cases
- **TC-1 — checkptr-ON race suite passes (headline acceptance)**
  - **Given**: the `unsafe.Add` rewrite applied (C1-C3), gate not yet edited.
  - **When**: `go test -race ./...` runs with checkptr at its default (ON).
  - **Then**: all packages pass; no `fatal error: checkptr: pointer arithmetic
    result points to invalid allocation`. (Baseline: this currently aborts at
    `binary_entry.go:54` then `format/entry.go:115`.)

- **TC-2 — heap-backed `RelativePath` regression via serialiser test**
  - **Given**: `pkg/entry_serialiser_test.go` switched to call `RelativePath()` on
    its heap-allocated entry buffer.
  - **When**: run under `go test -race` (checkptr ON).
  - **Then**: the returned path equals the expected path **and** no checkptr abort —
    proving C2 is clean on the heap-backed case, not just mmap.

- **TC-3 — behaviour unchanged (round-trip / path correctness)**
  - **Given**: existing `pkg/format` round-trip and path tests
    (`roundtrip_test.go`, `entry`/codec tests).
  - **When**: run normally (checkptr on and off).
  - **Then**: identical results pre/post change; the pre-existing C2-vs-C3 8-byte
    `pathStart` discrepancy is **preserved** (no test should newly pass/fail from
    unifying them — if one does, the refactor altered behaviour and must be fixed).

- **TC-4 — gate re-armed and green**
  - **Given**: `.githooks/pre-commit` with `-d=checkptr=0` removed.
  - **When**: run the hook's race command form (`go test -race -short ./...`).
  - **Then**: exits 0; no checkptr abort.

### Non-Functional Test Cases
- **Performance / zero-copy (NFR)**: `go test -bench=. -benchmem` on any accessor/
  path benchmark (or a focused `RelativePath` micro-bench) shows **0 extra
  allocs/op** vs baseline — `unsafe.String` must still alias, no copy introduced.
- **Static analysis**: `go vet ./...` clean (the `unsafeptr` warnings that the
  `//nolint:govet` comments suppressed should no longer fire → suppressions removed);
  `golangci-lint run ./...` clean (gosec G115/G304 etc. unaffected).
- **Reliability**: no new panics; the `Size ∈ [minEntrySize,65535]` guard at
  `entry.go:99-101` remains in place bounding the scan.

## Test Environment
### Setup Requirements
- Local toolchain go1.26.x (checkptr behaviour is toolchain-sensitive; module is
  Go 1.24.3). No external services, no database, no fixtures beyond existing testdata.
- Heap-backed index path must be the one exercised (tests already construct
  in-memory/heap buffers — confirm via the serialiser test and `legacy_load_test.go`).

### Automation
- `.githooks/pre-commit` race gate (now checkptr-ON) is the CI/local enforcement point.
- No new CI wiring required; the gate edit *is* the automation change.

## Validation Criteria
- [ ] TC-1: `go test -race ./...` (checkptr ON) passes across all packages.
- [ ] TC-2: serialiser test calls `RelativePath()` and passes under `-race`.
- [ ] TC-3: round-trip/path tests unchanged; 8-byte discrepancy preserved.
- [ ] TC-4: edited pre-commit hook race command exits 0.
- [ ] NFR: 0 extra allocs/op on the accessor path; `go vet` + `golangci-lint` clean.
- [ ] `grep -rn checkptr .` shows no residual gate disable (only local-settings hit).

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-exec 10
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All test cases passed (commit `61fff80`): TC-1 (full `-race` suite checkptr ON, all
packages ok), TC-2 (serialiser test via `RelativePath()` on heap-backed entry under
`-race`), TC-3 (round-trip checkptr on+off identical; 8-byte discrepancy preserved), TC-4
(hook command exit 0). NFR: 0 B/op 0 allocs/op on `RelativePath`; `go vet` + `golangci-lint`
clean; residual-checkptr grep shows no gate disable. Security review (testing phase): no
findings. See g-testing-exec.md.

## Lessons Learned
The "decisive signal is the heap-backed `-race` path" framing was correct — TC-2's
conversion of the serialiser assertion to `RelativePath()` is what turns the headline
acceptance into a live, named regression. See j-retrospective.md.
