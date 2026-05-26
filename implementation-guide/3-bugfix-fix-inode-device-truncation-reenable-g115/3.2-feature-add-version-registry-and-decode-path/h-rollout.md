# Add version registry and decode path - Rollout
**Task**: 3.2 (feature)

## Task Reference
- **Task ID**: internal-3.2
- **Task URL**: N/A (internal task)
- **Parent Task**: 3
- **Branch**: feature/3.2-add-version-registry-and-decode-path
- **Template Version**: 2.1

## Goal
Define deployment strategy and rollout plan for Add version registry and decode path.

## Skip Rationale
This phase is **Skipped** — not applicable to this task.

`dcfh` is a single-binary CLI tool/library with no external service, server fleet,
or user-segmented deployment surface. There is no blue-green/canary/phased rollout
to design, no live monitoring or alerting to configure, and no per-user rollback.
The change ships through the project's normal release path (merge → tag →
`goreleaser` binary build, see CLAUDE.md), identical to every other code change.

The change is also **on-disk-format-neutral**: it adds an internal
read-version-dispatch + write-version-selection seam (`StrategyForVersion`,
version-less `SetHeaderForWritableIndex`) and a header-size bounds guard, with no
change to the index format, entry width, or version number. Existing v2/v3 indices
read and write byte-identically (verified by TC-5/TC-6), so there is no migration
or compatibility step to roll out, and "rollback" is simply reverting the merge.

Per `workflow-steps.md#status-values`, "Rollout for internal tools (this tool has
no external deployment)" is a canonical Skipped case.

## Status
**Status**: Skipped
**Next Action**: /cwf-maintenance
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
N/A — phase skipped.

## Lessons Learned
N/A — phase skipped.
