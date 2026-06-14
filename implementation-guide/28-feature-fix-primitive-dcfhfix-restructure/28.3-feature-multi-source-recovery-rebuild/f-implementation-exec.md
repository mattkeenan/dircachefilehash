# Multi-source recovery rebuild - Implementation Execution
**Task**: 28.3 (feature)

## Task Reference
- **Task ID**: internal-28.3
- **Task URL**: N/A (internal task)
- **Parent Task**: 28
- **Branch**: feature/28.3-multi-source-recovery-rebuild
- **Template Version**: 2.1

## Goal
Execute the implementation following the plan in d-implementation-plan.md and e-testing-plan.md.

## Execution Checklist
- [x] Read d-implementation-plan.md and e-testing-plan.md thoroughly
- [x] Verify all prerequisites met
- [x] Execute implementation steps sequentially
- [x] Update "Actual Results" for each step
- [x] Document any deviations from plan
- [x] Update status to "Finished" when complete

## Implementation Steps (from d-implementation-plan.md)

Three milestones, each gated with `go test ./...` + `golangci-lint run ./...`
(+ `-race -d=checkptr=0` on the new tests). Net change: one new file
`pkg/fix_recovery.go` (merge core + ordering + snapshot readback + the
`runRecoveryRebuild` batch branch), a small recovery-branch insertion in
`pkg/fix_run.go`, and two new test files. `repoCore.Fix` unchanged.

## Actual Results

### Milestone 1 — Merge core (LD2/LD3/LD4)
- **Planned**: `orderedSourcePaths` + `mergeSourcesIntoEntries`; per-source read via
  `collectForEdit(path, map[string]bool{}, "", "", options)` (no wrapper);
  union-by-path keep-first; drop deleted; sort; read-source confinement.
- **Actual**: Implemented in `pkg/fix_recovery.go`. Inlined `collectForEdit` with an
  empty pathSet (no `collectAllEntries` wrapper, per Rule of Three). Confirmed the
  empty-pathSet walk keeps deleted tombstones (needed for cross-source suppression)
  and drops a truncated tail with the discard counted. Read-source confinement reuses
  `confineWriteDest(path, metaDir)` (canonicalise + reject escape). TC-1…TC-8 in
  `pkg/fix_recovery_test.go` (union, precedence+determinism, truncated prefix,
  tombstone suppression, sort, mixed-checksum skip, empty/all-deleted, out-of-MetaDir
  reject) all pass.
- **Deviations**:
  - `mergeSourcesIntoEntries` returns **no `err`** (unparam: it never errors — a
    failing source is skipped tolerantly). The d-plan signature carried `err`; dropped
    as dead per lint. It additionally returns **`contributing []string`** (the sources
    that fed ≥1 entry) — needed by the snapshot readback (see Milestone 2).
  - **Checksum policy made concrete (refines LD5)**: the output `checksumType` is set
    by the **first contributing source** (highest precedence with ≥1 entry); a later
    source whose header type differs is skipped with its entries counted as discards
    (no abort, no re-hash). TC-6 tests this with `cache.idx` (SHA256) outranking and
    skipping a SHA512 `main.idx`.

### Milestone 2 — RunFix recovery branch (LD1/LD5/LD6/LD7/LD8/LD9)
- **Planned**: `FixOpRecoveryRebuild` const (write-by-omission, out of
  `fixOpMutatesIndex`); op-gated batch branch in `RunFix` lifting the single-ref guard;
  `runRecoveryRebuild` sequence order→merge→empty-guard→dry-run→snapshot+readback→
  ctx→confine→`writeRepairedIndex`.
- **Actual**: `FixOpRecoveryRebuild` added; classifies as a write via `fixOpIsWrite`
  and stays out of `fixOpMutatesIndex` (no double-backup). `RunFix` routes via
  `hasRecoveryOp` before the single-ref guard and rejects op-mixing. `runRecoveryRebuild`
  in `fix_recovery.go` builds `ms := initMetaStoreBase("", writeRoot)` in-branch (no
  `RunFix` signature change, `repoCore.Fix` untouched). Snapshot readback
  (`verifyRecoverySnapshot`) uses `os.Lstat` on the **contributing** sources under
  `recovery/`, requiring regular + size>0 and rejecting a symlink. TC-9…TC-15 pass.
