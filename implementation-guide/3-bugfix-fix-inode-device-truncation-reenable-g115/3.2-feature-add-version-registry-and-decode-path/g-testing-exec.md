# Add version registry and decode path - Testing Execution
**Task**: 3.2 (feature)

## Task Reference
- **Task ID**: internal-3.2
- **Task URL**: N/A (internal task)
- **Parent Task**: 3
- **Branch**: feature/3.2-add-version-registry-and-decode-path
- **Template Version**: 2.1

## Goal
Execute the tests defined in e-testing-plan.md and verify implementation from d-implementation-plan.md.

## Execution Checklist
- [x] Read e-testing-plan.md and d-implementation-plan.md thoroughly
- [x] Verify test environment ready (Go 1.24.x, golangci-lint with gosec v2 linter)
- [x] Execute test cases sequentially
- [x] Record pass/fail for each test
- [x] Document failures with reproduction steps
- [x] Update status to "Finished" when all pass

## Test Results

### Functional Tests

| Test ID | Test Case | Expected | Actual | Status | Notes |
|---------|-----------|----------|--------|--------|-------|
| TC-1 | `StrategyForVersion` table (`pkg/format`) | `{2,3}`→ZeroCopy; `{0,1,4,0xFFFFFFFF}`→Reject+range-naming error; no panic | All 6 sub-cases pass; errors name version+range | PASS | 100% func coverage (all arms incl. default) |
| TC-2 | Out-of-range version via dcfhfind path (`version:0`) | clean error, no refs, no panic | `{4, 0xFFFFFFFF}` both rejected; error mentions version | PASS | The path the resolver primarily protects |
| TC-3 | Out-of-range version via tracking path (`version:Current`) | clean error, no panic, mmap released | `{4, 0xFFFFFFFF}` both rejected (belt-and-braces w/ ValidateVersion) | PASS | Clean under `-race` (no double-free) |
| TC-4 | v3-header truncation `[88,103]` via both loaders + `L==104` boundary | sizes `{88,90,103}`→"too small" error (no panic); `104`→empty index loads | all truncation sizes error cleanly; 104 loads 0 refs | PASS | Targets the Step 3b guard; caught the use-after-munmap (now fixed) |
| TC-5 | v2/v3 byte-correct invariance (3.1 gate) | round-trip tests green; v2@88, v3@104 | `TestRoundTrip_{V3_ByteIdentical,V2_ParseOffset,HeaderSizeInvariant}` pass | PASS | No on-disk behaviour change |
| TC-6 | Write-version ownership | written + empty indices carry `CurrentIndexVersion`; no caller passes a version | end-to-end `dcfh init/update` → `main.idx` version byte (offset 16) = 3; compiler+grep confirm no version-passing caller | PASS | Verified on a real written index, not just a fixture |

### Non-Functional Tests

| Gate | Method | Result | Status |
|------|--------|--------|--------|
| Reliability (NFR5) — no over-read/double-free | new tests under `-race` | TC-1/2/3/4 race-clean | PASS |
| Full regression | `go test ./pkg/... ./cmd/...` | all packages ok | PASS |
| Full regression under race | `GOFLAGS="-gcflags=all=-d=checkptr=0" go test -race -short ./pkg/... ./cmd/...` (project canonical gate, `.githooks/pre-commit:102-105`) | all packages ok | PASS |
| Security (NFR4) — static floor | G115 recount via 3.1 un-exclude/run/revert | **52** (unchanged); `.golangci.yml` restored byte-identical | PASS |
| Security (NFR4) — no new lint | `golangci-lint run ./pkg/...` | only 2 pre-existing findings in untouched files (`filter_run.go:75`, `binary_entry_scan_test.go:200`); pre-commit gate `0 issues` | PASS |
| Security — no raw-index of version byte | switch-with-default design + TC-1 rejection arm | structurally guaranteed; `0xFFFFFFFF`→error not index | PASS |
| Performance (NFR1) — zero-copy retained | dispatch gates per-load not per-entry (helper at loader head); benchmarks green | no per-entry allocation added | PASS |

## Test Failures

None in the final run. One failure was encountered and resolved **during implementation** (recorded
in f-implementation-exec.md, deviation 2): the TC-4 truncation case initially SIGSEGV'd because the
header-size guard read `header.Version` after `cleanup()`/`DecRef()` had munmapped the backing data
(use-after-free). Fixed by forming the error in the shared helper before any unmap; TC-4 now passes,
including under `-race`.

**Reproduction note for the `-race` story**: a naive `go test -race ./pkg/` aborts with
`checkptr: pointer arithmetic result points to invalid allocation` in `binaryEntryRef.GetBinaryEntry`
(`TestBESkiplist`). This is **pre-existing** (fails identically on baseline with this task stashed)
and expected — the codebase uses intentional `unsafe.Pointer` mmap/zero-copy arithmetic that
`checkptr` mis-flags, so the project pins `checkptr=0` for its race gate. Use the canonical
invocation above.

## Coverage Report
- `StrategyForVersion`: **100.0%** (all three arms — current, recognised-legacy, default-reject —
  exercised by the TC-1 table).
- `checkEntryRegionAccess` + both loader gates + Step 3b guard: exercised by TC-2/3/4 across both the
  dcfhfind (`version:0`) and tracking (`version:Current`) loaders.
- Full `pkg`/`cmd` regression green (no coverage regression vs baseline).

## Security Review

**State**: no findings

no findings: empty changeset

The `security-review-changeset --phase=testing` helper emitted 0 files (anchor `e6c966b`). The
task's diff is entirely Go (`pkg/format/*.go`, `pkg/index.go`, `pkg/temp_index_writer.go`, tests),
outside the helper's security-relevant pathspec (CWF-internal tooling + shebang scripts). The Go
security floor is gosec via golangci-lint: G115 == 52 (unchanged), no new findings.

## Status
**Status**: Finished
**Next Action**: /cwf-rollout
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Lessons Learned
The project's canonical race gate is `GOFLAGS="-gcflags=all=-d=checkptr=0" go test -race`
(`.githooks/pre-commit:102`); a naive `go test -race` misleads by tripping on the pre-existing
zero-copy `checkptr` path. Reach for the documented invocation first when a standard tool behaves
unexpectedly. See j-retrospective.md.
