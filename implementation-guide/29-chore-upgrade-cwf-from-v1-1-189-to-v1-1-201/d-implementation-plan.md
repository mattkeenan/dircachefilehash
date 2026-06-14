# upgrade CwF from v1.1.189 to v1.1.201 - Implementation Plan
**Task**: 29 (chore)

## Task Reference
- **Task ID**: internal-29
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/29-upgrade-cwf-from-v1-1-189-to-v1-1-201
- **Template Version**: 2.1

## Goal
Lay down CWF v1.1.201 over the current v1.1.189 install using the existing
read-tree mechanism, producing a single non-merge commit, with `.cwf/version`
and the install manifest correctly updated and validated.

## Workflow
Patterns first → verify mechanism → run upgrade → reconcile inter-repo artefacts → validate → commit

## Mechanism (verified against source)
The upgrade is driven by the **installed** `.cwf/scripts/cwf-manage` (v1.1.189),
invoked as `cwf-manage update v1.1.201`. Verified in `cmd_update`:
- `cwf_method=read-tree` already recorded → **no subtree→read-tree migration**, no
  `run_detect_merges`, laydown is **merge-free** (single-parent commit only).
- Clones `cwf_source` (`file:///home/matt/repo/coding-with-files`), resolves tag
  `v1.1.201` (present locally), checks it out, and **delegates laydown to the
  target ref's `install.bash`** with `CWF_FORCE=1 CWF_METHOD=read-tree
  CWF_SOURCE=file://<clone> CWF_REF=<sha>`. So v1.1.201's own laydown governs.
- Then: `run_apply_artefacts` (.cwf-rules/, .gitignore, CLAUDE.md preamble,
  .claude/rules/ symlinks), `run_settings_merge` (.claude/settings.json Bash
  allowlist + Stop hooks), `apply_exact_perms_or_die`, authoritative
  `write_version_file` (re-pins cwf_source, cwf_sha, manifest sha).

### Self-block check (Task 191) — resolved, no action needed
v1.1.189's `check_clean_tree` does not exclude `.cwf/.update.lock` (the fix is in
201). **Verified** this repo's `.gitignore:39` already lists `.cwf/.update.lock`
and git reports it ignored, so 189's clean-tree check will not see its own lock as
dirty. The upgrade will not self-block.

## Files Modified (by the upgrade tool — not hand-edited)
### Primary (CWF-internal — out of security-review scope per a-task-plan)
- `.cwf/**` — scripts, docs, templates, skills relaid by target install.bash
- `.cwf/version` — `cwf_version`/`cwf_ref`/`cwf_sha`/`cwf_installed`/manifest sha → v1.1.201

### Supporting (inter-repo integration surface — IN security-review scope)
- `.claude/settings.json` — merged Bash allowlist + Stop hooks (`run_settings_merge`)
- `.gitignore` — CWF-managed lines (`run_apply_artefacts`)
- `CLAUDE.md` — CWF preamble block (`run_apply_artefacts`)
- `.claude/rules/` (and `.cwf-rules/`, skill/agent symlinks) — symlink refresh

## Implementation Steps
### Step 1: Pre-flight
- [ ] Confirm on branch `chore/29-...`; record current `HEAD` sha as the
      **recovery anchor** (see Recovery below)
- [ ] Clean-tree check matching the tool's own gate (it aborts on any of these,
      not just `.cwf`): `git status -- .cwf .cwf-rules .cwf-agents .cwf-skills`
      must be empty
- [ ] **Pre-flight validate** (catches pre-existing manifest tampering before the
      upgrade, which `cmd_update`'s `validate_install_manifest_sha` would abort
      on anyway): `.cwf/scripts/cwf-manage validate` → OK
- [ ] Record current state: `.cwf/version` (v1.1.189, sha 6af636e…), and snapshot
      `.claude/settings.json` + `.gitignore` + `CLAUDE.md` for before/after diff
- [ ] Confirm the source repo has tag `v1.1.201`:
      `git --git-dir=/home/matt/repo/coding-with-files/.git tag -l v1.1.201`
      (cross-repo query — `--git-dir` form, not `git -C`, per repo convention)

