# Fix primitive + dcfhfix restructure - Requirements
**Task**: 28 (feature)

## Task Reference
- **Task ID**: internal-28
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/28-fix-primitive-dcfhfix-restructure
- **Template Version**: 2.1

## Goal
Define functional and non-functional specifications for the `Repo.Fix`
primitive, the relocation of the fsck repair helpers into `pkg/`, and the
multi-source recovery write path.

## Functional Requirements
### Core Features
- **FR1 — Fix primitive on the Repo interface**: `Repo.Fix(ctx, FixRequest) (*FixResult, error)` exists, with `FixRequest{IndexSelectors []string, Commands, DryRun, Backup}` and `FixResult{RepairsApplied, EntriesDiscarded, BackupID}`. `IndexSelectors` reuses the existing plural slice + `ResolveIndexSelectors` vocabulary (per `FilterRequest`), not a new singular selector type. The implementation lands on the same concrete receiver as the sibling primitives (`repoCore`, surfaced through `localRepo`). *Acceptance*: interface and impl compile; a `Fix` call against a known-corrupt fixture returns a `FixResult` with the expected counts. *(Design note: confirm whether `BackupID` carries information a caller cannot get from `fixes list`; drop if redundant.)*
- **FR2 — Command coverage**: `FixRequest.Commands` can express every current dcfhfix operation as it exists today: header (show, edit), entry (show, edit, append, remove), and fixes/backup-stack (list, pop, discard, clear). *Acceptance*: a table maps each existing subcommand to a command variant; each variant has at least one test exercising it through `Repo.Fix`. *(There is no `entry resort` subcommand today — excluded deliberately.)*
- **FR3 — Helpers relocated to pkg/**: `ValidatedEntry`, the entry-processing workflow (`processEntriesWithWorkflow` and its corruption-handling helpers), and the backup-stack helpers (`createBackup`/`listBackups`/promotion) move from `cmd/dcfhfix` into `pkg/`, reachable by the Fix implementation. The bounds-checked accessor (`pkg/format.SafeEntry`) is reused, not re-created. *Acceptance*: the symbols resolve from `pkg/`; `cmd/dcfhfix` imports them rather than defining them; `go build ./...` is clean. *(Scope note: the read/validation/backup helpers move behaviour-preserving; the entry **writer** is corrected under FR9, which is a behaviour change, not a pure move.)*
- **FR4 — dcfhfix as thin translator**: each `dcfhfix` subcommand parses CLI input and emits one or more `FixRequest` batches. Observed behaviour of the **inspection** subcommands (header/entry show, fixes list) and of edit **intent** is unchanged (same exit codes, same backup artefacts). *Acceptance*: `cmd/dcfhfix/main_test.go` and `options_test.go` (which cover argument parsing/dispatch, not write-byte output) pass with only call-site adaptation; any test that encoded the pre-FR9 incomplete-writer output is corrected and that correction is documented as a fix, not a regression.
- **FR5 — Auto-fix batch mode (new)**: `Fix` delivers an auto-fix mode that applies all safe repairs in one batch and writes the result via the single-writer path. This is **new** capability — dcfhfix has no interactive/prompting code today, so there is no "legacy interactive default" to preserve. Per-entry interactive prompting is explicitly **deferred** (out of scope for this task; may be a later mode). *Acceptance*: against a fixture where every repair is "safe", auto-fix produces a valid repaired index and a `FixResult` with the expected counts.
- **FR6 — Dry-run**: `FixRequest.DryRun` previews repairs and reports counts via `FixResult` without writing any index or backup file. *Acceptance*: after a dry-run against a corrupt fixture, no files are created/modified and `FixResult` still reports the would-be counts.
- **FR7 — Backup control**: `FixRequest.Backup` governs whether originals are preserved (backup stack / `.pre-fix-*` sibling), and `FixResult.BackupID` identifies the artefact (stack position or sibling path) when one is made. *Acceptance*: with backup on, the original is recoverable and `BackupID` is non-empty; with backup off, no backup artefact is produced.
- **FR8 — Multi-source recovery rebuild**: a `Fix` batch with multi-source `IndexSelectors` rebuilds `main.idx` from any readable combination of `main.idx` / `cache.idx` / timestamped-cache files. It **reuses** the surviving `pkg/recovery.go` validation/processor and `createPreRecoverySnapshot` helpers; the merge/precedence orchestration (reading multiple sources and emitting one merged `main.idx`) is **net-new** code, not present in `recovery.go` today. *Acceptance*: (a) given a destroyed `main.idx` but intact `cache.idx`, the rebuild produces a valid `main.idx`; (b) given the same path present in two sources, a documented precedence rule resolves the conflict deterministically; (c) given a source with a readable header but truncated body, the rebuild yields a concrete, asserted entry count (not merely "best-effort") and reports the discards.
- **FR9 — Correct variable-length path round-trip (writer fix)**: the relocated write path serialises entries through `TempIndexWriter`/`EntrySerialiser` and round-trips the full variable-length path, replacing the current `appendValidatedEntryToTmpIndex` raw-`O_APPEND` writer that is self-documented as incomplete (drops path bytes). *Acceptance*: an entry edit/append written through `Fix` and re-read reproduces the exact path; a round-trip test over multi-byte and maximum-length paths passes. *(This is the behaviour change FR4 carves out.)*

### User Stories
- **As a** library consumer **I want** a `Repo.Fix` primitive symmetric to `Filter` **so that** repair/recovery is driven through the same batch API as the rest of the library, not only the CLI.
- **As a** forensic/audit user **I want** dcfhfix to keep its current subcommand surface (commands, flags, exit codes) **so that** existing repair workflows and scripts are unaffected — accepting that the entry-write path is corrected (FR9) so repaired indices are now valid where they were previously truncated.
- **As a** user with a damaged repository **I want** `main.idx` rebuilt from whatever index sources survive **so that** I recover state without `rm -rf .dcfh && dcfh init` destroying forensic evidence.

## Non-Functional Requirements
### Performance (NFR1)
- No measurable regression in dcfhfix subcommand latency versus the pre-restructure baseline.
- Recovery rebuild is bounded by total readable index size (single pass; no quadratic re-scan of sources).

### Usability (NFR2)
- dcfhfix CLI surface (subcommands, flags, help text, error messages, exit codes) is unchanged.
- `FixRequest`/`FixResult` follow the naming and shape conventions of the existing `DiffRequest`/`ApplyRequest`/`FilterRequest` primitives.

### Maintainability (NFR3)
- Relocated helpers have single responsibility and a documented `pkg/` surface; `cmd/dcfhfix` shrinks to argument translation.
- The move (FR3) lands as a pure refactor, separable from the API addition (FR1–FR2) and the recovery path (FR8), so each can be reviewed and reverted independently.

### Security (NFR4)
- Repair code continues to assume input may be corrupt: every field read is bounds-checked via `SafeEntry`; forward-progress-on-corruption semantics are preserved.
- **Write-destination confinement**: `Fix` turns the `IndexSelectors` vocabulary — whose fall-through resolves an unrecognised selector to an arbitrary direct path (`RefTypeFile`) — from a read-only filter input into a **write/backup** target. The rebuilt `main.idx` and any backup artefact must be validated to lie within the resolved `MetaDir` (or be the explicitly-named index); a multi-source selector cannot steer a write outside it. *Acceptance*: a `Fix` call whose selector resolves outside `MetaDir` is rejected before any write.
- **Re-anchor migrated gosec rationales**: the existing per-line suppressions (G304 untrusted-path open, G703/G306 taint-tracked write to `.dcfh/`) assume the CLI constrained the destination. After FR3/FR4 the caller of `pkg`-level `Fix` is no longer guaranteed to be the CLI, so the migrated code must re-establish the `.dcfh`/`MetaDir` path invariant in `pkg/` rather than inheriting it from the command line, keeping each rationale true. No new unguarded untrusted-path open is introduced; `golangci-lint run ./...` stays clean.
- The CWF `cwf-security-reviewer-changeset` verdict is recorded for the implementation changeset.

### Reliability (NFR5)
- All index writes use a new temp index + atomic rename via the single `TempIndexWriter` path; no in-place mutation of `main.idx`/`cache.idx`.
- A pre-write snapshot (`createPreRecoverySnapshot`) is taken before any recovery rebuild.
- The recovery write path has fault-injection coverage (the Task 23 atomic-replacement harness is the model) proving no partial/corrupt `main.idx` can be left behind on interruption.
- **Defined corruption-path semantics**: the existing workflow's unfixable-entry cap (100, then abort) and skip-to-next-entry resync (`trySkipToNextEntry`) carry over with explicit `FixResult` reporting. *Acceptance*: when the cap trips, `Fix` returns an error and `FixResult.EntriesDiscarded` reflects the discards; when resync fails mid-file, the documented choice (abort with no write vs. commit the validated prefix) is asserted by test — `Fix` must not silently emit a partial index.

## Constraints
- **Single writer**: `TempIndexWriter` remains the only writer of binaryEntries to disk; auto-fix and recovery serialise via `EntrySerialiser` → `WriteSerialised`.
- **File-type separation**: main/cache stay read-only mmap; temp stays pure-vectorio. No new mmap-for-write path.
- **Format compatibility**: produced indices remain valid per the existing header/checksum/layout contract; no on-disk format change.
- **British spelling** in prose/comments; no AI-tool references in committed code (local branch is fine for CWF files).
- No new third-party dependencies.

## Decomposition Check
- [x] **Time**: >1 week across three milestones (helper move, Fix primitive, recovery path).
- [ ] **People**: Solo.
- [x] **Complexity**: 3+ distinct concerns (cmd→pkg migration, batch API + CLI translation, recovery write path).
- [x] **Risk**: Recovery write path is data-destructive-on-bug and benefits from isolation with its own fault-injection gate.
- [x] **Independence**: Migration is a self-contained prerequisite; Fix primitive builds on it; recovery consumes the primitive.

**Result: 4 of 5 → decomposition strongly recommended (carried from planning; to be confirmed after design).** The requirements review *strengthened* this: the helper move is not a pure refactor (the entry writer is broken and FR9 corrects it), and FR8's merge orchestration is net-new — so the "migration", "Fix primitive + writer fix", and "recovery rebuild" concerns each carry real, independently-reviewable work.

## Acceptance Criteria
- [ ] AC1: `Repo.Fix` + `FixRequest{IndexSelectors,...}`/`FixResult` exist and are exercised by tests through the interface; every current subcommand maps to a command variant (FR1, FR2).
- [ ] AC2: fsck helpers live in `pkg/`; `cmd/dcfhfix` imports rather than defines them; build clean (FR3).
- [ ] AC3: `cmd/dcfhfix` parsing/dispatch tests (`main_test.go`, `options_test.go`) and `recovery_test.go` pass; any test encoding pre-FR9 incomplete-writer output is corrected as a documented fix (FR4).
- [ ] AC4: auto-fix mode, dry-run, and backup control each have passing tests; entry edit/append round-trips the full variable-length path (FR5–FR7, FR9).
- [ ] AC5: multi-source recovery rebuild produces a valid `main.idx` from surviving sources, resolves same-path conflicts by a documented precedence rule, yields an asserted entry count on a truncated-body source, and reports discards — with fault-injection coverage proving atomicity (FR8, NFR5).
- [ ] AC6: corruption-path semantics asserted — unfixable cap reported via `FixResult`, no silent partial index on resync failure (NFR5).
- [ ] AC7: write-destination confinement enforced (selector resolving outside `MetaDir` rejected before write); `golangci-lint run ./...` clean; CWF changeset security verdict recorded (NFR4).

## Status
**Status**: Finished
**Next Action**: /cwf-design-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All functional requirements satisfied via the three subtasks (the Fix primitive,
the pkg/ helper relocation + thin translator, and the multi-source recovery
rebuild); NFRs (single-writer, atomic-rename, confinement, no-new-index-passes)
held on the assembled whole. Two clauses deferred by design: manual interactive
mode (fail-closed `ErrManualModeUnimplemented`) and the under-floor recovery
guard (descoped at 28.3 design).

## Lessons Learned
Writing the parent requirements at the right altitude — capabilities, not CLI
verbs — let the API shape evolve at the subtask level (IndexSelectors/Mode/Flags)
without re-opening the requirements.
