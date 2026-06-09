# Upgrade CwF to v1.1.185 - Implementation Execution
**Task**: 14 (chore)

## Task Reference
- **Task ID**: internal-14
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/14-upgrade-cwf-to-v1-1-185
- **Template Version**: 2.1

## Goal
Execute the design's pinned mechanism: drive the 183→185 upgrade with a throwaway
v1.1.185 `cwf-manage`, verify the merge-free read-tree laydown and the
`cwf_method`→`read-tree` migration, and land it as a single linear commit.

## Execution Checklist
- [x] Read d-implementation-plan.md and e-testing-plan.md thoroughly
- [x] Verify all prerequisites met (branch, scoped clean tree, version)
- [x] Execute implementation steps sequentially
- [x] Update "Actual Results" for each step
- [x] Document any deviations from plan
- [x] Update status when complete

## Actual Results

### Step 1: Pre-flight snapshot
- **Planned**: Snapshot `.claude/settings.json` + record `.cwf/version` to scratch.
- **Actual**: Scratch dir `/tmp/-home-matt-repo-dircachefilehash-task-14` (mode 0700);
  copied `settings.before.json` and `version.before`. Pre-state confirmed:
  `cwf_version=v1.1.183`, `cwf_method=subtree`, `cwf_sha=faf92479…`.
- **Deviations**: None.

### Step 2: Confirm scoped clean tree
- **Planned**: `git status --short -- .cwf .cwf-skills .cwf-rules .cwf-agents` empty.
- **Actual**: Empty. Branch confirmed `chore/14-upgrade-cwf-to-v1-1-185`.
- **Deviations**: None.

### Step 3: Materialise the 185 driver
- **Planned**: `rm -rf /tmp/cwf-185-driver`, then `git clone --branch v1.1.185`.
- **Actual**: Clone OK, detached at `6659c1cca72ef033d92546fcd9d42a0f4d817dd9`.
  `cwf-manage` present in the driver checkout.
- **Deviations**: None.

### Step 4: Run the migration-aware driver
- **Planned**: `/tmp/cwf-185-driver/.cwf/scripts/cwf-manage update v1.1.185` from
  repo root; expect read-tree laydown + method migration + version write +
  detect-merges advisory. Non-zero exit ⇒ STOP (fail-closed).
- **Actual**: **Exit 0.** Driver reported `method: subtree, ref: v1.1.185`, then the
  bootstrap ran with **`Method: read-tree`** (translation confirmed — `subtree` was
  never passed to the installer). Laid down `.cwf/ .cwf-skills/ .cwf-rules/
  .cwf-agents/` (staged, not committed), re-created skill/rule/agent symlinks,
  clamped permissions, wrote `.cwf/version`. Final line:
  `Updated to v1.1.185 (6659c1cca72ef033d92546fcd9d42a0f4d817dd9)`.
  Detect-merges advisory: **4 merges total, all 4 fingerprinted as old CWF
  subtree installs, 0 from elsewhere** — advisory only (TC-8), out of scope here.
  Captured: `update.stdout` / `update.stderr` in the task scratch dir.
- **Deviations**: None.

### Step 5: settings.json review (FR6)
- **Planned**: diff pre/post `.claude/settings.json`; explicitly note any
  added/widened hook or Bash allowlist entry.
- **Actual**: The settings-merge added **exactly one Bash allowlist entry** and
  **zero hook entries** (installer log: `settings: added 1 allowlist entries,
  0 hook entries, 0 env keys`). The added entry:
  ```
  + "Bash(.cwf/scripts/command-helpers/cwf-detect-merges:*)"
  ```
  **Widening note**: this is a **scoped** allowlist entry for the single new 185
  helper `cwf-detect-merges` (the merge-advisory tool). It is **not** a broad
  widening (no wildcard expansion of an existing rule, no new hook in the hooks
  list, no env-var grant). Security-relevant surface added is limited to
  permitting that one specific helper to run.
- **Deviations**: None.

### Step 6: FR1/FR3 version verify
- **Planned**: `.cwf/version` shows `cwf_version=v1.1.185`, `cwf_ref=v1.1.185`,
  `cwf_sha=6659c1c…`, `cwf_method=read-tree`.
- **Actual**: All confirmed:
  ```
  cwf_version=v1.1.185
  cwf_ref=v1.1.185
  cwf_sha=6659c1cca72ef033d92546fcd9d42a0f4d817dd9   (commit-object form)
  cwf_method=read-tree
  cwf_source=file:///home/matt/repo/coding-with-files
  cwf_installed=2026-06-08T22:17:57Z
  cwf_install_manifest_sha=74fb7e26…  (re-pinned; value matches 183 — manifest
                                       content unchanged between releases)
  ```
  The annotated-tag object (`dd6e934c…`) does NOT appear. `cwf_source` is the
  canonical path (not the transient clone path) — the driver path's authoritative
  write, as designed.
