# Upgrade CWF to v1.1.177 via cwf-manage update - Implementation Plan
**Task**: 9 (chore)

## Task Reference
- **Task ID**: internal-9
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/9-upgrade-cwf-to-v1-1-177-via-cwf-manage-update
- **Baseline Commit**: 65fd21484eb25d83fa1d42dfdd315ee526599656
- **Template Version**: 2.1

## Goal
Upgrade the CWF subtree from v1.1.169 to v1.1.177 via `cwf-manage update v1.1.177`,
then validate the result is clean and the workflow tooling still runs.

## Workflow
Operational upgrade (no source edits). Pre-flight → run updater → validate → commit.

## Approach Rationale
For the **subtree** method, the installed `cwf-manage update` clones the source,
resolves the ref, holds the update lock, then **delegates laydown to the target
ref's `scripts/install.bash`** (verified: installed `cwf-manage` lines 458–489:
`system('bash', $installer)` with cwd = git root). The target installer does the
heavy lifting (subtree remove-then-add under force, skill/rule/agent relink,
`cwf-apply-artefacts`, settings-merge, exact-perms pass). Then the **outer**
cwf-manage performs the **authoritative `.cwf/version` write** (lines 502–510),
overwriting whatever base version file install.bash wrote.

**Critical consequence for verification — the `cwf_sha` value.** The authoritative
writer is the *currently-installed* cwf-manage (**v1.1.169, pre-T175**). Its
`resolve_sha` (line 209) is `git rev-parse $ref` with **no `^{commit}`**, so it
records the **annotated-tag object SHA** `1cae055bf1b52bea0fd9b0cfce63871893757ab7`,
**not** the dereferenced commit `ed664b25541f0ae35de09633fe5155c500a502bc`. T175
(which adds `^{commit}`) ships *in* v1.1.177 but is **forward-only**: it governs
the *next* upgrade, when v1.1.177's own cwf-manage is the writer. This is the same
forward-only pattern as T159 (`git_describe_version`). Do not "correct" the
expectation to the commit SHA for this task.

**No known laydown-blocking defect at v1.1.177.** The v1.1.163 `rules-inject`
artefact defect that aborted Task 5's first attempt was fixed by T167 and we are
already past it (installed at v1.1.169). The span 170→177 carries no analogous
manifest/artefact packaging defect (CHANGELOG review below). The Task-5 failure
escape hatch is retained in Step 2 regardless.

## Behavioural deltas v1.1.170→v1.1.177 (from CWF CHANGELOG, 8 tasks)
- **T170**: recorded permissions enforced as an **upper bound** (ceiling) in
  validate. May surface perm-ceiling violations on any consumer file whose
  on-disk mode exceeds its recorded value — read validate output verbatim;
  repair via `fix-security`, not manual chmod.
- **T171**: completed tasks excluded from the recency signal in
  `task-context-inference`. Regression check only — confirm it still resolves
  task 9 correctly post-upgrade.
- **T172**: "Adapt CWF to new Claude Code harness" (discovery) — docs/inventory;
  no consumer behaviour change expected.
- **T173**: show-toplevel sites audited for worktree-safety (helper internals
  routed through a worktree-safe resolver). Transparent to us (not in a
  worktree); informational.
- **T174**: `security-review-changeset` now reviews **all changed files**, not
  just CWF-internal + shebang scripts. **Consumer-affecting**: our *future*
  exec-phase security reviews (this task's f/g included, run under the
  newly-laid-down helper) will scope Go/project changes into the changeset, not
  just CWF files. For this docs-only chore the changeset is CWF-metadata only, so
  the immediate effect is small — but note the behaviour change.
- **T175**: `.cwf/version` records commit SHA not tag-object SHA — **forward-only**
  (see Approach Rationale). Does not change *this* upgrade's recorded `cwf_sha`.
- **T176**: `workflow-steps.md` split into `workflow-steps/{phase}.md`; the 8
  plan-phase SKILL Step-5 references repointed to the per-phase files. After
  laydown, `.cwf/docs/workflow/workflow-steps/` must exist and the installed
  `.claude/skills/cwf-*` must reference readable paths.
- **T177**: EnterWorktree/ExitWorktree grounding (discovery) — docs/backlog only;
  no code.

## Files to Modify
All rewritten **by the updater**, not hand-edited:
### Primary (subtree laydown, target v1.1.177)
- `.cwf/` — scripts, docs (incl. the new `docs/workflow/workflow-steps/` dir from
  T176), Perl libs, templates, security hashes → replaced at v1.1.177
