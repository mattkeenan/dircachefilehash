# Backlog

Open work for dircachefilehash. Managed with the CWF `backlog-manager` helper.

## Task: Phase 1b-2: Fix primitive + dcfhfix restructure

### Task-Type: feature
### Priority: Medium

Phase 1b landed the Filter primitive and moved the find DSL behind
`Repo.Filter`. The symmetric Fix primitive (`Repo.Fix(FixRequest) → FixResult`)
and the dcfhfix migration were deferred.

Why deferred: dcfhfix is analogous to `fsck`. The rest of dcfh trusts the
on-disk format — parse errors are fatal — whereas dcfhfix explicitly assumes
an index may be corrupt and must still make forward progress (bounds-checked
field reads via `pkg/format.SafeEntry`, entry-by-entry validation via
`ValidatedEntry`, backup-stack rollback). Reshaping that around a batch-mode
`FixRequest{commands}` primitive is a different workflow from the Diff/
Apply/Filter path and warrants its own design pass.

Why medium, not high: given dcfh's scan speed, the pragmatic recovery in
most situations is `rm -rf .dcfh && dcfh init . && dcfh update` (or `dcfh
status` for a dry view). That short-circuits the need for targeted repair
in the common case. `dcfhfix` remains useful for forensic/audit scenarios
where the index is the evidence and reinitialising would destroy state,
but that's a narrower use case than daily operation.

Scope when picked up:
- Define `FixRequest{IndexSelector, Commands, DryRun, Backup}` and
  `FixResult{RepairsApplied, EntriesDiscarded, BackupID}` as batch-mode
  encodings of the current interactive subcommands (header show/edit,
  entry show/edit/append/remove/resort, fixes list/pop/discard/clear).
- Move the fsck-shape helpers (`ValidatedEntry`,
  `processEntriesWithWorkflow`, backup stack) from cmd/dcfhfix into
  pkg/ so the Repo implementation can drive them. (The bounds-checked
  accessor is already done — Task 3.1 moved it to `pkg/format.SafeEntry`.)
- Add `Repo.Fix` to the interface; `localRepo.Fix` delegates to the
  moved helpers. Interactive dcfhfix subcommands translate CLI input
  into one or more `FixRequest` batches.
- Two operating modes, mirroring the historical evolution of `fsck`:
  per-entry interactive (ask once per change, the legacy default) and
  auto-fix (apply all safe repairs in one shot). Auto-fix is safe by
  default *because* the entry below makes `dcfhfix` write to a new
  index file rather than overwriting — there is no in-place mutation
  to roll back, so "auto" doesn't carry the historical risk it had
  with on-disk filesystems.
- Land the v0.7 recovery write path here too. Rebuilding `main.idx`
  from whatever combination of `main.idx` / `cache.idx` /
  timestamped-cache files is readable is conceptually a Fix batch
  with a multi-source `IndexSelector`. The orchestration shell in
  `pkg/recovery.go` was deleted in the ARCHITECTURE-IMPROVEMENTS
  item #4 cleanup; the validation/processor and snapshot helpers
  that survived are reusable from this work.
- Preserve existing dcfhfix tests (main_test.go, options_test.go).

Dependency: none blocking — Phase 2 (audit mode) does not need Fix since
the remote host holds no dcfh state to repair.

## Task: Add comprehensive integration tests for edge cases

### Task-Type: chore
### Priority: High

Edge-case coverage (mid-scan interrupts, partial writes, concurrent modification) is uneven across packages. `pkg/shutdown_test.go` covers context cancellation; partial writes and concurrent modification during scan are not exercised.

## Task: Implement dry-run mode for `dcfh update`

### Task-Type: feature
### Priority: Medium

The global `--dry-run` flag is honoured by `snapshot` and `dupes` but `cmd/dcfh/update.go` has no dry-run plumbing. Wiring `update` to preview effects without writing would close the consistency gap.

## Task: Add progress reporting for long-running operations

### Task-Type: feature
### Priority: Medium

Long scans/updates currently appear silent — no spinner, no entries-processed counter, no ETA. Progress reporting would improve UX on large trees.

## Task: Handle edge cases in ignore pattern matching

### Task-Type: bugfix
### Priority: Medium

`pkg/ignore_test.go` covers only basic transitions, deindex, and suppress; no negation (`!`), directory-only (trailing `/`), or symlink-target coverage. Likely fixes follow once the gaps are exercised.

