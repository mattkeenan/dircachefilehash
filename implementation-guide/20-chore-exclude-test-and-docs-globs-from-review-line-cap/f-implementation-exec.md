# Exclude test and docs globs from review line cap - Implementation Execution
**Task**: 20 (chore)

## Task Reference
- **Task ID**: internal-20
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/20-exclude-test-and-docs-globs-from-review-line-cap
- **Template Version**: 2.1

## Goal
Execute the implementation following the plan in d-implementation-plan.md and e-testing-plan.md.

## Execution Checklist
- [x] Read d-implementation-plan.md and e-testing-plan.md thoroughly
- [x] Verify all prerequisites met
- [x] Execute implementation steps sequentially
- [x] Update "Actual Results" for each step
- [x] Document any deviations from plan
- [x] Update status to "Finished" when complete

## Implementation Steps (from d-implementation-plan.md)

Single-file config edit: append `**/*_test.go`, `docs/**`, `*.md` to
`security.review.max-lines-exclude-paths` in `implementation-guide/cwf-project.json`.

## Actual Results

### Step 1: Setup
- **Planned**: Confirm branch `chore/20-…`, tree clean, baseline `a22530d`.
- **Actual**: On branch `chore/20-exclude-test-and-docs-globs-from-review-line-cap`;
  HEAD `1d754b7` (Task 19 tip `a22530d` is the recorded baseline anchor); tree clean
  apart from the untracked f/g/j workflow templates.
- **Deviations**: None.

### Step 2: Edit config
- **Planned**: Change `max-lines-exclude-paths` from `["implementation-guide/**"]`
  to add the three new globs, preserving 2-space indentation and key order.
- **Actual**: Array now reads
  `["implementation-guide/**", "**/*_test.go", "docs/**", "*.md"]`. Indentation and
  key order preserved.
- **Deviations**: None.

### Step 3: Validate
- **Planned**: `cwf-manage validate` passes; each glob resolves; negative-match
  assertion (`*.md` root-only); helper reports a sane production-weighted count.
- **Actual**:
  - `.cwf/scripts/cwf-manage validate` → `[CWF] validate: OK` (script lives at
    `.cwf/scripts/cwf-manage`, not under `command-helpers/` — minor path note).
  - `git ls-files -- ':(glob)**/*_test.go'` → root **and** nested tests
    (`cmd/dcfh/dcfh_test.go`, `cmd/dcfh/internal/tui/render_test.go`, …). ✓
  - `git ls-files -- ':(glob)docs/**'` → the whole `docs/` tree. ✓
  - `git ls-files -- ':(glob)*.md'` → **only** `BACKLOG.md`, `CHANGELOG.md`,
    `CLAUDE.md`, `README.md` — **no** `.cwf/`, `.claude/`, or `.cwf-*` path. ✓
    (TC-3 security boundary: the vendored prompt surface stays under the cap.)
  - Helper run on this changeset: see ## Security Review below.
- **Deviations**: None.

## Blockers Encountered

None.

## Deferral Check
Before marking status=Finished, verify:
- [x] All steps from d-implementation-plan.md executed
- [x] All success criteria from a-task-plan.md met (BACKLOG/CHANGELOG updates are
  retrospective-phase per CWF convention)
- [x] All requirements from b-requirements-plan.md addressed (N/A — chore, b skipped)
- [x] All design guidance in c-design-plan.md followed (N/A — chore, c skipped)
- [x] No planned work deferred without user approval
- [x] If work deferred: Follow-up task created and linked (N/A)

## Status
**Status**: Finished
**Next Action**: /cwf-testing-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Security Review

**State**: no findings

Helper: `security-review-changeset --wf-step=implementation-exec` → exit 0, wrote
398 lines (0 production), anchor=a22530d, no deprecation warning. N>0 so the
subagent was invoked (not skipped); 0 production lines (all 4 changed files are
under the existing `implementation-guide/**` exclude). Subagent verdict verbatim:

I have the full changeset and the threat model. Let me analyse this changeset.

This changeset is entirely documentation and a single config-key edit. Specifically:

1. Three new CWF workflow-doc markdown files under `implementation-guide/20-…/` (a-task-plan, d-implementation-plan, e-testing-plan).
2. One four-line edit to `implementation-guide/cwf-project.json` that appends `**/*_test.go`, `docs/**`, and `*.md` to `security.review.max-lines-exclude-paths`.

