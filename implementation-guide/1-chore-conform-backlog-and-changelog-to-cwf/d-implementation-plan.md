# Conform backlog and changelog to CWF format - Implementation Plan
**Task**: 1 (chore)

## Task Reference
- **Task ID**: internal-1
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/1-conform-backlog-and-changelog-to-cwf
- **Template Version**: 2.1

## Goal
Convert `BACKLOG.md` to the CWF heading-tree schema (so `backlog-manager list/add/modify/retire` operate on it) and replace the version-based `CHANGELOG.md` with a fresh CWF by-task changelog, archiving the old one — without disturbing Debian packaging.

## Resolved Decision (from a-task-plan Open Decision)
CHANGELOG: **archive + fresh start**. Move current `CHANGELOG.md` → `docs/changelog-old.md` (preserves Keep-a-Changelog/SemVer history), create a new conforming `# Changelog` file. Convert BACKLOG in place. Do not touch Debian packaging.

## Contract (the conformance oracle — `.cwf/lib/CWF/Backlog.pm`)
- **BACKLOG entry** splits on `^## (Task|Bug):`. Required metadata: `### Task-Type:` + `### Priority:` (BACKLOG-001). Priority ∈ {Very High, High, Medium, Low, Very Low} (BACKLOG-002). No HTML comments (BACKLOG-004). No struck titles `~~`/`✓` (BACKLOG-005). `### Name` (no colon) = subsection, preserved. `Status`/`Identified in` optional.
- **CHANGELOG**: exactly one `# Changelog` H1 (CHANGELOG-001); entries `## Task N:` need `### Status:` + `### Impact:` (CHANGELOG-002); subsections ordered Changes → Notable → Retired Backlog Items (CHANGELOG-003). **Zero entries validates clean.**
- **Global**: no BOM, no CRLF, no control chars in headings.

