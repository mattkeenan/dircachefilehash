# upgrade CwF from v1.1.189 to v1.1.201 - Testing Execution
**Task**: 29 (chore)

## Task Reference
- **Task ID**: internal-29
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/29-upgrade-cwf-from-v1-1-189-to-v1-1-201
- **Template Version**: 2.1

## Goal
Execute e-testing-plan.md test cases against the post-upgrade tree and record results.

## Test Results

### Functional Tests

| Test ID | Test Case | Status | Evidence |
|---------|-----------|--------|----------|
| TC-1 | Version recorded | **PASS** | `.cwf/version`: `cwf_version=v1.1.201`, `cwf_ref=v1.1.201`, `cwf_method=read-tree`, `cwf_sha=2933eba8…` |
| TC-2 | Integrity validates | **PASS** | `cwf-manage validate` → `[CWF] validate: OK` (manifest sha + exact perms incl. new hook at 0500). `/cwf-security-check` is the user-facing wrapper over this same deterministic check. |
| TC-3 | Linear history preserved | **PASS** | `git log --merges e1255c3a..HEAD` empty — read-tree laydown, single-parent commit |
| TC-4 | settings.json migration exactly expected | **PASS** | (a) legacy PreToolUse matcher=="UserPromptSubmit" reshaped to top-level UserPromptSubmit hook; (b) rules-inject command byte-identical `cat .cwf/rules-inject.txt 2>/dev/null \|\| true`; (c) exactly one new PreToolUse/Bash group → `pretooluse-bash-tool-check` (timeout 5) + allowlist entry; (d) no other command/hook entries |
| TC-5 | New Bash hook fail-open & inert | **PASS** | `.cwf/tool-check/` does not exist — zero checked-in rules; only the gitignored `.cwf/tool-check/*/settings.local.json` could carry live rules. Bash calls ran unimpeded all phase. Hook header documents fail-open posture. |
| TC-6 | No surprise files | **PASS** | post-f-commit `git status --untracked-files=all` shows only this phase's wf files (g/j) untracked; all upgrade changes inside the Step-3 allowlist |
| TC-7 | .gitignore/CLAUDE.md CWF-region only | **PASS** | `.gitignore` +1 line `.cwf/tool-check/*/settings.local.json` (CWF-managed region); `CLAUDE.md` no diff |

### Non-Functional Tests
- **Regression (reliability)**: **PASS** — `make build` produced all three binaries
  (`v0.13.0-LOCAL-682a2615`); `go test ./pkg/... ./cmd/...` all `ok`
  (pkg 0.263s, cmd/dcfh 5.286s, rest cached). Upgrade touched no Go code paths.
- **Workflow tooling (usability)**: **PASS** — `workflow-manager status --workflow 29`
  runs clean; `security-review-changeset` resolves the task baseline (anchor=e1255c3)
  without error.
- **Security**: covered by the `cwf-security-reviewer-changeset` agent bounded to the
  inter-repo surface; see `## Security Review` below and f-exec's review.

## Test Failures
None.

## Coverage Report
100% of e-testing-plan.md test cases (TC-1…TC-7) plus both non-functional bands executed.
All pass.

## Status
**Status**: Finished
**Next Action**: /cwf-retrospective (chore: rollout/maintenance phases skipped)
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Security Review

**State**: no findings

### Cap note (mechanical helper result)
`.cwf/scripts/command-helpers/security-review-changeset --wf-step=testing-exec`
exited **2**: `cap exceeded: 1384 production lines > 500` (30 files, anchor e1255c3).
The production count is **identical** to the f-exec run because this phase shipped
**no code** — only markdown test-result docs. The 1384 lines are the same
CWF-internal `.cwf/**` relaid tree, **out of scope** per `a-task-plan.md`
§ "Security Review Scope".

### In-scope surface unchanged since f-exec
`git diff 682a2615 -- .claude/settings.json .gitignore CLAUDE.md` is **empty** and
no non-markdown file changed since the f-commit. The inter-repo integration surface
is therefore **byte-identical** to the changeset the `cwf-security-reviewer-changeset`
agent already reviewed in f-implementation-exec and classified **no findings**
(fail-open/inert Bash hook, minimal perm grant, rules-inject byte-identical,
defensive `.gitignore`). Re-running the agent on an identical surface would be
redundant; the f-exec verdict stands for this phase.

## Lessons Learned
When a phase ships only markdown (no code/config change), the in-scope security surface
is provably unchanged from the prior phase — `git diff <f-commit> -- <inter-repo files>`
empty — so the earlier `no findings` verdict carries forward without a redundant agent
re-run. Verifying the diff is empty is the cheap, defensible substitute for re-review.
