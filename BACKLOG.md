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

## Task: Validate atomic index replacement under failure conditions

### Task-Type: chore
### Priority: High

Atomicity of the temp-write + rename path needs explicit failure-injection coverage (crash mid-write, rename failure, full disk). No `os.Rename` fault-injection tests exist today.

## Task: Update API documentation with current architecture

### Task-Type: chore
### Priority: High

`pkg/doc.go` and exported-symbol godoc pre-date the layered/pipeline architecture and the scan-index workflow; library consumers see stale guidance.

## Task: Add usage examples for library consumers

### Task-Type: chore
### Priority: High

`pkg/` has no `example_*_test.go` files and no `examples/` directory; consumers must read source to figure out the entry points.

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

CI pins `go-version: '1.21'` with no matrix. A Go-version matrix across supported versions would catch toolchain-specific issues earlier.

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

## Task: Reconcile RelativePath vs calculatePathLength 8-byte pathStart discrepancy

### Task-Type: bugfix
### Priority: Medium
### Identified in: task 10

RelativePath() computes pathStart as entryStart+Sizeof(Entry); calculatePathLength() uses &be.Path[0] (= Sizeof-8). They disagree by 8 bytes over the same entry memory. Task 10 preserved both byte-for-byte (checkptr-clean only). Audit which offset is canonical against the on-disk writer (EntrySerialiser) and fix the wrong one; add a test pinning path-start.

## Task: Wide-rune (CJK) column width in interactive-tree viewer

### Task-Type: bugfix
### Priority: Low
### Identified in: task 11

The tcell viewer (cmd/dcfh/internal/tui) treats every rune as one display column in drawText/truncation. Wide runes (CJK, some emoji) therefore under-count width and can misalign or overlap the size column / stats-pane divider. Cosmetic only — the sanitiser already guarantees no escape bytes reach the terminal. Fix: account for rune width (tcell vendors rivo/uniseg) in drawText and the rune-aware truncation.

## Task: Add test-file globs to security.review.max-lines-exclude-paths

### Task-Type: chore
### Priority: Medium
### Identified in: Task 12 retrospective (j-retrospective.md)

Both Task 12 exec-phase security reviews (f and g) exited 2 (cap exceeded) and recorded State: error, so the cwf-security-reviewer-changeset subagent never ran semantically. Cause: security.review.max-lines-exclude-paths in implementation-guide/cwf-project.json lists implementation-guide/** but NOT Go test files, so **/*_test.go lines count as production. Task 12 added 411 test lines vs ~154 production lines, pushing the production-weighted count to 565/577 > 500. Fix: add **/*_test.go (and any other test conventions) to security.review.max-lines-exclude-paths so test files are reviewed but discounted from the cap, letting the semantic review run on the production diff for test-heavy tasks. Config change only.
