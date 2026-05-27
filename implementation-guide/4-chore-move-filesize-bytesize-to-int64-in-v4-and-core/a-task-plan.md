# Move FileSize/ByteSize to int64 in v4 and core - Plan
**Task**: 4 (chore)

## Task Reference
- **Task ID**: internal-4
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/4-move-filesize-bytesize-to-int64-in-v4-and-core
- **Baseline Commit**: 79e4bc55059aaa6cf7700f16da4894792998800e
- **Template Version**: 2.1

## Goal
Reinterpret the on-disk `FileSize`/`ByteSize` field as signed `int64` (the
native type of `os.FileInfo.Size()`) throughout `pkg/format` and core, so the
signed↔unsigned friction is gone and the file-size G115 suppressions Task 3.3
added can be retired — without changing the on-disk format (v4 stays v4).

## Success Criteria
- [ ] **SC1 — Signed type, no format bump**: `format.ByteSize` is `int64`;
  `CurrentIndexVersion` stays 4; the v4 round-trip golden
  (`TestRoundTrip_V4_ByteIdentical`) and layout anchor still pass byte-for-byte
  (this is a signedness reinterpretation of an already-8-byte field, not a layout
  change).
- [ ] **SC2 — Size G115 suppressions retired**: every `//nolint:gosec` whose
  rationale is a `FileSize`/`ByteSize`/`.Size()` signed↔unsigned conversion is
  removed because the conversion no longer exists (notably
  `binary_entry_scan.go` `entry.FileSize = uint64(fileInfo.Size())` becomes a
  direct assignment). `golangci-lint run ./...` reports **0 G115** whole-tree and
  the `--new` staged gate is clean.
- [ ] **SC3 — Legacy reads stay correct, corruption fail-closed**: the v2/v3→v4
  transcode reinterprets the existing 8-byte on-disk size as signed; v2/v3/v4
  golden round-trips pass; a negative (corrupt/over-large legacy) size is
  rejected by the codec/validator rather than silently propagated.
- [ ] **SC4 — Suite + race green**: full suite and the canonical race gate
  `GOFLAGS=-gcflags=all=-d=checkptr=0 go test -race -short ./...` pass across
  every package.

## Original Estimate
**Effort**: 0.5–1 day
**Complexity**: Low–Medium (one type flip; the work is the ripple, not the idea)
**Dependencies**: Stacks on Task 3 (`79e4bc5`), which delivered `pkg/format` as
the single layout owner and the live G115 gate this task draws down.

## Major Milestones
1. **Compiles signed**: flip the `ByteSize` alias to `int64`; retype the three
   non-alias consumers that won't follow it — the filter `FileSize() (uint64,…)`
   interface + its adapters, `EntryInfo.FileSize`, `EntryJSON.FileSize` — so the
   whole tree builds.
2. **G115 drawn down**: delete the size-conversion suppressions now that the
   casts are gone; whole-tree golangci-lint at 0 G115.
3. **Legacy + corruption proven**: guard negative sizes in the decode/validate
   path; v2/v3/v4 goldens + full race suite green.

## Risk Assessment
### High Priority Risks
- **R1 — Signed reinterpretation of legacy bytes**: a legacy on-disk size with
  bit 63 set reinterprets as negative `int64`. Real file sizes are bounded far
  below 2⁶³, so genuine data is unaffected, but corrupt/hostile input could now
  surface as a negative size.
  - **Mitigation**: treat negative `FileSize` as corruption — reject it in the
    codec/recovery validator (the existing `MaxFileSize 1<<62` upper bound gains
    a `>= 0` lower bound); the v2/v3 goldens prove honest data still decodes.

### Medium Priority Risks
- **R2 — Non-alias consumers silently break or re-introduce casts**: the filter
  `FileSize()` interface, `EntryInfo.FileSize`, and `EntryJSON.FileSize` are bare
  `uint64`, not the `ByteSize` alias, so they don't follow the flip.
  - **Mitigation**: retype them to signed in the same change; the compiler flags
    every mismatched call site, so "build is green" is itself the coverage.
- **R3 — Stale suppression left live**: removing a `//nolint:gosec` without the
  underlying cast actually disappearing leaves an active G115 (or, worse,
  removing one that guards a still-unsigned conversion elsewhere — e.g. the
  `fsdedupe` byte-total accumulators, a distinct type, not FileSize).
  - **Mitigation**: scope suppression removal strictly to FileSize/.Size()
    bridges; let whole-tree + `--new` golangci-lint be the gate; leave unrelated
    byte-total suppressions to a future task if they don't trivially follow.

## Dependencies
- Task 3 tip `79e4bc5` (pkg/format ownership; G115 gate active).
- No external/team dependencies — internal library + CLI change.

## Constraints
- **No on-disk format change**: `CurrentIndexVersion` stays 4; existing v4
  indices and goldens must round-trip byte-identical.
- **G115 stays structural**: suppressions are retired by removing the conversion,
  never by adding new `//nolint` — the Task 3 rule that size/inode truncations
  are fixed in the type, not suppressed, still holds.
- Backlog wording note: the item says "uint32→int64 widen in the transcoder" —
  inaccurate; `FileSize` was always 8-byte `uint64` in v2/v3/v4, so there is **no
  width change**, only signedness. Do not introduce a spurious narrow/widen.

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [ ] **Time**: Will this take >1 week? No — ~1 day.
- [ ] **People**: Does this need >2 people? No.
- [ ] **Complexity**: Does this involve 3+ distinct concerns? Borderline (format
  / core / filter interface / cmd), but they are one atomic type flip that cannot
  land in halves.
- [ ] **Risk**: High-risk components needing isolation? No — R1 is contained to
  the legacy decode guard.
- [ ] **Independence**: Can parts be worked on separately? No — a half-flipped
  alias does not compile.

**Decision**: Do not decompose. A single alias change plus its compiler-enforced
ripple must land as one commit.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All four success criteria met (see g-testing-exec.md). SC1: `ByteSize` is `int64`,
v4 round-trip byte-identical, `CurrentIndexVersion` unchanged. SC2: 7 size G115
suppressions retired by removing the cast; 0 G115 whole-tree, `--new` clean. SC3:
v2/v3→v4 goldens decode unchanged; negative size fail-closed. SC4: full suite +
race green. Effort within the 0.5–1 day estimate.

## Lessons Learned
Decomposition decision (do not decompose — atomic alias flip) was correct: the
change could not have landed in halves (a half-flipped alias does not compile).
The risk assessment held — R1/R2/R3 were all mitigated as written.
