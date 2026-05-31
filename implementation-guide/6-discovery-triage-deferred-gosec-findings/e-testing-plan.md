# Triage deferred gosec findings - Testing Plan
**Task**: 6 (discovery)

## Task Reference
- **Task ID**: internal-6
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: discovery/6-triage-deferred-gosec-findings
- **Template Version**: 2.1

## Goal
Define how each requirement (FR1–FR6, NFR4/NFR5) is verified. This is an audit/config task:
"testing" means gate-and-artefact verification, not new unit tests. The existing Go suite is
the regression guard for any `fix` code change.

## Test Strategy
### Test Levels
- **Tooling-gate tests**: `golangci-lint run ./...` (gosec) outcomes — the primary verification.
- **Artefact tests**: inventory completeness + per-rule reconciliation; doc/grep assertions.
- **Regression tests**: existing `go test ./pkg/...` + cmd suites (guard any `fix`).
- **Hook test**: `.githooks/pre-commit` `--new` staged path on the changeset.
- *(No new unit tests are added unless a `fix` introduces new logic; none is expected.)*

### Test Coverage Targets
- **Critical path**: 100% of in-scope gosec findings appear in the inventory with a disposition.
- **Reconciliation**: per-rule identity `emitted = fix + suppress + exclude + accept` balances
  exactly against the measured census (no off-by-N).
- **Regression**: existing suite stays green; no coverage *target* change (config/doc task).

## Test Cases
### Functional Test Cases

- **TC-1 — Scratch run is trustworthy (guards FR1, design Decision 1 failure-guard)**
  - **Given**: a disposable worktree with G304 removed from `gosec.excludes` and the two
    `os.WriteFile` `G703` comments removed.
  - **When**: `golangci-lint run --output.json.path=stdout ./...` runs and `go build ./...` runs.
  - **Then**: golangci-lint exits with only lint findings (no execution/compile error) **and**
    `go build` succeeds. If either fails, the JSON is rejected and the run repeated — the
    inventory is never built on a partial dataset.

- **TC-2 — Inventory completeness + per-rule reconciliation (guards FR1, AC1)**
  - **Given**: the scratch JSON and the Step-1 `grep` census (live measured count).
  - **When**: the inventory table is assembled and counts are summed per rule.
  - **Then**: every in-scope finding appears exactly once; for each rule
    `emitted == fix + suppress + exclude + accept`; G103/G401/G505 appear as category-level
    `exclude` rows; out-of-scope items (`convert-index:65`, fixtures, `_test.go`) are listed.

- **TC-3 — Every row dispositioned (guards FR2, AC2)**
  - **Given**: the assembled inventory.
  - **When**: each row is inspected.
  - **Then**: each row carries exactly one of `{exclude, accept, fix, suppress}` and a one-line
    rationale; every `fix` row names a concrete change.

- **TC-4 — G304 per-site trust classification + precedence (guards FR3, NFR4, AC3)**
  - **Given**: the live G304 sites with traced path origins.
  - **When**: each is classified trusted/untrusted from its *actual* source (CLI/env/config vs
    index/wire content) — distinguishing the `resolveRel`-guarded `hashOne` read from the
    config-sourced `loadHashCache` read.
  - **Then**: no untrusted site carries a bare suppress — it is `fix` or a suppress that cites
    a provable escape guard; if any untrusted live site exists, the policy is "convert".

- **TC-5 — No non-emittable rule IDs remain (guards FR4, AC4)**
  - **Given**: the post-change tree.
  - **When**: `grep -rn "nolint:gosec" --include="*.go" . | grep -v _test.go` is inspected and
    cross-checked against the empirical rule per line.
  - **Then**: no comment cites `G703` (or any rule gosec cannot emit); every retained comment's
    ID matches the emitted rule; corrections are listed old→new in the inventory.

- **TC-6 — Clean gate, atomic landing (guards FR5, NFR5, AC5)**
  - **Given**: dispositions applied to the real tree.
  - **When**: `golangci-lint run ./...` and the staged `.githooks/pre-commit --new` path run;
    `git show --stat` on the apply commit is inspected.
  - **Then**: zero gosec findings except documented excludes; pre-commit passes; if G304 was
    converted, the exclude removal and all per-line suppressions are in the **same** commit
    (no intermediate red state). On failure, the commit reverts as one unit (abort path).

- **TC-7 — Docs + backlog reconciled (guards FR6, AC6a/b/c)**
  - **Given**: close-out edits applied.
  - **When**: `grep -n "deferred bug" CLAUDE.md` and a read of the Security Review section and
    `.golangci.yml` comments; `backlog-manager validate`.
  - **Then**: the stale G115 "real deferred bug" text is gone; CLAUDE.md + `.golangci.yml`
    describe the final exclude set and G304 policy; the backlog item is retired against task 6
    and both files still validate.

### Non-Functional Test Cases
- **Security (NFR4)**: TC-4 and TC-5 are the security tests — they prove no untrusted-input
  path is silenced without a guard and no dead/mislabelled suppression remains. No secret/
  credential finding is suppressed (if any surfaced it is raised, per Constraints).
- **Reliability (NFR5)**: TC-1 (no partial dataset) and TC-6 (atomic landing + abort) are the
  reliability tests. Regression: `go test ./pkg/...` + cmd suites green; on-disk index format
  and CLI output byte-identical (no `fix` alters them).
- **Performance (NFR1)**: n/a — no runtime path changed; if a `fix` guard is added it is an
  O(1) path check, no benchmark regression expected.
- **Usability (NFR2)**: the inventory reads as a standalone table; suppression comments follow
  the existing `//nolint:gosec // Gxxx: <rationale>` form (spot-check).

## Test Environment
### Setup Requirements
- `golangci-lint` v2.11.2 (gosec linter), Go toolchain for `go build`/`go test`.
- A disposable git worktree outside the primary tree for the scratch run (TC-1).
- No test database, network, or fixtures beyond the repo itself.

### Automation
- All gates are existing CLI invocations (`golangci-lint`, `go test`, pre-commit hook,
  `backlog-manager validate`) — runnable locally and in CI. No new test harness is introduced.
- CI integration point: the same `golangci-lint run` the `--new` hook already uses.

## Validation Criteria
- [ ] TC-1 scratch run guarded-valid
- [ ] TC-2 inventory complete; per-rule reconciliation balances
- [ ] TC-3 every row dispositioned with rationale
- [ ] TC-4 G304 sites classified; no untrusted bare suppress
- [ ] TC-5 no `G703`/non-emittable IDs; comments match emitted rules
- [ ] TC-6 gosec-clean gate; pre-commit passes; atomic landing verified
- [ ] TC-7 CLAUDE.md/​.golangci.yml updated; backlog retired; both validate
- [ ] Regression: `go test ./pkg/...` + cmd suites green

## Decomposition Check
- [ ] **Time**: >1 week? No. **People**: >2? No. **Complexity**: 3+ concerns? No (one gate set).
- [ ] **Risk**: isolation needed? No. **Independence**: separable? No.
**Conclusion**: single task; no decomposition.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-exec 6
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
TC-1…TC-6 PASS; TC-7 PARTIAL at g-exec time (backlog retirement deferred to user review),
completed at retrospective. Gate gosec-clean; per-rule reconciliation balances; G304 trust
classification + atomic landing confirmed. See g-testing-exec.md.

## Lessons Learned
TC-1's build guard was load-bearing: the fresh worktree lacked the gitignored generated
version file, so the first scratch build failed until `go generate` ran. See j-retrospective.md.
