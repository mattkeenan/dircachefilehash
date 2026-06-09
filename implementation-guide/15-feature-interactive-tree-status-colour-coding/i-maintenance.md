# interactive-tree status colour coding - Maintenance
**Task**: 15 (feature)

## Task Reference
- **Task ID**: internal-15
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/15-interactive-tree-status-colour-coding
- **Template Version**: 2.1

## Goal
Define ongoing maintenance, monitoring, and support requirements for interactive-tree status colour coding.

## Monitoring Requirements
Not applicable — local single-binary CLI, no server/uptime/telemetry/SLA. No
runtime metrics or alerting surface. The only "signal" is direct user
observation of the `--interactive-tree` viewer and any issue reports.

## Maintenance Tasks
### Future-edit hazards (the load-bearing maintenance content)
- **Glyph safety invariant**: `nodeStyle` (`render.go`) must keep returning a
  glyph from the closed alphabet `{'+','~','-','*',' '}` — never `rune(0)`. It
  feeds `drawRow`'s `%c` verb into `drawText`, whose sanitised-string contract
  assumes no control runes. `TestNodeStyle` asserts the alphabet across all 8
  present-sets; **do not weaken that assertion**. Both exec-phase security
  reviews flagged this as the one pattern to audit on any future change to
  `nodeStyle`'s return values or the `drawRow` format string.
- **tcell colour-name trap**: tcell has **no** `ColorCyan`/`ColorMagenta` —
  cyan is `ColorAqua`, magenta is `ColorFuchsia`. A future edit reaching for the
  intuitive name will not compile (or worse, pick a different palette entry if
  someone adds a wrapper). Keep to the ANSI-palette names already used.
- **Colour/glyph single source of truth**: colour and glyph are returned
  together from one `switch` in `nodeStyle` so they cannot drift. Keep new
  category combinations in that one switch — do not add a parallel
  glyph-vs-colour mapping.
- **styleModified is shared**: it feeds both the resolver's single-category path
  and the stats-pane legend. Changing it changes both in lockstep (by design,
  FR8) — verify the stats-pane assertion (`TestStatsPaneModifiedIsBlue`) after
  any colour change.
- **Stats-pane alignment**: the legend relies on a 2-col leading slot on every
  `drawStats` line to keep colons aligned. Adding a line without the slot breaks
  alignment — `TestStatsPaneLegend` guards the glyph-prefixed lines.
- **Dead-code audit**: `.cwf/docs/dead-code-audit.md` — periodic sweep; the old
  `categoryStyle` is fully removed (grep clean), so no orphan remains.

### Dependency note
- The viewer depends on `github.com/gdamore/tcell/v2`. Colour-constant names and
  `Style.Decompose()` semantics are the only API surface this task touches; a
  tcell major bump should re-run the `tui` suite (the render tests inspect the
  `SimulationScreen` cell buffer directly and will catch any behavioural change).

## Incident Response
### Common Issues
- **Colours look wrong / washed out**: the viewer uses the 16-colour ANSI
  palette by design (Constraints) so terminal themes remap them. "Blue looks
  purple" etc. is the user's theme, not a bug. Resolution: adjust the terminal
  theme; the glyph (`+`/`~`/`-`/`*`) carries the status regardless of colour
  (FR5), so meaning is preserved even with a hostile palette.
- **No colour/bold at all**: terminal is not a TTY or `TERM` is `dumb`. The
  viewer is gated behind a TTY check (golang.org/x/term); off-TTY the
  `--interactive-tree` viewer does not launch. Resolution: run in a real
  terminal.
- **Legend missing**: the stats pane (and its legend) is suppressed below
  `MinWidthForStats`. Resolution: widen the terminal; tree glyphs remain visible
  at any width.

### Troubleshooting Guide
- **Symptom**: a changed directory shows as unchanged (no glyph/colour).
  **Diagnosis**: check whether the change is deletion-only — `Stats.Files`
  excludes deletions, so keying "changed?" off `Files` would miss it.
  **Resolution**: `nodeStyle` already keys on `Added+Modified+Deleted`
  (`TestNodeStyle` deleted case + `TestRenderDirectoryBlend` docs/ case guard
  this); if it regresses, that is the line to inspect.

## Performance
Per-row pure computation over already-aggregated `Stats`; no extra tree pass,
no I/O, no allocation hot-path. No optimisation or scaling concern (NFR1).

## Documentation
- The feature is self-documenting via the on-screen legend (FR9).
- The colour/glyph map and the design rationale live in this task's
  `b-`/`c-` plans (the decision record).

## Success Criteria
- [x] Monitoring requirements assessed (N/A for a local CLI — recorded)
- [x] Maintenance hazards documented (glyph invariant, tcell name trap, shared
      style var, alignment slot)
- [x] Common issues + troubleshooting documented
- [x] Performance characteristics recorded

## Status
**Status**: Finished
**Next Action**: /cwf-retrospective
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Captured the load-bearing future-edit hazards (glyph safety invariant, tcell
`ColorAqua`/`ColorFuchsia` naming, shared `styleModified`, stats-pane alignment
slot) and user-facing troubleshooting (theme remap, off-TTY, narrow-width legend
suppression). Monitoring N/A for a local CLI.

## Lessons Learned
For a local tool the valuable maintenance artefact is not a runbook but the list
of invariants a future edit could silently break — each is now backed by a named
test, so a regression fails CI rather than shipping.
