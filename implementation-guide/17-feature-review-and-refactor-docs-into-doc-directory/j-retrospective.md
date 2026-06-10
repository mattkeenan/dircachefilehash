# Review and refactor docs into doc directory - Retrospective
**Task**: 17 (feature)

## Task Reference
- **Task ID**: internal-17
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/17-review-and-refactor-docs-into-doc-directory
- **Template Version**: 2.1
- **Retrospective Date**: 2026-06-10

## Executive Summary
- **Duration**: ~1 working session (estimated: 1–2 days; well under estimate).
- **Scope**: Delivered as planned — relocate five docs into `docs/`, rewrite the
  root README around the CLI trio, fix references, mark historical docs, add a
  tagged `docs/README.md` index. One optional micro-fix deliberately skipped; one
  doc fix turned out larger than planned (§6). No scope added or dropped.
- **Outcome**: Success. All five success criteria (SC1–SC5) met; 9/9 test cases
  pass; build + full test suite green; the only `.go` change is two comment lines.
  Unblocks a clean public release (the stated downstream purpose).

## Variance Analysis
### Time and Effort
- **Estimated**: 1–2 days, Medium complexity (a-task-plan.md).
- **Actual**: Single session across the full a→j workflow. No phase stalled.
- **Variance**: Under estimate. The "Medium" complexity sat almost entirely in
  *verification* (is each cited symbol still real?), not in editing. Because the
  design phase front-loaded the FR3 code-grounded classification, implementation
  was mechanical and fast.

### Scope Changes
- **Additions**: none.
- **Removals / deliberate skips**:
  - The optional `:93` `architecture-v0.6.md → v0.7.md` micro-fix in
    `ARCHITECTURE-IMPROVEMENTS.md` was **not** applied — it sits inside a
    struck-through *closed-item* record describing a past action; editing it would
    falsify a history note. Kept that file a pure rename (zero content change).
- **In-flight enlargement (not a scope change, a correction)**: the §6 RWMutex fix
  in `ARCHITECTURE.md` was planned as a load-bearing→defensive *reframe*, but the
  cited writer `appendEntryToNamedIndex@pkg/index.go:967` proved **non-existent**
  too (like the already-known phantom `AppendEntryToScanIndex@index.go:1008`), so
  the whole stale mremap-mechanism paragraph was replaced, consistent with
  CLAUDE.md. Caught only because every citation was verified against source.

### Quality Metrics
- **Test Coverage**: N/A in the unit sense (no runtime code). AC coverage:
  every AC1–AC7 + every applicable NFR mapped to an executed check; 9/9 PASS.
- **Defect Rate**: zero defects found in the deliverable. Two *upstream* doc
  inaccuracies discovered and fixed (the two phantom function citations).
- **Performance**: N/A (NFR1 N/A throughout — no runtime change).

## What Went Well
- **Verify-before-edit paid off twice.** Treating the drifted docs as untrusted
  and grepping every cited symbol/file/line against `cmd/`/`pkg/` caught two
  phantom function references the plan had carried forward from the docs
  themselves. A trust-the-doc approach would have copied the errors.
- **FR3 code-grounded classification was the leverage point.** Doing the
  CURRENT/HISTORICAL/NEEDS-REWRITE triage in the design phase (with file/line
  evidence) made implementation a checklist rather than a judgement call.
- **`git mv` preserved history cleanly** — `--follow` shows pre-move history; the
  relocation is a trivial, reversible single-`git revert`.
- **Right-sizing the service-ops templates.** Rollout/maintenance templates assume
  a running service; reframing them to "trunk merge" and "doc-drift prevention"
  kept the docs honest instead of inventing N/A uptime/SLA/canary ceremony.

## What Could Be Improved
- **The security-review cap fired on both exec and testing-exec** (2604 / 2632
  production lines > 500) because no `security.review.max-lines-exclude-paths`
  glob covers `docs/` or `*.md`, so prose counts as "production". For a docs-heavy
  task the changeset subagent never ran — recorded `State: error` both times. The
  rationale is sound (only two `pkg/doc.go` comments are non-Markdown), but the
  gate produced an `error` token rather than a clean "reviewed" signal. A `docs/`
  / `*.md` exclude glob would let the gate run against the genuine code surface.
- **The plan inherited a phantom citation.** §6's `appendEntryToNamedIndex:967`
  was wrong in the plan because it was wrong in the doc. Plans that quote the
  artefact under repair should flag such citations as "verify at exec time".

## Key Learnings
### Technical Insights
- Drifted documentation is an **untrusted input**: its own cross-references can be
  stale. Grep every cited symbol/path/line before relying on it.
- `ARCHITECTURE-IMPROVEMENTS.md`'s struck-through/`resolved` closed-item style is
  a good pattern: it names deleted mechanisms *as deleted*, so a removed-symbol
  grep flags it but it is not a false "current" assertion. Worth keeping.

### Process Learnings
- Front-loading classification into design (FR3 table) compresses implementation
  and removes mid-edit judgement calls — the estimate beat because of it.
- For docs-only tasks, the service-ops rollout/maintenance templates need active
  reframing, not box-ticking; mark runtime sections N/A *with rationale*.

### Risk Mitigation Strategies
- The link-integrity sweep + removed-API grep are the two highest-value, cheapest
  checks for a docs change; both ran clean and are captured as a reusable runbook
  in i-maintenance.md.

## Recommendations
### Process Improvements
- When a plan cites a function/line lifted from the doc being repaired, annotate
  it "verify at exec" so the exec phase doesn't propagate a stale citation.

### Tool and Technique Recommendations
- Adopt the i-maintenance.md "verify docs are honest" runbook (link sweep +
  removed-API grep + build/test) as the standard pre-merge check for any future
  doc-touching task.

### Future Work
- **(Low) Add a `docs/` / `*.md` exclude glob to
  `security.review.max-lines-exclude-paths`** so the changeset security gate runs
  against actual code on docs-heavy tasks instead of capping out on prose. Logged
  to BACKLOG.
- Documentation honesty is now trigger-based (see i-maintenance.md): future
  `pkg/`/`cmd/` symbol removals should update the CURRENT docs in the same task.

## Status
**Status**: Finished
**Next Action**: Task complete
**Blockers**: None identified
**Completion Date**: 2026-06-10
**Sign-off**: Matt Keenan (with Claude Opus 4.8)

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Archived Materials
- Planning: `a-task-plan.md` … `e-testing-plan.md` (this task directory).
- Implementation: commit `c15999c` (exec); `pkg/doc.go`, root `README.md`,
  `docs/` set, `CLAUDE.md`.
- Testing: `g-testing-exec.md` (9/9 PASS); changeset diffs under
  `/tmp/-home-matt-repo-dircachefilehash-task-17/`.
- Rollout/Maintenance: `h-rollout.md`, `i-maintenance.md`.
