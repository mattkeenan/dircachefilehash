# upgrade CWF to v1.1.169 - Testing Plan
**Task**: 5 (chore)

## Task Reference
- **Task ID**: internal-5
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/5-upgrade-cwf-to-v1-1-169
- **Template Version**: 2.1

## Goal
Verify the v1.1.155→v1.1.169 upgrade landed cleanly: correct version + tag-object SHA
recorded, integrity clean, all symlinks resolve, SubagentStop hook present and
executable, the workflow tooling still runs, and the revert path is available.

## Test Strategy
This is an operational upgrade with **no project source edits**, so there are no new
unit/integration tests to author. The strategy is **acceptance verification**: each
success criterion in `a-task-plan.md` maps to one concrete post-upgrade check, plus a
revert-path smoke test and a deterministic half-applied-state probe. CWF ships its own
Perl test suite, but that runs in the CWF source repo, not the consuming repo — out of
scope here.

### Test Levels
- **Unit**: N/A — no new code
- **Integration**: N/A — no new code
- **System / Acceptance**: TC-1..TC-8 below, run against the upgraded install
- **Regression**: re-run the helpers tasks 1–4 depend on; confirm no behavioural break

### Coverage Targets
- 100% of `a-task-plan.md` success criteria mapped to an executable check
- Both upgrade outcomes covered: success path (TC-1..6, TC-8) **and** failure/revert path (TC-7)
- Half-applied state detection covered (TC-8) — gap identified in robustness review of d-plan

