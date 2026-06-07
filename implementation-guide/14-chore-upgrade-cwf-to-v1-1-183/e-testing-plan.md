# Upgrade CWF to v1.1.183 - Testing Plan
**Task**: 14 (chore)

## Task Reference
- **Task ID**: internal-14
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/14-upgrade-cwf-to-v1-1-183
- **Template Version**: 2.1

## Goal
Verify the v1.1.177→v1.1.183 upgrade landed cleanly: correct version + **commit**
SHA recorded, integrity clean under the perms-ceiling (and T183 fix-on-sight), all
symlinks resolve, the two new PreToolUse hooks merge **inertly** into a well-formed
`.claude/settings.json` with correct `command` targets, the workflow tooling still
runs (incl. an unblocked planning-write), and the revert path is available.

## Test Strategy
Operational upgrade with **no project source edits** → no new unit/integration tests.
The strategy is **acceptance verification**: each `a-task-plan.md` success criterion
maps to one concrete post-upgrade check, plus a revert-path smoke test and a
deterministic half-applied-state probe. CWF's own Perl suite runs in the CWF source
repo, not here — out of scope.

### Test Levels
- **Unit**: N/A — no new code
- **Integration**: N/A — no new code
- **System / Acceptance**: TC-1..TC-9 below, against the upgraded install
- **Regression**: re-run the helpers this workflow depends on; confirm no break

### Coverage Targets
- 100% of `a-task-plan.md` success criteria mapped to an executable check
- Both outcomes covered: success path (TC-1..6, TC-9) **and** failure/revert path
  (TC-7, TC-8)
- The span-specific behaviour changes that touch verification each have a dedicated
  assertion: the `cwf_sha` commit-form flip (TC-1), the new PreToolUse hooks +
  settings-merge (TC-5), the planning-write guard being inert (TC-6), and the new
  `sandbox` config stanza (TC-9)

## Test Cases
### Functional Test Cases
- **TC-1: Version + SHA recorded correctly (the commit-form flip)**
  - **Given**: upgrade run via `cwf-manage update v1.1.183`
  - **When**: `cwf-manage status` and `cat .cwf/version`
  - **Then**: `Version: v1.1.183`; `cwf_sha` ==
    `faf92479fac564f241ce10afb8ec00c986ad37f1` (the **dereferenced commit**
    returned by `git rev-parse v1.1.183^{commit}`, **not** the annotated-tag
    object `1842f7b04e0819364529ae123f3365a69b56b99e`) — because the
    authoritative writer is now the outer **post-T175 v1.1.177** cwf-manage, whose
    `resolve_sha` uses `^{commit}`. This is the **inverse of Task 9** (which
    recorded the tag-object). `cwf_ref` == `v1.1.183` (flipped from `v1.1.177`);
    `cwf_version` == `v1.1.183`

- **TC-2: Integrity clean under perms-ceiling + T183 fix-on-sight**
  - **Given**: completed laydown
  - **When**: `cwf-manage validate`
  - **Then**: exit 0, zero violations. The perms-ceiling enforces recorded perms as
    an **upper bound**; the laydown's exact-perms pass should itself clamp the 5
    pre-existing floor-drift files seen at the a/d checkpoints. If only **fixable
    perms** remain, `cwf-manage fix-security` (run **once**, clamp-only) clears them
    and a re-run is exit 0 (T183 fix-on-sight). If validate is still dirty after one
    pass → failed laydown, revert (TC-7); do not loop, and **never recompute a hash
    to clear a warning** (T183). New hierarchy/template-ref advisories are
    informational on historical content, blocking only on
    `implementation-guide/14-.../`

- **TC-3: Skill symlinks resolve**
  - **Given**: laydown recreated `.claude/skills/cwf-*`
  - **When**: `for l in .claude/skills/cwf-*; do [ -e "$l" ] || echo "BROKEN: $l"; done`
  - **Then**: no `BROKEN:` output; every symlink points at an existing
    `.cwf-skills/cwf-*` target

