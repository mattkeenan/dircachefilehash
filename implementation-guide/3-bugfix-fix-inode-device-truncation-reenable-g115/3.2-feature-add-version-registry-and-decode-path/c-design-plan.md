# Add version registry and decode path - Design
**Task**: 3.2 (feature)

## Task Reference
- **Task ID**: internal-3.2
- **Task URL**: N/A (internal task)
- **Parent Task**: 3
- **Branch**: feature/3.2-add-version-registry-and-decode-path
- **Template Version**: 2.1

## Goal
Design the single-owned version-dispatch seam in `pkg/format`: one resolver that gates the
entry zero-copy cast on the version, one owner for the write version — both ready for 3.3 to add
the v4 layout as a localised edit, with no on-disk change in 3.2.

## Design Priorities
Testability → Readability → Consistency → Simplicity → Reversibility

## Architecture Preferences
Composition over inheritance. Interfaces over singletons. Explicit over implicit.

## Key Decisions

### Architecture Choice — a strategy resolver, not a decoder registry (yet)
- **Decision**: Add one exported function to `pkg/format`,
  `StrategyForVersion(version uint32) (DecodeStrategy, error)`, returning a small enum
  (`DecodeReject`, `DecodeZeroCopy`). It is a `switch`-with-`default`. The two `pkg/index.go`
  entry-walk loaders call it on **`header.Version` (the value read from the file)** — *not*
  `ms.version` — immediately before the entry cast.
- **Load-bearing detail**: the resolver is called on `header.Version` **independent of** the
  `ValidateVersion` clamp, not strictly "after" it. For `loadIndexFromFileWithTracking` the clamp
  and cast are in one function; but for `collectEntryRefs` the clamp runs in the sibling
  `openAndValidateIndex` and — critically — is a **no-op** for the dcfhfind validation path
  (`MetaStore{version: 0}`, `dcfhfind_support.go:115` → `ValidateVersion(0)` returns nil for any
  version). So for that path the resolver is the *first and only* real version gate, and it must
  read `header.Version` at the head of `collectEntryRefs`. Feeding it `ms.version` (0 there) would
  defeat the gate.
- **Rationale**: The parent design sketched a "version registry keyed by map/switch-with-default";
  the **concrete** realisation for 3.2 is a switch with two reachable arms — `CurrentIndexVersion`
  and recognised-legacy both map to `DecodeZeroCopy` (entry layout is byte-identical across all
  shipped versions), everything else to `DecodeReject`. A `map[uint32]decoder` registry would have
  one real entry today; per "concrete over generic" (parent design) and Rule of Three, the switch
  is the right shape until 3.3 gives it a second, *behaviourally distinct* version.
- **3.3 seam**: when 3.3 bumps current to v4 (divergent entry layout), it adds a `DecodeHeap` arm
  and flips the legacy case from `DecodeZeroCopy` to `DecodeHeap` — a localised edit in one
  function, plus the heap decoder itself. 3.2 deliberately ships **no** `DecodeHeap` arm (no
  divergent layout exists to exercise it; unreachable code is worse than a deferred edit).
- **Trade-offs**: Introduces an enum + function where today the cast is unconditional. Accepted:
  it names and tests the version→cast decision that is currently implicit (correct only by the
  v2/v3 layout coincidence), and it is the seam the whole parent task exists to create.

### Write-version ownership
- **Decision**: `SetHeaderForWritableIndex` **drops its `version` parameter** and sources
  `format.CurrentIndexVersion` internally. The lower-level `SetHeader(…, version, …)` primitive
  **keeps** its explicit `version` (tests and the repair tool legitimately write a chosen version).
  The two production callers (`temp_index_writer.go:99,176`) stop passing `tiw.ms.version`.
- **Empty-index path** (`index.go:774`) currently calls `SetHeader(ms.signature, ms.version, …)`
  directly; it is re-pointed at the version-less writable-header path so every *normal* write
  sources current from one owner.
- **Rationale**: Closes the parent design's "write-current is owned, not passed" gap
  (c-design-plan.md:141-143). `ms.version` is only ever `CurrentIndexVersion` (metastore.go:215),
  so this is behaviour-preserving while removing the ability for a caller to write a divergent
  version by mistake.
