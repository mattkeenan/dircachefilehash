# Backlog

Append-only list of open work. New entries go at the bottom.

Each entry follows this template:

```markdown
## Entry: <title>

### Priority: <High|Medium|Low>

<one-line rationale>

### Scope (optional)
- bullet

### Notes (optional)
- bullet
```

Useful greps:

```bash
grep '^## Entry: ' BACKLOG.md                              # all entries
grep '^### Priority: High' BACKLOG.md                      # all High items
grep '^#\+ ' BACKLOG.md | grep -B 3 '^### Priority: High'  # title context
```

---

## Entry: Phase 1b-2: Fix primitive + dcfhfix restructure

### Priority: Medium

Phase 1b landed the Filter primitive and moved the find DSL behind
`Repo.Filter`. The symmetric Fix primitive (`Repo.Fix(FixRequest) → FixResult`)
and the dcfhfix migration were deferred.

Why deferred: dcfhfix is analogous to `fsck`. The rest of dcfh trusts the
on-disk format — parse errors are fatal — whereas dcfhfix explicitly assumes
an index may be corrupt and must still make forward progress (bounds-checked
field reads via `SafeEntryAccessor`, entry-by-entry validation via
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
- Move the fsck-shape helpers (`SafeEntryAccessor`, `ValidatedEntry`,
  `processEntriesWithWorkflow`, backup stack) from cmd/dcfhfix into
  pkg/ so the Repo implementation can drive them.
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

## Entry: dcfhfix: default to non-destructive fix-to-new-file

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

## Entry: Add FindEntries helper for path-array lookups

### Priority: High

`Find`, `Insert`, and `ForEach` are already exported on `skiplistWrapper`
(pkg/skiplist.go), but external tools like `dcfhfix` still iterate O(n)
when looking up multiple paths — the documented intentional fallback at
cmd/dcfhfix/main.go:719. A `FindEntries(indexPath, paths []string)`
helper would give callers efficient bulk O(log n) lookup.

## Entry: Add comprehensive integration tests for edge cases

### Priority: High

Edge-case coverage (mid-scan interrupts, partial writes, concurrent modification) is uneven across packages. `pkg/shutdown_test.go` covers context cancellation; partial writes and concurrent modification during scan are not exercised.

## Entry: Validate atomic index replacement under failure conditions

### Priority: High

Atomicity of the temp-write + rename path needs explicit failure-injection coverage (crash mid-write, rename failure, full disk). No `os.Rename` fault-injection tests exist today.

## Entry: Update API documentation with current architecture

### Priority: High

`pkg/doc.go` and exported-symbol godoc pre-date the layered/pipeline architecture and the scan-index workflow; library consumers see stale guidance.

## Entry: Add usage examples for library consumers

### Priority: High

`pkg/` has no `example_*_test.go` files and no `examples/` directory; consumers must read source to figure out the entry points.

## Entry: Implement dry-run mode for `dcfh update`

### Priority: Medium

The global `--dry-run` flag is honoured by `snapshot` and `dupes` but `cmd/dcfh/update.go` has no dry-run plumbing. Wiring `update` to preview effects without writing would close the consistency gap.

## Entry: Add progress reporting for long-running operations

### Priority: Medium

Long scans/updates currently appear silent — no spinner, no entries-processed counter, no ETA. Progress reporting would improve UX on large trees.

## Entry: Handle edge cases in ignore pattern matching

### Priority: Medium

`pkg/ignore_test.go` covers only basic transitions, deindex, and suppress; no negation (`!`), directory-only (trailing `/`), or symlink-target coverage. Likely fixes follow once the gaps are exercised.

## Entry: Implement coloured output for better readability

### Priority: Medium

Status output (modified/added/deleted) and diagnostics would benefit from terminal colour, gated on TTY detection. No `isatty`/`IsTerminal` check exists today.

## Entry: Add `dcfh config validate` subcommand

### Priority: Medium

`cmd/dcfh/config.go` exposes `get`/`set`/`--list` but no `validate`. The validators in `pkg/config.go` exist; surfacing them as a subcommand lets users check `.dcfh/config` without running a full operation.

## Entry: Clean up stale scan temp files at startup

### Priority: Low

`scan-*.idx` files left behind by SIGINT or unexpected exit are not swept on startup — `pkg/recovery.go` only cleans up after a recovery run, and `pkg/filter_run.go` globs scan files only inside filter operations. A startup sweep of stale temp files is overdue.

## Entry: Add metrics collection for performance monitoring

### Priority: Low

No prometheus/expvar/metrics hooks exist anywhere. Optional metrics (timings, throughput, lock contention) would aid both library consumers and our own benchmarking work.

## Entry: Test on additional Unix variants

### Priority: Low

`.github/workflows/ci.yml` runs only on `ubuntu-latest`. Portability claims need exercising on at least one BSD and macOS.

## Entry: Test with various Go versions

### Priority: Low

CI pins `go-version: '1.21'` with no matrix. A Go-version matrix across supported versions would catch toolchain-specific issues earlier.
