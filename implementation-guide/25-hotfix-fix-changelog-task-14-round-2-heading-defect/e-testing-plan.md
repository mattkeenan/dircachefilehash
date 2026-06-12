# Fix CHANGELOG Task 14 round 2 heading defect - Testing Plan
**Task**: 25 (hotfix)

## Task Reference
- **Task ID**: internal-25
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: hotfix/25-fix-changelog-task-14-round-2-heading-defect
- **Template Version**: 2.1

## Goal
Define the acceptance checks that confirm the CHANGELOG round-2 heading defect is fixed and nothing else regressed.

## Test Strategy
### Test Levels
- **Acceptance (primary)**: `backlog-manager validate --all` is the authoritative format gate — it *is* the contract this task exists to satisfy.
- **Scope/regression**: `git diff` inspection to prove the edit is confined to the two affected entries and no Go/test/config file changed.
- **Not applicable**: Unit, integration, system, performance, security test code — this is a documentation-only Markdown change with no executable surface.

### Test Coverage Targets
- **Critical path**: 100% — the single defect (round-2 heading not parsing → duplicate `Changes` under Task 15) must be eliminated.
- **Regression**: the full `validate --all` run covers every other BACKLOG/CHANGELOG entry, so any collateral format breakage is caught.

## Test Cases
### Functional Test Cases
- **TC-1: Validator passes after fix (acceptance gate)**
  - **Given**: the three edits from d-implementation-plan applied to `CHANGELOG.md`.
  - **When**: `.cwf/scripts/command-helpers/backlog-manager validate --all` is run from the repo root.
  - **Then**: exit code 0, no `[CWF] ERROR` or `[CWF] WARN` lines.

- **TC-2: Red state reproduced before fix**
  - **Given**: `CHANGELOG.md` at baseline `7dbced1a` (pre-edit).
  - **When**: `validate --all` is run.
  - **Then**: exit 1 with `CHANGELOG.md:198 [CHANGELOG-003] subsections out of order`. (Establishes the gate distinguishes fail from pass.)

- **TC-3: Round-2 heading parses as a Task 14 entry**
  - **Given**: the fixed `CHANGELOG.md`.
  - **When**: the heading is inspected.
  - **Then**: it reads `## Task 14: Upgrade CWF subtree to v1.1.185 (round 2)` — colon immediately after `14`, matching `Backlog.pm:231`.

- **TC-4: Round-2 entry carries exactly one Status + one Impact, before Changes**
  - **Given**: the fixed `CHANGELOG.md`.
  - **When**: the renamed round-2 block is inspected.
  - **Then**: exactly one `### Status:` and one `### Impact:` (the v1.1.185 text), both before `### Changes`.

- **TC-5: Task 15 retains exactly its own single Status/Impact pair**
  - **Given**: the fixed `CHANGELOG.md`.
  - **When**: the `## Task 15` block head is inspected.
  - **Then**: exactly one `### Status:`/`### Impact:` pair (the TUI-colour text); the two stray v1.1.185 duplicate pairs are gone.

- **TC-6: Change scope confined**
  - **Given**: the fixed working tree.
  - **When**: `git diff CHANGELOG.md` and `git status --porcelain` are inspected.
  - **Then**: diff touches only the Task 15 head (−4 lines), the round-2 heading rename (±1), and the round-2 Status/Impact insert (+2); no other entry's prose changes; no non-CHANGELOG file is modified (the wf files are committed separately).

### Non-Functional Test Cases
- None applicable (documentation-only change; no runtime, no auth, no performance surface).

## Test Environment
### Setup Requirements
- Repo at baseline `7dbced1a` or later on the task branch.
- `perl` on PATH (already required by CWF helpers); no other tooling.
- Planning-phase verification used `/tmp` copies driven through `parse_changelog_tree`/`validate_changelog_tree` (`.cwf/lib/CWF/Backlog.pm`) — g-testing-exec runs the real `backlog-manager validate --all` against the edited repo file.

### Automation
- `backlog-manager validate` already runs inside the CWF checkpoint commit (`cwf-manage validate`) and the repo format gate, so the fix is continuously re-checked after landing. No new automation added.

## Validation Criteria
- [ ] TC-1 passes: `validate --all` exits 0, clean.
- [ ] TC-2 confirms the pre-fix red state (documented, not a gate on the final tree).
- [ ] TC-3–TC-5 structural assertions hold.
- [ ] TC-6: diff confined to the two entries; no code/config/test files touched.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All six planned test cases executed and PASS (see g-testing-exec.md). TC-1 (validate green) and TC-2 (baseline red via Perl harness) bracketed the fix; TC-3…TC-6 verified structure and scope.

## Lessons Learned
Pairing a forward acceptance gate (TC-1) with an independent baseline-red reproduction (TC-2) is a strong pattern for a format fix — it proves both that the fix works and that the gate would have caught the original defect. See j-retrospective.md.
