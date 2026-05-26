# Changelog

Per-task record of changes to dircachefilehash, maintained through the CWF workflow. Entries are added as tasks complete.

Pre-CWF history — organised by release version under Keep a Changelog / Semantic Versioning, plus the earlier rejected-design log — is preserved in [`docs/changelog-old.md`](docs/changelog-old.md).

## Task 3: Fix inode/device truncation and re-enable gosec G115

### Status: Complete (parent task; completed 2026-05-26 — ~4 calendar days across 3 subtasks vs a ~1.5–2 week single-task estimate)

### Impact: `dcfh` stored `Dev`/`Ino` as `uint32` and keyed hardlink dedup on a truncated `[2]uint32`, silently collapsing distinct files as hardlinks on large-inode filesystems (XFS, Btrfs, large ext4). This parent task eliminates that by widening `Dev`/`Ino` to 64-bit and, to make the change single-sited, first encapsulates the entire versioned on-disk format behind one module (`pkg/format`) — then re-enables the gosec G115 gate Task 2 had deferred. Closes the **Very High** inode/device-truncation backlog item.

### Changes (delivered across three subtasks — see their entries below for detail)
- **3.1 (chore)** — extract `pkg/format` as the single, cycle-free owner of the on-disk layout (vocabulary, canonical `Entry`/`Header`, bounds-checked codec, version constants/validation); migrate core + dcfhfix onto it; delete both dcfhfix duplicates and the parallel offset table. No width/version/behaviour change.
- **3.2 (feature)** — add the owned `version → DecodeStrategy` dispatch seam and give `pkg/format` ownership of the write version, so the v4 bump becomes "flip one dispatch arm". No format change.
- **3.3 (bugfix)** — widen `DevID`/`Inode` to `uint64` (on-disk format **v4**); legacy v2/v3 read via a bounds-checked heap transcode; `dupes` key `[2]uint64`; ingest truncation removed; gosec G115 re-enabled and whole-tree clean.

### Notable
- All five parent success criteria verified end-to-end on the merged branch (f-implementation-exec.md): full-width dev/ino + full-width dupes key, single-module format ownership, v2/v3→v4 heap-transcode round-trip, G115 active with 0 whole-tree findings, full race suite green.
- The decomposition retired both named high-priority risks structurally: the no-behaviour-change extraction (3.1) landed and was verified green before any width/version change (3.3), and the type-alias vocabulary made the widen a compiler-driven audit rather than a manual cross-codebase sweep.
- Five pre-existing latent defects (dcfhfix `GetPath` offset + header over-read, a v3-header truncation over-read, a use-after-munmap, and a `GetDev`/`GetIno` width over-read) were found and fixed in-flight via planned gates; 0 escaped.
- G115's whole-tree count went 63 → 52 → 52 → **0**; the Dev/Ino class fixed structurally (zero suppressions on it). Three pre-existing non-G115 findings (cyclop ×2, unparam ×1) in untouched functions remain backlogged — not regressions; the enforcing `--new` gate is clean.

## Task 3.3: Widen Dev/Ino to uint64 and re-enable gosec G115

### Status: Complete (subtask of Task 3; completed 2026-05-25, within the 2–3 day estimate)

### Impact: dcfh stored `Dev`/`Ino` as uint32 and keyed hardlink dedup on a truncated `[2]uint32`, so on large-inode filesystems (XFS, Btrfs, large ext4) two distinct files whose low 32 bits collided were silently treated as hardlinks and one was dropped from `dcfh dupes`. This widens `DevID`/`Inode` to `uint64` (on-disk format **v4**), removes the ingest truncation, and re-enables the gosec G115 gate that was disabled to land Task 2. Closes the parent's **Very High** backlog item.

