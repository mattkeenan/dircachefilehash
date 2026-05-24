# Changelog

Per-task record of changes to dircachefilehash, maintained through the CWF workflow. Entries are added as tasks complete.

Pre-CWF history — organised by release version under Keep a Changelog / Semantic Versioning, plus the earlier rejected-design log — is preserved in [`docs/changelog-old.md`](docs/changelog-old.md).

## Task 2: Adopt full Go pre-commit hook and security review

### Status: Complete

### Impact: The repo ran without a security-focused static-analysis gate. `gosec` is now wired into `.golangci.yml` (so it fires on every `golangci-lint` run, including the staged `--new` pre-commit path) and contributes zero findings to a clean tree while staying active for new code. Adopting it immediately surfaced one genuine latent bug (inode/device truncation in `dcfh dupes`, now backlogged Very High).

### Changes
- Enabled `gosec` in `.golangci.yml`. Architectural/intentional rules excluded with documented rationale (G103 unsafe/mmap, G304 file-scanner paths, G401/G505 git-compatible SHA-1) plus G115 deferred to a backlogged bug fix. Test-only false positives scoped via `{linters:[gosec], path: _test\.go}`.
- Suppressed 26 production false positives with per-line `//nolint:gosec // Gxxx: rationale` (perms on non-secret `.dcfh/` files, `DirEntry.Name()` base paths, opt-in localhost pprof, fixed-`"ssh"`-binary subprocess) — never a blanket disable; perms/subprocess rules stay active for new code.
- Lifted golangci-lint's issue-display caps (`max-same-issues: 0`, `max-issues-per-linter: 0`) so the security gate never silently hides a duplicate finding.
- Bumped the Go toolchain to `go1.26.3` (go.mod `toolchain` directive) to clear `GO-2026-4971`, which the hook's govulncheck step correctly blocked on.
- Documented the dual security-review process (gosec floor + CWF `cwf-security-reviewer-changeset`) in `CLAUDE.md`.

### Notable
- Measure security linters through the enforcement path, not standalone: setting `gosec.excludes` activates gosec's full ruleset, and golangci-lint's default `max-same-issues: 3` had hidden over half the findings during planning (true count was 26 sites, not ~12).
- Read the code, don't trust the verdict: per-finding review found exactly one real bug among ~230 raw findings and kept suppression precise rather than blanket.
- The pre-commit gate proved itself — govulncheck blocked a freshly-published CVE on unchanged code; resolved via toolchain bump with no `--no-verify`.
- Follow-ups backlogged: Very High (inode/device truncation fix + re-enable G115), High (deliberate suppression-review pass), Low (clear 3 pre-existing non-gosec lint failures).

## Task 1: Conform BACKLOG and CHANGELOG to CWF format

### Status: Complete

### Impact: The 15 existing BACKLOG entries were invisible to the `backlog-manager` tooling — they used the legacy `## Entry:` heading, so `list` returned nothing and `validate` passed only because it recognised zero entries. After conversion all entries are tool-visible and the heading-tree contract is enforced on every change.

### Changes
- Converted `BACKLOG.md` to the CWF heading-tree schema: `## Entry:` → `## Task:`, added a `### Task-Type:` to each of the 15 entries (feature ×8, chore ×6, bugfix ×1), and replaced the self-documenting template header with a one-line intro. All titles and bodies preserved verbatim.
- Archived the version-based `CHANGELOG.md` (Keep a Changelog / SemVer, plus the `## Rejected` design log) to `docs/changelog-old.md` byte-identically, and started this fresh CWF by-task changelog.

### Notable
- The `list` count, not the `validate` exit code, is the real conformance oracle for this kind of migration — a clean `validate` on zero recognised entries is a false positive.
- Archive-then-recreate at the same path defeats git's `R100` rename detection; archive integrity was verified by blob-hash equality instead.
- `pkg/ignore.go:106`'s stale "see CHANGELOG" reference was left untouched (out of scope for a docs chore) and logged as a Low-priority follow-up.
