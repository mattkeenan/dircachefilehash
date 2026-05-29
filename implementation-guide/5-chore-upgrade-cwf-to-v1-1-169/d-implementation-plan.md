# upgrade CWF to v1.1.169 - Implementation Plan
**Task**: 5 (chore)

## Task Reference
- **Task ID**: internal-5
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/5-upgrade-cwf-to-v1-1-169
- **Template Version**: 2.1

## Goal
Upgrade the CWF subtree install from v1.1.155 to v1.1.169 via `cwf-manage update`, then
validate the result is clean and the workflow tooling still runs.

## Workflow
Operational upgrade (no source edits). Pre-flight → run updater → validate → commit.

## Approach Rationale
For the **subtree** method, the installed `cwf-manage update` clones the source, then
**delegates laydown to the target ref's `scripts/install.bash`** (`cwf-manage:406-427`).
So the heavy lifting (subtree remove-then-add under `CWF_FORCE`, skill/rule/agent
relink) runs from v1.1.169's installer — the stale v1.1.155 wrapper resolves the ref,
holds the update lock, then runs `cwf-apply-artefacts`, the settings-merge, the
exact-perms pass, and the authoritative `.cwf/version` write.

Pinning the **explicit tag** `v1.1.169` sidesteps the v1.1.155 FR1 version-recording
weakness: `cmd_update` *unconditionally* writes the resolved ref string into
`cwf_version`, valid only when the ref is itself a semver tag (Task 159 later fixed
this via `git_describe_version`).

**Crucially for this attempt**: v1.1.169 inherits Task 167 (v1.1.167), which removed
the `rules-inject` artefact from the manifest. That artefact was the failure mode of
our v1.1.163 attempt — manifest source was a 0-byte placeholder, on-disk via the
subtree was a 331-byte populated file, and `apply_replace` aborted on the 3-way
disagreement under no-TTY. With Task 167's removal, v1.1.155's `cwf-apply-artefacts`
sees no `rules-inject` entry in the new manifest after install.bash runs.

## Files to Modify
All of these are rewritten **by the updater**, not hand-edited:
### Primary (subtree laydown)
- `.cwf/` — core scripts, docs, templates, Perl libs, security hashes → replaced at v1.1.169
- `.cwf-skills/cwf-*` — skill definitions → replaced
- `.cwf-rules/` — rules → reconciled via `cwf-apply-artefacts`
- `.cwf-agents/cwf-*` and `.claude/agents/cwf-*` — agent defs, incl. Task 162's `cwf-security-reviewer-changeset` rewrite (trailing ` ```cwf-review ` verdict block)

### Supporting
- `.claude/skills/cwf-*` — symlinks recreated by install.bash (handles renames)
- `.claude/settings.json` — settings-merge registers the new **SubagentStop** hook (Task 162)
- `.cwf/version`, `.cwf/install-manifest.json`, `.cwf/security/script-hashes.json` — version + integrity metadata refreshed

