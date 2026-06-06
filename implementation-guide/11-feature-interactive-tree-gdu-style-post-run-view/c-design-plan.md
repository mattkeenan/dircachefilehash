# interactive-tree gdu-style post-run view - Design
**Task**: 11 (feature)

## Task Reference
- **Task ID**: internal-11
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/11-interactive-tree-gdu-style-post-run-view
- **Template Version**: 2.1

## Goal
Define the architecture for a post-run, read-only, gdu-style interactive tree viewer launched by `--interactive-tree` on `dcfh status` and `dcfh update`: a left directory tree and a width-gated right before/after stats pane, built from in-memory/on-disk index structures (no second filesystem walk).

## Design Priorities
Correctness → Testability → Readability → Consistency → Simplicity → Reversibility
(Correctness leads per the project guideline correct > maintainable > performant; the
before/after classification and terminal teardown are the correctness-critical parts.)

## Architecture Preferences
Composition over inheritance. Interfaces over singletons. Explicit over implicit.

## Key Decisions

### KD1 — Two-layer split: pure data layer (in `pkg`) + separate render package (the load-bearing decision)
The viewer is split so that **all tree/stats logic is terminal-free and unit-testable**, and the TUI library only does input+paint. The split is by **purpose, not necessarily by package**, because the data the builder consumes (`skiplistWrapper`, `binaryEntry`) is unexported in `package dircachefilehash`:

- **Data layer — lives *inside* `package dircachefilehash`** (new terminal-free files, e.g. `pkg/treeview.go`): builds an exported `Tree` of `*Node` from (a) the merged index skiplist (full file set + per-file sizes + deleted flag) and (b) a `ChangeSet` (path-set membership for category labels). Aggregates stats up every directory. It can name the internal types directly and is unit-tested within the existing `pkg` test suite — "pure" here means *no terminal import*, not a separate module.
- **Render layer — separate package** (`cmd/dcfh/internal/tui` or `pkg/treeview/tui`): owns the terminal, draws the two panes, handles keys/resize/teardown. Imports **only the exported `Tree`/`Node`/`Stats`/`Category`** — never `skiplistWrapper`/`binaryEntry`. Never mutates index or filesystem.

**Rationale**: resolves the package-boundary problem (unexported types) while preserving the testability seam (NFR3). Hard correctness (aggregation, before/after classification) is tested without a TTY in `pkg`; risky terminal handling is isolated in the render package.
**Trade-off**: the data layer is not its own package, so the seam is enforced by file/discipline + the exported `Tree` boundary rather than the compiler. Acceptable — the meaningful boundary (no terminal in the data path; render sees only `Tree`) still holds.

### KD2 — Data source: one merged-index reload is the single source of all per-file sizes
Per the requirements' Data-Source Decision, the viewer needs the *full* file set (incl. unchanged and deleted) with per-file sizes. The diff/update paths **discard unchanged entries in memory**, so after the command finishes:

- **Tree + sizes (single source)**: reload via the existing exported helper **`(*MetaStore).LoadMergedMainCacheIndex()`** (`pkg/index_loading.go:89`) — it already does load-main + overlay-cache + missing-cache tolerance (replacing the hand-rolled `LoadMainIndex`+`loadCacheIndex`+`Merge` the first draft listed). Iterate with `skiplistWrapper.ForEach(func(*binaryEntry, string) bool)` (second arg is the **relative-path string**, not a context). Each entry yields path, size, and `IsDeleted()`. **This is an index-file read (mmap), not a filesystem walk** — it honours "no second *filesystem* walk". The memo refreshes on the index mtime change from the atomic rename, so the reload sees post-run state (implementation-plan to assert).
- **Single-source rule for sizes (resolves the deleted-entry double-count risk)**: the merged index is the **only** source of per-file sizes for **every** category. `cache.idx` retains deleted-flagged entries *with their last-known size* for both commands, so the merged skiplist already contains added, modified, unchanged **and** deleted entries with sizes. The `ChangeSet` is used **only to label** live (non-deleted) entries as added/modified/unchanged by path-set membership; it is never a byte source for the tree. Deleted is determined purely by the entry's `IsDeleted()` flag. This removes any mixing of "reload for sizes" vs "ops for bytes".

