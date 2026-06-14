# Relocate fsck helpers to pkg and fix entry writer - Testing Execution
**Task**: 28.1 (chore)

## Task Reference
- **Task ID**: internal-28.1
- **Task URL**: N/A (internal task)
- **Parent Task**: 28
- **Branch**: chore/28.1-relocate-fsck-helpers-to-pkg-and-fix-entry-writer
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
| TC-1 | Pure relocation behaviour-preserving (M1 regression gate) | `go build ./...` + `go test ./...` green, no expectation changes | Full suite green | PASS | Verified at HEAD; M1 (fba94a96) regression gate intact. |
| TC-2 | Variable-length path round-trip (FR9 core) | Every path byte-identical incl. CJK + long path | All 3 paths round-trip | PASS | `TestWriter_VariableLengthPathRoundTrip` — the FR9 fix. |
| TC-3 | checksum_type preserved across repair (sha1/256/512) | Header checksum_type == subject; footer validates | All 3 algos preserved + load-validate | PASS | `TestWriter_ChecksumTypePreserved` (subtests sha1/sha256/sha512). |
| TC-4 | checksum_type mismatch refused | Error before write; subject untouched | Unsupported type rejected; supported round-trip asserted | PASS | `TestNewFixMetaStore_ChecksumTypeAssertion`. |
| TC-5 | Legacy v2/v3 → v4 upgrade, checksum_type preserved | v4 output, checksum_type kept, paths/metadata at widened offsets | v3 golden upgrades to v4, 3 paths intact | PASS | `TestWriter_LegacyV3UpgradesToV4`. |
| TC-6 | Footer checksum validates after edit | Production loader accepts produced index | Folded into TC-3 (load-validation under preserved algo) | PASS | Validation load = footer-checksum verification. |
| TC-7 | Abort discards temp, subject untouched | No rename, temp removed, subject byte-identical | Preservation-failure abort removes `.fix.tmp`, subject (dir) untouched | PASS | `TestWriteRepairedIndex_AbortRemovesTemp`. |
| TC-8 | Forward-progress past mid-stream corrupt entry | Corrupt entry discarded+counted; flanking entries kept | survivors=[aaa.txt, zzz.txt], discarded=1, fixed=0 | PASS | `TestWorkflow_ForwardProgressPastCorruptEntry` — **added this phase** (exec deferred the fixture here). Null-out middle path → "path empty" reject; intact Size field → `trySkipToNextEntry` resyncs by size. |
| TC-9 | Promote targets subject, not main.idx; O_EXCL sibling | Rename lands on subject; `.pre-fix-*` preserved; main.idx untouched | Covered by promote integration matrix | PASS | `cmd/dcfhfix/promote_integration_test.go` (entryEdit/Append/Remove/headerEdit each preserve sibling on subject path) + `promote_test.go` (O_EXCL refuses symlink/dir, bound exhaustion). |

### Non-Functional Tests
- **Security (gosec floor)**: `golangci-lint run ./...` → **0 issues** after the errorlint fix (see Test Failures). gosec full ruleset active; migrated G304/G306 suppressions carry the "user-supplied subject path, no trust boundary at this layer" rationale (not a false `.dcfh` invariant).
- **Security (CWF changeset)**: see `## Security Review` — cap exceeded (relocation-dominated diff from the task baseline anchor); recorded as `error`, subagent not invoked per the exec-phase rule.
- **Reliability**: TC-6 / TC-7 are the data-integrity gates — produced index is always valid-or-absent, never partial. Both pass.
- **Performance**: no new latency target; existing benches not re-run (informational, not gating per the plan).
- **Usability**: dcfhfix subcommand output / exit codes / help unchanged — covered by `main_test.go` + `promote_integration_test.go` message-matrix cases (green in full suite).

## Test Failures

**errorlint regression surfaced by the full-run gate (found and fixed this phase).**

- **Symptom**: `golangci-lint run ./...` reported 11 `errorlint` findings (`non-wrapping format verb for fmt.Errorf`) — 10 in `pkg/fix_entry_workflow.go`, 1 in `pkg/fix_parse.go`.
- **Reproduction**: `golangci-lint run ./...` at the post-M2 HEAD (before the fix).
- **Root cause**: `.golangci.yml` has an intentional `errorlint` exclusion scoped to `path: cmd/dcfhfix/` (repair tool — `%v`, errors not wrapped). The M1 relocation moved these functions verbatim into `pkg/`, which is **not** excluded, so the carried-over `%v`-on-error patterns became visible. The pre-commit `--new` gate did not catch them (it flags changed lines, and the moved lines did not trip errorlint under that gate's behaviour); the stricter full `./...` run did. The full run was clean before 28.1 — the relocation introduced the regression.
- **Resolution**: wrapped the 11 error sites with `%w` (the `pkg` convention — proper error wrapping). Rejected the alternative of extending the `cmd/dcfhfix/` errorlint carve-out into `pkg/`: spreading a CLI-tool exemption into the library package is the wrong direction, and `%w` is strictly an improvement (enables `errors.Is`/`As` on the repair-tool error chain). `Fprintf` warning lines that use `%v` intentionally were left untouched (errorlint flags only `Errorf`).
- **Re-verification**: `golangci-lint run ./...` → 0 issues; `go test ./...` green (no error-string assertion perturbed by `%v`→`%w`).

No other failures.

## Coverage Report

- **Critical writer path (100% target)**: conversion (`beScanEntryFromValidated`), checksum-type seed+assert (`newFixMetaStore`), temp→promote, abort-discard — all exercised by TC-2…TC-7.
- **Forward-progress / resync**: `trySkipToNextEntry` clean-resync branch now exercised by TC-8 (was untested before this phase).
- **Promote / preserve / gate**: covered by the relocated `promote_test.go` + `promote_integration_test.go` (TC-9), unchanged by this task.
- **Regression**: full `go test ./...` green; pre-commit `-race` (`-d=checkptr=0`) green on the writer/workflow subset. No reduction in existing coverage; no re-export shims.

## Status
**Status**: Finished
**Next Action**: /cwf-retrospective 28.1 (chore pool is a,d,e,f,g,j — no rollout/maintenance)
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Security Review

**State**: error

error: cap exceeded: 1804 production lines > 500

The changeset (anchor=3a64a0e — the task baseline; includes uncommitted; 19
files, 3056 lines / 1804 production) exceeds the per-review production-line cap,
so per the testing-exec rule the automated `cwf-security-reviewer-changeset`
agent was NOT invoked. The count is dominated by the Milestone-1
behaviour-preserving relocation re-counted from the baseline anchor; the
genuinely-new testing-exec delta is small (the TC-8 test + the 11 `%v`→`%w`
one-token edits). No `warning:` line in helper stderr. The FR4 assessment of the
net-new Milestone-2 surface recorded in `f-implementation-exec.md` still holds;
this phase added no new untrusted-input, secret, auth, env-var, or path-handling
surface — the `%w` change only widens the repair-tool error chain (no new I/O,
no new external input).

## Actual Results
All planned test cases (TC-1…TC-9) pass. TC-8 was implemented this phase (the
exec phase deferred its fixture here). The full-run lint gate surfaced an
errorlint regression introduced by the M1 relocation (code moved out from under
the `cmd/dcfhfix/` errorlint carve-out); fixed by wrapping 11 error sites with
`%w`. `golangci-lint run ./...` is clean (0 issues) and the full suite is green.

## Lessons Learned
Run the full `golangci-lint run ./...` before declaring a relocation milestone done — the
`--new` staged gate misses findings that only appear once code leaves a path-scoped lint
carve-out. Captured in full in j-retrospective.md.
