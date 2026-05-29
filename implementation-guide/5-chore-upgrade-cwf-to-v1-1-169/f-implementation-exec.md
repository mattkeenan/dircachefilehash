# upgrade CWF to v1.1.169 - Implementation Execution
**Task**: 5 (chore)

## Task Reference
- **Task ID**: internal-5
- **Branch**: chore/5-upgrade-cwf-to-v1-1-169
- **Template Version**: 2.1

## Goal
Execute the v1.1.155→v1.1.169 upgrade per `d-implementation-plan.md`.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-exec
**Blockers**: None

## Step 1: Pre-flight — Complete

- [x] Tree clean under `.cwf/`, `.cwf-skills/`, `.cwf-rules/`, `.cwf-agents/`
- [x] Tag `v1.1.169` present in source `/home/matt/repo/coding-with-files`; `scripts/install.bash` present at that tag
- [x] CHANGELOG v1.1.156→169 reviewed (14 tasks; deltas in d-plan)
- [x] Source tag dereferenced commit: `0764380e60a6c1fb3788406942dfab7ae13bb585`
- [x] Source tag annotated-object SHA: `473baea2dd1d77bac9f100a1036f091eeccd0a4b`
- [x] **Pre-upgrade repo HEAD (precondition gate, revert target)**: `3ecf3b86f7cd76498da68a039ef059b4f7394693` (= e-plan checkpoint)
- [x] No stale `.cwf/.update.lock`
- [x] Pre-run `.cwf/version` confirms `cwf_version=v1.1.155`, `cwf_sha=cfe1a6e…`

## Step 2: Run the upgrade — Complete (clean run)

### Command
```bash
.cwf/scripts/cwf-manage update v1.1.169
```

### Outcome
End-to-end success. Final stdout line: `[CWF] Updated to v1.1.169 (473baea2dd1d77bac9f100a1036f091eeccd0a4b)`.

