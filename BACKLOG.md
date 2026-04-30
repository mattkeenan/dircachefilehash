# Backlog

## Phase 1b-2: Fix primitive + dcfhfix restructure (medium priority)

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

## dcfhfix: default to non-destructive fix-to-new-file (medium priority)

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

