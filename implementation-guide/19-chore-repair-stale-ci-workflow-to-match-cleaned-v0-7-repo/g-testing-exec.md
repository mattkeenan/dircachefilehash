# Repair stale CI workflow to match cleaned v0.7 repo - Testing Execution
**Task**: 19 (chore)

## Task Reference
- **Task ID**: internal-19
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/19-repair-stale-ci-workflow-to-match-cleaned-v0-7-repo
- **Template Version**: 2.1

## Goal
Execute e-testing-plan.md: local reproduction of every CI step (TC-1…TC-6), then
the authoritative GitHub `pull_request` run (TC-7). No runtime code changed, so
testing is CI-step reproduction + the real PR run, not unit tests.

## Test Results

### Functional Tests

| Test ID | Test Case | Expected | Actual | Status | Notes |
|---------|-----------|----------|--------|--------|-------|
| TC-1 | `test` job builds with generated version constants [critical] | `make build`+`make test` generate first, build, pass; no `undefined: getVersionString` | Removed `cmd/*/constants_version.go`; `make build` exit 0, `make test` all `ok`; 0 `undefined:` hits | **PASS** | The original CI failure is fixed |
| TC-2 | `lint` job green at golangci-lint v2 [critical] | v2.11.2 → 0 issues, exit 0 | `golangci-lint run ./...` (v2.11.2) → `0 issues` | **PASS** | No exit-3 config-parse error |
| TC-3 | CLI smoke | `--version` non-empty, `--help` exit 0 | `v0.13.0-LOCAL-42a57ef8`; `--help` exit 0 | **PASS** | |
| TC-4 | benchmark step runs | `run_benchmarks.sh -t small` exit 0 | `PASS`, exit 0 | **PASS** | Gates on compile/exec, not timing |
| TC-5 | no stale references | no `cmd/dcfh.go`/`1.21`/`claude-code-experiment`/`gotags`/`@v3`/`@v4`; Go via `go-version-file` | grep clean; `go-version-file: go.mod` ×2; only `ci.yml` in workflows dir | **PASS** | `tags-check.yml` removed |
| TC-6 | change scope CI-config-only [critical] | only `.github/workflows/*.yml` changed | `git diff --name-only main...HEAD` (excl docs) = `ci.yml`, `tags-check.yml` | **PASS** | No `.go`/`Makefile`/`go.mod`/`.golangci.yml` |
| TC-7 | authoritative GitHub PR run [critical] | `test` + `lint` jobs both green on `pull_request` | **Pending** — needs branch push + PR to `main` (user-authorised outward action) | **PENDING** | The merge gate; see below |

### Non-Functional Tests
- **Security**: docs/CI-config only. Exec-phase `cwf-security-reviewer-changeset`
  returned **no findings** (recorded in `f-implementation-exec.md`). The new YAML
  has no `${{ github.event.* }}` interpolated into any `run:` block (script-injection
  vector absent); triggers are `push`/`pull_request` to `main` only (no
  `pull_request_target`). Removing `tags-check.yml` drops a `go install …@latest`
  fetch — a small supply-chain reduction.
- **Reliability**: single-file revert restores prior state; the PR run (TC-7) proves
  it before `main`.
- **Performance**: N/A — benchmark step is a build/exec gate, not a threshold.
- **Static check**: `python3 yaml.safe_load(ci.yml)` → parse ok (actionlint not
  installed locally; the PR run is the authoritative action-syntax check).

## Test Failures
None. TC-1…TC-6 all PASS. TC-7 is **pending**, not failed — it is the GitHub
`pull_request` run, which requires pushing the task branch and opening a PR to
`main` (an outward action requiring explicit user authorisation; `main` is
protected so the change lands via PR, not direct push).

## Coverage Report
Every success criterion has at least one executed local check:
- SC1 → TC-1 (PASS); SC2 → TC-2 (PASS); SC3 → TC-3/TC-4 (PASS); SC4 → TC-5 (PASS);
  SC5 → TC-6 (PASS, scope) + **TC-7 (pending, the green-PR gate)**.
Local critical paths (TC-1, TC-2, TC-6) 100% clean. The remaining critical path
(TC-7) is the authoritative GitHub run, pending push authorisation.

