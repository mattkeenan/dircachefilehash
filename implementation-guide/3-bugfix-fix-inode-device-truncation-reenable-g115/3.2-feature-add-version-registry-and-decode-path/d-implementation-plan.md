# Add version registry and decode path - Implementation Plan
**Task**: 3.2 (feature)

## Task Reference
- **Task ID**: internal-3.2
- **Task URL**: N/A (internal task)
- **Parent Task**: 3
- **Branch**: feature/3.2-add-version-registry-and-decode-path
- **Template Version**: 2.1

## Goal
Implement the `StrategyForVersion` dispatch resolver and version-less writable header per the
approved design, gating both mmap entry-walk loaders on the file's `header.Version` and sourcing
the write version from `pkg/format` — no on-disk change.

## Workflow
Patterns first → Test → Minimal impl → Refactor green → Commit message explains "why"

## Files to Modify
### Primary Changes
- `pkg/format/version_dispatch.go` (**new**) — `DecodeStrategy` enum (`DecodeReject`,
  `DecodeZeroCopy`; **no** `DecodeHeap` — deferred to 3.3) and
  `StrategyForVersion(version uint32) (DecodeStrategy, error)` as a switch-with-default. Keep the
  legacy arm a *separate* case from current (both return `DecodeZeroCopy` today) so 3.3 flips one
  arm to `DecodeHeap`.
- `pkg/format/header.go` — `SetHeaderForWritableIndex` **drops its `version` param** and sources
  `CurrentIndexVersion` internally. `SetHeader(…, version, …)` primitive unchanged.
- `pkg/index.go` — gate both entry-walk loaders on `header.Version` immediately before the cast:
  `collectEntryRefs` (~:329, cast :337) and `loadIndexFromFileWithTracking` (~:540, cast :620);
  re-point the empty-index write (`writeEmptyIndex`, :774) from `SetHeader(…, ms.version, …)` to
  the version-less `SetHeaderForWritableIndex(…)`.
- `pkg/temp_index_writer.go` — drop the `tiw.ms.version` argument at :99 and :176.

### Supporting Changes
- `pkg/format/version_dispatch_test.go` (**new**) — table test for `StrategyForVersion`.
- `pkg/index_test.go` (or a focused new `_test.go`) — load-path version-rejection + truncated-index
  negatives (detailed in e-testing-plan.md).
- No `.golangci.yml` change (G115 stays excluded; count re-measured, not modified).

## Implementation Steps
### Step 1: Resolver (pkg/format) — test first
- [ ] Write `version_dispatch_test.go`: table over `{2→ZeroCopy, 3(current)→ZeroCopy, 0→err,
      1→err, current+1→err, 0xFFFFFFFF→err}`; assert exact strategy value and that errors name the
      supported range; assert no panic.
- [ ] Add `version_dispatch.go` with `DecodeStrategy` + `StrategyForVersion` (switch-with-default;
      current and legacy as separate `DecodeZeroCopy` arms; `default → DecodeReject` + error).
- [ ] `go test ./pkg/format/` green.

### Step 2: Write-version ownership (pkg/format + callers)
- [ ] Change `SetHeaderForWritableIndex` signature: drop `version`; source `CurrentIndexVersion`.
- [ ] Update `temp_index_writer.go:99,176` to the new signature.
- [ ] Re-point `index.go:774` (`writeEmptyIndex`) to `SetHeaderForWritableIndex` (behaviour-equal:
      `baseFlags=0`, and `0 &^ Clean == 0`).
- [ ] `go build ./...` — compiler confirms every caller migrated.

### Step 3: Gate the load paths (pkg/index.go)
- [ ] In `collectEntryRefs`, at the head (before the walk): `if _, err :=
      format.StrategyForVersion(header.Version); err != nil { return nil, err }`. **No** `munmap`
      here — the caller owns `indexFile` cleanup (matches existing `:335` return).
