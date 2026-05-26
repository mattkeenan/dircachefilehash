# Widen Dev/Ino to uint64 and re-enable G115 - Testing Plan
**Task**: 3.3 (bugfix)

## Task Reference
- **Task ID**: internal-3.3
- **Task URL**: N/A (internal task)
- **Parent Task**: 3
- **Branch**: bugfix/3.3-widen-dev-ino-to-uint64-and-re-enable-g115
- **Template Version**: 2.1

## Scope of this plan
Prove the read-old/write-new contract for the v4 widen: **read v2, v3 and v4; write v4 only**. v4 loads
zero-copy and round-trips byte-identically; v2/v3 decode via the heap transcode with every field
correct (and are *routed through* `DecodeHeap`, not cast); unsupported versions reject; ingest no
longer truncates `Dev`/`Ino`; `dcfh dupes` is correct on >32-bit inodes; the heap-backed file never
munmaps; and `golangci-lint run ./...` is clean with G115 active.

## Goal
Every supported version reads into one canonical entry, only v4 is written, and the inode/device
truncation bug is gone — verified by field-level decode assertions, an end-to-end dupes test, and the
re-enabled static gate.

## Test Strategy
### Test Levels
- **Unit** (`pkg/format`): `layoutForVersion` table; `TranscodeLegacyIndex` over crafted legacy
  regions (positive v2+v3, corrupt/empty negatives) — pure, no I/O. Primary level for the new seam.
- **Integration** (`pkg/`): both mmap loaders (`LoadIndexFromFileForValidation` and the tracking
  loader) over crafted v2/v3/v4 fixtures and out-of-range/truncated files — under `-race`.
- **End-to-end** (`pkg/` + `cmd/`): `NewMetaStore`+`runUpdate` writes a v4 index (write-version
  ownership); `dcfh dupes` over a synthesised >2^32-inode pair; `dcfhfix` repair of a v3 file.
- **Static**: gosec G115 via `golangci-lint run ./...` (the enforcement path, per CLAUDE.md).

### Shared fixtures (the read-old test seam)
The existing `pkg/format/roundtrip_test.go` helpers (`layEntry`, `layIndex`) cast through the **current**
`Entry` struct — after the widen they lay **v4** bytes. Add a sibling **`layLegacyEntry`** that lays the
pre-widen layout via `legacyEntry` (uint32 `Dev`/`Ino`), so v2/v3 fixtures are byte-accurate legacy
images. Fixtures are crafted in-memory (no committed binaries); real indices come from `t.TempDir()` via
`NewMetaStore`+`runUpdate`. Version-byte/truncation manipulation operates on a copy — never a real `.dcfh/`.

**Principle: byte-identity round-trip is a *current-version-only* test.** We write only the current
version, so "write → read → identical bytes" can exist only for `CurrentIndexVersion` (v4 today). Legacy
versions are **never** round-tripped — reading v2/v3 and writing back yields v4 by design — so they get
**one-way decode assertions** (TC-2/TC-3), not round-trips. The round-trip fixture must lay
`CurrentIndexVersion` (the constant, so it follows the next bump), never a literal version.

**Existing-test migration**: `TestRoundTrip_V3_ByteIdentical` already lays `CurrentIndexVersion`, so it
*becomes* the v4 round-trip with no change beyond intent. `TestRoundTrip_V2_ParseOffset` is **misnamed**
— it is a parse-offset / header-size check, not a byte-identity round-trip; reframe it onto
`layLegacyEntry` (legacy entry under the 88-byte v2 header) and treat it as "v2 entries decode at offset
88", **not** as a v2 round-trip.

### Coverage Targets
- **`layoutForVersion`** and **`StrategyForVersion`**: 100% of arms (the reject/error arm is the
  security boundary — not optional).
- **`TranscodeLegacyIndex`**: v2 + v3 positive (every field) + corrupt-region + oversized-`EntryCount`
  + empty negatives.
- **Both loaders** exercised for the v3 transcode-load and the out-of-range reject.
- **Regression**: no drop vs the 3.1/3.2 baseline; v4 fixtures load/round-trip byte-correctly.

## Test Cases
### Functional Test Cases

- **TC-1 (FR: write-v4 / read-v4, AC1) — v4 round-trip byte-identity** — `pkg/format`
  - **Given**: the migrated `TestRoundTrip_V3_ByteIdentical` (now laying `CurrentIndexVersion`==4).
  - **When**: an entry set is laid as v4, read via the codec, every value written back via setters.
  - **Then**: the buffer is byte-for-byte unchanged; getters return laid values; v4 entry sits at the
    v4 offset. The gate that proves the v4 layout is self-consistent.

- **TC-2 (FR: read v3, AC3) — v3 decodes via DecodeHeap, every field correct** — `pkg/` integration
  - **Given**: a v3 legacy index (`layLegacyEntry` + 104-byte v3 header), with an inode value
    **> 2^32** (e.g. `0x1_0000_0001`) so the widening is observable.
  - **When**: loaded via **both** `LoadIndexFromFileForValidation` and the tracking loader.
  - **Then**: every field — `Dev`/`Ino` (full 64-bit value, not truncated), Mode/UID/GID/FileSize/
    EntryFlags/HashType/Hash/Path — matches the laid legacy values; and the load is **routed through
    `DecodeHeap`** (assert the resulting index file is `heapBacked`), proving the legacy bytes were
    transcoded, never cast in place.

