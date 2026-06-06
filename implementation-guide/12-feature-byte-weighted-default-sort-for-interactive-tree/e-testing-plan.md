# Byte-weighted default sort for interactive-tree - Testing Plan
**Task**: 12 (feature)

## Task Reference
- **Task ID**: internal-12
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/12-byte-weighted-default-sort-for-interactive-tree
- **Template Version**: 2.1

## Goal
Validate the `change_bytes` default sort, the `change`→`change_files`
rename, and the dual-source deleted-byte attribution against FR1–FR9 /
AC1–AC7, while proving the non-interactive path stays byte-identical and no
second filesystem walk is introduced.

## Test Strategy
### Test Levels
- **Unit — data layer (`pkg/treeview_test.go`)**: the pure
  `buildTreeFromEntries`/`leafStats`/`aggregate` byte logic with
  `treeEntry` + `ChangeSet` literals (no terminal, no skiplist). Bulk of
  correctness coverage.
- **Unit/integration — enrichment (`pkg/treeview_enrichment_test.go`)**:
  `Apply(CollectChanges:true)` populates `UpdateResult.DeletedSizes`;
  cross-path deleted/modified-byte identity via a real temp-repo refresh;
  byte-identity regression.
- **Unit — sort (`cmd/dcfh/internal/tui/sort_test.go`)**: `metric`
  (`int64`), comparators, label/key map — built from exported `dcfh.Node`.
- **Render (`cmd/dcfh/internal/tui/render_test.go`)**: tcell
  `SimulationScreen` — default header, key toggles, pane byte annotations,
  no-re-read on re-sort.
- **Regression**: `go test ./...`, `golangci-lint run ./...` (gosec),
  `go test -race -d=checkptr=0 ./pkg/`; byte-identical non-interactive
  output + on-disk index.

### Test Coverage Targets
- **Critical paths (100%)**: `change_bytes` aggregation (added+modified+
  deleted bytes); deleted-byte dual source (index tombstone vs
  `DeletedSizes`) incl. both-present precedence; `metric` int64; the
  rename labels + key map; default-sort selection.
- **Edge cases**: empty/no-change; >2³¹-byte subtree; deleted path in both
  sources; nil `DeletedSizes` (status); modified file's current size.
- No numeric line-coverage gate (consistent with the repo); coverage is the
  case list below.

## Test Cases
### Functional (→ acceptance criteria)

- **TC-1 (FR1/FR2 — AC3) — per-category byte aggregation**:
  *Given* nested `treeEntry` literals with known sizes/categories plus a
  `ChangeSet`. *When* `buildTreeFromEntries` runs. *Then* each directory's
  `AddedBytes/ModifiedBytes/DeletedBytes` equals the sum of its children;
  live `Bytes`/`Files` still exclude deleted (invariant preserved).

- **TC-2 (FR3 — AC3) — deleted-byte dual source identical**:
  *Given* (a) a deleted entry present in the merged set with `Size=X`
  (status/tombstone case) and (b) the same path supplied only via
  `ChangeSet.DeletedSizes[path]=X` (update case). *When* built. *Then* both
  yield the same `Stats.DeletedBytes=X` and the same `change_bytes` order.

- **TC-3 (FR3/KD2 — AC3) — both-present precedence**:
  *Given* a path that is both an in-index deleted tombstone (`Size=X`) and
  present in `DeletedSizes[path]=Y` (Y≠X). *When* built. *Then* exactly one
  deleted node, size `X` (tombstone wins via `seenDeleted`); no double-count.

- **TC-4 (FR1/KD1 — AC3) — modified-byte = current size, cross-path**:
  *Given* a real temp repo: modify a file, then run the status refresh
  path and the update path. *When* `PostRunTree`/`Apply` build the tree.
  *Then* the modified file's `ModifiedBytes` equals its **post-change**
  size on both paths (guards the cache-refresh `FileSize` timing).

- **TC-5 (FR4 — AC3) — `Apply` populates `DeletedSizes`**:
  *Given* a fixture deleting a known-size file with
  `ApplyRequest.CollectChanges=true`. *When* `Apply` runs. *Then*
  `UpdateResult.DeletedSizes[path]` equals the last-known size; `Added`/
  `Modified`/`Deleted` path-sets unchanged from task 11 behaviour.

- **TC-6 (FR1/NFR5 — AC6) — int64, no overflow**:
  *Given* a subtree whose changed bytes sum > 2³¹. *When*
  `metric(node, sortChangeBytes)` runs. *Then* it returns the correct
  `int64` and orders above a smaller sibling (no truncation/overflow).

