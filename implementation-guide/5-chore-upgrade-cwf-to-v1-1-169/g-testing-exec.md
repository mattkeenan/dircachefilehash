# upgrade CWF to v1.1.169 - Testing Execution
**Task**: 5 (chore)

## Task Reference
- **Task ID**: internal-5
- **Branch**: chore/5-upgrade-cwf-to-v1-1-169
- **Template Version**: 2.1

## Goal
Execute the test cases from `e-testing-plan.md` against the v1.1.169 install and record
PASS/FAIL with evidence.

## Status
**Status**: Finished
**Next Action**: /cwf-rollout
**Blockers**: None

## Test Strategy Recap
Operational chore — no project source edits, no new unit/integration tests. Strategy is
acceptance verification: each success criterion from `a-task-plan.md` maps to one
post-upgrade check (TC-1..TC-6), plus a revert-path smoke (TC-7) and a half-applied
probe (TC-8). TC-7 and TC-8 are negative-path tests and are not exercised on a clean
success — they are verified as *available* (components in place, behaviour predictable),
not *triggered*.

## Functional Test Results

### TC-1: Version + SHA recorded correctly — PASS
**Command**: `.cwf/scripts/cwf-manage status` + `cat .cwf/version`
**Expected**: `Version: v1.1.169`; `cwf_sha == 473baea2dd1d77bac9f100a1036f091eeccd0a4b` (tag-object SHA); `cwf_ref == v1.1.169`; `cwf_version == v1.1.169` (semver)
**Actual**:
```
Version:  v1.1.169
Ref:      v1.1.169
SHA:      473baea2dd1d77bac9f100a1036f091eeccd0a4b
cwf_version=v1.1.169
cwf_ref=v1.1.169
cwf_sha=473baea2dd1d77bac9f100a1036f091eeccd0a4b
cwf_install_manifest_sha=e1926a2f6fc5982c6a614e581546978185f6b175f8c43e7c5284328638855cac   # new field, v1.1.169 only
```
All four match. Tag-object SHA expectation (rev-parse, not rev-list) confirmed — the v1.1.163 attempt's TC-1 inversion is corrected.

### TC-2: Integrity clean — PASS
**Command**: `.cwf/scripts/cwf-manage validate`
**Expected**: exit 0
**Actual**: `[CWF] validate: OK` exit 0
The 10 pre-existing 0600→0444 perms drifts resolved: 9 chmod'd by the laydown's exact-perms pass; 1 (`.cwf/templates/install/rules-inject.txt`) disappeared from the report because the file was removed at v1.1.169 (upstream T167) — as predicted in e-plan TC-2.

### TC-3: Skill symlinks resolve — PASS
**Command**: `for l in .claude/skills/cwf-*; do [ -e "$l" ] || echo "BROKEN: $l"; done`
**Expected**: no BROKEN: output
**Actual**: 0 BROKEN out of 59 skill symlinks; every link resolves to an existing `.cwf-skills/cwf-*/SKILL.md` target.

