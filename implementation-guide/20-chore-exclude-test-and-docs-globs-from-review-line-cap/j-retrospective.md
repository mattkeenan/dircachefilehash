# Exclude test and docs globs from review line cap - Retrospective
**Task**: 20 (chore)

## Task Reference
- **Task ID**: internal-20
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/20-exclude-test-and-docs-globs-from-review-line-cap
- **Template Version**: 2.1
- **Retrospective Date**: 2026-06-11

## Executive Summary
- **Duration**: <0.5 day across two sessions (compaction mid-flow); estimated <0.5 day — within estimate.
- **Scope**: Exactly as planned — three globs (`**/*_test.go`, `docs/**`, `*.md`) appended to `security.review.max-lines-exclude-paths`; `.cwf/**` deliberately left out of scope. No additions, no removals.
- **Outcome**: Success. Restores the exec-phase `cwf-security-reviewer-changeset` gate on test- and docs-heavy tasks (which previously tripped the 500-line cap and skipped the subagent). 5/5 test cases PASS, 0 defects, security review **no findings** both exec phases.

## Variance Analysis
### Time and Effort
- **Estimated** (chore — b/c/h/i skipped): Planning <0.1d, Implementation-plan <0.1d, Testing-plan <0.1d, Implementation-exec <0.1d, Testing-exec <0.1d.
- **Actual**: Each phase completed in a single working pass; the only wall-clock spread was a context compaction between the exec-phase pause-for-review and resumption. No phase overran.
- **Variance**: ~0%. A single-key config change behaved exactly as a Low-complexity chore should.

### Scope Changes
- **Additions**: None.
- **Removals**: None. `.cwf/**`/`.claude/**` excludes were never in scope (the Task 5/9/14 caveat) and remain a separate open BACKLOG item.
- **Impact**: None — the plan and the delivered change are identical.

### Quality Metrics
- **Test Coverage**: 5/5 functional TCs PASS (4 criticals clean); every success criterion maps to ≥1 passing TC. No Go runtime code changed, so no unit-test coverage delta.
- **Defect Rate**: 0 defects in testing; 0 post-implementation.
- **Mechanism proof**: TC-4 measured the discount at exactly 830 lines, equal to the excluded files' own added+deleted total — git's `:(glob,exclude)` engine discounts precisely the intended paths and nothing else.

## What Went Well
- **Planning verified glob semantics empirically before committing.** `git ls-files -- ':(glob)…'` runs in the d-plan pinned `*` vs `**` boundary behaviour, so implementation was a checklist and the security-critical `*.md`-is-root-only choice was evidence-backed, not assumed.
- **The change is self-demonstrating.** Task 20's own exec-phase review ran the subagent at 0 production-weighted lines because the changed files sit under the pre-existing `implementation-guide/**` exclude — a live demonstration that the gate runs rather than skips, which is exactly the behaviour the task restores for the broader test/docs case.
- **Security boundary independently corroborated.** Both exec-phase `cwf-security-reviewer-changeset` passes reached the same conclusion the plan authors did: root-only `*.md` keeps the vendored prompt-injection surface cap-counted, and the change is fail-safe in direction (worst case = *more* subagent invocation, never hidden content).
- **Fail-safe-by-construction.** Any unmatched/unconfigured path still counts as production, so the cap fires earlier, never later; a malformed glob makes git fatal → helper exit 1, not a silent pass.

## What Could Be Improved
- **Exec checkpoint paused mid-flow then resumed across a compaction.** The implementation-exec work was completed and held for review before its Step 9 checkpoint commit; the commit landed in a later session. No defect resulted, but the pending-commit state had to be re-established after compaction. Minor process friction, not a quality issue.
- **`docs/**` is content-agnostic** — it discounts everything under `docs/`, not just Markdown. Accepted consciously (the directory's purpose is docs), but a future non-doc file dropped there would be silently discounted from the cap count. Recorded below as a standing watch-item.

## Key Learnings
### Technical Insights
- **Git pathspec `*` vs `**` is a load-bearing security boundary here.** A bare `*` does not cross `/`, so `:(glob)*.md` is root-only; `**/*.md` would sweep the 287 vendored `.cwf/**`/`.claude/**`/`.cwf-*` mirror prompt files and let edits to the prompt surface ride under the cap. The literal `*.md` is the scope-correct choice *because* it is root-only.
- **A config change to a security gate can be reviewed by the very gate it tunes.** The exec-phase review of this change is the downstream consumer of the cap config; the change strictly increases that gate's coverage (more invocation, never less), so the meta-loop is benign and self-reinforcing.

### Process Learnings
- **Empirical verification in the design phase collapses execution risk.** Pinning glob behaviour with real `git ls-files` output (not reasoning about pathspec docs) made the implementation a single edit with no surprises — the same pattern that beat the estimate on the recent docs tasks.
- **Mechanism tests beat assertion tests.** TC-4 didn't just assert "the cap is lower" — it proved the discount equals the excluded files' own line total via git's own engine, independent of the helper. That is the test that would catch a future regression in `count_production_lines`.

### Risk Mitigation Strategies
- The two medium-priority risks from a-task-plan (over-broad `*.md`/`docs/**` discounting a security-relevant change; `**/*_test.go` masking prod logic in test files) were both retired by the same invariant: **the full changeset is always emitted to the reviewer; the glob only relaxes the cap count, never what the subagent reads.** Net effect is strictly more review coverage.

## Recommendations
### Process Improvements
- For "pause-for-review" exec phases, land the checkpoint commit before yielding context where practical, or record the exact pending-stage state, to avoid re-establishing it after a compaction.

### Tool and Technique Recommendations
- Keep favouring mechanism-level verification (prove the engine discounts the right bytes) over surface assertions for config that feeds a deterministic gate.

### Future Work
- **`.cwf/**`/`.claude/**` exclude decision remains open by design** (Task 5/9/14 BACKLOG item): excluding the vendored CWF surface would flip pure-upgrade tasks from "error→skipped" to "subagent on the full vendored delta" at real token cost. Reserve any such exclude for *mixed* tasks; pure upgrades may prefer the cheap deterministic `cwf-manage validate` gate. Left standing.
- **Watch `docs/**` for executable fixtures.** If `docs/` ever accrues non-doc files (e.g. a `.go` fixture) whose review depends on the cap count, revisit the content-agnostic discount.

## Status
**Status**: Finished
**Next Action**: Task complete
**Blockers**: None identified
**Completion Date**: 2026-06-11
**Sign-off**: Matt Keenan (with Claude Code)

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Archived Materials
- Plan: `a-task-plan.md` (`08a2198`), `d-implementation-plan.md` (`babb7f4`), `e-testing-plan.md` (`1d754b7`)
- Implementation: `f-implementation-exec.md` (`5e20529`) — config edit + exec-phase security review (no findings)
- Testing: `g-testing-exec.md` (`4e016bf`) — TC-1…TC-5 all PASS + testing-exec security review (no findings)
- Change: `implementation-guide/cwf-project.json` — `+["**/*_test.go", "docs/**", "*.md"]` on `security.review.max-lines-exclude-paths`
- Resolves BACKLOG: "Add test-file globs…" (Task 12), "Add docs/Markdown globs…" (Task 17)
