# Review and refactor docs into doc directory - Testing Plan
**Task**: 17 (feature)

## Task Reference
- **Task ID**: internal-17
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/17-review-and-refactor-docs-into-doc-directory
- **Template Version**: 2.1

## Goal
Define the verification approach for a docs-only refactor: relocation, link
integrity, reference correctness, README accuracy, banner honesty, and
no-source-regression — each AC mapped to a concrete, runnable check.

## Test Strategy
### Test Levels
This task ships no runtime code, so testing is **static verification**, not
unit/integration tests:
- **Structural checks**: `ls`/`git` assertions on file locations + history.
- **Content greps**: presence/absence of specific strings (removed API, banners,
  command names, stale references).
- **Link-integrity sweep**: extract every relative markdown link from in-scope
  docs and `stat` each target.
- **Build/regression**: `go build ./...` + `go test ./...` (guards the one
  comment-only `pkg/doc.go` edit and proves docs-only).

### Coverage Targets
- **Every AC (AC1–AC7) has at least one check** below.
- **Critical path** = the link-integrity sweep + the removed-API grep (the two
  highest-value failure modes); both must be 100% clean.
- **Regression**: full `go build`/`go test` green; no out-of-scope tree touched.

## Test Cases
### Functional Test Cases

- **TC-1 — Root tidy + history preserved (AC1/FR1)**
  - **Given**: the five docs moved via `git mv`.
  - **When**: `ls *.md` at root; `ls docs/`; `git log --follow -- docs/ARCHITECTURE.md`.
  - **Then**: root Markdown = exactly `README.md`, `CHANGELOG.md`, `BACKLOG.md`,
    `CLAUDE.md`; the five docs resolve under `docs/`; `--follow` shows pre-move
    history for the spot-checked file.

- **TC-2 — No stale references (AC2/FR2)**
  - **Given**: relocation + reference fixes done.
  - **When**: grep in-scope trees (exclude `.cwf/**`, `.cwf-skills/**`,
    `.cwf-rules/**`, `implementation-guide/**`, `.claude/sessions/**`) for the
    five moving basenames as old root-path links and for the `pkg/doc.go`
    "at the repo root" phrase.
  - **Then**: zero stale hits; `pkg/doc.go:32` says "in `docs/`"; `pkg/doc.go:65–67`
    matches the CLAUDE.md "defensive" wording (no load-bearing `mremap` claim).

- **TC-3 — Link-integrity sweep (AC2(iii)/AC6, FR2/FR6) [critical]**
  - **Given**: all edits done.
  - **When**: for each in-scope doc (`docs/*.md` + root `README.md`), extract
    relative markdown links (`grep -oE '\]\(([^)#]+)'`, strip `](`, drop anchors),
    resolve each against the file's own directory, and `test -e`.
  - **Then**: every link this task authored/touched resolves — in particular the
    §5 table: siblings (same `docs/`), `cmd/dcfhfind/DESIGN.md` &
    `cmd/dcfhfix/DESIGN.md` via `../cmd/…`, `CLAUDE.md` & `BACKLOG.md` via `../`.
    Pre-existing bare-prose mentions (not links) are excluded.

- **TC-4 — Classification recorded (AC3/FR3)**
  - **Given**: `c-design-plan.md`.
  - **When**: read its FR3 table.
  - **Then**: all six in-scope docs (five moved + README) classified
    CURRENT/HISTORICAL/NEEDS-REWRITE with a one-line code-grounded justification.

