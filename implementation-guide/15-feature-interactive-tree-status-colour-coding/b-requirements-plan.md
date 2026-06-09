# interactive-tree status colour coding - Requirements
**Task**: 15 (feature)

## Task Reference
- **Task ID**: internal-15
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/15-interactive-tree-status-colour-coding
- **Template Version**: 2.1

## Goal
Specify the status-aware visual encoding (glyph + colour + weight) for nodes in
the interactive-tree post-run viewer, so change status is legible at a glance
and remains legible without colour.

## Functional Requirements
### Core Features
- **FR1 — Leaf encoding**: Each file leaf is rendered per its change category:
  added → glyph `+`, green; modified → glyph `~`, blue; deleted → glyph `-`, red;
  unchanged → no glyph, default colour. (Weight per FR4.)
  - **AC**: each of the four categories renders the specified glyph and colour; unchanged leaves carry no glyph and default colour.
- **FR2 — Directory colour blend**: A directory's foreground colour is the
  presence-based additive blend over all descendants, channels R=deleted /
  G=added / B=modified: added=green, modified=blue, deleted=red,
  added+modified=cyan, modified+deleted=magenta, added+deleted=yellow,
  all-three=white. A single descendant of a category turns that channel on
  regardless of count.
  - **AC**: each of the 7 non-empty combinations renders the specified colour; changing the *counts* (not the set of categories present) does not change the colour.
- **FR3 — Directory glyph**: A directory whose changed descendants are all one
  category shows that category's glyph (`+`/`~`/`-`); a directory with two or
  more categories present shows `*`; a directory with no changed descendants
  shows no glyph.
  - **AC**: single-category dir → that glyph; any mix → `*`; no changes → no glyph.
- **FR4 — Bold rule**: A node (leaf or directory) is bold iff its subtree change
  count `Added+Modified+Deleted > 0`, else non-bold. (Every glyph/colour-bearing
  node is therefore bold; this also distinguishes the all-three *white* directory
  from an unchanged default-foreground row.)
  - **AC**: changed nodes bold; unchanged nodes non-bold.
- **FR5 — Glyph is the primary signal**: The status glyph is always present for
  changed nodes, independent of colour, so status is distinguishable without
  relying on colour (colour-vision-deficiency safe); colour reinforces.
  - **AC**: with colour removed, added/modified/deleted/mixed remain distinguishable via glyph (+ bold).
- **FR6 — Composition with existing styles**: Glyph/colour/weight compose with
  the selected-row reverse-video highlight and existing header-bold / footer-dim
  styles. Selection applies `Reverse(true)` *on top of* the category style, and
  the glyph is still rendered.
  - **AC**: for a selected *changed* row the simulation cell buffer shows the glyph present and the row reversed over the category style; sort header and help footer styling are byte-for-byte unchanged.
- **FR8 — Stats-pane consistency**: The stats-pane legend colours
  (`Added`/`Modified`/`Deleted` lines) use the same colours as the tree leaves,
  so `modified` reads blue in both. (See the Constraints reconciliation note —
  the shipped legend currently shows modified as yellow.)
  - **AC**: the stats-pane `Modified` line and a modified leaf render the same colour (blue).
- **FR7 — No behavioural regression**: Navigation, expand/collapse, sort keys
  (c/f/a/m/d/n), reverse, and the size column (tasks 11–13) behave identically;
  only row *presentation* gains glyph/colour/weight.
  - **AC**: existing interactions and the size column match baseline behaviour.
- **FR9 — On-screen legend**: The stats pane labels its category lines with the
  matching glyph in its colour (`+ Added`, `~ Modified`, `- Deleted`) and carries
  a `* = mixed (directory)` note, so the glyph/colour meanings are discoverable.
  Visible when the stats pane is shown; the tree glyphs are always present
  regardless of width.
  - **AC**: the stats pane shows the three glyph-prefixed, colour-matched category lines plus the `* mixed` note.

### User Stories
- **As a** user reviewing post-run changes **I want** changed files marked with a glyph and colour **so that** I can spot what changed without reading every row.
- **As a** user with red-green colour-vision deficiency **I want** an always-present status glyph **so that** I can tell added from deleted without relying on colour.
- **As a** user scanning a collapsed tree **I want** a directory's colour/glyph to summarise the kinds of change inside **so that** I know whether to expand it.

## Non-Functional Requirements
### Performance (NFR1)
- Styling is derived per visible row at render time from already-aggregated
  per-node stats; no new full-tree pass, no measurable render-latency or memory
  change versus baseline.

