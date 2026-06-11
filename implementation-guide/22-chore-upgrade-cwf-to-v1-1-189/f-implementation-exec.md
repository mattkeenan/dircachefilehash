# upgrade CwF to v1.1.189 - Implementation Execution
**Task**: 22 (chore)

## Task Reference
- **Task ID**: internal-22
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/22-upgrade-cwf-to-v1-1-189
- **Template Version**: 2.1

## Goal
Execute the implementation following the plan in d-implementation-plan.md and e-testing-plan.md.

## Execution Checklist
- [x] Read d-implementation-plan.md and e-testing-plan.md thoroughly
- [x] Verify all prerequisites met
- [x] Execute implementation steps sequentially
- [x] Update "Actual Results" for each step
- [x] Document any deviations from plan
- [x] Update status to "Implemented" when complete

## Actual Results

### Step 1: Preconditions
- **Planned**: Confirm branch, clean `.cwf*` dirs, `CWF_SOURCE` unset, tag present, record pre-upgrade version.
- **Actual**: On `chore/22-upgrade-cwf-to-v1-1-189`. `git status --porcelain -- .cwf .cwf-skills .cwf-rules .cwf-agents` empty. `CWF_SOURCE` UNSET. Tag `v1.1.189` dereferences to commit `6af636e32ad1ffaebd2601c7101dd46c8a3c30b7`. Pre-upgrade: `cwf_version=v1.1.185`, `cwf_sha=6659c1cca72ef033d92546fcd9d42a0f4d817dd9`, `cwf_method=read-tree`.
- **Deviations**: None.

### Step 2: Lay down v1.1.189
- **Planned**: Run `.cwf/scripts/cwf-manage update v1.1.189`, expect exit 0, no commit.
- **Actual**: Exit 0. Laid down `.cwf/`, `.cwf-skills/`, `.cwf-rules/`, `.cwf-agents/` (staged, not committed); 20 skill + 1 rule + 5 agent symlinks; `settings` merge added 0 entries; wrote `.cwf/version`. A set of `chmod` lines normalised perms (0444 docs/agents/manifest, 0500 helpers/hooks) — expected exact-perms enforcement, not a deviation.
- **Deviations**: None.

### Step 3: Reconcile cwf-project.json (task-188)
- **Planned**: Confirm task-188 dropped exactly `version` + `_version-note` from the template, then remove only those two keys from our config; keep `cwf-version`, `_cwf-version-note`, `versioning`.
- **Actual**: Laid-down template `.cwf/templates/cwf-project.json.template` top-level keys confirmed to contain `cwf-version` + `_cwf-version-note` and **no** `version`/`_version-note`. Applied two non-adjacent edits to `implementation-guide/cwf-project.json`: removed `_version-note` (line 3) and `version: v0.13.0` (line 50). Remaining version-ish keys: `_cwf-version-note`, `cwf-version`, `versioning` (`last_released=v0.13.21`, `major_minor=v0.13`). File validates as JSON.
- **Deviations**: None.

### Step 4: Verify version file + history
- **Planned**: `.cwf/version` pinned to v1.1.189; `git log --merges 82ff991..HEAD` empty.
- **Actual**: `.cwf/version` shows `cwf_version=v1.1.189`, `cwf_ref=v1.1.189`, `cwf_sha=6af636e32ad1ffaebd2601c7101dd46c8a3c30b7`, `cwf_method=read-tree`. `git log --merges 82ff991..HEAD` empty. HEAD unchanged by laydown (still `bd11b749`), confirming read-tree made no commit.
- **Deviations**: None.

### Step 5: Validate
- **Planned**: `.cwf/scripts/cwf-manage validate` exits 0.
- **Actual**: `[CWF] validate: OK`, exit 0. No permission or sha256 violations.
- **Deviations**: None.

### Step 6: Commit
- **Planned**: Stage laydown changes + config + this f-exec file; checkpoint commit with a "why" message.
- **Actual**: See checkpoint commit (Step 9 of the workflow).
- **Deviations**: None.

## Blockers Encountered

None.

## Deferral Check
Before marking status=Finished, verify:
- [x] All steps from d-implementation-plan.md executed
- [x] All success criteria from a-task-plan.md met
- [x] All requirements from b-requirements-plan.md addressed (N/A — chore, no b phase)
- [x] All design guidance in c-design-plan.md followed (N/A — chore, no c phase)
- [x] No planned work deferred without user approval
- [x] If work deferred: Follow-up task created and linked (none deferred)

## Status
**Status**: Finished
**Next Action**: /cwf-testing-exec 22
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Security Review

**State**: no findings

This is a CWF version-upgrade changeset (v1.1.185 → v1.1.189). It's almost entirely documentation, agent/skill frontmatter, a JSON schema cleanup, and CWF planning files. Let me reason through the five threat categories.