- **Boundary (explicit)**: `dcfhfix`'s repair write path is **not** forced to current — a repair
  may legitimately preserve the original file's version. Write-version ownership governs the
  *normal* index write path, not the forensic repair tool. Its `SetHeader(version)` use stays.

### dcfhfix read path — deferred to 3.3 (explicit)
- **Decision**: `dcfhfix`'s repair *read* path (`entry_workflow_main.go:122`) does **not** route
  through the resolver in 3.2.
- **Rationale**: Its entry cast is correct today (identical v2/v3 layout) and dcfhfix carries its
  own corruption-assuming bounds discipline (`SafeEntry`). Routing it through the resolver has **no
  functional benefit until 3.3**, where it becomes load-bearing (a v3 entry must be heap-decoded
  and widened on read). Adopting it then keeps 3.2 thin and avoids disturbing the repair tool's
  careful handling for no behaviour change. Recorded so it is a decision, not an omission.

### Resolver applies to entry materialisation only
- **Decision**: Only the two **entry-walk** loaders consult the resolver
  (`collectEntryRefs` ~`index.go:337`; the `Index`-returning loader ~`index.go:614`). The
  **header-only** loader (~`index.go:150`) materialises no entries, so it keeps its
  `ValidateVersion` check and gains no resolver call.
- **Rationale**: The resolver gates the *entry* zero-copy cast; a header read has no entry cast to
  gate. Scoping it here keeps FR1 honest (the requirements draft over-reached by naming the
  header-only loader).
- **Residual on-disk casts, tracked for 3.3 (not 3.2 work)**:
  - `BEIndexFileIOEntry.readEntryData` (`binary_entry_index_file.go:73`) is a third on-disk→entry
    cast, but it has **no production callers** (constructed only in tests). Excluded from 3.2; in
    3.3 it must be routed through the resolver **or deleted** before v4 diverges the layout, else it
    becomes a silent ungated path. Recorded here so it is not rediscovered as a surprise.
  - The header-only loader's `expected=0` callers (e.g. `snapshot.go:486`
    `ValidateIndexHeader(path, true, 0)`) derive `headerSizeForVersion(header.Version)` from an
    unvalidated version, but the header read itself is bounds-tolerant (page-rounded mmap,
    `index.go:146-150`) and materialises no entries — no new defect. Flagged so 3.3's
    version-dependent entry sizing does not silently leave these callers unprotected.

## System Design

### Component Overview
- **`format.DecodeStrategy` + `StrategyForVersion`** (`pkg/format`, new): the single owned
  read-dispatch decision. Pure function of `version`; default-bearing; no I/O, no buffer access —
  unit-testable over a table of version inputs.
- **`format.Header.SetHeaderForWritableIndex`** (`pkg/format`, modified): version-less; owns the
  write version via `CurrentIndexVersion`.
- **`pkg/index.go` entry-walk loaders** (modified): call `StrategyForVersion` after
  `ValidateVersion`; on `DecodeZeroCopy` perform today's cast; on error, fail closed.
- **Unchanged**: `SafeEntry` bounds checks (the resolver gates *which* path, never bypasses the
  per-field bounds check), `headerSizeForVersion`, `ValidateVersion`, the checksum order.

### Data Flow
1. Open + mmap index → header cast (unchanged) → `ValidateSignature` / `ValidateByteOrder` /
   `ValidateVersion` (unchanged).
