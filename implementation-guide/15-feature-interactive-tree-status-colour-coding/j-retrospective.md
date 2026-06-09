# interactive-tree status colour coding - Retrospective
**Task**: 15 (feature)

## Task Reference
- **Task ID**: internal-15
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/15-interactive-tree-status-colour-coding
- **Template Version**: 2.1
- **Retrospective Date**: 2026-06-09

## Executive Summary
- **Duration**: ~1 day (estimated: 1–2 days; within estimate).
- **Scope**: Delivered exactly the requested encoding (status glyph + additive
  colour + bold, directory blend) plus one mid-flight scope addition agreed with
  the user — an on-screen legend (FR9). No descopes.
- **Outcome**: Success. All success criteria met; `nodeStyle`/`drawRow` at 100%
  statement coverage; full regression + `-race` green; security review clean on
  both exec phases.

## Variance Analysis
### Time and Effort
- **Estimated**: 1–2 days, Low–Medium complexity, confined to the TUI render
  layer.
- **Actual**: ~1 day across all ten phases. The bulk was planning/review, not
  code — the production change is ~40 net lines in one file.
- **Variance**: Within estimate. Effort skewed toward the plan-review and
  testing phases rather than implementation, as expected for a small,
  well-bounded render change.

### Scope Changes
- **Additions**:
  - On-screen legend (FR9/AC12): added after the user approved it during the
    plan-review checkpoint. Rationale: the colour/glyph map is discoverable in
    the stats pane the viewer already renders, at the cost of one static line.
- **Removals**: None.
- **Impact**: The legend was folded into the b–e plans before exec, so the
  plans stayed authoritative; no rework.

### Quality Metrics
- **Test Coverage**: `nodeStyle` 100%, `drawRow` 100% (target: resolver
  table-tested across all combinations — met and exceeded with render-path
  simulation tests). Package total 84.4%.
- **Defect Rate**: 0 bugs found in testing; 0 post-implementation. One compiler
  catch during edit (unused `glyph` var) resolved immediately by completing the
  `drawRow` format-string change.
- **Performance**: per-row pure computation, no new tree pass (NFR1 met); full
  suite timing unchanged.

## What Went Well
- **One pure resolver for leaves and directories**: keying on
  `Stats.{Added,Modified,Deleted} > 0` collapsed the leaf and directory cases
  into a single count-driven `switch`, eliminating the `IsDir` special-case the
  old `categoryStyle` carried.
- **Plan review caught real issues pre-code**: the modified=yellow→blue
  collision and the non-existent `ColorCyan`/`ColorMagenta` names were both
  surfaced in review, not at compile time.
- **Glyph-as-primary-signal** satisfied the colour-vision-deficiency requirement
  cleanly and gave the tests a colour-independent assertion (the safe alphabet).
- **Existing simulation-test harness** (`SimulationScreen` + `GetContents`) made
  per-cell colour/bold/reverse assertions straightforward to add.

## What Could Be Improved
- **Test plan referenced an unrenderable "root row"**: e-testing-plan TC-S3/S6
  assumed the all-three blend would show on the root row, but the implicit root
  (label "") is never rendered. Caught at test-writing time and worked around
  with the pure table + a `treeAllThreeDir` fixture, but the design/testing plan
  could have verified "is the node actually a visible row?" against
  `rebuildRows` first. Cheap lesson: when a test case names a specific rendered
  row, confirm it is in `m.rows`.

## Key Learnings
### Technical Insights
- A 3-bit present-set (`A|M|D`) over a single `switch` is the right shape for
  "blend N independent statuses" — colour and glyph returned together so they
  cannot drift, and the `default` arm makes the all-three case fall out for free.
- `Stats.Files` **excludes** deletions (`Files == Added+Modified+Unchanged`), so
  "is this changed?" must key on `Added+Modified+Deleted`. A deleted-only
  directory is the canonical trap; it now has a dedicated test.
- tcell colour-constant naming: cyan = `ColorAqua`, magenta = `ColorFuchsia`
  (no `ColorCyan`/`ColorMagenta`). Worth remembering for any future TUI colour
  work in this repo.

### Process Learnings
- Folding an approved scope addition (the legend) back into the planning docs
  *before* exec kept the plans as the single source of truth and avoided a
  drift between "what we said" and "what we built".
- The `PERL5OPT=-CDSLA` environment clashes with the markdown-reader skill's
  `-CDSL` shebang ("Too late for -CDSL"); `env -u PERL5OPT` is the workaround.

### Risk Mitigation Strategies
- The planned top risk (terminal/theme colour variance) was mitigated by design
  — glyph as primary signal — so it never became a delivery risk. The mandatory
  always-present glyph turned an accessibility requirement into a testability
  asset.

## Recommendations
### Process Improvements
- When a testing-plan test case pins a *specific rendered row*, cross-check it
  against the row-building logic during the e-phase, not at test-writing time.

### Tool and Technique Recommendations
- The present-set `switch` returning `(glyph, style)` together is a reusable
  pattern for any future "summarise child statuses" rendering.

### Future Work
- None required. The maintenance doc records the future-edit hazards (glyph
  safety invariant, tcell name trap, shared `styleModified`, alignment slot). A
  maintainer multi-terminal eyeball pass (light + dark) at release time is the
  only open manual check; not blocking.

## Status
**Status**: Finished
**Next Action**: Task complete
**Blockers**: None identified
**Completion Date**: 2026-06-09
**Sign-off**: Matt Keenan

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Archived Materials
- Planning & exec docs: `implementation-guide/15-feature-interactive-tree-status-colour-coding/` (a–j)
- Implementation: `82d68a7` (exec), tests in `cmd/dcfh/internal/tui/render_test.go`
- Testing results: `g-testing-exec.md`; security reviews: `f-`/`g-` § Security Review (both "no findings")
- Baseline commit: `ca08ce6`
