# Re-enable checkptr in the race gate - Design
**Task**: 10 (bugfix)

## Task Reference
- **Task ID**: internal-10
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: bugfix/10-reenable-checkptr-race-gate
- **Template Version**: 2.1

## Goal
Define the approach for making the zero-copy mmap accessors checkptr-clean so the
`-race` gate can run with checkptr enabled. **Outcome: option 1 (fix) — confirmed
viable by spike; option 2 (document-only) is not needed.**

## Design Priorities
Testability → Readability → Consistency → Simplicity → Reversibility

## Root Cause (verified)
checkptr's `checkptrArithmetic` (runtime/checkptr.go) fires on
`unsafe.Pointer(uintptr_expr)` conversions: when the *result* points into a Go
heap object, **one of the original pointers in the same conversion expression must
point into that same object**. Our accessors compute the address in a `uintptr`
*variable* across several statements:

```go
entryPtr := uintptr(unsafe.Pointer(&Data[0])) + uintptr(headerSize) + uintptr(offset)
return (*binaryEntry)(unsafe.Pointer(entryPtr))   // provenance of &Data[0] lost
```

The `uintptr` round-trip severs the provenance, so checkptr cannot match the
result to the `&Data[0]` base and panics with *"pointer arithmetic result points
to invalid allocation"*. This only manifests for **heap-backed** index data (the
`heapBacked` path used by tests); for true mmap memory `checkptrBase` returns 0 and
the check is skipped — which is why production never crashed and why the gate could
paper over it.

This is **spec-conformant Go usage that checkptr cannot follow**, not undefined
behaviour. `unsafe.Add` (Go 1.17+) is the idiomatic replacement: it keeps the
result typed as `unsafe.Pointer` with provenance intact, so checkptr matches the
base.

### Spike evidence
Replacing the `GetBinaryEntry` body with
`unsafe.Add(unsafe.Pointer(&Data[0]), headerSize+offset)` under plain
`go test -race` (checkptr ON) moved the failure **past** `binary_entry.go` to the
next identical pattern at `format/entry.go:115`. This confirms the pattern fixes
the site without copies and without changing behaviour. The spike was reverted;
implementation re-applies it across all sites.

## Key Decisions

### Architecture Choice
- **Decision**: Convert every `uintptr`-arithmetic-then-`unsafe.Pointer` site to
  `unsafe.Add(base unsafe.Pointer, delta)` keeping `unsafe.Pointer` provenance
  end-to-end. No on-disk format change, no API change, no copies.
- **Rationale**: `unsafe.Add` is the canonical checkptr-clean idiom and is
  **already used in this codebase** at `binary_entry_index_file_mmap.go:238`
  (`unsafe.Add(unsafe.Pointer(entry), unsafe.Sizeof(*entry))`) — so this is
  consistency, not novelty. Preserves zero-copy: `unsafe.String` / the struct cast
  still alias the original bytes.
- **Trade-offs**: Still `unsafe`, still requires correctness reasoning — but the
  same reasoning as today, now also machine-checked by checkptr. No downside vs
  the status quo.

### Rejected alternatives
- **Keep `-d=checkptr=0` and document (option 2)**: Rejected — the spike proves a
  clean fix exists, so accepting permanently reduced race coverage fails the "very
  very good reason" bar.
- **Copy bytes into a heap buffer per access**: Rejected — defeats the zero-copy
  design (NFR/perf), allocates on the hot read path.
- **Single-expression uintptr arithmetic** (collapse the two statements into one):
  Would also satisfy checkptr, but `unsafe.Add` is clearer, matches existing code,
  and is the documented idiom. Prefer it.

## System Design

### Sites to change (the complete set)
Verified by grepping every `unsafe.Pointer(<uintptr expr>)` conversion; only these
three round-trip arithmetic back into a pointer:

- **C1 — `pkg/binary_entry.go` `GetBinaryEntry` (L52-54)**: base
  `&Data[0]`, delta `headerSize+offset` → `*binaryEntry`. (Spike-confirmed.)
- **C2 — `pkg/format/entry.go` `RelativePath` (L103-120)**: backward null scan
  (L115) and final `unsafe.String` pointer (L120). Use a single `base :=
  unsafe.Pointer(be)`; derive `pathStart`/`pathEnd` via `unsafe.Add`; dereference
  via `unsafe.Add(end, -1)`; compare addresses with `uintptr(...)` (comparison
  needs no provenance).
- **C3 — `pkg/format/entry.go` `calculatePathLength` (L124-136)**: same backward-
  scan pattern as C2.