2. **New**: `strategy, err := format.StrategyForVersion(header.Version)` at the head of each
   entry-walk loader (on the file's `header.Version`, not `ms.version`).
   - `err != nil` (unknown / out-of-range / `default`) → **fail closed using that loader's existing
     cleanup contract** (see below), return error.
   - `DecodeZeroCopy` → proceed to the existing entry-walk cast.
   - (`DecodeHeap` → 3.3, not present in 3.2.)
   - **Per-loader cleanup contract** (do not insert a raw `munmap` — it would double-free):
     `loadIndexFromFileWithTracking` owns its `mmapIndexFile` (`refCount: 1`) and releases via
     `indexFile.DecRef()` (matching its other error paths). `collectEntryRefs` does **not** own its
     `indexFile` — `openAndValidateIndex` created it and the caller closes it — so it simply
     returns the error (matching its existing error returns), leaving cleanup to the owner.
3. Entry walk → existing `(*binaryEntry)(unsafe.Pointer(&entryData[offset]))` cast + `SafeEntry`/
   chaining validation (unchanged).
4. Write path → `SetHeaderForWritableIndex` sources `CurrentIndexVersion` (no caller version).

## Interface Design

### New / changed surface in `pkg/format`
```
// DecodeStrategy says how an index of a (validated) version is materialised.
type DecodeStrategy int
const (
    DecodeReject   DecodeStrategy = iota // unknown / out-of-range — fail closed
    DecodeZeroCopy                       // mmap cast (current + identical-layout legacy)
    // DecodeHeap — added in 3.3 when v4 makes a legacy entry layout diverge
)

// StrategyForVersion maps a header version to its materialisation strategy.
// Default-bearing: an unrecognised version returns (DecodeReject, error) rather
// than indexing a table. This is the real safety boundary for the dcfhfind
// validation path, which builds MetaStore{version: 0} (dcfhfind_support.go:115)
// so its ValidateVersion clamp is a no-op for any on-disk version. Callers must
// pass header.Version (the file's value), never ms.version.
func StrategyForVersion(version uint32) (DecodeStrategy, error)

// SetHeaderForWritableIndex — VERSION PARAMETER REMOVED; sources CurrentIndexVersion.
func (ih *Header) SetHeaderForWritableIndex(signature [4]byte, entryCount uint32,
    baseFlags FlagBits, checksumType HashKind)
```

### Data Models
No on-disk data model change. `DecodeStrategy` is an in-memory dispatch tag only; it is never
serialised. The on-disk `Header`/`Entry` layout and version numbers are unchanged from 3.1.

## Constraints
- Inherited (parent c-design-plan.md): one place for versioned format code; host-order zero-copy
  preserved for the current version; no width/version/behaviour change in 3.2; British spelling;
  no superlatives.
- **Single version gate — clarified**: `StrategyForVersion` does encode a supported-range arm, so
  it is technically a second range test alongside `ValidateVersion`. They are not independent
  duplications: both key off the same `MinIndexVersion`/`CurrentIndexVersion` constants, so a 3.3
  bump updates both. The resolver is retained as the *materialisation-strategy authority* (and the
  only real gate for the `version:0` dcfhfind path); `ValidateVersion` stays as the early
  signature/byte-order/version triple. A code comment records that 3.3 must keep both in lockstep.
- The resolver is a *gate in front of* the existing cast, not a replacement for `SafeEntry`'s
  per-field bounds checks (G103 reliance preserved).

## Decomposition Check
- [ ] **Time**: <1 week. No.
- [ ] **People**: 1. No.
- [ ] **Complexity**: one concern (the dispatch seam + write-version owner). No.
- [ ] **Risk**: contained by invariance + boundary gates. No.
- [ ] **Independence**: not further separable usefully.

**Outcome**: 0 signals — proceed as a single subtask. Fold-into-3.3 remains the user's pre-exec
review call (a-task-plan.md), not a decomposition trigger.

## Validation
- [ ] Design review completed (plan-review subagents, Step 8)
- [ ] Architecture approved by user
- [ ] Integration points verified (both entry-walk loaders route through the resolver; write path
      sources current; dcfhfix untouched in 3.2)

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Design realised with 3 recorded deviations (gate consolidation into `checkEntryRegionAccess`,
`parseTrackedEntries` extraction, a use-after-munmap fix), all documented in f-implementation-exec.md.
Design intent preserved: single-owned dispatch, no parallel second gate, zero-copy hot path intact.

## Lessons Learned
Consolidating the version gate and header-size guard into one helper improved on the design's
two-gate sketch without losing the dcfhfind `version:0` security property (the gate runs in
`openAndValidateIndex`, which always precedes `collectEntryRefs`). See j-retrospective.md.
