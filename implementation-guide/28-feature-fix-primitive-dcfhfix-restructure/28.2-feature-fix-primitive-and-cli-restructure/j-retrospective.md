# Fix primitive and CLI restructure - Retrospective
**Task**: 28.2 (feature)

## Task Reference
- **Task ID**: internal-28.2
- **Task URL**: N/A (internal task)
- **Parent Task**: 28
- **Branch**: feature/28.2-fix-primitive-and-cli-restructure
- **Template Version**: 2.1
- **Retrospective Date**: 2026-06-13

## Executive Summary
- **Duration**: ~1 day (estimated: 3–4 days; variance −70%). All 13 checkpoint
  commits landed 2026-06-13.
- **Scope**: Delivered as planned — `Repo.Fix` primitive symmetric to
  `Repo.Filter`, dcfhfix re-expressed as a thin `RunFix` translator, backup-stack
  relocated to `pkg/`, write-destination confinement (D2/NFR4). Manual
  interactive mode deferred by design; multi-source recovery rebuild remains 28.3.
- **Outcome**: Success. AC1–AC7 met (AC5 resolved by dropping `BackupID`, LD7);
  full suite + `golangci-lint` + pre-commit `-race` green on every commit.

## Variance Analysis
### Time and Effort
- **Estimated**: 3–4 days, High complexity (a-task-plan.md).
- **Actual**: single day, three milestones each its own commit + gate:
  - Plan phases (a–e): same-day, leveraged the 28-parent decomposition.
  - Implementation (f, M1–M3): `76d65d1a`, `32c38169`, `a4ca494e`.
  - Testing (g): `0338fb2c` (+ AC6 fix), `eb5803de`.
  - Rollout/maintenance (h/i): docs.
- **Variance**: −70%. The estimate assumed the confinement design and the
  collect/write split would each cost discovery time; in practice 28.1 had
  already landed the single-writer path, `newFixMetaStore`, and `hasPathPrefix`,
  so M2 was assembly of proven parts rather than invention. The `Filter`
  primitive gave an exact shape to mirror.

### Scope Changes
- **Additions**:
  - AC6 discards-on-cap-error fix (2 production lines) — test-driven; collectors
    now return partial counts on the cap-trip so `FixResult.EntriesDiscarded`
    reflects reality. Not in the original plan; surfaced by TC-9.
  - Pop/Discard/Clear promoted to `pkg` cores returning data/counts (beyond a
    literal "move") to pre-position M3.
- **Removals / deferrals**:
  - `BackupID` dropped from `FixResult` (AC5 resolved) — `fixes-list` already
    exposes everything it would have; keeping it would duplicate.
  - `FixCommand.Index`/`Append` dropped — every real op is path-based; `Value`
    carries raw JSON for the append/json-edit forms (reuses `ParseEntryFromJSON`).
  - Manual interactive mode → `ErrManualModeUnimplemented` (deferred).
  - Multi-source recovery rebuild → 28.3 (out of scope, as planned).
- **Impact**: net simplification (fewer moving parts than the design's data
  model); no timeline cost.

### Quality Metrics
- **Test Coverage**: TC-R1 + TC-1…TC-12 PASS; AC1–AC7 covered. Confinement
  accept/reject (both sides), fail-closed classification (100%), cap-predicate
  boundary, dry-run gate, all 10 op families covered. ~1666 insertions /
  647 deletions across 12 files (`git diff --stat` vs baseline a0bcf0e0).
- **Defect Rate**: one test-driven product fix (AC6, 2 lines); one
  test-assumption correction (no product defect). Zero post-landing defects.
- **Performance**: no new index passes in the translation layer (NFR1); the
  CLI is now a thin translator over one shared `RunFix`.

## What Went Well
- **Mirroring `Filter` paid off**: cloning the `FilterRequest`/`RunFilter`/
  `repoCore` shape gave the primitive an unambiguous target and made the
  interface addition land on both `localRepo` and `wireRepo` for free (embedded
  `repoCore`).
- **Confinement designed fail-closed from the start**: `readOnlyFixOps`
  allow-list + `default`-is-write classification, symlink-resolved prefix check,
  `writeRoot==""` exemption that `Repo.Fix` structurally cannot reach. The
  focused security review found nothing.
