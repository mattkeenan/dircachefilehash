# Conform backlog and changelog to CWF format - Implementation Execution
**Task**: 1 (chore)

## Task Reference
- **Task ID**: internal-1
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/1-conform-backlog-and-changelog-to-cwf
- **Template Version**: 2.1

## Goal
Execute the conversion in d-implementation-plan.md: convert `BACKLOG.md` to the CWF heading-tree schema, archive the version-based `CHANGELOG.md` to `docs/changelog-old.md`, and write a fresh CWF by-task changelog — verified against the `backlog-manager` contract oracle.

## Execution Checklist
- [x] Read d-implementation-plan.md and e-testing-plan.md thoroughly
- [x] Verify all prerequisites met
- [x] Execute implementation steps sequentially
- [x] Update "Actual Results" for each step
- [x] Document any deviations from plan
- [x] Update status to "Finished" when complete

## Actual Results

### Step 1: Safety net + audit
- **Planned**: Capture pre-move CHANGELOG blob hash; capture the 15 real entry titles (excluding the line-8 `<title>` stub); audit BACKLOG for contract-breakers (HTML comments, struck titles, colon-headings, tab/CRLF/BOM).
- **Actual**:
  - Pre-move CHANGELOG blob hash: `73e6e7b60f3c784c40bfc05e174324cdc34f7cf0`.
  - 15 real titles captured via `grep '^## Entry: ' BACKLOG.md | grep -vF '<title>'` (count = 15, stub excluded).
  - Audit clean: no HTML comments, no `~~`/`✓` struck titles, no CRLF. `docs/` already exists.
- **Deviations**: None.

### Step 2: Convert BACKLOG.md
- **Planned**: Replace self-documenting header (incl. line-8 stub) with CWF intro; per entry rewrite `## Entry:` → `## Task:` and inject `### Task-Type:` immediately before `### Priority:`; preserve all bodies. Gate: `validate --all --strict` exit 0 AND `list` count = 15.
- **Actual**:
  - Header replaced with `# Backlog` + one-line description (template stub + grep block dropped).
  - 16 targeted Edits: 1 header + 15 per-entry. Each per-entry Edit matched the unique `## Entry: <title>` + blank + `### Priority:` block, so bodies were untouched. (First entry needed a follow-up edit to drop a stray blank between Task-Type and Priority — now all entries match the canonical `Task-Type`/`Priority` adjacency.)
  - Task-Type inference (see Judgement Calls below): feature ×8, chore ×6, bugfix ×1.
  - Gate: `validate --all --strict` exit 0; `list --all-items | grep -c '^  - '` = **15**. ✓
- **Deviations**: None to the plan's method.

### Step 3: Archive + recreate CHANGELOG.md
- **Planned**: `git mv CHANGELOG.md docs/changelog-old.md`; write new `# Changelog` + intro + prose pointer to the archive; gate `validate --all --strict` exit 0.
- **Actual**:
  - `git mv` reported `R CHANGELOG.md -> docs/changelog-old.md`.
  - New `CHANGELOG.md` written: `# Changelog` H1, one intro line, prose pointer (markdown link, not HTML comment) to `docs/changelog-old.md`. Zero task entries.
  - Gate: `validate --all --strict` exit 0. ✓
- **Deviations**: None.

### Step 4: Validation
- **Planned**: validate both strict; list = 15; identity gate (15 baseline titles preserved); rename gate (archived blob == pre-move blob, R100); `git status` blast-radius review.
- **Actual**:
  - `validate --all --strict` exit 0 (both files). ✓
  - `list --all-items | grep -c '^  - '` = 15. ✓
  - Title identity: `grep '^## Task: ' BACKLOG.md | sed 's/^## Task: //'` returns the exact 15 baseline titles — zero additions/deletions. ✓
  - Archive blob: `git rev-parse :docs/changelog-old.md` = `73e6e7b60f3c784c40bfc05e174324cdc34f7cf0` — byte-identical to pre-move. ✓
  - Blast radius: `BACKLOG.md` (M), `CHANGELOG.md` (M), `docs/changelog-old.md` (A); no `.go` files; `.goreleaser.yaml` untouched. ✓
