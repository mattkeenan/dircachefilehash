# Upgrade CwF to v1.1.185 - Design
**Task**: 14 (chore)

## Task Reference
- **Task ID**: internal-14
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/14-upgrade-cwf-to-v1-1-185
- **Template Version**: 2.1

## Goal
Specify the mechanism that upgrades this repo's CWF subtree from v1.1.183 to
v1.1.185 merge-free, migrating `cwf_method` `subtree`→`read-tree`, working
around CWF's forward-only limitation (the installed 183 `cwf-manage` cannot
drive a 185 installer that refuses `subtree`).

## Design Priorities
Testability → Readability → Consistency → Simplicity → Reversibility

## Key Decision — Drive the upgrade with 185's migration-aware `cwf-manage`
**Decision**: Materialise a clean v1.1.185 checkout of the CWF tooling and run
**its** `cwf-manage update v1.1.185` against this repo (cwd = this repo), instead
of the installed `.cwf/scripts/cwf-manage`.

**Rationale**: 185's `cmd_update` (`v1.1.185:.cwf/scripts/cwf-manage:504-569`)
is the *only* code path that performs the full canonical `subtree`→`read-tree`
migration in one shot:
- gate accepts `read-tree` (`:504`); installed 183 gate only accepts
  `subtree`/`copy` and would `die "Unknown install method"` on `read-tree`;
- `$laydown_method = ($method eq 'subtree') ? 'read-tree' : $method`,
  `$migrated_subtree = 1` (`:512-513`) — translate **before** the `CWF_METHOD`
  env is built, so the target `install.bash` never receives the refused
  `subtree` value;
- delegates laydown to the *target ref's* `install.bash` with
  `CWF_METHOD=read-tree`, `CWF_SOURCE=file://$clone_dir`, `CWF_REF=$sha`
  (`:519-528`) — merge-free read-tree laydown;
- **authoritative version write** (`:556-562`): `cwf_method=read-tree`,
  `cwf_sha` = `$sha` (commit form via `resolve_sha`→`^{commit}`),
  `cwf_version`/`cwf_ref`, plus restored `cwf_source`, fresh `cwf_installed`,
  and re-pinned `cwf_install_manifest_sha`;
- `run_detect_merges($git_root) if $migrated_subtree` (`:569`) — advisory scan
  surfacing the pre-existing subtree-install merges.