### Sites deliberately NOT changed (verified non-violating)
These do `unsafe.Pointer → uintptr` arithmetic that **terminates in an int/uintptr
offset and is never reconverted to a pointer**; checkptr only instruments
`uintptr → unsafe.Pointer`, so they are clean as-is:
- `pkg/binary_entry.go:72-74` `createBinaryEntryRef` (add then subtract → int offset)
- `pkg/index.go:988` and `pkg/format/entry.go:68-69` `validateLayout`
- `pkg/recovery.go:230`
Element-address casts (also clean — `unsafe.Pointer(&slice[i])`, no arithmetic,
alignment holds): `pkg/index.go:156`, `pkg/format/codec.go:120/129`.

> Line numbers above reflect the baseline commit and have already drifted once;
> **re-grep `unsafe.Pointer(` at implementation time** rather than trusting them.

### The gate
- **`.githooks/pre-commit` (L101-105)**: remove `GOFLAGS="-gcflags=all=-d=checkptr=0"`
  so the race gate runs default checkptr, and replace the now-incorrect comment
  (which claims checkptr "incorrectly flags" the accessors — a wrong rationale that
  would invite a future re-disable) with the real reason (provenance preserved via
  `unsafe.Add`).
- **Removal must be total**: grep the whole repo for `checkptr` to confirm the
  disable lives in exactly **one** place — no residual `-d=checkptr=0` in a
  `Makefile`, `.golangci.yml`, or CI config that would silently re-defeat the gate.

### Documentation debt to clear
- `[[project_race_checkptr_disabled]]` auto-memory becomes false → rewrite/delete.
- Any CLAUDE.md prose asserting checkptr "incorrectly flags" the accessors is
  wrong (checkptr was correct that the *expression form* was unfollowable) —
  correct it.

## Data Flow
Unchanged. Index read path: `binaryEntryRef.GetBinaryEntry()` →
`(*Entry).RelativePath()` / field reads → zero-copy string/struct views over the
same backing bytes. Only the address-computation form changes.

## Interface Design
No public API, signature, struct, or on-disk format changes. Internal accessor
bodies only.

## Constraints
- Preserve zero-copy: no allocations/copies on the accessor hot path.
- No on-disk format change; no behavioural change (round-trip tests must be
  unaffected).
- Do not weaken the gate by any other means.
- British spelling in comments/prose. The `//nolint:govet // intentional pointer
  arithmetic` suppressions at the three sites exist because govet's unsafeptr check
  flagged the round-trip; after the `unsafe.Add` rewrite govet may no longer fire —
  if so **delete** the suppression (don't reword a dead one); where a suppression
  still fires, update its rationale to describe the new form. Keep G115 rationales
  on bounded offsets accurate once arithmetic moves into `unsafe.Add`.
- **Correctness-over-convenience ordering**: re-arm the gate (remove the flag)
  **only after** a full checkptr-ON run passes (see Validation). If a non-enumerated
  site surfaces at runtime, fix it first — never partially weaken the gate.
- Target the local toolchain (go1.26.x); `unsafe.Add` requires Go ≥1.17 (module is
  Go 1.24.3 — fine).

## Decomposition Check
- [ ] **Time**: >1 week? No (≈1 day).
- [ ] **People**: >2 people? No.
- [ ] **Complexity**: 3+ distinct concerns? No — one idiom applied to 3 sites + a
  gate edit + doc cleanup.
- [ ] **Risk**: High-risk needing isolation? No — spike already de-risked the core
  uncertainty.
- [ ] **Independence**: Separable parts? No — all share one verification (gate
  green under checkptr).

**Conclusion**: No decomposition.

## Validation
- [ ] Plan review (Step 8) completed.
- [ ] Approach confirmed by spike (done — see Spike evidence).
- [ ] Integration points verified (the three sites + gate enumerated by grep).
- [ ] **Acceptance bar is a full runtime run, not grep**: `go test -race ./...`
  with checkptr **ON** must pass across *all* packages, exercising the read paths
  (`GetBinaryEntry`, `RelativePath`, `calculatePathLength`) on **heap-backed**
  indices — grep finds source sites, but checkptr fires at runtime, so a
  grep-invisible site (generic/inlined helper) would only show here.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-plan 10
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
The `unsafe.Add` approach was applied to all three sites as designed. One refinement was
needed during exec: the design's "derive pathEnd via unsafe.Add" form held a past-the-end
pointer live, which checkptr's arithmetic check accepts but the GC heap scan rejects
("found bad pointer in Go heap"). The fix kept the same idiom (single typed base,
provenance-preserving) but switched the trailing-NUL scan to an integer length so no
out-of-bounds pointer is ever held live. No on-disk format or API change, as designed.

## Lessons Learned
The root-cause analysis was correct about provenance but incomplete about GC pointer
validity — making an accessor checkptr-clean is necessary but not sufficient; the derived
pointers must also never be live past-the-end. See j-retrospective.md (Technical Insights).
