# Add version registry and decode path - Implementation Execution
**Task**: 3.2 (feature)

## Task Reference
- **Task ID**: internal-3.2
- **Task URL**: N/A (internal task)
- **Parent Task**: 3
- **Branch**: feature/3.2-add-version-registry-and-decode-path
- **Template Version**: 2.1

## Goal
Execute the implementation following the plan in d-implementation-plan.md and e-testing-plan.md.

## Execution Checklist
- [x] Read d-implementation-plan.md and e-testing-plan.md thoroughly
- [x] Verify all prerequisites met (3.1 merged at baseline; pkg/format owns version constants)
- [x] Execute implementation steps sequentially
- [x] Update "Actual Results" for each step
- [x] Document any deviations from plan
- [x] Update status to "Finished" when complete

## Actual Results

### Step 1: Resolver (`pkg/format/version_dispatch.go`) — test-first
- **Planned**: `DecodeStrategy` enum (`DecodeReject`, `DecodeZeroCopy`; no `DecodeHeap`) +
  `StrategyForVersion` switch-with-default; table test for positive/rejection boundaries.
- **Actual**: Added `version_dispatch.go` (current + legacy as separate `DecodeZeroCopy` arms so 3.3
  flips one) and `version_dispatch_test.go` (TC-1: `{2,3}`→ZeroCopy; `{0,1,4,0xFFFFFFFF}`→Reject,
  errors name version + range; no panic). `go test ./pkg/format/` green.
- **Deviations**: None.

### Step 2: Write-version ownership (`pkg/format/header.go` + callers)
- **Planned**: drop `version` param from `SetHeaderForWritableIndex` (source `CurrentIndexVersion`);
  migrate `temp_index_writer.go:99,176`; re-point `writeEmptyIndex` (index.go) to it.
- **Actual**: Signature changed; both temp-writer callers and the empty-index write migrated.
  `go build ./...` confirmed every caller migrated; grep confirmed no remaining caller passes a
  version literal. `SetHeader(…, version, …)` kept for the repair tool / tests.
- **Deviations**: None.

### Step 3 + 3b: Gate the load paths + header-size bounds guard (`pkg/index.go`)
- **Planned**: version gate at the head of `collectEntryRefs`; version gate + header-size guard in
  `loadIndexFromFileWithTracking`; header-size guard in `openAndValidateIndex`. Step 3b fixes the
  latent v3-header truncation over-read (NFR5).
- **Actual**: Both protections were consolidated into **one shared helper**,
  `checkEntryRegionAccess(header, fileSize) error`, called by the two top-level mmap loaders
  (`openAndValidateIndex` and `loadIndexFromFileWithTracking`). `collectEntryRefs` is now gated by
  its sole caller (`openAndValidateIndex`) and carries a precondition comment instead of its own
  gate. `StrategyForVersion`/`ValidateVersion` coexistence comment added per the design.
- **Deviations** (3, all documented with rationale below): helper consolidation; a use-after-munmap
  bug found-and-fixed during execution; an entry-loop extraction to hold the complexity gate.

### Step 4: Load-path negatives + truncation (`pkg/version_dispatch_load_test.go`, new)
- **Planned**: out-of-range version via both loaders; v3-header truncation `[88,103]` via both
  loaders; `L==104` empty-index success boundary; all under `-race`.
- **Actual**: Added TC-2/3 (`CurrentIndexVersion+1`, `0xFFFFFFFF` rejected via the dcfhfind
  `version:0` path and the tracking path) and TC-4 (sizes `{88,90,103}` error cleanly; `104` loads
  empty). Fixtures are crafted unclean so the loaders skip checksum, isolating the gates under test.
  All pass; **race-clean** (the truncation case previously segfaulted — see deviation 2).
- **Deviations**: None vs the test plan.

### Step 5: Static floor + sanity
- **Planned**: G115 == 52 via the 3.1 un-exclude/run/revert method; grep for open-coded version
  dispatch / version-passing writers.
- **Actual**: **G115 == 52** confirmed (un-excluded G115, `golangci-lint run ./...` counted 52,
  reverted; `.golangci.yml` byte-identical after — empty `git diff`). Grep: version handling routes
  through `headerSizeForVersion` / `StrategyForVersion` only; no writer passes a version.
  `golangci-lint run ./pkg/...` leaves only two **pre-existing** findings in untouched files
  (`filter_run.go:75` cyclop, `binary_entry_scan_test.go:200` unparam — both present on baseline).

