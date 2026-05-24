# Adopt full Go pre-commit hook and security review - Testing Plan
**Task**: 2 (chore)

## Task Reference
- **Task ID**: internal-2
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/2-adopt-full-go-pre-commit-hook-and-security-review
- **Template Version**: 2.1

## Goal
Validate that gosec is wired with precise, justified suppressions (zero gosec findings, rules still active for new code) and that the pre-commit hook and existing behaviour are unaffected. This is a config + comment-only change, so the strategy is **verification of lint/hook state**, not new unit tests.

## Test Strategy
### Test Levels
- **Static analysis verification**: golangci-lint/gosec output assertions (the substance of this task)
- **Hook behaviour**: pre-commit gate exercised in staged (`--new`) mode
- **Regression**: existing `go test -race ./...` suite unchanged
- **No new unit tests**: no new Go logic is introduced (nolint comments are behaviour-neutral)

### Coverage Targets
- 100% of the 13 firing gosec rules have an explicit, verified disposition (exclude / test-scope / nolint)
- All existing tests continue to pass (zero regression)

## Test Cases
### Functional
- **TC-1 — gosec is silent on the full tree**
  - **Given**: the end-state `.golangci.yml` + production nolints
  - **When**: `golangci-lint run ./...`
  - **Then**: zero `(gosec)` lines; the only failures are the 3 documented pre-existing non-gosec issues

- **TC-2 — every suppression is load-bearing and scoped**
  - **Given**: the committed suppressions
  - **When**: each is individually removed (exclude entry, the `_test\.go` rule, one production nolint) and gosec re-run
  - **Then**: the corresponding finding(s) re-appear — proving no suppression is dead and none masks more than intended

- **TC-3 — gosec still catches NEW production issues (the whole point)**
  - **Given**: a planted snippet using a zero-current-finding active rule (candidate G404 `math/rand`) in a non-test file
  - **When**: `golangci-lint run --new ./...` (the hook's staged path)
  - **Then**: it fails on the planted finding; reverting restores green

- **TC-4 — test-path scope does not leak to production**
  - **Given**: the `{linters:[gosec], path: _test\.go}` rule
  - **When**: a G204-style `exec.Command(var)` is planted in a **production** file
  - **Then**: it is still flagged (the rule is suppressed only in `_test.go`), then revert

- **TC-5 — no behavioural regression**
  - **Given**: the nolint comments added to ≈12 production sites
  - **When**: `GOFLAGS="-gcflags=all=-d=checkptr=0" go test -race ./...`
  - **Then**: full suite passes exactly as before (comments change nothing)

- **TC-6 — clean-tree hook passes end-to-end**
  - **Given**: a clean working tree on the task branch
  - **When**: a no-op commit triggers `.githooks/pre-commit`
  - **Then**: gofmt, go fix, gopls, golangci-lint `--new`, govulncheck, `go test -race` all pass

- **TC-7 — config is valid**
  - **Given**: the edited `.golangci.yml`
  - **When**: `golangci-lint config verify`
  - **Then**: valid (correct v2 `linters.settings.gosec.excludes` / `exclusions.rules` schema)

- **TC-8 — security-review phase documented + exercised**
  - **Given**: the CLAUDE.md "Security Review" subsection
  - **When**: `cwf-security-reviewer-changeset` is run against this task's diff
  - **Then**: it completes and reports no blocking FR4 issues introduced by this changeset (comment + config only)

### Non-Functional
- **Performance**: commit-time lint cost rises modestly (gosec added); acceptable — hook uses `--new` staged mode. No assertion beyond "hook completes".
- **Security**: posture *improves* — gosec now guards new code for all non-excluded rules (verified by TC-3/TC-4).
- **Reliability**: nolint comments are inert; TC-5 guards against accidental behaviour change.

## Test Environment
- 64-bit Linux (codebase requirement); tools on PATH: `gosec`, `golangci-lint` v2.11.2, `govulncheck`, `gopls` (all confirmed present)
- No test data / mocks needed; scratch snippets for TC-3/TC-4 created and reverted in-tree
- **DB note**: N/A — this task touches no database

## Validation Criteria
- [ ] TC-1..TC-8 pass
- [ ] All 13 gosec rules have a verified disposition
- [ ] Zero regression in `go test -race`
- [ ] Residual lint failures are exactly the 3 pre-existing non-gosec issues (backlogged)

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-exec
**Blockers**: None identified

## Actual Results
TC-1..TC-8 all PASS (see g-testing-exec.md). Notably TC-2 proved suppressions load-bearing (removing the `_test\.go` rule resurfaced 210 findings; removing G401 resurfaced SHA-1 findings) and TC-6 ran the full hook green via commit 16d041d.

## Lessons Learned
The "13 firing rules" coverage target was an undercount — measured under the default `max-same-issues: 3` cap. Real disposition: 26 production sites. Lift the cap before stating coverage for a security linter.
