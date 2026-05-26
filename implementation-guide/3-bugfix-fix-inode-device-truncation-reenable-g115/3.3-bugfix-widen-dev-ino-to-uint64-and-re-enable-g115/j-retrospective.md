# Widen Dev/Ino to uint64 and re-enable G115 - Retrospective
**Task**: 3.3 (bugfix)

## Task Reference
- **Task ID**: internal-3.3
- **Task URL**: N/A (internal task)
- **Parent Task**: 3
- **Branch**: bugfix/3.3-widen-dev-ino-to-uint64-and-re-enable-g115
- **Template Version**: 2.1
- **Retrospective Date**: 2026-05-25

## Executive Summary
- **Duration**: ~2 days (estimated 2-3 days; within estimate). Planning + design 2026-05-24; implementation, testing, retrospective 2026-05-25.
- **Scope**: Delivered the full leaf scope — `DevID`/`Inode`→`uint64` (format **v4**), legacy v2/v3 read-as-transcode, ingest/accessor/dedup widening, version-aware dcfhfix read+write, dead-code deletion, gosec G115 re-enabled. Three additions beyond plan (below); one deferral (FileSize→int64, subtask 3.4).
- **Outcome**: Success. Closes parent Task 3's **Very High** backlog item (inode/device truncation). 10/10 test cases pass; race gate green; `golangci-lint run ./...` reports 0 G115.

## Variance Analysis
### Time and Effort
- **Estimated**: Planning 2-3 days total (bugfix workflow: a/c/d/e/f/g/j; no b/h/i).
- **Actual**: a+c on 2026-05-24; d+e+f+g+j on 2026-05-25. ~2 calendar days.
- **Variance**: Within estimate. The atomic v4 bump compiled green on the first full pass; the only mid-exec correction was the `GetDev`/`GetIno` width bug (caught by a reframed test, fixed same session).

### Scope Changes
- **Additions**:
  1. **Three per-version layout files** (`v2_layout.go`/`v3_layout.go`/`v4_layout.go` + `entry_layout.go`) instead of the planned single `legacy_layout.go`. User-directed for conceptual clarity and future-version divergence headroom; v2/v3 pinned byte-identical by a build assertion. `layoutForVersion` became an explicit per-version switch (case 2/3/Current) rather than a range arm.
  2. **Whole-tree G115 clean** (all 55 sites annotated) rather than triaging only residual findings against the 52 baseline. User-directed; the Dev/Ino class fixed structurally (zero suppressions on it).
  3. **Committed binary goldens + `.gitattributes`** (`testdata/v3.idx`, `testdata/v4.idx`, `*.idx binary`) per the e-plan robustness section.
- **Removals / deferrals**:
  1. **FileSize/ByteSize → int64** deferred to **subtask 3.4** — matches `off_t`, ring-fences the uint64→int64 cast in the v2/v3 transcoder, retires ~6 of the 55 G115 suppressions. On-disk-compatible, no format bump.
  2. **Stale tracked root debris** (`./cache.idx`, `./cache-2.idx`) flagged in the e-plan was surfaced but not removed — deferred (see Recommendations).
- **Impact**: Additions improved clarity/robustness at modest extra file count; no timeline slip.

### Quality Metrics
- **Test Coverage**: New untrusted-input/dispatch code well covered — `TranscodeLegacyIndex` 88.9%, `transcodeEntry` 100%, `layoutForVersion` 100%, `StrategyForVersion` 100%, `NewSafeEntry` 94.4%, `GetDev` 100%.
- **Defect Rate**: One defect found and fixed in-phase (`GetDev`/`GetIno` read 8 bytes regardless of version, over-reading legacy Dev into Ino). No escaped defects.
- **Security**: `golangci-lint run ./...` = 0 G115; changeset security review (re-run post-commit over the full parsing surface) = no findings, two category-(e) pattern notes (no action).

## What Went Well
- **The read-old/write-new transcode held.** Legacy bytes are never cast as v4; the transcode produces a v4 heap image that the existing `collectEntryRefs`/ref/skiplist walk consumes verbatim — no parallel legacy path.
- **Compiler-driven widen.** Keeping `DevID`/`Inode` as `uint64` *aliases* turned every width-coupled call site into a compile error; the c/d "Hand-edited sites" enumeration correctly listed the non-propagating sites (interfaces, struct fields, the `scan.go:300` cast, the `entry.go` floor).
- **Plan review earned its keep.** The c/d Step-8 reviews caught, before exec, the dcfhfix corrupt-output bug (v3 header stamped over v4-shaped entries) and the `heapBacked` Cleanup guard (fd-nil does not discriminate a heap slice from a read-only mmap).
- **Golden-first ordering.** `testdata/v3.idx` was captured from the real v3 writer *before* the bump removed the ability to emit v3 — the irreplaceable decode-compat oracle was preserved.
- **G115 fixed structurally on the bug class** — zero suppressions on any Dev/Ino/EntryCount/Size conversion.

