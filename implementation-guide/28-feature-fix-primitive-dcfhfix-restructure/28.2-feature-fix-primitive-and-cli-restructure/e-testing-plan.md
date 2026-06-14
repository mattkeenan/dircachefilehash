# Fix primitive and CLI restructure - Testing Plan
**Task**: 28.2 (feature)

## Task Reference
- **Task ID**: internal-28.2
- **Task URL**: N/A (internal task)
- **Parent Task**: 28
- **Branch**: feature/28.2-fix-primitive-and-cli-restructure
- **Template Version**: 2.1

## Goal
Define the test contract for the `Repo.Fix` primitive, the backup-stack relocation,
and the dcfhfix-as-translator restructure (single-source). Maps to AC1–AC7 in
b-requirements-plan.md and the LD1–LD7 decisions in c-design-plan.md.

## Test Strategy
### Test Levels
- **Unit**: `confineWriteDest`/`confineWriteDir` (the security-critical primitive); the shared cap predicate; the relocated pure backup-stack cores; `FixOp` classification (fail-closed default).
- **Integration**: full `Repo.Fix` paths through the **interface** (`Repo`, not `*repoCore`) — header/entry edit/append/remove, dry-run, backup — re-reading the produced index via the production loader; the dcfhfix CLI handlers end-to-end.
- **Regression**: the entire existing `cmd/dcfhfix` + `pkg` suite after M1 (relocation) and again after M2/M3.
- **Static/security**: `golangci-lint run ./...` (gosec floor) + CWF `cwf-security-reviewer-changeset`.

### Test Coverage Targets
- **Critical paths — 100%**: `confineWriteDest`/`confineWriteDir` (both accept and reject branches), the dry-run gate (no-write), the write-vs-read routing, the cap predicate.
- **Regression**: no reduction in existing `cmd/dcfhfix`/`pkg` coverage; every relocated symbol still exercised (via CLI presenters + new pkg-core tests, not shims).
- **Edge cases**: confinement bypass attempts, cap boundary, resync failure, legacy stub preservation.

## Test Cases
### Functional Test Cases

- **TC-R1 — M1 relocation is behaviour-preserving (regression gate, FR3/AC2)**
  - **Given**: backup-stack cluster split into pkg cores + CLI presenters; call sites updated.
  - **When**: `go build ./...` and `go test ./...` run.
  - **Then**: full suite green, dcfhfix CLI output bytes/exit codes unchanged — proves the relocation changed no behaviour.

- **TC-1 — `Repo.Fix` exists and every subcommand maps to a `FixCommand` (FR1/FR2/AC1)**
  - **Given**: a known-corrupt single-source fixture and a `localRepo` opened on its MetaDir.
  - **When**: a `FixRequest` is run **through the `Repo` interface** for each `FixOp` (header show/edit, entry show/edit/append/remove, fixes list/pop/discard/clear).
  - **Then**: each variant executes and returns a `FixResult` with the expected `{RepairsApplied, EntriesDiscarded}`; a mapping table (subcommand→`FixOp`→class) is asserted complete.

