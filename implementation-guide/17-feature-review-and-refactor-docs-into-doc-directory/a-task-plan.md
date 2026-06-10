# Review and refactor docs into doc directory - Plan
**Task**: 17 (feature)

## Task Reference
- **Task ID**: internal-17
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/17-review-and-refactor-docs-into-doc-directory
- **Baseline Commit**: 0634f9a78762d0c035c19d55ad623ecf02996176
- **Template Version**: 2.1

## Goal
Relocate the project's architecture/design documentation into the existing
`docs/` directory and bring every retained doc (README + architecture set) back
into sync with the current v0.7 CLI codebase, so the repository's documentation
accurately reflects the shipped tool.

## Success Criteria
- [ ] **SC1 — Root is tidy**: the repository root holds only `README.md`,
  `CHANGELOG.md`, `BACKLOG.md`, and `CLAUDE.md` as Markdown; every other
  doc (`ARCHITECTURE.md`, `ARCHITECTURE-IMPROVEMENTS.md`, `architecture-v0.7.md`,
  `design.md`, `streaming-iterator-architecture.md`) lives under `docs/`.
- [ ] **SC2 — No dangling references**: every retained cross-reference to a moved
  doc resolves (README, `CLAUDE.md`, `pkg/doc.go`, and any surviving doc); a
  link/anchor sweep reports zero broken internal doc links.
- [ ] **SC3 — README matches the product**: README documents the actual tools
  (`dcfh` / `dcfhfind` / `dcfhfix`) and `--interactive-tree`, contains no
  reference to the removed `DirectoryCache` / `FileEntry` library API, and any
  code/usage sample is valid against current code (or clearly framed as CLI).
- [ ] **SC4 — Architecture docs are honest**: each architecture/design doc kept
  under `docs/` is either verified current against code or explicitly marked
  historical/superseded — no doc silently presents removed architecture as
  current.
- [ ] **SC5 — Build/tests unaffected**: docs-only change; `go build ./...` and
  `go test ./...` remain green; no source behaviour change.

## Original Estimate
**Effort**: 1–2 days
**Complexity**: Medium
**Dependencies**: None external. Docs-only (no code behaviour change). Acts as a
prerequisite for a clean **public** release (origin push / GitHub release).

## Major Milestones
1. **Inventory & classify**: enumerate every doc; classify each as
   *current* / *historical* / *needs-rewrite*; map all cross-references to the
   moveable docs.
2. **Relocate & re-link**: `git mv` the non-root docs into `docs/`; fix every
   reference uncovered in milestone 1.
3. **Content resync**: rewrite README around the CLI trio + interactive-tree;
   update or explicitly mark the architecture/design docs against current code.
4. **Verify**: internal link/anchor sweep, `go build`/`go test`, manual
   read-through for accuracy.

## Risk Assessment
### High Priority Risks
- **R1 — README rewrite inaccuracy / scope creep**: a rewritten README that still
  misstates the tool (or balloons into a manual) is worse than a stale one.
  - **Mitigation**: ground every claim in current code (grep/verify command
    surface, flags, examples); keep README a front-door overview, deferring depth
    to `docs/`; CLI examples must be runnable.

### Medium Priority Risks
- **R2 — Broken cross-references after the move**: links in README/`CLAUDE.md`/
  `pkg/doc.go`/other docs break when files move.
  - **Mitigation**: build a grep-based reference map before the move; re-run it
    after; internal link/anchor sweep in milestone 4.
- **R3 — Misclassifying a historical doc as current (or vice versa)**: e.g.
  treating pre-v0.7 `ARCHITECTURE.md`/`design.md` as describing the shipped code.
  - **Mitigation**: diff each doc's claims against code; when uncertain, mark
    "historical (pre-vX)" rather than assert currency — `CLAUDE.md` is the
    authoritative current-architecture reference to cross-check against.
- **R4 — Clobbering existing `docs/` contents**: `docs/` already holds
  `changelog-old.md`, `feasibility/`, `ssh-shell-mode.md`, and `CHANGELOG.md`
  already links `docs/changelog-old.md`.
  - **Mitigation**: additive move only; preserve existing structure and the
    `CHANGELOG → docs/changelog-old.md` link.

## Dependencies
- **External**: none.
- **Downstream**: a clean public release (resolving the origin push / GitHub
  release) wants this done first — a README whose Quick Start won't compile must
  not ship publicly.
- **Out of scope (do not touch)**: `.cwf/**`, `implementation-guide/**`,
  `.claude/sessions/**` — CWF-internal and historical session docs are not part
  of this refactor.

## Constraints
- **Docs-only**: no source/behaviour change (SC5).
- **Target dir**: the **existing** `docs/` (plural), not a new `doc/` — confirmed
  with the user; consolidates with what's already there.
- **`CLAUDE.md` stays at root**: required there by Claude Code; it is also the
  authoritative current-architecture reference for resync.
- **British spelling** in prose (favour, colour, optimise…), per project
  convention.
- **Use `git mv`** so history follows the files.

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [ ] **Time**: Will this take >1 week? **No** (1–2 days).
- [ ] **People**: Does this need >2 people? **No** (single owner).
- [x] **Complexity**: 3+ distinct concerns? **Yes** — (a) relocate+re-link,
  (b) README rewrite, (c) architecture/design-doc reconciliation.
- [ ] **Risk**: High-risk components needing isolation? **No** (docs-only,
  reversible).
- [x] **Independence**: Can parts be worked on separately? **Yes** — the
  relocation, the README rewrite, and the architecture-doc review are largely
  independent.

**Result**: 2 signals (Complexity, Independence). Subtasks are *viable*
(17.1 relocate+re-link / 17.2 README rewrite / 17.3 architecture-doc review).
Recommendation: **keep as a single task with three milestones** — single owner,
tightly-coupled docs, <1 week, and the inventory step (milestone 1) is shared
across all three concerns so splitting would duplicate it. Flagged here for the
review gate; decompose only if the review prefers independent landing.

## Status
**Status**: Finished
**Next Action**: /cwf-requirements-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All five success criteria (SC1–SC5) met. Delivered in a single session, under the
1–2 day estimate. No scope added or dropped; one optional micro-fix deliberately
skipped (history-note preservation).

## Lessons Learned
The estimate was beaten because the complexity was verification, not editing, and
the design phase front-loaded it. See `j-retrospective.md` for the full variance
analysis.