- **Deviations**:
  - **Write options = `EditInPlace: true, Force: true`** (not the default preserve
    mode). Rationale: the pre-recovery snapshot (LD6) is the backup of record, and a
    destroyed (removed) `main.idx` cannot be preserved by `PreserveOriginal`
    (`os.Open` ENOENT) — EditInPlace lets the atomic rename create `main.idx` whether
    or not it existed. Cosmetic wart: `PromoteRepairedIndex` prints a hardcoded
    "original is NOT preserved" warning to stderr even though the snapshot did preserve
    it; library-only op, acceptable, noted for a future message refinement.
  - **Readback verifies `contributing`, not all `ordered`** (resolves the AC1↔AC4
    tension): a zeroed/too-small `main.idx` that is present-but-non-contributing must
    not force a false abort, so only sources that actually fed entries are required in
    the snapshot.

### Milestone 3 — Fault-injection + edge coverage (NFR5/AC5)
- **Planned**: atomicity test modelled on the Task 23 harness; lint clean; security
  verdict recorded.
- **Actual**: TC-16 in `pkg/fix_recovery_run_test.go` uses the existing `withSyncFault`
  seam (the snapshot copy uses `os.WriteFile`, so the seam is isolated to
  `writeRepairedIndex`): a sync fault leaves no `main.idx` and no `.fix.tmp`; a
  subsequent clean rebuild succeeds and loads clean via the production loader
  (`assertLoadsClean`). `golangci-lint run ./...` = 0 issues. No new gosec
  suppressions were needed — the new `os.Lstat`/`os.ReadFile` (via `collectForEdit`)
  sites reuse existing confined paths; the merge reads are confinement-checked in
  `orderedSourcePaths` before any open.

## Blockers Encountered

None. One surprise: `CreateMetaStore` (`NewMetaStore`) seeds an empty `main.idx`, so
the destroyed-main fixtures explicitly `os.Remove` it to model "destroyed (removed)".

## Deferral Check
Before marking status=Finished, verify:
- [x] All steps from d-implementation-plan.md executed (3 milestones)
- [x] All success criteria from a-task-plan.md met
- [x] All requirements from b-requirements-plan.md addressed
- [x] All design guidance in c-design-plan.md followed (LD1–LD9; LD5 checksum policy
  made concrete, LD7 floor descoped by design)
- [x] No planned work deferred without user approval
- [x] If work deferred: the optional under-floor guard (FR5 2nd clause) was descoped
  in design (LD7), not deferred here — no follow-up needed

**If deferral required**: Get user approval, document rationale, create follow-up task.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Security Review

**State**: no findings

> Note on changeset capture: the first review run produced `findings` solely
> because `pkg/fix_recovery.go` was untracked and thus invisible to `git diff`
> (the helper saw only the 23-line `RunFix` branch). The file was staged and the
> changeset re-built (288 production lines), and the reviewer re-ran against the
> complete surface — verdict below is the authoritative result. The one
> follow-up the first run raised (does `Force: true` weaken atomicity?) is
> answered by the code + TC-16: `writeRepairedIndex` *always* writes a temp index
> then `PromoteRepairedIndex` (atomic rename); `Force`/`EditInPlace` only suppress
> the redundant `.pre-fix` preservation, never the temp+rename discipline, and
> TC-16 proves a mid-write fault leaves no partial `main.idx`.

The tests confirm the confinement, snapshot-gate, empty-guard, and atomicity scenarios are exercised. I have enough to form a verdict.

## Security review of the 28.3 changeset

The reviewable production surface is two files: the new `pkg/fix_recovery.go` (recovery orchestration) and a small addition to `pkg/fix_run.go` (the op constant + batch-branch dispatch in `RunFix`). The rest of the diff is CWF process markdown (a–e plan files) and two test files. I read the production code in full and cross-checked its load-bearing claims against the actual sources it reuses (`confineWriteDest`, `createPreRecoverySnapshot`, `collectForEdit`).

