# Reconcile RelativePath pathStart offset - Implementation Plan
**Task**: 24 (bugfix)

## Task Reference
- **Task ID**: internal-24
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: bugfix/24-reconcile-relativepath-pathstart-offset
- **Template Version**: 2.1

## Goal
Correct both `pkg/format/entry.go` readers that assume "Path is the last 8 bytes"
— `calculatePathLength()` (Decision 1) and `validateLayout()` (Decision 4) — so
`ValidateEntry()` genuinely validates again, and pin the behaviour with positive
and negative regression tests.

## Corrected Bug Model (measured, not assumed)
`Sizeof(Entry)=144`, `Offsetof(Path)=132` → 4 bytes tail padding; path data is
written at `144`. So `&be.Path[0]` = `Sizeof-12`, and the over-count is **12
bytes** (len 1→13, len 7→19, len 21→33, measured). `validateLayout` asserts
`Offsetof(Path) == Sizeof-8 (136)`, which is false → panics on every valid entry →
swallowed by `ValidateEntry`'s `recover()` → `ValidateEntry` always returns `nil`
today.

## Workflow
Patterns first → Test → Minimal impl → Refactor green → Commit message explains "why"

## Files to Modify
### Primary Changes
- `pkg/format/entry.go`:
  - `calculatePathLength()` (lines 126-149): replace body with
    `return len(be.RelativePath())`; delete duplicate unsafe arithmetic + its
    `//nolint:gosec` G115 site; one-line contract comment.
  - `validateLayout()` (lines 67-86): change `expectedOffset := unsafe.Sizeof(*be)
    - 8` → `expectedOffset := unsafe.Offsetof(be.Path)`; correct the comment to
    describe the real layout (Path at `Offsetof(Path)`, 4-byte tail pad, path data
    at `Sizeof`).

### Supporting Changes
- `pkg/format/entry_test.go` (new file, package `format`) — positive + negative
  regression tests using `layEntry` (defined in `roundtrip_test.go`, same package).

## Implementation Steps
### Step 1: Confirm invariants (measure twice)
- [ ] Grep-confirm `calculatePathLength` still has exactly one caller
      (`ValidateEntry`); if a second caller exists outside the `Size` guards, use
      design Decision 2 (correct-offset-in-place) for it instead.
- [ ] Confirm `unsafe` stays used by other functions in entry.go after the edits
      (RelativePath, validateLayout, HashString, …) — no import change.
- [ ] Note: a third `&entry.Path[0]` site exists at `pkg/index.go:988`
      (memorylayout debug branch of `validateEntryChaining`). It computes the
      *true* field offset and only prints it — it makes no `Sizeof-8` assumption
      and asserts nothing. Reviewed and **out of scope** (not a bug).

### Step 2: Write failing tests first
- [ ] Add `pkg/format/entry_test.go` with:
  - `TestEntry_PathLength_MatchesRelativePath` (positive pin, ≥2 mod-8 paths):
    asserts `RelativePath()==path`, `calculatePathLength()==len(path)`,
    `validateLayout()` does not panic, `ValidateEntry()==nil`.
  - `TestEntry_ValidateEntry_RejectsCorruptSize` (negative pin): on a *long*-path
    `layEntry` buffer, corrupt `e.Size -= 8` (in-bounds, still 8-aligned and
    `> minSize`) so the size-consistency branch fires, assert `ValidateEntry()`
    errors. **Must corrupt downward, not upward**: bumping `Size` up makes the
    post-fix `RelativePath` scan read past the exactly-sized buffer (OOB heap
    read → `checkptr` fatal under `go test -race`).
- [ ] Run against unmodified code and record baselines:
  - `calculatePathLength` assertion fails (returns `len(path)+12`).
  - `validateLayout` no-panic assertion fails (it panics).
  - negative test fails (pre-fix `ValidateEntry` returns `nil` via swallowed panic).

### Step 3: Apply the minimal fixes
- [ ] Edit `calculatePathLength()` (Decision 1).
- [ ] Edit `validateLayout()` (Decision 4).
- [ ] Re-run the new tests: all green.

### Step 4: Regression sweep
- [ ] `go test ./pkg/...` (full suite, incl. format goldens/roundtrips).
- [ ] `golangci-lint run ./pkg/format/...` — confirm clean after removing the
      G115 suppression (cannot add a finding on deleted code; verify regardless).
- [ ] Race gate per repo convention (pre-commit runs `-race -d=checkptr=0`).

### Step 5: Documentation
- [ ] Contract comment on `calculatePathLength`; corrected layout comment in
      `validateLayout` (both in Step 3 edits).
- [ ] No user-facing/API docs affected (unexported helpers, no format change).

