# Fix inode device truncation re-enable G115 - Plan
**Task**: 3 (bugfix)

## Task Reference
- **Task ID**: internal-3
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: bugfix/3-fix-inode-device-truncation-reenable-g115
- **Baseline Commit**: 885a4efc11a2bd7f40364e5d9b2167042ce4632d
- **Template Version**: 2.1

## Goal
Eliminate inode/device truncation by widening `Dev`/`Ino` to 64-bit, and remove the recurring multi-site type-churn by encapsulating the versioned on-disk format (field types, widths, signedness, per-version layout) behind a single codec module — then re-enable the G115 lint gate.

## Success Criteria
- [ ] `Dev`/`Ino` are stored and read as 64-bit; no `uint32(...)` truncation of `stat` dev/ino remains, and `dcfh dupes` keys on full-width values.
- [ ] On-disk field type information (width, signedness, offset, version applicability) is defined in exactly one module; no hand-typed widths or offset tables survive elsewhere (dcfhfix included).
- [ ] Existing v2/v3 indices are read correctly at their original width, and new writes produce v4; round-trip verified by test.
- [ ] G115 removed from `gosec.excludes` in `.golangci.yml` and `golangci-lint run ./...` passes clean.
- [ ] Full existing test suite passes, plus new tests for cross-version read and full-width dev/ino.

## Original Estimate
**Effort**: ~1.5–2 weeks (if delivered as a single task)
**Complexity**: High
**Dependencies**: Task 2 (gosec config that defers G115) — this task's baseline

## Major Milestones
1. **Format codec module**: Single module owns the on-disk format — semantic type vocabulary + generic codec helpers. All current fields migrated to it with no behaviour or width change (still v3).
2. **Version-aware read/write**: The module gains per-version layout descriptors; reads v2/v3, writes the current version. dcfhfix consumes the same descriptors.
3. **Widen Dev/Ino + v4**: `Dev`/`Ino` become 64-bit, format bumps v3→v4, consumer (dupes key) and ingest casts fixed.
4. **Re-enable G115**: Remove the exclude, run full lint + test suite green.

## Risk Assessment
### High Priority Risks
- **On-disk format change + multi-version reads risk data misparse/corruption**: Reading old-width and writing new-width entries is error-prone.
  - **Mitigation**: Encapsulate all versioned layout in one module with version-tagged descriptors; golden-file round-trip tests per version; dcfhfix validates against the same source.
- **Wide refactor blast radius**: Converting all fields touches the struct, ~8 `BinaryEntryInterface` implementers, the serialiser, and dcfhfix's parallel offset table.
  - **Mitigation**: Stage as subtasks; land the no-behaviour-change encapsulation refactor first and verify green before any width or version change.

### Medium Priority Risks
- **dcfhfix must repair all historical versions**: A single compiled offset table cannot describe both v3 (32-bit) and v4 (64-bit) entries.
  - **Mitigation**: Version-keyed descriptors in the shared module, consumed by dcfhfix instead of its hand-rolled table.
- **Re-enabling G115 may surface other int-overflow conversions** beyond Dev/Ino.
  - **Mitigation**: Run gosec early to enumerate all G115 sites; fix or per-line justify with rationale (mirroring existing `//nolint:gosec` style).

## Dependencies
- **Task 2** — provides the gosec/golangci config in which G115 is currently excluded; this task edits that config to re-enable it.
- Zero-copy/mmap and skiplist infrastructure (unchanged, but the format module must keep feeding it host-order entries).

## Constraints
- **One place for format versioning**: All versioned on-disk format code (types, widths, signedness, per-version layout, read/write) must live in a single module/package and not be scattered across the codebase (explicit user directive).
- **Preserve host-order zero-copy fast path** for the current version — no fixed-endian rewrite of the direct struct cast.
- **Format invariants unchanged**: 8-byte alignment, SHA-1 footer, git-compatible header.
- British spelling in prose; no superlatives.

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [ ] **Time**: Will this take >1 week? Likely yes as a single task — borderline trigger.
- [ ] **People**: Does this need >2 people working on different parts? No.
- [x] **Complexity**: Does this involve 3+ distinct concerns? Yes — codec-module encapsulation, all-field migration, version-aware read/write, Dev/Ino widening + v4, lint re-enable.
- [x] **Risk**: Are there high-risk components that need isolation? Yes — on-disk format change and multi-version parsing affect data integrity.
- [x] **Independence**: Can parts be worked on separately? Yes — the no-behaviour-change encapsulation refactor can land and be verified before any width/version change.

**Outcome**: 3 signals triggered (Complexity, Risk, Independence) → **strongly recommend decomposition into subtasks**. Proposed split:
- **3.1 (chore/refactor)**: Establish the format codec module + type vocabulary + generic codec; migrate all current fields; no behaviour, width, or version change (still v3). Lowest-risk, fully regression-testable.
- **3.2 (feature)**: Add version-aware read/write (per-version layout descriptors) to the module; dcfhfix consumes them. Read v2/v3, write current.
- **3.3 (bugfix)**: Widen `Dev`/`Ino` to 64-bit, bump v3→v4, fix dupes key + ingest casts, re-enable G115.

## Status
**Status**: Finished
**Next Action**: /cwf-new-subtask (decompose — recommended) or /cwf-requirements-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All five success criteria met (see f-implementation-exec.md verification): Dev/Ino 64-bit with
full-width dupes key; on-disk format types single-owned in `pkg/format`; v2/v3 read via heap
transcode, writes emit v4; G115 active and whole-tree clean; full race suite green. Delivered in
~4 calendar days across 3 subtasks vs the ~1.5–2 week single-task estimate — decomposition plus the
type-alias vocabulary retired the predicted wide-blast-radius risk. The Decomposition Check (3 signals)
proved correct: the no-behaviour-change extraction (3.1) landed and verified before any width/version
change (3.3).

## Lessons Learned
Estimating a type-system-guarded mechanical refactor by call-site count overshoots — the compiler does
the blast-radius audit. Staging the risky width/version change last, behind a green extraction, was the
decisive call. Full synthesis in j-retrospective.md.
