# Review and refactor docs into doc directory - Implementation Plan
**Task**: 17 (feature)

## Task Reference
- **Task ID**: internal-17
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/17-review-and-refactor-docs-into-doc-directory
- **Template Version**: 2.1

## Goal
Execute the design: relocate five docs into `docs/`, fix references, fix
`ARCHITECTURE.md`, banner three historical docs, rewrite README, add a tagged
`docs/README.md` index — mechanically, in an order that never leaves a dangling
link mid-stream.

## Workflow
Relocate → fix-in-place (so links target final paths) → rewrite README → index →
verify. The git mv comes first so every subsequent edit references the final
`docs/` locations.

## Files to Modify
### Primary Changes
- `git mv` → `docs/`: `ARCHITECTURE.md`, `ARCHITECTURE-IMPROVEMENTS.md`,
  `architecture-v0.7.md`, `design.md`, `streaming-iterator-architecture.md`.
- `docs/ARCHITECTURE.md` — four targeted fixes + §5 table → links (Decision 3).
- `docs/architecture-v0.7.md`, `docs/streaming-iterator-architecture.md`,
  `docs/design.md` — prepend historical banner (Decision 5).
- `README.md` (root) — full CLI-first rewrite (Decision 2).
- `docs/README.md` (NEW) — single tagged index (Decision 1 / FR6).
- `pkg/doc.go` — comment-only: `:32` locator + `:65–67` RWMutex reframe.

### Supporting Changes
- `docs/ARCHITECTURE-IMPROVEMENTS.md` — confirm sibling cross-refs resolve
  in-`docs/` (lines 4, 175–176); no content change otherwise (Decision 4).
- `docs/architecture-v0.7.md:1155` — the `streaming-iterator-architecture.md`
  mention resolves in-`docs/` (sibling).
- `CLAUDE.md` — review-gated minimal correction (Decision 6): add
  `completion`/`diff`/`remote`/`subrepo` to the command list and fix the
  `--interactive-tree` "global" mislabel. **Default: apply the minimal fix**;
  skip only if the review gate says leave CLAUDE.md untouched.

### Out of scope (do NOT touch)
`.cwf/**`, `.cwf-skills/**`, `.cwf-rules/**`, `implementation-guide/**`,
`.claude/sessions/**`; `CHANGELOG.md` (its `docs/changelog-old.md` link is already
correct — FR7); existing `docs/` contents (`changelog-old.md`, `feasibility/`,
`ssh-shell-mode.md`).

## Implementation Steps

### Step 1: Setup & reference map
- [ ] Re-read `c-design-plan.md` (FR3 table + reference-fix inventory).
- [ ] Capture a before-map: grep the repo (excluding out-of-scope trees) for bare
  references to the five moving basenames and the `pkg/doc.go` "repo root"
  phrase; save to the task scratch dir as the FR2 checklist.

### Step 2: Relocate (`git mv`)
- [ ] `git mv` each of the five docs into `docs/` (one per file, history
  preserved). Do **not** rename.
- [ ] Verify: `ls *.md` at root = exactly `README.md`, `CHANGELOG.md`,
  `BACKLOG.md`, `CLAUDE.md` (AC1); each moved file resolves under `docs/`;
  `git log --follow` shows continuity for one spot-checked file.

### Step 3: Fix `docs/ARCHITECTURE.md` (Decision 3)
- [ ] `:57` "four entry storage modes" → "three concrete implementations + one
  conceptual (unimplemented) mode".
- [ ] `:60` delete the `BEIndexFileIOEntry` / `binary_entry_index_file.go` row;
  **keep** the `:61` `binary_entry_index_file_mmap.go` / `BEIndexFileMmapEntry`
  row (live — verify it still exists in `pkg/` first).
- [ ] `:137` strike `AppendEntryToScanIndex` / `pkg/index.go:1008` (gone).
- [ ] `:186` (§6) reframe RWMutex as "defensive, not load-bearing" — **reuse
  CLAUDE.md's exact wording** ("defensive rather than load-bearing… the mremap'd
  scan-index path… has been removed") so this and the `pkg/doc.go` reframe
  (Step 5) cannot drift apart.
- [ ] Re-verify the Layer-2 file/line table rows against current `pkg/` (fix any
  drifted file:line while here).
- [ ] **Path-citation rule**: bare `pkg/…`/`cmd/…` inline-code path citations in
  the body (e.g. `:50` `pkg/config.go:469`, the Layer tables) are repo-root-
  relative **by convention and stay unchanged** — only true markdown *links* get
  a `../` prefix. Do not prefix path citations.
- [ ] §5 table (`:256–263`): convert the bare names in the **left "Doc" column
  only** (leave the "When to read it" description prose unlinked) to markdown
  links **by target** — siblings (`architecture-v0.7.md`,
  `streaming-iterator-architecture.md`, `design.md`, `ARCHITECTURE-IMPROVEMENTS.md`)
  bare-relative; `cmd/dcfhfind/DESIGN.md` & `cmd/dcfhfix/DESIGN.md` →
  `../cmd/.../DESIGN.md`; `CLAUDE.md` & `BACKLOG.md` → `../CLAUDE.md` /
  `../BACKLOG.md`.

### Step 4: Historical banners (Decision 5)
- [ ] Prepend the banner block (immediately under the H1) to
  `docs/architecture-v0.7.md` (spec), `docs/streaming-iterator-architecture.md`
  (proposal), `docs/design.md` (pre-v0.7 design). `design.md`'s banner also names
  the concrete renames (`main.idx`/`cache.idx`, `NewMetaStore`, 4-stage pipeline).
