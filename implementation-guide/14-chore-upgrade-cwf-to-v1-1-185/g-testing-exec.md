# Upgrade CwF to v1.1.185 - Testing Execution
**Task**: 14 (chore)

## Task Reference
- **Task ID**: internal-14
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/14-upgrade-cwf-to-v1-1-185
- **Template Version**: 2.1

## Goal
Execute the tests in e-testing-plan.md against the post-upgrade repo state and
confirm the 183→185 upgrade landed merge-free, migrated `cwf_method` to
`read-tree`, pinned the correct version, and left the tree validate-clean with
tooling functional.

## Execution Checklist
- [x] Read e-testing-plan.md and d-implementation-plan.md thoroughly
- [x] Verify test environment ready
- [x] Execute test cases sequentially
- [x] Record pass/fail for each test
- [x] Document failures with reproduction steps (none)
- [x] Update status

## Test Results

### Functional Tests

| Test ID | Test Case | Expected | Actual | Status |
|---------|-----------|----------|--------|--------|
| TC-1 | Version pinned (FR1) | `cwf_version=v1.1.185`, `cwf_ref=v1.1.185`, `cwf_sha=6659c1c…` (commit form) | `cwf_version=v1.1.185`, `cwf_ref=v1.1.185`, `cwf_sha=6659c1cca72ef033d92546fcd9d42a0f4d817dd9`; tag object `dd6e934c…` absent | PASS |
| TC-2 | Method migrated (FR3) | `cwf_method=read-tree` | `cwf_method=read-tree` | PASS |
| TC-3 | Merge-free (FR2) | `git log --merges 700baba..HEAD` empty; HEAD single-parent | merge log empty; parent count = 1 | PASS |
| TC-4 | Validate-clean (FR4) | `validate: OK`, exit 0 | `[CWF] validate: OK` (rc=0) | PASS |
| TC-5 | Tooling functional (FR5) | `workflow-manager status 14` exit 0 | rc=0; reports `14 (chore): upgrade-cwf-to-v1-1-185 - 25%` | PASS |
| TC-6 | Settings reviewed (FR6) | pre/post diff captured + widening note | one scoped allowlist entry added, zero hooks (see note) | PASS |

### Non-Functional / Edge Tests

| Test ID | Test Case | Expected | Actual | Status |
|---------|-----------|----------|--------|--------|
| TC-7 | Fail-closed on driver failure (NFR5) | on non-zero exit, `cwf_method` stays `subtree`, nothing committed | Confirmed by inspection — driver exited 0, abort path not exercised; the cmd_update ordering (laydown/artefacts/perms → only then write `cwf_method=read-tree`) was independently confirmed in the f-phase security review | PASS (by inspection) |
| TC-8 | detect-merges advisory | flag 4 pre-existing subtree-install merges | advisory printed `4 total, 4 from old CWF subtree installs, 0 elsewhere`; independently confirmed merge SHAs `103537c`/`28cfb50`/`a2c7635`/`75e3ae4`, all in base history before `700baba` | PASS (advisory) |
| TC-9 | Regression — dcfh untouched | no `*.go` changes | `git diff --name-only 700baba..HEAD -- '*.go'` empty; working tree has no `*.go` changes | PASS |

### TC-6 widening review note (FR6 / AC5)
The settings-merge added **exactly one** Bash allowlist entry and **zero** hook
entries:
```
+ "Bash(.cwf/scripts/command-helpers/cwf-detect-merges:*)"
```
This is a **scoped** entry permitting the single new 185 helper `cwf-detect-merges`
(read-only, counts-only merge advisory). It is **not** a broad widening: no
wildcard broadening of an existing rule, no new hook in the hooks list, no env-var
grant. Reviewed and accepted.

## Test Failures
None. All 9 test cases pass (TC-7 by abort-path inspection, TC-8 advisory).

## Coverage Report
- 6/6 acceptance criteria (AC1–AC5 via TC-1..TC-6) covered by executable
  assertions, all PASS.
- TC-7 (fail-closed) verified by inspection of the cmd_update ordering, not by
  deliberately breaking the run (per plan).
- TC-8 advisory recorded (not a task failure).
- TC-9 regression-free by construction (no Go change).

## Status
**Status**: Finished
**Next Action**: /cwf-rollout 14
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Lessons Learned
All 9 test cases passed without rework. TC-8's advisory (4 pre-existing subtree
merges) was independently cross-checked against `git log --merges` — all four are in
base history before `700baba`, so none were added by this task. See
j-retrospective.md for the consolidated analysis.

## Security Review

**State**: no findings

The on-disk source matches the diff exactly. I have everything I need to render a verdict.

## Security Review — testing-exec changeset (Task 14: CWF v1.1.183 → v1.1.185 upgrade)

I read the full changeset (`/tmp/-home-matt-repo-dircachefilehash-task-14/security-review-changeset-testing-exec.out`) and cross-checked the load-bearing claims against the actual on-disk files (`.cwf/scripts/command-helpers/cwf-detect-merges` and `.cwf/scripts/cwf-manage:480-569`) rather than trusting the diff narrative.

### Nature of the diff

This is a vendored CWF subtree upgrade (v1.1.183 → v1.1.185) plus this task's own process documents. There is **no** project source code (`pkg/`, `cmd/`, `go.*`) in the changeset. The executable surface that is genuinely new or changed is small and fully visible:

