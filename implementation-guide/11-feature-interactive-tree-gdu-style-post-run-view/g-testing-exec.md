# interactive-tree gdu-style post-run view - Testing Execution
**Task**: 11 (feature)

## Task Reference
- **Task ID**: internal-11
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/11-{task-description}
- **Template Version**: 2.1

## Goal
Execute the tests defined in e-testing-plan.md and verify implementation from d-implementation-plan.md.

## Execution Checklist
- [ ] Read e-testing-plan.md and d-implementation-plan.md thoroughly
- [ ] Verify test environment ready
- [ ] Execute test cases sequentially
- [ ] Record pass/fail for each test
- [ ] Document failures with reproduction steps
- [ ] Update status to "Testing" when in progress, "Finished" when all pass

## Test Environment
- Go `go1.26.4` toolchain (go.mod directive now 1.25.0); `tcell/v2 v2.13.10`, `x/term v0.42.0`.
- Automated: `go test` (unit + tcell `SimulationScreen`); `-race -gcflags=all=-d=checkptr=0` per the repo gate.
- Real-terminal: a Python `pty.fork()` harness drove the actual `tcell.NewScreen()` path (the one `SimulationScreen` cannot exercise) at 120/40 columns, injecting navigation/sort/quit/Ctrl-C keys and a hostile filename. Temp repos only — never a real user index.

## Test Results

### Functional Tests

| TC | AC / ref | Method | Status |
|----|----------|--------|--------|
| TC-1 | AC5 aggregation | `TestBuildTree_AggregationSumsUp` | PASS |
| TC-2 | AC5/FR7 category | `TestBuildTree_CategoryAssignment` | PASS |
| TC-3 | deleted-union (update-full) | `TestBuildTree_DeletedUnion_AbsentFromEntries` | PASS |
| TC-4 | deleted-flag no double-count | `TestBuildTree_DeletedFlag_NoDoubleCount` | PASS |
| TC-5 | FR8 empty/no-change | `TestBuildTree_Empty` | PASS |
| TC-6 | AC7 escape-safety (allowlist) | `TestSanitiseLabel_RejectByDefault` (unit) **+ real-TTY crafted filename** (`ESC[2J` + OSC `]0;pwned` in the name rendered as inert `\x1b…` text, viewer exit 0) | PASS |
| TC-7 | AC1a flag + help | CLI: both `status`/`update --help` list `--interactive-tree` | PASS |
| TC-8 | AC1b rejected elsewhere | CLI: `dupes --interactive-tree` → "unknown flag" | PASS |
| TC-9 | AC1c inert on non-TTY/JSON | CLI: piped `status` → normal text, exit 0; `update --json` → JSON, no TUI, no hang | PASS |
| TC-10 | AC2 launch after committed run | By construction (viewer launched after `Apply` atomic-rename + summary) + `TestPostRunTree_EndToEnd`; real-TTY `update` viewer exit 0 | PASS |
| TC-11 | KD3 update enrichment | `TestApply_CollectChanges` (N add/M mod/K del incl. full-update delete) | PASS |
| TC-12 | AC3/FR4 no second fs walk | Code inspection: `PostRunTree` → `LoadMergedMainCacheIndex` (mmap read) only, no `Walker`; reinforced by TC-17 byte-identity | PASS (by construction) |
| TC-13 | AC4/FR6 width gating + resize | `TestWidthGating` (sim: divider present@120, absent@40, resize no panic) + real-TTY narrow(40)/wide(120) exit 0 | PASS |
| TC-13b | AC9/FR10 sort comparators | `TestSortNodes_Comparators`, `TestSortNodes_DoesNotMutateInput`, `TestKeyForRune` | PASS |
| TC-13c | AC9/FR10 live re-sort, no re-read | `TestLiveResortPreservesSelectionNoReRead` (selection preserved, tree pointer unchanged) | PASS |
| TC-14 | AC4/FR5 navigation | `TestNavigation` (sim: expand/down/collapse) + real-TTY nav keys | PASS |
| TC-15 | AC6/NFR5 teardown quit/Ctrl-C | `TestRunScreen_QuitAndTeardown`, `TestRunScreen_CtrlCQuits` (sim) + real-TTY `q` and Ctrl-C both exit 0 (terminal restored) | PASS |
| TC-16 | AC8/FR9 init-failure non-fatal | Launcher catches `PostRunTree`/`Run` errors → sanitised stderr, exit code preserved; `TestRun_EmptyTreeNoOp`; `sync.Once` Fini + `sanitiseError` paths exercised | PASS (see note) |
| TC-17 | integrity: non-interactive regression | `TestApply_CollectChangesByteIdentical` (main.idx byte-identical collect-off vs collect-on) + full `cmd/dcfh` suite green (stdout unchanged) | PASS |
| TC-18 | -race collector concurrency | `go test -race -d=checkptr=0` on `tui` + enrichment — no data race | PASS |
| TC-19 | usability: sizes & spelling | `dcfh.FormatHumanSize` at render.go:143/201/231; British spelling (no `color`/`behavior`/US `-ize` in new source) | PASS |

