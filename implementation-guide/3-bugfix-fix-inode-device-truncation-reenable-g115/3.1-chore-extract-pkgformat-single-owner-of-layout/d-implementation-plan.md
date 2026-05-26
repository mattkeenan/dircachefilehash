# Extract pkg/format single owner of layout - Implementation Plan
**Task**: 3.1 (chore)

## Task Reference
- **Task ID**: internal-3.1
- **Task URL**: N/A (internal task)
- **Parent Task**: 3
- **Branch**: chore/3.1-extract-pkgformat-single-owner-of-layout
- **Template Version**: 2.1

## Goal
Move the on-disk layout into a new `pkg/format` package and migrate the core package + dcfhfix
onto it, deleting the duplicates — behaviour, widths, and on-disk bytes unchanged.

## Workflow
Patterns first → Test → Minimal impl → Refactor green → Commit message explains "why"

## Key technique: aliases make call sites transparent — but methods MOVE
- **Vocabulary as aliases at current widths**: `type DevID = uint32`, `type Inode = uint32`,
  `type FileMode = uint32`, `type WallTime = uint64`, `type EntrySize = uint32`,
  `type HashKind = uint16`, … A Go alias is *interchangeable* with its underlying type, so
  switching accessor signatures (`Dev() (uint32,…)` → `Dev() (DevID,…)`) and field types is a
  **no-op for all consumer sites** — they keep compiling. Width flip deferred to 3.3.
- **Core-package alias to the moved type**: in `pkg/` add `type binaryEntry = format.Entry` and
  `type indexHeader = format.Header`. This makes *references* and *method calls*
  (`be.Dev()`, `h.SetHeader(…)`) compile unchanged — `be` *is* a `format.Entry`.
- **CRITICAL — methods cannot stay on the alias.** Go forbids declaring a method whose receiver
  base type lives in another package. So **every receiver method on `binaryEntry`/`indexHeader`
  must physically move into `pkg/format`** (callers still work via the alias). Three consequences:
  - **`binaryEntryRef` STAYS in core** — it references the core `mmapIndexFile` type, so it (and
    `GetBinaryEntry`/`createBinaryEntryRef`) cannot move without an import cycle. It operates on
    `*format.Entry`. Only the *pure layout* type `Entry` + its self-contained methods move.
  - **`asFilterEntry` (`filter.go:105`) becomes a free function in core** — it returns core types
    (`FilterEntry`, `binaryEntryAdapter`), so it cannot move to `format`. Rewrite as
    `func asFilterEntry(be *format.Entry) FilterEntry` and update call sites.
  - **Unexported methods called from core won't link** once moved (e.g. `header.setClean()` from
    `temp_index_writer.go:179`; `isClean`/`clearClean`). Per method, either export it
    (`SetClean`) or reimplement in core as a free function over the exported `Flags` field
    (`h.Flags |= format.IndexFlagClean`). Decide per method; default to the free-function form to
    keep the `format` API minimal.
