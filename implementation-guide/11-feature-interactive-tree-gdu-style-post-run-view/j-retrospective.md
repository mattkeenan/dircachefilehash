# interactive-tree gdu-style post-run view - Retrospective
**Task**: 11 (feature)

## Task Reference
- **Task ID**: internal-11
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/11-{task-description}
- **Template Version**: 2.1
- **Retrospective Date**: 2026-06-05

## Executive Summary
- **Duration**: Plans (a–e) prepared earlier; exec→retrospective (f–j) completed in one focused session. Estimate was 3–5 days / Medium-High complexity; AI-assisted delivery landed well under that.
- **Scope**: Delivered exactly the requested feature — `--interactive-tree` on `dcfh status`/`update`, left tree + width-gated right stats pane, runtime-switchable sort (default sum-of-changes), all opt-in and read-only. No scope cut from the success criteria.
- **Outcome**: Success. 22/22 test cases pass; gosec 0 issues; FR4 security review **no findings**; non-interactive path proven byte-identical. Two genuine enhancements deferred to BACKLOG.

## Variance Analysis
### Time and Effort
- **Estimated** (a-task-plan): ~3–5 days, Medium-High; TUI library decision deferred to design.
- **Actual**: One session for f–j. The pure-data-layer-first sequencing meant the bulk of correctness work landed and was green before any terminal code existed.
- **Variance**: Faster than estimated. The plan-review phase (b/c/d) front-loaded the hard thinking, so exec had few surprises.

### Scope Changes
- **Additions (during exec, all small)**:
  - `runScreen(screen,…)` seam in `tui.go` so the event loop + teardown are testable with `SimulationScreen` (the public `Run` makes a real screen).
  - Exported `dcfh.SanitiseLabel` so the render layer sanitises wrapped error text via the *same* helper (KD6) — no policy duplication.
  - Real-TTY `pty` test harness (not in the test plan) to exercise the actual `tcell.NewScreen()` path, incl. an end-to-end crafted-filename escape-safety check.
- **Removals / deferrals**:
  - Byte-weighted sort — explicitly deferred in KD8 (deleted/old sizes not retained); BACKLOG (Low).
  - Wide-rune (CJK) column-width accounting — cosmetic; BACKLOG (Low).
  - TC-16 forced `Init()`-failure on an attached TTY — verified by code path, not reproduced headlessly.
- **Impact**: Net positive — the additions raised testability/confidence without expanding the user-facing surface.

### Quality Metrics
- **Coverage**: data layer (`pkg/treeview.go`) 97–100% on the core builder/sanitiser; `tui` 78.9%.
- **Defects**: 0 found in testing; 0 known post-implementation. The one signature-churn item (`runUpdate` variadic) was an `unparam` lint catch, fixed in-phase.
- **Performance**: no regression — the viewer is post-run, reads the index via one extra mmap (no second fs walk, no hashing), TTY-only.

## What Went Well
- **Plan-review earned its keep.** The b/c/d reviewer pass caught two *blocking* issues before any code: the unexported-types package boundary (resolved by KD1 — data layer inside `pkg`, render consumes the exported `Tree`) and the FR4/FR7 data-source conflict (resolved by reloading the merged index — "no second *filesystem* walk"). Both would have been expensive mid-exec.
- **The two-layer seam paid off exactly as designed.** All correctness (aggregation, category, deleted-union, sanitiser) is unit-tested with plain literals, no TTY; risky terminal code is isolated in `tui`.
- **The collector's concurrency invariant held under scrutiny** — single-writer (one comparison goroutine), read-after-join; `-race` clean, and the security review independently confirmed it.
- **Byte-identity test** gave hard evidence the default-off path is unperturbed — the strongest guard for the "non-interactive unchanged" requirement.
- **pty harness** validated the real terminal path that `SimulationScreen` can't, including proving the escape-injection defence works against a hostile filename in a live terminal.

## What Could Be Improved
- **The Go-1.25 floor bump was an unplanned consequence**, surfaced only at `go get` time. tcell requires Go ≥ 1.25, which silently raised the project's `go.mod` directive from 1.24.3. KD4 (the TUI-library decision) should have recorded the candidate's toolchain floor alongside its dependency weight.
- **Background-modernizer churn caused staging friction**: a gopls "modernize" rewrote `wg.Add/Done` → `wg.Go` in pipeline files (and tried unrelated files) after staging, requiring a reconcile + revert of unrelated drift before the clean commit.
- **Testing-phase security-review cap**: exit 2 (cap exceeded) is expected because that phase counts test files as production — worth knowing up front so it isn't mistaken for a failure.

## Key Learnings
### Technical Insights
- A "pure" layer needn't be a separate package — *purpose* (no terminal import) + an exported boundary type (`Tree`) is enough to get the testability and keep unexported index types in `pkg`.
- Reject-by-default printable **allowlist** is the right shape for terminal-escape safety; a blocklist of known CSI/OSC sequences would have missed bytes outside the enumerated set (the pty test confirmed the allowlist neutralises both CSI and OSC).
- Lock-free is fine when the invariant is genuinely single-writer/read-after-join — but document it in code so the next reader doesn't "add a mutex to be safe" or, worse, add a second writer.

### Process Learnings
- Front-loading judgement into plan-review (b/c/d) measurably reduced exec rework — the pattern to keep.
- When a design picks a dependency, capture its **toolchain/runtime floor** as a first-class line item, not just its dependency count.
- A `pty`-based smoke test is a cheap, high-value complement to `SimulationScreen` for any TUI work in this repo.

### Risk Mitigation Strategies
- The highest-risk areas (terminal teardown, escape-safety) were isolated and given dedicated tests (idempotent `sync.Once` Fini; allowlist + live crafted-filename check) — the risks named in a-task-plan were exactly the ones that needed proof, and they got it.

## Recommendations
### Process Improvements
- Add a design-phase checklist item: "new dependency → record its minimum Go/runtime version and confirm CI meets it."

### Tool and Technique Recommendations
- Standardise the `pty.fork()` smoke harness for terminal features; keep `SimulationScreen` for logic and `pty` for the real-screen/teardown/escape path.

### Future Work
- BACKLOG: `Wide-rune (CJK) column width…` (bugfix, Low); `Byte-weighted sort option…` (feature, Low).
- Confirm the release pipeline runs Go ≥ 1.25 before tagging; add the CHANGELOG entry at release.
- Revisit TC-16 (real attached-TTY init failure) if an init-failure incident is ever reported.

## Status
**Status**: Finished
**Next Action**: Task complete — ready for squash + merge (human-run)
**Blockers**: None identified
**Completion Date**: 2026-06-05
**Sign-off**: Matt Keenan (Claude-assisted)

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Archived Materials
- Links to planning documents and artefacts
- Links to implementation PRs and commits
- Links to test results and quality reports
- Links to deployment and monitoring dashboards
