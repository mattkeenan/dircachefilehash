# Fix primitive + dcfhfix restructure - Rollout
**Task**: 28 (feature)

## Task Reference
- **Task ID**: internal-28
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/28-fix-primitive-dcfhfix-restructure
- **Template Version**: 2.1

## Goal
Land the assembled three-subtask feature onto the `main` trunk and cut it into a
release. This is a single-binary CLI tool + Go library distributed via git trunk
and goreleaser artefacts — there is no fleet, no per-user ramp, no live service.
"Rollout" here is: ff-merge to trunk → version tag → goreleaser build, with the
working tree as the only blast radius and `git revert`/re-tag as rollback.

## Deployment Strategy
### Release Type
- **Strategy**: Linear trunk integration (ff-only merge), then a tagged
  goreleaser release. Not blue-green/canary/rolling — those model a running
  service with traffic to shift; this ships an executable + library on a linear
  history (memory: main is the trunk, ff-only, never `--no-ff`).
- **Rationale**: The whole feature already landed behind the three subtask
  squashes on `feature/28-…`; promoting it is one fast-forward, preserving the
  audit trail without a merge commit. A CLI/library has no gradual-exposure knob
  — it is present in a build or it is not.
- **Rollback Plan**: Before any tag/release — `feature/28` not yet merged, so
  rollback is "do not ff-merge" (zero trunk impact). After merge — `git revert`
  the range (history is linear and the feature adds no migrations/state, so a
  revert is clean) or simply do not advance the release tag. No data migration,
  no on-disk format change (the index format is unchanged by this task), so there
  is no backward-compat or downgrade hazard.

### Pre-Deployment Checklist
- [x] Code review completed — per-subtask `cwf-security-reviewer-changeset`
  verdicts `no findings` (28.1/28.2/28.3); parent union exceeds the 500-line cap
  by construction (recorded in f/g), adds zero production lines.
- [x] All tests passing — full union suite `go test ./pkg/... ./cmd/...` `ok`;
  `-race -gcflags=all=-d=checkptr=0` exit 0 (g IT-1…IT-5 PASS).
- [x] Security scan clean — `golangci-lint run ./...` (gosec floor) 0 issues;
  `govulncheck ./...` 0 applicable.
- [x] Performance validated — NFR1 (no new index passes) holds by construction;
  parent adds no production code.
- [x] Documentation — CHANGELOG/BACKLOG updated at retrospective (j); one known
  doc nit: CLAUDE.md's stale `dcfhfix … scan/header` example syntax predates the
  28.2 translator (tracked as a doc-refresh, not a release blocker).
- [x] Monitoring/alerting — N/A for a CLI/library (no runtime telemetry); the
  "monitor" is the standing CI gate + `dcfhfix`/recovery self-validation.
- [x] Rollback plan ready — `git revert` / hold-the-tag (above).

## Rollout Plan
A CLI/library has no phased user ramp; the equivalent gates are build → merge →
tag → publish. Recorded as the actual stages, not fictional %-of-users phases.

### Phase 1: Trunk integration
- **Scope**: ff-merge `feature/28-…` → `main` (the three subtask squashes land
  linearly). **Owner: human** — the merge is a human decision, suggested at the
  end of the retrospective (j), not executed by the workflow.
- **Success metric**: `main` builds clean; full suite + `-race` green on trunk
  post-merge; `git log --merges` on the landed range stays empty (linear).

### Phase 2: Release cut
- **Scope**: version bump (j-phase `cwf-version-bump`, top-level task → real
  bump) + annotated tag (human-applied per project policy; CwF never tags from
  script), then `goreleaser release --snapshot --clean` for deb/rpm/tar.gz
  (memory: goreleaser is the artefact path).
- **Success metric**: tag points at the post-squash commit; goreleaser produces
  all three artefacts; the built `dcfh`/`dcfhfind`/`dcfhfix` run `--version`.

### Phase 3: Availability
- **Scope**: artefacts published; users pick them up on next install/upgrade. No
  forced rollout — adoption is pull-based at the user's pace.
- **Monitoring**: none runtime; regressions would surface via the standing CI
  gate on subsequent work.

## Monitoring
### Key Metrics
- **Correctness**: the CI gate (`go test`, `-race`, `golangci-lint`,
  `govulncheck`) on every subsequent commit is the continuous signal.
- **Integrity**: `dcfhfix`/recovery operate inside the user's own `MetaDir`;
  the single-writer + atomic-rename + snapshot-readback invariants are the
  in-product safety net (a failed fix leaves a valid-or-absent index, never a
  partial one).
- **Errors/Business/Performance telemetry**: N/A — offline CLI, no service.

### Alerting
- N/A (no running service). The equivalent is a red CI run on future commits.

## Rollback Plan
### Triggers
- Trunk build or suite breaks post-merge.
- A user-reported recovery/fix data-safety defect (the high-severity class).
- A vulnerability surfaced by `govulncheck` on a dependency post-release.

### Procedure
1. **Pre-tag**: do not advance the release tag; fix forward on a new subtask
   (the decomposition rule — gaps become subtasks, never inline parent patches).
2. **Post-tag**: `git revert` the offending range (linear history → clean
   revert; no state/migration to unwind) and cut a patch release.
3. **Communication**: note in CHANGELOG; no user broadcast channel for a CLI.
4. **Analysis**: root-cause in a follow-up bugfix task via the CWF workflow.

## Success Criteria
- [ ] ff-merge to `main` leaves history linear (no merge commit) and trunk green.
- [ ] Version bumped + tag applied at the post-squash commit (j-phase).
- [ ] goreleaser produces deb/rpm/tar.gz; all three binaries report `--version`.
- [ ] No rollback required.

(Boxes left unchecked: these execute at/after the j-phase squash + the
human-owned merge, which are deliberately outside this document's authority.)

## Status
**Status**: Finished
**Next Action**: /cwf-maintenance
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Rollout reframed from the generic SaaS phased-ramp template to the project's real
distribution model (linear trunk + goreleaser artefacts). Pre-deployment gate is
fully green (build, union suite, `-race`, lint 0, govulncheck 0). The merge and
release-cut stages are deliberately human/j-phase-owned and recorded as such; no
runtime monitoring applies to an offline CLI/library.

## Lessons Learned
The phased %-of-users rollout template is a poor fit for a CLI tool/library —
the honest mapping is build → ff-merge → tag → publish, with `git revert`/hold-
the-tag as rollback and the standing CI gate as the only "monitoring". A
coordinating parent's rollout adds no new deployment surface beyond promoting the
already-tested assembly.
