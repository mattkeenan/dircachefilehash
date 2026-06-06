# Byte-weighted default sort for interactive-tree - Retrospective
**Task**: 12 (feature)

## Task Reference
- **Task ID**: internal-12
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/12-byte-weighted-default-sort-for-interactive-tree
- **Template Version**: 2.1
- **Retrospective Date**: 2026-06-06

## Executive Summary
- **Duration**: 1 session (estimated: 0.5–1 day) — within estimate.
- **Scope**: Final scope matched the requirements-review decision exactly:
  `change_bytes` (Added+Modified+Deleted **bytes**) as the new default,
  `change`→`change_files` rename, `c`/`f` key split, and the full
  deleted-byte plumbing (the larger of the two scope options the user
  was offered at requirements review).
- **Outcome**: Success. The viewer now ranks directories by volume of
  change, directly answering the question that triggered the task ("why
  did this sort here?"). No on-disk format change; non-interactive output
  byte-identical.

## Variance Analysis
### Time and Effort
- **Estimated**: 0.5–1 day, Medium complexity (a-task-plan).
- **Actual**: One working session across all ten phases (a–j).
  Planning (a–e) had been completed in the prior session and reviewed by
  the user before exec; this session ran f–j.
- **Variance**: On estimate. The dual-source deleted-byte design (settled
  in c-) meant exec was mechanical — no rework, no plan deviations.

### Scope Changes
- **Additions**: KD6 stats-pane byte breakdown (`Added: N (size)` …) was
  folded in as a usability affordance — it directly addresses the
  user-confusion origin and reuses data the metric already computes.
- **Removals**: None. The added+modified-only (zero-plumbing) alternative
  was explicitly rejected at requirements review so large deletions rank
  as large changes.
- **Impact**: None negative — the byte fields KD6 needed were already
  required by the sort metric.

### Quality Metrics
- **Test Coverage**: TC-1..TC-16 all PASS (case-list coverage, no numeric
  gate, consistent with the repo). Four canary tests pin the invariants
  most likely to drift (byte aggregation, cross-path byte identity,
  byte-identity regression, no-walk).
- **Defect Rate**: 0 defects found in testing; 0 plan deviations.
- **Performance**: O(nodes) aggregation, no second filesystem walk
  (TC-12), int64 metric (no overflow on large trees).

## What Went Well
- **Design front-loaded the only hard problem.** The deleted-byte
  attribution asymmetry (status retains tombstones; update discards them)
  was flagged as the #1 risk in a-, resolved in c- (KD2 dual-source, one
  "last-known size" rule), so exec had a single code path to implement
  and a single fixture to prove both paths agree.
- **Single-write-site discipline preserved the race argument.** Widening
  the existing `changeCollector.add` (rather than adding a second method)
  kept the "one goroutine, one writer" invariant intact, so `-race`
  stayed clean and the byte-identity guarantee (TC-13) held with no new
  reasoning.
- **int64 end-to-end avoided a gosec suppression.** Keeping byte widths
  `int64` through `metric`/`nodeLess` meant no new G115 `//nolint`.
- **The KD6 pane closes the loop on the originating confusion** — the
  sort order is now self-explaining.

## What Could Be Improved
- **The exec-phase security review never ran semantically.** Both f- and
  g-phase changeset reviews exited `2` (cap exceeded) because this repo
  does not list `**/*_test.go` in
  `security.review.max-lines-exclude-paths`, so the four new test suites
  (411 lines) pushed the production-weighted count over 500 even though
  the production code is ~154 low-risk lines. The contract correctly
  recorded `error` (not a silent "no findings"), but the semantic review
  the gate exists to provide did not happen. Filed as a follow-up.
- **Mishandled tooling output, then over-corrected.** The pre-commit
  `go fix` modernised two unrelated files (`wg.Add`/`go func` →
  `sync.WaitGroup.Go`) during exec. The first instinct — exclude them to
  keep the commit "focused" and track them in BACKLOG — was wrong: it left
  the working tree in permanent limbo and forced history surgery at
  close-out. The right move was to commit tool-produced changes in the
  phase that produced them. Corrected by folding them into Task 12 (a new
  checkpoint commit + rebuilt squash), since they were produced during the
  task and belong with it.

## Key Learnings
### Technical Insights
- **"Last-known size" is the unifying rule for deleted bytes.** Both the
  status tombstone and the update collector resolve to the same value, so
  a single `Stats.DeletedBytes` total is consistent regardless of launch
  path — proven by one cross-path fixture rather than two divergent ones.
- **Reframing a constraint can dissolve it.** "No second *filesystem*
  walk" (task 11) is what made the update-path capture viable: the
  comparison goroutine is the last place the pre-update entry is in hand,
  so capturing there needs no re-stat of the now-absent file.
- **Keep the three byte fields separate, not summed.** The sort needs
  only the sum, but KD6's pane prints each category — a reminder that a
  data-model choice should serve all consumers, not just the headline one.

### Process Learnings
- **Front-loading the #1 risk into the design phase paid for itself.**
  Exec had zero deviations because the only ambiguous decision was made
  (and user-confirmed) before any code was written.
- **The security-cap contract is correct but coarse.** A deterministic
  `error` on cap-exceeded is the right fail-safe, but a repo whose tests
  aren't in the exclude globs will routinely skip the semantic review on
  test-heavy tasks. The fix is config, not contract.
- **Commit tool/hook-produced changes in the phase that produced them.**
  Don't curate them out of the working tree to keep a commit "tidy" — that
  creates a dirty-tree limbo and forces history surgery later. Changes
  made during a task land with that task.

### Risk Mitigation Strategies
- The deleted-byte asymmetry risk (a-) was mitigated by a design decision
  (KD2) plus a single shared fixture (`TestPostRunTree_CrossPathByteIdentity`)
  — the early-flagged risk became the most-tested seam.

## Recommendations
### Process Improvements
- Add `**/*_test.go` (and any other test conventions) to
  `security.review.max-lines-exclude-paths` so the exec-phase semantic
  security review runs on the production diff for test-heavy tasks instead
  of erroring out on the cap.

### Tool and Technique Recommendations
- The "widen the single write site rather than add a method" technique is
  worth reusing whenever a lock-free single-writer collector needs a new
  field — it preserves the race argument for free.

### Future Work (filed to BACKLOG)
- **chore**: add test-file globs to `security.review.max-lines-exclude-paths`.
- Pre-existing items unchanged: wide-rune (CJK) column width (Low).
- (The `go fix` modernisation is NOT a follow-up — it was committed into
  Task 12.)

## Status
**Status**: Finished
**Next Action**: Task complete — suggest merge to integration branch
**Blockers**: None identified
**Completion Date**: 2026-06-06
**Sign-off**: Matt Keenan / Claude Opus 4.8

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Archived Materials
- Planning: a-task-plan.md … e-testing-plan.md (all Finished).
- Exec: f-implementation-exec.md (`bd0ba54`), g-testing-exec.md (`2c4558d`).
- Rollout/maintenance: h-rollout.md (`25b1a09`), i-maintenance.md (`48f480a`).
- Code: pkg/treeview.go, pkg/update.go, pkg/comparison_sink.go, pkg/repo.go,
  pkg/repo_local.go, cmd/dcfh/update.go, cmd/dcfh/internal/tui/{sort,render}.go
  + four test files.
