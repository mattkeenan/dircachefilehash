# Fix inode device truncation re-enable G115 - Retrospective
**Task**: 3 (bugfix)

## Task Reference
- **Task ID**: internal-3
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: bugfix/3-fix-inode-device-truncation-reenable-g115
- **Template Version**: 2.1
- **Retrospective Date**: 2026-05-26

## Scope of this retrospective
Parent / **cross-subtask** reflection: how the decomposition, the shared `pkg/format` seam, and the
correctness-first sequencing performed across 3.1/3.2/3.3 as a whole. Per-subtask detail lives in each
subtask's own `j-retrospective.md`; this synthesises the parent-level outcome and learnings.

## Executive Summary
- **Duration**: ~4 focused calendar days across the three subtasks (3.1 ~1 session 2026-05-23/24;
  3.2 ~1 day 2026-05-24; 3.3 ~2 days 2026-05-24/25; parent close-out 2026-05-26). **Estimated as a
  single task: ~1.5–2 weeks** (a-task-plan), High complexity.
- **Scope**: Delivered in full. `Dev`/`Ino` widened to 64-bit (on-disk format **v4**); the versioned
  on-disk layout (types, widths, signedness, per-version offsets) now has a single owner in
  `pkg/format`; legacy v2/v3 indices read via a bounds-checked heap transcode; `dcfh dupes` keys on
  full-width values; gosec **G115 re-enabled and whole-tree clean**.
- **Outcome**: Success. Closes the **Very High** inode/device-truncation backlog item (distinct files
  on large-inode filesystems were being collapsed as hardlinks). All five parent success criteria met;
  full race suite green; whole-tree 0 G115.

## Variance Analysis
### Time and Effort
- **Estimated (single task)**: ~1.5–2 weeks. The estimate explicitly assumed a wide, risky refactor
  blast radius (struct + ~10 interface implementers + serialiser + dcfhfix's parallel table) plus a
  multi-version on-disk format change.
- **Actual (decomposed)**: ~4 calendar days total — 3.1 well under its 2–4 day estimate, 3.2 within
  1–2 days, 3.3 within 2–3 days.
- **Variance**: Substantially under the single-task estimate. Two factors collapsed it: (1)
  **decomposition** isolated the risky width/version change behind a no-behaviour-change extraction
  that was verified green first; (2) the **type-alias vocabulary** turned the predicted "wide blast
  radius" into a compiler-driven audit — narrowing stayed a compile error, so the feared hidden
  width-coupled call site never materialised. The high-priority risk (multi-version misparse) was
  retired by the golden-file round-trip gate plus version-gated casting, not by extra time.

### Scope Changes
- **Additions (latent bugs fixed in-flight across subtasks, none regressions)**:
  - 3.1 — dcfhfix `GetPath` read at the wrong offset (124 vs 136); dcfhfix header 8-byte over-read
    (96-byte duplicate cast as 104). Both closed by adopting `pkg/format`'s canonical types.
  - 3.2 — latent v3-header truncation over-read (88–103-byte file panicked on `data[104:]`), now
    fails closed; a use-after-munmap introduced and caught in-phase by the planned truncation test.
  - 3.3 — `GetDev`/`GetIno` read 8 bytes regardless of version (over-read legacy Dev into Ino),
    fixed with a width-aware `narrowDevIno` per-layout flag.
- **Additions (user-directed, beyond the parent design sketch)**:
  - 3.3 — three per-version layout files (`v2/v3/v4_layout.go` + `entry_layout.go`) instead of one
    `legacy_layout.go`; whole-tree G115 clean (all 55 sites) rather than triaging against the 52
    baseline; committed binary goldens + `.gitattributes`.
- **Removals / deferrals (by design)**:
  - 3.2 — concrete legacy *entry* decoder deferred to 3.3 (no divergent entry layout existed until
    v4 — building it earlier would be speculative generality). This resolved the parent design's
    "3.2 may fold into 3.3" boundary note.
  - 3.3 — `FileSize`/`ByteSize` → `int64` deferred to **subtask 3.4** (on-disk-compatible; would
    ring-fence the signed↔unsigned cast in the transcoder and retire ~6 G115 suppressions).
- **Impact**: Net positive — the extraction surfaced and closed five latent defects in the
  repair/load paths while leaving the production read/write fast path unchanged. No timeline slip.

### Quality Metrics
- **Test Coverage**: new untrusted-input/dispatch code in `pkg/format` well covered
  (`TranscodeLegacyIndex` 88.9%, `transcodeEntry`/`layoutForVersion`/`StrategyForVersion`/`GetDev`
  100%, `NewSafeEntry` 94.4%); full regression suite green at every subtask gate.
- **Defect Rate**: 5 pre-existing latent defects found-and-fixed in-flight; **0 escaped**. Every one
  was caught by a planned gate (round-trip, truncation negative, reframed width test) — the gates
  earned their place.
- **Static security**: G115 63 → 52 (3.1) → 52 (3.2) → **0** (3.3, whole-tree), the Dev/Ino class
  fixed structurally with zero suppressions on it.
- **Performance**: current-version (v4) load stays zero-copy; the heap transcode fires only on the
  rare pre-v4 legacy branch.

## What Went Well
- **Decomposition along the `pkg/format` public contract.** The parent design fixed the seam first;
  each subtask then built against a stable contract and landed behind its own passing gate, with no
  subtask depending on another's internal weeds. Correctness-first ordering (extract → version-aware
  dispatch → widen) held exactly as planned.
