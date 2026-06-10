# Changelog

Per-task record of changes to dircachefilehash, maintained through the CWF workflow. Entries are added as tasks complete.

Pre-CWF history — organised by release version under Keep a Changelog / Semantic Versioning, plus the earlier rejected-design log — is preserved in [`docs/changelog-old.md`](docs/changelog-old.md).

## Task 17: Review and refactor docs into doc directory

### Status: Complete (completed 2026-06-10, single session, under the 1–2 day / Medium estimate)
### Impact: Brings the repository's documentation back into sync with the shipped v0.7 CLI and tidies the root. Five architecture/design docs (`ARCHITECTURE.md`, `ARCHITECTURE-IMPROVEMENTS.md`, `architecture-v0.7.md`, `design.md`, `streaming-iterator-architecture.md`) move under `docs/` via history-preserving `git mv`, leaving the root with only `README.md`, `CHANGELOG.md`, `BACKLOG.md`, `CLAUDE.md`. The root README is rewritten from the removed `DirectoryCache`/`FileEntry` library API to the actual CLI trio (`dcfh`/`dcfhfind`/`dcfhfix`) plus `--interactive-tree`; `docs/ARCHITECTURE.md` is corrected against source; the three superseded docs gain "Historical — superseded." banners; and a new tagged `docs/README.md` indexes everything Current/Historical. Docs-only — the sole `.go` change is two comment lines in `pkg/doc.go`. Unblocks a clean public release. Branch: a–j phase checkpoints, squashed.

### Changes
- **Relocation (`git mv`)**: the five docs into `docs/`; root Markdown reduced to the four canonical files. `--follow` history preserved across the rename.
- **`README.md`** (root): full CLI-first rewrite — tools overview, install (Go 1.25, `make build`, goreleaser deb/rpm/tar.gz for linux amd64+arm64), Quick Start, `dcfh` command table, global-options table, `dcfhfind`/`dcfhfix` sections, Documentation links. Zero `DirectoryCache`/`FileEntry`/`NewDirectoryCache`; `remote` omitted (it is `Hidden: true`).
- **`docs/ARCHITECTURE.md`**: dropped the deleted `BEIndexFileIOEntry` / `binary_entry_index_file.go` row (kept the live `_mmap.go` neighbour); struck the phantom `AppendEntryToScanIndex@index.go:1008`; "four entry storage modes" → "three concrete + one conceptual"; §6 RWMutex reframed load-bearing → defensive (per CLAUDE.md); §5 doc table relinked by target.
- **Historical banners**: `architecture-v0.7.md`, `streaming-iterator-architecture.md`, `design.md` (line 3, under H1).
- **`docs/README.md`** (new): single tagged index — Architecture & design (Current/Historical) + Reference tables; root README "Documentation" section points to it (≤2 clicks to any doc).
- **`pkg/doc.go`**: two comment edits — ARCHITECTURE.md locator "at the repo root" → "in docs/"; RWMutex framing "now a defensive guard, since the `mremap`'d scan path it once protected has been removed".
- **`CLAUDE.md`**: CLI Usage command list gains `diff`/`subrepo`/`completion` + `remote` (flagged hidden); Security Review section untouched.

### Notable
- **Verify-before-edit caught two phantom citations.** Treating the drifted docs as untrusted input and grepping every cited symbol/file/line against `cmd/`/`pkg/` surfaced that not just `AppendEntryToScanIndex@index.go:1008` but also §6's `appendEntryToNamedIndex@index.go:967` were non-existent — so §6's stale mremap paragraph was replaced wholesale, not merely reframed. A trust-the-doc pass would have copied both errors.
- **FR3 code-grounded classification was the leverage point.** Doing the CURRENT/HISTORICAL/NEEDS-REWRITE triage with file/line evidence in the design phase made implementation a checklist and beat the estimate.
- **Honest history preserved.** The optional `:93` `architecture-v0.6.md` mention in `ARCHITECTURE-IMPROVEMENTS.md` was deliberately left untouched — it sits inside a struck-through *closed-item* record describing a past action; editing it would falsify a history note. That file stayed a pure rename.
- **Testing.** Static verification (no runtime code): 9/9 test cases PASS; link-integrity sweep 0 broken; removed-API grep clean; `go build`/`go test ./...` green; only `pkg/doc.go` `.go`-changed.
- **Security review.** Both exec phases hit the 500-line cap (2604 / 2632 production lines) because no `docs/`/`*.md` glob is in `security.review.max-lines-exclude-paths`, so prose counts as production — the `cwf-security-reviewer-changeset` subagent did **not** run (recorded `State: error`). Benign: the only non-Markdown change is two `pkg/doc.go` comments. Logged a low-priority BACKLOG item to add a docs exclude glob.

## Task 16: `z` key hides unchanged entries in the interactive tree

### Status: Complete (completed 2026-06-09, <0.5 day, within the <0.5 day / Low estimate)
### Impact: Adds a `z` toggle to the `--interactive-tree` post-run viewer (`dcfh status`/`update`) that hides unchanged entries, collapsing the tree to only what changed. The mode is OFF by default (the viewer still launches with the full tree); pressing `z` hides every unchanged file and every wholly-unchanged directory, pressing it again restores them. A directory stays visible whenever its subtree contains any change (so the path to a changed leaf is never broken) and is **not** force-expanded. The footer help line advertises the binding (`z hide`). Render-layer only — no on-disk format change, no change to `Stats`/`Node`, no effect on `--json` or any non-interactive path. Branch: a–j phase checkpoints, squashed.

### Changes
- **`cmd/dcfh/internal/tui/render.go`**: new `hideUnchanged bool` field on `model`; new pure predicate `hasChange(n) = n.Stats.Added+n.Stats.Modified+n.Stats.Deleted > 0` (next to `nodeStyle`, the exact complement of its `set == 0` unchanged arm); one `if m.hideUnchanged && !hasChange(c) { continue }` guard inside `rebuildRows.walk`, before append/recurse, so a wholly-unchanged subtree is pruned in one step; footer help string gains ` z hide`.
- **`cmd/dcfh/internal/tui/tui.go`**: `case 'z':` in `handleRune`, a line-for-line analogue of the `r` reverse toggle (capture `current()` → flip `hideUnchanged` → `rebuildRows()` → `selectNode(cur)`), so selection-preservation and clamp-on-hidden fall out of existing machinery.
- **`cmd/dcfh/internal/tui/render_test.go`**: `TestHasChange` (pure table incl. the deletion-only case) + TC-1…TC-9 simulation/event tests (toggle round-trip, unchanged-leaf hide, deletion-only collapsed dir stays, wholly-unchanged dir hidden, composition with sort+reverse, selection preserved, selection clamped, footer advertises `z`, all-hidden empty-state + navigation-no-panic). Two new fixtures (`treeWithUnchangedDir`, `treeAllUnchanged`) + helpers (`simModelFor`, `pressZ`).

### Notable
- **The `Stats.Files` deletion trap, pre-empted.** Keying the predicate on `Added+Modified+Deleted` (never `Stats.Files`, which excludes deletions) is what keeps a deletion-only directory visible in hide mode. The trap was recorded in task 15's retrospective and designed against from the first line; TC-U1 (deleted-only) and TC-3 (deletion-only collapsed dir) guard it.
- **Directory visibility is free.** Because a directory's `Stats` already aggregate its whole subtree, filtering a directory by its own change-sum keeps the path to a changed leaf without an ancestor walk and without force-expanding — a derived consequence, not extra code.
- **One choke point.** Filtering inside `rebuildRows` (the one function every view-mutating key already calls) makes hide compose with sort/reverse/expand with no second code path (TC-5).
- **Coverage / gates.** `hasChange` and the `rebuildRows` hide branch 100% covered; package total 88.1%. Full `cmd`+`pkg` regression and `go test -race ./...` green; `golangci-lint run ./cmd/dcfh/internal/tui/...` → 0 issues; `govulncheck` → 0 called. Both exec-phase `cwf-security-reviewer-changeset` runs recorded **no findings** (261 production lines, under the 500 cap) — read-only TUI, no new node-derived strings rendered. The selection-clamp path was the only pattern note (panic-only ceiling, covered by TC-7/TC-9).

## Task 15: Interactive-tree status colour coding

### Status: Complete (completed 2026-06-09, ~1 day, within the 1–2 day / Low–Medium estimate)
### Impact: Makes change status legible at a glance in the `--interactive-tree` post-run viewer (`dcfh status`/`update`). Every changed node now carries a status glyph (`+` added / `~` modified / `-` deleted / `*` mixed-directory), a status colour, and bold weight; unchanged nodes show no glyph, default colour, non-bold. Directory rows blend their descendants' statuses additively (presence-based, channels R=deleted / G=added / B=modified): added=green, modified=blue, deleted=red, add+mod=cyan, mod+del=magenta, add+del=yellow, all-three=white — a single changed descendant flips the channel. The glyph is the primary (colour-vision-deficiency-safe) signal; colour reinforces. The stats pane gains a glyph-prefixed, colour-matched legend (`+ Added` / `~ Modified` / `- Deleted` + `* mixed`). Render-layer only — no on-disk format change, no change to `Stats`/`Node` or non-interactive output. Branch: a–j phase checkpoints, squashed.

### Changes
- **`cmd/dcfh/internal/tui/render.go`**: replaced `categoryStyle(n) tcell.Style` (which special-cased `IsDir`→default) with a single pure `nodeStyle(n) (rune, tcell.Style)` — one `switch` over the 3-bit present-set derived from `Stats.{Added,Modified,Deleted} > 0`, returning glyph and colour together so they can't drift; bold iff the set is non-empty. `drawRow` inserts a fixed-width glyph slot after the expand marker (`"%*s%s%c %s"`); the selection `Reverse` compose and the `colX > x+1` value guard are unchanged. `styleModified` recoloured yellow→blue (frees yellow for the add+del blend; updates the stats pane in lockstep). `drawStats` gains the glyph-prefixed legend lines + a dimmed `* mixed (directory)` note, with a 2-col leading slot on every line to keep colons aligned.
- **`cmd/dcfh/internal/tui/render_test.go`**: `TestNodeStyle` (8-set table: glyph, colour, bold, safe-alphabet, `bold==set≠∅`) + simulation tests TC-S1…S10 (glyph placement, leaf colour/bold, directory blend, all-three dir via a `treeAllThreeDir` fixture, selection compose, stats-pane modified=blue, narrow-width value-drop, legend). New helpers: `expandAllVisible`, `rowYOf`, `styleOfRuneInRow`, `fgBoldReverse`.

