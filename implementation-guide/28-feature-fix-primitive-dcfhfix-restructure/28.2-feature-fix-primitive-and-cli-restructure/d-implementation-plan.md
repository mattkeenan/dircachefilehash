# Fix primitive and CLI restructure - Implementation Plan
**Task**: 28.2 (feature)

## Task Reference
- **Task ID**: internal-28.2
- **Task URL**: N/A (internal task)
- **Parent Task**: 28
- **Branch**: feature/28.2-fix-primitive-and-cli-restructure
- **Template Version**: 2.1

## Goal
Implement the `Repo.Fix` primitive and the dcfhfix-as-thin-translator restructure
per c-design-plan.md (LD1–LD7), single-source only. Three milestones, each its
own checkpoint + regression gate.

## Workflow
Patterns first → Test → Minimal impl → Refactor green → Commit message explains "why"

## Files to Modify
### New files
- `pkg/fix_run.go` — `FixOp`/`FixCommand`/`FixRequest`/`FixResult`; the op classification (LD2); `RunFix` (route-by-family); `confineWriteDest` + `confineWriteDir` (LD3); `ErrManualModeUnimplemented`.
- `pkg/fix_backup.go` — relocated backup stack (FR3). **Full cluster** (omitting any breaks the M1 build): `BackupMetadata`, `getIndexType`, `getBackupDir`, `createBackup`, `saveMetadata`, `loadMetadata`, `removeBackupFiles`, `listBackups`, and the fixes-stack ops `fixesList`/`fixesPop`/`fixesDiscard`/`fixesClear`. **Not** `copyFile` — see the collision note below. Each function is split into a pure pkg core (stack manipulation, returns data/counts/`io.Writer`-rendered) and the CLI keeps a thin presenter (see M1).

### Primary changes
- `pkg/repo.go` — add `Fix(ctx, FixRequest) (*FixResult, error)` to the `Repo` interface beside `Filter` (`:186`); drop/refresh the stale "Fix is deferred to Phase 1b" comment (`:170`).
- `pkg/repo_local.go` — add `func (r *repoCore) Fix(...)` mirroring `Filter` (`:346`): resolve selectors via `ResolveIndexSelectors(r.ms.MetaDir, …)`, default-fill, MetaDir-confinement mode, delegate to `RunFix`. `localRepo` **and** `wireRepo` inherit it via the embedded `repoCore` (no separate wire impl; wire's Fix acts on the invoker-side MetaDir — remote-fix semantics out of scope).
- `pkg/fix_entry_workflow.go` — split the fused `ProcessEntriesWithWorkflow` / append / removal wrappers into pure collectors (`processAllEntriesFor{Edit,Append,Removal}`, already pure at `:210/:476/:514`) + a separate write step, so `RunFix` calls collect then conditionally `writeRepairedIndex` (LD5). Extract the shared cap-check predicate (LD4).
- `cmd/dcfhfix/main.go` — handlers (`handleHeaderCommand`/`handleEntryCommand`/`handleFixesCommand`, `:393/:416/:452`) shrink to arg-parse → `FixCommand` batch → shared core; delete the relocated backup/fixes function bodies (now imported from `pkg`). Preserve help text, exit codes, inspection output bytes.

### Supporting changes
- `cmd/dcfhfix/*_test.go` (`main_test.go`, `options_test.go`, `promote*_test.go`, `writepath_test.go`) — call-site adaptation to relocated/exported symbols; correct any expectation that encoded pre-FR9 output (documented as a fix).
- gosec suppression rationales on the relocated G304/G306/G703 sites — rewritten to cite the `confineWriteDest`/`MetaDir` invariant (M2, once the guard exists).

## Implementation Steps

### Milestone 1 — Backup-stack relocation (FR3, behaviour-preserving) — commit 1
**Not a verbatim move** — these functions are CLI-coupled (take `*ParsedOptions`, call `options.GetBool`/`getFormat`, `fmt.Printf` to stdout). `pkg/` cannot import the cmd-local `ParsedOptions`, so each splits into a pure pkg core + a thin CLI presenter that keeps the output bytes:
- [ ] Create `pkg/fix_backup.go`; relocate the **full cluster** (incl. `saveMetadata`/`loadMetadata`/`removeBackupFiles`). Replace `*ParsedOptions` params with explicit scalars (`backup bool`, `verbose int`, `quiet bool`, `format string`) or a small pkg options struct; route any human/JSON rendering through an `io.Writer` so the CLI keeps owning stdout.
- [ ] **`copyFile` collision**: `pkg/recovery_test.go:241` already declares `func copyFile(src, dst string)` in package `dircachefilehash`, and `pkg/fix_promote.go:74` already does the open-create-`io.Copy` dance. Do **not** relocate cmd's `copyFile`. Pick the survivor: promote one production helper (reuse `fix_promote.go`'s copy or extract one unexported `copyFile` in pkg) and delete the `recovery_test.go` duplicate, reconciling its callers. Name the survivor in exec.
- [ ] Rewire `cmd/dcfhfix/main.go` + `*_test.go` to the exported `dircachefilehash.*` cores via thin presenters; delete the moved bodies.
- [ ] Move the gosec suppressions with the code, keeping their **current** honest "user-supplied subject path" wording for now (still CLI-only callers in M1; full re-anchor in M2).
- [ ] **Gate**: `go build ./...` + `go test ./...` green; pre-commit `--new` lint clean. CLI presenters preserve output bytes/exit codes (TC-R1); the new pkg cores are independently testable.

