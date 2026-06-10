# Repair stale CI workflow to match cleaned v0.7 repo - Implementation Execution
**Task**: 19 (chore)

## Task Reference
- **Task ID**: internal-19
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/19-repair-stale-ci-workflow-to-match-cleaned-v0-7-repo
- **Template Version**: 2.1

## Goal
Execute the d-implementation-plan.md rewrite of `.github/workflows/ci.yml` and
removal of `tags-check.yml`, then locally reproduce every CI step. CI-config-only;
no Go source change.

## Implementation Steps (from d-implementation-plan.md)

### Step 1: Setup
- **Planned**: Confirm branch, clean tree, re-read both workflows.
- **Actual**: On `chore/19-…`; working tree carried only the three untracked wf
  files (f/g/j). Read both `.github/workflows/ci.yml` and `tags-check.yml` in full.
- **Deviations**: None.

### Step 2: Rewrite `ci.yml`
- **Planned**: Triggers `[main]` only; `test` job = checkout@v5 → setup-go@v5
  (`go-version-file: go.mod`) → `go mod verify` → `make build` → `make test` →
  CLI smoke → benchmarks; `lint` job = checkout@v5 → setup-go@v5 → `go generate
  ./...` → `golangci-lint-action@v7` `version: v2.11.2`. Drop gotags steps, the
  dead `go build -o dcfh cmd/dcfh.go`, and the `claude-code-experiment` trigger.
- **Actual**: Wrote the new `ci.yml` exactly to the step list. `test` job calls
  the Makefile targets (`make build`, `make test`) so CI tracks the build
  contract; `lint` job runs a standalone `go generate ./...` before
  `golangci-lint-action@v7` (the action manages its own golangci binary and does
  not go through `make`, so it needs the generated `constants_version.go` to
  compile the packages). Concrete `version: v2.11.2` pin (matches `.golangci.yml`
  `version: "2"`). Both jobs on `actions/*@v5` / `golangci-lint-action@v7`
  (Node 24 runtime — clears the Node 20 deprecation).
- **Deviations**: None.

### Step 3: Remove `tags-check.yml`
- **Planned**: `git rm` the stale gotags-based always-passing workflow (default);
  modernise only if review/user prefers.
- **Actual**: `git rm .github/workflows/tags-check.yml`. Rationale: it validates an
  editor `tags` artefact that is untracked, so its `git diff tags` gate is always
  empty and the job always passes — no code-health value. Plan recommended removal;
  no contrary review/user signal, so removed.
- **Deviations**: None.

### Step 4: Validation (local reproduction of each CI step)
- **Planned**: Reproduce generate / build / test / CLI / lint / benchmarks; YAML
  parse; diff scope = `.github/workflows/*.yml` only.
- **Actual** (all green):
  - `rm -f cmd/*/constants_version.go && go generate ./...` → regenerated all
    three `cmd/*/constants_version.go` (the fix for the original `undefined:
    getVersionString`).
  - `make build` → three binaries built.
  - `./dcfh --version` → `v0.13.0-LOCAL-c7b3379d`; `./dcfh --help` → exit 0.
  - `make test` → `go test ./...` all `ok` (no failures).
  - `golangci-lint run ./...` at **v2.11.2** → **0 issues**, exit 0 (no exit-3
    config-parse error).
  - `./run_benchmarks.sh -t small` → `PASS`, exit 0.
  - `python3 -c "yaml.safe_load(...)"` on `ci.yml` → parse ok (actionlint not
    installed locally).
  - Stale-ref grep (`cmd/dcfh.go|1.21|claude-code-experiment|gotags|@v3|@v4`) over
    `.github/workflows/*.yml` → none.
  - Scope: working tree change = `ci.yml` modified + `tags-check.yml` deleted;
    no `.go`/`Makefile`/`go.mod`/`.golangci.yml` change.
- **Deviations**: The authoritative GitHub `pull_request` run (SC5/TC-7) happens
  in the testing-exec phase via a PR to `main` — local reproduction is the
  pre-push gate, not the merge gate.

## Blockers Encountered
None.

## Deferral Check
- [x] All steps from d-implementation-plan.md executed
- [x] All success criteria from a-task-plan.md met locally (SC5's PR run is the
  testing-exec gate, by design)
- [ ] b-requirements-plan.md — N/A (chore, no requirements phase)
- [ ] c-design-plan.md — N/A (chore, no design phase)
- [x] No planned work deferred without approval

## Status
**Status**: Finished
**Next Action**: /cwf-testing-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
`ci.yml` rewritten and `tags-check.yml` removed; every CI step reproduced green
locally (generate, build, test, CLI smoke, golangci-lint v2.11.2 = 0 issues,
benchmarks). Change scope is `.github/workflows/*.yml` only. Authoritative
`pull_request` run deferred to testing-exec by design.

