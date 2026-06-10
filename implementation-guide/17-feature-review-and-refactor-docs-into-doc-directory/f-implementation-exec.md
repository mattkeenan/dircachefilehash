# Review and refactor docs into doc directory - Implementation Execution
**Task**: 17 (feature)

## Task Reference
- **Task ID**: internal-17
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/17-review-and-refactor-docs-into-doc-directory
- **Template Version**: 2.1

## Goal
Execute d-implementation-plan.md: relocate five docs into `docs/`, fix
references, fix `ARCHITECTURE.md`, banner three historical docs, rewrite README,
add a tagged `docs/README.md` index — docs-only except two `pkg/doc.go` comments.

## Actual Results

### Step 1 — Setup & reference map
- **Actual**: Before-map grep (excluding `.cwf*`/`implementation-guide`/`.claude`)
  found the predicted in-scope references: `ARCHITECTURE-IMPROVEMENTS.md` bare
  prose (lines 4, 85, 93, 173–176), `ARCHITECTURE.md:9` + §5 table, `pkg/doc.go:32`
  + `:65`, `architecture-v0.7.md:1155`. All `.claude/sessions/**` hits out of scope.
- **Deviations**: none.

### Step 2 — Relocate (`git mv`)
- **Actual**: `git mv` of all five docs into `docs/`. Root Markdown now exactly
  `README.md`, `CHANGELOG.md`, `BACKLOG.md`, `CLAUDE.md` (AC1 ✓). `git status`
  shows all five as renames (`R`), preserving `--follow` history (resolves
  post-commit; verified in g-testing-exec / TC-1).
- **Deviations**: none.

### Step 3 — Fix `docs/ARCHITECTURE.md` (Decision 3)
- **Actual**: Verified source facts first — `pkg/binary_entry_index_file.go` is
  gone (only `_mmap.go` remains); `BEIndexFileMmapEntry` is live;
  `AppendEntryToScanIndex` exists nowhere; `index.go` is 994 lines.
  - (i) Layer-2 row: "four entry storage modes" → "three concrete
    implementations plus one conceptual read/write mode that is no longer
    implemented" (matches the `binary_entry_interface.go:16` comment: modes 1/3/4
    concrete, mode 2 read/write deleted).
  - (ii) dropped the `BEIndexFileIOEntry` / `binary_entry_index_file.go` row;
    kept the live `binary_entry_index_file_mmap.go` neighbour.
  - (iii) §3.1: struck the non-existent `AppendEntryToScanIndex@index.go:1008`,
    replaced with the honest "no mmap-backed scan-`*.idx` files" statement.
  - (iv) §6: reframed RWMutex as "defensive, not load-bearing" using CLAUDE.md's
    wording.
  - §5 table: converted the left "Doc" column to markdown links by target —
    siblings bare-relative, `cmd/.../DESIGN.md` via `../cmd/…`, `CLAUDE.md` /
    `BACKLOG.md` via `../`.
- **Deviations**: §6's cited writer `appendEntryToNamedIndex@pkg/index.go:967`
  was found to be **non-existent too** (not just the line number). So the §6 fix
  replaced the entire stale mremap-mechanism description, not merely the
  load-bearing/defensive framing — kept consistent with CLAUDE.md. Heading
  annotated "(defensive)".

### Step 4 — Historical banners (Decision 5)
- **Actual**: Prepended the "Historical — superseded" banner immediately under
  the H1 of `architecture-v0.7.md` (design spec),
  `streaming-iterator-architecture.md` (proposal), and `design.md` (pre-v0.7;
  banner additionally names `main.idx`/`cache.idx`, `NewMetaStore`, the 4-stage
  pipeline). All banner links (`ARCHITECTURE.md`, `../CLAUDE.md`) resolve.
- **Deviations**: none.

### Step 5 — Out-of-doc reference fixes (FR2)
- **Actual**: `pkg/doc.go:32` "at the repo root" → "in `docs/`"; `pkg/doc.go:65–67`
  RWMutex reframed to "now a defensive guard, since the `mremap`'d scan path it
  once protected has been removed" (mirrors Step 3 §6 wording).
  `architecture-v0.7.md:1155` and `ARCHITECTURE-IMPROVEMENTS.md` lines 4/175–176
  are bare-prose sibling names that resolve in-`docs/` — left bare per Decision 4.
- **Deviations**: the *optional* `:93` `architecture-v0.6.md` → `architecture-v0.7.md`
  micro-fix was **not** applied — that mention sits inside a closed-item status
  record describing a past action ("references … were left alone"); editing it
  would falsify a history note. `ARCHITECTURE-IMPROVEMENTS.md` therefore remains a
  pure rename (`R`) with zero content change, satisfying Decision 4.

