# dcfhfix non-destructive fix-to-new-file - Testing Plan
**Task**: 8 (feature)

## Task Reference
- **Task ID**: internal-8
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/8-dcfhfix-non-destructive-fix-to-new-file
- **Template Version**: 2.1

## Goal
Validate that `dcfhfix` preserves a byte-identical `.pre-fix-<ts>` sibling by
default across all four write paths, that the force-gated `--edit-in-place`
suppresses it, and that the preserve-before-rename safety invariant holds.

## Test Strategy
### Test Levels
- **Unit** (`cmd/dcfhfix/promote_test.go`, NEW): the four helpers in isolation —
  `siblingPreFixPath`, `preserveOriginal`, `validateEditInPlaceGate`,
  `promoteRepairedIndex`. Fast, filesystem-on-`t.TempDir()`, no command dispatch.
- **Integration** (`cmd/dcfhfix/main_test.go` / `writepath_test.go`, EXTEND):
  drive each real write path end-to-end on a fixture index built with the existing
  `layHeaderBytes`/`layEntryBytes` helpers, asserting on-disk artefacts.
- **Regression**: the full existing `cmd/dcfhfix` suite plus `go test ./pkg/...`
  must stay green (FR5 — `fixes` stack untouched).

### Test Coverage Targets
- **Critical paths (100%)**: `preserveOriginal` success + every refusal branch;
  the gate; the preserve-before-rename ordering in `promoteRepairedIndex`.
- **All four write paths** exercised in default and `--edit-in-place` modes.
- **Edge/error cases**: symlink/dir destination, EEXIST counter, bound
  exhaustion, Sync/Close failure propagation.
- **Regression**: existing suite passes unchanged.

## Test Cases
### Functional — Unit (promote_test.go)
- **TC-U1 — sibling naming shape**
  - *Given* `indexFile = "/x/main.idx"`.
  - *When* `siblingPreFixPath` is called.
  - *Then* result has prefix `/x/main.idx.pre-fix-` and a compact-UTC suffix; same
    directory as the input.
- **TC-U2 — preserve creates byte-identical sibling, original intact**
  - *Given* a temp index file with known bytes.
  - *When* `preserveOriginal` succeeds.
  - *Then* the returned sibling is byte-identical to the source **and** the source
    still exists unchanged (proves copy-before-rename ordering — AC6).
- **TC-U3 — refuse symlink destination, target untouched**
  - *Given* the resolved sibling path pre-exists as a symlink to a sentinel file.
  - *When* `preserveOriginal` runs.
  - *Then* it returns an error, writes nothing, and the symlink's **target file is
    unmodified** (no write traversed the link — D4/AC7).
- **TC-U4 — refuse directory destination**
  - *Given* the resolved sibling path pre-exists as a directory.
  - *When* `preserveOriginal` runs.
  - *Then* error, no write.
- **TC-U5 — EEXIST counter advances**
  - *Given* the base sibling name already exists as a regular file.
  - *When* `preserveOriginal` runs.
  - *Then* it succeeds at the `-1` candidate, leaving the pre-existing copy intact.
- **TC-U6 — bound exhaustion is a hard refusal**
  - *Given* base + `-1…-100` all exist.
  - *When* `preserveOriginal` runs.
  - *Then* it returns an error and leaves no new partial sibling.
- **TC-U7 — gate logic**
  - *Given* option combinations.
  - *When* `validateEditInPlaceGate` is called.
  - *Then* `edit-in-place` && !`force` → error; `edit-in-place` && `force` → nil;
    neither → nil (AC2).

### Functional — Integration (per write path: header edit, entry edit, append, remove)
- **TC-I1 — default preserves sibling, repairs canonical**
  - *Given* a fixture index needing a repair on path P.
  - *When* the command runs with no mode flags.
  - *Then* a `.pre-fix-<ts>` sibling exists byte-identical to the pre-run index;
    the canonical path holds the repaired index; exit 0 (AC1).
- **TC-I2 — `--force --edit-in-place` suppresses sibling**
  - *When* the command runs with both flags.
  - *Then* canonical replaced; **no** `.pre-fix-*` sibling exists (AC2).
- **TC-I3 — `--force` alone still preserves**
  - *When* the command runs with only `--force`.
  - *Then* canonical replaced **and** sibling present (AC2 — guards against
    `--force` accidentally suppressing the sibling).
- **TC-I4 — lone `--edit-in-place` refused**
  - *When* the command runs with `--edit-in-place` and no `--force`.
  - *Then* non-zero exit, actionable error, filesystem byte-for-byte unchanged
    (AC2). Also verify on a read-only command (`entry show`) that the gate refuses
    consistently (documented behaviour).
- **TC-I5 — dry-run previews, writes nothing**
  - *When* the command runs with `--dry-run`.
  - *Then* "would preserve … `.pre-fix-<timestamp>`" appears; no canonical
    rewrite, no sibling, no new `fixes`-stack entry (AC4/FR6).
- **TC-I6 — dry-run + destructive preview**
  - *When* `--dry-run --force --edit-in-place`.
  - *Then* destructive warning shown, **no** "would preserve" line, nothing
    written.
- **TC-I7 — fixes stack still populated (FR5)**
  - *When* a default-mode repair runs.
  - *Then* a new `fixes`-stack entry is created (default-mode parity with today).

### Non-Functional Test Cases
- **Reliability (NFR5)** — TC-U2 plus a Sync/Close-failure injection (or a
  documented fault-injection seam) asserting that a preservation error returns
  **before** any rename, leaving the canonical index intact.
- **Security (NFR4)** — TC-U3/TC-U4 are the load-bearing symlink-follow defences;
  `golangci-lint run ./...` (gosec gate) must pass with rationale-bearing
  suppressions present on the new write/copy code (AC7).
- **Usability (NFR2)** — TC-A1 (below): preservation notice obeys `--quiet`; the
  destructive warning is emitted even under `--quiet` (AC3).
- **Performance (NFR1)** — no dedicated benchmark; the added cost is one file copy
  of the same order as the existing `fixes`-stack copy (out of scope for timing).

### Acceptance message test
- **TC-A1 — message/quiet matrix**: default run prints "Original preserved at
  <sibling>" (suppressed under `--quiet`); `--force --edit-in-place` prints the
  destructive warning to stderr **and still prints it under `--quiet`** (AC3).

## Test Environment
### Setup Requirements
- `go test ./cmd/dcfhfix/...` and `go test ./pkg/...`; Unix-like host (symlink
  test cases require symlink support).
- Fixtures via existing `layHeaderBytes` / `layEntryBytes` + `t.TempDir()`; no
  network, no external services, no production indices.
- `golangci-lint run ./...` for the gosec gate.
### Automation
- Standard `go test`; gated by the `.githooks/pre-commit` staged lint on commit.

## Validation Criteria
- [ ] TC-U1…U7, TC-I1…I7 (×4 paths where applicable), TC-A1 all pass.
- [ ] `go test ./cmd/dcfhfix/... ./pkg/...` green (new + existing/regression).
- [ ] `golangci-lint run ./...` passes; gosec suppressions present with rationale.
- [ ] Coverage of `promote.go` critical paths at 100% (success + every refusal).
- [ ] Maps to AC1–AC7 in `b-requirements-plan.md`.

## Decomposition Check
- [ ] Time >1 week? No. [ ] People >2? No. [ ] 3+ concerns? No. [ ] Risk
  isolation? No. [ ] Independent parts? No. **Decision**: Do not decompose.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
*To be filled upon completion*

## Lessons Learned
*To be captured during implementation*
