# z key hides unchanged tree entries - Requirements
**Task**: 16 (feature)

## Task Reference
- **Task ID**: internal-16
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/16-z-key-hides-unchanged-tree-entries
- **Template Version**: 2.1

## Goal
Specify a `z` key toggle in the `--interactive-tree` viewer that hides unchanged
entries, so the user can reduce the tree to only what changed.

## Functional Requirements
### Core Features
- **FR1 — `z` toggles hide-unchanged mode**: Pressing `z` in the viewer flips a
  "hide unchanged" mode between off (default) and on. Pressing it again restores
  the previous view. Default on viewer launch is OFF (full tree, unchanged
  behaviour). The mode is a persistent boolean held on the viewer model and
  consulted by `rebuildRows`, so it survives — and is re-applied across — every
  subsequent sort/reverse/expand rebuild (it is not a one-shot transform).
  - *AC1*: From the initial view, one `z` enters hide mode; a second `z` returns
    to a view showing the unchanged entries again.

- **FR2 — "unchanged" predicate**: A node is *unchanged* iff
  `Stats.Added + Stats.Modified + Stats.Deleted == 0`. The predicate keys on that
  sum, NEVER on `Stats.Files` (which excludes deletions), so a deletion-only node
  is correctly treated as *changed*.
  - *AC2*: With hide mode on, a file with only a deletion in its lineage / a
    directory whose only change is a deleted child remains visible; a genuinely
    unchanged file is hidden.

- **FR3 — leaf hiding**: With hide mode on, an unchanged file row is not rendered.
  - *AC3*: An unchanged leaf present in the full view is absent from `m.rows`
    when hide mode is on.

- **FR4 — directory hiding by subtree**: With hide mode on, a directory is hidden
  iff its entire subtree is unchanged — judged by the directory's own aggregated
  `Stats` change-sum (`Added+Modified+Deleted == 0`), which is already
  subtree-wide. The filter is applied per-node as each node is encountered during
  the existing single-pass flatten in `rebuildRows`: a wholly-unchanged directory
  is pruned (its children are not walked) and the filter never force-expands a
  collapsed directory. Keeping a directory with a changed descendant visible (so
  the path to a changed leaf is never broken) is therefore a *derived consequence*
  of the subtree aggregation, not a separate ancestor-walk — no new traversal is
  introduced.
  - *AC4*: A directory whose subtree contains a change is shown; a directory whose
    whole subtree is unchanged is hidden, even if it contains many unchanged files.
  - *AC4b*: A **collapsed** directory that contains a changed descendant stays
    visible in hide mode (visibility derives from aggregated `Stats`, not from
    walking the collapsed children); hide mode does not force it open.

- **FR5 — selection preservation**: Toggling the mode preserves the selected node
  if it is still visible; if the selected node was hidden by the toggle, selection
  falls back gracefully (clamped to a valid row) without panic — matching the
  existing `r`/sort-key behaviour.
  - *AC5*: Selecting a changed node then pressing `z` keeps that node selected;
    selecting an unchanged node then pressing `z` leaves a valid in-range selection.

- **FR6 — composition with sort/reverse/expand**: Hide mode composes with the
  active sort key (`c/f/a/m/d/n`), reverse (`r`), and expand/collapse state.
  Changing sort or reverse while hide mode is on keeps unchanged entries hidden;
  toggling hide mode preserves the active sort/reverse/expand state.
  - *AC6*: With hide mode on and sort=`a`, the visible changed rows are ordered by
    the added metric; pressing `r` reverses them and they stay filtered.

- **FR7 — footer help advertises `z`**: The footer help line names the `z` binding
  and its effect (hide/show unchanged).
  - *AC7*: The rendered footer contains a `z` hint for the hide-unchanged toggle.

- **FR8 — empty-state safety**: If hide mode hides every row (changeset empty or
  fully unchanged), the existing empty-state message renders and the viewer
  remains operable (toggling `z` back restores the tree). With zero visible rows
  `current()` returns nil and selection stays clamped in range (no panic); the
  header (which reads the unfiltered root `Stats`) and the stats pane (which shows
  "(nothing selected)" when `current()` is nil) both still render correctly —
  these are code paths separate from the tree body.
  - *AC8*: With an all-unchanged tree and hide mode on, the viewer shows the
    "(no changes to display)" body message, the header still shows the root file
    count/size, the stats pane shows "(nothing selected)", `current()` returns nil,
    and there is no panic; `z` restores the full view.

### User Stories
- **As a** user reviewing a large `dcfh status`/`update` result **I want** to hide
  unchanged files and directories **so that** I can focus on what actually changed
  without scrolling past noise.
- **As a** keyboard-driven user **I want** the hide toggle on a single discoverable
  key advertised in the footer **so that** I can find and use it without docs.

## Non-Functional Requirements
### Performance (NFR1)
- Per-toggle cost is one `rebuildRows` pass over already-aggregated `Stats` — no
  re-read, no I/O, no extra tree traversal beyond the existing flatten. No
  perceptible latency on a toggle (same class as the existing sort/reverse keys).

### Usability (NFR2)
- The binding follows the established single-key toggle pattern (`r`); it is
  advertised in the footer (FR7) and is reversible by re-pressing (FR1).
- Hiding never breaks the path to a changed leaf (FR4), so the tree stays
  navigable.

### Maintainability (NFR3)
- One boolean model field + one filter predicate at the single `rebuildRows`
  choke point; the predicate is a pure function, unit-testable without a screen.
- No parallel filter logic — the same predicate governs leaves and directories.

### Security (NFR4)
- None beyond the existing viewer contract: read-only, no index/filesystem
  mutation, no new input surface (a single bound key), no new strings rendered to
  the terminal (the glyph/label sanitisation contract is unchanged).

### Reliability (NFR5)
- Graceful on empty/all-unchanged trees (FR8) and on hiding the selected node
  (FR5): no panic, selection always clamped to a valid row.

## Constraints
- Confined to `cmd/dcfh/internal/tui` (viewer); read-only, no mutation.
- `z` must be unbound elsewhere — verified against `handleRune` (current binds:
  `q/j/k/l/h/r` + sort keys `c/f/a/m/d/n`; `z` is free).
- Pure render-layer operation over `dcfh.Stats`; no new data sources.

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [ ] **Time**: >1 week? No.
- [ ] **People**: >2 people? No.
- [ ] **Complexity**: 3+ distinct concerns? No.
- [ ] **Risk**: High-risk components needing isolation? No.
- [ ] **Independence**: Can parts be worked on separately? No.

**Result**: 0 signals — no decomposition.

## Acceptance Criteria
- [ ] AC1–AC8 above (one per FR, plus AC4b for the collapsed-directory case) pass.
- [ ] Full `go test ./cmd/dcfh/internal/tui/...` and `./...` green; golangci-lint
      clean on the staged change; pre-commit `-race` gate green.
- [ ] No existing `tui` test disabled or weakened.

## Status
**Status**: Finished
**Next Action**: /cwf-design-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
FR1–FR8 + AC4b all implemented and tested (TC-1…TC-9 + TC-U1, all PASS). NFRs
held: per-toggle pure pass (NFR1), single choke point + pure predicate (NFR3),
read-only no-new-surface (NFR4), no-panic/clamp on empty + hidden selection
(NFR5). See g-testing-exec.md / j-retrospective.md.

## Lessons Learned
The reviewer suggestion to consolidate 8 FRs → ~5 was declined to keep 1:1 FR→AC
traceability; recommend making this a standing convention (see j-retrospective.md).