### Usability (NFR2)
- Consistent encoding (same glyph/colour meaning everywhere); glyph occupies a
  fixed-width position so column alignment does not shift between changed and
  unchanged rows; legible on both light and dark terminal backgrounds.

### Maintainability (NFR3)
- The mapping `(category / aggregated stats) → (glyph, colour, bold)` extends the
  existing `categoryStyle(n *dcfh.Node)` resolver (`render.go:276`) rather than
  adding a parallel one; it stays a single pure function with no I/O, unit-tested
  table-driven across all 7 directory combinations plus the four leaf cases.

### Security (NFR4)
- None applicable: TUI presentation only — no new inputs, file access, command
  execution, secrets, or environment-variable handling. No prompt-injection or
  FR4(a–e) surface introduced.

### Reliability (NFR5)
- Deterministic mapping; no panic for any node, including deleted-only
  directories (zero live bytes) and the empty-changeset case; the existing
  "(no changes to display)" path is preserved.

## Constraints
- Use tcell named colours (8 ANSI base colours) so terminal themes can remap;
  truecolour not required.
- **Reconcile with shipped code (load-bearing)**: the viewer currently colours
  modified **yellow** (`styleModified`, `render.go:270`) in both leaf rows and
  the stats-pane legend (`render.go:234-236`). FR1 re-specifies modified →
  **blue**, because the additive model needs the B channel for modified; yellow
  (= R+G) is reassigned to the added+deleted directory blend (FR2). `styleModified`
  must change to blue so tree and stats pane agree — this is a deliberate change
  to existing behaviour, not a regression under FR7 (which covers *interaction*,
  not the modified colour value).
- **No treeview-model change**: FR2–FR4 read the existing `Stats.Added/Modified/
  Deleted` and `Node.Cat` fields (already aggregated by `aggregate()` in
  `pkg/treeview.go`); no new fields are added.
- TUI-only: must not alter index, scan, or status/update computation.
- Glyph occupies a fixed-width column position so alignment with the existing
  expand/collapse marker and the size column does not shift between changed and
  unchanged rows.
- British spelling in prose ("colour").

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [ ] **Time**: Will this take >1 week? No
- [ ] **People**: Does this need >2 people working on different parts? No
- [ ] **Complexity**: Does this involve 3+ distinct concerns? No — one concern (render presentation)
- [ ] **Risk**: Are there high-risk components that need isolation? No
- [ ] **Independence**: Can parts be worked on separately? No

**Result**: 0 signals triggered — no decomposition.

## Acceptance Criteria
- [ ] AC1: Resolver returns the correct (glyph, colour, bold) for added/modified/deleted/unchanged leaves.
- [ ] AC2: Resolver returns the correct colour for all 7 directory combinations, independent of per-category counts.
- [ ] AC3: Directory glyph is the single-category glyph when one category is present, `*` when ≥2, none when unchanged.
- [ ] AC4: Bold is set iff subtree change count > 0.
- [ ] AC5: Status remains distinguishable with colour removed (glyph + bold).
- [ ] AC6: For a selected, changed row the cell buffer shows the glyph present and `Reverse(true)` over the category style; header/footer styles intact.
- [ ] AC7: Navigation, sort, reverse, and size-column behaviour match baseline.
- [ ] AC8: An all-unchanged directory renders default colour, no glyph, non-bold (the empty-blend boundary).
- [ ] AC9: A deleted-only directory (zero live files) renders red / `-` / bold — *not* unchanged (guards against keying "changed?" off `Stats.Files`, which excludes deletions, instead of `Added+Modified+Deleted`).
- [ ] AC10: An all-three (white) directory is distinguishable from an unchanged directory via bold + the `*` glyph.
- [ ] AC11: The stats-pane `Modified` legend line and a modified leaf render the same colour (blue).
- [ ] AC12: The stats pane shows glyph-prefixed colour-matched `+ Added` / `~ Modified` / `- Deleted` lines and a `* mixed` note (legend); column alignment preserved.

## Status
**Status**: Finished
**Next Action**: /cwf-design-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All FRs (FR1–FR9) and ACs (AC1–AC12) implemented and verified by test. The
modified=yellow→blue reconciliation (Constraints) and the additive-blend model
held without change. FR9 (legend) was added mid-flight with user approval.

## Lessons Learned
Pinning the deleted-only-directory case (AC9) and the all-three-vs-unchanged
distinction (AC10) as explicit ACs paid off — both became dedicated tests and
AC9 guards the real `Stats.Files`-excludes-deletions trap.