- **TC-2 — Entry edit round-trips the full variable-length path through `Fix` (FR9 carry, AC4)**
  - **Given**: an index with a CJK and a maximum-length path.
  - **When**: an `entry-edit` is applied through `Repo.Fix` and the produced index re-read via the production loader.
  - **Then**: every path is byte-identical (28.1's single-writer path, now driven by the primitive).

- **TC-3 — Auto-fix produces a valid repaired index + counts (FR5/AC4)**
  - **Given**: a fixture where all repairs are "safe".
  - **When**: `FixModeAuto` runs.
  - **Then**: the produced index passes production checksum-validation on load; `FixResult` counts match.

- **TC-4 — Dry-run writes nothing, reports would-be counts (FR6/AC4)**
  - **Given**: a corrupt fixture.
  - **When**: `FixRequest{DryRun:true}` runs.
  - **Then**: no `.fix.tmp`, no `.pre-fix-*`, no `fixes/` artefact created/modified; `FixResult` still reports the would-be counts.

- **TC-5 — `--backup --dry-run` composes (FR6×FR7/AC4)**
  - **Given**: backup enabled and dry-run set together.
  - **When**: the fix runs.
  - **Then**: no backup artefact is produced (the design dry-run gate and `createBackup`'s own `!backup` early-return do not conflict); subject untouched.

- **TC-6 — Backup control on/off; recovery via `fixes-list`+`fixes-pop` (FR7/AC4)**
  - **Given**: a valid subject.
  - **When**: a write op runs with `Backup:true`, then with `Backup:false`.
  - **Then**: with backup on, the original is recoverable via `fixes-list`/`fixes-pop`; with backup off, no backup artefact. (No `BackupID` field exists — LD7.)

- **TC-7 — `FixModeManual` returns a typed error, no write (FR5)**
  - **Given**: a valid subject.
  - **When**: `FixRequest{Mode:FixModeManual}` runs.
  - **Then**: `RunFix` returns `ErrManualModeUnimplemented`; no index/backup artefact; CLI exit is non-zero. (Not a panic, not a silent no-op.)

- **TC-8 — Write-destination confinement, BOTH sides (NFR4/AC7)**
  - **Given**: a `MetaDir` and crafted selectors/subjects.
  - **When/Then** (each a subtest):
    - (a) a write op whose selector resolves to a path **outside** `MetaDir` is rejected **before any write**;
    - (b) a path that lexically sits under `MetaDir` but traverses a **symlinked parent** is rejected;
    - (c) `confineWriteDir`: an **upward-walked `.dcfh` outside the root** (out-of-repo subject whose ancestor `.dcfh` escapes) is rejected;
    - (d) explicit-subject permit: the CLI repairing an explicitly-named subject **outside** `MetaDir` is permitted, yet a library `IndexSelectors` value **cannot** reach that same out-of-`MetaDir` location (the exemption is not a general bypass);
    - (e) `confineWriteDest` accepts a legitimate in-`MetaDir` target (positive control).

- **TC-9 — Corruption-path semantics via `FixResult`, all three loops (NFR5/AC6)**
  - **Given**: fixtures with >100 unfixable entries, exercised on the edit, append, and removal paths.
  - **When**: each runs.
  - **Then**: the cap trips on the **101st** unfixable entry on **all three** loops; `RunFix` returns an error and `FixResult.EntriesDiscarded` reflects the discards; discard counts are not double-counted by the shared-predicate refactor.

- **TC-10 — No silent partial index on resync failure (NFR5/AC6)**
  - **Given**: a fixture that trips a resync `stop` mid-file.
  - **When**: the fix runs.
  - **Then**: no rename occurs, the temp is removed, the subject is byte-identical (abort-discards-temp, the 28.1 invariant, holds through the primitive).

- **TC-11 — CLI parity for inspection ops (FR4/NFR2)**
  - **Given**: the relocated handlers.
  - **When**: `header show`, `entry show`, `fixes list` run (human + JSON formats).
  - **Then**: output bytes, exit codes, and help text are unchanged from the pre-restructure baseline (`main_test.go`/`options_test.go`).

- **TC-12 — `entry edit json` stub preserved (FR4)**
  - **Given**: the restructured `entry-edit` path with a JSON payload.
  - **When**: `entry edit json …` runs.
  - **Then**: observable behaviour matches today — the "not yet implemented" error (and its current pre-error backup side-effect unless `--dry-run`), or, if no test pins the backup, error-only as a documented micro-change.

### Non-Functional Test Cases
- **Security**: `golangci-lint run ./...` clean; the M1-relocated G304/G703/G306 suppressions carry **rewritten** rationales citing the `confineWriteDest`/`MetaDir` invariant (not the stale "no trust boundary" copy); CWF `cwf-security-reviewer-changeset` verdict recorded. Confirm `.fix.tmp`/final-rename writes are not symlink-followable (TOCTOU defence-in-depth, alongside `PreserveOriginal`'s `O_EXCL`).
- **Reliability**: TC-4/TC-10 are the data-integrity gates — produced index always valid-or-absent, never partial.
- **Performance**: no new latency target; confirm the translation layer adds no extra index passes (informational, not gating).
- **Usability**: dcfhfix subcommand surface, flags, help, error messages, exit codes unchanged (TC-11 + the message-matrix cases in `main_test.go`).

## Test Environment
### Setup Requirements
- Go test on a Unix-like FS supporting atomic rename + symlinks (for TC-8 b/c/d).
- Fixtures: small valid/corrupt indices via the existing test helpers (`pkg/fix_writer_test.go` patterns from 28.1); a symlinked-parent layout and an out-of-repo subject with an escaping ancestor `.dcfh` for the confinement subtests.
- No network, no external services.

### Automation
- Standard `go test ./...`; runs in the pre-commit `-race` gate (`-d=checkptr=0` per repo convention) and CI.
- New tests live beside subjects: `pkg/fix_run_test.go` (primitive, confinement, routing), `pkg/fix_backup_test.go` (relocated cores), updated `cmd/dcfhfix/*_test.go` (CLI parity).

## Validation Criteria
- [ ] TC-R1 green after M1; full suite green after M2/M3.
- [ ] TC-1…TC-12 implemented and passing; AC1–AC7 each covered (AC5 is a design decision, already resolved — LD7).
- [ ] `golangci-lint run ./...` clean; CWF changeset security verdict recorded.
- [ ] No reduction in existing coverage; no re-export shims left behind.

## Decomposition Check
- [ ] Time / People / Complexity / Risk / Independence — all No. Test set is cohesive with the single subtask. **0 of 5.**

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-exec 28.2
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
TC-R1 + TC-1…TC-12 all PASS; AC1–AC7 covered. The cap-boundary case (TC-9) drove
a real product fix (AC6 discards-on-cap). See g-testing-exec.md.

## Lessons Learned
A test that asserts an exact count at a boundary (101st entry trips the cap)
earned its keep — it exposed that discards were being zeroed on the error path,
a defect no happy-path test would have caught.
