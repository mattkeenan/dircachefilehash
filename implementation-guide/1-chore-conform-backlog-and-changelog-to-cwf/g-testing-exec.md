# Conform backlog and changelog to CWF format - Testing Execution
**Task**: 1 (chore)

## Task Reference
- **Task ID**: internal-1
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/1-conform-backlog-and-changelog-to-cwf
- **Template Version**: 2.1

## Goal
Execute the 7 contract-based test cases from e-testing-plan.md and verify the conversion from d-implementation-plan.md: BACKLOG conforms and is tool-visible, CHANGELOG recreated cleanly, archive byte-identical, blast radius contained.

## Execution Checklist
- [x] Read e-testing-plan.md and d-implementation-plan.md thoroughly
- [x] Verify test environment ready
- [x] Execute test cases sequentially
- [x] Record pass/fail for each test
- [x] Document failures with reproduction steps
- [x] Update status to "Finished" when all pass

## Test Results

### Functional Tests

| Test ID | Test Case | Expected | Actual | Status |
|---------|-----------|----------|--------|--------|
| TC-1 | BACKLOG `validate --all --strict` | exit 0 | exit 0 | PASS |
| TC-2 | `list --all-items \| grep -c '^  - '` | 15 | 15 | PASS |
| TC-3 | Every entry has Task-Type + valid Priority | 15 valid Task-Type, all priorities in 5-band set | 15 Task-Type (feature ×8, chore ×6, bugfix ×1); priorities High ×4, Medium ×7, Low ×4; zero invalid | PASS |
| TC-4 | Title identity vs 15 baseline titles | sets equal (0 add, 0 del) | `diff` empty — identical | PASS |
| TC-5 | New CHANGELOG `validate --all --strict` | exit 0; one `# Changelog` H1; 0 entries valid | exit 0; H1 count 1; 0 task entries | PASS |
| TC-6 | Old CHANGELOG archived unchanged | archived blob == pre-move blob `73e6e7b6…` | `git rev-parse HEAD:docs/changelog-old.md` = `73e6e7b60f3c784c40bfc05e174324cdc34f7cf0` — byte-identical | PASS |
| TC-7 | Blast radius contained | only BACKLOG.md, CHANGELOG.md, docs/changelog-old.md; no `.go`; `.goreleaser.yaml` untouched | exactly those three product paths (M/M/A); zero `.go`; `.goreleaser.yaml` untouched | PASS |

### Non-Functional Tests
- **Reliability / data integrity**: TC-4 (no backlog content lost — all 15 titles preserved) and TC-6 (archive intact, byte-identical) both PASS — the two integrity guards.
- **British spelling / no fabricated SemVer**: the new `CHANGELOG.md` intro contains no invented version history; it points to `docs/changelog-old.md` for the pre-CWF Keep-a-Changelog record. Prose uses British spelling. PASS by inspection.
- **Performance / Security / Usability**: N/A — documentation change, no runtime surface.

## Test Failures
None. All 7 functional test cases passed; both non-functional guards passed.

### Note on TC-6 method (matches f-implementation-exec deviation)
TC-6 is asserted by **blob-hash equality**, not git's `R100` rename flag. Because the archive-then-recreate writes new content back to the original `CHANGELOG.md` path, git no longer reports a pure rename (it reports `M CHANGELOG.md` + `A docs/changelog-old.md`). Blob-hash equality is the stronger integrity guarantee and is what TC-6 checks; the byte-identical result confirms the archive is the original content untouched.

## Coverage Report
Contract-coverage, not line-coverage (documentation chore — no Go code). All four e-testing-plan coverage targets met:
- Contract: both files pass `validate --all --strict`. ✓
- Conversion completeness: 100% (15/15 entries tool-visible). ✓
- Content preservation: 100% titles retained; archive byte-identical. ✓
- Blast radius: exactly the 3 intended paths. ✓

## Status
**Status**: Finished
**Next Action**: /cwf-retrospective
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Security Review

**State**: no findings

no findings: empty changeset

The `security-review-changeset --phase=testing` helper emitted an empty changeset. This phase touched only `g-testing-exec.md` (and a one-word count correction in `f-implementation-exec.md`) — markdown workflow files under no CWF-internal security-relevant prefix and carrying no script shebang, so the helper correctly excludes them.

## Lessons Learned
*To be captured during retrospective*
