# Move FileSize/ByteSize to int64 in v4 and core - Implementation Plan
**Task**: 4 (chore)

## Task Reference
- **Task ID**: internal-4
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/4-move-filesize-bytesize-to-int64-in-v4-and-core
- **Template Version**: 2.1

## Goal
Flip the `format.ByteSize` alias from `uint64` to `int64` and follow the
compiler-driven ripple — retyping the two `FileSize()` interfaces and the
size-threshold plumbing to signed — so the seven `Entry.FileSize`/`.Size()`
G115 suppressions added in Task 3.3 are retired by removing the conversions,
not by re-suppressing. No on-disk format change (v4 stays v4).

## Workflow
Patterns first → Test → Minimal impl → Refactor green → Commit message explains "why"

## Scope boundary (what this task does NOT touch)
The `fsdedupe` byte totals (`Result.BytesReclaimed/TotalPlanned/TotalReclaimed`
and `fileTarget.size`) are a **distinct `uint64` type** fed directly by
`st.Size()`, not by `Entry.FileSize`. They carry their own JSON contract
(`bytes_reclaimed`, …) and their 3 suppressions (`fsdedupe_linux.go:288,326,398`)
plus the 3 formatting casts in `cmd/dcfh/dupes.go:257,285,288` are **out of
scope** — flipping `ByteSize` does not reach them and must not add new
suppressions there. (Confirmed in planning; matches a-plan R3.)

## Files to Modify

### Primary — the type and the interfaces it must drag signed
- `pkg/format/vocabulary.go` — `ByteSize = uint64` → `int64` (the single
  load-bearing edit; update the alias comment). The `Entry`/v2/v3/v4 struct
  fields, codec `Get/SetFileSize`, and `transcode.go:116` all use the alias and
  follow transparently — **no edit** to those sites.
- `pkg/binary_entry_interface.go` — `FileSize() (uint64, error)` → `(int64, error)`
  on the `BinaryEntryInterface` (line 44). **The `needsHash` field-comparison
  slice (line 266) is typed `[]func(BinaryEntryInterface) (uint64, error)` and its
  FileSize closure (line 267) would stop compiling.** Do **NOT** mirror the
  sibling closures with `uint64(v)` — that adds a fresh int64→uint64 G115,
  defeating the task. Instead pull FileSize out of the slice and compare it as
  `int64` directly (the loop at lines 275-284 only does `ev != sv` equality, so
  an inline `existingEntry.FileSize() != scannedEntry.FileSize()` check before the
  slice is conversion-free). The remaining 5 closures stay `uint64` (UID/GID/Mode
  widen uint32→uint64 safely; CTime/MTime are native uint64 — no G115).
- `pkg/filter.go` — `FilterEntry.FileSize()` → `int64` (line 21);
  `binaryEntryAdapter.FileSize()` returns `a.e.FileSize` directly (line 109);
  `SizeTest.Evaluate` drops `size := int64(fs)` + its suppression (now `fs` is
  `int64`, and `SizeTest.Size` is **already** `int64`); `MinSizeTest.Min` and
  `MaxSizeTest.Max` `uint64` → `int64` (lines 272, 287).

### Primary — the FileSize() implementors (all seven must return int64)
*(Seven concrete methods: `binaryEntryAdapter` above, plus the six below. The
`needsHash` closure is a caller, not an implementor — handled in the interfaces
section.)*
- `pkg/binary_entry_scan.go` — `BEScanEntry.FileSize()` → `int64` (line 206,
  returns `entry.FileSize` directly); **line 74** `entry.FileSize = uint64(fileInfo.Size())`
  → `entry.FileSize = fileInfo.Size()` (drop cast + suppression — `Size()` is int64).
- `pkg/binary_entry_skiplist.go` — `BESkiplistEntry.FileSize()` → `int64` (line 169).
- `pkg/binary_entry_index_file_mmap.go` — `BEIndexFileMmapEntry.FileSize()` →
  `int64` (line 173, returns `entry.FileSize` directly). **(found in plan review)**
- `pkg/dcfhfind_support.go` — `EntryInfo.FileSize` `uint64` → `int64` (line 16);
  `entryInfoAdapter.FileSize()` → `int64` (line 37). The `> (1<<62)` corruption
  checks (lines ~251, 302) gain a `< 0` floor (negative = corrupt).
- `pkg/scan.go` — `scanFilterEntry.FileSize()` → `int64`, body
  `return e.info.Size(), nil` (drop `uint64(...)` cast + suppression, line 262).
- `pkg/mock_binary_entry_test.go` — `mockBinaryEntry.FileSize()` → `int64`
  (line 24; the field it returns, `m.size`, retypes to int64 too). **(found in
  plan review — a signature, not a literal)**

