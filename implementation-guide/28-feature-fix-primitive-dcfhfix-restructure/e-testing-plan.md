# Fix primitive + dcfhfix restructure - Testing Plan
**Task**: 28 (feature)

## Task Reference
- **Task ID**: internal-28
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/28-fix-primitive-dcfhfix-restructure
- **Template Version**: 2.1

## Goal
Define the **integration** test strategy for the coordinating parent. Unit/
edge-case coverage already exists inside the three subtasks (28.1/28.2/28.3); the
parent does not add new unit tests. Its test job is to (1) run the **union** of
the subtask suites as a single regression gate on the assembled branch, and (2)
assert each of the five parent success criteria (a-task-plan.md) holds on the
integrated whole.

## Test Strategy
### Test Levels
- **Unit**: owned by the subtasks (`fix_*_test.go`, `fix_recovery_*_test.go`,
  `main_test.go`, `options_test.go`, `recovery_test.go`). Not re-authored here.
- **Integration**: the assembled `pkg`/`cmd` surfaces compile and the full suite
  passes together — the parent's primary level.
- **System**: end-to-end dcfhfix subcommand + library recovery-rebuild smoke on
  throwaway temp repos.
- **Acceptance**: the five parent success criteria each map to a landed, tested
  surface (the IT-1…IT-5 cases below).

### Test Coverage Targets
- **Regression**: 100% of the pre-existing dcfhfix + recovery suites pass
  unchanged alongside the new `fix_*` suites — zero regressions is the gate.
- **Critical paths**: the data-destructive write paths (recovery rebuild, auto-fix
  write) keep their subtask coverage (confinement reject, snapshot-readback gate,
  empty-guard, fault-injection atomicity) green on the assembled branch.
- **No new parent unit coverage target** — adding parent-level unit tests would
  duplicate the subtasks; the integration assertions are the parent's coverage.

## Test Cases
### Functional Test Cases (integration — map success criteria to the whole)
- **IT-1 (SC1 — Fix primitive on the interface)**:
  - **Given**: the assembled branch.
  - **When**: `Repo.Fix` is resolved and exercised through the interface.
  - **Then**: it exists on `Repo` (`pkg/repo.go`), `repoCore.Fix` implements it
    (`pkg/repo_local.go`), and it delegates to the single `RunFix` engine
    (`pkg/fix_run.go`); `pkg/fix_run_test.go` exercises it end-to-end.
- **IT-2 (SC2 — dcfhfix is a thin translator, no behaviour change)**:
  - **Given**: the assembled branch.
  - **When**: the dcfhfix CLI suites run.
  - **Then**: `cmd/dcfhfix` builds a `FixRequest` and calls `RunFix` (no private
    writer); `main_test.go`/`options_test.go` pass unchanged (CLI subcommands/
    flags/exit codes preserved); fsck helpers resolve from `pkg/`.
- **IT-3 (SC3 — multi-source recovery rebuild)**:
  - **Given**: a seeded temp repo (destroyed `main.idx` + intact `cache.idx`).
  - **When**: the `recovery-rebuild` Fix op runs through the library.
  - **Then**: a re-readable, checksum-valid `main.idx` is produced; the negative
    guards hold — recovery cannot combine with other ops (`fix_run.go:216`) and
    requires a confinement root (`fix_recovery.go:33`); `fix_recovery_*_test.go`
    (16 cases) pass.
- **IT-4 (SC4 — both fsck modes; new-file write, no in-place mutation)**:
  - **Given**: the assembled branch.
  - **When**: auto-fix and the deferred manual mode are exercised.
  - **Then**: auto-fix writes a new index via the single-writer path + atomic
    rename (never in-place); manual mode returns the typed
    `ErrManualModeUnimplemented` with no write (`fix_run.go:208`).
- **IT-5 (SC5 — existing tests pass + new coverage exists)**:
  - **Given**: the assembled branch.
  - **When**: `go test ./pkg/... ./cmd/...` runs.
  - **Then**: the pre-existing dcfhfix/recovery suites **and** the new `fix_*`/
    `fix_recovery_*` suites all pass in one run — no symbol collision, no import
    cycle, clean build.

### Non-Functional Test Cases
- **Reliability**: pre-commit `-race -d=checkptr=0` green on the assembled branch;
  the no-partial-index invariant holds (recovery empty-guard + snapshot-readback +
  fault-injection TC-16; 28.2 dry-run-writes-nothing) — produced index is always
  valid-or-absent.
- **Security**: whole-parent-changeset review — `golangci-lint run ./...` (gosec
  floor) 0 issues, `govulncheck` 0 applicable, and the CWF
  `cwf-security-reviewer-changeset` agent over the integrated changeset (the
  confinement / single-writer invariants assessed against the assembled surface).
- **Performance (informational, NFR1)**: no new index passes introduced by
  integration — the parent adds no production code; the subtasks already asserted
  the collect-once/write-once shape.
- **Usability**: dcfhfix CLI help/subcommand surface unchanged (covered by IT-2).

## Test Environment
### Setup Requirements
- Throwaway temp repos (`t.TempDir()` / `mktemp -d`) for all system smoke — never
  a real index, so a mid-write failure cannot strand a half-written index.
- No external services, network, or fixtures beyond the in-repo test data.
- Unix-like host, 64-bit, Go 1.25.0+ floor (per CLAUDE.md / go.mod).

### Automation
- `go test ./pkg/... ./cmd/...` — the union regression gate.
- Pre-commit hook: `go fmt`/`go fix`/gopls/golangci-lint/govulncheck/`go test
  -race -d=checkptr=0` (the standing gate on every commit).
- CWF changeset security review at testing-exec (`g`).

## Validation Criteria
- [ ] IT-1…IT-5 each verified against the assembled branch.
- [ ] Full `go test ./pkg/... ./cmd/...` + `-race` green (zero regressions).
- [ ] `golangci-lint run ./...` 0 issues; `govulncheck` 0 applicable.
- [ ] Whole-changeset CWF security review recorded.
- [ ] All five parent success criteria mapped to a landed, tested surface.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
The union regression gate executed green (IT-1…IT-5 all PASS): the pre-existing
dcfhfix/recovery suites and the new `fix_*`/`fix_recovery_*` suites passed in one
run with no symbol collision or import cycle; `-race`, lint(0), govulncheck(0)
all green. The only non-PASS was the expected security-review cap-exceeded error
(parent union > 500-line cap by construction).

## Lessons Learned
The parent's right test level is the union suite + per-criterion mapping, not new
unit tests — re-authoring subtask units would duplicate coverage. The changeset
security-review line cap, however, is a poor fit for a coordinating parent and
should anchor at the last child squash (recorded as a process recommendation).
