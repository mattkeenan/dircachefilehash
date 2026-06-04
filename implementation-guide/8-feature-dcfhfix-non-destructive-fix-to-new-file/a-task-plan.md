# dcfhfix non-destructive fix-to-new-file - Plan
**Task**: 8 (feature)

## Task Reference
- **Task ID**: internal-8
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/8-dcfhfix-non-destructive-fix-to-new-file
- **Baseline Commit**: 4598a81b2f76d1838462d249952b0ef95ecf56b9
- **Template Version**: 2.1

## Goal
Make `dcfhfix` write repairs to a separate fixed index file by default — leaving the original index byte-identical — and require an explicit, force-gated flag for the legacy in-place behaviour.

## Success Criteria
- [ ] By default, every `dcfhfix` write path leaves the original index file byte-identical and produces a distinct fixed output file at a predictable, collision-safe path.
- [ ] In-place mutation of the original happens only when the user passes an explicit opt-in flag, gated behind `--force`; without it the original path is never renamed or replaced.
- [ ] The default path prints where the fixed file was written and that the original is preserved; the in-place path prints a prominent destructive-action warning.
- [ ] Help text and `cmd/dcfhfix/DESIGN.md` document the new default and the opt-in, with no stale "in-place by default" references.
- [ ] All existing `dcfhfix` tests pass and new tests cover: default → new file + original untouched, opt-in → in-place, and the force-gating refusal.

## Original Estimate
**Effort**: 1-2 days
**Complexity**: Medium
**Dependencies**: Soft alignment with the "Fix-primitive restructure" backlog item (non-destructive output is a property of `FixRequest` semantics); no hard external dependency.

## Major Milestones
1. **Reconcile current safety model**: Requirements pin down how fix-to-new-file relates to the *already existing* `--backup`/`fixes` stack and atomic-rename — complement or replace — and confirm the real command surface.
2. **Default output behaviour**: Design + implement the new-file default across all write paths with deterministic naming and original preservation.
3. **Gated in-place opt-in**: Add the `--force`-gated in-place flag with warnings; refuse in-place without the gate.
4. **Docs + tests**: Update help/DESIGN; add coverage for default, opt-in, and refusal paths.

## Risk Assessment
### High Priority Risks
- **Stale premise / overlapping safety mechanisms**: The tool already writes atomically (temp + `os.Rename`) and already backs up by default (`--backup=true`, with `fixes pop/discard/clear`). Building fix-to-new-file naively risks two overlapping, confusing safety models.
  - **Mitigation**: In requirements, explicitly decide whether fix-to-new-file replaces, complements, or supersedes the backup stack; document the resulting single coherent UX before any code.
- **Command-surface drift**: Project docs describe `header`/`entry`/`scan`, but the actual command table is `header`/`entry`/`fixes`. Speccing a non-existent `scan` write path would waste effort.
  - **Mitigation**: Confirm the actual write-command surface during requirements; scope only the real paths.

### Medium Priority Risks
- **Output-path collisions on re-runs**: A fixed `<name>.fixed.idx` naming scheme collides when run twice or when the target already exists.
  - **Mitigation**: Define deterministic, collision-safe naming (e.g. refuse-or-suffix) in design; add a re-run test.
- **Backward-compatibility break for scripted callers**: Anything relying on in-place-by-default changes behaviour.
  - **Mitigation**: Intentional feature change — capture clear messaging and a rollout note; the opt-in flag preserves the old behaviour for those who need it.

## Dependencies
- Soft alignment with backlog item "Phase 1b-2: Fix primitive + dcfhfix restructure" — fix-to-new-file is a natural property of `FixRequest` semantics; decide in requirements whether to land independently or fold in.
- Reusable existing helpers: `copyFile()` / `createBackup()` in `cmd/dcfhfix/main.go`; `MetaStore.copyFileWithMetadata()` in `pkg/recovery.go`.

## Constraints
- Unix-like, Go 1.24.3; must preserve the existing atomic-rename safety guarantee.
- Must not regress the existing `--backup` / `fixes` stack behaviour.
- British spelling in all docs and user-facing text; gosec security gate (G304/G703 write-path suppressions) still applies to any new write code.

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [ ] **Time**: Will this take >1 week? No — estimated 1-2 days.
- [ ] **People**: Does this need >2 people? No — single developer.
- [ ] **Complexity**: Does this involve 3+ distinct concerns? Borderline (CLI flags, write-path destination, docs/tests) but tightly coupled around one behaviour change.
- [ ] **Risk**: Are there high-risk components needing isolation? No — additive flag + destination logic.
- [ ] **Independence**: Can parts be worked on separately? No — the paths share one destination-selection change.

**Decision**: Do not decompose. Single focused feature; 0-1 signals firmly triggered.

## Status
**Status**: Finished
**Next Action**: /cwf-requirements-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
*To be filled upon completion*

## Lessons Learned
*To be captured during implementation*
