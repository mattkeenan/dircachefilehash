# interactive-tree status colour coding - Testing Execution
**Task**: 15 (feature)

## Task Reference
- **Task ID**: internal-15
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/15-interactive-tree-status-colour-coding
- **Template Version**: 2.1

## Goal
Execute the tests defined in e-testing-plan.md and verify implementation from d-implementation-plan.md.

## Execution Checklist
- [x] Read e-testing-plan.md and d-implementation-plan.md thoroughly
- [x] Verify test environment ready
- [x] Execute test cases sequentially
- [x] Record pass/fail for each test
- [x] Document failures with reproduction steps
- [x] Update status to "Testing" when in progress, "Finished" when all pass

## Test Results

Command: `go test ./cmd/dcfh/internal/tui/` (and `-run` per case); full
regression via `go test ./cmd/... ./pkg/...`; race gate via the pre-commit
`go test -race`. All green.

### Functional Tests

| Test ID | Go test | ACs / FRs | Result | Notes |
|---------|---------|-----------|--------|-------|
| TC-U1   | `TestNodeStyle` (8 subtests) | AC1–AC4, FR1–FR5 | PASS | All 8 present-sets: glyph, fg colour, bold, safe-alphabet, `bold==set≠∅`. |
| TC-S1   | `TestRenderGlyphPlacement` | AC1/AC3, FR1/FR3 | PASS | `+`/`~`/`-` on new.go/main.go/old.md; `-` on docs/; `*` on src/; no glyph on readme.md. |
| TC-S2   | `TestRenderLeafColourAndBold` | AC1/AC4, FR1 | PASS | new.go=Green+Bold, main.go=Blue+Bold, old.md=Red+Bold, readme.md=default/non-bold. |
| TC-S3   | `TestRenderDirectoryBlend` | AC2, FR2 | PASS | docs/=Red+Bold, src/=Aqua(cyan)+Bold. |
| TC-S4   | (folded into TC-S2 readme.md case) | AC8 | PASS | unchanged leaf: default fg, no glyph, non-bold. |
| TC-S5   | `TestRenderDirectoryBlend` (docs/) | AC9 | PASS | deleted-only dir renders Red/`-`/Bold — keys on `Added+Modified+Deleted`, not `Files`. |
| TC-S6   | `TestRenderAllThreeDirectory` | AC10 | PASS | all-three `proj/` dir = White/`*`/Bold, distinct from unchanged via bold+glyph. |
| TC-S7   | `TestRenderSelectionComposes` | AC6, FR6 | PASS | selected docs/ shows AttrReverse over Red/Bold; glyph still present. |
| TC-S8   | `TestStatsPaneModifiedIsBlue` | AC11, FR8 | PASS | stats-pane `Modified` line renders Blue (not Yellow). |
| TC-S9   | `TestRenderNarrowWidthDropsValue` | AC7, FR7 | PASS | width 12: no panic, docs label survives, `900 B` value dropped (guard holds). |
| TC-S10  | `TestStatsPaneLegend` | AC12, FR9 | PASS | `+ Added` / `~ Modified` / `- Deleted` + `* mixed` present. |

**Deviation from plan**: TC-S3/TC-S6 in e-testing-plan referenced a "root row"
rendering the all-three blend. The implicit root (label "") is not a rendered
row (`rebuildRows` walks `m.root.Children`), so all-three white is covered by
the pure table (`TestNodeStyle`, authoritative) plus a dedicated
`treeAllThreeDir` fixture (`TestRenderAllThreeDirectory`). Recorded in
f-implementation-exec.md Step 6.

### Non-Functional Tests
- **Usability/CVD (FR5)**: TC-U1 asserts a distinct glyph per category and the
  closed safe alphabet `{'+','~','-','*',' '}`; TC-S1 confirms glyphs render —
  status legible without colour. PASS.
- **Reliability (NFR5)**: TC-S5 (deleted-only), TC-S9 (narrow), and the existing
  `TestRun_EmptyTreeNoOp` exercise the no-panic / empty-changeset paths. PASS.
- **Performance (NFR1)**: no dedicated test; per-row pure computation over
  already-aggregated stats. Full suite timing unchanged. N/A.
- **Security (NFR4)**: glyph alphabet is a closed set (asserted TC-U1); no new
  input/exec surface. Confirmed by the changeset security review (no findings).