It loads its libs via `FindBin` (`use lib "$FindBin::Bin/../lib"`), so invoking
the v1.1.185 checkout's `cwf-manage` pulls 185's `CWF::` modules while
`$git_root` resolves to **this** repo (from cwd). This is exactly CWF's
documented forward-only recovery ("subsequent updates use the freshly-installed
cwf-manage", INSTALL.md).

**Trade-offs**: Requires a throwaway 185 checkout to bootstrap the driver (small
cost; local `file://` clone). In exchange we get the complete migration —
method rewrite + authoritative version file + detect-merges advisory — that the
bare bootstrap installer does **not** provide (see Alternatives).

## Alternatives Considered (rejected)
1. **Installed `cwf-manage update v1.1.185` (183 driver)** — REJECTED: passes
   `CWF_METHOD=subtree`; 185 `install.bash:82-83` hard-refuses it. Fails at
   preflight (no tree change). This is the forward-only gap.
2. **Bare bootstrap `install.bash` directly** (`CWF_FORCE=1 CWF_REF=v1.1.185
   CWF_METHOD=read-tree CWF_SOURCE=… bash install.bash`) — VIABLE but inferior:
   `post_install` writes **6** version fields
   (`v1.1.185:scripts/install.bash:294-301`) — `cwf_version`/`cwf_method`/
   `cwf_ref`/`cwf_sha`/`cwf_installed`/`cwf_source` — but does **not** re-pin
   `cwf_install_manifest_sha` or run detect-merges, and the `cwf_source` it
   writes is the transient `file://$clone_dir`, not the canonical path.
   Retained as the **fallback** if the driver path fails (followed by reconciling
   `cwf_install_manifest_sha` + `cwf_source`; see fallback note below).
3. **Hand-edit `.cwf/version` `subtree`→`copy`, then installed
   `cwf-manage update`** — REJECTED: yields a `copy` laydown (not the preferred
   `read-tree`), and editing the version file out-of-band defeats the manifest
   SHA guard.

## System Design
### Component Overview
- **185 driver checkout** (`/tmp/…`, throwaway): supplies 185's `cwf-manage` +
  `CWF::` libs. Sourced by `git clone --branch v1.1.185 file://…coding-with-files`.
- **185 `cmd_update`** (the driver): orchestrates clone-of-source → ref/sha
  resolve → method migration → laydown delegation → artefacts → settings merge →
  perms clamp → authoritative version write → detect-merges advisory.
- **185 `install.bash` (read-tree)**: the actual merge-free laydown
  (`git fetch` + `git read-tree --prefix` into the index; no commit, no merge).
- **`validate` gate** (post-clamp): the FR4 acceptance gate.
- **The consumer commit** (us): a single normal commit captures all laid-down
  changes — `.cwf .cwf-skills .cwf-rules .cwf-agents` (read-tree-staged) plus
  `.claude/settings.json`, the `.claude/` artefact symlinks, and `.cwf/version`
  (written later, not auto-staged) — no merge parent.

### Data Flow
1. Ensure the CWF-managed prefixes are **clean**. 185 `cmd_update`'s
   `check_clean_tree` is **scoped** — it inspects only `.cwf .cwf-skills
   .cwf-rules .cwf-agents`, not the whole tree; uncommitted work elsewhere
   (e.g. `implementation-guide/`) does not abort the update. The read-tree
   laydown runs `git rm -r --cached` + `rm -rf` on those four prefixes
   unconditionally, so commit any in-progress work under them first. Changes
   elsewhere don't block but should still be committed so the consumer commit is
   a clean single parent.
2. `git clone --branch v1.1.185 file:///home/matt/repo/coding-with-files
   /tmp/cwf-185-driver` (throwaway; detached at the tag).
3. From this repo's root: `/tmp/cwf-185-driver/.cwf/scripts/cwf-manage update
   v1.1.185`.
   - reads `cwf_method=subtree`; `resolve_source` → `file://…coding-with-files`;
   - migrates to `read-tree`; delegates read-tree laydown; merges settings;
     clamps perms; writes the authoritative version file; runs detect-merges.
4. `git status` — review laydown delta + settings diff; `cwf-manage validate`
   (the now-185 installed one) → expect exit 0. **`validate` is a fail-closed
   gate: on any non-OK result (perms, script-hash, or manifest mismatch) do NOT
   commit — treat it as a driver-path failure and fall back.**
5. Stage **all** changed paths with `git add -A` (read-tree pre-stages the four
   `.cwf*` prefixes into the index, but `run_settings_merge` →
   `.claude/settings.json`, the `.claude/` artefact symlinks, and the
   authoritative `.cwf/version` are written *after* and are **not** auto-staged);
   checkpoint-commit `f` (single-parent).
6. Clean up `/tmp/cwf-185-driver`.
7. (Retrospective) soft-reset to baseline `700baba` → single squash commit →
   ff-only onto local-main. `git log --merges 700baba..HEAD` stays empty.

### Interface (exact commands — pinned for implementation/exec)
```bash
# preconditions: clean working tree on chore/14-upgrade-cwf-to-v1-1-185
git clone --quiet --branch v1.1.185 \
    file:///home/matt/repo/coding-with-files /tmp/cwf-185-driver

# driver runs against THIS repo (cwd), loads 185 libs via FindBin
/tmp/cwf-185-driver/.cwf/scripts/cwf-manage update v1.1.185

# verify — cmd_update's apply_exact_perms_or_die already clamped perms (fatal on
# mismatch), so a successful update leaves validate clean. validate is the gate:
.cwf/scripts/cwf-manage validate          # expect: validate: OK; non-OK => do NOT
                                          # commit, treat as driver-path failure
# fix-security is a contingency only (not a routine step) — see fallback below

# cleanup
rm -rf /tmp/cwf-185-driver
```
Fallback (contingency — implement **only** if the driver path is observed to
fail; do not build both): run the 185 bootstrap installer directly with
`CWF_METHOD=read-tree`, then reconcile `.cwf/version` by hand — re-pin
`cwf_install_manifest_sha` (absent from `post_install`) and correct `cwf_source`
(written as the transient `file://$clone_dir`). Then **re-run `cwf-manage
validate` — a hand-written `cwf_install_manifest_sha` that does not re-validate
is a stop condition, not a workaround.**

## Constraints
- **Clean-tree precondition (scoped)**: 185 `cmd_update`'s `check_clean_tree`
  aborts only if `.cwf`/`.cwf-skills`/`.cwf-rules`/`.cwf-agents` have uncommitted
  changes — not the whole tree. Run the upgrade with those prefixes clean; the
  exec phase commits `f` *after* the upgrade. In-progress edits elsewhere
  (`implementation-guide/`) don't block but commit them for a clean single
  parent.
- **Linear landing**: read-tree creates no commit; we commit once → single
  parent. Land ff-only; never a merge ([[feedback_never_merge_commits]]).
- **Source trust**: `file:///home/matt/repo/coding-with-files`; integrity is
  `validate`'s post-laydown script-hash check, not the path.
- **Worktree/clone bookkeeping** targets the *source* repo (coding-with-files),
  never this repo — no `git -C` against this repo ([[feedback_no_git_dash_c]]).

## Reversibility
- The 185 `install.bash` refusal of `subtree` fires at **preflight, before any
  tree change** — a failed installed-driver attempt is a no-op on the tree.
- The driver path is gated by `check_clean_tree` + update lock; a post-laydown
  failure leaves a partial install recoverable by re-running the bootstrap
  (`CWF_FORCE=1` remove-then-add) or `cwf-manage rollback`.
- The whole task is a branch off `700baba`; abandoning the branch reverts to the
  landed 183 state with zero impact on local-main.

## detect-merges advisory (expected, not a defect)
`run_detect_merges` will flag the **4 pre-existing subtree-install merges** in
the base history (`75e3ae4`/`a2c7635`/`28cfb50`/`103537c`). This is advisory and
out of scope here — it relates to the parked `.githooks` chore and the
[[project_cwf_subtree_merge_commits]] cleanup decision, not this upgrade.

## Decomposition Check
- [x] **Time**: >1 week? No.
- [x] **People**: >2 people? No.
- [x] **Complexity**: 3+ distinct concerns? No — one driven upgrade.
- [x] **Risk**: isolation needed? No — reversible, gated, branch-isolated.
- [x] **Independence**: separable? No — single atomic upgrade.

**Decision**: No decomposition — 0 signals triggered.

## Validation
- [ ] Design review (4 subagents) completed and findings reduced
- [ ] Mechanism verified against `v1.1.185` source (cmd_update + install.bash)
- [ ] Exact commands pinned for the implementation plan

## Status
**Status**: Finished
**Next Action**: Proceed to implementation planning (/cwf-implementation-plan 14)
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
The pinned mechanism worked verbatim: clone v1.1.185 → its `cwf-manage update
v1.1.185` translated `subtree`→`read-tree` before invoking the installer (log:
`Method: read-tree`), wrote the authoritative version file, ran detect-merges.
Exit 0 on first attempt; the Fallback (bare bootstrap) was not needed.

## Lessons Learned
The design's source citations (`cmd_update:504-569`, `install.bash:294-301`) matched
the running code exactly, including the 6-field post_install write corrected during
review. Reading the target version's source ahead of exec removed all exec-time
surprise.
