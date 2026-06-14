# Relocate fsck helpers to pkg and fix entry writer - Plan
**Task**: 28.1 (chore)

## Task Reference
- **Task ID**: internal-28.1
- **Task URL**: N/A (internal task)
- **Parent Task**: 28
- **Branch**: chore/28.1-relocate-fsck-helpers-to-pkg-and-fix-entry-writer
- **Baseline Commit**: 3a64a0ed81041c12f0e2dd71f77045e842e69efe
- **Template Version**: 2.1

## Goal
Relocate the dcfhfix fsck helpers (validated-entry, entry-processing workflow,
backup stack) from `cmd/dcfhfix` into `pkg/`, and replace the incomplete
`O_APPEND` entry writer with the single `TempIndexWriter`/`EntrySerialiser`
path — the behaviour-preserving prerequisite that unblocks `Repo.Fix` (28.2).

## Inherited Design
This subtask implements parent Task 28 decisions **D3** (single-writer
correction, delete `appendValidatedEntryToTmpIndex`) and **D6** (two-field
MetaStore seam, checksum-type assertion, version policy). See
`../c-design-plan.md`. No separate requirements/design phase (chore flow);
the parent design is the contract.

## Success Criteria
- [ ] `ValidatedEntry`/`ApplyFieldFix`, the entry-processing workflow (`processEntriesWithWorkflow`, `processSingleEntry`, `trySkipToNextEntry`, the unfixable cap), the backup-stack helpers (`createBackup`/`listBackups`/promotion + `.pre-fix-*`), and `EntryJSON` live in `pkg/` (new `pkg/fix_entry.go`, `pkg/fix_backup.go`); `cmd/dcfhfix` imports rather than defines them; `go build ./...` clean.
- [ ] `appendValidatedEntryToTmpIndex` deleted; entry edit/append writes route through `EntrySerialiser.Serialise` → `NewTempIndexWriter` → `WriteSerialised` → temp→atomic-rename, and round-trip the full variable-length path (test over multi-byte and max-length paths).
- [ ] The synthesised MetaStore for the explicit-file path seeds its hash type from the subject header's `checksum_type`; `RunFix`/writer asserts `writer-checksum-type == subject-header-checksum-type` before writing (no silent re-hash). Version policy is an explicit, documented, tested choice (legacy fixture round-trips).
- [ ] On abort (unfixable cap tripped or resync `stop`), the temp index is discarded and the subject is untouched — no partial index (test asserts this gained invariant).
- [ ] Existing dcfhfix tests (`main_test.go`, `options_test.go`) pass; any test that encoded the pre-fix incomplete-writer output is corrected as a documented fix. `golangci-lint run ./...` clean with migrated G304/G703/G306 rationales re-anchored to the `MetaDir`/`.dcfh` path invariant in `pkg/`.

## Original Estimate
**Effort**: 2–3 days
**Complexity**: Medium
**Dependencies**: Parent 28 design (done). Reuses existing `pkg/format.SafeEntry`, `TempIndexWriter`, `EntrySerialiser`, `BEScanEntry`, `(*MetaStore).GetCurrentHashType` — all present.

## Major Milestones
1. **Pure relocation**: move types/functions/`EntryJSON` to `pkg/fix_entry.go`+`pkg/fix_backup.go`, update `cmd/dcfhfix` imports; read/validation/backup behaviour unchanged; build + existing tests green. (Separate commit — the behaviour-preserving half.)
2. **Writer correction**: delete `appendValidatedEntryToTmpIndex`; route writes through the single-writer path; synthesise the two-field MetaStore; add the checksum-type assertion and version policy. Add path round-trip + abort-discards-temp + legacy-version tests.
3. **Lint/security reconcile**: re-anchor migrated gosec rationales to `MetaDir`/`.dcfh` in `pkg/`; reconcile any dcfhfix test that encoded the old broken output; `golangci-lint` + CWF changeset review clean.

## Risk Assessment
### High Priority Risks
- **Writer correction silently changes corruption-path behaviour**: the entry workflow assumes corrupt input and must keep forward progress (cap + resync); rewiring the writer could perturb those semantics.
  - **Mitigation**: land Milestone 1 as a pure refactor (no writer change) with existing tests as the regression gate; correct the writer in a separate Milestone-2 commit so the diff isolates behaviour change; add the abort-discards-temp test.

### Medium Priority Risks
- **Checksum-type mismatch on the explicit-file path**: a synthesised MetaStore defaulting to SHA-256 would re-hash a non-SHA-256 index under the wrong algorithm — a silent corruption masquerading as repair.
  - **Mitigation**: seed hash type from the subject header; hard-assert equality before any write (Success Criterion 3).
- **Legacy version upgrade on repair**: the existing read-old/write-new convention writes current (v4); requirements say "no format change". Tension for v2/v3 inputs.
  - **Mitigation**: make the version policy an explicit documented decision with a legacy-fixture round-trip test; default to preserving `checksum_type` always and asserting the chosen version behaviour.
- **gosec rationale drift**: migrated G304/G703/G306 suppressions assumed a CLI-constrained path; `getBackupDir`'s walk-up can resolve an unexpected ancestor `.dcfh` for an out-of-repo subject.
  - **Mitigation**: re-establish the `MetaDir`/`.dcfh` path invariant in `pkg/`; create temp in `filepath.Dir(subjectAbs)` with a non-attacker-derived base name; preserve/tighten the walk-up deliberately.

## Dependencies
- Parent Task 28 design decisions D3/D6 (`../c-design-plan.md`).
- Existing infrastructure: `TempIndexWriter`, `EntrySerialiser`, `BEScanEntry`, `pkg/format.SafeEntry`, `finaliseMainIndex` atomic-rename pattern. No new third-party deps.

## Constraints
- Single-writer (`TempIndexWriter`) and main/cache-read-only / temp-pure-vectorio separation preserved.
- No on-disk format **spec** change; produced indices satisfy the existing header/checksum/layout contract.
- This subtask delivers no `Repo.Fix` surface (that is 28.2) — it only relocates + corrects, keeping dcfhfix subcommand behaviour observable-identical apart from the writer fix.
- British spelling in prose/comments.

## Decomposition Check
- [ ] **Time**: 2–3 days — under a week.
- [ ] **People**: Solo.
- [ ] **Complexity**: One cohesive concern (relocate + correct one writer); the milestones are sequential steps, not distinct concerns.
- [ ] **Risk**: The one data-touching risk (writer correction) is isolated by the Milestone-1/2 split, not by further subtasks.
- [ ] **Independence**: Milestones are ordered, not parallelisable.

**Result: 0 of 5 → no further decomposition. This is the right-sized prerequisite leaf.**

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-plan 28.1
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Delivered within the 2–3 day estimate (~2 days). Relocation (M1) + writer correction
(M2/FR9) both landed; backup stack descoped to 28.2 (user-approved); parse-helper
relocation absorbed. All success criteria met except the file layout (finer split than
the planned two files; no `fix_backup.go` — backup stayed in cmd). See j-retrospective.md.

## Lessons Learned
The 0/5 decomposition call was correct — one cohesive concern, milestones sequential not
parallel. The Milestone-1/2 split (pure relocation → isolated behaviour change) was the
load-bearing risk control and worked exactly as planned.
