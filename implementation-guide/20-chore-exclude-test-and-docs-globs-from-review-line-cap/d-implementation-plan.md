# Exclude test and docs globs from review line cap - Implementation Plan
**Task**: 20 (chore)

## Task Reference
- **Task ID**: internal-20
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/20-exclude-test-and-docs-globs-from-review-line-cap
- **Template Version**: 2.1

## Goal
Add `**/*_test.go`, `docs/**`, and `*.md` to
`security.review.max-lines-exclude-paths` in
`implementation-guide/cwf-project.json` so the exec-phase security-review cap
counts only consumer production code, restoring the
`cwf-security-reviewer-changeset` gate on test-heavy and docs-heavy tasks.

## Source-of-truth verification (done before this plan)
- **Consumer of the config**: `.cwf/scripts/command-helpers/security-review-changeset`
  reads `security.review.max-lines-exclude-paths` and converts each entry to a
  git `:(glob,exclude)<pattern>` magic pathspec (`max_lines_exclude_paths`,
  ~line 454; applied in `count_production_lines`, ~line 492). **Git owns the
  matching — no Perl-side path classification.** The paths are *still emitted in
  the diff*; the exclude only drops them from the cap count.
- **Git glob semantics verified empirically** (`git ls-files -- ':(glob)…'`):
  - `:(glob)**/*_test.go` → matches root **and** nested test files (e.g.
    `cmd/dcfh/dcfh_test.go`, `cmd/dcfh/internal/tui/render_test.go`). ✓
  - `:(glob)docs/**` → matches the whole docs tree. ✓
  - `:(glob)*.md` → matches **root-level only** (`README.md`, `CHANGELOG.md`,
    `CLAUDE.md`, `BACKLOG.md`) — a bare `*` does **not** cross `/`. ✓
  - `:(glob)**/*.md` → matches **287 tracked Markdown files**, including the
    vendored `.cwf/**`, `.claude/**`, and `.cwf-*` mirror prompt files.
    **Rejected** — that reaches into the deliberately-deferred `.cwf/**`
    territory (the Task 5/9/14 caveat) and would let edits to the workflow/skill
    prompt surface ride under the cap. `*.md` is the scope-correct choice
    precisely because it is root-only.
- **Existing entry**: `implementation-guide/**` is already present and proves
  the mechanism works (the workflow-doc dir is discounted today).

## Files to Modify
### Primary Changes
- `implementation-guide/cwf-project.json` — append `**/*_test.go`, `docs/**`,
  `*.md` to the `security.review.max-lines-exclude-paths` array (currently
  `["implementation-guide/**"]`).

### Supporting Changes
- None. (CHANGELOG.md / BACKLOG.md updates happen at the retrospective phase per
  CWF convention.)

### Out of scope (do NOT touch)
- `.cwf/**` exclude glob (Task 5/9/14 item) — flips pure-CWF-upgrade tasks from
  "error→skipped" to "subagent on full vendored delta"; unresolved here by
  design.
- `**/*.md` (would sweep vendored Markdown — see verification above).
- `cmd/dcfhfind/DESIGN.md`, `cmd/dcfhfix/DESIGN.md` (nested consumer design
  docs): left under the production cap rather than reaching for `**/*.md` and
  breaching the vendored boundary. Acceptable — they rarely change in bulk.
- Any `.go`/`Makefile`/`.golangci.yml`/build-contract change.

### Note: `docs/**` is content-agnostic
`docs/**` discounts *everything* under `docs/`, not just Markdown — today that
tree is all `.md`, but a future non-doc file dropped there (e.g. a `.go`
fixture) would be silently discounted from the cap. Accepted, low-risk trade
(the directory's purpose is docs); flagged so it isn't a surprise. Excluded
content is still emitted to the reviewer regardless — the glob only relaxes the
cap count, never what the subagent reads.

## Implementation Steps
### Step 1: Setup
- [ ] Confirm branch `chore/20-…`, tree clean, baseline `a22530d` (Task 19 tip).

### Step 2: Edit config
- [ ] In `implementation-guide/cwf-project.json`, change
  `security.review.max-lines-exclude-paths` from
  `["implementation-guide/**"]` to include the three new globs (see Code
  Changes). Preserve the file's existing 2-space JSON indentation and key order.

### Step 3: Validate
- [ ] `cwf-manage validate` passes (no hash/schema drift; this also parses the
  JSON, so a separate round-trip is redundant).
- [ ] Each new glob resolves as expected via `git ls-files -- ':(glob)<p>'`
  (already verified in planning; re-confirm post-edit).
- [ ] **Negative-match assertion** (the security rationale for `*.md` over
  `**/*.md`): `git ls-files -- ':(glob)*.md'` lists **only** root `.md`
  (README/CHANGELOG/CLAUDE/BACKLOG) — **no** `.cwf/` or `.claude/` path. This
  confirms the vendored prompt surface stays under the cap.
- [ ] `security-review-changeset --wf-step=implementation-exec` run on this
  task's own changeset reports a sane production-weighted count (this diff is
  tiny — a few config + workflow-doc lines, and `implementation-guide/**` is
  already excluded — so it stays well under the cap regardless).

## Code Changes
### Before
```json
"security" : {
    "review" : {
      "max-lines-exclude-paths" : [
        "implementation-guide/**"
      ]
    }
  },
```

### After
```json
"security" : {
    "review" : {
      "max-lines-exclude-paths" : [
        "implementation-guide/**",
        "**/*_test.go",
        "docs/**",
        "*.md"
      ]
    }
  },
```

## Test Coverage
**See e-testing-plan.md for complete test plan** — config-only, so verification
is glob-resolution checks + helper cap-count behaviour, not Go unit tests.

## Validation Criteria
**See e-testing-plan.md for validation criteria and test results**

## Scope Completion
**IMPORTANT**: Complete all planned implementation before marking task Finished.

Deferring work creates technical debt and scope creep. Task 37 deferred documentation updates,
marked the task complete anyway, and created Task 38 to fix the deferred work.

**If you must defer work**:
1. Get user approval with clear rationale
2. Update success criteria to reflect descoped work
3. Create follow-up task immediately
4. Document deferral in Actual Results section

## Status
**Status**: Finished
**Next Action**: /cwf-testing-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Implemented exactly as planned — the single-file edit appended `**/*_test.go`,
`docs/**`, `*.md` to `max-lines-exclude-paths`, preserving indentation and key order.
`cwf-manage validate` → OK; all three globs resolved as predicted; the `*.md`
negative-match assertion held (root-only, no vendored path). No deviations.

## Lessons Learned
The `*` vs `**` git-pathspec boundary is load-bearing for security here: root-only
`*.md` keeps the 287 vendored prompt files cap-counted. Pinning that with real
`git ls-files` output before the edit removed all execution risk.
