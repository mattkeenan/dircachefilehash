# Upgrade CwF to v1.1.185 - Testing Plan
**Task**: 14 (chore)

## Task Reference
- **Task ID**: internal-14
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/14-upgrade-cwf-to-v1-1-185
- **Template Version**: 2.1

## Goal
Validate that the 183→185 upgrade landed merge-free, migrated `cwf_method` to
`read-tree`, pinned the correct version, and left the tree validate-clean and the
tooling functional.

## Test Strategy
### Test Levels
This task writes **no Go code**, so there are **no unit tests**. Validation is at
the **system / acceptance** level: command-output assertions against the
post-upgrade repo state, executed in `g-testing-exec.md`. Each TC maps to an
acceptance criterion from `b-requirements-plan.md`.

### Coverage Target
100% of the 5 ACs covered by an executable assertion. No partial passes — every
TC is a deterministic check with a single expected result.

## Test Cases
### Functional Test Cases
- **TC-1 (AC1 / FR1) — version pinned**:
  - **Given**: the upgrade has run.
  - **When**: `grep -E 'cwf_(version|ref|sha)=' .cwf/version`.
  - **Then**: `cwf_version=v1.1.185`, `cwf_ref=v1.1.185`,
    `cwf_sha=6659c1cca72ef033d92546fcd9d42a0f4d817dd9` (commit-object form; the
    annotated-tag object `dd6e934c…` must NOT appear).

- **TC-2 (AC3 / FR3) — method migrated to read-tree**:
  - **Given**: pre-upgrade `cwf_method=subtree`.
  - **When**: `grep cwf_method= .cwf/version`.
  - **Then**: `cwf_method=read-tree` (not `subtree`, not `copy`).

- **TC-3 (AC2 / FR2) — merge-free**:
  - **Given**: the consumer commit (`f`) is made.
  - **When**: `git log --merges 700baba..HEAD` and
    `git cat-file -p HEAD | grep -c '^parent'`.
  - **Then**: the merge log is **empty** and the parent count is **1**
    (single-parent). The upgrade added no merge commit.

- **TC-4 (AC3 / FR4) — validate-clean**:
  - **Given**: the post-laydown perms clamp (`apply_exact_perms_or_die`) ran.
  - **When**: `.cwf/scripts/cwf-manage validate`.
  - **Then**: exit 0, prints `validate: OK`. (A non-OK result is a fail-closed
    stop — the upgrade is not committed.)

- **TC-5 (AC4 / FR5) — tooling functional**:
  - **Given**: 185 is installed.
  - **When**: `.cwf/scripts/command-helpers/workflow-manager status 14`.
  - **Then**: exits 0 and reports task-14 phase status (helpers/skills not broken
    by the laydown).

- **TC-6 (AC5 / FR6) — settings change observed & reviewed**:
  - **Given**: `settings.before.json` snapshot taken pre-upgrade.
  - **When**: `diff settings.before.json .claude/settings.json`.
  - **Then**: the delta is captured in `f`, with an **explicit note** stating
    whether any hooks-list entry or Bash allowlist entry was added/widened
    (a widening must be called out, not silently filed).

### Non-Functional / Edge Test Cases
- **TC-7 (NFR5 — fail-closed on driver failure)**: If the driver `update` exits
  non-zero (simulated reasoning, not forced), the expectation is `.cwf/version`
  still reads `cwf_method=subtree` and nothing is staged/committed. Verified by
  inspection of the abort path, not by deliberately breaking the run.
- **TC-8 (detect-merges advisory)**: The driver's `run_detect_merges` is expected
  to flag the **4 pre-existing** subtree-install merges
  (`75e3ae4`/`a2c7635`/`28cfb50`/`103537c`). **Expected/advisory** — its presence
  is recorded, not treated as a task failure.
- **TC-9 (regression — dcfh untouched)**: No Go files changed
  (`git diff --name-only 700baba..HEAD -- '*.go'` is empty), so `dcfh` behaviour
  is unaffected; no `go test` run is required by this task.

## Test Environment
- This repo on branch `chore/14-upgrade-cwf-to-v1-1-185` (baseline `700baba`).
- CWF source `file:///home/matt/repo/coding-with-files` with tag `v1.1.185`.
- Throwaway 185 driver checkout at `/tmp/cwf-185-driver`.
- No test database / external services involved (no `dcfh` data operations).

## Validation Criteria
- [ ] TC-1..TC-6 all PASS (the 5 ACs).
- [ ] TC-7 fail-closed behaviour confirmed by abort-path inspection.
- [ ] TC-8 detect-merges advisory recorded (not a failure).
- [ ] TC-9 no Go diff (regression-free by construction).

## Decomposition Check
- [x] Time / People / Complexity / Risk / Independence — all No.

**Decision**: No decomposition — 0 signals triggered.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-exec 14
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All 9 test cases executed in g-testing-exec.md: TC-1..TC-6 PASS (the 5 ACs),
TC-7 fail-closed confirmed by abort-path inspection, TC-8 detect-merges advisory
recorded (4 pre-existing subtree merges), TC-9 regression-free (no Go diff).

## Lessons Learned
The command-output-assertion strategy (no Go unit tests for a doc/tooling chore) was
the right fit; every TC was a deterministic single-result check, so g-exec was a
fast mechanical pass with zero ambiguity.
