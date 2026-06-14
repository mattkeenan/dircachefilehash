# Fix primitive + dcfhfix restructure - Retrospective
**Task**: 28 (feature)

## Task Reference
- **Task ID**: internal-28
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/28-fix-primitive-dcfhfix-restructure
- **Template Version**: 2.1
- **Retrospective Date**: 2026-06-14

## Executive Summary
- **Duration**: ~3 days wall-clock (2026-06-12 plan → 2026-06-14 retrospective)
  vs the original ~1.5–2 week estimate — variance ≈ −75%. The coordinating
  parent's own phases (a–j) plus three child tasks all landed in that window.
- **Scope**: Delivered exactly as decomposed — `Repo.Fix` primitive symmetric to
  `Repo.Filter`, fsck helpers relocated to `pkg/` with dcfhfix reduced to a thin
  `RunFix` translator, and a multi-source `recovery-rebuild` Fix op. No scope
  added; two items deferred by design (manual interactive mode; the `BackupID`
  field, resolved via `fixes-list`).
- **Outcome**: Success. All five parent success criteria (SC1–SC5) map to a
  landed, tested surface; the assembled branch is green on build + union suite +
  `-race` + `golangci-lint`(0) + `govulncheck`(0). The parent added **zero**
  production lines — it integrated three already-tested squashes.

## Variance Analysis
### Time and Effort
- **Estimated** (parent a-plan): ~1.5–2 weeks, High complexity, across three
  milestones — explicitly flagged for decomposition.
- **Actual** (per subtask retrospectives):
  - 28.1 (helper relocation + FR9 entry-writer fix): ~2 days (est 2–3, within).
  - 28.2 (Fix primitive + CLI translator): ~1 day (est 3–4, −70%).
  - 28.3 (multi-source recovery rebuild): ~1 day (est 2–3, −60%).
  - Parent coordination (a–e plans + f/g integration + h/i/j): folded into the
    same 3-day window; each phase was verification/reframing, not new code.
- **Variance**: large under-run (−75%). Causes: (1) decomposition produced three
  tightly-scoped, independently-shippable subtasks that each beat their own
  estimate; (2) the parent's working phases were integration verification with
  no production code to write; (3) the prerequisite (`pkg/format.SafeEntry`,
  Task 3.1) was already in place, so 28.1 had a clean base.

### Scope Changes
- **Additions**: none at the parent level.
- **Removals / deferrals (all by design, user-approved at subtask level)**:
  - Manual interactive fsck mode → typed `ErrManualModeUnimplemented`, fail-closed
    (no write); re-expressing it is a future task.
  - `FixResult.BackupID` dropped (28.2 LD7); backups surfaced via `fixes-list`.
  - 28.1 backup-stack relocation pushed into 28.2 where the consumer lived.
- **Impact**: the API shape evolved from the parent a-plan prose
  (`FixRequest.IndexSelectors`/`Mode`/`Flags`, not a single `IndexSelector`) — an
  accepted evolution, reconciled in the parent d-plan so docs match code.

### Quality Metrics
- **Test coverage**: recovery merge core 93%+, all destructive/confinement/
  atomicity guard predicates 100% (`hasRecoveryOp`, `verifyRecoverySnapshot`,
  `fixOpIsWrite`, `fixOpMutatesIndex`); low figures are cosmetic helpers only.
  460 `Test*`/`Example*` functions in the union suite.
- **Defect rate**: zero integration defects. The only findings were a doc nit
  (stale CLAUDE.md dcfhfix examples) and a cosmetic stderr message — both routed
  to BACKLOG, neither a code defect.
- **Performance**: NFR1 (no new index passes) holds by construction — parent adds
  no production code; subtasks asserted collect-once/write-once.
- **Security**: each subtask changeset reviewed under-cap with `no findings`; the
  parent union trips the 500-line cap by construction (recorded as the expected
  exit-2 error in f/g, not a skipped review).

## What Went Well
- **Decomposition paid off precisely as the a-plan predicted** (4/5 signals).
  The migration→primitive→recovery dependency chain let each subtask ship and be
  reviewed in isolation, keeping every changeset under the security-review cap and
  the highest-risk code (recovery write path) in its own task.
- **Risk-first ordering held**: the data-destructive recovery path landed last,
  on top of a proven primitive, behind snapshot-readback + atomic-rename +
  fault-injection coverage — the headline High risk never materialised.
- **Behaviour-preserving prerequisite first**: 28.1 moved helpers as a pure
  refactor with dcfhfix tests as the gate before any re-routing through
  `Repo.Fix`, exactly as the risk mitigation prescribed.
