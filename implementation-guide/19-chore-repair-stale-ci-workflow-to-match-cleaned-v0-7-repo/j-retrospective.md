# Repair stale CI workflow to match cleaned v0.7 repo - Retrospective
**Task**: 19 (chore)

## Task Reference
- **Task ID**: internal-19
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/19-repair-stale-ci-workflow-to-match-cleaned-v0-7-repo
- **Template Version**: 2.1
- **Retrospective Date**: 2026-06-10

## Executive Summary
- **Duration**: <0.5 day (estimated: <0.5 day, variance: ~0%)
- **Scope**: As planned — rewrite `.github/workflows/ci.yml` and remove the stale
  `tags-check.yml` so GitHub Actions builds/lints the cleaned v0.7 tree. No Go
  source change. One scope decision (remove vs modernise `tags-check.yml`)
  resolved to removal.
- **Outcome**: Success at the local layer — every CI step reproduces green
  locally (build/test with generated version constants, golangci-lint v2.11.2 =
  0 issues, CLI smoke, benchmarks). The authoritative GitHub `pull_request` run
  (TC-7) is the remaining gate, executed when the branch is pushed and a PR to
  `main` is opened (a human-authorised outward action; `main` is protected).

## Variance Analysis
### Time and Effort
- **Estimated**: Planning (a/d/e) + implementation (f) + testing (g) + j, all
  within <0.5 day. Chore skips b-requirements, c-design, h-rollout, i-maintenance.
- **Actual**: Matched. Planning was front-loaded (the diagnosis — five mismatches
  between the stale workflow and the cleaned tree — was done while triaging the
  red CI run before the task existed), so exec was mechanical: one file rewritten,
  one removed, six local checks.
- **Variance**: ~0%. The accurate estimate came from the fix being fully
  understood before the task opened.

### Scope Changes
- **Additions**: None.
- **Removals**: `tags-check.yml` removed rather than modernised — it gated on an
  untracked editor `tags` artefact (`git diff tags` always empty → always passed),
  so it carried no code-health value. Removal was the plan default; no contrary
  review/user signal arose.
- **Impact**: Net reduction — one fewer workflow and one fewer `go install
  …@latest` supply-chain fetch in CI.

### Quality Metrics
- **Test Coverage**: Every SC has an executed local check; TC-1…TC-6 PASS. TC-7
  (GitHub PR run) pending push. No runtime code → no unit-test delta.
- **Defect Rate**: 0 defects found in execution. Security review (both exec
  phases): no findings.
- **Performance**: N/A — benchmark step is a build/exec gate, not a threshold.

## What Went Well
- **Source-of-truth pinning.** The plan treated `make`/`go.mod`/`.golangci.yml`
  as the contract and made CI track them (`make build`/`make test`,
  `go-version-file: go.mod`) rather than re-spelling commands. CI now follows the
  Makefile automatically — no second place to drift.
- **Local reproduction before the cloud run.** Reproducing each CI step locally
  (including `rm -f cmd/*/constants_version.go` to mimic a fresh runner) caught
  that generation must precede both the build/test path *and* the lint path,
  before any GitHub round-trip.
- **Plan review caught a real defect.** The robustness reviewer flagged that
  `golangci-lint-action`'s `version` input takes `latest` or a concrete tag — a
  `v2.x` wildcard would error. Pinned `v2.11.2` instead. That would have been a
  red CI run discovered only on GitHub.

## What Could Be Improved
- **The fix shipped one release too late.** The cleaned-history force-push to
  `main` ran CI against the new tree for the first time and it failed; the
  workflow mismatch could have been repaired in the same cleanup task (17) that
  created the divergence. CI config was not in that task's scope.
- **No local `actionlint`.** Action-syntax validity (e.g. the `version`-input
  constraint above) can only be fully proven on GitHub. A local `actionlint`
  would shift that left.

## Key Learnings
### Technical Insights
- **Gitignored generated files are a CI contract, not a local convenience.**
  `cmd/*/constants_version.go` defines `getVersionString`/`getGitCommit` and is
  produced only by `go generate` (driven by `make`). Any CI lane that compiles
  the packages — build, test, *and* golangci-lint — must generate first. The lint
  lane needs a standalone `go generate ./...` because `golangci-lint-action`
  manages its own binary and never goes through `make`.
- **golangci-lint v2 config requires a v2 action/binary.** `.golangci.yml`
  `version: "2"` against the old `golangci-lint-action@v3` + `version: latest`
  produced exit 3 (config-parse). The action major version and the pinned binary
  version must both match the config schema.

### Process Learnings
- **Diagnose-then-task produces accurate estimates.** Because the five mismatches
  were identified while triaging the live failure, the <0.5-day estimate held
  exactly. Front-loaded diagnosis is worth the up-front read.
- **Pin the constraint, not just the value.** Task 18's lesson recurred: the plan
  pinned `v2.11.2` *and* the rule that the input rejects wildcards — pinning the
  meaning prevented a plausible-but-wrong `v2.x`.

### Risk Mitigation Strategies
- The headline risk ("can't fully verify locally") was mitigated by a two-layer
  plan: local reproduction as the pre-push gate, the GitHub `pull_request` run as
  the merge gate. Local cleared every step; the cloud run remains the final proof.

## Recommendations
### Process Improvements
- When a task rewrites history or moves the build contract, **include the CI lane
  in that task's scope** — divergence and its CI fallout belong together.

### Tool and Technique Recommendations
- Add `actionlint` to the local toolchain (and optionally the pre-commit gate) so
  GitHub-Actions syntax errors surface before a PR run.

### Future Work
- **CI enhancements deferred by scope** (BACKLOG): coverage upload, a Go-version
  matrix, module/build caching. Out of scope for "make the existing two jobs pass".
- **Re-enable the `main` ruleset protection** after this PR merges (user-owned;
  relaxed earlier this session for the cleaned-history force-push).

## Status
**Status**: Finished
**Next Action**: Task complete — push branch, open PR to `main`, confirm green, merge
**Blockers**: TC-7 (GitHub PR run) pending push authorisation — the merge gate
**Completion Date**: 2026-06-10
**Sign-off**: Matt Keenan (claude@mattkeenan.net)

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Archived Materials
- Planning: `a-task-plan.md`, `d-implementation-plan.md`, `e-testing-plan.md`
- Execution: `f-implementation-exec.md` (commit `42a57ef`), `g-testing-exec.md`
  (commit `6643020`)
- Changeset: `.github/workflows/ci.yml` (rewritten), `.github/workflows/tags-check.yml`
  (removed)
- Baseline: `fb33327`
