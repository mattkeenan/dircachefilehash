# Extract pkg/format single owner of layout - Testing Execution
**Task**: 3.1 (chore)

## Task Reference
- **Task ID**: internal-3.1
- **Task URL**: N/A (internal task)
- **Parent Task**: 3
- **Branch**: chore/3.1-extract-pkgformat-single-owner-of-layout
- **Template Version**: 2.1

## Goal
Execute the tests defined in e-testing-plan.md and verify the implementation from
d-implementation-plan.md.

## Execution Checklist
- [x] Read e-testing-plan.md and f-implementation-exec.md thoroughly
- [x] Verify test environment ready (Go 1.24.3, golangci-lint w/ gosec)
- [x] Execute test cases sequentially
- [x] Record pass/fail for each test
- [x] Document failures with reproduction steps (none)
- [x] Update status to "Finished" — all pass

## Test Results

### Functional Tests

| Test ID | Test Case | Expected | Actual | Status | Where |
|---------|-----------|----------|--------|--------|-------|
| TC-1 | v3 round-trip (byte-identical) | getters return laid values; idempotent setter write leaves buffer byte-for-byte identical | header parses at 0 (v3, count=2); both entries decode (Dev/Ino/Path); buffer unchanged after re-writing all 6 setter-backed fields | PASS | `pkg/format/roundtrip_test.go::TestRoundTrip_V3_ByteIdentical` |
| TC-2 | v2 round-trip + parse-offset assertion | entry read at **88** (`V2HeaderSize`), not 96/104; v2 decode clean; wrong offset must fail | `HeaderSizeForVersion(2)==88`, `(3)==104`; entry at 88 decodes (Dev=0xAABBCCDD, path); `NewSafeEntry(...,96)` errors (tier-1 rejects bogus Size) | PASS | `pkg/format/roundtrip_test.go::TestRoundTrip_V2_ParseOffset` |
| TC-3 | dcfhfix write-path header | 104-byte header written via `format.Header`; Timestamp preserved; **no 8-byte over-read**; entries intact at offset 104 | `writeIndexWithCustomHeader` output: 104-byte v3 header, byte-order magic OK, distinctive `Timestamp=0x0123456789ABCDEF` intact, entry decodes at 104 (Dev/Path) | PASS | `cmd/dcfhfix/writepath_test.go::TestWritePath_CustomHeader_NoOverRead` |
| TC-3b | Header struct-size invariant | `sizeof(Header)==HeaderSize` so write-cast reads exactly the struct | `unsafe.Sizeof(Header{})==104`; full header field round-trip incl. Timestamp | PASS | `pkg/format/roundtrip_test.go::TestRoundTrip_HeaderSizeInvariant` |
| TC-4 | codec tier-1 (entry-level) bounds | each malformed Size errors, no panic | Size 0 / `<minEntrySize` / `>4096` / `offset+Size>len` all error; 2-byte truncated buffer errors; out-of-range offset errors | PASS | `pkg/format/codec_test.go::TestSafeEntry_Tier1_*` |
| TC-5 | codec tier-2 (field vs maxOffset) bounds | read bounded by entry's declared `maxOffset`, not `len(buf)`; no spill into next entry | large buffer, entry Size ends early → `GetPath` bounded by declared Size; no-path entry returns "" and does not leak buffer tail | PASS | `pkg/format/codec_test.go::TestSafeEntry_Tier2_*` |
| TC-6 | truncated buffer | clean error, no over-read | 2-byte buffer (mid-Size) errors cleanly | PASS | `pkg/format/codec_test.go::TestSafeEntry_Tier1_TruncatedBuffer` |

### Non-Functional Tests

