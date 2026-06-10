# Showcase key features in README - Retrospective
**Task**: 18 (chore)

## Task Reference
- **Task ID**: internal-18
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/18-showcase-key-features-in-readme
- **Template Version**: 2.1
- **Retrospective Date**: 2026-06-10

## Executive Summary
- **Duration**: single session (estimated: <0.5 day, Low — within estimate)
- **Scope**: Unchanged from plan — three additive `README.md` edits (Features
  section + dedupe subsection + interactive-tree expansion). No `.go` change.
- **Outcome**: Success. The README now sells the two undersold differentiators
  (block-level filesystem dedupe; the change-tracking tree viewer) while every
  claim stays grep-verified against shipped source — meeting task 17's honesty
  bar. SC1–SC5 all met; TC-1…TC-7 all PASS.

## Variance Analysis
### Time and Effort
- **Estimated**: <0.5 day total (chore — phases a/d/e/f/g/j only).
  - Planning (a/d/e): bulk of the estimate (verify-then-pin).
  - Implementation (f): minutes (three prose edits).
  - Testing (g): minutes (static greps + build/test).
- **Actual**: Single session, comfortably under the half-day estimate. The
  source-of-truth verification was front-loaded into the d-plan, so exec and
  testing were mechanical.
- **Variance**: Favourable (under). Same pattern as tasks 16/17 — a chore whose
  risk is entirely accuracy, pinned in planning, executes as a checklist.

### Scope Changes
- **Additions**: None.
- **Removals**: `--exclusive` was dropped from the sell copy during the d-plan
  review (it is path-scoped, not a flat filter — would have misled). SC2 was
  refined from "enumerate the flags inline" to "sell by category + point to
  `dcfh dupes help`", matching the README's table=purpose/help=flags pattern.
  Both happened in planning, before any edit — net simplification, not descope
  of delivered value.
- **Impact**: None on timeline; improved accuracy and kept the README's tone.

### Quality Metrics
- **Test Coverage**: All five success criteria covered by TC-1…TC-7; critical
  paths (TC-2/3/4/7) 100% clean.
- **Defect Rate**: One in-exec defect, caught before commit — the first draft of
  Edit C mislabelled the `c/f/a/m/d/n` sort keys ("changes, files, size, mtime,
  dirs, name"). Reading `tui/sort.go` showed the real metrics
  (changed-bytes / changed-files / added / modified / deleted / name) and it was
  corrected before validation. Zero defects reached the testing phase or commit.
- **Performance**: N/A — docs-only, no runtime change.

## What Went Well
- **Verify-at-exec caught the one error.** Re-reading `sort.go` at edit time
  (rather than trusting the plan's pin) surfaced the wrong sort-key labels — the
  exact "sell-copy drifts ahead of code" risk the a-plan flagged as Risk 1.
- **Front-loaded source pinning made exec mechanical.** Every claim already had
  a `file:line` pin from the d-plan, so the three edits were transcription +
  re-verification, not discovery.
- **Plan review earned its keep again.** It removed `--exclusive`, corrected the
  "no-op" → "skips and reports device" wording, and de-duplicated Edit A vs B/C
  before any prose was written.
- **The claim-to-source grep table (g-phase) is the right test shape** for a
  keep-the-docs-honest chore: accuracy regressions surface mechanically.

## What Could Be Improved
- **The sort-key slip should have been caught in planning, not exec.** The
  d-plan pinned the footer string and glyphs but not the sort *metric* labels;
  had it pinned `sort.go:15–45` explicitly, the first draft would have been
  right. Minor — exec verification caught it — but a tighter pin would have
  avoided the rework.
- **Security-review line cap, again.** Same docs-heavy-changeset shape as task
  17, but here it stayed under the cap (41 production-weighted lines, because the
  bulk of this changeset is the prose README plus plan files, not thousands of
  moved doc lines) so the subagent *did* run both phases (no findings). The
  task-17 BACKLOG item to add a `docs/`/`*.md` exclude glob remains the right
  fix and is still open.

## Key Learnings
### Technical Insights
- The interactive-tree footer string (`render.go:154`) is the canonical key
  list, but the *meaning* of each sort key lives in `sort.go` — documenting the
  viewer needs both pins, not just the footer.
- `--fs-dedupe`'s unsupported-filesystem path **skips and reports** the device
  (`dupes.go:281`), it does not silently no-op — a distinction that matters in
  user-facing copy and was nearly miswritten.

### Process Learnings
- For documentation chores, pin the *semantics* source (what a key/flag means),
  not only the *surface* source (where it is listed). Surface pins prove a token
  exists; semantic pins prove the description is right.
- Estimation for accuracy-bounded chores is reliable when verification is moved
  into planning: a/d/e absorb the risk, f/g stay short.

### Risk Mitigation Strategies
- Treating the existing README as untrusted-until-verified (the task-17 habit)
  generalises: re-grep every claim at exec time even when the plan already pinned
  it, because the plan can pin the wrong line.

## Recommendations
### Process Improvements
- When a plan documents a keyed/flagged UI surface, require a pin to the
  *definition* site (enum/switch), not just the help/footer string.

### Tool and Technique Recommendations
- Keep the claim-to-source grep table as the standard g-phase artefact for
  docs-accuracy tasks; it doubles as a regression guard for future doc edits.

### Future Work
- No new BACKLOG item generated by this task. The task-17 follow-up — *Add
  docs/Markdown globs to `security.review.max-lines-exclude-paths`* (BACKLOG,
  Low) — remains the open documentation-tooling item.

## Status
**Status**: Finished
**Next Action**: Task complete
**Blockers**: None identified
**Completion Date**: 2026-06-10
**Sign-off**: Matt Keenan (with Claude Opus 4.8)

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Archived Materials
- Planning: `a-task-plan.md`, `d-implementation-plan.md`, `e-testing-plan.md`
- Execution: `f-implementation-exec.md` (commit `54c9a5d`),
  `g-testing-exec.md` (commit `a629eda`)
- Deliverable: `README.md` (Features section + dedupe subsection +
  interactive-tree expansion)
- Security reviews: both exec phases recorded **no findings**
  (`cwf-security-reviewer-changeset`).
