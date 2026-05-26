# Add version registry and decode path - Testing Plan
**Task**: 3.2 (feature)

## Task Reference
- **Task ID**: internal-3.2
- **Task URL**: N/A (internal task)
- **Parent Task**: 3
- **Branch**: feature/3.2-add-version-registry-and-decode-path
- **Template Version**: 2.1

## Goal
Prove the version-dispatch seam: `StrategyForVersion` routes/rejects by value; both mmap entry-walk
loaders fail closed on an out-of-range version **and** on a v3-header truncation (never over-read or
panic); v2/v3 still load byte-correctly; the write version is sourced from one owner; G115 == 52.

## Test Strategy
### Test Levels
- **Unit** (`pkg/format`): `StrategyForVersion` as a pure function over a version table — positive
  routes and rejection boundaries, no I/O. This is the primary, exhaustive level for the seam.
- **Integration** (`pkg/`): the two loaders (`collectEntryRefs` via `LoadIndexFromFileForValidation`,
  and the tracking loader via `MetaStore{version: CurrentIndexVersion}`) over crafted on-disk
  fixtures — version rejection + truncation, under `-race`.
- **Regression**: existing `pkg/format` round-trip gate (3.1) + full `go test ./pkg/... ./cmd/...`.
- **Static**: gosec G115 site count re-measured at the 3.2 boundary.

### Test Coverage Targets
- **`StrategyForVersion`**: 100% — every switch arm incl. `default` exercised (the rejection arm is
  the security boundary, so it is not optional coverage).
- **Loader gates**: both the new version gate and the Step 3b header-size guard exercised in **each**
  loader (the dcfhfind `version:0` path is the one the version gate primarily protects).
- **Regression**: no drop vs the 3.1 baseline; v2 + v3 fixtures still load/round-trip byte-correctly.

## Test Cases
### Functional Test Cases

- **TC-1 (FR1/FR3, AC2+AC3) — `StrategyForVersion` table** — `pkg/format/version_dispatch_test.go`
  - **Given**: the pure resolver, no fixtures.
  - **When**: called with `{2, 3(=CurrentIndexVersion)}` and `{0, 1, CurrentIndexVersion+1,
    0xFFFFFFFF}`.
  - **Then**: the first set returns `DecodeZeroCopy, nil`; the second returns `DecodeReject` and a
    **non-nil** error whose message names the offending version **and** the supported `Min..Current`
    range (mirrors `ValidateVersion`'s wording, NFR2). No panic on any input. Asserts the exact
    enum value (not just nil/non-nil) so a future `DecodeHeap` mis-wire is caught.

- **TC-2 (FR3/NFR4/NFR5, AC3) — out-of-range version via the dcfhfind path** — `pkg/` integration
  - **Given**: a real v3 index built end-to-end (`NewMetaStore` + `runUpdate` on a `t.TempDir()`
    tree, as in `basic_integration_test.go`), copied to a temp file with its header **version byte
    patched** to an out-of-range value (test both `CurrentIndexVersion+1` and `0xFFFFFFFF`). The
    SHA-1 footer is left stale deliberately — version dispatch must fire regardless.
  - **When**: loaded via `LoadIndexFromFileForValidation(path)` (the `MetaStore{version:0}` path, so
    `ValidateVersion(0)` is a no-op and the resolver is the *only* real gate).
  - **Then**: returns a clean, descriptive error (unsupported version); **no panic, no over-read, no
    entries returned**. This is the load-path realisation of FR3/NFR4 for the path that has no other
    guard.

- **TC-3 (FR3, AC3) — out-of-range version via the tracking path** — `pkg/` integration
  - **Given**: the same patched fixture as TC-2.
  - **When**: loaded through a `MetaStore{version: CurrentIndexVersion}` tracking load
    (`loadIndexFromFileWithTracking`, e.g. via `LoadMainIndex`).
  - **Then**: clean error, no panic. (Belt-and-braces: `ValidateVersion(Current)` would also reject
    here; the test asserts the loader fails closed and releases its mmap — exercised under `-race` so
    a missed `DecRef()` / double-free surfaces.)

- **TC-4 (NFR5, AC3) — v3-header truncation over-read (Step 3b guard)** — `pkg/` integration
  - **Given**: a valid v3 index file truncated to a length **L ∈ [V2HeaderSize, HeaderSize)** i.e.
    `[88, 103]` (passes the `< V2HeaderSize` size gate, but `data[104:]` would slice past the mmap).
  - **When**: loaded via **both** `LoadIndexFromFileForValidation` and the tracking loader.
  - **Then**: a clean `"file too small for v3 header"`-style error — explicitly **not** a
    slice-bounds panic and **not** the already-handled `offset >= len(entryData)` entry-walk error.
    Run under `-race`. (Boundary: `L == HeaderSize (104)` with zero entries must instead **succeed**
    with an empty index — confirms the guard is `>` not `>=`.)

- **TC-5 (FR4, AC2) — v2/v3 byte-correct invariance** — regression
  - **Given**: the existing `pkg/format` gate (`TestRoundTrip_V3_ByteIdentical`,
    `TestRoundTrip_V2_ParseOffset`) and a v2 + v3 fixture.
  - **When**: full `go test ./pkg/... ./cmd/...` (incl. `-race`).
  - **Then**: all green; v2 entries still parse at offset 88, v3 at 104; no byte drift. The gate that
    proves 3.2 changed no on-disk behaviour.

- **TC-6 (FR2, AC1) — write-version ownership** — `pkg/`
  - **Given**: `SetHeaderForWritableIndex` with its `version` parameter removed.
  - **When**: a normal index write (`temp_index_writer` path) **and** the empty-index write
    (`writeEmptyIndex`) produce a file; the header is read back.
  - **Then**: both carry `CurrentIndexVersion`; no production caller passes a version literal
    (compiler-enforced by the signature change + grep evidence). Round-trip unchanged.

### Non-Functional Test Cases
- **Reliability (NFR5)**: every negative (TC-2/3/4) runs under `-race`, giving "no over-read / no
  double-free" a detection mechanism rather than an assertion of intent.
- **Security (NFR4)**:
  - **G115 == 52** at the 3.2 boundary, measured by the 3.1 method: temporarily remove `G115` from
    `linters.settings.gosec.excludes` in `.golangci.yml`, `golangci-lint run ./...`, count, revert.
  - **No raw-index of the untrusted version byte**: structurally guaranteed by the switch-with-default
    design and proven by TC-1's rejection arm (an arbitrary `0xFFFFFFFF` returns an error, not an
    out-of-range index).
