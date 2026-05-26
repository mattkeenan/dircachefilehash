# Fix inode device truncation re-enable G115 - Implementation Plan
**Task**: 3 (bugfix)

## Task Reference
- **Task ID**: internal-3
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: bugfix/3-fix-inode-device-truncation-reenable-g115
- **Template Version**: 2.1

## Scope of this plan
Parent-level **cross-subtask** implementation strategy: subtask sequencing, the shared
`pkg/format` seam each subtask builds against, migration order, and the inter-subtask
verification gates. **Per-call-site edits, exact signatures, and per-field test matrices are
delegated to each subtask's own `d-`/`e-` plans** — this plan deliberately stays out of those
weeds.

## Goal
Sequence the extraction of `pkg/format`, the version-aware decode path, and the `Dev`/`Ino`
widening so that each lands behind a passing gate and no subtask depends on weeds owned by
another.

## Workflow
Patterns first → Test → Minimal impl → Refactor green → Commit message explains "why"

## Files to Modify (seam-level; grouped by owning subtask)
### Shared seam — created in 3.1, consumed by all
- `pkg/format/` (**new package**) — vocabulary types, canonical `Entry` + header layout,
  generic bounds-checked codec, version registry (absorbs `headerSizeForVersion` + version
  constants). The single source of truth.

### 3.1 — Extraction & migration (no behaviour/width/version change)
- `pkg/binary_entry.go` — `binaryEntry` → `format.Entry` (canonical type moves/export);
  build-time layout assertions + `validateLayout` move with it.
- `pkg/binary_entry_interface.go` + implementers (`binary_entry_skiplist.go`,
  `binary_entry_scan.go`, `binary_entry_index_file{,_mmap}.go`, `scan.go`, `filter.go`,
  `dcfhfind_support.go`) **and `pkg/entry_serialiser.go`** (assigns `be.Dev/be.Ino` — on the
  round-trip path) — accessor return types switch to vocabulary types.
- `pkg/index.go`, `pkg/constants.go` — version constants + `headerSizeForVersion`
  (**5 call sites in `index.go`, all-or-nothing**) + the `ValidateVersion` range check move
  into `pkg/format`.
- `cmd/dcfhfix/safe_entry_accessor.go`, `cmd/dcfhfix/main.go` — **delete** the duplicate
  `binaryEntry`, the duplicate `indexHeader`, and the parallel offset table; import `pkg/format`.
- **Seam inventory is non-exhaustive**: the 3.1 subtask MUST `grep '\.Dev\|\.Ino'` (and the
  `Dev()/Ino()` accessors) and size from that, not from this list.

### 3.2 — Version-aware read/write (no width change)
- `pkg/format/` — version registry keyed by **map/switch-with-default** (never raw-indexed by
  the untrusted version byte), consulted **after** the `ValidateVersion` clamp; `Decode` rejects
  unknown / out-of-range / newer-than-current; zero-copy cast hard-gated on `version==current`.
- **Boundary note**: the *header-tier* version difference (v2/v3 `Timestamp`) is real, immediate
  work here — this re-homes `ValidateVersion`/`headerSizeForVersion` (one surviving gate, not a
  parallel second one). The *entry-tier* decoder has no work until v4 exists, so it **may fold
  into 3.3** (decided at subtask creation).

### 3.3 — Widen + bug fix + lint gate
- `pkg/format` vocabulary — `DevID`/`Inode` widen to 64-bit; format version bump; per-version
  layout assertions updated (v3 asserts v3 sizing, v4 asserts v4 sizing).
- **Ingest casts** (genuine truncation): `pkg/binary_entry_scan.go:69-70` — remove `uint32(...)`.
- **Accessor return-width** changes: `pkg/scan.go:300` (`scanFilterEntry.Dev()` — **Dev-only**, no
  `Ino()` there) plus the `Dev()/Ino()` implementers; `dcfhfind` consumes the widened accessor.
- `pkg/dupes.go:256` — dedup key `[2]uint32` → full-width.
- `.golangci.yml` — remove G115 from `gosec.excludes`.

## Parent invariants inherited by every subtask
- **Vocabulary type choice (steer, final call in 3.1)**: prefer a type *alias* (`type DevID =
  uint64`) for low churn — note that under an alias a narrowing `uint64→uint32` assignment at a
  consumer is **still a compile error**, so 3.3's widening truncation stays caught; the
  named-type benefit is only forcing review at *same-width* boundaries. Pick named only if a
  demonstrated same-width bug justifies the conversion friction.
