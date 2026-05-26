# Widen Dev/Ino to uint64 and re-enable G115 - Implementation Execution
**Task**: 3.3 (bugfix)

## Task Reference
- **Task ID**: internal-3.3
- **Task URL**: N/A (internal task)
- **Parent Task**: 3
- **Branch**: bugfix/3.3-widen-dev-ino-to-uint64-and-re-enable-g115
- **Template Version**: 2.1

## Goal
Execute the implementation following the plan in d-implementation-plan.md and e-testing-plan.md.

## Execution Checklist
- [ ] Read d-implementation-plan.md and e-testing-plan.md thoroughly
- [ ] Verify all prerequisites met
- [ ] Execute implementation steps sequentially
- [ ] Update "Actual Results" for each step
- [ ] Document any deviations from plan
- [ ] Update status to "Implemented" when complete

## Implementation Steps (from d-implementation-plan.md)

[Reference to planning file, updated with actual results]

## Actual Results

### Step 0: Capture the v3 golden (IRREVERSIBLE — done first)
- **Planned**: Generate `pkg/format/testdata/v3.idx` from the real v3 writer before Step 1 bumps the version.
- **Actual**: Built `dcfh` at baseline (`CurrentIndexVersion`==3), ran `init`+`update` on a deterministic
  tree (`file1.txt`="alpha\n", `file2.txt`="beta\n", `sub/file3.txt`="gamma\n", all mode 0644). Captured
  `main.idx` → `pkg/format/testdata/v3.idx`: version byte 4@offset16 == 3, EntryCount == 3, 560 bytes.
  Stable content fields for the decode test: sizes 6/5/6; SHA-1 alpha=d046cd9b…, beta=6c007a14…,
  gamma=37f385b0…; paths file1.txt / file2.txt / sub/file3.txt.
- **Deviations**: First `dcfh update <dir>` from the repo root operated on the wrong repo context (files
  stayed untracked); rerun from inside the tree produced the populated index. No plan change.

### Step 1: Width + version + assertions + per-version layout
- **Planned**: Widen `vocabulary.go`; bump `constants.go`; add `legacyEntry`/`layoutForVersion`/legacy
  build assertion; fix the `Size<48` floor → `minEntrySize`.
- **Actual**: `DevID`/`Inode` → `uint64`; `CurrentIndexVersion` 3→4. `entry.go:97` floor now
  `uint32(minEntrySize)`. `RelativePath`/`validateLayout` assertions unchanged (already struct-derived).
- **Deviation (user-directed)**: instead of one `legacy_layout.go` + a single `legacyEntry`, the layout
  is split into **per-version files** `v2_layout.go` / `v3_layout.go` / `v4_layout.go` + shared
  `entry_layout.go` (`entryLayout` type + `layoutForVersion`). `entryV2`/`entryV3` are real structs
  (frozen, 32-bit Dev/Ino) pinned byte-identical by a build assertion; `entryV4 = Entry` (alias to the
  canonical write type). `layoutForVersion` is an explicit per-version switch (future-proof for v5).

### Step 2: Legacy transcoder
- **Actual**: `transcode.go` — `TranscodeLegacyIndex` self-validates (len before header cast, per-entry
  Size + region bounds), grows the output incrementally (never sizes an allocation from the untrusted
  `EntryCount`), preserves v3 `Timestamp` / zeroes it for v2. `transcodeEntry` reads via the version
  layout's offsets and widens Dev/Ino uint32→uint64.

### Step 3: Decode-on-load branch + cleanup guard
- **Actual**: `checkEntryRegionAccess` returns `DecodeStrategy`; both loaders
  (`openAndValidateIndex`, `loadIndexFromFileWithTracking`) branch — `DecodeHeap` transcodes, releases the
  original mapping, and wraps the v4 image in a `heapBacked` `mmapIndexFile`. `Cleanup()` skips
  `unix.Munmap` when `heapBacked`. Checksum is verified on the original bytes before transcode; the new
  fork runs error-cleanup (does not copy the pre-existing checksum-arm leak).
- **Dispatch**: `version_dispatch.go` — `DecodeHeap` is now a live value; the legacy arm returns it.

