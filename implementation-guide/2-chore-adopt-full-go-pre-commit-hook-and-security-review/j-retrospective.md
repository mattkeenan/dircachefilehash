# Adopt full Go pre-commit hook and security review - Retrospective
**Task**: 2 (chore)

## Task Reference
- **Task ID**: internal-2
- **Branch**: chore/2-adopt-full-go-pre-commit-hook-and-security-review
- **Template Version**: 2.1
- **Retrospective Date**: 2026-05-24

## Executive Summary
- **Duration**: <1 day (estimated <1 day — on target)
- **Scope**: Enable `gosec` in `.golangci.yml` + adopt the CWF security-review phase. Grew slightly in exec: an issue-cap lift and a go1.26.3 toolchain bump were added (both forced by what the tooling surfaced).
- **Outcome**: Success. gosec contributes **0** findings while staying active for new code; full hook is green end-to-end; one genuine latent bug was discovered and backlogged.

## Variance Analysis
### Time and Effort
- **Estimated**: Low complexity, <1 day (planning + impl + test).
- **Actual**: ~matches. Most effort went to per-finding code reading and re-measuring after the cap discovery, not to writing code.
- **Variance**: Effort on-target; *shape* differed — diagnosis-heavy, not code-heavy (26 one-line nolints + 1 config block + 1 go.mod line + docs).

### Scope Changes
- **Additions**
  - Lifted golangci-lint's issue-display caps (`max-same-issues: 0`, `max-issues-per-linter: 0`) — a security gate that silently hides a 4th duplicate is a real gap; this also revealed the true firing set.
  - Bumped toolchain to `go1.26.3` (go.mod `toolchain` directive) — to clear `GO-2026-4971`, which the hook's govulncheck step blocked on. User-approved.
- **Removals / Deferrals** (all backlogged, per prior user decisions)
  - G115 genuine inode/device truncation fix → **Very High** backlog (G115 stays excluded until then).
  - Deliberate suppression-review pass → **High** backlog.
  - 3 pre-existing non-gosec lint failures (cyclop ×2, unparam ×1) → **Low** backlog.

### Quality Metrics
- **gosec findings**: 0 (target 0).
- **Disposition coverage**: 100% of firing rules — 26 production nolint sites, every one read and justified.
- **Regression**: zero (`go test -race` green standalone and in the commit hook).
- **Defects found by the new tooling**: 1 genuine (inode/device truncation) — exactly the payoff of adopting gosec.

## What Went Well
- **Plan review earned its keep**: the 4 subagents caught that planning measured with standalone `gosec` rather than through golangci-lint (different rules/counts; `excludes` activates the full ruleset).
- **"Read the code, don't trust the tool"** (user directive) found the single real bug among ~230 raw findings and avoided blanket over-suppression — strategy flipped to precise per-line nolints.
- **The gate works**: the hook's govulncheck step correctly blocked a freshly-published CVE on unchanged code; resolved properly (toolchain bump) with no `--no-verify`.

## What Could Be Improved
- **Measure with the cap off**: golangci-lint's default `max-same-issues: 3` hid >half the findings, so the plan undercounted (12 vs 26 sites). For any linter audit, measure with `--max-same-issues=0 --max-issues-per-linter=0` *first*.
- **Don't assume a rule is test-only**: the plan tagged G204 as test-only; the production ssh wire transport (`wire_*.go`) also fires it. Grep production for the pattern before classifying.

## Key Learnings
### Technical Insights
- Setting `linters.settings.gosec.excludes` activates gosec's **full** ruleset (wider than golangci-lint's default subset) — always measure through the enforcement path, never standalone `gosec`.
- golangci-lint hides duplicate findings by default (`max-same-issues: 3`, `max-issues-per-linter: 50`); for a security gate, set both to 0.
- gosec skips fully-unused (dead-code) functions — detection probes must be reachable to fire.
- `//nolint:gosec // Gxxx: rationale` is the right suppression mechanism when gosec runs *through* golangci-lint (consistent with the existing `//nolint:govet` sites).

### Process Learnings
- The CWF `security-review-changeset` helper scopes to CWF-internal/shebang scripts only — for a Go-source change it legitimately yields an empty changeset; gosec is the security review for Go code, the changeset agent is for workflow-tooling tampering.
- Surfacing deviations in the exec doc (and a pointer back into the plan) kept the record honest without rewriting the plan wholesale.

## Recommendations
### Process / Tooling
- Add "lift issue caps before auditing" to the linting playbook.
- Keep gosec perms rules (G301/G302/G306) **active** — suppress per-line, never blanket — so new over-permissive writes are still caught.
### Future Work
- **Very High**: fix inode/device truncation in `dupes` (widen Dev/Ino to uint64), then re-enable G115.
- **High**: deliberate review pass over all 26 suppressions; decide whether to tighten `.dcfh/` perms to 0750/0600.
- **Low**: clear the 3 pre-existing non-gosec lint failures so full-tree `golangci-lint run ./...` is green.

## Status
**Status**: Finished
**Next Action**: Task complete — suggest merge to user
**Blockers**: None
**Completion Date**: 2026-05-24

## Archived Materials
- Plan/exec/test docs: `a-task-plan.md`, `d-implementation-plan.md`, `e-testing-plan.md`, `f-implementation-exec.md`, `g-testing-exec.md`
- Commits: `f1a6cdd` (a), `3732fc8` (d), `89483d9` (e), `16d041d` (f), `a578459` (g)
