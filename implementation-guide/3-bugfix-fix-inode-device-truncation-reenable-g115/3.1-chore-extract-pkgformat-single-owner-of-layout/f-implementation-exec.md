# Extract pkg/format single owner of layout - Implementation Execution
**Task**: 3.1 (chore)

## Task Reference
- **Task ID**: internal-3.1
- **Task URL**: N/A (internal task)
- **Parent Task**: 3
- **Branch**: chore/3.1-extract-pkgformat-single-owner-of-layout
- **Template Version**: 2.1

## Goal
Execute the extraction of `pkg/format` per d-implementation-plan.md / e-testing-plan.md.

## Actual Results

### Step 1 — G115 baseline
- **Planned**: record G115 site count by temporarily un-excluding it.
- **Actual**: baseline (reviewer-measured, pre-3.1) = 63. Post-extraction count = **52**
  (measured by commenting `.golangci.yml:59` and running `golangci-lint run ./...`, then
  reverting). The extraction **reduced** G115 by 11 (deleting dcfhfix's hand-typed offset-table
  reads + consolidating into the generic codec removed narrowing sites). No NEW G115 sites — the
  gate's intent (count must not go up) is satisfied.

### Step 2 — create `pkg/format`
- **Actual**: created `vocabulary.go` (same-width aliases: DevID/Inode/FileMode/UserID/GroupID/
  WallTime/ByteSize/RecordSize/FlagBits/HashKind), `constants.go` (layout constants + hash-name
  helpers), `entry.go` (canonical `Entry` + methods + build-time layout assertions),
  `header.go` (canonical `Header` + methods), `codec.go` (two-tier bounds-checked `SafeEntry` +
  generic `readField`/`writeField`). Package compiles standalone and imports **nothing** from the
  core package (verified via `go list` — cycle-free).

### Step 3 — migrate core package
- **Actual**: `type binaryEntry = format.Entry` and `type indexHeader = format.Header` aliases;
  moved layout constants re-exported from `pkg/format`; `headerSizeForVersion`/
  `HeaderSizeForVersion`/`HashTypeName`/`HashTypeFromName` thin forwarders; `asFilterEntry`
  converted to a free function; clean-bit method calls updated to the exported `SetClean`/
  `IsClean`/`ClearClean`; unused `time` import removed. `binaryEntryRef` stayed in core (it
  references the core `mmapIndexFile`). Core + tests green.

### Step 4 — migrate dcfhfix
- **Actual**: deleted `cmd/dcfhfix/safe_entry_accessor.go` (duplicate `binaryEntry` + offset
  table + accessor) and the duplicate `indexHeader` in `main.go`; added `format_aliases.go`
  (`binaryEntry`/`indexHeader` aliases); `validated_entry.go` uses `format.NewSafeEntry`. dcfhfix
  builds and its tests pass.

### Step 5 — gates
- **Actual**: full suite green (`go test ./...`: dcfh, dcfhfind, dcfhfix, pkg, fsdedupe). New
  `pkg/format/codec_test.go` proves both bounds tiers (tier-1 Size validation incl.
  zero/too-small/overrun/too-large; tier-2 path bounded by declared Size not buffer; truncated
  buffer errors). `gofmt` clean; `golangci-lint run ./pkg/format/` = 0 issues. Single-owner
  verified: no layout structs / offset tables remain outside `pkg/format`.

## Deviations from plan
1. **Deleted dead `RelativePathModern`** (was the only format→core tendril — it called
   `IsDebugEnabled`). Zero callers anywhere; removing it kept the moved method set cycle-free
   ("best part is no part"). Not a behaviour change (unused).
2. **Fixed a latent `GetPath` bug** while porting dcfhfix's accessor: the old dcfhfix `GetPath`
   read the path from the unused trailing `Path[8]` field (`offsetPath`=124), but the
   authoritative writer (`EntrySerialiser`) and `Entry.RelativePath` place the path at
   `sizeof(Entry)`=136. The codec now reads from `minEntrySize`, matching the writer (verified via
   `RelativePath` oracle). This corrects empty/short paths in dcfhfix repair output.
3. **dcfhfix header unification fixes a latent over-read**: the old 96-byte `indexHeader`
   duplicate, when cast to `[HeaderSize=104]byte` in the write path, over-read 8 bytes. Field
   offsets 0–92 are identical to `format.Header` (104 B), so adopting it is read-safe and
   preserves the v3 `Timestamp`.
4. **`entry_processor_workflow.go:116,132` stray `*(*uint32)` reads kept**: these read only the
   first 4 bytes (Size) for entry-skipping during corruption recovery, with their own
   `*offset+4 <= len(data)` guards. They do not duplicate the layout/offset table, so they are an
   accepted carve-out rather than forced through the codec.
5. **Round-trip integration tests (TC-1/TC-2/TC-3) deferred to g-testing-exec**: they require
   building real v2/v3 index files (writer scaffolding) and naturally belong to the testing-exec
   phase. The security-critical codec bounds tests (TC-4/TC-5/TC-6) are landed here.

## Blockers Encountered
None unresolved. The two review-flagged blockers (cannot define methods on an alias to an
out-of-package type; cross-package unexported method calls) were handled exactly as the plan
anticipated: methods moved into `pkg/format`; clean-bit methods exported.

## Deferral Check
- [x] All d-implementation-plan.md steps executed (round-trip *integration* tests → g phase).
- [x] Success criteria met: single owner (grep-verified), both dcfhfix duplicates deleted, full
      suite green, G115 not increased.
- [x] c-design constraints honoured: host-order zero-copy preserved, `pkg/format` cycle-free,
      bounds-check invariant preserved (two tiers).
- [x] Deferred work (round-trip integration tests) tracked in g-testing-exec, not dropped.

## Security Review

**State**: no findings

The codec preserves both bounds tiers and the unsafe casts in readField/writeField are gated by validateFieldAccess against maxOffset (the declared entry end), not len(data); no new narrowing/overflow conversions were introduced (vocabulary types are same-width Go aliases). One FR4(e) note for the record, not a finding: the new `GetPath` (/home/matt/repo/dircachefilehash/pkg/format/codec.go:186-203) intentionally diverges from the deleted accessor — it reads the path from `offset+minEntrySize` (after the fixed struct, matching the authoritative writer) rather than from the unused trailing `Path[8]` field, fixing a latent empty-path bug. It is bounds-safe here because the slice `data[pathStart:se.maxOffset]` is guarded by `pathStart >= se.maxOffset` and `maxOffset <= len(data)` is established in tier-1, so the slice cannot exceed the backing buffer; the NUL scan stays within that slice. Unlike the other readers it does not call `validateFieldAccess`, but it does not need to since it derives its own bound from `maxOffset` directly — audit any future change that lets `pathStart` be computed from an untrusted field rather than the compile-time `minEntrySize`, where that invariant would no longer hold.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Lessons Learned
*To be captured during retrospective*
