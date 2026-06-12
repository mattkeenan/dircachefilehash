# Fix CHANGELOG Task 14 round 2 heading defect - Implementation Plan
**Task**: 25 (hotfix)

## Task Reference
- **Task ID**: internal-25
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: hotfix/25-fix-changelog-task-14-round-2-heading-defect
- **Template Version**: 2.1

## Goal
Make `## Task 14 (round 2)` parse as a real CHANGELOG entry by moving the `(round 2)` marker after the colon, relocate the two stray v1.1.185 `### Status:`/`### Impact:` pairs out of `## Task 15`, and install one correct pair under the now-real round-2 entry, so `backlog-manager validate` passes.

## Workflow
Patterns first → Test → Minimal impl → Refactor green → Commit message explains "why"

## Root Cause (verified against baseline 7dbced1a — see "Empirical Verification" below)
The CHANGELOG entry regex is `qr/^## Task[ \t]+(\d+):[ \t]*(.+?)[ \t]*\n?\z/` (`.cwf/lib/CWF/Backlog.pm:231`). It requires the colon **immediately** after the digits. `## Task 14 (round 2):` has `(round 2)` between `14` and `:`, so it **does not parse as an entry at all** — the line and everything under it is absorbed as body of the preceding `## Task 15` entry.

Consequently Task 15's *merged* subsection sequence becomes `Status, Impact, …, Changes, Notable, [round-2 heading as body], Changes, Notable`. The duplicate `Changes` is what trips `CHANGELOG-003` (`subsections out of order`) at line 198 — it is **not** a missing-Status/Impact "anchor" problem. The two stray Status/Impact pairs at lines 180–183 are byte-identical v1.1.185 text that belongs to round 2.

```
176  ## Task 15: Interactive-tree status colour coding
178  ### Status: ... ~1 day ...              <- Task 15's OWN (keep)
179  ### Impact: Makes change status legible <- Task 15's OWN (keep)
180  ### Status: ... ~0.5 day ...            <- STRAY copy 1 (v1.1.185) — delete
181  ### Impact: Upgrades the vendored CWF.. <- STRAY copy 1 — delete
182  ### Status: ... ~0.5 day ...            <- STRAY copy 2 — delete
183  ### Impact: Upgrades the vendored CWF.. <- STRAY copy 2 — delete
185  ### Changes / 189 ### Notable           (Task 15's own)
196  ## Task 14 (round 2): Upgrade CWF subtree to v1.1.185   <- DOES NOT MATCH regex
198  ### Changes / 206 ### Notable           (parsed as Task 15 body → dup Changes)
```

## Empirical Verification (done during planning, against /tmp copies via `parse_changelog_tree`/`validate_changelog_tree`)
| Edits applied | Validator |
|---|---|
| none (baseline) | ❌ `CHANGELOG-003` @198 |
| strays removed + Status/Impact added, **no rename** | ❌ `CHANGELOG-003` @197 — still fails |
| heading rename **only** | ❌ `CHANGELOG-002` @196 (round-2 now parses, lacks Status/Impact) |
| **rename + strays removed + Status/Impact** | ✅ **CLEAN** |

The heading rename is the load-bearing edit; all three together are required and sufficient.

## Files to Modify
### Primary Changes
- `CHANGELOG.md` — (1) rename the round-2 heading so it parses; (2) remove the two stray Status/Impact pairs from `## Task 15`; (3) add one Status/Impact pair under the renamed round-2 entry before its `### Changes`.

### Supporting Changes
- None. Documentation-only; no Go code, no tests, no config.

## Fix Mechanism Decision
- `backlog-manager` has **no** subcommand that renames a heading or relocates an entry's Status/Impact body (`modify` = `--priority` only; `retire`/`add`/`delete` operate on whole entries; `normalise` only converts legacy `**Field**:` form — inapplicable, file is already heading-tree).
- Therefore the fix is a **minimal raw `Edit`** of `CHANGELOG.md`, with `backlog-manager validate --all` as the acceptance gate (the validator enforces the same heading-tree contract the helper would, so a green validate proves the raw edit stayed in-format).

## Known Acceptable Consequence
After the rename there are two `## Task 14:` entries (round 1 at line 213, renamed round 2 at line 196). The validator allows this (the corrected file validates CLEAN). `find_changelog_entry_by_task_num` (`Backlog.pm`) returns the first match, so any task-14 tooling lookup resolves to round 1 — a benign pre-existing quirk, not introduced by this fix. Recorded here for the retrospective; no action.

