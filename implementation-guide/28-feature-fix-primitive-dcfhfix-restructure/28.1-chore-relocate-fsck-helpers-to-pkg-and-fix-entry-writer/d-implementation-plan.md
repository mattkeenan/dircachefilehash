# Relocate fsck helpers to pkg and fix entry writer - Implementation Plan
**Task**: 28.1 (chore)

## Task Reference
- **Task ID**: internal-28.1
- **Task URL**: N/A (internal task)
- **Parent Task**: 28
- **Branch**: chore/28.1-relocate-fsck-helpers-to-pkg-and-fix-entry-writer
- **Template Version**: 2.1

## Goal
Relocate the dcfhfix fsck helpers into `pkg/` (package `dircachefilehash`) and
replace the incomplete `O_APPEND` entry writer with the single-writer path,
behaviour-preserving except for the writer correction.

## Workflow
Patterns first → Test → Minimal impl → Refactor green → Commit message explains "why"

## Writer approach — RESOLVED: Approach A (uphold parent design D3)
**User decision (2026-06-12)**: route writes through the single
`TempIndexWriter`/`EntrySerialiser` path (D3), and keep the backup-stack
relocation in 28.1. The 4-agent review found three mechanics that A must
solve to be correct; this plan now specifies each so exec does not rediscover
them:

1. **Checksum-type preservation** — `GetCurrentHashType()` (`pkg/hash.go:144`)
   reads `ms.config`'s hash **name** string and defaults SHA-256; the writer
   uses it for both the checksum algorithm and the header `checksum_type`
   (`pkg/temp_index_writer.go:37,99,176`). *Mechanism*: in `newFixMetaStore`,
   reverse-map the subject header's numeric `checksum_type` → algorithm name
   via the existing `HashAlgorithm` registry, and synthesise a minimal
   `Config` whose `hash.default` is that name (built on `initMetaStoreBase`,
   `pkg/metastore.go:208`, which is no-I/O). Then `GetCurrentHashType()`
   returns the subject's type. **Assert** writer-type == subject-header type
   before writing — fail loudly rather than re-hash under the wrong algorithm.
2. **Version policy** — `SetHeaderForWritableIndex` hardcodes
   `CurrentIndexVersion` (v4, `pkg/format/header.go:103`), so A **upgrades**
   any legacy subject to v4. This matches the existing dcfhfix read-old/
   write-new convention (`entry_workflow_main.go:71`). *Decision under A*:
   accept the v4 upgrade as deliberate and documented; **preserve
   `checksum_type`** (orthogonal header field). The legacy test asserts
   "legacy-input → v4-output, `checksum_type` preserved" — NOT version
   preservation. The legacy v2/v3 `ValidatedEntry` fields must be mapped to
   v4 offsets correctly (Dev/Ino widen) — see Step 3.
3. **Rename target** — do **not** reuse `finaliseMainIndex` (renames to
   `main.idx` + cleans cache). Use the relocated `promoteRepairedIndex`
   (`promote.go:124`), which targets the **subject** path and keeps the
   `O_EXCL` preservation guard.

## Key Facts Verified (measure-twice)
- `pkg/` package is `dircachefilehash`, imported by dcfhfix as alias `dcfh`. `cmd/dcfhfix` is `package main`. Relocation ⇒ drop the `dcfh.` qualifier and the `dcfh "..."` import on moved code; resolve `binaryEntry`/`indexHeader` (aliased in `cmd/dcfhfix/format_aliases.go` to `format.Entry`/`format.Header`) to their pkg-native spellings.
- **D6 seam**: `initMetaStoreBase` (`pkg/metastore.go:208`) is no-I/O (no dir creation, no config load). `newFixMetaStore` builds on it and synthesises a minimal `Config` whose `hash.default` maps to the subject's numeric `checksum_type` (so `GetCurrentHashType()` returns it — see RESOLVED §1).
- **Writer target (Approach A / D3)**: `NewBEScanEntry` (`pkg/binary_entry_scan.go:36`) + `SetHash` (line 271) → `EntrySerialiser.Serialise` → `NewTempIndexWriter`/`WriteSerialised`. The reusable pieces are the `runWriteStage` batch loop (`pkg/pipeline_update.go:101-162`), **not** `finaliseMainIndex` — rename via the relocated `promoteRepairedIndex` (`promote.go:124`) to the subject. `BEScanEntry` implements `BinaryEntryInterface`.
- **Backup-stack symbols are real** and live in `cmd/dcfhfix/main.go`: `getBackupDir` (938), `createBackup` (960), `listBackups` (1053), `removeBackupFiles` (1092), `BackupMetadata` (375); `fixesList/Pop/Discard/Clear` (1109–1227) are CLI handlers that call them. `promote.go`: `siblingPreFixPath` (14), `preserveOriginal` (30), `promoteRepairedIndex` (124), `validateEditInPlaceGate` (112), `maxPreFixCollisionSuffix` (21).
- **Coupling challenge**: the relocated workflow/backup/promote functions take `*ParsedOptions` (a `cmd/dcfhfix` CLI type). It cannot move into `pkg/`. → introduce a pkg-level `FixOptions` seam (below).