### Notable
- **One resolver for leaves and directories.** Keying on `Added+Modified+Deleted` counts (not `Stats.Files`, which **excludes** deletions) is what makes a deleted-only directory render as changed (AC9) rather than unchanged — the canonical trap, now guarded by a dedicated test.
- **Plan review earned its keep.** Three of four reviewers flagged the modified=yellow→blue collision (the additive model needs the B channel); review also caught that tcell has no `ColorCyan`/`ColorMagenta` (cyan=`ColorAqua`, magenta=`ColorFuchsia`) before any code was written.
- **Scope addition mid-flight.** An on-screen legend (FR9) was added with user approval and folded back into the b–e plans before exec, so the plans stayed authoritative.
- **Test-plan gap caught at test-writing.** TC-S3/S6 assumed an all-three "root row", but the implicit root (label "") is never rendered (`rebuildRows` walks `m.root.Children`); covered instead by the pure table plus a `treeAllThreeDir` fixture. Recorded as a lesson (e-/j-).
- **Coverage / gates.** `nodeStyle` and `drawRow` 100% covered; package total 84.4%. Full `cmd`+`pkg` regression and `go test -race` green; `golangci-lint run ./cmd/dcfh/internal/tui/...` → 0 issues; `govulncheck` → 0 called. Both exec-phase `cwf-security-reviewer-changeset` runs recorded **no findings** (388 production lines, under the 500 cap) — TUI-only, glyph constrained to a closed alphabet pinned by `TestNodeStyle`.

## Task 14 (round 2): Upgrade CWF subtree to v1.1.185

### Status: Complete (completed 2026-06-09, ~0.5 day, within the ~0.5 day / Medium estimate)
### Impact: Upgrades the vendored CWF workflow tooling from v1.1.183 to v1.1.185 **merge-free**, layered as a single linear commit on top of the preserved 183 landing (`700baba`). v1.1.185 is the release that replaces `git subtree` with a `read-tree` laydown ("Task 185: Replace git-subtree with merge-free read-tree laydown") — the root-cause fix for the subtree merge-commit bug recorded against this repo. The recorded laydown method migrates `subtree`→`read-tree`. No dircachefilehash product code changes (no `*.go` diff). Branch: a–g + j phase checkpoints (incl. the b/c phases run at user request, normally skipped for a chore), squashed.

### Changes
- **`.cwf/version`**: `cwf_version=v1.1.185`, `cwf_ref=v1.1.185`, `cwf_sha=6659c1cca72ef033d92546fcd9d42a0f4d817dd9` (commit form, consistent with the 183 record); **`cwf_method=read-tree`** (was `subtree`).
- **`.cwf/scripts/cwf-manage`**: gains `read-tree` as an accepted method and the `subtree`→`read-tree` migration in `cmd_update`; wires in the new merge-advisory helper.
- **`.cwf/scripts/command-helpers/cwf-detect-merges`** (new): read-only Perl helper that fingerprints subtree-install merges; counts-only output, list-form git spawn, `-z`/`%x1f` porcelain.
- **`.cwf/lib/CWF/Backlog.pm`**: adds a CHANGELOG-005 stale-brand warning check (constant-needle `index`, intro-array scoped).
- **`.cwf/security/script-hashes.json`**: re-pinned by the installer atomically with the scripts it hashes (hence `validate: OK` on first run, no separate hash refresh).
- **`.claude/settings.json`**: settings-merge added **exactly one** scoped Bash allowlist entry — `Bash(.cwf/scripts/command-helpers/cwf-detect-merges:*)` — and **zero** hook entries.

### Notable
- **The forward-only gap was the crux, caught in planning.** The *installed* 183 `cwf-manage` reads `cwf_method=subtree` and passes `CWF_METHOD=subtree`, which 185's `install.bash` hard-refuses (`:82-83`) — the `subtree`→`read-tree` translation lives only in 185's `cmd_update`. Three of four b-phase plan reviewers independently flagged this. Resolved by the user's suggestion: materialise a throwaway v1.1.185 checkout and drive the upgrade with **its** migration-aware `cwf-manage` (FindBin loads 185 libs while `$git_root` resolves to this repo via cwd) — CWF's documented forward-only recovery.
- **Merge-free landing achieved.** read-tree (`git fetch` + `git read-tree --prefix`) stages the four `.cwf*` prefixes with no commit; our single consumer commit is single-parent. `git log --merges 700baba..HEAD` stayed empty.
- **Fail-closed migration confirmed.** `cwf_method` is rewritten to `read-tree` only after laydown/artefacts/perms succeed — a mid-update abort leaves `subtree` intact. Driver exited 0 first attempt; the design's bare-bootstrap fallback was not needed.
- **detect-merges advisory (expected).** Flagged the 4 pre-existing subtree-install merges (`103537c`/`28cfb50`/`a2c7635`/`75e3ae4`), all in base history before `700baba` — advisory only; relates to the parked `.githooks`/re-linearisation decision, out of scope here.
- **Security review: both exec phases `no findings`.** The changeset stayed under the production-weighted cap (209 lines), unlike the 183 round — the 183→185 source delta is small (4 files). The new `cwf-detect-merges` helper was reviewed in full and judged clean.

## Task 14: Upgrade CWF subtree to v1.1.183

### Status: Complete (completed 2026-06-07, ~1 day, within the <0.5–1 day / Low estimate)
### Impact: Upgrades the vendored CWF workflow tooling from v1.1.177 to v1.1.183 via `cwf-manage update v1.1.183` (subtree method). No dircachefilehash product code changes — `.cwf/`, `.cwf-skills/`, `.cwf-rules/`, `.cwf-agents/`, `.claude/{skills,agents}` symlinks, `.cwf/version`, `.claude/settings.json`, and `.gitignore` only. Brings in the v1.1.178→183 deltas: Claude Code sandboxing substrate (T179), the phase-scoped planning-write PreToolUse guard (T180), the guarded-worktree convention (T181), the `security-review-changeset` `--wf-step=` contract (T182), and the permission-drift fix-on-sight rule (T183). All new sandbox/guard surface is **inert** in this repo. Branch: a/d/e/f/g/j phase checkpoints, squashed.

