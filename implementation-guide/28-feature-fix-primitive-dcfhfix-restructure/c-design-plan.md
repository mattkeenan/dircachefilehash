# Fix primitive + dcfhfix restructure - Design
**Task**: 28 (feature)

## Task Reference
- **Task ID**: internal-28
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/28-fix-primitive-dcfhfix-restructure
- **Template Version**: 2.1

## Goal
Define the architecture for `Repo.Fix`, the relocation of the fsck helpers
into `pkg/`, the single-writer correction of the entry write path, and the
multi-source recovery rebuild — mirroring the existing `Repo.Filter` wiring.

## Design Priorities
Testability → Readability → Consistency → Simplicity → Reversibility

## Architecture Preferences
Composition over inheritance. Interfaces over singletons. Explicit over implicit.

## Key Decisions

### D1 — `Fix` mirrors `Filter` end-to-end (Consistency)
- **Decision**: Add `Fix(ctx, FixRequest) (*FixResult, error)` to the `Repo` interface, implemented on `*repoCore` exactly as `Filter` is (`pkg/repo_local.go:346`). `repoCore.Fix` resolves selectors via `ResolveIndexSelectors(r.ms.MetaDir, selectors)` and delegates to a package-level `RunFix(ctx, refs, req, warnOut)` in a new `pkg/fix_run.go` (sibling to `filter_run.go`).
- **Rationale**: the primitive set already has a proven shape; copying it keeps the library surface uniform (NFR2) and lets the CLI and library share one entry point.
- **Trade-offs**: `RunFix` carries more behaviour than `RunFilter` (it writes, not just reads), so the symmetry is structural, not line-for-line.

### D2 — Selectors are read-only; write destinations are never selector-derived (Security, closes NFR4)
- **Decision**: `IndexSelectors` resolve **sources** only. No write destination is ever taken from a selector. A recovery rebuild always writes to `r.ms.IndexFile`; an entry/header edit writes a temp sibling of the MetaStore-owned (or explicitly-named subject) index and atomic-renames over it. Before any write, the destination is **canonicalised** (`filepath.Clean`; `filepath.EvalSymlinks` on its parent dir) and both `destAbs` and `metaDirAbs` are absolute, *then* asserted within the resolved `MetaDir` via `hasPathPrefix(destAbs, metaDirAbs)` (`pkg/wire_handler.go:240`). The lexical prefix check alone is insufficient — a path that lexically sits under MetaDir but traverses a symlink must be rejected. Read-only commands (entry-show/header-show) may target an arbitrary `RefTypeFile`; write commands may not escape.
- **Rationale**: the `ResolveIndexSelectors` fall-through types any unknown string as `RefTypeFile` (an arbitrary path, `filter_run.go:143`). Keeping that as a *read* input while pinning *writes* to MetaStore-owned paths removes the "selector steers a write outside the repo" surface entirely, and keeps the migrated G304/G703/G306 rationales true once the caller is no longer guaranteed to be the CLI.
- **Trade-offs**: the dcfhfix CLI, which legitimately repairs an explicitly-named file anywhere, is handled as a distinct "explicit subject" path (D6), not via the MetaDir assertion — the confinement there is "temp sibling of the named subject".

### D3 — Reuse the single-writer path; delete the incomplete raw-append writer (Reliability, FR9)
- **Decision**: repaired entries are converted to the heap entry type (`BEScanEntry`) and serialised through `EntrySerialiser.Serialise`, written via `NewTempIndexWriter` + `WriteSerialised`, finalised with the temp→atomic-rename sequence used by `finaliseMainIndex` (`pkg/pipeline_update.go:170`). `appendValidatedEntryToTmpIndex` (the `O_APPEND` raw-struct writer, `entry_processor_workflow.go:156`) is deleted. Its correctness hole is broader than path bytes: it also writes no valid header `EntryCount`/`Version` and recomputes no footer checksum. The replacement inherits all three from `TempIndexWriter` (correct count, stamped header, incremental checksum) — FR9's path round-trip is then satisfied by construction.
- **Gained invariant (state + test)**: because writes now go to a temp file that is atomic-renamed only on success, an abort on the unfixable cap or a failed resync (`outcome.stop`) **discards the temp and leaves the subject untouched** — there is no partial index. The old `O_APPEND` path had already mutated the temp before the stop. This is a load-bearing reliability property the 28.1 gate must assert (satisfies AC6).
- **Rationale**: the serialiser already emits correct wire format including the variable-length path, restoring the "single writer of binaryEntries" invariant the CLAUDE.md constraints require.
- **Trade-offs**: `NewTempIndexWriter` requires a `*MetaStore`; the explicit-file CLI path has no repo — see D6.

