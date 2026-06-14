# Multi-source recovery rebuild - Maintenance
**Task**: 28.3 (feature)

## Task Reference
- **Task ID**: internal-28.3
- **Task URL**: N/A (internal task)
- **Parent Task**: 28
- **Branch**: feature/28.3-multi-source-recovery-rebuild
- **Template Version**: 2.1

## Goal
Ongoing maintenance for the multi-source `recovery-rebuild` Fix op. As with
28.2, this is a CLI/library (no runtime service), so the template's
uptime/SLA/scaling sections map to the standing test/lint gates plus a small set
of concrete carry-forward invariants specific to a data-destructive rebuild.

## Monitoring Requirements
### System Health
- **No runtime service** — health == the CWF gates on `main`: `go test
  ./pkg/... ./cmd/...`, `golangci-lint run ./...` (gosec floor), and the
  pre-commit `-race` (`-d=checkptr=0`) gate.
- **Build**: the three binaries must build clean; next `goreleaser --snapshot
  --clean` is the packaging check.

### Application Metrics
- **Correctness invariants** (the real "KPIs"): the rebuilt `main.idx` is always
  valid-or-absent — empty-merge guard aborts before any rename (TC-14); dry-run
  writes nothing (TC-10); snapshot-readback gate (TC-13) and fault-injection
  atomicity (TC-16) ensure no partial index survives a mid-write fault.
- **Adoption**: `recovery-rebuild` is the first real consumer of the 28.2
  `Repo.Fix` batch path. It completes parent Task 28 — the v0.7 recovery write
  path now runs through the single-writer atomic primitive.

### Alerting Rules
- **Critical**: any suite/lint failure on `main`, a read-source confinement
  escape, a snapshot-gate bypass, or a single-writer-invariant breach found in
  review → fix-forward or revert.
- **Info**: linter modernize/deprecation nudges — fold into the next touch.

## Maintenance Tasks
### Regular Maintenance Schedule
- **Per-change**: full suite + lint + `-race` (already enforced by pre-commit).
- **Periodic**: dependency bumps via the normal CWF chore flow.
- **Periodic**: dead-code audit (`.cwf/docs/dead-code-audit.md`).

### Preventive Maintenance
- **Read-source confinement audit (load-bearing)**: every candidate source in
  `orderedSourcePaths` — named selectors **and** auto-discovered timestamped
  caches — passes through `confineWriteDest(c, metaDir)` *before* any open, and
  the merge read (`collectForEdit`/`os.ReadFile`, carrying the G304 "never a raw
  selector" rationale) is symlink-safe **only because** of that. Any **new**
  source candidate that reaches `collectForEdit` without first passing
  confinement must be audited — this is the one invariant a future change can
  silently break. `orderedSourcePaths` also `Lstat`-rejects non-regular leaves;
  keep that before the read if the candidate set grows.
- **Snapshot-readback is presence/size, not byte-integrity (documented residual,
  LD6)**: `verifyRecoverySnapshot` `Lstat`s each *contributing* source's copy
  under `recovery/`, requiring regular + size>0 and rejecting symlinks, but does
  **not** verify the copy's bytes match the source, and a TOCTOU window exists
  between snapshot, readback, and rebuild write. Safe today (runs entirely inside
  the user's own MetaDir, no privilege boundary, atomic rename bounds the blast
  radius). **Audit** if the op ever becomes reachable across a privilege boundary
  or with an attacker-writable `recovery/` — there the existence check is
  insufficient and content/atomicity verification would be required.

## Incident Response
### Common Issues
- **Recovery aborts with "no surviving entries"** (by design, not a bug): the
  empty-merge guard (TC-14) fires when every source is empty/all-deleted/
  unreadable. The original index is left intact. Resolution: supply at least one
  source with a live entry, or accept that nothing is recoverable.
- **A source is silently skipped during merge**: a source whose header checksum
  type disagrees with the first *contributing* source is skipped with its entries
  counted as discards (no abort, no re-hash) — `FixResult.EntriesDiscarded`
  reflects them. This is the concrete checksum policy (refines LD5). Resolution:
  expected; the highest-precedence source with ≥1 entry establishes the type.
- **Cosmetic stderr wart**: `PromoteRepairedIndex` prints a hardcoded "original
  is NOT preserved" warning even though the pre-recovery snapshot *did* preserve
  it (the write uses `EditInPlace: true, Force: true`). Harmless, library-only;
  a message refinement is a candidate follow-up, not a bug.

### Troubleshooting Guide
- **Symptom**: recovery rebuild produced no `main.idx`. **Diagnosis**: check for
  the empty-guard abort (zero survivors) or a snapshot-readback failure (a
  contributing source's `recovery/` copy missing/empty/symlinked). **Resolution**:
  both are intentional fail-closed aborts that leave the original untouched —
  inspect the sources and the `recovery/` snapshot, then re-run.
- **Symptom**: a destroyed (removed) `main.idx` blocks the rebuild. **Diagnosis**:
  `CreateMetaStore`/`NewMetaStore` seeds an empty `main.idx`; a caller modelling
  "destroyed" must `os.Remove` it. The recovery write uses `EditInPlace` so the
  atomic rename creates `main.idx` whether or not it pre-existed. **Resolution**:
  working as designed; no preserve step can save a file that does not exist.

### Escalation Procedures
- Single-maintainer trunk: no formal tiers. Confinement/data-integrity breaches
  are the only "critical" class → revert the squash commit, open a bugfix task.

## Performance Optimisation
### Optimisation Areas
- None outstanding. The rebuild folds each source once via `collectForEdit` then
  writes once via the single-writer path — no extra index passes (NFR1).

### Scaling Strategy
- N/A (local CLI/library; bounded by the combined source entry count).

## Documentation
### Runbooks
- Library: invoke the `recovery-rebuild` op through `Repo.Fix` (library path;
  `writeRoot != ""` is asserted — the op refuses the unconfined CLI exemption).
  Sources are merged in precedence order timestamped-caches-newest→oldest >
  cache.idx > main.idx; the pre-recovery snapshot under `.dcfh/recovery/` is the
  backup of record.

### Knowledge Base
- Deviations + rationale: f-implementation-exec.md (no-err merge + `contributing`
  return; concrete checksum policy refining LD5; `EditInPlace`/`Force` write
  rationale; readback verifies contributing-not-ordered, resolving AC1↔AC4).
- Security surface: f-/g- changeset reviews (both **no findings**) + the
  load-bearing confinement and snapshot-readback residual notes above.

## Success Criteria
- [x] Carry-forward invariants documented (read-source confinement audit;
  presence-only snapshot-readback residual).
- [x] Standing gates identified as the health signal (suite/lint/-race on main).
- [x] Common issues captured (empty-guard abort; checksum-skip; destroyed-main
  modelling; cosmetic stderr wart).
- [x] No new runtime monitoring required (CLI/library).

## Status
**Status**: Finished
**Next Action**: /cwf-retrospective 28.3
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Maintenance captured the two load-bearing carry-forwards (read-source
confinement before any open; the presence-only snapshot-readback residual with
its TOCTOU caveat per LD6) and the operational quirks from the execs (empty-guard
abort, checksum-skip discards, destroyed-main modelling, the cosmetic
"NOT preserved" stderr wart). Health == the standing CWF gates on `main`.

## Lessons Learned
For a data-destructive op the durable maintenance hazards are the two safety
boundaries: confinement-before-read and the snapshot-readback gate. Both are
fail-closed today but presence-only on the snapshot side — recording the exact
"audit if reachable across a privilege boundary" condition now is cheaper than
re-deriving the residual after a future change moves the op.
