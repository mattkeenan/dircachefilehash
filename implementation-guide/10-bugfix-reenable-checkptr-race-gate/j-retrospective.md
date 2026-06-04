# Re-enable checkptr in the race gate - Retrospective
**Task**: 10 (bugfix)

## Task Reference
- **Task ID**: internal-10
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: bugfix/10-reenable-checkptr-race-gate
- **Template Version**: 2.1
- **Retrospective Date**: 2026-06-04

## Executive Summary
- **Duration**: ~1 day (estimated: 1-2 days; within estimate).
- **Scope**: As planned — make the three zero-copy accessors checkptr-clean and re-arm
  the `-race` gate (option 1, fix). Two unplanned-but-small additions surfaced during
  exec: a `cwf-project.json` security-review exclude config, and one Medium backlog item
  for a pre-existing discrepancy.
- **Outcome**: Success. `go test -race ./...` passes with checkptr **ON** across all
  packages (previously aborted); gate re-armed; zero-copy preserved (0 allocs/op); no
  on-disk format or API change. Both phase security reviews returned no findings.

## Variance Analysis
### Time and Effort
- **Estimated**: Planning ~0.25d, Design ~0.25d, Impl ~0.5d, Testing ~0.25d (bugfix:
  no requirements/rollout/maintenance phases).
- **Actual**: Roughly on estimate. The spike (done pre-task, recorded in c-design-plan)
  de-risked the core uncertainty, so design and implementation were fast. The only
  time not anticipated was a second debugging cycle in exec (see Scope/What Went Well).
- **Variance**: Negligible. The upfront spike converted the highest-risk unknown into a
  known quantity before planning, which is why the estimate held.

### Scope Changes
- **Additions**:
  1. `security.review.max-lines-exclude-paths: ["implementation-guide/**"]` in
     `cwf-project.json` — the exec-phase security-review cap (500 production lines) was
     tripped by ~606 lines of CWF plan markdown counted as production. Adding the
     process-docs exclude (the canonical use the security-review doc describes) dropped
     the count to 87. General, repo-wide fix, not a per-task workaround.
  2. Medium backlog item "Reconcile RelativePath vs calculatePathLength 8-byte
     pathStart discrepancy" — a pre-existing latent correctness bug discovered while
     making the accessors checkptr-clean (the two functions disagree on where the path
     starts by 8 bytes). Preserved byte-for-byte here; queued for a dedicated audit.
- **Removals**: None. Option 2 (document-only) was rejected per the design (spike proved
  a clean fix exists).
- **Impact**: Both additions were small and improved the repo (one fixes a tooling gap,
  one records a real latent bug). No timeline impact.

### Quality Metrics
- **Test coverage**: No %-target (refactor of existing lines). Critical paths C1/C2/C3
  exercised under checkptr-ON `-race`; C2 now has a direct heap-backed regression
  (converted serialiser test).
- **Defect rate**: One self-caught defect during exec (see below); zero escaped defects.
- **Performance**: `BenchmarkBESkiplist/RelativePath` → 0 B/op, 0 allocs/op — zero-copy
  fully preserved (the explicit NFR).

## What Went Well
- **The pre-task spike paid off.** Replacing one accessor with `unsafe.Add` and watching
  the failure advance to the next site proved option 1 viable before any planning — the
  estimate held because the core risk was already retired.
- **Correctness-over-convenience ordering held.** The gate was edited only after a full
  checkptr-ON green run, so the gate was never weakened on a partially-fixed tree.
- **The plan's behaviour-preservation trap was respected.** The d-plan flagged the 8-byte
  C2/C3 discrepancy in advance, so the implementation preserved both addresses byte-for-
  byte instead of "helpfully" unifying them — and logged the discrepancy for a proper audit.
- **Adversarial security review added signal.** The changeset reviewer independently
  re-derived the C2 underflow guard and the C3 guarded-non-dereference, confirming the
  memory-safety reasoning rather than rubber-stamping it.

