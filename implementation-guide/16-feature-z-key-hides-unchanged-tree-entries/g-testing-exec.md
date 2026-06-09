# z key hides unchanged tree entries - Testing Execution
**Task**: 16 (feature)

## Task Reference
- **Task ID**: internal-16
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/16-z-key-hides-unchanged-tree-entries
- **Template Version**: 2.1

## Goal
Execute the tests defined in e-testing-plan.md and verify implementation from d-implementation-plan.md.

## Execution Checklist
- [x] Read e-testing-plan.md and d-implementation-plan.md thoroughly
- [x] Verify test environment ready (Go test + tcell SimulationScreen, no TTY)
- [x] Execute test cases sequentially
- [x] Record pass/fail for each test
- [x] Document failures with reproduction steps (none)
- [x] Update status to "Finished" when all pass

## Test Results

### Functional Tests

`go test -run 'TestHasChange|TestHide|TestFooterAdvertisesHide' -v ./cmd/dcfh/internal/tui/...`

| Test ID | Go test | Covers | Status |
|---------|---------|--------|--------|
| TC-U1 | TestHasChange (5 subtests) | AC2/FR2 — pure predicate incl. deleted-only | PASS |
| TC-1  | TestHideToggleRoundTrip | AC1/FR1 — z toggles, second z restores | PASS |
| TC-2  | TestHideUnchangedLeaf | AC3/FR3 — unchanged leaf hidden, changed kept | PASS |
| TC-3  | TestHideKeepsDeletionOnlyCollapsedDir | AC2/AC4b, FR2/FR4 — deletion-only collapsed dir stays, not force-expanded | PASS |
| TC-4  | TestHideWhollyUnchangedDir | AC4/FR4 — wholly-unchanged dir hidden | PASS |
| TC-5  | TestHideComposesWithSortReverse | AC6/FR6 — hide persists across sort+reverse | PASS |
| TC-6  | TestHideSelectionPreserved | AC5/FR5 — selection preserved (visible node) | PASS |
| TC-7  | TestHideSelectionClampedWhenHidden | AC5/FR5 — clamp branch, in-range, no panic | PASS |
| TC-8  | TestFooterAdvertisesHide | AC7/FR7 — footer advertises `z hide` | PASS |
| TC-9  | TestHideAllUnchangedEmptyStateAndNav | AC8/FR8 — empty-state + header/stats + nav-no-panic + restore | PASS |

All 10 task-16 tests PASS (TC-U1 = 5 subtests over the change-category combinations).

### Non-Functional Tests
- **Reliability (NFR5)**: TC-7 (hidden selection clamps) and TC-9 (empty + navigation
  on zero rows) assert the no-panic / clamp contract; `current()==nil` verified.
  Existing `TestRun_EmptyTreeNoOp` still green.
- **Usability (NFR2)**: TC-8 (discoverable binding) + TC-1 (reversible) + TC-3/TC-4
  (path to a changed leaf never broken).
- **Performance (NFR1)**: no dedicated test — per-row pure predicate over
  already-aggregated stats; covered by the existing suite's timing.
- **Security (NFR4)**: see Security Review below — TUI-only, no new node-derived
  strings rendered.

### Regression
`go test ./cmd/... ./pkg/...` — all packages ok. The pre-commit `-race ./...`
gate (run on the phase-f commit b5f81b2) passed across all packages. No existing
test was disabled or weakened; the hide default is OFF so all prior tests run
hide-off and are unaffected.

## Test Failures
None.

## Coverage Report
- `cmd/dcfh/internal/tui` package: **88.1%** of statements.
- `hasChange` — **100.0%** (both arms).
- `rebuildRows` (new hide filter branch) — **100.0%**.
- `handleRune` (incl. the new `z` case) — 80.0% (the `z` case is exercised by
  TC-1/5/6/7/9; the uncovered remainder is pre-existing rune branches unrelated
  to this task).

## Security Review

**State**: no findings

I have the full changeset and threat model. Let me review the testing-exec changeset, which adds the test code for the `z`-key hide-unchanged TUI toggle (the production code was reviewed at implementation-exec; this step's new surface is `render_test.go`, plus the process docs).

Let me reason through the five threat categories.

**(a) Bash injection / unsafe command construction.** The diff contains no shell invocation. The new test code in `render_test.go` constructs `dcfh.Tree`/`dcfh.Node` fixtures with hardcoded literal labels (`"main.go"`, `"lib.go"`, `"readme.md"`, `"a.txt"`, `"b.txt"`), drives the tcell `SimulationScreen` via `handleKey`, and asserts on `m.rows`/`screenText`. There is no `os/exec`, `system`, backtick, or any command construction. The process docs (`a-`…`f-*.md`) are plain markdown. Nothing to flag.

**(b) Perl helpers consuming git/user output without `-z`.** No Perl anywhere in the diff. The added files are Go test code and markdown CWF artefacts — no executable helpers, no git porcelain parsing. N/A.

**(c) Prompt injection via user-supplied strings.** No LLM context surface is introduced. The test fixtures use only hardcoded string literals; no task description, slug, branch name, or git output flows into any model context. The TUI under test renders node labels via the existing `drawText` path, unchanged by this task — and the tests assert only on fixed expected substrings (`"z hide"`, `"(no changes to display)"`, `"2 files"`, `"(nothing selected)"`). Nothing to flag.

**(d) Unsafe environment-variable handling.** No env vars are read, set, or introduced. The tests construct fixtures and a simulation screen in-process; `tcell.NewSimulationScreen("UTF-8")` requires no TTY and reads no environment. Nothing to flag.

**(e) Pattern-based risks (safe-here-but-risky-elsewhere).** The implementation-exec security review already surfaced the one genuine pattern note here — the selection-clamp path. When hide mode prunes the currently-selected node, the toggle relies on `selectNode`/`clampSel` to keep `m.sel` in range, and the all-hidden case leaves zero rows with `current()` returning nil. This testing-exec changeset is the half that *exercises* that contract: `TestHideSelectionClampedWhenHidden` asserts `0 <= m.sel < len(m.rows)` after the selected node is hidden, and `TestHideAllUnchangedEmptyStateAndNav` drives `Down`/`Right` navigation against an empty row set and checks `current() == nil` with no panic. The tests therefore reinforce the invariant rather than introduce a new risk. Safe here because the only memory-safety ceiling is a local panic in read-only viewer code (no index/filesystem mutation); a future row-mutating key binding that bypasses `selectNode`/clamp would be the place this could regress — audit such future bindings to re-clamp the selection. No actionable defect in this changeset.

A note on test integrity: the new tests genuinely assert behaviour (row presence/absence, footer content, selection bounds, empty-state body) rather than being tautological or disabled — `TestHasChange` covers the deletion-only correctness point that guards against the `Stats.Files` trap. No existing test is weakened or disabled. This is sound from a security-review standpoint (no test was hollowed out to mask a defect).

No actionable security concerns. The testing-exec changeset is in-process Go test code over hardcoded fixtures plus markdown process docs — no shell, env-var, network, file-write, or LLM-context surface.

```cwf-review
state: no findings
summary: testing-exec adds in-process TUI tests over hardcoded fixtures + markdown docs; no shell/env/file/LLM surface. Tests reinforce the selection-clamp/empty-state invariant (panic-only ceiling).
```

## Status
**Status**: Finished
**Next Action**: /cwf-rollout
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Lessons Learned
*To be captured during retrospective*
