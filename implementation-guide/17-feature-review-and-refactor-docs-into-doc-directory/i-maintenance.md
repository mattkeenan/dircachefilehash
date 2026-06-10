# Review and refactor docs into doc directory - Maintenance
**Task**: 17 (feature)

## Task Reference
- **Task ID**: internal-17
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/17-review-and-refactor-docs-into-doc-directory
- **Template Version**: 2.1

## Goal
Keep the relocated/rewritten docs honest as the code evolves. There is no running
system to operate — the maintenance burden of a documentation set is **drift**:
the exact failure this task was created to fix ("docs so out of sync with code we
need a full review"). So system-health/uptime/scaling/incident sections are N/A;
this plan is about preventing the docs from rotting again.

## Monitoring Requirements
### System Health
- **N/A** — no service, no uptime/latency/resource metrics. Nothing runs.

### Application Metrics
- **N/A** — no business KPIs or user-engagement metrics for static docs.

### Drift signals (the only thing worth watching)
- **Link integrity**: the TC-3 relative-link sweep (extract `](…)`, resolve
  against each file's dir, `test -e`) is the one cheap, repeatable health check.
  Re-runnable any time; a non-zero broken count is the alert.
- **Stale-symbol signal**: a CURRENT-tagged doc (`docs/ARCHITECTURE.md`,
  `docs/ARCHITECTURE-IMPROVEMENTS.md`, root `README.md`) naming a symbol/file/path
  that no longer exists in `cmd/`/`pkg/`. Detected by grep, not by a daemon.

## Maintenance Tasks
### Trigger-based (not calendar-based)
Docs don't need daily/weekly cron sweeps; they need to be touched **when the code
they describe changes**. The durable triggers:
- **A `pkg/` or `cmd/` change that removes/renames a symbol or file named in a
  CURRENT doc** → update that doc in the *same* task (the convention that would
  have prevented this task existing). The natural watch-list: `docs/ARCHITECTURE.md`
  (layer/file table, system metaphors), `docs/ARCHITECTURE-IMPROVEMENTS.md`
  (per-item file/line citations), root `README.md` (command tables, global flags).
- **A new top-level `dcfh`/`dcfhfind`/`dcfhfix` command or global flag** → add it
  to the README command/flag tables and (if architecturally significant) to
  `docs/ARCHITECTURE.md`. `remote` stays omitted from user-facing tables while
  `Hidden: true`.
- **A new doc added under `docs/`** → add one row to `docs/README.md` with a
  Current/Historical marker (the single tagged index; tags live in exactly one
  place).

### Preventive
- Re-run the TC-3 link sweep + the removed-API grep (`DirectoryCache`/`FileEntry`/
  `NewDirectoryCache`) before any future doc-touching task lands — cheap, catches
  the two highest-value regressions.
- Dead-code audit (see `.cwf/docs/dead-code-audit.md`): when it deletes a symbol,
  check the CURRENT docs for a citation in the same sweep.

## Incident Response
### Common Issues
- **Broken relative link after a move/rename**: symptom — TC-3 sweep reports
  `BROKEN: <file> -> <link>`, or a 404 on the hosting platform. Diagnosis — the
  target moved or the link wasn't reanchored to the file's new directory.
  Resolution — fix the link relative to the *editing* file's dir; re-run the
  sweep to confirm 0 broken.
- **CURRENT doc cites a removed symbol** (the original task-17 disease): symptom —
  grep finds e.g. `AppendEntryToScanIndex` / `BEIndexFileIOEntry` /
  `binary_entry_index_file.go` presented as live. Diagnosis — code deleted the
  symbol without updating the doc. Resolution — either correct the claim or, if
  it's a record of a *completed deletion*, phrase it as resolved/struck-through
  (the `ARCHITECTURE-IMPROVEMENTS.md` closed-item pattern) so it reads as history,
  not a current assertion.
- **Historical doc mistaken for current**: symptom — a reader acts on
  `design.md`/`architecture-v0.7.md`/`streaming-iterator-architecture.md` as if it
  describes shipped behaviour. Diagnosis — banner missing or index marker wrong.
  Resolution — ensure the line-3 "Historical — superseded." banner is present and
  the `docs/README.md` marker reads **Historical**.

### Escalation
- **N/A** — no on-call. A wrong/broken doc is a normal bug: fix-forward via an
  ordinary doc edit or a re-opened/follow-up task. No SLA.

## Performance Optimisation
- **N/A** — no runtime, no queries, no caches, no scaling. (NFR1 was N/A
  throughout.)

## Documentation
### Runbooks
- **Verify docs are honest** (the reusable check this task leaves behind):
  1. Link sweep — for `README.md` + `docs/*.md`, extract `](…)` relative links and
     `test -e` each against the file's directory; expect 0 broken.
  2. Removed-API grep — `grep -rnE 'DirectoryCache|FileEntry|NewDirectoryCache'
     README.md docs/` → expect none.
  3. Build/test guard — `go build ./... && go test ./...` green (proves any
     `doc.go`-style comment edits didn't break the package).
### Knowledge Base
- `docs/README.md` is the entry point (tagged index, Current/Historical).
- `docs/ARCHITECTURE.md` is the canonical current overview; the three Historical
  docs are kept for rationale only, each bannered.

## Success Criteria
- [x] "Monitoring" right-sized to drift signals (link integrity + stale-symbol
  grep); system-health metrics correctly marked N/A.
- [x] Maintenance defined as trigger-based (code-change-driven), not calendar cron.
- [x] Common doc-rot issues documented with concrete resolutions.
- [x] Reusable "verify docs are honest" runbook captured.
- [N/A] On-call/SLA/training — no operational service.

## Status
**Status**: Finished
**Next Action**: /cwf-retrospective
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Maintenance plan reframed from the service-ops template to documentation-drift
prevention: trigger-based updates tied to code changes that touch cited
symbols/commands, a re-runnable link-sweep + removed-API grep runbook, and clear
resolutions for the three doc-rot failure modes (broken link, stale symbol in a
CURRENT doc, historical doc mistaken for current). All runtime-ops sections marked
N/A with rationale.

## Lessons Learned
*To be captured during retrospective*
