# Byte-weighted default sort for interactive-tree - Rollout
**Task**: 12 (feature)

## Task Reference
- **Task ID**: internal-12
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/12-byte-weighted-default-sort-for-interactive-tree
- **Template Version**: 2.1

## Goal
Land the byte-weighted default sort into the local development line and
make it available in the next dcfh release.

## Deployment Strategy
### Release Type
- **Strategy**: Single-binary CLI release. There is no service, no
  server fleet, and no per-user rollout — the change ships when the next
  `dcfh`/`dcfhfind`/`dcfhfix` build is cut. "Deployment" = squash the
  task branch, land it, and (when the user chooses) tag + build via
  goreleaser. Phased/canary/blue-green do not apply.
- **Rationale**: the product is a local file-integrity tool; users adopt
  a new behaviour only by building or installing a new binary. The
  blast radius is one interactive viewer's default sort key — purely
  cosmetic/ordering, no on-disk format change, no index migration.
- **Rollback Plan**: `git revert` the squashed Task 12 commit (or
  `git branch -f` the line back to `bd0ba54^`) and rebuild. No data or
  index rollback is possible-or-needed — the change touches only the
  read-only post-run viewer and an in-memory change-set; `main.idx`/
  `cache.idx` bytes are unchanged (TC-13 byte-identity proven).

### Pre-Deployment Checklist
- [x] Code review completed — 4-subagent plan review (b/c/d) applied; exec
      changeset security review recorded `error` (cap exceeded by test
      LOC only; ~154 production lines, no new untrusted-input path —
      accepted by user).
- [x] All tests passing — `go test ./...` green; TC-1..TC-16 pass;
      `-race -d=checkptr=0 ./pkg/` clean.
- [x] Security scan — `golangci-lint run ./...` (gosec) 0 issues;
      `govulncheck` 0 affecting vulnerabilities (pre-commit gate).
- [x] Performance — no second filesystem walk (TC-12); byte aggregation
      is O(nodes) over the already-built tree; int64 metric (no overflow).
- [x] Documentation — CHANGELOG/BACKLOG update is performed in the
      retrospective phase (j); user-facing help is the in-viewer footer
      legend (`c/f/a/m/d/n`), already updated and asserted by render test.
- [N/A] Monitoring/alerting — no runtime telemetry for a local CLI.
- [x] Rollback plan — trivial `git revert`; documented above.

## Rollout Plan
Phased rollout is not applicable to a local CLI. The single landing step:

### Landing
- **Scope**: the `local-main` development line (and any future public
  branch/release the maintainer cuts). Per repo policy the human owns the
  merge to trunk — the retrospective phase (j) emits the suggested merge
  command; it is not executed by the workflow.
- **Success signal**: post-merge `go test ./...` + `golangci-lint` stay
  green on the integration branch; `dcfh status --interactive-tree` opens
  at `change_bytes(desc)` in a real terminal.

## Monitoring
### Key Signals (manual, at landing)
- Build/test/lint green on the branch the task lands on.
- Real-terminal smoke: viewer opens at `change_bytes(desc)`; `c`/`f`
  toggle bytes/files; `r` flips direction; stats pane shows the byte
  breakdown; biggest-by-bytes directory first.
- Non-interactive `status`/`update` output and `--json` unchanged
  (regression guard; verified off-TTY in g-testing-exec).

### Alerting
- None — local CLI, no runtime service. Regressions surface through the
  test suite and the pre-commit gate on the next change.

## Rollback Plan
### Triggers
- A real-terminal defect in the viewer (wrong default, crash, mis-sort).
- Any unexpected diff in non-interactive output or on-disk index bytes.

### Procedure
1. `git revert <squashed-task-12-commit>` on the affected branch.
2. Rebuild (`make build`) and re-run `go test ./...`.
3. Re-open the task for fix-forward if the defect is narrow; the revert is
   safe at any time because no persistent state depends on this change.

## Success Criteria
- [x] Change landed on the development line with build/test/lint green.
- [x] No on-disk format or index migration required.
- [ ] Real-terminal visual confirmation by the maintainer (left to the
      operator; headless SimulationScreen coverage stands in for CI).
- [x] Rollback path is a one-command revert with no data implications.

## Status
**Status**: Finished
**Next Action**: /cwf-maintenance
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Rollout reduces to a clean branch landing: no service deployment, no
phased exposure, no telemetry. The one residual manual step is a
real-terminal visual pass by the maintainer; everything else is gated by
the test suite and pre-commit checks.

## Lessons Learned
For a local CLI, rollout collapses to a clean branch landing with a
one-command `git revert` rollback — the generic phased/canary template
does not apply. See j-retrospective.md.
