# interactive-tree gdu-style post-run view - Testing Plan
**Task**: 11 (feature)

## Task Reference
- **Task ID**: internal-11
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/11-interactive-tree-gdu-style-post-run-view
- **Template Version**: 2.1

## Goal
Define the test strategy that validates the `--interactive-tree` viewer against requirements AC1a–AC8 and the design's correctness-critical points (deleted-entry union, collector concurrency, escape-safety, terminal teardown), while proving the non-interactive path is unchanged.

## Test Strategy
### Test Levels
- **Unit (Go, `pkg/treeview_test.go`)**: the pure `buildTreeFromEntries` aggregation/category/union logic and `sanitiseLabel` — plain `treeEntry` literals, no terminal, no skiplist fixture. This is the bulk of correctness coverage.
- **Unit/Integration (`pkg`)**: `BuildTree` adapter over a populated index; update-enrichment collector against a built fixture index; `PostRunTree` end-to-end on a temp repo.
- **Render (`cmd/dcfh/internal/tui`)**: drive the event loop with tcell's `SimulationScreen` (no real TTY) — layout, width-gating, key handling, teardown idempotency.
- **CLI/acceptance (existing `cmd/dcfh` test style + manual)**: flag accept/reject, help text, non-TTY/JSON skip, and a scripted manual checklist for real-terminal behaviour that the simulation can't fully cover.
- **Regression**: full `go test ./...`, `go vet`, `golangci-lint run ./...` (gosec gate), `-race` on the pipeline tests; byte-identical non-interactive output and on-disk index.

### Test Coverage Targets
- **Critical paths (100%)**: `buildTreeFromEntries` category assignment incl. deleted-union; `sanitiseLabel`; the collector's canonical-pass-only attachment; the TTY/JSON guard decision.
- **Edge cases**: empty/no-change tree, single root entry, deep/wide nesting, deleted-only change set, crafted-filename bytes, narrow terminal, resize, init failure.
- **Regression**: all existing `pkg` and `cmd` tests stay green; non-interactive `status`/`update` output and index bytes unchanged.
- No numeric line-coverage gate is imposed (consistent with the repo); coverage is defined by the case list below.

## Test Cases
### Functional Test Cases (→ acceptance criteria)

- **TC-1 (AC5) — tree aggregation sums up**: 
  - *Given* `treeEntry` literals across nested dirs with known sizes/categories.
  - *When* `buildTreeFromEntries` runs.
  - *Then* each directory `Stats` equals the sum of its children (files, bytes, per-category counts); root totals equal the whole fixture.

- **TC-2 (AC5 / FR7) — category assignment**: 
  - *Given* a merged set plus a `ChangeSet{Added, Modified, Deleted}`.
  - *When* built.
  - *Then* live entries are labelled Added/Modified by membership and Unchanged otherwise; counts per category are exact.

- **TC-3 (correctness: deleted-union, update-full) — deleted absent from merged set**: 
  - *Given* entries with **no** deleted-flagged members but `ChangeSet.Deleted = {a/b/x}`.
  - *When* built.
  - *Then* a synthesised `Deleted` node `a/b/x` exists (count-only, size 0) and propagates into ancestor `Stats.Deleted`. (Guards the `updateFullRepository` cache-removal case.)

- **TC-4 (correctness: deleted via flag, status) — deleted present in merged set**: 
  - *Given* a merged entry with `IsDeleted()` true (status/cache case) **and** the same path in `ChangeSet.Deleted`.
  - *When* built.
  - *Then* exactly one `Deleted` node (no duplicate from the union step).

- **TC-5 (FR8) — empty / no-change**: 
  - *Given* empty entries and empty `ChangeSet`.
  - *When* built.
  - *Then* a valid tree with a root and zeroed `Stats` (no nil-deref); renderer shows an explicit no-change state.

- **TC-6 (AC7) — escape-safety, allowlist not blocklist**: 
  - *Given* labels containing `\x1b[2J`, an OSC `\x1b]0;…\x07`, DEL `0x7f`, a lone C1 `0x9b`, raw `\r`/`\b`, and an invalid-UTF-8 byte.
  - *When* `sanitiseLabel` runs.
  - *Then* output contains only printable runes (escaped form), no raw control/escape byte survives — including the bytes **outside** the enumerated CSI set (so a regression to a literal blocklist fails).

- **TC-7 (AC1a) — flag parses + help**: 
  - *Given* the built CLI.
  - *When* `dcfh status --interactive-tree --help` / `dcfh update … --help`.
  - *Then* both parse and list `--interactive-tree` in help.

- **TC-8 (AC1b) — rejected elsewhere**: 
  - *When* `dcfh dupes --interactive-tree`.
  - *Then* an "unknown flag" error (confirms per-command, not persistent, registration).

- **TC-9 (AC1c / FR3) — inert on non-TTY / JSON**: 
  - *When* `dcfh status --interactive-tree | cat` and `dcfh status --interactive-tree --json`.
  - *Then* exit 0, normal/JSON output, no TUI, no hang (guard short-circuits before `tui.Run`).

- **TC-10 (AC2 / FR2) — launch after committed run**: 
  - *Given* a repo with pending changes and a stubbed/simulated viewer.
  - *When* `update --interactive-tree`.
  - *Then* the index is fully committed (post-rename state) before the viewer is invoked; the normal summary is still printed.

