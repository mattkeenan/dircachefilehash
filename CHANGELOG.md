# Changelog

Per-task record of changes to dircachefilehash, maintained through the CWF workflow. Entries are added as tasks complete.

Pre-CWF history — organised by release version under Keep a Changelog / Semantic Versioning, plus the earlier rejected-design log — is preserved in [`docs/changelog-old.md`](docs/changelog-old.md).

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
