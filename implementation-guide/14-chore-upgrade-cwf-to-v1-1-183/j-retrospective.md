# upgrade CwF to v1.1.183 - Retrospective
**Task**: 14 (chore)

## Task Reference
- **Task ID**: internal-14
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/14-upgrade-cwf-to-v1-1-183
- **Template Version**: 2.1
- **Retrospective Date**: 2026-06-07

## Executive Summary
- **Duration**: ~1 day across planning + exec (estimated: <0.5–1 day, on-target).
- **Scope**: Unchanged from a-task-plan.md — upgrade the CWF subtree v1.1.177 →
  v1.1.183 via `cwf-manage update`, leave the tree validate-clean, the workflow
  tooling functional, and the newly-shipped PreToolUse hooks merged without
  surprising the active workflow. No additions, no descopes.
- **Outcome**: Success. `cwf-manage update v1.1.183` applied in one pass — no
  artefact-conflict prompt, no settings-merge abort, no perms abort, no
  fix-security pass. `validate` clean exit 0 on first run; all executable
  acceptance cases (TC-1..6, TC-9) PASS; project code untouched. The central
  `cwf_sha` commit-form prediction held exactly.

## Variance Analysis
### Time and Effort
- **Estimated** (chore phase set a/d/e/f/g/j): Planning ~0.25 day; Impl exec
  ~0.25 day; Testing exec ~0.1 day.
- **Actual**: Planning was again the heavier half — establishing the **commit-SHA
  flip** (reading the installed v1.1.177 `cwf-manage` `resolve_sha` to confirm
  `^{commit}` is now present) and pre-reading the v1.1.178→183 changelog deltas
  for consumer-affecting changes (T180 guard, T181 worktree, T182 reviewer
  contract). Exec + testing were fast because every expected value was pinned.
- **Variance**: Within estimate; effort front-loaded into verifying the success
  criterion, making exec a confirmation pass rather than a discovery.

### Scope Changes
- **Additions**: none.
- **Removals**: none. b/c/h/i phases are not part of the chore template (skipped
  by type, not descoped).
- **Impact**: none.

### Quality Metrics
- **Test Coverage**: 7/7 executable acceptance cases PASS (TC-1..6, TC-9); 2/2
  negative/safety cases (TC-7 revert, TC-8 half-applied) correctly N/A — nothing
  aborted, so there was nothing to recover from.
- **Defect Rate**: 0 defects. No revert, no half-applied state.
- **Integrity**: `cwf-manage validate` exit 0 (sha256 + T170 perms-ceiling).
- **Security**: both exec-phase changeset reviews `no findings` (this time the
  cap was raised once and the subagent actually ran — see Improvements).

## What Went Well
- **The commit-form `cwf_sha` prediction held exactly — the inverse of Task 9.**
  Task 9's writer was the pre-T175 v1.1.169 binary, so it recorded the
  **tag-object** SHA. This time the writer is the post-T175 v1.1.177 binary, so
  it recorded the **commit** SHA `faf92479…`, NOT the tag-object `1842f7b0…`.
  Reading the installed `cwf-manage` `resolve_sha` (by sub name, not line number)
  instead of trusting the changelog headline made this a green run.
- **Clean single-pass laydown.** This span (178→183) carried no
  manifest/artefact packaging defect; the retained Task-5-style abort escape
  hatch in d-plan Step 2 was never needed.
- **T170/T183 perms-ceiling self-cleanup.** The laydown's exact-perms pass
  clamped the 5 pre-existing floor-drift files (agent `.md` 0600→0444 and the
  helper scripts 0700→0500) back to the recorded ceiling *during* the update —
  exactly the T183 "permission-drift fix-on-sight" behaviour — so `validate` was
  clean on the first run with no separate `fix-security` pass.
- **The biggest pre-identified risk did not fire — and failed *safe*.** The
  medium risk was the +260-line settings-merge injecting PreToolUse hooks into
  `.claude/settings.json`. In practice the merge added **0 hook entries**: the
  hook files are on-disk but unregistered, so the harness never executes them.
  That is strictly safer than the planned "registered but off" and still meets
  a-plan SC3.

