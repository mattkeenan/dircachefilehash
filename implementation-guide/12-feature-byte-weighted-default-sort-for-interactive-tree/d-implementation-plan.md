# Byte-weighted default sort for interactive-tree - Implementation Plan
**Task**: 12 (feature)

## Task Reference
- **Task ID**: internal-12
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/12-byte-weighted-default-sort-for-interactive-tree
- **Template Version**: 2.1

## Goal
Implement the `change_bytes` default sort, the `change`→`change_files`
rename, and the dual-source deleted-byte plumbing, per c-design-plan.md.

## Workflow
Patterns first → Test → Minimal impl → Refactor green → Commit explains "why".
Order = correctness/dependency order: data layer → update capture → result
wiring → sort layer → view. Each step compiles and tests green before the next.

## Files to Modify
### Primary Changes
- `pkg/treeview.go` — `Stats` byte fields + doc rewrite; `leafStats`
  per-category bytes (incl. Deleted-retains-size); `aggregate` sums them;
  `ChangeSet.DeletedSizes`; union step uses it.
- `cmd/dcfh/internal/tui/sort.go` — rename `sortChange`→`sortChangeFiles`;
  add `sortChangeBytes` (iota 0); `metric`→`int64` with byte branch;
  `label()`; `keyForRune` `c`/`f` + default.
- `cmd/dcfh/internal/tui/render.go` — `newModel` default `sortChangeBytes`;
  footer legend `f`; stats-pane byte annotations (KD6).
- `pkg/update.go` — `changeCollector.deletedSizes`; widen `add(op,path,size)`.
- `pkg/comparison_sink.go` — `record` captures `FileSize()` (err→0) on `OpDeleted`.
- `pkg/repo.go` — `UpdateResult.DeletedSizes map[string]int64`.
- `pkg/repo_local.go` — `Apply` copies `collector.deletedSizes`.
- `cmd/dcfh/update.go` — set `ChangeSet.DeletedSizes = result.DeletedSizes`.

### Supporting Changes (tests — detailed in e-)
- `pkg/treeview_test.go`, `pkg/treeview_enrichment_test.go`,
  `cmd/dcfh/internal/tui/sort_test.go`, `render_test.go`.

## Implementation Steps

### Step 1 — Data layer: per-category bytes (`pkg/treeview.go`)
- [ ] Add `AddedBytes`, `ModifiedBytes`, `DeletedBytes int64` to `Stats`.
      (Keep all THREE separate — not just their sum — because KD6's pane
      prints each on its own line; do not "simplify" to one field.)
- [ ] Rewrite the `Stats` doc-comment: live `Bytes`/`Files` still exclude
      deleted; `DeletedBytes` separately retains last-known deleted size.
- [ ] `leafStats`: Added→`AddedBytes:size`, Modified→`ModifiedBytes:size`,
      Deleted→`Stats{Deleted:1, DeletedBytes:size}` (still NOT in
      `Files`/`Bytes`), Unchanged unchanged.
- [ ] `aggregate`: sum the three new fields alongside existing.
- [ ] Add `DeletedSizes map[string]int64` to `ChangeSet` (+ doc noting the
      `*Sizes` vs aggregate `*Bytes` distinction).
- [ ] Union step: `insert(p, cs.DeletedSizes[p], Deleted)` (nil map → 0;
      no guard). status path still feeds deleted size via the in-index
      tombstone entry through `BuildTree`/`leafStats`.
- [ ] Unit tests: aggregation of new fields; deleted-via-index-tombstone
      vs deleted-via-`DeletedSizes` give identical `Stats`; modified-byte
      identity; **both-present precedence** (a path in BOTH the tombstone
      entries and `DeletedSizes` → tombstone size wins via `seenDeleted`,
      no double-count); empty/no-change still zeroed. Build + `go test ./pkg/`.

### Step 2 — Update capture (`pkg/update.go`, `pkg/comparison_sink.go`)
- [ ] `changeCollector`: add `deletedSizes map[string]int64`; widen
      `add(op PipelineOp, path string, size int64)` — initialise the map
      lazily, store only for `OpDeleted`; existing add(...) call sites pass
      the size (0 for non-deleted).
- [ ] `scanWriteSink.record(op, entry)`: for `OpDeleted`, read
      `size, err := entry.FileSize()` (err→0), pass to `add`. Other ops
      pass size 0. Keep the single write site.