- **Type-alias strategy was the whole ballgame.** Owning `DevID`/`Inode` in `pkg/format` and aliasing
  everywhere else kept the on-disk struct zero-copy-interchangeable while making the widen a
  compiler-checked edit. This is the reusable pattern for any future on-disk type change.
- **Plan-review subagents caught real bugs before exec** (3.3): the dcfhfix corrupt-output case (v3
  header stamped over v4-shaped entries) and the `heapBacked` Cleanup guard (fd-nil cannot
  distinguish a heap slice from a read-only mmap).
- **Golden-first capture.** `testdata/v3.idx` was taken from the real v3 writer *before* the v4 bump
  removed the ability to emit v3 — preserving the irreplaceable decode-compat oracle.
- **Silent-corruption hazard structurally prevented.** The zero-copy cast is version-gated to
  `== current`; a test asserts a v3 fixture is *routed through* `DecodeHeap` (`heapBacked`), not
  merely that it reads correctly — the mis-wire is caught, not just its symptom.

## What Could Be Improved
- **Security-review tooling friction recurred in every subtask.** `security-review-changeset`
  returned an empty changeset on the pure-Go diffs (by-design CWF-internal/script-only scoping, plus
  the CwF v1.1.155 uncommitted-diff bug), forcing manual agent reviews. → **CwF v1.1.163 upgrade
  backlogged.**
- **A duplicate BACKLOG entry was created in 3.3** — the cyclop/unparam follow-up duplicated a
  pre-existing Task-2 entry; the dedup search grepped only its own title strings. Reconciled.
- **An unsourced "standing constraint" was enforced for several commits.** `.cwf/version must NOT be
  committed` propagated through compaction summaries into the early plans and was applied mechanically
  until challenged — it is not in CLAUDE.md, memory, or `.gitignore`. Corrected; `.cwf/version` is now
  committed normally and a memory records the correction.
- **The `-race` invocation cost a detour** (3.2): a naive `go test -race` aborts on the pre-existing
  zero-copy `checkptr` path; the canonical `GOFLAGS=-gcflags=all=-d=checkptr=0` gate
  (`.githooks/pre-commit:102`) had to be rediscovered.

