# Review and refactor docs into doc directory - Rollout
**Task**: 17 (feature)

## Task Reference
- **Task ID**: internal-17
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/17-review-and-refactor-docs-into-doc-directory
- **Template Version**: 2.1

## Goal
Land the docs refactor on the `main` trunk. This is a documentation-only change
(five relocated/edited Markdown docs, a rewritten root README, a new tagged
`docs/README.md`, and two `pkg/doc.go` comment lines) — there is no binary, no
service, and no user-facing runtime behaviour to roll out. "Deployment" here
means the linear ff-only merge of the task branch onto `main`; the standard
canary/phased/monitoring machinery does not apply and is marked N/A.

## Deployment Strategy
### Release Type
- **Strategy**: Direct merge to trunk (ff-only, linear history). No
  blue-green/rolling/canary — there is no running system to shift traffic
  between; the artefact is Markdown in the repo.
- **Rationale**: CWF trunk (`main`) stays linear; tasks land ff-only, never via
  merge commits. The change is inert at runtime (build + full test suite already
  green at testing-exec), so a phased rollout would add ceremony with no signal
  to gather. The retrospective phase (j) performs the squash + version actions;
  the actual `main` merge is a **human decision** and is only *suggested* here,
  never executed.
- **Rollback Plan**: `git revert` the squash/merge commit on `main` (docs return
  to their prior root locations and the old README). Because the move was done
  with `git mv`, the revert is clean and history-preserving. No data migration,
  no cache invalidation, no coordination needed.

### Pre-Deployment Checklist
- [x] Code review completed — implementation-exec deviations recorded and
  reviewed by the user before testing-exec.
- [x] All tests passing — `go build ./...` + `go test ./...` green at testing-exec
  (TC-9); only `pkg/doc.go` `.go`-changed (comment-only).
- [x] Security scan completed — exec + testing-exec changeset gate ran; both hit
  the 500-line cap (docs-heavy diff) and recorded `State: error` with rationale.
  No code/secret/auth surface (only two `pkg/doc.go` comments are non-Markdown).
- [N/A] Performance testing — no runtime change (NFR1 N/A).
- [x] Documentation updated — this task *is* the documentation update; root README
  + `docs/` set verified honest against v0.7/v0.13 code (TC-1…TC-8).
- [N/A] Monitoring and alerting — no service to monitor.
- [x] Rollback plan ready — single `git revert` (above).

## Rollout Plan
Phased rollout is **N/A** for a docs-only trunk merge — there are no user cohorts
to stage across. The effective rollout is a single step:

### Phase 1 (the only phase): Land on trunk
- **Scope**: Merge `feature/17-review-and-refactor-docs-into-doc-directory` onto
  `main` (ff-only), performed by the user after the retrospective squash.
- **Suggested command** (human-run, not executed by the workflow):
  ```bash
  git checkout main && git merge --ff-only feature/17-review-and-refactor-docs-into-doc-directory
  ```
- **Success check**: `main` builds, `docs/` links resolve (re-run the TC-3 sweep
  post-merge if desired), and the GitHub Markdown renders the relocated docs +
  banners correctly.

### Phase 2 / Phase 3
- **N/A** — no gradual user expansion for a documentation change.

## Monitoring
### Key Metrics
- **N/A (runtime)** — no response times, throughput, or error rates: nothing runs.
- **Doc-integrity signal**: relative-link resolution (TC-3) and correct Markdown
  rendering on GitHub are the only meaningful post-merge checks; both are
  one-shot, not continuous.

### Alerting
- **N/A** — no thresholds, no channels. A future reader spotting a broken link or
  a stale claim is the de facto feedback loop; corrections are ordinary doc edits.

## Rollback Plan
### Triggers
- A relocated doc fails to render or a link breaks on the hosting platform.
- A reader reports a factual regression introduced by the rewrite (e.g. a command
  that does not exist).
- (Runtime/perf/security triggers are N/A — nothing executes.)

### Procedure
1. **Immediate**: identify the offending commit (the squashed task-17 commit on
   `main`).
2. **Rollback**: `git revert <commit>` — restores prior doc locations + README.
3. **Communication**: none required (local trunk; no downstream consumers).
4. **Analysis**: fold the correction into a follow-up doc edit or a re-opened
   task rather than re-merging unchanged.

## Success Criteria
- [x] Deployment strategy defined (ff-only trunk merge) with rationale.
- [x] Pre-deployment checklist completed (N/A items justified).
- [x] Rollout plan specified (single-step; phasing N/A with reason).
- [x] Rollback plan documented (single `git revert`).
- [ ] Merge to `main` executed — deferred to the user (human decision; suggested
  command above). Not a blocker for this phase.

## Status
**Status**: Finished
**Next Action**: /cwf-maintenance
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Rollout plan right-sized for a docs-only trunk merge: single-step ff-only merge
(human-run), single-`git revert` rollback, monitoring/alerting/phasing marked N/A
with rationale. All applicable pre-deployment checks already satisfied at
testing-exec. The `main` merge itself is suggested, not executed (human decision).

## Lessons Learned
*To be captured during retrospective*
