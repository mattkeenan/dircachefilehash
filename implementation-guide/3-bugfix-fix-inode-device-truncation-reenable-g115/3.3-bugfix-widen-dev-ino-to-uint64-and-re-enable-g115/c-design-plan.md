# Widen Dev/Ino to uint64 and re-enable G115 - Design
**Task**: 3.3 (bugfix)

## Task Reference
- **Task ID**: internal-3.3
- **Task URL**: N/A (internal task)
- **Parent Task**: 3
- **Branch**: bugfix/3.3-widen-dev-ino-to-uint64-and-re-enable-g115
- **Template Version**: 2.1

## Scope of this design
The leaf subtask that performs the actual width change: `DevID`/`Inode` → `uint64` (format **v4**),
the legacy (v2/v3) read path that v4 makes necessary, the ingest/consumer widening, and re-enabling
gosec G115. Builds on 3.1 (`pkg/format` ownership) and 3.2 (the `StrategyForVersion` resolver +
version-less writable header), both present at baseline `cbfa32f`.

## Goal
Decode any supported on-disk version into one canonical in-memory entry, always write the current
version, and stop `dcfh dupes` truncating `Dev`/`Ino` — with the materialisation-by-version decision
single-owned in `pkg/format` and no version branching leaking into consumers.

## Design Priorities
Testability → Readability → Consistency → Simplicity → Reversibility

## Architecture Preferences
Composition over inheritance. Interfaces over singletons. Explicit over implicit. **Best part is no
part**: reuse the existing entry-walk/ref/skiplist machinery rather than add a parallel legacy path.

## Key Decisions

### Architecture Choice — legacy read as a whole-region transcode, not a parallel entry path
- **Decision**: There is **one** canonical in-memory entry type (`format.Entry`, post-widen = the v4
  layout) and **one** writer (always current version). Version multiplicity is confined to a single
  read-time step. When `StrategyForVersion(header.Version)` returns `DecodeZeroCopy` (current) the
  existing mmap path is unchanged; when it returns `DecodeHeap` (legacy v2/v3) the loader **transcodes
  the whole entry region** from the legacy byte layout into a v4-layout heap buffer, then wraps that
  buffer in a synthetic (non-mmap) `mmapIndexFile` and runs the **existing** `collectEntryRefs` walk
  over it. `binaryEntryRef{Offset, IndexFile}` resolves as `&Data[0] + headerSize + Offset`, so its
  base is just a `[]byte` — pointing it at the heap buffer needs no ref/skiplist change.