### Step 4: Widen the hand-typed sites (compiler-driven)
- **Actual**: `BinaryEntryInterface`/`FilterEntry` `Dev()/Ino()` → `uint64`; every implementer
  (`BESkiplistEntry`, `BEIndexFileMmapEntry`, `BEScanEntry`, `scanFilterEntry`, `binaryEntryAdapter`,
  `entryInfoAdapter`). Ingest casts dropped (`binary_entry_scan.go:69-70`, `scan.go` signature+cast).
  `dupes` dedup key `[2]uint32`→`[2]uint64`. `EntryInfo.Dev`→uint64. `go build ./...` green.

### Step 5: Version-aware SafeEntry + dcfhfix read/write
- **Actual**: `NewSafeEntry(data, idx, offset, version)` resolves the layout via `layoutForVersion`;
  `NewValidatedEntry` threads `version` from the validated header at all four call sites.
  `createTempIndexWithHeader` now stamps a **v4** header (was copying the source header → would have
  produced a v3 header over v4 entries). The resync floor in `trySkipToNextEntry` is version-aware via
  `format.MinEntrySizeForVersion`. (The `finalizeTempIndex:119` floor stays v4 — it runs on the
  freshly-written v4 temp file, so the v4 minimum is correct there.)
- **Bug found & fixed mid-exec**: `GetDev`/`GetIno` used `readField[DevID]` (8 bytes) regardless of
  version, so a legacy entry over-read Dev into Ino (`0x99887766aabbccdd`). Added `narrowDevIno` to
  `entryLayout`; `GetDev`/`GetIno` read uint32 and widen for legacy. Caught by the reframed v2 test.

### Step 6: Delete dead code
- **Actual**: removed `pkg/binary_entry_index_file.go` (`BEIndexFileIOEntry`, no production callers) +
  its `_test.go`; trimmed the `BEIndexFileIO` enum + `String()` arm and 2 test references.

### Step 7: Re-enable the gate (G115)
- **Actual**: removed the G115 exclude from `.golangci.yml`. The Dev/Ino truncation class is fixed
  **structurally** (widened type, casts removed) — zero suppressions on Dev/Ino. **User chose whole-tree
  clean**: all 55 surfaced sites annotated with per-line `//nolint:gosec // G115: <rationale>` (bounded
  mmap/struct offsets, fds, non-negative file sizes/times, packed wall-time). `golangci-lint run ./...`
  reports **0 G115**; the `--new` enforcement gate is fully clean. Remaining `--all` issues (2 `cyclop`,
  1 `unparam`) are **pre-existing** in untouched functions (`parseTestToken`, `resolveOneSelector`,
  `createTestEntry`) — never `--new`-gated, unrelated to this task; surfaced, not bundled.

### Step 8: Regression
- **Actual**: `go test ./pkg/... ./cmd/...` green; canonical race gate
  `GOFLAGS=-gcflags=all=-d=checkptr=0 go test -race -short ./...` green. Existing tests updated to the new
  reality: `TestCachePortability` 136→144 (the +8 widen); round-trip renamed `TestRoundTrip_V4_ByteIdentical`;
  `TestRoundTrip_V2_ParseOffset`→`TestParseOffset_V2HeaderSize` reframed onto a new `layLegacyEntry`;
  `TestStrategyForVersion` legacy arms now `DecodeHeap`; `NewSafeEntry` test call sites + redundant
  `uint64()` casts fixed. **End-to-end**: fresh `dcfh update` writes version byte 4; the v3 golden loads
  via the transcode path with correct paths/sizes/modes (read-v3 / write-v4 proven).

## Agreed follow-up (out of scope for 3.3)
- **Subtask 3.4** (to create when 3.3 wraps): move `ByteSize` (FileSize) from `uint64` → `int64` in v4
  + core code to match `off_t`/`os.FileInfo.Size()`, ring-fencing the uint64→int64 cast in the v2/v3
  transcoder (mirrors the Dev/Ino pattern). Retires ~6 of the G115 suppressions structurally;
  on-disk-compatible (no format bump — v4 stays v4). User-agreed.

## Blockers Encountered

None. (One decode bug — `GetDev`/`GetIno` width — found and fixed during exec, see Step 5.)

## Security Review

**State**: no findings

The `security-review-changeset` helper returned an empty changeset (CwF v1.1.155 has the
uncommitted-diff bug, fixed in v1.1.163; it also scopes to CWF-internal/script files by design, so a
pure-Go changeset surfaces little). Worked around it: staged the changes and ran the
`cwf-security-reviewer-changeset` agent on the focused untrusted-input/security diff (transcode.go,
entry_layout.go, version_dispatch.go, the index.go load-path + Cleanup guard, .golangci.yml; 434 lines).

