# Triage deferred gosec findings - Plan
**Task**: 6 (discovery)

## Task Reference
- **Task ID**: internal-6
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: discovery/6-triage-deferred-gosec-findings
- **Baseline Commit**: e4ed90713e4745d77a0d72cb60908462712a8b50
- **Template Version**: 2.1

## Goal
Reach a documented, defensible end-state for every gosec rule that is currently blanket-excluded or per-line-suppressed, so the security gate carries no untriaged debt.

## Context: how the landscape changed since the backlog item (Task 2)
The backlog entry describes the Task-2 state, where perms/subprocess/pprof/http rules
were *disabled* and deferred. That premise is now partly stale:
- **G301/G302/G306 (perms)**: no longer excluded — active with per-line
  `//nolint:gosec` rationale already in place (`.dcfh/` non-secret metadata/hash files).
- **G204 (subprocess)**: the two `ssh` sites (`wire_transport.go`,
  `wire_client_shell.go`) are suppressed; build-time `git` calls in
  `generate_version.go` use fixed args.
- **G108 pprof / G114 http**: suppressed in `cmd/dcfh/dcfh.go` (opt-in, localhost-only).
- **G304 (file-path-from-variable)**: still **blanket-excluded** in `.golangci.yml`.
- ~70 production `//nolint:gosec` suppressions now exist (mostly G115, accreted Tasks 3–5).

So the live work is **audit + close-out + one policy decision (G304)**, not clearing a
backlog of untriaged findings. The deliverable is an inventory and disposition, plus any
config/suppression changes that follow from it.

## Success Criteria
- [ ] Every gosec rule currently in `.golangci.yml` `gosec.excludes` has a recorded
      disposition: keep-as-architectural, or convert to per-line suppressions.
- [ ] G304 specifically: decision recorded on whether the blanket exclude stays or is
      replaced by per-line `//nolint:gosec` at each genuine site, with traversal-risk rationale.
- [ ] Every production `//nolint:gosec` suppression is confirmed to still carry an
      accurate, rule-specific rationale (no stale/incorrect comments; note: some read `G703`
      where gosec's rule is `G304` — reconcile).
- [ ] A full-tree `golangci-lint run ./...` is clean for gosec, OR every remaining finding
      is consciously dispositioned in the triage inventory.
- [ ] The backlog item is closed/retired with the inventory as evidence; CLAUDE.md and
      `.golangci.yml` comments reflect the final policy.

## Original Estimate
**Effort**: 0.5–1 day
**Complexity**: Low–Medium (mostly audit + one policy call; risk is in G304 analysis)
**Dependencies**: golangci-lint with gosec (already wired); no external blockers.

## Major Milestones
1. **Inventory**: Produce ground-truth list of all active gosec excludes + every per-line
   suppression, each tagged with rule, site, and current rationale.
2. **Disposition**: For each category decide fix / suppress-with-rationale / keep-excluded;
   resolve the G304 blanket-exclude question explicitly.
3. **Reconcile + close**: Apply any config/comment changes, confirm gate is clean, update
   CLAUDE.md security section, retire the backlog item.

## Risk Assessment
### High Priority Risks
- **Risk 1**: G304 analysis mistakes a real traversal path for "expected scanner behaviour"
  and we suppress a genuine vulnerability.
  - **Mitigation**: Enumerate every G304 site individually; for each, identify the path's
    trust boundary (user CLI arg vs. attacker-controlled index content). Treat
    index-file-derived paths as untrusted in the analysis.

### Medium Priority Risks
- **Risk 2**: Stale/incorrect suppression rationales (e.g. `G703` comments where gosec
  emits `G304`) mask which rule is actually firing, leading to wrong dispositions.
  - **Mitigation**: Cross-check each comment against the rule gosec actually reports for
    that line before accepting it.
- **Risk 3**: Scope creep into fixing non-gosec lint debt (cyclop/unparam) tracked under a
  separate backlog item.
  - **Mitigation**: Hard scope boundary — gosec only; defer anything else to its backlog item.

## Dependencies
- `golangci-lint` (gosec runs as a v2 linter inside it) — already configured.
- Accurate measurement requires `golangci-lint run ./...`, never standalone `gosec`
  (setting `gosec.excludes` activates the full ruleset; see CLAUDE.md Security Review).

## Constraints
- gosec runs only via golangci-lint; do not add a standalone gosec binary/hook.
- Permanent architectural excludes (G103 unsafe/mmap, G401/G505 SHA-1 git-compat) are
  settled — confirm rationale only, do not relitigate.
- Suppressions must keep the existing `//nolint:gosec // Gxxx: <rationale>` style; perms
  rules stay active (no new blanket excludes for G301/G302/G306).

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [ ] **Time**: Will this take >1 week? No (~0.5–1 day).
- [ ] **People**: Does this need >2 people? No.
- [ ] **Complexity**: Does this involve 3+ distinct concerns? Partially — but the concerns
      (audit, G304 policy, reconcile) are sequential, not parallel; single-task is fine.
- [ ] **Risk**: Are there high-risk components needing isolation? G304 is the only real risk
      and is handled within the design/requirements phases.
- [ ] **Independence**: Can parts be worked on separately? Not usefully — disposition
      depends on the inventory.

**Conclusion**: 0 strong signals. Keep as a single task.

## Status
**Status**: Finished
**Next Action**: /cwf-requirements-plan 6
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Within the 0.5–1 day estimate. All success criteria met: every exclude/suppression
dispositioned, G304 decided (convert), comments reconciled, gate gosec-clean, backlog retired.

## Lessons Learned
The flagged risk (G304 analysis) was not the cost driver — a worktree-CWD process slip was
(work lost, recovered from a dangling stash). See j-retrospective.md.
