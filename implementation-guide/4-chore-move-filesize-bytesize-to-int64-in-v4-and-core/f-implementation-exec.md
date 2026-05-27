# Move FileSize/ByteSize to int64 in v4 and core - Implementation Execution
**Task**: 4 (chore)

## Task Reference
- **Task ID**: internal-4
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/4-move-filesize-bytesize-to-int64-in-v4-and-core
- **Template Version**: 2.1

## Goal
Execute the implementation following d-implementation-plan.md and e-testing-plan.md:
flip `format.ByteSize` from `uint64` to `int64`, follow the compiler-driven ripple,
and retire the seven Task-3.3 size G115 suppressions by removing the conversions.

## Actual Results

### Step 1: Flip the alias, make the tree compile
- **Planned**: `ByteSize = int64`; let `go build` enumerate the non-alias ripple;
  retype both `FileSize()` interfaces, all seven implementors, the threshold
  plumbing, `EntryInfo`/`EntryJSON`/`ValidationConfig`, and the `validated_entry`
  parse.
- **Actual**: Single edit at `pkg/format/vocabulary.go:31`. `go build` surfaced
  exactly the predicted set plus three sites the plan had already flagged as
  "follows transparently" (`entry_serialiser.go:120`, `filter.go:67`
  `materialiseEntryInfo`, and the iterator/skiplist interface-assignment sites) —
  all resolved once the implementors returned `int64`, no extra edits.
  - Interfaces: `BinaryEntryInterface.FileSize` (binary_entry_interface.go:44),
    `FilterEntry.FileSize` (filter.go:21) → `int64`.
  - Seven implementors → `int64`: `binaryEntryAdapter` (filter.go),
    `entryInfoAdapter` (dcfhfind_support.go), `scanFilterEntry` (scan.go, body now
    `return e.info.Size(), nil`), `BEScanEntry` (binary_entry_scan.go; also `:74`
    `entry.FileSize = fileInfo.Size()`), `BESkiplistEntry`, `BEIndexFileMmapEntry`,
    `mockBinaryEntry` (test).
  - `needsHash` (binary_entry_interface.go): FileSize pulled **out** of the
    `[]func(...) (uint64,error)` slice and compared inline as `int64` — the
    equality-only loop makes this conversion-free (no `uint64(v)`, no new G115).
  - Threshold plumbing → `int64`: `ParseSizeBound`, `SizeBoundString`,
    `FilterOptions.MinSize/MaxSize`, `MinSizeTest.Min`, `MaxSizeTest.Max`,
    `FindOptions.ExactSize`, `cmd/dcfh/dupes.go dedupeDefaultMinSize`.
    `cmd/dcfh/filters.go` and `cmd/dcfhfind/main.go` followed with no edits.
  - JSON/validate: `EntryInfo.FileSize`, `EntryJSON.FileSize`,
    `ValidationConfig.MaxFileSize` → `int64`; added `parseInt64` mirroring
    `parseUint64` (both `file_size` callers repointed) so the dcfhfix repair path
    parses signed end-to-end with no `uint64→int64` cast.
- **Deviations**:
  1. **`ParseSizeBound` sign guard (added)**: switching `strconv.ParseUint`→
     `ParseInt` would have *accepted* `"-1"`/`"+1"` (ParseInt allows a leading
     sign), silently widening the documented unsigned-magnitude surface and
     breaking `TestParseSizeBound`'s `wantErr` cases. Added an explicit
     leading-sign rejection to preserve the prior behaviour exactly.
  2. **`dedupeDefaultMinSize` (cmd/dcfh/dupes.go)** was a *typed* `uint64` const
     feeding `FilterOptions.MinSize` — flipped to `int64`. This is filter
     plumbing (in scope), distinct from the fsdedupe byte totals (out of scope).

### Step 2: Retire the conversions (the point of the task)
- **Planned**: delete the 7 size suppressions; add the `< 0` corruption floor;
  whole-tree 0 G115; confirm no suppression merely relocated.
- **Actual**: all 7 retired by removing the cast (binary_entry_scan:74, scan:262,
  filter SizeTest, comparison_sink ×3, metastore:155). Negative-size floor added
  in `recovery.go` (via new `validateFileSizeBounds` helper) and the two
  `dcfhfind_support.go` corruption checks (`< 0 || > 1<<62`). Invariant comments
  added at the surviving `filter.go` comparison sites (FR4(e)).
  `golangci-lint run ./...` → **0 G115** whole-tree; `golangci-lint run --new ./...`
  → **0 issues**.
