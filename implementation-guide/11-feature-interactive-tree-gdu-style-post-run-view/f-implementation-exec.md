# interactive-tree gdu-style post-run view - Implementation Execution
**Task**: 11 (feature)

## Task Reference
- **Task ID**: internal-11
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/11-{task-description}
- **Template Version**: 2.1

## Goal
Execute the implementation following the plan in d-implementation-plan.md and e-testing-plan.md.

## Execution Checklist
- [ ] Read d-implementation-plan.md and e-testing-plan.md thoroughly
- [ ] Verify all prerequisites met
- [ ] Execute implementation steps sequentially
- [ ] Update "Actual Results" for each step
- [ ] Document any deviations from plan
- [ ] Update status to "Implemented" when complete

## Implementation Steps (from d-implementation-plan.md)

Pre-exec gate: KD4 (tcell) confirmed by the user; recorded in c-design-plan.md.

## Actual Results

### Step 1: Setup (deps)
- **Planned**: `go get tcell/v2` + `x/term`; `go mod tidy`; build green.
- **Actual**: Added `github.com/gdamore/tcell/v2 v2.13.10` and `golang.org/x/term v0.42.0` (+ transitive `gdamore/encoding`, `lucasb-eyer/go-colorful`, `rivo/uniseg`). `go mod tidy` clean; build green.
- **Deviations**: tcell requires Go ≥ 1.25, so `go.mod`'s `go` directive auto-bumped `1.24.3 → 1.25.0` (toolchain is `go1.26.4`, so no build impact). This raises the project's minimum Go from 1.24.3 — noted for the rollout/CLAUDE.md update. Not a blocker.

### Step 2: Pure data layer (`pkg/treeview.go` + test)
- **Planned**: `Category/Stats/Node/Tree/ChangeSet`, pure `buildTreeFromEntries`, `BuildTree` adapter, `sanitiseLabel` allowlist; literal-driven tests.
- **Actual**: Implemented as planned. `Stats.Files/Bytes` count the **live** footprint only; deleted nodes are count-only (`Stats.Deleted`), giving the clean invariant `Files == Added+Modified+Unchanged`. `sanitiseLabel` is a reject-by-default printable allowlist (`safeRune` + `strconv.QuoteRune` escapes + `\xNN` for invalid UTF-8). 7 unit tests (aggregation, category, deleted-union, deleted-flag no-double-count, empty, canonical order, sanitiser incl. bytes outside the CSI set) — all green.
- **Deviations**: None of substance.

### Step 3: Facade seam (`PostRunTree`)
- **Planned**: `PostRunTree(ctx, ChangeSet)` on `repoCore` + `Repo` interface; `LoadMergedMainCacheIndex` + `BuildTree`.
- **Actual**: Implemented on `repoCore` (both `localRepo`/`wireRepo` satisfy it); added to the `Repo` interface. End-to-end test via the local repo.
- **Deviations**: None.

### Step 4: Update change-set enrichment (KD3)
- **Planned**: additive `UpdateResult.Added/Modified/Deleted`; `ApplyRequest.CollectChanges`; optional collector threaded canonical-pass-only.
- **Actual**: Added `changeCollector` (lock-free, single-writer). Threaded `newScanWriteSink → RunUpdatePipeline → performPipelineScan → updateFull/SpecificPaths → runUpdateCollecting → Apply`. Canonical sink records `OnRightOnly→Added`, `OnMatch(needsHash)→Modified`, `OnLeftOnly→Deleted`; delta (cache-refresh) sink passes `nil`. Drops a path on `RelativePath()` error. Tests: TC-11 (add/modify/delete incl. full-update delete), nil-by-default, and a byte-identity check (collect-off vs collect-on main.idx identical) — all green; `-race` clean.
- **Deviations**: To keep the 28 existing `runUpdate` test call-sites unchanged, I added `runUpdateCollecting(... collector, paths...)` as the real entry point and made `runUpdate` a thin wrapper. The plan said "runUpdate gains an optional collector param"; the wrapper split is functionally equivalent and lower-churn. `runUpdate`'s variadic `paths` was then **removed** (it was always nil after the split — `unparam` flagged it; all callers do whole-repo updates).

### Step 5: Render package (tcell) — `tui/{tui,render,sort}.go`
- **Planned**: `Run`, two-pane width-gated layout, nav/resize loop, idempotent teardown, sort comparators, rune-aware truncation, sanitised error text.
- **Actual**: Implemented. Keys: ↑/↓ + j/k move; →/Enter + l expand/descend; ←/h collapse/parent; `c/a/m/d/n` sort (change/added/modified/deleted/name); `r` reverse; `q`/Esc/Ctrl-C quit. Stats pane shown at width ≥ 80. Teardown via `sync.Once` over `screen.Fini`. Error/teardown text routed through the exported `dcfh.SanitiseLabel`. Render tests via tcell `SimulationScreen`: width-gating, navigation, live re-sort (selection preserved, no data re-read), clean quit + idempotent Fini, Ctrl-C quit, empty-tree no-op — all green; `-race` clean.
- **Deviations**: (1) Added an unexported `runScreen(screen, t, o)` seam so the event loop + teardown are testable with `SimulationScreen` (Run supplies a real screen). (2) Exported `dcfh.SanitiseLabel` (thin wrapper over the unexported `sanitiseLabel`) so the render layer sanitises wrapped error text via the *same* helper (KD6) without duplicating the policy. (3) Per-rune width treated as 1 (no wide-rune width accounting) — cosmetic only; the sanitiser already guarantees safety. Noted for a possible follow-up.

