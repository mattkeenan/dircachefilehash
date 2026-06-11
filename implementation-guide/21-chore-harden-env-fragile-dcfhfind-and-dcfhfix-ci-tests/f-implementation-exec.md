# Harden env-fragile dcfhfind and dcfhfix CI tests - Implementation Execution
**Task**: 21 (chore)

## Task Reference
- **Task ID**: internal-21
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/21-harden-env-fragile-dcfhfind-and-dcfhfix-ci-tests
- **Template Version**: 2.1

## Goal
Execute the implementation following the plan in d-implementation-plan.md and e-testing-plan.md.

## Execution Checklist
- [x] Read d-implementation-plan.md and e-testing-plan.md thoroughly
- [x] Verify all prerequisites met
- [x] Execute implementation steps sequentially
- [x] Update "Actual Results" for each step
- [x] Document any deviations from plan
- [x] Update status to "Implemented" when complete

## Actual Results

### Step 1: dcfhfind — skip on missing binary
- **Planned**: Convert the four `t.Fatal("dcfhfind executable not found"…)` binary-missing
  checks (lines 34, 171, 253, 328) to `t.Skip(…)`, unifying on the suffixed message.
- **Actual**: Done. Line 34 (`t.Fatal` → `t.Skip`, kept its existing suffixed message) via a
  unique-string edit; lines 171/253/328 (identical bare message) via `replace_all`. All four
  now read `t.Skip("dcfhfind executable not found. Run 'make build' first.")`. Verified with
  the binary moved aside: `TestPerformanceWarning` reports `--- SKIP` (was FAIL).
- **Deviations**: None.

### Step 2: dcfhfix — hermetic `.dcfh/` for the list case
- **Planned**: Add `os`/`path/filepath` imports; create `t.TempDir()` + `os.Mkdir(.../.dcfh, 0755)`;
  add per-case `indexFile`; route `handleFixesCommand(tt.indexFile, …)`; leave loop body intact.
- **Actual**: Done exactly as planned, mirroring `promote_integration_test.go:157-163`
  (`os.Mkdir`, bare `0755`). Error cases use `"nonexistent.idx"` (never dereferenced); the
  `list` case uses `filepath.Join(tmpDir, "nonexistent.idx")`. The two error-case assertions
  (`strings.Contains(err.Error(), errMsg)`) are unchanged. All three sub-cases PASS.
- **Deviations**: None.

### Step 3: Validate
- **Actual**:
  - `go test ./cmd/dcfhfix/ -run TestHandleFixesCommand -v` → all 3 sub-cases PASS
    (list emits "No backups found for nonexistent").
  - `TestPerformanceWarning` with `./dcfhfind` removed → `--- SKIP` (the exact CI condition).
  - `make test` → full suite green; git diff confined to the two `_test.go` files
    (`constants_version.go` regenerations are gitignored).

## Blockers Encountered
None.

## Deferral Check
- [x] All steps from d-implementation-plan.md executed
- [x] All success criteria from a-task-plan.md met
- [x] b-requirements-plan.md — N/A (chore; requirements phase skipped)
- [x] c-design-plan.md — N/A (chore; design phase skipped)
- [x] No planned work deferred

## Status
**Status**: Finished
**Next Action**: /cwf-testing-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
*To be filled upon completion*

## Lessons Learned
*To be captured during retrospective*

## Security Review

**State**: no findings

I have the full changeset and threat model. Let me reason through the five threat categories.

This changeset is entirely test-code and CWF process documentation. The diff touches three real files plus three new implementation-guide markdown docs:

- `cmd/dcfhfind/integration_test.go` — four `t.Fatal` → `t.Skip` conversions for the missing-binary guard.
- `cmd/dcfhfix/main_test.go` — adds `os`/`path/filepath` imports, a hermetic `t.TempDir()` with a `.dcfh/` subdir, a per-case `indexFile` field, and routes it through `handleFixesCommand(tt.indexFile, …)`.
- Three new `implementation-guide/21-…/*.md` plan/testing docs (process artefacts, no executable surface).

Reasoning through each category:

**(a) Bash injection / unsafe command construction.** No shell invocation anywhere. The Go changes call `os.Stat`, `os.Mkdir`, `filepath.Join`, and `t.Skip`/`t.Fatalf` — no `exec.Command`, no `system`, no string-built shell line. Nothing to interpolate. Clean.

**(b) Perl helpers consuming git/user output.** No Perl in the changeset; no git porcelain parsing. N/A.

**(c) Prompt injection via user-supplied strings.** The new markdown docs are CWF process artefacts, not SKILL templates with `{arguments}` substitution. Their content is descriptive prose authored by the workflow, not a conduit for untrusted free-text into LLM context. No new `{arguments}`-style surface. Clean.

**(d) Unsafe environment-variable handling.** No env-var reads introduced. `t.TempDir()` is the standard hermetic temp root (system `TMPDIR`-derived but managed and auto-cleaned by the test framework), and the `.dcfh` path is built with `filepath.Join` from that trusted root — no `..`, no user input, no `chmod`/`rm` of an env-derived path. Clean.

**(e) Pattern-based risks (safe-here-but-risky-elsewhere).** Two observations, both benign:

1. `os.Mkdir(filepath.Join(tmpDir, ".dcfh"), 0755)` — world-readable/traversable dir perms. Safe here: it is a `t.TempDir()`-scoped throwaway holding no secrets, and the plan deliberately mirrors the existing `promote_integration_test.go:157-163` precedent. The repo's gosec config keeps G301/G302/G306 active for production but scopes `_test.go` out via an `exclusions.rules` entry, so this is intentionally not a gate concern. Worth the standard framing: safe here because it is a test-only temp dir with no sensitive contents; audit future uses where a `0755` `os.Mkdir` is copied into a production code path that writes secret or integrity-tracked files (there the active G301 perms rule and the `.dcfh/` perm rationale would apply).

2. The `t.Fatal` → `t.Skip` conversions slightly weaken the safety net (a missing-binary regression now skips rather than fails), but this is a test-determinism trade-off the plan documents explicitly, not a security concern. No injection or data-exposure dimension.

No actionable security findings. The change narrows environment coupling (hermetic `.dcfh/` under a trusted temp root) rather than widening any attack surface.

```cwf-review
state: no findings
summary: Test-only + CWF doc changes; no shell/env/injection surface. The 0755 t.TempDir .dcfh mkdir is test-scoped (gosec _test.go-excluded) and mirrors existing precedent.
```
