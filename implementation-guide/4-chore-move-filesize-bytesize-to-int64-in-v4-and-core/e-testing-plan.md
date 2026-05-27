# Move FileSize/ByteSize to int64 in v4 and core - Testing Plan
**Task**: 4 (chore)

## Task Reference
- **Task ID**: internal-4
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/4-move-filesize-bytesize-to-int64-in-v4-and-core
- **Template Version**: 2.1

## Goal
Prove the `ByteSize` signedness flip changes no on-disk bytes and no observable
behaviour for honest data (SC1/SC3), retires the 7 size G115 suppressions without
relocating any (SC2), fail-closes on the one new edge the flip creates — a
negative size (SC3) — and keeps the race suite green (SC4). This is a type
refactor with strong existing coverage, so the strategy is **regression-first +
one new edge case**, not new feature testing.

## Test Strategy
### Test Levels
- **Unit**: codec round-trip, transcode decode, `ParseSizeBound`/`SizeBoundString`,
  the size-filter predicates, and the new negative-size validator floor.
- **Integration**: full `go test ./...` (status/update/dupes paths exercise the
  flipped `FileSize()` interfaces end-to-end).
- **Static gate**: `golangci-lint run ./...` is a first-class test here — it is the
  acceptance check for SC2 (0 G115 whole-tree).
- **No system/acceptance layer**: internal library + CLI, no deployment surface.

### Test Coverage Targets
- **New code (negative-size floor)**: 100% — both the reject branch and the
  accept-when-`>= 0` branch.
