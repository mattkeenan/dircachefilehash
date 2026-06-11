# Exclude test and docs globs from review line cap - Plan
**Task**: 20 (chore)

## Task Reference
- **Task ID**: internal-20
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/20-exclude-test-and-docs-globs-from-review-line-cap
- **Baseline Commit**: a22530df8cad8e4829dcd03fb4433bc0d8a6833e
- **Template Version**: 2.1

## Goal
Restore the exec-phase security-review gate for test-heavy and docs-heavy tasks
by discounting `**/*_test.go`, `docs/**`, and `*.md` from the
`security.review.max-lines-exclude-paths` cap count in
`implementation-guide/cwf-project.json`, so the production-weighted line count
reflects only consumer production code and stops silently tripping the 500-line
cap (which skips the `cwf-security-reviewer-changeset` subagent entirely).

## Success Criteria
- [ ] `security.review.max-lines-exclude-paths` in `cwf-project.json` lists
  `**/*_test.go`, `docs/**`, and `*.md` alongside the existing
  `implementation-guide/**`.
- [ ] The exec-phase `security-review-changeset` helper, run on a changeset
  whose only large additions are test files and/or `docs/**`/`*.md`, reports a
  production-weighted count that excludes those paths (cap measures consumer
  production code only) — verified against a representative diff.
- [ ] The documented caveat is recorded as a conscious decision: excluding a
  path only changes the **cap count**, not what the subagent sees — the FULL
  changeset is still emitted to the reviewer, so a test/docs-heavy task flips
  from "cap exceeded → subagent skipped" to "subagent invoked on the full diff".
- [ ] The CWF-vendored `.cwf/**` glob is explicitly **out of scope** (its
  caveat — flipping pure-CWF-upgrade tasks to a full-vendored-delta review —
  is not resolved here).
- [ ] No Go/source change; `cwf-manage validate` passes.

## Original Estimate
**Effort**: <0.5 day
**Complexity**: Low
**Dependencies**: None (config-only; stacked on Task 19 tip a22530d)

## Major Milestones
1. **Config edit**: add the three globs to `max-lines-exclude-paths`.
2. **Verification**: confirm the helper discounts test/docs paths from the cap
   on a representative changeset, and `cwf-manage validate` passes.
3. **Documentation**: BACKLOG.md items (Task 12 `**/*_test.go`, Task 17
   `docs/**`/`*.md`) marked resolved; `.cwf/**` caveat left standing.

## Risk Assessment
### High Priority Risks
- *(none — single-file config change, already-excluded `implementation-guide/**`
  shows the mechanism works)*

### Medium Priority Risks
- **Glob too broad**: `*.md` / `docs/**` could discount a genuinely
  security-relevant Markdown change (e.g. a doc embedding a command).
  - **Mitigation**: the caveat above — the full changeset is *still* emitted to
    the subagent; the globs only relax the cap, they never hide content. Net
    effect is strictly more review coverage, not less.
- **`**/*_test.go` masking prod logic in test files**: test files can contain
  helpers that exercise prod paths.
  - **Mitigation**: same — content is still reviewed; only the cap count drops.

## Dependencies
- Stacked on Task 19 tip (`a22530d`, v0.13.19); lands ff-only after Task 19.

## Constraints
- Config-only: no `.go`, `Makefile`, or build-contract change.
- CWF workflow files edited only via CWF skills.

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [ ] **Time**: Will this take >1 week? No — minutes.
- [ ] **People**: Does this need >2 people working on different parts? No.
- [ ] **Complexity**: Does this involve 3+ distinct concerns? No — one config key.
- [ ] **Risk**: Are there high-risk components that need isolation? No.
- [ ] **Independence**: Can parts be worked on separately? No.

No signals triggered — single atomic config change. No decomposition.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All five success criteria met. The three globs were appended to
`max-lines-exclude-paths`; the helper discounts test/docs/`*.md` paths from the cap
(TC-4 measured the discount at exactly 830 lines = the excluded files' own total);
the "cap count only, full changeset still reviewed" caveat is recorded and was
independently confirmed by both exec-phase security reviews; `.cwf/**` left out of
scope; `cwf-manage validate` passes; no Go/source change. Single session (plus a
compaction), within the <0.5 day / Low estimate. 0 defects.

## Lessons Learned
Empirical glob-semantics verification (`git ls-files -- ':(glob)…'`) in planning made
the security-critical root-only-`*.md` choice evidence-backed and turned execution
into a single edit. See j-retrospective.md for the full write-up.
