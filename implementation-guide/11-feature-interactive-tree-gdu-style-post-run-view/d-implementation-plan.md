# interactive-tree gdu-style post-run view - Implementation Plan
**Task**: 11 (feature)

## Task Reference
- **Task ID**: internal-11
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/11-interactive-tree-gdu-style-post-run-view
- **Template Version**: 2.1

## Goal
Implement the `--interactive-tree` post-run viewer per the approved design: a pure tree builder + before/after stats in `package dircachefilehash`, a `tcell` render package, a per-command flag, and a contained `update` change-set enrichment.

## Workflow
Patterns first → Test → Minimal impl → Refactor green → Commit message explains "why"

## Package & Seam Layout (from design KD1)
- **Data layer** — new files in `package dircachefilehash` (`pkg/`), terminal-free, unit-tested in the existing pkg suite. Exports `Tree/Node/Stats/Category/ChangeSet`. Two-level builder for testability:
  - pure `buildTreeFromEntries(entries []treeEntry, cs ChangeSet) *Tree` where `treeEntry{Path string; Size int64; Deleted bool}` — **tested directly with plain literals**, no skiplist fixture needed (resolves the `createBESkiplist`-yields-one-entry fixture gap).
  - thin adapter `BuildTree(merged *skiplistWrapper, cs ChangeSet) *Tree` that walks `merged.ForEach(func(*binaryEntry, string) bool)` into `[]treeEntry` (raw `*binaryEntry` accessors per the `pkg/filter.go:108-118` / `pkg/dupes.go:94-138` precedent — error-free `RelativePath()`/`IsDeleted()`, `FileSize` field) then calls the pure function.
  - unexported `sanitiseLabel`.