### Changes
- `pkg/format`: `DevID`/`Inode` `uint32`→`uint64`; `CurrentIndexVersion` 3→4 (every entry field after `Ino` shifts +8). v4 is written exclusively.
- Legacy read-as-transcode: v2/v3 indices load via a new bounds-checked `TranscodeLegacyIndex` into a v4-layout heap image (never cast in place); `StrategyForVersion`'s legacy arm flipped `DecodeZeroCopy`→`DecodeHeap`; both mmap loaders branch on the strategy and back the index with a `heapBacked` `mmapIndexFile` (`Cleanup` never munmaps the GC buffer).
- Per-version layout split: `entry_layout.go` + `v2_layout.go`/`v3_layout.go`/`v4_layout.go`, with build-time assertions pinning v3==v2 and v4==v2+8; `layoutForVersion` is a fail-closed per-version switch. `SafeEntry`/dcfhfix reads are version-aware; `GetDev`/`GetIno` read legacy fields as uint32 and widen (`narrowDevIno`).
- Ingest/consumers: removed both truncation casts (`binary_entry_scan.go`, `scan.go`); `dupes` dedup key is `[2]uint64`; the two accessor interfaces, their implementers, and backing struct fields widened.
- dcfhfix: read-old/write-v4 — `createTempIndexWithHeader` stamps a v4 header (fixing a corrupt v3-header-over-v4-entries output), version threaded into `NewValidatedEntry`/`NewSafeEntry`, two size floors made version-aware.
- Deleted dead `BEIndexFileIOEntry` (a parallel cast site).
- Re-enabled gosec **G115** (`.golangci.yml`): `golangci-lint run ./...` reports 0 G115; the Dev/Ino truncation class fixed structurally (zero suppressions on it), 55 provably-safe sites annotated per-line with rationale.
- Tests: v3 decode-via-`DecodeHeap` (both loaders, every field), v4 round-trip byte-identity, `TranscodeLegacyIndex` positive + fail-closed (incl. oversized `EntryCount` with no allocation), `layoutForVersion` table, dupes-correct-on-large-inodes, heap-backed `Cleanup` no-munmap, dcfhfix v4 stamp. Committed goldens `testdata/v3.idx` (genuine, captured pre-bump) + `testdata/v4.idx`, and `.gitattributes *.idx binary`.

### Notable
- The width fix is structural, not suppression: G115 stays active and the Dev/Ino/EntryCount/Size conversions were resolved by widening, not `//nolint`.
- A real decode defect was caught in exec: `GetDev`/`GetIno` read 8 bytes regardless of version, over-reading a legacy entry's Dev into Ino — fixed with the per-layout `narrowDevIno` flag.
- Accepted degradation: pre-existing v3 entries already lost their high Dev/Ino bits at ingest; the v4 bump rewrites them on the next `update`, so there is no separate migration tool.
- The changeset security review returned an empty changeset (CwF v1.1.155 uncommitted-diff bug + by-design Go-code scoping); reviewed manually (no findings, two category-(e) pattern notes) and a v1.1.163 upgrade was backlogged.
- Follow-up subtask 3.4 backlogged: move `FileSize`/`ByteSize` to `int64` and ring-fence the uint64→int64 cast in the transcoder, retiring ~6 G115 suppressions (on-disk-compatible).

### Retired Backlog Items
#### Inode/device truncation makes dcfh dupes under-report on large-inode filesystems
Retired from BACKLOG (was **Very High**, identified in Task 2). Closed by this task: `DevID`/`Inode` widened to `uint64` (format v4), both ingest truncation casts removed, dedup key now `[2]uint64`, and gosec G115 re-enabled and structurally clean.

## Task 3.2: Add version-aware read/write dispatch seam in pkg/format

### Status: Complete (subtask of Task 3; completed 2026-05-24, within the 1–2 day estimate)

### Impact: The index load path's "cast the mmap to the entry layout" step was implicitly correct only because every shipped version (v2, v3) has a byte-identical entry layout — there was no owned decision tying a version to *how* it is materialised, and no gate rejecting an out-of-range version byte before use. Task 3.2 makes that decision an explicit, single-owned, tested seam (`StrategyForVersion`) and gives `pkg/format` ownership of the write version, so Task 3.3's v4 becomes "flip one dispatch arm", not "add a new load-path branch". No on-disk format, width, or version change.

