# Upgrade CWF to v1.1.177 via cwf-manage update - Testing Plan
**Task**: 9 (chore)

## Task Reference
- **Task ID**: internal-9
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/9-upgrade-cwf-to-v1-1-177-via-cwf-manage-update
- **Template Version**: 2.1

## Goal
Verify the v1.1.169→v1.1.177 upgrade landed cleanly: correct version + **tag-object**
SHA recorded, integrity clean under T170's perms-ceiling, all symlinks resolve, the
T176 doc-split is present and the skills can read phase guidance, the workflow tooling
still runs, and the revert path is available.

## Test Strategy
Operational upgrade with **no project source edits** → no new unit/integration tests.
The strategy is **acceptance verification**: each `a-task-plan.md` success criterion
maps to one concrete post-upgrade check, plus a revert-path smoke test and a
deterministic half-applied-state probe. CWF's own Perl suite runs in the CWF source
repo, not here — out of scope.

### Test Levels
- **Unit**: N/A — no new code
- **Integration**: N/A — no new code
- **System / Acceptance**: TC-1..TC-8 below, against the upgraded install
- **Regression**: re-run the helpers tasks 1–8 depend on; confirm no behavioural break

### Coverage Targets
- 100% of `a-task-plan.md` success criteria mapped to an executable check
- Both outcomes covered: success path (TC-1..6, TC-8) **and** failure/revert path (TC-7)
- The two span-specific behaviour changes that touch verification — T175 (cwf_sha,
  forward-only) and T176 (doc-split) — each have a dedicated assertion (TC-1, TC-5)

## Test Cases
### Functional Test Cases
- **TC-1: Version + SHA recorded correctly (T175 forward-only)**
  - **Given**: upgrade run via `cwf-manage update v1.1.177`
  - **When**: `cwf-manage status` and `cat .cwf/version`
  - **Then**: `Version: v1.1.177`; `cwf_sha` == `1cae055bf1b52bea0fd9b0cfce63871893757ab7`
    (the **annotated-tag object** returned by `git rev-parse v1.1.177`, **not** the
    dereferenced commit `ed664b25541f0ae35de09633fe5155c500a502bc`) — because the
    authoritative writer is the outer pre-T175 v1.1.169 cwf-manage; `cwf_ref` ==
    `v1.1.177` (flipped from `HEAD`/`v1.1.169`); `cwf_version` == `v1.1.177`
    (`git_describe_version` on the tag-object SHA resolves to the tag)

- **TC-2: Integrity clean under T170 perms-ceiling**
  - **Given**: completed laydown
  - **When**: `cwf-manage validate`
  - **Then**: exit 0, zero violations. T170 now enforces recorded perms as an
    **upper bound**, so validate may newly flag any file whose on-disk mode
    *exceeds* its recorded value; if only **fixable perms** remain, `cwf-manage
    fix-security` (run **once**) clears them and a re-run is exit 0. If validate is
    still dirty after one fix-security pass → failed laydown, revert (TC-7), do not
    loop. Any new hierarchy/template-ref advisory is informational on historical
    content (BACKLOG/CHANGELOG/older tasks), blocking only on
    `implementation-guide/9-.../`

- **TC-3: Skill symlinks resolve**
  - **Given**: laydown recreated `.claude/skills/cwf-*`
  - **When**: `for l in .claude/skills/cwf-*; do [ -e "$l" ] || echo "BROKEN: $l"; done`
  - **Then**: no `BROKEN:` output; every symlink points at an existing
    `.cwf-skills/cwf-*` target