- **Rationale**: The zero-copy cast at `index.go:372` (and the tracking loader's twin) is the only
  site that misreads a legacy file once the struct widens. Transcoding *before* that cast means the
  cast always sees genuine v4 bytes regardless of source — the on-disk legacy bytes are **never** cast
  as v4. Honours the parent's read-old/write-new directive while reusing the validate→walk→ref→skiplist
  pipeline verbatim. Legacy files are transient (rewritten to v4 on the next `update`), so the one-pass
  transcode cost is bounded and rare.
- **Trade-offs**: Loses zero-copy for legacy files (accepted — rare, transient) and adds a heap buffer
  ~one index in size on that path. The alternative (per-entry heap `BEScanEntry`, threading
  interface-entries through a `collectEntryRefs` that today returns `[]binaryEntryRef`) diverges far
  more from the shared path for no real gain. Rejected.
- **Cleanup nuance (correctness gate, not polish)**: a synthetic heap-backed `mmapIndexFile` must
  **not** `munmap` its `Data` on `Cleanup()` (the buffer is GC'd heap, not a mapping). `Cleanup()`
  (`index.go:56-65`) munmaps whenever `Data != nil`, and `File == nil` is *already* normal for
  read-only main/cache indices — so a nil-fd guard does **not** discriminate and would still munmap
  the heap slice (UB/crash). The guard must be an **explicit marker** on `mmapIndexFile` (a
  `heapBacked bool` or a dedicated `Type` sentinel) that `Cleanup()` keys off, with a test that
  constructs a synthetic heap-backed file and calls `Cleanup()` proving no munmap. d-plan pins the
  exact field and branch.

### Type Vocabulary (the width single-source)
- `DevID`/`Inode` change from `= uint32` to `= uint64` in `pkg/format/vocabulary.go`. They stay
  **aliases**: a narrowing `uint64→uint32` assignment at any consumer is still a compile error, so the
  widening cannot silently re-truncate. **But the alias does not propagate everywhere**: hand-typed
  interface method signatures (`Dev() (uint32, error)`) and adapter struct fields (`Dev uint32`) are
  literal `uint32`, not the alias, so they must be edited **by hand** — see "Hand-edited sites" below.

### Hand-edited sites (the alias does NOT auto-propagate — d-plan enumerates, exec must hit all)
- **Two accessor interfaces**: `BinaryEntryInterface.Dev()/Ino()` (`binary_entry_interface.go:39-40`)
  and `FilterEntry.Dev()` (`filter.go:25`) — widen the return types `uint32 → uint64`.
- **Implementers in lockstep**: `binary_entry_skiplist.go`, `binary_entry_index_file_mmap.go`,
  `binary_entry_scan.go`, `dcfhfind_support.go:41`, `filter.go:113`, `scan.go:295` (and the deleted
  `binary_entry_index_file.go`).
- **Second ingest truncation**: `scan.go:300` `return uint32(sys.Dev)` — a hand-typed cast the alias
  will *not* catch; remove alongside `binary_entry_scan.go:69-70`.
- **Backing struct fields**: `EntryInfo.Dev uint32` (`dcfhfind_support.go:20`) and the dcfhfix JSON
  struct `Dev uint32`/`Ino *uint32` (`cmd/dcfhfix/entry_append_remove.go:20-21`).
- **Runtime floor**: `Entry.RelativePath`'s hardcoded `Size < 48` panic floor (`entry.go:97`) no longer
  matches the struct minimum after +8 — replace the literal with `unsafe.Sizeof(Entry{})`/`minEntrySize`.
- **Dedup key literal**: keep it a value array — `[2]uint64` over `map[[2]uint64]struct{}`
  (`dupes.go:253-256`); do not introduce a struct key or hashing helper.
- **Wire path note** (no change): `wire.go:83` already carries `Dev uint64` and the remote walker
  leaves `Ino=0` (`walker_wire.go:175`), so remote-walked entries gain no inode dedup from this widen —
  documented to avoid a false expectation, not a code site.

### Version constants & the lockstep gate
- `CurrentIndexVersion` 3 → 4; `MinIndexVersion` stays 2. `StrategyForVersion`'s current arm matches 4,
  its legacy arm (`>= Min && < Current`) now covers {2,3} and flips from `DecodeZeroCopy` to
  `DecodeHeap`. `ValidateVersion` accepts the same [2,4] range — moved in lockstep (the 3.2 code
  comment already flags this requirement).

### Per-version layout, single-owned
- v2/v3 entry layouts are **byte-identical** (they differ only in the header), so there is exactly one
  legacy entry layout to decode. Its offsets come from a `legacyEntry` struct (uint32 `Dev`/`Ino`)
  whose offsets are `unsafe.Offsetof`-derived — never hand-coded — exactly as the canonical offsets in
  `codec.go` are today. A `layoutForVersion(version)` selector returns the current-or-legacy offset
  set. This is the single owner consumed by **both** the transcode decoder and dcfhfix's repair reads.

### Build-time layout assertions become version-aware
- The existing `Sizeof(Entry{})%8==0` / `Path==8` build assertions now assert the **v4** sizing; an
  added assertion pins the **legacy** struct's sizing so a future edit can't silently desync the two.

## System Design

### Component Overview
- **Vocabulary** (`format`): `DevID`/`Inode` widened to `uint64` — the sole width declaration.
- **Canonical `Entry`** (`format`): unchanged shape, now the v4 layout; build-time assertions assert v4.
- **`legacyEntry` + `layoutForVersion`** (`format`, **new**): the v2/v3 fixed-field layout and the
  version→offset-set selector. Single owner of legacy offsets; no second table anywhere.
- **Legacy transcoder** (`format`, **new**): `decode legacy entry-region bytes → v4 entry-region bytes`.
  Per entry: **advance the walk using the legacy `Size`/offsets** (the legacy entry's own `Size` field
  describes the legacy total — mixing it with the v4 size desyncs the walk by +8/entry), read legacy
  fixed fields at legacy offsets, write v4 fixed fields, append the unchanged path bytes, and **emit a
  recomputed v4 `Size`+padding** via `BESizeFromPathLen`. Validates each declared `Size` and the region
  length before reading — short/corrupt legacy input errors, never over-reads.
- **Loader branch** (`pkg/index.go`): on `DecodeHeap`, transcode then wrap in a synthetic
  `mmapIndexFile`; on `DecodeZeroCopy`, today's path. Single new branch per loader; the walk is shared.
- **Version-aware `SafeEntry`** (`format`): selects its offset set from `layoutForVersion`, so dcfhfix
  bounds-checks v3 *and* v4 bytes correctly.
- **Consumers** (`dupes`, `dcfhfind`, ingest): widen the dedup key, the two accessor interfaces + their
  implementers, the backing struct fields, and remove both ingest truncation casts — the full set is in
  "Hand-edited sites" above. **No version logic** — they only ever see canonical `Entry`.
- **Deletions**: dead `BEIndexFileIOEntry` (no production callers) — removes a parallel cast site.

### Data Flow
1. **Scan/ingest** → populate `Entry` with full-width `stat` `Dev`/`Ino` (no `uint32(...)` cast).
2. **Serialise/write** → always current (v4) via the owned `SetHeaderForWritableIndex` → atomic rename.
3. **Load (v4)** → `DecodeZeroCopy` → mmap cast → `collectEntryRefs` → skiplist (fast path, unchanged).
4. **Load (v2/v3)** → `DecodeHeap` → transcode region to v4 heap buffer → synthetic `mmapIndexFile` →
   **same** `collectEntryRefs` → skiplist. Mmap of the legacy file released after transcode.
5. **dupes** → dedup on the full-width `(Dev, Ino)` key over canonical entries.
6. **dcfhfix** → `layoutForVersion(version)` + `SafeEntry` for bounds-checked field access/repair.

## Interface Design

### Public contract added to `pkg/format` (the cross-subtask seam)
- `DecodeHeap` becomes a live `DecodeStrategy` value; `StrategyForVersion`'s legacy arm returns it.
- `layoutForVersion(version uint32) (entryLayout, error)` — the version→offset-set selector (current
  `codec.go` offsets for v4, `legacyEntry`-derived offsets for v2/v3). Its default arm **fails closed
  with an error** — a bogus/zero version must never silently select v4 offsets against legacy/garbage
  bytes. Repair callers (dcfhfix) source `version` from the **validated header**, never a zeroed
  `MetaStore.version`.
- A region transcoder usable by the loader (exact signature pinned in d-plan; takes legacy entry-region
  bytes + entry count, returns a v4 entry-region buffer or a clean error on malformed input).
- `SafeEntry` construction gains a version (or layout) parameter so repair reads pick the right offsets.

### Data Models
```
format.Entry {                    // canonical, in-memory, = v4 on-disk layout
  Size RecordSize; CTimeWall/MTimeWall WallTime
  Dev DevID; Ino Inode            // uint32 -> uint64 (this task); every field below shifts +8
  Mode FileMode; UID UserID; GID GroupID; FileSize ByteSize
  EntryFlags FlagBits; HashType HashKind; Hash [64]byte; Path [8]byte
}
format.legacyEntry { ... Dev uint32; Ino uint32; ... }   // v2/v3 fixed layout (offsets via Offsetof)
layoutForVersion(v): v==4 -> current offsets; v in {2,3} -> legacy offsets; else -> error (fail closed)
```

## Cross-subtask correctness & safety (inherited parent invariants honoured here)
- **Version-gated cast**: the mmap zero-copy cast fires only for `version == current`; every other
  version is transcoded, never cast in place. Asserted by a test that a v3 fixture is *routed through*
  `DecodeHeap` (not merely read correctly).
- **Bounds-checked decode**: the transcoder validates each legacy entry's declared `Size` and the
  region length before reading — a short/corrupt legacy file errors, never over-reads. `readField`'s
  internal bounds check is preserved (non-bypassable).
- **Write-current owned**: writes source `CurrentIndexVersion` from `pkg/format` (already true after 3.2).
- **Dedup correctness boundary**: widening fixes *future* under-reporting only; pre-existing v3 entries
  already lost high bits at ingest and recover when a re-scan rewrites them to v4. The v4 bump triggers
  that rewrite on the next `update` — no separate migration tool. A documented decision.

## Constraints
- Unix/64-bit/atomic-rename (unchanged). Host-order on-disk (unchanged). `unsafe`/mmap zero-copy design
  (the G103 exclusion) preserved — all new `unsafe` reads go through `pkg/format`.
- Project rules: no `--no-verify`; never disable tests; British prose; `.cwf/version` not committed.

## Decomposition Check
- [ ] **Time**: >1 week? No — 2-3 days.
- [ ] **People**: >2? No.
- [x] **Complexity**: 3+ concerns (widen, legacy decode, lint gate) — triggered, but interdependent.
- [ ] **Risk**: needs isolation? The risky legacy-decode logic is already isolated in `pkg/format`.
- [ ] **Independence**: separable? No — the v4 bump is atomic across these pieces.

**Verdict**: As in a-task-plan — one signal, tightly coupled around the atomic v4 bump. Keep as one task.

## Validation
- [x] Design review completed (Step 8 plan review — 4 subagents; findings synthesised below).
- [x] Integration points verified against the codebase: `binaryEntryRef` base model, both loader
      entry-walks, `codec.go` Offsetof offsets, `BEIndexFileIOEntry` has no production callers.
- [ ] Acceptance step for e/d-plan: after removing the G115 exclude, run **`golangci-lint run ./...`**
      (not standalone gosec) and confirm zero residual G115 findings outside the three known sites.

### Plan-review synthesis (Step 8)
Four reviewers (improvements/misalignment/robustness/security) confirmed the architecture is sound and
reuse-first. Applied: the Cleanup guard corrected to an explicit `heapBacked` marker (fd-nil does not
discriminate); the `Dev()`/`Ino()` widening reframed as an interface-contract change with a full
"Hand-edited sites" list (two interfaces + implementers + struct fields + the `scan.go:300` cast + the
`entry.go:97` floor + the dedup literal); the transcoder's legacy-Size-walk / v4-Size-emit clarified;
`layoutForVersion` made fail-closed. No unapplied findings of substance — remaining reviewer notes
("pin exact field in d-plan", "budget file count") are deferred to the implementation plan by design.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
The whole-region transcode design held: legacy bytes are never cast as v4, and the existing
walk/ref/skiplist pipeline was reused verbatim via a heap-backed `mmapIndexFile`. Deviation: the single
planned `legacyEntry`/`legacy_layout.go` became three per-version files with an explicit per-version
`layoutForVersion` switch (user-directed, for clarity and future divergence). The `heapBacked` Cleanup
guard shipped exactly as designed (correctness gate, not polish).

## Lessons Learned
A single legacy layout would still have needed width-aware reads: reading uint32 Dev/Ino with the wide
type over-reads into the adjacent field. Surfaced in exec as the `GetDev`/`GetIno` bug; fixed with a
per-layout `narrowDevIno` flag (read uint32, then widen).
