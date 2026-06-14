# upgrade CwF from v1.1.189 to v1.1.201 - Implementation Execution
**Task**: 29 (chore)

## Task Reference
- **Task ID**: internal-29
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/29-upgrade-cwf-from-v1-1-189-to-v1-1-201
- **Recovery anchor (pre-upgrade HEAD)**: 1c410cc591a1058a962e7dff315ff7a998e4e426
- **Template Version**: 2.1

## Goal
Lay down CWF v1.1.201 over v1.1.189 via the installed read-tree `cwf-manage`,
producing a single non-merge commit with `.cwf/version` updated and validated,
and reconcile the inter-repo artefacts (`.claude/settings.json`, `.gitignore`,
`CLAUDE.md`).

## Actual Results

### Step 1: Pre-flight — PASS
- **Clean-tree gate** (`git status --porcelain -- .cwf .cwf-rules .cwf-agents .cwf-skills`): empty.
- **Pre-flight validate**: `[CWF] validate: OK`.
- **Recorded state**: `.cwf/version` = v1.1.189, sha `6af636e3…`, method read-tree.
- **Snapshots** of `.claude/settings.json`, `.gitignore`, `CLAUDE.md` captured to
  `/tmp/-home-matt-repo-dircachefilehash-task-29/` (mode 0700) for before/after diff.
- **Source tag**: `git --git-dir=/home/matt/repo/coding-with-files/.git tag -l v1.1.201` → present.

### Step 2: Run the upgrade — PASS
- `.cwf/scripts/cwf-manage update v1.1.201` completed:
  `Updated to v1.1.201 (2933eba88cc0936d612bb0024537e63bde3861d1)`.
- Laydown via target ref's `install.bash` (read-tree, no merge detection).
- Tool-reported settings merge: *"added 1 allowlist entries, 2 hook entries, 0 env
  keys (migrated 1 legacy dead PreToolUse/UserPromptSubmit hook entry)"* —
  matches the planned Task 195 migration + Task 201 new hook exactly.
- gitignore: +1 line. CLAUDE.md preamble: already up to date.
- Permission tightening applied by the tool (hooks/helpers 0700→0500, libs/docs
  0600→0444). No deviation — this is v1.1.201's stricter posture.

### Step 3: Reconcile inter-repo artefacts (security surface) — PASS
- **`.claude/settings.json`** diff matches TC-4 exactly:
  - (a) Legacy `PreToolUse` group `matcher == "UserPromptSubmit"` reshaped: the
    rules-inject moved to a proper top-level `UserPromptSubmit` hook (Task 195).
  - (b) Rules-inject command **byte-identical** before/after:
    `cat .cwf/rules-inject.txt 2>/dev/null || true`.
  - (c) Exactly one new `PreToolUse`/matcher-`Bash` group →
    `.cwf/scripts/hooks/pretooluse-bash-tool-check` (timeout 5), plus its
    allowlist entry `Bash(.cwf/scripts/hooks/pretooluse-bash-tool-check)` (Task 201).
  - (d) No other command/hook entries introduced.
- **`.gitignore`**: +1 line `.cwf/tool-check/*/settings.local.json` (the gitignored
  live-rules path for the new Bash hook). CWF-managed region only.
- **`CLAUDE.md`**: no diff.
- **No merge commit**: `git log --merges 1c410cc5..HEAD` and `…e1255c3a..HEAD` empty.
- **HEAD unchanged** at 1c410cc5 (laydown stages, does not commit).
- **Surprise-file scan** (`git status --untracked-files=all`): every change inside
  the allowlist — relaid `.cwf/**` (scripts, libs, docs, templates, new
  `ToolCheck.pm`/`Validate/Agents.pm`/`tool-check-rules.md`, refreshed
  `script-hashes.json`, removed legacy `.cwf/utils/*.md`), the new
  `pretooluse-bash-tool-check` hook, `.cwf/version`, and the two inter-repo
  artefacts (`.claude/settings.json`, `.gitignore`). Nothing outside the set.

### Step 4: Validate — PASS
- `.cwf/scripts/cwf-manage validate` → `[CWF] validate: OK` (manifest + exact perms).
- `.cwf/version` shows `cwf_version=v1.1.201`, `cwf_ref=v1.1.201`,
  `cwf_sha=2933eba8…`, `cwf_method=read-tree`, source preserved.
