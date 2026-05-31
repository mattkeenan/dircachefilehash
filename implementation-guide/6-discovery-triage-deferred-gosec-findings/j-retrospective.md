# Triage deferred gosec findings - Retrospective
**Task**: 6 (discovery)

## Task Reference
- **Task ID**: internal-6
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: discovery/6-triage-deferred-gosec-findings
- **Template Version**: 2.1
- **Retrospective Date**: 2026-05-31

## Executive Summary
- **Duration**: ~1 day (estimate 0.5–1 day) — within estimate despite a self-inflicted
  worktree mishap that cost ~one recovery cycle.
- **Scope**: As planned — audit every gosec exclude/suppression, decide the G304 policy, and
  close out. The backlog premise was already known to be stale; the empirical run confirmed
  *how* stale (perms/subprocess/pprof/http long since handled; only G304 was a live decision).
- **Outcome**: Success. The gate is gosec-clean with G304 converted from a blanket exclude to
  23 per-line, trust-classified suppressions; the one untrusted-reachable open carries a cited
  escape guard. Two empirical corrections to the plan landed (see below).

## Variance Analysis
### Time and Effort
- **Estimated** (a-task-plan): 0.5–1 day total, audit-weighted.
- **Actual**: Planning a–e in a prior session; exec f–g in this session. Net within estimate.
  The notable cost was not the triage itself (mechanical once ground truth was in hand) but
  the worktree-CWD mishap and its recovery (~one extra verify cycle).
- **Variance**: Effort tracked the estimate. The risk the plan flagged (G304 analysis) was
  *not* where time went — it went into a process hazard the plan did not anticipate.

### Scope Changes
- **Additions**: None to deliverables. Two **factual corrections** vs the plan:
  1. G703 is a real gosec rule ("path traversal via taint analysis") — the plan assumed it
     was not and predicted G306 for the `os.WriteFile` sites. Disposition flipped to *accept*.
  2. The mislabelled comments were the env-var `os.Create` sites (real rule G304), not the
     WriteFile sites.
- **Removals**: None. The out-of-scope items named in FR1 (`convert-index-v1-to-v2.go`,
  `_test.go`, fixtures) stayed out of scope.
- **Impact**: No timeline impact; the corrections were caught by the very method the design
  mandated (empirical ground truth over remembered rule semantics).

### Quality Metrics
- **Gate**: `golangci-lint run ./...` → **0 gosec findings** (only out-of-scope
  cyclop:2/unparam:1 remain, tracked separately). Full `go test -race` green via pre-commit.
- **Defect Rate**: 0 functional defects (config/comment/doc task; on-disk format and CLI
  behaviour byte-unchanged). 1 *process* defect (lost worktree edits), fully recovered.
- **Reconciliation**: per-rule `emitted = fix+suppress+exclude+accept` balances
  (G304: 23 suppress; G703: 2 accept; G103/G401/G505 category-exclude; pre-existing
  G115/G301/G306/G302/G204/G114/G108 accepted, IDs confirmed by the clean gate).

## What Went Well
- **The empirical method paid for itself.** Design Decision 1 (gather ground truth from the
  tool, not from comment text or memory) directly caught the G703/G306 error and the env-site
  mislabel. A plan that trusted the rationale text would have written wrong suppressions.
- **Trust-boundary analysis stayed honest.** The wire path was traced to its real guard
  (`hashOne`→`resolveRel`→`hasPathPrefix`), and `loadHashCache`'s config-sourced read was kept
  distinct rather than lumped in — exactly the distinction the d-plan reviewers had forced.
- **Atomic landing held.** Exclude removal + all 23 suppressions shipped in one commit
  (`90ff0f2`); the gate was never left red.
- **Recovery was clean.** The lost work was reconstructed from the dangling stash commit, not
  redone from memory — so the committed changeset is provably identical to what passed
  verification.

