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

## Task: dcfhfix: default to non-destructive fix-to-new-file

### Task-Type: feature
### Priority: Medium

`dcfhfix` should default to writing repairs to a *new* index file (e.g.
`<name>.fixed.idx` or similar), moving the original out of the way
rather than mutating it in place. In-place editing should require an
explicit `--force --edit-in-place` (or equivalent) — a "break glass in
case of nuclear war" flag, not the default.

Why: dcfhfix operates on potentially-corrupted state where the only
remaining evidence is the index itself. The current in-place default
makes destructive edits the path of least resistance; a fix-to-new-file
default preserves the original automatically and matches the safety
posture of comparable tools (`fsck -n`, `git fsck` write nothing).

Scope when picked up:
- Make non-destructive output the default for `dcfhfix scan`, `entry`,
  `header` write paths.
- Original index moved aside (`<name>.idx.pre-fix` or timestamped) so a
  user can compare/roll back without thinking about it.
- `--edit-in-place` (gated by `--force` or its own clear flag) for the
  rare case where users explicitly want the old behaviour.
- Big warnings on the in-place path; clean-flag header check still
  applies as the in-use lockout (separate concern).
- Update help text and DESIGN docs to reflect the new default.

Dependency: aligns naturally with the Fix-primitive restructure above
(non-destructive output is a property of `FixRequest` semantics).

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

## Task: Clear pre-existing full-tree golangci-lint failures (cyclop, unparam)

### Task-Type: chore
### Priority: Low
### Identified in: Task 2

A full golangci-lint run ./... is red (masked by the hooks --new staged mode) on three pre-existing non-gosec issues: cmd/dcfhfind/main.go:455 parseTestToken cyclop complexity 21 over 20; pkg/filter_run.go:75 resolveOneSelector cyclop complexity 21 over 20; pkg/binary_entry_scan_test.go:200 createTestEntry unparam unused parameter t. Unrelated to the Task 2 gosec work. Fix or scope these so full-tree and CI lint can be green (prerequisite for using the hooks --all mode in CI).

## Task: Configure security.review.test-paths to exclude upstream-shipped CWF directories

### Task-Type: chore
### Priority: Low
### Status: Follow-up from Task 5
### Identified in: Task 5 retrospective (j-retrospective.md)

The exec-phase security-review changeset helper (T168 in CWF v1.1.168) caps the review at 500 production-weighted lines, where "production" means added+deleted lines outside any `security.review.test-paths` glob in `cwf-project.json`. This repo currently declares no `test-paths` patterns, so every line counts as production — including upstream-shipped CWF content laid down by `cwf-manage update`. Task 5's f-exec and g-exec both recorded `error: cap exceeded: 1632 production lines > 500` for this reason, even though the actually-new-in-task surface was tiny (settings.json hook entries + .cwf/version + workflow MD).

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