### Milestone 2 — Fix primitive core (FR1/FR2/FR5/FR6; D1/D2/LD2–LD5) — commit 2
- [ ] `pkg/fix_run.go`: `FixOp` constants + classification; `FixCommand`/`FixRequest`/`FixResult{RepairsApplied,EntriesDiscarded}`; `ErrManualModeUnimplemented`.
- [ ] **Fail-closed classification — invert the naive map**: a `map[FixOp]bool` returns `false` for a missing key, which would make an unclassified op *read-only* (the opposite of fail-closed). Use a `readOnlyOps` set and treat "not in the set" as **write**, so a future `FixOp` added without thought is confined.
- [ ] `confineWriteDest(dest, root)` + `confineWriteDir(dir, root)` (LD3, sketch above): fail-closed on any abs/resolve error. Apply to **every** artefact: final index, `.fix.tmp`, `.pre-fix-*` (file confinement) and the upward-walked backup dir (dir confinement, resolve the existing `.dcfh` ancestor then MkdirAll under it).
- [ ] **Verify `.fix.tmp` open mode**: confirm `writeRepairedIndex` (`fix_entry_workflow.go:119`) opens the temp without following a planted symlink (O_EXCL or equivalent); otherwise the prefix check passes and a symlink redirects the write. `PreserveOriginal` already guards the `.pre-fix-*` dest via `O_EXCL` (`fix_promote.go:47`) — confirm the temp and final-rename paths are equally non-followable so the TOCTOU defence holds across all four artefacts.
- [ ] Split `fix_entry_workflow.go` wrappers (pure collect vs write, LD5); extract **only** the shared cap-check predicate (LD4). **Discard-counting stays per-loop**: the edit path counts in `processSingleEntry`/`handleCorruptedEntry`, append/removal count inline — sharing only the `> 100` guard avoids double-counting or shifting the 101st-entry boundary.
- [ ] `RunFix(ctx, refs, req, writeRoot, warnOut)` (route-by-family, LD2): **`writeRoot` is an unexported confinement-root argument** set by the caller (`repoCore.Fix` → `MetaDir`; CLI → resolved subject dir), **not** a `FixRequest` field — so a library consumer cannot set/relax it. index-mutating → collect → confine(writeRoot) → (dry-run gate) → `writeRepairedIndex`; inspection → existing render helpers; backup-stack → relocated stack ops (stack dir confined via `confineWriteDir`). `FixModeManual` → `ErrManualModeUnimplemented`.
- [ ] `repoCore.Fix` (`repo_local.go`) + `Fix` on the `Repo` interface (`repo.go`). Confirm `localRepo`/`wireRepo` still satisfy `var _ Repo` (build).
- [ ] Re-anchor the M1-moved gosec rationales to the `confineWriteDest`/`MetaDir` invariant (rewrite text, not blanket re-exclude).
- [ ] **Gate**: `go build ./...` + `go test ./...` green; `golangci-lint run ./...` clean. (Full TC-2…TC-9 land in e/g.)

### Milestone 3 — CLI re-expression (FR4; D6/LD6) — commit 3
- [ ] Rewrite the three handlers to build `FixRequest{IndexSelectors:[subject], Commands:[…], Mode:Auto, DryRun, Backup}` and call the shared core. Repo-less path synthesises the two-field `*MetaStore` via 28.1's `newFixMetaStore(metaDir, subjectChecksumType)`.
- [ ] Map `edit json` forms onto `*-edit` Op with `Field:"json"`, `Value:<json>`; preserve `entry edit json`'s current "not yet implemented" stub (`:835`). **Note its current side-effect**: today the stub creates a backup *before* returning the error unless `--dry-run` (`:828-833`). Preserve the observable behaviour (backup-then-error) to keep `main_test.go` parity; if no test pins the pre-error backup, error-only is acceptable and recorded as a deliberate documented micro-change.
- [ ] Keep inspection (show/list) output bytes, exit codes, and help text verbatim; `fixFlags()` feeds `DryRun`/`Backup`.
- [ ] Reconcile `main_test.go`/`options_test.go`; correct any pre-FR9 expectation as a documented fix.
- [ ] **Gate**: full `go test ./...` + `golangci-lint run ./...` clean; CWF changeset security review recorded (f-implementation-exec).