**(a) Bash / unsafe command construction** — Not applicable. This is Go, not a shell helper; there is no `exec`, `os/exec`, `system`, or shell string construction anywhere in the diff. No finding.

**(b) Perl / git-output parsing** — Not applicable. No Perl, no git porcelain parsing in this changeset. No finding.

**(c) Prompt injection** — Not applicable to this code. The recovery path consumes index *selectors* (file paths) and binary `.idx` content, none of which flows into LLM context. The plan markdown files are CWF process docs authored by the workflow, not untrusted external input reaching a downstream model. No finding.

**(d) Environment-variable handling** — No env vars are read in the diff. The write destination is the fixed `ms.IndexFile` (structurally inside `MetaDir`), never env- or selector-derived. No finding.

**(e) Pattern-based / path-handling risks** — This is the category that matters for a data-destructive, path-driven rebuild. I checked the path-handling carefully and found the design defends the relevant invariants:

- **Read-source confinement.** Every candidate source path (named selectors *and* auto-discovered timestamped caches) is passed through `confineWriteDest(c, metaDir)` in `orderedSourcePaths` (`pkg/fix_recovery.go:1050`) before it is ever opened. A path escaping `MetaDir` is a hard error, not a silent skip. This is what makes the reused `collectForEdit` `os.ReadFile` (`pkg/fix_entry_workflow.go:195`, carrying a `//nolint:gosec // G304` rationale that asserts "never a raw selector") stay truthful on the new caller: the path reaching it is MetaDir-confined.
- **Symlink handling.** Both `orderedSourcePaths` (line 1060) and `verifyRecoverySnapshot` (line 1149) use `os.Lstat` and require `Mode().IsRegular()`, so a planted symlink in the source set or in `recovery/` is rejected/skipped rather than followed. `confineWriteDest` itself resolves the parent via `EvalSymlinks` and fail-closes.
- **Destructive-write guards.** The empty-merge guard aborts before any snapshot or rename (line 950); the snapshot precondition is wrapped with a fatal readback (`verifyRecoverySnapshot`) that converts the best-effort `createPreRecoverySnapshot` into a hard precondition; the `writeRoot == ""` library-only assertion (line 928) keeps the destructive op off the unconfined CLI exemption; and the final write goes through the single-writer `writeRepairedIndex` (temp + atomic rename). These are correctness/data-safety mitigations and they are present and test-covered.

One pattern worth recording with the required framing, not as a defect:

1. **`verifyRecoverySnapshot` verifies existence + non-emptiness, not byte-integrity, with an acknowledged TOCTOU window** (`pkg/fix_recovery.go:1145`). The readback `Lstat`s each contributing source's snapshot copy in `recovery/` and checks regular-file + non-zero size, but does not verify the copy's content matches the source, and there is a window between snapshot, readback, and the rebuild write. *Safe here because* the operation runs entirely inside the user's own `MetaDir` with no privilege boundary crossed, the destination is the fixed in-`MetaDir` `main.idx`, and the pre-recovery snapshot plus atomic rename bound the blast radius — this exact residual risk is called out and accepted in design LD6. *Audit future uses* where this readback pattern (presence-only validation of a "backup") might be relied on as an integrity guarantee, or where the recovery op could ever be reachable across a privilege boundary or with an attacker-writable `recovery/` directory — there the existence check would not be sufficient and content/atomicity verification would be needed.

No actionable security defects. The path-confinement, symlink, and destructive-write invariants the threat model cares about are enforced before the read and before the write, and the one residual pattern is correctly scoped and documented.

```cwf-review
state: no findings
summary: Recovery rebuild confines read sources and the fixed write dest to MetaDir, Lstat-rejects symlinks, and gates the destructive write behind empty-merge + snapshot-readback guards; one documented presence-only/TOCTOU pattern noted as safe-here per LD6, no actionable defect.
```

## Lessons Learned
Stage net-new production files before running `security-review-changeset` — the
first review run saw only the 23-line RunFix branch because `fix_recovery.go` was
untracked. Concrete checksum policy and contributing-not-ordered readback were the
two exec-time refinements. See j-retrospective.md.