## Implementation Steps
### Step 1: Capture baseline
- [ ] Run `.cwf/scripts/command-helpers/backlog-manager validate --all`; confirm it fails (exit 1) with `CHANGELOG-003` at `CHANGELOG.md:198` (records the red starting state for g-testing-exec).

### Step 2: Rename the round-2 heading (Edit 1)
- [ ] `Edit` `CHANGELOG.md`: `## Task 14 (round 2): Upgrade CWF subtree to v1.1.185` → `## Task 14: Upgrade CWF subtree to v1.1.185 (round 2)`. The heading is unique, so the match is unambiguous.

### Step 3: Remove the two stray pairs from Task 15 (Edit 2)
- [ ] `Edit` `CHANGELOG.md`: match the contiguous 4-line stray block (lines 180–183: Status/Impact/Status/Impact, v1.1.185 text) and delete it, leaving Task 15 with only its own pair (178–179) followed by the blank line before `### Changes`. The 4-line concatenation is unique in the file.

### Step 4: Add the Status/Impact pair under the renamed round-2 entry (Edit 3)
- [ ] `Edit` `CHANGELOG.md`: between the renamed heading's trailing blank line and `### Changes`, insert one pair:
  - `### Status: Complete (completed 2026-06-09, ~0.5 day, within the ~0.5 day / Medium estimate)`
  - `### Impact: Upgrades the vendored CWF workflow tooling from v1.1.183 to v1.1.185 **merge-free**, ... squashed.` (verbatim text lifted from the stray copy before its deletion — transcription confirmed clean by the CLEAN run above).

### Step 5: Validate (acceptance gate)
- [ ] Re-run `backlog-manager validate --all`; require **exit 0** with no error/warning lines.

### Step 6: Confirm scope
- [ ] `git diff CHANGELOG.md`: verify the diff is confined to the Task 15 head (−4 lines), the round-2 heading rename (±1 line), and the round-2 Status/Impact insert (+2 lines) — and touches **no** other entry's prose.

## Code Changes
### Edit 1 — round-2 heading
```markdown
- ## Task 14 (round 2): Upgrade CWF subtree to v1.1.185
+ ## Task 14: Upgrade CWF subtree to v1.1.185 (round 2)
```
### Edit 2 — Task 15 head (remove strays)
```markdown
  ### Status: Complete (... ~1 day ...)
  ### Impact: Makes change status legible ...squashed.
- ### Status: Complete (... ~0.5 day ...)            <- delete
- ### Impact: Upgrades the vendored CWF ... v1.1.185  <- delete
- ### Status: Complete (... ~0.5 day ...)            <- delete
- ### Impact: Upgrades the vendored CWF ... v1.1.185  <- delete

  ### Changes
```
### Edit 3 — renamed round-2 head (add Status/Impact)
```markdown
  ## Task 14: Upgrade CWF subtree to v1.1.185 (round 2)
+
+ ### Status: Complete (completed 2026-06-09, ~0.5 day, within the ~0.5 day / Medium estimate)
+ ### Impact: Upgrades the vendored CWF ... v1.1.185 ... squashed.

  ### Changes
```

## Test Coverage
**See e-testing-plan.md for complete test plan** — acceptance is `backlog-manager validate --all` green plus a scoped `git diff`. No unit/integration code tests (doc-only change).

## Validation Criteria
**See e-testing-plan.md.** Summary: validate exits 0; round-2 heading parses as a Task 14 entry; it has one Status + one Impact before Changes; Task 15 has exactly one Status/Impact pair; diff confined to the two entries.

## Scope Completion
**IMPORTANT**: Complete all planned implementation before marking task Finished.

No deferral anticipated — the change is three edits to one file.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Plan executed as written (see f-implementation-exec.md). The only deviation: planned Edits 1 and 3 (heading rename + round-2 Status/Impact insert) were applied as a single `Edit` because they touch adjacent lines — no content difference. `validate --all` green; diff confined to the two entries (4 ins / 5 del).

## Lessons Learned
The "Empirical Verification" section that re-attributed the failure to the heading-parse defect was the highest-value part of this plan — it turned implementation into a mechanical application of an already-proven edit set. See j-retrospective.md.