- **TC-3 (FR: read v2, AC3) — v2 decodes through the 88-byte header path** — `pkg/` integration
  - **Given**: a v2 legacy index (`layLegacyEntry` + 88-byte v2 header; same legacy entry layout as v3).
  - **When**: loaded.
  - **Then**: fields decode correctly (entries begin at `V2HeaderSize`=88; the transcoder reads the
    shorter header and emits a v4 image with a zero `Timestamp`). Confirms v2 is read, not just v3.

- **TC-4 (FR: write-v4 ownership, AC1) — only v4 is written** — `pkg/` + `cmd/`
  - **Given**: `NewMetaStore`+`runUpdate` on a `t.TempDir()` tree.
  - **When**: the index is written and the header read back.
  - **Then**: header version byte == 4; no production caller passes a version (compiler + grep). A v4
    written index reloads via the zero-copy path (not `DecodeHeap`).

- **TC-5 (FR: transcode, NFR5) — `TranscodeLegacyIndex` unit (positive + fail-closed)** — `pkg/format`
  - **Given**: crafted legacy entry-regions.
  - **When**: transcoded.
  - **Then**: (a) v2 and v3 regions → a v4 image whose every field matches; (b) a region truncated
    mid-entry, a bogus per-entry `Size`, and a header-declared `EntryCount`=`0xFFFFFFFF` against a tiny
    file all **error cleanly** with no over-read and **no allocation sized from `EntryCount`**; (c) a
    zero-entry index → a header-only v4 image.

- **TC-6 (FR: dispatch, AC) — `layoutForVersion` table** — `pkg/format`
  - **Given**: the pure selector.
  - **When**: called with `{4}`, `{2,3}`, `{0,1,5,0xFFFFFFFF}`.
  - **Then**: current offsets / legacy offsets / **non-nil error** respectively; no panic; default arm
    fails closed (a bogus version never selects v4 offsets).

- **TC-7 (FR3/NFR4, AC3) — unsupported version rejected via both loaders** — `pkg/` integration
  - **Given**: the 3.2 `version_dispatch_load_test` fixtures, re-anchored to current==4 (patch the
    header version to 5 / `0xFFFFFFFF`).
  - **When**: loaded via both loaders.
  - **Then**: clean descriptive error, no panic, no over-read, mmap released (under `-race`).

- **TC-8 (the bug fix, AC2) — dupes correct on >32-bit inodes** — `pkg/` end-to-end
  - **Given**: two **distinct** entries whose inodes share low-32 bits but differ above (e.g.
    `0x1_0000_0005` vs `0x2_0000_0005`), same `Dev`.
  - **When**: `dedupByInode` / `dcfh dupes` runs.
  - **Then**: they are **not** collapsed as hardlinks (the pre-fix `[2]uint32` key dropped one); both
    survive into duplicate analysis. The direct regression proof of the reported bug.

- **TC-9 (NFR5) — heap-backed `Cleanup()` never munmaps** — `pkg/` integration
  - **Given**: a synthetic `mmapIndexFile{heapBacked:true, Data:<heap buffer>}`.
  - **When**: `Cleanup()` is called (and via the normal legacy-load drain path).
  - **Then**: no `unix.Munmap` on the heap slice; no crash/EINVAL. Run under `-race`. (Boundary: a real
    mmap-backed file still munmaps — the guard is `heapBacked`, not a blanket skip.)

- **TC-10 (FR: dcfhfix read-v3/write-v4, AC4) — repair stamps v4** — `cmd/dcfhfix` + reload
  - **Given**: a v3 index repaired via dcfhfix.
  - **When**: the repaired file is written and reloaded.
  - **Then**: the output header version == 4, entries are v4-shaped and reload cleanly (the
    `createTempIndexWithHeader` v4 stamp + version-aware size floors); and dcfhfix's bounds-checked
    repair-read of the v3 input used the **legacy** offsets (version-aware `SafeEntry`) — fields
    correct, no mis-stride, a legitimate v3 entry not rejected by a v4-sized floor.

### Non-Functional Test Cases
- **Reliability (NFR5)**: every negative (TC-5/7) and the heap path (TC-2/9) run under the project's
  canonical race gate `GOFLAGS="-gcflags=all=-d=checkptr=0" go test -race -short ./...`
  (`.githooks/pre-commit:102`) — gives "no over-read / no double-free / no munmap-of-heap" a detector.
- **Security (NFR4)**:
  - **G115 clean** with G115 *active*: remove the exclude, `golangci-lint run ./...`, triage. **No
    suppression on any `Dev`/`Ino`/`EntryCount`/`Size` conversion** — those must be fixed structurally;
    only provably width-safe conversions elsewhere may carry a justified per-line `//nolint:gosec`.
  - **No raw-index of the untrusted version byte**: structural (switch-with-default in both
    `StrategyForVersion` and `layoutForVersion`) + proven by TC-6's error arm.