- **Deviations**: `cwf_install_manifest_sha` value is byte-identical to the 183
  record. Not a defect — `validate: OK` confirms manifest consistency; the install
  manifest simply did not change between 183 and 185.

### Step 7: FR4 validate gate
- **Planned**: `.cwf/scripts/cwf-manage validate` → `validate: OK`; non-OK ⇒ do
  not commit (fail-closed).
- **Actual**: `[CWF] validate: OK` (rc=0). The update's `apply_exact_perms_or_die`
  clamp already ran (44 chmod lines in the installer log), so validate was clean on
  first run — `fix-security` not needed.
- **Deviations**: None.

### Step 8: FR5 tooling smoke
- **Planned**: `workflow-manager status 14` succeeds.
- **Actual**: rc=0; reports `14 (chore): upgrade-cwf-to-v1-1-185 - 25%`. (The 25%
  is the chore-template phase-set accounting; b/c are extra phases not counted —
  cosmetic, not a defect.)
- **Deviations**: None.

### Step 9: FR2 no-merge check
- See post-commit verification below (run after the checkpoint commit).

### Laydown delta (content actually changed 183→185)
`git status` after the update (pre-stage):
- `M .claude/settings.json` — the one allowlist entry (Step 5).
- `M .cwf/lib/CWF/Backlog.pm`, `M .cwf/scripts/cwf-manage`,
  `A .cwf/scripts/command-helpers/cwf-detect-merges` (new helper),
  `M .cwf/security/script-hashes.json` — the genuine 183→185 source delta. The
  read-tree re-staged all four `.cwf*` prefixes; only these files differ from
  baseline `700baba`.
- `.cwf/version` rewritten (authoritative).
- **No `*.go` changes** (`git diff --name-only 700baba..HEAD -- '*.go'` empty;
  working tree has no `*.go` modifications) — `dcfh` behaviour unaffected (TC-9).
- `script-hashes.json` was written by the installer atomically with the scripts it
  hashes (hence `validate: OK`) — no separate in-task hash refresh required.

## Blockers Encountered
None. The driver path succeeded on the first attempt; the c-design Fallback
(bare bootstrap installer) was not needed.

## Deferral Check
- [x] All steps from d-implementation-plan.md executed
- [x] All success criteria from a-task-plan.md met
- [x] All requirements from b-requirements-plan.md addressed
- [x] All design guidance in c-design-plan.md followed
- [x] No planned work deferred

## Status
**Status**: Finished
**Next Action**: /cwf-testing-exec 14
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Lessons Learned
- The forward-only gap predicted in requirements held exactly: the installed 183
  `cwf-manage` would have passed `CWF_METHOD=subtree` (refused by 185); driving
  with the 185 checkout translated `subtree`→`read-tree` before the installer ran,
  exactly as the design's `cmd_update` citation described.
- The 183→185 source delta is small (4 files); the read-tree re-stage of all four
  prefixes is the expected mechanism, not a sign of a wide change.

## Security Review

**State**: no findings

The changeset comprises:
1. `.claude/settings.json` — adds one allowlist entry for the new helper.
2. `.cwf/lib/CWF/Backlog.pm` — adds a CHANGELOG-005 stale-brand warning check.
3. `.cwf/scripts/command-helpers/cwf-detect-merges` — new Perl helper (the substantive new code).
4. `.cwf/scripts/cwf-manage` — wires the helper into update/check-merges.
5. `.cwf/security/script-hashes.json`, `.cwf/version` — integrity metadata (out of scope per the doc; handled by `cwf-manage validate`).
6. The remaining files are CWF process documentation (implementation-guide markdown) — deleted v1.1.183 task docs and added v1.1.185 task docs. These are process artefacts, not executable.

I've read the full `cwf-detect-merges` source (lines 53-172) and the `cwf-manage` changes (lines 172-294) and the Backlog.pm change (lines 15-47). Let me reason through each threat category against the executable changes. Let me first verify the input flows in `cwf-detect-merges` and `cwf-manage` more carefully — particularly how `$root`/`$git_root` reaches `chdir` and `system`.

The substantive code is fully visible. Let me reason through it.

## Security review reasoning

**(a) Bash injection / unsafe command construction.**

`cwf-detect-merges` uses `capture_git` which is list-form `open(my $fh, '-|')` then `exec('git', @args)` — a true fork/exec with no shell, so git refs/args are never metacharacter-exposed (the comment at lines 91-93 states this and the code matches). The only non-literal arguments passed to git are commit SHAs from `%P` (`$p[1]` in `second_parent_is_squash`), which originate from git's own porcelain output and are passed list-form to `git show -s --format=%s $p[1]` — no shell. In `cwf-manage`, `run_detect_merges` calls `system($helper, $git_root)` — list form, two-arg, no shell. No single-string `system`/backtick interpolation of partly-controlled strings was introduced. Clean.

**(b) Perl helpers consuming git output without `-z` / input validation.**