- **TC-11 (KD3 enrichment) — update change-sets correct**: 
  - *Given* a fixture with N added, M modified, K deleted and `ApplyRequest.CollectChanges=true`.
  - *When* `Apply` runs.
  - *Then* `UpdateResult.Added/Modified/Deleted` match exactly, including the full-update delete case.

- **TC-12 (AC3 / FR4) — no second filesystem walk**: 
  - *When* the viewer is built post-run.
  - *Then* tree data derives from the merged index reload only; assert no additional filesystem walk/stat beyond the command's own run (e.g. via a walk-counter seam or by asserting `PostRunTree` performs no scan).

- **TC-13 (AC4 / FR6) — width gating + resize** (SimulationScreen): 
  - *Given* a `Tree` and a simulated screen.
  - *When* width ≥ threshold vs < threshold, and a resize event crosses the threshold.
  - *Then* two panes vs tree-only render respectively; resize re-flows without panic or garbled cells.

- **TC-14 (AC4 / FR5) — navigation** (SimulationScreen): 
  - *When* ↑/↓, →/Enter (expand), ←/h (parent/collapse), q are injected.
  - *Then* selection/expansion state updates as specified; `q` returns nil.

- **TC-13b (AC9 / FR10) — sort comparators** (`tui/sort.go` unit, no screen): 
  - *Given* a sibling set with known per-category counts.
  - *When* each comparator is applied (total-change default, added, modified, deleted, name) in asc and desc.
  - *Then* ordering matches the key; total-change = add+mod+del; ties broken by name-asc; direction flips order; name-asc is stable/deterministic.

- **TC-13c (AC9 / FR10) — live re-sort, no re-read** (SimulationScreen): 
  - *Given* a rendered `Tree`.
  - *When* the sort/reverse keys are injected.
  - *Then* the visible child order changes accordingly, selection is preserved where possible, and no `pkg`/index/filesystem re-read occurs (data layer untouched — assert via the same no-walk seam as TC-12).

### Non-Functional Test Cases
- **TC-15 (AC6 / NFR5) — teardown on quit/Ctrl-C/panic** (SimulationScreen + unit): `Fini()` runs exactly once on each exit path; a forced panic in the loop still restores the screen; Ctrl-C maps to quit via tcell `EventInterrupt`.
- **TC-16 (AC8 / FR9) — init-failure non-fatal**: with a failing screen factory (or `TERM=`), `Run` returns a sanitised error, the command's normal summary was already printed, exit code reflects the completed work, and `Fini()` after a failed `Init()` is safe (idempotent).
- **TC-17 (security/integrity) — non-interactive regression**: with `CollectChanges=false`, `update` stdout, behaviour, **and resulting on-disk index bytes** are byte-identical to a pre-change capture (collector must not perturb serialisation/rename).
- **TC-18 (-race) — collector concurrency**: run the update-enrichment pipeline test under `go test -race`; no data race (collector written only by the comparison goroutine, read after the pipeline returns). Note the repo's race gate runs with `-d=checkptr=0`.
- **TC-19 (usability) — sizes & spelling**: stats pane uses `FormatHumanSize` output and British spelling in labels.

## Test Environment
### Setup Requirements
- Go toolchain per `go.mod` (1.24.x); `tcell/v2` + `golang.org/x/term` added.
- Temp-repo fixtures created with existing helpers (`createBESkiplist`/`TestEntryData`, temp `.dcfh` init) — **test databases/indices only, never a real user index** (per project rule).
- `tcell.SimulationScreen` for render-layer tests (no real TTY required in CI).
### Automation
- Standard `go test ./...`; render and enrichment tests run in CI like any other.
- Race: `go test -race` (pipeline/enrichment packages), matching the existing pre-commit `-race -d=checkptr=0` gate.
- Lint/security: `golangci-lint run ./...` (gosec) on the changeset.
- **Manual checklist (real terminal, recorded in g-testing-exec)**: wide→two panes; narrow→tree only; interactive resize; `q` and Ctrl-C leave the shell usable; piped/`--json` skip; a crafted-filename file renders safely.

## Validation Criteria
- [ ] TC-1…TC-19 (incl. TC-13b/c sort) pass (unit + simulation + CLI + manual checklist).
- [ ] `go build ./...`, `go vet ./...`, `golangci-lint run ./...` green; gosec gate clean.
- [ ] `go test ./...` and `-race` pipeline tests green.
- [ ] Non-interactive `status`/`update` output and on-disk index bytes unchanged (TC-17).
- [ ] Manual real-terminal checklist completed in g-testing-exec.

## Decomposition Check
- [ ] Time / People / Risk: unchanged. The test plan rides the same data/render/enrichment seam; no split.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-exec 11
**Blockers**: None. (KD4 tcell choice still flagged for user confirmation at the pre-exec review gate.)

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All planned cases TC-1…TC-19 (incl. TC-13b/c) executed and PASS (see g-testing-exec.md). The plan's tcell `SimulationScreen` strategy worked for render logic; a `pty.fork()` harness (added during exec, beyond the plan) covered the real `tcell.NewScreen()` path and proved escape-safety end-to-end against a crafted filename.

## Lessons Learned
The plan correctly identified the literal-driven `buildTreeFromEntries` tests as the bulk of coverage and reserved the simulation screen for render. The one gap worth noting for future TUI plans: schedule a real-`pty` smoke explicitly (the manual checklist understated how much it adds — it caught nothing broken here but is the only thing that exercises real Init/Fini + escape rendering).
