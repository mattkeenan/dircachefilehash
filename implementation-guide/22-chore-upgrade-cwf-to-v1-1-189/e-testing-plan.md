# upgrade CwF to v1.1.189 - Testing Plan
**Task**: 22 (chore)

## Task Reference
- **Task ID**: internal-22
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/22-upgrade-cwf-to-v1-1-189
- **Template Version**: 2.1

## Goal
Verify the v1.1.189 laydown landed correctly: version file pinned, history
linear, integrity validates, config reconciled, and workflow tooling still
functions. No new product code is added, so "testing" here is outcome
verification of the upgrade, not new unit tests.

## Test Strategy
### Test Levels
- **Integrity/config verification**: assert post-laydown state of `.cwf/version`,
  `cwf-project.json`, and `cwf-manage validate`.
- **History verification**: assert linear history (no merge commit).
- **Tooling smoke test**: confirm a CWF skill/helper still runs after laydown.
- **Regression**: confirm the Go suite is unaffected (sanity — the upgrade
  touches only `.cwf`/docs/artefacts, no Go sources).

### Test Coverage Targets
- **Critical paths**: 100% of the five success criteria in a-task-plan.md
  are asserted by a test case below.
- **Regression**: existing Go tests still pass (no incidental breakage).

## Test Cases
### Functional Test Cases

- **TC-1: Version file pinned to v1.1.189**
  - **Given**: `cwf-manage update v1.1.189` has run.
  - **When**: `cat .cwf/version`.
  - **Then**: `cwf_version=v1.1.189`, `cwf_ref=v1.1.189`,
    `cwf_sha=6af636e32ad1ffaebd2601c7101dd46c8a3c30b7`, `cwf_method=read-tree`.

- **TC-2: No merge commit (linear history)**
  - **Given**: laydown + checkpoint commits done.
  - **When**: `git log --merges 82ff991..HEAD`.
  - **Then**: output is empty.

- **TC-3: Integrity validates clean**
  - **Given**: post-laydown tree.
  - **When**: `.cwf/scripts/cwf-manage validate`.
  - **Then**: exits 0 (`validate: OK`). A permission violation is fixed on
    sight via `cwf-manage fix-security`; a sha256 violation is a blocker.

- **TC-4: cwf-project.json reconciled (task-188)**
  - **Given**: Edit A + Edit B applied.
  - **When**: inspect `implementation-guide/cwf-project.json`.
  - **Then**: top-level `version` and `_version-note` are absent;
    `cwf-version`, `_cwf-version-note`, and the `versioning` block
    (`last_released=v0.13.21`, `major_minor=v0.13`) are present and unchanged;
    file parses as valid JSON.

- **TC-5: Workflow tooling functional post-upgrade**
  - **Given**: v1.1.189 laid down.
  - **When**: run `cwf-status` (or a `workflow-manager control` call) for task 22.
  - **Then**: command succeeds and reports the task hierarchy without error.

### Non-Functional Test Cases
- **Regression**: `go test ./pkg/...` passes (no Go sources changed; this is a
  sanity guard, not expected to be impacted).
- **Reliability**: a failed/partial laydown is fail-closed (cwf_method stays
  `read-tree`, version unchanged) — not separately exercised; relies on the
  tool's tested behaviour.

## Test Environment
### Setup Requirements
- Branch `chore/22-upgrade-cwf-to-v1-1-189`, baseline `82ff991`.
- CWF source `file:///home/matt/repo/coding-with-files` with tag `v1.1.189`.
- `CWF_SOURCE` unset (see d-plan Step 1).

### Automation
- No new automated tests. Verification is manual command execution recorded in
  g-testing-exec.md. The repo's pre-commit gate (`cwf-manage validate`) provides
  continuous integrity checking on each checkpoint commit.

## Validation Criteria
- [ ] TC-1 through TC-5 pass.
- [ ] `go test ./pkg/...` passes (regression sanity).
- [ ] All five a-task-plan.md success criteria satisfied.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-exec 22
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
TC-1 through TC-5 all PASS; Go regression green. See g-testing-exec.md for the
results table. One test-command usage correction (TC-5 `--task-path` → positional
`TASK_PATH`); no product defect.

## Lessons Learned
Test cases exercising a CLI helper should pin the exact invocation form (flags vs
positional), especially across a tool-version bump. See j-retrospective.md
§ Recommendations.