### Changes
- **`.cwf/` (+ `.cwf-skills/`, `.cwf-rules/`, `.cwf-agents/`)**: replaced at v1.1.183 by the subtree remove-then-add laydown. New runtime surface: `.cwf/scripts/hooks/pretooluse-planning-write-guard`, `.cwf/scripts/hooks/pretooluse-sandbox-logging`, `.cwf/lib/CWF/PlanningGuard.pm`, plus docs (`sandboxing.md`, `worktree-process.md`) and the rewritten `security-review-changeset` helper.
- **`.cwf/version`**: `cwf_version=v1.1.183`, `cwf_ref=v1.1.183`, `cwf_sha=faf92479fac564f241ce10afb8ec00c986ad37f1` — the **commit** SHA, recorded by the outer post-T175 v1.1.177 `cwf-manage` whose `resolve_sha` uses `^{commit}`.
- **`.claude/settings.json`**: the only mutation was removal of a stray empty `"deny": []` array (net −1 line). The settings-merge added **0 hook entries** — the new hooks ship as on-disk files but stay unregistered until a project enables sandbox.
- **`.gitignore`**: `+.cwf/sandbox-violations.log` (the sandbox-logging hook's output path).
- **`.claude/skills/cwf-*` / `.claude/agents/cwf-*`**: 20 skill + 5 agent symlinks recreated (incl. the T182-hardened `cwf-security-reviewer-changeset`).

### Notable
- **The `cwf_sha` form is the exact inverse of Task 9.** Task 9 recorded the tag-object SHA (pre-T175 writer); this upgrade records the commit SHA because the authoritative writer is now post-T175. Pinned in planning by reading the installed `cwf-manage` `resolve_sha` by sub name (drift-proof), not the changelog headline — making exec a confirmation pass.
- **The top settings-merge risk failed safe.** The +260-line merge was expected to inject two PreToolUse hooks; it added none (hooks opt-in, unregistered with sandbox off). Verifying hook *registration state* — not just a behaviour knob — was the right check.
- **T183 perms fix-on-sight ran in-laydown.** The exact-perms pass clamped the 5 pre-existing floor-drift files (agent `.md` 0600→0444, helper scripts 0700→0500), so `cwf-manage validate` was exit 0 on the first run with no `fix-security` pass.
- **Security review: both exec phases `no findings`.** `security-review-changeset` (T182 `--wf-step=`) exited 2 (cap exceeded: 1302/1305 production lines > 500 — the vendored `.cwf/` delta; `implementation-guide/**` already excluded). Per user direction the cap was raised once per phase (`--max-lines=5000`, no persistent config change) and the `cwf-security-reviewer-changeset` subagent reviewed the full delta → `no findings` both times. This is a fresh data point for the standing BACKLOG item on excluding upstream CWF dirs from the cap (Task 5/9): it confirms excluding `.cwf/**` would route every CWF self-upgrade through a full ~100k-token subagent pass over upstream-vetted code.

## Task 13: Interactive-tree size column tracks the active sort metric

### Status: Complete (completed 2026-06-07, single session, within the <0.5 day / Low estimate)
### Impact: Fixes a display bug shipped in task 12. The `--interactive-tree` viewer's right-aligned per-row number now reflects the **active sort metric** instead of the directory's on-disk footprint. Under the default `change_bytes(desc)` sort a directory shows its **change volume** (Added+Modified+Deleted bytes), so the number matches its position in the order — e.g. a directory with ~112 MB of churn no longer renders its 51.9 GB disk size. Toggling the sort key swaps the column to match: `change_bytes`/`name` show a humanised size, `change_files`/`added`/`modified`/`deleted` show the integer count; the header sort indicator already names the active metric so the unit is unambiguous. Render-layer only — no on-disk format change, no change to `Stats`/`ChangeSet` or non-interactive `status`/`update` output. Branch: a–j phase checkpoints, squashed.

### Changes
- **`cmd/dcfh/internal/tui/sort.go`**: new `columnText(n, key)` helper, co-located with `metric()`. It reuses `metric()` (the same value the comparator ranks on) so the displayed number can never diverge from the order; `change_bytes` formats via `dcfh.FormatHumanSize`, count keys via `strconv.FormatInt`. `name` has no numeric key (`metric()` returns 0), so it is remapped to `change_bytes` **before** the value is read.
- **`cmd/dcfh/internal/tui/render.go`**: `drawRow` now draws `columnText(row.node, m.sortKey)` instead of the hardcoded `dcfh.FormatHumanSize(row.node.Stats.Bytes)`; locals renamed `size`→`colVal`, `sizeX`→`colX`. The stats pane still shows `Stats.Bytes` as the live `Size:` line (unchanged).
- **`cmd/dcfh/internal/tui/sort_test.go`**: `TestColumnText` — per-key matrix incl. the `name`→change-bytes guard (must not regress to `0 B`) and a deleted-only discriminator (Stats.Bytes 0 vs change_bytes 900).
- **`cmd/dcfh/internal/tui/render_test.go`**: `TestColumnTracksActiveSortMetric` — headless SimulationScreen test asserting the `docs/` row (deleted child) shows `900 B` under `change_bytes` and the count after `f`. Incidental cleanups: `rowLine` helper uses `strings.SplitSeq`; the dead `h` parameter on `newSimModel` was dropped (cleared a pre-existing full-tree `unparam`).

### Notable
- **Root cause: a metric/display split from task 12.** Task 12 changed the default sort to `change_bytes` but left `drawRow` rendering `Stats.Bytes`; the ordering was correct while the visible number contradicted it. The fix re-points the column at the same `metric()` the sort uses, making divergence structurally impossible.
- **The `name`→0 trap was caught before code.** Three of four design plan-reviewers independently flagged that `metric(n, sortName)` returns 0, so the fallback had to remap the key first; promoted to an explicit ordered requirement and locked by a test.
- **Genuine regression guard.** Tests assert on the `docs/` node where `Stats.Bytes` (0) and change_bytes (900) differ — the old code drew `0 B`, the fix draws `900 B`. A colliding node (`src`, 250==250) was explicitly rejected.
- **Coverage / gates.** `columnText` 100% covered; package 82.1%. Full `cmd`+`pkg` regression and `go test -race` green; `golangci-lint run ./...` → 0 issues; `govulncheck` → 0 called. Both exec-phase `cwf-security-reviewer-changeset` runs recorded **no findings** (132 production lines, under the 500 cap — unlike task 12).
- **Process learning.** Task 12's render tests asserted header/legend/stats-pane text but never a per-row data cell, so the column could silently disagree with the sort. Recommendation recorded: for sort/aggregation-metric changes in a viewer, assert the rendered per-row value, not just the chrome.

## Task 12: Byte-weighted default sort for the interactive-tree viewer

### Status: Complete (completed 2026-06-06, single session, within the 0.5–1 day / Medium estimate)
### Impact: The `--interactive-tree` viewer now sorts by **volume of change** by default instead of count. A new `change_bytes` metric (Added+Modified+Deleted **bytes**) is the default sort, so the directories where the most *data* changed — large deletions included — surface first; the header opens at `sort:change_bytes(desc)`. The former count metric is renamed `change_files` and reachable on the `f` key (`c` now selects bytes); `a`/`m`/`d`/`n`/`r` unchanged. The right-hand stats pane annotates each change line with its bytes (`Added: 12 (3.4 MB)` …) so the order is self-explaining. No on-disk format change; non-interactive `status`/`update` output and index bytes are byte-identical (proven). Retires the **Low** BACKLOG item "Byte-weighted sort option for interactive-tree viewer". Branch: f–j phase checkpoints, squashed.

### Changes
- **`pkg/treeview.go`**: `Stats` gains `AddedBytes`/`ModifiedBytes`/`DeletedBytes` (`int64`), aggregated up every directory; `leafStats` retains the deleted leaf's last-known size (still excluded from live `Files`/`Bytes`); `ChangeSet` gains `DeletedSizes map[string]int64` and the deleted-union step uses it.
- **`pkg/update.go`, `pkg/comparison_sink.go`**: `changeCollector` gains `deletedSizes`; the single `add` write site is widened to `add(op, path, size)` (preserving the lock-free single-writer invariant); `scanWriteSink.record` captures the left entry's `FileSize()` (err→0) on `OpDeleted` — the only no-extra-walk source for the update path, since the entry is gone after the atomic rename.
- **`pkg/repo.go`, `pkg/repo_local.go`**: additive `UpdateResult.DeletedSizes` (`json:"deleted_sizes,omitempty"`), copied from the collector in `Apply`. `status.go` needs no change — the deleted size comes from the in-index tombstone the cache refresh retains (KD2 dual-source, one "last-known size" rule).
- **`cmd/dcfh/internal/tui/sort.go`**: add `sortChangeBytes` (default/zero value), rename `sortChange`→`sortChangeFiles`; `metric()` widened to `int64` (no overflow on large trees, no new G115 suppression); `label()` + `keyForRune` (`c`→bytes, `f`→files).
- **`cmd/dcfh/internal/tui/render.go`**: default `sortKey: sortChangeBytes`; footer legend `c/f/a/m/d/n`; stats-pane per-category byte annotations via `FormatHumanSize`.
- **`cmd/dcfh/update.go`**: threads `ChangeSet.DeletedSizes = result.DeletedSizes`.

### Notable
- **Dual-source deleted bytes resolve to one value.** `status` reads the deletion tombstone the cache-delta refresh keeps (with its `FileSize`); `update` carries the captured size on `ChangeSet.DeletedSizes`. Both resolve to "last-known size", so a single `Stats.DeletedBytes` total is consistent across launch paths — proven by one cross-path fixture (`TestPostRunTree_CrossPathByteIdentity`, status + update subtests against a real temp repo) rather than two divergent paths. On a path present in both sources the tombstone wins (the existing `seenDeleted` guard).
- **The #1 design risk became the most-tested seam.** The deleted-byte attribution asymmetry was flagged in planning and settled in design (KD2) before exec, so implementation had zero plan deviations.
- **Stats-pane bytes close the loop on the originating confusion.** The task was triggered by "why did this sort here?" — the pane now shows the bytes behind the order.
- **Security review: both exec-phase `cwf-security-reviewer-changeset` runs recorded `error` (cap exceeded).** This repo does not list `**/*_test.go` in `security.review.max-lines-exclude-paths`, so the four new test suites (411 lines) pushed the production-weighted count over the 500 cap even though the production code is ~154 low-risk lines (deleted sizes are `int64` from the index/collector, carried in a map, rendered only via numeric `FormatHumanSize`; `sanitiseLabel` untouched). Recorded as `error` per the CWF contract, not silently downgraded. `golangci-lint run ./...` → 0 issues; `-race -d=checkptr=0 ./pkg/` clean. A config follow-up to add test globs to the exclude list is filed to BACKLOG.
- **Incidental `go fix` modernisation** (pre-commit ran `go fix ./...`, rewriting `pkg/wire_handler.go` + `pkg/binary_entry_interface_test_framework_test.go` from `wg.Add`/`go func` to `sync.WaitGroup.Go`) is included in this task's squash — the change was produced during Task 12, so it lands with Task 12 rather than being curated out.

### Retired Backlog Items
#### Byte-weighted sort option for interactive-tree viewer

Sorting is by change COUNTS (added+modified+deleted) because per-category byte sizes are not retained for deleted/old-modified files (design KD8). A byte-weighted sort would need the update collector to capture the left-entry size at OnLeftOnly/OnMatch and thread an aggregated byte delta onto the tree Stats. Deferred from task 11 as out-of-scope plumbing.

<!-- Note: Implemented as the new default change_bytes sort with change_files rename; deleted-byte plumbing added (collector left-size capture + dual-source builder). -->

## Task 11: `--interactive-tree` gdu-style post-run viewer for status/update

### Status: Complete (completed 2026-06-05, single session, under the 3–5 day / Medium-High estimate)
### Impact: `dcfh status` and `dcfh update` gain an opt-in `--interactive-tree` flag that, after the normal run, opens a full-screen gdu/Midnight-Commander-style viewer: a navigable directory tree on the left and a width-gated before/after stats pane on the right. Default-off and TTY-only (inert with `--json` or when piped), so existing behaviour is byte-for-byte unchanged (proven). Read-only — navigating never touches the index or filesystem. Sort is runtime-switchable (default = sum of added+modified+deleted, plus per-category and name, with reverse), which matters because `update` is destructive and the change snapshot is one-shot. No on-disk format change. **Raises the minimum Go toolchain to 1.25.0** (tcell requirement). Branch: f–j phase checkpoints, squashed.

### Changes
- **`pkg/treeview.go`** (new): terminal-free data layer — exported `Tree/Node/Stats/Category/ChangeSet`, the pure `buildTreeFromEntries` (path-split + up-tree aggregation; deleted paths unioned in count-only for the full-update case), the `BuildTree` skiplist adapter, and `sanitiseLabel`/`SanitiseLabel` (reject-by-default printable allowlist neutralising C0/C1/DEL/ESC/CSI/OSC and invalid UTF-8).
- **`pkg/repo.go`, `pkg/repo_local.go`**: `Repo.PostRunTree(ctx, ChangeSet)` (reloads the merged main+cache index via mmap — no second filesystem walk); additive `UpdateResult.Added/Modified/Deleted` and `ApplyRequest.CollectChanges`.
- **`pkg/update.go`, `pkg/comparison_sink.go`, `pkg/pipeline_update.go`, `pkg/pipeline_status.go`**: a lock-free `changeCollector` threaded through the canonical update pass only (never the cache-refresh delta pass), populating the change-set labels when requested.
- **`cmd/dcfh/internal/tui/`** (new package): tcell render layer — `Run`/`runScreen`, two-pane width-gated layout, navigation (arrows + hjkl + Enter), sort keys (`c/a/m/d/n` + `r` reverse), idempotent `sync.Once` teardown, rune-aware truncation of sanitised labels.
- **`cmd/dcfh/filters.go`, `status.go`, `update.go`, `common.go`**: per-command `--interactive-tree` flag, the `flag ∧ !--json ∧ IsTerminal(stdout)` guard, and the post-run launch.
- **`go.mod`/`go.sum`**: add `github.com/gdamore/tcell/v2` + `golang.org/x/term`; `go` directive 1.24.3 → 1.25.0. **`CLAUDE.md`** dependencies section updated to match.

### Notable
- **Two-layer split by purpose, not package.** The data layer lives *inside* `package dircachefilehash` (so it can name the unexported skiplist/entry types) but imports no terminal; only the exported `Tree` crosses to the `tui` package. This resolved the package-boundary blocker the plan-review caught, while keeping all correctness logic unit-testable without a TTY.
- **"No second filesystem walk" was reframed to "no second *filesystem* walk".** The stats pane needs the full file set with sizes, which the thin result structs discard; reloading the merged index (an mmap read) supplies it without re-scanning the tree.
- **Escape-injection defence verified in a real terminal.** A `pty.fork()` harness drove the actual `tcell` screen; a filename carrying `ESC[2J` + an OSC title-injection was escaped to inert text and rendered harmlessly — the allowlist (not a blocklist) is what makes this hold for bytes outside any enumerated sequence.
- **Collector is lock-free and provably so**: single comparison-goroutine writer, read after `wg.Wait()`; `-race` clean and independently confirmed by the FR4 security review.
- **Security review: f-phase `cwf-security-reviewer-changeset` → no findings.** The g-phase (testing) review hit the production-weighted line cap (exit 2) because that phase counts test files as production — recorded as `error` per the CWF contract; the production code was already cleared in f. `golangci-lint run ./...` → 0 issues (one rationale-bearing `//nolint:gosec // G115` on the bounded stdout-fd narrowing).
- **Deferred** (filed to BACKLOG, Low): wide-rune (CJK) column-width accounting (cosmetic) and a byte-weighted sort option (needs deleted/old-size capture). Counts are exact today; byte figures for changed files are current-state.

## Task 10: Re-enable checkptr in the race gate

### Status: Complete (completed 2026-06-04, ~1 day, within the 1–2 day estimate)
### Impact: The pre-commit `-race` gate now runs with **checkptr ON** (the Go default) — the `GOFLAGS="-gcflags=all=-d=checkptr=0"` suppression is gone. The gate can again catch genuine pointer-arithmetic bugs tree-wide; the zero-copy mmap accessors were made checkptr-clean rather than the checker being disabled (option 1, not the document-and-accept option 2). No on-disk format change, no public API change, zero-copy preserved (0 allocs/op on the accessor path). Retires the **Very High** BACKLOG item "Re-enable checkptr in the race gate". Branch: f/g phase checkpoints (`29667d0`, `61fff80`) + j-retro, squashed.

### Changes
- **`pkg/binary_entry.go`** (C1, `GetBinaryEntry`): replaced the `uintptr`-round-trip pointer computation with `unsafe.Add(unsafe.Pointer(&Data[0]), headerSize+Offset)`. Both operands are `int`, so the dead `//nolint:gosec // G115` and `//nolint:govet` suppressions were removed.
- **`pkg/format/entry.go`** (C2 `RelativePath`, C3 `calculatePathLength`): the trailing-NUL scan now walks an **integer length** over a single in-bounds base pointer (`base`/`&be.Path[0]`), materialising the result pointer only in the final `unsafe.String`/return expression. C3 preserves its `&be.Path[0]` start byte-for-byte (the pre-existing 8-byte disagreement with C2 is intentionally **not** unified here).
- **`pkg/entry_serialiser_test.go`**: the round-trip assertion now calls `RelativePath()` on the heap-backed buffer (was a raw byte scan with a now-false checkptr comment) — turning the headline acceptance scenario into a live, named regression for the heap-backed C2 path.
- **`.githooks/pre-commit`**: dropped the `GOFLAGS=...checkptr=0` prefix; comment rewritten to cite `unsafe.Add` provenance. The gate now runs `go test -race -short ./...`.
- **`implementation-guide/cwf-project.json`**: added `security.review.max-lines-exclude-paths: ["implementation-guide/**"]` so the exec-phase security-review cap measures production code, not CWF plan docs.

### Notable
- **`unsafe.Add` clears checkptr's *arithmetic* check but not the GC's pointer-validity check.** The first implementation attempt (holding `pathEnd = base+Size` live as an `unsafe.Pointer`) cleared the original "pointer arithmetic result points to invalid allocation" abort but introduced a *different* fatal — `found bad pointer in Go heap` — because the GC scans a live past-the-end pointer. Fixed by never holding an out-of-bounds pointer in a live variable (integer-length trim). The full-runtime acceptance bar (`go test -race ./...`, not a grep) is what caught this.
- **checkptr only bites heap-backed memory.** For true mmap regions `checkptrBase` returns 0 and the check is skipped — which is exactly why production never crashed and the disabled gate could hide the issue. TC-2 deliberately exercises the heap-backed path.
- **A pre-existing latent bug was found and preserved, not silently "fixed".** `RelativePath` starts the path at `Sizeof(Entry)`; `calculatePathLength` starts it at `&be.Path[0]` (= `Sizeof(Entry)-8`) — an 8-byte disagreement over the same memory. Preserved byte-for-byte and filed as a Medium backlog item for a proper canonical-offset audit.
- **Security review: f-phase and g-phase `cwf-security-reviewer-changeset` → both no findings** (the reviewer independently re-derived the C2 underflow guard and C3's guarded non-dereference). `golangci-lint run ./...` → 0 issues (one rationale-bearing `//nolint:gosec // G115` on a bounded `Sizeof-8` constant in C3).

### Retired Backlog Items
#### Re-enable checkptr in the race gate: make zero-copy accessors checkptr-clean

The `.githooks/pre-commit` gate runs `go test -race` with checkptr globally
disabled (`GOFLAGS="-gcflags=all=-d=checkptr=0"`) because the zero-copy core
does intentional `unsafe.Pointer` arithmetic that checkptr flags. As a result
the race gate cannot catch genuine pointer-arithmetic bugs anywhere in the tree
— a real coverage hole.

Running `go test -race ./...` *with* checkptr enabled (the default) fails with
`checkptr: pointer arithmetic result points to invalid allocation` at multiple
zero-copy accessors, e.g.:
- `pkg/binary_entry.go:53-54` — `(*binaryEntryRef).GetBinaryEntry` (uintptr
  round-trip; `unsafe.Add` fixes this one).
- `pkg/format/entry.go` — `RelativePath` / `calculatePathLength` read the
  trailing variable-length path past the fixed `Entry` struct; checkptr objects
  when the test's backing allocation is struct-sized (`unsafe.Add` does NOT fix
  these — it is the struct+trailing-data pattern itself).

Goal: make the zero-copy accessors checkptr-clean (or scope them precisely with
`//go:nocheckptr` and/or adjust how the test framework backs entries so the
whole struct+path lives in one allocation checkptr can see), then re-enable
checkptr in the race gate so it catches real defects. Investigate whether any
checkptr hit reflects a genuine out-of-bounds read versus a tracking false
positive.

Acceptance: `go test -race ./...` passes with checkptr ENABLED (drop the
`-d=checkptr=0` flag from `.githooks/pre-commit`), with rationale recorded for
any `//go:nocheckptr` retained.

Identified during task 8 (dcfhfix non-destructive default), 2026-06-04, when a
manual `go test -race` surfaced the failures the gate's disabled-checkptr config
otherwise hides.

<!-- Note: Delivered as option 1 (fix): unsafe.Add + integer-length trimming made the zero-copy accessors checkptr-clean; gate re-armed (dropped -d=checkptr=0). -->

### New Backlog Items
- **Reconcile RelativePath vs calculatePathLength 8-byte pathStart discrepancy** (Medium) — the two accessors disagree by 8 bytes on where the path begins; audit which offset is canonical against the on-disk writer (`EntrySerialiser`) and fix the wrong one.

### Backlog Items Touched
- The `implementation-guide/**` exclude addresses CWF *process docs* tripping the security-review production-line cap. It is adjacent to — but does **not** close — the open Task-5/Task-9 Low item about discounting upstream-vendored `.cwf/**` from the cap; that remains open (a different path set with different trade-offs for pure-upgrade tasks).

## Task 9: Upgrade CWF to v1.1.177 via cwf-manage update

### Status: Complete (completed 2026-06-04, ~1 day, within the <0.5–1 day estimate)
### Impact: The installed CWF tooling subtree moves v1.1.169 → v1.1.177 (8 upstream tasks: T170–T177). `cwf-manage update v1.1.177` applied in a single clean pass — no artefact-conflict prompt, no perms abort, no `fix-security` pass — and `cwf-manage validate` is exit 0. No dircachefilehash source changed (`git diff --stat <baseline>..HEAD -- pkg cmd go.mod go.sum` empty); the only consumer-authored writes were `.cwf/version` and this task's workflow docs. CWF lives only on the `local-*` line, so the CWF-free public `main` is untouched.

### Changes
- **CWF subtree v1.1.169 → v1.1.177** via subtree remove-then-re-add (`.cwf/`, `.cwf-skills/`, `.cwf-rules/`, `.cwf-agents/`), driven by `cwf-manage update`. 20 skill + 5 agent + 1 rule symlinks recreated; settings-merge added 0 entries.
- **`.cwf/version`**: `cwf_version`/`cwf_ref` → `v1.1.177`; `cwf_sha` → the **annotated-tag object** `1cae055bf1b52bea0fd9b0cfce63871893757ab7`.
- **T176 doc-split landed**: `.cwf/docs/workflow/workflow-steps.md` is now a ToC pointing at 10 per-phase files under `workflow-steps/`; the plan-phase SKILL Step-5 references repoint accordingly (all symlinks resolve).

### Notable
- **`cwf_sha` records the tag-object SHA, not the commit — by design for *this* upgrade.** T175 (ships *in* v1.1.177) changes the writer to record `git rev-parse <ref>^{commit}`, but it is **forward-only**: the authoritative `.cwf/version` write is performed by the *outer*, currently-installed **v1.1.169** cwf-manage (pre-T175), whose `resolve_sha` has no `^{commit}`. The commit form (`ed664b25…`) will land only on the *next* upgrade. Predicted by reading the installed `cwf-manage` source during planning (lines 209/448/502–510) — not the T175 changelog headline — and confirmed live. Same pattern as T159 (`git_describe_version`).
- **T170 perms-ceiling self-cleaned the laydown.** The exact-perms pass tightened drifted modes (agent `.md` 0600→0444, `.cwf/scripts/**` 0700→0500) back to the recorded ceiling during the update, so `validate` was clean on the first run with no separate `fix-security`.
- **Clean run, unlike Task 5.** The v1.1.163 `rules-inject` artefact defect that aborted Task 5's first attempt was fixed by T167; this span (170→177) carried no analogous packaging defect and the retained revert escape hatch was never needed.
- **Security review recorded `error` in both exec phases — the documented contract, not a finding.** `security-review-changeset` exited `2` (1759 / 1906 production lines > the 500 cap) because the changeset is entirely upstream-vendored CWF content with no `max-lines-exclude-paths` discount configured; the deterministic `cwf-manage validate` (sha256 + perms) is the integrity gate and passed.

### Backlog Items Touched
- **Augmented** (not retired) the Task 5 follow-up *"Configure security.review.test-paths to exclude upstream-shipped CWF directories"* (Low) with Task 9's second data point and a caveat: discounting `.cwf/**` from the cap would flip a *pure* upgrade task from "error/skip" to "invoke the subagent on the entire vendored delta" — so the config fix is better-aimed at *mixed* tasks. No new backlog item created (the concern was already tracked).

## Task 8: dcfhfix — default to non-destructive fix-to-new-file

### Status: Complete (completed 2026-06-04, ~1 day, within the 1–2 day estimate)
### Impact: `dcfhfix` no longer mutates a potentially-corrupt index in place by default. Every write path now preserves the pre-repair original at a visible `<index>.pre-fix-<UTC>` sibling *before* the atomic rename, matching the safety posture of `fsck -n`/`git fsck`. The legacy in-place behaviour survives behind a `--force`-gated `--edit-in-place` opt-out. The on-disk contract is purely additive — the repaired index still lands at the original path — so the change is backwards-compatible for anything that reads the result and trivially reversible. Retires the **Medium** BACKLOG item of the same name. Branch: 9 phase checkpoints (a `2a21d23` … i `14594ea`) + j-retro, squashed.

### Changes
- **`cmd/dcfhfix/promote.go`** (new, 143 LOC): `promoteRepairedIndex` is the single choke-point all four write paths route through. `preserveOriginal` copies the original to a `.pre-fix-<UTC>` sibling via `os.OpenFile(O_WRONLY|O_CREATE|O_EXCL)`, classifying any EEXIST occupant with `Lstat` (regular → advance a bounded `-1`…`-100` counter; non-regular → hard refusal) and cleaning up the partial sibling on any copy/sync failure (NFR5: preserve-before-rename, never consume the temp on a failed preserve). `validateEditInPlaceGate` refuses lone `--edit-in-place` without `--force`; `reportDryRunPreservation` previews the action.
- **`cmd/dcfhfix/{main,entry_workflow_main,entry_append_remove}.go`**: replaced the four raw `os.Rename` promotions (header edit, entry edit, entry append, entry remove) with `promoteRepairedIndex`; un-blanked the header path's `*ParsedOptions`; added the `--edit-in-place` option, the gate call, and dry-run previews; removed a stale `entry resort` reference from usage/help.
- **`cmd/dcfhfix/DESIGN.md`**: non-destructive default recorded under Safety Features.
- **`go.mod`**: Go toolchain `go1.26.3 → go1.26.4` (forced — govulncheck hard-failed on GO-2026-5039/5037 in the 1.26.3 stdlib during the f-phase commit).
- **Tests** (`promote_test.go` +399, `promote_integration_test.go` +340): unit coverage of the helpers (naming, byte-identical preserve, symlink/dir refusal, EEXIST counter, bound exhaustion, gate matrix) and a per-write-path integration matrix (default preserves / `--edit-in-place` suppresses), plus force-gating refusal, dry-run, `fixes`-stack coexistence (FR5), message/quiet matrix, and NFR5 failure-ordering.

### Notable
- **One choke-point made the behaviour change a four-line call-site diff.** Routing all four paths through `promoteRepairedIndex` kept the change small, reviewable, and rollback-trivial (additive on-disk contract).
- **`O_EXCL` + Lstat-classify is the safe sibling-write idiom.** EXCL prevents clobbering; classifying the EEXIST occupant (advance on regular, refuse on symlink/dir/device) prevents writing through a non-regular path. Bounded retry (100) prevents spinning.
- **Self-inflicted process error, caught in-task.** A plain `go test -race` checkptr failure was briefly mistaken for a commit blocker before reading `.githooks/pre-commit` — which *deliberately* disables checkptr (`-d=checkptr=0`) for the zero-copy core. Not a regression; the gate's reduced power is now tracked by a **Very High** backlog item.
- **Security review: f-phase `cwf-security-reviewer-changeset` → no findings; `golangci-lint run ./...` → 0 issues** (rationale-bearing `//nolint:gosec // G304/G302` on the O_EXCL open).

### Retired Backlog Items
#### dcfhfix: default to non-destructive fix-to-new-file

Medium-priority item proposing `dcfhfix` preserve the original and require an explicit opt-in for in-place edits. Delivered as specified: non-destructive default across all four real write paths (header/entry-edit/append/remove — the docs' `scan` path does not exist), original preserved at a timestamped `.pre-fix-<UTC>` sibling, `--edit-in-place` gated behind `--force`, with help/DESIGN updated. Reconciled against the *existing* `--backup`/`fixes` stack during requirements: the sibling **complements** the backup stack rather than replacing it.

### New Backlog Items
- **Re-enable checkptr in the race gate** (Very High) — make the zero-copy accessors checkptr-clean so the `-race` gate (currently `-d=checkptr=0`) can catch genuine pointer-arithmetic bugs tree-wide.
- **`.pre-fix-*` sibling retention/GC** (Low) — optional cleanup subcommand if preserved siblings accumulate enough to annoy.
- **Fault-injection seam for `preserveOriginal`** (Low) — to cover the residual Sync/Close/Lstat defensive branches (75.8% → 100%).

## Task 7: Clear pre-existing full-tree golangci-lint failures (cyclop, unparam)

### Status: Complete (completed 2026-05-31, < 0.5 day, on estimate)
### Impact: Makes `golangci-lint run ./...` green across the whole tree — 0 issues, down from 3. These three findings were masked by the `.githooks/pre-commit` `--new` staged mode and only surfaced on a full-tree run, blocking any future `--all` CI lint gate. All fixes are behaviour-preserving; on-disk index format and CLI output are byte-identical. Branch: 3 planning checkpoints (a/d/e) + f-exec (`77af72c`) + g-exec (`4e25ada`) + j-retro.

### Changes
- **`pkg/filter_run.go`**: extracted the `case "scan":` body of `resolveOneSelector` into a new unexported `resolveScanSelector(metaDir)` helper. The recursive `case "all":` inherits it unchanged. cyclop 21 → within threshold; pure code movement.
- **`cmd/dcfhfind/main.go`**: extracted the four state-dependent cases (`--min-size`/`--max-size`/`--start-date`/`--end-date`) of `parseTestToken` into `(p *ExpressionParser) parseStatefulArgTest`. `parseTestToken` delegates and returns early when `handled`. cyclop 21 → within threshold.
- **`pkg/binary_entry_scan_test.go`**: removed the unused `t *testing.T` parameter from `(*scanTestHelper).createTestEntry` (unparam) and updated its 4 call sites. The two sibling `createTestEntry` methods (`skiplistTestHelper`, `indexFileMmapTestHelper`) legitimately use `t` and were left untouched.
- **`cmd/dcfhfind/expressions_test.go`**: added `TestParseStatefulArgTest` (7 sub-cases) covering the four stateful tokens and pinning the `handled=true`-on-error invariant — closing the only coverage gap the refactor touched.

### Notable
- **The `handled=true`-on-error invariant.** Each moved case returns `handled=true` even on its error paths, so a malformed `--min-size`/`--start-date` surfaces its error rather than falling through to the `argTestTable` lookup. Preserved verbatim and now guarded by TC-4.
- **Name-collision caveat (flagged by plan review).** Three distinct `createTestEntry` methods share the name across receivers; only `scanTestHelper`'s `t` is unused. A blanket find-replace would have broken ~9 unrelated call sites — the per-receiver scope avoided it.
- **Security review: empty changeset.** Go-source-only change; the CWF changeset reviewer scopes to CWF-internal/shebang-script files, so Go is covered by the always-on gosec floor (`golangci-lint run ./...` = 0 issues), not the semantic changeset pass.

### Retired Backlog Items
#### Clear pre-existing full-tree golangci-lint failures (cyclop, unparam)

Identified in Task 2 (Low priority) when gosec was wired into `.golangci.yml`, and explicitly carved out of Task 6's gosec scope. The three non-gosec findings — `parseTestToken`/`resolveOneSelector` cyclop overruns and the `createTestEntry` unparam — are now fixed via behaviour-preserving extract-method refactors plus an unused-parameter removal. `golangci-lint run ./...` is green tree-wide, unblocking a future hooks `--all` CI lint mode.

## Task 6: Triage deferred gosec findings (perms, subprocess, pprof, http timeout, G304 paths)

### Status: Complete (completed 2026-05-31, ~1 day within the 0.5–1 day estimate)
### Impact: Closes out the gosec security gate. The backlog premise (written at Task 2) was largely stale — perms (G301/G302/G306), subprocess (G204), pprof (G108) and http (G114) had all been handled per-line in Tasks 3–5; only **G304** (file-path-from-variable) remained a blanket exclude. This task audits every gosec exclude and per-line suppression against empirical ground truth, converts the G304 blanket exclude to **23 per-line, trust-classified `//nolint:gosec // G304` suppressions**, and corrects mislabelled comments — leaving `golangci-lint run ./...` gosec-clean with no untriaged debt. No project runtime behaviour changes (comment/config/doc only; on-disk index format and CLI output byte-identical). Branch: 5 planning checkpoints + f-exec (`90ff0f2`) + g-exec (`60e79d4`) + j-retro.

### Changes
- **`.golangci.yml`**: removed `G304` from `gosec.excludes` (now G103/G401/G505 only); added an inline note documenting the conversion. G115 already active (Task 3.3); perms rules already active (Tasks 3–5).
- **21 G304 per-line suppressions** added at the previously blanket-excluded sites: `cmd/dcfhfix/{entry_append_remove,entry_workflow_main,main}.go` (CLI-arg index paths), `pkg/index.go` (own `.dcfh` index opens), `pkg/recovery.go`/`pkg/snapshot.go` (`.dcfh`-internal reads), `pkg/fsdedupe/fsdedupe_linux.go` (scan candidate, `O_NOFOLLOW`), `pkg/hash.go:82/186` (generic hasher — the one untrusted-reachable open, suppression **cites** the `resolveRel`→`hasPathPrefix` wire guard), `pkg/wire_handler.go:398` (operator cache path).
- **2 comment corrections** `G703`→`G304`: `cmd/dcfh/dcfh.go:26,43` (`os.Create` of `DCFH_CPUPROFILE`/`DCFH_MEMPROFILE` env vars) — these emit G304, not G703.
- **CLAUDE.md** Security Review section: removed the stale "G115 is a real deferred bug" wording; recorded the final exclude set (G103/G401/G505), the G304 conversion, and a G703 note.

### Notable
- **Empirical method overturned the plan's central FR4 assumption.** Phases a–e assumed `G703` was *not* a gosec rule and that the `os.WriteFile(dst,…,Mode())` sites would emit **G306**. The scratch ground-truth run proved **G703 is a real gosec rule** in v2.11.2 ("Path traversal via taint analysis"): `pkg/recovery.go:414` and `pkg/snapshot.go:448` already cite the **correct** rule → **accept** (the base-name `dst` origin was verified, not rubber-stamped). The genuinely mislabelled comments were the *other* pair (the env-var `os.Create` sites). Design Decision 1 ("trust the tool, not the comment text") is exactly why this surfaced.
- **G304 policy = CONVERT.** Decision 4 forces conversion when any *untrusted live* G304 site exists. `pkg/hash.go:82/186` is reachable from wire-supplied `req.Paths` via `RemoteHandler.hashOne`; its per-line suppression cites the existing `resolveRel`/`hasPathPrefix` escape guard rather than letting a blanket exclude silence a trust-boundary site. No code `fix` was needed — the guard already existed.
- **G304 live-site count was 21, not the backlog's "27"** (a stale Task-2 figure).
- **Process incident, fully recovered.** A `cd` into the disposable scratch worktree left the shell CWD there, so subsequent `cd "$(git rev-parse --show-toplevel)"` resolved to the *worktree* root: the implementation edits, lint, and tests all ran in the worktree and were deleted with it. The work survived as a dangling `git stash` commit (`a49e33b`, "task6-verify") taken during verification, recovered via `git fsck --unreachable`, re-applied to the main tree, and re-verified from scratch (gosec-clean, `go build`, full `go test -race` green). The committed changeset is provably identical to what passed verification.

### Retired Backlog Items
#### Triage deferred gosec findings (perms, subprocess, pprof, http timeout, G304 paths)

Identified in Task 2 when gosec was wired into `.golangci.yml`. The deferred rules were triaged across the intervening tasks (perms/subprocess/pprof/http handled per-line in Tasks 3–5; G115 re-enabled in Task 3.3); this task closed the remaining work — the G304 blanket exclude — by converting it to 23 trust-classified per-line suppressions and reconciling all suppression-comment rule IDs against gosec's actual emission. Gate verified gosec-clean. (The separate Low-priority "cyclop/unparam full-tree lint" backlog item is explicitly **not** part of gosec scope and remains open.)

## Task 5: Upgrade CWF to v1.1.169

### Status: Complete (completed 2026-05-29, ~1 day across 2 sessions vs <0.5 day estimate — ~2× variance attributable entirely to the v1.1.163 packaging defect)
### Impact: Operational chore — upgrades the installed CWF subtree from v1.1.155 to v1.1.169 (SHA `473baea2dd1d77bac9f100a1036f091eeccd0a4b`, the annotated-tag object SHA). Retires the **Medium**-priority BACKLOG item "Upgrade CwF v1.1.155 to v1.1.163" — repointed forward to v1.1.169 mid-task because the original target carried a packaging defect (upstream T167) that aborted `cwf-apply-artefacts` under non-TTY on every consumer. The pivot resolved that blocker by construction (T167 dropped the offending `rules-inject` manifest artefact entirely) and picked up T164/T165/T166/T168 as side benefits. Branch tip: 9 install.bash auto squash commits + 1 post-laydown metadata commit + 6 phase checkpoints. `cwf-manage validate: OK` on completion; all 6/6 success-path TCs pass.

### Changes
- **`.cwf/`, `.cwf-skills/`, `.cwf-rules/`, `.cwf-agents/`**: replaced wholesale at v1.1.169 via `cwf-manage update v1.1.169` (subtree path; the v1.1.155 wrapper delegates laydown to the target ref's `scripts/install.bash`). 9 auto-generated squash commits, no manual edits.
- **`.cwf/version`**: rewritten with `cwf_version=v1.1.169`, `cwf_ref=v1.1.169` (flipped from `HEAD`), `cwf_sha=473baea2…`, plus new field `cwf_install_manifest_sha=e1926a2f…` (added in v1.1.169 as part of T167's INV-1/INV-2 plumbing).
- **`.claude/settings.json`**: settings-merge added 1 `SubagentStop` hook entry (matcher `cwf-security-reviewer-changeset`, command `.cwf/scripts/hooks/subagentstop-security-verdict-guard`, timeout 5; T162 contract) plus 3 allowlist entries for new helper scripts. Existing `Stop` hook intact.
- **No project source code touched.**

### Notable
- **The v1.1.163 attempt failed at `cwf-apply-artefacts`** because that version's manifest's `rules-inject` source pointed at an empty placeholder file (`.cwf/templates/install/rules-inject.txt`, SHA `e3b0c442…`) while the subtree shipped `.cwf/rules-inject.txt` populated (331 bytes). `apply_replace` saw on-disk ≠ baseline ≠ new and aborted on the 3-way conflict under no-TTY. Upstream Task 167 (in v1.1.167) acknowledged this as a packaging defect and dropped the artefact entry entirely (subtree becomes the sole distribution mechanism). We pivoted forward to v1.1.169 rather than working around with `CWF_UPGRADE_RESOLVE=keep`.
- **Recovery from the half-applied v1.1.163 state was non-destructive.** Soft-reset + targeted `git checkout -- .cwf .cwf-skills .cwf-rules .cwf-agents .claude` + `git checkout -- .cwf/version` + drop 3 orphan untracked helper files. No `git reset --hard` (the user had explicitly denied that). The pattern is now codified in d-plan Step 2 as the documented escape hatch for any future laydown abort.
- **T166's subtask-aware fix delivered observable consumer-side value.** Same branch state under v1.1.155 had `task-context-inference` returning `inconclusive, uncorrelated, task_nums: 2,5`. Under v1.1.169 it returns `conclusive, correlated, task_num: 5, workflow_step: f-implementation-exec`. First in-band confirmation of T166 in this consumer.
- **Plan-prediction correction**: `cwf_sha` is the **annotated-tag object** SHA from `git rev-parse v1.1.169` (= `473baea2…`), not the dereferenced commit from `git rev-list -n1` (= `0764380…`). The v1.1.163 attempt's TC-1 had this inverted; the v1.1.169 plan corrected and observed it.
- **Plan-review subagents found real gaps.** 4-parallel reviewers (improvements/misalignment/robustness/security) against the d-plan caught 8 substantive issues: precondition HEAD gate, deterministic half-applied probe, per-invocation (not per-artefact) `CWF_UPGRADE_RESOLVE` semantics, `git clean -fdx --dry-run` in revert path, hook-helper executable check, smoke-test pass/fail rubric, named-path staging in commit, and the manifest/hashes-ride-the-subtree-commit clarification. None fired on the clean v1.1.169 run — they're insurance for future consumer upgrades.
- **Security-review state recorded as `error: cap exceeded`** in both f-exec and g-exec. The helper reports `reviewed 19 files, 2252 lines (1632 production), anchor=07366ad`. Anchor at the task baseline includes the entire v1.1.169 laydown as production. The actually-new-in-this-task surface (settings.json hook entries, .cwf/version, workflow MD) is small and surfaced inline. Filed BACKLOG follow-up: configure `security.review.test-paths` to exclude upstream-shipped CWF directories so future upgrades don't trip the cap by construction.

### Retired Backlog Items
#### Upgrade CwF v1.1.155 to v1.1.163

The installed CwF tooling was v1.1.155, pinned via the subtree method from `file:///home/matt/repo/coding-with-files`. The original BACKLOG item targeted v1.1.163 to pick up a `security-review-changeset` empty-changeset fix observed during Task 3.3's f-phase. Landed as v1.1.169 instead — the v1.1.163 target carried an unrelated `rules-inject` packaging defect (upstream T167) that aborted `cwf-apply-artefacts` under non-TTY. Pivoting to v1.1.169 resolved that defect by construction and also picked up: T164 (hierarchy-aware validation), T165 (template-reference linter), T166 (subtask-aware context inference — *observably broken* on this branch under v1.1.155, *fixed* under v1.1.169), and T168 (production-weighted security-review cap). Verified end-to-end: `cwf-manage status` reports v1.1.169 with `cwf_sha=473baea2…`; `cwf-manage validate` exit 0; all 6/6 success-path TCs PASS.

## Task 4: Move FileSize/ByteSize to int64 in v4 and core

### Status: Complete (completed 2026-05-27, ~0.5 day — within the 0.5–1 day estimate)
### Impact: `os.FileInfo.Size()` is `int64`, but the on-disk `ByteSize` was `uint64`, so every bridge between them was a gosec-G115-flagged signed↔unsigned conversion. Task 3.3 had annotated 7 of these on the `FileSize`/`.Size()` path. This task reinterprets the on-disk size field as signed `int64` throughout `pkg/format` and core, retiring those 7 suppressions **by removing the conversions** rather than re-suppressing. It is a signedness reinterpretation of an already-8-byte field — **not** a format bump: `CurrentIndexVersion` stays 4 and v4 indices round-trip byte-identical.

### Changes
- **`pkg/format/vocabulary.go`**: `ByteSize` alias `uint64`→`int64` (the single load-bearing edit). The `Entry`/v2/v3/v4 struct fields, codec `Get/SetFileSize`, and `transcode.go` follow transparently via the alias.
- **Interfaces + 7 implementors** retyped to `int64`: `BinaryEntryInterface.FileSize`, `FilterEntry.FileSize`, and their `binaryEntryAdapter`/`entryInfoAdapter`/`scanFilterEntry`/`BEScanEntry`/`BESkiplistEntry`/`BEIndexFileMmapEntry`/`mockBinaryEntry` implementations. `needsHash` compares FileSize inline as `int64` (pulled out of the `uint64`-typed field slice) to avoid re-introducing a conversion.
- **Signed size-threshold plumbing**: `ParseSizeBound`→`int64` (with an explicit leading-sign guard preserving the unsigned-magnitude grammar `ParseUint` enforced), `SizeBoundString`, `FilterOptions.MinSize/MaxSize`, `MinSizeTest.Min`, `MaxSizeTest.Max`, `FindOptions.ExactSize`, `dedupeDefaultMinSize`. `EntryInfo`/`EntryJSON` `FileSize`, `ValidationConfig.MaxFileSize`, and the dcfhfix repair parse (`parseInt64`) all signed end-to-end.
- **Corruption floor (SC3)**: a negative `FileSize` (the only value the signed reinterpret can newly produce, from a corrupt/≥2⁶³ legacy field) is rejected fail-closed by `validateFileSizeBounds` (recovery.go) and the dcfhfind corruption checks — mirroring the pre-1885 wall-time underflow guard.
- **7 size G115 suppressions retired** (binary_entry_scan:74, scan:262, filter SizeTest, comparison_sink ×3, metastore) by deleting the cast. `golangci-lint run ./...` reports **0 G115** whole-tree; the `--new` staged gate is clean.

### Notable
- All four success criteria verified (g-testing-exec.md): SC1 v4 byte-identical + large-positive round-trip, SC2 0 G115 whole-tree + `--new` clean + the 7 lines suppression-free, SC3 legacy decode unchanged + negative-size fail-closed, SC4 full suite + race green. 0 escaped defects.
- `fsdedupe` byte totals (`BytesReclaimed`/…, `fileTarget.size`) and the dupes.go formatting casts are a **distinct `uint64` type** with its own JSON contract — deliberately out of scope (3 suppressions retained there), recorded not deferred.
- The four-reviewer implementation-plan pass converted a "looks like one line" change into a correctly-counted 27-file compiler-driven ripple before exec; the two in-flight additions (`ParseSizeBound` sign guard, `validateFileSizeBounds` cyclop extraction) were second-order interactions only the build/lint gate surfaces.
- Closes the **Medium** BACKLOG item "Move FileSize/ByteSize to int64 in v4 + core" (carried the stale "subtask 3.4" naming; landed as top-level Task 4).

## Task 3: Fix inode/device truncation and re-enable gosec G115

### Status: Complete (parent task; completed 2026-05-26 — ~4 calendar days across 3 subtasks vs a ~1.5–2 week single-task estimate)
### Impact: `dcfh` stored `Dev`/`Ino` as `uint32` and keyed hardlink dedup on a truncated `[2]uint32`, silently collapsing distinct files as hardlinks on large-inode filesystems (XFS, Btrfs, large ext4). This parent task eliminates that by widening `Dev`/`Ino` to 64-bit and, to make the change single-sited, first encapsulates the entire versioned on-disk format behind one module (`pkg/format`) — then re-enables the gosec G115 gate Task 2 had deferred. Closes the **Very High** inode/device-truncation backlog item.
### Status: Complete (subtask of Task 3; completed 2026-05-25, within the 2–3 day estimate)
### Impact: dcfh stored `Dev`/`Ino` as uint32 and keyed hardlink dedup on a truncated `[2]uint32`, so on large-inode filesystems (XFS, Btrfs, large ext4) two distinct files whose low 32 bits collided were silently treated as hardlinks and one was dropped from `dcfh dupes`. This widens `DevID`/`Inode` to `uint64` (on-disk format **v4**), removes the ingest truncation, and re-enables the gosec G115 gate that was disabled to land Task 2. Closes the parent's **Very High** backlog item.
### Status: Complete (subtask of Task 3; completed 2026-05-24, within the 1–2 day estimate)
### Impact: The index load path's "cast the mmap to the entry layout" step was implicitly correct only because every shipped version (v2, v3) has a byte-identical entry layout — there was no owned decision tying a version to *how* it is materialised, and no gate rejecting an out-of-range version byte before use. Task 3.2 makes that decision an explicit, single-owned, tested seam (`StrategyForVersion`) and gives `pkg/format` ownership of the write version, so Task 3.3's v4 becomes "flip one dispatch arm", not "add a new load-path branch". No on-disk format, width, or version change.
### Status: Complete (subtask of Task 3; completed 2026-05-24, well under the 2–4 day estimate)
### Impact: The on-disk layout (entry/header structs, field widths, offset tables, version constants/validation) was duplicated across `pkg` and `cmd/dcfhfix`, so the upcoming inode/device width change (Task 3.3) would have had to be applied in several places. A new `pkg/format` package is now the single, cycle-free owner of that layout; core and dcfhfix alias onto it. No on-disk format, field-width, version, or behaviour change.

### Changes (delivered across three subtasks — see their entries below for detail)
- **3.1 (chore)** — extract `pkg/format` as the single, cycle-free owner of the on-disk layout (vocabulary, canonical `Entry`/`Header`, bounds-checked codec, version constants/validation); migrate core + dcfhfix onto it; delete both dcfhfix duplicates and the parallel offset table. No width/version/behaviour change.
- **3.2 (feature)** — add the owned `version → DecodeStrategy` dispatch seam and give `pkg/format` ownership of the write version, so the v4 bump becomes "flip one dispatch arm". No format change.
- **3.3 (bugfix)** — widen `DevID`/`Inode` to `uint64` (on-disk format **v4**); legacy v2/v3 read via a bounds-checked heap transcode; `dupes` key `[2]uint64`; ingest truncation removed; gosec G115 re-enabled and whole-tree clean.

### Notable
- All five parent success criteria verified end-to-end on the merged branch (f-implementation-exec.md): full-width dev/ino + full-width dupes key, single-module format ownership, v2/v3→v4 heap-transcode round-trip, G115 active with 0 whole-tree findings, full race suite green.
- The decomposition retired both named high-priority risks structurally: the no-behaviour-change extraction (3.1) landed and was verified green before any width/version change (3.3), and the type-alias vocabulary made the widen a compiler-driven audit rather than a manual cross-codebase sweep.
- Five pre-existing latent defects (dcfhfix `GetPath` offset + header over-read, a v3-header truncation over-read, a use-after-munmap, and a `GetDev`/`GetIno` width over-read) were found and fixed in-flight via planned gates; 0 escaped.
- G115's whole-tree count went 63 → 52 → 52 → **0**; the Dev/Ino class fixed structurally (zero suppressions on it). Three pre-existing non-G115 findings (cyclop ×2, unparam ×1) in untouched functions remain backlogged — not regressions; the enforcing `--new` gate is clean.

## Task 3.3: Widen Dev/Ino to uint64 and re-enable gosec G115

### Changes
- `pkg/format`: `DevID`/`Inode` `uint32`→`uint64`; `CurrentIndexVersion` 3→4 (every entry field after `Ino` shifts +8). v4 is written exclusively.
- Legacy read-as-transcode: v2/v3 indices load via a new bounds-checked `TranscodeLegacyIndex` into a v4-layout heap image (never cast in place); `StrategyForVersion`'s legacy arm flipped `DecodeZeroCopy`→`DecodeHeap`; both mmap loaders branch on the strategy and back the index with a `heapBacked` `mmapIndexFile` (`Cleanup` never munmaps the GC buffer).
- Per-version layout split: `entry_layout.go` + `v2_layout.go`/`v3_layout.go`/`v4_layout.go`, with build-time assertions pinning v3==v2 and v4==v2+8; `layoutForVersion` is a fail-closed per-version switch. `SafeEntry`/dcfhfix reads are version-aware; `GetDev`/`GetIno` read legacy fields as uint32 and widen (`narrowDevIno`).
- Ingest/consumers: removed both truncation casts (`binary_entry_scan.go`, `scan.go`); `dupes` dedup key is `[2]uint64`; the two accessor interfaces, their implementers, and backing struct fields widened.
- dcfhfix: read-old/write-v4 — `createTempIndexWithHeader` stamps a v4 header (fixing a corrupt v3-header-over-v4-entries output), version threaded into `NewValidatedEntry`/`NewSafeEntry`, two size floors made version-aware.
- Deleted dead `BEIndexFileIOEntry` (a parallel cast site).
- Re-enabled gosec **G115** (`.golangci.yml`): `golangci-lint run ./...` reports 0 G115; the Dev/Ino truncation class fixed structurally (zero suppressions on it), 55 provably-safe sites annotated per-line with rationale.
- Tests: v3 decode-via-`DecodeHeap` (both loaders, every field), v4 round-trip byte-identity, `TranscodeLegacyIndex` positive + fail-closed (incl. oversized `EntryCount` with no allocation), `layoutForVersion` table, dupes-correct-on-large-inodes, heap-backed `Cleanup` no-munmap, dcfhfix v4 stamp. Committed goldens `testdata/v3.idx` (genuine, captured pre-bump) + `testdata/v4.idx`, and `.gitattributes *.idx binary`.

### Notable
- The width fix is structural, not suppression: G115 stays active and the Dev/Ino/EntryCount/Size conversions were resolved by widening, not `//nolint`.
- A real decode defect was caught in exec: `GetDev`/`GetIno` read 8 bytes regardless of version, over-reading a legacy entry's Dev into Ino — fixed with the per-layout `narrowDevIno` flag.
- Accepted degradation: pre-existing v3 entries already lost their high Dev/Ino bits at ingest; the v4 bump rewrites them on the next `update`, so there is no separate migration tool.
- The changeset security review returned an empty changeset (CwF v1.1.155 uncommitted-diff bug + by-design Go-code scoping); reviewed manually (no findings, two category-(e) pattern notes) and a v1.1.163 upgrade was backlogged.
- Follow-up subtask 3.4 backlogged: move `FileSize`/`ByteSize` to `int64` and ring-fence the uint64→int64 cast in the transcoder, retiring ~6 G115 suppressions (on-disk-compatible).

### Retired Backlog Items
#### Inode/device truncation makes dcfh dupes under-report on large-inode filesystems
Retired from BACKLOG (was **Very High**, identified in Task 2). Closed by this task: `DevID`/`Inode` widened to `uint64` (format v4), both ingest truncation casts removed, dedup key now `[2]uint64`, and gosec G115 re-enabled and structurally clean.

## Task 3.2: Add version-aware read/write dispatch seam in pkg/format

### Changes
- Added `pkg/format/version_dispatch.go`: `StrategyForVersion(version) → (DecodeStrategy, error)`, a switch-with-default that maps current/recognised-legacy versions to `DecodeZeroCopy` and rejects everything else — never raw-indexed by the untrusted version byte. (`DecodeHeap` is intentionally absent until v4 gives a legacy entry layout to decode in 3.3.)
- `SetHeaderForWritableIndex` dropped its `version` parameter and now sources `CurrentIndexVersion` from `pkg/format`; all three production writers migrated (compiler + grep confirmed no caller passes a version).
- Consolidated the version gate and a header-size bounds guard into one shared `checkEntryRegionAccess`, called by both top-level mmap loaders (`openAndValidateIndex`, `loadIndexFromFileWithTracking`); extracted `parseTrackedEntries` to keep the tracking loader under the cyclop limit.
- Added `pkg/format/version_dispatch_test.go` (resolver table, 100% coverage) and `pkg/version_dispatch_load_test.go` (out-of-range rejection + v3-header truncation, both loaders, race-clean).

### Notable
- The header-size guard closed a **latent v3-header truncation over-read**: an 88–103-byte file with a v3 header passed the old `V2HeaderSize` size check and then panicked slicing `data[104:]`. It now fails closed. The planned truncation test (TC-4) caught a use-after-munmap in the first guard cut — reading `header.Version` after the mmap was released — fixed by forming the error before any unmap.
- gosec G115 unchanged at 52 (the 3.1 baseline); no on-disk format change, so v2/v3 indices still load and write byte-identically.
- Deliberately the *non-speculative* half of the parent's read-old/write-new model: the concrete legacy entry decoder, dcfhfix repair read-path resolver adoption, `BEIndexFileIOEntry.readEntryData` routing, the `Dev`/`Ino` widening, and re-enabling G115 are deferred to Task 3.3, where v4 gives them a divergent layout to exercise.

## Task 3.1: Extract pkg/format as single owner of on-disk layout

### Changes
- Added `pkg/format`: vocabulary aliases (`DevID`/`Inode`/`WallTime`/… — same-width, single owner of width/signedness), canonical `Entry` + `Header` (with all methods), version constants + `headerSizeForVersion`/`ValidateVersion`, and a two-tier bounds-checked `SafeEntry` codec (generic `readField`/`writeField`). Verified cycle-free via `go list`.
- Migrated `pkg` (`type binaryEntry = format.Entry`, `type indexHeader = format.Header`, thin forwarders, `asFilterEntry` → free function, exported clean-bit methods) and `cmd/dcfhfix` (deleted the duplicate `binaryEntry`/`indexHeader` + the parallel `unsafe.Offsetof` table; now imports `pkg/format`).
- Fixed two latent dcfhfix defects surfaced by the consolidation: `GetPath` read the path from the unused trailing `Path[8]` field instead of after the fixed struct; the 96-byte header duplicate over-read 8 bytes when cast to `[104]byte` in the write path.
- Added round-trip (v3 byte-identity), version-offset (v2 entries at 88, v3 at 104), header-size-invariant, and codec bounds-tier tests.

### Notable
- The type-alias strategy (`type T = U`) made the migration a compile-checked near-zero-diff — the compiler, not manual auditing, found every width-coupled call site. Go's prohibition on declaring methods on an alias to an out-of-package type was the one wrinkle: resolved by owning the type in `pkg/format` and aliasing everywhere else.
- gosec G115 went 63 → 52 (the narrowing conversions were consolidated, not added); the gate's "must not increase" intent is satisfied. G115 stays excluded until Task 3.3 widens `Dev`/`Ino` to `uint64`.
- Does not complete the Very High backlog item (inode/device truncation + re-enable G115) — that is Task 3.3; 3.1 is the enabling extraction. Partially advances "Fix primitive + dcfhfix restructure" by moving the bounds-checked accessor into `pkg/`.

## Task 2: Adopt full Go pre-commit hook and security review

### Status: Complete
### Impact: The repo ran without a security-focused static-analysis gate. `gosec` is now wired into `.golangci.yml` (so it fires on every `golangci-lint` run, including the staged `--new` pre-commit path) and contributes zero findings to a clean tree while staying active for new code. Adopting it immediately surfaced one genuine latent bug (inode/device truncation in `dcfh dupes`, now backlogged Very High).

### Changes
- Enabled `gosec` in `.golangci.yml`. Architectural/intentional rules excluded with documented rationale (G103 unsafe/mmap, G304 file-scanner paths, G401/G505 git-compatible SHA-1) plus G115 deferred to a backlogged bug fix. Test-only false positives scoped via `{linters:[gosec], path: _test\.go}`.
- Suppressed 26 production false positives with per-line `//nolint:gosec // Gxxx: rationale` (perms on non-secret `.dcfh/` files, `DirEntry.Name()` base paths, opt-in localhost pprof, fixed-`"ssh"`-binary subprocess) — never a blanket disable; perms/subprocess rules stay active for new code.
- Lifted golangci-lint's issue-display caps (`max-same-issues: 0`, `max-issues-per-linter: 0`) so the security gate never silently hides a duplicate finding.
- Bumped the Go toolchain to `go1.26.3` (go.mod `toolchain` directive) to clear `GO-2026-4971`, which the hook's govulncheck step correctly blocked on.
- Documented the dual security-review process (gosec floor + CWF `cwf-security-reviewer-changeset`) in `CLAUDE.md`.

### Notable
- Measure security linters through the enforcement path, not standalone: setting `gosec.excludes` activates gosec's full ruleset, and golangci-lint's default `max-same-issues: 3` had hidden over half the findings during planning (true count was 26 sites, not ~12).
- Read the code, don't trust the verdict: per-finding review found exactly one real bug among ~230 raw findings and kept suppression precise rather than blanket.
- The pre-commit gate proved itself — govulncheck blocked a freshly-published CVE on unchanged code; resolved via toolchain bump with no `--no-verify`.
- Follow-ups backlogged: Very High (inode/device truncation fix + re-enable G115), High (deliberate suppression-review pass), Low (clear 3 pre-existing non-gosec lint failures).

## Task 1: Conform BACKLOG and CHANGELOG to CWF format

### Status: Complete
### Impact: The 15 existing BACKLOG entries were invisible to the `backlog-manager` tooling — they used the legacy `## Entry:` heading, so `list` returned nothing and `validate` passed only because it recognised zero entries. After conversion all entries are tool-visible and the heading-tree contract is enforced on every change.

### Changes
- Converted `BACKLOG.md` to the CWF heading-tree schema: `## Entry:` → `## Task:`, added a `### Task-Type:` to each of the 15 entries (feature ×8, chore ×6, bugfix ×1), and replaced the self-documenting template header with a one-line intro. All titles and bodies preserved verbatim.
- Archived the version-based `CHANGELOG.md` (Keep a Changelog / SemVer, plus the `## Rejected` design log) to `docs/changelog-old.md` byte-identically, and started this fresh CWF by-task changelog.

### Notable
- The `list` count, not the `validate` exit code, is the real conformance oracle for this kind of migration — a clean `validate` on zero recognised entries is a false positive.
- Archive-then-recreate at the same path defeats git's `R100` rename detection; archive integrity was verified by blob-hash equality instead.
- `pkg/ignore.go:106`'s stale "see CHANGELOG" reference was left untouched (out of scope for a docs chore) and logged as a Low-priority follow-up.
