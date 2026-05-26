# Add version registry and decode path - Retrospective
**Task**: 3.2 (feature)

## Task Reference
- **Task ID**: internal-3.2
- **Task URL**: N/A (internal task)
- **Parent Task**: 3
- **Branch**: feature/3.2-add-version-registry-and-decode-path
- **Template Version**: 2.1
- **Retrospective Date**: 2026-05-24

## Executive Summary
- **Duration**: ~1 day (estimated 1–2 days; within estimate).
- **Scope**: Delivered the non-speculative half of the parent's read-old/write-new model —
  an owned `version → strategy` dispatch seam, write-version ownership, and negative-path
  tests. The concrete legacy *entry* decoder was deliberately deferred to 3.3 (no divergent
  layout exists to decode until v4). Standalone-vs-fold decision was surfaced pre-exec and
  confirmed standalone.
- **Outcome**: Success. All 5 success criteria met; one latent memory-safety bug on the load
  path found and fixed beyond the stated scope.

## Variance Analysis
### Time and Effort
- **Estimated**: 1–2 days total (Low–Medium complexity; structural refactor + tests, no format change).
- **Actual**: ~1 day across all phases (a→j) in a single focused work stream. Planning/requirements/
  design were light (parent task 3 carried most context); implementation + testing exec held the bulk.
- **Variance**: Within estimate. The pre-exec scope finding (3.1 had already landed the version
  constants, `HeaderSizeForVersion`, and `ValidateVersion`) pulled the residual 3.2 work toward the
  lower bound — the task was a thin seam, not a new subsystem.

### Scope Changes
- **Additions**:
  - Fixed a **latent v3-header truncation over-read** (Step 3b): an 88–103-byte file with a v3
    header passed the `V2HeaderSize` size gate, then `data[104:]` panicked. Now fails closed. This
    was in-scope as a guard but turned out to be a real pre-existing bug, not just hardening.
  - Found and fixed a **use-after-munmap** I introduced in the first guard cut (read `header.Version`
    after `DecRef()` unmapped the backing mmap) — caught by the planned TC-4 truncation test.
- **Removals (deferred to 3.3, by design)**: the concrete `DecodeHeap` legacy entry decoder, dcfhfix
  read-path resolver adoption, and route-or-delete of `BEIndexFileIOEntry.readEntryData`. No divergent
  entry layout exists until v4, so building the decoder now would be speculative generality.
- **Impact**: Net scope was smaller than the parent design's 3.2 sketch (decoder deferred) but the
  load-path safety guard delivered concrete value the sketch had not isolated.

### Quality Metrics
- **Test Coverage**: `StrategyForVersion` 100% (all three arms). New load-path negatives (TC-2/3/4)
  exercise both loaders. No coverage regression on full `pkg`/`cmd`.
- **Defect Rate**: 1 defect found-and-fixed during exec (use-after-munmap); 0 escaped. 1 latent
  pre-existing bug (v3 truncation over-read) closed.
- **Static analysis**: gosec G115 == 52 (unchanged vs 3.1 baseline); no new golangci-lint findings.
- **Performance**: dispatch gates per-load (helper at loader head), not per-entry — zero-copy retained,
  no per-entry allocation added.

## What Went Well
- **Test-first caught a real memory-safety bug.** TC-4 (planned in e-testing-plan) immediately
  SIGSEGV'd on the first guard implementation, surfacing the use-after-munmap before it could ship.
- **Consolidation over duplication.** Folding the version gate + header-size guard into one shared
  `checkEntryRegionAccess` (called by both top-level mmap loaders) made the version→materialisation
  decision single-owned — the task's whole point — while preserving the dcfhfind `version:0` security
  property (`openAndValidateIndex` always precedes `collectEntryRefs`).
- **Honest complexity fix.** When the new branch tipped `loadIndexFromFileWithTracking` past the
  cyclop limit, the fix was extracting `parseTrackedEntries`, not suppressing the linter.
- **Scope discipline.** The "concrete over generic" call (defer the entry decoder to 3.3) held; no
  speculative version-decode engine was built against a single real layout.

## What Could Be Improved
- **The use-after-munmap was self-inflicted** — reading a pointer into an mmap after unmapping it is
  an avoidable ordering error. The structural fix (form the full error string before releasing the
  mapping) is now in place, but the lesson is to treat mmap lifetime like any other freed resource.
- **The `-race` story cost a detour.** A naive `go test -race` aborts on the pre-existing zero-copy
  `checkptr` flag; it took stashing the change to confirm the failure was pre-existing and to locate
  the project's canonical `checkptr=0` gate (`.githooks/pre-commit:102`). An initial f-exec note was
  wrong and had to be corrected. The canonical gate should be the first thing reached for, not rediscovered.

## Key Learnings
### Technical Insights
- Form error strings from mmap-resident fields **before** any `DecRef()`/unmap; a shared helper that
  returns a fully-formed error removes the use-after-free trap structurally rather than per-site.
- A function sitting exactly at a complexity limit turns any new branch into a lint failure —
  extracting a cohesive sub-block is the correct response.
- The dcfhfind validation path builds `MetaStore{version:0}`, making `ValidateVersion` a no-op for any
  on-disk version; the version *resolver* (default→reject) is therefore the only real version gate on
  that path. Worth keeping explicit in the resolver doc comment for 3.3.

### Process Learnings
- Verify the project's canonical test invocation early when a standard tool (here `-race`) behaves
  unexpectedly — the answer was already encoded in the pre-commit hook.
- Scope findings measured against the *current* tree (not the original parent design) prevented
  building a decoder with nothing to decode. Re-measuring before exec paid off.

### Risk Mitigation Strategies
- The "v2 + v3 byte-correct load + negative-path" gate planned up front was exactly what proved the
  refactor changed no load semantics and caught the truncation/over-read class.

## Recommendations
### Process Improvements
- When a workflow's exec phase depends on a plan phase, confirm the plan phase exists before starting
  exec (e-testing-plan had to be written before g could run this time).

### Tool and Technique Recommendations
- Keep the `GOFLAGS="-gcflags=all=-d=checkptr=0" go test -race` invocation documented as *the* race
  gate; it is non-obvious and was briefly mis-recorded.

### Future Work (→ Task 3.3)
- Flip the `DecodeZeroCopy` legacy arm to `DecodeHeap` and add the concrete legacy entry decoder when
  v4 diverges the entry layout.
- Adopt the resolver in the dcfhfix repair read path (`entry_workflow_main.go:122`) and
  route-or-delete `BEIndexFileIOEntry.readEntryData` (`binary_entry_index_file.go:73`).
- Widen `Dev`/`Ino` to `uint64`, fix the dupes `[2]uint32` key, bump format to v4, re-enable gosec G115.
- Keep `StrategyForVersion` and `ValidateVersion` version ranges in lockstep on the v4 bump.

## Status
**Status**: Finished
**Next Action**: Task complete
**Blockers**: None identified
**Completion Date**: 2026-05-24
**Sign-off**: Matt Keenan / Claude Opus 4.7

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Archived Materials
- Planning/design: a–e workflow files in this task directory.
- Implementation/testing: f-implementation-exec.md, g-testing-exec.md (TC-1..6 + NFR gates, all PASS).
- Key commits: dispatch seam + gate (`pkg/format/version_dispatch.go`, `pkg/index.go`,
  `pkg/temp_index_writer.go`), tests (`pkg/format/version_dispatch_test.go`,
  `pkg/version_dispatch_load_test.go`).
