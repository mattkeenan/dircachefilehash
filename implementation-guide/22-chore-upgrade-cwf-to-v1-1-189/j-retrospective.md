# upgrade CwF to v1.1.189 - Retrospective
**Task**: 22 (chore)

## Task Reference
- **Task ID**: internal-22
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/22-upgrade-cwf-to-v1-1-189
- **Template Version**: 2.1
- **Retrospective Date**: 2026-06-11

## Executive Summary
- **Duration**: ~1 hour wall-clock across the planning + exec session (first checkpoint 12:54, last exec checkpoint 13:59), spread over two sittings with a user plan-review pause between. Estimated ~0.5 day; well under estimate.
- **Scope**: Final scope matched the plan exactly — one `cwf-manage update v1.1.189` laydown plus the task-188 `cwf-project.json` reconciliation. No additions, no removals.
- **Outcome**: Success. All five a-task-plan success criteria met; CWF pinned to v1.1.189 via merge-free read-tree; tree validates clean; linear history preserved.

## Variance Analysis
### Time and Effort
- **Estimated** (a-task-plan): ~0.5 day, Low complexity.
  - Planning (a/d/e): bundled
  - Implementation (f): bundled
  - Testing (g): bundled
- **Actual**:
  - Planning (a/d/e): ~12 min of commits (12:54–13:06), preceded by the prior session's research; one criterion-refinement commit after the d-plan review
  - Implementation (f): one laydown + two-key JSON edit + security review (~13:48)
  - Testing (g): five TCs + Go regression + security review (~13:59)
- **Variance**: Under estimate. The mechanical read-tree path and the empirically-verified config edit left little room for surprise. The dominant cost was process (plan review, two security reviews), not the change itself.

### Scope Changes
- **Additions**: None.
- **Removals**: None deferred. The task-188 reconciliation was kept deliberately narrow — only `version` + `_version-note` removed; `cwf-version`/`_cwf-version-note` retained per upstream's own deferral.
- **Impact**: None — scope held.

### Quality Metrics
- **Test Coverage**: 5/5 functional TCs PASS; each maps 1:1 to an a-plan success criterion. Go regression suite green (sanity).
- **Defect Rate**: 0 defects introduced. One test-command usage correction (TC-5 `--task-path` → positional `TASK_PATH`), not a product defect.
- **Performance**: N/A — no performance-sensitive change.

## What Went Well
- **Merge-free read-tree laydown held**: `cwf_method=read-tree` produced no commit; `git log --merges 82ff991..HEAD` stayed empty, satisfying the linear-history invariant without any fallback.
- **Plan review earned its keep**: the four-agent review caught the misleading "adjacent keys" framing in the d-plan's Code Changes excerpt — the two removed keys sit at file lines 3 and 50, not together. Splitting into Edit A / Edit B prevented a wrong edit during exec.
- **Empirical verification over plan text**: Step 3 diffed the laid-down template to confirm task-188 dropped exactly two keys, rather than trusting the plan's excerpt. The config edit landed correctly first try.
- **Both security reviews returned `no findings`** and independently surfaced the same documented awareness item (reviewer-agent `Bash` widening), confirming the deterministic classifier and the changeset helper work end-to-end on a docs-heavy diff.

## What Could Be Improved
- **TC-5 invocation syntax**: the e-plan named `workflow-manager` generically; the first exec attempt guessed `--task-path=22`, which the v1.1.189 status-aggregator rejects (positional `TASK_PATH`). Pinning the exact command form in the test plan would have avoided the retry. Low impact — caught and corrected immediately.
- **`echo "EXIT=$?"` suffix habit**: an exec command appended the exit-echo suffix, which the user blocks (matches the CLAUDE.md anti-pattern). Reinforced going forward — read the exit code from the helper's own output, never append the echo.

## Key Learnings
### Technical Insights
- **Annotated-tag dereferencing**: `v1.1.189` is annotated, so bare `git rev-parse v1.1.189` yields the tag-object SHA (`ba42ab1…`) while the recorded `cwf_sha` is the dereferenced commit (`6af636e…`). The plan pre-empted a false blocker by documenting this; worth carrying into future upgrade tasks.
- **Progress is a MIN-bottleneck, not an average**: `TaskState::state_done` returns `min(phase%)` floored at 25 once any phase ≥ 25. A single non-terminal phase (here `j` at Backlog/0%) pins the whole task at 25% regardless of how many phases are Finished. The pre-retrospective 25% is expected, not stale status.

### Process Learnings
- **The helper is the single source of truth for the changeset and the verdict**: running `security-review-changeset` bare (no redirect/`wc`/`grep`) and piping the subagent output through `security-review-classify` kept the review deterministic and avoided prose-rule drift.
- **Verify the artefact, not the plan's copy of it**: the d-plan deliberately instructed re-deriving the task-188 delta from the laid-down template at exec time. This caught nothing wrong this round but is the right default when a plan quotes an upstream change.

### Risk Mitigation Strategies
- The headline risk (a merge commit breaking linear history) was retired by checking `git log --merges` immediately after laydown rather than only at the end — early confirmation that read-tree behaved.

## Recommendations
### Process Improvements
- When a test case exercises a CLI helper, record the **exact** invocation (flags vs positional) in the e-plan, especially across a tool-version bump where option parsing may have changed.

### Tool and Technique Recommendations
- Continue the "diff the laid-down template to confirm the upstream delta" pattern for any future CWF upgrade that carries a config-schema change.

### Future Work
- **Possible follow-up (not scheduled)**: upstream CWF still carries `cwf-version`/`_cwf-version-note` in the template (task-188 deferred their removal). If a future CWF release retires them, a small reconciliation task will mirror that here. No action now — tracked only as awareness.
- The reviewer-agent `Bash` widening is a documented FR4(c) posture choice in CWF itself; no local action required.

## Status
**Status**: Finished
**Next Action**: Task complete
**Blockers**: None identified
**Completion Date**: 2026-06-11
**Sign-off**: Matt Keenan (claude@mattkeenan.net)

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Archived Materials
- Planning: `a-task-plan.md`, `d-implementation-plan.md`, `e-testing-plan.md`
- Execution: `f-implementation-exec.md`, `g-testing-exec.md`
- Commits (pre-squash): `77edb7c`→`8fd062d`→`2e6e0fb`→`bd11b74`→`1ab7bfbc`→`74e7a8cf`
- Security review changesets: `/tmp/-home-matt-repo-dircachefilehash-task-22/security-review-changeset-{implementation,testing}-exec.out`