- **Retyped threshold parser (`ParseSizeBound`)**: keep existing coverage and add
  the `MaxInt64` overflow boundary (`ParseInt`'s own range + the `* mult` guard).
- **Critical paths** (codec read/write, transcode, validator, size filters): 100%.
- **Regression**: full existing suite green; `pkg/format` coverage held at the
  Task-3 baseline (~58.8% overall, new-code arms 88–100%).

## Test Cases
### Functional Test Cases

- **TC-1 (SC1) — v4 on-disk bytes unchanged**
  - **Given**: a v4 index entry carrying a `FileSize` value.
  - **When**: read → re-serialise after the `int64` flip (`TestRoundTrip_V4_ByteIdentical`,
    `TestRoundTrip_HeaderSizeInvariant`, `TestParseOffset_V2HeaderSize`).
  - **Then**: output is byte-identical and the header-size invariant holds — the
    8-byte host-order field is signedness-agnostic, so no format bump.

- **TC-2 (SC1) — large positive size round-trips exactly**
  - **Given**: a `FileSize` near the supported ceiling (e.g. `0x0FFFFFFFFFFFFFFF`,
    < `MaxFileSize 1<<62`).
  - **When**: `SetFileSize` → `GetFileSize` via the codec.
  - **Then**: the value round-trips exactly as a positive `int64` (no truncation,
    no sign flip). *(Extend `roundtrip_test.go`; the existing `0xdeadbeef` literal
    already recompiles clean as int64.)*

- **TC-3 (SC3) — legacy v2/v3 decode is unchanged**
  - **Given**: the committed v3 golden under `pkg/format/testdata/`.
  - **When**: transcoded to v4 (`TestGolden_V3_DecodesToV4`, `TestGolden_V4_LayoutAnchor`,
    `TestTranscodeLegacyIndex_Positive`).
  - **Then**: `FileSize` decodes identically — legacy on-disk size was already
    8-byte unsigned `< 2⁶³`, so the signed reinterpret is a no-op for honest data.

- **TC-4 (SC3) — negative size is rejected (NEW)**
  - **Given**: an entry whose `FileSize` is negative (the only value the
    reinterpret can newly produce, from a corrupt/`≥2⁶³` legacy field).
  - **When**: the `ValidationConfig` validator runs (`pkg/recovery.go:256`).
  - **Then**: it returns a "negative (corrupt)" error and the entry is dropped —
    fail-closed, mirroring the pre-1885 time underflow guard. *(NEW case in
    `recovery_test.go`, alongside the repaired `1<<63` oversize fixture which must
    change since `1<<63` no longer fits int64.)*

- **TC-5 (SC2) — G115 floor, no relocation**
  - **Given**: the fully flipped tree.
  - **When**: `golangci-lint run ./...` (whole-tree) and the `--new` staged gate
    (`.githooks/pre-commit`) run; and `grep -n '//nolint:gosec'` is run over the
    7 named lines (binary_entry_scan:74, scan:262, filter:251, comparison_sink
    ×3, metastore:155).
  - **Then**: **0 G115** whole-tree, `--new` clean, and **none** of the 7 lines
    still carry a suppression (proves retired-by-removal, not relocated). No new
    G115 anywhere (notably not in `needsHash` or `validated_entry.go`).

- **TC-6 (filters) — size predicates behave on signed values**
  - **Given**: `--size`/`--min-size`/`--max-size`/exact bounds parsed via
    `ParseSizeBound` (now `int64`) and `--size` spec parsing.
  - **When**: `TestMinMaxSizeTest`, `TestParseSizeBound`, `TestSizeBoundString`,
    `TestParseSizeTestSpec`, `TestBuildFilterSizeAndAge` run (with their `uint64`
    fixtures retyped per the d-plan).
  - **Then**: identical results to pre-flip for all valid (non-negative) bounds;
    add a `ParseSizeBound` boundary case at/over `MaxInt64` confirming clean error
    (no overflow, no panic).

- **TC-7 (no dupes/dedup regression)**
  - **Given**: the flip does not touch `fsdedupe` (distinct `uint64` type) and the
    dupes key is `[2]uint64` dev/ino (size-independent).
  - **When**: `TestDedupByInode_{DistinguishesHighBitInodes,CollapsesGenuineHardlinks,DistinguishesDevices}`
    and `TestFSDedupe_Integration_{DryRun,Apply}` run.
  - **Then**: unchanged grouping and reclaim totals — size signedness does not
    reach dedup.

### Non-Functional Test Cases
- **Performance (NFR1)**: v4 load stays zero-copy — `TestRoundTrip_V4_ByteIdentical`
  reload is not `heapBacked`; `int64` and `uint64` are both 8 bytes host-order, so
  there is no layout or load-path cost. No new benchmark needed.
- **Security (NFR4)**: `golangci-lint run ./...` → 0 G115 (TC-5); the new
  negative-size floor is a fail-closed corruption guard, not a new attack surface;
  no new exec/file-write/env-var/hook surface (confirmed in the plan-review
  security pass).
- **Reliability / data integrity (NFR5)**: golden round-trips (TC-1/TC-3) prove
  honest data survives the reinterpret unchanged; the negative-size floor (TC-4)
  proves corrupt input fail-closes; SHA-1 footer + 8-byte alignment invariants
  unaffected (no width change).
- **Race (SC4)**: canonical gate
  `GOFLAGS=-gcflags=all=-d=checkptr=0 go test -race -short ./...` green across
  every package.

## Test Environment
### Setup Requirements
- Go 1.24.3; `golangci-lint` with `gosec` active (the always-on G115 floor).
- Tests use `t.TempDir()`; the v3/v4 goldens live under `pkg/format/testdata/`
  (committed in Task 3 with `.gitattributes` binary handling).
- No database, no network, no external services.

### Automation
- `go test ./...`, then the race gate, then `golangci-lint run ./...`.
- The `--new` staged gate runs automatically at the `f`-phase checkpoint via
  `.githooks/pre-commit`.

## Validation Criteria
- [ ] **SC1**: TC-1/TC-2 pass — v4 bytes identical, large positive size round-trips.
- [ ] **SC2**: TC-5 passes — 0 G115 whole-tree, `--new` clean, 7 lines suppression-free,
  no new G115.
- [ ] **SC3**: TC-3/TC-4 pass — legacy decode unchanged; negative size rejected.
- [ ] **SC4**: full `go test ./...` and the race gate green across all packages.
- [ ] Regression: TC-6/TC-7 pass — filters and dedup unchanged for honest data.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All TC-1..TC-7 + NFRs passed (see g-testing-exec.md). The regression-first strategy
held: existing v4/v3 goldens carried SC1/SC3 with no new fixtures. Two planned
additions executed: TC-2 large-positive round-trip and TC-4 negative-size
rejection. Coverage held at the Task-3 baseline.

## Lessons Learned
Treating `golangci-lint run ./...` as a first-class acceptance test (SC2) worked —
the 0-G115 whole-tree result and the suppression-free grep of the 7 named lines
together proved "retired by removal, not relocated".