- [ ] In `loadIndexFromFileWithTracking`, before the `:620` cast: same gate, but on error
      `indexFile.DecRef()` then return (matches its existing `:604/:616/:623` error paths).
- [ ] Keep the value discarded (`_,`) in 3.2: `DecodeZeroCopy` is the only non-error result, so no
      `switch` and no unreachable `DecodeHeap` branch (3.3 introduces the switch). `DecodeZeroCopy`
      is still exercised — by the resolver's own unit test.
- [ ] **Resolver coexists with `ValidateVersion`, does not replace it.** Leave the existing
      `ValidateVersion` calls (`:301`, `:576`) in place. The resolver is the *materialisation-strategy
      authority* and the *only real gate* for the dcfhfind `MetaStore{version:0}` path (where
      `ValidateVersion(0)` is a no-op); `ValidateVersion` stays as the early signature/byte-order/
      version triple. Add a one-line code comment on `StrategyForVersion` noting its range arm and
      `ValidateVersion` share the `MinIndexVersion`/`CurrentIndexVersion` constants and **3.3 must
      bump both in lockstep** — so a future reader does not delete `ValidateVersion` as "redundant".

### Step 3b: Header-size bounds guard (fixes a latent v3-truncation over-read — NFR5)
- [ ] **Both entry-walk loaders gate file size only against `V2HeaderSize` (88)** —
      `openAndValidateIndex:286` and `loadIndexFromFileWithTracking:552` — then slice
      `data[headerSizeForVersion(header.Version):]`. A **v3** file of 88–103 bytes passes the gate,
      then `data[104:]` **panics** (mmap len == real file size, not page-rounded). The new version
      gate does **not** close this (v3 → `DecodeZeroCopy`, success). Add, after `ValidateVersion`
      and before the entry slice in **both** loaders:
      `if int64(headerSizeForVersion(header.Version)) > stat.Size() { <cleanup>; return …,
      fmt.Errorf("file too small for v%d header: %d < %d", header.Version, stat.Size(), hdrSize) }`.
      Cleanup per the per-loader contract (plain return in `openAndValidateIndex`'s `cleanup()`;
      `DecRef()` in the tracking loader). This is the concrete realisation of NFR5's
      "truncated index → error, never over-read".

### Step 4: Regression + negatives
- [ ] Add load-path negatives (e-testing-plan): (a) a crafted index with an out-of-range version
      byte routed via **both** the `ms.version=Current` path **and** the dcfhfind
      `MetaStore{version:0}` path (the latter is the one the gate primarily protects); (b) the
      **v3-header-truncation** case — a valid v3 header on an 88–103-byte file — asserting a clean
      error, **not** a slice-bounds panic (this targets the Step 3b guard, not the already-handled
      `offset >= len(entryData)` path). All negatives run under `-race`.
- [ ] `go test ./pkg/... ./cmd/...` green incl. `-race`.

### Step 5: Static floor + sanity
- [ ] Re-measure G115 via the 3.1 method (un-exclude in `.golangci.yml`, `golangci-lint run ./...`,
      revert); confirm count == 52.
- [ ] `grep` confirms no `pkg/` mmap read path open-codes a version-conditioned materialisation
      branch outside the resolver; write callers pass no version.

## Code Changes
### `pkg/format/version_dispatch.go` (new)
```go
// DecodeStrategy says how an index of a (validated) version is materialised.
type DecodeStrategy int

const (
	DecodeReject   DecodeStrategy = iota // unknown / out-of-range — fail closed
	DecodeZeroCopy                       // mmap cast (current + identical-layout legacy)
	// DecodeHeap is added in Task 3.3 when v4 makes a legacy entry layout diverge.
)

// StrategyForVersion maps a header version to its materialisation strategy.
// Default-bearing: an unrecognised version is rejected, never used to index a table.
// Callers pass header.Version (the file's value), never ms.version — see header.go ValidateVersion.
func StrategyForVersion(version uint32) (DecodeStrategy, error) {
	switch {
	case version == CurrentIndexVersion:
		return DecodeZeroCopy, nil
	case version >= MinIndexVersion && version < CurrentIndexVersion:
		// Entry layout is byte-identical across shipped versions; 3.3 flips this arm to DecodeHeap.
		return DecodeZeroCopy, nil
	default:
		return DecodeReject, fmt.Errorf("unsupported index version %d (supported %d-%d)",
			version, MinIndexVersion, CurrentIndexVersion)
	}
}
```

