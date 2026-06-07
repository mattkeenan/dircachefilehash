# size column shows disk size not change bytes - Retrospective
**Task**: 13 (bugfix)

## Task Reference
- **Task ID**: internal-13
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: bugfix/13-size-column-shows-disk-size-not-change-bytes
- **Template Version**: 2.1
- **Retrospective Date**: 2026-06-07

## Executive Summary
- **Duration**: single session (estimated: <0.5 day / Low) — within estimate.
- **Scope**: Unchanged from plan. Render-layer-only fix: the interactive-tree
  size column now tracks the active sort metric (change volume by default)
  instead of the hardcoded on-disk `Stats.Bytes`. Two incidental test-file
  cleanups folded in.
- **Outcome**: Success. The reported symptom (a directory with ~112 MB of
  churn rendering its 51.9 GB disk size under `change_bytes(desc)`) is fixed;
  `columnText` at 100% coverage, full regression + race green, security clean.

## Variance Analysis
### Time and Effort
- **Estimated**: <0.5 day total (Low complexity), no per-phase split.
- **Actual**: single session across a→j. Implementation was two edits
  (helper + wire-up) with zero deviations on the core change.
- **Variance**: On estimate. The up-front column-rule decision (AskUserQuestion)
  and the plan-review F1 catch removed the usual mid-exec churn.

### Scope Changes
- **Additions**: Two minor cleanups in `render_test.go`, both in a file already
  being edited: (1) `rowLine` uses `strings.SplitSeq` per the `stringsseq`
  linter; (2) dropped the dead `h` parameter from the shared `newSimModel`
  helper, clearing a pre-existing full-tree `unparam` finding the new caller
  surfaced. Neither changes behaviour.
- **Removals**: None.
- **Impact**: Negligible; left the tree cleaner than before (one latent lint
  retired).

### Quality Metrics
- **Test Coverage**: `columnText` 100.0% (every sort-key branch + name remap);
  `cmd/dcfh/internal/tui` package 82.1%. Target (100% on the new helper) met.
- **Defect Rate**: 0 defects found in test; 0 plan deviations on the core fix.
- **Performance**: N/A — per-visible-row `strconv`/format only, no data re-read.

## What Went Well
- **Settling the product fork up front paid off.** The "what does the column
  show under each sort key" decision was a genuine UX fork; asking it via
  AskUserQuestion *before* writing the design (answer: "track active sort")
  meant the design recorded the user's choice and exec had nothing to revisit.
- **The plan-review caught the one real trap.** Three of four design reviewers
  independently flagged that `metric(n, sortName)` returns `0`, so the
  `name`→`change_bytes` fallback had to remap the key *before* reading it
  (design F1). It was promoted to an explicit ordered requirement and locked by
  a dedicated test (`name` must not render `0 B`). The trap never reached code.
- **The test fixture is a genuine regression guard, not a tautology.** Asserting
  on the `docs/` node (deleted child → `Stats.Bytes` 0 but change_bytes 900)
  distinguishes the fix from the bug: the old code drew `0 B`, the fix draws
  `900 B`. A node where the two values collide (`src`, 250==250) was explicitly
  rejected.
- **Reuse over re-derivation.** `columnText` calls the same `metric()` the
  comparator ranks on, so the displayed number and the ordering are structurally
  unable to diverge — the exact failure class being fixed.

## What Could Be Improved
- **This bug was introduced by task 12 and missed there.** Task 12 changed the
  default sort metric to `change_bytes` but left `drawRow` rendering
  `Stats.Bytes`. The task-12 plan reviews focused on the data plumbing
  (deleted-byte attribution) and the sort comparator, but no reviewer checked
  that the *displayed* column tracked the new ranking metric. A metric/display
  mismatch is exactly the kind of thing a screenshot or a render assertion would
  have caught.
- **The user found it by eye, in production-like use.** The headless render
  tests existed but only asserted header/stats-pane text, never the per-row
  column value — so the column could silently disagree with the sort.

## Key Learnings
### Technical Insights
- When a ranking/sort metric changes, the value rendered next to each item must
  be re-pointed at the same metric in the same change. Sort order and the
  displayed magnitude are one feature; splitting them across the comparator and
  the renderer invites silent divergence.
- Deriving a display string from the canonical ranking function (rather than a
  parallel hand-sum) makes column/order divergence unrepresentable.

### Process Learnings
- For viewer/TUI changes, a render-layer assertion on the *visible value* (not
  just header/legend text) is the cheap guard. Task 12's tests asserted chrome,
  not the data cells — the gap the user closed manually.
- AskUserQuestion is the right tool for a real UX fork during planning; it
  converts a likely review-cycle round-trip into a one-shot recorded decision.

### Risk Mitigation Strategies
- The plan-review map/reduce earned its keep here: the `name`→0 trap was a
  subtle correctness bug that three independent reviewers surfaced before any
  code existed.

## Recommendations
### Process Improvements
- For any change to a sort/ranking/aggregation metric in a viewer, add (or
  require) a test that asserts the **rendered per-row value**, not only the
  header/sort indicator. Consider this a standing checklist item for TUI tasks.

### Tool and Technique Recommendations
- Keep deriving displayed magnitudes from the ranking function (single source),
  the pattern that made this fix small and self-proving.

### Future Work
- No new follow-up task is warranted. The pre-existing **Medium** BACKLOG item
  "Add test-file globs to security.review.max-lines-exclude-paths" remains
  relevant (it was task 12 that hit the review cap; task 13's smaller production
  diff did not, but the config gap stands). No new BACKLOG items added.

## Status
**Status**: Finished
**Next Action**: Task complete
**Blockers**: None identified
**Completion Date**: 2026-06-07
**Sign-off**: Matt Keenan (with Claude Opus 4.8)

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Archived Materials
- Planning: a-task-plan.md, c-design-plan.md, d-implementation-plan.md,
  e-testing-plan.md (this task directory).
- Execution: f-implementation-exec.md (security review: no findings),
  g-testing-exec.md (TC-1…TC-6 PASS, coverage, security review: no findings).
- Commits: `862d34b` (a) → `a811614` (g); granular a–j checkpoints preserved on
  the `-checkpoints` branch, squashed into a single `Task 13:` commit.
