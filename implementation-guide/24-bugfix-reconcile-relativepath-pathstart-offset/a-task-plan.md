# Reconcile RelativePath pathStart offset - Plan
**Task**: 24 (bugfix)

## Task Reference
- **Task ID**: internal-24
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: bugfix/24-reconcile-relativepath-pathstart-offset
- **Baseline Commit**: 7c174cea06145a71415aa6b08770eac0fd6394a6
- **Template Version**: 2.1

## Goal
Fix `Entry.calculatePathLength()` so its path-start offset agrees with the
canonical on-disk layout (path appended at `Sizeof(Entry)`), eliminating the
8-byte discrepancy that makes `ValidateEntry()` reject every valid entry under
the `extravalidation` debug flag.

## Context / Root Cause (confirmed)
The `Entry` struct ends with `Path [8]byte`, so `&be.Path[0]` sits at
`Sizeof(Entry) - 8`. Two readers disagree over where path bytes begin:
- `RelativePath()` (entry.go:90) reads from `base + Sizeof(Entry)` — **canonical**.
- `calculatePathLength()` (entry.go:127) reads from `&be.Path[0]` = `Sizeof-8`,
  counting the 8-byte trailing `Path` field as path content and returning a
  length 8 bytes too large.

The authoritative writer settles it: `pkg/entry_serialiser.go:144-145` writes the
path at `pathOffset := int(unsafe.Sizeof(*be))` ("// Path starts after the struct
(matching RelativePath)"). `SafeEntry.GetPath()` (codec.go:216) independently
agrees (`offset + layout.minSize`) and its comment records that the dcfhfix
duplicate which read from the "unused trailing Path[8] field" was a latent bug.
So `calculatePathLength()` is the lone outlier and must be corrected to the
struct-end offset.

Blast radius is tiny and debug-gated: `calculatePathLength()` is called only by
`ValidateEntry()`, which runs only at `pkg/index.go:436` and `:744`, both behind
`IsDebugEnabled("extravalidation")`. With the bug, `expectedSize` is ~8 bytes off
and `ValidateEntry` fails on every well-formed entry — i.e. extravalidation is
currently unusable. Task 10 knowingly preserved the discrepancy byte-for-byte
(checkptr-clean) and deferred the fix to this task.

## Success Criteria
(Scope widened after design-phase discovery — see Update below.)
- [ ] `calculatePathLength()` computes path length from the canonical struct-end
      offset (`Sizeof(Entry)`), matching `RelativePath()` and the writer.
- [ ] `validateLayout()`'s Path-offset assertion is corrected so it no longer
      panics on well-formed entries (was `Sizeof-8`; real offset is
      `Offsetof(Path)`).
- [ ] `ValidateEntry()` *genuinely* passes on a well-formed serialiser-produced
      entry and *genuinely* errors on a corrupt one — no longer a swallowed-panic
      no-op.
- [ ] A test pins path length, the no-panic `validateLayout`, and both the
      positive and negative `ValidateEntry` outcomes so the discrepancy cannot
      silently reappear.
- [ ] Stale "Path is the last 8 bytes" / "tracked as a separate backlog item"
      comments removed; remaining comments describe the corrected layout.
- [ ] `go test ./pkg/...` (incl. the race gate) passes; backlog item retired.

## Update (design phase): scope corrected to "fix both"
The backlog framed this as an "8-byte" discrepancy. Empirical layout measurement
shows `Sizeof(Entry)=144` but `Offsetof(Path)=132` (4 bytes tail padding), so the
over-count is **12 bytes**, and the same false premise breaks a *second* reader,
`validateLayout()`, which panics on every valid entry. Because `ValidateEntry`
runs `validateLayout` inside a `recover()`, `ValidateEntry` has been a no-op that
always returns `nil`. User approved fixing both in this task (still one file). See
c-design-plan.md Decisions 1 and 4.

## Original Estimate
**Effort**: <1 day
**Complexity**: Low
**Dependencies**: None

## Major Milestones
1. **Design**: Confirm canonical offset and choose between (a) correcting the
   offset in place vs (b) collapsing `calculatePathLength` onto `RelativePath`
   to remove the duplicate unsafe arithmetic ("best part is no part").
2. **Implement**: Apply the fix in `pkg/format/entry.go`; update comments.
3. **Test**: Add a pinning test driving a serialiser-produced entry through
   `ValidateEntry`/path-length; confirm it failed before and passes after.

## Risk Assessment
### Medium Priority Risks
- **Risk 1**: Reusing `RelativePath()` from `calculatePathLength()` could panic
  on a corrupt `Size` (RelativePath panics; calculatePathLength is called inside
  ValidateEntry's recover). Impact: behaviour change on corrupt input.
  - **Mitigation**: ValidateEntry already bounds-checks `Size` (minSize..4096)
    before calling calculatePathLength, so RelativePath's guard cannot fire; if
    risk remains, correct the offset in place rather than delegating.

### Low Priority Risks
- **Risk 2**: Legacy (v2/v3) layouts have their own struct sizes; a naive fix
  must use the current `Entry` `Sizeof`, not a hard-coded constant.
  - **Mitigation**: `Entry.calculatePathLength` operates on the v4 `Entry` type
    only; legacy reads go through `SafeEntry`/layout. Use `unsafe.Sizeof(*be)`.

## Dependencies
- None.

## Constraints
- On-disk format is unchanged — this is a read-side reconciliation only.
- Must stay checkptr-clean (zero-copy unsafe); honour the race gate's
  `-d=checkptr=0` caveat.

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [ ] **Time**: Will this take >1 week? No.
- [ ] **People**: Does this need >2 people? No.
- [ ] **Complexity**: 3+ distinct concerns? No — one function, one test.
- [ ] **Risk**: High-risk components needing isolation? No.
- [ ] **Independence**: Separable parts? No.

No decomposition signals triggered — single-function bugfix with a pinning test.

## Status
**Status**: Finished
**Next Action**: /cwf-requirements-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Both fixes landed in `pkg/format/entry.go` with a new `pkg/format/entry_test.go`:
`calculatePathLength` delegates to `RelativePath`; `validateLayout` asserts
`unsafe.Offsetof(be.Path)` instead of `Sizeof-8`. `ValidateEntry` now genuinely
validates (was a swallowed-panic no-op). Full suite + race (checkptr on and off) +
golangci-lint (0 issues) + govulncheck (0) all green. Two CWF security reviews:
no findings. Scope widened from the backlog's "8-byte" to "12-byte + validateLayout"
after empirical measurement (user-approved). See j-retrospective.md.

## Lessons Learned
Re-derive any concrete number a backlog item quotes (the "8-byte" was 12) before
building a plan on it. A `recover()` that discards its value silently converts
"always panics" into "always passes" — `validateLayout` had been latently dead.
See j-retrospective.md for the full set.
