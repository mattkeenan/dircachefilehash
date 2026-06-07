# Upgrade CWF to v1.1.183 - Plan
**Task**: 14 (chore)

## Task Reference
- **Task ID**: internal-14
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/14-upgrade-cwf-to-v1-1-183
- **Baseline Commit**: c3ccca52a554417205683891608098b1b7c9f9bf
- **Template Version**: 2.1

## Goal
Upgrade the installed CWF subtree from v1.1.177 to v1.1.183 using
`.cwf/scripts/cwf-manage update v1.1.183`, leaving the tree validate-clean, the
workflow tooling functional, and the newly-injected PreToolUse hooks merged
into `.claude/settings.json` without surprising the active workflow.

## Success Criteria
- [ ] `.cwf/version` records `cwf_version=v1.1.183` and `cwf_sha` =
      **commit-object SHA** `faf92479fac564f241ce10afb8ec00c986ad37f1` — **not**
      the annotated-tag-object SHA `1842f7b04e0819364529ae123f3365a69b56b99e`.
      This is the semantics flip Task 9 predicted: the authoritative writer for
      *this* upgrade is the **v1.1.177** cwf-manage, whose `resolve_sha`
      (installed `cwf-manage` line 225) already uses `$ref^{commit}` (the T175
      fix). Verified by reading the installed binary, not by trusting headlines.
- [ ] `.cwf/scripts/cwf-manage validate` exits 0 (config + workflow-file +
      script-hash + perms integrity all pass post-laydown).
- [ ] The two new PreToolUse hooks are present on-disk
      (`.cwf/scripts/hooks/pretooluse-planning-write-guard`,
      `.cwf/scripts/hooks/pretooluse-sandbox-logging`) and the project
      `.claude/settings.json` is still well-formed JSON after the +260-line
      `cwf-claude-settings-merge` runs — with the new guards **inert by default**
      (`sandbox.enabled=false`, `planning-write-guard=off`), so no write is
      unexpectedly blocked mid-workflow.
- [ ] A workflow smoke check passes under the new version — e.g.
      `task-context-inference` and `context-manager hierarchy 14` still resolve
      this task correctly, and a planning-phase file write is *not* blocked.
- [ ] Project code is untouched: `git diff --stat` for the upgrade commit shows
      only CWF-managed paths (`.cwf/`, `.claude/`, install scripts) — no `pkg/`,
      `cmd/`, or `go.*` changes.

## Original Estimate
**Effort**: <0.5–1 day
**Complexity**: Low
**Dependencies**:
- `cwf-manage update` requires **no uncommitted changes under `.cwf/`** (it
  refuses otherwise) — the a-plan checkpoint and any scratch must be
  clean/committed before laydown.
- Upstream source reachable: `file:///home/matt/repo/coding-with-files` (the
  `cwf_source` in `.cwf/version`); v1.1.183 is already listed by
  `cwf-manage list-releases`.

## Major Milestones
1. **Precondition gate**: capture pre-upgrade HEAD + record current
   `.cwf/version` values as the deterministic revert/compare anchor; confirm
   `.cwf/` is clean.
2. **Laydown**: run `cwf-manage update v1.1.183`; the subtree merge updates
   `.cwf/`, `.claude/` (incl. settings-merge of the new hooks), and install
   scripts in one shot.
3. **Verify**: `cwf-manage validate` clean; `.cwf/version` fields correct (incl.
   the **commit-SHA** form); new hooks present + settings.json well-formed +
   guards inert; workflow smoke check incl. a planning-write path.
4. **Land**: checkpoint + squash on the chore branch, stacked on local-main.

