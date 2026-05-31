# Triage deferred gosec findings - Testing Execution
**Task**: 6 (discovery)

## Task Reference
- **Task ID**: internal-6
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: discovery/6-triage-deferred-gosec-findings
- **Template Version**: 2.1

## Goal
Execute the TC-1…TC-7 gates from e-testing-plan.md against the committed implementation
(`90ff0f2`). This is an audit/config task: "tests" are tooling-gate, artefact, and regression
checks — there are no new unit tests.

## Environment
- `golangci-lint` v2.11.2 (gosec linter), Go 1.26.0; commit under test `90ff0f2`.
- Ground-truth JSON preserved at `/tmp/-home-matt-repo-dircachefilehash-task-6/gosec.json`.

## Test Results

### Functional Tests
| Test ID | Test Case | Expected | Actual | Status |
|---------|-----------|----------|--------|--------|
| TC-1 | Scratch run trustworthy (FR1, Decision-1 guard) | run exits with only lint findings + worktree `go build` succeeds | build guard passed (after `go generate` produced `constants_version.go`); JSON valid, 23 gosec issues parsed | PASS |
| TC-2 | Inventory completeness + per-rule reconciliation (FR1, AC1) | every in-scope finding once; per-rule `emitted = fix+suppress+exclude+accept` | inventory in f-implementation-exec.md; G304: 23 suppress; G703: 2 accept; G103/G401/G505 category-exclude; balances | PASS |
| TC-3 | Every row dispositioned (FR2, AC2) | one of {exclude,accept,fix,suppress} + rationale per row | all rows carry exactly one disposition + a one-line rationale; no `fix` needed | PASS |
| TC-4 | G304 per-site trust classification + precedence (FR3, NFR4, AC3) | no untrusted bare suppress; guard cited; untrusted ⇒ convert | `hash.go:82/186` (the only untrusted-reachable opens) cite `resolveRel`→`hasPathPrefix` (wire_handler.go:231); `loadHashCache:398` distinguished as operator/CLI-sourced; policy = CONVERT | PASS |
| TC-5 | No non-emittable rule IDs (FR4, AC4) | no comment cites a rule gosec cannot emit; IDs match emitted rule | retained IDs: G115/G304/G301/G306/G703/G302/G204/G114/G108 — all emittable. **G703 empirically confirmed real** ("path traversal via taint analysis") → the 2 `os.WriteFile` comments are correct, not mislabels; the 2 env `os.Create` comments corrected G703→G304 | PASS |
| TC-6 | Clean gate + atomic landing (FR5, NFR5, AC5) | 0 gosec findings except documented excludes; `--new` passes; exclude-removal + suppressions one commit | `golangci-lint run ./...` → 0 gosec (only out-of-scope cyclop:2/unparam:1); pre-commit `--new` + full `go test -race` green; G304 exclude removal and all 23 suppressions in the single commit `90ff0f2` | PASS |
| TC-7 | Docs + backlog reconciled (FR6, AC6a/b/c) | stale G115 text gone; CLAUDE.md + `.golangci.yml` describe final excludes + G304 policy; backlog retired | CLAUDE.md updated (G115 "deferred bug" text removed; final exclude set + G304-conversion + G703 note added); `.golangci.yml` documents the conversion. **Backlog retirement (AC6c) intentionally DEFERRED to user review** — flagged, not silently skipped | PARTIAL |

### Non-Functional Tests
- **Security (NFR4)** — TC-4 + TC-5 are the security gates. No untrusted-input path is silenced
  without a cited guard; no dead/mislabelled suppression remains in production (the one residual
  mislabel, `convert-index-v1-to-v2.go:65`, is the out-of-scope root utility, documented). No
  secret/credential finding surfaced. **PASS.**
- **Reliability (NFR5)** — TC-1 (no partial dataset: build-guarded) and TC-6 (atomic single
  commit; full `go test -race` green via the pre-commit hook) are the reliability gates.
  On-disk index format and CLI behaviour unchanged (changes are comment-only + one YAML line).
  **PASS.**
- **Performance (NFR1)** — n/a; no runtime path changed. No `fix` guard was added.
- **Usability (NFR2)** — the inventory reads standalone (Markdown table in f-implementation-exec.md);
  all new suppressions follow the established `//nolint:gosec // Gxxx: <rationale>` form. **PASS.**

## Test Failures
None. (During implementation, ad-hoc `go test ./cmd/...` reported `dcfhfind executable not
found` and `could not find .dcfh directory`; both were verified pre-existing on clean HEAD via
`git stash` and are environmental — they require the built binaries, which the `.githooks/
pre-commit` path builds first, after which all packages pass under `go test -race`.)

## Coverage Report
No coverage-target change (config/comment/doc task). The clean-gate result is the primary
verification: **0 gosec findings** across the in-scope tree modulo the three documented
architectural excludes (G103, G401, G505).

## Status
**Status**: Finished
**Next Action**: /cwf-retrospective 6 (after user review; backlog retirement pending approval)
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Lessons Learned
- TC-1's build guard mattered: the fresh worktree lacked the gitignored generated
  `constants_version.go`, so the first scratch build failed on `getVersionString` — `go generate`
  in each `cmd/*` dir fixed it before trusting the JSON.
- Process hazard recorded for the retrospective: a `cd "$wt"` into the scratch worktree left
  the shell's CWD there, so later `cd "$(git rev-parse --show-toplevel)"` resolved to the
  *worktree* root; the implementation edits + lint + tests all ran in the worktree and were
  deleted with it. The work was recovered intact from the dangling `git stash` commit
  (`a49e33b`, "task6-verify") via `git fsck --unreachable`, re-applied to the main tree, and
  re-verified before commit. Mitigation: use absolute paths for scratch-worktree work, or
  never `cd` into a disposable worktree from the primary session.

## Security Review

**State**: no findings

no findings: empty changeset

(The `security-review-changeset --phase=testing` helper reviewed 0 files — the testing phase
added only `g-testing-exec.md`, outside the CWF security-relevant pathspec. The substantive
security gates for this task are TC-4/TC-5 above.)