- [ ] Banner links to `ARCHITECTURE.md` resolve (same `docs/` dir).

### Step 5: Out-of-doc reference fixes (FR2)
- [ ] `pkg/doc.go:32` "at the repo root" → "in `docs/`" (comment-only). Scope:
  only this locator and the `:65–67` reframe below change in `doc.go` — its
  five-layer paraphrase (`:30`+) is not wrong, only differently organised, and is
  left as-is.
- [ ] `pkg/doc.go:65–67` reframe the RWMutex `mremap`-SIGSEGV claim using the
  **same CLAUDE.md wording** as Step 3's §6 fix, so the two agree verbatim.
- [ ] `docs/ARCHITECTURE-IMPROVEMENTS.md` (lines 4, 175–176): these are **bare
  prose** sibling names that **stay bare** (Decision 4 = no content change; do not
  linkify). One permitted micro-fix: `:93`'s stale `architecture-v0.6.md` prose
  mention → `architecture-v0.7.md` (no `v0.6` doc exists) — honest, one word,
  optional.
- [ ] `docs/architecture-v0.7.md:1155` `streaming-iterator-architecture.md`
  mention resolves in-`docs/` (sibling).

### Step 6: README rewrite (Decision 2)
- [ ] Replace `README.md` wholesale, CLI-first and **source-grounded** (verify
  every command/flag against `cmd/`, not CLAUDE.md): tools overview
  (`dcfh`/`dcfhfind`/`dcfhfix`); install/build (`make build`; goreleaser
  deb/rpm/tar.gz, Linux-only); runnable Quick Start (`dcfh init <dir>` → `status`
  → `update` → `dupes`); `dcfh` daily subcommands; `--interactive-tree`
  (status/update, TTY-only); brief `dcfhfind`/`dcfhfix`; `diff`/`subrepo`/
  `completion` as one-line "see `dcfh <cmd> help`"; a "Documentation" section.
  **Omit `remote`** — it is `Hidden: true` (`cmd/dcfh/remote.go:23`), the
  machine-only SSH audit-mode endpoint, not an end-user command.
- [ ] Zero occurrences of `DirectoryCache` / `FileEntry` / `NewDirectoryCache`
  (AC4 precedence — delete/replace, never "make compile"). No surviving `go`
  block uses the removed API.

### Step 7: `docs/README.md` index (FR6)
- [ ] Create `docs/README.md`: one row per doc with a one-line purpose and a
  CURRENT/HISTORICAL marker **copied verbatim from the FR3 table** (single source
  of truth). Include the existing `docs/` contents (changelog-old, ssh-shell-mode,
  feasibility) for completeness.
- [ ] Root README "Documentation" section points to `docs/README.md` and links
  `docs/ARCHITECTURE.md` directly (≤2 clicks; no duplicate tag list).

### Step 8: CLAUDE.md minimal correction (Decision 6 — review-gated)
- [ ] Default: add `completion`/`diff`/`subrepo` (and `remote`, flagged as the
  hidden SSH-internal endpoint) to the command list and correct
  `--interactive-tree` from "global option" to "status/update only". Nothing else.
  (Skip iff the review gate asked to leave CLAUDE.md untouched.)
- [ ] If CLAUDE.md text is touched, its `## Security Review` section is unchanged
  (NFR4).

### Step 9: Verify (exit criteria — detail in e-testing-plan)
- [ ] `go build ./...` and `go test ./...` green (only `.go` edit = `pkg/doc.go`
  comments).
- [ ] Link-integrity sweep: enumerate every relative **markdown link** in the
  moved/edited docs + `docs/README.md`; stat each target; zero misses for links
  this task authored or touched (catches the §5 mixed-target case). Pre-existing
  **bare-prose** mentions of non-moved files are not links and are out of scope.
- [ ] FR2 before-map re-run: the gate is the **five moving basenames + the
  `pkg/doc.go` "repo root" phrase** only — confirm none remain stale in-scope. It
  is **not** a sweep of every `pkg/…` citation (those stay repo-root-relative per
  the Step 3 path-citation rule).
- [ ] `CHANGELOG.md → docs/changelog-old.md` still resolves (FR7); existing
  `docs/` contents untouched.
- [ ] `git status`: no file under the out-of-scope trees modified.

## Code Changes
Doc-only except `pkg/doc.go` (two comment edits). No before/after code block
needed — the reference-fix inventory in `c-design-plan.md` is the diff spec.

## Test Coverage
**See e-testing-plan.md for complete test plan** — centred on the link-integrity
sweep, the grep-for-stale-refs gate, AC1 root-tidy check, and `go build`/`go test`.

## Validation Criteria
**See e-testing-plan.md.** Exit = AC1–AC7 + build/test green + link sweep clean.

## Scope Completion
**IMPORTANT**: Complete all planned implementation before marking task Finished.
The one explicitly conditional item is Step 8 (CLAUDE.md), gated on the review;
its default is "apply minimal". Everything else is unconditional.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All planned steps executed (see `f-implementation-exec.md` for per-step detail).
Two deviations: §6 enlarged (phantom writer citation), and the optional `:93`
micro-fix skipped to preserve a history note. Only `.go` change: `pkg/doc.go`
comments.

## Lessons Learned
A step-by-step plan keyed to the FR3 verdicts left little room for judgement
drift at exec time. See `j-retrospective.md`.