- **TC-4: Agent defs resolve**
  - **Given**: agent laydown
  - **When**: `for l in .claude/agents/cwf-*; do [ -e "$l" ] || echo "BROKEN: $l"; done`
    and inspect `.cwf-agents/`
  - **Then**: all present/resolving; the changeset reviewer definition still carries
    its ` ```cwf-review ` verdict-block contract (already present at v1.1.169;
    confirms the agent set survived the laydown intact)

- **TC-5: T176 doc-split present AND readable by skills**
  - **Given**: v1.1.177 laydown (T176 split shipped via recursive `.cwf/` copy)
  - **When**: `test -d .cwf/docs/workflow/workflow-steps` and list it; check a
    representative per-phase file is readable (`test -s
    .cwf/docs/workflow/workflow-steps/planning.md`); confirm `workflow-steps.md`
    is the reduced ToC (`grep -q '## Steps' .cwf/docs/workflow/workflow-steps.md`)
  - **Then**: the `workflow-steps/` dir exists with per-phase files
    (`planning.md`, `implementation-planning.md`, `testing-planning.md`, …), each
    non-empty; `workflow-steps.md` retained as a ToC. This is the functional proof
    that the plan-phase SKILLs' Step-5 reads will resolve post-upgrade (not just a
    file-exists check)

- **TC-6: Workflow tooling regression (tasks 1–8 dependencies)**
  - **Given**: upgraded install
  - **When**: `task-context-inference`, `context-manager hierarchy 9`,
    `workflow-manager status 9 --workflow`, `backlog-manager validate`
  - **Then**: each exits 0. Rubric: non-zero exit = blocker; new informational
    warnings against historical content = expected. `task-context-inference`
    resolves task 9 / step `e-testing-plan` (or later) — exercises T171's
    completed-task recency exclusion as a regression check; `context-manager
    hierarchy 9` reports the task-9 chore dir; `backlog-manager validate` clean

- **TC-7: Revert path is clean (negative / safety)**
  - **Given**: the pre-upgrade HEAD captured in f-implementation-exec.md Step 1
    (d-plan precondition gate)
  - **When**: (only if the upgrade must be abandoned) `git reset --soft
    <pre-upgrade-HEAD> && git restore --staged . && git checkout -- .cwf .cwf-skills
    .cwf-rules .cwf-agents .claude && git clean -fdx --dry-run -- .cwf .cwf-skills
    .cwf-rules .cwf-agents .claude/skills .claude/agents` (read output, confirm,
    then re-run without `--dry-run`); remove `.cwf/.update.lock` only per the flock
    rule
  - **Then**: tree returns to v1.1.169 with the Task 9 planning commits (a/d/e)
    intact; `cwf-manage status` reports v1.1.169; `git status --untracked-files=all`
    shows only the expected uncommitted f/g/j templates. The documented escape
    hatch for any laydown abort

- **TC-8: Half-applied state probe (negative / safety)**
  - **Given**: an aborted laydown where the tree is at v1.1.177 but `.cwf/version`
    may still read v1.1.169 (perms-pass aborts *before* the version write —
    `cwf-manage` line 500 before 510)
  - **When**: `grep ^cwf_version .cwf/version` AND `test -d
    .cwf/docs/workflow/workflow-steps`
  - **Then**: a half-applied state is identified deterministically when version
    still reads `v1.1.169` AND the known-v1.1.177-only `workflow-steps/` dir
    (added by T176) is present. The discriminative signal triggers the revert path
    (TC-7), not a fix-forward — `fix-security` is clamp-only and cannot complete the
    version pin

### Non-Functional Test Cases
- **Integrity/Security**: covered by TC-2 (sha256 + T170 perms-ceiling via
  `cwf-manage validate`) and the exec-phase changeset security review. **Note T174**:
  the review helper now scopes *all* changed files, not just CWF-internal — for this
  CWF-metadata-only chore the changeset is still CWF files, so the review is likely
  `no findings`, but the scoping behaviour itself is now broader
- **Reliability**: covered by TC-7 (atomic revert via soft-reset + checkout + clean)
  and TC-8 (deterministic half-applied detection; no fix-forward)
- **Performance/Usability**: N/A for a version bump

## Test Environment
### Setup Requirements
- CWF source clone at `/home/matt/repo/coding-with-files` with tag `v1.1.177` present
  (confirmed via `cwf-manage list-releases`)
- Clean working tree under `.cwf/`, `.cwf-skills/`, `.cwf-rules/`, `.cwf-agents/`
  before update (`cwf-manage update` refuses otherwise)
- Pre-upgrade repo HEAD recorded into f-implementation-exec.md before the update runs
  (TC-7 dependency; d-plan Step 1 precondition gate)
- No stale `.cwf/.update.lock` (d-plan Step 1 flock rule)

### Automation
- No CI integration; all checks are manual one-liners run in `g-testing-exec`
- No test doubles — checks run against the real upgraded install

## Validation Criteria
- [ ] TC-1 — version `v1.1.177`, `cwf_sha` == tag-object `1cae055…` (NOT commit
      `ed664b25…`), `cwf_ref` flipped to `v1.1.177`, `cwf_version` == `v1.1.177`
- [ ] TC-2 — `validate` exit 0 (after at most one `fix-security` pass under T170)
- [ ] TC-3 — skill symlinks resolve
- [ ] TC-4 — agent defs resolve; verdict-block contract intact
- [ ] TC-5 — `workflow-steps/` dir present with non-empty per-phase files;
      `workflow-steps.md` is the ToC
- [ ] TC-6 — workflow helpers exit 0 with rubric-correct outputs; task 9 resolves
- [ ] TC-7 — revert path verified available (executed only if upgrade abandoned)
- [ ] TC-8 — half-applied probe deterministic (executed only if a laydown aborts)

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Executed in g-testing-exec.md. TC-1..6 all PASS (version+tag-object SHA, validate
clean under T170 ceiling, skill/agent symlinks, T176 split present-and-readable,
workflow tooling regression). TC-7 (revert) and TC-8 (half-applied probe) N/A —
the upgrade applied cleanly so neither safety case triggered; both procedures
remain documented for future aborts. Security review recorded `error` in both
exec phases (changeset cap exceeded — the documented contract for a CWF-vendored
subtree upgrade with no `max-lines-exclude-paths`).

## Lessons Learned
TC-5 deliberately checked readability (`test -s` on a per-phase file), not just
existence — the right call for a doc-split (T176) where the failure mode is a
skill's Step-5 read resolving to an empty/missing file. TC-8's marker
(`workflow-steps/` present) doubled as the half-applied discriminator.
