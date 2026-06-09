# z key hides unchanged tree entries - Rollout
**Task**: 16 (feature)

## Task Reference
- **Task ID**: internal-16
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/16-z-key-hides-unchanged-tree-entries
- **Template Version**: 2.1

## Goal
Define the rollout for the `z` hide-unchanged toggle in the `--interactive-tree` viewer.

## Deployment Strategy
### Release Type
- **Strategy**: Ships in the next `dcfh` binary, landed via the standard CWF
  squash-to-`main` + version-tag flow. There is no server/service component —
  the change is a single new key binding inside the local TUI binary, gated
  behind the existing `!--json && IsTerminal(stdout)` guard for `--interactive-tree`.
- **Rationale**: A CLI tool has no phased/canary surface; "rollout" is the merge
  to trunk and the binary users get on their next `make build` / release pull.
  The feature is additive and OFF by default (the viewer launches with the full
  tree; `z` is opt-in per session), so it cannot change any existing default
  behaviour or any non-interactive (`--json`, piped, CI) path.
- **Rollback Plan**: Revert the single squash commit on `main` (or `git branch -f`
  back to the prior tag) and rebuild. No data migration, no persisted state, no
  config — the toggle is in-memory only, so revert is clean and total.

### Pre-Deployment Checklist
- [x] Code review completed — CWF 4-agent plan reviews (b/c/d) + security review
      (implementation-exec & testing-exec) all clean.
- [x] All tests passing — full `go test ./...` green; `-race ./...` green in the
      pre-commit gate; 10 task-16 cases pass.
- [x] Security scan completed — gosec via golangci-lint: 0 issues on the package;
      changeset security review: `no findings` at both exec phases.
- [x] Performance validated — per-toggle cost is one `rebuildRows` pass over
      already-aggregated stats; no I/O, no new traversal (NFR1).
- [x] Documentation updated — footer help line advertises `z hide` (in-app
      discoverability, FR7); CHANGELOG/BACKLOG handled at retrospective (phase j).
- [N/A] Monitoring/alerting — no runtime service to monitor (local CLI).
- [x] Rollback plan ready — single-commit revert, documented above.

## Rollout Plan
Not phased — a local CLI binary has no per-user gating. Single step: land on
`main` (squash) and tag the version (phase j). Users receive the binding on
their next build/release. No feature flag is required because the binding is
inherently opt-in (a keypress) and defaults to the existing full-tree view.

## Monitoring
### Key Metrics
- **Manual smoke**: run `dcfh status --interactive-tree` in a real terminal,
  press `z` to confirm unchanged entries hide and the footer shows the hint,
  press `z` again to restore. (Automated equivalent: the tcell SimulationScreen
  suite, already green.)
- No telemetry exists or is added — consistent with the tool's no-network design.

### Alerting
- N/A — no service. A regression would surface as a failing `tui` test in CI.

## Rollback Plan
### Triggers
- A panic or visibly wrong filtering reported against the `z` toggle.
- Any regression in the existing viewer keys (sort/reverse/expand) traced to the
  shared `rebuildRows` filter point.

### Procedure
1. **Immediate**: Reproduce via `dcfh status --interactive-tree` or the `tui` test
   suite.
2. **Rollback**: Revert the task-16 squash commit on `main`; rebuild. The change
   touches only `cmd/dcfh/internal/tui/{render.go,tui.go}` + tests, so the revert
   is isolated.
3. **Communication**: N/A (single-maintainer local tool).
4. **Analysis**: Add a failing test reproducing the defect before re-attempting.

## Success Criteria
- [x] Change is additive, OFF by default, and cannot affect `--json`/non-TTY paths.
- [x] Revert path is a clean single-commit rollback (no state/migration).
- [x] All gates green prior to merge.

## Status
**Status**: Finished
**Next Action**: /cwf-maintenance
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Lessons Learned
*To be captured during retrospective*