## What Could Be Improved
- **Security-review tooling friction.** `security-review-changeset` returned an empty changeset both in-phase (v1.1.155 uncommitted-diff bug) and post-commit (by-design CWF-internal/script-only scoping over a pure-Go diff), forcing a manual agent review twice. → CwF **v1.1.163** upgrade backlogged.
- **A duplicate BACKLOG entry was created.** The cyclop/unparam follow-up I added duplicated a pre-existing Task-2 entry ("Clear pre-existing full-tree golangci-lint failures"); my dedup search was too narrow (grepped my own title strings, not the full list). Reconciled in this retrospective (duplicate deleted, original kept). Lesson: list the whole backlog before adding.
- **Unsourced "standing constraint" enforced for several commits.** `.cwf/version must NOT be committed` propagated through compaction summaries into the a/c/d plans and was applied mechanically until challenged — it is not in CLAUDE.md, memory, or `.gitignore`, and no user message established it. Corrected: `.cwf/version` is now committed; a memory records the correction.
- **Stale root debris not removed** (`./cache.idx`, `./cache-2.idx`) — flagged in e-plan, deferred.

## Key Learnings
### Technical Insights
- **Width-aware field reads.** Reading a narrower on-disk field (legacy 32-bit Dev/Ino) with a wider in-memory type spills into the adjacent field. The fix is a per-layout `narrowDevIno` flag: legacy reads `uint32` then widens; v4 reads the full 64 bits. This hazard is independent of the file-split decision.
- **Per-version layout split.** Three small declarative files (frozen struct + build assertion per version) trade a little duplication for clarity and isolate a future version's divergence — a deliberate "duplication over premature abstraction" call.
- **Untrusted-count discipline.** `TranscodeLegacyIndex` never sizes an allocation from the header `EntryCount`; the output grows per validated entry, so a crafted `0xFFFFFFFF` count errors instead of amplifying memory.

### Process Learnings
- **Capture-the-golden-first** is mandatory when a format bump destroys the producer of the old format. Ordering Step 0 before Step 1 preserved the oracle.
- **Verify carried-over "standing constraints" against their actual source before enforcing.** Compaction summaries can propagate unsourced folklore; a quick check (CLAUDE.md / memory / .gitignore / transcript) is cheap relative to enforcing a wrong rule across commits.
- **List the whole backlog before adding a follow-up** to avoid duplicates.

### Risk Mitigation Strategies
- The central silent-corruption hazard (zero-copy cast firing on a legacy file) is structurally prevented: the cast is version-gated to `== current`, and a test asserts a v3 fixture is *routed through* `DecodeHeap` (`heapBacked`), not merely that it reads correctly. The mis-wire is caught, not just the symptom.

## Recommendations
### Process Improvements
- Upgrade CwF to v1.1.163 before the next task's exec phases so the security-review changeset resolves correctly (backlogged).
- When recording follow-ups, run `backlog-manager list` first and reconcile against existing entries.

### Tool and Technique Recommendations
- Keep the build-time per-version size assertions (`v3 == v2`, `v4 == v2 + 8`) — they make layout drift a compile error rather than a runtime decode bug.

### Future Work
- **Subtask 3.4** (backlogged, ready to promote via `/cwf-new-subtask 3 3.4`): move `FileSize`/`ByteSize` to `int64` in v4 and core code; ring-fence the uint64→int64 cast inside the v2/v3 transcoder; retire ~6 G115 suppressions. On-disk-compatible.
- **CwF v1.1.163 upgrade** (backlogged, Medium).
- **Remove stale tracked root debris** `./cache.idx` / `./cache-2.idx` (unloadable v1 / conversion-tool v2; superseded by `pkg/format/testdata/`).

## Status
**Status**: Finished
**Next Action**: Task complete — suggest merge to parent (Task 3 branch)
**Blockers**: None identified
**Completion Date**: 2026-05-25
**Sign-off**: Matt Keenan (with Claude)

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Archived Materials
- Planning: a-task-plan.md, c-design-plan.md, d-implementation-plan.md, e-testing-plan.md
- Execution: f-implementation-exec.md (incl. post-commit security re-run), g-testing-exec.md
- Commits (pre-squash): 1d457b0 (a), 7d483f0 (c), da2a130 (d), fa366f3 (e), 68aa823 (f), b6c13b4 (g), ff7318f (security re-run + backlog + .cwf/version)
- Baseline: cbfa32f (Task 3.2 tip)
