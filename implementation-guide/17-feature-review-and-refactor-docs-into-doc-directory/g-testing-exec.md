# Review and refactor docs into doc directory - Testing Execution
**Task**: 17 (feature)

## Task Reference
- **Task ID**: internal-17
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/17-review-and-refactor-docs-into-doc-directory
- **Template Version**: 2.1

## Goal
Execute the static-verification tests from e-testing-plan.md (TC-1…TC-9) against
the docs-only refactor and record results. No runtime code shipped, so testing is
structural/grep/link-integrity plus a build+test regression guard.

## Execution Checklist
- [x] Read e-testing-plan.md and d-implementation-plan.md thoroughly
- [x] Verify test environment ready (local repo on task branch; git/grep/go)
- [x] Execute test cases sequentially
- [x] Record pass/fail for each test
- [x] Document failures with reproduction steps (none)
- [x] Update status to "Finished" — all pass

## Test Results

### Functional Tests

| Test ID | AC | Test Case | Actual | Status |
|---------|----|-----------|--------|--------|
| TC-1 | AC1/FR1 | Root tidy + history preserved | Root `*.md` = exactly `BACKLOG.md`, `CHANGELOG.md`, `CLAUDE.md`, `README.md`; five docs resolve under `docs/`; `git log --follow -- docs/ARCHITECTURE.md` shows pre-move history (`6178b22`, `a546d70`, …). | **PASS** |
| TC-2 | AC2/FR2 | No stale references | Zero stale root-path links to the five moved basenames across in-scope `*.md`/`*.go`; `pkg/doc.go` "at the repo root" phrase gone; `:32` reads "in docs/"; `:66` reads "now a defensive guard" (matches CLAUDE.md). | **PASS** |
| TC-3 | AC2(iii)/AC6, FR2/FR6 | Link-integrity sweep **[critical]** | Extracted every relative `](…)` link from `README.md` + `docs/*.md`, resolved against each file's dir, `test -e` each. **0 broken** out of all touched links (incl. §5 table siblings, `../cmd/dcfhfind/DESIGN.md`, `../cmd/dcfhfix/DESIGN.md`, `../CLAUDE.md`, `../BACKLOG.md`). | **PASS** |
| TC-4 | AC3/FR3 | Classification recorded | `c-design-plan.md` FR3 table classifies all six in-scope docs (README + five moved) CURRENT / HISTORICAL / NEEDS-REWRITE, each with a code-grounded one-line justification. | **PASS** |
| TC-5 | AC4/FR4 | README accurate & complete **[critical]** | Zero `DirectoryCache`/`FileEntry`/`NewDirectoryCache`; `dcfhfind` (7×), `dcfhfix` (7×), `--interactive-tree` (1×) all present; Quick Start uses real cobra commands; `remote` NOT listed as a user command (`Hidden: true`). | **PASS** |
| TC-6 | AC5/FR5 | Historical banners + no uncaveated stale architecture | All three (`architecture-v0.7.md`, `streaming-iterator-architecture.md`, `design.md`) carry a "Historical — superseded." banner at line 3 (under H1). `docs/ARCHITECTURE.md` cites none of `BEIndexFileIOEntry`/`binary_entry_index_file.go`/`AppendEntryToScanIndex`/`index.go:1008`/"four entry storage modes", and KEEPS the live `binary_entry_index_file_mmap.go` / `BEIndexFileMmapEntry` row. | **PASS** (see note) |
| TC-7 | AC6/FR6 | Single tagged index + discoverability | `docs/README.md` lists each doc with one-line purpose + Current/Historical marker matching FR3 (ARCHITECTURE.md & ARCHITECTURE-IMPROVEMENTS.md = **Current**; architecture-v0.7.md, streaming-iterator-architecture.md, design.md = **Historical**); root README "Documentation" links `docs/README.md` + `docs/ARCHITECTURE.md`; per-doc tags live in exactly one place; every doc ≤2 clicks from root README. | **PASS** |
| TC-8 | AC7/FR7 | Existing `docs/` preserved | `docs/changelog-old.md`, `docs/ssh-shell-mode.md`, `docs/feasibility/{posix-support,fideduperange}.md` all present; `CHANGELOG.md:5` → `docs/changelog-old.md` link still resolves. | **PASS** |
| TC-9 | NFR5 | Docs-only / no regression **[critical]** | `go build ./...` clean; `go test ./...` all green (`pkg` 0.246s, rest cached/ok); `git diff --name-only main...HEAD -- '*.go'` = exactly `pkg/doc.go` (comment-only); no `.cwf*`/`.cwf-skills`/`.cwf-rules` tree modified. | **PASS** |

