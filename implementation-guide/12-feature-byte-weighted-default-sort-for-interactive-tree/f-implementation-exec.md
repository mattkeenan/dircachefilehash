# Byte-weighted default sort for interactive-tree - Implementation Execution
**Task**: 12 (feature)

## Task Reference
- **Task ID**: internal-12
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/12-byte-weighted-default-sort-for-interactive-tree
- **Template Version**: 2.1

## Goal
Execute the implementation following d-implementation-plan.md (6 ordered
steps) and e-testing-plan.md (TC-1..TC-16).

## Actual Results

### Step 1 — Data layer: per-category bytes (`pkg/treeview.go`)
- **Planned**: add `AddedBytes/ModifiedBytes/DeletedBytes int64` to
  `Stats`; rewrite doc-comment; `leafStats` per-category bytes (Deleted
  retains size); `aggregate` sums them; `ChangeSet.DeletedSizes`; union
  step uses `cs.DeletedSizes[p]`.
- **Actual**: done exactly as planned. `Stats` gained the three int64
  fields with a rewritten doc-comment clarifying live `Bytes`/`Files`
  still exclude deleted while `DeletedBytes` separately retains the
  last-known size. `leafStats` Deleted branch now returns
  `Stats{Deleted: 1, DeletedBytes: size}` (the load-bearing status-path
  edit); Added/Modified set their `*Bytes` field. `aggregate` sums all
  three. `ChangeSet` gained `DeletedSizes map[string]int64` (named
  `*Sizes` to avoid colliding with aggregate `Stats.DeletedBytes`); union
  step `insert(p, cs.DeletedSizes[p], Deleted)` (nil map → 0, no guard).
- **Deviations**: none.

### Step 2 — Update capture (`pkg/update.go`, `pkg/comparison_sink.go`)
- **Planned**: `changeCollector.deletedSizes`; widen `add(op,path,size)`,
  single write site; `record` captures `FileSize()` (err→0) on `OpDeleted`.
- **Actual**: `changeCollector` gained `deletedSizes map[string]int64`
  (lazily initialised). `add` widened to `add(op, path, size int64)` —
  size stored only in the `OpDeleted` branch (the single write site
  preserved, so the one-goroutine-one-writer race argument is unchanged).
  `scanWriteSink.record` reads `entry.FileSize()` only for `OpDeleted`,
  dropping to 0 on error (mirrors the existing `RelativePath()` policy).
- **Deviations**: none. Confirmed the only `add` caller is `record`, and
  only `OnLeftOnly` (canonical policy) routes `OpDeleted`; the delta pass
  still passes a nil collector.

### Step 3 — Result wiring (`pkg/repo.go`, `pkg/repo_local.go`, `cmd/dcfh/update.go`)
- **Planned**: `UpdateResult.DeletedSizes` (json omitempty); `Apply`
  copies from collector; `update.go` sets `ChangeSet.DeletedSizes`;
  `status.go` unchanged (KD2).
- **Actual**: `UpdateResult` gained
  `DeletedSizes map[string]int64 \`json:"deleted_sizes,omitempty"\``.
  `Apply` sets `res.DeletedSizes = collector.deletedSizes` inside the
  existing `collector != nil` block. `cmd/dcfh/update.go` threads
  `DeletedSizes: result.DeletedSizes` into the ChangeSet.
  `cmd/dcfh/status.go` left untouched — verified.
- **Deviations**: none.

### Step 4 — Sort layer (`cmd/dcfh/internal/tui/sort.go`)
- **Planned**: `sortChangeBytes` (iota 0, default); rename
  `sortChange`→`sortChangeFiles`; `metric`→`int64` with byte branch;
  `label()` strings; `keyForRune` `c`→bytes/`f`→files/default→bytes.
- **Actual**: const block leads with `sortChangeBytes` (zero value), then
  `sortChangeFiles`, then a/m/d/name. `metric` widened to `int64`;
  count keys return `int64(count)`, `sortChangeBytes` returns
  `AddedBytes+ModifiedBytes+DeletedBytes`. `nodeLess` compares int64
  unchanged in shape. `label()` returns `change_bytes`/`change_files`/….
  `keyForRune` maps `c`→bytes, `f`→files, default→bytes.
- **Deviations**: none.

### Step 5 — View (`cmd/dcfh/internal/tui/render.go`)
- **Planned**: `newModel` default `sortChangeBytes`; footer legend adds
  `f`; stats pane byte annotations.
