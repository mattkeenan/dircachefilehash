# upgrade CwF to v1.1.183 - Testing Execution
**Task**: 14 (chore)

## Task Reference
- **Task ID**: internal-14
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/14-upgrade-cwf-to-v1-1-183
- **Template Version**: 2.1

## Goal
Execute the TC-1..TC-9 acceptance checks in e-testing-plan.md against the
upgraded v1.1.183 install and verify each a-task-plan success criterion.

## Execution Checklist
- [x] Read e-testing-plan.md and d-implementation-plan.md thoroughly
- [x] Verify test environment ready (upgrade landed in f-implementation-exec)
- [x] Execute test cases sequentially
- [x] Record pass/fail for each test
- [x] Document deviations
- [x] Update status

## Test Results

### Functional Tests

| Test ID | Test Case | Expected | Actual | Status |
|---------|-----------|----------|--------|--------|
| TC-1 | Version + SHA flip | `Version: v1.1.183`; `cwf_sha` = **commit** `faf92479…` (NOT tag-object `1842f7b0…`); `cwf_ref=v1.1.183` | status + `.cwf/version` show `v1.1.183` / `faf92479fac564f241ce10afb8ec00c986ad37f1` / `cwf_ref=v1.1.183` | **PASS** |
| TC-2 | Integrity clean | `validate` exit 0, zero violations | `validate: OK`, exit 0 — no `fix-security` pass needed (laydown exact-perms clamped floor-drift) | **PASS** |
| TC-3 | Skill symlinks resolve | no broken links | 20 skills checked, 0 broken | **PASS** |
| TC-4 | Agent defs resolve (+T182) | 5 agents resolve; changeset reviewer carries `cwf-review` block + `{wf_step}` (not `{phase}`) | 5 agents, 0 broken; `cwf-review` / `{wf_step}` markers present in `cwf-security-reviewer-changeset.md` | **PASS** |
| TC-5 | Hooks present + settings well-formed + inert + targets | both hook files present; settings.json valid; hooks inert; targets correct | both hooks PRESENT; settings.json VALID JSON; **INERT=True** (no `sandbox`/`worktree` keys, hooks unregistered) — see Deviation 1 | **PASS** (deviation) |
| TC-6 | Workflow regression + planning-write | helpers exit 0; planning-write not blocked | `task-context-inference`, `context-manager hierarchy 14`, `workflow-manager control`, `backlog-manager validate` all exit 0; writes to `implementation-guide/14-…` succeeded | **PASS** |
| TC-7 | Revert path (negative) | available if upgrade abandoned | **Not exercised** — upgrade succeeded. Path available: pre-upgrade HEAD `e24658415b788005937bfea7c5d4458223a8e001` recorded in f-exec Step 1 | **N/A (available)** |
| TC-8 | Half-applied probe (negative) | deterministic detection if laydown aborts | **Not exercised** — laydown completed atomically (`.cwf/version` = v1.1.183 AND marker hook present, the consistent end-state) | **N/A (no abort)** |
| TC-9 | `sandbox` stanza present-or-absent | guard off either way | `sandbox` stanza ABSENT in project `cwf-project.json`; guard off (absence does not enable) | **PASS** |

### Non-Functional Tests
- **Integrity/Security**: TC-2 (`cwf-manage validate` exit 0 — sha256 + perms-ceiling)
  passes. The one consumer trust-boundary file changing this upgrade
  (`.claude/settings.json`) was reviewed: only an empty `"deny": []` array was
  removed (net −1 line); no hook/`worktree` injection. Exec-phase changeset
  security review (testing-exec) recorded below.
- **Reliability**: revert path (TC-7) verified available but not needed;
  half-applied probe (TC-8) confirms the end-state is internally consistent
  (version pinned AND v1.1.183-only marker present), i.e. not half-applied.

## Test Failures
None. All executable cases (TC-1..6, TC-9) PASS; TC-7/TC-8 are abort-only
negative cases not triggered by a clean upgrade.

## Deviations from plan
1. **TC-5 — hooks unregistered, not "registered but inert".** The plan
   anticipated the v1.1.183 settings-merge would inject the two new PreToolUse
   hooks (and `worktree.baseRef`) into `.claude/settings.json`, then asserted
   they be inert with correct `command` targets. **Actual**: the merge added
   `0 hook entries` — the hook *files* are on-disk but were **not registered**,
   so there is no new PreToolUse `command` to target-verify and the harness does
   not execute them at all. This is **strictly safer** than the planned
   outcome and still satisfies a-plan SC3 (hooks present on-disk, settings.json
   well-formed, guards inert). The "correct command targets" sub-check is
   therefore vacuously satisfied (no new entries to mis-target). Root cause: the
   hooks are opt-in and stay unregistered until sandbox is enabled in project
   config — which it is not (TC-9).

