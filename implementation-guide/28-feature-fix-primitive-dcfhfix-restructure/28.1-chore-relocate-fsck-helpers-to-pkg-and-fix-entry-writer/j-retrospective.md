# Relocate fsck helpers to pkg and fix entry writer - Retrospective
**Task**: 28.1 (chore)

## Task Reference
- **Task ID**: internal-28.1
- **Task URL**: N/A (internal task)
- **Parent Task**: 28
- **Branch**: chore/28.1-relocate-fsck-helpers-to-pkg-and-fix-entry-writer
- **Baseline Commit**: 3a64a0ed81041c12f0e2dd71f77045e842e69efe
- **Template Version**: 2.1
- **Retrospective Date**: 2026-06-13

## Executive Summary
- **Duration**: ~2 days (estimated 2–3; within estimate)
- **Scope**: Relocated the dcfhfix fsck helpers into `pkg/` and replaced the broken
  `O_APPEND` entry writer with the single `TempIndexWriter`/`EntrySerialiser` path
  (FR9). Backup stack descoped to 28.2 (user-approved); parse-helper relocation
  absorbed (coupling found at exec).
- **Outcome**: Success. FR9 fixed (variable-length paths now round-trip), single-writer
  invariant preserved, all planned tests pass, lint/vuln clean. The behaviour-preserving
  prerequisite that unblocks `Repo.Fix` (28.2) is in place.

## Variance Analysis
### Time and Effort
- **Estimated**: 2–3 days (chore flow — no requirements/design/rollout phases; parent 28
  design is the contract).
- **Actual** (commit dates): plan/impl-plan/test-plan 2026-06-12; M1 + M2 + testing-exec
  2026-06-13. ~2 calendar days.
- **Variance**: Within estimate. The Milestone-1/2 split added no measurable overhead and
  paid for itself by isolating the one behaviour change.

### Scope Changes
- **Additions**:
  - **Parse-helper relocation** (`pkg/fix_parse.go`, exported `ParseX`): the relocated
    `ApplyFieldFix`/`ParseEntryFromJSON` depended on the `main.go` value parsers. Found at
    exec, absorbed as in-scope and behaviour-preserving.
  - **TC-8 (forward-progress past a mid-stream corrupt entry)**: the exec phase deferred
    its fixture to g-testing-exec, where it was implemented (null-out path → "path empty"
    reject; intact Size → `trySkipToNextEntry` clean resync).
- **Removals**:
  - **Backup stack descoped to 28.2** (user-approved, AskUserQuestion 2026-06-13):
    `createBackup` reads `backup`+`verbose` (would force `FixEntryFlags` wide) and drags
    `getIndexType`/`copyFile` shared with cmd-resident `fixes*` handlers. Kept in
    `cmd/dcfhfix`; `FixEntryFlags` stays `{Quiet, EditInPlace, Force}`.
  - **File layout differs from a-task-plan**: planned `pkg/fix_entry.go` + `pkg/fix_backup.go`;
    actual `fix_options.go`, `fix_parse.go`, `fix_validated_entry.go`, `fix_entry_workflow.go`,
    `fix_promote.go` (no `fix_backup.go` — backup stayed in cmd). A finer, more cohesive split
    than the two-file plan; consequence of the descope.
- **Impact**: Net neutral on timeline. The descope removed the widest `FixEntryFlags`
  pressure and kept this leaf a clean prerequisite.

### Quality Metrics
- **Test Coverage**: Critical writer path (conversion, checksum-type seed+assert,
  temp→promote, abort-discard) fully exercised by TC-2…TC-7; `trySkipToNextEntry` clean-resync
  now covered by TC-8 (was untested before this phase). TC-1…TC-9 all PASS.
- **Defect Rate**: One regression found in-phase — 11 `errorlint` findings surfaced by the
  full `golangci-lint run ./...` after M1 (see Process Learnings); fixed by `%v`→`%w`. No
  escaped defects; full suite green.
- **Security**: gosec floor clean (0 issues). CWF changeset review recorded `error`
  (cap exceeded: 1804 production lines > 500, relocation-dominated) — subagent not invoked
  per the exec-phase rule; net-new M2 surface assessed manually in f-implementation-exec.md.

## What Went Well
- **Milestone-1/2 split worked as the headline risk mitigation**: landing the relocation as
  a pure refactor (existing tests as regression gate), then correcting the writer in an
  isolated commit, meant the FR9 behaviour change was reviewable in a small diff.