- **Performance (NFR1)**: dispatch is O(1) **per load**, not per entry — verified by inspection (the
  gate sits at the head of each loader, before the entry walk) and by existing `pkg` benchmarks
  staying green (no new per-entry allocation on the zero-copy current path).

## Test Environment
### Setup Requirements
- Go 1.24.3 toolchain; `golangci-lint` (gosec is a v2 linter inside `.golangci.yml`, per CLAUDE.md
  Security Review §1) for the G115 measurement.
- Fixtures: `pkg/format` unit tests need none (pure function). Integration fixtures are built in
  `t.TempDir()` via `NewMetaStore` + `runUpdate`, then byte-patched/truncated on a temp copy — no
  committed binary fixtures, no touching the repo's own `.dcfh/`.
- Version byte/truncation manipulation operates on a **copy** in `t.TempDir()`; never mutates a real
  repository index.

### Automation
- `go test ./pkg/format/` (unit), `go test -race ./pkg/... ./cmd/...` (integration + regression).
- G115 measurement is a manual gated step (documented in g-testing-exec), not wired into CI here.

## Validation Criteria
- [ ] TC-1 passes: positive routes + all four rejection boundaries error with range-naming messages; no panic.
- [ ] TC-2 + TC-3 pass: out-of-range version rejected cleanly via **both** loaders, under `-race`.
- [ ] TC-4 passes: v3-header truncation `[88,103]` errors cleanly (no panic); `L==104`/empty succeeds.
- [ ] TC-5 passes: 3.1 round-trip gate green; full `go test ./pkg/... ./cmd/...` green incl. `-race`.
- [ ] TC-6 passes: written + empty indices carry `CurrentIndexVersion`; no caller passes a version.
- [ ] G115 site count == 52 (3.1 un-exclude/run/revert method).

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
TC-1..6 and all NFR gates PASS — recorded in g-testing-exec.md. TC-4 (v3-header truncation) caught a
use-after-munmap during exec; the boundary case (`L==104`) confirmed the guard is `>` not `>=`.
`StrategyForVersion` reached 100% coverage. See j-retrospective.md.

## Lessons Learned
The planned truncation/boundary test (TC-4) caught a real memory-safety bug on the first
implementation cut — negative and boundary cases earned their place in the plan. See j-retrospective.md.
