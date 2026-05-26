# Fix inode device truncation re-enable G115 - Design
**Task**: 3 (bugfix)

## Task Reference
- **Task ID**: internal-3
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: bugfix/3-fix-inode-device-truncation-reenable-g115
- **Template Version**: 2.1

## Scope of this design
Parent-level / **cross-subtask** architecture only: the shared format package, its
public contract, the type vocabulary, and the read-old/write-new model. Subtask-internal
mechanics (exact call-site edits, per-field test matrices) are deferred to each subtask's
own design/implementation plan.

## Goal
Make the on-disk format's type information (width, signedness, offset, per-version layout)
have a single owner, so widening `Dev`/`Ino` to 64-bit — and any future type change — is a
single-sited edit rather than a cross-codebase sweep.

## Design Priorities
Testability → Readability → Consistency → Simplicity → Reversibility

## Architecture Preferences
Composition over inheritance. Interfaces over singletons. Explicit over implicit.

## Key Decisions

### Architecture Choice
- **Decision**: Introduce a dedicated, exported Go package (working name `pkg/format`,
  package `format`) that is the **single source of truth** for the on-disk format. It owns:
  (1) the semantic **type vocabulary**, (2) the canonical **entry/header layout**, (3) the
  **generic codec** (typed read/write), and (4) **per-version layout descriptors**. Both the
  core package (`dircachefilehash`) and `cmd/dcfhfix` import it.
- **Rationale**: Today the layout is asserted in *five* places — the core `binaryEntry`
  struct, the `BinaryEntryInterface` accessor signatures (~10 implementers), a **hand-copied
  duplicate `binaryEntry` in dcfhfix** (`safe_entry_accessor.go`, source comment `pkg/util.go`
  already stale/deleted), a **hand-copied `indexHeader` in dcfhfix** (`main.go`), and dcfhfix's
  parallel `unsafe.Offsetof` offset table. A shared exported package collapses these to one,
  deletes both dcfhfix duplicates, and makes type changes single-sited.
- **Scope of "single owner"**: the package owns the entry layout, the **header layout**, the
  canonical **offset table**, and the version registry. The registry **subsumes**
  `headerSizeForVersion()` and the `CurrentIndexVersion`/`MinIndexVersion`/`TimestampMinVersion`
  constants (`constants.go`) — they move into `pkg/format` rather than sitting beside it, so
  version state has exactly one owner.
- **Trade-offs**: Requires exporting the canonical entry type and a mechanical rename sweep
  in the core package (`binaryEntry` → `format.Entry`). Net new package boundary. Accepted
  because it is the only structure that prevents the duplication from regrowing.

### Type Vocabulary (the width/sign single-source)
- **Decision**: One named type per on-disk concept (e.g. `DevID`, `Inode`, `FileMode`,
  `WallTime`, `EntrySize`, `HashKind`), used by the struct, the accessor interface, and
  consumers. Width and signedness of each concept live in exactly one `type` line.
- **Rationale**: A future width change (the recurring pain) becomes a one-line edit that the
  compiler propagates.
- **Trade-offs**: Alias (`=`) gives zero friction but no type-safety; named type gives
  type-safety but adds boundary conversions. Choice deferred to subtask 3.1.

### Read-old / Write-new model
- **Decision**: Reads decode **any supported version** into the canonical in-memory entry
  (always widest types); writes always emit the **current** version. The zero-copy direct
  mmap cast is **preserved only for current-version files**; non-current versions take a
  decode-on-load path into heap entries (reusing the existing `BEScanEntry` heap-entry
  pattern).
- **Rationale**: Honours the user directive (read v2/v3, write v4) without a forced re-scan,
  while keeping the hot path zero-copy. Old-version files are transient (rewritten to current
  on the next update), so the decode-copy cost is bounded and rare.
- **Trade-offs**: Loses zero-copy for legacy files; adds a per-version decode path. Accepted
  as explicit and localised inside `pkg/format`.