### Step 6 — README rewrite (Decision 2)
- **Actual**: Replaced the stale library-API README wholesale with a CLI-first,
  source-grounded one. Verified the surface against `cmd/`: global persistent
  flags from `root.go`; the `dcfh` command set (`init`/`status`/`update`/`dupes`/
  `snapshot`/`config`/`diff`/`subrepo`/`completion`/`version`); `--interactive-tree`
  documented as status/update-only, TTY-required; brief `dcfhfind`/`dcfhfix`;
  install (Go 1.25, `make build`; goreleaser deb/rpm/tar.gz, linux amd64+arm64);
  a "Documentation" section linking `docs/`. `remote` **omitted** (`Hidden: true`,
  `cmd/dcfh/remote.go:23`). Zero `DirectoryCache`/`FileEntry`/`NewDirectoryCache`;
  no surviving `go` block uses the removed API.
- **Deviations**: none.

### Step 7 — `docs/README.md` index (FR6)
- **Actual**: Created `docs/README.md` as the single tagged index — one row per
  doc with a one-line purpose and a **Current**/**Historical** marker copied from
  the FR3 table, plus a Reference table for the pre-existing `docs/` contents
  (`ssh-shell-mode.md`, `changelog-old.md`, `feasibility/*`). Root README's
  "Documentation" section points to `docs/README.md` and `docs/ARCHITECTURE.md`
  directly (≤2 clicks; no duplicate tag list).
- **Deviations**: none.

### Step 8 — CLAUDE.md minimal correction (Decision 6)
- **Actual**: Added `diff`/`subrepo`/`completion` to the CLI Usage command list,
  plus `remote` flagged as the hidden SSH audit-mode endpoint. Security Review
  section unchanged (NFR4 ✓).
- **Deviations**: the `--interactive-tree` "global option" mislabel that
  Decision 6 anticipated **does not exist** in current CLAUDE.md — lines 500–501
  already scope it correctly to `status`/`update` (corrected in a prior task). So
  only the command-list half of the minimal correction was required.

### Step 9 — Verify
- **Actual**:
  - `go build ./...` — clean.
  - `go test ./...` — green. (One flake: `TestWireRepoDiffAndApplyAgainstInProcessRemote`
    failed once with "wire client closed", a pre-existing wire-teardown race —
    passes 5/5 in isolation; a comment-only `doc.go` change cannot affect it.
    Baseline `doc.go` reproduced both pass and fail purely by timing.)
  - Link-integrity sweep: 32/32 relative markdown links across both READMEs and
    the moved/edited docs resolve (`test -e`). Zero misses.
  - FR2 gate: no stale root-path link to a moved doc in any in-scope file;
    `pkg/doc.go` "repo root" phrase gone, "in docs/" + "defensive guard" present.
  - CHANGELOG → `docs/changelog-old.md` still resolves (FR7).
  - `git status`: only in-scope files changed; no `.cwf/**`, `.cwf-skills/**`,
    `.cwf-rules/**`, `.claude/sessions/**` touched. Only `.go` edit is `pkg/doc.go`
    (comments).

## Blockers Encountered
None.

## Deferral Check
- [x] All steps from d-implementation-plan.md executed
- [x] All success criteria from a-task-plan.md met
- [x] All requirements from b-requirements-plan.md addressed
- [x] All design guidance in c-design-plan.md followed (deviations documented above)
- [x] No planned work deferred without user approval (the only un-applied item is
  the *optional* `:93` micro-fix, deliberately skipped with rationale above)
- [x] No follow-up task required

## Security Review

**State**: error

error: cap exceeded: 2604 production lines > 500

The changeset helper (`--wf-step=implementation-exec`) wrote the full 3600-line
diff (13 files, 2604 production lines, anchor `0634f9a`) to
`/tmp/-home-matt-repo-dircachefilehash-task-17/security-review-changeset-implementation-exec.out`
but exited `2` because the production-weighted count exceeds the default 500-line
cap, so the `cwf-security-reviewer-changeset` subagent was **not** invoked (per
the exec-skill contract for exit 2). The overflow is benign: this is a docs-only
task (README full rewrite + five moved/edited Markdown docs + a tagged index),
and no `security.review.max-lines-exclude-paths` glob is configured for `docs/`
or `*.md`, so the prose counts as "production". The only non-Markdown change is
two comment lines in `pkg/doc.go`. No code path, secret, credential, auth, or
env-var handling is touched (NFR4). A manual reviewer can read the `.out` file at
the path above.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
See per-step results above.

## Lessons Learned
*To be captured during retrospective*