## What Could Be Improved
- **The worktree-CWD hazard.** `cd "$wt"` into the disposable worktree left the shell CWD
  there; subsequent `cd "$(git rev-parse --show-toplevel)"` resolved to the *worktree* root, so
  every "real-tree" edit, the lint, and the tests ran in the worktree and were deleted with it.
  Verification passed — against the wrong tree. This is the core lesson.
- **Earlier reflog instinct.** When the changes "vanished", the first diagnostic was the HEAD
  reflog (correct that it showed nothing, but the wrong tool — uncommitted edits never touch
  it). The user's prompt redirected to `git fsck`/stash-reflog, which found the dangling
  `task6-verify` stash immediately. Recovery-tool selection should match the loss mode:
  uncommitted → fsck/stash objects; committed → reflog.
- **A `sed` substitution stripped instead of replaced** the env-var comments (delimiter/
  pattern interaction with an apostrophe). Append-then-verify was more reliable than
  substitute-in-place for comment edits.

## Key Learnings
### Technical Insights
- **gosec construct→rule mapping**: `os.WriteFile(dst, …)` with a taint-tracked destination
  emits **G703** (taint path-traversal); `os.Open`/`ReadFile`/`Create` with a variable path
  emit **G304**. The construct predicts the rule; the comment text does not.
- **`//nolint:gosec // Gxxx`** suppresses *all* gosec on the line — the trailing `Gxxx` is
  free-text, not parsed. So a wrong ID never affects the gate; it only misleads humans (which
  is precisely the FR4 hygiene concern this task fixed).
- **Per-line beats blanket for a small, reason-diverse rule set.** The 21 G304 sites split
  across genuinely different rationales (CLI arg / env / `.dcfh` / guarded-wire / operator
  cache); per-line suppression documents each honestly and makes a future *unguarded* open
  fail the gate.

### Process Learnings
- **Verification is only as good as the tree it runs against.** A green gate proves nothing if
  it ran in a throwaway worktree. Pin scratch work to absolute paths, or never `cd` into a
  disposable worktree from the primary session.
- **Estimation held** for an audit task; the variance driver was a tooling-discipline slip,
  not analytical difficulty.

### Risk Mitigation Strategies
- The plan's named risk (mis-suppressing a real G304 traversal) was mitigated as designed —
  per-site trust classification, guard-citing on the one untrusted-reachable site.
- The *unplanned* risk (work loss) was survivable only because a `git stash` had been taken
  during verification; the stash object outlived the worktree. Recoverability by construction.

## Recommendations
### Process Improvements
- Add a convention note: **scratch-worktree work uses absolute paths**; do not `cd` into a
  disposable worktree from the session that also edits the primary tree. (Candidate addition to
  `.cwf/docs/conventions/tmp-paths.md`.)
- When changes "disappear", branch the diagnostic on loss mode: committed→`git reflog`;
  uncommitted/worktree→`git fsck --unreachable` + stash reflog.

### Tool and Technique Recommendations
- Prefer **append + grep-verify** over in-place `sed` substitution for editing existing
  comments containing punctuation.
- Keep capturing gosec ground truth as JSON (`--output.json.path=stdout`) and parsing rule IDs
  from `Issues[].Text` — it is the single source of truth for "what rule fires here".

### Future Work
- No new follow-up task warranted. The pre-existing **cyclop/unparam** backlog item remains
  open (explicitly out of scope here). No new security debt was introduced.

## Status
**Status**: Finished
**Next Action**: Task complete; suggest merge (see below)
**Blockers**: None identified
**Completion Date**: 2026-05-31
**Sign-off**: Matt Keenan / Claude (CWF discovery workflow)

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Archived Materials
- Planning: a-task-plan.md … e-testing-plan.md (commits 6296d9c, 094065b, 78a6e15, 31e426c, 8370211)
- Implementation: f-implementation-exec.md (commit 90ff0f2)
- Testing: g-testing-exec.md (commit 60e79d4)
- Ground-truth JSON: `/tmp/-home-matt-repo-dircachefilehash-task-6/gosec.json`
- Recovered changeset: dangling stash `a49e33b` ("task6-verify")
