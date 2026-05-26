# Fix inode device truncation re-enable G115 - Testing Execution
**Task**: 3 (bugfix)

## Task Reference
- **Task ID**: internal-3
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: bugfix/3-fix-inode-device-truncation-reenable-g115
- **Template Version**: 2.1

## Goal
Parent **cross-subtask** test close-out: verify the e-testing-plan.md gate cases (TC-1..TC-6) hold
end-to-end on the merged parent HEAD (`fd3546e`), and confirm the standing invariants (regression
suite, zero-copy fast path, G115 floor) survive the integration of all three subtasks. Per-field
matrices were executed in each subtask's `g-`; this phase re-runs the cross-subtask gate against the
combined tree and cites the subtask results.

## Execution Checklist
- [x] Read e-testing-plan.md and d-implementation-plan.md
- [x] Test environment ready (Go 1.24.3; golangci-lint with gosec active; tests use `t.TempDir()`)
- [x] Execute the cross-subtask gate cases on merged HEAD
- [x] Record pass/fail for each
- [x] Status updated

## Test Results

### Functional Tests (parent TC-1..TC-6, re-run on merged HEAD `fd3546e`)

| TC | Test Case | Verifying test(s) | Status | Notes |
|----|-----------|-------------------|--------|-------|
| TC-1 | round-trip equivalence | `TestRoundTrip_V4_ByteIdentical`, `TestRoundTrip_HeaderSizeInvariant` (format) | PASS | read → re-serialise → byte-identical; header-size invariant holds |
| TC-2 | malformed input errors, never over-reads | `TestTranscodeLegacyIndex_FailClosed`, `TestTranscodeLegacyIndex_Empty` (format) | PASS | sub-header / truncated-entry / bogus-size / oversized-EntryCount → clean error, no panic/OOB |
| TC-3 | version gate | `TestStrategyForVersion`, `TestLayoutForVersion`, `TestIndexHeader_ValidateVersion`, `TestLoad_RejectsOutOfRangeVersion` | PASS | unknown/newer/out-of-range rejected; zero-copy cast hard-gated on `version==current`; fail-closed switch arms |
| TC-4 | full-width dev/ino | `TestDedupByInode_DistinguishesHighBitInodes` + v4 round-trip (`TestRoundTrip_V4_ByteIdentical`) | PASS | >2³² dev/ino survive write→read without truncation |
| TC-5 | cross-version decode integrity | `TestLegacyLoad_V3_RoutesThroughHeapTranscode`, `TestGolden_V3_DecodesToV4`, `TestTranscodeLegacyIndex_Positive`, `TestGolden_V4_LayoutAnchor` | PASS | v3 decodes every post-Ino field correct, routed through heap decode (not cast through widened struct); v4 golden layout anchored |
| TC-6 | dupes correctness | `TestDedupByInode_{DistinguishesHighBitInodes,CollapsesGenuineHardlinks,DistinguishesDevices}` (pkg) | PASS | inodes differing only above bit 32 not falsely grouped; genuine hardlinks still collapse |

Supporting: `TestHeapBackedCleanup_NeverMunmaps` PASS (heap-backed `Cleanup()` never munmaps the GC
buffer — the legacy decode path's safety invariant).

### Subtask gate citations (per-field matrices already executed)
- **3.1** — TC-1..TC-6 + header-size invariant pass; full regression green; gofmt clean;
  `golangci-lint run ./pkg/format/ ./cmd/dcfhfix/` = 0; static G115 63→52.
- **3.2** — version gate + read-old/write-current verified; negatives error cleanly; full `pkg`/`cmd`
  regression green; G115 == 52 (unchanged).
- **3.3** — 10/10 TC pass; coverage 88–100% on new untrusted-input/dispatch code; race gate green;
  v3/v4 goldens + `.gitattributes` committed; whole-tree **0 G115**.

### Non-Functional Tests
- **Reliability / data integrity (NFR5)**: canonical race gate
  `GOFLAGS=-gcflags=all=-d=checkptr=0 go test -race -short ./...` green across every package
  (`cmd/dcfh`, `cmd/dcfhfind`, `cmd/dcfhfix`, `pkg`, `pkg/format`, `pkg/fsdedupe`) — run in the f-phase
  on the same merged HEAD. SHA-1 footer + 8-byte alignment invariants hold across the v3→v4 bump
  (layout assertions fire per-version).
- **Security static (NFR4)**: `golangci-lint run ./...` reports **0 G115** at merged HEAD; G115 fixed
  structurally (zero Dev/Ino suppressions), 55 provably-safe sites annotated. The enforcing `--new`
  staged gate (`.githooks/pre-commit`) is clean.
- **Performance (NFR1)**: current-version (v4) load stays zero-copy — `TestRoundTrip_V4_ByteIdentical`
  reload is not `heapBacked`; the heap transcode fires only on the rare pre-v4 legacy branch.

## Test Failures
None on the merged tree. (The two expected-update failures during 3.3 authoring — `TestCachePortability`
136→144 and the v3-golden hash framing — were corrected within 3.3; suite is green here.)

## Coverage Report
Per-subtask profiles carry the real figures (a filtered cross-subtask re-run under-reports because only
the gate tests execute). 3.3 recorded `pkg/format` 58.8% overall with the new untrusted-input/dispatch
code well covered: `TranscodeLegacyIndex` 88.9%, `transcodeEntry` 100%, `layoutForVersion` 100%,
`StrategyForVersion` 100%, `NewSafeEntry` 94.4%, `GetDev` 100%. The full race suite passes across all
packages.

## Status
**Status**: Finished
**Next Action**: /cwf-retrospective 3
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
TC-1..TC-6 all PASS on merged HEAD `fd3546e` via their named tests; race suite green across all packages;
whole-tree 0 G115. Three pre-existing non-G115 lint findings (cyclop ×2, unparam ×1) remain in untouched
functions, backlogged, not regressions. Rollout (h) and maintenance (i) are N/A for an internal
library/CLI change with no deployment surface.

## Lessons Learned
The cross-subtask test close-out is best run as named-test verification on the merged tree plus citation
of per-subtask coverage, not a re-derivation: a filtered re-run under-reports coverage (only gate tests
execute), so the real figures must come from the subtask profiles. The named-test mapping (parent TC →
concrete `Test*` functions) is the durable record that the abstract gate cases are actually exercised.
Full synthesis in j-retrospective.md.

## Security Review

**State**: no findings

no findings: empty changeset

The `security-review-changeset --phase=testing` helper emitted 0 files (anchor `885a4ef`). The
testing-phase changeset is test code and workflow docs only — no new production attack surface — and
the cross-subtask gate re-runs existing tests rather than adding production code. The production
untrusted-input surface (the v2/v3 transcoder) was reviewed semantically in 3.3's f-phase with no
findings. The standing Go security floor is gosec via golangci-lint: whole-tree **0 G115** at merged
HEAD.
