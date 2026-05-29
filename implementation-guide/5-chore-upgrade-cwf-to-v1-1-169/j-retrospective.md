# upgrade CWF to v1.1.169 - Retrospective
**Task**: 5 (chore)

## Task Reference
- **Task ID**: internal-5
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/5-upgrade-cwf-to-v1-1-169
- **Template Version**: 2.1
- **Retrospective Date**: 2026-05-29

## Executive Summary
- **Duration**: 2 days calendar (2026-05-27 task creation → 2026-05-29 completion), single-operator effort under 1 day across two sessions
- **Scope**: Originally a v1.1.155→v1.1.163 upgrade per BACKLOG item. After v1.1.163's `rules-inject` packaging defect aborted the first attempt, the target was pivoted forward to v1.1.169 (which carries upstream Task 167's fix for that exact defect). Final scope = upgrade + record the pivot/recovery as a documented escape hatch.
- **Outcome**: Success. CWF pinned to v1.1.169 (SHA `473baea2…`), `cwf-manage validate: OK`, all 6/6 success-path TCs pass. As a side benefit, the upgrade resolved a real consumer-side defect that was observable on this branch: `task-context-inference` went from `inconclusive, uncorrelated, task_nums: 2,5` (under v1.1.155) to `conclusive, correlated, task_num: 5` (under v1.1.169) via upstream Task 166.

## Variance Analysis
### Time and Effort
- **Estimated** (a-task-plan): <0.5 day, Low complexity
- **Actual**: ~1 working day across 2 sessions, including the v1.1.163 abort + recovery + pivot to v1.1.169 + re-planning
- **Variance**: ~2× the estimate, attributable entirely to the v1.1.163 packaging defect. The successful v1.1.169 run alone took ~5 minutes from `cwf-manage update` to validate-OK. Without the pivot, the original estimate would have held.

### Scope Changes
- **Target version moved from v1.1.163 to v1.1.169 mid-task**: the BACKLOG item named v1.1.163; the upstream broke at v1.1.163; upstream fixed it at v1.1.167; we landed v1.1.169. The directory and branch were renamed accordingly. Net scope unchanged ("upgrade CWF"), version increment widened by 6 patches.
- **Added retrospective lessons + improved d/e plans**: the v1.1.163 attempt surfaced gaps in failure-handling discipline (precondition HEAD gate, deterministic half-applied probe, `git clean -fdx` in revert path, named-path staging, hook-helper executable check, smoke-test pass/fail rubric). All folded into d-plan Step 2/3/4 and e-plan TC-5/TC-6/TC-8.

### Quality Metrics
- **Test Coverage**: 6/6 success-path TCs PASS; 2/2 negative-path TCs verified available without triggering. 100% of `a-task-plan.md` success criteria covered.
- **Defect Rate**: 0 escaped defects in the final landing. The v1.1.163 attempt itself surfaced as a defect (real and recoverable), corrected by pivoting forward rather than working around.
- **Performance**: N/A.

## What Went Well
- **The plan-review subagents earned their keep again.** The robustness reviewer surfaced 8 findings against the v1.1.163-targeted d-plan; 7 went directly into the v1.1.169-retargeted plan and would have made the v1.1.163 abort recoverable on first contact. None of those failure paths fired this time, but they're documented for any future consumer upgrade that hits an artefact conflict.
- **Pivoting forward beat retrying.** When v1.1.163 broke at `rules-inject`, the temptation was to force `CWF_UPGRADE_RESOLVE=keep` and retry. The right move was to recognise the bug as upstream-acknowledged (Task 167 fix shipped in v1.1.167) and bump to a version that doesn't carry the defect. Cheaper, cleaner, and the consumer also picks up T164/T165/T166/T168 improvements.
- **The recovery from the half-applied v1.1.163 state was non-destructive.** Soft-reset + targeted checkout + named-path discard + `git checkout -- .cwf/version` returned the tree to v1.1.155 cleanly without `git reset --hard` (which the user had explicitly denied). The pattern is now codified in d-plan Step 2 as the documented escape hatch.
- **Task 166's fix was observably correct on this consumer.** `task-context-inference` failed deterministically on this branch under v1.1.155 (top-level + nested candidate set collision); resolved deterministically under v1.1.169. First in-band confirmation of T166 in this repo.