- Workflow helper smoke: `workflow-manager status --workflow 29` runs clean.
- **TC-5 (new Bash hook inert)**: `.cwf/tool-check/` does not exist — the hook
  ships with zero checked-in rules; only the gitignored
  `.cwf/tool-check/*/settings.local.json` could ever carry live rules.

## Deviations
- None affecting outcome. `.cwf/version` appears as staged-delete + untracked
  (tool rewrote the file); resolves to a normal modification once staged at commit.

## Blockers Encountered
None.

## Deferral Check
- [x] All steps from d-implementation-plan.md executed
- [x] All success criteria from a-task-plan.md met
- [x] No planned work deferred

## Status
**Status**: Finished
**Next Action**: /cwf-testing-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Security Review

**State**: no findings

### Cap note (mechanical helper result)
`.cwf/scripts/command-helpers/security-review-changeset --wf-step=implementation-exec`
exited **2**: `cap exceeded: 1384 production lines > 500` (31 files, anchor e1255c3).
Those 1384 production lines are **overwhelmingly CWF-internal** (relaid `.cwf/**`
scripts/libs/docs) — explicitly **out of scope** for this task's security review
per `a-task-plan.md` § "Security Review Scope" (CWF internals already cleared by 3
upstream reviews). Per the user's direct scoping instruction (which outranks the
SKILL's blanket changeset construction in the instruction-priority order), the
review was bounded to the **inter-repo integration surface** only.

### Scoped inter-repo review (the in-scope surface)
A changeset scoped to `.claude/settings.json`, `.gitignore`, `CLAUDE.md`
(`/tmp/-home-matt-repo-dircachefilehash-task-29/interrepo-changeset.out`, 55 lines)
was reviewed by the `cwf-security-reviewer-changeset` agent. Verdict classified by
`security-review-classify`: **no findings**.

<verbatim subagent output>

## Security Review — Inter-repo Integration Surface (task 29, CWF v1.1.189 → v1.1.201)

Scope honoured: reviewed only `.claude/settings.json`, `.gitignore`, `CLAUDE.md`.
CWF internals not re-audited; the registered hook's header banner was read only to
confirm the registration contract (fail-open / inert posture) the settings depend on.

- **(a) Injection**: both hook commands are static literals — `.cwf/scripts/hooks/pretooluse-bash-tool-check` (bare path, no args/metachars) and the byte-identical `cat .cwf/rules-inject.txt 2>/dev/null || true`. New PreToolUse/Bash matcher intercepts every Bash call but the hook is fail-OPEN + `timeout: 5`; a fault degrades to allow, never bricks/hijacks. No injection introduced.
- **(b) Secrets**: none in diff. `.gitignore` `+.cwf/tool-check/*/settings.local.json` *reduces* exposure (keeps the operator-local rule layer — the only layer permitted live `perl` rules — out of git).
- **(c) Auth/privilege**: one allowlist entry `Bash(.cwf/scripts/hooks/pretooluse-bash-tool-check)` (no `:*` — tighter than a glob). Hook is a gate (deny-or-silent only); cannot escalate Bash. No privilege expansion.
- **(d) Env-vars**: no changes; pre-existing `PERL5OPT=-CDSLA` (Unicode layers only) untouched.
- **(e) Prompt-injection (pattern)**: deny-reason is the matched rule's `guidance` VERBATIM, never reflected tool_input. Safe here because CWF ships zero active rules (inert default) and guidance is operator-authored. Audit future: operator-authored guidance under the gitignored local layer is a verbatim injection channel, but that is an operator-trust boundary. rules-inject move is byte-identical and event-correct (behavioural fix, not new surface).

Conclusion: inter-repo integration is clean — no actionable finding.

```cwf-review
state: no findings
summary: Inter-repo surface clean; Bash hook is fail-open/inert/timeout-bounded, minimal perm grant, rules-inject byte-identical, gitignore is defensive.
```

</verbatim subagent output>

## Lessons Learned
The `security-review-changeset` cap (exit 2, 1384 production lines) is structurally
mismatched to a vendored-tooling upgrade: relaid `.cwf/**` counts as production but is
out of scope. The honest handling — record the mechanical exit-2, then run the in-scope
55-line review explicitly — is correct but not reusable; a config lever for
upgrade-class tasks is recommended in `j-retrospective.md`.