## Risk Assessment
### High Priority Risks
- **Mid-laydown abort leaves a half-applied subtree** (the failure mode Task 5
  hit at v1.1.163's `rules-inject`).
  - **Mitigation**: Milestone 1 captures pre-upgrade HEAD as the revert target;
    on abort, recover non-destructively via soft-reset + targeted checkout +
    `git checkout -- .cwf/version` (no `git reset --hard`, which the user has
    denied before). v1.1.183 is well past the T167 rules-inject fix, so that
    specific defect should not recur — but the escape hatch stays in the d-plan.

### Medium Priority Risks
- **The new settings-merge injects PreToolUse hooks into the project's
  `.claude/settings.json`.** The `cwf-claude-settings-merge` helper grew +260
  lines and now wires in `pretooluse-planning-write-guard` and
  `pretooluse-sandbox-logging`. A bad merge could corrupt project settings or a
  surprise-active guard could block legitimate writes mid-workflow.
  - **Mitigation**: upstream defaults make the guards inert
    (`sandbox.enabled=false`, `planning-write-guard=off` in the v1.1.183
    `cwf-project.json` template — verified by reading the upstream diff).
    Testing plan asserts `.claude/settings.json` parses as JSON post-laydown and
    exercises a planning-write path to prove nothing is silently blocked.
- **`cwf_sha` semantics flip mis-asserted.** This is the *inverse* of Task 9's
  expectation: Task 9 correctly recorded the **tag-object** SHA because its
  writer was the pre-T175 v1.1.169 binary; *this* upgrade's writer is the
  post-T175 v1.1.177 binary, so it records the **commit** SHA `faf92479…`. A
  plan that copied Task 9's tag-object assertion would flash a false failure.
  - **Mitigation**: success criterion + testing plan assert
    `cwf_sha == git rev-parse v1.1.183^{commit}` (`faf92479…`), derived from the
    installed `cwf-manage` line 225, not from the changelog.
- **Project `cwf-project.json` may need the new `sandbox` block.** Upstream added
  a `sandbox` stanza to its own project config; the new hook code may read it. If
  laydown does not add it to *our* `implementation-guide/cwf-project.json`, the
  guards should still default-off, but this needs an explicit check.
  - **Mitigation**: design/testing plan verifies whether the block is present or
    needed, and that absence does not enable a guard.
- **`script-hashes.json` drift / perms floor.** Post-laydown integrity must be
  exact, not just ceiling-clean (cf. T175's perms-floor lesson). T183 itself is
  "permission-drift repair" — extra reason to lean on the tool.
  - **Mitigation**: rely on `cwf-manage validate`; if it reports fixable perm
    drift, use `cwf-manage fix-security` (not manual chmod) and re-validate.

## Dependencies
- Upstream CWF repo at `file:///home/matt/repo/coding-with-files` with the
  `v1.1.183` tag (confirmed available via `list-releases`).
- Clean `.cwf/` working tree at laydown time.

## Constraints
- Subtree install method (`cwf_method=subtree`) — upgrade is a subtree merge via
  `cwf-manage`, not a re-clone.
- CWF lives only on the `local-*` line; this upgrade does not touch the CWF-free
  public `main` branch (per CLAUDE.md branch policy).
- Use `cwf-manage` for the mechanism; do not hand-edit `.cwf/version` or
  hand-apply the subtree.

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [ ] **Time**: Will this take >1 week? No (<1 day).
- [ ] **People**: Does this need >2 people? No.
- [ ] **Complexity**: 3+ distinct concerns? No — one mechanism (cwf-manage
  update) plus a verify step; the settings-merge is part of the same laydown.
- [ ] **Risk**: High-risk components needing isolation? No — the one real risk
  (mid-laydown abort) is handled by the precondition gate, not decomposition.
- [ ] **Independence**: Can parts be worked separately? No.

**Decision**: 0 signals triggered — single chore, no decomposition.

## Status
**Status**: Finished
**Next Action**: /cwf-requirements-plan 14
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All five success criteria met. `.cwf/version` records `cwf_version=v1.1.183`,
`cwf_ref=v1.1.183`, and `cwf_sha=faf92479fac564f241ce10afb8ec00c986ad37f1` (the
**commit** SHA, exactly as the inverse-of-Task-9 analysis predicted — **not** the
tag-object `1842f7b0…`). `cwf-manage validate` exit 0 on first run (no
fix-security pass — the laydown's exact-perms pass clamped the floor-drift).
Both new hooks present on-disk; `.claude/settings.json` well-formed and inert —
in fact the settings-merge added 0 hook entries, so the hooks are unregistered
(strictly safer than the planned "registered but off"). Workflow smoke check
green (`task-context-inference`, `context-manager hierarchy 14`,
`workflow-manager control`, `backlog-manager validate` all exit 0; planning-write
unblocked). Project code untouched (no `pkg/`/`cmd/`/`go.*` in the changeset). 0
decomposition signals held — single chore.

## Lessons Learned
The crux risk ("`cwf_sha` semantics flip mis-asserted") was retired by
source-reading the installed `cwf-manage` `resolve_sha` before exec, exactly as
in Task 9 — but with the opposite conclusion (commit form, because the writer is
now post-T175). The medium "settings-merge injects hooks" risk did not fire and
failed safe (0 hook entries). See j-retrospective.md.
