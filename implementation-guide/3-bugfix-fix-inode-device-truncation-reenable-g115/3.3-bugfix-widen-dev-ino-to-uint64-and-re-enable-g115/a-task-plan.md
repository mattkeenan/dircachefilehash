# Widen Dev/Ino to uint64 and re-enable G115 - Plan
**Task**: 3.3 (bugfix)

## Task Reference
- **Task ID**: internal-3.3
- **Task URL**: N/A (internal task)
- **Parent Task**: 3
- **Branch**: bugfix/3.3-widen-dev-ino-to-uint64-and-re-enable-g115
- **Baseline Commit**: cbfa32f6a7dece5d7f7a37515669f0f74f4e795f
- **Template Version**: 2.1

## Goal
Widen `DevID`/`Inode` to 64-bit (format v4), removing the ingest truncation that makes `dcfh dupes`
silently drop distinct files as hardlinks on large-inode filesystems, and re-enable the gosec G115
gate — closing the parent's Very High backlog item.

## Success Criteria
- [ ] `DevID`/`Inode` are `uint64`; `CurrentIndexVersion` is 4; build-time + `validateLayout` assertions
      assert **v4** sizing, and the post-`Ino` field shift is encoded in the v4 descriptor.
- [ ] No ingest truncation: the `uint32(...)` casts at `binary_entry_scan.go:69-70` are gone, the
      `dupes.go` dedup key is full-width, and the `Dev()`/`Ino()` accessors (+ dcfhfind consumer)
      return 64-bit. `dcfh dupes` no longer collapses two files whose low-32-bit ino collides.
- [ ] Legacy reads decode, not cast: a **v3** fixture loads via `DecodeHeap` with **every** post-`Ino`
      field (Mode/UID/GID/FileSize/flags/hash/path) correct; v4 round-trips byte-identically; an
      out-of-range / version-mismatch / truncated file still errors cleanly (no over-read, no panic).
- [ ] dcfhfix read path consumes the `pkg/format` resolver/descriptor (`entry_workflow_main.go:122`),
      `BEIndexFileIOEntry.readEntryData` (`binary_entry_index_file.go:73`) is routed-or-deleted, and no
      second offset table exists anywhere.
- [ ] G115 removed from `.golangci.yml` `gosec.excludes`; `golangci-lint run ./...` is clean (any new
      G115 site triaged — fixed or justified per-line, never blanket); `StrategyForVersion` and
      `ValidateVersion` version ranges moved to v4 in lockstep.

## Original Estimate
**Effort**: 2-3 days
**Complexity**: High
**Dependencies**: 3.1 (pkg/format extraction) and 3.2 (version-dispatch seam) — both merged into the
parent branch at baseline `cbfa32f`. No external dependencies.

## Major Milestones
1. **Widen + v4 bump**: `DevID`/`Inode` → `uint64`; `CurrentIndexVersion` → 4; per-version layout
   assertions and the v4 field-offset descriptor (every field after `Ino` shifts +8).
2. **Legacy decode path**: add `DecodeHeap` + the concrete v3-entry decoder; flip `StrategyForVersion`'s
   legacy arm from `DecodeZeroCopy` to `DecodeHeap`; wire the load-path switch (current → zero-copy cast,
   legacy → heap decode).
3. **Ingest & consumers**: remove the truncation casts; widen the dedup key and `Dev()`/`Ino()`
   accessors; adopt the resolver in the dcfhfix read path; dispose of `readEntryData`.
4. **Re-enable the gate**: remove the G115 exclude, drive `golangci-lint run ./...` clean, verify
   `dupes` correctness on >32-bit inodes end-to-end, and close the Very High backlog item.

## Risk Assessment
### High Priority Risks
- **Zero-copy cast fires on a legacy file** → uint32-layout bytes misread as a uint64 struct (silent
  corruption). This is the central hazard the read-old/write-new model exists to prevent.
  - **Mitigation**: cast is version-gated to `== current` only (parent invariant); legacy → `DecodeHeap`.
    The gate asserts a v3 fixture is *routed through heap decode* (not just that it reads correctly),
    so a future mis-wire is caught.
- **Field-offset drift**: every field after `Ino` shifts +8 in v4; if descriptor offsets / layout
  assertions are not updated in lockstep, the codec reads corrupt fields.
  - **Mitigation**: per-version descriptor offsets + build-time `Sizeof%8==0`/`validateLayout`
    assertions asserting the correct version's sizing; golden-file check that the v4 writer emits the
    new offsets; v3-fixture every-field-correct gate.

### Medium Priority Risks
- **Re-enabling G115 surfaces sites beyond the known ingest casts**: widening can newly expose latent
  narrowings (e.g. uint64→uint32 at other boundaries) once the gate is live.
  - **Mitigation**: per-subtask G115 enumeration diffed against the 3.1 baseline (52); triage each new
    site individually (fix or `//nolint:gosec` with rationale), never a blanket exclude.
- **dcfhfix repair path on corrupt data**: the descriptor must bounds-check both v3 and v4 layouts when
  the input may be malformed.
  - **Mitigation**: descriptor-declared offsets validated against buffer length (non-bypassable
    `readField`); malformed-input negatives for both versions.

## Dependencies
- 3.1 and 3.2 merged (the `pkg/format` seam, the `StrategyForVersion` resolver, the version-less
  writable header). Present at the baseline commit.
- No external/team dependencies.

## Constraints
- **Parent invariants** (c-design-plan §Cross-subtask correctness): version-gated cast; single version
  gate (`StrategyForVersion`/`ValidateVersion` in lockstep); bounds-checked, non-bypassable decode;
  write-current owned in `pkg/format`.
- **Accepted degradation**: widening fixes *future* under-reporting only — pre-existing v3 entries
  already lost their high `Dev`/`Ino` bits at ingest; the v4 bump rewrites them on the next `update`,
  so there is no separate migration tool. A documented decision, not a surprise.
- Project rules: no `--no-verify`; never disable tests; British prose; `.cwf/version` not committed.

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [ ] **Time**: Will this take >1 week? No — scoped leaf, 2-3 days.
- [ ] **People**: Does this need >2 people? No.
- [x] **Complexity**: 3+ distinct concerns (width change, legacy decode path, lint gate) — triggered.
- [ ] **Risk**: High-risk components needing isolation? The risky legacy-decode logic is already
      isolated inside `pkg/format` by the read-old/write-new design.
- [ ] **Independence**: Can parts be worked on separately? No — the v4 bump is atomic: widening
      without the decode path, or re-enabling G115 without removing the casts, leaves the tree
      non-compiling or the gate inconsistent mid-way.

**Verdict**: One signal (complexity) is triggered, but the pieces are tightly interdependent around a
single atomic v4 bump; further decomposition would create broken intermediate states. Keep as one task.

## Status
**Status**: Finished
**Next Action**: /cwf-design-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All five success criteria met. Net-positive deviations from plan: (1) the legacy layout shipped as three
per-version files (v2/v3/v4) rather than one `legacy_layout.go`; (2) G115 made whole-tree clean (55 sites)
rather than residual-only triage; (3) committed binary goldens + `.gitattributes` for fixture robustness.
One scope item deferred to subtask 3.4 (FileSize/ByteSize→int64). Within the 2-3 day estimate.

## Lessons Learned
The `.cwf/version must NOT be committed` constraint listed in this plan was unsourced folklore (not in
CLAUDE.md, memory, or `.gitignore`); corrected during the retrospective — it is now committed normally.
Golden-first ordering (capture v3 before the v4 bump) preserved the irreplaceable decode oracle.
