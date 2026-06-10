# Repair stale CI workflow to match cleaned v0.7 repo - Testing Plan
**Task**: 19 (chore)

## Task Reference
- **Task ID**: internal-19
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/19-repair-stale-ci-workflow-to-match-cleaned-v0-7-repo
- **Template Version**: 2.1

## Goal
Verify the rewritten CI workflow builds, tests, and lints the cleaned tree
correctly. Two layers: **local reproduction** of each CI step (fast, pre-push),
then the **authoritative GitHub `pull_request` run** (the real gate — actions,
runner toolchain, golangci-lint v2 install only prove out on GitHub). No runtime
code changes, so there are no new unit tests.

## Test Strategy
### Test Levels
- **Local reproduction**: run each CI step on the dev machine exactly as the
  workflow will (`go mod verify`, `make build`, `make test`, CLI smoke,
  `run_benchmarks.sh -t small`, `golangci-lint run ./...` at v2).
- **Workflow static check**: `actionlint`/`yamllint` if available, else a YAML
  parse; confirm only `.github/workflows/*.yml` changed.
- **GitHub PR run (authoritative)**: open a PR to `main`; the `test` and `lint`
  jobs must both go green on the `pull_request` trigger.

### Test Coverage Targets
- **Every SC (SC1–SC5)** has at least one check below.
- **Critical paths** = the two failures the rewrite fixes: `test` job no longer
  hits `undefined: getVersionString` (SC1), `lint` job no longer exits 3 (SC2).
  Both must be green on the PR run.
- **Regression**: the build contract (`make`, `go.mod`, `.golangci.yml`) is
  unchanged; only CI config changed.

## Test Cases
### Functional Test Cases

- **TC-1 — `test` job builds with generated version constants (SC1) [critical]**
  - **Given**: a clean checkout with `cmd/*/constants_version.go` absent (as on a
    fresh CI runner).
  - **When**: `make build` then `make test` (the workflow's steps).
  - **Then**: generation runs first, binaries build, `go test ./...` passes; no
    `undefined: getVersionString`/`getGitCommit`. Locally reproduced after
    `rm -f cmd/*/constants_version.go`.

- **TC-2 — `lint` job is green at golangci-lint v2 (SC2) [critical]**
  - **Given**: generated tree, `.golangci.yml` `version: "2"`.
  - **When**: `go generate ./...` then `golangci-lint run ./...` at v2.11.2
    (locally) / `golangci-lint-action@v7` `version: v2.11.2` (CI).
  - **Then**: `0 issues`, exit 0 (no exit-3 config-parse error).

- **TC-3 — CLI smoke (SC3)**
  - **Given**: `make build` has produced `./dcfh`.
  - **When**: `./dcfh --version` and `./dcfh --help`.
  - **Then**: both exit 0; `--version` prints a non-empty version string.

- **TC-4 — benchmark step runs (SC3)**
  - **Given**: the test job.
  - **When**: `./run_benchmarks.sh -t small`.
  - **Then**: exits 0 (gates on compile/execute success, not timing).

- **TC-5 — no stale references remain (SC4)**
  - **Given**: the new `ci.yml` (and removed `tags-check.yml`).
  - **When**: grep the workflows for `cmd/dcfh.go`, `1.21`,
    `claude-code-experiment`, `gotags`, `@v3`/`@v4` action pins.
  - **Then**: none present; `tags-check.yml` removed (or modernised per review);
    Go version comes from `go-version-file: go.mod`.

- **TC-6 — change scope is CI-config-only (SC5) [critical]**
  - **Given**: all edits.
  - **When**: `git diff --name-only main...HEAD` (excluding the workflow doc dir).
  - **Then**: only `.github/workflows/*.yml` changed; no `.go`/`Makefile`/
    `go.mod`/`.golangci.yml` change.

- **TC-7 — authoritative GitHub PR run (SC1–SC5) [critical]**
  - **Given**: the branch pushed and a PR opened to `main`.
  - **When**: GitHub Actions runs the `pull_request` workflow.
  - **Then**: both `test` and `lint` jobs report **success**; no Node 20
    deprecation failure (warnings acceptable). This is the gate for merge.

### Non-Functional Test Cases
- **Security**: docs/CI-config only; the exec-phase `cwf-security-reviewer-changeset`
  run is the gate. No `${{ github.event.* }}` interpolation into `run:` blocks
  (script-injection vector) — confirmed by reading the new YAML.
- **Reliability**: workflow is idempotent; single-file revert restores the prior
  state. The PR run proves it before it reaches `main`.
- **Performance**: N/A — benchmark step is a build/exec gate, not a threshold.

## Test Environment
### Setup Requirements
- Local: `go` 1.25+ toolchain, `make`, `golangci-lint` v2.x, `git`. No DB/network
  beyond module downloads.
- GitHub: a PR to `main` to trigger the `pull_request` workflow (the only way the
  workflows run for a non-`main` branch).

### Automation
- The workflow itself is the automation under test. Local reproduction mirrors it
  step-for-step; the PR run is the authoritative execution.

## Validation Criteria
- [ ] TC-1…TC-7 pass; critical paths (TC-1, TC-2, TC-6, TC-7) clean.
- [ ] Local reproduction of every CI step green before push.
- [ ] GitHub `test` and `lint` jobs both green on the PR run.
- [ ] Only `.github/workflows/*.yml` changed.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
TC-1…TC-6 executed and PASS (see `g-testing-exec.md`): clean build/test with
regenerated constants, golangci-lint v2.11.2 = 0 issues, CLI smoke, benchmarks,
no stale refs, CI-config-only scope. TC-7 (authoritative GitHub `pull_request`
run) is **pending** — it requires pushing the branch and opening a PR to `main`,
the human-authorised merge gate.

## Lessons Learned
The two-layer strategy (local CI-step reproduction as the pre-push gate + the
GitHub PR run as the merge gate) is the right shape when the artefact under test
*is* the CI config: local proves the steps, only GitHub proves the action wiring.
See `j-retrospective.md`.
