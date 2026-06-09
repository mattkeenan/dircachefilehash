# Upgrade CwF to v1.1.185 - Retrospective
**Task**: 14 (chore)

## Task Reference
- **Task ID**: internal-14
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/14-upgrade-cwf-to-v1-1-185
- **Template Version**: 2.1
- **Retrospective Date**: 2026-06-09

## Executive Summary
- **Duration**: ~0.5 day (estimated: ~0.5 day, variance: ~0%).
- **Scope**: Upgrade the installed CWF subtree v1.1.183 → v1.1.185 **merge-free**,
  migrating the recorded laydown method `subtree`→`read-tree`, layered as a single
  linear commit on top of the preserved 183 landing (`700baba`). Final scope matched
  the plan; the only addition vs. the original "chore" framing was running the full
  b (requirements) and c (design) phases at the user's request — which proved
  load-bearing (see below).
- **Outcome**: Success. v1.1.185 pinned (`cwf_sha=6659c1c…`, commit form),
  `cwf_method=read-tree`, `validate: OK`, zero merge commits added, all 9 test
  cases pass, both exec-phase security reviews clean.

## Variance Analysis
### Time and Effort
- **Estimated**: ~0.5 day total (a-task-plan). Medium complexity (the
  laydown-method × linear-history interaction was the non-trivial part).
- **Actual**: ~0.5 day. Planning (a–e) was the larger share because the b-phase
  plan review uncovered the forward-only gap (below); exec (f/g) was fast — one
  driver invocation, exit 0 first attempt, no fallback needed.
- **Variance**: ~0%. The added effort of running b/c was offset by a frictionless
  exec.

### Scope Changes
- **Additions**: (1) Full b/c phases run (not in the default chore template) — the
  user explicitly requested task → reqs → design → impl → test. (2) The upgrade
  delivered a new `cwf-detect-merges` helper and one scoped settings allowlist
  entry — installer-owned, not hand-authored.
- **Removals**: None. The c-design Fallback (bare bootstrap installer) was retained
  as contingency but not needed.
- **Impact**: The b-phase review changed the mechanism (see Key Learnings) — without
  it the exec would have failed at the installer's `subtree` refusal.

### Quality Metrics
- **Test Coverage**: 6/6 acceptance criteria covered by executable assertions, all
  PASS; TC-7 fail-closed by inspection; TC-8 advisory recorded; TC-9 regression-free
  by construction (no Go change).
- **Defect Rate**: 0 defects in exec; 0 in either security review.
- **Performance**: One local `file://` clone + one update invocation; no hang. No
  `dcfh` Go change, so hashing/scan performance unaffected.

## What Went Well
- **The plan-review map/reduce earned its keep.** Three of four b-phase reviewers
  independently flagged that a plain `cwf-manage update v1.1.185` would fail —
  catching the forward-only gap *before* exec rather than at a broken install.
- **The user's "clone 185 and run its cwf-manage" suggestion was exactly right** and
  became the pinned mechanism; it did the full canonical migration (method rewrite +
  authoritative version write + detect-merges advisory) in one invocation.
- **Merge-free landing achieved as designed.** read-tree staged the four `.cwf*`
  prefixes with no commit; our single consumer commit is single-parent. `git log
  --merges 700baba..HEAD` stayed empty throughout.
- **Fail-closed ordering held.** Driver exited 0; had it failed mid-laydown,
  `cwf_method` would have remained `subtree` (verified by the security review's
  reading of the cmd_update ordering).
- **Exec was frictionless** — perms self-clamped (44 chmods in the installer),
  `validate: OK` on first run, no `fix-security` needed.

## What Could Be Improved
- **`workflow-manager status` shows 25% for a fully-complete chore** because b/c are
  outside the chore template's tracked phase set even though the files exist and are
  Finished. Cosmetic, but it reads as "incomplete" — a recurring confusion when a
  chore is run with extra phases.
- **Stale scratch artefacts** from the prior 183 round lingered in the shared
  per-task `/tmp` dir (`security-review-output-testing-exec.out` still described the
  177→183 upgrade), tripping a "read before write" on overwrite. Minor, but a clean
  scratch dir at task start would avoid it.

## Key Learnings
### Technical Insights
- **CWF's forward-only upgrade limitation is real and bites at exactly the predicted
  point.** The *installed* (old) `cwf-manage` reads `cwf_method=subtree` and passes
  `CWF_METHOD=subtree`, which the *target* 185 `install.bash` hard-refuses
  (`install.bash:82-83`). The `subtree`→`read-tree` translation lives only in 185's
  `cmd_update`. Driving with a throwaway 185 checkout (FindBin loads 185 libs while
  `$git_root` resolves to this repo via cwd) is the documented recovery and worked
  verbatim.
- **read-tree is genuinely merge-free**: `git fetch` + `git read-tree --prefix`
  stages into the index; the consumer makes one normal single-parent commit. This is
  the root-cause fix v1.1.185 ships ("Replace git-subtree with merge-free read-tree
  laydown") for the [[project_cwf_subtree_merge_commits]] bug.
- The 183→185 source delta is small (4 files: `cwf-manage`, new `cwf-detect-merges`,
  `Backlog.pm`, `script-hashes.json`); read-tree re-staging all four prefixes is the
  mechanism, not a sign of a wide change.

### Process Learnings
- **Running b/c on a "chore" was the right call here.** The default chore template
  skips them, but this chore had a non-obvious mechanism; the requirements review is
  precisely what surfaced the forward-only gap. Chore-type defaults are a floor, not
  a ceiling — escalate phases when the mechanism is uncertain.
- Estimation was accurate (~0.5 day) because the risk was correctly identified up
  front and the design pinned exact commands.

### Risk Mitigation Strategies
- **R1 (185 installer still merges under subtree)** never materialised — the design's
  source-reading confirmed read-tree ahead of exec, and the ff-only linear landing
  is a backstop regardless.
- The branch-off-`700baba` isolation meant the whole task was abandonable with zero
  impact on local-main at any point.

## Recommendations
### Process Improvements
- When a chore's *mechanism* (not just its surface) is uncertain, run the
  requirements + design phases even though the chore template omits them. The cost
  is small; the forward-only catch paid for it outright.

### Tool and Technique Recommendations
- The "materialise the target-version tooling and drive the upgrade with it" pattern
  is the canonical CWF forward-only recovery — reuse it for any future cross-version
  CWF upgrade that changes the laydown method.

### Future Work (recommendations only — not scheduled here)
- **Parked `.githooks` merge-blocking chore**: now overlaps 185's own
  `cwf-detect-merges` helper; reconcile the two when that chore is picked up.
- **Pre-Task-1 subtree-install merges** (`103537c`/`28cfb50`/`a2c7635`/`75e3ae4`):
  the detect-merges advisory flags these 4; the re-linearisation decision remains
  the user's, out of scope here.

## Status
**Status**: Finished
**Next Action**: Task complete — suggest ff-only merge to local-main
**Blockers**: None identified
**Completion Date**: 2026-06-09
**Sign-off**: Matt Keenan (with Claude Code)

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Archived Materials
- Planning: a-task-plan.md, b-requirements-plan.md, c-design-plan.md,
  d-implementation-plan.md, e-testing-plan.md
- Execution: f-implementation-exec.md (commit `de9d293`),
  g-testing-exec.md (commit `444de3f`)
- Baseline: `700baba` (the preserved v1.1.183 landing)
- Upgrade source: `file:///home/matt/repo/coding-with-files` tag `v1.1.185`
  (commit `6659c1cca72ef033d92546fcd9d42a0f4d817dd9`)