## Lessons Learned
*To be captured during retrospective*

## Security Review

**State**: no findings

Now I have the threat model. Let me reason through the changeset, which is a GitHub Actions workflow rewrite plus CWF planning docs.

## Review

**What changed (security-relevant surface):**
The only executable artefact in this changeset is `.github/workflows/ci.yml` (rewritten) and `.github/workflows/tags-check.yml` (deleted). The remaining ~390 lines are CWF planning markdown (`a-task-plan.md`, `d-implementation-plan.md`, `e-testing-plan.md`) — process docs that ship no code and reach no LLM tool-dispatch path. I focused the review on the workflow YAML, since that is what GitHub Actions executes with repo-scoped credentials.

**(a) / (b) Bash injection / unsafe command construction in the workflow:**
I checked every `run:` block in the new `ci.yml` for interpolation of attacker-influenced data into shell commands. The new `run:` steps are all static literals:
- `go mod verify`, `make build`, `make test` — fixed commands, no interpolation.
- `./dcfh --version` / `./dcfh --help` — fixed.
- `chmod +x ./run_benchmarks.sh` and the benchmark invocation — fixed path literals.
- `go generate ./...`, `golangci-lint` via the action — fixed.

Critically, there is **no `${{ ... }}` expression interpolated into any `run:` block**. The well-known GitHub Actions script-injection class (where `${{ github.event.pull_request.title }}`, `head_ref`, issue/comment bodies, etc. are expanded into a shell line before the shell sees them, allowing `$(...)`/backtick injection from an attacker-controlled PR) is **not present** here. The testing-plan doc (TC at line 535–536) explicitly asserts this same property, and the diff confirms it. No bash-injection finding.

**(c) Prompt injection:**
None of the changed files introduce `{arguments}`-style untrusted-string substitution into LLM context. The planning docs are static prose authored by the task; they do not define a new SKILL prompt surface or a new place where git/user output flows verbatim into a model. No finding.

**(d) Environment-variable handling:**
The new workflow reads no secrets and sets no env vars feeding `chmod`/`rm`/`open`/clone paths. The deleted `tags-check.yml` ran `go install github.com/jstemmer/gotags@latest` — removing it actually *reduces* supply-chain surface (one fewer `@latest` third-party tool fetched in CI). No finding.

**(e) Pattern-based / supply-chain risks (safe-here-but-worth-noting):**
A few observations, none rising to an actionable defect for this CI-config-only chore:

1. **Action version pinning by mutable major tag.** The workflow pins `actions/checkout@v5`, `actions/setup-go@v5`, and `golangci/golangci-lint-action@v7` by floating major tag rather than by commit SHA. This is the repo's stated convention (the implementation-plan doc at lines 411–413 explicitly records "Actions are pinned by major tag per repo convention (not commit SHA) … no SHA-pin policy is in force"). Floating tags mean a compromised-upstream or retagged action could execute in CI with the workflow's token. **Safe here because** (i) all three are well-known first-party/widely-audited actions, (ii) the repo has consciously chosen tag-pinning, and (iii) the `test`/`lint` jobs run on `push`/`pull_request` with default (read-mostly) `GITHUB_TOKEN` scope and no secrets exposed. **Audit future uses** where a workflow gains write permissions, secret access, or `pull_request_target` — at that point SHA-pinning becomes materially more important. This is informational, consistent with documented policy, not a change-introduced regression.

2. **`golangci-lint-action@v7` with `version: v2.11.2`** is a concrete tool pin (good — the golangci binary itself is not floated to `latest`, unlike the old `version: latest`). This is a net improvement over the prior config.

3. **`go-version-file: go.mod`** replaces the hard-coded `1.21`. This tracks the toolchain from a tracked file rather than a workflow literal — no security concern, and it removes drift.

**Net assessment:** The change removes a `@latest` third-party tool fetch, pins the linter to a concrete version, and introduces no `${{ }}`→shell interpolation. The triggers remain `push`/`pull_request` to `main` only (no `pull_request_target`, no `workflow_run`), so untrusted fork PRs cannot gain elevated token access through this workflow. The only thing I'd flag for the record is the floating major-tag action pins, which match an explicitly documented repo policy and carry no secrets/write scope at present — so it is a pattern to watch, not a defect to fix in this chore.

No actionable security findings in this changeset.

```cwf-review
state: no findings
summary: CI-config-only rewrite; no ${{}}->shell interpolation, no secrets/elevated token, removes a @latest tool fetch. Floating major-tag action pins match documented repo policy (no SHA-pin in force) — noted, not a defect.
```
