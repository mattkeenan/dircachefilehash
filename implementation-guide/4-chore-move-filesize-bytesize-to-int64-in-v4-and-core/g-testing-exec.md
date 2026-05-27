# Move FileSize/ByteSize to int64 in v4 and core - Testing Execution
**Task**: 4 (chore)

## Task Reference
- **Task ID**: internal-4
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/4-move-filesize-bytesize-to-int64-in-v4-and-core
- **Template Version**: 2.1

## Goal
Execute the e-testing-plan.md test cases (TC-1..TC-7 + NFRs) against the
implemented int64 flip and record results.

## Environment
- Go 1.24.3; golangci-lint with gosec active (G115 floor).
- v3/v4 goldens under `pkg/format/testdata/` (committed Task 3).
- No database, no network, no external services.

## Test Results

### Functional Test Cases

| TC | SC | What | Result |
|----|----|------|--------|
| TC-1 | SC1 | v4 on-disk bytes unchanged | PASS |
| TC-2 | SC1 | large positive size round-trips exactly | PASS (test added) |
| TC-3 | SC3 | legacy v2/v3 decode unchanged | PASS |
| TC-4 | SC3 | negative size rejected (NEW) | PASS (case added) |
| TC-5 | SC2 | G115 floor, no relocation | PASS |
| TC-6 | filters | size predicates on signed values | PASS |
| TC-7 | dedup | no dupes/dedup regression | PASS |

- **TC-1** — `TestRoundTrip_V4_ByteIdentical` PASS (idempotent setter round-trip,
  buffer byte-identical), `TestRoundTrip_HeaderSizeInvariant` PASS,
  `TestParseOffset_V2HeaderSize` PASS. The 8-byte host-order field is
  signedness-agnostic, so no format bump (`CurrentIndexVersion` unchanged).
- **TC-2** — added `TestRoundTrip_V4_LargePositiveFileSize` (pkg/format): sets
  `0x0FFFFFFFFFFFFFFF` (< MaxFileSize 1<<62) via `SetFileSize`, reads back via
  `GetFileSize`, exact match — no truncation, no sign flip. PASS.
- **TC-3** — `TestGolden_V3_DecodesToV4` PASS, `TestGolden_V4_LayoutAnchor` PASS,
  `TestTranscodeLegacyIndex_Positive{,/v2,/v3}` PASS, `TestTranscodeLegacyIndex_FailClosed`
  PASS, the legacy_load tracking/validation loaders PASS with `wantSizes []int64`.
  Legacy on-disk size was already 8-byte unsigned < 2⁶³, so the signed reinterpret
  is a no-op for honest data.
- **TC-4** — NEW `TestRecoveryValidationProcessor/NegativeFileSize` (`FileSize = -1`)
  PASS: validator (`validateFileSizeBounds`) rejects fail-closed. Companion
  `ExcessiveFileSize` fixture repaired from `1<<63` (now overflows int64) to
  `(1<<62)+1` — PASS.
- **TC-5** — `golangci-lint run ./...` → **0 G115** whole-tree (3 unrelated
  pre-existing findings only: cyclop on `parseTestToken`/`resolveOneSelector`,
  unparam on `createTestEntry` — none touched by this task). `golangci-lint run
  --new ./...` → **0 issues** (also confirmed by the pre-commit hook at the f-phase
  checkpoint). The 7 named lines are suppression-free (`binary_entry_scan:74`,
  `scan:262`, `filter` SizeTest, `comparison_sink` ×3, `metastore:155`) — retired
  by removing the cast, not relocated. No new G115 in `needsHash` or
  `validated_entry.go` (signed end-to-end via `parseInt64`).
- **TC-6** — `TestMinMaxSizeTest` (6 subcases) PASS, `TestParseSizeBound` (incl.
  `-1`/`+1` still `wantErr` via the added sign guard, and the overflow path) PASS,
  `TestSizeBoundString` PASS, `TestParseSizeTestSpec` PASS,
  `TestBuildFilterSizeAndAge` PASS. Identical results to pre-flip for all valid
  (non-negative) bounds.
- **TC-7** — `TestDedupByInode_{DistinguishesHighBitInodes,CollapsesGenuineHardlinks,DistinguishesDevices}`
  PASS, `cmd/dcfh` `TestBuildDupeFilter_*` / `TestRunDedupe_*` PASS, fsdedupe
  `TestRun_*` PASS. Size signedness does not reach the dedup key ([2]uint64
  dev/ino) or the fsdedupe `uint64` byte totals (out of scope).

### Non-Functional Test Cases
- **Performance (NFR1)**: v4 load stays zero-copy — `int64`/`uint64` are both
  8 bytes host-order; no layout or load-path change. `TestRoundTrip_V4_ByteIdentical`
  reload not `heapBacked`. No regression, no new benchmark needed.
- **Security (NFR4)**: 0 G115 whole-tree (TC-5); the negative-size floor is a
  fail-closed corruption guard (TC-4), not new attack surface; no new
  exec/file-write/env-var/hook surface. Changeset security review below.
- **Reliability / integrity (NFR5)**: golden round-trips (TC-1/TC-3) prove honest
  data survives the reinterpret unchanged; negative floor (TC-4) fail-closes
  corrupt input; SHA-1 footer + 8-byte alignment unaffected (no width change).
- **Race (SC4)**: `GOFLAGS=-gcflags=all=-d=checkptr=0 go test -race -short ./...`
  green across every package.

### Coverage
- `pkg/format` 58.8% (held at Task-3 baseline), `pkg` 62.5%, `pkg/fsdedupe` 14.6%,
  `cmd/dcfh` 25.6%, `cmd/dcfhfind` 44.2%, `cmd/dcfhfix` 19.7%. New-code arms
  (negative-size floor, large-positive round-trip, signed parser) covered by
  TC-4/TC-2/TC-6.

## Test Failures
None. All planned test cases pass.

## Status
**Status**: Finished
**Next Action**: /cwf-retrospective
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Security Review

**State**: no findings

no findings: empty changeset

`security-review-changeset --phase=testing` reports `reviewed 0 files, 0 lines`
(anchor=79e4bc5): the testing-phase change is Go test code (`roundtrip_test.go`,
`recovery_test.go`) plus this markdown workflow file, none of which match CWF's
security-relevant classifier (CWF-internal directories or shebang-interpreted
scripts). Nothing for the changeset subagent to review. The task's semantic threat
surface was covered in the d-plan security pass and exercised by TC-4 (negative-size
fail-closed) here.

## Lessons Learned
- The regression-first strategy held: the existing v4/v3 goldens carried SC1/SC3
  with no new fixtures; only two small test additions were needed (TC-2
  large-positive round-trip, TC-4 negative-size rejection), both for edges the
  signedness flip newly makes reachable.