- `.cwf-skills/cwf-*`, `.cwf-rules/`, `.cwf-agents/cwf-*`, `.claude/agents/cwf-*`
  — skill/rule/agent defs reconciled via `cwf-apply-artefacts`
### Supporting
- `.claude/skills/cwf-*` — symlinks recreated by install.bash (handles any
  add/rename from T176)
- `.claude/settings.json` — settings-merge (no new hook expected in this span;
  confirm no unexpected drift)
- `.cwf/version`, `.cwf/install-manifest.json`, `.cwf/security/script-hashes.json`
  — version + integrity metadata refreshed

## Implementation Steps
### Step 1: Pre-flight (precondition gate)
- [ ] Confirm tag `v1.1.177` present in source `/home/matt/repo/coding-with-files`
      and `scripts/install.bash` exists at that tag.
- [ ] Record the verification anchor in `f-implementation-exec.md`: the
      **tag-object** SHA `git rev-parse v1.1.177` =
      `1cae055bf1b52bea0fd9b0cfce63871893757ab7`, which is what the outer
      v1.1.169 cwf-manage records as `cwf_sha` (the forward-only commit-SHA
      rationale is in Approach Rationale — do not record/compare `ed664b25…`
      this upgrade).
- [ ] **Precondition gate**: capture `git rev-parse HEAD` into
      `f-implementation-exec.md` BEFORE invoking `cwf-manage update`. The Step-2
      revert path depends on this SHA; if it is not recorded, do not proceed. The
      expected value is the d/e checkpoint tip at exec time (do not hardcode).
- [ ] Confirm `.cwf/` working tree is **clean** (`cwf-manage update` refuses with
      uncommitted changes under `.cwf/`): `git status --porcelain -- .cwf .cwf-skills`
      empty. Commit/stash any stragglers first.
- [ ] Verify no stale lock: `ls -la .cwf/.update.lock 2>/dev/null` absent. Do
      **not** pre-emptively `rm` it on a process guess — the lock is a `flock`
      (`cwf-manage` `acquire_update_lock`), so a live holder makes the update
      refuse with an "in progress" error of its own accord. Only remove the lock
      file if the update refuses AND you have confirmed (e.g. `ps`) no
      `cwf-manage` is running.

### Step 2: Run the upgrade (primary path)
- [ ] `.cwf/scripts/cwf-manage update v1.1.177`
- [ ] Expect: update lock acquired; source cloned; target (v1.1.177) install.bash
      performs subtree remove-then-add (several auto squash commits +
      `.cwf-rules/.cwf-skills/.cwf-agents` adds); `cwf-apply-artefacts` runs on
      the new manifest; settings-merge; exact-perms pass; outer cwf-manage writes
      authoritative `.cwf/version` (tag-object `cwf_sha`, `cwf_ref=v1.1.177`).
- [ ] **If `cwf-apply-artefacts` aborts on an artefact** (e.g. `claude-md-preamble`,
      `gitignore-entries`, `cwf-rules-bundle`, `regenerate-symlinks`):
      `CWF_UPGRADE_RESOLVE` is **per-invocation, not per-artefact**. Options:
      (a) re-run the *entire* update with `CWF_UPGRADE_RESOLVE=keep` (preserve
      consumer mods) or `=new` (take manifest version wholesale); (b) manually
      reconcile the conflicting file on-disk to match one side, then re-run. Pick
      (a) keep when the consumer file is load-bearing.
- [ ] **If `apply_exact_perms_or_die` aborts**: tree is at v1.1.177 but
      `.cwf/version` still reads v1.1.169 — half-applied. Detect:
      `grep ^cwf_version .cwf/version` still `v1.1.169` AND a known-v1.1.177-only
      marker present (e.g. `test -d .cwf/docs/workflow/workflow-steps`). Do **not**
      fix-forward (`fix-security` is additive/clamp-only and cannot complete the
      version pin). Treat as failed laydown and revert.
- [ ] **Fallback / revert on failure**: (i) `git reset --soft <pre-upgrade-HEAD>`
      (Step 1); (ii) `git restore --staged . && git checkout -- .cwf .cwf-skills
      .cwf-rules .cwf-agents .claude` (this single checkout restores
      `.cwf/version` too — it lives under `.cwf` — no separate invocation needed);
      (iii) preview orphans with `git clean -fdx --dry-run -- .cwf .cwf-skills
      .cwf-rules .cwf-agents .claude/skills .claude/agents`, **read the dry-run
      list and confirm it before** the live run — unlike the soft reset (reflog-
      recoverable), `git clean` deletion is **irreversible**; then run without
      `--dry-run` to remove install.bash-created files absent from the pre-upgrade
      HEAD (the T176 `workflow-steps/` dir is exactly this kind of new path);
      (iv) remove a stale `.cwf/.update.lock` only per the Step-1 flock rule.
      Soft (not hard) reset so install.bash commits stay reachable via reflog;
      the user has previously denied `git reset --hard`.

