# Widen Dev/Ino to uint64 and re-enable G115 - Testing Execution
**Task**: 3.3 (bugfix)

## Task Reference
- **Task ID**: internal-3.3
- **Task URL**: N/A (internal task)
- **Parent Task**: 3
- **Branch**: bugfix/3.3-widen-dev-ino-to-uint64-and-re-enable-g115
- **Template Version**: 2.1

## Goal
Execute the tests defined in e-testing-plan.md and verify implementation from d-implementation-plan.md.

## Execution Checklist
- [ ] Read e-testing-plan.md and d-implementation-plan.md thoroughly
- [ ] Verify test environment ready
- [ ] Execute test cases sequentially
- [ ] Record pass/fail for each test
- [ ] Document failures with reproduction steps
- [ ] Update status to "Testing" when in progress, "Finished" when all pass

## Test Results

All planned test cases pass. New tests added: `pkg/format/transcode_test.go`,
`pkg/legacy_load_test.go`, `pkg/dedup_inode_test.go`, `cmd/dcfhfix/repair_v4_test.go`; existing tests
migrated to v4 reality in the f phase.

### Functional Tests

| TC | Test Case | Test(s) | Status | Notes |
|----|-----------|---------|--------|-------|
| TC-1 | v4 round-trip byte-identity | `TestRoundTrip_V4_ByteIdentical` (format) | PASS | migrated from the v3 name; setters write where getters read, buffer byte-identical |
| TC-2 | v3 decodes via DecodeHeap, every field, **both loaders** | `TestLegacyLoad_V3_RoutesThroughHeapTranscode` (pkg) | PASS | tracking + validation loaders; asserts `heapBacked` routing + path/size decode on the genuine v3 golden |
| TC-3 | v2 decodes (88-byte header) | `TestTranscodeLegacyIndex_Positive/v2` (format) | PASS | v2 covered at the transcoder level (no live v2 producer ⇒ no committed v2 golden, per e-plan); v2+v3 share the entry layout, differ only in header size |
| TC-4 | only v4 is written | `cmd/dcfhfix` writepath_test + f-phase E2E smoke | PASS | fresh `dcfh update` writes version byte 4; write path asserts `CurrentIndexVersion` |
| TC-5 | `TranscodeLegacyIndex` unit (positive + fail-closed) | `TestTranscodeLegacyIndex_{Positive,Empty,FailClosed}` (format) | PASS | v2+v3 every-field; negatives: sub-header, non-legacy version, truncated mid-entry, bogus size, **oversized EntryCount vs tiny file (no allocation)**; empty→header-only |
| TC-6 | `layoutForVersion` table, 100% arms | `TestLayoutForVersion` (format) | PASS | {2,3,4}→layout (+narrowDevIno flag); {0,1,5,0xFFFFFFFF}→error |
| TC-7 | unsupported version rejected, both loaders | `TestLoad_RejectsOutOfRangeVersion` + `TestStrategyForVersion` | PASS | pre-existing 3.2 tests, still green at current==4; `StrategyForVersion` legacy arms now `DecodeHeap` |
| TC-8 | **dupes correct on >2³² inodes (the bug)** | `TestDedupByInode_{DistinguishesHighBitInodes,CollapsesGenuineHardlinks,DistinguishesDevices}` (pkg) | PASS | distinct inodes sharing low-32 bits are NOT collapsed (the pre-fix `[2]uint32` key dropped one); genuine hardlinks still collapse |
| TC-9 | heap-backed `Cleanup()` never munmaps | `TestHeapBackedCleanup_NeverMunmaps` (pkg) | PASS | no munmap of the GC buffer; `Data` niled; idempotent; under `-race` |
| TC-10 | dcfhfix repair stamps v4 | `TestDcfhfixRepair_StampsV4Header` (dcfhfix) | PASS | `createTempIndexWithHeader` on the v3 golden emits a v4 header (the corrupt-output fix); signature/byte-order preserved |
| Golden | v3 decode-compat + v4 layout anchor | `TestGolden_V3_DecodesToV4`, `TestGolden_V4_LayoutAnchor` (format) | PASS | v3 golden decodes to content-stable fields (path/size/mode/content-hash); v4 golden first entry at offset 104 with v4 stride |

### Non-Functional Tests

- **Reliability (NFR5)**: full suite + canonical race gate
  `GOFLAGS=-gcflags=all=-d=checkptr=0 go test -race -short ./...` — all green. The heap path (TC-2/TC-9)
  and the negatives (TC-5/TC-7) run under `-race` with no over-read / double-free / munmap-of-heap.
- **Security (NFR4)**: `golangci-lint run ./...` reports **0 G115**; the `--new` enforcement gate is clean.
  No suppression on any Dev/Ino/EntryCount conversion (fixed structurally). `layoutForVersion` and
  `StrategyForVersion` both fail closed (switch-with-default), proven by TC-6/TC-7 error arms.
- **Performance (NFR1)**: the v4 hot path stays zero-copy (TC-4 reload is not `heapBacked`); transcode
  fires only on the rare legacy branch.

## Test Failures

None. (During authoring, two expected-update failures surfaced and were corrected: `TestCachePortability`
136→144 and the v3-golden hash assertions — dcfh stores a content-derived SHA-1 framing, not raw
`sha1sum`/`git hash-object`, so the test asserts the committed golden's actual stable hash values. Both
fixed; suite green.)

## Coverage Report

`pkg/format` 58.8% overall; the **new untrusted-input/dispatch code is well covered**:
`TranscodeLegacyIndex` 88.9%, `transcodeEntry` 100%, `layoutForVersion` 100%, `StrategyForVersion` 100%,
`NewSafeEntry` 94.4%, `GetDev` 100%. (`MinEntrySizeForVersion` shows 0% in `pkg/format`'s own profile —
it is exercised from `cmd/dcfhfix`'s resync floor, not format's tests.)

## Fixture robustness

Added `.gitattributes` (`*.idx binary`) so the committed binary goldens are byte-exact on every checkout
(no EOL/encoding normalisation) regardless of a contributor's `* text=auto`. The goldens encode their
mtime/dev/ino in the committed bytes; the tests read those bytes and assert only content-derived stable
fields, so a checkout's filesystem-mtime change is irrelevant (no tarball needed; and v3 can no longer be
regenerated — the v3 writer is gone post-widen, so the captured golden is the irreplaceable oracle).

## Security Review

**State**: no findings

no findings: empty changeset

The `security-review-changeset --phase=testing` helper returned an empty changeset (CwF v1.1.155
uncommitted-diff bug, fixed in v1.1.163; it also scopes to CWF-internal/script files by design). Unlike
the f phase, no manual agent review was warranted: the testing-phase changeset is test-only
(`transcode_test.go`, `legacy_load_test.go`, `dedup_inode_test.go`, `repair_v4_test.go`), a committed
binary golden (`testdata/v4.idx`), a `.gitattributes` line, and a one-line comment fix — no new production
attack surface. The production untrusted-input code was reviewed in f-implementation-exec (no findings).

## Status
**Status**: Finished
**Next Action**: /cwf-retrospective
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
10/10 test cases pass (commit b6c13b4, 8 files); coverage 88-100% on new code; race gate green. Goldens +
`.gitattributes` committed. Two expected-update failures during authoring (CachePortability 136→144; v3
golden hash framing) corrected.

## Lessons Learned
Capturing the v3 golden before the bump was essential — the v3 writer no longer exists post-widen, so the
committed golden is the irreplaceable decode-compat oracle.
