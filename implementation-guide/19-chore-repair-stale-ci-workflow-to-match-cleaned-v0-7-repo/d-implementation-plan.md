# Repair stale CI workflow to match cleaned v0.7 repo - Implementation Plan
**Task**: 19 (chore)

## Task Reference
- **Task ID**: internal-19
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/19-repair-stale-ci-workflow-to-match-cleaned-v0-7-repo
- **Template Version**: 2.1

## Goal
Rewrite `.github/workflows/ci.yml` to build/lint the cleaned tree correctly and
remove the stale `tags-check.yml`. CI-config-only; the build contract (`make`,
`go.mod`, `.golangci.yml`) is already correct and is the source of truth.

## Source-of-truth verification (done before this plan, reproduced locally)
- **Generate**: `//go:generate go run generate_version.go` lives at line 1 of
  `cmd/dcfh/dcfh.go`, `cmd/dcfhfind/main.go`, `cmd/dcfhfix/main.go`. **`go generate
  ./...` from the repo root** runs all three and writes `cmd/*/constants_version.go`
  (the gitignored files defining `getVersionString`/`getGitCommit`). Verified: it
  produced all three locally.
- **Toolchain**: `go.mod` → `go 1.25.0` / `toolchain go1.26.4`. Use
  `setup-go` with **`go-version-file: go.mod`** so CI tracks go.mod automatically
  (no hard-coded `1.21`).
- **Build**: `make build` = `generate-* → go build -o <bin> ./cmd/<bin>` for the
  three binaries. The old `go build -o dcfh cmd/dcfh.go` path is dead.
- **Lint**: `.golangci.yml` is `version: "2"`; local `golangci-lint run ./...`
  with **v2.11.2 → 0 issues**. CI must install golangci-lint **v2.x**. golangci
  must compile the packages, so generation must run **before** the lint step too.
- **Tests**: post-generate `go test ./...` → all pass locally; `go build ./...`
  OK.
- **`tags-check.yml`**: generates an editor `tags` file via `gotags` and fails if
  `git diff tags` is non-empty — but `tags` is untracked, so the diff is always
  empty and the job always passes. It validates an editor artefact, not code
  health. **Recommend removal** (see Step 3; alternative: modernise — flagged for
  review).

## Files to Modify
### Primary Changes
- `.github/workflows/ci.yml` — full rewrite (generate step, Go 1.25 via
  `go-version-file`, `make`-aligned build/test, golangci-lint v2, refreshed
  actions, drop `claude-code-experiment` trigger).

### Supporting Changes
- `.github/workflows/tags-check.yml` — **remove** (stale, no code-health value).
  If review/user prefers keeping a tags check, modernise instead (Go via
  `go-version-file`, drop `claude-code-experiment`, bump actions).

### Out of scope (do NOT touch)
Any `.go`, `Makefile`, `go.mod`, `.golangci.yml`, `run_benchmarks.sh` — the build
contract is correct. No CI feature additions (coverage upload, matrix, caching).

## Implementation Steps
### Step 1: Setup
- [ ] Confirm branch `chore/19-…`, tree clean. Re-read current
  `.github/workflows/ci.yml` and `tags-check.yml`.

### Step 2: Rewrite `ci.yml`
- [ ] **Triggers**: `push: branches: [main]`, `pull_request: branches: [main]`
  (drop `claude-code-experiment`).
- [ ] **`test` job** (ubuntu-latest): `actions/checkout@v5` → `actions/setup-go@v5`
  with `go-version-file: go.mod` → `go mod verify` → **`make build`** (runs
  `generate-*` then builds the three binaries) → **`make test`** (runs `generate`
  then `go test ./...`) → CLI smoke (`./dcfh --version`, `./dcfh --help`) →
  benchmarks (`chmod +x ./run_benchmarks.sh && ./run_benchmarks.sh -t small`).
  **Call the Makefile targets rather than re-spelling `go generate`/`go test`/
  `go build`** — the Makefile is the build contract (plan-review: improvements +
  misalignment), so CI tracks it automatically. Remove the gotags steps and the
  dead `go build -o dcfh cmd/dcfh.go` step. (`@v5` actions run on Node 24,
  clearing the deprecation warnings.) The benchmark step gates on
  **compile/execute success only**, not on performance timings (noisy shared
  runners) — `run_benchmarks.sh -t small` runs `go test -bench ./pkg/` and exits
  non-zero only on a test/build failure.