- **Checksum-type assertion closed the silent-corruption risk**: seeding the synthesised
  MetaStore from the subject header and hard-asserting `writer-type == subject-type` before
  write means a non-SHA-256 index can never be re-hashed under the wrong algorithm.
- **New abort-discards-temp invariant**: the single-writer path gives produced-index-is-
  valid-or-absent (never partial) — a strict improvement over the old O_APPEND writer.
- **Reused existing infrastructure** (`TempIndexWriter`, `EntrySerialiser`, `BEScanEntry`,
  `BESizeFromPathLen`) rather than reimplementing — "the best part is no part".

## What Could Be Improved
- **Per-path lint carve-outs don't travel with relocated code**: the `cmd/dcfhfix/`
  `errorlint` exclusion did not cover the code once it moved to `pkg/`, so pre-existing
  `%v`-on-error patterns became findings. The `--new` pre-commit gate missed them (changed-
  line scoping); only the full `./...` run caught it. Cost a fix cycle late in the task.
- **Security-review cap repeatedly tripped**: anchored at the task baseline, every exec
  re-counted the whole M1 relocation (1804 production lines), so the automated changeset
  review never ran. A large behaviour-preserving relocation defeats the line-count gate;
  the manual FR4 assessment had to stand in.

## Key Learnings
### Technical Insights
- The single-writer path (`EntrySerialiser.Serialise` → `WriteSerialised` → atomic rename)
  generalises cleanly to the repair tool — the repair "writer" is just a survivor-set
  serialisation, no special-casing needed. This is the reusable seam 28.2 will build on.
- `trySkipToNextEntry`'s size-based resync is robust to a corruption that nulls the path but
  leaves the `Size` field intact: validation rejects the entry ("path empty") yet forward
  progress is clean. Worth keeping in mind when designing future corruption fixtures.

### Process Learnings
- **When relocating code across the `cmd/` → `pkg/` boundary, run the full
  `golangci-lint run ./...` before declaring the relocation milestone done** — not just the
  `--new` staged gate. Lint carve-outs are path-scoped and do not follow the code.
- **A pure-relocation milestone inflates the security-review changeset.** For relocation-heavy
  tasks, expect the cap to trip and plan to record the manual FR4 assessment; consider whether
  the anchor should advance per-milestone rather than staying at the task baseline.

### Risk Mitigation Strategies
- The pre-identified "writer correction silently changes corruption-path behaviour" risk was
  neutralised exactly as planned by the M1/M2 commit split + the abort-discards-temp test.
- The "checksum-type mismatch" and "legacy version upgrade" medium risks were both closed by
  explicit tests (TC-3/TC-4 and TC-5) — the mitigations named at planning held.

## Recommendations
### Process Improvements
- Add a "run full `golangci-lint run ./...`" checkpoint to relocation/refactor milestones in
  the implementation plan, distinct from the staged pre-commit gate.
- For relocation-dominated tasks, note in the plan that the changeset security review will
  cap-out and the manual FR4 assessment is the substitute of record.

### Tool and Technique Recommendations
- The Milestone "pure relocation → isolated behaviour change" split is worth standardising for
  any task that both moves and modifies code; it keeps the behaviour diff small and reviewable.

### Future Work
- **Task 28.2** — `Repo.Fix` / FixRequest/FixResult/FixCommand, `RunFix`, D2 MetaDir
  write-confinement, CLI translation, auto-fix, **backup-stack relocation** (descoped here),
  deeper corruption fixtures.
- **Task 28.3** — multi-source recovery rebuild.

## Status
**Status**: Finished
**Next Action**: Task complete — suggest merge to parent 28 branch
**Blockers**: None identified
**Completion Date**: 2026-06-13
**Sign-off**: Matt Keenan (claude@mattkeenan.net)

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Archived Materials
- Planning: a-task-plan.md, d-implementation-plan.md, e-testing-plan.md
- Execution: f-implementation-exec.md, g-testing-exec.md
- Commits: 1c5e3688 (plan), 3d8ac2f6 (impl-plan), ee3f41a6 (test-plan),
  fba94a96 (M1 relocation), 87ed3ec6 (M2 writer correction, FR9), 3e66eb3f (testing-exec)
- Parent design contract: ../c-design-plan.md (D3, D6)
