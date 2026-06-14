# Fix primitive and CLI restructure - Plan
**Task**: 28.2 (feature)

## Task Reference
- **Task ID**: internal-28.2
- **Task URL**: N/A (internal task)
- **Parent Task**: 28
- **Branch**: feature/28.2-fix-primitive-and-cli-restructure
- **Baseline Commit**: a0bcf0e0da42922426447120b0d34a613eb34885
- **Template Version**: 2.1

## Goal
Add a `Repo.Fix` primitive (symmetric to `Repo.Filter`) and re-express the
dcfhfix CLI as a thin translator that builds `FixCommand` batches and calls one
shared `RunFix`, with auto-fix, dry-run, backup control, and write-destination
confinement — single-source repairs only (multi-source recovery rebuild is 28.3).

## Success Criteria
- [ ] `Repo.Fix(ctx, FixRequest) (*FixResult, error)` exists on `repoCore`/the `Repo` interface and is exercised through the interface; `FixRequest`/`FixResult`/`FixCommand` follow the `FilterRequest` shape (FR1).
- [ ] Every current dcfhfix subcommand (header show/edit, entry show/edit/append/remove, fixes list/pop/discard/clear) maps to a `FixCommand` variant with a test through `Repo.Fix` (FR2).
- [ ] dcfhfix CLI surface unchanged — `main_test.go`/`options_test.go` pass with call-site-only adaptation; same exit codes and backup artefacts (FR4).
- [ ] Auto-fix (FR5), dry-run (FR6, writes nothing), and backup control (FR7) each have passing tests; the backup stack lives in `pkg/` and `cmd/dcfhfix` imports it (FR3 remainder).
- [ ] A selector resolving outside `MetaDir` is rejected before any write (D2/NFR4/AC7); `golangci-lint run ./...` clean; CWF changeset verdict recorded.

## Original Estimate
**Effort**: 3-4 days
**Complexity**: High
**Dependencies**: 28.1 (`pkg/` fsck helpers + single-writer path) — landed at baseline a0bcf0e0.

## Major Milestones
1. **Backup-stack relocation** (FR3 remainder, descoped from 28.1): move `createBackup`/`getBackupDir`/`listBackups`/`getIndexType`/`copyFile` + fixes-stack ops into `pkg/fix_backup.go`, behaviour-preserving; `cmd/dcfhfix` imports them. Own regression gate.
2. **Fix primitive core**: `FixRequest`/`FixResult`/`FixCommand` + `RunFix` in `pkg/fix_run.go`; `repoCore.Fix` thin selector-resolve + delegate (D1); D2 write-destination confinement; FixModeAuto + dry-run (D5 reuse — Manual deferred).
3. **CLI re-expression + gate**: each subcommand parses args → `FixCommand` batch → `RunFix` (D6 two-field synthesised MetaStore for the explicit-subject path); auto-fix/dry-run/backup tests; full suite + lint + security review green.

## Risk Assessment
### High Priority Risks
- **Write-destination confinement (D2) is security-critical**: `IndexSelectors` fall-through types any string as `RefTypeFile`; a write steered outside `MetaDir` (including via a symlinked parent) is the threat. Impact: data write outside the repo.
  - **Mitigation**: canonicalise dest (`filepath.Clean` + `EvalSymlinks` on parent), assert `hasPathPrefix(destAbs, metaDirAbs)` before write; explicit reject-before-write test (AC7). Read commands may target arbitrary `RefTypeFile`; writes may not.
- **CLI behaviour drift**: re-expressing subcommands as batches risks changing exit codes / backup artefacts / output bytes.
  - **Mitigation**: keep `main_test.go`/`options_test.go` green with call-site-only adaptation; any test encoding pre-FR9 incomplete-writer output is corrected as a documented fix, not a silent change.

### Medium Priority Risks
- **Synthesised MetaStore seam (D6)** for the repo-less CLI subject path (two fields: signature + checksum-type).
  - **Mitigation**: pattern already proven by 28.1's `newFixMetaStore`; reuse it, don't re-invent.
- **`BackupID` redundancy** (open design note, FR1/FR7): may duplicate `fixes-list`.
  - **Mitigation**: decide explicitly in requirements/design — keep only if it exposes info `fixes-list` cannot; otherwise drop. Tracked as an AC.

## Dependencies
- 28.1 landed (baseline a0bcf0e0): `pkg/fix_{options,parse,validated_entry,entry_workflow,promote}.go` + single-writer path + `newFixMetaStore`.
- Reuses `ResolveIndexSelectors`, `RunFilter` shape, `FixMode` constants, `SafeEntry`, `EntrySerialiser`/`TempIndexWriter`.

## Constraints
- Single writer (`TempIndexWriter`), main/cache read-only mmap, temp pure-vectorio — preserved.
- No on-disk format change; British spelling; no new third-party deps.
- Multi-source recovery rebuild (FR8) and `mergeSourcesIntoEntries` are **out of scope** — 28.3. Manual interactive mode deferred.

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [ ] **Time**: 3-4 days, <1 week. No.
- [ ] **People**: Solo. No.
- [ ] **Complexity**: 3 milestones but one cohesive concern (the Fix primitive + its CLI), tightly coupled. The parent (task 28) already decomposed this into 28.1/28.2/28.3; 28.2 is the agreed unit. No further split.
- [ ] **Risk**: D2 confinement is the one high-risk item; isolated by its own AC + test, not a separate task. No.
- [ ] **Independence**: CLI depends on the primitive; not separable. No.

**Result: 0 of 5 → no further decomposition. 28.2 is correctly sized.**

## Status
**Status**: Finished
**Next Action**: /cwf-requirements-plan 28.2
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All 5 success criteria met. Delivered in ~1 day vs the 3–4 day estimate (−70%):
28.1's groundwork turned the "High complexity" core into assembly of proven
parts. 3 milestones, each its own commit + gate (M1 `76d65d1a`, M2 `32c38169`,
M3 `a4ca494e`). See j-retrospective.md.

## Lessons Learned
"High complexity" over-predicted effort once the predecessor (28.1) de-risked
the single-writer path and confinement helper. Distinguish inherent from
*residual* complexity after dependencies land.
