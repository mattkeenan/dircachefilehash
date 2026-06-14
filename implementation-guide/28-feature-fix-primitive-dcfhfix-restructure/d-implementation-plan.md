# Fix primitive + dcfhfix restructure - Implementation Plan
**Task**: 28 (feature)

## Task Reference
- **Task ID**: internal-28
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/28-fix-primitive-dcfhfix-restructure
- **Template Version**: 2.1

## Goal
This is the **coordinating parent**: the production code was implemented and
tested inside the three subtasks (28.1, 28.2, 28.3). The parent's implementation
job is **integration** — assemble the three landed subtasks onto one branch,
verify they compose into a single coherent feature, and confirm the parent's five
success criteria (a-task-plan.md) are met by the whole. No new production code is
written here; if integration reveals a gap, it is fixed in a new subtask, not
inline (per the decomposition that created 28.1–28.3).

## Workflow
Assemble (ff-only merges) → verify the whole builds and tests green → map each
parent success criterion to the landed surface → record evidence in
f-implementation-exec.md.

## Files to Modify
### Primary Changes
- **None (production)**. The implementing diffs already landed via the subtask
  squashes now on this branch:
  - 28.1 `a0bcf0e0` — fsck helpers relocated to `pkg/`; entry writer corrected.
  - 28.2 `61efe765` — `Repo.Fix` primitive + dcfhfix thin translator + backup stack.
  - 28.3 `752cde35` — multi-source `recovery-rebuild` Fix op.

### Supporting Changes
- `implementation-guide/28-.../f-implementation-exec.md` — integration evidence
  (build/test/lint output, per-criterion verification, security verdict).
- `implementation-guide/28-.../{d,e}-*.md` — this plan + the integration test plan.

## Implementation Steps
### Step 1: Confirm assembly
- [ ] Verify the ff-merge chain put **all three** subtasks on this branch with
  linear history — enumerate the three squashes `a0bcf0e0` (28.1), `61efe765`
  (28.2), `752cde35` (28.3); `git log --no-merges a0bcf0e0~1..752cde35` shows them
  consecutively with no merge commits. (Do **not** use `61efe765..752cde35` — that
  range starts at 28.2 and silently omits 28.1.)
- [ ] LSP/symbol check that the three surfaces coexist in `pkg` without collision:
  `FixRequest`/`RunFix` (fix_run.go), `runRecoveryRebuild` (fix_recovery.go),
  the backup stack (fix_backup.go) — confirmed at plan time.

### Step 2: Build the whole
- [ ] `go build ./...` — all three binaries (dcfh/dcfhfind/dcfhfix) compile on the
  assembled branch.

### Step 3: Full-suite verification (regression of the integrated whole)
- [ ] `go test ./pkg/... ./cmd/...` green.
- [ ] Pre-commit `-race -d=checkptr=0` green.
- [ ] `golangci-lint run ./...` (gosec floor) 0 issues; `govulncheck` 0 applicable.

### Step 4: Map parent success criteria to the landed surface
- [ ] SC1 (`Repo.Fix` on the interface + `FixRequest`/`FixResult`) → 28.2; verify
  `Repo.Fix` resolves and `RunFix` is its single engine.
- [ ] SC2 (fsck helpers in `pkg/`, dcfhfix a thin translator, no behaviour change)
  → 28.1 + 28.2; verify `cmd/dcfhfix` imports `pkg` helpers; CLI surface unchanged.
- [ ] SC3 (multi-source recovery rebuild of `main.idx`) → 28.3; verify the
  `recovery-rebuild` op + `mergeSourcesIntoEntries` are present and tested, and
  name the negative guards as integration assertions: mutual-exclusion (recovery
  cannot combine with other ops, `fix_run.go:216`) and library-only confinement
  (the op requires a write root, `fix_recovery.go:33`) — both covered by 28.3's
  suites.
- [ ] SC4 (both fsck modes; new-file write, no in-place mutation) → 28.2; verify
  auto-fix + the typed `ErrManualModeUnimplemented` deferral (fail-closed: no
  write occurs, `fix_run.go:208`), single-writer path.
- [ ] SC5 (existing dcfhfix/recovery tests still pass + new coverage) → all three;
  verify `main_test.go`/`options_test.go`/`recovery_test.go` green alongside the
  new `fix_*`/`fix_recovery_*` suites.

### Step 5: End-to-end coherence smoke
All smoke runs operate on a **throwaway temp repo** (e.g. `t.TempDir()`/`mktemp -d`),
never a real index, so a mid-write failure cannot strand a half-written index (the
atomic-rename invariant means the worst case is a leftover `.fix.tmp` in the
throwaway dir).
- [ ] dcfhfix subcommand smoke (`header`/`entry`/`fixes` families) on a temp index.
- [ ] Recovery rebuild through the library on a seeded temp repo (destroyed main +
  intact cache → re-readable main.idx).

## Code Changes
### Before / After
**N/A — integration parent.** All before/after diffs are recorded in the subtask
f-implementation-exec.md files. The one cross-subtask note for integration: the
landed API shape evolved from the parent a-plan prose — `FixRequest` now carries
`IndexSelectors`/`Mode`/`Flags` (not a single `IndexSelector`), and `FixResult`
dropped `BackupID` (28.2 LD7, AC5 resolved via `fixes-list`). This is an accepted
evolution, recorded here so the parent's documented API matches the code.

## Test Coverage
**See e-testing-plan.md for the integration test plan.** The parent adds no new
unit tests; it runs the union of the subtask suites as the integration regression
gate and asserts the five success criteria hold on the assembled whole.

## Validation Criteria
**See e-testing-plan.md.** Done when: the assembled branch builds; the full suite
+ `-race` + lint + govulncheck are green; all five parent success criteria map to
a landed, tested surface; and the whole-changeset security review is recorded.

## Scope Completion
**IMPORTANT**: Complete all planned integration verification before marking
Finished. Any integration gap is fixed in a new subtask (preserving the 28.1–28.3
decomposition), not patched inline on the parent — and only with user approval.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Integration plan executed exactly: linear ff-assembly of the three squashes (no
merge commits), clean build of all three binaries, full suite + `-race` + lint(0)
+ govulncheck(0) green, SC1–SC5 each mapped to a landed symbol. No integration
gap → no new subtask required. Zero production lines added by the parent.

## Lessons Learned
The "any gap is a new subtask, never an inline parent patch" rule was never
tested because the decomposition left no gap — but having it written kept the
integration phase honest about its zero-code remit.