- **Test-first cap coverage caught the AC6 gap** before landing — TC-9 failed
  honestly and drove a real (small) product fix.
- **28.1 groundwork** (single-writer path, `newFixMetaStore`, `hasPathPrefix`)
  meant M2 was assembly, not invention — the main driver of the −70% variance.

## What Could Be Improved
- **The design's "all index-mutating → writeRepairedIndex" grouping was
  imprecise** — that path re-serialises entries and recomputes the checksum, so
  it cannot express header (version/flags/signature) edits. Caught at exec time
  and resolved (header-edit keeps its surgical writer, relocated to
  `fix_header.go`), but a sharper design pass would have flagged it earlier.
- **Security-review auto-cap (500 lines) is a poor fit for a from-scratch
  primitive** — the legitimate changeset was ~1700 production lines, so the
  deterministic reviewer recorded `error` in both f and g. The focused manual
  review covered the security-critical surface, but the gate's binary cap gives
  no partial-credit path for large, single-concern features.
- **Backup-naming second-granularity limitation** surfaced only via a test
  assumption that turned out to be wrong — it is pre-existing, not introduced
  here, but it had never been documented until now.

## Key Learnings
### Technical Insights
- A "surgical" header writer and the bulk single-writer path are genuinely
  different tools: one preserves arbitrary header bytes, the other normalises
  layout + checksum. Don't force header edits through the entry writer.
- The `writeRoot` positional-parameter exemption model is a clean way to give
  the CLI an explicit-subject escape hatch while making it *unreachable* from the
  library — the trust boundary is encoded in the call site, not a request flag.
- Returning partial counts on an error (collectors) lets the caller surface
  meaningful progress (discards) even when an operation trips a cap.

### Process Learnings
- Estimation: when a predecessor task has de-risked the hard parts (here 28.1),
  a "High complexity" label over-predicts effort. Worth distinguishing inherent
  complexity from *residual* complexity after dependencies land.
- The CWF exec-phase security gate needs a documented path for legitimately
  large single-concern changesets (split review at rollout) — recorded as a
  recommendation below.

### Risk Mitigation Strategies
- The pre-identified high risk (D2 confinement) was isolated by its own AC + a
  reject-before-write test (AC7/TC-8) and a focused agent review — the right
  shape; it never became a blocker.
- The other high risk (CLI behaviour drift) was held down by keeping
  `main_test.go`/`options_test.go` green with call-site-only adaptation; the one
  changed expectation (header-edit error string) was documented as a fix, not a
  silent change.

## Recommendations
### Process Improvements
- For a from-scratch primitive that mirrors an existing one, plan the exec as
  "assemble + confine + test" rather than "design from blank"; budget accordingly.
- Add a documented "split/manual changeset review at rollout" path for changes
  that legitimately exceed the security-review auto-cap, so the `error` state is
  expected and pre-planned rather than a surprise in two consecutive phases.

### Tool and Technique Recommendations
- Keep the `Filter`/`Fix` symmetry as the template for any future
  `Repo.<Verb>` primitive — consistent request/result/command triples + a thin
  `repoCore` method + a single `Run<Verb>`.

### Future Work
- **28.3 — multi-source recovery rebuild** (`mergeSourcesIntoEntries`): the
  first real consumer of `Repo.Fix`; already scoped by the parent.
- **Manual interactive fix mode**: currently `ErrManualModeUnimplemented` —
  implement when there's a user need.
- **Backup-naming sub-second granularity** (bugfix, Low): widen the backup
  filename timestamp if same-second backup stacking is ever required. Pre-existing
  limitation, documented in i-maintenance.md.

## Status
**Status**: Finished
**Next Action**: Task complete
**Blockers**: None identified
**Completion Date**: 2026-06-13
**Sign-off**: Matt Keenan / Claude Opus 4.8

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Archived Materials
- Planning: a-task-plan.md … e-testing-plan.md (all Finished).
- Implementation: f-implementation-exec.md (M1 `76d65d1a`, M2 `32c38169`,
  M3 `a4ca494e`); deviations + focused security review recorded there.
- Testing: g-testing-exec.md (TC-R1 + TC-1…TC-12; AC6 fix `0338fb2c`).
- Rollout/maintenance: h-rollout.md, i-maintenance.md.
- Baseline commit: a0bcf0e0.
