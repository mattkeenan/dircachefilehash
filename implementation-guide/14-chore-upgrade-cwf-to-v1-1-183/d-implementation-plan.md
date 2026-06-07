# Upgrade CWF to v1.1.183 - Implementation Plan
**Task**: 14 (chore)

## Task Reference
- **Task ID**: internal-14
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/14-upgrade-cwf-to-v1-1-183
- **Template Version**: 2.1

## Goal
Upgrade the CWF subtree from v1.1.177 to v1.1.183 via `cwf-manage update
v1.1.183`, then validate the result is clean, the two newly-injected PreToolUse
hooks merge inertly into `.claude/settings.json`, and the workflow tooling still
runs.

## Workflow
Operational upgrade (no source edits). Pre-flight → run updater → validate → commit.

## Approach Rationale
For the **subtree** method, the installed `cwf-manage update` clones the source,
resolves the ref, holds the update lock, then **delegates laydown to the target
ref's `scripts/install.bash`** (installed `cwf-manage`, the `system('bash',
$installer)` call in `cmd_update`, cwd = git root). The target installer does the
heavy lifting (subtree remove-then-add under force, skill/rule/agent relink,
`run_apply_artefacts`, `run_settings_merge`, exact-perms pass). Then the **outer**
cwf-manage performs the **authoritative `.cwf/version` write** (the version-write
block in `cmd_update`), overwriting whatever base version file install.bash wrote.

**Critical consequence for verification — the `cwf_sha` value (the flip).** The
authoritative writer is the *currently-installed* cwf-manage (**v1.1.177,
post-T175**). Its `resolve_sha` sub is `git rev-parse $ref^{commit}` **with**
`^{commit}`, so it records the **dereferenced commit SHA**
`faf92479fac564f241ce10afb8ec00c986ad37f1`, **not** the annotated-tag object SHA
`1842f7b04e0819364529ae123f3365a69b56b99e`. The version-write block assigns
`$v{cwf_sha} = $sha` where `$sha = resolve_sha(...)` — so the commit form is what
lands. (Line numbers omitted deliberately: they drift between versions; verify by
sub name. As of writing `resolve_sha` is ~line 223–231 and the version write
~518–524.) **This is the exact inverse of Task 9**, which
recorded the tag-object SHA because *its* writer was the pre-T175 v1.1.169
binary. Do not copy Task 9's tag-object expectation; assert `faf92479…`.

**No known laydown-blocking defect at v1.1.183.** The v1.1.163 `rules-inject`
defect that aborted Task 5 was fixed by T167; we are well past it. The span
178→183 carries no analogous manifest/artefact packaging defect (changelog
review below). The Task-5 failure escape hatch is retained in Step 2 regardless.

## Behavioural deltas v1.1.178→v1.1.183 (from CWF CHANGELOG, 6 tasks)
- **T178**: "Integrate Claude Code sandboxing" (discovery) — docs/inventory; no
  consumer behaviour change.
- **T179**: Sandboxing substrate. Adds the **`sandbox` config block** (Config
  knobs), the R3 logging hook `pretooluse-sandbox-logging`, and the
  `cwf-claude-settings-merge` plumbing to register hooks. **Off by default**
  (`sandbox.enabled=false`).
- **T180**: **phase-scoped planning-write PreToolUse guard**. New hook
  `pretooluse-sandbox`… → `pretooluse-planning-write-guard` (matcher
  `Edit|Write`), guards crown jewels (`.cwf/`, `.claude/`); new
  `sandbox.planning-write-guard` enum (`off`|`observe`|`enforce`), **default
  `off` even with sandbox on**. Settings-merge matcher regex widened to a
  tool-name alternation. **Consumer-relevant but inert by default** — a crown-
  jewel Edit/Write is only denied when the knob is non-`off` AND not in an exec
  phase; our task-own writes under `implementation-guide/` are never crown
  jewels regardless.
- **T181**: guarded worktree process. Ships `worktree.baseRef: head` into the
  **committed `.claude/settings.json`** (so settings.json *will* change this
  upgrade, unlike Task 9), a convention doc, and an FR9 best-effort
  dangerous-allowlist scan folded into `cwf-claude-settings-merge`
  (warning-only, no JSON decode, cannot abort the merge).
- **T182**: `security-review-changeset` contract change. `--phase=` → **mandatory
  `--wf-step=<step>`**; `--max-lines` **defaults to 500**; the script
  **self-manages output** to `<scratch>/security-review-changeset-<wf-step>.out`
  and prints one confirmation line. The exec SKILLs + the
  `cwf-security-reviewer-changeset` agent are migrated by laydown.
  **Consumer-affecting for *this task's* f/g phases**, which run under the
  newly-laid-down helper — but the laid-down SKILLs call it correctly, so no
  manual action; just expect the new file-output form.
- **T183**: permission-drift repair (docs-only). Codifies a **fix-on-sight**
  rule: clamp perm drift via `cwf-manage fix-security` the moment `validate`
  flags it; never recompute a hash to clear a warning. No code, no new surface.
  Directly relevant — the 5 perms-floor violations seen at the a-phase checkpoint
  are exactly this class.

## Files to Modify
All rewritten **by the updater**, not hand-edited:
### Primary (subtree laydown, target v1.1.183)
- `.cwf/` — scripts, docs, Perl libs (incl. new `CWF::PlanningGuard` from T180),
  templates, security hashes → replaced at v1.1.183. New paths expected:
  `.cwf/scripts/hooks/pretooluse-planning-write-guard`,
  `.cwf/scripts/hooks/pretooluse-sandbox-logging`, `.cwf/docs/sandboxing.md`,
  `.cwf/docs/conventions/worktree-process.md`.
- `.cwf-skills/cwf-*`, `.cwf-rules/`, `.cwf-agents/cwf-*`, `.claude/agents/cwf-*`
  — skill/rule/agent defs reconciled via `run_apply_artefacts` (incl. the T182
  hardened `cwf-security-reviewer-changeset.md` agent).
### Supporting
- `.claude/skills/cwf-*` — symlinks recreated by install.bash (handles any
  add/rename, incl. the T182-updated exec SKILLs).
- `.claude/settings.json` — **settings-merge WILL modify this** (≠ Task 9):
  registers the two new PreToolUse hooks and adds `worktree.baseRef: head`.
  Verify it remains well-formed JSON and the hooks are inert by default.
- `.cwf/version`, `.cwf/install-manifest.json`, `.cwf/security/script-hashes.json`
  — version + integrity metadata refreshed.
- `implementation-guide/cwf-project.json` — upstream added a `sandbox` stanza to
  *its* template; check whether laydown adds it to *our* project config (it may
  not, as this file is project-owned). Absence must not enable a guard (defaults
  are off-on-absence).

## Implementation Steps
### Step 1: Pre-flight (precondition gate)
- [ ] Confirm tag `v1.1.183` present in source `/home/matt/repo/coding-with-files`
      and `scripts/install.bash` exists at that tag.
- [ ] Record the verification anchor in `f-implementation-exec.md`: the
      **commit** SHA `git rev-parse v1.1.183^{commit}` =
      `faf92479fac564f241ce10afb8ec00c986ad37f1`, which is what the outer
      v1.1.177 cwf-manage records as `cwf_sha` (the commit-SHA flip rationale is
      in Approach Rationale — do **not** record/compare the tag-object
      `1842f7b0…` this upgrade).
- [ ] **Precondition gate**: capture `git rev-parse HEAD` into
      `f-implementation-exec.md` BEFORE invoking `cwf-manage update`. The Step-2
      revert path depends on this SHA; if it is not recorded, do not proceed. The
      expected value is the d/e checkpoint tip at exec time (do not hardcode).
- [ ] Confirm `.cwf/` working tree is **clean** (`cwf-manage update` refuses with
      uncommitted changes under `.cwf/`):
      `git status --porcelain -- .cwf .cwf-skills .cwf-rules .cwf-agents` empty.
      Commit/stash any stragglers first.
- [ ] Verify no stale lock: `.cwf/.update.lock` absent. Do **not** pre-emptively
      `rm` it on a guess — the lock is a `flock` (`acquire_update_lock`), so a
      live holder makes the update refuse with an "in progress" error on its own.
      Only remove the lock file if the update refuses AND `ps` confirms no
      `cwf-manage` is running.

### Step 2: Run the upgrade (primary path)
- [ ] `.cwf/scripts/cwf-manage update v1.1.183`
- [ ] Expect: update lock acquired; source cloned; target (v1.1.183) install.bash
      performs subtree remove-then-add (several auto squash commits +
      `.cwf-rules/.cwf-skills/.cwf-agents` adds); `run_apply_artefacts` on the new
      manifest; `run_settings_merge` registers the two new PreToolUse hooks +
      `worktree.baseRef: head`; exact-perms pass; outer cwf-manage writes
      authoritative `.cwf/version` (**commit-SHA** `cwf_sha`, `cwf_ref=v1.1.183`).
- [ ] **If `run_apply_artefacts` aborts on an artefact** (e.g.
      `claude-md-preamble`, `gitignore-entries`, `cwf-rules-bundle`,
      `regenerate-symlinks`): `CWF_UPGRADE_RESOLVE` is **per-invocation, not
      per-artefact**. Options: (a) re-run the *entire* update with
      `CWF_UPGRADE_RESOLVE=keep` (preserve consumer mods) or `=new` (take manifest
      wholesale); (b) manually reconcile the conflicting file to match one side,
      then re-run. Pick (a) keep when the consumer file is load-bearing.
- [ ] **If `run_settings_merge` aborts**: the +260-line merge is the new risk
      surface this span. It dies on non-zero; the FR9 allowlist scan is
      warning-only by design and must not be the cause. If it aborts, the tree is
      laid down but `.cwf/version` may still read v1.1.177 — treat as half-applied
      and revert (do not hand-edit settings.json to fix-forward).
- [ ] **If `apply_exact_perms_or_die` aborts**: tree is at v1.1.183 but
      `.cwf/version` still reads v1.1.177 — half-applied. Detect:
      `grep ^cwf_version .cwf/version` still `v1.1.177` AND a known-v1.1.183-only
      marker present (e.g. `test -f .cwf/scripts/hooks/pretooluse-planning-write-guard`).
      Do **not** fix-forward (`fix-security` is clamp-only and cannot complete the
      version pin). Treat as failed laydown and revert.
- [ ] **Fallback / revert on failure**: (i) `git reset --soft <pre-upgrade-HEAD>`
      (Step 1); (ii) `git restore --staged . && git checkout -- .cwf .cwf-skills
      .cwf-rules .cwf-agents .claude` (this single checkout restores
      `.cwf/version` too — it lives under `.cwf`); (iii) preview orphans with
      `git clean -fd --dry-run -- .cwf .cwf-skills .cwf-rules .cwf-agents
      .claude/skills .claude/agents`, **read the dry-run list and confirm it
      before** the live run — `git clean` deletion is **irreversible** (unlike the
      reflog-recoverable soft reset); then run without `--dry-run` to remove the
      install.bash-created new paths absent from pre-upgrade HEAD (the new hook
      files, `sandboxing.md`, `worktree-process.md` are exactly this kind of new
      path). **Use `-fd`, not `-fdx`** — `.cwf/` is fully *tracked* (not
      gitignored), so `-x` adds nothing for the tracked dirs but would also delete
      legitimately-ignored local artefacts under `.claude/`; dropping `-x` also
      keeps the dry-run preview faithful to what the live run deletes. (iv) remove
      a stale `.cwf/.update.lock` only per the Step-1 flock rule. (v) After
      revert, assert `.claude/settings.json` still parses
      (`python3 -m json.tool .claude/settings.json`) — a settings-merge abort can
      leave a half-written file, and step (ii)'s checkout restores it (tracked)
      but the validity check confirms it. Soft (not hard) reset so install.bash
      commits stay reflog-reachable; the user has previously denied
      `git reset --hard`.

### Step 3: Validate & verify laydown
The laydown already recreates symlinks and sets exact perms; `cwf-manage validate`
is authoritative. Only recreate symlinks by hand if validate reports a dangling link.
- [ ] `cwf-manage status` → **Version: v1.1.183**; `cwf_ref` flips
      `v1.1.177` → `v1.1.183` (expected, not drift); `cwf_sha` =
      `faf92479fac564f241ce10afb8ec00c986ad37f1` (**commit** form, per Approach
      Rationale — **not** the tag-object `1842f7b0…`).
- [ ] `cwf-manage validate` → exit 0. The perms-ceiling check (T170) plus the
      T183 fix-on-sight context: if validate reports **fixable perms only** (incl.
      the 5 pre-existing floor-drift files seen at the a-checkpoint, which the
      laydown's exact-perms pass should itself clamp), run `cwf-manage
      fix-security` **once** and re-validate; if still dirty after that single
      clamp-only pass, treat as a failed laydown and revert per Step 2 — do not
      loop fix-security, and never recompute a hash to clear a warning (T183).
- [ ] **New-version markers present**: `test -f
      .cwf/scripts/hooks/pretooluse-planning-write-guard`,
      `test -f .cwf/scripts/hooks/pretooluse-sandbox-logging`. Every
      `.claude/skills/cwf-*` and `.claude/agents/cwf-*` symlink resolves.
- [ ] **settings.json well-formed + inert + correct hook targets**:
      `.claude/settings.json` parses as JSON (e.g. `python3 -m json.tool` /
      `jq .`); the new hooks are registered but inert (no `sandbox.enabled=true`,
      no `planning-write-guard` ≠ `off` anywhere in effect); `worktree.baseRef`
      present. **Also verify *what command each new PreToolUse entry runs***: the
      knob gates the hook's *behaviour*, but the harness still *executes* the
      `command` on every matching tool call — so confirm the two new entries'
      `command` values point at the expected `.cwf/scripts/hooks/pretooluse-*`
      paths (matching the existing hook-entry convention already in
      `settings.json`) and nothing else. (Determine during exec whether
      `cwf-manage validate` itself parses `settings.json`; if it does, the manual
      `jq` parse is belt-and-braces — keep it anyway, it is one read-only command
      and underpins the target check.)
- [ ] **Workflow smoke check** (functional, not just file-exists):
      `task-context-inference` and `context-manager hierarchy 14` resolve task 14;
      `workflow-manager control --current-step=d-implementation-plan
      --task-path=14` runs; a planning-phase file write under
      `implementation-guide/14-.../` is **not** blocked by the new guard. Rubric:
      non-zero exit on any helper = blocker; new informational warnings on
      historical content = expected.

### Step 4: Commit
- [ ] The subtree update auto-creates squash commits for the laydown (these carry
      `.cwf/install-manifest.json` and `.cwf/security/script-hashes.json` inside
      `.cwf/`). The post-laydown uncommitted writes are narrower: `.cwf/version`
      (authoritative write), `.claude/settings.json` (settings-merge — **new this
      span**), and any exact-perms mode changes.
- [ ] Stage with **named paths**, not `git add -A`: `git add .cwf/version
      .claude/settings.json` (add `.cwf/install-manifest.json`,
      `.cwf/security/script-hashes.json` only if they show modified after the
      subtree commits). Named-path staging also serves security: it keeps a stray
      laid-down clone artefact under `.cwf/` from being swept in past review.
      **Before committing, eyeball the `.claude/settings.json` diff specifically**
      — it is the only consumer trust-boundary file changing this upgrade (new
      hook `command` entries + `worktree.baseRef`). Record the squash + metadata
      commit SHAs in `f-implementation-exec.md`.

## Code Changes
N/A — operational upgrade driven by `cwf-manage`; no project source code is edited.

## Test Coverage
**See e-testing-plan.md for complete test plan.**

## Validation Criteria
**See e-testing-plan.md for validation criteria and test results.**

## Scope Completion
**IMPORTANT**: Complete all planned implementation before marking task Finished.

The upgrade is atomic in intent: a half-applied upgrade (laydown done, validate
failing, wrong version recorded, or a malformed `.claude/settings.json`) is not
"done". If `validate` cannot be made to pass cleanly via a single clamp-only
`fix-security` pass, revert per Step 2 and re-plan — do not mark Finished with a
dirty validate or a broken settings.json.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-plan 14
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Steps 1–4 executed as planned (see f-implementation-exec.md for the detailed
record). Single-pass laydown; commit-SHA flip confirmed; validate exit 0. One
deviation: the settings-merge added 0 hook entries (hooks unregistered, not
registered-inert) — the planned hook command-target / settings-merge-abort /
post-revert-reparse mitigations were correct contingencies but unexercised.

## Lessons Learned
The commit-SHA derivation by sub name (not line number) was accurate and
drift-proof. The plan over-anticipated the settings.json mutation; a pre-flight
"is sandbox enabled in this project?" check would have predicted the no-op merge.
See j-retrospective.md.