## Deviations from Plan (with rationale)

1. **Gate consolidated into one helper, `checkEntryRegionAccess`.** The plan placed the version gate
   at the head of `collectEntryRefs` and the header-size guard separately in `openAndValidateIndex`
   + the tracking loader. During execution this duplicated the version check across two loaders and
   split the two related safety checks. Consolidating both into one helper, called by the two
   top-level loaders, is DRY, makes the version→materialisation decision single-owned (the task's
   whole point), and keeps the dcfhfind `version:0` security property (the gate runs in
   `openAndValidateIndex`, which always precedes `collectEntryRefs` on that path). `collectEntryRefs`
   carries a precondition comment. **Design intent preserved; call site moved up one frame.**

2. **Latent use-after-munmap, found and fixed during execution.** My first cut of the header-size
   guard called `cleanup()` / `indexFile.DecRef()` (which munmaps the mmap) and *then* read
   `header.Version` (a pointer into that mmap) when formatting the error — a use-after-free that
   SIGSEGV'd under the TC-4 truncation test. The shared helper structurally removes the bug class:
   it forms the full error string (version materialised by value) **before** returning, so the
   caller may munmap immediately afterwards. The TC-4 test (which caught it) now passes under
   `-race`.

3. **Extracted `parseTrackedEntries` from `loadIndexFromFileWithTracking`.** `loadIndexFromFile-
   WithTracking` sat exactly at the cyclop limit (20) on baseline; the single safety-gate branch
   tipped it to 21. Rather than suppress the linter, the entry-walk loop was extracted into a
   private method (behaviour-preserving; DecRef-on-error stays with the owner). This drops the
   function below the threshold and mirrors the validation loader's `openAndValidateIndex` +
   `collectEntryRefs` split. Full suite green confirms equivalence.

## Testing-plan note (carried to g-testing-exec)
The e-plan called for the full suite under `-race`. The project's canonical race gate is
`GOFLAGS="-gcflags=all=-d=checkptr=0" go test -race -short ./...` (`.githooks/pre-commit:102-105`):
`checkptr` is deliberately disabled because the codebase uses intentional `unsafe.Pointer`
arithmetic for mmap/zero-copy operations that `checkptr` incorrectly flags. Under that invocation
the **full suite passes under `-race`**, and the pre-commit hook ran it green at this phase's commit.
A naive `go test -race` (checkptr on) instead trips on the pre-existing zero-copy path
(`binaryEntryRef.GetBinaryEntry`, `TestBESkiplist`) — verified pre-existing (fails identically on
baseline with this task's changes stashed); that is exactly why the project pins `checkptr=0`. The
new tests (TC-1/2/3/4) pass under both invocations, so "no over-read / no double-free" keeps a
detection mechanism either way.

## Deferral Check
- [x] All steps from d-implementation-plan.md executed
- [x] All success criteria from a-task-plan.md met (dispatch single-owned; write version owned;
      negatives reject cleanly; v2/v3 byte-correct; G115 == 52)
- [x] All requirements from b-requirements-plan.md addressed (FR1–FR4, NFR1–NFR5)
- [x] All design guidance in c-design-plan.md followed (deviations recorded above)
- [x] No planned work deferred without approval — design-approved 3.3 deferrals unchanged
      (`DecodeHeap` arm, dcfhfix read path, `BEIndexFileIOEntry.readEntryData`)

## Status
**Status**: Finished
**Next Action**: /cwf-testing-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Lessons Learned
- Reading a pointer into an mmap *after* the unmap is an easy ordering trap; form error strings
  before releasing the backing memory. A shared helper that returns a fully-formed error removes the
  trap structurally rather than relying on per-site discipline.
- A function sitting exactly at a complexity limit turns any new branch into a lint failure —
  extracting a cohesive sub-block is the honest fix, not a suppression.

## Security Review

**State**: no findings

no findings: empty changeset

The `security-review-changeset --phase=implementation` helper emitted 0 files (anchor
`e6c966b`). This task's diff is entirely Go application code (`pkg/format/*.go`,
`pkg/index.go`, `pkg/temp_index_writer.go`, tests), which is outside the helper's
security-relevant pathspec (CWF-internal tooling + shebang scripts) by design. The Go code's
security floor is gosec via golangci-lint (CLAUDE.md Security Review §1): G115 == 52 (unchanged),
and `golangci-lint run ./pkg/...` shows no new findings from this task.
