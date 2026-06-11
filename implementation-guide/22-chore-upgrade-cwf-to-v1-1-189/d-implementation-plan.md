# upgrade CwF to v1.1.189 - Implementation Plan
**Task**: 22 (chore)

## Task Reference
- **Task ID**: internal-22
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/22-upgrade-cwf-to-v1-1-189
- **Template Version**: 2.1

## Goal
Lay down CWF v1.1.189 via `cwf-manage update`, reconcile the task-188
`cwf-project.json` schema change, and verify version-file/no-merge/validate —
on a clean, linear task branch.

## Workflow
Patterns first → Test → Minimal impl → Refactor green → Commit message explains "why"

## Mechanism (verified against `.cwf/scripts/cwf-manage`)
- `cmd_update` (line 446) clones the source, resolves the ref, and **delegates
  laydown to the target ref's `install.bash`** with `CWF_METHOD=read-tree`
  (our recorded method), `CWF_REF=<resolved sha>`. read-tree lays files into the
  working tree — **no commit, hence no merge commit** is created by the tool.
- After laydown it runs artefact apply, settings merge, exact-perms, then writes
  `.cwf/version` authoritatively (lines 555-561): `cwf_version` =
  `git describe` of the SHA (→ `v1.1.189`), `cwf_method=read-tree`,
  `cwf_ref=v1.1.189`, `cwf_sha=<sha>`.
- `check_clean_tree` (line 151) is scoped to `.cwf .cwf-skills .cwf-rules
  .cwf-agents` only — untracked `implementation-guide/` wf files do **not** block
  the update.
- `CWF::Validate::Config` checks the `versioning` block but **not** the
  top-level `version` field, so removing the vestigial `version`/`_version-note`
  is safe and keeping it would also pass — it is alignment cleanup, not a
  validate requirement.

## Files to Modify
### Primary Changes (tool-produced — laid down by `cwf-manage update`)
- `.cwf/version` - version fields bumped to v1.1.189 (sha `6af636e…`).
- `.cwf/**` - subtree content for v1.1.189 (docs, agent/skill rules, templates,
  `.cwf/security/script-hashes.json`).
- `.cwf-skills/`, `.cwf-rules/`, `.cwf-agents/`, `.claude/` symlinks/artefacts -
  refreshed by install.bash + artefact apply (whatever the laydown changes).

### Supporting Changes (manual, by us)
- `implementation-guide/cwf-project.json` - remove the vestigial top-level
  `version` (`v0.13.0`) and `_version-note` lines to match the task-188 schema;
  leave `versioning` and all other keys untouched.

### Workflow files (per-phase checkpoint commits)
- `implementation-guide/22-.../{a,d,e,f,g,j}-*.md`

## Implementation Steps
### Step 1: Preconditions
- [ ] On branch `chore/22-upgrade-cwf-to-v1-1-189` (baseline = pre-task trunk
      tip `82ff991`; HEAD will already carry the a/d/e planning checkpoints).
- [ ] `.cwf .cwf-skills .cwf-rules .cwf-agents` are clean
      (`git status --porcelain -- .cwf .cwf-skills .cwf-rules .cwf-agents` empty).
- [ ] `CWF_SOURCE` is unset in the executing shell, so the laydown content
      provably comes from the recorded `cwf_source` in `.cwf/version` and not an
      environment override (`resolve_source` prefers `CWF_SOURCE`, cwf-manage:144).
- [ ] Source repo has tag `v1.1.189` (verified: dereferenced commit SHA
      `6af636e32ad1ffaebd2601c7101dd46c8a3c30b7`).
- [ ] Record pre-upgrade `.cwf/version` for the before/after diff.

### Step 2: Lay down v1.1.189
- [ ] Run `.cwf/scripts/cwf-manage update v1.1.189`.
- [ ] Command exits 0; capture its log output.

### Step 3: Reconcile cwf-project.json (task-188)
Task-188 (`c5797a3`) removed **exactly** the top-level `version` and
`_version-note` keys from the template and **deliberately kept** `cwf-version`
and `_cwf-version-note` (deferred to a follow-up). Confirm this empirically
rather than from this plan's excerpt:
- [ ] After laydown, diff the now-current `.cwf/templates/cwf-project.json.template`
      to confirm task-188 dropped precisely `version` + `_version-note` (and no
      others) from the top-level keys.
