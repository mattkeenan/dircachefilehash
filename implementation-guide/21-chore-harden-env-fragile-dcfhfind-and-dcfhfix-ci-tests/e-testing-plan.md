# Harden env-fragile dcfhfind and dcfhfix CI tests - Testing Plan
**Task**: 21 (chore)

## Task Reference
- **Task ID**: internal-21
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/21-harden-env-fragile-dcfhfind-and-dcfhfix-ci-tests
- **Template Version**: 2.1

## Goal
Define test strategy and validation approach for Harden env-fragile dcfhfind and dcfhfix CI tests.

## Test Strategy
This is a test-hardening chore: the "code under test" is itself the test suite. The
validation strategy is therefore behavioural — run the affected tests under the same
conditions CI uses (clean checkout: no pre-built `./dcfhfind`, no ambient `.dcfh/`) and
confirm green, then confirm the developer-machine path (binaries built) still works.

### Test Levels
- **Existing test suite**: `go test ./...` — primary signal (must pass on a clean checkout).
- **Targeted behaviour**: run the two affected tests directly and inspect SKIP/PASS status.
- **CI**: the GitHub Actions `make test` job on the branch is the authoritative clean-env run.

### Test Coverage Targets
- No new production code → no coverage delta expected.
- The two modified tests must continue to exercise their original assertions on the
  developer-machine path (binary built / `.dcfh/` present).

## Test Cases
### Functional Test Cases
- **TC-1**: dcfhfind integration tests skip cleanly when the binary is absent
  - **Given**: `./dcfhfind` does not exist (clean checkout, no `make build`)
  - **When**: `go test ./cmd/dcfhfind/` runs
  - **Then**: `TestPerformanceWarning` (and the three sibling integration tests) report SKIP,
    not FAIL; package result is `ok`/skipped, never FAIL.

- **TC-2**: dcfhfix list test succeeds without an ambient `.dcfh/`
  - **Given**: a clean checkout with no `.dcfh/` anywhere up the tree from the test's cwd
  - **When**: `go test ./cmd/dcfhfix/ -run TestHandleFixesCommand` runs
  - **Then**: the `list` case passes — `getBackupDir` resolves the test's own
    `t.TempDir()/.dcfh/`, `listBackups` returns no backups, `handleFixesCommand` returns nil.

- **TC-3**: error-case assertions are preserved
  - **Given**: the `TestHandleFixesCommand` "No subcommand" and "Unknown subcommand" cases
  - **When**: the test runs
  - **Then**: both still error and `strings.Contains(err.Error(), errMsg)` holds — the
    per-case `indexFile` change does not weaken these assertions.

- **TC-4**: developer-machine path unchanged
  - **Given**: `make build` has produced `./dcfhfind` and the repo has its `.dcfh/`
  - **When**: `make test` runs
  - **Then**: `TestPerformanceWarning` actually executes (`--help` invocation), not skips;
    full suite green.

### Non-Functional Test Cases
- **Reliability/Determinism**: the two tests no longer depend on ambient filesystem state —
  result is identical between a clean checkout and a developer machine (modulo SKIP vs run
  for the binary-dependent integration tests).
- **Security**: covered by the plan-review security pass — no FR4 concerns; hermetic
  `.dcfh/` is rooted under a trusted `t.TempDir()`.

## Test Environment
### Setup Requirements
- **Clean-env simulation** (for TC-1/TC-2): run `go test` from a state with no `./dcfhfind`
  binary; the dcfhfix case is hermetic by construction (its `.dcfh/` is under `t.TempDir()`),
  so it needs no special cwd.
- **Developer path** (for TC-4): `make build` then `make test`.

### Automation
- `make test` (= `generate` + `go test ./...`) is the project gate.
- GitHub Actions CI on the branch provides the authoritative clean-checkout run.

## Validation Criteria
- [ ] `make test` green locally
- [ ] `go test ./cmd/dcfhfind/` with `./dcfhfind` removed → SKIP, not FAIL (TC-1)
- [ ] `go test ./cmd/dcfhfix/ -run TestHandleFixesCommand` green, all three sub-cases (TC-2, TC-3)
- [ ] Branch CI run green (TC-1/TC-2 in the real clean-checkout env)
- [ ] No production code changed (git diff confined to the two `_test.go` files)

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
*To be filled upon completion*

## Lessons Learned
*To be captured during implementation*
