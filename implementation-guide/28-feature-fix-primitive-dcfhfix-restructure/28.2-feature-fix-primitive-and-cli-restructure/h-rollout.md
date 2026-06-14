# Fix primitive and CLI restructure - Rollout
**Task**: 28.2 (feature)

## Task Reference
- **Task ID**: internal-28.2
- **Task URL**: N/A (internal task)
- **Parent Task**: 28
- **Branch**: feature/28.2-fix-primitive-and-cli-restructure
- **Template Version**: 2.1

## Goal
Land the `Repo.Fix` primitive + dcfhfix restructure onto `main`. "Rollout" here
is a library/CLI source landing (ff-only branch merge → trunk → next tagged
build), not a service deployment — the template's phased %/monitoring/alerting
sections are mapped accordingly.

## Deployment Strategy
### Release Type
- **Strategy**: Trunk landing — ff-only merge of the task branch onto `main`,
  picked up by the next `goreleaser` build (dcfh/dcfhfind/dcfhfix). No runtime
  service, no staged user cohorts.
- **Rationale**: matches the project branch-landing model (main is the trunk,
  linear history, never `--no-ff`). The change is internal refactor + a new
  library primitive; the CLI surface (subcommands, flags, help, exit codes) is
  behaviour-preserving (TC-11), so there is no user-visible cutover to stage.
- **Rollback Plan**: `git revert` the squash commit on `main` (the change is a
  single landed unit); no data migration, no on-disk format change to undo.

### Pre-Deployment Checklist
- [x] Code review completed — CWF exec-phase changeset security review run
  (recorded `error`: over the 500-line auto-cap; focused confinement-surface
  review = **no findings**, f-implementation-exec.md). A full split/manual
  changeset pass at landing is the recommended belt-and-braces.
- [x] All tests passing — full `go test ./...` green; pre-commit `-race`
  (`-d=checkptr=0`) green on every commit.
- [x] Security scan completed — `golangci-lint run ./...` (gosec floor) 0 issues;
  gosec rationales re-anchored to the confinement/MetaDir invariant.
- [x] Performance validated — no new index passes in the translation layer; the
  primitive reuses the single-writer path (informational, NFR1).
- [x] Documentation updated — deviations recorded in f-/g-; CLI help text
  unchanged (TC-11). No user-doc change required (no surface change).
- [N/A] Monitoring/alerting configured — no runtime service.
- [x] Rollback plan ready — single-commit `git revert` (above).

## Rollout Plan
### Phase 1: Limited Release
- **Scope**: the task branch itself — exercised via the full test suite +
  end-to-end CLI smoke (init/update/remove/list/edit --dry-run/pop) in
  f-implementation-exec.md.
- **Duration**: complete (pre-landing gates).
- **Success Metrics**: suite + lint + `-race` green; smoke output bytes + backup
  metadata byte-identical to pre-28.2.

### Phase 2: Gradual Rollout
- **Scope**: land on `main`; the new `Repo.Fix` primitive becomes available to
  library consumers and the three binaries on the next build.
- **Duration**: n/a (atomic landing).
- **Success Metrics**: `cwf-manage validate` OK post-landing; `main` builds
  clean.

### Phase 3: Full Release
- **Scope**: next tagged `goreleaser` build ships the restructured dcfhfix and
  the library primitive.
- **Monitoring**: ongoing via the standing test suite + lint gate on `main`.

## Monitoring
### Key Metrics
- **Correctness**: the data-integrity gates (TC-4 dry-run writes nothing, TC-10
  no silent partial index) hold in CI — produced index is always valid-or-absent.
- **Errors**: the test suite is the regression signal; no telemetry surface.
- **Adoption**: `Repo.Fix` available alongside `Repo.Filter`; uptake tracked by
  follow-on task 28.3 (multi-source recovery rebuild) which consumes it.

### Alerting
- CI suite failure on `main` is the alert channel (no runtime alerting).

## Rollback Plan
### Triggers
- Post-landing suite/lint failure on `main`.
- A confinement escape or single-writer-invariant violation found in review.
- Any dcfhfix CLI behaviour regression (subcommand/flag/exit-code drift).

### Procedure
1. **Immediate**: identify the squash commit on `main`.
2. **Rollback**: `git revert <squash-sha>` (no format/data migration to unwind).
3. **Communication**: n/a (single-maintainer trunk).
4. **Analysis**: root-cause in a follow-up bugfix task via the CWF workflow.

## Success Criteria
- [x] Branch lands ff-only on `main`; linear history preserved.
- [x] `main` builds clean; `cwf-manage validate` OK.
- [x] No CLI surface regression (TC-11).
- [x] No rollback required.
- [ ] Full split/manual changeset security review at landing (recommended,
  over-cap belt-and-braces) — owner decision.

## Status
**Status**: Finished
**Next Action**: /cwf-maintenance 28.2
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Rollout strategy mapped to the trunk-landing model: ff-only merge → next
`goreleaser` build. All pre-landing gates green; rollback is a single-commit
`git revert`. One owner decision left open: whether to run a full split/manual
changeset security review at landing (the auto-cap was exceeded; the focused
confinement review found nothing).

## Lessons Learned
For a library/CLI the "rollout" is trunk landing + the next build; the SaaS
phased/monitoring template maps cleanly onto that once you treat the CI gates on
`main` as the monitoring surface and `git revert` as the rollback.
