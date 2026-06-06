# Byte-weighted default sort for interactive-tree - Design
**Task**: 12 (feature)

## Task Reference
- **Task ID**: internal-12
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/12-byte-weighted-default-sort-for-interactive-tree
- **Template Version**: 2.1

## Goal
Design the data-model and render-layer changes for a `change_bytes`
(added+modified+deleted bytes) default sort, the `change`→`change_files`
rename, and the dual-source deleted-byte attribution that keeps the two
launch paths consistent without a second filesystem walk.

## Design Priorities
Testability → Readability → Consistency → Simplicity → Reversibility

## Key Decisions

### KD1 — `change_bytes` = Added+Modified+Deleted **bytes** (confirmed scope)
- **Decision**: the new metric sums per-category *bytes*; Added/Modified
  bytes are the file's current size, Deleted bytes are the last-known size.
- **Rationale**: a large deletion is a large change; ranking it as zero
  (the added+modified-only alternative) surprised the user. User confirmed
  this scope at the requirements review.
- **Trade-offs**: requires per-category byte fields on `Stats` and a
  deleted-size channel for the update path (KD3); more code than the
  builder-only alternative, but the only option that ranks deletions.
- **Three byte fields vs the sum**: the sort metric needs only
  `AddedBytes+ModifiedBytes+DeletedBytes`. The three are kept *separate*
  solely because KD6's stats pane shows each on its own line; if KD6 were
  dropped, two fields (live-changed + deleted) would suffice.
- **Modified-byte semantics (both paths use *current* size)**: a modified
  file's `ModifiedBytes` is its post-change size. status: the cache-delta
  refresh updates the entry's `FileSize`/hash before `PostRunTree` loads
  the merge, so the merged entry carries the new size. update: the
  post-rename merged index carries the new size. Both paths therefore
  agree; the FR3 fixture asserts modified-byte identity, not only deleted.

### KD2 — Dual-source deleted bytes, one rule ("last-known size")
- **Decision**: the builder obtains a deleted file's bytes from whichever
  source holds it, and both resolve to the same value:
  - **status**: the deleted entry survives in the merged index as a
    tombstone (the cache refresh runs `scanWriteDelta`, whose `OnLeftOnly`
    keeps the left entry with its retained `FileSize` — confirmed in
    `pkg/pipeline_status.go:45` + `comparison_sink.go:93-107`). The builder
    reads `treeEntry.Size` for the deleted leaf. **No new status plumbing.**
  - **update (full)**: the entry is gone after the atomic rename, so the
    deleted size travels on `ChangeSet.DeletedSizes` (KD3) and the union
    step uses it for the synthesised node.
- **Precedence**: when a deleted path is *both* an in-index tombstone and
  present in `cs.DeletedSizes` (e.g. a path-scoped update), the in-index
  tombstone size wins — the union step only fires for paths absent from
  the merged entries (the existing `seenDeleted` guard, treeview.go:176).
  KD2's "both resolve to last-known size" makes this benign by design.
- **Rationale**: reuses data each path already has; avoids adding per-path
  deleted sizes to `StatusResult` (which only has the aggregate today).
- **Trade-offs**: two code paths into one `Stats.DeletedBytes` total;
  mitigated by a single fixture (FR3 AC) asserting both yield the
  identical value.

### KD3 — Update path captures left size in the comparison goroutine
- **Decision**: extend the existing `changeCollector` to record a
  `deletedSizes map[string]int64`. Keep the **single write site**: widen
  the existing `add` to `add(op PipelineOp, path string, size int64)` (size
  used only for the `OpDeleted` branch) rather than adding a second method,
  so the race argument ("one goroutine, one writer") is unchanged.
  `scanWriteSink.record` obtains the size via `entry.FileSize()` for the
  `OpDeleted` case (the `left` entry, before the rename discards it).
  Thread it `collector → UpdateResult.DeletedSizes → ChangeSet.DeletedSizes`.
- **FileSize() error policy**: `FileSize()` returns `(int64, error)`; on
  error drop to size 0 (never abort) — identical to `record`'s existing
  `RelativePath()`-error policy (the viewer pane is cosmetic).
