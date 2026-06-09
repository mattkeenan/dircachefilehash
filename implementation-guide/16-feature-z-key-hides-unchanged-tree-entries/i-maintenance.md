# z key hides unchanged tree entries - Maintenance
**Task**: 16 (feature)

## Task Reference
- **Task ID**: internal-16
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/16-z-key-hides-unchanged-tree-entries
- **Template Version**: 2.1

## Goal
Define ongoing maintenance and support for the `z` hide-unchanged toggle.

## Scope Note
This is a small, self-contained TUI feature in a local CLI binary — no service,
no runtime telemetry, no SLA, no scaling/capacity surface. The "monitoring,
alerting, on-call, scaling" template sections do not apply; the maintenance
burden is the unit-test suite plus the conventions below. The applicable
sections are filled; the rest are marked N/A rather than invented.

## Monitoring Requirements
- **N/A** — no service. The `tui` test suite (`go test ./cmd/dcfh/internal/tui/...`)
  is the regression signal; CI runs it on every change. A break in the `z`
  behaviour surfaces as a failing `TestHide*` / `TestHasChange` case.

## Maintenance Tasks
### Regular
- The feature has no scheduled maintenance of its own. It is covered by the
  repo-wide gates: `golangci-lint` (incl. gosec) and the `-race` pre-commit test
  run, both already green for this package.
- Dead-code audit (see `.cwf/docs/dead-code-audit.md`): if `hasChange` or the
  `hideUnchanged` field ever shows zero references in a sweep, that signals the
  binding was removed without removing its support code — clean both together.

### Invariants to preserve (the things a future change could break)
1. **Predicate keys on `Added+Modified+Deleted`, never `Stats.Files`.** `Stats.Files`
   excludes deletions, so keying on it would wrongly hide deletion-only nodes.
   `hasChange` and `nodeStyle`'s unchanged arm (`set == 0`) must stay in agreement;
   `TestHasChange` (deleted-only case) and `TestHideKeepsDeletionOnlyCollapsedDir`
   guard this.
2. **The filter stays at the single `rebuildRows` choke point.** Every view-mutating
   key already routes through `rebuildRows`, so the toggle composes with
   sort/reverse/expand for free. Do not add a second, parallel filter path.
3. **Selection always re-clamps after a row-set change.** The `z` case mirrors `r`:
   capture `current()`, mutate, `rebuildRows()`, `selectNode(cur)`. Any *new*
   row-mutating key binding must do the same, or it risks an out-of-range
   `m.sel` (the one pattern note both security reviews raised). `current()`
   returning nil on zero rows is the guard the draw/nav paths rely on.

## Incident Response
### Common Issues
- **Symptom**: A changed directory disappears in hide mode.
  - **Diagnosis**: Its aggregated `Stats` show zero Added/Modified/Deleted — i.e.
    the tree builder under-aggregated a descendant change.
  - **Resolution**: Fix is in the data layer (`pkg` tree aggregation), not the
    viewer; `hasChange` is correct by construction over correct stats.
- **Symptom**: Panic / wrong row after pressing `z` with a selection.
  - **Diagnosis**: A selection index not re-clamped after the row set shrank.
  - **Resolution**: Confirm the handler captured `current()` and called
    `selectNode`/`clampSel` after `rebuildRows` (invariant 3).
- **Symptom**: Footer no longer shows `z hide`.
  - **Diagnosis**: The hand-maintained footer literal in `drawFooter` drifted.
  - **Resolution**: Restore the hint; `TestFooterAdvertisesHide` should have
    caught it — re-run the suite.

## Performance Optimisation
- **N/A** — the toggle is one pass over already-aggregated, in-memory stats. No
  I/O, no extra traversal; no optimisation surface.

## Documentation
- In-app: the footer help line is the user-facing documentation (`z hide`).
- Design/requirements rationale lives in this task's `b-`/`c-` plans.

## Success Criteria
- [x] Regression signal identified (the `tui` test suite under CI).
- [x] Maintenance invariants documented (predicate basis, single choke point,
      selection re-clamp).
- [x] Common issues + troubleshooting captured.
- [x] Non-applicable template sections explicitly marked N/A, not padded.

## Status
**Status**: Finished
**Next Action**: /cwf-retrospective
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Lessons Learned
*To be captured during retrospective*
