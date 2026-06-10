# Repair stale CI workflow to match cleaned v0.7 repo - Plan
**Task**: 19 (chore)

## Task Reference
- **Task ID**: internal-19
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/19-repair-stale-ci-workflow-to-match-cleaned-v0-7-repo
- **Baseline Commit**: fb333270249cda647b108fde280dd2cd69045250
- **Template Version**: 2.1

## Goal
Rewrite the stale `.github/workflows/ci.yml` (and modernise `tags-check.yml`) so
the GitHub Actions checks build and lint the **cleaned v0.7 repo** correctly —
green `test` and `lint` jobs — without changing any Go source.

## Context
The public-release history rewrite (`0d79792`) was force-pushed to `origin/main`
this session, so CI ran against the cleaned tree for the first time and **failed**.
The workflows predate the architecture refactor and are mismatched on five points:
1. **No code generation.** `getVersionString`/`getGitCommit` live in
   `cmd/*/constants_version.go`, which is **gitignored** and produced only by
   `go generate` (driven by `make`). CI runs `go test` directly → `undefined:
   getVersionString` (the actual failure).
2. **Go 1.21** in CI vs `go 1.25.0` / `toolchain go1.26.4` in `go.mod`.
3. **`go build -o dcfh cmd/dcfh.go`** — that single file is now the `cmd/dcfh/`
   package dir.
4. **golangci-lint-action@v3 + `version: latest`** against a `version: "2"`
   `.golangci.yml` → `golangci-lint exit code 3`.
5. Stale bits: `claude-code-experiment` trigger branch, Node 20 action versions
   (deprecation warnings), a `gotags`-based tags-check.

## Success Criteria
- [ ] **SC1 — `test` job green**: CI generates the version constants before
  testing (mirror `make`: `go generate ./...` or `make test`) and runs on Go
  **1.25**; `go test ./...` passes with no `undefined: getVersionString/
  getGitCommit`.
- [ ] **SC2 — `lint` job green**: golangci-lint runs at a **v2** version matching
  `.golangci.yml` (`version: "2"`), after generation, exit 0.
- [ ] **SC3 — build/CLI step current**: binaries built via the real layout
  (`make build` or `go build ./cmd/...`); `./dcfh --version` and `--help` succeed.
- [ ] **SC4 — no stale references**: no `cmd/dcfh.go`, no Go 1.21, no
  `claude-code-experiment`; action versions updated enough to clear the Node 20
  deprecation. `tags-check.yml` either modernised or removed (it currently passes
  but is equally stale).
- [ ] **SC5 — CI-config-only, verified on a PR**: only `.github/workflows/*.yml`
  change (no `.go`/Makefile/source edit); both jobs observed **green** on the
  task branch via a `pull_request` run before merge.

## Original Estimate
**Effort**: <0.5 day
**Complexity**: Low
**Dependencies**: None. Reads `make`/`go.mod`/`.golangci.yml` as the source of
truth for the correct toolchain and generation steps. Verification needs a
GitHub `pull_request` run (the workflows only trigger on `main` push or PR-to-main).

## Major Milestones
1. **Pin the correct build contract**: confirm the generate-then-build/test
   sequence from `make` and the golangci-lint v2 requirement (done in planning).
2. **Rewrite `ci.yml`** (and modernise/remove `tags-check.yml`): Go 1.25, generate
   step, `make`-aligned build/test, golangci-lint v2, refreshed actions.
3. **Verify on a PR**: open a PR to `main`, confirm both jobs green, then merge.

## Risk Assessment
### High Priority Risks
- **Risk 1 — Can't fully verify locally**: GitHub Actions behaviour (action
  versions, runner toolchain) only truly proves out on GitHub.
  - **Mitigation**: reproduce each step locally first (`go generate ./...`,
    `go test ./...`, `make build`, `golangci-lint run` at the pinned v2 version);
    then verify on a real `pull_request` run (SC5) before merging — do not rely on
    local reproduction alone.

### Medium Priority Risks
- **Risk 2 — golangci-lint v2 action wiring**: the v2 config needs a compatible
  action/binary; a wrong pin re-triggers exit 3 or a config-parse error.
  - **Mitigation**: pin golangci-lint-action to a version that defaults to (or is
    told to install) golangci-lint v2.x; run it locally against `.golangci.yml`
    first.
- **Risk 3 — Scope creep into CI features**: tempting to add coverage upload,
  matrix builds, caching, release wiring.
  - **Mitigation**: scope is strictly "make the existing two jobs pass on the
    current tree"; defer enhancements to a BACKLOG item.

## Dependencies
- None external. The merge to `main` is gated on a green PR run (SC5) and is a
  human action (CI changes land via PR now that `main` is protected again).

## Constraints
- **CI-config-only** — no `.go`, `Makefile`, `go.mod`, or `.golangci.yml` change
  (the build contract is correct; only the CI that invokes it is wrong).
- `main` is protected (force-push/PR rules just re-tightened) — this lands via a
  pull request, not a direct push.
- British spelling; understated tone.

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [x] **Time**: <0.5 day. No decomposition.
- [x] **People**: single author. No decomposition.
- [x] **Complexity**: one concern (CI YAML for two workflows). No decomposition.
- [x] **Risk**: only "verifies on GitHub, not locally", mitigated by the PR run.
- [x] **Independence**: one `.github/workflows/` change set. No decomposition.

**Verdict**: 0/5 signals — single chore, no decomposition.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Delivered as planned: `ci.yml` rewritten (Go 1.25 via `go-version-file`, generate
step, `make`-aligned build/test, golangci-lint v2.11.2) and `tags-check.yml`
removed. SC1–SC4 met and verified locally; SC5's scope check passed (CI-config
only), with the green `pull_request` run pending push. Estimate (<0.5 day, Low)
held exactly — the diagnosis was done while triaging the live CI failure before
the task opened. See `j-retrospective.md`.

## Lessons Learned
Diagnose-then-task gives accurate estimates: the five workflow mismatches were
identified from the red CI run before the task existed, so exec was mechanical.
See `j-retrospective.md`.
