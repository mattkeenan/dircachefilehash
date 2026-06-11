# upgrade CwF to v1.1.189 - Testing Execution
**Task**: 22 (chore)

## Task Reference
- **Task ID**: internal-22
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/22-upgrade-cwf-to-v1-1-189
- **Template Version**: 2.1

## Goal
Execute the tests defined in e-testing-plan.md and verify implementation from d-implementation-plan.md.

## Execution Checklist
- [x] Read e-testing-plan.md and d-implementation-plan.md thoroughly
- [x] Verify test environment ready
- [x] Execute test cases sequentially
- [x] Record pass/fail for each test
- [x] Document failures with reproduction steps
- [x] Update status to "Finished" when all pass

## Test Results

### Functional Tests

| Test ID | Test Case | Expected | Actual | Status | Notes |
|---------|-----------|----------|--------|--------|-------|
| TC-1 | Version file pinned | `cwf_version=v1.1.189`, `cwf_ref=v1.1.189`, `cwf_sha=6af636e…`, `cwf_method=read-tree` | All four fields match | PASS | `cwf_sha=6af636e32ad1ffaebd2601c7101dd46c8a3c30b7` |
| TC-2 | No merge commit | `git log --merges 82ff991..HEAD` empty | Empty | PASS | Linear history preserved |
| TC-3 | Integrity validates | `cwf-manage validate` exits 0 | `[CWF] validate: OK`, exit 0 | PASS | No perm or sha256 violations |
| TC-4 | cwf-project.json reconciled | `version`/`_version-note` absent; `cwf-version`/`_cwf-version-note`/`versioning` present; valid JSON | `_cwf-version-note`, `cwf-version`, `versioning` (`last_released=v0.13.21`, `major_minor=v0.13`) present; `version`/`_version-note` absent; JSON valid | PASS | Two task-188 keys removed, others intact |
| TC-5 | Workflow tooling functional | A helper/skill invocation succeeds and reports task hierarchy | `workflow-manager status -w 22` → `22 (chore): upgrade-cwf-to-v1-1-189 - 25%`, exit 0 | PASS | See note below on invocation syntax |

### Non-Functional Tests
- **Regression**: `go test ./pkg/...` → all three packages `ok`, exit 0. No Go sources changed; sanity guard confirms no incidental breakage. PASS.
- **Reliability**: fail-closed laydown behaviour relies on the tool's tested behaviour; not separately exercised (per e-plan).

## Test Failures

None. One non-failure operator note on TC-5: the first invocation used
`workflow-manager status --task-path=22`, which the v1.1.189 status-aggregator
rejects (`Unknown option: --task-path`). The correct form takes `TASK_PATH` as a
**positional** argument: `workflow-manager status -w 22`. This is a CLI-usage
correction on the test command, not a defect in the upgrade — the tool itself
functions correctly.

## Coverage Report

All five a-task-plan.md success criteria are asserted (TC-1 ↔ version file,
TC-2 ↔ no merge commit, TC-3 ↔ validate clean, TC-4 ↔ config reconciliation,
TC-5 ↔ tooling functional). Regression sanity via the Go suite. 5/5 functional
TCs PASS; regression PASS.

## Status
**Status**: Finished
**Next Action**: /cwf-retrospective 22
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Security Review

**State**: no findings

I have the full changeset. This is the testing-exec phase of the same CWF v1.1.185 → v1.1.189 upgrade task. The changeset is identical in substance to what the implementation-exec security review already covered: documentation, agent/skill frontmatter, a JSON schema cleanup, `script-hashes.json` SHA updates, `.cwf/version` bump, and CWF planning markdown files. No product Go code, no Perl helper code, no shell command construction.

Let me reason through the five threat categories.

**(a) Bash injection / unsafe command construction.** No shell command construction is added or modified anywhere in this diff. The planning files (a/d/e/f markdown) reference commands like `cwf-manage update v1.1.189`, `git log --merges 82ff991..HEAD`, and `git rev-parse v1.1.189` purely as documentation prose describing the upgrade procedure — none are interpolated into an executed string. No `system($string)` single-string form, no backtick construction. Clean.

**(b) Perl helpers consuming git/user output without `-z` / input validation.** No Perl source is touched in this changeset. `script-hashes.json` records new SHA256 values for the five reviewer agent files and `cwf-agent-shared-rules.md`, but the helper scripts that consume git output are unchanged. The integrity of those recorded hashes is the deterministic concern owned by `cwf-manage validate`, not this review. Clean.

