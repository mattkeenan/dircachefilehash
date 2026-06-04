# Upgrade CWF to v1.1.177 via cwf-manage update - Retrospective
**Task**: 9 (chore)

## Task Reference
- **Task ID**: internal-9
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/9-upgrade-cwf-to-v1-1-177-via-cwf-manage-update
- **Template Version**: 2.1
- **Retrospective Date**: 2026-06-04

## Executive Summary
- **Duration**: ~1 day across planning + exec (estimated: <0.5–1 day, on-target).
- **Scope**: Unchanged from a-task-plan.md — upgrade the CWF subtree v1.1.169 →
  v1.1.177 via `cwf-manage update`, leave the tree validate-clean and the
  workflow tooling functional. No additions, no descopes.
- **Outcome**: Success. `cwf-manage update v1.1.177` applied in one pass — no
  artefact-conflict prompt, no perms abort, no fix-security pass. `validate`
  clean exit 0 on first run; all six acceptance cases PASS; project code
  untouched.

## Variance Analysis
### Time and Effort
- **Estimated** (chore phase set a/d/e/f/g/j): Planning ~0.25 day; Impl exec
  ~0.25 day; Testing exec ~0.1 day.
- **Actual**: Planning was the heavier half — the `cwf_sha` forward-only
  investigation (reading the installed `cwf-manage` source to establish which
  binary writes `.cwf/version`) took most of the effort. Exec + testing were
  fast because the plan had already pinned every expected value.
- **Variance**: Within estimate. Effort shifted *earlier* than a naive plan
  would — front-loaded into verifying the success criterion, which made exec a
  confirmation rather than a discovery.

### Scope Changes
- **Additions**: none.
- **Removals**: none. b/c/h/i phases are not part of the chore template (skipped
  by type, not descoped).
- **Impact**: none.

### Quality Metrics
- **Test Coverage**: 6/6 functional acceptance cases PASS (TC-1..6); 2/2
  negative/safety cases (TC-7 revert, TC-8 half-applied) correctly N/A — nothing
  aborted, so there was nothing to recover from.
- **Defect Rate**: 0 defects. No revert, no half-applied state.
- **Integrity**: `cwf-manage validate` exit 0 (sha256 + T170 perms-ceiling).

## What Went Well
- **The forward-only `cwf_sha` prediction held exactly.** The plan predicted the
  outer (pre-T175 v1.1.169) cwf-manage would record the **tag-object** SHA
  `1cae055…`, not the dereferenced commit `ed664b25…`. That is precisely what
  landed. Reading the installed source instead of trusting the T175 changelog
  headline was the difference between a green run and a false-failure scramble.
- **Clean single-pass laydown.** Unlike Task 5 (the v1.1.169 upgrade, which hit
  the v1.1.163 `rules-inject` artefact defect on its first attempt), this span
  (170→177) is past the T167 fix and carried no analogous packaging defect. The
  retained escape hatch in d-plan Step 2 was never needed.
- **T170 perms-ceiling did its own cleanup.** The laydown's exact-perms pass
  tightened drifted modes (agent `.md` 0600→0444, scripts 0700→0500) back to the
  recorded ceiling during the update, so `validate` was clean on the first run —
  no separate `fix-security` pass.
- **T176 doc-split landed transparently.** `.cwf/docs/workflow/workflow-steps/`
  arrived with 10 per-phase files and `workflow-steps.md` reduced to a ToC; every
  skill/agent/rule symlink resolves, so the plan-phase SKILL Step-5 reads keep
  working.

## What Could Be Improved
- **The security-review cap always fires on a CWF self-upgrade.** Both exec
  phases recorded `**State**: error` because `security-review-changeset` exited
  `2` (1759 then 1906 production lines > the 500 cap). This is the *documented*
  contract for a vendored-subtree upgrade with no `max-lines-exclude-paths`
  config, and the deterministic `cwf-manage validate` is the right integrity
  gate here — but a reader skimming the wf files sees `error` and has to read the
  rationale to learn it is benign. The signal could be less alarming for this
  recurring, structurally-benign case (see Recommendations).

