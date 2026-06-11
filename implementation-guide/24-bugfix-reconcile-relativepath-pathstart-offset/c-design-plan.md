# Reconcile RelativePath pathStart offset - Design
**Task**: 24 (bugfix)

## Task Reference
- **Task ID**: internal-24
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: bugfix/24-reconcile-relativepath-pathstart-offset
- **Template Version**: 2.1

## Goal
Correct the two `pkg/format/entry.go` readers that share the false "Path is the
last 8 bytes" premise — `calculatePathLength()` and `validateLayout()` — so the
`extravalidation` `ValidateEntry()` path stops being a swallowed-panic no-op and
genuinely validates entries against the canonical layout (path data at
`Sizeof(Entry)`).

## Design Priorities
Testability → Readability → Consistency → Simplicity → Reversibility

## Canonical Offset — Established Fact (corrected: 12 bytes, not 8)
Three independent producers/readers agree the variable-length path data begins at
`Sizeof(Entry)`:
- **Writer** `pkg/entry_serialiser.go:144-145`: `pathOffset := int(unsafe.Sizeof(*be))`.
- **Safe reader** `pkg/format/codec.go:216` `SafeEntry.GetPath`: `offset + layout.minSize`.
- **Zero-copy reader** `pkg/format/entry.go:90` `RelativePath`: `base + Sizeof(*be)`.

**Measured layout** (`go test` diagnostic): `Sizeof(Entry)=144`,
`Offsetof(Path)=132`, `Offsetof(Hash)+Sizeof(Hash)=68+64=132`. So the `Path[8]`
field occupies `[132,140)` and there are **4 bytes of tail padding** `[140,144)` —
`Path` is *not* the last 8 bytes of the struct. Path data is written at `144`,
making `Path[8]` + tail padding (`[132,144)`) entirely vestigial.

Consequently the backlog's "8-byte discrepancy" is imprecise: `&be.Path[0]` sits
at `Offsetof(Path)=132 = Sizeof-12`, so any reader keyed off `&be.Path[0]`
over-counts by **12 bytes**. Two readers share this false "Path is the last 8
bytes" premise and are both wrong:
- `calculatePathLength` (entry.go:139): starts the scan at `&be.Path[0]` (132).
  Measured over-count = 12 (len 1→13, len 7→19, len 21→33).
- `validateLayout` (entry.go:70): asserts `Offsetof(Path) == Sizeof-8 (136)`,
  which is false (real offset 132), so it **panics on every well-formed entry**.

Because `ValidateEntry` calls `validateLayout` first (entry.go:164) inside a
`recover()` (entry.go:155-162) that drops the panic and returns `nil`,
**`ValidateEntry` is currently a no-op that always returns `nil`** — its size /
hash-type checks are unreachable. (Verified: a well-formed entry returns `nil`;
were `validateLayout` not panicking, the inflated `calculatePathLength` would make
the size check *error*. The observed `nil` proves the swallowed panic.) This is
why the bug stayed latent: extravalidation has been silently inert.

## Key Decisions

### Decision 1: calculatePathLength delegates to RelativePath
- **Decision**: Replace the body of `calculatePathLength()` with
  `return len(be.RelativePath())`. Delete the duplicate unsafe pointer arithmetic
  and its `//nolint:gosec` site.
- **Rationale**:
  - **Single source of truth** — the two functions are required to agree by
    definition; routing one through the other makes future divergence
    *structurally impossible*, satisfying the "cannot silently reappear"
    criterion more strongly than re-deriving the offset would.
  - **"The best part is no part"** — removes ~15 lines of duplicate
    checkptr-sensitive unsafe code rather than maintaining a second copy.
  - **Zero cost** — `RelativePath()` returns via `unsafe.String` (no allocation,
    no copy); `len()` on the result is O(1).