- [ ] Remove **only** those two keys from `implementation-guide/cwf-project.json`:
      `_version-note` (file line ~3, top, beside `_cwf-version-note`/`cwf-version`)
      and `version` (file line ~50, beside `title`/`versioning`). These are
      **not** adjacent — two separate edits.
- [ ] **Leave untouched**: `cwf-version`, `_cwf-version-note` (task-188 kept
      them), the `versioning` block (our dircachefilehash version, not CWF's),
      and every other key.
- [ ] Confirm the file is still valid JSON (`python3 -m json.tool` or `jq .`).

### Step 4: Verify version file + history
- [ ] `.cwf/version` shows `cwf_version=v1.1.189`, `cwf_ref=v1.1.189`,
      `cwf_sha=6af636e32ad1ffaebd2601c7101dd46c8a3c30b7`, `cwf_method=read-tree`.
      Note: `v1.1.189` is an **annotated** tag — bare `git rev-parse v1.1.189`
      returns the tag-object SHA (`ba42ab1…`); the recorded `cwf_sha` is the
      dereferenced **commit** (`6af636e…`, via `resolve_sha`'s `^{commit}`). Do
      not raise a blocker on that difference.
- [ ] `git log --merges 82ff991..HEAD` is empty (will stay empty — laydown makes
      no commit; our checkpoint commits are linear).

### Step 5: Validate
- [ ] `.cwf/scripts/cwf-manage validate` exits 0. This runs the **post-laydown
      v1.1.189** validator (the one that gates), not the v1.1.185 one. A
      **permission** violation → `cwf-manage fix-security` on sight; a
      **sha256** violation → surface as a blocker, do not smooth.

### Step 6: Commit
- [ ] Stage all laydown changes (`.cwf`, `.cwf-skills`, `.cwf-rules`,
      `.cwf-agents`, `.claude`, `implementation-guide/cwf-project.json`) plus the
      f-exec wf file; commit via the f-phase checkpoint with a "why" message.
      (Tool-produced laydown changes land in this task, per
      `[[commit_tool_produced_changes]]`.)

## Code Changes
Two **separate, non-adjacent** deletions in `implementation-guide/cwf-project.json`.
The four version-ish keys are easy to confuse; remove only the two task-188 dropped.

### Edit A — top of file (remove `_version-note` only; keep its neighbours)
Before:
```json
  "_cwf-version-note" : "Should match your project version for consistency",
  "_version-note" : "Use git describe --tags --always format for version tracking",
  "cwf-version" : "HEAD",
```
After:
```json
  "_cwf-version-note" : "Should match your project version for consistency",
  "cwf-version" : "HEAD",
```

### Edit B — near `versioning` (remove `version` only)
Before:
```json
  "title" : "Coding with Files Project Configuration",
  "version" : "v0.13.0",
  "versioning" : {
    "last_released" : "v0.13.21",
    "major_minor" : "v0.13"
  },
```
After:
```json
  "title" : "Coding with Files Project Configuration",
  "versioning" : {
    "last_released" : "v0.13.21",
    "major_minor" : "v0.13"
  },
```
`cwf-version`, `_cwf-version-note`, and the `versioning` block are all retained.

## Test Coverage
**See e-testing-plan.md for complete test plan**

## Validation Criteria
**See e-testing-plan.md for validation criteria and test results**

## Scope Completion
**IMPORTANT**: Complete all planned implementation before marking task Finished.
No deferral planned — this is a single mechanical upgrade plus one config edit.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-plan 22
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All six steps executed as planned, no deviations — see f-implementation-exec.md
for per-step actuals. The two non-adjacent JSON edits (Edit A / Edit B) landed
correctly; the empirical template diff confirmed task-188 dropped exactly the two
keys before editing.

## Lessons Learned
The plan-review-driven split of the config edit into two non-adjacent edits, plus
the instruction to re-derive the task-188 delta from the laid-down template at
exec time, both proved correct. See j-retrospective.md.