The main `git log` enumeration uses `-z` with `%x1f` field separators (line 134-136) and splits on `/\0/` then `/\x1f/` — exactly the NUL-separated convention required by `docs/conventions/git-path-output.md`. Subjects can contain anything but NUL, and the code accounts for that (the comment at line 131-132 is explicit). The `%P` parents field is split on `/\s+/`, which is safe because git hex parent SHAs are whitespace-free by definition. `second_parent_is_squash` reads `git show -s --format=%s` for a single commit (not `-z`), but it consumes only one record and matches it against an anchored regex — newline-splitting is not in play. The classified subject strings are used display-only (counts only printed, never the raw subjects — lines 74-75 / 159-164 confirm output is counts only). Clean.

**(c) Prompt injection via user-supplied strings.**

None of the changed executable code feeds strings into LLM context. `cwf-detect-merges` prints only integer counts and fixed advisory text — it deliberately never prints raw commit subjects (line 74-75), which both avoids terminal-escape issues and keeps attacker-controlled commit messages out of any downstream context. The markdown process docs are CWF workflow artefacts, not SKILL templates with `{arguments}` substitution. No new prompt-injection surface.

**(d) Unsafe environment-variable handling.**

`cwf-manage` `cmd_update` now adds `read-tree` as an accepted method and translates a recorded `subtree` install to `read-tree` before building `CWF_METHOD`. The `$laydown_method` is derived from the recorded `$method` (from `.cwf/version`, an integrity-tracked file), constrained to the three-value whitelist by the `die_msg` guard above it (line 216-217). It flows into `CWF_METHOD` in a `local %ENV` block consumed by the delegated `install.bash` — it is one of a fixed set of literals (`subtree`/`copy`/`read-tree`), not free text, so no metacharacter exposure. `CWF_SOURCE` continues to use `file://$clone_dir` as before (unchanged). The migration is fail-closed: `$v{cwf_method}` is only rewritten to `read-tree` after laydown/artefacts/perms succeed (the comment at 244-250 and the assignment at 252 confirm this ordering), so a mid-update failure leaves `cwf_method=subtree` — correct fail-safe direction. No env var feeds `chmod`/`rm`/`open` on an unvalidated path here.

**(e) Pattern-based risks.**

One pattern worth flagging for future reuse, not as a defect:

`run_detect_merges` (`cwf-manage`, around line 186-191) builds `my $helper = "$git_root/.cwf/scripts/command-helpers/cwf-detect-merges"` and runs `system($helper, $git_root)`. This is **safe here because `$git_root` is the CWF-resolved repository root** (the `cwf-manage` `git_root` discovery, not user free-text), and `system` is invoked list-form so even a `$git_root` containing shell metacharacters would not reach a shell. It is also safe because the helper path is a fixed suffix under that root. Audit future uses where `$git_root` might be replaced by a partly-user-controlled path: the list-form `system` keeps it injection-safe, but a path-traversal-style `$git_root` could still select an unexpected executable to run — the invariant that protects this today is "`$git_root` is the trusted resolved repo root", and that invariant must hold at any new callsite.

Similarly, `cwf-detect-merges`'s `chdir $root` (line 124) trusts `$root` from `@ARGV`. Safe here because the only caller (`run_detect_merges`) passes the trusted `$git_root`, and a failed `chdir` fails open (prints a skip line, returns 0 — lines 124-128). Audit future direct CLI uses where `$root` could be attacker-influenced; the failure mode is bounded (it only changes which repo is scanned, and the helper is read-only and counts-only, so the blast radius is an inaccurate advisory count, not state mutation).

These are framing notes for future maintainers, not actionable concerns in this changeset — the current callsites are safe.

**Settings.json and integrity metadata.** The `.claude/settings.json` change adds a single PreToolUse-allowlist entry `Bash(.cwf/scripts/command-helpers/cwf-detect-merges:*)` for the new read-only helper — appropriately scoped to the one new helper, consistent with the existing command-helper entries. The `script-hashes.json` / `.cwf/version` changes are integrity metadata, explicitly out of scope here (covered deterministically by `cwf-manage validate`) per the security-review doc's boundary note.

**Backlog.pm change.** The CHANGELOG-005 check uses `index($_, $STALE_CHANGELOG_BRAND) >= 0` over `@{$tree->{intro}}` with a constant needle — no regex injection, no untrusted-string-as-pattern, no command construction. Scoped to the intro array only (comment at 35-37 explains why the body is excluded). Clean.

Conclusion: the executable changes follow the established list-form spawn and `-z`/`%x1f` porcelain conventions, are counts-only/read-only, and the subtree→read-tree migration is fail-closed. No actionable security findings. The two pattern notes under (e) are forward-looking framing only.

```cwf-review
state: no findings
summary: CWF tooling change; new cwf-detect-merges helper uses list-form git spawn + NUL/%x1f porcelain, counts-only output, fail-closed method migration. Two (e) pattern notes are forward-looking only.
```
