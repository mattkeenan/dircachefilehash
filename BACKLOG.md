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

## Entry: Review layer separation and ensure proper abstraction boundaries

### Priority: High

Layered architecture (Foundation → CLI) has accreted over many refactors; periodic review keeps abstractions honest and prevents leaks across boundaries.

## Entry: Export skiplist wrapper functions for low-level package access

### Priority: High

External tools like `dcfhfix` need efficient O(log n) entry lookup, but the current skiplist wrapper is "high-level" inside `pkg/` while remaining "low-level" for external users — they fall back to O(n) iteration.

### Scope
- Export `Find()`, `Insert()`, `ForEach()`, and other core skiplist operations.
- Consider adding a `FindEntries()` helper that takes an index path + paths array.

## Entry: Profile memory usage during large directory scans

### Priority: High

No standing profile data exists for very large trees; needed to validate memory bounds and inform future tuning.

## Entry: Optimise skiplist operations for better cache locality

### Priority: High

Cache locality of skiplist traversal hasn't been measured against alternatives; potential gains in hot paths during merge/Hwang-Lin.

## Entry: Benchmark vectorio vs traditional I/O patterns

### Priority: High

Vectorio is used in the write path on assumption of a win; benchmarks would either confirm the assumption or surface paths where simpler I/O is preferable.

## Entry: Add comprehensive integration tests for edge cases

### Priority: High

Edge-case coverage (mid-scan interrupts, partial writes, concurrent modification) is uneven across packages.

## Entry: Test concurrent scanning with multiple workers

### Priority: High

Worker-count variation (and the interaction with shutdown coordination) is under-tested at the integration level.

## Entry: Validate atomic index replacement under failure conditions

### Priority: High

Atomicity of the temp-write + rename path needs explicit failure-injection coverage (crash mid-write, rename failure, full disk).

## Entry: Update API documentation with current architecture

### Priority: High

Public API docs lag behind the layered/pipeline architecture; library consumers see stale guidance.

## Entry: Add usage examples for library consumers

### Priority: High

`pkg/` has no worked examples; consumers must read source to figure out the entry points.

## Entry: Document performance characteristics and tuning guidelines

### Priority: High

Tuning knobs (hash workers, lock timeout, symlink modes) exist but lack a single place that explains the trade-offs and recommended starting values.

## Entry: Create CONFIG.md documenting all configuration settings

### Priority: High

`.dcfh/config` has multiple sections (filehash, performance, symlink, …) with no consolidated reference.

### Scope
- Document each section with examples and valid values.
- Show command-line flag equivalents.
- Include precedence order (defaults → config file → command line).

## Entry: Add configuration validation on startup

### Priority: Medium

Bad config values are caught lazily at use site; a startup validation pass would surface problems immediately with better context.

## Entry: Implement dry-run mode for update operations

### Priority: Medium

Global `--dry-run` exists for some commands; making it consistent across `update` lets users preview effects before committing.

## Entry: Add progress reporting for long-running operations

### Priority: Medium

Long scans/updates currently appear silent; progress reporting (entries processed, ETA) would improve UX on large trees.

## Entry: Improve error messages with actionable suggestions

### Priority: Medium

Many errors surface low-level wrap chains without telling the user what to do next.

## Entry: Add recovery mechanisms for corrupted index files

### Priority: Medium

Beyond `dcfhfix`, automatic recovery paths could handle a broader class of header/entry corruption inside `dcfh` itself.

## Entry: Handle edge cases in ignore pattern matching

### Priority: Medium

`.dcfhignore` has corner cases (negation interaction, directory-only patterns, symlink targets) that need explicit coverage and likely fixes.

## Entry: Add tab completion support

### Priority: Medium

Bash/zsh completion for commands, subcommands, and indexed paths would speed daily use.

## Entry: Implement coloured output for better readability

### Priority: Medium

Status output (modified/added/deleted) and diagnostics would benefit from terminal colour, gated on TTY detection.

## Entry: Add configuration file validation command

### Priority: Medium

A `dcfh config validate` (or similar) lets users check `.dcfh/config` without running a full operation.

## Entry: Clean up temporary files on interrupted operations

### Priority: Low

Scan/temp index files can be left behind after SIGINT or unexpected exit; a startup sweep of stale temp files is overdue.

## Entry: Add metrics collection for performance monitoring

### Priority: Low

Optional metrics (timings, throughput, lock contention) would aid both library consumers and our own benchmarking work.

## Entry: Implement log rotation for verbose output

### Priority: Low

Verbose runs can produce large logs; rotation keeps them manageable for long-running or scheduled invocations.

## Entry: Fix goreleaser deprecation warning: replace `nfpms.builds` with `nfpms.ids`

### Priority: Low

Current goreleaser config uses the deprecated `builds:` field under `nfpms`.

### Scope
- Either use `ids:` to reference build IDs, or remove the field entirely to include all builds.

## Entry: Test on additional Unix variants

### Priority: Low

Coverage today is Linux-centric; portability claims need exercising on at least one BSD and macOS.

## Entry: Verify memory mapping behaviour on different filesystems

### Priority: Low

mmap semantics vary across filesystems (nfs, tmpfs, overlayfs, zfs); known-good list is only ext4/xfs.

## Entry: Test with various Go versions

### Priority: Low

We pin Go 1.24.3 in development; CI matrix across supported Go versions would catch toolchain-specific issues earlier.
