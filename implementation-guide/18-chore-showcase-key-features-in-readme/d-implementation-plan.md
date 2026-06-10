# Showcase key features in README - Implementation Plan
**Task**: 18 (chore)

## Task Reference
- **Task ID**: internal-18
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/18-showcase-key-features-in-readme
- **Template Version**: 2.1

## Goal
Make `README.md` sell the differentiators without inventing anything. Three
additive edits to the existing task-17 README, each grounded in a verified source
location. No `.go` change.

## Source-of-truth verification (done before this plan)
Every claim the edits make is pinned to a current source string:
- **Dedupe**: `cmd/dcfh/dupes.go:70–76` — "With --fs-dedupe (Linux only), after
  listing duplicates dcfh asks the kernel via FIDEDUPERANGE to share the underlying
  extents copy-on-write on reflink-capable filesystems (btrfs, XFS with reflink=1,
  bcachefs). … --fs-dedupe implies --ignore-hardlinks … defaults --min-size to
  4096". Filter flags: `-H/--ignore-hardlinks`, `--fs-dedupe`,
  `--min-size/--max-size`, `--start-date/--end-date` (`dupes.go:22–24,53–61`).
- **Unsupported-FS behaviour** (robustness pin): `dupes.go:281` /
  `pkg/fsdedupe/fsdedupe_linux.go` — on a device whose filesystem lacks
  FIDEDUPERANGE, dcfh **skips that device and reports it**
  (`skipped device %s: filesystem does not support FIDEDUPERANGE`). It is *not* a
  silent no-op; the README must say "skips and reports", not "no-ops".
- **`--exclusive`** (`dupes.go:42–46`): defaults to `yes` and is **path-scoped** —
  it controls whether cross-path groups report when paths are given, not a flat
  size/date filter. Do **not** list it among the selection filters in the sell
  copy (avoids the nuance); leave it to `dcfh dupes help`.
- **Interactive tree**: footer help string `cmd/dcfh/internal/tui/render.go:154` —
  `↑/↓ move  →/← expand/collapse  c/f/a/m/d/n sort  r reverse  z hide  q quit`;
  status glyphs/colour via `nodeStyle` in `render.go` (task 15); `z` hide-unchanged
  (task 16). TTY guard: viewer is status/update-only, already documented.
- **Speed**: `pkg/doc.go:5–6` — "~9× faster than `git status` on the same tree"
  (tracks change, not content). Established project claim; surface verbatim-ish.
- **`remote`** stays omitted: `cmd/dcfh/remote.go` `Hidden: true`.

## Files to Modify
### Primary Changes
- `README.md` — three additive edits (below). The only file changed.

### Supporting Changes
- None. No `.go`, no config, no test code (docs-only). `go build`/`go test` are
  run as the regression guard, not modified.

## Implementation Steps
### Step 1: Setup
- [ ] Confirm on branch `chore/18-showcase-key-features-in-readme`, tree clean.
- [ ] Re-read the current `README.md` sections being touched (lead-in lines 1–7,
  the `dcfh` commands table ~59–74, `### Interactive tree viewer` ~76–80).

### Step 2: Edit A — add a `## Features` section (teasers only)
- [ ] Insert a new `## Features` section immediately after the lead-in paragraph
  (after line 7, before `## The tools` at line 8). 4 **one-line teaser** bullets —
  the selling hook, NOT the detail. Per plan-review (improvements + misalignment):
  the FIDEDUPERANGE/filesystem list lives ONLY in Edit B, the glyph list ONLY in
  Edit C, and these bullets stay short to avoid a third/second maintenance copy.
  - **Block-level filesystem dedupe** — reclaim space from duplicates without
    deleting files (`dcfh dupes --fs-dedupe`; see below). *(No FS list here.)*
  - **Change-tracking tree viewer** — `--interactive-tree` shows what changed at a
    glance, in colour (see below). *(No glyph/key list here.)*
  - **Fast by design** — tracks change, not content: `dcfh status` runs roughly 9×
    faster than `git status` on the same tree (`pkg/doc.go`). *(Hook only — the
    "only hashes changed files" mechanism already lives at README lines 56–57; do
    NOT re-explain it here.)*
  - **Snapshots, diffs, and nested-repo discovery** on top of the mmap-loaded
    index. *(Drop the "SHA-1 + Unix metadata" clause — the lead-in lines 3–6
    already state it; this bullet carries only the additive verbs.)*
  - Tone: understated-technical; no marketing adjectives without evidence. Adjective
    for the index: match the lead-in ("git-inspired") — don't introduce a competing
    "git-compatible" in the same screenful.

