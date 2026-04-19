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
`FixRequest{commands}` primitive is a different workflow from the Survey/
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

## Bug: `dcfh dupes` reports all-zero SHA1 hash

`dcfh dupes --json` groups files under `"hash": "0000000000000000000000000000000000000000"`
(40 hex zeros, i.e. a zero SHA1) even when the index stores correct SHA256 hashes.

Reproduction:
```sh
mkdir /tmp/smoke && cd /tmp/smoke
echo hello > a.txt && echo world > b.txt && echo hello > c.txt
dcfh init . && dcfh update
dcfh dupes --json
# → one group with all 3 files under hash=000…000
dcfhfind main --printf '%H  %p\n'
# → correct SHA256 hashes (a.txt and c.txt match, b.txt differs)
```

The main index is fine (`dcfhfind` reads correct hashes). The bug is in
`FindDuplicatesUnified` / the dupes grouping path: it emits a zero-length
SHA1 placeholder instead of reading the stored hash.

Effects:
- All non-identical files are reported as duplicates of each other.
- `dupes` output (human, JSON, fdupes) is unusable when the repo uses
  anything other than SHA1.

Pre-existing — confirmed on unmodified `main` via `git stash` during the
Phase 1 Repo-abstraction refactor (which did not touch `FindDuplicatesUnified`).