## Test Cases
### Functional Test Cases
- **TC-1: Version + SHA recorded correctly**
  - **Given**: upgrade run via `cwf-manage update v1.1.169`
  - **When**: `cwf-manage status` and `cat .cwf/version`
  - **Then**: `Version: v1.1.169`; `cwf_sha` == `473baea2dd1d77bac9f100a1036f091eeccd0a4b` (the annotated-tag object returned by `git rev-parse v1.1.169`, **not** the dereferenced commit `0764380e60a6c1fb3788406942dfab7ae13bb585`); `cwf_ref` == `v1.1.169` (flipped from `HEAD`); `cwf_version` == `v1.1.169` (semver, possible because we pinned a tag — sidesteps v1.1.155's FR1 weakness)

- **TC-2: Integrity clean**
  - **Given**: completed laydown
  - **When**: `cwf-manage validate`
  - **Then**: exit 0, zero violations. The 10 pre-existing 0600→0444 perms drifts resolve as follows: 9 are corrected by laydown + exact-perms pass; 1 (`.cwf/templates/install/rules-inject.txt`) disappears from the report entirely because the file is removed at v1.1.169 (upstream T167). T164 may emit new hierarchy-consistency advisories — those are informational on historical content (BACKLOG/CHANGELOG/older tasks), blocking only on this task's directory. T165's template-ref linter excludes `implementation-guide/`. If only fixable perms remain, `cwf-manage fix-security` clears them and a re-run of validate is exit 0

- **TC-3: Skill symlinks resolve**
  - **Given**: laydown recreated `.claude/skills/cwf-*`
  - **When**: `for l in .claude/skills/cwf-*; do [ -e "$l" ] || echo "BROKEN: $l"; done`
  - **Then**: no `BROKEN:` output; every symlink points at an existing `.cwf-skills/cwf-*` target

- **TC-4: Agent defs resolve + Task 162 verdict-block present**
  - **Given**: agent laydown
  - **When**: inspect `.claude/agents/cwf-*` and `.cwf-agents/cwf-security-reviewer-changeset.md`
  - **Then**: all present/resolving; the changeset reviewer definition contains the trailing ` ```cwf-review ` verdict-block contract (confirms v1.1.162+ content, not stale v1.1.155)

- **TC-5: SubagentStop hook registered AND executable**
  - **Given**: settings-merge ran during update
  - **When**: inspect `.claude/settings.json` AND `test -x .cwf/scripts/hooks/subagentstop-security-verdict-guard`
  - **Then**: a `SubagentStop` hook entry referencing `subagentstop-security-verdict-guard` is present (Task 162); the helper file exists and is executable (registration alone does not prove the hook will fire); existing `Stop` hook and allowlist entries are intact (merge, not overwrite)

- **TC-6: Workflow tooling regression (tasks 1–4 dependencies)**
  - **Given**: upgraded install
  - **When**: `workflow-manager status`, `backlog-manager validate --all`, `task-context-inference`
  - **Then**: each exits 0. Rubric: non-zero exit = blocker; new informational warnings against historical content = expected (T164/T165 may add hierarchy or template-ref signal); new errors against `implementation-guide/5-chore-upgrade-cwf-to-v1-1-169/` = blocker. `workflow-manager status` still lists tasks 1–5; `task-context-inference` resolves task 5 / step `e-testing-plan` (or later); T166's subtask-aware inference is exercised as a regression check (top-level task here, but the upgraded resolver should still work)

- **TC-7: Revert path is clean (negative / safety)**
  - **Given**: the recorded pre-upgrade HEAD captured in f-implementation-exec.md Step 1 (the precondition gate from d-plan)
  - **When**: (only if the upgrade must be abandoned) `git reset --soft <pre-upgrade-HEAD> && git restore --staged . && git checkout -- .cwf .cwf-skills .cwf-rules .cwf-agents .claude && git checkout -- .cwf/version && git clean -fdx --dry-run -- .cwf .cwf-skills .cwf-rules .cwf-agents .claude/skills .claude/agents` (review output, then re-run without `--dry-run`); `rm .cwf/.update.lock` if present and no live process
  - **Then**: tree returns to v1.1.155 with the Task 5 planning commits (a/d/e checkpoints) intact; `cwf-manage status` reports v1.1.155; `git status --untracked-files=all` shows only the expected uncommitted f/g/j workflow templates. This is the documented escape hatch for any laydown abort

- **TC-8: Half-applied state probe (negative / safety)**
  - **Given**: an aborted laydown where the tree is at v1.1.169 but `.cwf/version` may still read v1.1.155
  - **When**: `grep ^cwf_version .cwf/version` AND `test -f .cwf/scripts/command-helpers/security-review-classify`
  - **Then**: a half-applied state is identified deterministically when version still reads `v1.1.155` AND a known-v1.1.169-only file (added by upstream T162) is present. The discriminative signal triggers the revert path (TC-7), not a fix-forward attempt — `fix-security` is additive-only and cannot complete the version pin

### Non-Functional Test Cases
- **Integrity/Security**: covered by TC-2 (sha256 + perms via `cwf-manage validate`), TC-5 (hook helper executable check), and the exec-phase changeset security review. T168's production-weighted cap may change the cap arithmetic; for a docs-only chore the review is likely `no findings` regardless
- **Reliability**: covered by TC-7 (atomic revert via soft-reset + checkout + clean) and TC-8 (deterministic half-applied detection; no fix-forward)
- **Performance/Usability**: N/A for a version bump

## Test Environment
### Setup Requirements
- CWF source clone at `/home/matt/repo/coding-with-files` with tag `v1.1.169` present (confirmed, fetched 2026-05-29)
- Clean working tree under `.cwf/`, `.cwf-skills/`, `.cwf-rules/`, `.cwf-agents/` before update (verified — the v1.1.163 laydown discard left these clean)
- Pre-upgrade repo HEAD recorded into f-implementation-exec.md before the update runs (TC-7 dependency; d-plan Step 1 precondition gate)
- No stale `.cwf/.update.lock` from prior runs (TC-7 dependency; d-plan Step 1 final check)

### Automation
- No CI integration; all checks are manual one-liners run in `g-testing-exec`
- No test doubles required — checks run against the real upgraded install

## Validation Criteria
- [ ] TC-1 — version `v1.1.169`, `cwf_sha` is the tag-object SHA `473baea2…`, `cwf_ref` flipped to `v1.1.169`
- [ ] TC-2 — `validate` exit 0; 9 perms drift fixed + 1 file removed; T164/T165 advisories informational only
- [ ] TC-3 — skill symlinks resolve
- [ ] TC-4 — agent defs resolve; T162 ` ```cwf-review ` verdict-block present
- [ ] TC-5 — SubagentStop hook registered AND helper executable; Stop hook intact
- [ ] TC-6 — workflow helpers exit 0 with rubric-correct outputs; tasks 1–5 still listed
- [ ] TC-7 — revert path verified available (executed only if upgrade abandoned)
- [ ] TC-8 — half-applied probe deterministic (executed only if a laydown aborts mid-flight)

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
*To be filled upon completion*

## Lessons Learned
*To be captured during implementation*