The changeset comprises: agent frontmatter changes (`allowed-tools:` → `tools:` plus added `LSP`/`Bash`, and `effort:` keys); doc updates to the tool-tier rubric and shared rules; the `security-review.md` doc itself; `script-hashes.json` SHA updates; removal of vestigial `version`/`_version-note` keys from `cwf-project.json` and its template; deletion of `.cwf/version`; and three new task-22 planning markdown files.

**(a) Bash injection / unsafe command construction.** No shell command construction is added anywhere in this diff. The planning files reference commands (`cwf-manage update`, `git log --merges`, `git rev-parse`) as documentation prose, not as executed interpolations. No `system($string)` or backtick construction is introduced. Clean.

**(b) Perl helpers consuming git/user output without `-z`.** No Perl source is touched. `script-hashes.json` records new SHAs for the agent/doc files but the helper scripts themselves are unchanged. Clean.

**(c) Prompt injection via user-supplied strings.** This is the category with genuine signal, and notably the changeset itself documents it. The most security-relevant change is the agent tool-grant widening: `cwf-security-reviewer-changeset.md` and the four `cwf-plan-reviewer-*.md` agents change from `allowed-tools: Read, Grep, Glob` to `tools: Read, Grep, Glob, LSP, Bash`. The reviewer agents consume untrusted input (plan files, diffs carrying task descriptions and `{arguments}`). Adding `Bash` to agents that ingest untrusted content widens the prompt-injection blast radius from "form a judgement" to "execute commands."

This is not an unflagged defect — the changeset's own `security-review.md` (lines 12, 14) explicitly documents this as a deliberate posture choice, names it as a residual FR4(c) threat, records the mitigations (tool-tier guidance steering to higher tiers; absence of `Edit`/`Write` so no in-place mutation), and notes the follow-up options (narrowing Bash back out, or scoping Skill access via settings-permissions). The threat is acknowledged, scoped, and justified (keeping the markdown-reader Perl-script skill usable). The residual mitigation rests on guidance plus the absence of `Edit`/`Write` rather than on the absence of `Bash` — a weaker control than before, but a documented and intentional trade-off. I record this as the posture change a reader should be aware of, not as an actionable defect to block on: the decision is made, documented, and within scope of an upstream-tracked CWF design.

One thing worth confirming: the diff shows the agent-side frontmatter key changing from `allowed-tools` to `tools`. If the harness only honours `allowed-tools` for restricting an agent's grant, renaming the key could change which tools the agent actually receives. That is a CWF-upstream framing concern, not a defect introduced by this repo's changeset, and `cwf-manage validate` plus the recorded `script-hashes.json` SHAs gate the integrity of these files. Out of scope to block here.

**(d) Unsafe environment-variable handling.** No env-var handling code changes. The `d-implementation-plan.md` planning file actually *strengthens* env-var hygiene by documenting a precondition that `CWF_SOURCE` be unset so the laydown source provably comes from the recorded `cwf_source` (lines 452-454 of the diff). No new env var feeds `chmod`/`rm`/`open`. Clean.

**(e) Pattern-based risks.** The widened `Bash` grant on reviewer agents is the closest fit here, framed properly: it is safe in the current CWF posture because the reviewer agents lack `Edit`/`Write` and the tool-tier guidance steers away from raw Bash; audit future uses where a reviewer agent is granted `Edit`/`Write` alongside `Bash`, or where the tool-tier guidance is removed — at that point untrusted plan/diff content reaching an agent with both command execution and file mutation would be a materially larger injection surface. The mitigation is documented but soft (prompt-level guidance, not a hard allowlist), so any future agent definition copying the `tools: …, Bash` pattern should re-evaluate against that invariant.

The remaining changes — deleting `.cwf/version`, removing two JSON keys, the `effort:` frontmatter additions, and the task-22 planning docs — carry no security surface. The `script-hashes.json` SHA churn is exactly the deterministic-integrity concern that `cwf-manage validate` owns, and per this doc's own boundary note (line 16) raising it here would be noise.

No secrets, credentials, tokens, or auth material appear anywhere in the diff.

Conclusion: no actionable defect was introduced. The one substantive security-relevant change — widening reviewer agents to include `Bash` — is a deliberate, documented posture decision with its residual FR4(c) threat and mitigations spelled out in the changeset itself, plus a noted follow-up. I am surfacing it as an awareness item with the pattern-risk framing for future agent definitions, but it is not a finding that should block this upgrade.

```cwf-review
state: no findings
summary: CWF v1.1.185→v1.1.189 upgrade; reviewer agents widened to include Bash is a deliberate, self-documented FR4(c) posture choice (no Edit/Write, tool-tier guidance) — awareness item, not an introduced defect. No injection/secret/env-var issues.
```

## Lessons Learned
Verifying the laid-down template before editing the config (rather than trusting
the plan's quoted excerpt) is the right default for upgrades carrying a
config-schema change. See j-retrospective.md § Process Learnings.
