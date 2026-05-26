# Widen Dev/Ino to uint64 and re-enable G115 - Implementation Plan
**Task**: 3.3 (bugfix)

## Task Reference
- **Task ID**: internal-3.3
- **Task URL**: N/A (internal task)
- **Parent Task**: 3
- **Branch**: bugfix/3.3-widen-dev-ino-to-uint64-and-re-enable-g115
- **Template Version**: 2.1

## Scope of this plan
Execute the c-design-plan: widen `DevID`/`Inode` to `uint64` (format **v4**), add the legacy (v2/v3)
read-as-transcode path, widen the ingest/accessor/dedup sites the alias does not auto-propagate, make
`SafeEntry`/dcfhfix version-aware, delete dead `BEIndexFileIOEntry`, and re-enable gosec G115. The v4
bump is **atomic**: the tree does not compile or pass G115 cleanly until the whole set lands, so the
steps below are a logical ordering, not separate shippable commits.

## Goal
A v4 index that round-trips byte-identically, a v3 index that loads via heap transcode with every field
correct, `dcfh dupes` correct on >32-bit inodes, and `golangci-lint run ./...` clean with G115 active.

## Workflow
Patterns first → Test → Minimal impl → Refactor green → Commit message explains "why".

## Files to Modify
### Primary — `pkg/format` (the owned layout)
- `vocabulary.go:22-23` — `DevID`/`Inode` `= uint32` → `= uint64`; update the task-3.1 NOTE comment.
- `constants.go:17` — `CurrentIndexVersion` 3 → 4 (`MinIndexVersion` stays 2).
- `version_dispatch.go` — flip the legacy arm (`>= Min && < Current`, now {2,3}) from `DecodeZeroCopy`
  to `DecodeHeap`; `DecodeHeap` becomes a live value. Keep the default→`DecodeReject` arm.
