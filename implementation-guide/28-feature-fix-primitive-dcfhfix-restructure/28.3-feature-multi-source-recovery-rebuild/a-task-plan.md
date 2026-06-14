# Multi-source recovery rebuild - Plan
**Task**: 28.3 (feature)

## Task Reference
- **Task ID**: internal-28.3
- **Task URL**: N/A (internal task)
- **Parent Task**: 28
- **Branch**: feature/28.3-multi-source-recovery-rebuild
- **Baseline Commit**: 61efe765d456876761cf1c883c5b2e3aa8634624
- **Template Version**: 2.1

## Goal
Land the v0.7 recovery write path as a multi-source `Repo.Fix` batch: rebuild
`main.idx` from any readable combination of `main.idx` / `cache.idx` /
timestamped-cache sources via a net-new `mergeSourcesIntoEntries`, writing through
the single-writer path with snapshot-protected, fault-injection-proven atomicity
(closes parent FR8/AC5/NFR5).

## Success Criteria
- [ ] A recovery `Fix` batch rebuilds a valid `main.idx` from surviving sources
  (e.g. destroyed `main.idx` + intact `cache.idx` → re-readable, checksum-valid `main.idx`).
- [ ] Same-path conflict across sources resolves by a documented, deterministic
  precedence — timestamped caches (newest→oldest) > `cache.idx` > `main.idx` —
  asserted by test.
- [ ] A source with a readable header but truncated body yields a **concrete
  asserted entry count** (its readable validated prefix), with discards surfaced
  via `FixResult` — never silently "best-effort".
- [ ] Atomicity proven by fault injection (Task 23 harness model): interruption
  mid-rebuild leaves all originals intact (no partial/empty `main.idx`); an
  empty/under-floor merged set aborts **before** the rename.
- [ ] `createPreRecoverySnapshot` is a hard precondition (snapshot failure ⇒ no
  write); `golangci-lint run ./...` clean and the CWF changeset security verdict recorded.

## Original Estimate
**Effort**: ~2–3 days
**Complexity**: High (data-destructive write path, though scope is one concern)
**Dependencies**: 28.2 `RunFix`/`FixRequest` + confinement (landed at baseline); `pkg/recovery.go` validators + `createPreRecoverySnapshot`; Task 23 fault-injection harness.

## Major Milestones
1. **Merge core**: `mergeSourcesIntoEntries(refs, precedence)` — read + validate each
   source via `ValidationProcessor` (lenient), union by relative path under the
   precedence rule, count discards. Unit-tested in isolation against fixtures.
2. **Recovery rebuild wired into `RunFix`**: a recovery operation distinct from the
   per-subject command loop (reads many sources, writes one `r.ms.IndexFile`);
   snapshot precondition + empty/under-floor guard + single-writer write + atomic
   rename; surfaced through `repoCore.Fix`.
3. **Fault-injection + edge coverage**: atomicity harness over the rebuild;
   truncated-body / same-path-conflict / empty-source / under-floor cases; security
   review; lint green.

## Risk Assessment
### High Priority Risks
- **Data-destructive write path**: rebuilding `main.idx` from partial/corrupt
  sources is the highest-consequence code in the repo — a bug can overwrite the
  user's only index with garbage or an empty header.
  - **Mitigation**: write-new + atomic rename only (never in-place); mandatory
    `createPreRecoverySnapshot` precondition; empty/under-floor guard aborts before
    rename; the fault-injection gate (Milestone 3) lands before this path is trusted.
- **Recovery is a batch-level op, not a per-subject command**: `RunFix` today
  dispatches one subject per `FixCommand`; the rebuild reads N sources and writes one
  destination. Forcing it into the per-command loop would muddle the semantics.
  - **Mitigation**: the design phase fixes the integration seam (a distinct
    batch-level branch in `RunFix`, not a 10th per-subject op) before any code.

### Medium Priority Risks
- **Merge/precedence correctness across truncated + conflicting sources is subtle**:
  off-by-one in the readable prefix or a wrong precedence tie-break corrupts state.
  - **Mitigation**: table-driven tests enumerating source combinations; precedence
    documented in `c-design-plan.md` and asserted, not assumed.
- **`FixResult` reporting gap**: 28.2 shipped `FixResult{RepairsApplied, EntriesDiscarded}`
  only (dropped `IndexFilesProcessed`/`BackupID`); recovery may need sources-processed
  reporting.
  - **Mitigation**: decide in design whether to re-add a counter or report via the
    existing two — do not expand the struct without a tested consumer.

## Dependencies
- 28.2 primitive (`RunFix`, `FixRequest`, `FixCommand`, `confineWriteDest`/`confineWriteDir`) — landed at baseline `61efe765`.
- `pkg/recovery.go`: `ValidationProcessor`/`RecoveryValidationProcessor`, `(*MetaStore).createPreRecoverySnapshot` — reused, not rewritten.
- `pkg/fault_inject_test.go` + `pkg/atomic_index_test.go` (Task 23) — fault-injection model.

## Constraints
- Single writer (`TempIndexWriter`/`EntrySerialiser`): rebuild serialises a merged
  entry set and atomic-renames; no in-place mutation of any `.idx`.
- Write destination is always `r.ms.IndexFile` — never selector-derived; D2
  confinement applies (selector resolving outside `MetaDir` rejected before write).
- No on-disk format change; no new third-party dependencies; British spelling in prose.

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [ ] **Time**: Will this take >1 week? No — ~2–3 days, one concern.
- [ ] **People**: Does this need >2 people? No — solo.
- [ ] **Complexity**: Does this involve 3+ distinct concerns? No — single concern
  (multi-source merge + atomic rebuild); the three milestones are sequential steps
  of one path, not separable concerns.
- [x] **Risk**: Are there high-risk components that need isolation? Yes — this *is*
  the isolated high-risk subtask the parent decomposition already carved out for the
  data-destructive write path; the fault-injection gate is its containment.
- [ ] **Independence**: Can parts be worked on separately? No — merge core → wiring →
  coverage is a single dependency chain.

**Result: 1 of 5 signals → no further decomposition.** 28.3 is itself the isolated
high-risk leaf the parent task split out; subdividing further would fragment one
write path.

## Status
**Status**: Finished
**Next Action**: /cwf-requirements-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All 5 success criteria met; delivered in ~1 day vs 2–3 day estimate (≈ −60%).
288 production + 533 test lines; 16/16 TCs; both security reviews no findings.
Full variance analysis in j-retrospective.md.

## Lessons Learned
The "High complexity" label tracked blast radius, not residual effort — 28.2 had
already de-risked the write mechanics, so this was assembly. See j-retrospective.md.
</content>
</invoke>
