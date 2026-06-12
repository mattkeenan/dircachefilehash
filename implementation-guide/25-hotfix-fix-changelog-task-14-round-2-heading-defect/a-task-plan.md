# Fix CHANGELOG Task 14 round 2 heading defect - Plan
**Task**: 25 (hotfix)

## Task Reference
- **Task ID**: internal-25
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: hotfix/25-fix-changelog-task-14-round-2-heading-defect
- **Baseline Commit**: 7dbced1ae86ce0e0a7a4cbeb8d568450f0329a57
- **Template Version**: 2.1

## Goal
Restore `backlog-manager validate` to green by making the round-2 heading parse as a real Task 14 entry (move `(round 2)` after the colon) and repairing the misplaced `### Status:`/`### Impact:` subsections so the round-2 entry carries its own metadata and `## Task 15` no longer holds duplicate stray copies.

## Success Criteria
- [ ] `.cwf/scripts/command-helpers/backlog-manager validate --all` exits 0 with no `CHANGELOG-003`/`CHANGELOG-002` (or any) error.
- [ ] The round-2 heading is renamed to `## Task 14: Upgrade CWF subtree to v1.1.185 (round 2)` so it matches the entry regex (colon immediately after the digits).
- [ ] The renamed round-2 entry has exactly one `### Status:` and one `### Impact:` (the v1.1.185 text), ordered before its `### Changes`.
- [ ] `## Task 15` (CHANGELOG.md:176) retains exactly its own single `### Status:`/`### Impact:` pair (178–179); the two stray v1.1.185 duplicate pairs (180–183) are removed.
- [ ] No other CHANGELOG/BACKLOG entry's content is altered (verified by `git diff` being confined to the two affected entries).

## Note on root cause (corrected during planning)
The defect is **not** primarily a missing Status/Impact "anchor"; it is that `## Task 14 (round 2):` fails the entry regex (`Backlog.pm:231` requires the colon right after the number), so the heading + its body are absorbed into Task 15, producing a duplicate `Changes` subsection (the actual `CHANGELOG-003` trigger). The heading rename is the load-bearing edit. Verified empirically — see d-implementation-plan.md "Empirical Verification".

## Original Estimate
**Effort**: <0.5 day
**Complexity**: Low
**Dependencies**: None

## Major Milestones
1. **Confirm fix mechanism**: Determine whether the relocation can be done via the `backlog-manager` helper or requires a guarded raw edit (the helper has no Status/Impact-relocation subcommand) — settled in the implementation plan.
2. **Apply repair**: Remove the two stray pairs from Task 15; add one correct pair under Task 14 (round 2).
3. **Verify**: `validate --all` green; `git diff` scoped to the two entries.

## Risk Assessment
### High Priority Risks
- **Risk 1**: No helper subcommand performs Status/Impact relocation, so a raw `Edit` of CHANGELOG.md may be unavoidable — bypassing the helper's heading-tree enforcement.
  - **Mitigation**: Make the minimal line-level edit, then immediately re-run `validate --all` as the acceptance gate; the validator is the same contract the helper enforces, so a green validate confirms the raw edit stayed in-format.

### Medium Priority Risks
- **Risk 2**: The two stray pairs are byte-identical, making it easy to delete the wrong instance or leave one behind.
  - **Mitigation**: Anchor edits on surrounding unique context (the Task 15 Impact at 179 vs the Task 14 round-2 heading at 196); confirm via `git diff` line count (−4 under Task 15, +2 under round 2).

## Dependencies
- None — documentation-only change, no product code, no build.

## Constraints
- Markdown-only change to `CHANGELOG.md`; must conform to the CWF heading-tree contract enforced by `backlog-manager validate`.
- Pre-existing defect (inherited in baseline `7dbced1a`); this task is the isolated hotfix for it.

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [ ] **Time**: Will this take >1 week? No.
- [ ] **People**: Does this need >2 people? No.
- [ ] **Complexity**: Does this involve 3+ distinct concerns? No — one file, one defect.
- [ ] **Risk**: Are there high-risk components that need isolation? No.
- [ ] **Independence**: Can parts be worked on separately? No.

No signals triggered — single atomic hotfix, no decomposition.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All five success criteria met. `validate --all` exits 0; the round-2 heading renamed to `## Task 14: Upgrade CWF subtree to v1.1.185 (round 2)`; the renamed entry carries exactly one Status/Impact pair before its Changes; Task 15 retains only its own pair (two stray pairs removed); the diff is confined to the two entries (4 ins / 5 del). Effort matched the <0.5-day estimate.

## Lessons Learned
The defect's symptom (`CHANGELOG-003` at the round-2 entry) and its cause (the heading one entry earlier failing the entry regex) were one entry apart — diagnosis, not the edit, was the work. Correcting the root-cause framing during planning (heading-parse failure, not a missing anchor) is what kept the fix minimal. See j-retrospective.md for the full write-up.
