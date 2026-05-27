# Move FileSize/ByteSize to int64 in v4 and core - Retrospective
**Task**: 4 (chore)

## Task Reference
- **Task ID**: internal-4
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/4-move-filesize-bytesize-to-int64-in-v4-and-core
- **Template Version**: 2.1
- **Retrospective Date**: 2026-05-27

## Executive Summary
- **Duration**: ~0.5 day effective (estimate: 0.5–1 day; variance: within estimate).
- **Scope**: Delivered exactly as planned — flip `format.ByteSize` `uint64`→`int64`,
  follow the compiler-driven ripple, retire the 7 Task-3.3 size G115 suppressions by
  removing the conversions, add a negative-size corruption floor. No on-disk format
  change (v4 stays v4). `fsdedupe` byte totals stayed out of scope by design.
- **Outcome**: Success. All four success criteria verified end-to-end; 0 G115
  whole-tree; full suite + race green; 0 escaped defects.

## Variance Analysis
### Time and Effort
- **Estimated** (chore phase set a/d/e/f/g/j): 0.5–1 day total.
- **Actual**: Planning (a/d/e) committed `ec29511`/`6366c78`/`6457257`; exec
  (f/g) `935224d`/`70ded40`. Effective active effort ~0.5 day.
- **Variance**: ~0%, within estimate. The four-reviewer d-plan pass front-loaded
  the discovery cost, so exec was near-mechanical.

### Scope Changes
- **Additions** (all small, all forced by the flip — none missed in planning that
  caused rework, they were discovered and handled in-phase):
  - `ParseSizeBound` leading-sign guard — `strconv.ParseInt` accepts `+1`/`-1`
    where `ParseUint` rejected them; guard preserves the documented unsigned-
    magnitude grammar.
  - `validateFileSizeBounds` extraction (recovery.go) — the inline `< 0` branch
    pushed `validateEntryLogical` to cyclop 21; extracting both size checks keeps
    the distinct fail-closed message and drops it back under the limit.
  - TC-2 `TestRoundTrip_V4_LargePositiveFileSize` and TC-4 negative-size case.
  - `dedupeDefaultMinSize` (cmd/dcfh) flipped `uint64`→`int64` (filter plumbing).
- **Removals**: none. `fsdedupe` byte totals + dupes.go formatting casts were
  out of scope by design (distinct `uint64` type, own JSON contract) — a
  documented boundary, not a descope.
- **Impact**: negligible on timeline; the additions were single-line/single-helper.

### Quality Metrics
- **Test Coverage**: `pkg/format` 58.8% (held at Task-3 baseline), `pkg` 62.5%;
  new-code arms (negative-size floor, large-positive round-trip, signed parser)
  covered by TC-4/TC-2/TC-6.
- **Defect Rate**: 0 escaped. The only in-flight surprises were the two
  preserve-behaviour guards, both caught at their own gate (test + lint) within
  the f-phase.
- **Performance**: zero-copy load preserved; `int64`/`uint64` both 8 bytes
  host-order — no layout or load-path cost.

## What Went Well
- **The four-reviewer d-plan amendment paid for itself.** The compiler ripple
  matched the amended plan almost exactly (27 files); the `needsHash` G115-trap
  was pre-identified and routed around conversion-free, and the three sites the
  plan flagged as "follows transparently" (`entry_serialiser`, `materialiseEntryInfo`,
  the iterator interface assignments) did exactly that — no edits.
- **Regression-first testing held.** Existing v4/v3 goldens carried SC1/SC3 with
  no new fixtures; only two small edge-case tests were added.
- **SC2 achieved by removal, not relocation.** All 7 named suppressions deleted
  with their casts; 0 G115 whole-tree; `--new` clean. Retained "file size"
  suppressions are mmap-offset arithmetic and the out-of-scope fsdedupe totals.

## What Could Be Improved
- **Raw plans under-estimate a type-flip blast radius.** The pre-review d-plan
  enumerated ~6 implementors; the four reviewers found the 7th/8th, the
  `ExactSize` consumer, the `validated_entry` parse, and the `needsHash` trap.
  The review caught them — but the lesson is that a signedness/width flip needs a
  compiler-driven enumeration step *in planning*, not just a manual file list.
- **Two second-order interactions weren't anticipated**: a parser grammar change
  (ParseInt accepts signs) and a cyclomatic-complexity budget already at its limit.
  Both are the kind of thing only the build/lint gate surfaces.

## Key Learnings
### Technical Insights
- A Go type **alias** (`=`) turns a width/signedness change into a compiler-driven
  audit for every alias-typed site — but the **non-alias interface boundaries**
  (`FileSize() (uint64, error)`) are exactly where the manual work concentrates,
  and they cascade (interface → all implementors → all callers).
- `strconv.ParseInt` is not a drop-in for `ParseUint`: it accepts a leading sign.
  A signedness flip on any parser surface must re-verify the accepted grammar.
- Adding a guard branch can trip a cyclomatic-complexity budget already at the
  limit; extract-to-helper is the clean fix and improves readability.

### Process Learnings
- The plan-review subagents are the highest-leverage step for a mechanical-looking
  type change — the change *looks* like one line, which is exactly when the ripple
  is under-counted.
- CWF's security-changeset classifier correctly reported `empty changeset` for an
  all-Go-source task; the semantic security work (FR4(e) negative-size floor) was
  done at the d-plan security pass, which is the right place for it.

### Risk Mitigation Strategies
- a-plan R1 (signed reinterpretation of legacy bytes) was mitigated exactly as
  planned: the negative-size validator floor + the v2/v3 goldens proving honest
  data still decodes. R2 (non-alias consumers) was handled by "build green is the
  coverage". R3 (stale suppression) held — scope was kept strictly to FileSize
  bridges; fsdedupe left untouched.

## Recommendations
### Process Improvements
- For width/signedness flips, add a planning sub-step: flip the type on a scratch
  build and capture the compiler error list *before* writing the d-plan file list.
  This would have produced the four-reviewer findings up front.

### Future Work
- No new backlog items from this task. The 3 pre-existing whole-tree lint findings
  (cyclop ×2, unparam ×1) remain covered by the existing backlog item "Clear
  pre-existing full-tree golangci-lint failures"; none were introduced here.

## Status
**Status**: Finished
**Next Action**: Task complete
**Blockers**: None identified
**Completion Date**: 2026-05-27
**Sign-off**: Matt Keenan / Claude Opus 4.7

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Archived Materials
- Planning: `a-task-plan.md` (`ec29511`), `d-implementation-plan.md` (`6366c78`),
  `e-testing-plan.md` (`6457257`).
- Execution: `f-implementation-exec.md` (`935224d`), `g-testing-exec.md` (`70ded40`).
- Closes BACKLOG item "Move FileSize/ByteSize to int64 in v4 + core" (the stale
  "subtask 3.4" naming — landed as top-level Task 4).
