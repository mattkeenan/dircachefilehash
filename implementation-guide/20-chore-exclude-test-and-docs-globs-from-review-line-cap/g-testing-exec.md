# Exclude test and docs globs from review line cap - Testing Execution
**Task**: 20 (chore)

## Task Reference
- **Task ID**: internal-20
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/20-exclude-test-and-docs-globs-from-review-line-cap
- **Template Version**: 2.1

## Goal
Execute the tests defined in e-testing-plan.md and verify implementation from d-implementation-plan.md.

## Execution Checklist
- [x] Read e-testing-plan.md and d-implementation-plan.md thoroughly
- [x] Verify test environment ready
- [x] Execute test cases sequentially
- [x] Record pass/fail for each test
- [x] Document failures with reproduction steps
- [x] Update status to "Testing" when in progress, "Finished" when all pass

## Test Results

### Functional Tests

| Test ID | Test Case | Expected | Actual | Status | Notes |
|---------|-----------|----------|--------|--------|-------|
| TC-1 | Config valid after edit (SC1, SC5) | `validate: OK`; array = `["implementation-guide/**", "**/*_test.go", "docs/**", "*.md"]` | `[CWF] validate: OK`; array matches | PASS | critical; no schema/hash drift |
| TC-2 | Each glob resolves to intended set (SC2) | `**/*_test.go` → root+nested; `docs/**` → docs tree; `*.md` → 4 root files | `**/*_test.go` → 80 files (root + nested, e.g. `cmd/dcfh/dcfh_test.go`, `cmd/dcfh/internal/tui/render_test.go`); `docs/**` → 10 files; `*.md` → 4 root | PASS | positive |
| TC-3 | `*.md` does NOT match vendored md (SC4) | only root `.md`; no `.cwf/`/`.claude/`/`.cwf-*` | exactly `BACKLOG.md`, `CHANGELOG.md`, `CLAUDE.md`, `README.md` — no vendored path | PASS | **critical, security** — vendored prompt surface stays cap-counted |
| TC-4 | Globs actually discount cap lines (SC2) | `b < a`; discount = excluded files' own added+deleted | range `826fab4..a22530d`: A=5286, B=4456, discount=830 = excluded files' own 830 | PASS | **critical, mechanism** — git exclude engine discounts exactly those paths |
| TC-5 | Helper runs clean, still emits full diff (SC3) | exit 0; N>0; production ≤ 500; no `warning:` | exit 0; N=526; 0 production; anchor=a22530d; no deprecation warning | PASS | critical; subagent invoked not skipped |

### Non-Functional Tests
- **Security (TC-3)**: PASS — `git ls-files -- ':(glob)*.md'` returns only the four
  root files; no `.cwf/`, `.claude/`, or `.cwf-*` path. The vendored
  workflow/skill prompt-injection surface stays under the cap (the rationale for
  `*.md` over `**/*.md`). The change is data (git pathspecs) flowing into git's
  `:(glob,exclude)` engine, never a shell.
- **Reliability**: PASS by construction — TC-1 (`validate: OK`) and TC-5 (helper
  exit 0) confirm all three patterns are well-formed and git-accepted. A malformed
  pattern would make git fatal → helper exit 1 (fail-safe: the path then counts as
  production, cap fires earlier not later).
- **Performance / Usability**: N/A — config-only, no runtime path.

## Test Failures

None. All five test cases passed; all four criticals (TC-1, TC-3, TC-4, TC-5) clean.

## Coverage Report

Every success criterion in a-task-plan.md maps to ≥1 passing TC (SC1→TC-1; SC2→TC-2,
TC-4; SC3→TC-5; SC4→TC-3; SC5→TC-1). No Go runtime code changed, so there are no Go
unit tests to run; verification is config-validation + git-pathspec resolution +
helper cap-count behaviour, exactly as scoped in e-testing-plan.md.

## Status
**Status**: Finished
**Next Action**: /cwf-retrospective
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All test cases executed on branch `chore/20-…`, anchor `a22530d`. TC-4's discount
(830 lines) matched the excluded files' own line total exactly, proving git's
`:(glob,exclude)` engine — the same one `count_production_lines` uses — discounts
precisely the intended paths and nothing else. TC-5 confirmed the helper still emits
the full unfiltered diff (N=526 > 0) so the security subagent is invoked, while the
production-weighted count is 0 (well under the 500 cap): the excludes relax the
*count* without starving the reviewer.

## Lessons Learned
TC-4's exact-match result (discount 830 = excluded files' own 830) is the regression
anchor: it pins git's exclude engine, not just the helper's output. See
j-retrospective.md.

## Security Review

**State**: no findings

