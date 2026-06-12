# Add runnable Repo library usage examples - Retrospective
**Task**: 26 (chore)

## Task Reference
- **Task ID**: internal-26
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/26-add-runnable-repo-library-usage-examples
- **Template Version**: 2.1
- **Retrospective Date**: 2026-06-12

## Executive Summary
- **Duration**: <1 day (estimated: <1 day, variance: ~0%)
- **Scope**: Delivered as planned — one new black-box example file covering the whole
  `Repo` consumer surface. The a-plan's opportunistic "fold in filter-DSL godoc gaps"
  was dropped at d-plan review (already documented by group comments); no other change.
- **Outcome**: Success. Eight runnable `Example*` functions ship in
  `pkg/example_repo_test.go`, all executed with verified `// Output:`; two backlog
  items resolved (one done, one superseded). No production behaviour change.

## Variance Analysis
### Time and Effort
- **Estimated** (chore flow = a/d/e/f/g/j; no requirements/design/rollout phases):
  - Planning (a): minor
  - Implementation plan (d): minor
  - Testing plan (e): minor
  - Implementation exec (f): bulk of the effort
  - Testing exec (g): minor
- **Actual**: Matched the estimate. Effort concentrated in f (writing the eight
  examples + helper and verifying every `// Output:` against the real API). No phase
  overran; total well under the <1-day estimate.
- **Variance**: Negligible. The Low-complexity / test-only / no-dependency framing
  in the a-plan held exactly.

### Scope Changes
- **Additions**: None.
- **Removals**: The opportunistic filter-DSL godoc edit (`MMinTest`, `CTimeTest`,
  `CMinTest`, `OrExpression`, `NotExpression`) was **dropped at d-plan review** — those
  AST nodes are already documented by deliberate group comments (`filter.go:445`,
  `:523`); the survey's "undocumented" finding was a heuristic miss. Adding per-type
  comments would have duplicated the established convention. This kept the task purely
  a new `_test.go` file with zero production changes.
- **Impact**: Positive — narrower, lower-risk changeset; no contradiction of an
  existing doc convention; the "Update API documentation" backlog item was confirmed
  fully superseded by Task 17 rather than partially reopened here.

### Quality Metrics
- **Test Coverage**: Documentation-coverage target met — every method on the consumer
  surface has ≥1 executed `Example` (`CreateRepo`, `OpenRepo`, `Repo.Diff`/`Apply`/
  `Groups`/`Filter`, `Repo.Config` Get+Set, `Repo.Snapshots` Create+List). 8/8 carry a
  verified `// Output:`. Not a line/branch % target — this is documentation coverage of
  the public API, not code-path coverage of production logic.
- **Defect Rate**: Zero defects. All eight examples passed first integrated run;
  `-count=2` stable (determinism); `go vet` examples analyser clean.
- **Performance**: N/A — three tiny seeded files per example, each <0.01s.

## What Went Well
- **Single shared fixture serves every example**: one seed set (two identical `.go`
  files + one `.txt`) simultaneously satisfies Diff (3 added), Apply (3 indexed),
  Groups (1 dupe group) and Filter (2 `*.go`, sorted). Minimal helper, maximal reuse.
- **Plan review caught two real ordering/shape bugs before code**: (1) index-only verbs
  (`Groups`/`Filter`) need a prior `Apply` or counts come back 0; (2) `Filter` errors on
  empty `Actions`. Both were encoded in the d-plan and avoided in exec — no wasted runs.
- **Black-box package choice proved its worth**: examples exercise only the exported
  surface through the `Repo` interface, so they double as a compile-time guard against
  public-API signature drift (`go test ./pkg/...` fails on drift).
- **Determinism discipline held**: only stable derived values (counts, fixed sorted
  names) appear in `// Output:`; no timestamp/temp-path/unsorted-slice leakage.
- **Both security reviews clean** (f and g): test-only file, literal-constant temp
  writes (taint-free), no shell/env/prompt-injection surface.

## What Could Be Improved
- **Survey heuristic over-reported missing godoc**: the a-plan listed five "undocumented"
  filter-DSL AST nodes that were in fact documented by group comments. The miss was
  caught at d-plan review, but a grep that recognises group-comment conventions would
  have avoided carrying the false finding into planning.
- **`go doc` CLI surprise**: the `go doc` CLI does not render `Example*` bodies (only the
  godoc HTTP server does), so the authoritative association check is `go vet`'s examples
  analyser. Worth remembering for any future example-doc task to avoid a misleading
  "examples don't show up" spot-check.

## Key Learnings
### Technical Insights
- A single carefully-chosen seed set can drive every example deterministically; design
  the fixture for the union of all verbs rather than one fixture per example.
- Index-reading verbs (`Groups`, `Filter`) operate on the main index, not the live
  filesystem — examples must `Apply` first. This mirrors the `dcfh` CLI's status→update
  relationship and is a useful teaching point the examples now encode implicitly.
- `go vet`'s examples analyser — not `go doc` — is the authoritative check that example
  names map to real symbols.

### Process Learnings
- The chore flow (a/d/e/f/g/j, skipping b/c/h/i) fit this task exactly; forcing
  requirements/design/rollout phases would have been pure overhead for a test-only add.
- Plan review earned its place: the two pre-code corrections (Apply-before-verb, non-empty
  Actions) would otherwise have surfaced as failed example runs during exec.

### Risk Mitigation Strategies
- The a-plan's top risk (non-deterministic `// Output:`) was pre-mitigated by printing
  only stable derived values; it never materialised.
- The "examples drift from API" risk is structurally mitigated: examples compile against
  `pkg`, so drift fails the build rather than landing silently.

## Recommendations
### Process Improvements
- When a survey flags "undocumented" symbols, verify against group-comment conventions
  before recording the finding — group comments are a legitimate documentation form here.

### Tool and Technique Recommendations
- For any future godoc-example work in this repo: validate example↔symbol association
  with `go vet ./pkg/...`, not the `go doc` CLI.

### Future Work
- None identified. Both originating backlog items are resolved; no technical debt
  incurred; no follow-up task required.

## Status
**Status**: Finished
**Next Action**: Task complete
**Blockers**: None identified
**Completion Date**: 2026-06-12
**Sign-off**: Claude (CWF retrospective, Task 26)

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Archived Materials
- Planning: `a-task-plan.md`, `d-implementation-plan.md`, `e-testing-plan.md`
- Execution: `f-implementation-exec.md`, `g-testing-exec.md`
- Deliverable: `pkg/example_repo_test.go`
- Checkpoint commits: 50a525e0 (a), 860b8cfa (d), c046efaf (e), f45e99f2 (f), 2ddb99d3 (g)
- Baseline commit: 0eae57f8
- Security reviews: clean (no findings) at both f and g — recorded verbatim in those files
