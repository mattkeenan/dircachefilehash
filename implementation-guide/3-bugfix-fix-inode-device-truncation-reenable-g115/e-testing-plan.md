# Fix inode device truncation re-enable G115 - Testing Plan
**Task**: 3 (bugfix)

## Task Reference
- **Task ID**: internal-3
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: bugfix/3-fix-inode-device-truncation-reenable-g115
- **Template Version**: 2.1

## Scope of this plan
Parent-level **cross-subtask** test strategy: the shared fixtures, the gate cases that prove the
sequencing is safe, and the standing invariants every subtask's tests must satisfy.
**Per-field / per-call-site test matrices are delegated to each subtask's `e-` plan.**

## Goal
Prove the extraction is behaviour-preserving and the format change is safe against both valid and
malformed multi-version input, without regressing the zero-copy fast path.

## Test Strategy
### Test Levels
- **Unit**: `pkg/format` codec (typed read/write + bounds check), vocabulary widths, version
  registry lookup/clamp, layout assertions.
- **Integration**: round-trip read→re-serialise→write through `pkg/format`; cross-version read
  (v2/v3 → canonical entry); dcfhfix reading via the shared codec.
- **Regression**: the full existing `go test ./pkg/...` suite, unchanged, green at every gate.
- **Negative / malformed-input**: corrupt/short buffers and bad version bytes must error.
- **Golden-file**: byte-exact fixtures pin on-disk layout per version.

### Shared fixtures (the cross-subtask seam)
- `v2.idx`, `v3.idx` — minimal valid indices at each current version (3.1 onward).
- `v4.idx` — golden file added in 3.3 once the widened layout exists.
- Malformed set — derived from the above: truncated entry region, `Size` below struct minimum,
  `Size` overrunning the buffer, out-of-range / newer-than-current version byte.

### Coverage Targets
- **`pkg/format`**: 100% of the bounds-check and version-reject branches (the safety floor).
- **Critical paths**: round-trip equivalence and cross-version decode fully covered.
- **Regression**: all pre-existing tests pass unchanged.

## Test Cases
### Functional (cross-subtask)
- **TC-1 (3.1 gate) — round-trip equivalence**
  - **Given**: a valid `v2.idx` and `v3.idx` fixture.
  - **When**: read → re-serialise via `pkg/format` → write.
  - **Then**: output bytes are identical to input (behaviour-preserving extraction).
- **TC-2 (3.1/3.2) — malformed input errors, never over-reads**
  - **Given**: each malformed fixture.
  - **When**: decoded via `pkg/format` / dcfhfix.
  - **Then**: a clear error is returned; no panic, no out-of-bounds read.
- **TC-3 (3.2) — version gate**
  - **Given**: indices with unknown, newer-than-current, and below-`MinIndexVersion` version bytes.
  - **When**: opened.
  - **Then**: rejected with a defined error; zero-copy cast never fires for non-current versions.
- **TC-4 (3.3) — full-width dev/ino**
  - **Given**: a file whose dev/ino exceed 32 bits.
  - **When**: scanned, written to v4, re-read.
  - **Then**: values round-trip without truncation.
- **TC-5 (3.3) — cross-version decode integrity**
  - **Given**: the `v3.idx` fixture.
  - **When**: decoded into the widened canonical entry.
  - **Then**: **every** post-Ino field (Mode/UID/GID/FileSize/flags/hash/path) is correct, and the
    file is routed through heap decode — *not* cast through the widened struct.
- **TC-6 (3.3) — dupes correctness**
  - **Given**: distinct files whose inodes differ only above bit 32.
  - **When**: `dcfh dupes` runs.
  - **Then**: they are not falsely grouped (full-width dedup key).

### Non-Functional
- **Performance**: the current-version zero-copy load path shows no regression (legacy decode cost
  is bounded and applies only to pre-v4 files). Spot-check against existing benchmarks.
- **Reliability / data integrity**: SHA-1 footer + 8-byte alignment invariants hold across the
  version bump; layout assertions fire for the correct version.
- **Security (static)**: G115 site count diffed against the 3.1 baseline at each subtask boundary;
  zero after 3.3 with `golangci-lint run ./...` clean.

## Test Environment
### Setup Requirements
- Go 1.24.3 toolchain; `golangci-lint` (gosec linter active) for the G115 gate.
- Tests operate on fixtures in `t.TempDir()` — **never** the repository's own `.dcfh/`.
- Fixtures generated programmatically or committed as `testdata/`; document which.

### Automation
- `go test ./pkg/...` and `go test ./cmd/...` per subtask gate.
- `golangci-lint run ./...` (and a G115-enumeration run) wired into each subtask's gate check.

## Validation Criteria
- [ ] TC-1..TC-6 passing at their respective subtask gates
- [ ] `pkg/format` safety-branch coverage target met
- [ ] Regression suite green at every subtask boundary
- [ ] No zero-copy performance regression for current-version loads
- [ ] G115 clean after 3.3; no new narrowing casts introduced earlier

## Status
**Status**: Finished
**Next Action**: /cwf-new-subtask (decompose) — then /cwf-implementation-exec per subtask
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
TC-1..TC-6 all passed at their respective subtask gates and were re-verified end-to-end on the merged
parent HEAD (see g-testing-exec.md). The malformed-input negative cases (the safety floor) caught real
defects: the v3-header truncation over-read (3.2) and a use-after-munmap (3.2) both surfaced through the
planned truncation test, not in production. The golden-file strategy preserved the v3 decode oracle
before the v4 bump removed the v3 writer. G115 site count was diffed at every subtask boundary as
planned; zero after 3.3 with `golangci-lint run ./...` clean.

## Lessons Learned
A negative/malformed-input gate planned up front, not bolted on, is what turned latent over-read bugs
into caught-in-phase fixes. Capturing the byte-exact golden before the producing writer is removed is
mandatory for any destructive format bump. Full synthesis in j-retrospective.md.