Verbatim agent verdict:

> no findings
> Reviewed the implementation-phase changeset (`pkg/format/transcode.go`, `entry_layout.go`,
> `v2_layout.go`, `v3_layout.go`, `v4_layout.go`, `version_dispatch.go`, and `pkg/index.go`, plus
> `.golangci.yml`) against threat categories (a)-(e). The legacy-index transcode path — the only new
> untrusted-input parser — is bounds-checked at every step, fails closed on unknown versions, never lets
> `EntryCount` size an allocation, and verifies the checksum on original bytes before transcoding. The
> `heapBacked` discriminator correctly prevents a `munmap` on GC-allocated memory. The G115 gosec rule is
> correctly reactivated with per-line rationaled suppressions per the repo's documented policy.
>
> Pattern note (not a finding; pre-existing, not introduced by this diff): in `openAndValidateIndex` at
> `pkg/index.go:357-358`, a checksum-verification failure returns `nil, nil, err` without calling
> `cleanup()`, leaking the mmap + fd. This path predates the change (the relocated block had the same
> omission) and is safe at present because the process typically exits on a load failure — but if a
> future caller retries loads in a long-lived process, audit it then. Every other error arm on these load
> paths (`cleanup()` / `DecRef()`) is correct, including the new transcode-failure arms.

**Disposition**: no action required for 3.3. The pre-existing checksum-arm leak is out of scope (not
introduced here; the d-plan explicitly told the new fork not to copy it, which it doesn't) — noted for a
possible future hardening pass.

### Re-run (post-commit, full-state)

Re-ran the review after f (68aa823) and g (b6c13b4) were committed. The `security-review-changeset
--phase=implementation` helper now reports `reviewed 0 files, 0 lines, anchor=cbfa32f, includes
uncommitted` — confirming the empty changeset is **not** the v1.1.155 uncommitted-diff bug (it sees the
committed state) but the by-design CWF-internal/script-only scoping: the 3.3 changeset is pure Go
application code. Re-fed the focused untrusted-input parsing surface (cbfa32f→HEAD: transcode.go,
codec.go, entry_layout.go, version_dispatch.go, entry.go, header.go, index.go; 699 lines) to the
`cwf-security-reviewer-changeset` agent, which read the files at HEAD directly.

**State**: no findings.

The agent verified, with the now-complete code: per-entry bounds gates (`size >= lay.minSize`, `<= 4096`,
`offset+size <= len`) before any read; `EntryCount` never sizes an allocation (output grows by `append`);
`narrowDevIno` reads legacy Dev/Ino as uint32 then widens (no adjacent-field spill); checksum verified on
original bytes before transcode; `heapBacked` prevents `munmap` of GC memory; post-transcode the
`binaryEntry` cast only ever sees v4-layout bytes. Two new pattern-risk notes (category (e), "safe here
because <invariant>", no action now): (1) `pkg/index.go` casts a v2 (88-byte) header to a 104-byte array
view but only dereferences `[:checksumOffset]` (28) — audit if `checksumOffset` ever grows past 88 for a
legacy header; (2) `cmd/dcfhfix/entry_processor_workflow.go` reads an entry size via `unsafe` during
resync, safe because each read is preceded by a co-located `+4` bounds check — audit future reuse where
that check is not co-located. The original run's checksum-arm leak note still stands (pre-existing, out of
scope).

## Deferral Check
Before marking status=Finished, verify:
- [ ] All steps from d-implementation-plan.md executed
- [ ] All success criteria from a-task-plan.md met
- [ ] All requirements from b-requirements-plan.md addressed (if applicable)
- [ ] All design guidance in c-design-plan.md followed (if applicable)
- [ ] No planned work deferred without user approval
- [ ] If work deferred: Follow-up task created and linked

**If deferral required**: Get user approval, document rationale, create follow-up task.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
v4 widen + legacy transcode + G115 re-enable delivered (commit 68aa823, 52 files). One in-phase defect
(`GetDev`/`GetIno` width over-read) found and fixed. Whole-tree G115 clean. Security review: no findings
(in-phase manual review + post-commit re-run over the full parsing surface).

## Lessons Learned
Reading a field at a layout offset is not enough — the read *width* must match the on-disk field width.
The empty security-review changeset (v1.1.155 uncommitted-diff bug + by-design Go-code scoping) needed a
manual agent workaround; CwF v1.1.163 upgrade backlogged.