### Step 6: CLI wiring + flag
- **Planned**: `filters.go` field/const/group; capture `state`; TTY/JSON guard; `ChangeSet`; `PostRunTree`; `tui.Run`; help lines.
- **Actual**: Added `flagInteractiveTree`, `filterFlagsState.interactiveTree`, and a `{cmdStatus, cmdUpdate}` registry group. Shared `interactiveTreeWanted`/`launchInteractiveTree` helpers in `common.go`. status builds the `ChangeSet` from `StatusResult`; update sets `ApplyRequest.CollectChanges` only when the viewer will run and builds the `ChangeSet` from the enriched result. Help `Long` lines added to both. Verified: `status/update --help` list the flag (AC1a); `dupes --interactive-tree` → "unknown flag" (AC1b); piped and `--json` runs skip the viewer, exit 0, no hang (AC1c).
- **Deviations**: Guard placed in `interactiveTreeWanted` (single predicate) so it also gates update's collection — avoids collecting on the piped/JSON path.

### Step 7: Validation
- `go build ./...`, `go vet ./...` clean; `make build` produces all three binaries.
- `go test ./...` green (full suite). `-race -d=checkptr=0` clean on the enrichment + viewer event-loop tests.
- `golangci-lint run ./...` → **0 issues** (gosec gate clean). One G115 false positive on `int(os.Stdout.Fd())` carries a per-line `//nolint:gosec // G115` with rationale, per the repo convention.
- Non-interactive byte-identity proven by `TestApply_CollectChangesByteIdentical`. Full real-terminal manual checklist (two-pane/narrow/resize/teardown) is deferred to g-testing-exec per the test plan.

## Blockers Encountered

None.

## Deferral Check
Before marking status=Finished, verify:
- [ ] All steps from d-implementation-plan.md executed
- [ ] All success criteria from a-task-plan.md met
- [ ] All requirements from b-requirements-plan.md addressed (if applicable)
- [ ] All design guidance in c-design-plan.md followed (if applicable)
- [ ] No planned work deferred without user approval
- [ ] If work deferred: Follow-up task created and linked

**If deferral required**: Get user approval, document rationale, create follow-up task.

## Security Review

**State**: no findings

## Security Review — Task 11 (implementation phase)

Reviewed the full changeset (tracked diff + the new `pkg/treeview.go` and `cmd/dcfh/internal/tui/{tui,render,sort}.go` files) against FR4(a–e).

- **(a) Injection — terminal-escape via crafted filenames**: `sanitiseLabel`/`safeRune` (`pkg/treeview.go`) is a true reject-by-default allowlist (keeps a rune only if `unicode.IsPrint` and not C0/DEL/C1/`RuneError`; escapes everything else; `\xNN` for invalid UTF-8). Neutralises ESC/CSI/OSC/DCS, bare `\r\b\n\t`, C1, DEL — i.e. bytes outside any enumerated set. No byte both survives `safeRune` and carries control semantics.
- **(b) Raw bytes to terminal**: every `Node.Label` is sanitised at construction; `drawText` clips the already-sanitised string on rune boundaries and never re-slices raw bytes; header/footer Title is a caller literal; FR9 error/teardown text is routed through `dcfh.SanitiseLabel` (and again at the CLI launcher). No raw filename byte reaches the terminal on any path.
- **(c) `uintptr→int` fd suppression**: `int(os.Stdout.Fd())` narrows the process's own stdout fd (constant 1) — cannot overflow; `x/term` requires `int`. Suppression justified; pattern-reuse caveat recorded.
- **(d) Collector concurrency**: `changeCollector` is lock-free but single-writer (only `hwangLin` in one goroutine drives the sink callbacks) and read after `wg.Wait()` — happens-before via goroutine join, no race; attached canonical-pass-only so no double-record; nil path leaves serialisation byte-identical.
- **(e) Other**: no secrets/env-var/auth changes; viewer is strictly read-only; new tcell + x/term dependency owns the terminal only for the viewer's lifetime behind the TTY guard with idempotent `sync.Once` teardown. Noted: `go.mod` go-directive bumped 1.24.3 → 1.25.0.

**Verdict**: No actionable security findings.

```cwf-review
state: no findings
summary: Escape sanitiser is a sound allowlist applied at model boundary and on error paths; no raw filename byte reaches the terminal; fd-narrow suppression justified; collector is single-writer read-after-join (race-safe); viewer is read-only.
```

## Status
**Status**: Finished
**Next Action**: /cwf-testing-exec 11
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
*To be filled upon completion*

## Lessons Learned
*To be captured during retrospective*