## Code Changes
### A. `calculatePathLength` — before (entry.go:126-149)
```go
// calculatePathLength finds the length of the null-terminated path
func (be *Entry) calculatePathLength() int {
	// ... ~15 lines of unsafe pointer arithmetic starting at &be.Path[0],
	// with two //nolint:gosec // G115 sites and a stale "tracked as a separate
	// backlog item" note ...
	base := unsafe.Pointer(be)
	pathStart := unsafe.Pointer(&be.Path[0])
	startOff := int(uintptr(pathStart) - uintptr(base)) //nolint:gosec // G115: ...
	n := int(be.Size) - startOff //nolint:gosec // G115: ...
	for n > 0 && *(*byte)(unsafe.Add(pathStart, n-1)) == 0 {
		n--
	}
	return n
}
```
### A. after
```go
// calculatePathLength returns the true length of the entry's variable-length
// path. It delegates to RelativePath so the canonical path-start offset
// (Sizeof(Entry), per the EntrySerialiser writer) has a single owner. Safe only
// because the sole caller, ValidateEntry, guards Size ∈ [minSize, 4096] — a
// strict subset of RelativePath's [minEntrySize, 65535] panic guard. Audit any
// new caller added outside those guards (see task 24).
func (be *Entry) calculatePathLength() int {
	return len(be.RelativePath())
}
```

### B. `validateLayout` — before (entry.go:68-75)
```go
	entryStart := uintptr(unsafe.Pointer(be))
	pathFieldOffset := uintptr(unsafe.Pointer(&be.Path[0])) - entryStart
	expectedOffset := unsafe.Sizeof(*be) - 8
	if pathFieldOffset != expectedOffset {
		panic(fmt.Sprintf("Entry layout assumption violated: Path field at offset %d, expected %d",
			pathFieldOffset, expectedOffset))
	}
```
### B. after
```go
	// Path is the trailing declared field but NOT the last 8 bytes: the struct
	// has 4 bytes of tail padding (Sizeof=144, Offsetof(Path)=132), and path data
	// is appended at Sizeof(Entry). Assert against the real field offset, not the
	// historical (wrong) Sizeof-8.
	entryStart := uintptr(unsafe.Pointer(be))
	pathFieldOffset := uintptr(unsafe.Pointer(&be.Path[0])) - entryStart
	expectedOffset := unsafe.Offsetof(be.Path)
	if pathFieldOffset != expectedOffset {
		panic(fmt.Sprintf("Entry layout assumption violated: Path field at offset %d, expected %d",
			pathFieldOffset, expectedOffset))
	}
```

### C. New test file (sketch — finalised in e-phase; imports `testing` + `unsafe`)
```go
package format

import (
	"testing"
	"unsafe"
)

func TestEntry_PathLength_MatchesRelativePath(t *testing.T) {
	for _, path := range []string{"a", "abcdefg", "some/relative/path.go"} {
		buf := layEntry(path, 1, 2)
		e := (*Entry)(unsafe.Pointer(&buf[0]))
		if got := e.RelativePath(); got != path {
			t.Fatalf("RelativePath = %q, want %q", got, path)
		}
		if got := e.calculatePathLength(); got != len(path) { // primary pin
			t.Errorf("calculatePathLength(%q) = %d, want %d", path, got, len(path))
		}
		func() { // validateLayout must not panic on a valid entry (Decision 4)
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("validateLayout panicked on valid entry %q: %v", path, r)
				}
			}()
			e.validateLayout()
		}()
		if err := e.ValidateEntry(); err != nil { // genuine pass now
			t.Errorf("ValidateEntry(%q) = %v, want nil", path, err)
		}
	}
}

func TestEntry_ValidateEntry_RejectsCorruptSize(t *testing.T) {
	// Long path so Size-8 stays > minSize and the scan stays inside the buffer.
	buf := layEntry("some/relative/path.go", 1, 2) // Size 168
	e := (*Entry)(unsafe.Pointer(&buf[0]))
	e.Size -= 8 // 160: in-bounds, 8-aligned, but inconsistent with the path
	if err := e.ValidateEntry(); err == nil {
		t.Fatal("ValidateEntry accepted a size-corrupted entry; the size check is dead")
	}
	// NB: corrupting upward (Size += 8) would make the post-fix RelativePath scan
	// read past the exactly-sized layEntry buffer — an OOB heap read that trips
	// checkptr under `go test -race`. Always corrupt downward.
}
```

## Test Coverage
**See e-testing-plan.md.** Primary pin: `calculatePathLength == len(path)`.
`validateLayout` no-panic pins Decision 4. Negative test proves `ValidateEntry` is
live again. Mixed mod-8 path lengths guard against padding masking.

## Validation Criteria
- All three baseline assertions fail pre-fix (proves the tests exercise the bugs).
- Post-fix `go test ./pkg/...` green, race gate green, format package lints clean.

## Scope Completion
**IMPORTANT**: Complete all planned implementation before marking task Finished.
Scope is two one-line fixes + one test file in a single package — no deferral
anticipated. The "delete the offset check entirely" cleanup is explicitly out of
scope (design Decision 4 alternative).

## Status
**Status**: Finished
**Next Action**: /cwf-testing-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Executed as planned with no deviations; the negative-test downward-corruption guard
(from round-2 plan review) prevented an OOB read. See f-implementation-exec.md.

## Lessons Learned
The plan-review panel's OOB catch on the negative test (`Size += 8` past an
exactly-sized buffer) was load-bearing. See j-retrospective.md.
