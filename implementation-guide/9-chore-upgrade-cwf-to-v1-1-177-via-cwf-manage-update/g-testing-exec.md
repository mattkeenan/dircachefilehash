# Upgrade CWF to v1.1.177 via cwf-manage update - Testing Execution
**Task**: 9 (chore)

## Task Reference
- **Task ID**: internal-9
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/9-upgrade-cwf-to-v1-1-177-via-cwf-manage-update
- **Template Version**: 2.1

## Goal
Execute the TC-1..8 acceptance cases from e-testing-plan.md against the upgraded
install and record results.

## Execution Checklist
- [x] Read e-testing-plan.md and d-implementation-plan.md thoroughly
- [x] Verify test environment ready (upgrade landed in f-phase, commit `dba0116`)
- [x] Execute test cases sequentially
- [x] Record pass/fail for each test
- [x] Document failures with reproduction steps (none)
- [x] Update status to "Finished" when all pass

## Test Results

### Functional Tests

| Test ID | Test Case | Expected | Actual | Status |
|---------|-----------|----------|--------|--------|
| TC-1 | Version + SHA recorded (T175 forward-only) | `v1.1.177`; `cwf_sha`=tag-object `1cae055…` (NOT commit `ed664b25…`); `cwf_ref`=`v1.1.177` | `Version/Ref v1.1.177`; `cwf_sha=1cae055bf1b52bea0fd9b0cfce63871893757ab7`; `cwf_ref=v1.1.177`; `cwf_version=v1.1.177` | **PASS** |
| TC-2 | Integrity clean under T170 perms-ceiling | `validate` exit 0, zero violations | `[CWF] validate: OK`, exit 0 — no `fix-security` pass needed (laydown's exact-perms pass left tree at ceiling) | **PASS** |
| TC-3 | Skill symlinks resolve | no `BROKEN:` output | 20 skill symlinks checked, 0 broken | **PASS** |
| TC-4 | Agent defs resolve; verdict-block contract intact | all resolve; `cwf-review` block present | 5 agent symlinks, 0 broken; `cwf-review` contract present in `cwf-security-reviewer-changeset.md` | **PASS** |
| TC-5 | T176 doc-split present AND readable | `workflow-steps/` dir with non-empty per-phase files; `workflow-steps.md` is ToC | dir present, 10 per-phase files (`planning.md`, `implementation-planning.md`, `testing-planning.md`, `design.md`, `requirements.md`, `implementation-execution.md`, `testing-execution.md`, `rollout.md`, `maintenance.md`, `retrospective.md`), representative files non-empty; `workflow-steps.md` retains ToC | **PASS** |
| TC-6 | Workflow tooling regression (tasks 1–8 deps) | each exits 0; task 9 resolves | `task-context-inference` (resolves task 9 / `f-implementation-exec`, exercises T171), `context-manager hierarchy 9`, `workflow-manager status 9 --workflow`, `backlog-manager validate` — all exit 0 | **PASS** |
| TC-7 | Revert path clean (negative/safety) | revert available if upgrade abandoned | **N/A — not triggered.** Upgrade succeeded (no abort, no resolve prompt, validate clean). Revert procedure (soft-reset + targeted checkout + dry-run-then-confirm `git clean`) is documented and available in d-plan Step 2; not exercised because there was nothing to revert | **N/A** |
| TC-8 | Half-applied state probe (negative/safety) | deterministic half-applied detection if a laydown aborts | **N/A — not triggered.** No abort occurred; the discriminating marker `test -d .cwf/docs/workflow/workflow-steps` is present *with* `cwf_version=v1.1.177` (the fully-applied state, not the half-applied `v1.1.169 + marker` signature). Probe logic documented in e-plan/d-plan Step 2 | **N/A** |

### Non-Functional Tests
- **Integrity/Security**: TC-2 covers sha256 + T170 perms-ceiling via `cwf-manage
  validate` (exit 0). The exec-phase changeset security review is recorded under
  `## Security Review` below (cap-exceeded → `error`, by design for a CWF
  subtree upgrade; see f-implementation-exec.md for the same rationale).
- **Reliability**: TC-7/TC-8 confirm the atomic-revert and half-applied-detection
  procedures exist and are deterministic; neither needed to fire.
- **Performance/Usability**: N/A for a version bump.

## Test Failures
None. All functional cases PASS; both negative/safety cases N/A (not triggered
because the upgrade applied cleanly).

## Coverage Report
- 6/6 functional acceptance cases executed and PASS.
- 2/2 negative/safety cases evaluated; both correctly N/A (no failure to recover
  from). Their detection/recovery logic is documented for future aborts.
- 100% of a-task-plan.md success criteria mapped and met:
  version+tag-object SHA (TC-1), validate clean (TC-2), T176 split present (TC-5),
  workflow smoke check (TC-6), project code untouched (verified in f-phase:
  `git diff --stat cc3ee2c..HEAD -- pkg cmd go.mod go.sum` empty).

## Security Review

**State**: error

error: cap exceeded: 1906 production lines > 500

(Per `cwf-testing-exec` Step 8: the `security-review-changeset --phase=testing`
helper exited `2` — production-weighted count exceeded `--max-lines=500`, so the
subagent is **not** invoked and the state is `error`. Helper summary: `reviewed
44 files, 2621 lines (1906 production), anchor=65fd214`. No `warning:` lines
emitted. Same situation as the implementation phase: the "production" lines are
the v1.1.169→v1.1.177 CWF source delta plus the task-9 workflow docs — no
project source in the changeset, and this repo declares no
`security.review.max-lines-exclude-paths` so the vendored subtree is not
discounted. The integrity gate `cwf-manage validate` passed exit 0 (TC-2).)

## Status
**Status**: Finished
**Next Action**: /cwf-rollout
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Lessons Learned
*To be captured during retrospective*