**Rationale**: reuses one battle-tested loader; one authoritative size source eliminates double-counting/missed entries; "unchanged" and "deleted" both fall out without extra bookkeeping.
**Trade-off**: a second index load after the run (cheap — mmap + skiplist, no hashing). Acceptable; bounded by index size, not file contents.

### KD3 — `status` needs no core change; `update` gets one contained (cross-layer) enrichment
- **status**: `repo.Diff` already returns `*StatusResult{Modified, Added, Deleted, *Bytes}`. The path-sets **are** the `ChangeSet` labels — adapt directly. Zero core change.
- **update**: `repo.Apply` returns only `*UpdateResult{FileCount, TotalSize, PathsUpdated}` — no per-file change list, and the pre-update state is gone after the atomic rename. The update pipeline already classifies every `PipelineEntry.Operation` (`OpUnchanged/OpModified/OpNewFile/OpDeleted`) and discards it. **Enrichment**: collect the changed-path sets during the pipeline and surface them on `UpdateResult` (additive fields `Added/Modified/Deleted []string`), populated only when the caller requests it (interactive-tree on). The viewer then labels update exactly like status.
  - **Honest reach (not just a struct edit)**: the op classification is consumed in `pkg/pipeline_update.go` (the writer/retired-entry stage), but `UpdateResult` is assembled in `pkg/repo_local.go` (`repoCore.Apply`), and `runUpdate` (`pkg/update.go`) returns only `error` today. Surfacing the sets requires threading a small **optional collector** from the writer loop → `runUpdate` → `Apply` — a new return path across three layers. The implementation-plan must budget for this, not treat it as a field addition.

**Rationale**: the only honest way to get update's change-set is to expose data update *already computes*; re-running a `Diff` would be a second filesystem walk (rejected).
**Trade-off**: touches core update result plumbing (not "TUI only"); accepted in requirements review. Default-off so the non-interactive/JSON path is byte-for-byte unchanged.
**Limitation (documented)**: per-file sizes (incl. for modified files) come from the post-run merged index (KD2), so the pane shows *current* sizes; an exact pre-update vs post-update byte delta for *modified* files (old size) is not retained and is out of scope. Counts per category are exact; the "before" byte figure for modified files is therefore approximate and labelled as such.

### KD4 — TUI library: `tcell` (recommended) ⚑ KEY DECISION FOR REVIEW
Rendering needs: two resizable panes, key/resize events, guaranteed terminal restore, and precise control to keep crafted filenames from reaching the terminal raw.

| Option | Deps added | Fit | Notes |
|---|---|---|---|
| **`github.com/gdamore/tcell/v2`** (recommended) | tcell + a couple transitive | Good | The stack **gdu itself uses**. Cell-level control, `EventResize`, guaranteed `Screen.Fini()` teardown, alt-screen. We hand-build the (simple) tree+stats widgets. |
| `github.com/rivo/tview` | tcell **+ tview** | Good ergonomics | Ready-made `TreeView`/`Flex`; more deps, less layout control, heavier than we need for two panes. |
| `bubbletea`+`lipgloss`+`bubbles` | 3+ charm deps | Nice arch | Elm-style Model/Update/View is testable, but our model is already testable in `treeview`; this adds the largest dependency surface. |
| Hand-rolled `golang.org/x/term` + raw ANSI | smallest | Risky | Most code; we'd re-implement resize, alt-screen, and teardown — the exact failure modes the requirements call out (terminal restore, escape-safety). Contradicts "reduce risk". |