- **Performance (NFR1)**: the v4 hot path stays zero-copy — dispatch is per-load (transcode only on the
  rare legacy branch), verified by inspection + existing `pkg` benchmarks staying green.

### Golden sample fixtures (per-version, committed under `pkg/format/testdata/`)
Two complementary fixture kinds — **in-memory** (parametric) and **committed golden** (independent
oracle). A golden only adds value if it is *not* a re-serialisation of the same `legacyEntry` the
decoder uses, so each version's golden is sourced deliberately:
- **`v3.idx` — genuine, captured before the widen (time-sensitive)**: generated by the **real v3
  writer** (`NewMetaStore`+`runUpdate` at baseline `cbfa32f`, where `CurrentIndexVersion`==3) on a fixed
  tree. Once the widen lands the tool can no longer emit v3, so **capture this early in exec** and
  commit it. The decode test asserts version==3 + content-derived **stable** fields (path/mode/
  filesize/hash); it does **not** assert `Dev`/`Ino` (machine-specific). Proves 3.3 reads what real v3
  dcfh wrote, independent of `legacyEntry`. *Caveat*: its inodes are already uint32-truncated, so it
  proves decode-compat **only** — the >2^32 fix is proven by the in-memory TC-8 fixture + a v4 file.
- **`v4.idx` — frozen layout anchor (post-impl)**: committed after the writer is v4; the test asserts
  byte-stability so a future struct edit that shifts the v4 layout is caught.
- **v2 — in-memory only (no committed golden)**: no live v2 producer exists, so any v2 golden is
  hand-built = same fidelity as the `layLegacyEntry` in-memory fixture. Use the in-memory case. (The
  tracked root-level `./cache-2.idx` is conversion-tool debris, **not** a trustworthy fixture.)
- **Housekeeping**: remove the stale tracked `./cache.idx` (v1, below `MinIndexVersion`, unloadable)
  and `./cache-2.idx` (v2) debris from `9fe00d4` — unreferenced and in the wrong place now that
  `pkg/format/testdata/` exists. (Surface to the user; pre-existing files.)

## Test Environment
### Setup Requirements
- Go 1.24.x toolchain; `golangci-lint` (gosec is a v2 linter inside `.golangci.yml`) for the G115 gate.
- In-memory fixtures: `layLegacyEntry`/`layIndex` build v2/v3/v4 images deterministically (for the
  parametric/negative/bug-proof cases). Committed goldens per the section above (compat/layout oracle).
  Integration fixtures: real indices via `NewMetaStore`+`runUpdate` in `t.TempDir()`, then
  byte-patched/truncated on a **copy**; never mutate a real repository index.
- The >2^32 inode values are synthesised in the fixture/struct directly (no special filesystem needed).

### Automation
- `go test ./pkg/format/` (unit), `GOFLAGS="-gcflags=all=-d=checkptr=0" go test -race -short ./pkg/... ./cmd/...`
  (integration + regression). G115 measurement is a manual gated step recorded in g-testing-exec.

## Validation Criteria
- [ ] TC-1 passes: v4 round-trip byte-identical.
- [ ] TC-2 + TC-3 pass: v3 **and** v2 decode with every field correct; v3 routed via `DecodeHeap`.
- [ ] TC-4 passes: written index is v4; no caller passes a version; v4 reloads zero-copy.
- [ ] TC-5 + TC-6 pass: transcoder positive + fail-closed negatives; `layoutForVersion` 100% arms.
- [ ] TC-7 passes: unsupported version rejected via both loaders under `-race`.
- [ ] TC-8 passes: dupes does not collapse a >32-bit-inode-distinct pair (the bug fix).
- [ ] TC-9 passes: heap-backed `Cleanup()` performs no munmap (under `-race`).
- [ ] TC-10 passes: dcfhfix repair writes v4 and reloads; repair-read of v3 uses legacy offsets.
- [ ] `golangci-lint run ./...` clean with G115 active; no suppression on the Dev/Ino/Size class.
- [ ] Golden fixtures committed: `testdata/v3.idx` (captured from the real v3 writer **before** the
      widen lands) decodes with stable fields correct; `testdata/v4.idx` byte-stable; stale root
      `./cache.idx`/`./cache-2.idx` debris removed.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All 10 test cases pass; race gate green; G115 whole-tree 0. Goldens committed (`v3.idx` genuine pre-bump,
`v4.idx` layout anchor) plus `.gitattributes *.idx binary`. The v3 golden's content-derived hash framing
(neither raw `sha1sum` nor `git hash-object`) was asserted from the committed oracle. Deferred: stale root
`./cache.idx` / `./cache-2.idx` debris removal (surfaced, not actioned).

## Lessons Learned
Byte-identity round-trip is current-version-only by design; legacy versions get one-way decode assertions.
Committed binary goldens require `.gitattributes ... binary` so a checkout reproduces them byte-exact
regardless of EOL normalisation or a contributor's `text=auto`.
