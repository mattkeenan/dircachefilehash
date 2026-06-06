# interactive-tree gdu-style post-run view - Requirements
**Task**: 11 (feature)

## Task Reference
- **Task ID**: internal-11
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/11-interactive-tree-gdu-style-post-run-view
- **Template Version**: 2.1

## Goal
Define functional and non-functional specifications for a gdu/Midnight-Commander-style post-run interactive tree view, launched via `--interactive-tree` on `dcfh status` and `dcfh update`, for ad-hoc assessment of the resulting state (status) or change (update).

## Data-Source Decision (resolves the FR4/FR7 tension)
The thin command result structs do **not** carry the data the stats pane needs:
`StatusResult` (`pkg/status.go`) holds only `Modified/Added/Deleted []string` path
lists plus three aggregate byte totals — no per-file sizes, no per-directory
rollup, and no "unchanged" set. `UpdateResult` (`pkg/repo.go`) holds only
`FileCount`, `TotalSize`, `PathsUpdated`. Building a real before/after, per-node,
size-aggregating tree from those alone is impossible.

**Decision**: The viewer is built from the **already-loaded in-memory index
structures** the command holds at completion — the main+cache merged skiplist and
the current-scan entries for `status`; the pre-update and post-update in-memory
index for `update`. The hard constraint is therefore "**no second *filesystem*
walk**" — reading in-memory skiplists/entries already populated during the run is
permitted and is not a re-walk. This keeps per-file sizes and genuine before/after
available without re-`stat`ing the tree. (Open design item: `update` must retain
its pre-merge in-memory state before the atomic rename so "before" survives; the
channel pipeline already holds this prior to write — design to confirm.)

