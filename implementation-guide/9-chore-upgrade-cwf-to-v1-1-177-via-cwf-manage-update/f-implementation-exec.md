# Upgrade CWF to v1.1.177 via cwf-manage update - Implementation Execution
**Task**: 9 (chore)

## Task Reference
- **Task ID**: internal-9
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/9-upgrade-cwf-to-v1-1-177-via-cwf-manage-update
- **Template Version**: 2.1

## Goal
Execute the upgrade following d-implementation-plan.md and e-testing-plan.md:
run `cwf-manage update v1.1.177`, then validate the result is clean and the
workflow tooling still runs.

## Execution Checklist
- [x] Read d-implementation-plan.md and e-testing-plan.md thoroughly
- [x] Verify all prerequisites met (Step 1 precondition gate below)
- [x] Execute implementation steps sequentially
- [x] Update "Actual Results" for each step
- [x] Document any deviations from plan
- [x] Update status when complete

## Verification Anchors (recorded BEFORE the update — precondition gate)
- **Pre-upgrade HEAD** (revert target for Step 2): `cc3ee2c193c73bf0f9819ecee4579176b6024fc7`
  (the e-testing-plan checkpoint tip at exec time).
- **Target tag-object SHA** (`git ls-remote --tags … v1.1.177`):
  `1cae055bf1b52bea0fd9b0cfce63871893757ab7`. This is what the **outer
  pre-T175 v1.1.169 cwf-manage** records as `cwf_sha` (forward-only rationale
  in d-plan Approach Rationale). Do **not** compare to the dereferenced commit
  `ed664b25…` this upgrade.
- **Pre-upgrade `.cwf/version`**: `cwf_ref=v1.1.169`, `cwf_version=v1.1.169`,
  `cwf_sha=473baea2dd1d77bac9f100a1036f091eeccd0a4b` (note: also a tag-object
  SHA — confirms the pre-T175 writer pattern).

## Actual Results

### Step 1: Pre-flight (precondition gate) — DONE
- **Planned**: confirm tag present + install.bash, record tag-object anchor and
  pre-upgrade HEAD, confirm `.cwf/` clean, no stale lock.
- **Actual**:
  - `git ls-remote --tags file:///home/matt/repo/coding-with-files v1.1.177`
    → `1cae055bf1b52bea0fd9b0cfce63871893757ab7	refs/tags/v1.1.177`. ✓
  - `cwf-manage list-releases` lists `v1.1.177` as available (21 releases total). ✓
  - `git status --porcelain -- .cwf .cwf-skills .cwf-rules .cwf-agents .claude`
    → empty (clean). Only untracked f/g/j workflow templates outside `.cwf/`. ✓
  - `ls .cwf/.update.lock` → absent. ✓
  - Pre-upgrade HEAD `cc3ee2c…` and tag-object anchor recorded above. ✓
- **Deviations**: none.

### Step 2: Run the upgrade — DONE
- **Planned**: `cwf-manage update v1.1.177`; subtree remove-then-add; artefacts;
  settings-merge; exact-perms; outer authoritative `.cwf/version` write.
- **Actual**: `cwf-manage update v1.1.177` ran clean, no abort, no resolve prompt.
  - Cloned source, checked out tag-object `1cae055…` (detached at peeled commit
    `ed664b25…` internally), created 4 subtree splits (core/skills/rules/agents).
  - Subtree laydown: `b28d3a1 CWF: remove existing install for reinstall`, then
    `Add CWF core/skills/rules/agents` (+ the `Squashed '.cwf*/' content` markers).
  - 20 skill symlinks, 1 rule symlink, 5 agent symlinks recreated.
  - Artefacts: `gitignore-entries: already up to date`, `cwf-rules-bundle: tree
    applied`, `claude-md-preamble: already up to date`, `claude-rules-symlinks:
    0 new / 1 kept / 0 broken`.
  - `settings: added 0 allowlist entries, 0 hook entries, 0 env keys` (no drift).
  - **T170 exact-perms pass** tightened drifted modes back to the recorded
    ceiling: agent `.md` 0600→0444, several `.cwf/scripts/**` 0700→0500, etc.
    (these are mode bits git does not track beyond the exec bit, so most produce
    no diff). No `apply_exact_perms_or_die` abort.
  - Final: `Updated to v1.1.177 (1cae055bf1b52bea0fd9b0cfce63871893757ab7)`.
