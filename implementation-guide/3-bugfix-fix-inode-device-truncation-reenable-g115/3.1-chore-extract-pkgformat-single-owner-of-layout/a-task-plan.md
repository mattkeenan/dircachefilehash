# Extract pkg/format single owner of layout - Plan
**Task**: 3.1 (chore)

## Task Reference
- **Task ID**: internal-3.1
- **Task URL**: N/A (internal task)
- **Parent Task**: 3
- **Branch**: chore/3.1-extract-pkgformat-single-owner-of-layout
- **Baseline Commit**: 4cbe4ae0520f82755bf9bdfc54853386a46fd78c
- **Template Version**: 2.1

## Goal
Create `pkg/format` as the single source of truth for the on-disk layout (type vocabulary +
canonical entry/header + bounds-checked codec + version constants/validation), migrate the core
package and dcfhfix onto it, and delete the duplicated layout definitions — with **no change** to
on-disk format, field widths, or behaviour.

## Success Criteria
- [ ] `pkg/format` is the **only** definition of the entry/header layout, vocabulary types,
      version constants, `headerSizeForVersion`, and `ValidateVersion`; a `grep` finds no other
      layout/offset/width declarations.
- [ ] Both dcfhfix duplicates (`binaryEntry`, `indexHeader`) and the parallel `unsafe.Offsetof`
      table are deleted; dcfhfix imports `pkg/format`.
- [ ] Byte-for-byte round-trip test passes over a **v2 and a v3** fixture (read → re-serialise →
      identical bytes).
- [ ] Full existing suite (`go test ./pkg/... ./cmd/...`) green; no behaviour/width/version change.
- [ ] G115 site-count baseline recorded; gosec G115 count unchanged vs baseline at completion.

## Original Estimate
**Effort**: ~2–4 days
**Complexity**: Medium
**Dependencies**: Parent task 3 baseline (`4cbe4ae`)

## Major Milestones
1. **Scaffold**: `pkg/format` with vocabulary + canonical `Entry`/header + codec + version
   constants/validation; compiles standalone.
2. **Core migration**: `binaryEntry`→`format.Entry`, accessor return types, `entry_serialiser.go`,
   version logic move; suite green.
3. **dcfhfix migration**: import `pkg/format`; delete both duplicates + offset table; suite green.
4. **Gates**: v2/v3 round-trip + G115-baseline checks pass.

## Risk Assessment
### High Priority Risks
- **Hidden width-coupled call site missed** (wide blast radius): a `.Dev`/`.Ino` consumer not on
  the inventory breaks compilation or, worse, changes behaviour.
  - **Mitigation**: `grep '\.Dev\|\.Ino'` + the `Dev()/Ino()` accessors before sizing; the
    byte-for-byte round-trip + full suite are the safety net; a type *alias* keeps any narrowing a
    compile error.

### Medium Priority Risks
- **Version-logic move creates a second gate or breaks call sites**: `headerSizeForVersion` has
  ~5 call sites in `index.go`; `ValidateVersion` must not be duplicated.
  - **Mitigation**: all-or-nothing move; exactly one surviving version gate; compiler catches the
    call sites.
- **Exporting the canonical entry type leaks internals.**
  - **Mitigation**: export only the layout contract + codec; keep the bounds-check
    internal/non-bypassable; document the zero-copy/host-order invariants in the package.

## Dependencies
- Parent task 3 (baseline `4cbe4ae`); no external dependencies.

## Constraints
Inherited from parent (not restated): one place; host-order zero-copy preserved; **no
width/version/behaviour change in 3.1**; vocabulary prefers a type alias; British spelling.

## Decomposition Check
- [ ] **Time**: <1 week.
- [ ] **People**: 1.
- [ ] **Complexity**: single concern (mechanical extraction + migration).
- [ ] **Risk**: contained by the round-trip + full-suite gates.
- [ ] **Independence**: not further separable usefully.

**Outcome**: 0 signals → no further decomposition; proceed as a single subtask.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All success criteria met. `pkg/format` is the single owner of layout (grep-verified, cycle-free via
`go list`); both dcfhfix duplicates + the offset table deleted; v2/v3 round-trip + header-size +
version-offset tests pass; full suite green incl. `-race`; G115 63 → 52 (not increased). Two latent
dcfhfix defects fixed in-flight (GetPath `Path[8]` read; header 8-byte over-read). Delivered well
under the 2–4 day estimate. See j-retrospective.md.

## Lessons Learned
The type-alias strategy made the migration a compile-checked near-zero-diff; for such chores,
estimate by duplicate-definitions-to-delete, not call-site count. See j-retrospective.md.
