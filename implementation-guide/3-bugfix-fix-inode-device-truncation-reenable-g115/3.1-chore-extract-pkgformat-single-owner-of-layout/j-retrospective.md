# Extract pkg/format single owner of layout - Retrospective
**Task**: 3.1 (chore)

## Task Reference
- **Task ID**: internal-3.1
- **Task URL**: N/A (internal task)
- **Parent Task**: 3
- **Branch**: chore/3.1-extract-pkgformat-single-owner-of-layout
- **Template Version**: 2.1
- **Retrospective Date**: 2026-05-24

## Executive Summary
- **Duration**: ~1 focused session (estimated 2–4 days; well under estimate).
- **Scope**: Delivered the planned extraction in full — `pkg/format` is the single owner of the
  on-disk layout (vocabulary aliases, canonical `Entry`/`Header`, bounds-checked codec, version
  constants/validation); core and dcfhfix migrated onto it; both dcfhfix duplicates + the parallel
  offset table deleted. Two latent bugs were fixed in-flight (see Scope Changes).
- **Outcome**: Success. No on-disk format / width / version / behaviour change; full suite green
  (incl. `-race`); G115 not increased (63 → 52); 0 lint issues.

## Variance Analysis
### Time and Effort
- **Estimated** (chore step set: plan / implementation / testing):
  - Planning (a): part of 2–4 day envelope
  - Implementation (d+f): bulk of the envelope
  - Testing (e+g): remainder
- **Actual**:
  - Planning (a, d, e): completed in one session; the parent-level c-design did the heavy
    architectural thinking, so subtask planning was light.
  - Implementation (f): one session — the mechanical move plus two latent-bug fixes.
  - Testing (g): one session — codec bounds tests landed in f; round-trip integration tests (TC-1/2/3)
    landed in g.
- **Variance**: Substantially under the 2–4 day estimate. The estimate assumed a wide, risky
  blast radius; in practice the type-alias strategy (`type binaryEntry = format.Entry`) made the
  migration a near-zero-diff at call sites — the compiler, not manual auditing, found every site.

### Scope Changes
- **Additions** (fixed in-flight, both pre-existing latent bugs, not regressions):
  - `GetPath` latent bug: dcfhfix's deleted accessor read the path from the unused trailing
    `Path[8]` field (offset 124) instead of after the fixed struct (offset 136 = `sizeof(Entry)`),
    where the authoritative writer places it. The consolidated codec now reads at `minEntrySize`,
    matching the writer — correcting empty/short paths in dcfhfix repair output.
  - dcfhfix header 8-byte over-read: the deleted 96-byte `indexHeader` duplicate, cast to
    `[HeaderSize=104]byte` in the write path, over-read 8 bytes. Adopting the 104-byte
    `format.Header` makes the write cast exact.
- **Removals**:
  - Dead `RelativePathModern` dropped — it was the only format→core tendril (called
    `IsDebugEnabled`) and had zero callers; removing it kept `pkg/format` cycle-free.
- **Impact**: Net positive — the extraction surfaced and closed two real defects in the repair tool
  while preserving the production read/write paths unchanged.

### Quality Metrics
- **Test Coverage**: codec.go reachable bounds-check branches 100% (`NewSafeEntry` tier-1 100%; all
  fixed-field getters/setters 100%; `GetPath` 100%). Residual `validateFieldAccess`/`GetHash` error
  branches are structurally unreachable for tier-1-valid entries (documented in g-testing-exec.md).
- **Defect Rate**: 0 regressions; 2 pre-existing latent defects fixed; 0 defects found in testing.
- **Static security**: gosec G115 63 → 52 (reduction); gate intent "must not increase" satisfied.
- **Performance**: pure extraction; zero-copy host-order read path untouched; full suite incl.
  `-race` green with no timing change.

## What Went Well
- **Type-alias strategy** kept the migration a compile-time-checked, near-zero-diff change.
  Narrowing stays a compile error, so the "hidden width-coupled call site" high-risk item never
  materialised.