## Code Changes
### `confineWriteDest` / `confineWriteDir` — the one security-critical primitive (sketch; LD3)
Two confinement entry points are needed because the artefacts differ in shape:
- **`confineWriteDest(dest, root)`** — a single *file* target (`.fix.tmp`, `.pre-fix-*`, final index). The leaf need not exist; resolve symlinks on its **existing parent** and confine.
- **`confineWriteDir(dir, root)`** — the backup *directory* (`<dcfh>/fixes/<type>/`), which is **multi-component** below the upward-walked `.dcfh` and is created by `MkdirAll` *after* the check. The single-component leaf guard would wrongly reject it. Instead: resolve the **deepest existing ancestor** of `dir` (the `.dcfh`, which always exists), confine that resolved ancestor `hasPathPrefix root`, then `MkdirAll` the remaining segments **under the already-confined ancestor**. This avoids `EvalSymlinks` erroring on the not-yet-created `fixes/<type>/` leaf (which would fail-closed-reject every first-time backup).

```go
// confineWriteDest: canonical abs file target within root, else error. Fail-closed.
func confineWriteDest(dest, root string) (string, error) {
    absDest := filepath.Clean(mustAbs(dest))           // abs error → reject
    parent, err := filepath.EvalSymlinks(filepath.Dir(absDest)) // existing parent
    if err != nil { return "", err }                   // fail-closed
    resolved := filepath.Join(parent, filepath.Base(absDest))
    rootR, err := filepath.EvalSymlinks(filepath.Clean(mustAbs(root)))
    if err != nil { return "", err }
    if !hasPathPrefix(resolved, rootR) {
        return "", fmt.Errorf("write destination %q escapes %q", dest, root)
    }
    return resolved, nil
}
```
(The post-`Clean` `..`-Base guard from the design is dead on an absolute path and is dropped; the symlink-resolved prefix check is the real guard. `hasPathPrefix` is reused from `wire_handler.go:240`.)
### CLI handler shape (before → after; FR4)
Before: each handler calls a bespoke `entryEdit`/`headerEdit`/`entryAppend`/… that
itself reads, mutates, backs up, and writes.
After: each handler parses args into a `[]FixCommand`, builds a `FixRequest`, and
calls the shared core; all read/mutate/backup/write logic lives behind `RunFix`.

## Test Coverage
**See e-testing-plan.md for the complete test plan.** Key gates per milestone:
- M1: TC-R1 (relocation behaviour-preserving — full suite unchanged).
- M2: `Repo.Fix` through-interface tests; `confineWriteDest`/`confineWriteDir` both-sides incl. an upward `.dcfh` outside the root rejected (AC7); cap boundary on all three loops (AC6); dry-run zero-artefact, incl. `--backup --dry-run` skipping the backup — confirm the design's dry-run gate and `createBackup`'s own `!backup` early-return compose (AC4).
- M3: CLI parity (`main_test.go`/`options_test.go`); `entry edit json` stub preserved.

## Validation Criteria
**See e-testing-plan.md.** Maps to AC1 (M2/M3), AC2 (M1), AC3 (M3), AC4 (M2/M3), AC5 (design, done), AC6 (M2), AC7 (M2).

## Scope Completion
**IMPORTANT**: Complete all planned implementation before marking task Finished.
Out of scope (28.3): `mergeSourcesIntoEntries` + multi-source recovery rebuild (FR8).
Deferred: Manual interactive mode (returns typed error). No other deferrals planned;
any that arise get user approval + a tracked follow-up per the rule below.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-plan 28.2
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
LD1–LD7 executed across M1–M3, each green at its build + `go test ./...` +
`golangci-lint` + pre-commit `-race` gate. Collect/write split (LD5) and the
shared `capExceeded` predicate (LD4) landed as planned. See
f-implementation-exec.md for per-milestone results and deviations.

## Lessons Learned
Splitting collect from write (LD5) was the enabler for both the dry-run gate and
surfacing partial discard counts on a cap-trip (the AC6 fix) — one structural
decision paid off twice.
