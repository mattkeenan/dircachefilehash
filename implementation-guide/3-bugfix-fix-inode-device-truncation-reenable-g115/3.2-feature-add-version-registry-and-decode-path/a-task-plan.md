# Add version registry and decode path - Plan
**Task**: 3.2 (feature)

## Task Reference
- **Task ID**: internal-3.2
- **Task URL**: N/A (internal task)
- **Parent Task**: 3
- **Branch**: feature/3.2-add-version-registry-and-decode-path
- **Baseline Commit**: e6c966be2c94f747d3beb054c5a48daace85976a
- **Template Version**: 2.1

## Goal
Make the read-version-dispatch and write-version selection an explicit, single-owned, tested
seam in `pkg/format` — so adding v4 in Task 3.3 is "register one more decoder", not a new
load-path branch — with **no** on-disk format, width, or version change.

## Scope finding (measured against the post-3.1 tree — drives the plan)
3.1 already landed more than the parent design assigned to it: the version **constants**,
`HeaderSizeForVersion`, and `ValidateVersion` already live in `pkg/format` (header.go /
constants.go), single-owned. And v2↔v3 differ **only** in the header offset (88 vs 104,
handled by `headerSizeForVersion`); the **entry** layout is byte-identical across every shipped
version. So the "read-old entry decoder" the parent design sketched for 3.2 has *no divergent
layout to decode* until v4 exists (3.3). Building a multi-version entry-decode engine now would
be speculative generality the parent design explicitly warns against ("concrete over generic").

3.2 therefore delivers the **non-speculative** half of the read-old/write-new model:
1. an explicit, owned **dispatch decision** (`version → {zero-copy current | heap-decode legacy |
   reject}`) replacing today's implicit "cast always fires" (correct only by the v2/v3 layout
   coincidence);
2. **write-version ownership** — `pkg/format` owns the current version for writes; callers stop
   passing it (closes the design's "write-current is owned, not passed" gap, header.go:97);
3. the **negative-path tests** that have value today (reject unknown / newer-than-current /
   below-min; registry never indexed by the untrusted version byte).

The concrete legacy *entry* decoder is deliberately deferred to 3.3, where v4 gives it a second
layout to exercise. **Whether 3.2 stays separate or folds into 3.3 is the headline review
decision** (the parent design, c-design-plan.md:174, permits the fold) — see Decomposition Check.

## Success Criteria
- [ ] Read dispatch is a single owned function in `pkg/format` (`version → strategy`), consulted
      **after** the `ValidateVersion` clamp and keyed by map/switch-with-default — never raw-indexed
      by the version byte; the zero-copy mmap cast fires **only** when `version == current`.
- [ ] Writes source the current version from `pkg/format` (callers no longer pass a version that
      could diverge); a single owned write-version constant.
- [ ] Negative tests prove `Decode`/dispatch rejects unknown, newer-than-current, and below-`Min`
      versions with a clear error (no over-read, no panic, no descriptor selected by indexing).
- [ ] No on-disk format / width / version change: full `go test ./pkg/... ./cmd/...` green incl.
      `-race`; a v2 and a v3 fixture still load byte-correctly.
- [ ] gosec G115 site count unchanged vs the 3.1 baseline (52) at the 3.2 boundary.

## Original Estimate
**Effort**: 1–2 days
**Complexity**: Low–Medium (structural refactor + tests; no format change)
**Dependencies**: 3.1 (merged at baseline `e6c966b`) — `pkg/format` is the single layout owner.

## Major Milestones
1. **Dispatch seam**: explicit owned `version → strategy` resolver in `pkg/format`; load path
   (`index.go` `openAndValidateIndex` / `collectEntryRefs`) consults it; cast hard-gated on current.
2. **Write-version ownership**: `pkg/format` owns the write version; `SetHeaderForWritableIndex`
   callers stop passing a divergent version.
3. **Gate**: read-old(v2/v3)/write-current verified byte-correct; version-mismatch negatives error
   cleanly; G115 unchanged; full suite green.

## Risk Assessment
### High Priority Risks
- **Speculative generality**: building a version-decode engine with only one real layout invites
  untested abstraction (bad abstraction > duplication).
  - **Mitigation**: scope to the dispatch *seam* + a single registered current strategy; the legacy
    entry decoder lands in 3.3 with v4 to exercise it. Rule-of-three respected.
- **Silent behaviour change on the hot load path**: gating the cast on `version==current` must not
  alter v2/v3 load results (both still load correctly).
  - **Mitigation**: v2 + v3 fixture byte-correct load tests as the gate; `-race`; the change is a
    refactor of an already-correct path, not new semantics.

### Medium Priority Risks
- **Write-version ownership ripples**: `SetHeaderForWritableIndex` takes `version` today; removing
  the parameter touches every writer call site.
  - **Mitigation**: compiler finds every site; keep a thin shim only if a caller legitimately needs
    a non-current version (none expected — grep first).
- **Scope ambiguity vs 3.3 (fold)**: if 3.2's residue is too thin, separate workflow overhead isn't
  justified.
  - **Mitigation**: the fold decision is surfaced for user review *before* exec (this plan's
    headline); proceed standalone only on confirmation.

## Dependencies
- 3.1 merged into the parent branch (baseline `e6c966b`); `pkg/format` is the single layout owner.
- No external/team dependencies.

## Constraints
Inherited from parent (c-design-plan.md, not restated): one place for versioned format code;
host-order zero-copy preserved for the current version; **no width/version/behaviour change in
3.2**; single version gate (no parallel second gate); British spelling.

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [ ] **Time**: <1 week (1–2 days). No.
- [ ] **People**: 1. No.
- [ ] **Complexity**: single concern (make the version-dispatch seam explicit + owned). No.
- [ ] **Risk**: contained by the v2/v3 byte-correct + negative-path gates. No.
- [ ] **Independence**: not further separable usefully.

**Outcome**: 0 decomposition signals — proceed as a single subtask. **However**, the inverse
question (should 3.2 *fold up* into 3.3?) is live: the parent design permits it, and the entry
decoder genuinely has no work until v4. Recommendation: keep 3.2 as a thin standalone subtask
(the dispatch seam + write-version ownership are real, non-speculative hardening that de-risks
3.3), **pending user confirmation at the pre-exec review**.

## Status
**Status**: Finished
**Next Action**: /cwf-requirements-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Proceeded standalone (fold-up into 3.3 rejected at the pre-exec review, as recommended). All 5
success criteria met; completed in ~1 day, within the 1–2 day estimate. The legacy entry decoder
stayed deferred to 3.3 as planned. See j-retrospective.md for full variance analysis.

## Lessons Learned
Re-measuring scope against the current (post-3.1) tree before exec prevented building a speculative
version-decode engine with no divergent layout to decode. See j-retrospective.md.