- **Deviations**:
  - **`validateFileSizeBounds` extracted (recovery.go)**: the naive inline
    `< 0` branch pushed `validateEntryLogical` to cyclomatic complexity 21 (cyclop
    max 20). Extracting both size checks into a tiny single-responsibility helper
    keeps the distinct "negative (corrupt)" fail-closed message *and* drops the
    function back under the limit. Cleaner than the planned inline `if`.

### Step 3: Tests green
- **Planned**: fix the `1<<63` oversize fixture; add a negative-size case;
  full suite + race; v4/v2/v3 goldens byte-identical.
- **Actual**:
  - `recovery_test.go`: `largeSizeEntry.FileSize = 1<<63` (overflows int64) →
    `(1<<62)+1`; added `NegativeFileSize` subtest (`FileSize = -1`, expects
    exclusion). Both pass under `TestRecoveryValidationProcessor`.
  - Retyped uint64 fixtures: `TestEntryData.FileSize`, `mockBinaryEntry.size`,
    `flat_filter_test` table/closure vars, `legacy_load_test` `wantSizes`,
    `dupes_test` `u64`/`extractMinSize`/`user`, `createMockEntry`, `scan_filter_test`.
  - Three now-redundant `int64(testData.FileSize)` casts removed (unconvert).
  - `go test ./...` green; race gate
    `GOFLAGS=-gcflags=all=-d=checkptr=0 go test -race -short ./...` green.
  - SC tests verified by name: `TestRoundTrip_V4_ByteIdentical`,
    `TestRoundTrip_HeaderSizeInvariant` (SC1); `TestGolden_V3_DecodesToV4`,
    `TestTranscodeLegacyIndex_Positive`, `TestRecoveryValidationProcessor/NegativeFileSize`
    (SC3); `TestMinMaxSizeTest`, `TestParseSizeBound`, `TestSizeBoundString` (filters).

## Success Criteria Status
- **SC1** (signed, no format bump): ✅ `ByteSize` is `int64`; v4 round-trip
  byte-identical; `CurrentIndexVersion` unchanged.
- **SC2** (size G115 retired): ✅ 7 suppressions removed by deleting the cast;
  0 G115 whole-tree; `--new` clean. The retained "file size" suppressions are
  mmap-offset arithmetic (binary_entry.go) and the out-of-scope fsdedupe `uint64`
  totals — neither is a FileSize conversion.
- **SC3** (legacy correct, corruption fail-closed): ✅ v2/v3→v4 goldens decode
  unchanged; negative size rejected by validator + dcfhfind corruption checks.
- **SC4** (suite + race): ✅ both green across every package.

## Blockers Encountered
None.

## Deferral Check
- [x] All steps from d-implementation-plan.md executed
- [x] All success criteria from a-task-plan.md met (SC1–SC4)
- [x] b/c plans N/A for chore
- [x] No planned work deferred. fsdedupe byte-total suppressions + dupes.go
  formatting casts remain out of scope **by design** (distinct `uint64` type,
  own JSON contract) — recorded in d-plan, not silently deferred.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Lessons Learned
- The compiler ripple matched the four-reviewer-amended d-plan precisely; the only
  unplanned edits were two preserve-behaviour guards (`ParseSizeBound` sign
  rejection, `validateFileSizeBounds` extraction for cyclop), both forced by the
  signed flip rather than missed in planning.
- `strconv.ParseInt` is not a drop-in for `ParseUint`: it accepts a leading sign.
  A signedness flip on a parser surface must re-check the accepted grammar.

## Security Review

**State**: no findings

no findings: empty changeset

The implementation changeset is entirely Go source (production + tests) plus this
markdown workflow file. `security-review-changeset --phase=implementation` reports
`reviewed 0 files, 0 lines` (anchor=79e4bc5): none of the changed paths match CWF's
security-relevant classifier (CWF-internal directories or shebang-interpreted
scripts), so there is nothing for the changeset subagent to review. The semantic
threat surface of this task — the signed reinterpretation of an on-disk field and
its effect on filter/validation behaviour (FR4(e)) — was reviewed in the d-plan
security pass, which produced the negative-size corruption floor (`recovery.go`
`validateFileSizeBounds`, `dcfhfind_support.go` corruption checks) now implemented
and tested (TC-4).