- **Single-owner discipline**: `pkg/format` verified cycle-free (`go list`); a grep confirms no
  layout/offset/width declarations survive outside it.
- **Tests now guard future format work**: the round-trip + version-offset + header-size-invariant
  tests are the safety net for the upcoming width change (3.3), not just this extraction.
- **"Best part is no part"** applied twice: dropped dead `RelativePathModern`; declined to write a
  test that constructs an illegal `SafeEntry` purely to touch a defensive, unreachable branch.

## What Could Be Improved
- **Methods-on-alias limitation surfaced mid-implementation**: Go forbids declaring methods on an
  alias to an out-of-package type. The plan anticipated moving methods, but the exact failure
  (filter.go `asFilterEntry`, header clean-bit methods) was discovered at compile time rather than
  designed up front. Cost was low (methods moved into `pkg/format`; `asFilterEntry` → free function;
  clean-bit exported), but naming the constraint in the design would have been cleaner.
- **Security-review changeset scope was initially misjudged**: in f I expected the
  changeset helper to diff the Go code and over-reached by reviewing the codec manually. The helper
  is *by design* scoped to the CWF automation/script surface, so Go code review is the developer's
  job, not the gate's. Clarified and documented in g.

## Key Learnings
### Technical Insights
- **Alias vs defined type is the whole ballgame for zero-copy layout**: `type T = U` (alias) keeps
  the on-disk struct interchangeable across packages but cannot host methods if `U` is out-of-package;
  `type T U` (defined) can host methods but is not assignment-interchangeable. The resolution —
  *own the type in `pkg/format`, alias it everywhere else, and put every method on the owner* — is
  the reusable pattern for the remaining 3.x subtasks.
- **Two-tier bounds checking is the load-bearing invariant**: tier-1 (`NewSafeEntry` validates
  declared `Size`) makes every fixed-field access provably in-bounds, so only the variable-length
  path needs an explicit per-read `maxOffset` check. This is why the fixed-field error branches are
  unreachable — a property to preserve, not "fix", when widths change in 3.3.

### Process Learnings
- **Subtask branches** must be created manually after `cwf-new-subtask` (it omits branch creation);
  the parent-branch `[CONSISTENCY]` warning on subtask checkpoints is expected and benign. Already
  captured in memory.
- **Estimation**: for mechanical extractions guarded by a strong type system, day-scale estimates
  overshoot — the compiler does the blast-radius audit. Estimate such chores by *number of duplicate
  definitions to delete*, not by call-site count.

### Risk Mitigation Strategies
- The plan's named safety net (byte-for-byte round-trip + full suite + alias-keeps-narrowing-a-
  compile-error) was the right one; it gave the confidence to delete the dcfhfix duplicates outright
  rather than incrementally.

## Recommendations
### Process Improvements
- When a design moves methods between packages, explicitly note the alias-method constraint in
  c-design so it is a planned step, not a compile-time surprise.

### Tool and Technique Recommendations
- Keep the round-trip + version-offset tests as the regression gate for any future on-disk format
  change; they are cheap and directly assert the version-aware parse offset (88 vs 104).

### Future Work
- **3.2 (feature)**: version-aware read/write registry — now unblocked; `pkg/format` is the place
  it belongs.
- **3.3 (bugfix)**: widen `Dev`/`Ino` to `uint64`, bump the on-disk format version, fix the
  `dupes` `[2]uint32` key + ingest casts, and re-enable gosec G115. The single-owner extraction
  localises the width change to `pkg/format/vocabulary.go` + the codec — exactly the outcome 3.1
  was meant to enable.

## Status
**Status**: Finished
**Next Action**: Task complete — merge 3.1, then proceed to 3.2
**Blockers**: None identified
**Completion Date**: 2026-05-24
**Sign-off**: Matt Keenan (with Claude)

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Archived Materials
- Plans: a-task-plan.md, d-implementation-plan.md, e-testing-plan.md
- Exec: f-implementation-exec.md (commit 65b863c), g-testing-exec.md (commit 5dc2e5e)
- Baseline: 4cbe4ae (parent task 3)