## What Could Be Improved
- **The v1.1.155 cwf-apply-artefacts pre-flight didn't catch the rules-inject defect**, so the failure was end-of-laydown rather than pre-laydown. Upstream T167 fixed this *by removing the artefact entry entirely*; there's no consumer-side change to make. The lesson is for *consumer-side discipline*: capture the pre-upgrade HEAD as a precondition gate, so any future mid-update abort has a deterministic revert target. d-plan Step 1 now requires this.
- **Plan-prediction errors propagated through multiple files.** TC-1's `cwf_sha == git rev-list -n1 v1.1.163` expectation was inverted (the correct value is the annotated-tag object SHA from `git rev-parse`). The v1.1.163 attempt's exec uncovered this; the v1.1.169 plan revision corrected it. Lesson: when a plan calls out a specific SHA-derived value, verify the derivation against the source tool (`resolve_sha` in `cwf-manage:173-181`), not the human-intuitive identity.
- **The 500-line security-review cap is anchor-sensitive.** Both f-exec and g-exec recorded `error: cap exceeded` because the changeset spans from the task's baseline (`07366ad`) through the entire v1.1.169 laydown to HEAD, and no `security.review.test-paths` patterns are configured to exclude upstream-shipped content. The substantively-new surface (settings.json hook, .cwf/version, workflow MD) is small and was surfaced inline in the exec write-ups for user decision. For future CWF upgrades the cap will exceed by construction unless `test-paths` includes the laydown directories — see Future Work below.

## Key Learnings
### Technical Insights
- **`cwf_sha` is the annotated-tag object SHA, not the dereferenced commit.** `resolve_sha` uses `git rev-parse`; for annotated tags that returns the tag object (`473baea2…`), not the underlying commit (`0764380…`). Material when verifying post-upgrade state.
- **The subtree update path delegates laydown to the *target* `install.bash`** (`cwf-manage:406-427`). The stale wrapper only resolves the ref, holds the lock, runs `cwf-apply-artefacts`, settings-merge, exact-perms, and the version write. Most upstream fixes ship in-band because install.bash itself is at the target version.
- **`apply_replace` 3-way conflict detection is unforgiving under no-TTY**: if on-disk ≠ baseline AND on-disk ≠ new, it aborts with `requires interactive resolution but no TTY`. The upstream fix path is either resolve at packaging time (drop dual-distribution, the T167 approach) or accept the consumer must set `CWF_UPGRADE_RESOLVE=keep|new` per invocation.

### Process Learnings
- **Forward-fix beats workaround when upstream already fixed it.** Verifying upstream CHANGELOG for the *exact* failure mode took two minutes; the workaround would have masked the defect locally and shipped less benefit (no T164/165/166/168).
- **Precondition gates pay rent on success runs too.** Capturing the pre-upgrade HEAD into f-exec is two lines of work and zero cost on success. On failure it's the difference between a clean revert and an inspection session.
- **Plan-review is structural, not stylistic.** The 4-parallel subagent review found *real* gaps (per-invocation env-var semantics, deterministic half-applied probe, untracked-file cleanup in revert) — not prose nits. Worth running on every non-trivial chore plan, not just feature/bugfix.

### Risk Mitigation Strategies
- **Soft-reset over hard-reset for in-task recovery**: keeps the failed attempt reachable via reflog and survives the "never rewrite history visibly" preference. The d-plan codifies this.
- **Named-path staging** (`git add .cwf/version .claude/settings.json`) over `git add -A` for post-laydown metadata: avoids sweeping in unrelated working-tree edits.
- **Two-tier sentinel for half-applied detection**: combine `grep ^cwf_version .cwf/version` (must still read v1.1.155 in a half-applied state) with `test -f` for a known-target-version-only file (e.g. `security-review-classify` introduced by T162). Either signal alone is ambiguous; together they're deterministic.

## Recommendations
### Process Improvements
- **Always pin an explicit semver tag** for `cwf-manage update`, never `HEAD` or `latest`. The v1.1.155 wrapper has FR1 (the resolved ref string is written verbatim to `cwf_version`); a non-semver ref records as a non-semver version field. T159 fixed this forward; consumers behind T159 must pin manually.
- **Capture pre-upgrade HEAD as a precondition gate** in any upgrade f-exec, before invoking the updater. Required for the documented revert path.

### Tool and Technique Recommendations
- **Use the 4-parallel plan-review subagents on operational chores.** Worth it for any change that touches integration/installer surface, not just feature work. The robustness reviewer was the substantive contributor here.

### Future Work
- **Configure `security.review.test-paths` to exclude upstream-shipped CWF directories** so the production-weighted cap measures only consumer-authored code. Candidate patterns: `.cwf/**`, `.cwf-skills/**`, `.cwf-rules/**`, `.cwf-agents/**`, `.claude/skills/**`, `.claude/agents/**`. Filed as new BACKLOG item.
- **No follow-up needed for the v1.1.163 attempt itself** — upstream T167 fix is the correct resolution; the failure was a one-time packaging defect, not a class of bugs.

## Status
**Status**: Finished
**Next Action**: Task complete; suggest merge to main
**Blockers**: None
**Completion Date**: 2026-05-29
**Sign-off**: Matt Keenan + Claude Opus 4.7

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Archived Materials
- Planning: `a-task-plan.md`, `d-implementation-plan.md`, `e-testing-plan.md`
- Execution: `f-implementation-exec.md`, `g-testing-exec.md`
- Branch tip: `0677cbb` (g-exec checkpoint); pre-upgrade tip `3ecf3b8`; 9 install.bash auto squash commits + 1 post-laydown metadata commit + 6 phase checkpoints between them