### Primary — signed size-threshold plumbing (forced by the interface flip)
- `pkg/flat_filter.go` — `ParseSizeBound` → `(int64, error)` using
  `strconv.ParseInt(digits,10,64)` and `math.MaxInt64/mult` overflow check
  (line 454; `mult` becomes `int64`); `SizeBoundString(int64)` (line 644) — its
  body also changes: `const k = uint64(1024)` → `int64`, and
  `strconv.FormatUint(u,10)` (line 656) → `strconv.FormatInt`. `FilterOptions.MinSize`/
  `MaxSize` `*uint64` → `*int64` (lines 29-30); the `*MinSize > *MaxSize` guard
  and `SizeBoundString` calls follow.
- `pkg/index.go` — `FindOptions.ExactSize` `*uint64` → `*int64` (line 514;
  compared to `entry.FileSize` at line 558 — equality, sign-safe). No callers set
  it, so zero ripple — but it is in scope and must flip to compile. **(found in
  plan review)**
- `cmd/dcfh/filters.go` — `n` (from `ParseSizeBound`) is now `int64`; `opts.MinSize = &n`
  / `MaxSize = &n` unchanged in shape (lines 220, 227).
- `cmd/dcfhfind/main.go` — `MinSizeTest{Min: n}` / `MaxSizeTest{Max: n}` follow
  (`n` from `ParseSizeBound` is int64; lines 481, 491).

### Primary — accumulation/validation suppressions retired
- `pkg/comparison_sink.go` — drop the three `int64(size)` casts + suppressions
  (lines 208, 242, 253); `size` is now `int64`, result fields are already `int64`.
- `pkg/metastore.go` — drop `int64(entry.FileSize)` cast + suppression (line 155);
  `totalSize` is already `int64`.
- `pkg/recovery.go` — `ValidationConfig.MaxFileSize` `uint64` → `int64` (struct at
  line 65, field at line 74; default `1<<62` at line 96 stays positive in int64);
  the validator (line 256) gains a **`entry.FileSize < 0`** rejection (the SC3
  corruption floor; the upper bound already exists). *(Plan-review naming fix: the
  struct is `ValidationConfig`, not `RecoveryConfig`.)*

### Supporting — JSON and tests
- `cmd/dcfhfix/entry_append_remove.go` — `EntryJSON.FileSize` `uint64` → `int64`
  (line 16; wire JSON unchanged — numbers); `entry.FileSize = entryJSON.FileSize`
  follows.
- `cmd/dcfhfix/validated_entry.go` — `fixed.FileSize = val` (line 156) where `val`
  comes from `parseUint64(value)` (line 152). After the flip this is `int64 = uint64`.
  Use a signed parse (or convert) so no new G115 appears. **(found in plan review)**
- `pkg/recovery_test.go` — the `largeSizeEntry.FileSize = 1 << 63` fixture **will
  not compile** (overflows int64); change to a valid oversize (e.g. `MaxFileSize + 1`)
  and add a **negative-size rejection** case for the new floor.
- Test fixtures fed from explicitly `uint64`-typed variables (NOT untyped
  literals) won't compile and must retype to int64: `pkg/flat_filter_test.go:12`
  (table field `size uint64`, with `min`/`max`) and `:407` (closure param
  `size uint64`); `cmd/dcfhfind/main_test.go:15` (`createMockEntry(… size uint64…)`).
  **(found in plan review)**
- Other `_test.go` `FileSize:` literals (1024/512/100/2048/4096/`0xdeadbeef`) are
  untyped constants that fit int64 — recompile clean; verify, no edits expected.

## Implementation Steps
### Step 1: Flip the alias, make the tree compile
- [ ] `ByteSize = int64` in `vocabulary.go` (+ comment); `go build ./...` to get
  the compiler's list of every non-alias mismatch.
- [ ] Retype both `FileSize()` interfaces and all seven implementors to `int64`;
  pull FileSize out of the `needsHash` slice (do not `uint64(v)` it).
- [ ] Retype the threshold plumbing (`ParseSizeBound`, `SizeBoundString`,
  `MinSize/MaxSize`, `MinSizeTest.Min`, `MaxSizeTest.Max`, `ExactSize`) to `int64`.
- [ ] Retype `EntryInfo.FileSize`, `EntryJSON.FileSize`,
  `ValidationConfig.MaxFileSize`, and the `validated_entry.go` parse.
- [ ] `go build ./... && go vet ./...` green.

### Step 2: Retire the conversions (the point of the task)
- [ ] Remove the 7 `//nolint:gosec` size suppressions whose cast no longer exists
  (binary_entry_scan:74, scan:262, filter:251, comparison_sink ×3, metastore:155),
  replacing `int64(x)`/`uint64(x)` with the direct value.
- [ ] Add the `entry.FileSize < 0` floor in `recovery.go` and the dcfhfind
  corruption checks (SC3).
- [ ] `golangci-lint run ./...` → **0 G115** whole-tree; confirm no suppression was
  merely relocated (grep that the 7 lines no longer carry `//nolint:gosec`).

### Step 3: Tests green
- [ ] Fix `recovery_test.go` oversize fixture + add negative-size case.
- [ ] `go test ./...` then the race gate
  `GOFLAGS=-gcflags=all=-d=checkptr=0 go test -race -short ./...`.