- **First divergent entry layout**: v2↔v3 differ only in the *header* (the `Timestamp`
  field); **entry** layout has been identical across all shipped versions. The width change is
  therefore the *first* time entry layout diverges — every field after `Ino` shifts. The
  version descriptors must encode **per-field offsets per version** (not just header size), and
  the decode path is genuinely new code, not a reuse of existing version machinery.
- **Concrete over generic**: prefer a small set of explicit, registered per-version decoders
  (e.g. one `decode<vN>Entry`) over an arbitrary-version field-table engine. This satisfies the
  "one place" directive without speculative generality; final shape decided in subtask 3.2.

## System Design

### Component Overview
- **Vocabulary** (`format`): named field types — the single declaration of width/sign.
- **Canonical Entry/Header** (`format`): exported structs using vocabulary types; the
  current-version, host-order, zero-copy layout. Carries build-time alignment assertions.
- **Generic codec** (`format`): `readField[T]/writeField[T]` deriving size from
  `unsafe.Sizeof(T)`; bounds-checked. Replaces dcfhfix's hand-typed `*(*uint32)` reads.
- **Version descriptors** (`format`): per-version field tables (offset/width/applicability),
  consumed by both the decoder and dcfhfix's repair path — no second offset table anywhere.
- **Integration seams**: core package builds/serialises via `format`; dcfhfix reads/repairs
  via `format`'s codec + descriptors; the duplicate dcfhfix struct is deleted.

### Data Flow
1. **Scan/ingest** → populate canonical `format.Entry` (vocabulary-typed; full-width `stat`
   values, no truncation cast).
2. **Serialise/write** → current-version bytes via the codec → `TempIndexWriter` → atomic
   rename (unchanged downstream).
3. **Load (current version)** → zero-copy mmap cast to `format.Entry` (fast path, unchanged).
4. **Load (legacy version)** → version descriptor decode → heap `format.Entry` (widened).
5. **dcfhfix** → version descriptor + generic codec for bounds-checked field access/repair.

## Interface Design

### Public contract of `pkg/format` (the cross-subtask seam)
This is the surface the subtasks build against; exact signatures are fixed in 3.1.
- Vocabulary types (`DevID`, `Inode`, …).
- Canonical `Entry` / header types + layout/alignment assertions.
- Codec: typed `readField`/`writeField`, entry (de)serialise helpers.
- Version registry: current version constant + per-version descriptors + a `Decode(version, bytes) → Entry` path.

### Data Models (conceptual, not final)
```
format.Entry {
  Size      EntrySize
  CTime     WallTime
  MTime     WallTime
  Dev       DevID      // widened in 3.3
  Ino       Inode      // widened in 3.3
  Mode      FileMode
  UID, GID  ...
  FileSize  ...
  Flags, HashKind, Hash, Path ...
}
format.LayoutDescriptor { Version; fields → {offset, width, signed, presentInVersion} }
```

## Cross-subtask correctness & safety constraints
These are parent-level invariants every subtask must honour (not 3.x-internal detail):
- **Version-gated cast**: the zero-copy mmap cast fires **only** after the header version is
  validated as `== current`. Any other version (including newer-than-current) → no cast, no
  decode-guess.
- **Defined version-mismatch handling**: unknown / newer-than-current / below `MinIndexVersion`
  → refuse with a clear error. `Decode(version, …)` carries forward today's `ValidateVersion`
  clamp; a corrupt version byte must never select a descriptor by indexing.
