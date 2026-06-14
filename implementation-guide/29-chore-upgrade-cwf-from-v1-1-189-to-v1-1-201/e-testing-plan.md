# upgrade CwF from v1.1.189 to v1.1.201 - Testing Plan
**Task**: 29 (chore)

## Task Reference
- **Task ID**: internal-29
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/29-upgrade-cwf-from-v1-1-189-to-v1-1-201
- **Template Version**: 2.1

## Goal
Verify the v1.1.201 laydown completed correctly, preserved linear history, left the
CWF tooling functional in this repo, and that the inter-repo artefact changes
(`.claude/settings.json`, `.gitignore`, `CLAUDE.md`) are exactly the expected
migration — no surprises.

## Test Strategy
### Test Levels
This task ships **no Go code**, so the usual unit/integration pyramid does not
apply. Testing is **outcome verification** of a tool-driven change, in three bands:
- **Upgrade integrity**: version/manifest/perms recorded and self-consistent.
- **Inter-repo contract**: the artefact diffs match the expected migration and
  introduce no unexpected commands/hooks (the security-review surface).
- **Regression**: this repo's own build + Go test suite and CWF workflow tooling
  remain green after the upgrade.

### Test Coverage Targets
- **Critical paths**: 100% of success criteria in `a-task-plan.md` verified.
- **Regression**: full `go test ./pkg/... ./cmd/...` green; `cwf-manage validate`
  and `/cwf-security-check` clean.

## Test Cases
### Functional Test Cases

- **TC-1**: Version recorded
  - **Given**: upgrade run via `cwf-manage update v1.1.201`
  - **When**: read `.cwf/version`
  - **Then**: `cwf_version=v1.1.201`, `cwf_ref=v1.1.201`, `cwf_method=read-tree`,
    `cwf_sha` = the v1.1.201 tag sha; `cwf_source` still the local clone path

- **TC-2**: Integrity validates
  - **Given**: post-upgrade tree
  - **When**: `.cwf/scripts/cwf-manage validate` and `/cwf-security-check`
  - **Then**: both report OK/clean (manifest sha + exact perms, e.g. the new
    `pretooluse-bash-tool-check` hook at `0500`)

- **TC-3**: Linear history preserved
  - **Given**: upgrade committed
  - **When**: `git log --merges e1255c3a..HEAD`
  - **Then**: empty (no merge commit — read-tree laydown)

- **TC-4**: settings.json migration is exactly expected (security surface)
  - **Given**: before/after snapshot of `.claude/settings.json`
  - **When**: diff them
  - **Then**: (a) legacy `PreToolUse` group with `matcher == "UserPromptSubmit"`
    is removed and re-registered as a real `UserPromptSubmit` hook (Task 195);
    (b) rules-inject command is **byte-identical** before vs after;
    (c) exactly one new `PreToolUse`/matcher-`Bash` group for
    `pretooluse-bash-tool-check` (Task 201); (d) no other command/hook entries

- **TC-5**: New Bash hook is fail-open and inert
  - **Given**: the registered `pretooluse-bash-tool-check` hook
  - **When**: run any Bash tool call post-upgrade (e.g. `git status`) and inspect
    `.cwf/tool-check/bash/` for active rules
  - **Then**: Bash calls succeed unimpeded; no active checked-in rules ship
    (only the gitignored `settings.local.json` could carry live rules)

- **TC-6**: No surprise files
  - **Given**: post-upgrade tree
  - **When**: `git status --untracked-files=all`
  - **Then**: only the expected set — relaid `.cwf/**`, refreshed skill/agent/rule
    symlinks, the new hook; nothing outside the Step-3 allowlist

- **TC-7**: `.gitignore` / `CLAUDE.md` touched in CWF-managed region only
  - **Given**: before/after snapshot
  - **When**: diff
  - **Then**: changes confined to the CWF-managed block/preamble; no project lines lost

### Non-Functional Test Cases
- **Regression (reliability)**: `make build` succeeds; `go test ./pkg/... ./cmd/...`
  green — confirms the upgrade touched no Go code paths.
- **Workflow tooling (usability)**: `cwf-status` runs clean; the
  `security-review-changeset` helper resolves the task baseline without error —
  confirms the new CWF version operates in this repo.
- **Security**: covered by the `cwf-security-reviewer-changeset` agent in
  implementation-exec, bounded to the inter-repo surface per `a-task-plan.md`
  (TC-4/TC-5 are the manual counterpart).

## Test Environment
### Setup Requirements
- Local CwF source at `/home/matt/repo/coding-with-files` with tag `v1.1.201`.
- Before/after snapshots of `.claude/settings.json`, `.gitignore`, `CLAUDE.md`
  captured in Step 1 of the implementation plan (prerequisite for TC-4/TC-7).
- Go toolchain (Go 1.25+) for the regression build/test.

### Automation
- No new automated tests added (no code change). Verification is manual +
  existing `go test` suite + `cwf-manage validate` / `/cwf-security-check`.

## Validation Criteria
- [ ] TC-1 … TC-7 all pass
- [ ] `go test ./pkg/... ./cmd/...` green (no regression)
- [ ] `cwf-manage validate` + `/cwf-security-check` clean
- [ ] Inter-repo changeset security review (exec phase) returns no blocking findings

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All planned test cases (TC-1…TC-7) plus both non-functional bands executed in
`g-testing-exec.md` — 100% PASS, zero failures.

## Lessons Learned
TC-4 (settings.json migration exactly expected) was the highest-value case: pinning
the precise expected diff up front meant execution confirmed a byte-for-byte match
rather than re-deriving what "correct" looked like at test time.
