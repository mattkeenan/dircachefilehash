# Byte-weighted default sort for interactive-tree - Maintenance
**Task**: 12 (feature)

## Task Reference
- **Task ID**: internal-12
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/12-byte-weighted-default-sort-for-interactive-tree
- **Template Version**: 2.1

## Goal
Capture the ongoing-maintenance surface for the byte-weighted sort: what
can drift, how to spot it, and how to fix it. This is a local CLI feature
— there is no service uptime, telemetry, or on-call. "Monitoring" reduces
to the test suite and the pre-commit gate; the entries below are scoped to
that reality rather than the generic SLA template.

## Monitoring Requirements
### System Health
- N/A for runtime — no daemon, no server. The only health signal is
  `go test ./...` + `golangci-lint run ./...` staying green on whatever
  branch the feature lives on.

### Application Metrics
- No telemetry is collected (privacy-preserving local tool). Feature
  adoption / correctness is observed manually in a real terminal.

### Alerting Rules
- None automated. Regressions surface as test failures on the next change
  touching `pkg/treeview.go` or `cmd/dcfh/internal/tui/`.

## Maintenance Tasks
### What can drift (feature-specific)
- **Stats field invariant**: `Files`/`Bytes` must keep excluding deleted
  entries while `AddedBytes/ModifiedBytes/DeletedBytes` track per-category
  bytes. A future change to `leafStats`/`aggregate` could silently break
  this — `TestBuildTree_ByteAggregation` guards it.
- **Dual-source deleted bytes**: the status path depends on the cache
  refresh keeping deletion tombstones with their `FileSize`
  (`pipeline_status.go` `scanWriteDelta`); the update path depends on the
  comparison goroutine capturing the left size. If either pipeline is
  reworked, `TestPostRunTree_CrossPathByteIdentity` (status + update
  subtests) is the canary.
- **Byte-identity**: any change near the collector / serialiser must keep
  `TestApply_CollectChangesByteIdentical` green — the collector must never
  touch on-disk index bytes.
- **int64 width**: keep the metric and byte fields `int64` end-to-end; a
  narrowing to `int` reintroduces overflow on large trees and would also
  trip a new gosec G115 suppression (avoid).

### Regular Maintenance
- Rides the repo's existing cadence (pre-commit gate, periodic dependency
  bumps, dead-code audit per `.cwf/docs/dead-code-audit.md`). No
  feature-specific schedule is warranted.

## Incident Response
### Common Issues
- **Viewer opens at the wrong default**: header not showing
  `change_bytes(desc)` → check `newModel` (`render.go`) initial `sortKey`
  and `keyForRune` default. Covered by `TestDefaultSortAndKeyToggles`.
- **A large deletion ranks as zero change**: `DeletedBytes` not reaching
  the node. For `update`, confirm `UpdateResult.DeletedSizes` is populated
  (`CollectChanges` on) and threaded into `ChangeSet.DeletedSizes`; for
  `status`, confirm the merged index still carries the deletion tombstone
  with its size.
- **Pane shows `0 B` for a changed category**: the relevant `*Bytes`
  field isn't aggregating — check `aggregate` sums all three new fields.

### Troubleshooting Guide
- **Symptom**: sort order looks wrong in a real terminal.
  - **Diagnosis**: press `c`/`f` to compare byte vs file ordering; the
    header names the active metric and direction. Use the stats pane byte
    breakdown to see the bytes behind the order (the KD6 affordance added
    precisely to answer "why did this sort here?").
  - **Resolution**: if bytes are present but order is wrong, inspect
    `metric()`/`nodeLess` in `sort.go`; if bytes are missing, trace the
    dual-source path above.

### Escalation
- N/A — single-maintainer local tool. Fix-forward or `git revert` the
  squashed Task 12 commit (no persistent state depends on it).

## Performance
- Byte aggregation is O(nodes) over the already-built tree; the runtime
  re-sort is a pure render-layer copy-and-sort with no filesystem access
  (no second walk — `TestLiveResortPreservesSelectionNoReRead`,
  `PostRunTree` fs-free). No scaling concern for the viewer.

## Documentation
- In-app: the viewer footer legend (`c/f/a/m/d/n sort  r reverse  q quit`)
  is the user-facing reference; the stats pane self-documents the byte
  breakdown.
- Repo: CHANGELOG entry + BACKLOG retirement of the byte-weighted-sort
  item are handled in the retrospective phase (j).

## Outstanding follow-ups (for retrospective / backlog)
- **Security-review cap vs test LOC**: both exec-phase changeset reviews
  recorded `error: cap exceeded` because this repo does not list
  `**/*_test.go` in `security.review.max-lines-exclude-paths`, so test
  lines count as production. Consider a follow-up chore to add the test
  glob to the exclude list so the semantic reviewer actually runs on the
  production diff. (Config change only; out of scope for Task 12.)
- **Incidental `go fix` modernisation** (resolved, not deferred): the
  pre-commit hook ran `go fix ./...` during exec and rewrote
  `pkg/wire_handler.go` and
  `pkg/binary_entry_interface_test_framework_test.go`
  (`wg.Add`/`go func` → `sync.WaitGroup.Go`). These were produced during
  Task 12, so they land with Task 12 — committed in the close-out
  reconciliation rather than left in working-tree limbo. No follow-up.

## Success Criteria
- [x] Drift surface and feature-specific canary tests documented.
- [x] Troubleshooting guide tied to concrete files/tests.
- [x] Maintenance folded into existing repo cadence (no new SLA process).
- [x] Outstanding follow-ups captured for the retrospective/backlog.

## Status
**Status**: Finished
**Next Action**: /cwf-retrospective
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Maintenance for this feature is test-suite-driven: four canary tests
(byte aggregation, cross-path byte identity, byte-identity regression,
no-walk) pin the invariants most likely to drift. One follow-up (the
security-review test-glob exclude) is recorded for the maintainer; the
`go fix` modernisation was committed into Task 12 rather than deferred.

## Lessons Learned
Maintenance for a no-telemetry CLI is test-suite-driven: four canary
tests pin the drift-prone invariants. One follow-up (security-review
test-glob) captured for the maintainer. See j-retrospective.md.