- **Bounds-checked decode**: every `unsafe` field read validates `offset + size <= len(buf)`
  before the cast (preserving today's `validateFieldAccess` in `dcfhfix`). The generic
  `readField[T]` keeps this check internal / non-bypassable (unexported or always-on), so
  centralising all `unsafe` casts in `pkg/format` cannot regress the out-of-bounds protection
  the G103 exclusion currently relies on. Descriptor-declared offsets are validated against the
  actual buffer length — short/corrupt legacy files error, never over-read.
- **Write-current is owned, not passed**: `pkg/format` owns the current-version constant and all
  write paths source it from there. (`SetHeaderForWritableIndex` takes `version` as a parameter
  today — the invariant must be enforced in `pkg/format`, not left to callers.)
- **Dedup correctness boundary**: widening the key stops *future* under-reporting only.
  Pre-existing v3 entries already truncated `Dev`/`Ino` at ingest (`binary_entry_scan.go:69-70`)
  — those high bits are permanently lost and recovered only when a re-scan rewrites the entry to
  v4. Accepted degradation: the version bump naturally rewrites entries on the next `update`; no
  separate migration tool. State this explicitly so it is a decision, not a surprise.

## Constraints
- **One place**: all versioned format code lives in `pkg/format`; nothing else hand-codes
  widths, offsets, or version layout (explicit user directive).
- **Host-order zero-copy** preserved for the current version; no fixed-endian rewrite.
- Format invariants unchanged: 8-byte alignment, SHA-1 footer, git-compatible header.
- British spelling in prose; no superlatives.

## Decomposition Check
- [ ] **Time**: Borderline >1 week as a single task.
- [x] **Complexity**: 3+ concerns — package extraction, version-aware codec, widening + v4.
- [x] **Risk**: On-disk format change + multi-version decode affect data integrity.
- [x] **Independence**: The no-behaviour-change extraction lands and verifies before any
  width/version change.

**Outcome**: 3 signals → decompose. The `pkg/format` public contract defines the seams:
- **3.1 (chore/refactor)**: Create `pkg/format`; vocabulary + exported canonical `Entry` + header
  + offset table + generic codec; migrate core package and dcfhfix; **delete both dcfhfix
  duplicates** (entry struct + `indexHeader`) and the parallel offset table. Ripples through the
  ~10 `BinaryEntryInterface` Dev/Ino implementers (informs the alias-vs-named choice — a named
  type forces conversions at every call site, an alias does not). No width, behaviour, or version
  change. **Gate before 3.2/3.3**: a byte-for-byte round-trip test (read an existing v3 index →
  re-serialise via `pkg/format` → identical bytes) proving the extraction is behaviour-preserving.
- **3.2 (feature)**: Add the version registry + read-old/write-new decode path to `pkg/format`
  (concrete registered per-version decoders); dcfhfix consumes them. No width change yet. *May
  fold into 3.3* if the decode path stays a single concrete v3-entry decoder — decided at subtask
  creation.
- **3.3 (bugfix)**: Widen `Dev`/`Ino` to 64-bit via the vocabulary; bump format version; fix
  dupes key + ingest casts; re-enable G115. Verify 3.1/3.2 introduced no *new* narrowing casts
  that the re-enabled G115 would then have to chase across already-merged code.

## Validation
- [ ] Design review completed (plan-review subagents, Step 8)
- [ ] Architecture approved by user
- [ ] Integration points verified (core package + dcfhfix both import `pkg/format`)

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
The design held end-to-end. `pkg/format` is the single owner of the on-disk layout (vocabulary,
canonical Entry/Header, bounds-checked codec, version registry); both dcfhfix duplicates and the
parallel offset table were deleted. The read-old/write-new model shipped as designed: version-gated
zero-copy cast for current files, heap transcode for legacy. The "concrete over generic" call (one
registered decoder, not a field-table engine) and the alias-vs-named-type guidance both proved correct.
The "first divergent entry layout" prediction was exactly right — the v4 widen is where entry layout
first diverges, and per-version offset descriptors were genuinely new code.

## Lessons Learned
The alias-method constraint (Go forbids methods on an alias to an out-of-package type) should have been
named in the design rather than discovered at compile time in 3.1 — low cost, but a planned step beats a
surprise. Otherwise the cross-subtask seam definition was the highest-leverage part of the whole task.
Full synthesis in j-retrospective.md.
