# Fix primitive and CLI restructure - Requirements
**Task**: 28.2 (feature)

## Task Reference
- **Task ID**: internal-28.2
- **Task URL**: N/A (internal task)
- **Parent Task**: 28
- **Branch**: feature/28.2-fix-primitive-and-cli-restructure
- **Template Version**: 2.1

## Goal
Specify the `Repo.Fix` primitive, the dcfhfix-as-thin-translator restructure,
and write-destination confinement for **single-source** repairs. These refine
parent task 28's FR1, FR2, FR3 (remainder), FR4–FR7 and NFR4. Multi-source
recovery rebuild (parent FR8) and Manual interactive mode are explicitly out of
scope (28.3 / deferred).

## Functional Requirements
### Core Features
- **FR1 — Fix primitive on the Repo interface** (parent FR1): `Repo.Fix(ctx, FixRequest) (*FixResult, error)` exists on the same concrete receiver as `Filter` (`repoCore`, surfaced through `localRepo`). `FixRequest` reuses the plural `IndexSelectors []string` + `ResolveIndexSelectors` vocabulary; `FixResult` carries `{RepairsApplied, EntriesDiscarded}` (the `BackupID` field is dropped — see FR7/AC5). (No per-file count: 28.2 is single-source, so it would be tautological; a multi-source count, if needed, is added with FR8 in 28.3.) *Acceptance*: interface + impl compile; a `Fix` call against a known-corrupt single-source fixture returns a `FixResult` with the expected counts, exercised **through the interface** (not the concrete type).
- **FR2 — Command coverage** (parent FR2): `FixRequest.Commands []FixCommand` expresses every current dcfhfix operation: header (show, edit), entry (show, edit, append, remove), fixes/backup-stack (list, pop, discard, clear). Each variant is classified **read-only or write** (the classification drives the NFR4 confinement check, which applies only to write variants). *Acceptance*: a table maps each subcommand to a `FixCommand` variant **and its read/write class**; each variant has ≥1 test exercising it through `Repo.Fix`. No `entry resort` (does not exist today).
- **FR3 — Backup-stack relocation to pkg/** (parent FR3 remainder, descoped from 28.1): `createBackup`, `getBackupDir`, `listBackups`, `getIndexType`, `copyFile`, and the fixes-stack ops (`fixesList`/`fixesPop`/`fixesDiscard`/`fixesClear`) move from `cmd/dcfhfix` into `pkg/`, behaviour-preserving. *Acceptance*: symbols resolve from `pkg/`; `cmd/dcfhfix` imports rather than defines them; `go build ./...` clean; existing backup behaviour unchanged.
- **FR4 — dcfhfix as thin translator** (parent FR4): each subcommand parses CLI input and emits one or more `FixRequest` batches via `RunFix`. Inspection subcommands (header/entry show, fixes list) and edit intent are unchanged: same exit codes, same backup artefacts, same help text. *Acceptance*: `main_test.go` and `options_test.go` pass with **call-site-only** adaptation; any test that encoded pre-FR9 incomplete-writer output is corrected and documented as a fix, not a regression.
- **FR5 — Auto-fix batch mode** (parent FR5): `Fix` delivers `FixModeAuto` — applies all safe repairs in one batch, writes via the single-writer path. *Acceptance*: against an all-safe-repair fixture, auto-fix produces a valid repaired index and a `FixResult` with expected counts. `FixModeManual` is defined but returns a **typed "interactive mode not implemented" error** (non-zero CLI exit, no partial write — not a panic or silent no-op), asserted by a test (deferred mode, but its error path is covered).
- **FR6 — Dry-run** (parent FR6): `FixRequest.DryRun` previews repairs and reports counts via `FixResult` without writing any index or backup file. *Acceptance*: after a dry-run against a corrupt fixture, no files are created/modified, and `FixResult` still reports the would-be counts.
- **FR7 — Backup control** (parent FR7): `FixRequest.Backup` governs whether originals are preserved (backup stack / `.pre-fix-*` sibling). **Open decision (parent design note), resolved in design LD7**: `FixResult.BackupID` is **dropped** — the "near-zero cost" premise was false (`createBackup` returns only `error`) and the proposed id conflated the always-on `.pre-fix-*` sibling with the `--backup`-gated stack entry. Backup discovery/rollback is `fixes-list` + `fixes-pop`. *Acceptance*: with backup on, the original is recoverable via `fixes-list`/`fixes-pop`; with backup off, no backup artefact is produced.

### User Stories
- **As a** library consumer **I want** a `Repo.Fix` primitive symmetric to `Filter` **so that** single-source repair is driven through the same batch API as the rest of the library, not only the CLI.
- **As a** forensic/audit user **I want** dcfhfix to keep its current subcommand surface (commands, flags, exit codes, backup artefacts) **so that** existing repair workflows are unaffected — now writing valid indices via the corrected writer (FR9, landed in 28.1).

## Non-Functional Requirements
### Performance (NFR1)
- No measurable regression in dcfhfix subcommand latency versus the pre-restructure baseline (the translation layer adds no extra index passes).

### Usability (NFR2)
- dcfhfix CLI surface (subcommands, flags, help, error messages, exit codes) unchanged.
- `FixRequest`/`FixResult` follow the naming/shape conventions of `FilterRequest`/`DiffRequest`/`ApplyRequest`.

### Maintainability (NFR3)
- `cmd/dcfhfix` shrinks to argument translation + `FixCommand` construction; relocated helpers have a documented single-responsibility `pkg/` surface.
- The backup-stack move (FR3) lands as a behaviour-preserving refactor, reviewable independently of the API addition (FR1–FR2).

### Security (NFR4) — write-destination confinement (parent NFR4 / AC7)
- `Fix` turns the `IndexSelectors` vocabulary (whose fall-through resolves an unrecognised selector to an arbitrary `RefTypeFile` path) from a read-only filter input into a **write/backup** target. Every write destination must be validated to lie within the resolved `MetaDir` — or be the single index file explicitly named on the CLI. A selector embedded in `IndexSelectors` cannot steer a write outside `MetaDir`, and the explicit-named-subject exemption is one validated path, **not** a flag that disables confinement wholesale (a library consumer cannot widen it into a general bypass).
- The check is **canonicalised**, not merely lexical: resolve symlinks before comparing, then assert the destination is a prefix of the resolved `MetaDir` on absolute paths — a path that lexically sits under `MetaDir` but traverses a symlink is rejected. Read-only commands (header/entry show) may target an arbitrary file; write commands may not. **Fail-closed**: a write target that does not yet exist (e.g. a new `.pre-fix-*` sibling) is confined by resolving its existing parent and confining the basename; any resolve error rejects the write. *(Reuses the existing `resolveRel`→`hasPathPrefix` guard pattern; the design phase specifies the not-yet-exists / resolve-error handling, which that precedent does not itself model.)*
- Migrated gosec rationales (G304/G703/G306) are re-anchored in `pkg/` to the `MetaDir`/explicit-subject invariant, since the caller is no longer guaranteed to be the CLI. No new unguarded untrusted-path open. *Acceptance*: see AC7 (both the reject and bounded-permit cases); `golangci-lint run ./...` clean; CWF `cwf-security-reviewer-changeset` verdict recorded.

### Reliability (NFR5)
- All writes use temp index + atomic rename via the single `TempIndexWriter` path (landed 28.1); no in-place mutation of `main.idx`/`cache.idx`.
- Corruption-path semantics surface through `FixResult`: the unfixable-entry cap and `trySkipToNextEntry` resync carry over with explicit reporting. The cap logic exists in multiple copies in `pkg/fix_entry_workflow.go` today (≈lines 218/484/522); the primitive must route through a **single** cap-enforcement site so the three cannot diverge. *Acceptance*: when the cap trips, `Fix` returns an error and `FixResult.EntriesDiscarded` reflects the discards, with a test pinning the exact boundary (the cap currently trips at `count > max`, i.e. the 101st unfixable entry); on resync failure mid-file, no silent partial index is emitted (abort-discards-temp, asserted in 28.1, holds through the primitive). A dry-run that also trips the cap surfaces the count/error and writes nothing (FR6 × NFR5 compose).

## Constraints
- Single writer (`TempIndexWriter`); main/cache read-only mmap; temp pure-vectorio. No new mmap-for-write path.
- No on-disk format change; produced indices satisfy the existing header/checksum/layout contract.
- British spelling in prose/comments; no new third-party dependencies.
- **Out of scope**: multi-source recovery rebuild + `mergeSourcesIntoEntries` (parent FR8 → 28.3); Manual interactive mode (deferred).

## Decomposition Check
- [ ] **Time**: 3-4 days, <1 week. No.
- [ ] **People**: Solo. No.
- [ ] **Complexity**: Cohesive concern (primitive + its CLI); parent already decomposed. No.
- [ ] **Risk**: D2 confinement isolated by its own AC + test. No.
- [ ] **Independence**: CLI depends on the primitive; not separable. No.

**Result: 0 of 5 → 28.2 correctly sized; no further decomposition.**

## Acceptance Criteria
- [ ] AC1 (parent AC1): `Repo.Fix` + `FixRequest{IndexSelectors,...}`/`FixResult` exist and are exercised through the interface; every current subcommand maps to a `FixCommand` variant (FR1, FR2).
- [ ] AC2 (parent AC2): backup-stack helpers live in `pkg/`; `cmd/dcfhfix` imports rather than defines them; build clean (FR3).
- [ ] AC3 (parent AC3): `cmd/dcfhfix` parsing/dispatch tests (`main_test.go`, `options_test.go`) pass with call-site-only adaptation; any test encoding pre-FR9 output is corrected as a documented fix (FR4).
- [ ] AC4 (parent AC4): auto-fix, dry-run (writes nothing — no `.fix.tmp`/`.pre-fix-*`/`fixes/` artefact), and backup control each have passing tests through `Repo.Fix` (FR5–FR7); `FixModeManual` returns a typed error with no partial write (FR5).
- [x] AC5: `BackupID` keep/drop decision made and recorded with rationale — **dropped** (design LD7); rollback is `fixes-list`+`fixes-pop` (FR7).
- [ ] AC6 (parent AC6): corruption-path semantics asserted via `FixResult` — single cap-enforcement site, exact-boundary test (101st), no silent partial index; dry-run × cap composes (count/error, no write) (NFR5).
- [ ] AC7 (parent AC7): write-destination confinement enforced — **both** sides tested: (a) a selector resolving outside `MetaDir` (incl. via a symlinked parent) is rejected before any write; (b) the explicit-named-subject exemption permits the CLI subject yet a selector cannot reach that same out-of-`MetaDir` location. `golangci-lint run ./...` clean; CWF changeset security verdict recorded (NFR4).

## Status
**Status**: Finished
**Next Action**: /cwf-design-plan 28.2
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
FR1–FR7, NFR4, NFR5 all addressed. AC5 (`BackupID`) resolved by dropping it —
`fixes-list` already exposes the equivalent (LD7). AC1–AC7 covered by tests; see
g-testing-exec.md.

## Lessons Learned
Pinning the `BackupID` redundancy as an explicit AC (rather than an open design
note) forced a clean drop instead of speculative scope.