- [ ] **`lint` job** (ubuntu-latest): `checkout@v5` → `setup-go@v5`
  (`go-version-file: go.mod`) → **`go generate ./...`** (the lint job uses
  `golangci-lint-action`, which manages its own golangci binary and does **not**
  go through `make`, so it needs a standalone generate; golangci-lint compiles
  the packages and without the generated files it re-hits `undefined:
  getVersionString`) → `golangci/golangci-lint-action@v7` with
  **`version: v2.11.2`** (a **concrete** v2 pin matching `.golangci.yml`
  `version: "2"` and the locally-verified clean run; **not** a `v2.x` wildcard —
  the action's `version` input takes `latest` or a concrete tag, so `v2.x` would
  error — plan-review: robustness). Bump the concrete patch only if a newer v2.x
  is confirmed clean locally first.

### Step 3: Remove `tags-check.yml`
- [ ] `git rm .github/workflows/tags-check.yml` (default). Rationale recorded in
  Actual Results. If review says keep it, modernise in place instead (same
  setup-go/go-version-file + trigger fix) — do **not** silently leave it stale.

### Step 4: Validation (mechanical — full plan in e-testing-plan)
- [ ] Locally reproduce each CI step: `go generate ./...`, `go test ./...`,
  `make build`, `./dcfh --version`/`--help`, `golangci-lint run ./...` (v2) — all
  green (already verified once in planning; re-run after any edit).
- [ ] `yamllint`/`actionlint` the workflow if available; otherwise a YAML
  parse check.
- [ ] `git diff --name-only` shows only `.github/workflows/*.yml`.
- [ ] **PR verification (the real gate)**: open a PR to `main`; confirm the
  `test` and `lint` jobs both go **green** on the `pull_request` run before merge.

## Code Changes
The new `ci.yml` is plain GitHub-Actions YAML; exact text finalised at exec time
against the step list above (no pseudocode needed). The external-version choices
to lock: `actions/checkout@v5`, `actions/setup-go@v5` (`go-version-file: go.mod`),
and `golangci/golangci-lint-action@v7` + **`version: v2.11.2`** (concrete pin,
verified against `.golangci.yml` `version: "2"` and the local clean run). Actions
are pinned by major tag per repo convention (not commit SHA) — noted for the
record (plan-review: security); no SHA-pin policy is in force.

## Test Coverage
**See e-testing-plan.md** — local CI-step reproduction + the authoritative
`pull_request` run on GitHub.

## Validation Criteria
**See e-testing-plan.md** — maps SC1–SC5 to local reproduction and the PR run.

## Scope Completion
**IMPORTANT**: Complete all planned implementation before marking task Finished.

Deferring work creates technical debt and scope creep. Task 37 deferred documentation updates,
marked the task complete anyway, and created Task 38 to fix the deferred work.

**If you must defer work**:
1. Get user approval with clear rationale
2. Update success criteria to reflect descoped work
3. Create follow-up task immediately
4. Document deferral in Actual Results section

## Status
**Status**: Finished
**Next Action**: /cwf-testing-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
`ci.yml` rewritten to the step list (no deviations); `tags-check.yml` removed.
Each CI step reproduced green locally: `go generate ./...` regenerates all three
`cmd/*/constants_version.go`, `make build`/`make test` pass, golangci-lint v2.11.2
reports 0 issues, CLI smoke and benchmarks exit 0. Change scope is
`.github/workflows/*.yml` only. See `f-implementation-exec.md`.

## Lessons Learned
The lint lane needs its **own** `go generate ./...` — `golangci-lint-action`
manages its own binary and never goes through `make`, so without it the linter
re-hits `undefined: getVersionString`. Pinning `v2.11.2` plus the rule that the
action's `version` input rejects wildcards (`v2.x`) prevented a plausible-but-wrong
pin. See `j-retrospective.md`.
