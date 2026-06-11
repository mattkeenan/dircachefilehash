# Fault-injection tests for atomic replacement - Requirements
**Task**: 23 (feature)

## Task Reference
- **Task ID**: internal-23
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/23-fault-injection-atomic-replacement
- **Template Version**: 2.1

## Goal
Specify the test seam and the failure-path/edge-case tests that prove the atomic
index-replacement integrity guarantee and close the uneven scan edge-case coverage,
asserting the **actual** production recovery contract (not an idealised one).

## Production Contract (verified — tests must assert this, not change it)
The replacement-finalise helpers `finaliseMainIndex` (`pipeline_update.go:170`)
and `finaliseStatusCache` (`status.go:129`) are **void and deferred**. Their
behaviour, which the constraint "production byte-for-byte unchanged" forbids us
from altering, is:

| Failure point | Error to caller | Temp file disposition |
|---|---|---|
| Main open/write/sync (`!ok`) | **propagates** (from write stage) | **removed** (`pipeline_update.go:175`) |
| Cache open/write/sync (`!ok`) | propagates from caller's write | **retained** for startup merge (`status.go:132`) |
| Main rename fails (`ok` branch) | **swallowed** (logged only, `:189-193`) | **retained** |
| Cache rename fails (`ok` branch) | **swallowed** (logged only, `:139-143`) | **retained** |

The load-bearing invariant in every row is identical and is the real target of
this task: **the live committed index (`main.idx`/`cache.idx`) is never corrupted
or lost.** Retained temp files are a recovery artefact, not a leak.

## Functional Requirements
### Core Features

- **FR1 — Injectable seam for replacement-path write primitives.** The os-level
  primitives used to promote a temp index — `os.Rename` (main:
  `pipeline_update.go:189`; cache: `status.go:139`), the temp-file `os.OpenFile`
  (`temp_index_writer.go:30`), and `file.Sync` (`temp_index_writer.go:205`) —
  must be reachable through a swap point that tests can override to return an
  error, and that production never overrides.
  - *Acceptance*: a test can force each of those three operations to fail
    independently; with no override the operation calls the real `os` function.
  - *Acceptance*: the production success path has no behavioural change — the swap
    defaults to the real function and adds no new runtime branch beyond a
    function-pointer indirection.

- **FR2 — The live committed index is never corrupted or lost under injected
  failure (the central invariant).** For every failure point in the Production
  Contract table — temp open, vectorio write, pre-rename `Sync`, and the final
  `Rename` — on both the main and cache paths, the pre-existing `main.idx` /
  `cache.idx` must remain byte-unchanged and loadable.
  - *Acceptance*: after each injected failure, loading the index returns the
    pre-operation entry set and a byte/checksum comparison of the prior index
    file (before vs after the failed run) is equal.

- **FR3 — Write/open/sync failures surface an error to the caller.** When the temp
  `OpenFile`, the vectorio write, or the pre-rename `Sync` fails, the operation
  returns a non-nil error (propagated from the write stage, `pipeline_update.go:117`
  / `temp_index_writer.go:205`). Rename failures are explicitly **out of scope for
  error-surfacing**: they are logged-and-swallowed by the deferred finalise helper
  by design, so FR2 (state preserved) is the only assertion for the rename path.
  - *Acceptance*: injected open/write/sync failure → operation returns non-nil
    error; injected rename failure → operation may return nil (assert only FR2).

- **FR4 — Temp-file disposition follows the documented recovery contract.** Tests
  assert the disposition in the Production Contract table rather than blanket
  "no residue": main `!ok` → temp removed; cache `!ok` and **all** rename-failure
  cases → temp retained. Any retained temp file must itself be a valid, loadable
  index (it is the recovery input), not a half-written artefact.
  - *Acceptance*: post-failure directory state matches the table per path; each
    retained temp loads and validates clean.
  - *Acceptance (secondary, success-with-residue)*: if `Rename` succeeds but
    `CleanupTimestampedCacheFiles` fails (`pipeline_update.go:195` / `status.go:145`),
    the promoted `main.idx`/`cache.idx` is still correct and a subsequent run is
    idempotent over the residue. Cover if low-cost; otherwise record as a known gap.

- **FR5 — Scan tolerates concurrent file churn between discovery and hash.** When a
  file present at scan time is either deleted or has its contents modified before
  the hash worker reads it, the operation completes without crashing, **does not
  abort the whole run on the single affected file** (per-entry tolerance), exits
  success, and produces an internally consistent index (the affected entry is
  consistently absent, or consistently present with a coherent value — never a
  half-written/corrupt entry).
  - *Acceptance (delete)*: a test deleting a scanned file before hashing completes
    the update with success exit; the produced index loads and validates clean.
  - *Acceptance (modify)*: a test rewriting a scanned file's contents before hashing
    completes the update with success exit; the produced index loads and validates
    clean.

