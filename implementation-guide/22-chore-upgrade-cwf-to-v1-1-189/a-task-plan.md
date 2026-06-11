# upgrade CwF to v1.1.189 - Plan
**Task**: 22 (chore)

## Task Reference
- **Task ID**: internal-22
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/22-upgrade-cwf-to-v1-1-189
- **Baseline Commit**: 82ff99113e06d4bf1da569ae3734b3bef28b9fdd
- **Template Version**: 2.1

## Goal
Upgrade the installed CWF subtree from v1.1.185 to v1.1.189 via the
merge-free `read-tree` laydown, leaving `.cwf/version` consistent, the tree
validate-clean, and the workflow tooling functional — with no merge commit on
the task branch.

## Context
Current install: `cwf_version=v1.1.185` (`cwf_sha=6659c1cc…`,
`cwf_method=read-tree`). Target: `v1.1.189`
(commit `6af636e32ad1ffaebd2601c7101dd46c8a3c30b7`), present in the source repo
`file:///home/matt/repo/coding-with-files`. The 185→189 delta is four
upstream tasks:
- **186** — reviewer agents prefer tools over Bash (agent shared-rules docs).
- **187** — low effort level for exec wf-step skills.
- **188** — retire the vestigial `version` / `_version-note` fields from
  `cwf-project.json` (template + upstream's own config).
- **189** — sync docs/README/COMMANDS/DESIGN with current CWF state.

The only functional, non-doc surfaces are `.cwf/security/script-hashes.json`
(re-validated by `cwf-manage validate`), the agent/skill docs, and the
`cwf-project.json` schema change from task 188.

## Success Criteria
- [ ] `.cwf/version` records `cwf_version=v1.1.189`, `cwf_ref=v1.1.189`, and
      `cwf_sha=6af636e32ad1ffaebd2601c7101dd46c8a3c30b7` (commit-object SHA,
      consistent with the commit-form recorded for prior upgrades).
- [ ] The upgrade introduces **no merge commit** on the task branch
      (`git log --merges 82ff991..HEAD` is empty).
- [ ] `.cwf/scripts/cwf-manage validate` exits 0 (config + workflow-file +
      script-hash + perms integrity all pass post-laydown).
- [ ] The task-188 schema change is reconciled in our
      `implementation-guide/cwf-project.json`: the vestigial top-level `version`
      / `_version-note` fields (ignored by the Config validator) are removed to
      match the new convention, while the `versioning` block is left untouched
      (it tracks the dircachefilehash project version `v0.13.x`, not CWF's).
- [ ] Workflow tooling remains functional post-upgrade (a helper/skill
      invocation succeeds — e.g. `workflow-manager` + a `cwf-status` call).

## Original Estimate
**Effort**: ~0.5 day
**Complexity**: Low (mechanical version bump; same read-tree path already in
use, so no laydown-method migration this round)
**Dependencies**: CWF source repo with tag `v1.1.189` present (verified);
baseline `82ff991` (v1.1.185 landed and clean).

## Major Milestones
1. **Requirements (b)**: Pin the functional contract — version-file fields,
   no-merge-commit invariant, validate-clean, and how the task-188
   `cwf-project.json` schema change is reconciled with our project config.
2. **Implementation (f)**: Run `cwf-manage update v1.1.189`, reconcile
   `cwf-project.json`, verify version file + no merge + validate.
3. **Testing (g)**: Confirm validate-clean, linear history, and a live
   workflow-tooling invocation.

## Risk Assessment
### High Priority Risks
- **Risk 1**: Laydown emits a merge commit, breaking the linear-history
  invariant (`[[project_cwf_subtree_merge_commits]]`, `[[never_merge_commits]]`).
  - **Mitigation**: v1.1.185 already uses `read-tree` (merge-free); confirm
    `cwf_method=read-tree` is honoured and check `git log --merges` immediately
    after laydown. Fallback: reset and re-apply as a single linear commit.

### Medium Priority Risks
- **Risk 2**: Task-188's removal of the `version` field is template-only and
  does **not** auto-clean our existing `implementation-guide/cwf-project.json`
  (which still carries `version: v0.13.0` + `_version-note`), leaving our
  config out of step with the new schema.
  - **Mitigation**: Explicitly decide in requirements/design whether to strip
    those fields to match the new convention; apply as part of this task.
- **Risk 3**: `script-hashes.json` mismatch causes `cwf-manage validate` to
  fail post-laydown.
  - **Mitigation**: The upgrade lays down the matching hashes; run `validate`
    as a gate and treat any failure as a blocker, not a workaround.

## Dependencies
- CWF source repo `file:///home/matt/repo/coding-with-files` with tag
  `v1.1.189` (verified present, SHA `6af636e`).
- Clean working tree on `chore/22-upgrade-cwf-to-v1-1-189` at baseline
  `82ff991`.

## Constraints
- Linear history only — ff-only landing, no `--no-ff`, no merge commits
  (`[[never_merge_commits]]`, `[[branch_landing_model]]`).
- Upgrade must go through `cwf-manage update`, not ad-hoc file copies.
- All workflow file changes go through CWF skills (`[[use_cwf_for_changes]]`).

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [x] **Time**: >1 week? No — ~0.5 day.
- [x] **People**: >2 people? No — single operator.
- [x] **Complexity**: 3+ distinct concerns? No — one laydown + one config
      reconciliation.
- [x] **Risk**: High-risk components needing isolation? No — read-tree is
      proven on this repo.
- [x] **Independence**: Separable parts? No — single atomic upgrade.

**Verdict**: No decomposition. 0 signals triggered.

## Status
**Status**: Finished
**Next Action**: /cwf-requirements-plan 22
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All five success criteria met. CWF pinned to v1.1.189 (`cwf_sha=6af636e…`) via
merge-free read-tree; no merge commit; `cwf-manage validate` clean; task-188
`cwf-project.json` reconciled (vestigial `version`/`_version-note` removed,
`versioning` untouched); workflow tooling functional. 0 decomposition signals
held — single atomic upgrade, no subtasks. See j-retrospective.md.

## Lessons Learned
Annotated-tag dereferencing and the MIN-bottleneck progress formula were the two
non-obvious points; both pre-empted false blockers. See j-retrospective.md § Key
Learnings.