### TC-4: Agent defs resolve + cwf-review verdict-block present — PASS
**Command**: inspect `.claude/agents/cwf-*` and `.cwf-agents/cwf-security-reviewer-changeset.md`
**Expected**: all present; ` ```cwf-review ` block present (T162 contract)
**Actual**: 5 agent defs (`cwf-plan-reviewer-{improvements,misalignment,robustness,security}` + `cwf-security-reviewer-changeset`), all resolve. `grep -c '```cwf-review'` returns 1 in both `.cwf-agents/` and `.claude/agents/` copies — verdict-block intact, confirming v1.1.162+ content.

### TC-5: SubagentStop hook registered AND helper executable — PASS
**Command**: parse `.claude/settings.json`; `test -x .cwf/scripts/hooks/subagentstop-security-verdict-guard`
**Expected**: SubagentStop entry referencing `subagentstop-security-verdict-guard`; helper executable; Stop entry intact
**Actual**:
- 1 SubagentStop entry; matcher `cwf-security-reviewer-changeset`, command `.cwf/scripts/hooks/subagentstop-security-verdict-guard`, timeout 5
- 1 Stop entry intact (merge, not overwrite)
- Helper file present and `-x` true
The robustness-review-driven addition (verify executable, not just registered) is met.

### TC-6: Workflow tooling regression — PASS
**Command**: three helpers
**Expected**: each exits 0; tasks 1–5 listed; T166's subtask-aware inference exercised
**Actual**:
- `workflow-manager status` exit 0; lists tasks 1 (100%), 2 (100%), 3 (100%), 4 (100%), 5 (25%) — full visibility
- `backlog-manager validate --all` exit 0; no warnings
- `task-context-inference` exit 0; `current: conclusive, confidence: correlated, task_num: 5, task_slug: upgrade-cwf-to-v1-1-169, workflow_step: f-implementation-exec`

**Notable**: on the same branch state under v1.1.155 earlier today, `task-context-inference` returned `inconclusive, uncorrelated, task_nums: 2,5`. Under v1.1.169 (Task 166 fix landed) it resolves task 5 conclusively. This is a **regression test that v1.1.169 fixes a real prior defect** — the upgrade is net-positive for this consumer, not just a version pin.

### TC-7: Revert path is clean (negative / safety) — NOT EXECUTED (verifiable)
**Status**: not exercised — upgrade did not abort, so revert was not needed.
**Verifiability evidence**:
- Pre-upgrade HEAD `3ecf3b86f7cd76498da68a039ef059b4f7394693` recorded in `f-implementation-exec.md` Step 1
- Reflog preserves all post-laydown commits — `git reflog -5` shows `bd0abfc`, `e267935`, plus prior reset moves
- Revert recipe `git reset --soft 3ecf3b8 && git restore --staged . && git checkout -- .cwf .cwf-skills .cwf-rules .cwf-agents .claude && git checkout -- .cwf/version && git clean -fdx --dry-run -- .cwf .cwf-skills .cwf-rules .cwf-agents .claude/skills .claude/agents` (then non-dry) is the documented escape hatch from d-plan Step 2; no missing components

### TC-8: Half-applied state probe (negative / safety) — NOT EXECUTED (verifiable)
**Status**: not exercised — laydown completed cleanly; no probe needed.
**Verifiability evidence**:
- `.cwf/scripts/command-helpers/security-review-classify` (v1.1.169-only sentinel file, added by T162) is **present** on disk — would be the "v1.1.169 marker" half of the probe
- `.cwf/version` reads `cwf_version=v1.1.169` — the other half of the probe; on a half-applied abort it would still read `v1.1.155` while the marker file would already exist, giving an unambiguous classification
- Discriminative signal works as designed; no half-applied state currently exists

## Non-Functional Test Results
- **Integrity/Security**: TC-2 (validate exit 0) and TC-5 (hook helper executable) covered the deterministic security checks. Exec-phase changeset security review state recorded in `f-implementation-exec.md` (`error: cap exceeded`, surfaced for user decision per T168 contract).
- **Reliability**: TC-7 and TC-8 negative-path components verified present without triggering. Revert path is available; half-applied probe is unambiguous.
- **Performance/Usability**: N/A.

## Validation Criteria
- [x] TC-1 — version `v1.1.169`, `cwf_sha` is tag-object SHA `473baea2…`, `cwf_ref` flipped to `v1.1.169`
- [x] TC-2 — `validate` exit 0; 9 perms drift fixed + 1 file removed
- [x] TC-3 — 59 skill symlinks resolve (0 broken)
- [x] TC-4 — 5 agent defs resolve; T162 ` ```cwf-review ` verdict-block present in both copies
- [x] TC-5 — SubagentStop hook registered AND helper executable; Stop hook intact
- [x] TC-6 — workflow helpers exit 0; tasks 1–5 listed; **T166 fix observable (inconclusive→conclusive)**
- [-] TC-7 — revert path verified available (not executed — success path)
- [-] TC-8 — half-applied probe verified deterministic (not executed — success path)

## Test Coverage
- 6/6 success-path TCs executed and PASS
- 2/2 negative-path TCs verified available (components present, behaviour predictable) without triggering
- 100% of `a-task-plan.md` success criteria covered by an executable or verifiable check

## Lessons Learned
- The plan-review additions (TC-5 hook-executable check, TC-8 deterministic probe, TC-6 pass/fail rubric) were polish but provide measurable confidence on the success path and would have made the v1.1.163 abort recoverable on first contact. They didn't fire this time; the next consumer upgrade gets them for free.
- T166 (subtask-aware context inference) failure was *observable* on this branch under v1.1.155 and *fixed* under v1.1.169. The upgrade delivers a real consumer-side improvement, not just a version pin.

## Security Review

**State**: error

error: cap exceeded: 1632 production lines > 500

Helper output: `reviewed 19 files, 2252 lines (1632 production), anchor=07366ad`. Identical to the f-exec changeset (anchor and file set unchanged between f and g phases since no testing-only files were added). Same rationale: the anchor at the baseline `07366ad` includes the entire v1.1.169 laydown in the diff, and with no `security.review.test-paths` patterns configured every line counts as production.

Per skill contract, the subagent is **not** invoked when exit=2. The new-in-this-task surface (`.claude/settings.json` settings-merge, `.cwf/version`, workflow MD files) is unchanged from f-exec — already surfaced there for user decision. No additional security-relevant content was added in the testing phase (no source code changes, only PASS/FAIL recordings in `g-testing-exec.md`).

