# Re-enable checkptr in the race gate - Plan
**Task**: 10 (bugfix)

## Task Reference
- **Task ID**: internal-10
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: bugfix/10-reenable-checkptr-race-gate
- **Baseline Commit**: 6d6f4a4c2430ca4a14809c65f962ce4e9bd96fc4
- **Template Version**: 2.1

## Goal
Make the zero-copy mmap accessors checkptr-clean so the pre-commit `-race` gate
runs with checkptr **on** (the default), restoring full race+checkptr coverage —
or, only with a very strong, cited justification, document precisely why a clean
fix is not viable and retain the suppression with that rationale.

## Success Criteria
- [ ] `go test -race -short ./...` passes with checkptr **enabled** (no
  `-gcflags=all=-d=checkptr=0`) across every package.
- [ ] `.githooks/pre-commit` no longer disables checkptr (the
  `-d=checkptr=0` flag is removed; the gate runs default checkptr).
- [ ] Zero-copy preserved: no new per-access allocation or content copy in the
  hot accessor path (verified by allocation count / benchmark, not just "looks
  fine").
- [ ] On-disk format unchanged and all existing tests pass — no behavioural
  change to the index read path.
- [ ] The stale [[project_race_checkptr_disabled]] memory and any CLAUDE.md
  wording that says "checkptr is deliberately disabled" are corrected.
- [ ] *(Option-2 fallback only)* If a clean fix is proven non-viable, a written
  rationale citing the specific runtime/spec mechanism lives in CLAUDE.md + the
  Very High backlog item is closed with that rationale; the flag is retained
  with an explanatory comment.

## Original Estimate
**Effort**: 1-2 days
**Complexity**: Medium (small surface, but unsafe-pointer + checkptr subtleties)
**Dependencies**: None external; needs understanding of how `mmapIndexFile.Data`
is constructed (mmap → `unsafe.Slice`) and the `Entry` struct+trailing-path layout.

## Major Milestones
1. **Enumerate**: Run plain `go test -race ./...` and list *every* checkptr-flagged
   site, not just the first. Known: `pkg/binary_entry.go:54` (`GetBinaryEntry`),
   `pkg/format/entry.go` `RelativePath` (~L120) and `calculatePathLength` (~L131).
2. **Spike the decision point**: Rewrite one accessor (`GetBinaryEntry`) using
   slice-index addressing / `unsafe.Add` instead of the `uintptr` round-trip and
   confirm it clears checkptr against the real mmap `Data` slice. This milestone
   *is* the option-1-vs-option-2 gate: if even slice-index addressing over the
   mmap region trips checkptr, escalate to the option-2 discussion before more work.
3. **Apply**: Roll the proven pattern to the remaining sites, including the
   trailing-path reads past `sizeof(Entry)` (the harder case in `RelativePath`).
4. **Re-arm the gate**: Remove `-d=checkptr=0` from `.githooks/pre-commit`; full
   suite green under default checkptr.
5. **Document**: Update/obsolete the disabled-checkptr memory and CLAUDE.md
   wording; retire the backlog item via the implementing task.

## Risk Assessment
### High Priority Risks
- **Trailing-path reads may be inherently checkptr-hostile**: `RelativePath`
  walks bytes *after* `sizeof(Entry)` within the same mmap region — checkptr may
  reject any pointer that crosses the nominal struct boundary.
  - **Mitigation**: Spike this specific case early (milestone 2/3). It is the
    most likely trigger for the option-2 path; decide before broad edits.
- **mmap memory is not a Go-tracked allocation**: `Data` is built via
  `unsafe.Slice` over an `mmap` region; checkptr's allocation-base lookup may not
  recognise it, so even `&Data[i]` addressing could fail.
  - **Mitigation**: Verify empirically in the spike how `Data` is constructed and
    whether slice-index addressing satisfies checkptr for that memory.

### Medium Priority Risks
- **Performance regression**: A "clean" rewrite that forces copies would defeat
  the zero-copy design.
  - **Mitigation**: Allocation-count / benchmark check is an explicit success
    criterion; reject any fix that copies on the hot path.
- **Toolchain sensitivity**: checkptr behaviour can shift between Go versions
  (currently go1.26 locally).
  - **Mitigation**: Note the toolchain in the design; the gate runs the local
    toolchain, so target that.

## Dependencies
- No external/team dependencies.
- Technical: `pkg/format` (on-disk `Entry` layout owner), `pkg/binary_entry.go`
  (`binaryEntryRef`/`GetBinaryEntry`), `mmapIndexFile.Data` construction.

## Constraints
- Must preserve zero-copy semantics (no copies/allocs in hot accessor path).
- Must not change the on-disk index format or any externally visible behaviour.
- Must not weaken the gate by any other means (no test skips, no broader excludes).
- British spelling in prose/comments.

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [ ] **Time**: Will this take >1 week? No — estimated 1-2 days.
- [ ] **People**: Does this need >2 people? No — single contributor.
- [ ] **Complexity**: 3+ distinct concerns? No — one concern (checkptr-clean
  unsafe accessors) across a small file set.
- [ ] **Risk**: High-risk components needing isolation? Contained to the spike;
  handled within the task via the milestone-2 gate, not a subtask.
- [ ] **Independence**: Can parts be worked separately? Not meaningfully — the
  accessors share one pattern and one verification (the re-armed gate).

**Conclusion**: No decomposition. Focused single-concern bugfix; keep as one task.

## Status
**Status**: Finished
**Next Action**: /cwf-requirements-plan 10
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All success criteria met. `go test -race ./...` passes with checkptr ON across every
package; `.githooks/pre-commit` no longer disables checkptr; zero-copy preserved
(0 allocs/op); on-disk format and all tests unchanged. Option 1 (fix) delivered — the
spike confirmed a clean fix, so the option-2 fallback was not invoked. CLAUDE.md had no
checkptr prose to correct; the auto-memory rewrite is a post-merge action. Completed
within the 1-2 day estimate. See j-retrospective.md.

## Lessons Learned
The pre-task spike retired the highest-risk unknown (whether the mmap/heap accessors
could be made checkptr-clean without copies) before planning, which is why the estimate
held. See j-retrospective.md for the full set.