**install.bash created 9 auto squash commits** (one more than the v1.1.163 attempt's 8 — the extra `b2babc0` "Squashed '.cwf/' content from commit 5e15153" is a v1.1.169-version artefact, not a defect):

| SHA | Subject |
|---|---|
| `b2babc0` | Squashed '.cwf/' content from commit 5e15153 |
| `0698a1e` | CWF: remove existing install for reinstall |
| `bacc6b4` | Squashed '.cwf-skills/' content from commit f516888 |
| `d1bd18c` | Add CWF core (473baea2...) |
| `a60b113` | Squashed '.cwf-rules/' content from commit 6f16b39 |
| `1c3ad63` | Add CWF skills (473baea2...) |
| `149f001` | Squashed '.cwf-agents/' content from commit 7d75633 |
| `d0e787d` | Add CWF rules (473baea2...) |
| `cefc814` | Add CWF agents (473baea2...) |

**`cwf-apply-artefacts` ran clean** — the d-plan's worry case (`rules-inject` conflict that killed v1.1.163) did not occur because upstream Task 167 removed the entry from the manifest. Observed inventory output:
```
[CWF] INFO: gitignore-entries: already up to date
[CWF] INFO: cwf-rules-bundle: tree applied
[CWF] INFO: claude-md-preamble: already up to date
[CWF] INFO: claude-rules-symlinks: 0 new symlink(s), 1 kept, 0 broken removed
```

**Settings-merge ran twice** — install.bash's pass (`added 3 allowlist entries, 1 hook entries, 0 env keys`) and then `cwf-manage`'s own pass (`added 0 allowlist entries, 0 hook entries, 0 env keys` — idempotent confirmation).

**`apply_exact_perms_or_die` cleared all perm drifts**:
- 9 × 0600→0444 for the manifest-owned content files (the 10th, `.cwf/templates/install/rules-inject.txt`, was removed at v1.1.169 — disappeared from the report rather than fixed, as predicted)
- ~30 × 0700→0500 for scripts and helpers (v1.1.169 tightened script perms; new since v1.1.155)

**Authoritative version write** — `.cwf/version` now records `cwf_version=v1.1.169`, `cwf_sha=473baea2…`, `cwf_ref=v1.1.169` (flipped from `HEAD`), plus a new `cwf_install_manifest_sha=e1926a2f…` field (new in v1.1.169, likely the T167 INV-1/INV-2 plumbing).

### Deviations from plan
None. The failure-handling branches (CWF_UPGRADE_RESOLVE re-run, half-applied probe, soft-reset+clean revert) were not exercised because the laydown was clean. They remain documented in d-plan for future use.

## Step 3: Validate & verify laydown — Complete

| Check | Expected | Actual | Pass |
|---|---|---|---|
| `cwf-manage status` Version | v1.1.169 | v1.1.169 | ✓ |
| `cwf-manage status` SHA | `473baea2…` (tag object) | `473baea2…` | ✓ |
| `cwf-manage status` Ref | `v1.1.169` (flipped from HEAD) | `v1.1.169` | ✓ |
| `cwf-manage validate` | exit 0 | exit 0, `validate: OK` | ✓ |
| `.claude/skills/cwf-*` symlinks resolve | all | no BROKEN: output | ✓ |
| ` ```cwf-review ` verdict-block | present in both `.cwf-agents/` and `.claude/agents/` copies | grep count 1 in each | ✓ |
| SubagentStop hook registered | yes | present in `.claude/settings.json` | ✓ |
| Hook helper executable | yes | `test -x .cwf/scripts/hooks/subagentstop-security-verdict-guard` true | ✓ |
| `workflow-manager status` | exit 0, lists tasks 1–5 | exit 0; task 5 at 25% (a/d/e done) | ✓ |
| `backlog-manager validate --all` | exit 0 | exit 0, no output | ✓ |
| `task-context-inference` | resolves task 5 | `conclusive, correlated, task_num=5, workflow_step=f-implementation-exec` | ✓ |

**Note**: `task-context-inference` previously returned `inconclusive, uncorrelated, task_nums: 2,5` on this branch under v1.1.155. After v1.1.169 (Task 166 fix) it resolves task 5 conclusively. Live confirmation of T166's subtask-aware enumeration + ancestry-collapse predicate.

## Step 4: Commit — Complete

Single follow-up commit for post-laydown metadata, named-path staged per plan (no `git add -A`):

```
e267935 Task 5: Post-laydown metadata for CWF v1.1.169 upgrade
```

Staged: `.cwf/version` (new) + `.claude/settings.json` (modified). `install-manifest.json` and `script-hashes.json` rode the subtree commits and required no follow-up (misalignment-review prediction confirmed).

The f-implementation-exec.md checkpoint commit (this file) follows in Step 9.

## Actual Results

- Version pinned to v1.1.169; SHA recorded as tag-object `473baea2…`; ref flipped to `v1.1.169`; cwf_version field is semver (FR1 weakness sidestepped via explicit tag).
- All 9 success-path TCs pass.
- 10 pre-existing 0600→0444 perms drifts resolved (9 corrected + 1 file removed at v1.1.169).
- Workflow tooling regression: clean. `task-context-inference` improved from inconclusive→conclusive (T166 fix landed and observed in-band).

## Lessons Learned

1. **The plan-review subagents caught the right things.** The robustness reviewer's eight findings translated directly into either pre-flight discipline (precondition HEAD gate, lock check, deterministic half-applied probe) or revert-path additions (`git clean -fdx --dry-run`, named-path staging). None of these failure paths fired this time — but every one of them addresses a real gap surfaced by the v1.1.163 attempt.
2. **Upstream Task 167 is the *right* fix.** Our v1.1.163 attempt failed because the manifest's `rules-inject` entry disagreed with what the subtree shipped. Patching the consumer (e.g. forcing `CWF_UPGRADE_RESOLVE=keep`) would have papered over the defect; T167 removed the dual-distribution entirely and added INV-1/INV-2 invariants upstream. Pivoting forward rather than retrying-with-workaround was the higher-quality path.
3. **`cwf_sha` is the annotated-tag object SHA, not the dereferenced commit.** `resolve_sha` calls `git rev-parse` which for annotated tags returns the tag object (`473baea2…`), not the commit (`0764380…`). Our v1.1.163 TC-1 had this inverted; this exec confirms the correction.
4. **Soft reset + targeted checkout + named-path discard is the right minimal revert.** The v1.1.163 recovery established this pattern: don't wholesale `git reset --hard`; use soft-reset so the reflog preserves the attempt, then mechanically restore tracked files and remove targeted orphans. The d-plan codifies it as the documented escape hatch.
5. **T166 (subtask-aware context inference) was observably broken on this branch under v1.1.155 and observably fixed under v1.1.169.** First fresh-task exercise of the upstream fix in this consumer.

## Security Review

**State**: error

error: cap exceeded: 1632 production lines > 500

The exec-phase changeset spans from the baseline (`07366ad`, task 4 tip) through the v1.1.169 laydown to HEAD. The helper reports `reviewed 19 files, 2252 lines (1632 production), anchor=07366ad`. The production-weighted count exceeds the 500-line cap because no `security.review.test-paths` patterns are declared in `cwf-project.json`, so every line counts as production — including the upstream-authored CWF laydown content (which is the bulk of the diff).

**What's actually new in this task's diff (security-relevant subset)**:
- `.claude/settings.json`: settings-merge added the `SubagentStop` hook entry (target: `.cwf/scripts/hooks/subagentstop-security-verdict-guard`, timeout 5, matcher `cwf-security-reviewer-changeset`) and 3 allowlist entries for new helper scripts (`cwf-check-tree-symlinks`, `security-review-classify`, `subagentstop-security-verdict-guard`).
- `.cwf/version`: rewritten with `cwf_version=v1.1.169`, `cwf_sha=473baea2…`, new field `cwf_install_manifest_sha`.
- Workflow MD files (a/d/e/f-implementation-exec.md): planning content only, no executable surface.
- All other diff content (~95%) is upstream CWF source code/configuration laid down by `cwf-manage update`, already reviewed at the upstream tag.

Per skill contract, the subagent is **not** invoked when exit=2. Surfacing for user decision: accept-and-record (no manual review needed for upstream-authored content) or perform a manual review of the security-relevant subset above.

