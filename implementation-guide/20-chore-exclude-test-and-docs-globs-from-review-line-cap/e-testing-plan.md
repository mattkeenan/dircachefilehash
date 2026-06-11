# Exclude test and docs globs from review line cap - Testing Plan
**Task**: 20 (chore)

## Task Reference
- **Task ID**: internal-20
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/20-exclude-test-and-docs-globs-from-review-line-cap
- **Template Version**: 2.1

## Goal
Verify the three new exclude globs (`**/*_test.go`, `docs/**`, `*.md`) are valid
config, resolve to the intended file sets via git's `:(glob,exclude)` engine,
actually discount those lines from the production-weighted cap count, and do
**not** reach into the vendored prompt surface (`.cwf/`/`.claude/`). No runtime
Go code changes → no Go unit tests; verification is config-validation plus
git-pathspec and helper-behaviour checks.

## Test Strategy
### Test Levels
- **Config validation**: `cwf-manage validate` (schema + hash-drift) and JSON
  parse — both satisfied by the validate run.
- **Glob-resolution (unit-equivalent)**: `git ls-files -- ':(glob)<p>'` for each
  new pattern — positive matches (intended files included) and the one
  security-critical negative match (`*.md` is root-only).
- **Mechanism (integration)**: prove the globs actually reduce a measured
  line count via git's own engine, independent of the helper.
- **Helper end-to-end (system)**: run `security-review-changeset` and confirm
  exit code, full-diff count vs production count, and clean stderr.

### Test Coverage Targets
- **Every success criterion** in a-task-plan.md has ≥1 mapped TC.
- **Critical paths**: the discount mechanism (TC-4) and the vendored-boundary
  negative match (TC-3) — both must pass.
- **Regression**: the pre-existing `implementation-guide/**` exclude still
  discounts (this task's own workflow-doc edits), and the helper still emits a
  full (unfiltered) diff so the subagent is never starved.

## Test Cases
### Functional Test Cases

- **TC-1 — config is valid after the edit (SC1, SC5) [critical]**
  - **Given**: the three globs appended to `max-lines-exclude-paths`.
  - **When**: `cwf-manage validate`.
  - **Then**: `validate: OK` (JSON parses; no schema/hash drift). The array
    reads `["implementation-guide/**", "**/*_test.go", "docs/**", "*.md"]`.

- **TC-2 — each new glob resolves to its intended file set (SC2) [positive]**
  - **Given**: the working tree.
  - **When**: `git ls-files -- ':(glob)**/*_test.go'`, `':(glob)docs/**'`,
    `':(glob)*.md'`.
  - **Then**: `**/*_test.go` lists root **and** nested tests (e.g.
    `cmd/dcfh/dcfh_test.go`, `cmd/dcfh/internal/tui/render_test.go`); `docs/**`
    lists the docs tree; `*.md` lists the four root files
    (README/CHANGELOG/CLAUDE/BACKLOG).

- **TC-3 — `*.md` does NOT match vendored Markdown (SC4) [critical, security]**
  - **Given**: the working tree.
  - **When**: `git ls-files -- ':(glob)*.md'`.
  - **Then**: output contains **only** root-level `.md` — **no** `.cwf/`,
    `.claude/`, or `.cwf-*` path. This is the boundary that keeps the workflow/
    skill prompt-injection surface under the cap (the rationale for `*.md` over
    `**/*.md`).

- **TC-4 — the new globs actually discount lines from the cap count (SC2)
  [critical, mechanism]**
  - **Given**: a representative historical commit range that touched test files
    and/or `docs/**` (chosen at exec time from `git log`).
  - **When**: compare `git diff --numstat <range>` summed added+deleted (a)
    against the same with `-- . ':(glob,exclude)**/*_test.go'
    ':(glob,exclude)docs/**' ':(glob,exclude)*.md'` applied (b).
  - **Then**: `b < a`, and the difference equals the added+deleted lines of the
    excluded test/docs files — proving git's exclude engine discounts exactly
    those paths (this is the same engine `count_production_lines` uses).

- **TC-5 — helper runs clean and still emits the full diff (SC3) [critical]**
  - **Given**: this task's branch (anchor = Task 19 tip `a22530d`).
  - **When**: `security-review-changeset --wf-step=testing-exec`.
  - **Then**: exit 0; the `wrote N lines` confirmation has **N > 0** (full diff
    non-empty → the security subagent is still invoked, not skipped); the
    stderr summary's `(M production)` figure is ≤ 500 (cap not tripped); no
    `warning:` deprecation line. Confirms excludes relax the *count* without
    starving the reviewer.

### Non-Functional Test Cases
- **Security**: TC-3 is the security gate — the vendored prompt surface stays
  under the cap. Additionally, the change is data (git pathspecs), not code: the
  globs flow only into git's `:(glob,exclude)` engine, never a shell.
- **Reliability**: a malformed exclude pattern would make git fatal → helper
  exit 1 (fail-safe: the path then counts as production, cap fires earlier not
  later). TC-1 + TC-5 passing confirm the three patterns are well-formed and
  git-accepted.
- **Performance / Usability**: N/A — config-only, no runtime path.

## Test Environment
### Setup Requirements
- The repo on branch `chore/20-…`, baseline `a22530d`; `git` and
  `cwf-manage`/`security-review-changeset` available. No DB, no network, no Go
  build required.

### Automation
- All checks are single shell commands run manually in g-testing-exec; no CI
  wiring. The exec-phase security review (Step 8 of testing-exec) is itself a
  live exercise of TC-5.

## Validation Criteria
- [ ] TC-1…TC-5 pass; criticals (TC-1, TC-3, TC-4, TC-5) clean.
- [ ] `cwf-manage validate` = OK.
- [ ] `*.md` confirmed root-only (no vendored path).
- [ ] Helper exits 0, full-diff N > 0, production count ≤ cap, no deprecation
  warning.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All five planned test cases executed in g-testing-exec.md — TC-1…TC-5 PASS, all four
criticals (TC-1, TC-3, TC-4, TC-5) clean. The mechanism test (TC-4) proved the
discount equals the excluded files' own line total exactly (830 = 830), and the
security boundary test (TC-3) confirmed `*.md` is root-only. See g-testing-exec.md.

## Lessons Learned
Mechanism-level tests beat assertion-level ones: TC-4 proved git's `:(glob,exclude)`
engine discounts the right bytes, which would catch a future regression in
`count_production_lines` that a "cap is lower" assertion would miss.
