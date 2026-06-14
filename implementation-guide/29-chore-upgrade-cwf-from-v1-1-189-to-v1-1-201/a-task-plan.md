# upgrade CwF from v1.1.189 to v1.1.201 - Plan
**Task**: 29 (chore)

## Task Reference
- **Task ID**: internal-29
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/29-upgrade-cwf-from-v1-1-189-to-v1-1-201
- **Baseline Commit**: e1255c3a2d2a915b6f18cebd35edc3b5f10c803b
- **Template Version**: 2.1

## Goal
Upgrade the vendored CWF tooling in `.cwf/` from v1.1.189 to v1.1.201 cleanly,
keeping linear history and validating that the new version's workflow tooling
operates correctly in this repo.

## Success Criteria
- [ ] `.cwf/version` records `cwf_version=v1.1.201` (and matching `cwf_ref`/`cwf_sha`)
- [ ] Upgrade lands as a single non-merge commit (linear history preserved)
- [ ] `.cwf/scripts/cwf-manage` integrity/security check passes post-upgrade
      (`/cwf-security-check` clean)
- [ ] A representative workflow helper runs clean against this repo post-upgrade
      (e.g. `cwf-status`, security-review-changeset against the task baseline)
- [ ] No untracked/unexpected files left in `.cwf/` after laydown

## Original Estimate
**Effort**: <1 day
**Complexity**: Low
**Dependencies**: Local CwF source at `/home/matt/repo/coding-with-files` with
tag `v1.1.201` present (verified); repo already on `cwf_method=read-tree`.

## Major Milestones
1. **Pre-flight**: Confirm clean tree, current version v1.1.189, read-tree method,
   and review the 189→201 upstream changelog for inter-repo-affecting changes.
2. **Upgrade**: Run the read-tree laydown to v1.1.201; verify `.cwf/version` and
   that no merge commit was produced.
3. **Validate**: Run integrity/security check + a representative workflow helper;
   confirm hooks/registrations still align with this repo's setup.

## Risk Assessment
### High Priority Risks
- **Risk 1**: New CwF version registers/changes git hooks or PreToolUse matchers
  (Task 195 touched a dead matcher; 199 changed tmp-paths) that conflict with this
  repo's linear-history githooks or sandbox assumptions.
  - **Mitigation**: This is the inter-repo integration surface — diff the
    registered hooks/settings before vs after laydown and reconcile explicitly.

### Medium Priority Risks
- **Risk 2**: Laydown leaves working tree dirty or emits a merge commit despite
  read-tree method.
  - **Mitigation**: Verify `git log --merges` shows nothing new and tree is clean
    before committing; checkpoint via the standard CWF checkpoint flow.
- **Risk 3**: Task 194 (untracked files now included in security-review changeset)
  changes what our own per-task security reviews scan.
  - **Mitigation**: Note the behaviour change; verify our changeset reviews still
    bound correctly to the task baseline.

## Security Review Scope (explicit)
CwF's own internal code has already passed **3 upstream security reviews**.
This task's security reviews (`cwf-plan-reviewer-security`,
`cwf-security-reviewer-changeset`) therefore check **only the inter-repo
integration surface** — how v1.1.201 interacts with *this* repository: hooks and
PreToolUse matchers it registers, tmp-path/sandbox assumptions (Task 199),
clean-tree/changeset behaviour (Tasks 191/194) against our linear-history +
githooks model — **not** CwF's internal implementation.

## Dependencies
- Local CwF source repo (`file:///home/matt/repo/coding-with-files`) with v1.1.201.
- Clean working tree before laydown (helpers may guard on this).

## Constraints
- Linear history only — no merge commits (read-tree method already in place per
  [[project_cwf_subtree_merge_commits]]).
- `.cwf/version` is committed normally ([[feedback_cwf_version_is_committed]]).

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [ ] **Time**: Will this take >1 week? No
- [ ] **People**: Does this need >2 people? No
- [ ] **Complexity**: Does this involve 3+ distinct concerns? No (single upgrade)
- [ ] **Risk**: Are there high-risk components that need isolation? No
- [ ] **Independence**: Can parts be worked on separately? No

0 signals triggered — no decomposition needed.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All 5 success criteria met. `.cwf/version` records v1.1.201 (`cwf_sha=2933eba8…`,
read-tree); upgrade landed as single non-merge commit `682a2615`; `cwf-manage
validate` clean; `workflow-manager status` + `security-review-changeset` ran clean
against the task baseline; no untracked/unexpected files. ~1.25h wall-clock vs the
`<1 day` estimate. See `j-retrospective.md` for full variance analysis.

## Lessons Learned
The inter-repo surface the plan pre-identified (Task 195 dead-matcher reshape, Task
201 Bash hook) matched the actual `settings.json` diff exactly, turning Risk 1 into
mechanical verification. The `security-review-changeset` cap tripped on relaid
`.cwf/**` (1384 production lines) — out of scope by design — and was handled with a
scoped 55-line inter-repo review (`no findings`). Captured as a process
recommendation in `j-retrospective.md`.
