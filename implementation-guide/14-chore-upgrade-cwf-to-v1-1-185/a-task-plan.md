# Upgrade CwF to v1.1.185 - Plan
**Task**: 14 (chore)

## Task Reference
- **Task ID**: internal-14
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/14-upgrade-cwf-to-v1-1-185
- **Baseline Commit**: 700babae5d6f283f0cbbee700208bbea31b90443
- **Template Version**: 2.1

## Goal
Upgrade the installed CWF subtree from v1.1.183 to v1.1.185 — the release
that replaces `git subtree` with a merge-free `read-tree` laydown — so the
upgrade itself lands **without a merge commit**, leaving the tree
validate-clean and the workflow tooling functional.

## Context
The v1.1.183 upgrade landed at `700baba` (task-14, first round) and stays in
history. This round layers the 183→185 upgrade on top, as a second commit on
the linear local-main line. v1.1.185 ships *"Task 185: Replace git-subtree
with merge-free read-tree laydown"* — the root-cause fix for the
merge-commit bug recorded against this repo (see
`project_cwf_subtree_merge_commits` memory). Because the installed 183
`cmd_update` **delegates laydown to the target ref's `install.bash`**
(`.cwf/scripts/cwf-manage:474-499`), the 185 read-tree installer governs this
upgrade — so the fix is expected to take effect on the very upgrade that
delivers it.

## Success Criteria
- [ ] `.cwf/version` records `cwf_version=v1.1.185`, `cwf_ref=v1.1.185`, and
      `cwf_sha` = the **commit-object SHA** of v1.1.185
      (`6659c1cca72ef033d92546fcd9d42a0f4d817dd9`), consistent with the
      commit-form recorded for the 183 upgrade (`resolve_sha` uses `$ref^{commit}`).
- [ ] The upgrade laydown introduces **no merge commit** on the task branch
      (`git log --merges 700baba..HEAD` is empty) — the headline win.
- [ ] `.cwf/scripts/cwf-manage validate` exits 0 (config + workflow-file +
      script-hash + perms integrity all pass post-laydown).
- [ ] Post-upgrade `cwf_method` recorded in `.cwf/version` is documented and
      consistent with the laydown actually used (the 185 migration may move
      `subtree` → `copy`/read-tree; design phase confirms).
- [ ] Workflow tooling remains functional after the upgrade (a helper/skill
      invocation succeeds; e.g. `cwf-manage validate` and a `workflow-manager`
      call).

## Original Estimate
**Effort**: ~0.5 day
**Complexity**: Medium (the laydown-method interaction with the linear-history
rule is the non-trivial part; the version bump itself is mechanical)
**Dependencies**: CWF source repo `file:///home/matt/repo/coding-with-files`
with tag `v1.1.185` present (verified); baseline `700baba` (183 landed).

## Major Milestones
1. **Requirements (b)**: Pin the functional contract — version-file fields,
   no-merge-commit invariant, validate-clean, method-migration handling,
   settings-merge expectations.
2. **Design (c)**: Determine the exact 185 laydown path under
   `CWF_METHOD=subtree` (read-tree vs migration to `copy`), how merge-freeness
   is achieved, and the linear-landing strategy (incl. fallback if any
   intermediate merge appears).
3. **Implementation plan (d)** + **Testing plan (e)**.
4. **Exec (f/g)**: Run `cwf-manage update v1.1.185`, verify no merge commit,
   validate clean, record deviations; run the test cases.
5. **Land linearly**: ff-only onto local-main — a second CWF-upgrade commit on
   top of `700baba`.

## Risk Assessment
### High Priority Risks
- **R1 — 185 installer still emits a merge under `CWF_METHOD=subtree`**:
  If the target installer's `subtree` path has not fully migrated to read-tree,
  the upgrade could still create a merge commit, violating the linear-history
  rule ([[feedback_never_merge_commits]]).
  - **Mitigation**: Design phase reads 185's `install.bash`/`cwf-manage` to
    confirm the laydown. The retrospective squash (soft-reset to baseline
    `700baba` → single commit) flattens any intermediate merge regardless, and
    landing is ff-only — so a merge can never reach local-main. Primary aim is
    to confirm the read-tree path works as intended.

### Medium Priority Risks
- **R2 — settings.json merge injects unexpected hooks** (as the 183 upgrade
  did with two PreToolUse hooks):
  - **Mitigation**: Review the merged `.claude/settings.json`, record any
    injected hooks as deviations; no surprise to the active workflow.
- **R3 — `cwf_method` migration changes future-upgrade behaviour** (subtree →
  copy):
  - **Mitigation**: Verify `.cwf/version` post-upgrade; document the new method
    and its implication for the next upgrade.
- **R4 — script-hash / perms drift post-laydown**:
  - **Mitigation**: `cwf-manage validate` is the gate; fix any drift in-task.
- **R5 — overlap with the parked merge-blocking githooks** (`stash@{0}`) and
  185's own `cwf-detect-merges` helper:
  - **Mitigation**: The parked hooks are not applied (stashed), so no
    interference this task; note the `cwf-detect-merges` overlap for the
    deferred `.githooks` chore.

## Dependencies
- CWF source `file:///home/matt/repo/coding-with-files`, tag `v1.1.185`
  (commit `6659c1c`) — verified present.
- Baseline `700baba` (the landed 183 upgrade) on the linear local-main line.

## Constraints
- **Linear history**: land ff-only; no merge commits ever
  ([[feedback_never_merge_commits]]).
- Never bypass commit hooks (`--no-verify`); never `git reset --hard`.
- `cwf_sha` commit-form consistency with the 183 record.

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [x] **Time**: Will this take >1 week? No — ~0.5 day.
- [x] **People**: Does this need >2 people? No — single operator.
- [x] **Complexity**: 3+ distinct concerns? No — one upgrade, one invariant to
      verify (no-merge), one validate gate.
- [x] **Risk**: High-risk components needing isolation? No — reversible
      (rollback / re-install), fully gated by validate.
- [x] **Independence**: Can parts be worked separately? No — single atomic
      upgrade.

**Decision**: No decomposition — 0 signals triggered.

## Status
**Status**: Finished
**Next Action**: /cwf-requirements-plan 14
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All five success criteria met: `.cwf/version` pins `v1.1.185` /
`cwf_sha=6659c1cca72ef033d92546fcd9d42a0f4d817dd9` (commit form);
`git log --merges 700baba..HEAD` empty (no merge — the headline win);
`validate: OK`; `cwf_method=read-tree` (documented); `workflow-manager status 14`
+ `validate` both succeed. Delivered in ~0.5 day, matching estimate.

## Lessons Learned
R1 (185 installer might still merge under subtree) did not materialise — read-tree
was confirmed by design-phase source reading and the ff-only linear landing is a
backstop regardless. The non-obvious mechanism (CWF forward-only gap) justified
running the full b/c phases on a chore. See j-retrospective.md.
