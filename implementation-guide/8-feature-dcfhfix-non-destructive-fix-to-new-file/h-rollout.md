# dcfhfix non-destructive fix-to-new-file - Rollout
**Task**: 8 (feature)

## Task Reference
- **Task ID**: internal-8
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/8-dcfhfix-non-destructive-fix-to-new-file
- **Baseline Commit**: 4598a81b2f76d1838462d249952b0ef95ecf56b9
- **Template Version**: 2.1

## Goal
Land the `dcfhfix` non-destructive default and ship it in the next release of
the CLI binaries.

## Deployment Strategy
### Release Type
- **Strategy**: Single-artefact CLI release. `dcfhfix` is a local command-line
  binary distributed (with `dcfh`/`dcfhfind`) via `goreleaser` (deb/rpm/tar.gz);
  there is no server, no fleet, and no per-user traffic split. The standard
  blue-green/canary/business-metric machinery in the template does not apply and
  has been removed in favour of what is real for this repo.
- **Rationale**: A behaviour change in a self-contained binary rolls out exactly
  when a user installs the new build. The only meaningful "rollout" control is
  release versioning plus the stacked-branch landing model, and the only
  meaningful safety control is the in-binary opt-out flag.
- **Rollback Plan**: Revert the feature branch's squash commit (or pin the prior
  release tag) — see Rollback Plan below. No data migration, so rollback is a
  pure binary swap; any `.pre-fix-*` siblings already written remain valid and
  harmless.

### Behaviour-Change Note (the actual rollout risk)
This task changes a **default**: `dcfhfix` previously edited the index in place
and now preserves the original at a `<index>.pre-fix-<UTC>` sibling before the
atomic rename. Anyone scripting `dcfhfix` and not expecting a sibling file could
be surprised.
- **Compatibility**: The repaired index still lands at the original path (atomic
  rename is unchanged), so any workflow that reads the fixed index keeps working;
  the only difference is an *extra* preserved file alongside it.
- **Opt-out**: `--edit-in-place` (force-gated via `--force`) restores the exact
  legacy in-place behaviour for callers who need it.
- **Discoverability**: Default path prints where the original was preserved;
  in-place path prints a prominent destructive-action warning. Help text and
  `cmd/dcfhfix/DESIGN.md` document both.

### Pre-Deployment Checklist
- [x] Code review completed (f-phase `cwf-security-reviewer-changeset`: no findings)
- [x] All tests passing — `go test ./...` green; full unit + per-write-path
      integration matrix in g-testing-exec.md
- [x] Security scan completed with no critical issues — `golangci-lint run ./...`
      → 0 issues (gosec gate, rationale-bearing `//nolint` on the O_EXCL open)
- [x] Race gate green under repo config (`-d=checkptr=0` `go test -race -short`)
- [x] Documentation updated — `dcfhfix` help text + `cmd/dcfhfix/DESIGN.md`
      (non-destructive default, `--edit-in-place`, stale refs removed)
- [x] No monitoring/alerting applicable (local CLI)
- [x] Rollback is a binary revert — trivially available, see below

## Rollout Plan
Single phase — there are no user cohorts to stage across for a local binary.

### Phase 1: Land + Release
- **Scope**: Merge the task branch per the stacked-branch landing model
  ([[project_branch_landing_model]]: task branches stack; do NOT ff-merge
  top-level tasks to `main`), then cut the next `goreleaser` build so the new
  default ships in the released binaries.
- **Verification**: Installed `dcfhfix` on a corrupt fixture index — confirm a
  `.pre-fix-<UTC>` sibling appears by default and `--edit-in-place --force`
  reproduces legacy in-place behaviour.
- **Success Metrics**: Build succeeds; smoke check above passes; no regression in
  the existing `dcfhfix` test suite.

## Monitoring
Not applicable in the service sense. For a local CLI the equivalent signals are:
- **Self-check**: the post-release smoke check above.
- **User-visible feedback**: the printed preservation notice / destructive
  warning are the in-band "telemetry" — they tell the operator exactly what the
  command did to their index.

## Rollback Plan
### Triggers
- The non-destructive default proves disruptive to an established `dcfhfix`
  scripting workflow that cannot adopt `--edit-in-place`.
- A defect is found in `preserveOriginal` (e.g. the sibling write or the
  preserve-before-rename ordering) that risks the original index.

### Procedure
1. **Immediate**: Stop cutting further releases from the affected build.
2. **Rollback**: Revert the task's squash commit on the landing branch (or
   re-pin the prior release tag) and rebuild. Because preservation is additive
   and the repaired index still lands at the original path, no on-disk state
   needs unwinding — already-written `.pre-fix-*` siblings stay valid.
3. **Communication**: N/A (internal/local tool); note the revert in BACKLOG.md
   if the feature is deferred rather than abandoned.
4. **Analysis**: Capture the failing case as a regression test before re-landing.

## Success Criteria
- [x] Deployment strategy defined and matched to a local-CLI reality
- [x] Pre-deployment checklist completed (review/tests/security/docs all green)
- [x] Rollout plan specified (land + release, single phase)
- [x] Rollback plan documented (binary revert; additive on-disk state)
- [ ] New default shipped in a release build (executed at release time)

## Status
**Status**: Finished
**Next Action**: /cwf-maintenance
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
- Rollout doc retargeted from the generic service template to the actual
  artefact: a local CLI binary shipped via `goreleaser`, landed under the
  stacked-branch model.
- All pre-deployment gates were already green from f/g phases; the only
  outstanding item is cutting the release build itself, which happens at
  release time (not within this task's branch work).

## Lessons Learned
- For a self-contained CLI, "rollout" collapses to release versioning + an
  in-binary opt-out; the real rollout risk is the **default behaviour change**,
  not deployment mechanics. Documenting the opt-out and the additive,
  non-destructive on-disk contract is what makes rollback a trivial binary swap.