- **TC-7 (FR5/FR2 — AC1) — default is change_bytes(desc)**:
  *Given* a built tree + SimulationScreen. *When* the viewer opens with no
  key press. *Then* header reads `sort:change_bytes(desc)` and siblings are
  ordered by descending changed bytes.

- **TC-8 (FR6 — AC2) — rename labels**:
  *When* `label()` is queried / header+footer rendered. *Then*
  `sortChangeBytes.label()=="change_bytes"`,
  `sortChangeFiles.label()=="change_files"`; no rendered label/header/footer
  contains the bare metric name `change`. (Assertion over `label()` output
  and rendered text, not raw source grep.)

- **TC-9 (FR7 — AC4) — key map + live re-sort**:
  *Given* a rendered tree. *When* `c`/`f`/`a`/`m`/`d`/`n`/`r` are injected.
  *Then* `keyForRune('c')==sortChangeBytes`, `('f')==sortChangeFiles`;
  visible order changes per key; selection preserved where possible; **no
  `pkg`/index/filesystem re-read** (same no-walk seam as task 11 TC-12).

- **TC-10 (FR8 — AC2) — direction toggle**:
  *When* `r` is injected. *Then* header direction flips `desc`↔`asc` for the
  active metric; ordering reverses.

- **TC-11 (KD6 — AC1 usability) — stats-pane byte annotations**:
  *Given* a wide SimulationScreen and a selected node. *When* drawn.
  *Then* the pane shows per-category bytes, e.g. `Added: N (… )`,
  `Modified: …`, `Deleted: …` via `FormatHumanSize`; narrow screen (< pane
  threshold) omits the pane without panic.

- **TC-12 (FR9/NFR1 — AC5) — no second walk**:
  *When* the viewer is built post-run and re-sorted. *Then* tree data
  derives from the merged-index reload only; no extra filesystem walk/stat
  (assert via the task-11 no-walk seam).

### Non-Functional

- **TC-13 (NFR5 — AC6) — byte-identity regression**: with
  `CollectChanges=false`, `update` stdout, behaviour, and on-disk index
  bytes are byte-identical to a pre-change capture (the new size-capture
  must not perturb serialisation/rename). Reuses task 11
  `TestApply_CollectChangesByteIdentical`.
- **TC-14 (NFR5 — AC6) — `-race`**: run the enrichment/pipeline tests under
  `go test -race -d=checkptr=0 ./pkg/`; no data race (`deletedSizes`
  written only by the comparison goroutine, read after the pipeline
  returns).
- **TC-15 (NFR4 — AC7) — gosec/lint**: `golangci-lint run ./...` clean; no
  new G115 suppression (byte widths stay `int64` end-to-end).
- **TC-16 (NFR2) — sizes & spelling**: pane/headers use `FormatHumanSize`
  and British spelling.

## Test Environment
### Setup Requirements
- Go toolchain per `go.mod` (≥1.25.0); `tcell/v2` + `golang.org/x/term`
  already present from task 11.
- Temp-repo fixtures via existing helpers — **test indices only, never a
  real user index** (project rule).
- `tcell.SimulationScreen` for render tests (no real TTY in CI).
### Automation
- `go test ./...`; render/enrichment tests run in CI like any other.
- `go test -race -d=checkptr=0 ./pkg/`; `golangci-lint run ./...` (gosec).
- **Manual checklist (real terminal, recorded in g-)**: `dcfh status
  --interactive-tree` opens at `change_bytes(desc)`, biggest-by-bytes
  first; `c`/`f` toggle bytes/files and the header updates; pane shows byte
  breakdown; piped/`--json` still skips the viewer.

## Validation Criteria
- [ ] TC-1…TC-16 pass (unit + simulation + enrichment + manual checklist).
- [ ] `go build ./...`, `go vet ./...`, `golangci-lint run ./...` green; gosec clean.
- [ ] `go test ./...` and `-race ./pkg/` green.
- [ ] Non-interactive `status`/`update` output and on-disk index bytes unchanged (TC-13).
- [ ] Deleted- and modified-byte identity across status/update (TC-2/TC-4).
- [ ] Manual real-terminal checklist completed in g-testing-exec.

## Decomposition Check
- [ ] Time / People / Risk: unchanged. Rides task 11's data/render/
      enrichment seams; no split.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-exec 12
**Blockers**: None.

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
TC-1..TC-16 all PASS (see g-testing-exec.md). The cross-path fixture
(TC-4) ran the real status-refresh and update paths against a temp repo,
catching any cache-refresh FileSize-timing regression as intended.

## Lessons Learned
Exercising the modified-byte "current size" assumption through a real
refresh path (not a literal builder) is what makes TC-4 a meaningful
guard. See j-retrospective.md.