### D4 — Command model: explicit tagged `FixCommand` list (Readability, FR2)
- **Decision**: `FixRequest.Commands []FixCommand`, where `FixCommand` is a tagged struct with an `Op` discriminator and typed payload fields. The variants are exactly today's subcommands (no `resort`):
  - header: `header-show`, `header-edit{Field,Value}` (editable: signature, version, flags, checksum_type)
  - entry: `entry-show`, `entry-edit{Index,Field,Value}` (editable: ctime, mtime, mode, uid, gid, file_size, flag_is_deleted), `entry-append{...}`, `entry-remove{Index}`
  - fixes/backup-stack: `fixes-list`, `fixes-pop`, `fixes-discard`, `fixes-clear`
- **Rationale**: a flat tagged struct (vs an interface) keeps the batch JSON-inspectable and the translation from CLI args mechanical; editable field sets come straight from the existing `ApplyFieldFix` switch and `headerFieldEditors` map, so coverage is verifiable against the source. The `entry-append` payload is **not** open: it reuses the existing `EntryJSON` struct (`cmd/dcfhfix/entry_append_remove.go:13`), which moves into `pkg/` with the relocation.
- **Input validation (Security, FR4(e))**: `Field` is checked against the closed allow-list *before* dispatch — an unknown field is rejected, never silently no-op'd; a `Value` parse failure is returned as an error, never written. The `entry-append` relative path is validated/confined like any other write input (no `../` escape into the produced index).
- **Trade-offs**: a tagged struct has unused fields per variant; acceptable for a small, closed command set.

### D5 — Reuse `FixMode` and the recovery validators (Simplicity, FR5/FR8)
- **Decision**: reuse the existing `FixMode` constants `FixModeNone`/`FixModeAuto`/`FixModeManual` (`pkg/recovery.go:49`). This task delivers `FixModeAuto` (apply all safe repairs in one batch). `FixModeManual` (per-entry interactive prompting) is defined but **deferred** — `RunFix` returns "interactive mode not implemented" for it. The recovery rebuild reuses `RecoveryValidationProcessor`/`ValidationProcessor` (`pkg/recovery.go:330`) and the `(*MetaStore).createPreRecoverySnapshot` **method** (`pkg/recovery.go:350` — a MetaStore method, so the repo-less CLI path inherits the same synthesised-MetaStore caveat as the writer, D6) unchanged.
- **Rationale**: no new mode enum, no new validation/snapshot code; the net-new code is confined to merge orchestration.
- **Trade-offs**: shipping a `Manual` enum value that errors is mild dead-surface, justified by keeping the door open without designing the prompt UX now.