- `entry.go` — add a build-time assertion pinning **legacy** struct sizing alongside the v4 ones
  (`:10-16`); replace the hand-coded `Size < 48` floor in `RelativePath` (`:97`) with
  `uint32(minEntrySize)`. (`48` is a loose sanity bound far below even the v3 struct minimum (~136),
  not the struct size — using `minEntrySize` makes it track the struct so it can't drift again.)
- `legacy_layout.go` (**new**) — `legacyEntry` struct (the pre-widen layout: `Dev`/`Ino` `uint32`,
  all else identical), its `unsafe.Offsetof`-derived offset set, and
  `layoutForVersion(version) (entryLayout, error)` (v4→current offsets, {2,3}→legacy, else→error).
- `transcode.go` (**new**) — `TranscodeLegacyIndex(legacyData []byte) ([]byte, error)`: read the legacy
  header (per its version's size) for `EntryCount`/`HashType`/flags, emit a complete **v4 image**
  (`SetHeaderForWritableIndex` + transcoded entries). Because it is exported and reusable standalone
  (not only behind the load path's `checkEntryRegionAccess`), it must **self-validate**:
  `len(legacyData) >= legacyHeaderSize` before the header cast, and each entry's `Size`/region length
  before read. The untrusted `EntryCount` must **never** size an allocation — grow the output buffer
  incrementally per validated entry (a crafted `0xFFFFFFFF` count is a memory-amplification vector).
- `codec.go` — make `SafeEntry` version-aware: `NewSafeEntry(data, entryIdx, offset, version)` selects
  its offset set via `layoutForVersion`; the field getters read from the selected layout.

### Primary — `pkg` (core load path + ingest)
- `index.go` — `checkEntryRegionAccess` returns the `DecodeStrategy`; both loaders
  (`openAndValidateIndex`→`collectEntryRefs`, `loadIndexFromFileWithTracking`→`parseTrackedEntries`)
  branch on it: `DecodeHeap` → `TranscodeLegacyIndex`, munmap the original, wrap the image in a
  **heap-backed** `mmapIndexFile`. Add `heapBacked bool` to the struct; `Cleanup()` skips `unix.Munmap`
  when set.
- `binary_entry_scan.go:69-70` — drop the `uint32(...)` casts (`entry.Dev = statInfo.Dev`, `.Ino = …`).
- `scan.go:295,300` — `scanFilterEntry.Dev()` signature `uint64` and `return uint32(sys.Dev)` → `return
  sys.Dev`.
- `dupes.go:253-256` — dedup key `[2]uint32`/`map[[2]uint32]` → `[2]uint64`/`map[[2]uint64]` (value
  array, not a struct key).

### Primary — accessor interfaces + implementers (hand-typed, NOT alias-propagated)
- `binary_entry_interface.go:39-40` — `BinaryEntryInterface.Dev()/Ino()` return `uint64`.
- `filter.go:25` — `FilterEntry.Dev()` returns `uint64`; impl `:113`.
- Implementers (widen `Dev()` **and** `Ino()` where present): `binary_entry_skiplist.go`,
  `binary_entry_index_file_mmap.go`, `binary_entry_scan.go`, `pkg/dcfhfind_support.go:41`.
  (`FilterEntry`/`EntryInfo` expose only `Dev()`, no `Ino()` — widen `Dev` only, do not invent `Ino`.)
- Backing fields: `pkg/dcfhfind_support.go:20` `EntryInfo.Dev uint32`→`uint64`;
  `cmd/dcfhfix/entry_append_remove.go:20-21` — `Dev uint32`→`uint64` and `Ino *uint32`→`*uint64`
  (a pointer with `omitempty`; update every deref/format/parse of it, not just the width).

### Primary — dcfhfix read path (version threading) + write path (v4 stamp)
dcfhfix is read-old / write-**v4**, so both the read offsets *and* the written header+entries must be v4.
- **Version into every `NewValidatedEntry`/`NewSafeEntry`** — source `header.Version` (validated),
  never a zeroed value: `cmd/dcfhfix/validated_entry.go:20`, `entry_append_remove.go:213`,
  `entry_append_remove.go:260`, `entry_processor_workflow.go:61`.
- **Write the v4 header, not the source header** (robustness): `createTempIndexWithHeader`
  (`entry_workflow_main.go:75`) copies the source header verbatim — so a repaired v3 file keeps
  `Version=3` while `appendValidatedEntryToTmpIndex` (`entry_processor_workflow.go:150`) now writes
  v4-shaped (+8/entry) structs → a corrupt file claiming v3. Stamp `CurrentIndexVersion` + the v4
  `HeaderSize` into the temp header.
- **Two dcfhfix struct-size floors** (the dcfhfix twins of the `entry.go:97` floor): the min-bytes
  guard in `finalizeTempIndex`'s count loop (`entry_workflow_main.go:119` — NOT a stride; the loop
  strides by `entry.Size` at `:127`) and the skip-floor in `trySkipToNextEntry`
  (`entry_processor_workflow.go:133`). After the widen both become the v4 minimum and over-reject a
  legitimate v3 entry — make them version-aware (legacy minimum for legacy files).

### Primary — dead code deletion
- Delete `pkg/binary_entry_index_file.go` (`BEIndexFileIOEntry` — no production callers, only the enum
  + tests) **and** trim its references so the package still compiles:
  `pkg/binary_entry_index_file_test.go`, `pkg/binary_entry_implementations_test.go:49-54`
  (`NewBEIndexFileIOEntry` + `testHashCoordinationMethods`), `pkg/binary_entry_hash_coordination_test.go:133`
  (`NewBinaryEntryBase(BEIndexFileIO)`), and the `BEIndexFileIO` enum constant + `String()` case
  (`binary_entry_interface.go:91-92,106-107`). Removing it drops a parallel v4 cast site.

### Primary — security gate
- `.golangci.yml:59-60` — remove `G115` from `linters.settings.gosec.excludes`.

### Supporting — tests (detailed in e-testing-plan)
- `pkg/format/*_test.go` (layout/transcode), `pkg/*_test.go` (v3-transcode load both loaders, v4
  round-trip, synthetic-file `Cleanup` no-munmap, dupes >32-bit ino), dcfhfix repair-read on v3.

## Implementation Steps
### Step 0: Capture the v3 golden — IRREVERSIBLE ordering, do FIRST
- [ ] Before *any* code change, generate `pkg/format/testdata/v3.idx` from the **real v3 writer** (the
      tree is still at baseline `CurrentIndexVersion`==3). Step 1 bumps the constant to 4 and the tool
      can no longer emit v3 — so this is a one-shot capture that must precede Step 1. Commit the golden.

### Step 1: Width + version + assertions + legacy layout (test first)
- [ ] Test: `layoutForVersion` table (v4→current, {2,3}→legacy, {0,1,5,0xFFFFFFFF}→error); legacy +
      v4 build assertions compile; `entry.go:97` floor equals `minEntrySize`.
- [ ] Widen `vocabulary.go`; bump `constants.go`; add `legacyEntry`/`layoutForVersion`; add legacy
      build assertion; fix the `Size<48` floor.

### Step 2: Legacy transcoder (test first)
- [ ] Test: a crafted v3 entry-region transcodes to a v4 image whose every field (Dev/Ino widened,
      Mode/UID/GID/FileSize/flags/hash/path) matches; a short/corrupt legacy region errors (no
      over-read); empty index → header-only image.
- [ ] Implement `TranscodeLegacyIndex`: walk via the **legacy** `Size`/offsets, emit recomputed v4
      `Size`+padding (`BESizeFromPathLen`), validate each `Size` and region length before reading.

### Step 3: Decode-on-load branch (both loaders) + cleanup guard
- [ ] `checkEntryRegionAccess` returns the `DecodeStrategy`.
- [ ] Both loaders: on `DecodeHeap`, transcode → munmap original → heap-backed `mmapIndexFile`
      (`heapBacked:true`, header = `(*indexHeader)(&image[0])`, `headerSize=HeaderSize`) → existing walk.
- [ ] Add `heapBacked bool`; guard `Cleanup()`'s `unix.Munmap`.
- [ ] **GC keep-alive**: `binaryEntryRef`s hold `unsafe.Pointer` into `Data`; for a heap image the
      refcount contract is the only thing keeping the buffer alive (a cast `unsafe.Pointer` is not a
      tracked GC root). Keep the image referenced for the index's lifetime — same contract as the mmap
      path; do not "optimise" the buffer away. The new transcode fork in `openAndValidateIndex` must
      run its error-cleanup like the other arms (note the pre-existing checksum arm at `index.go:352`
      returns without `cleanup()` — do **not** copy that pattern into the new branch).

### Step 4: Widen the hand-typed sites (compiler-driven)
- [ ] Widen the two accessor interfaces + every implementer (`Dev` and `Ino`) + backing struct fields;
      drop the ingest casts (`binary_entry_scan.go:69-70`, `scan.go:295` signature + `:300` cast);
      widen the dedup key. `go build ./...` green.

### Step 5: Version-aware SafeEntry + dcfhfix read/write path
- [ ] `NewSafeEntry`/`NewValidatedEntry` take `version`, sourced from the validated header (never a
      zeroed value) at all four call sites: `validated_entry.go:20`, `entry_append_remove.go:213`,
      `entry_append_remove.go:260`, `entry_processor_workflow.go:61`.
- [ ] Stamp `CurrentIndexVersion`+v4 `HeaderSize` in `createTempIndexWithHeader` (`entry_workflow_main.go:75`)
      so a repaired legacy file is written as v4 (matching the v4-shaped entries now appended).
- [ ] Make the two dcfhfix struct-size floors version-aware (`entry_workflow_main.go:119`,
      `entry_processor_workflow.go:133`).

### Step 6: Delete dead code
- [ ] Remove `pkg/binary_entry_index_file.go` + test; confirm no remaining references.

### Step 7: Re-enable the gate
- [ ] Remove the G115 exclude; `golangci-lint run ./...` (the enforcement path — never standalone
      gosec). Expect **more than the Dev/Ino sites** — `uint32(totalSize)`, `uint32(len(...))`,
      `uint32(unsafe.Sizeof(...))` etc. will surface (≈10+ candidates; the "three sites" the design
      names are specifically the Dev/Ino truncations this task *removes*, not the total finding count).
- [ ] Triage every site — fix, or `//nolint:gosec // G115: <rationale>` per-line (never blanket).
      **Hard rule**: no G115 suppression on any `Dev`/`Ino`/`EntryCount`/`Size` conversion — those are
      the truncation-prone fields this task exists to fix and must be resolved *structurally*. Only
      provably width-safe conversions elsewhere may carry a justified per-line suppression. ("G115
      clean" must be earned by the fix, not by suppressing the bug class.)

### Step 8: Regression
- [ ] `go test ./pkg/... ./cmd/...` and the canonical race gate
      `GOFLAGS="-gcflags=all=-d=checkptr=0" go test -race -short ./...` green; `dcfh dupes` correct on a
      synthesised >2^32-inode pair (end-to-end).

## Code Changes
### `mmapIndexFile.Cleanup()` — heap-backed guard (security-critical; explicit marker, not fd-nil)
```go
// mmapIndexFile gains: heapBacked bool // Data is a GC'd transcode buffer, not a mapping
func (mif *mmapIndexFile) Cleanup() error {
	mif.mutex.Lock()
	defer mif.mutex.Unlock()
	if mif.Data != nil && !mif.heapBacked { // never munmap a heap slice (UB)
		if err := unix.Munmap(mif.Data); err != nil {
			return fmt.Errorf("failed to unmap %s index: %w", mif.Type, err)
		}
	}
	mif.Data = nil
	// ... File.Close() unchanged (already nil-guarded) ...
}
```

### `layoutForVersion` — fail closed (a bogus version must not select v4 offsets over garbage bytes)
```go
func layoutForVersion(version uint32) (entryLayout, error) {
	switch {
	case version == CurrentIndexVersion:
		return currentLayout, nil
	case version >= MinIndexVersion && version < CurrentIndexVersion:
		return legacyLayout, nil
	default:
		return entryLayout{}, fmt.Errorf("no layout for index version %d", version)
	}
}
```

## Plan-review synthesis (Step 8)
Four reviewers verified every cited line and the load/ingest/repair paths. Applied: the dcfhfix section
was substantially expanded — **write the v4 header on repair** (a real corrupt-output bug: v3 header +
v4 entries), version threading at **all four** `NewValidatedEntry` sites, and **two** dcfhfix
struct-size floors (the twins of `entry.go:97`); the dead-code deletion now names the test files + enum
arm it must trim (else the package won't compile); the `entry_workflow_main.go` site reframed as a
guard *floor*, not a stride; `scan.go:295` signature added; `dcfhfind_support.go` repathed to `pkg/`;
the `Size<48` rationale corrected; the transcoder hardened to self-validate length and never size
allocation from untrusted `EntryCount`; Step 7 forbids suppressing the Dev/Ino/Size G115 class and
drops the "three sites" optimism; a GC keep-alive note added for the heap image. No unapplied findings
of substance.

## Test Coverage
**See e-testing-plan.md.** Summary: `layoutForVersion`/transcoder unit tables (incl. corrupt-input
negatives); v3→v4 transcode load via **both** loaders with full-field assertion + `DecodeHeap` routing
proof; v4 round-trip byte-identity; synthetic heap-backed `Cleanup()` no-munmap (under `-race`); dupes
>32-bit ino end-to-end; dcfhfix v3 repair-read (incl. the v4-header-stamp); G115 == clean.

## Validation Criteria
**See e-testing-plan.md.** Gate: a-task-plan success criteria — widened layout + v4 assertions; no
ingest truncation; v3 decodes via heap with every field correct; v4 byte-identical; dcfhfix resolver
adopted + `readEntryData` deleted; `golangci-lint run ./...` clean with G115 active; full suite green
incl. `-race`.

## Scope Completion
**IMPORTANT**: Complete all planned implementation before marking task Finished.

This is the **leaf** subtask — it has **no deferrals**. The items 3.2 deferred (concrete legacy
decoder, dcfhfix resolver adoption, `readEntryData` disposition, lockstep version ranges, G115
re-enable) are all in scope here and enumerated above. Marking 3.3 Finished closes the parent's Very
High backlog item.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All 8 steps executed. Step 0 golden captured first (irreplaceable). Deviations: three per-version layout
files instead of `legacy_layout.go`; whole-tree G115 (55 sites) instead of residual triage; `narrowDevIno`
width-aware getters added (not anticipated — `readField[DevID]` over-read legacy entries). Dead
`BEIndexFileIOEntry` deleted. dcfhfix v4-header stamp + version-aware size floors landed per the expanded plan.

## Lessons Learned
The plan-review-driven dcfhfix expansion (v4 header stamp, four version-threaded `NewValidatedEntry`
sites, two size floors) prevented a corrupt-output bug (v3 header over v4-shaped entries). The
compiler-driven alias widen worked, but hand-typed interface/struct sites needed the explicit enumeration
the c/d plans provided.
