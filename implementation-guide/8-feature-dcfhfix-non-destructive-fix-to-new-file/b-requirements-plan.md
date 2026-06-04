# dcfhfix non-destructive fix-to-new-file - Requirements
**Task**: 8 (feature)

## Task Reference
- **Task ID**: internal-8
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/8-dcfhfix-non-destructive-fix-to-new-file
- **Template Version**: 2.1

## Goal
Define functional and non-functional specifications for making `dcfhfix` preserve the pre-repair index at a visible, predictable sibling path by default, with the legacy no-sibling behaviour available only behind a force-gated opt-in.

## Resolved Design Decisions (from planning review)
Two decisions were taken with the repo owner before requirements, resolving an
ambiguity in the source backlog entry:

- **D1 — Output model**: The repaired index takes the **canonical path** (atomic
  rename, as today). The pre-repair original is **moved aside to a visible
  sibling** (`<name>.idx.pre-fix` or timestamped). The canonical file *is*
  rewritten; the prior state is always preserved adjacent to it. (Not the
  "original untouched, write to new path" reading.)
- **D2 — Backup stack**: The existing FIFO `fixes` backup stack
  (`--backup=true`) stays populated on **every** write in **both** modes. No
  change to `fixes list/pop/discard/clear`. The visible sibling is *additional*
  to the stack copy (accepted redundancy in exchange for zero stack-code change).

**Net effect**: today's behaviour already does (stack copy → temp → atomic
rename to canonical). This task's new default *adds* a visible, predictably-named
preserved sibling plus clear messaging. The force-gated `--edit-in-place` opt-in
*suppresses the sibling*, reverting to exactly today's behaviour.

**Command surface (confirmed against `handleEntryCommand`, main.go:404-431)**: the
real write paths are **four** — `header edit`, `entry edit`, `entry append`,
`entry remove`. There is **no** `scan` write command and **no** `entry resort`
handler: both are advertised in help text (`main.go:100,126,173`) but fall
through to "unknown subcommand" — the help strings are themselves stale and are
an FR7 cleanup target. `fixes` manages the backup stack and is out of scope for
sibling preservation.

- **D3 — `--force` interaction**: `--force/-f` already exists with the meaning
  "force operations even if validation passes" (`main.go:32`, used at `:266`).
  This task does **not** redefine it. `--edit-in-place` is a separate new flag
  that *additionally* requires `--force` to be present (break-glass). `--force`
  alone retains only its validation-bypass meaning and must **never** suppress
  the preserved sibling.
- **D4 — Sibling collision & target safety**: the preserved sibling must never
  silently destroy an earlier preserved original, and its destination must be a
  fresh regular file. The deterministic name carries a disambiguating component
  (e.g. a run timestamp) so a re-repair never overwrites a prior copy; if the
  resolved destination already exists as a symlink, directory, or other
  non-regular file the tool refuses rather than following/truncating it (the
  reused `copyFile` uses `os.Create`, which follows symlinks).

## Functional Requirements
### Core Features
- **FR1 — Non-destructive default**: Every write path (`header edit`, `entry
  edit`, `entry append`, `entry remove` — the four real paths) preserves the
  pre-repair original at a visible sibling path before the canonical index is
  atomically replaced.
  - *Acceptance*: After a default-mode run on `X.idx`, the sibling preserved
    file exists and is byte-identical to the pre-run `X.idx`; `X.idx` now holds
    the repaired index.
- **FR2 — Collision-safe, regular-file sibling (see D4)**: The preserved-sibling
  name is derived from the input path plus a disambiguating run-timestamp
  component, and the tool must never silently overwrite an earlier preserved
  original. The destination must resolve to a fresh regular file; if it already
  exists as a symlink, directory, or other non-regular file the tool refuses and
  writes nothing.
  - *Acceptance*: Two consecutive default-mode runs both succeed and the first
    preserved copy is still present and byte-identical after the second run; a
    pre-existing symlink/directory at the resolved sibling path causes a refusal
    (non-zero exit, no write through the symlink).
- **FR3 — Force-gated in-place opt-in (see D3)**: A new `--edit-in-place` flag
  suppresses the visible sibling (legacy behaviour). It requires the existing
  `--force` flag to also be present; supplied alone it refuses and writes
  nothing. `--force` alone (without `--edit-in-place`) keeps only its existing
  validation-bypass meaning and still produces the preserved sibling.
  - *Acceptance*: `--edit-in-place` without `--force` → non-zero exit, zero
    filesystem writes, actionable error. `--force --edit-in-place` → canonical
    replaced, no preserved sibling created. `--force` alone → canonical replaced
    **and** preserved sibling still created.
- **FR4 — Mode messaging**: The default path reports the preserved-original
  location and that the original is preserved; the in-place path prints a
  prominent destructive-action warning naming what is not preserved. The routine
  preservation notice obeys the existing `--quiet` flag; the destructive in-place
  warning is a safety message and is **not** suppressed by `--quiet`.
  - *Acceptance*: The respective messages appear on stdout/stderr and are
    asserted in tests with and without `--quiet`; the destructive warning is
    still emitted under `--quiet`, the routine preservation notice is not.
- **FR5 — Backup stack unchanged**: The existing `fixes` FIFO stack is still
  populated on every write in both modes; `fixes list/pop/discard/clear` are
  unchanged.
  - *Acceptance*: All existing `fixes` tests pass unchanged; a default-mode run
    still produces a `fixes`-stack entry.