### Changes
- Added `pkg/format/version_dispatch.go`: `StrategyForVersion(version) → (DecodeStrategy, error)`, a switch-with-default that maps current/recognised-legacy versions to `DecodeZeroCopy` and rejects everything else — never raw-indexed by the untrusted version byte. (`DecodeHeap` is intentionally absent until v4 gives a legacy entry layout to decode in 3.3.)
- `SetHeaderForWritableIndex` dropped its `version` parameter and now sources `CurrentIndexVersion` from `pkg/format`; all three production writers migrated (compiler + grep confirmed no caller passes a version).
- Consolidated the version gate and a header-size bounds guard into one shared `checkEntryRegionAccess`, called by both top-level mmap loaders (`openAndValidateIndex`, `loadIndexFromFileWithTracking`); extracted `parseTrackedEntries` to keep the tracking loader under the cyclop limit.
- Added `pkg/format/version_dispatch_test.go` (resolver table, 100% coverage) and `pkg/version_dispatch_load_test.go` (out-of-range rejection + v3-header truncation, both loaders, race-clean).

### Notable
- The header-size guard closed a **latent v3-header truncation over-read**: an 88–103-byte file with a v3 header passed the old `V2HeaderSize` size check and then panicked slicing `data[104:]`. It now fails closed. The planned truncation test (TC-4) caught a use-after-munmap in the first guard cut — reading `header.Version` after the mmap was released — fixed by forming the error before any unmap.
- gosec G115 unchanged at 52 (the 3.1 baseline); no on-disk format change, so v2/v3 indices still load and write byte-identically.
- Deliberately the *non-speculative* half of the parent's read-old/write-new model: the concrete legacy entry decoder, dcfhfix repair read-path resolver adoption, `BEIndexFileIOEntry.readEntryData` routing, the `Dev`/`Ino` widening, and re-enabling G115 are deferred to Task 3.3, where v4 gives them a divergent layout to exercise.

## Task 3.1: Extract pkg/format as single owner of on-disk layout

### Status: Complete (subtask of Task 3; completed 2026-05-24, well under the 2–4 day estimate)

### Impact: The on-disk layout (entry/header structs, field widths, offset tables, version constants/validation) was duplicated across `pkg` and `cmd/dcfhfix`, so the upcoming inode/device width change (Task 3.3) would have had to be applied in several places. A new `pkg/format` package is now the single, cycle-free owner of that layout; core and dcfhfix alias onto it. No on-disk format, field-width, version, or behaviour change.

### Changes
- Added `pkg/format`: vocabulary aliases (`DevID`/`Inode`/`WallTime`/… — same-width, single owner of width/signedness), canonical `Entry` + `Header` (with all methods), version constants + `headerSizeForVersion`/`ValidateVersion`, and a two-tier bounds-checked `SafeEntry` codec (generic `readField`/`writeField`). Verified cycle-free via `go list`.
- Migrated `pkg` (`type binaryEntry = format.Entry`, `type indexHeader = format.Header`, thin forwarders, `asFilterEntry` → free function, exported clean-bit methods) and `cmd/dcfhfix` (deleted the duplicate `binaryEntry`/`indexHeader` + the parallel `unsafe.Offsetof` table; now imports `pkg/format`).
- Fixed two latent dcfhfix defects surfaced by the consolidation: `GetPath` read the path from the unused trailing `Path[8]` field instead of after the fixed struct; the 96-byte header duplicate over-read 8 bytes when cast to `[104]byte` in the write path.
- Added round-trip (v3 byte-identity), version-offset (v2 entries at 88, v3 at 104), header-size-invariant, and codec bounds-tier tests.