## Key Learnings
### Technical Insights
- **Alias vs defined type decides zero-copy layout portability.** `type T = U` keeps the struct
  cross-package interchangeable but cannot host methods on an out-of-package `U`; own the type in
  `pkg/format`, alias it elsewhere, put every method on the owner.
- **Width-aware field reads.** Reading a narrower on-disk field with a wider in-memory type spills
  into the adjacent field; a per-layout `narrowDevIno` flag (read 32, widen) is the fix — a hazard
  independent of the file-split decision.
- **Untrusted-count discipline.** The transcoder never sizes an allocation from the header
  `EntryCount`; output grows per validated entry, so a crafted `0xFFFFFFFF` errors instead of
  amplifying memory.
- **Per-version layout split** (three declarative files + build-time size assertions `v3==v2`,
  `v4==v2+8`) trades a little duplication for clarity and turns layout drift into a compile error.

### Process Learnings
- **Estimate type-system-guarded mechanical refactors by duplicate-definitions-to-delete, not
  call-site count** — the compiler does the blast-radius audit, so day-scale estimates overshoot.
- **Capture the golden before a format bump removes its producer** — order that step zero.
- **Verify carried-over "standing constraints" against their actual source before enforcing** —
  compaction summaries can propagate unsourced folklore; a CLAUDE.md/memory/.gitignore check is cheap.
- **List the whole backlog before adding a follow-up** to avoid duplicates.
- **Reach for the project's canonical test invocation first** when a standard tool behaves
  unexpectedly — the answer was already in the pre-commit hook.

### Risk Mitigation Strategies
- The two named high-priority risks (multi-version misparse; wide refactor blast radius) were both
  retired by structure rather than effort: golden round-trip + version-gated cast for the former,
  the no-behaviour-change extraction + type-alias for the latter. Staging the risky change last,
  behind a green extraction, was the decisive call.

## Recommendations
### Process Improvements
- Upgrade CwF to v1.1.163 before the next task's exec phases so the security-review changeset resolves
  correctly (backlogged, Medium).
- When recording follow-ups, run `backlog-manager list` first and reconcile against existing entries.

### Tool and Technique Recommendations
- Keep the build-time per-version size assertions and the round-trip/version-offset tests as the
  standing regression gate for any future on-disk format change — cheap, and they assert the
  version-aware parse offset directly.

### Future Work
- **Subtask 3.4** (backlogged, ready to promote via `/cwf-new-subtask 3 3.4`): `FileSize`/`ByteSize`
  → `int64` in v4 + core; ring-fence the cast in the transcoder; retire ~6 G115 suppressions.
  On-disk-compatible, no format bump.
- **CwF v1.1.163 upgrade** (backlogged, Medium).
- **Clear pre-existing full-tree golangci-lint failures** (cyclop ×2 — `cmd/dcfhfind/main.go:455`,
  `pkg/filter_run.go:75`; unparam ×1 — `pkg/binary_entry_scan_test.go:200`) in untouched functions
  (backlogged, Low — not Task 3 regressions; the enforcing `--new` gate is clean).
- **Remove stale tracked root debris** `./cache.idx` / `./cache-2.idx` (superseded by
  `pkg/format/testdata/`) — backlogged.

## Status
**Status**: Finished
**Next Action**: Suggest merge of `bugfix/3-fix-inode-device-truncation-reenable-g115` to `main` (user-run)
**Blockers**: None identified
**Completion Date**: 2026-05-26
**Sign-off**: Matt Keenan / Claude Opus 4.7

## Archived Materials
- Planning/design/test: `a-task-plan.md`, `c-design-plan.md`, `d-implementation-plan.md`, `e-testing-plan.md`
- Execution: `f-implementation-exec.md`, `g-testing-exec.md`
- Subtask retrospectives: `3.1-*/j-retrospective.md`, `3.2-*/j-retrospective.md`, `3.3-*/j-retrospective.md`
- Squash commit: 3.3 = `fd3546e`; parent close-out checkpoints `c76ec8a` (f), `5f4ff5e` (g)