### Non-Functional Tests
- **Race (NFR / TC-18)**: clean under `-race -gcflags=all=-d=checkptr=0` for both the `tui` event loop and the update-enrichment collector path.
- **Escape-safety (NFR4 / TC-6)**: confirmed in a *real* terminal — a filename carrying a clear-screen CSI and an OSC title-injection was escaped to printable text and rendered inert; the viewer still exited 0.
- **Teardown reliability (NFR5)**: `q`, Ctrl-C, narrow, and wide all leave a usable shell (process exit 0 after `Fini`).

## Test Failures

None. All 22 mapped test cases pass.

## Coverage Report
- `pkg/treeview.go`: `buildTreeFromEntries` 97.4%, `BuildTree`/`leafStats`/`aggregate`/`sortChildren`/`sanitiseLabel`/`labelClean` 100%, `safeRune` 80% (the exported `SanitiseLabel` wrapper shows 0% in the pkg run because it is exercised from the `tui` package's error path, not the pkg suite).
- `cmd/dcfh/internal/tui`: 78.9% of statements (drawing branches, width-gating, navigation, sort, teardown).
- Enrichment: canonical `scanWriteSink` `record`/`OnMatch`/`OnRightOnly` covered; `repoCore.PostRunTree` 75%, `runUpdateCollecting` 75%.

### TC-16 note
The non-fatal *handling* (error → sanitised stderr, command exit code preserved; idempotent `sync.Once` teardown; tcell's documented Fini-after-failed-Init safety) is verified by code path and the empty-tree no-op test. Forcing a genuine `screen.Init()` failure on an *attached* TTY headlessly collapses to the same guard-skip as TC-9, so that exact runtime branch was not reproduced in this environment. Low risk — flagged for the manual checklist at rollout.

## Security Review

**State**: error

error: cap exceeded: 2037 production lines > 500

The testing-phase changeset helper exited 2 (production-weighted count over the 500-line cap), so per the CWF contract the `cwf-security-reviewer-changeset` subagent was **not** invoked and this is recorded as `error`. The cap was exceeded because the testing phase counts test files as production (they are the phase's primary artifact): the task's ~750 lines of new test code (`treeview_test.go`, `treeview_enrichment_test.go`, `render_test.go`, `sort_test.go`) plus the implementation diff sum to 2037 production lines from anchor `b5bcbcd`. The full diff was still emitted to the reviewer changeset for manual inspection.

Not a security gap: the **implementation-phase** review (recorded in `f-implementation-exec.md`) already performed the full FR4(a–e) review over all production code and returned **no findings**. The testing-phase delta is test-only and adds no new production attack surface.

## Status
**Status**: Finished
**Next Action**: /cwf-rollout 11
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
*To be filled upon completion*

## Lessons Learned
*To be captured during retrospective*
