# Extract pkg/format single owner of layout - Testing Plan
**Task**: 3.1 (chore)

## Task Reference
- **Task ID**: internal-3.1
- **Task URL**: N/A (internal task)
- **Parent Task**: 3
- **Branch**: chore/3.1-extract-pkgformat-single-owner-of-layout
- **Template Version**: 2.1

## Goal
Prove the `pkg/format` extraction preserves behaviour and on-disk bytes exactly, and that the
codec keeps the two-tier bounds-check invariant on untrusted input.

## Test Strategy
### Test Levels
- **Unit** (`pkg/format`): codec typed read/write + both bounds tiers; layout assertions hold.
- **Integration**: round-trip read→re-serialise→write through `pkg/format`; dcfhfix write path.
- **Regression**: full existing `go test ./pkg/... ./cmd/...` suite, unchanged, green.
- **Negative**: malformed/corrupt buffers error rather than panic or over-read.
- **Static**: gosec G115 site-count diff against the recorded baseline (63).

### Coverage Targets
- `pkg/format` codec bounds-check branches: 100% (the safety floor migrated from dcfhfix).
- No regression in existing coverage; all pre-existing tests pass unchanged.

## Test Cases
### Functional
- **TC-1 — v3 round-trip (byte-identical)**
  - **Given**: a valid v3 index produced by current tooling.
  - **When**: read → re-serialise via `pkg/format` → write.
  - **Then**: output bytes are byte-for-byte identical to input.
- **TC-2 — v2 round-trip + parse-offset assertion**
  - **Given**: a **synthesised** v2 fixture (tooling writes only v3; build it in-test).
  - **When**: parsed via `pkg/format`.
  - **Then**: entry data is read at offset **88** (`V2HeaderSize`), not 96; round-trip is
    byte-identical; checksum ordering (`index.go:296-314`) preserved.
- **TC-3 — dcfhfix write-path header**
  - **Given**: dcfhfix writing an index header via `format.Header` (was the 96-byte duplicate).
  - **When**: the header is written and re-read.
  - **Then**: 104-byte header is correct (Timestamp/padding); the prior 8-byte over-read at
    `main.go:1537` is gone, not regressed.
- **TC-4 — codec tier-1 (entry-level) bounds**
  - **Given**: buffers with `Size`==0, `Size`<`minEntrySize`, `Size`>4096, `offset+Size`>`len`.
  - **When**: decoded.
  - **Then**: each errors with a clear message; no panic.
- **TC-5 — codec tier-2 (field-level vs maxOffset) bounds** *(the regression the design phrasing risked)*
  - **Given**: a buffer that is large overall but whose entry `Size` ends *before* a field being read.
  - **When**: that field is read via the codec.
  - **Then**: it errors (read bounded by the entry's declared `maxOffset`, not `len(buf)`); it
    does **not** silently read into the next entry.
- **TC-6 — truncated buffer**
  - **Given**: an index truncated mid-entry / mid-header.
  - **When**: opened.
  - **Then**: clean error, no over-read.

### Non-Functional
- **Reliability / integrity**: SHA-1 footer + 8-byte alignment + layout assertions unchanged
  (assertions still fire on malformed `Size`).
- **Performance**: no change expected (pure extraction); current-version zero-copy load path
  untouched — spot-check existing benchmarks show no regression.
- **Security (static)**: G115 count == 63 after extraction (locations may shift; count is the
  invariant).

## Test Environment
### Setup Requirements
- Go 1.24.3; `golangci-lint` with gosec for the G115 diff (temporarily un-exclude G115 via
  `.golangci.yml:59-60`, run, revert).
- Fixtures in `t.TempDir()`; v2/v3 fixtures generated programmatically in-test (or committed
  under `pkg/format/testdata/` — document which). **Never** touch the repo's own `.dcfh/`.

### Automation
- `go test ./pkg/... ./cmd/...` at the 3.1 gate.
- A scripted G115 baseline check (un-exclude → run → count → revert).

## Validation Criteria
- [ ] TC-1..TC-6 pass
- [ ] `pkg/format` bounds-branch coverage target met
- [ ] Full regression suite green
- [ ] G115 count == 63 (unchanged); locations may differ
- [ ] No zero-copy load-path performance regression

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All TC-1..TC-6 (+ TC-3b header-size invariant) pass (see g-testing-exec.md, commit 5dc2e5e). Codec
reachable bounds branches 100%; full suite green incl. `-race`; G115 63 → 52. The round-trip
integration tests (TC-1/2/3) were landed in g (they need real index fixtures); the codec bounds
tests (TC-4/5/6) landed in f.

## Lessons Learned
TC-5 (field-vs-maxOffset) is only meaningfully reachable for the variable-length path; fixed-field
error branches are unreachable given tier-1, and were documented rather than force-tested. See
j-retrospective.md.
