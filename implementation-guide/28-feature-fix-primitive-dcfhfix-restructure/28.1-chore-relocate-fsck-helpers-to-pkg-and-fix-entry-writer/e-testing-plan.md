# Relocate fsck helpers to pkg and fix entry writer - Testing Plan
**Task**: 28.1 (chore)

## Task Reference
- **Task ID**: internal-28.1
- **Task URL**: N/A (internal task)
- **Parent Task**: 28
- **Branch**: chore/28.1-relocate-fsck-helpers-to-pkg-and-fix-entry-writer
- **Template Version**: 2.1

## Goal
Define the test contract for the helper relocation (behaviour-preserving) and
the entry-writer correction (Approach A / D3), per the 28.1 implementation plan.

## Test Strategy
### Test Levels
- **Unit**: `ValidatedEntry`→`BEScanEntry` field mapping; `newFixMetaStore` checksum-type resolution + assertion; the relocated promote/backup helpers (already covered by moved tests).
- **Integration**: full repair through `processEntriesWithWorkflow` → `TempIndexWriter` → `promoteRepairedIndex`, re-reading the produced index via the production loader.
- **Regression**: the entire existing dcfhfix + pkg suite (`go test ./...`) after Milestone 1 (pure relocation) and again after Milestone 2 (writer fix).
- **Static/security**: `golangci-lint run ./...` (gosec floor) + CWF changeset review.

### Test Coverage Targets
- **Critical paths — 100%**: the new writer route (conversion, checksum-type seeding+assertion, temp→promote, abort-discard). These are data-touching and untested today.
- **Regression**: no reduction in existing `cmd/dcfhfix` / `pkg` coverage; every moved symbol still exercised (via updated call sites, not shims).
- **Edge cases**: corrupt-entry resync, unfixable cap, legacy-version input, each hash type.

## Test Cases
### Functional Test Cases

- **TC-1 — Pure relocation is behaviour-preserving (regression gate, Milestone 1)**
  - **Given**: helpers moved to `pkg/`, `appendValidatedEntryToTmpIndex` still present, call sites + test imports updated.
  - **When**: `go build ./...` and `go test ./...` run.
  - **Then**: all pass with no test-expectation changes — proves the move alone changed no behaviour.

- **TC-2 — Variable-length path round-trip (FR9 core)**
  - **Given**: a valid index whose entries include a multi-byte/CJK path and a maximum-length path.
  - **When**: an `entry edit` (or `append`) is applied through the corrected writer and the produced index is re-read via the production loader.
  - **Then**: every entry's relative path is byte-identical to the intended value (the old writer dropped path bytes — this is the regression being fixed).

- **TC-3 — checksum_type preserved across repair**
  - **Given**: three subject indices with header `checksum_type` = SHA-1, SHA-256, SHA-512 respectively.
  - **When**: each is repaired through the writer.
  - **Then**: the produced index's header `checksum_type` equals the subject's, and its footer checksum validates under that algorithm (no silent SHA-256 default).

- **TC-4 — checksum_type mismatch is refused (RESOLVED §1 assertion)**
  - **Given**: a `newFixMetaStore` whose resolved hash type disagrees with the subject header `checksum_type` (fault-injected).
  - **When**: a write is attempted.
  - **Then**: the writer returns an error before writing; the subject is untouched. (Guards against re-hashing under the wrong algorithm.)

- **TC-5 — Legacy v2/v3 input → v4 output, checksum_type preserved (RESOLVED §2)**
  - **Given**: a legacy v2/v3 subject index with a non-trivial path and distinct Dev/Ino/UID/GID/times.
  - **When**: repaired through the writer.
  - **Then**: output header version is v4 (deliberate upgrade); `checksum_type` preserved; every entry re-reads with Dev/Ino at the widened v4 offsets and all metadata + path intact (no field shift, no double wall-encoding of times).

- **TC-6 — Footer checksum validates after edit**
  - **Given**: any valid subject.
  - **When**: an edit is applied and the index promoted.
  - **Then**: the produced index passes the production checksum verification on load (`Close()` finalised count + checksum correctly).