- **FR6 — Dry-run honoured**: `--dry-run` in the default mode reports the
  intended repaired-canonical write and sibling preservation without creating
  either.
  - *Acceptance*: A dry-run leaves the filesystem byte-for-byte unchanged
    (no canonical rewrite, no sibling, no new stack entry).
- **FR7 — Docs current**: `dcfhfix --help` and `cmd/dcfhfix/DESIGN.md` describe
  the new default, the preserved sibling, and the force-gated opt-in, with no
  stale "in-place by default" wording.
  - *Acceptance*: Help and DESIGN updated; a grep for stale in-place-default
    claims returns nothing.

### User Stories
- **As an** operator repairing a possibly-corrupt index, **I want** the prior
  state preserved at an obvious adjacent path automatically **so that** I can
  diff or roll back without having to know the `fixes` stack exists.
- **As a** power user, **I want** a clearly-gated in-place flag **so that** I can
  opt out of the preserved sibling when I deliberately don't want it.

## Non-Functional Requirements
### Performance (NFR1)
- At most one additional file copy (the preserved sibling) per write, on the
  same order as the existing backup-stack copy; no change to hashing or index
  load cost. Overhead is negligible relative to current behaviour.

### Usability (NFR2)
- The safe behaviour is the default with no flags. The rollback artefact is a
  visible, predictably-named file. The destructive opt-in is discoverable yet
  unmistakably gated (`--force --edit-in-place`) and warned.

### Maintainability (NFR3)
- Sibling preservation is centralised in a single helper, threaded into each of
  the existing rename-to-canonical sites rather than reimplemented per path
  (today those sites are duplicated: `entry_append_remove.go:142` `.append.tmp`,
  `:191` `.remove.tmp`, `entry_workflow_main.go:55` `.fix.tmp`, `main.go:1501`
  `.tmp`). Reuse the in-package `copyFile` / `createBackup` (`cmd/dcfhfix/main.go`)
  rather than new copy logic. (`MetaStore.copyFileWithMetadata` is **not** a reuse
  target: it is unexported and `MetaStore`-bound, unreachable from `package main`.)

### Security (NFR4)
- New write/copy code carries per-line gosec suppressions (G304/G306/G703) with
  rationale, per repo policy. The sibling path is derived from the already
  validated input index path — no new untrusted-input surface, no traversal.

### Reliability (NFR5)
- The atomic-rename guarantee for the canonical index is preserved. **Ordering**:
  the preserved sibling must be fully written (and the original still intact)
  *before* the canonical temp→rename executes, so a crash at any point leaves the
  pre-repair state recoverable from the sibling, the untouched original, and/or
  the `fixes` stack. There must be no window where both the canonical index and a
  preserved copy are simultaneously lost or corrupt.
  - *Acceptance*: a test asserts the sibling exists and is byte-identical to the
    original at the point immediately before the canonical rename (ordering
    invariant), and that aborting between the two steps leaves the original
    recoverable.

## Constraints
- Unix-like, Go 1.24.3; must preserve atomic-rename safety and must not regress
  the `fixes` stack behaviour.
- Scope limited to the real write command surface (`header`/`entry`); no `scan`
  write path exists.
- British spelling in all docs and user-facing text.
- gosec security gate applies to any new write code.

## Decomposition Check
- [ ] **Time**: >1 week? No (1-2 days).
- [ ] **People**: >2 people? No.
- [ ] **Complexity**: 3+ distinct concerns? No — one preservation behaviour
  applied uniformly across write paths via a shared helper.
- [ ] **Risk**: High-risk components needing isolation? No.
- [ ] **Independence**: Separable parts? No — all paths share one helper.

**Decision**: Do not decompose.

## Acceptance Criteria
- [ ] AC1 (FR1/FR2): Default-mode run preserves a byte-identical sibling and
  produces a repaired canonical index across all four write paths; a second run
  does not destroy the prior preserved copy; a symlink/directory at the resolved
  sibling path causes a refusal with no write through it.
- [ ] AC2 (FR3): `--edit-in-place` refuses without `--force` (no writes); with
  `--force` it rewrites the canonical index and creates no sibling; `--force`
  alone rewrites the canonical index **and** still creates the sibling.
- [ ] AC3 (FR4): Default and in-place runs emit their respective preservation /
  destructive-warning messages; the destructive warning survives `--quiet`, the
  routine preservation notice does not.
- [ ] AC4 (FR5/FR6): `fixes` stack still populated in both modes and unchanged;
  `--dry-run` leaves the filesystem byte-for-byte unchanged.
- [ ] AC5 (FR7): Help and DESIGN.md document the new default and gated opt-in;
  no stale in-place-default, `scan`, or `entry resort` write claims remain.
- [ ] AC6 (NFR3/NFR5): Preservation logic centralised in one helper threaded into
  the existing rename sites; sibling-before-rename ordering invariant holds and is
  asserted; atomic-rename safety retained.
- [ ] AC7 (NFR4): New write/copy code carries rationale-bearing gosec suppressions;
  sibling path derived from the validated input path with no traversal or
  symlink-follow.

## Status
**Status**: Finished
**Next Action**: /cwf-design-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
*To be filled upon completion*

## Lessons Learned
*To be captured during implementation*
