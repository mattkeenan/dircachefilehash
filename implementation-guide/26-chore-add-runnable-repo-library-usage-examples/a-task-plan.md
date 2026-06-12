# Add runnable Repo library usage examples - Plan
**Task**: 26 (chore)

## Task Reference
- **Task ID**: internal-26
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/26-add-runnable-repo-library-usage-examples
- **Baseline Commit**: 0eae57f8e5cfc5dd924151b50ba5179fd06ae9c9
- **Template Version**: 2.1

## Goal
Add `Example*` (`example_*_test.go`) functions for the `Repo` library surface so
external consumers have runnable, godoc-rendered usage guidance instead of having
to read source for the entry points.

## Background / Survey Findings
- Confirmed gap: `pkg/` has **no** `example_*_test.go` and no `examples/` dir —
  the "Add usage examples for library consumers" backlog item is genuinely open.
- The sibling backlog item "Update API documentation with current architecture" is
  **largely stale**: Task 17 rewrote `pkg/doc.go` to v0.7 and resynced `docs/`.
  `pkg/repo.go` (the consumer surface: `Repo`/`OpenRepo`/`CreateRepo` + all
  request/result types) is already fully godoc-documented. Only a few internal
  filter-DSL AST nodes (`MMinTest`, `CTimeTest`, `CMinTest`, `OrExpression`,
  `NotExpression`) lack doc comments. This item should be **retired as superseded**,
  with those trivial godoc gaps folded into this task opportunistically.
- Consumer surface to exemplify (`pkg/repo.go`):
  `CreateRepo`, `OpenRepo`; `Repo.Info`/`Stats`/`Diff`/`Apply`/`Groups`/`Filter`;
  `Snapshots()` (Create/List/Prune/Delete); `Config()` (Get/Set).

## Success Criteria
- [ ] `example_*_test.go` file(s) in `pkg/` provide `Example*` functions covering
      init/open, `Diff`, `Apply`, `Groups`, `Filter`, `Config`, and `Snapshots`.
- [ ] `go test ./pkg/...` passes; examples carrying `// Output:` are executed and
      verified against deterministic output; the rest compile against the real API.
- [ ] `go doc ./pkg` / godoc renders each example under its associated symbol.
- [ ] No production (`.go` non-test) behaviour change; existing tests still pass.
- [ ] Both backlog items resolved: "Add usage examples" retired as done; "Update
      API documentation" retired as superseded by Task 17.

## Original Estimate
**Effort**: <1 day
**Complexity**: Low
**Dependencies**: None — examples compile against the existing `pkg` API.

## Major Milestones
1. **Harness**: A deterministic example fixture (temp dir → `CreateRepo` → seed
    files) that examples can reuse without leaking timestamps/abs-paths into output.
2. **Core verbs**: `Example`s for `Diff` (status), `Apply` (update), `Groups`
    (dupes), `Filter` (find), with verified `// Output:` where output is stable.
3. **Ancillary surface**: `Example`s for `Config` Get/Set and `Snapshots`
    Create/List; compile-only where output is inherently non-deterministic.
4. **Verify + retire**: `go test ./pkg/...` + godoc render check; fold in the
    trivial filter-DSL godoc gaps; retire both backlog items.

## Risk Assessment
### High Priority Risks
- **Non-deterministic `// Output:` blocks** (timestamps, temp paths, entry
  ordering) make verified examples flaky.
  - **Mitigation**: Print only stable derived values (e.g. counts, fixed names) in
    `// Output:` examples; use compile-only `Example`s (no `// Output:`) for the
    rest — these still guarantee compilation and render in godoc.

### Medium Priority Risks
- **Scope creep** into a full godoc rewrite (the stale-but-mostly-done docs item).
  - **Mitigation**: godoc edits limited to the handful of undocumented filter-DSL
    AST nodes; package-level docs (Task 17) are out of scope.
- **Examples drifting from the real API** over time.
  - **Mitigation**: Examples live in `pkg/` and compile against it, so
    `go test ./pkg/...` fails on any signature drift — drift cannot land silently.

## Dependencies
- None blocking. Pure test-file additions against the existing `pkg` surface.

## Constraints
- Examples are `_test.go` files only — no production code change.
- Go example semantics: only `Example*` funcs with a trailing `// Output:` are
  executed/verified; those without are compiled but not run.
- British spelling in prose/comments; match existing test-file conventions.

## Decomposition Check
- [ ] **Time**: Will this take >1 week? No (<1 day).
- [ ] **People**: >2 people on different parts? No.
- [ ] **Complexity**: 3+ distinct concerns? No — single concern (example coverage).
- [ ] **Risk**: High-risk components needing isolation? No (test-only).
- [ ] **Independence**: Can parts be worked separately? Not meaningfully.

Decision: **No decomposition** — 0 signals triggered.

## Status
**Status**: Finished
**Next Action**: /cwf-requirements-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Delivered as planned and within the <1-day / Low-complexity estimate. All success
criteria met: eight `Example*` functions in `pkg/example_repo_test.go` cover the full
consumer surface; `go test ./pkg/...` passes with all `// Output:` verified; godoc
association confirmed via `go vet`; zero production change; both backlog items resolved.
The one milestone-4 sub-item (fold in filter-DSL godoc gaps) was dropped at d-plan
review — those nodes are already documented by group comments. Full analysis in
`j-retrospective.md`.

## Lessons Learned
See `j-retrospective.md` — key points: design one seed fixture for the union of all
verbs; index-reading verbs need a prior `Apply`; `go vet` (not `go doc` CLI) is the
authoritative example↔symbol check.