## Task: Implement coloured output for better readability

### Task-Type: feature
### Priority: Medium

Status output (modified/added/deleted) and diagnostics would benefit from terminal colour, gated on TTY detection. No `isatty`/`IsTerminal` check exists today.

## Task: Add `dcfh config validate` subcommand

### Task-Type: feature
### Priority: Medium

`cmd/dcfh/config.go` exposes `get`/`set`/`--list` but no `validate`. The validators in `pkg/config.go` exist; surfacing them as a subcommand lets users check `.dcfh/config` without running a full operation.

## Task: Clean up stale scan temp files at startup

### Task-Type: feature
### Priority: Low

`scan-*.idx` files left behind by SIGINT or unexpected exit are not swept on startup — `pkg/recovery.go` only cleans up after a recovery run, and `pkg/filter_run.go` globs scan files only inside filter operations. A startup sweep of stale temp files is overdue.

## Task: Add metrics collection for performance monitoring

### Task-Type: feature
### Priority: Low

No prometheus/expvar/metrics hooks exist anywhere. Optional metrics (timings, throughput, lock contention) would aid both library consumers and our own benchmarking work.

## Task: Test on additional Unix variants

### Task-Type: chore
### Priority: Low

`.github/workflows/ci.yml` runs only on `ubuntu-latest`. Portability claims need exercising on at least one BSD and macOS.

## Task: Test with various Go versions

### Task-Type: chore
### Priority: Low

CI now resolves the toolchain from `go-version-file: go.mod` (single version, no matrix) after Task 19. A Go-version matrix across supported versions would catch toolchain-specific issues earlier. See also the consolidated CI-enhancements item below.

## Task: Fix stale 'see CHANGELOG' reference in pkg/ignore.go

### Task-Type: chore
### Priority: Low
### Identified in: Task 1 retrospective (j-retrospective.md)

`pkg/ignore.go:106` prints "dcfh now uses gitignore syntax — see CHANGELOG." The
referenced gitignore-syntax note is in neither the current `CHANGELOG.md` nor the
archived `docs/changelog-old.md` (confirmed by grep during Task 1). Pre-existing
staleness — left untouched in Task 1 to keep that docs-conformance chore free of any
Go change. Either repoint the message at concrete docs or drop the "see CHANGELOG"
clause.

## Task: Configure security.review.test-paths to exclude upstream-shipped CWF directories

### Task-Type: chore
### Priority: Low
### Status: Follow-up from Task 5
### Identified in: Task 5 retrospective (j-retrospective.md)

The exec-phase security-review changeset helper (T168 in CWF v1.1.168) caps the review at 500 production-weighted lines, where "production" means added+deleted lines outside any `security.review.test-paths` glob in `cwf-project.json`. This repo currently declares no `test-paths` patterns, so every line counts as production — including upstream-shipped CWF content laid down by `cwf-manage update`. Task 5's f-exec and g-exec both recorded `error: cap exceeded: 1632 production lines > 500` for this reason, even though the actually-new-in-task surface was tiny (settings.json hook entries + .cwf/version + workflow MD). **Task 9 (v1.1.169→v1.1.177 upgrade) is a second data point** — f-exec `1759` / g-exec `1906` production lines, an even-purer case where the *only* consumer-authored surface was `.cwf/version` + the task's own workflow MD.

