# Upgrade CwF to v1.1.185 - Requirements
**Task**: 14 (chore)

## Task Reference
- **Task ID**: internal-14
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/14-upgrade-cwf-to-v1-1-185
- **Template Version**: 2.1

## Goal
Define the functional and non-functional contract for upgrading the installed
CWF subtree from v1.1.183 to v1.1.185, such that the upgrade lays the tree down
**merge-free** (via `read-tree`), migrates the recorded laydown method off the
deprecated `subtree`, and leaves the tree validate-clean. This is the **second
round of task 14**: the landed 183 upgrade (`700baba`) stays in history and 185
is layered on top as a second linear commit.

## Critical Mechanism Note (forward-only upgrade)
The installed **183** `cwf-manage` reads `cwf_method=subtree` from `.cwf/version`
and passes `CWF_METHOD=subtree` to the target installer; **185's `install.bash`
hard-refuses `subtree`** (`install.bash:82-83`) because it forces merge commits.
The `subtree`→`read-tree` migration lives in **185's** `cwf-manage` (translate
before building the `CWF_METHOD` env), not in the installed 183 driver — CWF's
documented **forward-only limitation** (INSTALL.md "Recovering an install stuck
on an old `cwf-manage`"). Therefore the upgrade MUST be driven by the **185
tooling** (a v1.1.185 checkout's migration-aware `cwf-manage`, or equivalently
the target bootstrap `install.bash`), not by `.cwf/scripts/cwf-manage update`
as currently installed. The exact invocation is pinned in design (c).

## Functional Requirements
### Core Features
- **FR1 — Version pinned to v1.1.185**: After the upgrade, `.cwf/version`
  records `cwf_version=v1.1.185`, `cwf_ref=v1.1.185`, and
  `cwf_sha=6659c1cca72ef033d92546fcd9d42a0f4d817dd9` (the **commit-object** SHA
  of the tag — verified `git rev-parse v1.1.185^{commit}`; the annotated-tag
  object `dd6e934c…` is **not** used). The authoritative writer is 185's
  `cwf-manage` cmd_update (or 185 `install.bash`'s post_install, which also uses
  `^{commit}`). `cwf_installed` and `cwf_install_manifest_sha` are also rewritten
  — "all three pinned fields exactly" does not mean "only three fields change".
  - *Acceptance*: `grep` of `.cwf/version` shows the three pinned fields exactly.

- **FR2 — Merge-free laydown (read-tree)**: The upgrade is laid down via
  `read-tree` (the merge-free method); the `subtree` path is **never invoked**
  against this repo. Zero merge commits are added to the task branch.
  - *Acceptance*: laydown performed with `CWF_METHOD=read-tree` (subtree refused
    / never passed); `git log --merges 700baba..HEAD` is empty; the final landed
    commit on local-main is single-parent.

- **FR3 — Laydown method migrated to `read-tree`**: The recorded `cwf_method`
  transitions from `subtree` to **`read-tree`** (the v1.1.185 migration target —
  *not* `copy`). The migration is fail-closed: on any mid-upgrade failure the
  recorded method remains `subtree` (no partial migration).
  - *Acceptance*: post-upgrade `.cwf/version` shows `cwf_method=read-tree`.

- **FR4 — Validate-clean tree (after perms clamp)**: After laydown, artefact
  application, settings merge, and the permission clamp, `cwf-manage validate`
  exits 0 (config + workflow-file + script-hash + permission integrity). The
  ordering is load-bearing: a bare `read-tree`/`cp` laydown honours umask, not
  the recorded permission ceiling, so validate is expected to pass **only after**
  the `fix-security` / `apply_exact_perms_or_die` clamp — never on the raw
  laydown. Any permission drift is repaired in-task via `fix-security`
  (permission-only; never a `sha256` smoothing). This is the single validate gate
  for the task.
  - *Acceptance*: `cwf-manage validate` exit code 0 after the perms clamp.

- **FR5 — Workflow tooling remains functional**: After the upgrade, the CWF
  command-helpers and skills continue to operate (no broken interpreter paths,
  missing helpers, or template breakage).
  - *Acceptance*: `workflow-manager status 14` succeeds post-upgrade (distinct
    from FR4's validate gate — a runtime smoke check, not re-running validate).

- **FR6 — settings.json changes are observed and reviewed**: Any change 185's
  settings-merge makes to `.claude/settings.json` is captured **and reviewed for
  security-relevant widening** — specifically added entries in the hooks list or
  a broadened Bash permission allowlist — not merely diffed and filed.
  - *Acceptance*: a pre/post diff of `.claude/settings.json` is captured in
    `f-implementation-exec.md`, with an explicit note on whether any hook or
    allowlist entry was added/widened.

### User Stories
- **As a** repo maintainer **I want** the 183→185 upgrade to add a single linear
  commit **so that** local-main's strictly-linear history is preserved and no
  merge commit ever lands.
- **As a** repo maintainer **I want** the recorded laydown method migrated to
  `read-tree` **so that** future `cwf-manage update`s stay merge-free without
  manual intervention.

## Non-Functional Requirements
### Performance (NFR1)
- One-shot local operation (single `file://` clone of the CWF source); the only
  target is "completes without hang". No `dcfh` Go changes, so hashing/scan
  performance is unaffected.

### Usability (NFR2)
- The operator runs the documented forward-only recovery (185 tooling) once;
  errors surface via CWF's existing `die`/`die_msg` paths with actionable text.
  No new UX surface is introduced.

### Maintainability (NFR3)
- Use CWF's **documented** upgrade/recovery path (the 185 migration-aware
  tooling) — no bespoke or duplicate upgrade logic ("the best part is no part").
  The installed `cwf-manage update` entrypoint is deliberately **not** used,
  because it cannot cross the forward-only gap; this is following CWF's
  documented procedure, not reinventing it. The task's own changes are confined
  to workflow docs plus the CWF-managed `.cwf/` tree the installer lays down.

### Security (NFR4)
- Laydown sourced from the trusted local checkout
  (`file:///home/matt/repo/coding-with-files`) at a pinned commit SHA via
  list-form `system`/`git clone` — no shell interpolation, no untrusted network
  fetch. The trust guarantee is `validate`'s post-laydown script-hash check, not
  the source path itself.
- Recorded permissions re-enforced post-laydown; script-hash integrity verified
  by `validate`. No secrets, credentials, or new env-var handling introduced
  (`CWF_SOURCE`/`CWF_REF`/`CWF_METHOD` are used, none added).

### Reliability (NFR5)
- Gated by `check_clean_tree` + update lock. Two failure shapes: (a) the
  `subtree`-refusal fires at the target installer's **preflight, before any tree
  change** — the working tree is left untouched, recover by re-running with the
  185 tooling; (b) a failure *after* laydown begins leaves a partial install —
  recover by re-running the bootstrap (`CWF_FORCE=1` remove-then-add) or
  `cwf-manage rollback`. The main `dcfh` index format and data integrity are
  untouched throughout.

## Constraints
- **Linear history**: land ff-only onto local-main; never a merge commit
  ([[feedback_never_merge_commits]]).
- Never bypass commit hooks (`--no-verify`); never `git reset --hard`.
- Keep the landed 183 upgrade (`700baba`) in history (layer, don't replace).
- `cwf_sha` recorded in commit-object form (consistent with the 183 record).
- The upgrade crosses CWF's **forward-only** gap — it must be driven by 185
  tooling, not the installed 183 `cwf-manage update`.

## Decomposition Check
- [x] **Time**: >1 week? No.
- [x] **People**: >2 people? No.
- [x] **Complexity**: 3+ distinct concerns? No — one upgrade, one invariant.
- [x] **Risk**: High-risk components needing isolation? No — reversible, gated.
- [x] **Independence**: Separable parts? No — single atomic upgrade.

**Decision**: No decomposition — 0 signals triggered.

## Acceptance Criteria
- [ ] AC1 (FR1): `.cwf/version` shows `cwf_version=v1.1.185`, `cwf_ref=v1.1.185`,
      `cwf_sha=6659c1c…`.
- [ ] AC2 (FR2/FR3): laydown ran with `CWF_METHOD=read-tree` (subtree never
      passed); `cwf_method=read-tree` recorded; `git log --merges 700baba..HEAD`
      empty; landed commit single-parent.
- [ ] AC3 (FR4): `cwf-manage validate` exits 0 **after** the perms clamp.
- [ ] AC4 (FR5): `workflow-manager status 14` succeeds post-upgrade.
- [ ] AC5 (FR6): `.claude/settings.json` pre/post diff captured, with an explicit
      added-hook / widened-allowlist review note.

## Status
**Status**: Finished
**Next Action**: Proceed to design (/cwf-design-plan 14)
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All ACs met — AC1 (version pinned), AC2 (read-tree, subtree never passed,
single-parent), AC3 (`validate: OK` post-clamp), AC4 (`workflow-manager status 14`
ok), AC5 (settings diff captured: one scoped `cwf-detect-merges` allowlist entry,
zero hooks). FR1–FR6 all satisfied. See f/g exec records.

## Lessons Learned
The "Critical Mechanism Note" (forward-only upgrade) was the decisive insight: the
b-phase plan review caught that the installed 183 driver would fail at 185's
`subtree` refusal. Driving with 185 tooling was the correct, documented recovery.