**TC-6 note**: the removed-mechanism grep flags two lines in
`docs/ARCHITECTURE-IMPROVEMENTS.md` (`:60`, `:62`) naming `AppendEntryToScanIndex`
et al. These sit inside a **struck-through, `— **resolved**`/`Status: closed`**
item that describes those symbols as *deleted* — an honest historical record of a
fix, not an assertion that the mechanism is current. This is consistent with the
implementation-exec decision to leave that closed-item record (and its `:93`
mention) untouched rather than falsify a history note. Not a violation.

### Non-Functional Tests
- **Usability (NFR2)** — TC-7: historical docs visibly labelled (banner + index
  marker); every doc reachable from root README in ≤2 clicks. **PASS**
- **Security (NFR4)** — docs-only; no secrets/credentials/executable content
  added. `CLAUDE.md`'s `## Security Review` section unchanged by this task (the
  only CLAUDE.md edit added four command-list lines, well above that section).
  The exec-phase changeset gate is the authority (see exec `## Security Review`:
  cap-exceeded `error` for a docs-heavy diff; only non-Markdown change is two
  `pkg/doc.go` comments). **PASS**
- **Reliability (NFR5)** — TC-1 (`git mv` history-preserving, reversible) + TC-9
  (green build/test). **PASS**
- **Performance (NFR1)** — N/A: no runtime change; not measured.

## Test Failures
None. All TC-1…TC-9 pass; both critical paths (link-integrity sweep, removed-API
grep) are 100% clean.

## Coverage Report
Not applicable in the unit-coverage sense — no runtime code added. AC coverage:
every AC (AC1–AC7) and every NFR (NFR1, NFR2, NFR4, NFR5) maps to at least one
executed check above; all map to **PASS** (NFR1 is N/A by design).

## Status
**Status**: Finished
**Next Action**: /cwf-rollout
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
See the Test Results table above. 9/9 functional test cases PASS; all applicable
non-functional checks PASS. Build and full test suite green; the only `.go` change
is comment-only (`pkg/doc.go`); no out-of-scope tree touched.

## Security Review

**State**: error

error: cap exceeded: 2632 production lines > 500

The changeset helper (`--wf-step=testing-exec`) wrote the full 3803-line diff
(15 files, 2632 production lines, anchor `0634f9a`) to
`/tmp/-home-matt-repo-dircachefilehash-task-17/security-review-changeset-testing-exec.out`
but exited `2` because the production-weighted count exceeds the default 500-line
cap, so the `cwf-security-reviewer-changeset` subagent was **not** invoked (per
the exec-skill contract for exit 2). This mirrors the implementation-exec phase:
the anchor is still `0634f9a`, so the diff spans the whole task-17 docs work
(README full rewrite + five moved/edited Markdown docs + the tagged index + the
two workflow exec files), all of which count as "production" because no
`security.review.max-lines-exclude-paths` glob covers `docs/` or `*.md`. The only
non-Markdown change across the entire changeset is two comment lines in
`pkg/doc.go`; no code path, secret, credential, auth, or env-var handling is
touched (NFR4). A manual reviewer can read the `.out` file at the path above.

## Lessons Learned
*To be captured during retrospective*