## Files to Modify
### New files in `pkg/`
- `pkg/fix_validated_entry.go` ← from `cmd/dcfhfix/validated_entry.go`: `ValidatedEntry`, `NewValidatedEntry`, `ApplyFieldFix`.
- `pkg/fix_parse.go` (exec addition) ← the value parsers `parseUint32`/`parseInt64`/`parseTimeValue`/`parseHashValue`/`parseBoolValue` (currently in `main.go`). They are consumed by the relocated `ApplyFieldFix`/`ParseEntryFromJSON`, so they must live in `pkg/` — exported as `ParseUint32`/`ParseInt64`/`ParseTimeValue`/`ParseHashValue`/`ParseBoolValue` because `main.go`'s header/entry field validators also call them (now `dircachefilehash.ParseX`). This coupling was not in the original plan; it is a direct consequence of moving `ApplyFieldFix`.
- `pkg/fix_entry_workflow.go` ← from `entry_processor_workflow.go` + `entry_workflow_main.go` + `entry_append_remove.go`: the processing workflow, `entryOutcome`, `EntryJSON`, append/remove processors. **`appendValidatedEntryToTmpIndex` is NOT moved — it is replaced (Step 3).** `TempIndexWriter` writes its own header (`writePlaceholderHeader`) and finalises entry-count + checksum in `Close()` — so `createTempIndexWithHeader`/`finalizeTempIndex` become redundant under A and must be **deleted**, with `processEntriesWithWorkflow` restructured to drive the `TempIndexWriter` lifecycle (open → `WriteSerialised` → `Close` → promote). Don't leave two parallel header/checksum paths (misalignment finding).
- `pkg/fix_promote.go` ← from `promote.go` (all symbols: `promoteRepairedIndex`/`preserveOriginal`/`siblingPreFixPath`/`validateEditInPlaceGate`/`reportDryRunPreservation`/`maxPreFixCollisionSuffix`). `promoteRepairedIndex` is the writer's rename primitive (RESOLVED §3), so it is consumed here. Reads only `{quiet, edit-in-place, force}` → narrow `FixEntryFlags`.
- **SCOPE REVISION (exec, 2026-06-13): the backup stack is DESCOPED to 28.2.** Exec found `createBackup` reads `backup`+`verbose` (forcing `FixEntryFlags` to grow to `{Quiet,EditInPlace,Force,Backup,Verbose}` — contradicting this plan's narrow-seam intent that "28.2 subsumes the DryRun/Backup concerns"), and that `getBackupDir`/`createBackup` drag `getIndexType`/`copyFile` (also called by the `fixes*` CLI handlers that stay in `cmd/`), forcing those to be exported now. The plan named this the natural split point ("flag to the user before descoping"); user chose descope (recommended). So `BackupMetadata`/`getBackupDir`/`createBackup`/`listBackups`/`removeBackupFiles` + `copyFile`/`saveMetadata`/`loadMetadata`/`getIndexType` and the `fixes*` handlers **stay in `cmd/dcfhfix`** and relocate alongside `Repo.Fix` in 28.2, where Backup/DryRun get their public home. `FixEntryFlags` stays narrow `{Quiet,EditInPlace,Force}` as designed.
- `pkg/fix_options.go` (new): a narrow relocation shim carrying only the option keys the relocated code actually reads from `ParsedOptions` — grep confirms exactly **`quiet`, `edit-in-place`, `force`** (`dry-run`/`backup`/`verbose` are read only in `main.go`, which stays in `cmd/`). Name it narrowly (e.g. `fixEntryFlags{Quiet, EditInPlace, Force bool}`) so it does **not** collide with 28.2's public `FixRequest`/`pkg.Options` — 28.2 subsumes the DryRun/Backup concerns. Replaces `*ParsedOptions` in moved signatures.

### Modified in `cmd/dcfhfix/`
- `validated_entry.go`, `entry_processor_workflow.go`, `entry_workflow_main.go`, `entry_append_remove.go`, `promote.go`: deleted (contents moved) — leave thin re-export shims only if a test needs the old name; prefer updating call sites.
- `main.go`: backup funcs + `fixes*` handlers now call `dcfh.`-qualified relocated functions; build a `dcfh.FixOptions` from `ParsedOptions` at each call site.
- `format_aliases.go`: keep (still used by remaining CLI code) or trim if fully unused.

### Test call-site updates
- `promote_test.go`, `promote_integration_test.go`: call relocated `dcfh.PreserveOriginal`/`dcfh.PromoteRepairedIndex`/`dcfh.SiblingPreFixPath`/`dcfh.ValidateEditInPlaceGate` (export-on-move) with `dcfh.FixOptions`.
- `repair_v4_test.go`, `writepath_test.go`: update to the relocated `ValidatedEntry`/workflow entry points; `writepath_test.go` asserts the **new** writer output (this is the FR9 behaviour change).
- `main_test.go`: `BackupMetadata`/dispatch — adapt imports.

## Implementation Steps
### Step 1: Patterns — study before moving
- [ ] Re-read `runWriteStage`/`finaliseMainIndex` (`pkg/pipeline_update.go:101–198`) and `entry_workflow_main.go:64–170` (the existing read-old/write-new header path) — the writer replacement follows these.
- [ ] Enumerate exactly which `ParsedOptions` fields the relocated functions read (grep `options\.` in the four files + promote.go) → define `FixOptions`.

### Step 2: Pure relocation (behaviour-preserving — separate commit)
- [ ] Create `pkg/fix_options.go` (`FixOptions`); create the three `pkg/fix_*.go` files with moved symbols, dropping `dcfh.` qualifiers, resolving format aliases, swapping `*ParsedOptions`→`*FixOptions`. Keep `appendValidatedEntryToTmpIndex` temporarily so this commit compiles behaviour-identical.
- [ ] Update `cmd/dcfhfix` call sites + tests to the relocated symbols + `FixOptions` mapping.
- [ ] `go build ./...` + full `go test ./...` green — proves no behaviour change.

### Step 3: Writer correction (the FR9 behaviour change — separate commit) — Approach A
- [ ] Add `newFixMetaStore(metaDir string, checksumType uint16) *MetaStore` (in-package): `initMetaStoreBase` + a synthesised minimal `Config` whose `hash.default` = reverse-map of `checksumType` via the `HashAlgorithm` registry (RESOLVED §1). Assert `GetCurrentHashType() == checksumType`.
- [ ] Map each repaired `ValidatedEntry`→`BEScanEntry` **field-by-field** (NOT `NewBEScanEntry`, which wants live `FileInfo`+`Stat_t`): copy the already-wall-encoded `CTimeWall`/`MTimeWall` directly (no re-`encodeWallTime`), Size/Dev/Ino/Mode/UID/GID/FileSize/EntryFlags, set path, `SetHash(hash, hashType)`. For a legacy v2/v3 subject, ensure the v4 widened Dev/Ino land at the correct offsets.
- [ ] Route the workflow through `NewTempIndexWriter`/`WriteSerialised` (batch loop per `runWriteStage`), `Close` to finalise header count + checksum; delete `appendValidatedEntryToTmpIndex`, `createTempIndexWithHeader`, `finalizeTempIndex`.
- [ ] Rename via `promoteRepairedIndex` to the **subject** (temp in `filepath.Dir(subjectAbs)`, `O_EXCL` preserve). On cap/`stop`, discard temp (no rename) — no-partial-index invariant.
- [ ] Migrate `//nolint:gosec` G304/G703/G306 rationales **keeping the honest "path is the user-supplied subject argument, no trust boundary at this layer" wording** (security + robustness both flagged that re-anchoring to a `.dcfh` invariant is FALSE — dcfhfix subjects are arbitrary paths). MetaDir confinement is 28.2's `repoCore.Fix` concern (design D2), not 28.1's.

### Step 4: Tests (see e-testing-plan.md)
- [ ] New: variable-length path round-trip (multi-byte + max-length); abort-discards-temp invariant; **legacy-input → v4-output, `checksum_type` preserved** (Approach A upgrades version to v4 deliberately; assert checksum_type unchanged + Dev/Ino at correct v4 offsets); checksum-type-mismatch assertion (writer refuses if `newFixMetaStore` type ≠ subject); footer-checksum-validates-after-edit.
- [ ] Reconcile `writepath_test.go` to the corrected output (documented as a fix); update relocated-symbol call sites in `promote_test.go`/`promote_integration_test.go`/`repair_v4_test.go`/`main_test.go` (no re-export shims — update call sites directly).
- [ ] Full `go test ./...` + `golangci-lint run ./...` clean.

### Step 5: Validation
- [ ] CWF changeset security review (re-anchored gosec rationales).
- [ ] dcfhfix manual smoke: header/entry/fixes subcommands behave as before; a repaired index now re-reads with correct paths.

## Code Changes
### Before (the incomplete writer — deleted)
```go
// cmd/dcfhfix/entry_processor_workflow.go:156
func appendValidatedEntryToTmpIndex(tmpIndexFile string, ve *ValidatedEntry) error {
    file, _ := os.OpenFile(tmpIndexFile, os.O_WRONLY|os.O_APPEND, 0644)
    entryBytes := (*[unsafe.Sizeof(*ve.Entry)]byte)(unsafe.Pointer(ve.Entry))
    _, err := file.Write(entryBytes[:]) // drops variable-length path; no header/checksum
    return err
}
```
### After (Approach A / D3 — single-writer path, in pkg/)
```go
// ms := newFixMetaStore(dir, subjectChecksumType)   // GetCurrentHashType()==subject
// w  := NewTempIndexWriter(ms, tempPath)            // writes its own header
// for each repaired entry:
//   sbe := beScanEntryFromValidated(ve)             // field-by-field; no re-wall-encode
//   data, _ := serialiser.Serialise(sbe)            // full wire format incl. path
//   batch = append(batch, data); WriteSerialised(batch) in chunks
// w.Close()                                          // finalise count + footer checksum
// promoteRepairedIndex(temp, subject)                // rename to SUBJECT, O_EXCL preserve
//   on cap/stop: discard temp, no rename (no partial index)
```

## Test Coverage
**See e-testing-plan.md for complete test plan**

## Validation Criteria
**See e-testing-plan.md for validation criteria and test results**

## Scope Completion
Both commits (relocation, writer-fix) land in this subtask. The backup-stack
relocation stays in scope per parent design D-section; if the `ParsedOptions`→
`FixOptions` decoupling balloons beyond the estimate, that is the natural split
point — flag to the user before descoping rather than deferring silently.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-plan 28.1
**Blockers**: None — writer approach RESOLVED to A (uphold D3) per user decision; the three blocking mechanics are specified above. Note: A deliberately upgrades legacy v2/v3 subjects to v4 (preserving `checksum_type`); confirmed acceptable as the existing read-old/write-new convention.

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Both milestones landed: M1 relocation (fba94a96, behaviour-preserving), M2 writer
correction (87ed3ec6, routes through `EntrySerialiser`→`WriteSerialised`→atomic rename,
checksum-type seeded+asserted, abort discards temp). Parse helpers absorbed into
`pkg/fix_parse.go`; backup stack descoped to 28.2. Full suite + lint clean. See
f-implementation-exec.md and j-retrospective.md.

## Lessons Learned
Per-path lint carve-outs do not travel with relocated code — the `cmd/dcfhfix/`
errorlint exclusion stopped applying once code moved to `pkg/`, surfacing 11 findings
only on the full `./...` run. Add a full-`golangci-lint` checkpoint to relocation
milestones, distinct from the `--new` staged gate.