- [ ] Confirm only `OnLeftOnly` (canonical policy) records `OpDeleted`;
      delta path still passes `nil` collector (no double-record).
- [ ] Build + `go test ./pkg/`.

### Step 3 — Result wiring (`pkg/repo.go`, `pkg/repo_local.go`, `cmd/dcfh/update.go`)
- [ ] `UpdateResult`: add `DeletedSizes map[string]int64
      \`json:"deleted_sizes,omitempty"\``.
- [ ] `Apply`: when `collector != nil`, set `res.DeletedSizes =
      collector.deletedSizes`.
- [ ] `cmd/dcfh/update.go`: `ChangeSet{… , DeletedSizes: result.DeletedSizes}`.
- [ ] `cmd/dcfh/status.go`: unchanged (KD2 — confirm no edit needed).
- [ ] Enrichment test: `Apply(CollectChanges:true)` populates
      `DeletedSizes` with the deleted file's size; byte-identity test
      (TC-17) still passes (collector off ⇒ identical index bytes). The
      cross-path fixture must exercise a **real modified file through the
      status refresh path** (not a literal builder) so a regression in the
      cache-refresh `FileSize` timing (the modified-byte "current size"
      assumption) is actually caught.

### Step 4 — Sort layer (`cmd/dcfh/internal/tui/sort.go`)
- [ ] `const`: `sortChangeBytes sortKey = iota` (0/default), then
      `sortChangeFiles` (was `sortChange`), `sortAdded`, `sortModified`,
      `sortDeleted`, `sortName`.
- [ ] `label()`: `change_bytes`, `change_files`, `added`, `modified`,
      `deleted`, `name`.
- [ ] `metric(n *dcfh.Node, key sortKey) int64`: `change_bytes` →
      `Added+Modified+Deleted Bytes`; `change_files` → count sum;
      a/m/d → counts (cast `int64`); name → 0.
- [ ] `nodeLess`: compare `int64`; default direction unchanged (counts/
      bytes desc, name asc); name-asc tiebreak retained.
- [ ] `keyForRune`: `c`→`sortChangeBytes`, `f`→`sortChangeFiles`,
      `a`/`m`/`d`/`n` as before; default→`sortChangeBytes`.
- [ ] Unit tests: `change_bytes` orders by byte sum incl. >2³¹ subtree;
      `c`/`f` map correctly; `label()` strings; reverse flips.

### Step 5 — View (`cmd/dcfh/internal/tui/render.go`)
- [ ] `newModel`: `sortKey: sortChangeBytes`.
- [ ] `drawFooter`: legend `c/f/a/m/d/n sort` (add `f`).
- [ ] `drawStats`: append bytes to change lines, e.g.
      `Added: 12 (3.4 MB)` / `Modified` / `Deleted` via `FormatHumanSize`.
- [ ] Render tests (SimulationScreen): default header
      `sort:change_bytes(desc)`; press `f` → `sort:change_files(desc)`;
      press `r` → `(asc)`; pane shows byte annotations; live re-sort does
      no data re-read (no-walk seam, as task 11).

### Step 6 — Whole-suite validation
- [ ] `go build ./...`; `go test ./...`; `golangci-lint run ./...` (gosec);
      `go test -race -d=checkptr=0 ./pkg/` (the collector write/read
      ordering lives in `pkg/`). Keep byte widths `int64` end-to-end in the
      sort/render path — do not narrow back to `int` (avoids a G115 suppress).
- [ ] Manual real-terminal smoke: `dcfh status --interactive-tree` shows
      `change_bytes(desc)` default; `c`/`f` toggle; biggest-by-bytes first.

## Test Coverage
**See e-testing-plan.md for the complete test plan** (maps TC cases to
FR1–FR9 / AC1–AC7; reuses task 11's SimulationScreen + byte-identity
seams).

## Validation Criteria
**See e-testing-plan.md.** Headline gates: default is `change_bytes(desc)`;
deleted-byte identity across status/update; no extra walk; byte-identical
non-interactive output + index; build/test/lint/-race green.

## Scope Completion
**IMPORTANT**: Complete all six steps before marking Finished. No deferral
planned — the rename, metric, default and plumbing are one atomic change.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All six steps executed in order with no deviation; each compiled and
tested green before the next. 8 source files + 4 test files changed
(~154 production lines). See f-implementation-exec.md.

## Lessons Learned
The correctness-ordered step list (data → capture → wiring → sort → view)
meant each layer's tests were available before the next depended on it.
See j-retrospective.md.