- **FR6 — Mid-scan interrupt promotes no partial index.** When the operation's
  context is cancelled mid-pipeline, no partial/temp index is promoted over the
  live `main.idx`; the prior committed state is preserved (FR2 invariant).
  - *Acceptance*: a context-cancellation test asserts the live index is unchanged
    after interrupt. Preferred path is un-skipping `pkg/shutdown_test.go:13` against
    the v0.7 pipeline; **the design phase must confirm the un-skip is viable within
    the test-only sizing** — if it requires non-trivial pipeline rework, an
    equivalent focused new test is the mandatory fallback.

### User Stories
- **As a** maintainer **I want** the atomic-replacement guarantee exercised under
  injected I/O failure **so that** a regression that corrupts or loses the live
  index is caught by CI rather than in the field.
- **As a** maintainer **I want** scan-time concurrent-modification handled
  deterministically in tests **so that** real-world file churn during a scan
  cannot silently produce a corrupt index.

## Non-Functional Requirements
### Performance (NFR1)
- The seam adds no measurable cost to the production write path: at most one
  function-pointer call where a direct `os.*` call stood. No allocation, no lock,
  no branch on operation success.
- New tests run within the existing `./pkg/...` suite time envelope; no test
  introduces a fixed wall-clock sleep > 100ms as a synchronisation device.

### Usability (NFR2)
- Injected-failure tests use a single shared helper to install/restore a fault so
  the pattern is obvious and copyable; restoration is automatic via `t.Cleanup`.
- Failure assertions name the invariant they protect (e.g. "prior main.idx
  intact") so a future failure message is self-explanatory.

### Maintainability (NFR3)
- The seam follows the idiomatic package-level-`var` indirection pattern; no new
  third-party mocking dependency is added.
- Seam surface is minimal: only the three primitives in FR1, scoped to the
  main+cache daily-operation paths. The wire-handler cache (`wire_handler.go:445`)
  is **deliberately out of scope** — leaving it unseamed means "no test coverage
  there", which is acceptable precisely because the seam is test-only and the wire
  handler is the one untrusted-reachable (SSH) write site; design records this
  boundary.
- New tests live in focused files (e.g. `atomic_index_test.go`,
  `scan_edge_cases_test.go`) consistent with the one-concern-per-file test layout.

### Security (NFR4)
- The seam must not be settable via any external input (env var, config, flag) —
  it is a test-only package-internal variable, eliminating any production override
  vector that could force index-write failures.
- Tests use temporary directories only; no production `.dcfh` or real user data is
  touched (per repo quality gate).

### Reliability (NFR5)
- Tests are deterministic: concurrency/edge-case races (FR5) are driven through the
  seam at a known pipeline stage, not via timing, so they do not flake under
  `-race` or CI load.
- Because the seam is shared mutable package state, seam-driven tests must **not**
  run under `t.Parallel()` (or must serialise the swap); the suite must pass under
  the repo's `-race` (`-d=checkptr=0`) gate.
- A deliberate temporary break of the production cleanup/rename logic must cause at
  least one new test to fail (teeth check — no vacuous passes).

## Constraints
- Production behaviour must be byte-for-byte unchanged; this task adds tests and a
  test-only seam, not features. The Production Contract table is asserted, not altered.
- No new third-party dependencies.
- Unix-only test assumptions are acceptable (repo is Unix-only by design).
- British spelling in prose/comments; match existing test idiom and helpers.
- Scope is `pkg/` test additions plus the minimal seam in the three named
  production files; no CLI, no format change.

## Decomposition Check
- [ ] **Time**: >1 week? No — ~2 days.
- [ ] **People**: >2 people? No.
- [x] **Complexity**: 3+ concerns (seam, atomic faults, scan edge cases).
- [ ] **Risk**: High-risk isolation needed? No — test-only additions.
- [x] **Independence**: Atomic-fault and scan-edge tests separable once seam exists.

2 signals triggered; per planning decision the task stays whole (cohesive
test-hardening sharing one seam). Re-evaluate only if the seam grows during design.

## Acceptance Criteria
- [ ] AC1: FR1 seam — each of rename/open/sync can be independently forced to fail
      from a test; production uses the real os funcs with no success-path branch.
- [ ] AC2: FR2-FR4 acceptance bullets met for every Production-Contract row on both
      main and cache paths (live index intact+loadable; error surfaced only on
      open/write/sync; temp disposition matches the table; retained temps valid).
- [ ] AC3: FR5 — delete-before-hash and modify-before-hash both exit success,
      per-entry tolerant, producing a loadable, validate-clean index.
- [ ] AC4: FR6 — mid-scan cancellation asserts no partial promotion (un-skip or new test).
- [ ] AC5 (standalone teeth/gate): `go test ./pkg/...` passes including under
      `-race`; a deliberate temporary break of cleanup/rename logic fails ≥1 new test.

## Status
**Status**: Finished
**Next Action**: /cwf-design-plan 23
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
*To be filled upon completion*

## Lessons Learned
*To be captured during implementation*