- **Actual**: `newModel` initialises `sortKey: sortChangeBytes`. Footer
  legend now `c/f/a/m/d/n sort`. Stats pane change lines annotated with
  bytes via `FormatHumanSize`, e.g. `Added: 1 (50 B)` /`Modified:`/
  `Deleted:`. Header already prints `m.sortKey.label()` + direction — the
  new labels surface there with no further change.
- **Deviations**: none.

### Step 6 — Whole-suite validation
- `go build ./...` — clean.
- `go vet ./...` — clean.
- `go test ./...` — all packages pass.
- `golangci-lint run ./...` (gosec inside) — **0 issues**. No new G115
  suppression (byte widths stay `int64` end-to-end).
- `go test -race -gcflags=all=-d=checkptr=0 ./pkg/` — pass (collector
  written by the single comparison goroutine, read post-pipeline).
- Rename completeness (gotcha #2): source grep for `sortChange\b` (old
  identifier) → none; bare `"change"` label → only in comments/test
  strings; stale `c/a/m/d/n` legend → none. Rendered output verified via
  SimulationScreen test asserting `sort:change_bytes(desc)` default,
  `sort:change_files(desc)` after `f`, `(asc)` after `r`.

## Tests added / updated
- `pkg/treeview_test.go`: `TestBuildTree_ByteAggregation` (TC-1b),
  `TestBuildTree_DeletedBytes_DualSourceIdentical` (TC-2, >2³² size),
  `TestBuildTree_DeletedBytes_BothPresentPrecedence` (TC-3).
- `pkg/treeview_enrichment_test.go`:
  `TestApply_CollectChangesDeletedSizes` (TC-5),
  `TestApply_NoDeletedSizesByDefault`,
  `TestPostRunTree_CrossPathByteIdentity` (TC-4, real temp-repo status +
  update paths).
- `cmd/dcfh/internal/tui/sort_test.go`: renamed `sortChange` refs;
  `TestSortNodes_ChangeBytes` (TC-6/TC-7, int64 >2³¹),
  `TestSortKeyLabels` (TC-8), `f` added to `TestKeyForRune` (TC-9).
- `cmd/dcfh/internal/tui/render_test.go`: byte fields in `dir`/`treeForSim`
  helpers; `TestDefaultSortAndKeyToggles` (TC-7/TC-8),
  `TestStatsPaneByteAnnotations` (TC-11).
- Byte-identity (TC-13) `TestApply_CollectChangesByteIdentical` and the
  no-walk seam (TC-12) `TestLiveResortPreservesSelectionNoReRead` from
  task 11 still pass unchanged.

## Blockers Encountered
None.

## Deferral Check
- [x] All steps from d-implementation-plan.md executed
- [x] All success criteria from a-task-plan.md met
- [x] All requirements from b-requirements-plan.md addressed
- [x] All design guidance in c-design-plan.md followed
- [x] No planned work deferred

## Status
**Status**: Finished
**Next Action**: /cwf-testing-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Lessons Learned
Widening the single `add` write site (vs adding a method) preserved the
lock-free single-writer race argument for free. The exec-phase security
review erroring on the cap (test LOC counted as production) is a config
gap, not a code problem — filed as a follow-up. See j-retrospective.md.

## Security Review

**State**: error

error: cap exceeded: 565 production lines > 500

Helper `security-review-changeset --phase=implementation --max-lines=500`
exited 2 (production-weighted count over cap), so per the cwf-implementation-exec
contract the `cwf-security-reviewer-changeset` subagent was NOT invoked and
this is recorded as `error` (never silently downgraded to "no findings").

Context for the human reviewer: the 565 production lines are dominated by
newly-added **test** files (treeview_enrichment_test.go 140, render_test.go
102, treeview_test.go 92, sort_test.go 77 = 411), which this repo does not
list in `security.review.max-lines-exclude-paths` and so count as production.
The actual production-code change is ~154 lines across treeview.go, sort.go,
render.go, update.go, comparison_sink.go, repo.go, repo_local.go,
cmd/dcfh/update.go. The full diff was printed to stdout by the helper and
remains available for manual review. The changeset adds no new untrusted-input
path: deleted sizes are `int64` read from the index/collector (`FileSize()`,
err→0), carried through a `map[string]int64`, and rendered only via
`FormatHumanSize` (numeric) — never as a label; `sanitiseLabel` is untouched.
