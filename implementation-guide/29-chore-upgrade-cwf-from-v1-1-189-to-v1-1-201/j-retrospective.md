# upgrade CwF from v1.1.189 to v1.1.201 - Retrospective
**Task**: 29 (chore)

## Task Reference
- **Task ID**: internal-29
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/29-upgrade-cwf-from-v1-1-189-to-v1-1-201
- **Template Version**: 2.1
- **Retrospective Date**: 2026-06-14

## Executive Summary
- **Duration**: ~1.25h wall-clock (estimate: `<1 day`, Low complexity — within estimate, no variance against the stated bound).
- **Scope**: Unchanged from plan. Upgrade vendored CWF `.cwf/` v1.1.189 → v1.1.201, reconcile the inter-repo surface (`.claude/settings.json`, `.gitignore`, `CLAUDE.md`), preserve linear history.
- **Outcome**: Success. All 5 success criteria met; landed as a single non-merge changeset (`682a2615`), `cwf-manage validate` clean, TC-1…TC-7 + full regression green, inter-repo security review `no findings`.

## Variance Analysis
### Time and Effort
- **Estimated**: `<1 day` total (chore — no requirements/design/rollout phases).
- **Actual** (commit timestamps, 2026-06-14): plan phases a/d/e 11:15→11:43 (~28m); implementation-exec 682a2615 12:24; testing-exec 93632f57 12:28. End-to-end ~1.25h including a context compaction mid-task.
- **Variance**: Well under the `<1 day` bound. The read-tree `cwf-manage update` mechanism did the mechanical work in one command; effort was concentrated in the inter-repo diff reconciliation and the scoped security review, both anticipated by the plan.

### Scope Changes
- **Additions**: None.
- **Removals**: None descoped. CWF-internal `.cwf/**` re-audit was *out of scope by design* (plan § "Security Review Scope" — 3 prior upstream reviews), not a removal.
- **Impact**: None — final scope equals planned scope.

### Quality Metrics
- **Test Coverage**: 100% of e-testing-plan.md (TC-1…TC-7) plus both non-functional bands (regression, workflow tooling). All PASS.
- **Defect Rate**: Zero defects found during execution; zero deviations affecting outcome.
- **Security**: Inter-repo surface reviewed by `cwf-security-reviewer-changeset` → `no findings`.

## What Went Well
- **Pre-identified inter-repo surface paid off.** The plan named the exact risk (Task 195 dead-matcher reshape, Task 201 new Bash hook). The actual `settings.json` diff matched TC-4 line-for-line, so reconciliation was verification rather than discovery.
- **read-tree laydown stayed linear.** No merge commit (`git log --merges` empty), HEAD unchanged until the checkpoint commit — exactly as [[project_cwf_subtree_merge_commits]] predicts for `cwf_method=read-tree`.
- **Scoped security review held under the user's explicit boundary.** Building a 55-line inter-repo-only changeset and reviewing that gave a clean, defensible `no findings` without re-auditing CWF internals.
- **rules-inject move verified byte-identical**, so the most prompt-injection-relevant change carried zero behavioural delta.

## What Could Be Improved
- **The `security-review-changeset` cap is a poor fit for a vendored-tooling upgrade.** Both exec phases exited 2 (`1384 production lines > 500`) because the relaid `.cwf/**` tree counts as production. The bulk is explicitly out of scope, so the mechanical verdict (`error: cap exceeded`) had to be hand-overridden with a scoped review and a prose justification in each wf file. This is the same cap-mismatch pattern Task 28 flagged for *coordinating parents*, surfacing here for *CWF-upgrade* tasks for a different structural reason (out-of-scope internals rather than a union diff).
- **No config lever exists to express "exclude `.cwf/**` from the cap for this task class."** The override lived entirely in reviewer judgement + wf-file prose, which is correct but not reusable for the next upgrade.

## Key Learnings
### Technical Insights
- v1.1.201 ships a **fail-open, inert-by-default, timeout-bounded** PreToolUse/Bash hook (`pretooluse-bash-tool-check`). With zero checked-in rules (`.cwf/tool-check/` absent) it is a no-op; the only live-rule path is the gitignored `.cwf/tool-check/*/settings.local.json`, an operator-trust boundary.
- The upgrade tightened script/lib perms (0700→0500, 0600→0444); `cwf-manage validate` enforces these exact modes, so any post-upgrade `chmod` drift would be caught.

### Process Learnings
- For a vendored-tooling upgrade, the **inter-repo diff is the whole review** — `settings.json` + `.gitignore` + `CLAUDE.md`. Snapshotting those three to a scratch dir before laydown made the after-diff trivial and auditable.
- When the automated security gate's scope and the task's stated scope diverge, the honest record is: (a) note the mechanical exit-2 verbatim, (b) run the in-scope review explicitly, (c) cite the instruction-priority justification. Do not silently suppress the cap.

### Risk Mitigation Strategies
- Pre-flight snapshots of the inter-repo files (mode 0700 scratch) turned Risk 1 (hook/matcher conflict) into a mechanical before/after diff.
- Risks 2 (dirty tree / merge commit) and 3 (Task 194 changeset behaviour) were verified non-issues by direct check (`git log --merges` empty; changeset helper resolved the task baseline correctly).

## Recommendations
### Process Improvements
- Consider a CWF convention for **upgrade-class tasks**: anchor or scope the `security-review-changeset` cap to the inter-repo surface (e.g. a `security.review.max-lines-exclude-paths` glob covering `.cwf/**` when the task type/marker is "vendored upgrade"), so the gate's mechanical verdict matches the reviewable surface instead of always tripping exit-2. See Future Work below.

### Tool and Technique Recommendations
- The snapshot-then-diff pattern for `.claude/settings.json` / `.gitignore` / `CLAUDE.md` should be the standard pre-flight for any future CWF version bump.

### Future Work
- **BACKLOG**: scope the changeset security-review cap for CWF-upgrade tasks so `.cwf/**` relaid lines don't force a hand-overridden exit-2 every upgrade.

## Status
**Status**: Finished
**Next Action**: Task complete — suggest ff-only merge to `main`
**Blockers**: None identified
**Completion Date**: 2026-06-14
**Sign-off**: Matt Keenan / Claude (CWF workflow)

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Archived Materials
- Planning: `a-task-plan.md`, `d-implementation-plan.md`, `e-testing-plan.md`
- Execution: `f-implementation-exec.md` (upgrade changeset `682a2615`), `g-testing-exec.md` (`93632f57`)
- Security: scratch dir `/tmp/-home-matt-repo-dircachefilehash-task-29/` — `interrepo-changeset.out`, `security-review-output-implementation-exec.out`
- Upgrade record: `.cwf/version` (`cwf_version=v1.1.201`, `cwf_sha=2933eba8…`, `cwf_method=read-tree`)