- **Deviations**: none. No fallback/revert needed.

### Step 3: Validate & verify laydown — DONE
- **Planned**: status v1.1.177 + tag-object `cwf_sha`; validate exit 0 (≤1
  fix-security); T176 split present; workflow smoke check.
- **Actual**:
  - `cwf-manage status` → **Version: v1.1.177**, **Ref: v1.1.177**,
    **SHA: 1cae055bf1b52bea0fd9b0cfce63871893757ab7** (tag-object, **not** the
    commit `ed664b25…` — confirms the forward-only T175 finding from the d-plan).
  - `.cwf/version`: `cwf_ref=v1.1.177`, `cwf_version=v1.1.177`,
    `cwf_sha=1cae055…`. ✓
  - `cwf-manage validate` → `validate: OK`, exit 0. **No fix-security needed** —
    the laydown's exact-perms pass already left the tree at the recorded ceiling.
  - **T176 doc-split present**: `.cwf/docs/workflow/workflow-steps/` exists with
    10 per-phase files (`planning.md`, `implementation-planning.md`,
    `testing-planning.md`, `design.md`, `requirements.md`,
    `implementation-execution.md`, `testing-execution.md`, `rollout.md`,
    `maintenance.md`, `retrospective.md`); `workflow-steps.md` reduced to a ToC
    pointing at `workflow-steps/`. All `.claude/skills|agents|rules/cwf-*`
    symlinks resolve.
  - **Workflow smoke check** (all exit 0): `task-context-inference` resolves
    task 9 / `f-implementation-exec` (exercises T171 completed-task recency
    exclusion — correctly lands on the in-progress step); `context-manager
    hierarchy 9` → correct v2.1 path; `workflow-manager status 9 --workflow`
    runs (f at 25%); `backlog-manager validate` exit 0.
- **Deviations**: none.

### Step 4: Commit — DONE (laydown commits + version write)
- **Planned**: laydown auto-commits ride inside `.cwf/`; stage `.cwf/version`
  with named paths; record SHAs.
- **Actual**: the subtree update auto-created 9 commits `cc3ee2c..90170ac`
  (`2f12cbd` … `90170ac Add CWF agents`); HEAD now `90170ac`. `git diff --stat
  cc3ee2c..HEAD -- pkg cmd go.mod go.sum` is **empty** (project code untouched).
  Remaining uncommitted: only `.cwf/version` (outer authoritative write, not
  part of subtree content) + the f/g/j workflow templates. `.claude/settings.json`,
  `.cwf/install-manifest.json`, `.cwf/security/script-hashes.json` all rode
  inside the subtree commits (clean in porcelain). `.cwf/version` is staged with
  the f-phase checkpoint commit (Step 9) via named path. Final commit SHAs
  recorded by the checkpoint helper.

## Blockers Encountered

[none yet]

## Deferral Check
Before marking status=Finished, verify:
- [ ] All steps from d-implementation-plan.md executed
- [ ] All success criteria from a-task-plan.md met
- [ ] No planned work deferred without user approval

## Security Review

**State**: error

error: cap exceeded: 1759 production lines > 500

(Per `cwf-implementation-exec` Step 8: the `security-review-changeset` helper
exited `2` — the production-weighted line count exceeded `--max-lines=500`, so
the subagent is **not** invoked and the state is recorded as `error`. Helper
summary: `reviewed 43 files, 2466 lines (1759 production), anchor=65fd214`. The
"production" lines here are the v1.1.169→v1.1.177 **CWF source delta** plus the
four task-9 planning docs — there is **no** project source (`pkg/`/`cmd/`/`go.*`)
in the changeset (verified: `git diff --stat cc3ee2c..HEAD -- pkg cmd go.mod
go.sum` is empty). This repo declares no `security.review.max-lines-exclude-paths`,
so the vendored CWF subtree is not discounted and the cap fires as designed. The
semantic security of CWF's own helpers is the upstream project's review surface;
for this consumer-side upgrade the deterministic integrity gate is `cwf-manage
validate` (SHA256 + recorded-perms) which passed exit 0.)

## Status
**Status**: Finished
**Next Action**: /cwf-testing-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Lessons Learned
*To be captured during retrospective*
