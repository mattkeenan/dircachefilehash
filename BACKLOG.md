# Backlog

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