- [ ] Confirm v4 round-trip + v2/v3→v4 goldens still pass byte-identical (SC1/SC3).

## Code Changes
### The single load-bearing edit
```go
// pkg/format/vocabulary.go
- ByteSize   = uint64 // file size in bytes
+ ByteSize   = int64  // file size in bytes (off_t-style signed; os.FileInfo.Size())
```

### Input cast disappears
```go
// pkg/binary_entry_scan.go:74
- entry.FileSize = uint64(fileInfo.Size()) //nolint:gosec // G115: file size, non-negative
+ entry.FileSize = fileInfo.Size()
```

### Interface + SizeTest stop casting
```go
// pkg/filter.go
- FileSize() (uint64, error)
+ FileSize() (int64, error)
...
// SizeTest.Evaluate: delete `size := int64(fs)` + its suppression and use `fs`
// directly in ALL THREE cases (the local `size` is referenced at "+", "-", "=").
- size := int64(fs) //nolint:gosec // G115: file size, bounded by storage size (<< int64 max)
  switch t.Mode {
- case "+": return size > t.Size, nil
- case "-": return size < t.Size, nil
- case "=": return size == t.Size, nil
+ case "+": return fs > t.Size, nil   // fs is int64; SizeTest.Size is already int64
+ case "-": return fs < t.Size, nil
+ case "=": return fs == t.Size, nil
```

### Threshold parser goes signed (no new suppression)
```go
// pkg/flat_filter.go
- func ParseSizeBound(s string) (uint64, error) {
-     mult := uint64(1)
+ func ParseSizeBound(s string) (int64, error) {
+     mult := int64(1)
  ...
-     n, err := strconv.ParseUint(digits, 10, 64)
+     n, err := strconv.ParseInt(digits, 10, 64)
  ...
-     if mult > 1 && n > math.MaxUint64/mult {
+     if mult > 1 && n > math.MaxInt64/mult {
```

### SC3 corruption floor
```go
// pkg/recovery.go validator (~line 256)
+ if entry.FileSize < 0 {
+     return fmt.Errorf("file size %d is negative (corrupt)", entry.FileSize)
+ }
  if entry.FileSize > config.MaxFileSize {
```

### Negative-size handling at surviving comparison sites (plan review, FR4(e))
The signed reinterpretation means a corrupt on-disk size with bit 63 set now
reads back negative, which changes filter behaviour on corrupt input: a negative
`fs` silently passes `--max-size` (`neg <= max`) and fails `--min-size`
(`neg >= min` false) — the opposite of the old huge-positive behaviour. The
invariant that makes this safe in practice: filter inputs come from
`os.FileInfo.Size()` (non-negative) or a transcoded legacy `uint64 < 2⁶³`, and the
SC3 validator floor rejects negatives upstream. Action: add a one-line invariant
comment at the surviving comparison sites (`SizeTest`/`MinSizeTest`/`MaxSizeTest`
in `filter.go:246-296`) noting `fs` is a validated non-negative size — so the
removed cast's safety reasoning lives at the comparison, not just in the deleted
`//nolint`. Note also that flipping `MinSize/MaxSize` to `*int64` slightly widens
the JSON contract (`min_size`/`max_size` now parse negatives that `uint64`
previously rejected); the `*MinSize > *MaxSize` guard still holds and a negative
bound is harmless to matching, so no `>= 0` wire guard is added — recorded, not
deferred.

## Test Coverage
**See e-testing-plan.md for complete test plan.** In brief: existing v4/v2/v3
golden round-trips prove SC1/SC3 (byte-identical, signed reinterpretation);
a new negative-FileSize case proves the corruption floor; the dedup/dupes tests
prove no behavioural change; `golangci-lint run ./...` proves SC2.

## Validation Criteria
**See e-testing-plan.md.** Gate summary: `go build`/`vet`/full `go test` green;
race gate green; `golangci-lint run ./...` reports 0 G115 whole-tree and the
`--new` staged gate is clean; the 7 named lines no longer carry `//nolint:gosec`;
v4 goldens byte-identical.

## Scope Completion
**IMPORTANT**: Complete all planned implementation before marking task Finished.
The `fsdedupe`/`dupes.go` byte-total suppressions are deliberately and explicitly
out of scope (distinct type) — recorded here, not deferred silently.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
The edit map was accurate: the single load-bearing edit plus the enumerated ripple
(7 implementors, threshold plumbing, `ExactSize`, `validated_entry`, the `needsHash`
trap) all landed as planned across 27 files. Three sites the plan flagged as
"follows transparently" did so with no edits. Two unplanned-but-forced additions:
a `ParseSizeBound` sign guard (ParseInt accepts signs) and a `validateFileSizeBounds`
extraction (cyclop budget) — both documented in f-implementation-exec.md.

## Lessons Learned
The four-reviewer plan-review pass was decisive — it converted a "looks like one
line" change into a correctly-counted 27-file ripple before exec, so execution was
near-mechanical with 0 escaped defects.
