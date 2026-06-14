# Fix primitive and CLI restructure - Maintenance
**Task**: 28.2 (feature)

## Task Reference
- **Task ID**: internal-28.2
- **Task URL**: N/A (internal task)
- **Parent Task**: 28
- **Branch**: feature/28.2-fix-primitive-and-cli-restructure
- **Template Version**: 2.1

## Goal
Ongoing maintenance for the `Repo.Fix` primitive + restructured dcfhfix. This is
a CLI/library (no runtime service), so the template's uptime/SLA/scaling sections
are mapped to the standing test/lint gates and a small set of concrete
carry-forward invariants.

## Monitoring Requirements
### System Health
- **No runtime service** — health == the CWF gates on `main`: `go test ./...`,
  `golangci-lint run ./...`, and the pre-commit `-race` (`-d=checkptr=0`) gate.
- **Build**: the three binaries (dcfh/dcfhfind/dcfhfix) must build clean; the
  next `goreleaser --snapshot --clean` is the packaging check.

### Application Metrics
- **Correctness invariants** (the real "KPIs"): produced index is always
  valid-or-absent (TC-10); dry-run never writes (TC-4); writes stay inside
  MetaDir for the library path (TC-8).
- **Adoption**: `Repo.Fix` consumed by follow-on task **28.3** (multi-source
  recovery rebuild, `mergeSourcesIntoEntries`). Watch that consumer for the
  first real library use.

### Alerting Rules
- **Critical**: any suite/lint failure on `main`, or a confinement-escape /
  single-writer-invariant breach found in review → fix-forward or revert.
- **Info**: linter modernize/deprecation nudges (e.g. `rangeint`) — fold into
  the next touch.

## Maintenance Tasks
### Regular Maintenance Schedule
- **Per-change**: full suite + lint + `-race` (already enforced by pre-commit).
- **Periodic**: dependency bumps (Go floor 1.25.0; zerocopyskiplist, vectorio,
  x/sys, tcell) via the normal CWF chore flow.
- **Periodic**: dead-code audit (`.cwf/docs/dead-code-audit.md`) — M3 deleted a
  cluster of dcfhfix machinery; confirm no stragglers re-accrete.

### Preventive Maintenance
- **Confinement-caller audit (load-bearing)**: the `os.Create` temp writers
  `writeHeaderAndEntries` (fix_header.go), `copyFile` (fix_backup.go), and
  `writeRepairedIndex` (fix_entry_workflow.go) are symlink-safe **only because**
  their destination is confined by `confineWriteDest`/`confineWriteDir` (or the
  CLI explicit-subject trust model) *before* the write. Any **new** caller that
  reaches these writers without first passing through confinement must be
  audited. This is the one invariant a future change can silently break.
- Security: gosec rationales in the fix_* files are anchored to the
  confinement/MetaDir invariant — keep them accurate if the write paths move.

## Incident Response
### Common Issues
- **Same-second edits collapse to one backup** (PRE-EXISTING, not a regression):
  backup filenames are second-granularity (`<unix>-<YYYYMMDDTHHMMSS>.idx`), so
  two edits within the same wall-clock second collide into a single stack entry.
  Surfaced by `TestRunFix_FixesDiscardAndClear` during testing-exec. Resolution:
  acceptable today; if sub-second backup stacking is ever required, widen the
  filename timestamp (a follow-up bugfix task, not in 28.2 scope).
- **`entry edit json` returns "not yet implemented"**: intentional stub
  preserved from pre-28.2 (TC-12); backup is still taken first. Not a bug.
- **Manual (interactive) fix mode unavailable**: `RunFix` returns
  `ErrManualModeUnimplemented` by design (deferred). A typed error, not a crash.

### Troubleshooting Guide
- **Symptom**: a fix write lands outside `.dcfh`. **Diagnosis**: check whether
  the call went through `Repo.Fix` (always confined to MetaDir) or the dcfhfix
  explicit-subject CLI path (`writeRoot==""`, intentionally unconfined — the user
  named the file). **Resolution**: library escapes are a confinement bug (audit
  the caller per the load-bearing note); CLI explicit-subject writes are working
  as designed.
- **Symptom**: `FixResult.EntriesDiscarded` looks low on a huge corrupt index.
  **Diagnosis**: the unfixable-entry cap is 100 (the 101st trips
  `capExceeded`); discards are still surfaced on the cap error (AC6 fix).
  **Resolution**: expected — re-run after addressing the flagged entries.

### Escalation Procedures
- Single-maintainer trunk: no formal tiers. Confinement/data-integrity breaches
  are the only "critical" class → revert the squash commit, open a bugfix task.

## Performance Optimisation
### Optimisation Areas
- None outstanding. The CLI is now a thin translator over one shared `RunFix`;
  no new index passes were introduced (NFR1). The collect/write split (LD5)
  reads once then writes once.

### Scaling Strategy
- N/A (local CLI/library; bounded by index size, unchanged by 28.2).

## Documentation
### Runbooks
- dcfhfix usage unchanged — `dcfhfix <index> <header|entry|scan> [options]`
  (CLAUDE.md). Backup recovery: `dcfhfix <index> fixes list|pop|discard|clear`.
- Library: `repo.Fix(ctx, FixRequest{...})` mirrors `repo.Filter`; writes are
  MetaDir-confined.

### Knowledge Base
- Deviations + rationale: f-implementation-exec.md (header-edit surgical writer;
  `FixCommand` simplification; `writeRoot==""` exemption model; AC6 fix).
- Security surface: f-implementation-exec.md focused review (no findings) +
  the load-bearing confinement caveat above.

## Success Criteria
- [x] Carry-forward invariants documented (confinement-caller audit; backup
  second-granularity; Manual-mode/json-edit stubs).
- [x] Standing gates identified as the health signal (suite/lint/-race on main).
- [x] Follow-up consumer noted (28.3 multi-source recovery rebuild).
- [x] No new runtime monitoring required (CLI/library).

## Status
**Status**: Finished
**Next Action**: /cwf-retrospective 28.2
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Maintenance captured the three concrete carry-forward items from the execs: (1)
the load-bearing confinement-caller audit invariant, (2) the pre-existing
second-granularity backup-naming limitation (test-surfaced, not a regression),
and (3) the intentional Manual-mode / `entry edit json` stubs. Health == the
standing CWF gates on `main`; no runtime monitoring applies.

## Lessons Learned
The one durable maintenance hazard is the load-bearing confinement invariant:
three `os.Create` writers are symlink-safe only because their destination is
confined first. Documenting it as an explicit "audit any new caller" note is
cheaper than re-deriving it after a future regression.