### Regression (FR7)
All pre-existing `tui` tests stay green (`TestNavigation`, `TestWidthGating`,
`TestDefaultSortAndKeyToggles`, `TestColumnTracksActiveSortMetric`,
`TestStatsPaneByteAnnotations`, `TestLiveResortPreservesSelectionNoReRead`,
`TestRunScreen_*`, `TestRun_EmptyTreeNoOp`). No test disabled or weakened. The
`TestStatsPaneByteAnnotations` substring assertions still hold because the new
glyph-prefixed legend lines contain the old labels.

## Test Failures

None.

## Coverage Report

`go test ./cmd/dcfh/internal/tui/ -coverprofile`:
- `nodeStyle`: **100.0%** (all 8 switch arms)
- `drawRow`: **100.0%**
- `drawStats`: 77.8% (uncovered: the `maxW<=0` / nil-selection early returns —
  pre-existing paths, not task-15 surface)
- package total: 84.4% of statements

Full regression `go test ./cmd/... ./pkg/...`: all packages OK. Pre-commit
`go test -race` (checkptr-disabled per repo gate): all packages OK.

## Status
**Status**: Finished
**Next Action**: /cwf-rollout
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
*To be filled upon completion*

## Lessons Learned
The `SimulationScreen` + per-cell `Style.Decompose()` pattern made
colour/bold/reverse assertions precise and cheap. Reusing `treeForSim` covered
most cases; the gap (all-three rendered row) was filled with a tiny dedicated
fixture rather than bending the shared one.

## Security Review

**State**: no findings

I've now read the complete changeset. The diff comprises the production render change (`render.go`), its tests (`render_test.go`), and the five CWF planning documents (`a-` through `e-`) plus the `f-implementation-exec.md` which embeds a prior security-review note. I'll reason through the five threat categories independently against the actual diff.

## Review

The substantive code change is a TUI rendering refactor in `cmd/dcfh/internal/tui/render.go`: `categoryStyle(n) tcell.Style` is replaced by `nodeStyle(n) (rune, tcell.Style)`, `styleModified` is recoloured yellow→blue, a fixed-width status glyph is inserted into each rendered row via `fmt.Sprintf("%*s%s%c %s", indent, "", marker, glyph, label)`, and a stats-pane legend gains glyph-prefixed lines plus a `* mixed` note. The remainder of the diff is test code and static markdown planning artefacts.

**(a) Injection / unsafe command construction.** No shell, `exec`, or command-string construction anywhere in the diff. The Go is pure in-memory computation feeding tcell screen writes; the markdown is descriptive prose. No slug, branch, or path is interpolated into a command. Clear.

**(b) Perl / git-output parsing.** No Perl added or modified; no git porcelain parsing introduced. Not applicable.

**(c) Prompt-injection via user-supplied strings.** No `{arguments}`-style substitution surface. `nodeStyle` and `drawRow` consume `dcfh.Node` data (labels, stat counts) sourced from the local filesystem scan and write to a terminal screen — not into any LLM prompt. The planning docs are static. No new prompt-injection surface.

**(d) Unsafe environment-variable handling.** No `os.Getenv`, no env-derived paths, no `chmod`/`rm`/`open` on env strings. Not applicable.

**(e) Pattern-based risk (safe-here-but-audit-future).** The one pattern worth naming: the glyph flows through the `%c` verb in `drawRow`'s format string into `drawText`, which carries a sanitised-string contract. A `rune(0)` through `%c` would emit a NUL into the terminal stream. This is **safe here** because `nodeStyle` returns a glyph from a closed alphabet `{'+','~','-','*',' '}` on every arm — the `case 0` arm returns `' '` (not `rune(0)`), the switch is exhaustive over the 3-bit present-set with a `default` arm, and `TestNodeStyle` asserts safe-alphabet membership across all 8 sets. The risk would materialise only if a future edit added a `nodeStyle` arm returning a caller-derived or zero rune, or routed a node `Label` through `%c` instead of `%s`; audit any future change to `nodeStyle`'s return alphabet or the `drawRow` format string. Maintenance note only — the current callsite is correct and self-guarded.

The node `Label` continues to reach the rendered string via `%s` and `drawText`; that is pre-existing behaviour and `drawText` owns the sanitisation contract — this changeset does not widen it.

No actionable security concerns. The change is TUI presentation only, introducing no new input, file-access, command-execution, secret-handling, env-var, or prompt-injection surface.

```cwf-review
state: no findings
summary: TUI-only render refactor (glyph + colour + bold); no injection/secret/auth/env/prompt-injection surface. Glyph constrained to a closed alphabet by an exhaustive switch and pinned by TestNodeStyle; pattern-risk noted for future nodeStyle/drawRow format-string edits only.
```
