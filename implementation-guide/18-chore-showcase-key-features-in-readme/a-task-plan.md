# Showcase key features in README - Plan
**Task**: 18 (chore)

## Task Reference
- **Task ID**: internal-18
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/18-showcase-key-features-in-readme
- **Baseline Commit**: 214b2d6de5316030361290aca04b7579acd25db5
- **Template Version**: 2.1

## Goal
Make the root `README.md` *sell* the tool's differentiating features — block-level
filesystem dedupe and the interactive-tree change-tracking viewer — instead of
listing them at their most literal, while keeping every claim accurate against the
shipped code (the honesty bar set by task 17).

## Success Criteria
- [ ] **SC1 — A "Features" section exists**: the README opens (before the command
  tables) with a short highlights section that names the differentiators a reader
  would otherwise miss; every bullet maps to a real, shipped capability.
- [ ] **SC2 — Dedupe is sold accurately**: the `dupes` coverage states that
  `--fs-dedupe` does **block-level** deduplication via Linux `FIDEDUPERANGE`
  (copy-on-write extent sharing on reflink filesystems — btrfs, XFS reflink=1,
  bcachefs), names the **Linux-only** constraint, says it *frees space without
  removing files*, and states the unsupported-FS behaviour accurately (skips and
  reports the device — not a silent no-op). It conveys that `dupes` has size-,
  date-, and hardlink-aware selection (by category + a pointer to `dcfh dupes help`,
  matching the README's table=purpose/help=flags pattern — not an exhaustive inline
  flag list, and not `--exclusive`, which is path-scoped rather than a filter). No
  invented flag or filesystem. *(Refined from a literal flag enumeration after
  plan-review: improvements/misalignment flagged inline enumeration as divergent
  from the README pattern; robustness corrected the no-op claim and the `--exclusive`
  semantics.)*
- [ ] **SC3 — Interactive-tree is sold as change tracking**: the README states it
  is a *change-tracking* viewer (not just disk-usage), covering the status glyphs
  (`+`/`~`/`-`/`*`) + colour, `z` hide-unchanged, and `r` sort/reverse, with the
  TTY requirement kept. Every key/behaviour matches `cmd/dcfh/internal/tui`.
- [ ] **SC4 — Accurate & link-clean**: zero invented features/flags; every relative
  link resolves; `remote` stays omitted (Hidden); British spelling; no removed-API
  references reintroduced.
- [ ] **SC5 — No regression**: docs-only; `go build ./...` and `go test ./...`
  remain green; no `.go` source behaviour change.

## Original Estimate
**Effort**: <0.5 day
**Complexity**: Low
**Dependencies**: None. Builds on the task-17 README (`214b2d6`). Pure prose; no
code change. Feature surface already verified to exist (fs-dedupe + tui).

## Major Milestones
1. **Confirm the sell-list**: enumerate the shipped differentiators worth
   surfacing (dedupe, change-tracking tree, ~9× faster-than-git, snapshots — scope
   pinned in implementation-plan) and verify each against `cmd/`/`pkg/`.
2. **Write**: add the Features section; expand the `dupes` and `--interactive-tree`
   subsections; keep caveats (Linux-only, TTY) honest.
3. **Verify**: link sweep, removed-API / invented-flag grep, `go build`/`go test`,
   manual read-through.

## Risk Assessment
### High Priority Risks
- **Risk 1 — Over-selling / inaccuracy**: marketing language drifts ahead of the
  code (claims a filesystem, flag, or behaviour that isn't shipped), reintroducing
  the very drift task 17 fixed.
  - **Mitigation**: every claim grep-verified against `cmd/dcfh/dupes.go`,
    `pkg/fsdedupe/`, and `cmd/dcfh/internal/tui/` before it lands; SC2/SC3 require
    exact flag/key matches; testing phase greps for invented flags.

### Medium Priority Risks
- **Risk 2 — Platform caveats buried**: readers on macOS/non-reflink FS try
  `--fs-dedupe` and it no-ops, or try `--interactive-tree` over a pipe.
  - **Mitigation**: state Linux-only (FIDEDUPERANGE) and TTY-required explicitly
    next to each feature, not in a footnote.
- **Risk 3 — Scope creep into a full marketing rewrite**: the README turns into a
  landing page, ballooning the chore.
  - **Mitigation**: keep the existing understated-technical tone; additive edits to
    a bounded set of sections (Features + dupes + interactive-tree); pin the exact
    section list in the implementation-plan.

## Dependencies
- None external. Reads the current `cmd/`/`pkg/` surface as the source of truth.

## Constraints
- Docs-only — no `.go` behaviour change (a stray `pkg/doc.go`-style comment is the
  most that could be justified, and only if needed).
- British spelling in prose; understated-technical tone (consistency with task 17).
- `remote` remains omitted from user-facing copy while `Hidden: true`.

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [x] **Time**: <0.5 day — well under a week. No decomposition.
- [x] **People**: single author. No decomposition.
- [x] **Complexity**: one concern (README prose). No decomposition.
- [x] **Risk**: only accuracy risk, mitigated by grep-verification. No isolation needed.
- [x] **Independence**: one file, one cohesive edit. No decomposition.

**Verdict**: 0/5 signals — no decomposition; proceed as a single chore.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All five success criteria met. SC1 Features section added (teaser-only); SC2
dedupe sold accurately (skip-and-report, no `--exclusive`); SC3 interactive-tree
sold as change-tracking; SC4 link-clean, `remote` omitted; SC5 build/test green,
docs-only. Delivered within the <0.5 day estimate. See `j-retrospective.md`.

## Lessons Learned
Pinning verification into the planning phases (a/d/e) kept exec and testing
mechanical — the accuracy risk that defines this kind of chore was retired before
any prose was written. See `j-retrospective.md`.