1. `.cwf/scripts/command-helpers/cwf-detect-merges` — new read-only Perl helper (the substantive new code).
2. `.cwf/scripts/cwf-manage` — wires the helper into `cmd_update`/`check-merges` and adds the `subtree`→`read-tree` method migration.
3. `.cwf/lib/CWF/Backlog.pm` — a new CHANGELOG-005 stale-brand warning check.
4. `.claude/settings.json` — one allowlist entry for the new helper.
5. `.cwf/security/script-hashes.json`, `.cwf/version` — integrity metadata (out of scope here per the security-review doc boundary; deterministically covered by `cwf-manage validate`).
6. `implementation-guide/**` markdown — deleted v1.1.183 task docs, added v1.1.185 task docs; process artefacts, not executable.

### Threat categories (a)–(e)

**(a) Bash injection / unsafe command construction.** `cwf-detect-merges` spawns git exclusively via `capture_git`, which is true list-form fork/exec — `open(my $fh, '-|')` then `exec('git', @args)` (lines 41–54) — no shell, so refs/args are never metacharacter-exposed. The only non-literal git arguments are commit SHAs taken from git's own `%P` porcelain (`$p[1]`, line 62), passed list-form. In `cwf-manage`, `run_detect_merges` invokes `system($helper, $git_root)` (list form, two-arg) and the laydown delegation uses `system('bash', $installer)` (line 528) — both shell-free. No single-string `system`/backtick interpolation of partly-controlled strings was introduced. Clean.

**(b) Git/user output without `-z` / validation.** The merge enumeration uses `-z` with `%x1f` field separators and splits on `/\0/` then `/\x1f/` (lines 80–93) — exactly the NUL-separated convention in `docs/conventions/git-path-output.md`. `%P` parents are split on `/\s+/`, safe because git hex SHAs are whitespace-free. `second_parent_is_squash` reads a single `git show -s --format=%s` record (not a file list) and matches an anchored regex — no newline-splitting hazard. Backlog.pm's `index($_, $STALE_CHANGELOG_BRAND) >= 0` uses a constant needle over the intro array — no regex injection, no untrusted-string-as-pattern. Clean.

**(c) Prompt injection via user-supplied strings.** None of the changed executable code feeds strings into LLM context. `cwf-detect-merges` deliberately prints **counts only** and fixed advisory text, never raw commit subjects (header lines 21–22, output lines 106–111) — attacker-controlled commit messages cannot leak into downstream context or terminal escapes. The added markdown is CWF workflow process docs, not SKILL templates with `{arguments}` substitution. No new surface.

**(d) Unsafe environment-variable handling.** `cmd_update` adds `read-tree` to the accepted-method whitelist (line 504, guarded by `die_msg` on any other value) and derives `$laydown_method` from the recorded `$method` (sourced from `.cwf/version`, an integrity-tracked file) — constrained to the three literals `subtree`/`copy`/`read-tree`, not free text. It flows into `CWF_METHOD` in a scoped `local %ENV` block (lines 522–527); `CWF_SOURCE` remains `file://$clone_dir` as before. The migration is **fail-closed**: `$v{cwf_method}` is only rewritten to `read-tree` after laydown/artefacts/perms all succeed (line 556, reached only past the `die_msg` gates at 532–534 and `apply_exact_perms_or_die` at 545), so a mid-update failure leaves `cwf_method=subtree` — correct fail-safe direction. No env var feeds `chmod`/`rm`/`open` on an unvalidated path. Clean.

**(e) Pattern-based risks (forward-looking notes, not defects).**

1. `run_detect_merges` builds `my $helper = "$git_root/.cwf/scripts/command-helpers/cwf-detect-merges"` and runs `system($helper, $git_root)`. **Safe here because `$git_root` is the CWF-resolved repository root** (internal `cwf-manage` discovery, not user free-text), and the list-form `system` keeps it shell-injection-safe regardless. *Audit future uses where `$git_root` could become partly user-controlled:* list-form still blocks shell injection, but a path-traversal-style `$git_root` could select an unexpected executable to run. The protecting invariant is "`$git_root` is the trusted resolved repo root" — it must hold at any new callsite.

2. `cwf-detect-merges`'s `chdir $root` (line 71) trusts `$root` from `@ARGV`. **Safe here because the only caller passes the trusted `$git_root`, and a failed `chdir` fails open** (prints a skip line, returns 0 — lines 71–75). *Audit future direct-CLI uses where `$root` could be attacker-influenced;* the blast radius is bounded — the helper is read-only and counts-only, so the worst case is an inaccurate advisory count, never state mutation.

### Other observations (non-findings)

- The `.claude/settings.json` change adds a single scoped allowlist entry `Bash(.cwf/scripts/command-helpers/cwf-detect-merges:*)` for the new read-only helper — no wildcard broadening of an existing rule, no new hook entry, no env-var grant. Consistent with the existing command-helper allowlist convention.
- `script-hashes.json` / `.cwf/version` are integrity metadata, explicitly the domain of `cwf-manage validate` per the doc's boundary note — not duplicated here.
- No secrets, credentials, or tokens introduced anywhere. No project auth/authz logic touched.

### Conclusion

The executable changes follow the established list-form-spawn and `-z`/`%x1f` porcelain conventions, are counts-only/read-only, and the `subtree`→`read-tree` migration is fail-closed. The on-disk source matches the diff. No actionable security concerns. The two (e) notes are forward-looking framing for future maintainers, not defects in this changeset.

```cwf-review
state: no findings
summary: CWF v1.1.183→v1.1.185 vendored upgrade plus task docs; new cwf-detect-merges helper uses list-form git spawn + NUL/%x1f porcelain with counts-only output, fail-closed read-tree method migration, single scoped allowlist entry, no shell injection, no env-to-command flow, no secrets, no project code touched. Two (e) pattern notes are forward-looking only.
```