### Step 2: Run the upgrade
- [ ] From repo root: `.cwf/scripts/cwf-manage update v1.1.201`
- [ ] Capture full stdout/stderr (logged for testing-exec evidence)
- [ ] If it dies after laydown begins (`update_in_progress`), follow Recovery —
      do not commit a partial install

### Step 3: Reconcile inter-repo artefacts (the security-review surface)
- [ ] Diff `.claude/settings.json` before/after. Expect a **migration, not purely
      additive** change:
  - **Task 195 prune+reshape**: the legacy `PreToolUse` group whose
    `matcher == "UserPromptSubmit"` (current `settings.json:6–16`, `cat
    .cwf/rules-inject.txt`) is **removed** and re-registered as a proper
    `UserPromptSubmit` hook. **Assert** the rules-inject command is byte-identical
    before vs after (it is a compile-time constant — no external data on the
    hook-command path).
  - **Task 201 new hook**: a new `PreToolUse` / matcher `Bash` group registering
    `.cwf/scripts/hooks/pretooluse-bash-tool-check`, which then intercepts **every
    Bash tool call** in this repo. Record its risk posture as a deliberate
    accepted decision: it is **fail-open** (any error/odd config → allow, never
    bricks Bash), **ships inert** (zero active rules), and drops checked-in `perl`
    rules before compile (only `.cwf/tool-check/bash/settings.local.json`, which is
    gitignored, can carry live rules). Confirm no unexpected commands beyond these.
  - Any Bash-allowlist / Stop-hook additions: confirm additive and expected.
- [ ] Diff `.gitignore` and `CLAUDE.md` preamble — confirm CWF-managed region only
- [ ] Confirm no new merge commit: `git log --merges <baseline>..HEAD` empty
- [ ] Confirm working tree has no surprise files
      (`git status --untracked-files=all`). **Expected** additions only:
      relaid `.cwf/**`, refreshed skill/agent/rule symlinks, the new
      `pretooluse-bash-tool-check` hook; the ignored `.cwf/.update.lock` may
      transiently appear but is gitignored. Anything outside this set is a fail.

### Step 4: Validate
- [ ] `.cwf/scripts/cwf-manage validate` → OK (manifest + perms)
- [ ] `/cwf-security-check` → clean
- [ ] `.cwf/version` shows `cwf_version=v1.1.201`
- [ ] Smoke a representative helper (e.g. `cwf-status`, and the
      security-review-changeset helper against the task baseline) → runs clean

### Step 5: Commit
- [ ] Single non-merge commit staging the upgrade changeset (`.cwf/**`,
      `.claude/settings.json`, `.gitignore`, `CLAUDE.md`, symlinks) +
      `d-implementation-plan.md` via the normal checkpoint flow

## Recovery (mid-laydown failure)
`cmd_update` warns that failures after `update_in_progress=1` may leave a partial
install. If the upgrade dies mid-laydown:
1. `rm -f .cwf/.update.lock` (clear any stale lock)
2. Restore the tracked tree to the recovery anchor:
   `git checkout -- .cwf .cwf-rules .cwf-agents .cwf-skills .claude/settings.json .gitignore CLAUDE.md`
   (or `git reset --hard <recorded HEAD>` if the working tree is the anchor)
3. Remove any untracked files the partial laydown left, then re-run from Step 1.

## Code Changes
No hand-written code. All changes are produced by `cwf-manage update`. The "change"
under review is the resulting diff, evaluated in testing/security phases.

## Test Coverage
**See e-testing-plan.md for complete test plan**

## Validation Criteria
**See e-testing-plan.md for validation criteria and test results**

## Scope Completion
Single discrete upgrade — no descoping anticipated. The full changeset (CWF tree +
inter-repo artefacts) lands in this task; nothing deferred.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Plan executed verbatim in `f-implementation-exec.md` (commit `682a2615`): pre-flight,
read-tree laydown, inter-repo reconcile, validate. No deviations affecting outcome.

## Lessons Learned
The snapshot-then-diff approach for `.claude/settings.json` / `.gitignore` /
`CLAUDE.md` (captured to a 0700 scratch dir pre-laydown) made the post-laydown
reconcile a mechanical before/after diff — the right standard for any CWF version bump.