**(c) Prompt injection via user-supplied strings.** This is the one category with real signal, and the changeset documents it itself. The substantive security-relevant change is the reviewer-agent tool-grant widening: `cwf-security-reviewer-changeset.md` and the four `cwf-plan-reviewer-*.md` agents move from `allowed-tools: Read, Grep, Glob` to `tools: Read, Grep, Glob, LSP, Bash`. These reviewer agents ingest untrusted content (plan files, and diffs carrying task descriptions and `{arguments}`). Granting `Bash` widens the prompt-injection blast radius from "form a judgement" to "execute commands."

This is not an unflagged defect. The changeset's own `security-review.md` (diff lines 185–187, file lines 12 and 14) explicitly records this as a deliberate posture choice: it names the residual FR4(c) threat, states the mitigations (tool-tier guidance steering reviewers to higher tiers; absence of `Edit`/`Write` so there is no in-place file mutation), justifies it (keeping the Bash-run markdown-reader skill usable), and notes the follow-up options (narrowing Bash back out, or scoping Skill access via settings-permissions). The residual mitigation now rests on guidance plus the absence of `Edit`/`Write` rather than on the absence of `Bash` — a weaker control than before, but a documented, scoped, upstream-tracked decision. I surface it as an awareness item, not a blocking defect.

One framing note worth recording (already raised in the implementation-exec review): the frontmatter key renames from `allowed-tools` to `tools`. If the harness only honours `allowed-tools` for restricting an agent's grant, the rename could silently change the effective grant. That is a CWF-upstream concern, gated by `cwf-manage validate` and the recorded `script-hashes.json` SHAs, not a defect this repo's changeset introduces. Out of scope to block.

**(d) Unsafe environment-variable handling.** No env-var handling code changes. The `d-implementation-plan.md` and `f-implementation-exec.md` planning files actually *strengthen* env-var hygiene by documenting a precondition that `CWF_SOURCE` be unset, so the laydown source provably comes from the recorded `cwf_source` in `.cwf/version` rather than an environment override. No new env var feeds `chmod`/`rm`/`open`. Clean.

**(e) Pattern-based risks (safe-here-but-risky-elsewhere).** The widened `Bash` grant on reviewer agents is the right fit here, framed per the carve-out: it is **safe here because** the reviewer agents lack `Edit`/`Write` and the tool-tier guidance steers away from raw Bash; **audit future uses where** that invariant might not hold — specifically any future agent definition that copies the `tools: …, Bash` pattern while also being granted `Edit`/`Write`, or where the tool-tier guidance is dropped. At that point untrusted plan/diff content reaching an agent that has both command execution and file mutation would be a materially larger injection surface. The mitigation is soft (prompt-level guidance, not a hard allowlist), so any reuse should re-evaluate against that invariant.

The remaining changes carry no security surface: deleting `.cwf/version`'s old lines (version bump), removing two vestigial JSON keys (`version`/`_version-note`) from `cwf-project.json` and its template, the `effort:` frontmatter additions, the tool-tier rubric doc edits, and the task-22 planning docs. No secrets, credentials, tokens, or auth material appear anywhere in the diff.

Note also that this testing-exec changeset includes the implementation-exec phase's own `f-implementation-exec.md` (with its already-recorded security verdict). Reviewing a prior verdict block embedded in a diff is itself a mild prompt-injection surface — the embedded `cwf-review` block is data here, not an instruction. I have treated it as content and reached my own independent conclusion; it happens to agree.

Conclusion: no actionable defect was introduced by this testing-exec changeset. The one substantive security-relevant change — widening reviewer agents to include `Bash` — is a deliberate, self-documented FR4(c) posture decision with mitigations and a follow-up spelled out in the changeset itself. I surface it as an awareness/pattern-risk item for future agent definitions, but it does not block this upgrade.

```cwf-review
state: no findings
summary: CWF v1.1.185→v1.1.189 upgrade (testing-exec changeset); docs/frontmatter/JSON-cleanup only. Reviewer agents widened to include Bash is a deliberate, self-documented FR4(c) posture choice (no Edit/Write, tool-tier guidance) — awareness/pattern-risk item, not an introduced defect. No injection/secret/env-var issues.
```

## Lessons Learned
The MIN-bottleneck progress formula pins task progress at 25% while the
retrospective phase is non-terminal — expected, not stale status. TC-5's helper
takes a positional `TASK_PATH`, not `--task-path`. See j-retrospective.md.