### Notable
- The type-alias strategy (`type T = U`) made the migration a compile-checked near-zero-diff — the compiler, not manual auditing, found every width-coupled call site. Go's prohibition on declaring methods on an alias to an out-of-package type was the one wrinkle: resolved by owning the type in `pkg/format` and aliasing everywhere else.
- gosec G115 went 63 → 52 (the narrowing conversions were consolidated, not added); the gate's "must not increase" intent is satisfied. G115 stays excluded until Task 3.3 widens `Dev`/`Ino` to `uint64`.
- Does not complete the Very High backlog item (inode/device truncation + re-enable G115) — that is Task 3.3; 3.1 is the enabling extraction. Partially advances "Fix primitive + dcfhfix restructure" by moving the bounds-checked accessor into `pkg/`.

## Task 2: Adopt full Go pre-commit hook and security review

### Status: Complete

### Impact: The repo ran without a security-focused static-analysis gate. `gosec` is now wired into `.golangci.yml` (so it fires on every `golangci-lint` run, including the staged `--new` pre-commit path) and contributes zero findings to a clean tree while staying active for new code. Adopting it immediately surfaced one genuine latent bug (inode/device truncation in `dcfh dupes`, now backlogged Very High).

### Changes
- Enabled `gosec` in `.golangci.yml`. Architectural/intentional rules excluded with documented rationale (G103 unsafe/mmap, G304 file-scanner paths, G401/G505 git-compatible SHA-1) plus G115 deferred to a backlogged bug fix. Test-only false positives scoped via `{linters:[gosec], path: _test\.go}`.
- Suppressed 26 production false positives with per-line `//nolint:gosec // Gxxx: rationale` (perms on non-secret `.dcfh/` files, `DirEntry.Name()` base paths, opt-in localhost pprof, fixed-`"ssh"`-binary subprocess) — never a blanket disable; perms/subprocess rules stay active for new code.
- Lifted golangci-lint's issue-display caps (`max-same-issues: 0`, `max-issues-per-linter: 0`) so the security gate never silently hides a duplicate finding.
- Bumped the Go toolchain to `go1.26.3` (go.mod `toolchain` directive) to clear `GO-2026-4971`, which the hook's govulncheck step correctly blocked on.
- Documented the dual security-review process (gosec floor + CWF `cwf-security-reviewer-changeset`) in `CLAUDE.md`.

### Notable
- Measure security linters through the enforcement path, not standalone: setting `gosec.excludes` activates gosec's full ruleset, and golangci-lint's default `max-same-issues: 3` had hidden over half the findings during planning (true count was 26 sites, not ~12).
- Read the code, don't trust the verdict: per-finding review found exactly one real bug among ~230 raw findings and kept suppression precise rather than blanket.
- The pre-commit gate proved itself — govulncheck blocked a freshly-published CVE on unchanged code; resolved via toolchain bump with no `--no-verify`.
- Follow-ups backlogged: Very High (inode/device truncation fix + re-enable G115), High (deliberate suppression-review pass), Low (clear 3 pre-existing non-gosec lint failures).

## Task 1: Conform BACKLOG and CHANGELOG to CWF format

### Status: Complete

### Impact: The 15 existing BACKLOG entries were invisible to the `backlog-manager` tooling — they used the legacy `## Entry:` heading, so `list` returned nothing and `validate` passed only because it recognised zero entries. After conversion all entries are tool-visible and the heading-tree contract is enforced on every change.

### Changes
- Converted `BACKLOG.md` to the CWF heading-tree schema: `## Entry:` → `## Task:`, added a `### Task-Type:` to each of the 15 entries (feature ×8, chore ×6, bugfix ×1), and replaced the self-documenting template header with a one-line intro. All titles and bodies preserved verbatim.
- Archived the version-based `CHANGELOG.md` (Keep a Changelog / SemVer, plus the `## Rejected` design log) to `docs/changelog-old.md` byte-identically, and started this fresh CWF by-task changelog.

### Notable
- The `list` count, not the `validate` exit code, is the real conformance oracle for this kind of migration — a clean `validate` on zero recognised entries is a false positive.
- Archive-then-recreate at the same path defeats git's `R100` rename detection; archive integrity was verified by blob-hash equality instead.
- `pkg/ignore.go:106`'s stale "see CHANGELOG" reference was left untouched (out of scope for a docs chore) and logged as a Low-priority follow-up.