### D6 — Two construction paths into one `RunFix` (Consistency, FR4)
- **Decision**: both callers build the same `FixRequest` and call `RunFix`:
  - `repoCore.Fix` (library): MetaDir-resolved refs, writes to `r.ms.IndexFile`, D2 assertion active.
  - `cmd/dcfhfix` (CLI): one `RefTypeFile` selector for the explicitly-named subject; the subject doubles as the write target (temp sibling + rename in the subject's directory).
- **The writer's MetaStore dependency is a two-field seam, not a full repo**: `NewTempIndexWriter` consumes only `ms.signature` (the constant `dcfh`, `metastore.go:214`) and `ms.GetCurrentHashType()`. So the CLI path synthesises a `*MetaStore` carrying just those two values; it does **not** need a real `.dcfh`. It inherits the existing **read-old / write-new** header convention already used by dcfhfix (`entry_workflow_main.go:71`): read the source header, write a freshly-stamped current-version header preserving signature/flags.
- **Checksum-type correctness (Reliability)**: the synthesised MetaStore must seed its hash type from the **subject header's `checksum_type`**, not the SHA-256 default `GetCurrentHashType()` falls back to (`temp_index_writer.go:46`). `RunFix` asserts `writer-checksum-type == subject-header-checksum-type` before writing, so a repair never silently re-hashes an index under a different algorithm.
- **Version policy (open decision for 28.1)**: the existing dcfhfix convention upgrades repaired entries to the current (v4) on-disk version. Requirements say "no on-disk format change". These are in mild tension for legacy v2/v3 inputs. **Recommended resolution**: always preserve `checksum_type` (a correctness requirement); follow the existing write-new version behaviour but make it an explicit, documented, tested choice in 28.1 (not an accidental upgrade) — or preserve the source version if a round-trip test shows the upgrade breaks legacy readers.
- **Backup-dir caveat**: `getBackupDir` (`cmd/dcfhfix/main.go:938`) walks *up* from the subject to find a `.dcfh`. For an out-of-repo subject this can resolve a `.dcfh` in an unexpected ancestor; 28.1 must preserve/tighten this deliberately, and the temp file is created in `filepath.Dir(subjectAbs)` with a non-attacker-derived base name, renaming only to the canonicalised subject path.
- **Rationale**: keeps one orchestration core; the CLI shrinks to arg-parsing + `FixCommand` construction (FR4).
- **Trade-offs**: rooting a synthesised MetaStore at an arbitrary directory with no `.dcfh` is the one genuinely new seam (now right-sized to two fields + the checksum-type/version policy above), not a full-repo synthesis.

## System Design

### Component Overview
- **`pkg/fix_run.go`** (new): `FixRequest`/`FixResult`/`FixCommand` types; `RunFix(ctx, refs, req, warnOut)` orchestration; mode dispatch; D2 destination assertion.
- **`pkg/fix_entry.go`** (moved from `cmd/dcfhfix`): `ValidatedEntry` + `ApplyFieldFix`, the entry-processing workflow (`processEntriesWithWorkflow`, `processSingleEntry`, `trySkipToNextEntry`, the unfixable cap), retargeted onto the D3 writer.
- **`pkg/fix_backup.go`** (moved from `cmd/dcfhfix`): backup-stack helpers (`createBackup`/`listBackups`/promotion) + `.pre-fix-*` sibling logic.
- **`pkg/recovery.go`** (reused): validators + `createPreRecoverySnapshot`, plus a new `mergeSourcesIntoEntries(refs, precedence)` merge function (the net-new FR8 orchestration).
- **`repoCore.Fix`** (`pkg/repo_local.go`): thin selector-resolve + delegate.
- **`cmd/dcfhfix/*`**: arg-parse → `FixCommand` list → `RunFix`; subcommand surface unchanged (FR4).

### Data Flow
**Entry/header edit (CLI or library)**
1. Caller builds `FixRequest{IndexSelectors, Commands, Mode:Auto, DryRun, Backup}`.
2. `RunFix` resolves refs; asserts write destination ⊆ MetaDir (D2); optional `createPreRecoverySnapshot` / backup (FR7).
3. Entry workflow validates each source entry via `SafeEntry`, applies `ApplyFieldFix`, accumulates repaired `BEScanEntry` values; unfixable cap + resync semantics reported into `FixResult` (NFR5).
4. If not `DryRun`: serialise → `WriteSerialised` → temp → atomic rename. Else: discard, report counts.

**Recovery rebuild (multi-source)**
1. `FixRequest` with multiple `IndexSelectors` (main, cache, timestamped caches) + a recovery command.
2. `createPreRecoverySnapshot` of all `.idx` files. **Snapshot success is a hard precondition**: on snapshot error `RunFix` returns without touching `r.ms.IndexFile`.
3. `mergeSourcesIntoEntries`: read each source, validate, union by relative path; conflict resolved by **precedence newest-first: timestamped caches (newest→oldest) > `cache.idx` > `main.idx`** (mirrors the cache-as-delta-over-main model). Truncated/short sources contribute their readable validated prefix; discards counted.
4. **Empty/under-floor guard**: if the merged validated set is empty (or below a threshold relative to the pre-recovery snapshot's entry count), abort *before* the atomic rename and leave originals intact, reporting discards — never overwrite a recoverable index with a header-only empty one.
5. Otherwise write merged set to `r.ms.IndexFile` via the D3 single-writer path; atomic rename.

## Interface Design

### Data Models
```go
type FixRequest struct {
    Options        Options      `json:"options"`
    IndexSelectors []string     `json:"index_selectors"`
    Repository     string       `json:"repository,omitempty"`
    Commands       []FixCommand `json:"-"`
    Mode           FixMode      `json:"-"` // Auto delivered; Manual deferred
    DryRun         bool         `json:"dry_run,omitempty"`
    Backup         bool         `json:"backup,omitempty"`
}

type FixResult struct {
    IndexFilesProcessed int    `json:"index_files_processed"`
    RepairsApplied      int    `json:"repairs_applied"`
    EntriesDiscarded    int    `json:"entries_discarded"`
    BackupID            string `json:"backup_id,omitempty"` // stack position or sibling path
}

type FixOp string // "header-edit","entry-edit","entry-remove","fixes-pop",...

type FixCommand struct {
    Op     FixOp
    Field  string     // header-edit / entry-edit
    Value  string     // header-edit / entry-edit
    Index  int        // entry-edit / entry-remove
    Append *EntryJSON // entry-append — reuses existing EntryJSON (entry_append_remove.go:13)
}
```
*Design decision (was a carried requirements note): `BackupID` is retained **only if** 28.2 confirms it exposes information not already available via `fixes-list` (`main.go:1109`); otherwise it is dropped. Tracked as an explicit 28.2 acceptance criterion rather than carried indefinitely.*

### API
```go
// On the Repo interface (pkg/repo.go), beside Filter:
Fix(ctx context.Context, req FixRequest) (*FixResult, error)

// Package-level core (pkg/fix_run.go), shared by repoCore.Fix and the CLI:
func RunFix(ctx context.Context, refs []IndexRef, req FixRequest, warnOut io.Writer) (*FixResult, error)
```

## Constraints
- Single writer (`TempIndexWriter`/`EntrySerialiser`), main/cache read-only mmap, temp pure-vectorio — all preserved (D3).
- No on-disk format change; produced indices satisfy the existing header/checksum/layout contract.
- No new third-party dependencies. British spelling in prose/comments.

## Decomposition Check
- [x] **Time**: >1 week — three concerns, each with its own review + tests.
- [ ] **People**: Solo.
- [x] **Complexity**: 3+ distinct concerns — (1) helper relocation + writer fix (D3, FR9), (2) `Fix` primitive + command model + CLI translation (D1/D4/D6), (3) recovery merge (D5/FR8).
- [x] **Risk**: the recovery write path (data-destructive) warrants isolation with its own fault-injection gate; the writer correction (D3/D6, now right-sized to a two-field MetaStore seam + checksum-type assertion) needs its own round-trip/version-policy gate.
- [x] **Independence**: clean dependency order — relocation+writer (prereq) → primitive+CLI → recovery merge (consumes the primitive).

**Result: 4 of 5 → decomposition recommended.** Concrete subtask boundaries now fall out of the design:
- **28.1 (chore/refactor + writer fix)**: relocate `ValidatedEntry`/workflow/backup-stack to `pkg/fix_entry.go`+`pkg/fix_backup.go`; replace `appendValidatedEntryToTmpIndex` with the single-writer path (D3/FR9); resolve the D6 minimal-MetaStore wrinkle. dcfhfix tests + a new path round-trip test are the gate. **Prerequisite.**
- **28.2 (feature)**: `FixRequest`/`FixResult`/`FixCommand`, `Repo.Fix` on `repoCore`, `RunFix`, D2 destination confinement, CLI re-expressed as `FixCommand` batches, auto-fix mode (D1/D4/D5/D6).
- **28.3 (feature)**: `mergeSourcesIntoEntries` + multi-source recovery rebuild with precedence/conflict/truncated-source edge cases and fault-injection coverage (FR8/NFR5).

## Validation
- [x] Design review completed (4-agent map/reduce — see checkpoint).
- [ ] Architecture approved by user.
- [x] Integration points verified against source (signatures confirmed via Explore).

## Status
**Status**: Finished
**Next Action**: Decide decomposition, then /cwf-new-subtask 28.1 (or /cwf-implementation-plan 28 if kept whole)
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
The design's central decision — decompose into a behaviour-preserving migration,
then the primitive, then the destructive recovery path — held end to end. `Fix`
landed symmetric to `Filter`; the single `RunFix` engine is the only writer;
recovery confinement + snapshot-readback were specified here and implemented as
designed in 28.3.

## Lessons Learned
For a coordinating parent, the decomposition *is* the design artefact: getting the
dependency order (migration → primitive → recovery) and the risk isolation right
at design time is what made the three subtasks compose without rework.
