# Upgrade CWF to v1.1.177 via cwf-manage update - Plan
**Task**: 9 (chore)

## Task Reference
- **Task ID**: internal-9
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/9-upgrade-cwf-to-v1-1-177-via-cwf-manage-update
- **Baseline Commit**: 65fd21484eb25d83fa1d42dfdd315ee526599656
- **Template Version**: 2.1

## Goal
Upgrade the installed CWF subtree from v1.1.169 to v1.1.177 using
`.cwf/scripts/cwf-manage update v1.1.177`, leaving the tree validate-clean and
the workflow tooling functional.

## Success Criteria
- [ ] `.cwf/version` records `cwf_version=v1.1.177` and `cwf_sha` =
      `git rev-parse v1.1.177` = the **annotated-tag object SHA**
      `1cae055bf1b52bea0fd9b0cfce63871893757ab7`. (T175's commit-SHA fix is
      **forward-only**: the *outer* cwf-manage doing the authoritative
      `.cwf/version` write is still the pre-T175 v1.1.169 binary — `cwf-manage`
      lines 502–510, `resolve_sha` line 209 with no `^{commit}`. The commit-SHA
      form `ed664b25…` lands only on the *next* update. This inverts the naive
      reading of T175 — verified by reading the installed cwf-manage.)
- [ ] `.cwf/scripts/cwf-manage validate` exits 0 (config + workflow-file +
      script-hash integrity all pass post-laydown).
- [ ] The T176 doc split is present on-disk: `.cwf/docs/workflow/workflow-steps/`
      exists with per-phase files and `workflow-steps.md` is the reduced ToC;
      the `.claude/skills/cwf-*` Step-5 references resolve to readable paths.
- [ ] A workflow smoke check passes under the new version — e.g.
      `task-context-inference` and `context-manager hierarchy 9` still resolve
      this task correctly.
- [ ] Project code is untouched: `git diff --stat` for the upgrade commit shows
      only CWF-managed paths (`.cwf/`, `.claude/`, install scripts) — no `pkg/`,
      `cmd/`, or `go.*` changes.

## Original Estimate
**Effort**: <0.5–1 day
**Complexity**: Low
**Dependencies**:
- `cwf-manage update` requires a working tree with **no uncommitted changes
  under `.cwf/`** (it refuses otherwise) — so the a-plan checkpoint and any
  scratch must be clean/committed before the laydown step.
- Upstream source reachable: `file:///home/matt/repo/coding-with-files` (the
  `cwf_source` in `.cwf/version`); v1.1.177 is already listed by
  `cwf-manage list-releases`.

## Major Milestones
1. **Precondition gate**: capture pre-upgrade HEAD + record current
   `.cwf/version` values as the deterministic revert/compare anchor; confirm
   `.cwf/` is clean.
2. **Laydown**: run `cwf-manage update v1.1.177`; the subtree merge updates
   `.cwf/`, `.claude/`, and install scripts in one shot.
3. **Verify**: `cwf-manage validate` clean; `.cwf/version` fields correct
   (incl. the T175 commit-SHA); T176 doc split present; workflow smoke check.
4. **Land**: checkpoint + squash on the chore branch, stacked on Task 8.

## Risk Assessment
### High Priority Risks
- **Mid-laydown abort leaves a half-applied subtree** (the failure mode Task 5
  hit at v1.1.163's `rules-inject`).
  - **Mitigation**: Milestone 1 captures pre-upgrade HEAD as the revert target;
    on abort, recover non-destructively via soft-reset + targeted checkout +
    `git checkout -- .cwf/version` (no `git reset --hard`, which the user has
    denied before). v1.1.177 is past the T167 fix, so the specific rules-inject
    defect should not recur — but the escape hatch stays in the d-plan.

### Medium Priority Risks
- **Verification asserts the wrong `cwf_sha` semantics.** T175 ships *in* this
  span but is **forward-only** — the authoritative writer for *this* upgrade is
  the pre-T175 outer cwf-manage (v1.1.169), which records the **tag-object**
  SHA. A plan that assumed the new `^{commit}` form (`ed664b25…`) would flash a
  false failure; equally, a future task must expect the commit form once
  v1.1.177's cwf-manage is the writer.
  - **Mitigation**: success criterion + testing plan assert
    `cwf_sha == git rev-parse v1.1.177` (tag-object `1cae055…`) for this
    upgrade, with a noted expectation flip on the next one. Derivation verified
    against `cwf-manage` lines 209/502–510, not the T175 headline.
- **The T176 split changes where skills read phase guidance.** Stale references
  or a missed file in the laydown would only surface when a skill runs.
  - **Mitigation**: testing plan includes a post-upgrade workflow smoke check
    that exercises a Step-5 doc read path, not just a static file-exists check.
- **`script-hashes.json` drift / perms floor.** Post-laydown integrity must be
  exact, not just ceiling-clean (cf. T175's TC-8 perms-floor lesson).
  - **Mitigation**: rely on `cwf-manage validate` (the laydown refreshes hashes);
    if it reports fixable perm drift, use `cwf-manage fix-security` (not manual
    chmod guesswork) and re-validate.

## Dependencies
- Upstream CWF repo at `file:///home/matt/repo/coding-with-files` with the
  `v1.1.177` tag (confirmed available).
- Clean `.cwf/` working tree at laydown time.

## Constraints
- Subtree install method (`cwf_method=subtree`) — upgrade is a subtree merge via
  `cwf-manage`, not a re-clone.
- CWF lives only on the `local-*` line; this upgrade does not touch the
  CWF-free public `main` branch (per CLAUDE.md branch policy).
- Use `cwf-manage` for the mechanism (user-specified); do not hand-edit
  `.cwf/version` or hand-apply the subtree.

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [ ] **Time**: Will this take >1 week? No (<1 day).
- [ ] **People**: Does this need >2 people? No.
- [ ] **Complexity**: 3+ distinct concerns? No — one mechanism (cwf-manage
  update) with a verify step.
- [ ] **Risk**: High-risk components needing isolation? No — the one real risk
  (mid-laydown abort) is handled by the precondition gate, not decomposition.
- [ ] **Independence**: Can parts be worked separately? No.

**Decision**: 0 signals triggered — single chore, no decomposition.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All five success criteria met. `.cwf/version` records `cwf_version=v1.1.177` and
`cwf_sha=1cae055bf1b52bea0fd9b0cfce63871893757ab7` (the tag-object SHA, exactly as
the forward-only T175 analysis predicted — **not** the commit `ed664b25…`).
`cwf-manage validate` exit 0; T176 doc-split present (`workflow-steps/` + 10
per-phase files, `workflow-steps.md` reduced to ToC); workflow smoke check green
(`task-context-inference`, `context-manager hierarchy 9` both resolve task 9);
project code untouched (`git diff --stat cc3ee2c..HEAD -- pkg cmd go.mod go.sum`
empty). 0 decomposition signals held — single chore, no subtasks needed.

## Lessons Learned
The Medium-priority risk "verification asserts the wrong `cwf_sha` semantics" was
the crux of the task and was retired by source-reading, not by trusting the T175
headline. Front-loading that into the plan made exec a confirmation pass.