No Go, Perl, shell, Makefile, or build-contract changes. Let me reason through each threat category.

**(a) Bash injection / unsafe command construction.** No shell commands are introduced or modified. The config values are git pathspec globs that flow into git's `:(glob,exclude)` engine inside `security-review-changeset`, not into a shell. The plan docs explicitly verified this ("Git owns the matching — no Perl-side path classification", d-plan ~line 128; "the globs flow only into git's `:(glob,exclude)` engine, never a shell", e-plan ~line 346). No injection surface. Clean.

**(b) Perl helpers consuming git/user output without `-z`.** No Perl helper is added or modified. The consuming helper (`security-review-changeset`) is unchanged by this diff — only its data input (the JSON config array) changes. No new git-output parsing. Clean.

**(c) Prompt injection via user-supplied strings.** The added markdown is CWF process documentation, not LLM-context-bearing prompt templates with `{arguments}` substitution. None of the new content introduces a new substitution point or routes untrusted free-text into a tool-selection decision. Worth noting the second-order effect, however: this very review is the downstream consumer of the cap config. The change's *purpose* is to lower the production-weighted count so test/docs-heavy tasks invoke this subagent rather than skipping it — that is strictly *more* review coverage. The plan correctly establishes (a-plan SC3, security-review.md:45) that excluded paths are still emitted in full to the reviewer; the glob only relaxes the cap count, never what the subagent reads. So no content is hidden from review. Clean.

**(d) Unsafe environment-variable handling.** No env vars are read, added, or routed into path/clone/chmod operations. Clean.

**(e) Pattern-based risks (safe-here-but-risky-elsewhere).** This is the one category worth a substantive note, and the plan authors reasoned about it themselves — I concur with their conclusion and record the invariant explicitly:

- The `*.md` glob is deliberately root-only (a bare `*` does not cross `/` in git pathspec semantics), and `**/*.md` was explicitly rejected (d-plan ~line 136, TC-3) precisely because it would discount the vendored `.cwf/**`, `.claude/**`, and `.cwf-*` mirror prompt files from the cap. That is the correct security decision: the vendored prompt surface is exactly where a prompt-injection edit would hide, so keeping it under the cap means a bulk edit there still trips the cap and forces an error/halt rather than riding under a relaxed count. **Safe here because the chosen glob is `*.md` (root-only), not `**/*.md`; audit future edits to `max-lines-exclude-paths` where someone might "broaden" `*.md` to `**/*.md` or add a `.cwf/**`/`.claude/**` exclude — that invariant (vendored prompt surface stays cap-counted) would no longer hold.**

- The `docs/**` glob is content-agnostic: it discounts *everything* under `docs/`, not just Markdown. Today that tree is all docs, but a future non-doc file dropped there (e.g. a `.go` fixture) would be silently discounted from the cap count. The plan flags this consciously (d-plan ~line 164). The residual risk is bounded: the cap only governs whether the subagent is *invoked*, and the full diff is *always* emitted regardless — so even a discounted `docs/**` file is still reviewed in full. The only failure mode is a very large all-in-`docs/**` change slipping under the 500-line cap and the subagent being invoked on it anyway (which is the desired direction). No content is ever hidden. Acceptable, and not an actionable defect.

Both (e) notes are observations about future misuse with the invariant named, not defects in this diff. The change is fail-safe in direction: per security-review.md:49, any unconfigured/unmatched path counts as production, so the cap fires earlier, never later; and the worst case of these globs is *more* subagent invocation, not less.

No actionable security concerns in this changeset. The one genuine forward-looking item — keep `*.md` root-only and never add a `.cwf/**`/`.claude/**` exclude — is already documented as a conscious out-of-scope decision in the plan (a-plan SC4, d-plan "Out of scope").

```cwf-review
state: no findings
summary: Config-only cap-exclude edit plus CWF process docs; globs flow into git's :(glob,exclude) engine (no shell), full diff still always reviewed, and *.md is correctly root-only to keep the vendored prompt surface cap-counted.
```

## Lessons Learned
The change is self-demonstrating: this task's own exec review ran the subagent at 0
production-weighted lines (changed files under `implementation-guide/**`), a live
proof the gate runs rather than skips. See j-retrospective.md.
