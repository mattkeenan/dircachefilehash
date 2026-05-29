# upgrade CWF to v1.1.169 - Plan
**Task**: 5 (chore)

## Task Reference
- **Task ID**: internal-5
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/5-upgrade-cwf-to-v1-1-169
- **Baseline Commit**: 07366adc9f78c1f2db6f3e65cf4b28eb0df23f06
- **Template Version**: 2.1

## Goal
Upgrade the CWF subtree install in this repo from v1.1.155 to v1.1.169, keeping the
`.cwf/` core, `.cwf-skills/` skills, and `.claude/skills/cwf-*` symlinks consistent and
validated. **Pivot note**: this task originally targeted v1.1.163 and hit the
`rules-inject` manifest/subtree packaging conflict (now fixed by upstream Task 167 in
v1.1.167). Repointing to v1.1.169 removes that blocker by construction.

## Success Criteria
- [ ] `cwf-manage status` reports **Version: v1.1.169** with the matching upstream SHA in `.cwf/version`
- [ ] `cwf-manage validate` exits 0 (config, workflow files, and security hashes intact)
- [ ] Every `.claude/skills/cwf-*` symlink resolves to an existing `.cwf-skills/cwf-*` target (no broken or orphaned links after rename)
- [ ] A representative helper still runs cleanly (`workflow-manager status`, `backlog-manager validate --all`, `task-context-inference`) — no regression in tooling used by tasks 1–4
- [ ] Upgrade committed on the task branch; working tree clean afterwards

## Original Estimate
**Effort**: <0.5 day
**Complexity**: Low
**Dependencies**: CWF source clone at `/home/matt/repo/coding-with-files` with tag `v1.1.169` (confirmed present; fetched 2026-05-29)

## Major Milestones
1. **Pre-flight**: Confirm clean `.cwf/` + `.cwf-skills/` + `.cwf-rules/` + `.cwf-agents/` working tree; review CHANGELOG v1.1.156→v1.1.169 for breaking changes (14 tasks: 156–169, including 167's `rules-inject` removal and 168's security-review cap change)
2. **Upgrade**: `.cwf/scripts/cwf-manage update v1.1.169` (subtree path delegates laydown to the target's install.bash)
3. **Validate**: `status` + `validate` (+ `fix-security` only if needed); smoke-test helpers; commit
4. **Record**: capture the v1.1.163 attempt as a lesson in the retrospective so the workflow remembers why the pivot happened

## Risk Assessment
### High Priority Risks
- **Stale updater wrapper**: the installed v1.1.155 `cwf-manage` predates Tasks 158, 159, 161, 167. For the **subtree** install method the wrapper delegates laydown to the *target's* `scripts/install.bash` (`cwf-manage:406-427`), so most fixes ship in-band. The wrapper still owns ref resolution, the update lock, the post-laydown `cwf-apply-artefacts` pass, the settings-merge, and the authoritative version write — and v1.1.155's `cwf-apply-artefacts` is the one that aborted on `rules-inject` in our v1.1.163 attempt.
  - **Mitigation**: Task 167 (in v1.1.167) drops the `rules-inject` manifest entry, so the v1.1.155 wrapper sees no rules-inject artefact in the *new* manifest after install.bash runs — the conflict class is gone. If a different artefact conflicts non-interactively, set `CWF_UPGRADE_RESOLVE=keep` and re-run. Baseline `07366ad` plus the recorded pre-upgrade HEAD allow a full revert.

### Medium Priority Risks
- **Source ahead of target**: the source repo HEAD may be ahead of v1.1.169. A bare `update`/`pull` could grab the wrong version.
  - **Mitigation**: Pin the explicit tag `v1.1.169`; verify `.cwf/version` SHA after. (FR1 caveat from v1.1.155 still applies — `cwf_version` records the resolved ref verbatim, which is only a valid semver string when the ref *is* a semver tag.)
- **Orphaned skill / rule / agent symlinks**: skill or agent renames across 14 versions leave dangling `.claude/skills/cwf-*` or `.claude/agents/cwf-*` links.
  - **Mitigation**: v1.1.169 ships `cwf-check-tree-symlinks` (T161); `cwf-manage validate` reports dangling links and `fix-security` repairs perms. Recreate by hand only if validate flags a link.
- **Security-review cap behaviour change (T168)**: the exec-phase security-review-changeset cap now weights production code differently than v1.1.155. Our chore changes only workflow MD files (no production code), so this is informational rather than blocking, but the verdict-line format and threshold may differ from prior tasks.
  - **Mitigation**: read the helper output verbatim; no exec-phase decision rests on the cap value for a docs-only chore.
- **Security-hash drift on partial laydown**: a half-applied update can leave script-hash mismatches.
  - **Mitigation**: `cwf-manage validate`; on a *post-laydown* abort (validate failing after a complete laydown) revert per the recorded pre-upgrade HEAD rather than fixing forward.

## Dependencies
- CWF source repo present locally with the `v1.1.169` tag
- Clean working tree under `.cwf/`, `.cwf-skills/`, `.cwf-rules/`, `.cwf-agents/` (update refuses otherwise)

## Constraints
- Subtree install method must be preserved (subtree commands / equivalent, not an ad-hoc copy that breaks history)
- No commit-hook bypass; British spelling in prose

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [ ] **Time**: Will this take >1 week? No — under half a day
- [ ] **People**: Does this need >2 people? No — single operator
- [ ] **Complexity**: 3+ distinct concerns? No — one concern (version bump + relink + validate)
- [ ] **Risk**: High-risk components needing isolation? No — fully revertible via recorded HEAD
- [ ] **Independence**: Can parts be worked on separately? No — sequential single flow

**Decision**: No decomposition — 0/5 signals triggered.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
*To be filled upon completion*

## Lessons Learned
- **Pivot rationale (pre-recorded)**: the v1.1.163 attempt aborted in `cwf-apply-artefacts` because v1.1.163's manifest carries a `rules-inject` artefact whose `source` is an empty placeholder while the subtree ships the file populated (331b). Non-interactive runs see a 3-way conflict (baseline ≠ on-disk ≠ new) and abort. Upstream Task 167 (v1.1.167) recognised this as a manifest defect — the file was never CWF-owned in the first place — and dropped the artefact entry entirely, plus added INV-1/INV-2 invariants in the upstream test suite to keep dual-distribution from re-emerging. Repointing here to v1.1.169 inherits the fix.
- Detailed lessons (the rev-parse vs rev-list cwf_sha discrepancy, the half-applied recovery path) captured in the retrospective.