**Recommendation: `tcell`.** It is the lightest dependency that still gives robust teardown/resize/alt-screen (the requirements' highest-risk areas) without us re-implementing them, and it matches the reference tool (gdu). The minimal-dependency tension is real but the repo already carries cobra+viper, so one focused TUI dependency is consistent with existing weight.
**This is the main decision to confirm at review** — if the minimal-dependency ethos should veto a TUI dep entirely, the fallback is hand-rolled `x/term`, accepting more code and more teardown/escape risk.

### KD5 — TTY detection and terminal sizing via `golang.org/x/term`
No isatty/term usage exists today. Add `golang.org/x/term` (small, official) for `term.IsTerminal(int(os.Stdout.Fd()))` (FR3 guard) and initial size; live resize comes from tcell's `EventResize`. (tcell pulls `x/term` transitively in any case.)
**Rationale**: official, tiny, avoids a second isatty dependency.

### KD6 — Escape-safe rendering utility (reject-by-default)
A single `sanitiseLabel(string) string` in the data layer neutralises unsafe bytes **before** any path is stored on a `Node`. It uses a **reject-by-default printable allowlist / `strconv.Quote`-style escape**, not a blocklist of CSI sequences — so it also neutralises OSC (`ESC ]`, title/clipboard injection), DCS, and bare `\r`/`\b`/`\n`/`\t`, the full C0 and C1 ranges, not just `ESC [`. The render layer only ever draws the already-sanitised `Node.Label`; **truncation to pane width operates on the sanitised label and cuts on rune boundaries** — the renderer never re-slices raw bytes or re-derives a display string from a path. Error/teardown messages — **including wrapped tcell/OS error text that may embed an attacker-influenced filename on the FR9 init-failure path** — are passed through the same helper before reaching the restored terminal.
**Rationale**: centralises the one real new attack surface (NFR4/AC7) at the model boundary with a default-deny policy so no raw filename byte can reach the terminal — via labels, truncation seams, or error strings.

### KD7 — Terminal teardown and SIGINT ownership
The render layer guarantees terminal restoration across normal quit, panic, Ctrl-C, and init failure:
- `defer screen.Fini()` is installed **immediately after a successful `screen.Init()` and before any draw**, and `Fini()` must be **idempotent / safe to call after a failed `Init()`** (FR9/AC8) so a half-initialised alt-screen cannot leave the terminal altered.
- **SIGINT ownership**: the root command already wires `signal.NotifyContext` (`cmd/dcfh/root.go:224`). For the viewer's lifetime, tcell owns terminal input and surfaces Ctrl-C as a key/`EventInterrupt`, which the event loop treats as quit (clean `Fini()` + return). The design avoids a second OS-level SIGINT handler racing tcell; if the inherited cancellable context fires, the loop still exits through the same teardown path. The implementation-plan pins the exact mechanism (tcell `EventInterrupt` vs. honouring `ctx.Done()`), but the invariant is: **every exit path runs `Fini()` exactly once and the terminal is usable afterwards.**
**Rationale**: terminal restore on Ctrl-C/panic is the requirements' highest-reliability ask (NFR5/AC6); making teardown idempotent and giving tcell unambiguous input ownership removes the raw-mode-leak race.

### KD8 — Sorting is a render-layer concern over existing `Stats` counts (FR10)
Sort order is **switchable at runtime**, so it must not be baked into the data layer:
- **`BuildTree` is pure and order-stable**: it emits each node's children in a **canonical name-ascending** order. It does no sort-key ranking. Switching sort therefore never re-enters `pkg` or re-reads any index/filesystem.
- **The render layer owns the active comparator**, applied to the (already-built) children at draw time. It re-sorts in place on a key press and re-renders.
- **Keys = `Stats` counts already present** (`Added/Modified/Deleted`): default comparator is `Added+Modified+Deleted` descending; toggles select added / modified / deleted / name; a direction toggle flips asc↔desc; name-ascending is the stable tiebreak. **No new byte aggregation** — counts are uniform and exact across all three change types, which is why the metric is counts, not bytes (deleted/old-modified sizes aren't retained; byte-weighting deferred).
**Rationale**: keeps the data layer deterministic/testable and the feature cheap (the data already exists); runtime switching is essential because `update` is destructive and the snapshot is one-shot.
**Trade-off**: the renderer holds children in a small re-sortable slice per expanded node — trivial memory, no correctness cost.

## System Design
### Component Overview
- **`Tree` / `Node` / `Stats` / `Category`** (exported, in `pkg`): immutable tree; per-node aggregated `Stats` (per-category counts + aggregated current size). Built once, read by the renderer through the exported types only.
- **`BuildTree(merged *skiplistWrapper, cs ChangeSet) *Tree`** (in `pkg`, same package so it can name internal types): splits relative paths on `/`, creates directory nodes, attaches files, assigns each live entry a `Category` by `ChangeSet` membership and deleted entries by `IsDeleted()`, takes per-file size from the merged entry (KD2 single source), rolls `Stats` upward, sanitises labels, emits children in **canonical name-ascending** order (runtime sort is a render concern — KD8).
- **`ChangeSet`**: command-agnostic *label* description — path-sets only (Added/Modified/Deleted); adapted from `StatusResult` or enriched `UpdateResult`. Optional aggregate byte totals are carried for a summary header line, not as the tree's size source.
- **`tui.Run(t *Tree, opts) error`** (render package): opens tcell screen, renders panes, runs the event loop, guarantees idempotent `Fini()` on every exit path (quit/panic/Ctrl-C/init-failure). Returns nil on quit; returns a (sanitised) init error (FR9) without having mutated anything.
- **CLI glue** in `cmd/dcfh/status.go` + `cmd/dcfh/update.go`: register per-command `--interactive-tree` flag; after the normal run + summary, if flag set ∧ stdout is a TTY ∧ not `--json`, build the `ChangeSet`, reload via `LoadMergedMainCacheIndex()`, `BuildTree`, and call `tui.Run`.
- **`pkg` update enrichment**: optional change-set collector threaded writer-loop → `runUpdate` → `Apply`, surfaced on additive `UpdateResult` fields (KD3).

### Data Flow
```
status:  repo.Diff  ─► StatusResult   ──┐ (path-sets)
update:  repo.Apply ─► UpdateResult*  ──┤ (path-sets, enriched KD3)
                                        ▼
                                  ChangeSet (labels only)
   (*MetaStore).LoadMergedMainCacheIndex() ─► merged *skiplistWrapper
        (single source of per-file sizes: added/modified/unchanged/deleted)
                                        ▼
                  BuildTree(merged, changeSet)        [pure-of-terminal, tested in pkg]
                  · size  ← merged entry (KD2)
                  · label ← ChangeSet membership / IsDeleted()
                  · sanitiseLabel() at Node construction (KD6)
                                        ▼
                                  Tree (immutable, exported)
                                        ▼
   guard: --interactive-tree ∧ term.IsTerminal(stdout) ∧ !--json
                                        ▼
                       tui.Run(tree)  ── tcell screen ──► panes
        keys: ↑/↓ move · →/Enter expand · ←/h parent · q/Ctrl-C quit
        sort (KD8/FR10): c=total-change(def) · a=added · m=modified · d=deleted · n=name · reverse asc↔desc
        layout: width ≥ threshold ⇒ [tree | stats]; else [tree]
        teardown (KD7): defer idempotent Fini() on quit/panic/Ctrl-C/init-fail
```

### Interface Design
Indicative Go shapes (final names settled in implementation-plan):
```go
// data layer — in package dircachefilehash (pkg), NO terminal import.
// Exported so the separate render package can consume it.

type Category int
const ( Unchanged Category = iota; Added; Modified; Deleted )

type Stats struct {
    Files                               int   // file count in subtree
    Bytes                               int64 // aggregated current size
    Added, Modified, Deleted, Unchanged int   // per-category file counts
}

type Node struct {
    Label    string   // sanitised display base name
    IsDir    bool
    Cat      Category // for files; dirs summarise via Stats
    Stats    Stats    // aggregated over subtree
    Children []*Node  // dirs only, sorted
}

type Tree struct{ Root *Node }

// labels only; bytes (optional) feed a summary header, not node sizes
type ChangeSet struct {
    Added, Modified, Deleted []string
}

// same package ⇒ may name the unexported *skiplistWrapper directly
func BuildTree(merged *skiplistWrapper, cs ChangeSet) *Tree
func sanitiseLabel(s string) string

// render layer — separate package, imports only the exported Tree/Node
package tui
type Options struct{ MinWidthForStats int; Title string }
func Run(t *Tree, o Options) error   // nil on quit; sanitised err on init failure (FR9)
```
```go
// pkg core enrichment (KD3) — additive, default-off; populated only when
// a change-set collector is supplied through runUpdate → Apply.
type UpdateResult struct {
    FileCount    int
    TotalSize    int64
    PathsUpdated []string
    Added, Modified, Deleted []string // new: change-set labels, optional
}
```

## Constraints
- No second **filesystem** walk; reloading index files (mmap) is permitted (KD2).
- Non-interactive path byte-for-byte unchanged; all new behaviour gated behind flag + TTY + non-JSON.
- Read-only: render layer never writes index/fs/config.
- Follow cobra+pflag per-command flag registration (`registerHelpFlags`/`Flags()` in `status.go`/`update.go`); no `options.go`.
- British spelling; reuse `FormatHumanSize` for all size strings.

## Decomposition Check
- [ ] **Time**: >1 week? No (~3-5 days).
- [ ] **People**: >2 people? No.
- [x] **Complexity**: 3+ concerns? Yes — data layer, render layer, update-result enrichment.
- [ ] **Risk**: needs isolation? Contained: render risk in `tui`, core change is one localised enrichment.
- [x] **Independence**: separable? Yes — `treeview` (pure) and `tui` (render) and the `pkg` enrichment can land independently behind the flag.

**Assessment**: Same 2 signals (Complexity, Independence). The clean data/render seam (KD1) plus the contained core enrichment (KD3) make subtasks unnecessary; the implementation-plan will sequence them so the pure data layer lands and is tested before the renderer. Revisit only if `tui` grows beyond a simple two-pane widget.

## Validation
- [x] Design review completed (Step 8 map/reduce reviewers); blocking package-boundary finding resolved (KD1: data layer in `pkg`, render package consumes exported `Tree`); deleted-size single-source fix applied (KD2); `LoadMergedMainCacheIndex` reuse adopted; KD3 cross-layer reach made explicit; sanitiser hardened + KD7 teardown added.
- [x] Integration points verified against real code (Repo facade, `skiplistWrapper.ForEach(func(*binaryEntry, string) bool)`, `LoadMergedMainCacheIndex`, pipeline op classification, `root.go` SIGINT wiring, go.mod deps) via codebase map.
- [x] KD4 (TUI library = tcell) confirmed by user at the agreed review gate (user selected tcell via the design-review question; recorded at the start of f-implementation-exec).

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-plan 11
**Blockers**: None — KD4 flagged for user confirmation at the agreed review gate.

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All key decisions held in implementation: KD1 two-layer seam (data in `pkg`, render in `tui` consuming exported `Tree`), KD2 single-source merged-index reload, KD3 contained update enrichment, KD4 tcell (user-confirmed), KD5 x/term guard, KD6 allowlist sanitiser, KD7 idempotent teardown, KD8 render-owned sort. No design rework needed during exec.

## Lessons Learned
KD4 should have recorded tcell's **toolchain floor** (Go ≥ 1.25) alongside its dependency weight — the floor bump surfaced only at `go get` time. Otherwise the design's correctness-first priority (sanitiser + teardown isolated and provable) matched exactly where the real risk lay.