Helper: `security-review-changeset --wf-step=testing-exec` → exit 0, wrote 526
lines (0 production), anchor=a22530d, no deprecation warning. N>0 so the subagent
was invoked (not skipped); 0 production lines (all 5 changed files are under the
existing `implementation-guide/**` exclude). Subagent verdict verbatim:

The changeset is entirely CWF workflow documentation and a single config-key edit. Let me reason through the threat model.

## Review

**What this changeset contains.** Five files, all under `implementation-guide/`:
- Four new CWF process-doc markdown files for Task 20 (a-task-plan, d-implementation-plan, e-testing-plan, f-implementation-exec).
- One four-line edit to `implementation-guide/cwf-project.json` appending `**/*_test.go`, `docs/**`, and `*.md` to `security.review.max-lines-exclude-paths`.

No Go, Perl, shell, Makefile, `.golangci.yml`, or build-contract change. The only functional change is data: three git-pathspec glob strings added to a JSON array.

**(a) Bash injection / unsafe command construction.** No shell commands are introduced or modified. The three added strings are git pathspec globs consumed by `security-review-changeset`, which (per security-review.md:45) hands them to git's own `:(glob,exclude)` engine — `git diff --numstat -- … ':(glob,exclude)<pattern>'`, list-form argv, no shell interpolation. A malformed pattern is a fail-safe: git exits non-zero, the helper exits 1, and the SKILL records `error` rather than silently passing. No injection surface. Clean.

**(b) Perl/git-output parsing without `-z`.** No Perl helper is added or modified. The consuming helper is unchanged by this diff; only its input data (the JSON array) changes. No new git-porcelain parsing. Clean.

**(c) Prompt injection via user-supplied strings.** The new markdown is CWF process documentation, not a SKILL prompt template with `{arguments}` substitution — no new LLM-context substitution point and no routing of untrusted free-text into a tool-selection decision. The second-order effect is worth noting and is *protective*: the change's purpose is to lower the production-weighted cap count so test/docs-heavy tasks invoke this subagent instead of skipping it — strictly more review coverage. Per security-review.md:45, excluded paths are still emitted in full to the reviewer; the glob relaxes only the cap count, never what the subagent reads. No content is hidden. Clean.

**(d) Unsafe environment-variable handling.** No env vars are read, added, or routed into path/clone/chmod operations. Clean.

**(e) Pattern-based risks (safe-here-but-risky-elsewhere).** Two forward-looking observations, both already reasoned about by the plan authors; I concur and record the invariants:

1. **`*.md` is deliberately root-only.** A bare `*` does not cross `/` in git pathspec semantics, so `:(glob)*.md` matches only `README.md`/`CHANGELOG.md`/`CLAUDE.md`/`BACKLOG.md` — not the vendored `.cwf/**`, `.claude/**`, or `.cwf-*` mirror prompt files (verified in f-implementation-exec TC-3). This is the correct security choice: the vendored prompt surface is exactly where a prompt-injection edit would hide, so keeping it cap-counted means a bulk edit there still trips the 500-line cap and forces `error`/halt rather than riding under a relaxed count. **Safe here because the chosen glob is `*.md` (root-only), not `**/*.md`; audit any future edit to `max-lines-exclude-paths` that broadens `*.md` → `**/*.md` or adds a `.cwf/**`/`.claude/**` exclude — that invariant (vendored prompt surface stays cap-counted) would no longer hold.**

2. **`docs/**` is content-agnostic.** It discounts everything under `docs/`, not just Markdown; a future non-doc file dropped there (e.g. a `.go` fixture) would be silently discounted from the cap count. The plan flags this consciously (d-plan ~line 164). Residual risk is bounded: the cap only governs whether the subagent is *invoked*, the full diff is *always* emitted, and the worst case is the subagent being invoked on a large `docs/**` change anyway — the desired direction. **Safe here because `docs/` currently holds only docs; audit if `docs/` ever accrues executable fixtures whose review depends on the cap count.**

Both (e) items are observations about future misuse with the invariant named, not defects in this diff. The change is fail-safe in direction (security-review.md:49): any unconfigured/unmatched path counts as production, so the cap fires earlier, never later, and the worst case of these globs is *more* subagent invocation, not less. The one genuine forward-looking item — keep `*.md` root-only, never add a `.cwf/**`/`.claude/**` exclude — is already documented as a conscious out-of-scope decision (a-plan SC4, d-plan "Out of scope").

No actionable security concerns in this changeset.

```cwf-review
state: no findings
summary: Config-only cap-exclude edit plus CWF process docs; globs flow into git's :(glob,exclude) engine (no shell), full diff is always reviewed, and *.md is correctly root-only so the vendored prompt surface stays cap-counted.
```