- **TC-5 — README accurate & complete (AC4/FR4) [critical]**
  - **Given**: rewritten `README.md`.
  - **When**: grep README; read its Quick Start; check fenced blocks.
  - **Then**: zero occurrences of `DirectoryCache` / `FileEntry` /
    `NewDirectoryCache`; `dcfh`, `dcfhfind`, `dcfhfix`, `--interactive-tree` all
    present; no surviving ```` ```go ```` block uses the removed API; Quick Start
    commands (`init`/`status`/`update`/`dupes`) are real cobra commands; `remote`
    is **not** listed as a user command (it's `Hidden: true`).

- **TC-6 — Historical banners + no uncaveated stale architecture (AC5/FR5)**
  - **Given**: bannered docs.
  - **When**: check the top of `docs/architecture-v0.7.md`,
    `docs/streaming-iterator-architecture.md`, `docs/design.md`; grep all in-scope
    `docs/*.md` for removed mechanisms (`BEIndexFileIOEntry`,
    `AppendEntryToScanIndex`, `pkg/workflow.go`) presented as current.
  - **Then**: each of the three has a "Historical — superseded" banner near its
    H1; no CURRENT-classified doc asserts a removed mechanism without a banner;
    `docs/ARCHITECTURE.md` no longer cites `BEIndexFileIOEntry` /
    `binary_entry_index_file.go` / `AppendEntryToScanIndex` / `index.go:1008` /
    "four entry storage modes", but **keeps** the live `binary_entry_index_file_mmap.go`
    row.

- **TC-7 — Single tagged index + discoverability (AC6/FR6)**
  - **Given**: `docs/README.md` + root README "Documentation" section.
  - **When**: read both.
  - **Then**: `docs/README.md` lists each doc with one-line purpose +
    CURRENT/HISTORICAL marker matching the FR3 table verbatim; root README links
    `docs/README.md` and `docs/ARCHITECTURE.md`; the per-doc tags appear in exactly
    one place; any doc reachable from root README in ≤2 clicks.

- **TC-8 — Existing `docs/` preserved (AC7/FR7)**
  - **Given**: the move.
  - **When**: check `docs/changelog-old.md`, `docs/ssh-shell-mode.md`,
    `docs/feasibility/`; follow `CHANGELOG.md:5`.
  - **Then**: all present and unchanged; the `CHANGELOG → docs/changelog-old.md`
    link still resolves.

- **TC-9 — Docs-only / no regression (NFR5)**
  - **Given**: all edits.
  - **When**: `go build ./...`; `go test ./...`; `git diff --name-only main...HEAD`.
  - **Then**: build + tests green; the only `.go` file changed is `pkg/doc.go`
    (comment-only); no file under the out-of-scope trees is modified.

### Non-Functional Test Cases
- **Usability (NFR2)**: TC-7 (≤2 clicks; historical docs visibly labelled).
- **Security (NFR4)**: docs-only — no secrets/credentials/executable content
  added; verify `CLAUDE.md`'s `## Security Review` section is byte-identical if
  CLAUDE.md is touched (`git diff` that section). The exec-phase
  `cwf-security-reviewer-changeset` run is the gate.
- **Reliability (NFR5)**: TC-1 (`git mv` reversibility/history) + TC-9 (green
  build/test).
- **Performance (NFR1)**: N/A — no runtime change; not measured.

## Test Environment
### Setup Requirements
- Local repo on the task branch; `git`, `grep`, `go` toolchain; `markdown-reader`
  for section reads. No database, no services, no network.
- The link-integrity sweep is a shell one-liner/small script (extract `](…)`
  relative links, `test -e` each, resolved against the file's dir) — no new tool.

### Automation
- Run ad hoc in g-testing-exec; `go build`/`go test` mirror the pre-commit gate.
  No CI change required (docs-only).

## Validation Criteria
- [ ] TC-1…TC-9 all pass (AC1–AC7 covered).
- [ ] Link-integrity sweep and removed-API grep 100% clean (critical paths).
- [ ] `go build ./...` and `go test ./...` green; only `pkg/doc.go` `.go`-changed.
- [ ] No out-of-scope tree modified.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All nine planned test cases executed at testing-exec; 9/9 PASS (see
`g-testing-exec.md`). Both critical paths (link-integrity sweep, removed-API grep)
100% clean. The static-verification strategy fit the docs-only change exactly.

## Lessons Learned
The link-sweep + removed-API grep are the two highest-value, cheapest checks for a
docs change; both are now captured as a reusable runbook in `i-maintenance.md`.
See `j-retrospective.md`.