## Coverage Report
- 100% of a-task-plan success criteria mapped and verified (TC-1↔SC1+SC-sha,
  TC-2↔SC2, TC-5↔SC3, TC-6↔SC4, project-code-untouched↔SC5 via empty
  `git diff … -- pkg cmd go.*`).
- Both outcomes covered: success path (TC-1..6, TC-9) exercised; failure/revert
  path (TC-7, TC-8) available and reasoned, not triggered by a clean upgrade.

## Security Review

**State**: no findings

(Step 8 — testing-exec changeset. The helper (`--wf-step=testing-exec`) first
exited `2`: **cap exceeded, 1305 production lines > 500** — the same vendored
v1.1.177→v1.1.183 `.cwf/` subtree delta as the implementation-exec phase (the
testing-exec phase adds only `g-testing-exec.md`, an `implementation-guide/**`
doc already discounted by `security.review.max-lines-exclude-paths`; no new
executable surface). Per the user direction carried from the implementation-exec
review, the cap was raised **once** (`--max-lines=5000`, no persistent config
change) and `cwf-security-reviewer-changeset` was invoked against the 2556-line
`.out`. Verdict classified by `security-review-classify` = `no findings`,
matching the f-phase verdict. Verbatim subagent output below.)

## Security Review — testing-exec changeset (Task 14: CWF v1.1.177 → v1.1.183 upgrade)

I read the full changeset and cross-checked the load-bearing claims against the on-disk files.

### Nature of the diff
Vendored CWF subtree upgrade plus this task's own planning documents. There is **no** project source code (`pkg/`, `cmd/`, `go.*`) in the changeset. The security-relevant runtime surface is the new/changed Perl machinery: the two new PreToolUse hooks, `cwf-claude-settings-merge` (+260), `CWF::PlanningGuard.pm`, `CWF/Validate/Config.pm`, and the rewritten `security-review-changeset`.

### Threat categories (a)–(e)
**(a) Bash injection.** Every new command-spawning site clean: `derive_roots` backtick `git rev-parse --show-toplevel` with fixed argv, no interpolation; `dep_on_path` pure-Perl PATH walk (`-x`), no shell. No new `system($string)`.
**(b) Git/user output without `-z`/validation.** Only single-path `--show-toplevel` consumed (chomped, length-checked); JSON via `JSON::PP`/`eval`; `warn_on_worktree_allowlist` reads raw blob with `index()`, no JSON decode. Clean.
**(c) Prompt injection.** Guard emits fixed enumerated tokens as `permissionDecisionReason`; `_phase_token` collapses unknown→`phase:unknown`; sandbox-logging records presence flag only; TCI warn/STDERR contained. Correct.
**(d) Env-var handling.** Only `$ENV{PATH}` in `dep_on_path` for `-x` tests, empty/`.` skipped; no env→command/path-write flow. Clean.
**(e) Pattern risks (non-defects).** (1) `security-review-changeset` scratch path built from `task_num`/`wf_step`, both allowlist/regex-validated before FS use — audit unvalidated future uses. (2) `classify_path` fail-closes to crown-jewel on unresolvable target — safe in deny-gated caller; audit allow-decision reuse.

### Other observations (non-findings)
- `script-hashes.json`/`install-manifest.json` updates consistent; integrity is `cwf-manage validate`'s domain.
- New hooks inert by default and not registered into this project's settings.json — no new PreToolUse command executed.
- No secrets/credentials/tokens introduced. Matches the f-phase verdict.

```cwf-review
state: no findings
summary: CWF v1.1.177→v1.1.183 vendored upgrade plus task planning docs; new PreToolUse hooks emit fixed-token deny reasons and presence-only logging, changeset helper validates --wf-step/--task-num against allowlist/regex before path use, no shell injection, no env-var-to-command flow, no secrets, no project code touched. Two (e) pattern notes documented with audit pointers.
```

## Status
**Status**: Finished
**Next Action**: /cwf-retrospective 14
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Lessons Learned
*To be captured during retrospective*