### Step 3: Edit B — sell dedupe near the `dupes` command (canonical detail)
- [ ] After the `dcfh` commands table, add a short `### Duplicate detection and
  dedupe` subsection. This is the **one** place the dedupe detail lives:
  - One sentence: `dcfh dupes` matches duplicate **content** (not just names), and
    supports size-, date-, and hardlink-aware selection (point to `dcfh dupes help`
    for the exact flags — do NOT enumerate every flag inline; the README pattern is
    table=purpose, help=flags, per line 74). Do **not** mention `--exclusive` (it is
    path-scoped, not a filter — see source pin).
  - One paragraph on `--fs-dedupe`: **Linux-only**, block-level via `FIDEDUPERANGE`,
    copy-on-write extent sharing on reflink filesystems (btrfs, XFS reflink=1,
    bcachefs); implies `--ignore-hardlinks` and defaults `--min-size` to 4096. State
    it *frees space without removing files*, and that on a filesystem without
    FIDEDUPERANGE support it **skips that device and reports it** (NOT a silent
    no-op — source pin `dupes.go:281`).
  - A 2–3 line fenced example (`dcfh dupes --min-size 1M`, `dcfh dupes --fs-dedupe`).

### Step 4: Edit C — expand `### Interactive tree viewer`
- [ ] Replace the single-sentence body with: it is a **change-tracking** viewer
  (not just disk usage); each row carries a status glyph + colour
  (`+` added / `~` modified / `-` deleted / `*` mixed dir); `z` hides unchanged
  entries; `c/f/a/m/d/n` sort and `r` reverse; `↑/↓`/`→/←` navigate; `q` quits.
  Keep the TTY requirement and that it is `status`/`update`-only. Optionally show
  the footer key-line verbatim in a fenced block.

### Step 5: Validation (mechanical — full checks in e-testing-plan)
- [ ] Link sweep: every relative `](…)` in `README.md` resolves (`test -e`).
- [ ] Invented-flag / removed-API grep: each flag named in the README exists in
  `cmd/dcfh/`; zero `DirectoryCache`/`FileEntry`/`NewDirectoryCache`.
- [ ] `go build ./...` + `go test ./...` green (proves docs-only, no regression).
- [ ] `git diff --name-only` shows only `README.md` (+ this wf file).

## Code Changes
Prose edits only — exact wording finalised at exec time against the source strings
quoted under "Source-of-truth verification". No code diff to pre-author here
(per repo convention: no pseudocode in plans unless the change is difficult; this
is plain Markdown). The three edit anchors (lead-in, dupes table, interactive-tree
subsection) are fixed above.

## Test Coverage
**See e-testing-plan.md for complete test plan** — static verification: per-claim
source-match greps, link sweep, invented-flag grep, build/test regression.

## Validation Criteria
**See e-testing-plan.md** — maps SC1–SC5 to concrete greps/builds.

## Scope Completion
**IMPORTANT**: Complete all planned implementation before marking task Finished.

Deferring work creates technical debt and scope creep. Task 37 deferred documentation updates,
marked the task complete anyway, and created Task 38 to fix the deferred work.

**If you must defer work**:
1. Get user approval with clear rationale
2. Update success criteria to reflect descoped work
3. Create follow-up task immediately
4. Document deferral in Actual Results section

## Status
**Status**: Finished
**Next Action**: /cwf-testing-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All three edits (A: Features section, B: dedupe subsection, C: interactive-tree
expansion) landed in `README.md` as planned, the only file changed. One in-exec
correction: the sort-key labels in Edit C were re-pinned to `tui/sort.go`
(changed-bytes/changed-files/added/modified/deleted/name) after a wrong first
draft. See `f-implementation-exec.md`.

## Lessons Learned
The plan pinned the footer string and glyphs but not the sort-key *semantics*
(the `sort.go` switch) — a definition-site pin would have made the first draft
correct. Pin the meaning, not just the surface. See `j-retrospective.md`.