- **TC-7 — Abort discards temp, subject untouched (no partial index)**
  - **Given**: a subject that trips the unfixable cap (>100 unfixable entries) OR a resync `stop`.
  - **When**: the repair runs and aborts.
  - **Then**: no rename occurs, the temp file is removed, and the subject index is byte-identical to its pre-operation state; `FixResult`/return counts report the discards.

- **TC-8 — Forward-progress on corruption preserved (relocation must not perturb)**
  - **Given**: an index with one corrupt entry mid-stream (recoverable via `trySkipToNextEntry`).
  - **When**: repaired.
  - **Then**: valid entries before and after the corrupt one are retained in the output; the corrupt entry is discarded and counted — identical to pre-relocation behaviour.

- **TC-9 — promote targets the subject, not main.idx (RESOLVED §3)**
  - **Given**: a subject named `cache.idx` (or an out-of-`.dcfh` path).
  - **When**: a repair is promoted.
  - **Then**: the rename lands on the subject path; `main.idx` is not created/clobbered; `O_EXCL` sibling preservation (`.pre-fix-*`) still fires per the moved `promote_test.go` expectations.

### Non-Functional Test Cases
- **Security**: `golangci-lint run ./...` clean; migrated G304/G703/G306 suppressions carry the "user-supplied subject path, no trust boundary at this layer" rationale (not a false `.dcfh` invariant). CWF `cwf-security-reviewer-changeset` verdict recorded. The relocated `preserveOriginal` retains its `O_EXCL` guard refusing planted symlinks/dirs (existing `promote_test.go` cases).
- **Reliability**: TC-6/TC-7 are the data-integrity gates — produced index always valid-or-absent, never partial.
- **Performance**: no new latency target; confirm `BenchmarkOptionsParse` and existing benches are not regressed by the import move (informational, not gating).
- **Usability**: dcfhfix subcommand output, exit codes, and help text unchanged (covered by `main_test.go` + `promote_integration_test.go` message-matrix cases).

## Test Environment
### Setup Requirements
- Go test on a Unix-like FS supporting atomic rename (repo baseline).
- Fixtures: small index files built with controlled header version + `checksum_type` and a mix of path widths — build via the existing test helpers (`format.SetHeaderForWritableIndex` / patterns in `writepath_test.go`, `repair_v4_test.go`) plus a legacy-version fixture. No network, no external services.
- Fault injection for TC-4 (forced checksum-type disagreement) and TC-7 (cap/stop) via crafted fixtures, not production code hooks.

### Automation
- Standard `go test ./...`; runs in the existing pre-commit `-race` gate (with `-d=checkptr=0` per repo convention) and CI.
- New tests live beside their subjects: `pkg/fix_*_test.go` for the writer/conversion units; updated `cmd/dcfhfix/*_test.go` for relocated call sites.

## Validation Criteria
- [ ] TC-1 green after Milestone 1; full suite green after Milestone 2.
- [ ] TC-2…TC-9 implemented and passing.
- [ ] `golangci-lint run ./...` clean; CWF changeset security verdict recorded.
- [ ] No reduction in existing coverage; no re-export shims left behind.

## Decomposition Check
- [ ] Time / People / Complexity / Risk / Independence — all No. Test set is cohesive with the single subtask. **0 of 5.**

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-exec 28.1
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
TC-1…TC-9 all PASS. TC-8 (forward-progress past a mid-stream corrupt entry) was
implemented at the g-testing-exec phase (its fixture was deferred from exec). The full
`golangci-lint run ./...` surfaced an errorlint regression (relocation artefact), fixed
by `%v`→`%w`. See g-testing-exec.md.

## Lessons Learned
The non-obvious part of the deferred corruption fixture was its design: null the path
bytes (→ "path empty" validation reject) while leaving the `Size` field intact, so
`trySkipToNextEntry` resyncs cleanly by size rather than via the heuristic scan — that is
what makes the survivors deterministic.