## Functional Requirements
### Core Features
- **FR1 — Flag exposure**: A `--interactive-tree` boolean flag is registered **per-command** on `status` and `update` only (via each command's `Flags()` in `cmd/dcfh/status.go` / `cmd/dcfh/update.go`), **not** as a root persistent flag — a persistent flag would be silently accepted by every subcommand. It appears in each command's help text and defaults to off. When absent, the command's behaviour and output are byte-for-byte unchanged.
  - *Accept (FR1)*: `dcfh status --interactive-tree` and `dcfh update --interactive-tree` parse without error and both list the flag in `--help`.
  - *Accept (FR1)*: `dcfh dupes --interactive-tree` (and other subcommands) reject the unknown flag, confirming it was not added persistently.

- **FR2 — Launch timing**: The TUI launches only after the command's normal Hwang-Lin run completes successfully (status: cache refresh + dirty detection; update: atomic main-index replacement). For `update`, the index mutation is fully committed before the viewer opens. The command's normal textual/JSON summary is still produced.
  - *Accept*: With the flag, the command performs its full normal work first; on a run that would normally mutate the index, the index is in its final post-run state when the viewer appears.

- **FR3 — TTY / output-mode guard**: The viewer is launched only when standard output is an interactive terminal and `--json` is not in effect. When stdout is not a TTY (piped/redirected) or `--json` is set, the flag is inert: the command runs normally, emits its standard output, and skips the viewer without error. A non-fatal notice MAY be emitted to stderr explaining the skip. (TTY detection is net-new infrastructure — there is no existing helper; the detection approach/dependency is a design item.)
  - *Accept*: `dcfh status --interactive-tree | cat` and `dcfh status --interactive-tree --json` both complete with normal output and exit 0, no TUI, no hang.

- **FR4 — Tree pane (left)**: The viewer renders a navigable directory tree on the left, rooted at the repository root, built **solely from in-memory data already populated by the command** (no second filesystem walk — see Data-Source Decision). Each node shows its name and a per-node summary glyph/figure (aggregate size and/or change indicator). Directories aggregate their descendants. The tree is a **point-in-time snapshot** of the completed run; filesystem changes occurring during viewing are out of scope and not reflected.
  - *Accept*: The set of files/dirs shown is derived from the command's in-memory index/scan structures; launching the viewer triggers no additional `stat`/filesystem walk (verified by no extra walk/stat calls).

- **FR5 — Navigation**: Keyboard navigation supports at minimum: move selection up/down, enter/expand a directory, go to parent/collapse, and quit. Quit returns control to the shell with the terminal restored and a normal exit code.
  - *Accept*: Each documented key performs its action; quitting leaves the terminal usable (no residual raw mode, cursor restored).

- **FR6 — Stats pane (right, width-gated)**: When terminal width ≥ a defined threshold, a right pane shows a **before/after** filesystem-stats comparison for the currently selected node, aggregated over that node's subtree. Below the threshold, the tree pane uses the full width and the right pane is suppressed. The layout adapts live to terminal resize.
  - *Accept*: At wide width both panes render; at narrow width only the tree renders full-width; resizing across the threshold re-flows without crash or garbled output.

- **FR7 — Before/after semantics**: The comparison reflects each command's distinct meaning, sourced per the Data-Source Decision:
  - *status*: **before** = last-indexed state (main+cache merged skiplist); **after** = current scan state.
  - *update*: **before** = pre-update in-memory index; **after** = post-update index.
  Per node, the pane reports counts and total size for before, after, and the delta, broken down by category (added / modified / deleted / unchanged). "Unchanged" is sourced from the in-memory index (it is absent from the thin result structs), consistent with FR4's data source.
  - *Accept*: For a known fixture with N added, M modified, K deleted files, the root node's pane figures equal those categories; child aggregates sum to the parent.

- **FR8 — Empty / no-change handling**: When the result set is empty or there are no changes, the viewer still opens (if FR3 permits) and presents an explicit "no changes"/empty state rather than a blank or broken screen; quitting works normally.
  - *Accept*: Running with the flag on a clean repo shows a coherent no-change view and exits cleanly on quit.

- **FR9 — Viewer init failure is non-fatal**: If the viewer cannot initialise after the command has run (e.g. `TERM` unset, `/dev/tty` unavailable, alternate-screen setup fails), the command's normal output has already been emitted, the init failure is reported to stderr only, and the process exits with the exit code the completed work would have produced. The index/filesystem are never affected by a viewer failure.
  - *Accept*: Forcing an init failure (e.g. `TERM=` ) still emits the normal summary and exits with the work's code; no panic, terminal left usable.

- **FR10 — Sort ordering (runtime-switchable)**: Children at each tree level are ordered by a selectable sort key that can be **switched live while viewing** (the snapshot is ephemeral — especially for the destructive `update` — so the user must be able to re-rank without re-running):
  - **Default**: *total change* = `added + modified + deleted` item counts in the subtree, **descending** (most-changed first), regardless of change type.
  - **Toggles**: sort by added-count, modified-count, or deleted-count individually; or by **name** (alphabetical). Direction (ascending/descending) is toggleable — default descending for change keys, ascending for name; tiebreak is name-ascending (stable, deterministic).
  - **Metric**: change is measured in **item counts**, not bytes (uniform across add/modify/delete; byte-weighting is deferred since deleted/old-modified sizes are not retained).
  - **Presentational only**: switching sort never re-reads the index or filesystem and never mutates state.
  - *Accept (FR10)*: with the flag on, the tree starts total-change-descending; the documented keys re-order live (added / modified / deleted / name, and asc↔desc) without any re-scan; on a fixture each mode's ordering matches its key.

### User Stories
- **As a** user running `dcfh status` **I want** to walk the directory tree and see which subtrees changed since the last index **so that** I can assess what is dirty without re-reading a long flat list.
- **As a** user running `dcfh update` **I want** a post-run tree showing before/after stats per directory **so that** I can confirm the update did what I expected and spot surprises.
- **As a** user piping output or using `--json` **I want** `--interactive-tree` to be safely ignored **so that** scripts and pipelines never hang on a TUI.

## Non-Functional Requirements
### Performance (NFR1)
- Tree construction is O(entries) over the in-memory structures (the no-walk guarantee itself is FR4, not restated here as a separate requirement).
- *Targets (aspirational, not AC-gated)*: viewer launch perceptually immediate — aim < 200 ms from command completion to first paint for ≤ 100k entries; navigation key response aim < 50 ms. These are design targets; no benchmark harness is mandated by this task.

### Usability (NFR2)
- Key bindings follow gdu/MC conventions where reasonable; an on-screen hint line or `?` help lists the active keys.
- The right pane's before/after figures use the existing human-size formatting (`FormatHumanSize`) and British spelling in all labels.
- Narrow-terminal behaviour degrades gracefully (tree-only) rather than erroring.

### Maintainability (NFR3)
- All TUI code is isolated in a single new package so the dependency surface is contained and the non-interactive code path is untouched.
- The tree/stats data model is decoupled from the rendering layer so it is unit-testable without a terminal.
- Any new third-party dependency is justified against the repo's minimal-dependency ethos and recorded in the design phase.

### Security (NFR4)
- The viewer is strictly read-only (authoritative statement; also a hard Constraint): no navigation action mutates the index, filesystem, or config. No auth/permission model is introduced.
- **Terminal-escape sanitisation (primary new attack surface)**: filenames are raw bytes in the index and are *not* escape-sanitised anywhere today. Before any path reaches the terminal it must be neutralised — at minimum C0 control bytes, the C1 range, and ESC-introduced/ANSI CSI sequences (`\x1b[…`) — via an escaping path equivalent to Go's `%q`/`strconv.Quote` or an explicit printable allowlist. Stripping only `\n`/`\t` is insufficient.
- **Every string rendered to the terminal** must originate from either a sanitised path or a program-controlled format string (e.g. `FormatHumanSize`); no raw index field reaches the terminal unescaped. This includes error and teardown messages — an error like `failed at <path>` must use the sanitised form.
- No secrets, network access, or new file writes are introduced by the viewer.

### Reliability (NFR5)
- Terminal state is always restored on exit, including on panic/interrupt (Ctrl-C) and on error paths, via guaranteed teardown.
- A viewer failure (e.g. unsupported terminal) must not corrupt the index nor change the command's exit code for the work already completed; the underlying command result stands.
- Handles edge cases without panic: empty result set, single root entry, very deep/wide trees, and filenames with unusual bytes.

## Constraints
- **No second filesystem walk** — tree is built from in-memory index/scan structures already populated by the command (see Data-Source Decision); reading those in-memory structures is permitted.
- **Non-interactive path unchanged** — no flag, non-TTY, or `--json` ⇒ identical behaviour/output to today.
- **Read-only** — editing, deleting, snapshotting, or re-running from within the TUI is out of scope.
- Follow the existing **cobra + pflag** flag system: persistent flags in `cmd/dcfh/root.go` (`registerRootPersistentFlagsBound`), per-command flags via each command's `Flags()` / `registerHelpFlags` in `cmd/dcfh/status.go` and `cmd/dcfh/update.go`. (Note: there is no `cmd/dcfh/options.go`; the CLAUDE.md reference to it is stale.) British spelling in user-facing text.
- Unix-like, 64-bit target consistent with the rest of the project.

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [ ] **Time**: Will this take >1 week? Borderline-no (~3-5 days).
- [ ] **People**: Does this need >2 people? No.
- [x] **Complexity**: 3+ distinct concerns? Yes — TUI shell/rendering, tree+stats data model, CLI wiring.
- [ ] **Risk**: High-risk components needing isolation? Contained in one new package.
- [x] **Independence**: Can parts be worked on separately? Yes — model, viewer, wiring.

**Assessment**: Unchanged from planning — 2 signals (Complexity, Independence). Proceed as a single task; revisit a split at the design/implementation-plan boundary if the viewer shell grows.

## Acceptance Criteria
- [ ] AC1a (FR1): `--interactive-tree` parses on `status` and `update` and is listed in each command's `--help`.
- [ ] AC1b (FR1): The flag is rejected by other subcommands (e.g. `dupes`), confirming per-command (not persistent) registration.
- [ ] AC1c (FR3): Inert and output-identical (exit 0, no TUI, no hang) when stdout is non-TTY or `--json` is set.
- [ ] AC2 (FR2): Viewer launches only after the full command run; `update` index is fully committed before the viewer opens.
- [ ] AC3 (FR4): Tree content is derived from the command's in-memory structures with zero additional filesystem walk (verified by no extra stat/walk calls); the view is a point-in-time snapshot.
- [ ] AC4 (FR5/FR6): Navigation keys work; right pane appears only at/above the width threshold and re-flows on resize without garbling.
- [ ] AC5 (FR7): Before/after per-node figures (incl. unchanged) match a known fixture for status and for update, with child aggregates summing to parents.
- [ ] AC6 (NFR5): Terminal state restored on normal quit, Ctrl-C, and panic; command exit code reflects the completed work regardless of viewer outcome.
- [ ] AC7 (NFR4): Filenames with C0/C1 control bytes and ESC/ANSI sequences are sanitised before rendering — including in error/teardown messages — with no escape injection; no index/filesystem mutation occurs during navigation.
- [ ] AC8 (FR9): A forced viewer init failure still emits the normal summary, exits with the work's code, reports to stderr only, and leaves the terminal usable.
- [ ] AC9 (FR10): Tree starts in total-change (add+mod+del count) descending order; live keys re-sort by added/modified/deleted/name and flip asc↔desc without re-scanning; fixture orderings match each mode; name tiebreak is stable.

## Status
**Status**: Finished
**Next Action**: /cwf-design-plan 11
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All FRs satisfied and verified in g-testing-exec.md: FR1–FR3 (per-command flag, post-run launch, TTY/JSON guard), FR4–FR6 (tree pane, navigation, width-gated stats), FR7 (before/after via merged-index reload), FR8 (empty), FR9 (init-failure non-fatal), FR10 (runtime-switchable sort). AC1a–AC9 all PASS.

## Lessons Learned
The Data-Source Decision section (reframing "no second walk" as "no second *filesystem* walk", index reload permitted) unblocked the whole design — worth stating explicitly. FR10 (sort) was added mid-requirements from user feedback and slotted in cleanly because it was scoped as presentational-only.
