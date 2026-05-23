# Conform backlog and changelog to CWF format - Testing Plan
**Task**: 1 (chore)

## Task Reference
- **Task ID**: internal-1
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/1-conform-backlog-and-changelog-to-cwf
- **Template Version**: 2.1

## Goal
Verify the converted `BACKLOG.md` and recreated `CHANGELOG.md` conform to the CWF contract, that no backlog content was lost in conversion, and that the archived CHANGELOG is an untouched rename.

## Test Strategy
### Test Levels
This is a documentation-conformance chore — no Go code changes, so no unit/integration test layers apply. Verification is **contract-validation + content-preservation**, using the existing `backlog-manager` helper as the oracle (`.cwf/lib/CWF/Backlog.pm`). All checks are command-line assertions runnable from the git root.

### Coverage Targets
- **Contract**: both files pass `validate --all --strict` (every rule, warnings-as-errors).
- **Conversion completeness**: 100% of the 15 real BACKLOG entries recognised by the tooling.
- **Content preservation**: 100% of entry titles retained; archive byte-identical to source.
- **Blast radius**: only the three intended paths change.

## Test Cases
### Functional Test Cases

- **TC-1: BACKLOG passes strict validation**
  - **Given**: converted `BACKLOG.md`
  - **When**: `backlog-manager validate --all --strict`
  - **Then**: exit 0, no errors, no warnings printed

- **TC-2: All 15 entries are tool-visible** (the real conversion oracle — guards against the "valid but 0 recognised entries" trap)
  - **Given**: converted `BACKLOG.md`
  - **When**: `backlog-manager list --all-items | grep -c '^  - '`
  - **Then**: output equals `15`

- **TC-3: Every entry has required metadata**
  - **Given**: converted `BACKLOG.md`
  - **When**: validate runs BACKLOG-001 (Task-Type + Priority present) and BACKLOG-002 (priority ∈ {Very High,High,Medium,Low,Very Low}) per entry
  - **Then**: zero BACKLOG-001/002 errors; every entry shows a `### Task-Type:` ∈ {feature,bugfix,hotfix,chore,discovery}

- **TC-4: Title identity preserved (no content loss)**
  - **Given**: scratch list of the 15 baseline titles captured pre-conversion
  - **When**: compare against `## Task:` titles in converted file (`grep '^## Task: ' BACKLOG.md | sed 's/^## Task: //'`)
  - **Then**: sets are equal — zero additions, zero deletions

- **TC-5: New CHANGELOG passes strict validation**
  - **Given**: recreated `CHANGELOG.md`
  - **When**: `backlog-manager validate --all --strict`
  - **Then**: exit 0; exactly one `# Changelog` H1 (CHANGELOG-001); zero entries is contract-valid

- **TC-6: Old CHANGELOG archived unchanged**
  - **Given**: pre-move blob hash from `git rev-parse HEAD:CHANGELOG.md`
  - **When**: after `git mv`, compare staged `git rev-parse :docs/changelog-old.md` (and `git diff --cached --stat` rename indicator)
  - **Then**: blob hash identical; git reports a pure rename (R100), no content delta

- **TC-7: Blast radius contained**
  - **Given**: the staged changeset
  - **When**: `git status --short`
  - **Then**: only `BACKLOG.md` (modified), `CHANGELOG.md` (renamed → `docs/changelog-old.md`), and the new `CHANGELOG.md` appear; no `.go` files; `.goreleaser.yaml` untouched

### Non-Functional Test Cases
- **Reliability / data integrity**: TC-4 and TC-6 are the integrity guards (no backlog content lost; archive intact). British-spelling and "no fabricated SemVer history" constraints checked by eyeballing the new intro.
- **Performance / Security / Usability**: N/A — documentation change, no runtime surface.

## Test Environment
### Setup Requirements
- Git root working tree on branch `chore/1-conform-backlog-and-changelog-to-cwf`.
- `.cwf/scripts/command-helpers/backlog-manager` executable (present).
- No test database, no services, no mocks — the files under test are the project's own working-tree copies; all checks are read-only assertions plus the conversion edits themselves.

### Automation
- Manual command-line gates run during g-testing-exec. Not wired into CI (one-off migration). The same `validate` runs in `cwf-manage validate` at each checkpoint commit, giving a second automated pass.

## Validation Criteria
- [ ] TC-1 — BACKLOG `validate --all --strict` clean
- [ ] TC-2 — `list` shows 15 entries
- [ ] TC-3 — required metadata present, priorities valid
- [ ] TC-4 — 15 titles preserved exactly
- [ ] TC-5 — new CHANGELOG `validate --all --strict` clean
- [ ] TC-6 — archive blob-identical / pure rename
- [ ] TC-7 — blast radius limited to the 3 intended paths

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All 7 test cases executed and PASS (see g-testing-exec.md). TC-1 strict-validate clean; TC-2 list=15; TC-3 all Task-Types/priorities valid; TC-4 15 titles identical; TC-5 new CHANGELOG strict-valid (1 H1, 0 entries); TC-6 archive byte-identical; TC-7 blast radius = exactly the 3 product paths, no Go. Both non-functional integrity guards (TC-4, TC-6) pass.

## Lessons Learned
TC-2 (the `list` count) proved to be the load-bearing case — it is the only one of the seven that distinguishes a genuine conversion from a validator false-positive on zero recognised entries.