- **Field names are preserved**: `format.Entry` keeps `CTimeWall`/`MTimeWall`/`HashType`/
  `EntryFlags` (the design's `CTime`/`HashKind`/`Flags` were conceptual). No cosmetic rename in
  3.1. `EntryFlags` stays `uint16` on the struct with the existing `uint32` accessor + deliberate
  narrowing at `entry_serialiser.go:137` — not reconciled here (width concern → 3.3).
- **Net effect**: "create package + move type definitions *and their methods* + add aliases +
  free-function the two exceptions + fix imports". The byte-for-byte round-trip + full suite prove
  behaviour is preserved.

## Files to Modify
### New package `pkg/format/` (import `.../pkg/format`; MUST NOT import the core package — cycle)
- Vocabulary aliases; canonical `Entry` (from `binaryEntry`) + **all its pure methods** +
  build-time layout assertions + `validateLayout`; canonical `Header` (from `indexHeader`) +
  **all its methods** (`SetHeader`, `SetHeaderForWritableIndex`, `ValidateSignature`,
  `ValidateByteOrder`, `ValidateVersion`, clean-bit ops — see visibility note above);
  `headerSizeForVersion` (+ keep `HeaderSizeForVersion` re-export reachable for external callers).
- **Two-tier bounds-checked codec** (replaces dcfhfix's hand-typed `*(*uintXX)` reads + offset
  table). Must preserve BOTH tiers of today's `SafeEntryAccessor`, not the weaker design phrasing:
  1. **entry-level**: `Size` read after a 4-byte check, then `Size` non-zero / `>= minEntrySize`
     / `<= 4096` / `offset+Size <= len(data)` → yields `maxOffset`;
  2. **field-level**: every field read checks `fieldOffset+size <= maxOffset` (the entry's
     declared end — **not** merely `len(buf)`).
  Scope: only `readField[T]`/`writeField[T]` + entry (de)serialise helpers. **No** version
  descriptor / `Decode(version,…)` machinery — that is 3.2.
- **Layout constants moved**: `HeaderSize`, **`V2HeaderSize`**, `ChecksumSize`, **`ByteOrderMagic`**,
  hash-type consts (+ `HashSize*`), `IndexFlag*` (incl. `IndexFlagClean`), `EntryFlag*`,
  `CurrentIndexVersion`, `MinIndexVersion`, `TimestampMinVersion`.

### Core package (`pkg/`, package `dircachefilehash`)
- `pkg/binary_entry.go` — `binaryEntry` struct + **its pure methods** + assertions move to
  `format`; leave `type binaryEntry = format.Entry` alias for callers. `binaryEntryRef` + its
  methods **stay** (reference `mmapIndexFile`). `asFilterEntry` → free function (see technique).
- `pkg/index.go` — `indexHeader` + **its methods** move to `format` (alias `type indexHeader =
  format.Header`); `headerSizeForVersion`/`HeaderSizeForVersion` re-export reachable for the call
  sites (218, 248, 386, 400, 661, 667, 775). Clean-bit calls (e.g. `temp_index_writer.go:179`
  `setClean`) resolved per the visibility note.
- `pkg/constants.go` — **split**: the layout constants enumerated in the `pkg/format` section
  (incl. `V2HeaderSize`, `ByteOrderMagic`) move to `format`; operational constants (contexts,
  file-naming) stay.
- `pkg/binary_entry_interface.go` (Dev/Ino:39-40), `pkg/filter.go` (interface:25, adapter:112,71,641),
  `pkg/binary_entry_skiplist.go` (104/117/113/126), `pkg/binary_entry_scan.go` (156/166/162/172),
  `pkg/binary_entry_index_file{,_mmap}.go` (171/184/180/193, 108/121/117/130),
  `pkg/dcfhfind_support.go` (41/139), `pkg/scan.go` (295, Dev-only), `pkg/entry_serialiser.go`
  (105/108, round-trip path), `pkg/index.go` (502/503 logs) — accessor/field types → vocabulary
  aliases (transparent under aliasing).
- `pkg/metastore.go` (185 `TimestampMinVersion`, 215 `CurrentIndexVersion`) — reference the moved
  constants.

### dcfhfix (`cmd/dcfhfix/`, package main)
- `safe_entry_accessor.go` — **delete** duplicate `binaryEntry` (10) + offset table (36-50);
  `GetDev/GetIno` etc. read via `format` codec.
- `main.go` — **delete** duplicate `indexHeader` (23, **96 bytes**); use `format.Header`
  (**104 bytes**). NOTE: this fixes a latent **8-byte over-read** at `main.go:1537`
  (`(*[HeaderSize=104]byte)(unsafe.Pointer(customHeader))` over a 96-byte struct) and therefore
  **changes the trailing 8 bytes dcfhfix writes** — an intended correction, so the gate must
  exercise the dcfhfix *write* path and verify the new bytes are correct (Timestamp/padding), not
  merely identical to the old buggy output. (1378/1420/1544 refs.)
- `validated_entry.go` (57/62), `entry_append_remove.go` (65/76/77/203/250),
  `entry_workflow_main.go`, `entry_processor_workflow.go` — use `format` types/codec.
- `entry_processor_workflow.go:116,132` — stray hand-typed `*(*uint32)(unsafe.Pointer(...))` reads
  (guarded today) NOT routed through `SafeEntryAccessor`. Migrate to the codec so "single owner of
  unsafe reads" holds; if left raw, record why here.

### Out of scope for 3.1 (transient, not on-disk layout — CONFIRMED distinct from `Entry`)
- `pkg/walker_wire.go:185` (`scannedPath` + `syscall.Stat_t`), `pkg/wire_handler.go:312` (wire
  message `m.Dev`), `pkg/index_loading.go:20-21` (`cachedStat`, type at `metastore.go:62`, lowercase
  `dev/ino`) — verified separate types, not the `Entry` layout. Their widths are a 3.3 truncation
  concern; **untouched here**.

## Implementation Steps
### Step 1: Baseline
- [ ] Record G115 site count = **63** (current). Mechanism: temporarily comment `.golangci.yml`
      lines 59-60 (the G115 exclude) and run `golangci-lint run ./...`; revert. Note: the *count*
      is the invariant; individual finding *locations* will shift as casts move into `pkg/format`.

### Step 2: Create `pkg/format`
- [ ] Vocabulary aliases (current widths); canonical `Entry`/`Header` + **all their methods**
      (clean-bit ops resolved per visibility note) + layout assertions + moved layout constants
      + `headerSizeForVersion` + the **two-tier** bounds-checked codec. Compiles standalone
      (no core import). Unit tests for both bounds tiers, incl. the undersized-`Size` case.

### Step 3: Migrate core package
- [ ] Add `type binaryEntry = format.Entry` / `type indexHeader = format.Header`; convert
      `asFilterEntry` to a free function; resolve clean-bit calls; switch accessor/field types to
      vocabulary aliases; point moved-constant references at `format`. `binaryEntryRef` stays.
      `go build ./pkg/...` + `go test ./pkg/...` green.

### Step 4: Migrate dcfhfix
- [ ] Delete both duplicates + offset table; import `format`; route field access (incl. the
      `entry_processor_workflow.go` stray reads) through the codec. `go build ./cmd/...` +
      `go test ./cmd/...` green.

### Step 5: Gates
- [ ] **v3** byte-for-byte round-trip (read → re-serialise → identical bytes).
- [ ] **v2** round-trip from a **synthesised** fixture (tooling writes only v3): assert entry data
      parses at offset **88** (`V2HeaderSize`), not 96 — byte-identity alone won't catch a
      symmetric read/write offset error. Preserve the checksum ordering (`index.go:296-314`,
      `temp_index_writer.go:213`) verbatim.
- [ ] dcfhfix **write-path** round-trip: header bytes correct under `format.Header` (the 1537
      over-read fix is reflected, not regressed).
- [ ] Malformed-input negative tests error (no panic/over-read): truncated entry region,
      `Size`<min, `Size` overruns buffer, **valid buffer but entry `Size` < field offset**.
- [ ] Full suite green; G115 count == 63 (unchanged vs baseline).

## Code Changes
Illustrative (full edits during exec):
```go
// pkg/format/vocabulary.go (3.1: aliases at CURRENT widths; 3.3 flips DevID/Inode to uint64)
type DevID = uint32
type Inode = uint32

// pkg/binary_entry.go (core) — canonical def now lives in format
type binaryEntry = format.Entry
```

## Test Coverage
**See e-testing-plan.md** — round-trip v2/v3 fixtures, codec bounds-check unit tests, full
regression suite, G115-baseline diff.

## Validation Criteria
- `grep` confirms a single definition of layout/vocabulary/version logic (in `pkg/format`); no
  stray hand-typed `unsafe` layout reads remain outside the codec (incl. the dcfhfix strays).
- Both dcfhfix duplicates + offset table deleted; dcfhfix imports `pkg/format`.
- v3 + (synthesised) v2 round-trip pass, with the v2 parse-offset assertion; dcfhfix write-path
  header correct.
- Codec preserves both bounds tiers (entry-level `maxOffset` + field-level against `maxOffset`),
  proven by the undersized-`Size` negative test.
- Full suite green; G115 count == 63 (unchanged).

## Scope Completion
**IMPORTANT**: Complete all planned implementation before marking task Finished.

Deferring work creates technical debt and scope creep. Task 37 deferred documentation updates,
marked the task complete anyway, and created Task 38 to fix the deferred work.

**If you must defer work**:
1. Get user approval with clear rationale
2. Update success criteria to reflect descoped work
3. Create follow-up task immediately
4. Document deferral in Actual Results section

## Status
**Status**: Finished
**Next Action**: /cwf-testing-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Executed as planned (see f-implementation-exec.md, commit 65b863c). Two review-flagged blockers
(methods on an alias to an out-of-package type; cross-package unexported method calls) handled as
anticipated: methods moved into `pkg/format`; `asFilterEntry` made a free function; clean-bit
methods exported. `binaryEntryRef` stayed in core. Five deviations documented in f.

## Lessons Learned
Name the alias-method constraint in design next time, rather than discovering it at compile time.
See j-retrospective.md.