**Caveat surfaced by Task 9 (weigh before implementing):** the (renamed) `security.review.max-lines-exclude-paths` only discounts paths from the *cap count* — the **full changeset is always emitted to the subagent regardless of the cap**. So adding `.cwf/**` et al. to the exclude list would, for a *pure* CWF-upgrade task, flip its disposition from "cap exceeded → error → subagent skipped" to "cap not reached → subagent **invoked on the entire vendored delta**" (reviewing all of upstream's source change). For a pure upgrade the current error/skip outcome — backed by the deterministic `cwf-manage validate` (sha256 + perms) integrity gate — may be preferable to spending a subagent pass on upstream-vetted code. The config fix is best-aimed at *mixed* tasks where CWF laydown is incidental to consumer-authored work; the two cases deserve separate consideration.

**Task 14 (v1.1.177→v1.1.183 upgrade) empirically confirms the caveat.** This repo *already* lists `implementation-guide/**` in `max-lines-exclude-paths`, yet both exec phases still tripped the cap (1302 / 1305 production lines) on the vendored `.cwf/` delta alone. The user chose to raise the cap once per phase (`--max-lines=5000`, no persistent config) and let the `cwf-security-reviewer-changeset` subagent review the full delta — outcome `no findings` both phases, at ~95–100k subagent tokens each. So the Task-9 prediction is now observed fact: excluding `.cwf/**` would route every CWF self-upgrade through a full subagent pass over upstream-vetted code at real token cost. Decision still pending: for *pure* upgrades the cheap deterministic `cwf-manage validate` gate may be preferable; reserve the exclude for *mixed* tasks.

Scope when picked up:
- Add `security.review.test-paths` to `implementation-guide/cwf-project.json` covering upstream-shipped CWF directories (candidate patterns: `.cwf/**`, `.cwf-skills/**`, `.cwf-rules/**`, `.cwf-agents/**`, `.claude/skills/**`, `.claude/agents/**`).
- Verify the cap measures only consumer-authored code by re-running `security-review-changeset --max-lines=500` against a CWF-upgrade-shaped changeset.
- Document the conscious decision that consumer overrides under `.claude/` (if any) are reviewed only when they sit *outside* these prefixes.

Rationale: the cap is a useful gate for consumer-authored code, but tripping it on upstream-vetted laydown content is noise that masks signal. The fix is configuration, not code.

Identified in: Task 5 retrospective.

## Task: Remove stale tracked root debris cache.idx and cache-2.idx

### Task-Type: chore
### Priority: Low
### Status: Follow-up from Task 3
### Identified in: Task 3 retrospective (j-retrospective.md)

Two stale index files are tracked at the repository root: `./cache.idx` and `./cache-2.idx`
(13 MB each, dated 2025-07). They are an unloadable legacy `cache.idx` and a `convert-index-v1-to-v2`
conversion-tool artefact — both superseded by the byte-exact goldens under `pkg/format/testdata/`.
They are not referenced by any test or tool and bloat every checkout.

Scope: `git rm cache.idx cache-2.idx` and confirm no code path references them (grep), then verify the
full suite still passes.

Identified in: Task 3 retrospective (j-retrospective.md); first flagged in subtask 3.3's e-testing-plan
robustness review and deferred.

## Task: dcfhfix: .pre-fix-* sibling retention/GC

### Task-Type: feature
### Priority: Low
### Status: Follow-up from Task 8
### Identified in: Task 8 retrospective (j-retrospective.md)

`dcfhfix` now preserves the pre-repair index at a `<index>.pre-fix-<UTC>`
sibling on every default-path repair (task 8). There is no automatic pruning,
so repeated repairs against the same index accumulate `.pre-fix-*` files next to
it. Operators currently prune them by hand.

Scope: add an optional cleanup/retention subcommand mirroring the existing
`fixes` stack (e.g. keep-newest-N, or age-based), so preserved siblings can be
garbage-collected without manual `rm`. Low priority — only worth doing if the
accumulation becomes a real annoyance in practice.

## Task: Fault-injection seam for dcfhfix preserveOriginal

### Task-Type: chore
### Priority: Low
### Status: Follow-up from Task 8
### Identified in: Task 8 retrospective (j-retrospective.md)

`preserveOriginal` in `cmd/dcfhfix/promote.go` is 75.8% covered; the residual
uncovered lines are the `Sync`/`Close`/`Lstat`-error defensive branches, which
need a fault-injection seam to exercise. The copy-error branch is already
covered.

Scope: introduce a small injectable failure seam (interface or test hook) so the
Sync/Close/Lstat error paths can be driven from a unit test, lifting
`preserveOriginal` to 100%. Low priority — these are defensive branches with no
known live trigger.

## Task: Wide-rune (CJK) column width in interactive-tree viewer

### Task-Type: bugfix
### Priority: Low
### Identified in: task 11

The tcell viewer (cmd/dcfh/internal/tui) treats every rune as one display column in drawText/truncation. Wide runes (CJK, some emoji) therefore under-count width and can misalign or overlap the size column / stats-pane divider. Cosmetic only — the sanitiser already guarantees no escape bytes reach the terminal. Fix: account for rune width (tcell vendors rivo/uniseg) in drawText and the rune-aware truncation.

## Task: CI enhancements deferred in Task 19 (coverage, caching, actionlint)

### Task-Type: chore
### Priority: Low
### Identified in: Task 19 retrospective (j-retrospective.md)

Task 19 scoped strictly to "make the existing two CI jobs pass on the cleaned
v0.7 tree" and deliberately deferred all CI feature additions. Candidate
enhancements, none load-bearing for correctness:
- **Coverage upload** — emit `go test -coverprofile` and publish (e.g. Codecov or
  a job-summary artefact).
- **Module/build caching** — `actions/setup-go@v5` cache or an explicit
  `actions/cache` keyed on `go.sum` to cut CI wall-clock.
- **Local `actionlint`** — add to the toolchain (and optionally the pre-commit
  gate) so GitHub-Actions syntax errors (like the `golangci-lint-action` `version`
  input rejecting a `v2.x` wildcard, caught by plan review in Task 19) surface
  before a PR run rather than on GitHub.

Related: the Go-version-matrix item above ("Test with various Go versions") is the
matrix half of the same theme; fold together if picked up. Scope is
`.github/workflows/` + toolchain only — no Go change.

## Task: Reconcile ValidateIndexHeader checksum with production writer

### Task-Type: bugfix
### Priority: Low
### Identified in: Task 23 retrospective (j-retrospective.md), deviation D1

The repair/dcfhfind path validator validateHeaderChecksum (via ValidateIndexHeader) diverges from the production verifyHeaderChecksum: a normally-promoted main.idx fails ValidateIndexHeader even though it loads cleanly via loadIndexFromFileWithTracking. Reconcile the two so the repair and dcfhfind paths can validate a normally-written index. Surfaced when Task 23 had to switch its "loads clean" test oracle off ValidateIndexHeader onto the production loader.

## Task: Retire stale skipped pkg/shutdown_test.go

### Task-Type: chore
### Priority: Low
### Identified in: Task 23 retrospective (j-retrospective.md), Risk-3 deferral

pkg/shutdown_test.go is skipped pending pipeline migration and is structurally stale against the v0.7 channel pipeline. Task 23 wrote a fresh focused cancellation test (TC-9 in scan_edge_cases_test.go) rather than un-skipping it, so the v0.7 mid-scan-cancel invariant is now covered. Remove the obsolete skipped test.

## Task: Fix non-conformant Task 14 (round 2) CHANGELOG heading

### Task-Type: bugfix
### Priority: Low
### Identified in: Task 23 retrospective (j-retrospective.md), backlog-manager validate --all

The CHANGELOG entry "## Task 14 (round 2): Upgrade CWF subtree to v1.1.185" violates the heading grammar ^## Task N: (Backlog.pm parser regex requires a colon immediately after the digits). The (round 2) parenthetical means the heading is not recognised as an entry boundary, so the preceding entry Notable subsection bleeds in and backlog-manager validate --all reports CHANGELOG-003 (subsections out of order) against that entry Changes line. Pre-existing on HEAD; the lighter cwf-manage validate used by the checkpoint hook does not catch it. Two entries currently share Task number 14 (the v1.1.183 round and this v1.1.185 round 2), so the fix needs a disambiguation the grammar accepts.

## Task: Audit recover()-and-swallow sites for latently-dead logic

### Task-Type: discovery
### Priority: Low
### Status: Follow-up from Task 24
### Identified in: Task 24 retrospective (j-retrospective.md)

Task 24 found that ValidateEntry wrapped its body in a deferred recover() that discards the panic value (`_ = r`), silently converting validateLayout's always-panic into an always-nil pass — the validator had been dead for an unknown period. Other recover-and-swallow sites may hide similarly dead logic. Scope: grep for `recover()` blocks that drop the recovered value without re-surfacing it as an error/log, audit whether the protected body can panic on normal input, and add a test (or remove the dead guard) where one does. Low priority — investigative.

## Task: Reject "## Task N (...):" CHANGELOG headings at write time

### Task-Type: chore
### Priority: Low
### Identified in: Task 25 retrospective (j-retrospective.md)

The CHANGELOG entry regex (CWF/Backlog.pm) requires the colon immediately after the task digits; a parenthetical between the number and the colon (e.g. "## Task 14 (round 2):") silently fails to parse and is absorbed into the preceding entry, surfacing only as a downstream CHANGELOG-003 one entry later. Task 25 fixed one instance by hand. Add a backlog-manager check (or lint rule) that rejects "## Task N (...):" headings at write time so the trap cannot recur. Low priority — the validator already catches the downstream symptom; this would catch the cause at the source and give a clearer error.
