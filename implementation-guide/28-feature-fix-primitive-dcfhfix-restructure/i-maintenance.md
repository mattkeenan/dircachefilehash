# Fix primitive + dcfhfix restructure - Maintenance
**Task**: 28 (feature)

## Task Reference
- **Task ID**: internal-28
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/28-fix-primitive-dcfhfix-restructure
- **Template Version**: 2.1

## Goal
Define ongoing maintenance for the assembled feature: the `Repo.Fix` primitive,
the dcfhfix thin translator, and the multi-source `recovery-rebuild` op. This is
an offline CLI/library — no uptime/SLA/on-call/scaling applies; maintenance is
the standing CI gate plus the few code-health follow-ups this integration
surfaced.

## Monitoring Requirements
### System Health
- **N/A (no running service)**. There is no uptime, latency SLA, or resource
  budget to alarm on — `dcfh`/`dcfhfix` run to completion and exit. The
  continuous health signal is the **CI gate** (`go test ./pkg/... ./cmd/...`,
  `-race -gcflags=all=-d=checkptr=0`, `golangci-lint`, `govulncheck`) on every
  subsequent commit.

### Application Metrics
- **Correctness signal**: a red CI run on future work is the regression alarm.
- **Data-safety signal**: the in-product invariants are the real "monitor" — a
  failed fix/recovery leaves a **valid-or-absent** index (single-writer + atomic
  rename + snapshot-readback), never a partial one. A user-reported partial or
  corrupt `main.idx` after a fix is the one high-severity class to watch for.

### Alerting Rules
- **Critical**: any report of recovery/fix producing a corrupt or partial index,
  or a confinement escape (a fix touching a path outside `MetaDir`). → open a
  bugfix task immediately.
- **Warning**: `govulncheck` flags a *called* vuln in a dependency the fix path
  reaches. → dependency-bump task.
- **Info**: the known doc nit (below); periodic dead-code sweep.

## Maintenance Tasks
### Regular Maintenance Schedule
- **Per-commit (automated)**: the pre-commit gate above — no human cadence.
- **Periodic**: `govulncheck ./...` for dependency CVEs; dead-code audit
  (`.cwf/docs/dead-code-audit.md`) over the new `fix_*`/`fix_recovery_*` surface
  to catch any helper left unreferenced after the 28.1 relocation.
- **No DB/log/backup tasks** — no database, no server logs, no live backups; the
  only "backups" are the user-local `.pre-fix` sibling stack + pre-recovery
  snapshots under `recovery/`, which the user manages via `dcfhfix fixes`.

### Preventive Maintenance
- Dependency audit (`go list -m -u all` / `govulncheck`).
- Dead-code audit of the fix/recovery surface (documented methodology).
- Watch the `PromoteRepairedIndex` cosmetic wart (28.3 LD): it prints a
  hardcoded "original is NOT preserved" stderr line even when the pre-recovery
  snapshot *did* preserve it — a message refinement, not a correctness issue.

## Incident Response
### Common Issues
- **Partial/corrupt index after a fix**: should be impossible (temp+atomic
  rename). Symptom: `dcfh status` fails to load `main.idx` post-fix. Resolution:
  the prior state is recoverable from the `.pre-fix` stack (`dcfhfix fixes pop`)
  or the `recovery/` snapshot; file a bugfix task with the repro index.
- **`recovery-rebuild` rejects a source**: by design — sources are
  `confineWriteDest`-checked and `Lstat`-rejected if symlink/non-regular.
  Resolution: confirm the source path is a regular file inside `MetaDir`; this
  is a guard firing correctly, not a defect.
- **`dcfhfix` example from CLAUDE.md fails** (`scan`/`header …` legacy syntax):
  known doc drift — the shipped surface is `header|entry|fixes` families.
  Resolution: use `dcfhfix --help`; the doc-refresh is tracked below.

### Troubleshooting Guide
- **Symptom**: a fix/recovery op errors before writing. **Diagnosis**: read the
  typed error — `ErrManualModeUnimplemented` (manual mode deferred, no write),
  empty-merge guard, confinement reject, or `writeRoot == ""` (recovery is
  library-only, CLI-unreachable by design). **Resolution**: each is a guard, not
  a crash; no index was mutated (fail-closed, verified in g IT-4).

### Escalation Procedures
- Single-maintainer project — "escalation" is: triage → CWF bugfix task →
  fix-forward (never an inline parent patch; gaps become subtasks).

## Performance Optimisation
### Optimisation Areas
- None outstanding. The parent adds no production code and no new index passes
  (NFR1, by construction); the fix/recovery paths are collect-once/write-once.

### Scaling Strategy
- N/A — no service to scale. Index size scales with the user's tree; the
  existing mmap-read / vectorio-write design is unchanged by this task.

## Documentation
### Runbooks
- `dcfhfix --help` is the authoritative CLI surface (`header`/`entry`/`fixes`).
- Recovery-rebuild is a **library** entry point (confinement-guarded off the
  CLI); its runbook is the `TestRecovery_*` fixtures (destroyed-main +
  intact-cache).

### Knowledge Base
- Design decision records live in the subtask `c-design-plan.md` files
  (28.1/28.2/28.3) and the parent f/g exec evidence.

## Follow-ups Identified
1. **Doc-refresh (low)**: update CLAUDE.md's stale `dcfhfix … scan/header`
   example syntax to the shipped `header|entry|fixes` surface. → BACKLOG at
   retrospective.
2. **Cosmetic (very low)**: refine the `PromoteRepairedIndex` "NOT preserved"
   stderr message for the snapshot-backed recovery path.
Neither blocks release; both are recorded for the retrospective's BACKLOG step.

## Success Criteria
- [x] Monitoring reframed to the CI gate + in-product data-safety invariants
  (no fictional uptime/SLA for an offline CLI).
- [x] Maintenance schedule = automated per-commit gate + periodic dep/dead-code
  audits.
- [x] Common issues + troubleshooting documented (all current "errors" are
  correct guards firing).
- [x] Follow-ups captured for the retrospective BACKLOG step.

## Status
**Status**: Finished
**Next Action**: /cwf-retrospective
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Maintenance reframed from the SaaS-ops template to the project's reality: an
offline CLI/library whose "monitoring" is the standing CI gate and whose
data-safety net is the single-writer/atomic-rename/snapshot-readback invariants.
Two non-blocking follow-ups surfaced (doc-refresh of stale CLAUDE.md dcfhfix
examples; a cosmetic recovery stderr message) and are routed to the BACKLOG.

## Lessons Learned
For an offline tool, the maintenance phase's value is enumerating the real
operational signals (CI gate, in-product invariants, dependency audits) and the
genuine follow-ups — not populating an uptime/on-call/scaling template that has
no referent. The integration surfaced no maintenance defects, only two cosmetic/
doc follow-ups, consistent with a zero-production-line coordinating parent.