## Status
**Status**: Finished
**Next Action**: /cwf-retrospective
**Blockers**: TC-7 (GitHub PR run) pending user authorisation to push the branch
and open the PR — the merge gate, executed before the human merge, not a blocker
to the local testing layer.

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Local reproduction of every CI step is green (TC-1…TC-6). The rewritten `ci.yml`
fixes both original failures: `make build`/`make test` generate the version
constants before compiling (TC-1), and golangci-lint v2.11.2 parses
`.golangci.yml` `version: "2"` cleanly with 0 issues (TC-2). Change scope is
`.github/workflows/*.yml` only (TC-6). TC-7 — the authoritative `pull_request`
run on GitHub — is pending the user's go-ahead to push and open the PR.

## Lessons Learned
*To be captured during retrospective*

## Security Review

**State**: no findings

The changeset is now visible. Let me review it against the threat model. This is a testing-exec phase changeset that contains the CI workflow rewrite (`ci.yml`), the deletion of `tags-check.yml`, and CWF planning/exec markdown docs.

## Review

**What changed (security-relevant surface):**
The only executable artefacts are `.github/workflows/ci.yml` (rewritten) and `.github/workflows/tags-check.yml` (deleted). Everything else is CWF planning/exec markdown (`a-task-plan.md`, `d-implementation-plan.md`, `e-testing-plan.md`, and the `f-implementation-exec.md` whose embedded prior security review appears verbatim in this diff). The markdown ships no code and reaches no LLM tool-dispatch path. I focused on the workflow YAML, which GitHub Actions executes with the repo's `GITHUB_TOKEN`.

**(a)/(b) Injection / unsafe command construction in the workflow:**
I checked every `run:` block in the new `ci.yml` for interpolation of attacker-influenced data into a shell line. All `run:` steps are static literals: `go mod verify`, `make build`, `make test`, `./dcfh --version`/`--help`, `chmod +x ./run_benchmarks.sh` and the benchmark call, and (in the lint job) `go generate ./...`. There is **no `${{ ... }}` expression interpolated into any `run:` block**, so the classic GitHub Actions script-injection class (expanding `github.event.*.title`, `head_ref`, comment bodies, etc. into a shell line before the shell parses it) is absent. No finding.

**(c) Prompt injection:**
None of the changed files introduce untrusted-string substitution into LLM context or define a new SKILL/prompt surface. The markdown is static authored prose. No finding.

**(d) Environment-variable handling:**
The new workflow reads no secrets and sets no env vars feeding `chmod`/`rm`/`open`/clone paths. The deleted `tags-check.yml` ran `go install github.com/jstemmer/gotags@latest`; removing it *reduces* supply-chain surface (one fewer `@latest` third-party fetch in CI). No finding.

**(e) Pattern-based / supply-chain notes (safe here, worth recording):**

1. **Floating major-tag action pins.** `actions/checkout@v5`, `actions/setup-go@v5`, `golangci/golangci-lint-action@v7` are pinned by mutable major tag, not commit SHA. A retagged/compromised upstream could run in CI with the workflow token. **Safe here because** (i) the repo explicitly documents this policy (`d-implementation-plan.md` lines 411–413: "pinned by major tag per repo convention … no SHA-pin policy is in force"), (ii) all three are widely-audited actions, and (iii) triggers are `push`/`pull_request` to `main` only — no `pull_request_target`/`workflow_run`, so fork PRs run with read-mostly token scope and no secret exposure. **Audit future uses** where a workflow gains write permissions, secret access, or a `pull_request_target` trigger — SHA-pinning matters materially more there. Informational, matches documented policy, not a change-introduced regression.

2. **`version: v2.11.2`** concretely pins the golangci binary, replacing the old `version: latest` — a net supply-chain improvement.

3. **`go-version-file: go.mod`** replaces the hard-coded `1.21`, tracking a committed file rather than a workflow literal — no security concern.

**Net assessment:** The change removes a `@latest` third-party tool fetch, pins the linter to a concrete version, introduces no `${{ }}`→shell interpolation, and keeps triggers limited to `push`/`pull_request` on `main` (no elevated-token trigger). The floating major-tag pins are the only thing worth watching, and they conform to an explicitly documented repo policy with no secrets or write scope at present.

No actionable security findings in this changeset.

Relevant file: `/home/matt/repo/dircachefilehash/.github/workflows/ci.yml` (rewritten); `/home/matt/repo/dircachefilehash/.github/workflows/tags-check.yml` (deleted).

```cwf-review
state: no findings
summary: CI-config-only rewrite; no ${{}}->shell interpolation, no secrets/elevated token, triggers limited to push/PR on main, removes a @latest tool fetch. Floating major-tag action pins match documented repo policy (no SHA-pin in force) — noted, not a defect.
```