- **Facade seam** — new method on `repoCore` (so both `localRepo` and `wireRepo` satisfy it), surfaced on the `Repo` interface, `PostRunTree(ctx, ChangeSet) (*Tree, error)`, which internally calls `LoadMergedMainCacheIndex()` + `BuildTree`. Keeps the unexported `*skiplistWrapper` entirely inside `pkg`; `package main` only ever holds a `*Tree`. **Known limitation**: `PostRunTree` reads the local `ms`; for a `wireRepo` (ssh://) that is local state, so the viewer is scoped to local-TTY use (the call-site TTY guard already enforces this).
- **Render layer** — new package `cmd/dcfh/internal/tui` (imports `dcfh "…/pkg"` for the exported `Tree`; imports `tcell`). Single entry point `Run(t *dcfh.Tree, o Options) error`. A doc comment states it must only be invoked behind the `!JSON ∧ IsTerminal` guard (the sanitiser is defence-in-depth, the guard is the primary control).

## Files to Modify
### Primary Changes (new files)
- `pkg/treeview.go` — `Tree/Node/Stats/Category/ChangeSet`, `BuildTree`, `sanitiseLabel`; path-split + up-tree `Stats` aggregation; per-file size from merged entry (KD2 single source); label by `ChangeSet` membership / `IsDeleted()`.
- `pkg/treeview_test.go` — table tests for `BuildTree` aggregation + `sanitiseLabel` (built with the existing `createBESkiplist`/`TestEntryData` fixture helpers in `pkg/binary_entry_skiplist_test.go`).
- `cmd/dcfh/internal/tui/tui.go` — `Run`, two-pane render, key/resize loop, width-gating, idempotent teardown (KD7).
- `cmd/dcfh/internal/tui/render.go` (optional split) — pane drawing + rune-aware truncation of already-sanitised labels (KD6/S2).
- `cmd/dcfh/internal/tui/sort.go` — sort comparators over `Stats` counts + active-key state (KD8/FR10); unit-tested independently of the screen.

### Supporting Changes (existing files)
- `cmd/dcfh/filters.go` — add `interactiveTree bool` to `filterFlagsState`; add `flagInteractiveTree = "interactive-tree"` const; add a `cmdFlagGroup{commands: {cmdStatus, cmdUpdate}, perSegment: false, register: …BoolVar(&state.interactiveTree, flagInteractiveTree, false, "…")}` entry to `cmdFlagRegistry`.
- `cmd/dcfh/status.go` — capture the `*filterFlagsState` from `resolveScopes` (currently `_`); after the normal run + summary, if `state.interactiveTree ∧ !JSON ∧ term.IsTerminal(stdout)`, build `ChangeSet` from `status.Modified/Added/Deleted`, call `repo.PostRunTree`, `tui.Run`.
- `cmd/dcfh/update.go` — same guard; build `ChangeSet` from the enriched `result.Added/Modified/Deleted`; pass the interactive flag down so the enrichment collector is only attached when needed.
- `pkg/repo.go` — add `PostRunTree(ctx, ChangeSet) (*Tree, error)` to the `Repo` interface; add additive `Added/Modified/Deleted []string` to `UpdateResult`.
- `pkg/repo_local.go` — implement `repoCore.PostRunTree`; in `Apply`, when `CollectChanges`, thread the collector and populate the new `UpdateResult` fields.
- `pkg/update.go` — `runUpdate` gains an optional `*changeCollector` **param** (not a `ScanRun` field — see Step 4 rationale), threaded through **both** `updateFullRepository` and `updateSpecificPaths` to `performPipelineScan`; default-nil keeps the non-interactive path unchanged.
- `pkg/pipeline_update.go` + `pkg/comparison_sink.go` — `scanWriteSink` gains an optional collector, appended **only on the canonical pass** (never the cache-refresh delta pass); emit-path appends `(op, relPath)` for `OpNewFile/OpModified/OpDeleted` when non-nil. `RunUpdatePipeline`/`performPipelineScan` signatures carry the optional collector through.
- `pkg/repo.go` (`ApplyRequest`) — add a `CollectChanges bool` (or `WantTree bool`) field so `Apply` knows whether to attach the collector.
- `go.mod` / `go.sum` — `go get github.com/gdamore/tcell/v2` and `golang.org/x/term` (version aligned with existing `golang.org/x/*`).
- `cmd/dcfh/status.go` / `update.go` help `Long` text — one line documenting `--interactive-tree`.

## Implementation Steps
### Step 1: Setup
- [ ] Re-read approved `b`/`c` plans; confirm KD2 single-source-size and KD3 cross-layer reach.
- [ ] `go get github.com/gdamore/tcell/v2` and `golang.org/x/term`; `go mod tidy`; confirm build still green.

### Step 2: Pure data layer (TDD, no terminal, no flag yet)
- [ ] Define `Category`, `Stats`, `Node`, `Tree`, `ChangeSet`, and internal `treeEntry` in `pkg/treeview.go`.
- [ ] Implement `sanitiseLabel` as a **reject-by-default printable allowlist** (render only printable runes; escape everything else `strconv.Quote`-style). It is an allowlist that *covers* C0/C1, ESC/CSI/OSC/DCS, DEL `0x7f`, `\r\b\n\t`, and invalid UTF-8 — **not** an enumerated denylist of those sequences.
- [ ] Implement pure `buildTreeFromEntries(entries, cs)`: split `Path` on `/`, create dir nodes, attach files; `Size` taken from the entry; `Category` = `Deleted` if `entry.Deleted` else membership in `cs.Modified`/`cs.Added` else `Unchanged`; **then union in any `cs.Deleted` path absent from `entries`** as a synthesised `Deleted` node (size 0 / count-only — see correctness note); roll `Stats` upward; **order children canonically (name asc)** — runtime sort is a render concern (KD8/FR10); sanitise every `Label`.
- [ ] Implement adapter `BuildTree(merged, cs)` per the seam above.
- [ ] Write `pkg/treeview_test.go` against `buildTreeFromEntries` with **plain `treeEntry` literals** (no skiplist fixture): assert child-aggregates-sum-to-parent (AC5), category assignment, **deleted-via-union when absent from entries** (the update-full case), empty tree (FR8). `sanitiseLabel` test (AC7) must include at least one byte **outside** the enumerated set — DEL `0x7f`, a lone C1 `0x9b`, and invalid UTF-8 — plus `\x1b[2J`/OSC, so a regression to a literal blocklist fails.
- [ ] `go test ./pkg/...` green.

**Correctness note — deleted entries (resolves design KD2 gap)**: `cache.idx` retains deleted entries for `status`, but `updateFullRepository` removes `cache.idx` and the canonical pass drops deletions, so after a full `update` the merged index has **no** deleted entries. Therefore deleted nodes are sourced from `ChangeSet.Deleted` (union step above), not from `IsDeleted()` alone. Per-file deleted *size* is not retained post-rename, so synthesised deleted nodes are count-only (size 0); this matches the documented "counts exact, some bytes approximate" stance. Capturing deleted size in the collector (the left entry is available at `OnLeftOnly` emit) is a cheap future enhancement, out of scope.

### Step 3: Facade seam
- [ ] Add `PostRunTree(ctx, ChangeSet) (*Tree, error)` to `Repo` (`pkg/repo.go`) and implement in `repo_local.go` (call `LoadMergedMainCacheIndex` + `BuildTree`). Unit-test against a built fixture index.

### Step 4: Update change-set enrichment (KD3 — cross-layer)
**Why not reuse `repo.Diff`?** `Diff` compares main against a fresh **fs-scan** (`right=FsScan`); running it after `Apply` would be a second filesystem walk (forbidden) *and* report empty (main now matches the FS). The pipeline's own op classification is the only no-extra-walk source — hence the collector.
- [ ] Add additive `Added/Modified/Deleted []string` to `UpdateResult` (comment: distinct from `PathsUpdated`, which is just the requested path args, not op-classified results); add `CollectChanges bool` to `ApplyRequest`.
- [ ] Thread an **explicit optional collector parameter** (not a `ScanRun` field): `scanWriteSink` → `RunUpdatePipeline` → `performPipelineScan` → **both** `updateFullRepository` *and* `updateSpecificPaths` → `runUpdate` → `Apply`. Append `(op→path)` only when the collector is non-nil.
- [ ] **Attach the collector ONLY to the canonical update sink** (`scanWriteCanonical`, `pipeline_update.go:51`) — **never** to the `refreshFsScanCache` delta pass in `updateSpecificPaths` (`pipeline_status.go:43`), or its second scan would corrupt the change-set. Explicit param threading (not a `ScanRun` field) is what guarantees this.
- [ ] **Concurrency invariant** (state in code): the collector is appended only by the single comparison goroutine (`OnMatch`/`OnLeftOnly`/`OnRightOnly`) and read only after `RunUpdatePipeline` returns (`wg.Wait()` complete) — no shared-write race under `-race`. On `RelativePath()` error mid-emit, **drop the path** (the pane is cosmetic; never abort an otherwise-successful update).
- [ ] Test: with `CollectChanges=true`, the sets match a fixture (N added/M modified/K deleted, incl. the full-update delete case). With `CollectChanges=false`: stdout/behaviour byte-identical **and** the resulting **on-disk index bytes identical** (collector nil must not perturb serialisation order or the atomic-rename result — guards the index-integrity invariant).

### Step 5: Render package (tcell)
- [ ] `cmd/dcfh/internal/tui/tui.go`: `Run(t, o)`; `screen.Init()`, `defer` idempotent `Fini()` installed before any draw (KD7/FR9); event loop: `EventResize` re-layout, key handling (↑/↓, →/Enter expand, ←/h parent, q/Ctrl-C quit), width-gated stats pane (`o.MinWidthForStats`).
- [ ] **Sort (KD8/FR10)** in `tui/sort.go`: comparators over `Stats` counts — default `Added+Modified+Deleted` desc; toggles `c`/`a`/`m`/`d`/`n` (total-change/added/modified/deleted/name) and a reverse key flipping asc↔desc; name-asc stable tiebreak. The active comparator re-sorts the displayed children **in place** (no re-read of `pkg`/index/fs); current selection is preserved across a re-sort where possible. Finalise exact keys in exec to avoid clashing with navigation.
- [ ] Rune-aware truncation of already-sanitised labels only (KD6/S2); sizes via `dcfh.FormatHumanSize`.
- [ ] Sanitise any init-failure/wrapped error text before printing to the restored terminal (S3).
- [ ] Build green; manual smoke (deferred formal cases to e-testing-plan).

### Step 6: CLI wiring + flag
- [ ] `filters.go`: add field, const, and `cmdFlagGroup` entry (scoped status+update, `perSegment:false`).
- [ ] `status.go`/`update.go`: capture `state`, add the post-run guard (`flag ∧ !JSON ∧ term.IsTerminal(int(os.Stdout.Fd()))` — note the `int()` cast; `Fd()` is `uintptr`), build `ChangeSet`, call `repo.PostRunTree`, `tui.Run`; emit a stderr notice on non-TTY/JSON skip (optional). For update, set `ApplyRequest.CollectChanges` only when the flag is on.
- [ ] Add help `Long` line on both commands.
- [ ] Verify `dcfh dupes --interactive-tree` is rejected (AC1b); `status/update --help` list the flag (AC1a).

### Step 7: Validation
- [ ] `go build ./...`, `go vet ./...`, `golangci-lint run ./...` (gosec gate) green.
- [ ] `go test ./pkg/...` green; non-interactive `status`/`update` output unchanged (diff against pre-change capture).
- [ ] Manual: wide terminal (two panes), narrow (tree only), resize across threshold, `q`/Ctrl-C restore terminal, piped/`--json` skip.

## Code Changes
### Flag registry (cmd/dcfh/filters.go) — After
```go
const flagInteractiveTree = "interactive-tree"
// + field on filterFlagsState: interactiveTree bool
// Append ONE new entry to the existing cmdFlagRegistry (currently the
// filter group + the dupes group); do not reorder the existing entries:
cmdFlagGroup{
    commands:   []string{cmdStatus, cmdUpdate},
    perSegment: false,
    register: func(fs *pflag.FlagSet, state *filterFlagsState) {
        fs.BoolVar(&state.interactiveTree, flagInteractiveTree, false,
            "after the run, open an interactive tree view of the result (TTY only)")
    },
}
```
### UpdateResult / Repo (pkg/repo.go) — After
```go
// Repo already has Close/Info/Stats/DiffRefs/Groups/Filter/Snapshots/Config
// besides Diff/Apply — add one method (implemented on repoCore so both
// localRepo and wireRepo satisfy it):
type Repo interface {
    // …existing methods…
    PostRunTree(ctx context.Context, cs ChangeSet) (*Tree, error) // new
}
type UpdateResult struct {
    FileCount    int
    TotalSize    int64
    PathsUpdated []string          // requested path args (unchanged meaning)
    Added, Modified, Deleted []string // new: op-classified results; populated only when ApplyRequest.CollectChanges
}
```

## Test Coverage
**See e-testing-plan.md for the complete test plan.** Headlines: `BuildTree` aggregation/category/empty (AC5/FR8); `sanitiseLabel` escape neutralisation (AC7); update-enrichment set correctness + non-interactive regression (AC2/KD3); flag accept/reject + help (AC1a/b); guard skips on non-TTY/JSON (AC1c). Render-layer key/resize/teardown behaviour is exercised via tcell's `SimulationScreen` where practical (AC4/AC6/AC8).

## Validation Criteria
**See e-testing-plan.md for validation criteria and test results.** Gate: build/vet/lint/gosec green; pkg tests green; non-interactive output byte-identical; manual two-pane/narrow/resize/teardown/skip checks pass.

## Scope Completion
**IMPORTANT**: Complete all planned implementation before marking task Finished. The KD3 enrichment is in-scope (not deferrable) — without it `update`'s pane has no change labels. If any item must be deferred, get user approval, update success criteria, and create a follow-up task immediately.

## Decomposition Check
- [x] **Complexity**: 3+ concerns (data layer, render package, update enrichment) — sequenced TDD-first below, no subtask split needed.
- [x] **Independence**: Steps 2-4 (pure + enrichment) land and test before Step 5-6 (render + wiring).
- [ ] Time / People / Risk: unchanged from design (no split).

**Assessment**: Same 2 signals; the step ordering (pure-and-tested core before terminal code) gives the isolation a subtask split would, without the overhead.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-plan 11
**Blockers**: None. (KD4 — the tcell dependency choice — is flagged for user confirmation at the agreed pre-exec review gate, not a blocker on completing the planning phases.)

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Implemented per plan. Deviations (all minor, recorded in f-implementation-exec.md): `runUpdate` kept stable for its 28 test call-sites via a `runUpdateCollecting` variant (its now-always-nil variadic dropped per `unparam`); added a `runScreen` test seam; exported `dcfh.SanitiseLabel` for the render-layer error path (KD6).

## Lessons Learned
Threading the optional collector as an explicit param (not a `ScanRun` field) was the right call — it made "canonical-pass-only" a compile-time guarantee and kept the cache-refresh delta pass provably uninstrumented. The pure `buildTreeFromEntries(treeEntry literals)` seam removed the need for any skiplist fixture in the bulk of the correctness tests.