## What Could Be Improved
- **The d-plan code snippet shipped a latent GC bug.** The planned C2/C3 form held
  `pathEnd` (= `base + Size`, a past-the-end pointer) live in a variable. That cleared
  checkptr's *arithmetic* error but introduced a *different* runtime failure —
  `fatal error: found bad pointer in Go heap` — caught only when the suite ran. A plan
  reviewing unsafe-pointer code should explicitly check for *live past-the-end pointers*,
  not just provenance. (Resolved in-task by switching to integer-length trimming.)
- **The security-review cap config gap was discoverable earlier.** Any doc-heavy CWF task
  branch would trip the production-line cap; the exclude could have been set repo-wide
  before this task rather than reactively.

## Key Learnings
### Technical Insights
- **`unsafe.Add` fixes checkptr arithmetic but not GC pointer validity.** Two distinct
  runtime checks fire on bad unsafe-pointer use: `checkptrArithmetic` ("pointer arithmetic
  result points to invalid allocation", provenance) and the GC heap scan ("found bad
  pointer in Go heap", a live pointer that doesn't point into a valid object). The robust
  idiom for end-of-region scans is to keep **only an in-bounds base pointer live** and
  index with an **integer length**, materialising any boundary pointer solely in the final
  consuming expression. Holding a one-past-the-end `unsafe.Pointer` across a safepoint is
  the trap.
- **checkptr only bites heap-backed memory.** For true mmap regions `checkptrBase`
  returns 0 and the check is skipped — which is exactly why production never crashed and
  the disabled gate could hide the issue. The heap-backed test path (`heapBacked`) is the
  one that matters for this gate.

### Process Learnings
- **A spike before planning is high-leverage for unsafe/runtime-sensitive work.** It
  turned a "might need option 2" task into a confidently-scoped option-1 fix.
- **Append-only history should not be retro-edited.** Historical Task-8 CHANGELOG entries
  describing the (then-true) disabled-checkptr state were left as-is; the new state is
  recorded when this task retires to the CHANGELOG. Falsifying past entries to match the
  present is the wrong move.

### Risk Mitigation Strategies
- The plan's enumerated "sites NOT to change" list (offset-only arithmetic that never
  reconverts to a pointer) kept the change surface minimal and avoided touching
  checkptr-clean code unnecessarily.
- The full-runtime acceptance bar (`go test -race ./...`, not a grep) caught the
  grep-invisible GC failure that a static check would have missed.

## Recommendations
### Process Improvements
- When a plan reviews `unsafe` pointer code, add an explicit check: *is any derived
  past-the-end pointer held in a live variable across a call/safepoint?* — not just
  *is provenance preserved?*

### Tool and Technique Recommendations
- Keep `security.review.max-lines-exclude-paths` configured for the process-docs
  directory in every CWF-managed repo so doc-heavy task branches don't trip the
  production-line cap.

### Future Work
- **Medium backlog item filed**: "Reconcile RelativePath vs calculatePathLength 8-byte
  pathStart discrepancy" — audit which path-start offset is canonical against the on-disk
  writer (`EntrySerialiser`), fix the wrong one, add a test pinning path-start.
- **Post-merge** (not a repo file): rewrite/remove the now-false
  `project_race_checkptr_disabled` auto-memory once this change lands on the trunk.

## Status
**Status**: Finished
**Next Action**: Task complete — suggest merge to user
**Blockers**: None identified
**Completion Date**: 2026-06-04
**Sign-off**: Claude (Opus 4.8) / Matt Keenan

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Archived Materials
- Plans: `a-task-plan.md`, `c-design-plan.md`, `d-implementation-plan.md`, `e-testing-plan.md`
- Execution: `f-implementation-exec.md` (commit `29667d0`), `g-testing-exec.md` (commit `61fff80`)
- Production change: `pkg/binary_entry.go`, `pkg/format/entry.go`, `pkg/entry_serialiser_test.go`,
  `.githooks/pre-commit`, `implementation-guide/cwf-project.json`
- Backlog: BACKLOG.md (C2/C3 discrepancy item)
