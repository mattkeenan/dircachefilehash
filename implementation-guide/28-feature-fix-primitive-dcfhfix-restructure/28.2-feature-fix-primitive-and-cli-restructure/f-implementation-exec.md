# Fix primitive and CLI restructure - Implementation Execution
**Task**: 28.2 (feature)

## Task Reference
- **Task ID**: internal-28.2
- **Task URL**: N/A (internal task)
- **Parent Task**: 28
- **Branch**: feature/28.2-fix-primitive-and-cli-restructure
- **Template Version**: 2.1

## Goal
Execute d-implementation-plan.md (LD1–LD7), single-source. Three milestones,
each its own commit + regression gate.

## Execution Checklist
- [x] Read d-implementation-plan.md + e-testing-plan.md and the source they touch
- [x] Verified prerequisites (28.1 single-writer path, newFixMetaStore, hasPathPrefix)
- [x] Executed M1–M3 sequentially, each green at its gate
- [x] Documented deviations below
- [x] Status updated to Finished

## Actual Results

### Milestone 1 — Backup-stack relocation (FR3) — commit 76d65d1a
- **Planned**: relocate the backup cluster to `pkg/fix_backup.go` as pure cores +
  thin CLI presenters; resolve the `copyFile` collision.
- **Actual**: `pkg/fix_backup.go` holds `BackupMetadata`, `BackupIndexType`,
  `BackupDir`, `CreateBackup`, `ListBackups`, `PopBackup`, `DiscardBackup`,
  `ClearBackups` (+ unexported `copyFile`, `saveBackupMetadata`,
  `loadBackupMetadata`, `removeBackupFiles`). `cmd/dcfhfix` kept thin presenters
  (`createBackup` projection — later removed in M3 — and the `fixes` handlers).
  `copyFile` collision resolved onto the single pkg production helper; the
  `recovery_test.go` duplicate deleted and its two callers rebound.
- **Deviations**: relocated error sites switched `%v`→`%w` (pkg errorlint;
  output strings unchanged). Pop/Discard/Clear became pkg cores returning
  data/counts (pre-positions M3), beyond a literal "move".

### Milestone 2 — Fix primitive core (FR1/2/5/6, D2) — commit 32c38169
- **Planned**: `pkg/fix_run.go` types + fail-closed classification + confinement
  + `RunFix`; collect/write split (LD5); shared cap predicate (LD4); interface +
  `repoCore.Fix`; re-anchor gosec rationales.
- **Actual**: all delivered. `RunFix` routes by op family; `confineWriteDest`
  (file) + `confineWriteDir` (multi-component backup dir, deepest-existing-
  ancestor resolution) are symlink-resolved and fail-closed. `repoCore.Fix`
  passes `MetaDir` as the confinement root; inherited by `localRepo` and
  `wireRepo` via the embedded `repoCore`. Collect/write split via
  `collectForEdit/Append/Removal`; `capExceeded` (>100) shared across all three
  walk loops. gosec rationales re-anchored to the confinement/MetaDir invariant.
- **Deviations** (see "Deviations from plan/design" below): header-edit kept its
  own surgical writer (relocated, not `writeRepairedIndex`); `FixCommand` model
  simplified; `FixRequest` gained `Verbose`/`Flags`; `writeRoot==""` exemption
  model.

### Milestone 3 — CLI re-expression (FR4) — commit a4ca494e
- **Planned**: handlers → `FixCommand` batch → shared core; preserve output,
  exit codes, help; `entry edit json` stub preserved.
- **Actual**: `runFixWrite` translates one write subcommand → single-command
  `FixRequest` → `RunFix` (writeRoot ""). Edit handlers keep arg-parse + dry-run
  preview + result messages; `RunFix` owns backup + write. header/entry edit-json
  route through `RunFix` (backup-then-stub-error preserved). Dead cmd machinery
  removed (`headerFieldEditors`, `loadIndexIntoSkiplist`, `getIndexHeader`,
  `writeIndexWith{Modified,Custom}Header`, `EntryData`, `parseUint16`,
  per-handler `createBackup`). TC-3 relocated to `pkg/fix_header_test.go`; cmd
  wire fixtures → `testhelpers_test.go`. End-to-end smoke (init/update/remove/
  list/edit --dry-run/pop) verified output bytes + backup metadata unchanged.

## Deviations from plan/design (for review)
1. **header-edit does NOT route through `writeRepairedIndex`** (design LD2's loose
   "index-mutating → writeRepairedIndex" grouping). That single-writer path
   re-serialises entries to the current layout and recomputes the checksum, so it
   **cannot express** version/flags/signature header edits nor preserve entry
   bytes. header-edit therefore keeps its own surgical writer, relocated verbatim
   to `pkg/fix_header.go` (`ApplyHeaderEdit`/`writeHeaderAndEntries`). Behaviour
   preserved (FR4); still confined by `RunFix` for the library path.
2. **`FixCommand` simplified** vs the design data model: dropped `Index int` and
   `Append *EntryJSON`; use `Paths []string` (every real op is path-based) and
   `Value` carries the raw JSON for append / the json-edit forms (consumed by the
   existing `ParseEntryFromJSON`). Fewer moving parts, reuses 28.1 code.