### Step 3: Validate & verify laydown
The laydown already recreates symlinks and sets exact perms; `cwf-manage validate`
is authoritative. Only recreate symlinks by hand if validate reports a dangling link.
- [ ] `cwf-manage status` → **Version: v1.1.177**; `cwf_ref` flips
      `HEAD`/`v1.1.169` → `v1.1.177` (expected, not drift); `cwf_sha` =
      `1cae055bf1b52bea0fd9b0cfce63871893757ab7` (tag-object, per Approach
      Rationale — **not** the commit `ed664b25…`).
- [ ] `cwf-manage validate` → exit 0. T170's perms-ceiling check may newly flag
      any file whose on-disk mode exceeds its recorded value; if validate reports
      **fixable perms only**, run `cwf-manage fix-security` **once** and
      re-validate; if validate is still dirty after that single pass (fix-security
      is clamp-only and idempotent, so one pass should suffice), treat it as a
      failed laydown and revert per Step 2 — do not loop fix-security. Read any
      new hierarchy/template-ref signal verbatim; new signal against *historical*
      content is informational, new errors against this task's dir
      (`implementation-guide/9-.../`) are a blocker.
- [ ] **T176 doc-split present**: `test -d .cwf/docs/workflow/workflow-steps` and
      it contains per-phase files (e.g. `planning.md`, `implementation-planning.md`);
      `workflow-steps.md` reduced to a ToC. Every `.claude/skills/cwf-*` and
      `.claude/agents/cwf-*` symlink resolves.
- [ ] **Workflow smoke check** (functional, not just file-exists):
      `task-context-inference` and `context-manager hierarchy 9` resolve task 9;
      `workflow-manager status 9 --workflow` runs; `backlog-manager validate`
      exit 0. Pass/fail rubric: non-zero exit on any helper = blocker; new
      informational warnings on historical content = expected.

### Step 4: Commit
- [ ] The subtree update auto-creates squash commits for the laydown (these carry
      `.cwf/install-manifest.json` and `.cwf/security/script-hashes.json`, which
      ride inside `.cwf/`). The post-laydown uncommitted writes are narrower:
      `.cwf/version` (authoritative write) and any settings-merge / exact-perms
      mode changes.
- [ ] Stage with **named paths**, not `git add -A`: `git add .cwf/version`
      (add `.claude/settings.json`, `.cwf/install-manifest.json`,
      `.cwf/security/script-hashes.json` only if they show modified after the
      subtree commits). Record the squash + metadata commit SHAs in
      `f-implementation-exec.md`.

## Code Changes
N/A — operational upgrade driven by `cwf-manage`; no project source code is edited.

## Test Coverage
**See e-testing-plan.md for complete test plan.**

## Validation Criteria
**See e-testing-plan.md for validation criteria and test results.**

## Scope Completion
**IMPORTANT**: Complete all planned implementation before marking task Finished.

The upgrade is atomic in intent: a half-applied upgrade (laydown done, validate
failing, or wrong version recorded in `.cwf/version`) is not "done". If `validate`
cannot be made to pass cleanly, either repair via `fix-security` (perms only) or
revert per Step 2 and re-plan — do not mark Finished with a dirty validate.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Plan executed verbatim, no deviations. Step 1 pre-flight: tag-object `1cae055…`
confirmed via `git ls-remote`, `.cwf/` clean, no lock, pre-upgrade HEAD `cc3ee2c`
recorded. Step 2: `cwf-manage update v1.1.177` ran clean — 9 subtree commits
(`cc3ee2c..90170ac`), 20 skill + 5 agent + 1 rule symlinks, settings-merge 0
entries, T170 exact-perms pass tightened drifted modes; no abort, no resolve
prompt, escape hatch unused. Step 3: status v1.1.177 / `cwf_sha`=tag-object
`1cae055…`, `validate: OK` (no fix-security needed), T176 split present, all
smoke checks exit 0. Step 4: `.cwf/version` staged by named path, committed with
the f-phase checkpoint `dba0116`. Full detail in f-implementation-exec.md.

## Lessons Learned
The Approach Rationale's `cwf_sha` forward-only derivation (cwf-manage lines
209/448/502–510) proved correct on the live run. The "no known laydown-blocking
defect at v1.1.177" assessment held — being past T167 mattered.