### `pkg/format/header.go` — `SetHeaderForWritableIndex`
```go
// Before: func (ih *Header) SetHeaderForWritableIndex(signature [4]byte, version uint32,
//             entryCount uint32, baseFlags FlagBits, checksumType HashKind)
// After:
func (ih *Header) SetHeaderForWritableIndex(signature [4]byte, entryCount uint32,
	baseFlags FlagBits, checksumType HashKind) {
	flags := baseFlags &^ IndexFlagClean
	ih.SetHeader(signature, CurrentIndexVersion, entryCount, flags, checksumType)
}
```

### `pkg/index.go` — gate (collectEntryRefs shown; tracking loader uses DecRef on error)
```go
// At the head of collectEntryRefs, before the entry walk:
if _, err := format.StrategyForVersion(header.Version); err != nil {
	return nil, err // caller owns indexFile cleanup
}
```

### `pkg/index.go` — header-size bounds guard (Step 3b; both loaders, after ValidateVersion)
```go
// openAndValidateIndex: file must be at least its version's header size before data[hdrSize:].
if hdrSize := headerSizeForVersion(header.Version); int64(hdrSize) > stat.Size() {
	cleanup() // tracking loader: indexFile.DecRef() instead
	return nil, nil, fmt.Errorf("file too small for v%d header: %d bytes < %d",
		header.Version, stat.Size(), hdrSize)
}
```

## Test Coverage
**See e-testing-plan.md for complete test plan.** Summary: unit table test for `StrategyForVersion`
(positive + rejection boundaries); load-path negatives (out-of-range version, truncated index) under
`-race`; v2 + v3 byte-correct round-trip retained (3.1 gate still green); full regression suite.

## Validation Criteria
**See e-testing-plan.md.** Gate: AC1–AC4 from b-requirements-plan.md — single owned read dispatch +
write version; positive/negative version boundaries error/route correctly; v2/v3 byte-correct; full
suite green incl. `-race`; G115 == 52.

## Scope Completion
**IMPORTANT**: Complete all planned implementation before marking task Finished.

Deferring work creates technical debt and scope creep. Task 37 deferred documentation updates,
marked the task complete anyway, and created Task 38 to fix the deferred work.

**Deliberate deferrals (design-approved, recorded — not silent)**:
- `DecodeHeap` arm + concrete legacy entry decoder → 3.3 (no divergent entry layout until v4).
- dcfhfix repair read path (`entry_workflow_main.go:122`) resolver adoption → 3.3 (load-bearing
  only when v3 entries need widening on read).
- `BEIndexFileIOEntry.readEntryData` (`binary_entry_index_file.go:73`) route-or-delete → 3.3
  (test-only today, no production callers).

## Status
**Status**: Finished
**Next Action**: /cwf-testing-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All planned steps executed; per-step results and the 3 deviations are recorded in
f-implementation-exec.md. Build/grep confirmed every `SetHeaderForWritableIndex` caller migrated off
the version parameter. See j-retrospective.md.

## Lessons Learned
A function sitting exactly at the cyclop limit (`loadIndexFromFileWithTracking` at 20) turns any new
branch into a lint failure — extracting a cohesive sub-block (`parseTrackedEntries`) is the honest
fix, not a suppression. Worth anticipating when planning edits to near-limit functions. See
j-retrospective.md.
