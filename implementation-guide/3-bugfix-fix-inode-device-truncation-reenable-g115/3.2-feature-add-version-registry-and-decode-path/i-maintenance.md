# Add version registry and decode path - Maintenance
**Task**: 3.2 (feature)

## Task Reference
- **Task ID**: internal-3.2
- **Task URL**: N/A (internal task)
- **Parent Task**: 3
- **Branch**: feature/3.2-add-version-registry-and-decode-path
- **Template Version**: 2.1

## Goal
Define ongoing maintenance, monitoring, and support requirements for Add version registry and decode path.

## Skip Rationale
This phase is **Skipped** — not applicable to this task.

`dcfh` is a locally-run CLI binary, not a hosted service: there is no uptime SLA,
on-call rotation, live alerting, runbook, or incident-response surface to define.
The change adds an internal version-dispatch seam and a header-size bounds guard;
it introduces no new runtime component, background process, or operational metric
to monitor.

The two ongoing concerns this change does create are already tracked outside the
maintenance phase:

- **Deferred 3.3 work** (flip the `DecodeZeroCopy` legacy arm to `DecodeHeap`,
  adopt the resolver in the dcfhfix read path, route-or-delete
  `BEIndexFileIOEntry.readEntryData`, widen `Dev`/`Ino` to `uint64`, re-enable
  G115) is captured in the parent task (3) and BACKLOG, and the code carries the
  lockstep reminder in `version_dispatch.go`. It is a future development task, not
  runtime maintenance.
- **Static-analysis floor** (gosec via golangci-lint, G115 baseline 52) is enforced
  continuously by the existing `.githooks/pre-commit` gate — no per-task
  maintenance action.

Per `workflow-steps.md#status-values`, "Maintenance for a specific bugfix (this fix
doesn't need ongoing monitoring)" is a canonical Skipped case; the same applies to
this internal refactor.

## Status
**Status**: Skipped
**Next Action**: /cwf-retrospective
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
N/A — phase skipped.

## Lessons Learned
N/A — phase skipped.