## Key Learnings
### Technical Insights
- **"Ships in version X" ≠ "governs the upgrade *to* X".** A self-modifying tool
  upgrade has an asymmetry: the *outer* (currently-installed) binary is the
  authoritative writer of post-laydown state, while the target's code only takes
  effect from the *next* invocation. T175's `^{commit}` fix shipped *in* v1.1.177
  but is forward-only — same pattern as T159 (`git_describe_version`). The
  general rule for any tool that upgrades itself: identify which copy runs the
  decisive write, and read *that* copy's source.
- **A vendored-subtree upgrade is content-diffed, not history-diffed.** Even
  though `cwf-manage` does a remove-then-re-add (9 commits incl. `Squashed …`
  markers), `git diff baseline..HEAD` collapses to the real v1.1.169→v1.1.177
  source delta. The squash at retrospective is therefore safe: future
  `cwf-manage update` re-clones the source and does not rely on our subtree
  merge history.

### Process Learnings
- **Front-loading the riskiest assumption into planning paid off.** The single
  hardest thing to get right (the `cwf_sha` semantics) was nailed down in the
  a-plan via source-reading and re-checked by all four plan reviewers, so exec
  was a confirmation pass with pre-pinned expected values. For operational
  upgrades, the verification anchor belongs in the plan, not discovered at exec.

### Risk Mitigation Strategies
- **The retained revert escape hatch cost nothing and bought confidence.**
  Carrying the soft-reset + targeted-checkout + dry-run-`git clean` procedure in
  d-plan Step 2 (even though Task 5's specific defect was already fixed) meant a
  mid-laydown abort would have had a known, non-destructive recovery. It was not
  needed, but planning for it is cheap insurance for a self-modifying-tool change.

## Recommendations
### Process Improvements
- When recording a `security-review` `error` that is the *cap-exceeded* contract
  on a CWF-vendored-only changeset, keep stating the benign rationale inline (as
  done in f/g) so the `error` token is never read as a real finding.

### Tool and Technique Recommendations
- For any future self-modifying-tool upgrade, default to reading the
  *currently-installed* tool's source to predict post-upgrade state, rather than
  the target's changelog. Capture the predicted decisive value as a plan-level
  verification anchor.

### Future Work
- This is **not a new** item — Task 5 already filed *"Configure
  security.review.test-paths to exclude upstream-shipped CWF directories"*
  (Low, BACKLOG.md), which asks to verify the cap "against a CWF-upgrade-shaped
  changeset". Task 9 **is** that changeset and provides a second data point
  (1759/1906 production lines, vs Task 5's 1632). That item has been augmented
  with Task 9's measurement **and a caveat Task 9 surfaced**: discounting
  `.cwf/**` from the cap does *not* simply silence the noise — because the full
  changeset is always emitted to the subagent regardless of the cap, excluding
  `.cwf/**` would flip a *pure* upgrade task's disposition from "error/skip" to
  "invoke the subagent on the entire vendored delta". For a pure upgrade the
  current "error/skip + rely on `cwf-manage validate`" outcome may actually be
  the preferable one; the config fix is better-aimed at *mixed* tasks where CWF
  content is incidental. Whoever picks up that item should weigh the two cases
  separately.

## Status
**Status**: Finished
**Next Action**: Task complete
**Blockers**: None identified
**Completion Date**: 2026-06-04
**Sign-off**: Matt Keenan (with Claude)

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Archived Materials
- Planning: `a-task-plan.md`, `d-implementation-plan.md`, `e-testing-plan.md`
- Execution: `f-implementation-exec.md` (laydown), `g-testing-exec.md` (TC-1..8)
- Key commits (pre-squash): f `dba0116`, g `1a6edaf`; laydown subtree commits
  `cc3ee2c..90170ac`.
- Recorded state: `.cwf/version` → `v1.1.177`, `cwf_sha=1cae055…` (tag-object).