## Files to Modify
### Primary Changes
- `BACKLOG.md` — **15 real entries** (lines 31–186). Note: `grep -c '^## Entry: '` returns **16** because line 8 is the `## Entry: <title>` template stub *inside* the self-documenting header — that header (and its stub) is deleted in Step 2, so the conformance target is 15. Rewrite each `## Entry:` → `## Task:`, inject `### Task-Type:` per entry, drop the self-documenting template header, add CWF intro. Priority values unchanged (already valid). Preserve all bodies/subsections verbatim.
- `CHANGELOG.md` — `git mv` to `docs/changelog-old.md`; create new file with `# Changelog` + one-line intro + a prose pointer to the archive. No task entries yet (this task's entry is written at retrospective/rollout). The current file's `## Rejected` design-decision log is archived wholesale with the version history (see note below).

### Out of scope (noted, not changed)
- `pkg/ignore.go:106` prints "dcfh now uses gitignore syntax — see CHANGELOG". The referenced gitignore-syntax note is in **neither** the current `CHANGELOG.md` nor (therefore) the archive — confirmed by grep. This is pre-existing staleness that our change does not worsen (a `CHANGELOG.md` still exists). Leave it untouched; record as an optional follow-up backlog item rather than touching Go in a docs chore. Keeps this task free of any Go change / `go build` gate.
- No change needed to `.goreleaser.yaml` (the `CHANGELOG*` archive glob still matches the new file; deb deliberately ships no changelog — `lintian_overrides: changelog-file-missing-in-native-package`).

### `## Rejected` section — successor home
The old CHANGELOG opens with a `## Rejected` section (rejected-design records). The CHANGELOG parser only recognises `## Task N:` entries, so `## Rejected` parsed as intro — `git mv` archives it intact. Going forward, rejected-design rationale belongs in the implementing task's CHANGELOG `### Notable` subsection (or is simply omitted). This is documented here so the content is not silently lost.

## Method note (deliberate exception)
The `cwf-backlog-manager` skill says not to hand-edit these files. But the helper has **no migration path** for the `## Entry:` legacy format (`normalise` only handles `**Field**:`, confirmed: reports "0 entries"). This one-time migration therefore uses direct edits, gated immediately by `validate --all --strict` + `list` (the same contract the helper enforces). All edits after this task go through the helper. Rejected alternative: deconstruct each entry and re-`add` via the helper — higher risk (routes rich multi-paragraph bodies through `--body-file`, canonicaliser may reflow) for no extra safety once we gate on validate.

## Implementation Steps
### Step 1: Safety net + audit
- [ ] Capture the pre-move CHANGELOG blob hash for the rename gate: `git rev-parse HEAD:CHANGELOG.md`.
- [ ] Capture baseline: the **15 real** entry titles (lines 31–186, i.e. `grep '^## Entry: ' BACKLOG.md | grep -vF '<title>'`) to a scratch file for the identity gate. Do **not** include the line-8 template stub.
- [ ] Scan current BACKLOG for contract-breakers: HTML comments (`<!--`/`-->`), struck titles (`~~`/`✓`), any `### Key:` colon-headings in bodies that would be misread as metadata, and tab/CRLF/BOM. (Pre-audit indicates clean, but re-confirm.)

### Step 2: Convert BACKLOG.md
- [ ] Replace the self-documenting header (lines ~1-29, incl. the line-8 `## Entry: <title>` stub) with CWF intro: `# Backlog` + one-line description (drop the `## Entry:` template + grep block).
- [ ] For each of the 15 entries: `## Entry: <title>` → `## Task: <title>`; insert `### Task-Type: <type>` **immediately after the heading, before `### Priority:`**, inferring type per entry (feature|bugfix|hotfix|chore|discovery) from its content. Rationale for placement: both required metadata keys must precede body prose, else `--strict` escalates the BACKLOG-007 "body before metadata" warning to an error. Keep `### Priority:` value as-is. Leave bodies/subsections untouched.
- [ ] Gate (both must hold — `validate` alone is insufficient because it passes on 0 *recognised* entries; the count gate is what actually proves the BACKLOG conversion worked): `backlog-manager validate --all --strict` exits 0 **and** `backlog-manager list --all-items | grep -c '^  - '` equals **15** (the `  - <title>` bullet is the per-entry marker; band headers are `## <band> (N)` and won't match `^  - `).

### Step 3: Archive + recreate CHANGELOG.md
- [ ] `git mv CHANGELOG.md docs/changelog-old.md` (preserves history; `docs/` already exists).
- [ ] Write new `CHANGELOG.md`: `# Changelog` + intro line + a prose pointer to `docs/changelog-old.md` for pre-CWF history. Prose, not an HTML comment (HTML comments are a BACKLOG-004 error; avoid in CHANGELOG too).
- [ ] Gate: `backlog-manager validate --all --strict` exits 0. (No add/retire round-trip needed — the contract states a `# Changelog` file with zero entries validates clean, CHANGELOG-001 being the only applicable rule; `retire` creating `### Retired Backlog Items` is exercised later when real entries exist, not here.)

### Step 4: Validation
- [ ] `backlog-manager validate --all --strict` exits 0 (validates both files on every call; `--all` = print every error, not a file selector).
- [ ] `backlog-manager list --all-items | grep -c '^  - '` equals 15; spot-check priorities render.
- [ ] Identity gate: every baseline title (15) present post-conversion (diff against scratch title list) — zero spurious additions/deletions.
- [ ] Rename gate: `git rev-parse HEAD:docs/changelog-old.md`... after staging the rename, confirm the archived blob hash equals the Step 1 pre-move hash (pure rename, content untouched); cross-check with `git diff --stat` showing `R100`.
- [ ] `git status` review: only `BACKLOG.md`, `CHANGELOG.md` (deleted), `docs/changelog-old.md` (added) changed. No Go changes (pkg/ignore.go intentionally untouched).

## Code Changes
### Before (BACKLOG entry)
```markdown
## Entry: Phase 1b-2: Fix primitive + dcfhfix restructure

### Priority: Medium

Phase 1b landed the Filter primitive ...
```
### After
```markdown
## Task: Phase 1b-2: Fix primitive + dcfhfix restructure

### Task-Type: feature
### Priority: Medium

Phase 1b landed the Filter primitive ...
```

## Test Coverage
**See e-testing-plan.md for complete test plan** — verification is contract-based (`validate`/`list`) plus content-preservation gates, not Go unit tests.

## Validation Criteria
**See e-testing-plan.md.** Headline gate: `validate --all --strict` clean AND `list --all-items` shows 15 entries (`grep -c '^  - '`) AND all 15 baseline titles preserved AND archived blob hash == pre-move `CHANGELOG.md` blob hash.

## Scope Completion
**IMPORTANT**: Complete all planned implementation before marking task Finished.

If Task-Type inference for any entry is genuinely ambiguous, surface it rather than guessing silently.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Executed as planned across all 4 steps. BACKLOG converted via 16 targeted Edits (1 header + 15 entries, unique-match so bodies untouched); CHANGELOG archived via `git mv` + fresh file. All gates passed: `validate --all --strict` exit 0, `list` count 15, 15 baseline titles identical, archived blob == pre-move blob `73e6e7b6…`. No Go touched (`pkg/ignore.go` left as planned). See f-implementation-exec.md for the per-step record.

## Lessons Learned
One deviation worth recording: archive-then-recreate at the same path defeats git's `R100` rename detection (`git status` shows `M`+`A`, not `R`); archive integrity was therefore asserted by blob-hash equality, the stronger guarantee.
