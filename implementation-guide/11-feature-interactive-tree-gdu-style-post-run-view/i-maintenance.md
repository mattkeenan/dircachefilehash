# interactive-tree gdu-style post-run view - Maintenance
**Task**: 11 (feature)

## Task Reference
- **Task ID**: internal-11
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/11-{task-description}
- **Template Version**: 2.1

## Goal
Define ongoing maintenance, monitoring, and support requirements for interactive-tree gdu-style post-run view.

> Local CLI/library — no hosted runtime, telemetry, SLA, or on-call. The
> web-style monitoring/scaling template is replaced below by the real
> maintenance surface: regression sentinels, dependency hygiene, and a
> troubleshooting runbook for the viewer.

## Monitoring / Regression Sentinels
There is no runtime telemetry. The "monitors" are CI gates that must stay green:
- **Non-interactive invariant** — `TestApply_CollectChangesByteIdentical` (pkg) guards that the collector never perturbs the on-disk index. If this fails, the change-set enrichment has leaked into serialisation/rename. **Highest-value sentinel.**
- **Escape-safety** — `TestSanitiseLabel_RejectByDefault` (pkg) guards the allowlist. A failure means a control/escape byte could reach the terminal.
- **Race** — the enrichment + viewer tests under `-race -gcflags=all=-d=checkptr=0` guard the lock-free collector's single-writer/read-after-join invariant.
- **Render logic** — the `tui` `SimulationScreen` suite guards width-gating, navigation, live re-sort, and teardown without a TTY.

## Maintenance Tasks
- **Dependency hygiene**: `github.com/gdamore/tcell/v2` and `golang.org/x/term` are the new deps. Track tcell releases (`go get -u github.com/gdamore/tcell/v2`); re-run the `tui` suite + a real-TTY smoke after any bump. tcell is mature/low-churn.
- **Go toolchain floor**: this feature pins Go ≥ 1.25 (tcell). If the project later needs to drop below 1.25, the viewer (and tcell) must go with it — see the rollout rollback path.
- **Periodic**: dead-code audit (see `.cwf/docs/dead-code-audit.md`) — the data layer is small and fully reachable; the `tui` package is only reached behind the CLI guard.

## Incident Response / Troubleshooting Runbook
- **Viewer doesn't open** — *Expected* when stdout is piped/redirected, `--json` is set, or `$TERM` is unusable. Diagnosis: it is gated by `interactiveTreeWanted` (`flag ∧ !json ∧ IsTerminal(stdout)`). Not a bug; document for users.
- **"interactive tree unavailable: …" on stderr** — `tcell.NewScreen()`/`Init()` failed (e.g. unknown/empty `$TERM`). Non-fatal by design (FR9): the `status`/`update` run already completed and printed its summary; exit code reflects that work. Resolution: set a valid `$TERM`.
- **Terminal left altered after quit** — should not occur: teardown is `sync.Once(screen.Fini)` on every exit path (quit/Ctrl-C/panic/init-fail). If it ever does, `reset` / `stty sane` restores the shell; capture `$TERM`, terminal emulator, and repro, then re-open via `/cwf-maintenance`.
- **Misaligned columns with CJK / wide glyphs** — *Known limitation*: `drawText` counts one column per rune. Cosmetic only (no safety impact — the sanitiser still runs). Tracked as backlog item **"Wide-rune (CJK) column width in interactive-tree viewer"** (Low).
- **"Before" byte sizes look approximate for modified/deleted files** — *By design* (KD2/KD3): per-file sizes come from the post-run merged index; old/deleted sizes are not retained. **Counts are exact**; bytes for changed files are current-state. Byte-weighted sort is tracked as backlog item **"Byte-weighted sort option…"** (Low).

## Known Limitations & Follow-ups (backlog)
- `Wide-rune (CJK) column width in interactive-tree viewer` (bugfix, Low) — added to BACKLOG, identified-in task 11.
- `Byte-weighted sort option for interactive-tree viewer` (feature, Low) — added to BACKLOG, identified-in task 11.
- TC-16 (a forced `screen.Init()` failure on an *attached* TTY) was not reproduced headlessly; the non-fatal handling is verified by code path + empty-tree test. Low risk; revisit if an init-failure incident is reported.

## Success Criteria
- [x] Regression sentinels identified (byte-identity, escape-safety, race, render).
- [x] Dependency-hygiene + Go-floor maintenance documented.
- [x] Troubleshooting runbook covers the expected-skip, init-failure, teardown, CJK, and byte-approximation cases.
- [x] Deferred improvements captured in BACKLOG.

## Status
**Status**: Finished
**Next Action**: /cwf-retrospective 11
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
*To be filled upon completion*

## Lessons Learned
*To be captured during implementation*