3. **`FixRequest` gained `Verbose int` + `Flags FixEntryFlags`** — required by the
   writer/promote/backup helpers (Quiet/EditInPlace/Force + the verbose backup
   notice). `Options` retained for shape-symmetry with `FilterRequest`.
4. **Inspection ops are no-ops at the `RunFix` primitive level** (read-only;
   classified but the caller renders via the existing inspection helpers, per
   LD2). The CLI `header/entry show`, `fixes list/pop/discard/clear` presenters
   call the pkg cores directly (pop/discard need the returned metadata to render
   their message) rather than through `RunFix`. Only the index-mutating ops route
   through `RunFix` in the CLI.
5. **CLI dry-run for non-json edits short-circuits** with the preview (no `RunFix`
   call), preserving the exact pre-28.2 dry-run output (no collect, no stderr
   warnings). The primitive's richer `DryRun` (collect → would-be counts, no
   write — LD5) is exercised directly via `TestRunFix_DryRunWritesNothing`.
6. **Confinement-root exemption model**: `RunFix`'s `writeRoot==""` is the
   dcfhfix explicit-named-subject exemption (skip confinement — the user named
   the file directly). `repoCore.Fix` always passes `MetaDir` (non-empty), so a
   library consumer via `Repo.Fix` cannot reach the exemption (D2/LD3 satisfied:
   the exemption is a CLI-set path, not a request flag).
7. **`main_test.go` header-edit error expectation corrected** ("failed to load
   index" → "failed to write modified index") — a documented call-site fix from
   the relocation (AC3), not a behaviour regression.

## Blockers Encountered
None. All three milestone gates (build + `go test ./...` + `golangci-lint run`)
passed; the pre-commit `-race` gate passed on each commit.

## Deferral Check
- [x] All d-implementation-plan.md steps executed (M1–M3).
- [x] a-task-plan.md success criteria met (primitive + interface, command
  coverage, CLI parity, auto-fix/dry-run/backup, confinement).
- [x] b-requirements FR1–FR7, NFR4, NFR5 addressed; AC5 already resolved (LD7).
- [x] c-design LD1–LD7 followed, with the documented deviations above.
- [x] No planned work deferred. Out of scope (28.3): multi-source recovery
  rebuild. Deferred by design: Manual interactive mode (returns
  `ErrManualModeUnimplemented`).

## Security Review

**State**: error

error: cap exceeded: 1734 production lines > 500

Per `/cwf-implementation-exec` Step 8, the deterministic changeset reviewer is
not invoked when the production-weighted line count exceeds the cap (the
changeset spans the three-milestone primitive + relocation + CLI rewrite). The
verdict is recorded as `error` and the workflow proceeds. Because the
confinement primitive IS this task's security deliverable (D2/NFR4), a
**supplementary focused review** of the security-critical surface
(`fix_run.go` confinement + classification, `fix_header.go`/`fix_backup.go`/
`fix_entry_workflow.go` write paths) was run via the
`cwf-security-reviewer-changeset` agent and is recorded below. The
larger-than-cap changeset should also get a split changeset review in
g-testing-exec (where the diff is re-anchored) or a manual pass.

### Supplementary focused review

**State**: no findings

`cwf-security-reviewer-changeset` (focused on `fix_run.go` confinement +
classification, and the `fix_header.go`/`fix_backup.go`/`fix_entry_workflow.go`/
`fix_promote.go` write paths) verdict:

```cwf-review
state: no findings
summary: Write-confinement primitive is symlink-resolved, fail-closed, boundary-safe; backup dir independently confined; writeRoot="" exemption unreachable from Repo.Fix; os.Create temp writers safe under confinement (audit future unconfined callers).
```

Threats evaluated and cleared:
1. **Selector-steered write outside MetaDir (library path)** — closed.
   `repoCore.Fix` always passes `MetaDir`; `confineWriteDest` (abs →
   EvalSymlinks-parent → `hasPathPrefix`) rejects escapes fail-closed; the
   `.tmp`/`.fix.tmp`/`.pre-fix` siblings derive by suffix from the confined
   dest; the upward-walked backup dir is independently confined via
   `confineWriteDir`.
2. **Fail-closed classification** — confirmed: `readOnlyFixOps` allow-list +
   `default` dispatch arm errors; unknown/zero-value ops are writes.
3. **TOCTOU** — `.pre-fix` uses `O_EXCL`; `os.Create` on the temp/backup paths is
   safe *because* confinement bounds them first.
4. **`writeRoot==""` exemption** — unreachable from `Repo.Fix` (positional param,
   not a `FixRequest` field; `repoCore.Fix` hard-codes `MetaDir`).

**Load-bearing caveat to carry forward (maintenance)**: the `os.Create` temp
writers (`writeHeaderAndEntries`, `copyFile`, `writeRepairedIndex`) are
symlink-safe *only because* the destination is confined before the write — any
future caller that reaches these writers without first passing through
`confineWriteDest`/the explicit-subject trust model must be audited.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-exec 28.2
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
M1 76d65d1a, M2 32c38169, M3 a4ca494e — all green at each gate. See per-milestone
results above.

## Lessons Learned
Encoding the CLI explicit-subject exemption as a positional `writeRoot` param
(not a `FixRequest` field) made the library path structurally unable to reach it
— a trust boundary enforced by the call site rather than a runtime flag check.
