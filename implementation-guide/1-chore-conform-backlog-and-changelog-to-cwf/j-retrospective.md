# Conform backlog and changelog to CWF format - Retrospective
**Task**: 1 (chore)

## Task Reference
- **Task ID**: internal-1
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/1-conform-backlog-and-changelog-to-cwf
- **Template Version**: 2.1
- **Retrospective Date**: 2026-05-23

## Executive Summary
- **Duration**: ~1 session (estimated: 0.5–1 day; within estimate).
- **Scope**: As planned. `BACKLOG.md` converted to the CWF heading-tree schema in place; version-based `CHANGELOG.md` archived to `docs/changelog-old.md` and replaced with a fresh CWF by-task changelog. Debian packaging deliberately untouched. One follow-up backlog item added; one stale Go reference left as documented out-of-scope.
- **Outcome**: Success. Both files pass `validate --all --strict`; all 15 BACKLOG entries are now tool-visible (was 0); archive is byte-identical to the original.

## Variance Analysis
### Time and Effort
- **Estimated** (a-task-plan): ~0.5–1 day, Low–Medium complexity. Chore step set: a, d, e, f, g, j.
- **Actual**: Within estimate. Planning (a/d/e) was the larger share because the CHANGELOG paradigm decision and the 15-vs-16 entry-count defect were resolved at plan-review time; exec (f/g) was mechanical once the gates were defined.
- **Variance**: Negligible. The plan-review investment front-loaded the risk and made exec low-friction.

### Scope Changes
- **Additions**:
  - First `## Task 1:` CHANGELOG entry written at retrospective (documents this task; seeds the new by-task changelog).
  - Follow-up BACKLOG item "Fix stale 'see CHANGELOG' reference in pkg/ignore.go" (chore, Low) added via `backlog-manager add`.
- **Removals**: The planned `go build` gate and any edit to `pkg/ignore.go` were removed during plan review — grep proved the "see CHANGELOG" target is in neither the old nor new changelog, so repointing would have been misleading. Kept the task free of Go changes.
- **Impact**: Net simplification. No timeline impact.

### Quality Metrics
- **Test Coverage**: Contract-coverage (not line-coverage — docs chore). All 7 e-testing-plan cases PASS; all 4 coverage targets met (contract, conversion completeness, content preservation, blast radius).
- **Defect Rate**: Zero post-conversion defects. One documentation typo (`chore ×5` → `×6` in f-exec prose) caught and fixed during testing-exec. One plan defect (15 vs 16 entries) caught and fixed during plan review, before any edit.
- **Performance**: N/A (no runtime surface).

## What Went Well
- **Plan review caught the load-bearing defect**: the robustness reviewer identified that the `## Entry: <title>` template stub made the headline gate (`list` = 16) unsatisfiable; the real target was 15. Fixing this in the plan, not in exec, avoided a failed gate.
- **The right oracle was chosen**: gating on `list` count rather than `validate` exit code — `validate` is a false positive on zero recognised entries.
- **Targeted Edits over file rewrite**: 16 unique-match Edits (1 header + 15 entries) kept all entry bodies verbatim with zero risk of silent body corruption; title-identity diff confirmed 0 additions / 0 deletions.
- **Archive integrity proven, not assumed**: blob-hash equality (`73e6e7b6…`) is a stronger guarantee than git's rename heuristic.

## What Could Be Improved
- **The earlier `git add -A` for a measurement** swept unrelated future-phase template files into staging and prompted a (rejected) `git reset`. Lesson: to read a blob hash of an unstaged file, prefer `git hash-object <path>` over staging the whole tree.
- **f-exec prose count typo** (`chore ×5`) slipped through until testing-exec recomputed the breakdown. The per-entry table was correct; the summary line was hand-totalled. Prefer deriving such counts from a command, not by hand.

## Key Learnings
### Technical Insights
- A validator that splits on a specific heading silently ignores content under the wrong heading. "Passes validation" must be paired with "tooling sees N entries" before declaring conformance.
- `git mv` then writing new content to the original path breaks `R100` rename detection — the source path is no longer deleted. Use blob-hash equality to assert an archive is untouched.

### Process Learnings
- For a bespoke migration with no helper migration path, a deliberate, gated direct-edit exception (validate + count, the same contract the helper enforces) is defensible and was the lower-risk route versus deconstruct-and-re-`add`.
- Resolving the one open decision (CHANGELOG paradigm) at plan review before any edit kept exec single-pass.

### Risk Mitigation Strategies
- Count + title-identity gates (mirroring the helper's cardinality/identity checks) were the effective guard against content loss.
- Capturing the pre-move blob hash in Step 1 made the archive-integrity assertion trivial later.

## Recommendations
### Process Improvements
- When a migration's success metric is "tooling now recognises the content", make the visibility/count check the headline gate, not the validator exit code.
### Tool and Technique Recommendations
- Use `git hash-object <path>` to fingerprint working-tree files without staging.
- Add new BACKLOG/CHANGELOG content through `backlog-manager` (add/retire) now that the files conform — direct edits were a one-time migration exception only.
### Future Work
- Follow-up logged in BACKLOG: "Fix stale 'see CHANGELOG' reference in pkg/ignore.go" (chore, Low; identified in this retrospective).

## Status
**Status**: Finished
**Next Action**: Task complete — suggest merge to main
**Blockers**: None identified
**Completion Date**: 2026-05-23
**Sign-off**: Matt Keenan / Claude

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Archived Materials
- Planning: a-task-plan.md, d-implementation-plan.md, e-testing-plan.md
- Execution: f-implementation-exec.md (commit 4b09a68), g-testing-exec.md (commit c069916)
- Products: BACKLOG.md (converted), CHANGELOG.md (fresh by-task), docs/changelog-old.md (archive, blob 73e6e7b6)