- **Layout assertions survive the move and become version-aware**: the build-time
  (`Sizeof%8==0`, `Path==8`) and `validateLayout` bounds checks live in `pkg/format` and assert
  the *correct version's* sizing — a corrupt `Size` must not pass the wrong version's check.
- **Single version gate**: version validation has one owner in `pkg/format` (the re-homed
  `ValidateVersion`); no subtask creates a second parallel gate.
- **Per-subtask G115 enumeration**: at the end of *each* subtask, run gosec with G115 active
  (temporarily un-excluded / `--enable-only=gosec`) and diff the G115 site count against a
  baseline recorded at the start of 3.1 — so a narrowing cast added while G115 is off cannot
  hide until 3.3.

## Implementation Steps (cross-subtask sequencing)
### Step 1: Subtask 3.1 — extract & migrate
- [ ] Record the G115 site baseline (the a-task-plan early-enumeration mitigation).
- [ ] Stand up `pkg/format` with vocabulary + canonical `Entry`/header + codec (bounds-check
      internal/non-bypassable).
- [ ] Migrate core package + `entry_serialiser.go` + all `Dev()/Ino()` implementers; delete both
      dcfhfix duplicates + offset table; everything imports `pkg/format`.
- [ ] **Gate**: byte-for-byte round-trip over **both a v2 and a v3 fixture** (read → re-serialise
      → identical bytes); a malformed-input negative test (truncated entry, `Size`<min, `Size`
      overruns buffer, corrupt version byte) that **errors, not over-reads/panics**; G115 count
      unchanged vs baseline; full suite green. No width/version change.

### Step 2: Subtask 3.2 — version-aware read/write
- [ ] Re-home header-tier version handling into the registry (map/switch-keyed, post-clamp);
      `Decode` rejects unknown/out-of-range/newer; descriptor offsets validated against buffer
      length; zero-copy cast hard-gated on `version==current`.
- [ ] **Gate**: read-old/write-current verified; version-mismatch negative tests error cleanly;
      G115 count unchanged. Entry-tier decoder may fold into 3.3 if it stays one concrete decoder.

### Step 3: Subtask 3.3 — widen, fix, re-enable gate
- [ ] Widen `DevID`/`Inode`; bump format version; remove ingest truncation casts; widen dedup key.
- [ ] Remove G115 exclude; `golangci-lint run ./...` clean.
- [ ] **Gate**: full-width dev/ino round-trips; **a v3 fixture decodes with *every* post-Ino
      field (Mode/UID/GID/FileSize/flags/hash/path) correct, and is routed through heap decode —
      not cast through the widened struct**; golden-file check that the v4 writer emits the new
      offsets; dupes correct on >32-bit inodes.

### Step 4: Parent close-out
- [ ] All subtasks merged; each subtask's gate result recorded in its `f-`/`g-` files and cited
      here; parent success criteria (a-task-plan) verified end-to-end.

## Code Changes
Deferred to subtask plans — this parent plan intentionally carries no per-call-site before/after.

## Test Coverage
**See e-testing-plan.md** (parent-level cross-subtask test strategy); per-subtask matrices live
in each subtask's `e-` plan.

## Validation Criteria
- Each subtask passes its gate above before the next begins (gates enforce the design's
  correctness-first ordering).
- **Every subtask gate includes a malformed-input negative test** proving the migrated
  bounds/version checks still fire (error, not over-read or panic) — the gates otherwise only
  exercise the happy path.
- G115 site count is diffed against the 3.1 baseline at every subtask boundary.
- Parent success criteria in `a-task-plan.md` all met.

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
The cross-subtask sequencing executed as planned: Step 1 (3.1 extract, G115 63→52), Step 2 (3.2
version-aware dispatch, G115 52 unchanged), Step 3 (3.3 widen + v4 + re-enable G115, whole-tree 0),
Step 4 (parent close-out — this f/g phase). Each subtask passed its gate before the next began, and the
per-subtask G115 enumeration confirmed no narrowing cast was introduced while G115 was off. The
seam-inventory directive ("grep, don't trust this list") held — 3.1 sized from the actual `.Dev`/`.Ino`
grep, not the plan's non-exhaustive list.

## Lessons Learned
The "single version gate" and "layout assertions become version-aware" parent invariants were the right
constraints to push down into every subtask — they prevented a second parallel version gate and made
layout drift a compile error. Full synthesis in j-retrospective.md.