## What Could Be Improved
- **The security-review cap still fires on every CWF self-upgrade.** As in Task
  9, `security-review-changeset` exited `2` (1302 then 1305 production lines >
  the 500 cap). The `implementation-guide/**` exclude that Task 9's retrospective
  recommended **is now configured** (and correctly discounts this task's planning
  docs), but the vendored `.cwf/` subtree delta alone still exceeds 500.
  *Resolution this time*: rather than record `**State**: error` and move on (Task
  9's path), the cap was raised once per phase (`--max-lines=5000`, no persistent
  config change) and the `cwf-security-reviewer-changeset` subagent actually
  reviewed the full delta → `no findings` both phases. This is a better record —
  an actual review of the new hook/Perl surface — but it required a manual
  decision each phase. A standing `security.review.max-lines-exclude-paths` entry
  for `.cwf/**` (vendored upstream code, reviewed upstream) would let the cap
  measure only genuine consumer surface and remove the recurring manual step.
- **The plan over-anticipated the settings.json change.** d-plan invested
  several mitigations (hook command-target verification, settings-merge abort
  branch, post-revert JSON re-parse) in a settings.json mutation that did not
  occur. Not wasted — they were correct contingencies — but a quick
  "does this project enable sandbox?" check up front would have predicted the
  no-op merge and right-sized the plan.

## Key Learnings
### Technical Insights
- **The "which copy writes the decisive state" rule, applied with the opposite
  result.** Same asymmetry as Task 9 (the outer/currently-installed binary is the
  authoritative writer of post-laydown state), but here the outer binary is
  *post*-T175, so the conclusion inverts to the commit form. The rule is
  direction-agnostic; the answer depends entirely on which binary runs the write.
- **Opt-in hooks ship as unregistered files, not as registered-but-disabled
  entries.** The T179/T180 sandbox hooks are laid down on-disk but only wired
  into `settings.json` when the project enables sandbox. "Inert by default"
  understated it: with sandbox off, the hooks are not even present in the harness
  hook list. Verifying *registration state*, not just a behaviour knob, is the
  right check.
- **A vendored-subtree upgrade is content-diffed, not history-diffed** (confirmed
  again). Despite the remove-then-re-add (9 laydown commits incl. `Squashed …`
  markers), `git diff baseline..HEAD` collapses to the real source delta, so the
  retrospective squash is safe.

### Process Learnings
- **Following on-disk source over stale skill text was load-bearing for T182.**
  The exec SKILL instructions in context at exec time were the pre-upgrade
  v1.1.177 form (`--phase=`, capture-stdout). The just-laid-down v1.1.183 helper
  uses `--wf-step=` and self-manages a `.out` file. Reading the freshly-installed
  `security-review.md` + helper `--help` and following *those* (per the
  instruction-priority rule: source over summary) was the difference between a
  working review and a contract mismatch.

### Risk Mitigation Strategies
- Front-loading the success-criterion derivation (the SHA flip) into the plan
  turned exec into a confirmation pass — the same payoff as Task 9.
- The precondition gate (pre-upgrade HEAD `e2465841` recorded before the update)
  made the revert path concrete; it was available but unneeded.

## Recommendations
### Process Improvements
- **The `.cwf/**` cap-exclude decision now has a third data point** (already
  tracked in BACKLOG "Configure security.review.test-paths to exclude
  upstream-shipped CWF directories", from Task 5, reinforced by Task 9 — no new
  item created, the existing one is enriched). Task 14 *observed* the outcome the
  Task-9 caveat predicted: with the cap raised, the subagent reviewed the entire
  vendored delta → `no findings` at ~95–100k tokens/phase. The trade-off stands
  unresolved — for *pure* upgrades the cheap deterministic `cwf-manage validate`
  gate may beat a full subagent pass over upstream-vetted code; reserve the
  exclude for *mixed* tasks. Decision deferred to that backlog item.

### Tool and Technique Recommendations
- For self-upgrading tools, keep reading the *installed* binary's decisive
  writer by sub/function name (not line number, which drifts) to pin
  post-upgrade state.

### Future Work
- No new follow-up tasks. The one candidate (cap-exclude for `.cwf/**`) is
  already an open BACKLOG item (Task 5/9), enriched with Task 14's data point.

## Status
**Status**: Finished
**Next Action**: Task complete — suggest merge to local-main
**Blockers**: None identified
**Completion Date**: 2026-06-07
**Sign-off**: Matt Keenan (claude@mattkeenan.net)

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Archived Materials
- Planning: a-task-plan.md, d-implementation-plan.md, e-testing-plan.md
- Execution: f-implementation-exec.md (commit 4745f9c), g-testing-exec.md
  (commit 9aeb47c)
- Upgrade laydown: subtree squash/add commits + authoritative `.cwf/version`
  write (`cwf_sha=faf92479fac564f241ce10afb8ec00c986ad37f1`)
- Precedent: implementation-guide/9-chore-upgrade-cwf-to-v1-1-177-via-cwf-manage-update/