- **TC-4: Agent defs resolve (incl. T182-hardened changeset reviewer)**
  - **Given**: agent laydown
  - **When**: `for l in .claude/agents/cwf-*; do [ -e "$l" ] || echo "BROKEN: $l"; done`
    and inspect `.cwf-agents/cwf-security-reviewer-changeset.md`
  - **Then**: all present/resolving; the changeset reviewer carries its
    ` ```cwf-review ` verdict-block contract and the T182 update (reads a
    `{changeset_file}` rather than inlined `{changeset}`; `{wf_step}` not `{phase}`)

- **TC-5: New PreToolUse hooks present + settings.json well-formed + inert + correct targets**
  - **Given**: v1.1.183 laydown ran `run_settings_merge` (the +260-line helper)
  - **When**: `test -f .cwf/scripts/hooks/pretooluse-planning-write-guard` and
    `test -f .cwf/scripts/hooks/pretooluse-sandbox-logging`; parse settings with
    `python3 -m json.tool .claude/settings.json` (or `jq .`); inspect the
    PreToolUse hook entries and the sandbox/worktree keys
  - **Then**: both hook files exist and are executable; `.claude/settings.json`
    parses as valid JSON; the two new PreToolUse entries' `command` values point at
    the expected `.cwf/scripts/hooks/pretooluse-*` paths (matching the existing
    hook-entry convention) **and nothing else** — the harness *runs* the command
    regardless of the behaviour knob; the hooks are **inert by default**
    (`sandbox.enabled` not true, `planning-write-guard` not ≠ `off` in effect);
    `worktree.baseRef` (== `head`) present. This is the functional proof that the
    injected-hook change is safe, not just present

- **TC-6: Workflow tooling regression + planning-write not blocked**
  - **Given**: upgraded install
  - **When**: `task-context-inference`; `context-manager hierarchy 14`;
    `workflow-manager control --current-step=e-testing-plan --task-path=14`;
    `backlog-manager validate`; and an actual Edit/Write to a file under
    `implementation-guide/14-.../`
  - **Then**: each helper exits 0; `task-context-inference` resolves task 14 /
    current step; `context-manager hierarchy 14` reports the task-14 chore dir;
    `backlog-manager validate` clean; the planning-write to the task dir is **not**
    denied by the new guard (it is off by default, and the task dir is not a crown
    jewel anyway). Rubric: non-zero exit = blocker; informational warnings on
    historical content = expected

- **TC-7: Revert path is clean (negative / safety)**
  - **Given**: the pre-upgrade HEAD captured in f-implementation-exec.md Step 1
    (d-plan precondition gate)
  - **When**: (only if the upgrade must be abandoned) `git reset --soft
    <pre-upgrade-HEAD> && git restore --staged . && git checkout -- .cwf .cwf-skills
    .cwf-rules .cwf-agents .claude && git clean -fd --dry-run -- .cwf .cwf-skills
    .cwf-rules .cwf-agents .claude/skills .claude/agents` (read output, confirm,
    then re-run without `--dry-run`); then assert `.claude/settings.json` still
    parses; remove `.cwf/.update.lock` only per the flock rule. **`-fd`, not
    `-fdx`** — `.cwf/` is tracked, so `-x` would also delete legitimately-ignored
    local artefacts and inflate the dry-run preview
  - **Then**: tree returns to v1.1.177 with the Task 14 planning commits (a/d/e)
    intact; `cwf-manage status` reports v1.1.177; `.claude/settings.json` is valid
    JSON; `git status --untracked-files=all` shows only the expected uncommitted
    f/g/j templates. The documented escape hatch for any laydown abort

- **TC-8: Half-applied state probe (negative / safety)**
  - **Given**: an aborted laydown where the tree is at v1.1.183 but `.cwf/version`
    may still read v1.1.177 (settings-merge / perms-pass abort *before* the
    authoritative version write)
  - **When**: `grep ^cwf_version .cwf/version` AND
    `test -f .cwf/scripts/hooks/pretooluse-planning-write-guard`
  - **Then**: a half-applied state is identified deterministically when version
    still reads `v1.1.177` AND the known-v1.1.183-only `pretooluse-planning-write-guard`
    hook (added by T180) is present. The discriminative signal triggers the revert
    path (TC-7), **not** a fix-forward — `fix-security` is clamp-only and cannot
    complete the version pin

- **TC-9: `sandbox` config stanza — present-or-absent, guard stays off**
  - **Given**: upstream added a `sandbox` block to *its* `cwf-project.json`
    template; our `implementation-guide/cwf-project.json` is project-owned
  - **When**: `grep -A8 '"sandbox"' implementation-guide/cwf-project.json` (may be
    absent) and confirm the effective guard state via TC-5's inertness check
  - **Then**: whether or not the `sandbox` stanza was added to our project config,
    the planning-write guard is **off** (default-off both when the stanza is absent
    and when present with `planning-write-guard: off` / `enabled: false`). Absence
    must not enable a guard

### Non-Functional Test Cases
- **Integrity/Security**: covered by TC-2 (sha256 + perms-ceiling via
  `cwf-manage validate`), TC-5 (hook `command`-target verification — the one new
  trust-boundary file `.claude/settings.json`), and the exec-phase changeset
  security review. **Note T182**: the review helper now takes `--wf-step=<step>`,
  defaults `--max-lines=500`, and writes the diff to a `.out` file with a single
  confirmation line — the laid-down exec SKILLs call it in this new form
- **Reliability**: covered by TC-7 (atomic revert via soft-reset + checkout +
  `-fd` clean + settings.json re-parse) and TC-8 (deterministic half-applied
  detection; no fix-forward)
- **Performance/Usability**: N/A for a version bump

## Test Environment
### Setup Requirements
- CWF source clone at `/home/matt/repo/coding-with-files` with tag `v1.1.183`
  present (confirmed via `cwf-manage list-releases`)
- Clean working tree under `.cwf/`, `.cwf-skills/`, `.cwf-rules/`, `.cwf-agents/`
  before update (`cwf-manage update` refuses otherwise)
- Pre-upgrade repo HEAD recorded into f-implementation-exec.md before the update
  runs (TC-7 dependency; d-plan Step 1 precondition gate)
- No stale `.cwf/.update.lock` (d-plan Step 1 flock rule)

### Automation
- No CI integration; all checks are manual one-liners run in `g-testing-exec`
- No test doubles — checks run against the real upgraded install

## Validation Criteria
- [ ] TC-1 — version `v1.1.183`, `cwf_sha` == **commit** `faf92479…` (NOT
      tag-object `1842f7b0…`), `cwf_ref` flipped to `v1.1.183`, `cwf_version` ==
      `v1.1.183`
- [ ] TC-2 — `validate` exit 0 (after at most one clamp-only `fix-security` pass)
- [ ] TC-3 — skill symlinks resolve
- [ ] TC-4 — agent defs resolve; verdict-block + T182 contract intact
- [ ] TC-5 — both new hook files present; settings.json valid JSON; hook `command`
      targets correct; hooks inert by default; `worktree.baseRef` present
- [ ] TC-6 — workflow helpers exit 0; task 14 resolves; planning-write not blocked
- [ ] TC-7 — revert path verified available (executed only if upgrade abandoned)
- [ ] TC-8 — half-applied probe deterministic (executed only if a laydown aborts)
- [ ] TC-9 — `sandbox` stanza present-or-absent; guard remains off either way

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-exec 14
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All executable cases PASS (TC-1..6, TC-9); TC-7/TC-8 correctly N/A (clean
upgrade, no abort). See g-testing-exec.md for the full table. TC-5 passed with a
deviation: the hooks were unregistered rather than registered-inert, so the
"correct command targets" sub-check is vacuously satisfied (no new entries).

## Lessons Learned
Verifying hook *registration state* (not just a behaviour knob) was the right
check — it surfaced that opt-in hooks ship unregistered. The cap-exceeded
security review was handled by a one-off cap raise + actual subagent review
(no findings), improving on Task 9's recorded-error path. See j-retrospective.md.