- **Safety analysis (durable, audit-triggering form)**: Safe here because
  `calculatePathLength` is unexported and has exactly one caller — `ValidateEntry`
  — whose `Size ∈ [minSize, 4096]` guard (entry.go:167-174) is a strict subset of
  `RelativePath`'s `[minEntrySize, 65535]` guard (`minSize == minEntrySize ==
  Sizeof(Entry)`, upper 4096 < 65535). So `RelativePath`'s panic guard cannot
  fire and the old signed-int sub-minSize branch is unreachable. **Audit trigger**:
  any future caller of `calculatePathLength` added *outside* `ValidateEntry`'s
  guards voids this analysis — re-confirm the single-caller invariant by grep at
  exec time, and if a second caller exists, fall back to Decision 2.
- **Defence in depth (do not rely on)**: `ValidateEntry` wraps its body in a
  deferred `recover()` (entry.go:155-162) that converts any panic to a (silently
  dropped) error. A stray `RelativePath` panic therefore could not crash the
  process — but recover() also *masks* it, so the test pin must assert
  `calculatePathLength` equality directly (see Test Oracle), not lean on
  `ValidateEntry() == nil`.
- **Trade-offs**: Introduces a call-coupling from `calculatePathLength` to
  `RelativePath`. This is desirable (it *is* the same computation) and is the
  point of the change.

### Decision 2 (considered, rejected): correct the offset in place
- Keep `calculatePathLength` standalone but change `pathStart` to `base +
  Sizeof(*be)` and length to `Size - Sizeof(*be)`.
- **Rejected because**: leaves two near-identical unsafe implementations that must
  be kept byte-for-byte in sync by hand — exactly the failure mode that produced
  this bug. Kept on record as the fallback if Decision 1's panic-safety analysis
  is ever invalidated (e.g. a new caller of `calculatePathLength` outside
  ValidateEntry's guards).

### Decision 3: comment cleanup
- Remove the stale "discrepancy is tracked as a separate backlog item" note
  (entry.go:128-137). `RelativePath`'s own comment block already documents the
  canonical offset; no per-offset commentary remains to drift.
- The removed block is currently the helper's *entire* doc body, so replace it
  with a one-line contract comment ("returns the true path length, delegating to
  the canonical `RelativePath`") so the function still states its contract locally.

### Decision 4: fix validateLayout's Path-offset assertion (NEW — scope: fix both)
- **Decision**: In `validateLayout` (entry.go:67-86) change
  `expectedOffset := unsafe.Sizeof(*be) - 8` to
  `expectedOffset := unsafe.Offsetof(be.Path)`, and update the surrounding comment
  to record the real layout (Path at `Offsetof(Path)`, 4-byte tail padding, path
  data at `Sizeof`). This stops the false panic on every valid entry, so
  `ValidateEntry` reaches and runs its real size/hash-type checks again.
- **Rationale**: Same root cause as Decision 1 — the "Path is the last 8 bytes"
  premise. Without this, fixing `calculatePathLength` alone leaves `ValidateEntry`
  a swallowed-panic no-op and the success criterion "ValidateEntry passes on a
  well-formed entry" would be satisfied only vacuously.
- **Note**: the offset check becomes effectively tautological (runtime
  `&be.Path[0]-be` always equals the compile-time `Offsetof(Path)`), but it is
  kept rather than deleted to preserve the function's tripwire shape; its real
  value-add (8-byte alignment + Size-range checks) is now reachable. The
  load-bearing layout invariants remain the build-time assertions (entry.go:10-16).
- **Alternative considered**: delete the offset check entirely ("best part is no
  part"). Rejected to keep the change minimal and the diff focused on correcting,
  not removing, the assertion; deletion can be a later cleanup if desired.

## System Design

### Component Overview
- **`Entry.calculatePathLength()`** (changed, Decision 1): length-only helper; now
  derived from `RelativePath`.
- **`Entry.validateLayout()`** (changed, Decision 4): corrected Path-offset
  assertion so it no longer panics on valid entries.
- **`Entry.RelativePath()`** (unchanged): canonical zero-copy path reader; sole
  owner of the path-start offset.
- **`Entry.ValidateEntry()`** (unchanged): consumer; with Decisions 1+4 it stops
  being a swallowed-panic no-op — its layout/size/hash-type checks now execute.

### Data Flow (ValidateEntry, post-change)
1. `validateLayout()` now passes (Path-offset assertion corrected) instead of
   panicking, so execution proceeds past entry.go:164 rather than unwinding into
   the `recover()`.
2. `ValidateEntry` guards `Size ∈ [minSize, 4096]`.
3. Calls `calculatePathLength()` → `len(RelativePath())` → true path length `L`.
4. `expectedSize = minSize + L + 1 + padding` now equals the on-disk `Size`.
5. Validation genuinely passes for well-formed entries and genuinely errors for
   corrupt ones (previously: always `nil` via swallowed panic).

## Interface Design
No exported-surface change. `calculatePathLength` is unexported; its `int` return
contract is unchanged (true path length, was length+8). No on-disk format change.

## Test Oracle (for the e-phase)
Reuse the existing `layEntry(path, dev, ino)` helper in
`pkg/format/roundtrip_test.go`, which lays the path at `buf[MinEntrySize():]` —
byte-identical to the production serialiser. On a `layEntry`-produced entry cast
to `*Entry`:

**Positive pin** (≥2 paths in different residue classes mod 8, e.g. 1-char and
7-char, so the 12-byte over-count cannot be absorbed by `expectedSize` padding at
entry.go:182-184):
- `RelativePath() == path`.
- `calculatePathLength() == len(path)` — **primary regression pin** (returns
  `len(path)+12` before the fix). Asserted directly, not via `ValidateEntry`,
  because the `recover()` can mask a panic regression.
- `validateLayout()` does not panic (call it directly in-package, wrapped in a
  `recover` that fails the test on panic) — pins Decision 4 independently of
  `ValidateEntry`'s own recover.
- `ValidateEntry() == nil` — now a *genuine* pass (not the pre-fix swallowed
  panic).

**Negative pin** (proves `ValidateEntry` is live again, not a no-op): take a
well-formed *long*-path `layEntry` buffer and corrupt `e.Size` **downward** (e.g.
`-8`, kept `> minSize` and 8-aligned) so it is inconsistent with the path and the
size-consistency branch (entry.go:186) fires, then assert `ValidateEntry()`
returns a non-nil error. Before the fix this also returns `nil` (swallowed panic);
after, it correctly errors — distinguishing the two states. **Corrupt downward,
not upward**: inflating `Size` makes the post-fix `RelativePath` scan read past
the exactly-sized buffer (OOB heap read → `checkptr` fatal under `-race`).

## Constraints
- On-disk format unchanged; read-side reconciliation only.
- Stay checkptr-clean. Net effect removes unsafe code, reducing checkptr surface.

## Decomposition Check
- [ ] **Time**: >1 week? No.
- [ ] **People**: >2 people? No.
- [ ] **Complexity**: 3+ concerns? No — one helper, one test.
- [ ] **Risk**: Isolation needed? No.
- [ ] **Independence**: Separable? No.

No signals triggered.

## Validation
- [x] Design review completed (canonical offset triangulated across writer +
      two readers; single caller of the changed helper verified)
- [ ] Architecture approved by team
- [ ] Integration points verified

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Decisions 1 (delegate calculatePathLength) and 4 (validateLayout offset) both
implemented as designed; corrected 12-byte model held. See f-implementation-exec.md
and j-retrospective.md.

## Lessons Learned
Design-phase empirical measurement (the throwaway `go test` layout diagnostic) was
the turning point that corrected the offset model and surfaced the validateLayout
no-op. See j-retrospective.md.