## Behavioural deltas v1.1.156→v1.1.169 (from CHANGELOG review, 14 tasks)
- **T158**: install.bash force-reinstall + post_install settings-merge fixes (in-band).
- **T159**: `git_describe_version` so `cwf_version` records semver not ref; forward-only — lands for the *next* update. Sidestepped here by pinning the tag.
- **T161**: copy-method convergence + `cwf-check-tree-symlinks` helper (copy-only; no effect on subtree).
- **T162**: security-reviewer verdict deterministic ` ```cwf-review ` block + new `security-review-classify` helper + `subagentstop-security-verdict-guard` hook registered in settings.json. Changes how this repo's exec-phase security reviews classify — verify the hook lands.
- **T163**: version helpers skip cleanly on subtask numbers (only matters at a subtask retrospective; not this top-level task).
- **T164**: hierarchy-aware consistency validation in `cwf-manage validate`. May surface new violation classes — read output verbatim.
- **T165**: template-reference linter wired into `cwf-manage validate`. Will run on our `implementation-guide/5-.../`. Excludes `implementation-guide/` per design — should not flag anything in our task dir.
- **T166**: subtask-aware `task-context-inference`. Top-level task here; helpful as regression check.
- **T167**: **fixes our v1.1.163 blocker** — drops `rules-inject` from the manifest entirely; subtree becomes the sole distribution mechanism for `.cwf/rules-inject.txt`. Adds INV-1/INV-2 manifest invariants upstream (regression tests in CWF source, not run here).
- **T168**: `security-review-changeset` cap now weights production code rather than raw diff lines. Informational for this task (docs-only chore).
- **T169**: README sync (docs-only upstream; no consumer effect).

## Implementation Steps
### Step 1: Pre-flight
- [x] Task pivoted from v1.1.163 → v1.1.169; dir + branch renamed; v1.1.163 laydown discarded (`git restore --staged .` + `git checkout -- .cwf .cwf-skills .cwf-rules .cwf-agents .claude` + `git checkout -- .cwf/version` + drop 3 untracked v1.1.163-only helpers); tree back at v1.1.155 clean
- [x] Tag `v1.1.169` present in source `/home/matt/repo/coding-with-files`; `scripts/install.bash` present at that tag
- [x] CHANGELOG v1.1.156→169 reviewed for breaking changes (see deltas above)
- [ ] Record the source tag's dereferenced commit (`0764380e60a6c1fb3788406942dfab7ae13bb585`) for post-update commit verification
- [ ] Record the source tag's annotated-object SHA (`473baea2dd1d77bac9f100a1036f091eeccd0a4b`) — this is what `cwf-manage`'s `resolve_sha` (`git rev-parse v1.1.169`) writes into `.cwf/version`'s `cwf_sha` field, **not** the dereferenced commit (lesson from the v1.1.163 attempt)
- [ ] **Precondition gate**: capture `git rev-parse HEAD` into `f-implementation-exec.md` BEFORE invoking `cwf-manage update`. The revert path in Step 2 depends on this SHA; if it is not recorded, do not proceed with Step 2. The expected value is the d/e checkpoint tip at exec time (do not hardcode)
- [ ] Verify no stale update lock: `ls -la .cwf/.update.lock 2>/dev/null` should be absent; if present from a prior aborted run, confirm no `cwf-manage` process is running and remove

### Step 2: Run the upgrade (primary path)
- [ ] `.cwf/scripts/cwf-manage update v1.1.169`
- [ ] Expect: update lock (`.cwf/.update.lock`) acquired; source cloned; target install.bash performs subtree remove-then-add (creates ~8 auto squash commits + .cwf-rules/.cwf-skills/.cwf-agents adds); `cwf-apply-artefacts` runs on the new manifest (no `rules-inject` entry — see T167); settings-merge runs; exact-perms pass clears the 10 pre-existing 0600→0444 drifts; authoritative `.cwf/version` written
- [ ] **If `cwf-apply-artefacts` aborts on a different artefact** (e.g. `claude-md-preamble`, `gitignore-entries`, `cwf-rules-bundle`, `regenerate-symlinks`): the env var `CWF_UPGRADE_RESOLVE` is **per-invocation, not per-artefact** (`cwf-apply-artefacts:159-163,245-275`) — there is no fine-grained knob. The only mechanisms are: (a) re-run the *entire* update with `CWF_UPGRADE_RESOLVE=keep` (preserves all consumer modifications) or `=new` (overwrites with manifest version, wholesale), or (b) manually reconcile the conflicting file on-disk to match either side, then re-run. Pick (a) keep when the consumer file is load-bearing; (b) when an in-place merge is required
- [ ] **If `apply_exact_perms_or_die` aborts**: the tree is at v1.1.169 but `.cwf/version` still reads v1.1.155 — a half-applied state. Detect deterministically by `grep ^cwf_version .cwf/version` (should still read `v1.1.155`) and confirm a known-v1.1.169-only file is present (e.g. `test -f .cwf/scripts/command-helpers/security-review-classify`). Do **not** fix-forward — `fix-security` is additive-only and cannot complete the version pin. Treat as failed laydown and revert
- [ ] **Fallback / revert on failure**: (i) `git reset --soft <pre-upgrade-HEAD>` (recorded in Step 1); (ii) `git restore --staged . && git checkout -- .cwf .cwf-skills .cwf-rules .cwf-agents .claude && git checkout -- .cwf/version`; (iii) preview orphan untracked files with `git clean -fdx --dry-run -- .cwf .cwf-skills .cwf-rules .cwf-agents .claude/skills .claude/agents`, then run without `--dry-run` to remove them (install.bash commonly creates new helper scripts, symlinks, or hook entries that `checkout` leaves behind because they were never in v1.1.155's HEAD); (iv) if `.cwf/.update.lock` remains, `ls -la .cwf/.update.lock` to confirm no live process, then `rm .cwf/.update.lock`. (Soft over hard so install.bash's commits are reachable via reflog for inspection; the working-tree cleanup is the minimal mechanical revert — same pattern that recovered the v1.1.163 attempt.)

### Step 3: Validate & verify laydown
The laydown (install.bash + `cwf-apply-artefacts` + `apply_exact_perms_or_die`) already
recreates the skill/rule/agent symlinks and sets exact perms; `cwf-manage validate` is the
authoritative check. Only recreate symlinks by hand if validate reports a dangling link.
- [ ] `cwf-manage status` → **Version: v1.1.169**; `cwf_sha` equals `473baea2dd1d77bac9f100a1036f091eeccd0a4b` (the annotated-tag object recorded in Step 1, **not** the dereferenced commit `0764380…`). Note `cwf_ref` flips `HEAD`→`v1.1.169` — expected, not drift
- [ ] `cwf-manage validate` → exit 0. Validate is read-only (it reports, it does not chmod): what clears the **pre-existing** 0600→0444 drift on the 10 flagged files is the laydown replacing them followed by the exact-perms pass resetting them to 0444. Note that `.cwf/templates/install/rules-inject.txt` (one of the 10) does not exist in v1.1.169 (removed by upstream T167) — it disappears from the validate report, it is not "fixed". T164 may surface new hierarchy-consistency violation classes; T165's template-ref linter runs but excludes `implementation-guide/`. If validate flags fixable perms only, run `cwf-manage fix-security` and re-validate
- [ ] `.claude/settings.json` contains the new SubagentStop hook (Task 162); the hook target `.cwf/scripts/hooks/subagentstop-security-verdict-guard` exists and is executable (`test -x`); every `.claude/skills/cwf-*` and `.claude/agents/cwf-*` resolves
- [ ] Smoke-test helpers used by tasks 1–4: `workflow-manager status`, `backlog-manager validate --all`, `task-context-inference`. **Pass/fail rubric**: non-zero exit on any helper = blocker; new informational warnings against historical content = expected (T164/T165 may add hierarchy or template-ref signal); new errors against this task's directory (`implementation-guide/5-chore-upgrade-cwf-to-v1-1-169/`) = blocker

### Step 4: Commit
- [ ] The subtree update auto-creates squash commits for the laydown — these include `.cwf/install-manifest.json` and `.cwf/security/script-hashes.json` because both live *inside* `.cwf/` and ride the subtree. The post-laydown writes left uncommitted are narrower: `.cwf/version` (authoritative write), `.claude/settings.json` (settings-merge), and any exact-perms `chmod`s (file-mode changes; git records them only if `core.fileMode=true`)
- [ ] Stage with **named paths**, not `git add -A`, to avoid sweeping in unrelated working-tree edits: `git add .cwf/version .claude/settings.json` (add `.cwf/install-manifest.json` and `.cwf/security/script-hashes.json` only if they show as modified after the subtree commits — they shouldn't)
- [ ] Make a single follow-up commit for the metadata; record the squash + metadata commit SHAs in `f-implementation-exec.md`

## Code Changes
N/A — operational upgrade driven by `cwf-manage`; no project source code is edited.

## Test Coverage
**See e-testing-plan.md for complete test plan**

## Validation Criteria
**See e-testing-plan.md for validation criteria and test results**

## Scope Completion
**IMPORTANT**: Complete all planned implementation before marking task Finished.

The upgrade is atomic in intent: a half-applied upgrade (laydown done, validate failing,
or wrong version recorded in `.cwf/version`) is not "done". If `validate` cannot be made
to pass cleanly, either repair via `fix-security` (perms only) or revert per Step 2 and
re-plan — do not mark Finished with a dirty validate.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
*To be filled upon completion*

## Lessons Learned
*To be captured during implementation*