- **Deviations**: **Rename indicator (TC-6 sub-assertion).** Before writing the new `CHANGELOG.md`, `git status` showed `R CHANGELOG.md -> docs/changelog-old.md`. After repopulating `CHANGELOG.md` with new content, the staged view shows `M CHANGELOG.md` + `A docs/changelog-old.md` rather than `R100` — git's rename detection no longer pairs them because the source path is no longer deleted. This is expected for archive-then-recreate-at-same-path. The substantive integrity guarantee (archive content byte-identical to the original) is proven by the matching blob hash, which is the stronger check. g-testing-exec should assert TC-6 via blob-hash equality, not the `R100` flag.

## Judgement Calls (Task-Type inference)
Plan instruction: "If Task-Type inference for any entry is genuinely ambiguous, surface it rather than guessing silently." Final assignments:

| Entry | Task-Type | Note |
|-------|-----------|------|
| Phase 1b-2: Fix primitive + dcfhfix restructure | feature | new Repo.Fix primitive |
| dcfhfix: default to non-destructive fix-to-new-file | feature | **judgement call** — changes default behaviour + adds `--edit-in-place` flag; feature over chore |
| Add comprehensive integration tests for edge cases | chore | test coverage |
| Validate atomic index replacement under failure conditions | chore | failure-injection tests |
| Update API documentation with current architecture | chore | docs |
| Add usage examples for library consumers | chore | docs/examples |
| Implement dry-run mode for `dcfh update` | feature | new flag plumbing |
| Add progress reporting for long-running operations | feature | new UX surface |
| Handle edge cases in ignore pattern matching | bugfix | **judgement call** — bugfix vs discovery; gitignore negation/dir-only are standard semantics currently broken/uncovered, so bugfix |
| Implement coloured output for better readability | feature | new TTY-gated output |
| Add `dcfh config validate` subcommand | feature | new subcommand |
| Clean up stale scan temp files at startup | feature | **judgement call** — new runtime behaviour (startup sweep); feature over chore |
| Add metrics collection for performance monitoring | feature | new hooks |
| Test on additional Unix variants | chore | CI matrix |
| Test with various Go versions | chore | CI matrix |

Three genuine judgement calls flagged: rows 2, 9, 12. None affects the conformance gates (any valid type passes); they are backlog metadata only and trivially editable later via `backlog-manager modify`.

## Blockers Encountered
None.

## Deferral Check
- [x] All steps from d-implementation-plan.md executed
- [x] All success criteria from a-task-plan.md met (validate clean; list shows all entries; content preserved; CHANGELOG conforms; no content lost)
- [x] b-requirements-plan.md — N/A (chore)
- [x] c-design-plan.md — N/A (chore)
- [x] No planned work deferred
- [x] `pkg/ignore.go:106` stale "see CHANGELOG" reference left untouched per plan (out of scope; optional follow-up backlog item)

## Status
**Status**: Finished
**Next Action**: /cwf-testing-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Lessons Learned
- The `list` count, not the `validate` exit code, is the real conversion oracle — validate passed on the pre-conversion file precisely because it recognised 0 entries.
- Archive-then-recreate at the same path defeats git's `R100` rename detection; verify archive integrity by blob-hash equality instead.

## Security Review

**State**: no findings

no findings: empty changeset

The `security-review-changeset --phase=implementation` helper emitted an empty changeset. The only files touched this phase are `BACKLOG.md`, `CHANGELOG.md`, `docs/changelog-old.md`, and the workflow markdown — none under a CWF-internal security-relevant prefix and none carrying a script shebang, so the helper's classification correctly excludes them all.
