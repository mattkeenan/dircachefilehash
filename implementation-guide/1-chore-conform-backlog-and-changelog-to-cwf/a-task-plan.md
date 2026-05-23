# Conform backlog and changelog to CWF format - Plan
**Task**: 1 (chore)

## Task Reference
- **Task ID**: internal-1
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/1-conform-backlog-and-changelog-to-cwf
- **Baseline Commit**: 64e5abdc24f0278a4c78d0b2001517b36f3a8461
- **Template Version**: 2.1

## Goal
Convert `BACKLOG.md` and `CHANGELOG.md` from the project's ad-hoc/Keep-a-Changelog formats to CWF's canonical heading-tree schema so the `backlog-manager` tooling (`list`/`add`/`modify`/`retire`/`validate`) can read and mutate the existing content.

## Context / Findings (from current-state audit)
- `backlog-manager validate --all --strict` already exits **0** — but this is misleading: the validator recognises **0 entries** in either file, so there is nothing for it to reject. Passing means "no recognised entries are malformed", not "conformant".
- `backlog-manager list` returns **empty** despite 16 BACKLOG entries → the existing content is invisible to the tooling.
- **BACKLOG gap**: uses `## Entry: <title>` + 3-band `### Priority:` + `### Scope/Notes`. CWF canonical: `## Task: <title>` + `### Task-Type:` + 5-band `### Priority:` (Very High…Very Low) + `### Status:` + `### Identified in:`.
- **CHANGELOG gap**: organised by **version** (Keep a Changelog + SemVer, 12 `## [x.y.z]` sections) + a `## Rejected` section. CWF organises by **task** (`## Task N:` with `### Status/Duration/Impact/Notable/Retired Backlog Items`).
- `normalise` does **not** apply — it only migrates the `**Field**:` (Task-131-era) legacy format. Conversion here is bespoke.

## Success Criteria
- [ ] `backlog-manager validate --all --strict` exits 0 after conversion (regression guard — must stay clean).
- [ ] `backlog-manager list --all-items` lists every active BACKLOG entry (currently 0 of 16 visible).
- [ ] All 16 BACKLOG entries preserved (titles + body content intact) and re-expressed with required CWF metadata headings (`Task-Type`, `Priority`, `Status`, `Identified in`).
- [ ] CHANGELOG conforms to the agreed target structure (see Open Decision) and a dry/test `retire` can locate or create `### Retired Backlog Items` correctly.
- [ ] No content lost: entry/section count and titles preserved vs baseline (verified by diff + count check).

## Open Decision (resolve at plan review, before exec)
**CHANGELOG organising principle.** The project deliberately adopted Keep a Changelog + SemVer (by version); CWF is by task. These are mutually exclusive top-level structures. Options:
- **(A) Full conversion** to CWF by-task model — discards SemVer/Keep-a-Changelog and the 12 version→release mappings.
- **(B) Hybrid** — retain version history, graft on CWF's `### Retired Backlog Items` slot so `retire` works. Risk: may not be what the contract expects; needs validation.
- **(C) Scope CHANGELOG out** — conform BACKLOG only; leave CHANGELOG on Keep a Changelog (a recognised external standard) and document the deviation.

Recommendation deferred to review. This single decision drives most of the CHANGELOG effort and risk.

## Original Estimate
**Effort**: ~0.5–1 day (BACKLOG mechanical; CHANGELOG depends on Open Decision)
**Complexity**: Low–Medium
**Dependencies**: `backlog-manager` helper (present); the agreed CHANGELOG target (Open Decision)

## Major Milestones
1. **Decision locked**: CHANGELOG target chosen (A/B/C) at plan review.
2. **BACKLOG converted**: 16 entries re-expressed in CWF schema; `list` shows all; validate strict-clean.
3. **CHANGELOG converted**: per chosen option; `retire` round-trips; validate strict-clean.

## Risk Assessment
### High Priority Risks
- **CHANGELOG paradigm conflict**: full conversion abandons the project's deliberate SemVer/Keep-a-Changelog choice and loses release-version mapping.
  - **Mitigation**: resolve the Open Decision at review before any edit; prefer the least-destructive option that satisfies the tooling.

### Medium Priority Risks
- **Content loss during bespoke conversion** (16 rich BACKLOG entries + 12 CHANGELOG sections, `normalise` unavailable).
  - **Mitigation**: count + title-preservation gate (mirror `normalise`'s AC5 cardinality/identity checks); review `git diff` before commit.
- **Validator blind-spot masks partial conversion** (passes on 0 recognised entries).
  - **Mitigation**: gate on `list` output count, not just `validate` exit code.

## Dependencies
- `backlog-manager` helper (installed, working).
- CHANGELOG target decision (user, at review).

## Constraints
- Must preserve all existing content (this is reformatting, not pruning).
- Use the `backlog-manager` contract as the conformance oracle, not hand-judgement.
- British spelling in prose; no SemVer history fabrication.

## Decomposition Check
- [ ] **Time**: >1 week? No.
- [ ] **People**: >2 people? No.
- [x] **Complexity**: 2–3 distinct concerns (BACKLOG field-schema conversion; CHANGELOG paradigm decision + conversion).
- [ ] **Risk**: high-risk component needing isolation? CHANGELOG paradigm is the risky piece but is small.
- [x] **Independence**: BACKLOG and CHANGELOG are fully independent and could be split into subtasks 1.1 / 1.2.

2 signals triggered (Complexity, Independence). Splitting into 1.1 (BACKLOG) + 1.2 (CHANGELOG) is defensible, but each half is small and they share one conversion approach. **Recommendation**: keep as one task unless the review picks CHANGELOG option (A) full conversion, which would justify isolating it as 1.2.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-plan (chore skips requirements & design)
**Blockers**: Open Decision (CHANGELOG target) pending review

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All success criteria met. `validate --all --strict` clean; `list` shows all entries (15 BACKLOG, was 0 visible); 15 titles + bodies preserved; CHANGELOG conforms (fresh by-task file, old archived byte-identical); no content lost. The Open Decision (CHANGELOG paradigm) was resolved at review as **archive + fresh start** (option C-adjacent: archive the version history, start a conforming by-task file). Kept as one task — the Independence/Complexity signals did not justify splitting given the shared conversion approach.

## Lessons Learned
The decisive choice was made here at planning: gate on `list` visibility, not the `validate` exit code, because the validator passes on zero recognised entries. See j-retrospective.md for full learnings.
