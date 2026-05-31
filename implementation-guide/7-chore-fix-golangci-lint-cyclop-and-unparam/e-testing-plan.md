# Fix outstanding golangci-lint cyclop and unparam - Testing Plan
**Task**: 7 (chore)

## Task Reference
- **Task ID**: internal-7
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/7-fix-golangci-lint-cyclop-and-unparam
- **Template Version**: 2.1

## Goal
Confirm the three extract-method/parameter-removal edits are behaviour-preserving and that the lint gate is clean.

## Test Strategy
Behaviour-preserving refactor — the test approach is **regression-based, not new-feature-based**. No new runtime behaviour is introduced, so the acceptance bar is "the existing suite still passes and lint is clean", plus closing the one coverage gap the refactor touches.

### Test Levels
- **Static gate (primary)**: `golangci-lint run ./...` → 0 issues (was 3). This is the direct acceptance signal for the task.
- **Unit/regression**: `go test ./...` — existing suites exercise the touched code paths:
  - `cmd/dcfhfind/expressions_test.go`, `main_test.go` — expression-parser construction (the `parseTestToken` path).
  - `pkg/api_test.go`, `pkg/cache_test.go` — selector resolution / index references (the `resolveOneSelector` path).
  - `pkg/binary_entry_scan_test.go` + the BEScan suite — exercise `scanTestHelper.createTestEntry`.
- **Build**: `go build ./...` clean.

### Coverage Gap (identified during planning)
The four stateful tokens moved by the `parseStatefulArgTest` extraction — `--min-size`, `--max-size`, `--start-date`, `--end-date` — have **no direct unit test** in `cmd/dcfhfind/`. The extraction is verbatim code movement so risk is low, but this is the only refactored block lacking automated coverage. **Decision point for exec**: either (a) add a minimal table test asserting these four tokens parse to the expected `*MinSizeTest`/`*MaxSizeTest`/`*MTimeRangeTest` types and that a malformed arg returns an error (does not fall through), or (b) cover via a CLI smoke check (TC-4). Recommend (a) — cheap, closes the gap permanently, and directly guards the `handled=true`-on-error invariant.

## Test Cases
### Functional Test Cases
- **TC-1 — Lint clean**
  - **Given**: the three edits applied
  - **When**: `golangci-lint run ./...`
  - **Then**: exit 0, "0 issues"; specifically no cyclop on `resolveOneSelector`/`parseTestToken`, no unparam on `createTestEntry`.
- **TC-2 — Selector resolution unchanged**
  - **Given**: a repo with `main.idx`, `cache.idx`, and one or more `scan-*.idx`
  - **When**: resolving selectors `main`, `cache`, `scan`, `all`, and a bare `scan-<id>`
  - **Then**: identical `IndexRef` sets (paths, types, ScanIDs) to pre-refactor — `all` still recurses through the extracted `resolveScanSelector`. Covered by `go test ./pkg/...`.
- **TC-3 — Expression parsing unchanged**
  - **Given**: dcfhfind token streams using no-arg tests (`--empty`/`--valid`/`--corrupt`) and table-driven arg tests
  - **When**: `go test ./cmd/dcfhfind/...`
  - **Then**: all existing parser assertions pass unchanged.
- **TC-4 — Stateful arg tokens (gap closure)**
  - **Given**: tokens `--min-size 1M`, `--max-size 1M`, `--start-date 2024`, `--end-date 2024`, and a malformed `--min-size notasize`
  - **When**: parsed via `parseTestToken`/`parseStatefulArgTest`
  - **Then**: valid tokens yield the expected Expression types; the malformed one returns an error and `handled=true` (no fall-through to `argTestTable`).

### Non-Functional Test Cases
- **Performance**: N/A — no hot-path or allocation change (helpers are one-call extractions).
- **Security**: N/A — confirmed no new I/O, exec, or env surface (security plan-review: no findings).
- **Reliability**: error-propagation invariant for stateful tokens covered by TC-4.

## Test Environment
### Setup Requirements
- Standard Go toolchain (Go 1.24.3) + `golangci-lint` already configured via `.golangci.yml`.
- TC-2 uses the existing pkg test fixtures (temp `.dcfh` dirs); no production data.

### Automation
- `make build` / `go test ./...` / `golangci-lint run ./...` — same gates as `.githooks/pre-commit`.

## Validation Criteria
- [ ] TC-1: `golangci-lint run ./...` reports 0 issues.
- [ ] TC-2/TC-3: `go test ./...` passes (regression).
- [ ] TC-4: stateful-token coverage added (or smoke-verified) and passing.
- [ ] `go build ./...` clean.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
*To be filled upon completion*

## Lessons Learned
*To be captured during implementation*