- **Single-writer invariant preserved end-to-end**: `TempIndexWriter` remained
  the only entry writer; 28.1 even fixed a pre-existing broken `O_APPEND` writer
  (FR9) as a bonus correctness win.

## What Could Be Improved
- **The security-review-changeset cap is a poor fit for a coordinating parent.**
  Anchored at the pre-decomposition baseline, the parent diffs the union of all
  three subtasks (3732 lines) and always trips the 500-line cap — producing an
  `error` state that *looks* like a skipped review until you read the rationale.
  The meaningful review unit is the per-subtask diff (each already `no findings`).
  Worth a CWF enhancement: let a coordinating-parent exec anchor at the last
  child squash (empty/near-empty diff) rather than the decomposition baseline.
- **The SaaS-shaped h/i templates needed wholesale reframing** for an offline
  CLI/library (no uptime/SLA/canary/on-call/scaling). Not a defect, but each
  phase spent effort translating a fictional template to the real distribution
  model (trunk ff-merge + goreleaser).
- **Doc drift surfaced late**: CLAUDE.md's `dcfhfix … scan/header` examples
  predate the 28.2 restructure and were only caught at the parent smoke test.

## Key Learnings
### Technical Insights
- A coordinating parent's exec phases are **verification, not authorship**: LSP
  symbol resolution (surfaces coexist, no collision) + the union test suite + one
  live CLI round-trip prove composition without re-reading every diff.
- Recovery-rebuild's confinement (`writeRoot == ""` ⇒ library-only) plus
  `Lstat`-reject-symlink plus empty-merge/snapshot-readback guards are the load-
  bearing data-safety invariants; they are individually test-covered and the
  right place to focus review attention on a destructive path.

### Process Learnings
- **Estimation**: High-complexity multi-concern tasks systematically over-
  estimate once decomposed — the sub-parts, sized independently, each came in
  well under. The decomposition itself is the estimate-improver.
- **The integration parent earns its keep cheaply**: zero production lines, but
  it produces the single green regression-gate evidence + success-criteria map
  that no individual subtask covers.

### Risk Mitigation Strategies
- "Move first as pure refactor, re-route second" (28.1→28.2) kept the dcfhfix
  behaviour-regression risk from ever co-occurring with the API change — an
  effective, repeatable pattern for cmd→pkg migrations.

## Recommendations
### Process Improvements
- Add a CWF affordance for coordinating-parent security review: anchor the
  changeset at the last child squash (so the parent diff is empty and the helper
  reports `no findings: empty changeset`) instead of the pre-decomposition
  baseline that guarantees a cap-exceeded `error`.
- Consider a lightweight "offline-tool" variant of the h-rollout / i-maintenance
  templates (build→merge→tag→publish; CI-gate-as-monitoring) so CLI/library tasks
  don't re-derive the same reframing each time.

### Tool and Technique Recommendations
- LSP symbol resolution as the primary integration-verification tool for
  coordinating parents (cheaper and more reliable than diff re-reading).

### Future Work (→ BACKLOG)
1. **Doc-refresh (chore, Low)**: update CLAUDE.md's stale `dcfhfix` example
   syntax (`scan`/`header …`) to the shipped `header|entry|fixes` surface.
2. **Cosmetic (chore, Very Low)**: refine `PromoteRepairedIndex`'s hardcoded
   "original is NOT preserved" stderr line for the snapshot-backed recovery path.
3. **Deferred feature**: implement the manual interactive fsck mode currently
   behind `ErrManualModeUnimplemented` (if user demand warrants).

## Status
**Status**: Finished
**Next Action**: Task complete — suggest ff-merge to main (human-owned)
**Blockers**: None identified
**Completion Date**: 2026-06-14
**Sign-off**: Matt Keenan / Claude (CWF workflow)

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Archived Materials
- Planning/design: `a-task-plan.md` … `e-testing-plan.md` (this directory).
- Integration evidence: `f-implementation-exec.md`, `g-testing-exec.md`.
- Rollout/maintenance: `h-rollout.md`, `i-maintenance.md`.
- Subtasks: `28.1-…/`, `28.2-…/`, `28.3-…/` (each with full a–j + own
  retrospective).
- Commits: subtask squashes `a0bcf0e0` (28.1) / `61efe765` (28.2) / `752cde35`
  (28.3); parent phase commits `486c6bc3`…`ecd82069`; baseline `c385cb8`.
