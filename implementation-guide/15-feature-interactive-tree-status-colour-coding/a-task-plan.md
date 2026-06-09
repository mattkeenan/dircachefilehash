# interactive-tree status colour coding - Plan
**Task**: 15 (feature)

## Task Reference
- **Task ID**: internal-15
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/15-interactive-tree-status-colour-coding
- **Baseline Commit**: ca08ce6c5d10b6fbf95d094f3de4c92fe5e83664
- **Template Version**: 2.1

## Goal
Make change status legible at a glance in the interactive-tree viewer by giving
each node a status glyph, a status colour, and bold weight, with directories
blending their descendants' statuses additively.

## Success Criteria
- [ ] Each changed leaf shows a status glyph (`+`/`~`/`-`) as the primary signal plus a reinforcing colour (added=green, modified=blue, deleted=red); unchanged leaves show no glyph, default colour, non-bold.
- [ ] Directory rows blend descendant statuses additively/presence-based: add+mod=cyan, mod+del=magenta, add+del=yellow, all-three=white — a single changed descendant flips the corresponding channel. A directory whose changed descendants are all one status shows that status's glyph; any mix shows `*`.
- [ ] A node is bold iff `count(Added+Modified+Deleted) > 0` over its subtree; unchanged nodes are non-bold (this also disambiguates all-three white from unchanged white).
- [ ] Glyph/colour/bold compose correctly with the existing selected-row reverse-video highlight and stay readable.
- [ ] No regression to existing sort, navigation, or size-column behaviour (tasks 11–13); resolver behaviour is unit-tested across all status combinations.

## Original Estimate
**Effort**: 1–2 days
**Complexity**: Low–Medium (confined to the TUI render layer; the node `Category`/`Stats` data it needs already exists in `pkg/treeview.go`)
**Dependencies**: tcell (already vendored), existing treeview model and render layer

## Major Milestones
1. **Style resolver**: pure function mapping a node's category / aggregated stats → (glyph, foreground colour, bold) per the additive RGB scheme.
2. **Render integration**: apply glyph + resolved style in `cmd/dcfh/internal/tui/render.go`, composing with the selected-row reverse-video.
3. **Verification**: unit tests for the resolver (7 colour combinations + unchanged + bold rule) and a manual multi-terminal eyeball pass.

## Risk Assessment
### High Priority Risks
- **Terminal/theme colour variance**: named ANSI colours are remapped by themes, and some terminals render "bold" as a brighter colour rather than heavier weight.
  - **Mitigation**: use tcell named colours; keep the glyph as the *primary* signal so colour is only reinforcement; eyeball on ≥2 terminals (light + dark).

### Medium Priority Risks
- **Selection composition**: foreground colour + reverse-video swaps fg/bg on the selected row, which could hurt readability.
  - **Mitigation**: explicit style compose; test selected coloured rows specifically.
- **Glyph set** (decided): leaves use `+`/`~`/`-`; a single-status directory uses that status's glyph; any mixed directory uses `*` regardless of which combination. Colour still carries the specific combination.
- **Red/green accessibility**: added=green vs deleted=red is the classic colour-vision-deficiency confusion pair.
  - **Mitigation**: mandatory always-present glyph (decided) is the colour-blind-safe primary signal.

## Dependencies
- `github.com/gdamore/tcell/v2` (already a dependency)
- Existing `pkg/treeview.go` model: `Node`, `Stats` (Added/Modified/Deleted counts + *Bytes), `Category`, and `DeletedSizes` (deleted last-known sizes already plumbed)
- Existing render layer `cmd/dcfh/internal/tui/render.go`

## Constraints
- TUI-only change — must not alter index, scan, or status/update computation
- Must follow existing tcell style patterns in the `tui` package and not regress tasks 11–13
- British spelling in prose ("colour")
- No new scan/stat plumbing expected: the tree already carries per-node status counts

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [ ] **Time**: Will this take >1 week? No (1–2 days)
- [ ] **People**: Does this need >2 people working on different parts? No
- [ ] **Complexity**: Does this involve 3+ distinct concerns? No — one concern (render styling)
- [ ] **Risk**: Are there high-risk components that need isolation? No
- [ ] **Independence**: Can parts be worked on separately? No

**Result**: 0 signals triggered — single task is appropriate, no decomposition.

## Status
**Status**: Finished
**Next Action**: /cwf-requirements-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Delivered within the 1–2 day estimate. All five success criteria met; the three
planned milestones (resolver, render integration, verification) completed as
scoped, plus an agreed on-screen legend addition (FR9). See j-retrospective.md
for full variance analysis.

## Lessons Learned
The "Low–Medium, confined to render layer" sizing held — effort skewed to
plan-review and testing rather than code (~40 net production lines). The one
estimate-relevant surprise (a test plan referencing an unrenderable root row)
was a testing-phase catch, not a planning miss.
