# interactive-tree status colour coding - Rollout
**Task**: 15 (feature)

## Task Reference
- **Task ID**: internal-15
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/15-interactive-tree-status-colour-coding
- **Template Version**: 2.1

## Goal
Define deployment strategy and rollout plan for interactive-tree status colour coding.

## Deployment Strategy
### Release Type
- **Strategy**: Branch landing → trunk, then bundled into the next tagged
  release. No phased/canary rollout — this is a single-binary local CLI with no
  server fleet or live user cohort to ramp.
- **Mechanism**:
  1. Fast-forward land `feature/15-interactive-tree-status-colour-coding` onto
     `local-main` (linear history, ff-only — never `--no-ff`; see the branch
     landing model). The task's checkpoint commits are the audit trail.
  2. The change reaches users when the maintainer next cuts a public release:
     CWF semver tag `v0.13.15` (major.minor human-set, patch = task 15) on the
     publish branch, then `goreleaser release` produces the deb/rpm/tar.gz
     artefacts.
- **Rationale**: The change is a TUI presentation-only change to the
  `--interactive-tree` post-run viewer — no on-disk index format change, no
  scan/status/update computation change, no config/flag surface change. It is
  inert unless a user opts into `--interactive-tree` on a TTY. Risk is confined
  to how changed rows look on screen.

### Pre-Deployment Checklist
- [x] Code review completed (CWF plan review across b/c/d; security-review
      changeset agent = no findings for both exec phases)
- [x] All tests passing (unit table + simulation render tests; full
      `go test ./cmd/... ./pkg/...` + pre-commit `-race` green)
- [x] Security scan completed with no critical issues (gosec via golangci-lint:
      0 issues; CWF changeset review: no findings)
- [x] Performance validated — per-row pure computation over already-aggregated
      stats; no new tree pass (NFR1). No measurable change.
- [x] Documentation updated — the viewer is self-documenting (on-screen legend
      added this task, FR9); no external user-doc/runbook owns the colour map.
- [N/A] Monitoring and alerting — local CLI, no telemetry surface.
- [x] Rollback plan ready (single-commit revert; see below)

## Rollout Plan
Single-step. No cohorted ramp applies to a local binary.
- **Land**: ff-merge the task branch onto `local-main`.
- **Release**: included in the next `v0.13.x` tag + goreleaser build at the
  maintainer's discretion. No separate gating for this feature.
- **Verification after release**: run `dcfh status --interactive-tree` against a
  repo with added/modified/deleted files and confirm glyph/colour/bold render
  and the legend appears in the stats pane.

## Monitoring
Not applicable — local CLI tool, no server, no telemetry, no live metrics or
alerting surface. Post-release signal is direct user observation / issue
reports.

## Rollback Plan
### Triggers
- Visual regression making status harder to read than the prior viewer.
- Any panic in the viewer on a real tree (none expected; covered by tests).

### Procedure
1. `git revert` the landed change (or drop it from `local-main` before the next
   tag). The change is two functions + one var + a legend block in a single
   file — clean, isolated revert with no data migration.
2. No data cleanup needed: nothing on disk changed, so reverting the binary
   fully restores prior behaviour.

## Success Criteria
- [x] Change lands on trunk with linear history (ff-only)
- [x] No test or lint regression
- [ ] Renders correctly in a live `--interactive-tree` session (maintainer
      spot-check at/after release)
- [x] Cleanly revertible (single-file, no on-disk format change)

## Status
**Status**: Finished
**Next Action**: /cwf-maintenance
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Pre-deployment checklist satisfied (review, tests, security, performance, docs;
monitoring N/A). Lands ff-only on `local-main`; ships in the next `v0.13.x`
goreleaser tag. No phased ramp — single local binary, TUI-only, single-file
revertible.

## Lessons Learned
The generic SaaS rollout template (canary/SLA/alerting) doesn't fit a local CLI;
rewriting it to the branch-landing + goreleaser model kept the doc honest. The
only real "rollout" content is revertibility and the maintainer eyeball check.
