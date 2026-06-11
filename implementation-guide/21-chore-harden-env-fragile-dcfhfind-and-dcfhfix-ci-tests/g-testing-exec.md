# Harden env-fragile dcfhfind and dcfhfix CI tests - Testing Execution
**Task**: 21 (chore)

## Task Reference
- **Task ID**: internal-21
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/21-harden-env-fragile-dcfhfind-and-dcfhfix-ci-tests
- **Template Version**: 2.1

## Goal
Execute the tests defined in e-testing-plan.md and verify implementation from d-implementation-plan.md.

## Execution Checklist
- [x] Read e-testing-plan.md and d-implementation-plan.md thoroughly
- [x] Verify test environment ready
- [x] Execute test cases sequentially
- [x] Record pass/fail for each test
- [x] Document failures with reproduction steps
- [x] Update status to "Testing" when in progress, "Finished" when all pass

## Test Results

### Functional Tests

| Test ID | Test Case | Expected | Actual | Status | Notes |
|---------|-----------|----------|--------|--------|-------|
| TC-1 | dcfhfind integration tests skip when binary absent | SKIP, not FAIL | All four (`TestIntegration`, `TestValidationIntegration`, `TestActionFormats`, `TestPerformanceWarning`) report `--- SKIP`; package `ok` | PASS | Ran `./cmd/dcfhfind/` with `cmd/dcfhfind/dcfhfind` moved aside — the exact CI clean-checkout condition |
| TC-2 | dcfhfix list test succeeds without ambient `.dcfh/` | list case passes, no backups | `--- PASS`; emits "No backups found for nonexistent" via the test's own `t.TempDir()/.dcfh/` | PASS | Hermetic — independent of cwd/ambient repo |
| TC-3 | error-case assertions preserved | both error cases still assert `errMsg` | `No_subcommand` + `Unknown_subcommand` `--- PASS` | PASS | Per-case `indexFile` change did not weaken assertions |
| TC-4 | developer-machine path unchanged | full suite green, `TestPerformanceWarning` executes | `make test` all packages `ok`; with binary present `TestPerformanceWarning` runs (not skips) | PASS | `make test` = generate + `go test ./...` |

### Non-Functional Tests
- **Determinism/Reliability**: PASS — both tests now produce identical verdicts on a clean
  checkout and a developer machine (SKIP vs run only for the binary-dependent integration tests,
  which is the intended, documented behaviour).
- **Security**: covered by the changeset security review below (no findings); plan-phase
  security reviewer also reported no FR4 concerns.

### Pre-commit gate (recorded at f-phase commit f10fa5e)
golangci-lint: 0 issues · govulncheck: 0 affecting vulns · `go test -race`: all packages `ok`.

## Test Failures
None.

## Coverage Report
No new production code → no coverage delta. The git diff is confined to the two `_test.go`
files (`cmd/dcfhfind/integration_test.go`, `cmd/dcfhfix/main_test.go`); generated
`constants_version.go` files are gitignored and not part of the change.

## Status
**Status**: Finished
**Next Action**: /cwf-retrospective
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
*To be filled upon completion*

## Lessons Learned
*To be captured during retrospective*

## Security Review

**State**: no findings

Both claims are verified: the `promote_integration_test.go:157-163` precedent exists exactly as cited (`os.Mkdir(filepath.Join(dir, ".dcfh"), 0755)`), and `.golangci.yml` carries `_test\.go` exclusion entries.

### Review

This is a testing-exec changeset. The substantive code surface is two test files; the rest is CWF process documentation (three new `implementation-guide/21-…/*.md` files). I reviewed the actual diff against the five FR4 threat categories.

**(a) Bash injection / unsafe command construction.** No shell invocation in the diff. The Go changes use `os.Stat`, `os.Mkdir`, `filepath.Join`, `t.Skip`, `t.Fatalf`, and `handleFixesCommand`. No `exec.Command`, no `os/exec`, no string-built shell line. Nothing to interpolate. Clean.

**(b) Perl / git-output parsing.** No Perl, no git porcelain parsing in the changeset. N/A.

**(c) Prompt injection via user-supplied strings.** The three new markdown files are CWF workflow artefacts (plan/testing/exec records) authored by the workflow, not SKILL templates with `{arguments}` substitution. They introduce no new conduit for untrusted free-text into LLM context. Note that `f-implementation-exec.md` embeds a prior `## Security Review` block including a `cwf-review` fence — that is recorded prose, not an instruction surface, and it is the implementation-exec verdict, distinct from this review. Clean.

**(d) Unsafe environment-variable handling.** No env-var reads introduced. `t.TempDir()` is the standard framework-managed hermetic temp root (auto-cleaned), and the `.dcfh` path is composed via `filepath.Join` from that trusted root — no `..`, no user input, no `chmod`/`rm` of an env-derived path. The change in fact narrows environment coupling: the dcfhfix list test no longer walks up to an ambient repo `.dcfh/`, and the dcfhfind tests skip rather than fail on a missing artefact. Clean.

**(e) Pattern-based risks.** One worth framing:

- `os.Mkdir(filepath.Join(tmpDir, ".dcfh"), 0755)` uses world-readable/traversable perms. Safe here because it is a `t.TempDir()`-scoped throwaway directory holding no secrets, and it mirrors the verified existing precedent at `cmd/dcfhfix/promote_integration_test.go:158`. The gosec config keeps G301/G302/G306 active for production but scopes `_test.go` out, so this is intentionally outside the static gate. Audit future uses where a bare `0755` `os.Mkdir` is copied into a production path that writes secret or integrity-tracked files — there the active perms rules and the `.dcfh/` non-secret-metadata rationale would govern, and `0700`/`0600` would likely be required.

The `t.Fatal` → `t.Skip` conversions marginally weaken the test safety net (a missing-binary regression now skips), but that is a test-determinism trade-off documented in the plan, with no injection or data-exposure dimension.

No actionable security findings.

```cwf-review
state: no findings
summary: Test-only Go changes plus CWF process docs; no shell/env/injection surface. The 0755 t.TempDir .dcfh mkdir is test-scoped (gosec _test.go-excluded) and matches the verified promote_integration_test.go precedent.
```
