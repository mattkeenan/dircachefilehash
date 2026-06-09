# Upgrade CwF to v1.1.185 - Implementation Plan
**Task**: 14 (chore)

## Task Reference
- **Task ID**: internal-14
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/14-upgrade-cwf-to-v1-1-185
- **Template Version**: 2.1

## Goal
Execute the design's pinned mechanism: drive the 183→185 upgrade with a
throwaway v1.1.185 `cwf-manage`, verify the merge-free read-tree laydown and the
`cwf_method`→`read-tree` migration, and land it as a single linear commit.

## Files / Artefacts Affected
This task writes **no Go code**. The changes are produced by the 185 installer,
not hand-edited:
- `.cwf/**`, `.cwf-skills/`, `.cwf-rules/`, `.cwf-agents/` — laid down by 185
  `install.bash` (read-tree).
- `.claude/settings.json` — settled by `run_settings_merge` (FR6 watch point).
- `.claude/` artefact symlinks (rules/agents) — re-applied by `run_apply_artefacts`.
- `.cwf/version` — authoritative write (`cwf_version`/`cwf_ref`/`cwf_sha`/
  `cwf_method`/`cwf_installed`/`cwf_source`/`cwf_install_manifest_sha`).
- `implementation-guide/14-…-185/f-implementation-exec.md` — exec record.
**Do not** hand-edit any `.cwf/`-managed file; the installer owns them.

## Implementation Steps (actionable checklist)
1. **Pre-flight snapshot** (for FR6 diff + FR1 before/after):
   - Copy `.claude/settings.json` to a scratch path
     (`<task-tmp>/settings.before.json`; derive `<task-tmp>` per
     `.cwf/docs/conventions/tmp-paths.md`, `mkdir -m 0700` on first use).
   - Record current `.cwf/version` (expect `cwf_method=subtree`,
     `cwf_version=v1.1.183`).
2. **Confirm scoped clean tree**: `git status --short -- .cwf .cwf-skills
   .cwf-rules .cwf-agents` is empty (the only gate `check_clean_tree` enforces).
   Uncommitted `implementation-guide/` edits are fine and don't block.
3. **Materialise the 185 driver**: `rm -rf /tmp/cwf-185-driver` first (clear any
   leftover from an aborted run — `git clone` refuses an existing destination),
   then `git clone --quiet --branch v1.1.185
   file:///home/matt/repo/coding-with-files /tmp/cwf-185-driver` (throwaway,
   detached at the tag). If the clone fails, stop — nothing has touched this repo.
4. **Run the migration-aware driver** from this repo's root:
   `/tmp/cwf-185-driver/.cwf/scripts/cwf-manage update v1.1.185`.
   Capture stdout/stderr/exit. Expect: clone-of-source, read-tree laydown,
   settings merge, perms clamp, authoritative version write, detect-merges
   advisory (the 4 known pre-existing install merges).
   **Gate: a non-zero exit (e.g. `install.bash laydown failed`, or the fatal
   `apply_exact_perms_or_die` sha mismatch) ⇒ STOP. Do not stage, do not run
   later steps; treat as driver-path failure (see "On driver-path failure"
   below). On a clean abort `.cwf/version` still reads `cwf_method=subtree`
   (fail-closed, FR3) — no manual repair; a mid-laydown abort is recoverable per
   design Reversibility.**
5. **FR6 — settings review**: `diff <task-tmp>/settings.before.json
   .claude/settings.json`; record the delta and **explicitly note** whether any
   hooks-list entry or Bash allowlist entry was **added/widened**.
6. **FR1/FR3 verify**: `grep` `.cwf/version` →
   `cwf_version=v1.1.185`, `cwf_ref=v1.1.185`,
   `cwf_sha=6659c1cca72ef033d92546fcd9d42a0f4d817dd9`, `cwf_method=read-tree`.
7. **FR4 gate**: `.cwf/scripts/cwf-manage validate` → expect `validate: OK`.
   **Fail-closed**: any non-OK result ⇒ do NOT commit; treat as driver-path
   failure → design's fallback (contingency). `fix-security` is not a routine
   step (the update's `apply_exact_perms_or_die` already clamped).
8. **FR5 smoke**: `.cwf/scripts/command-helpers/workflow-manager status 14`
   succeeds.
9. **FR2 no-merge check**: after the commit (step 11), assert
   `git log --merges 700baba..HEAD` is empty **and** the new commit is
   single-parent — `git cat-file -p HEAD | grep -c '^parent'` returns `1`. (The
   definitive recheck runs again at landing.)
10. **Record results** in `f-implementation-exec.md`: actual command outputs,
    the `.cwf/version` before/after, the settings diff + added-hook/allowlist
    note, the detect-merges advisory output, a note that the driver ran from the
    pinned `v1.1.185` checkout, and any deviations.
11. **Stage + checkpoint**: `git add -A` (laydown + settings + symlinks +
    `.cwf/version`), then `cwf-checkpoint-commit 14 f "<why>"` (stages the f doc
    and commits everything as one single-parent commit). The exec skill runs the
    `cwf-security-reviewer-changeset` review before this commit.
12. **Cleanup**: `rm -rf /tmp/cwf-185-driver` (run this on the abort paths too,
    not only on success).

## On driver-path failure
Do not improvise. Follow the **Fallback (contingency)** in `c-design-plan.md`
(bare bootstrap installer + reconcile `cwf_install_manifest_sha`/`cwf_source`),
then **re-run `cwf-manage validate`** — a hand-written `cwf_install_manifest_sha`
that does not re-validate is a stop condition, not a workaround. Do not build the
fallback unless the driver path is observed to fail.

## Sequencing note
Run step 4 (the upgrade) **before** staging/committing `f`. The update mutates
`.cwf*`; the consumer commit (step 11) captures laydown + the `f` record
together. Editing `f-implementation-exec.md` mid-flight does not block the update
(it's outside the scoped clean-tree prefixes).

## Test Coverage
No Go unit tests (no Go change). Verification is command-output assertions,
detailed in `e-testing-plan.md` and mapped 1:1 to ACs:
- AC1/AC3 ← step 6 (`.cwf/version` fields incl. `cwf_method=read-tree`).
- AC2 ← step 9 (`git log --merges` empty; single-parent).
- AC3 (validate) ← step 7.
- AC4 ← step 8 (`workflow-manager status 14`).
- AC5 ← step 5 (settings diff + widening note).

## Validation Criteria (before marking d Finished)
- [ ] Steps form a runnable sequence with no missing precondition.
- [ ] Every AC has a corresponding step.
- [ ] Fail-closed handling on `validate` is explicit (no commit on non-OK).
- [ ] No hand-editing of `.cwf/`-managed files in the plan.

## Decomposition Check
- [x] Time / People / Complexity / Risk / Independence — all No (single upgrade).

**Decision**: No decomposition — 0 signals triggered.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-plan 14
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
The 12-step checklist executed cleanly in order; no step required the abort/fallback
path. The exit-code gate (step 4) passed (rc=0), the single-parent assertion
(step 9) returned 1, and the `git add -A` (step 11) correctly captured the
settings/symlinks/version files the read-tree did not auto-stage. See
f-implementation-exec.md for per-step results.

## Lessons Learned
The robustness-review tightenings (re-runnable clone via `rm -rf` first, exit-code
fail-closed gate, single-parent assertion) all proved appropriate even though the
happy path held — they cost nothing and made the run auditable.