| Area | Criterion | Result | Status |
|------|-----------|--------|--------|
| Reliability / integrity | layout assertions + 8-byte alignment hold | build-time assertions in `entry.go` compile; full suite green | PASS |
| Performance | no zero-copy load-path regression (pure extraction) | full suite incl. `cmd/dcfh` (4.9s) unchanged; no new allocation on read path | PASS |
| Security (static) | G115 count must **not increase** vs baseline 63 | active G115 = **52** (un-excluded `.golangci.yml:59`, ran `golangci-lint run ./...`, reverted). Reduced by 11; gate intent satisfied | PASS |

**G115 distribution (active, 52 total)**: pkg 28, pkg/format 12, cmd/dcfh 3, fsdedupe 7, cmd/dcfhfix 1, root 1. The 12 in `pkg/format` are the narrowing conversions consolidated out of core + dcfhfix into the single codec (a move, not new debt).

## Test Failures
None.

## Coverage Report

`go test ./pkg/format/ -coverprofile` — codec.go (the safety floor migrated from dcfhfix):

| Function | Coverage | Note |
|----------|----------|------|
| `NewSafeEntry` (tier-1) | 100.0% | all size + offset guards exercised |
| `GetSize/GetDev/GetIno/GetMode/GetUID/GetGID/GetFileSize/GetCTimeWall/GetMTimeWall/GetHashType/GetEntryFlags` | 100.0% | |
| `GetPath` (path bounded by maxOffset) | 100.0% | |
| Setters (`SetCTimeWall/...`/SetFileSize) | 100.0% | idempotent round-trip (TC-1) |
| `validateFieldAccess` (tier-2) | 75.0% | success path + path-bound covered |
| `readField/writeField` | 80.0% | generic happy path covered |
| `GetHash` | 80.0% | 64-byte copy covered; error branch not |

**On the residual `validateFieldAccess`/`readField`/`writeField`/`GetHash` error branches**: these are
*structurally unreachable* for any tier-1-valid entry. Tier-1 guarantees `Size >= minEntrySize`
(= `sizeof(Entry)` = 136), so `maxOffset >= offset+136` and every **fixed** field (the last, `Hash`,
ends at offset 124) always fits inside the declared entry — `validateFieldAccess` can never fail for a
fixed-field getter. The only field whose access genuinely depends on declared `Size` is the
variable-length **path**, which `GetPath` bounds directly against `maxOffset` (covered by TC-5). The
field-level error returns are retained as defence-in-depth for a future layout where a fixed field
could sit beyond a (hypothetically smaller) `maxOffset`; reaching them through the public API would
require constructing an illegal `SafeEntry` that bypasses tier-1, which would be a misleading test.
The e-plan's "100% of bounds-check branches" target is met for the **reachable** bounds logic.

## Status
**Status**: Finished
**Next Action**: /cwf-rollout
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All seven test cases (TC-1..TC-6, plus the TC-3b header-size invariant) pass; full regression suite
green across all packages; gofmt clean; `golangci-lint run ./pkg/format/ ./cmd/dcfhfix/` = 0 issues;
static G115 reduced (63 → 52), gate intent satisfied. The deferred round-trip integration tests
(TC-1/TC-2/TC-3) flagged in f-implementation-exec.md are now landed here.

## Lessons Learned
*To be captured during retrospective*

## Security Review

**State**: no findings

no findings: empty changeset

The testing-phase changeset (anchor 4cbe4ae → working tree) added only Go **test** files
(`pkg/format/roundtrip_test.go`, `cmd/dcfhfix/writepath_test.go`, and assertions in
`pkg/format/codec_test.go`). The `security-review-changeset --phase=testing` helper scopes the
reviewable surface to the CWF automation/tooling attack surface (`.cwf/scripts/`, `.cwf/lib/`,
skills docs, templates, agent/hook/rule configs) plus shebang scripts; test `.go` files fall
outside that scope, so the changeset is empty by design. The new tests introduce no production
behaviour, secrets, or injection vectors. The security-critical production surface (the codec's
two bounds tiers) was reviewed in f-implementation-exec.md (State: no findings) and is unchanged
here — these tests only *exercise* it.