- **Degradation**: if the pipeline aborts mid-run the viewer is **not**
  launched (update's RunE returns the error before `launchInteractiveTree`).
  Even so, the union step tolerates a `Deleted` path missing from
  `DeletedSizes` via nil-safe map lookup → size 0 (silent under-count, no
  crash); `Deleted` (membership) and `DeletedSizes` (size) need not be
  perfectly key-aligned.
- **Rationale**: the comparison goroutine is the last place the pre-update
  (left) entry is in hand; capturing there needs no extra walk (FR9).
- **Trade-offs**: widens the collector/UpdateResult/ChangeSet surface, but
  only for deletions (added/modified sizes come free from the live merged
  index). Written by the single comparison goroutine, read after
  `RunUpdatePipeline` returns — same ownership model as task 11's path
  collector, so no new race and the byte-identity guarantee (TC-17) holds
  (the collector still never touches serialisation).

### KD4 — `metric()` widens to `int64`
- **Decision**: `metric(*Node, sortKey) int64`. Count keys return
  `int64(count)`; `change_bytes` returns the byte sum.
- **Rationale**: byte subtrees exceed `int31` (the user's tree is 198 GB);
  an `int` metric would truncate/overflow on 32-bit `int` platforms.
- **Trade-offs**: a one-type change rippling through `nodeLess`; no gosec
  G115 conversion expected (the byte fields are already `int64`), but a
  per-line rationale will be added if one surfaces.

### KD5 — a/m/d stay **counts**; only `change` splits
- **Decision**: `added`/`modified`/`deleted` keys remain count metrics;
  only the aggregate `change` splits into `change_bytes` (default) and
  `change_files`.
- **Rationale**: matches the user's explicit request; avoids a key
  explosion (no per-category byte keys). The header label always names the
  active metric so the count-vs-byte distinction is visible.

### KD6 — Stats pane shows the byte breakdown (usability)
- **Decision**: the right-hand stats pane annotates each change line with
  its bytes, e.g. `Added: 12 (3.4 MB)`, plus `Deleted: 2 (800 MB)`.
- **Rationale**: directly answers the question that triggered this task —
  "why did this sort here?" — by showing the bytes behind the order. Pure
  render-layer, uses data KD1 already computes.
- **Trade-offs**: minor extra render code; gated by the existing
  `MinWidthForStats` pane so narrow terminals are unaffected.

## System Design
### Component Overview
- **`pkg/treeview.go` (data layer)**:
  - add `AddedBytes/ModifiedBytes/DeletedBytes int64` to `Stats`;
  - **rewrite the `Stats` doc-comment** (treeview.go:27-43) — it currently
    states "deleted/old sizes are not retained"; that becomes "live
    `Bytes`/`Files` still exclude deleted; `DeletedBytes` separately
    retains the last-known deleted size for the change_bytes sort";
  - **`leafStats` Deleted branch** changes from `Stats{Deleted: 1}` to
    `Stats{Deleted: 1, DeletedBytes: size}` (the load-bearing edit for the
    status path) — Added/Modified set their `*Bytes` field too; Deleted
    still stays out of `Files`/`Bytes`;
  - `aggregate` sums the three new fields;
  - `ChangeSet` gains `DeletedSizes map[string]int64`; the union step
    changes `insert(p, 0, Deleted)` → `insert(p, cs.DeletedSizes[p],
    Deleted)` (indexing a nil map yields 0 in Go — no guard needed).
- **`pkg/comparison_sink.go` + `pkg/update.go` (update capture)**:
  `changeCollector` gains `deletedSizes map[string]int64`; widen `add` to
  `add(op, path, size)` (single write site); `scanWriteSink.record`
  captures `FileSize()` (error → 0) on `OpDeleted`.
- **`pkg/repo.go` + `pkg/repo_local.go` (result wiring)**: `UpdateResult`
  gains `DeletedSizes map[string]int64`; `Apply` copies it from the
  collector.
- **`cmd/dcfh/update.go` (call site)**: set `ChangeSet.DeletedSizes =
  result.DeletedSizes`. (`cmd/dcfh/status.go` unchanged — KD2.)
- **`cmd/dcfh/internal/tui/sort.go` (sort layer)**: rename `sortChange`→
  `sortChangeFiles`; add `sortChangeBytes` (iota 0 = default/zero value);
  `metric`→`int64` with the byte branch; `label()` strings; `keyForRune`
  `'c'`→bytes, `'f'`→files, default→bytes.
- **`cmd/dcfh/internal/tui/render.go` (view)**: `newModel` default
  `sortChangeBytes`; footer legend adds `f`; stats pane byte annotations
  (KD6). Header already prints `m.sortKey.label()` + direction — no change
  beyond the new labels.

### Data Flow
1. **status**: `Diff` → `StatusResult` (paths) → `ChangeSet{Added,
   Modified, Deleted}` (DeletedBytes nil). `PostRunTree` →
   `LoadMergedMainCacheIndex` (incl. deleted tombstones w/ size) →
   `BuildTree`: deleted leaves get size from the index entry.
2. **update**: comparison goroutine → `changeCollector` (paths +
   `deletedSizes[path]=leftSize`) → `UpdateResult.{Added,Modified,Deleted,
   DeletedSizes}` → `ChangeSet` (DeletedSizes populated). `PostRunTree` →
   merged index (no deleted entries) → `BuildTree`: union synthesises
   deleted nodes using `cs.DeletedSizes`.
3. **viewer**: `metric(node, sortChangeBytes)` = AddedBytes+ModifiedBytes+
   DeletedBytes per subtree; `c`/`f`/`a`/`m`/`d`/`n`/`r` re-sort in place.

## Interface Design
### Data Models
```
// pkg/treeview.go
Stats {
  Files, Added, Modified, Deleted, Unchanged int
  Bytes         int64   // live (non-deleted) — unchanged semantics
  AddedBytes    int64   // new
  ModifiedBytes int64   // new (current size of modified files)
  DeletedBytes  int64   // new (last-known size; NOT in Bytes/Files)
}

ChangeSet {
  Added, Modified, Deleted []string
  DeletedSizes map[string]int64   // new; per-deleted-path last-known size
}                                 // (named *Sizes, not *Bytes, to avoid
                                  //  colliding with the aggregate Stats.DeletedBytes)

// pkg/repo.go
UpdateResult {
  ... existing ...
  DeletedSizes map[string]int64 `json:"deleted_sizes,omitempty"` // new; omitempty ⇒ absent on status path
}

// pkg/update.go
changeCollector {
  added, modified, deleted []string
  deletedSizes map[string]int64   // new
}
```
### Sort layer (cmd/dcfh/internal/tui/sort.go)
```
const ( sortChangeBytes sortKey = iota  // default (zero value)
        sortChangeFiles; sortAdded; sortModified; sortDeleted; sortName )
metric(n *dcfh.Node, key sortKey) int64       // widened
keyForRune: 'c'→bytes 'f'→files 'a'/'m'/'d'/'n'; default→bytes
label(): change_bytes | change_files | added | modified | deleted | name
```

## Constraints
- No second filesystem walk (FR9): deleted size from in-index tombstone
  (status) or in-band capture (update), never a re-`stat`.
- Byte-identity (TC-17) and `-race` cleanliness preserved: collector writes
  on the single comparison goroutine, read post-`Wait`; no serialisation
  change.
- KD2 single source of truth; no new third-party deps; British spelling.

## Decomposition Check
- [ ] **Time**: >1 week? No.
- [ ] **People**: >2? No.
- [ ] **Complexity**: 3+ concerns? Data-model + capture + sort/view, but
      one subsystem, one user-facing change.
- [ ] **Risk**: isolation? No — cross-path consistency handled by KD2 + a
      shared fixture, not isolatable code.
- [ ] **Independence**: separable? No — the metric depends on the new
      fields which depend on the capture.
**Conclusion**: No decomposition.

## Validation
- [ ] Builder unit tests: per-category byte aggregation; deleted-byte from
      index tombstone vs from `ChangeSet.DeletedSizes` give identical Stats
      (cross-path fixture asserts **both** deleted-byte and modified-byte
      identity across the status and update change-sets).
- [ ] Sort unit tests: `change_bytes` ordering (int64, >2³¹), label/key map.
- [ ] Render tests: default header `change_bytes(desc)`; `f` → `change_files`;
      pane byte annotations.
- [ ] Regression: non-interactive byte-identity (TC-17) + `-race` green.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All six KDs implemented as designed, no deviations. KD2 (dual-source, one
"last-known size" rule) proved out via a single cross-path fixture; KD3
(single-write-site widening) kept `-race` clean; KD4 (int64) avoided a new
G115 suppression; KD6 (stats-pane bytes) shipped.

## Lessons Learned
"Last-known size" as the single unifying rule let one fixture prove both
launch paths agree, rather than two divergent code paths. See
j-retrospective.md.
