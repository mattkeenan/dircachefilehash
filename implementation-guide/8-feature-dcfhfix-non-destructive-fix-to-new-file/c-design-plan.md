# dcfhfix non-destructive fix-to-new-file - Design
**Task**: 8 (feature)

## Task Reference
- **Task ID**: internal-8
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/8-dcfhfix-non-destructive-fix-to-new-file
- **Template Version**: 2.1

## Goal
Define the architecture for preserving the pre-repair index at a visible sibling
before the canonical atomic rename, with a single shared helper threaded into the
four existing rename sites, plus a force-gated `--edit-in-place` opt-out.

## Design Priorities
Testability → Readability → Consistency → Simplicity → Reversibility

## Key Decisions

### KD1 — One preservation helper at the rename boundary
- **Decision**: Replace the four bare `os.Rename(tmp, indexFile)` calls with a
  single shared helper `promoteRepairedIndex(tmpFile, indexFile, options)`. The
  helper owns: sibling preservation (default mode), the destructive-mode warning,
  and the atomic rename. The four write paths keep their existing temp-build and
  `finalizeTempIndex` logic untouched.
- **Rationale**: The rename is the exact point where the original is about to be
  replaced — the only correct place to preserve it. Centralising avoids four
  divergent copies (NFR3) and gives one test surface.
- **Trade-offs**: A new helper (small) vs. four near-identical inline blocks. The
  helper takes `options` so it can read `edit-in-place`/`quiet`; this couples it
  to `ParsedOptions`, consistent with the rest of the package.
- **Rename sites replaced**: `entry_workflow_main.go:55`,
  `entry_append_remove.go:142`, `entry_append_remove.go:191`, `main.go:1501`.

### KD2 — Preserve by O_EXCL copy, not rename, ordered before the canonical rename
- **Decision**: In default mode the helper copies the *current* `indexFile` to the
  sibling path **first** (fully written + `Sync`), then performs
  `os.Rename(tmp, indexFile)`. Preservation is a copy (original stays in place
  until the rename), not a move.
- **Rationale (NFR5 ordering invariant)**: copy-then-rename guarantees that at
  every instant at least one intact pre-repair representation exists. The
  *load-bearing* guarantee in the copy→rename window is the **untouched canonical
  original** (it is not modified until the rename); the sibling copy is the
  durable artefact *after* the rename. Note `file.Sync` flushes the sibling's data
  but not its directory entry, so the sibling is best-effort-durable until the
  rename completes — the recoverability claim rests on the original, not on
  sibling durability. This is simpler to reason about than rename-aside (which has
  a window with no canonical file).
- **Preserve-before-rename invariant (NFR5, critical)**: if preservation fails for
  any reason — `O_EXCL` collision exhausting the KD4 bound, copy error, non-regular
  target — the helper returns the error **before** `os.Rename` and the canonical
  index is left intact. There is no code path that renames after a failed
  preservation. In-place mode is the only path that skips preservation, and it
  does so by explicit branch, not by ignoring an error.
- **Trade-offs**: One extra full-file read+write per repair (NFR1: same order as
  the existing `fixes`-stack copy; negligible for index files).

### KD3 — Symlink-safe sibling creation (D4)
- **Decision**: The helper does **not** reuse `copyFile` (`main.go:997`, which
  opens the destination with `os.Create` — follows symlinks and truncates).
  Instead it opens the sibling with `os.OpenFile(sibling,
  O_WRONLY|O_CREATE|O_EXCL, 0644)` and streams `io.Copy` from the original.
- **Rationale**: `O_EXCL` refuses any pre-existing destination, including a
  symlink, closing the "write through a planted symlink" hazard the security
  review flagged. It also gives collision-safety for free (FR2): an existing
  sibling causes `EEXIST` rather than a silent overwrite.
- **TOCTOU contract**: collision handling (KD4) re-attempts the `O_EXCL` open for
  each candidate name; it must **never** `os.Stat`-then-open (that reintroduces
  the check-to-use race `O_EXCL` exists to avoid). The source (`indexFile`) and
  every candidate sibling path are guaranteed distinct (the sibling stem always
  carries a suffix), so the source is never opened with `O_EXCL`.
- **Trade-offs**: Cannot reuse `copyFile` verbatim; the helper mirrors its short
  `io.Copy` body with a stricter open. Documented deviation from the NFR3 "reuse
  copyFile" note — the security requirement (D4) wins.

### KD4 — Sibling naming: deterministic stem + run-timestamp + EEXIST counter (FR2)
- **Decision**: Sibling path = `<indexFile>.pre-fix-<UTC>` where `<UTC>` is a
  compact sortable stamp (`20060102T150405Z`). If `O_EXCL` returns `EEXIST`
  (same-second re-run), append `-<n>` (n = 1,2,…) up to a small bound (e.g. 100),
  else fail with a clear error. Never overwrite an existing preserved copy.
  Hitting the bound is a **hard refusal**: the helper returns the error before any
  rename (per KD2's preserve-before-rename invariant); the canonical index is left
  untouched and no partial sibling remains.
- **Rationale**: Timestamped stem makes repeated repairs non-colliding and
  human-orderable; the counter handles sub-second re-runs without silent loss;
  the bound prevents an unbounded retry loop.
- **Trade-offs**: Name is not a single fixed string (so AC1 asserts the
  invariant "prior copy survives", not an exact filename). Preserved siblings
  accumulate across many repairs — acceptable for a manual repair tool; cleanup
  is out of scope.

### KD5 — `--edit-in-place` flag, gated on `--force` (D3)
- **Decision**: Add `options.DefineOption("edit-in-place", "", OptionTypeBool,
  "false", "...")`. A single guard, `validateEditInPlaceGate(options)`, runs once
  in the dispatch chokepoint `dispatchCommand` (`main.go:86`, body `:100-113`) —
  every write subcommand routes through it — and enforces `edit-in-place &&
  !force → error, exit non-zero, no writes`. `--force` alone (without
  `--edit-in-place`) still preserves the sibling.
- **Correction (review)**: `--force/-f` is currently **defined but unconsumed** —
  `main.go:32` defines it, but there is **no `GetBool("force")` in production
  code** (the `:266` reference is help text, not a validation site). So this task
  is the *first* real reader of `--force`; the earlier "keeps its existing
  validation-bypass meaning" framing (D3) is inaccurate — today `--force` alone is
  a no-op that, post-change, still yields the preserved sibling.
- **Rationale**: The backlog explicitly specifies `--force --edit-in-place` as a
  deliberate break-glass gesture; keeping the two-flag friction honours that
  intent, and `--edit-in-place` keeps the destructive intent explicit/greppable.
- **Trade-offs**: Two flags for the destructive path (intentional friction);
  `--force` gains its first behavioural dependency here.

## System Design

### Component Overview
- **`promoteRepairedIndex(tmpFile, indexFile string, options *ParsedOptions)
  error`** — new, in a new small file `cmd/dcfhfix/promote.go`. The preservation
  + rename boundary. The helper **owns and prints** the preservation notice and
  the destructive warning itself, so it returns only `error` (no sibling-path
  return — that would force redundant signature churn across the four inner
  functions for a value the helper already emits).
- **Header-path wiring note**: three of the four rename sites already hold a live
  `options`. The header path `writeIndexWithModifiedHeader(..., _ *ParsedOptions)`
  (`main.go:1485`) currently **discards** it via `_`; this site must un-blank and
  thread the parameter through before the helper can read `edit-in-place`/`quiet`.
  It is the one non-symmetric rename site.
- **`siblingPreFixPath(indexFile string) string`** — pure helper computing the
  timestamped stem (testable without filesystem).
- **`preserveOriginal(indexFile string) (string, error)`** — O_EXCL copy with the
  EEXIST counter; returns the final sibling path. Owns KD3/KD4.
- **Option definition + guard** — `options.go` (define `edit-in-place`);
  `validateEditInPlaceGate(options)` invoked once in the command entry path
  (alongside the existing validation near `main.go:266`).
- **Messaging** — inside `promoteRepairedIndex`: default success notice (honours
  `--quiet`); destructive warning to **stderr**, emitted regardless of `--quiet`.

### Data Flow (per write command, default mode)
1. Caller builds temp index + `finalizeTempIndex` (unchanged).
2. Caller calls `promoteRepairedIndex(tmp, indexFile, options)` in place of the
   old `os.Rename`.
3. Helper, default mode: `preserveOriginal(indexFile)` → copy current canonical
   to `…​.pre-fix-<UTC>` via O_EXCL, `Sync`.
4. Helper: `os.Rename(tmp, indexFile)` (existing atomic semantics).
5. Helper: print "Original preserved at <sibling>" unless `--quiet`; return path.
6. Caller's existing success path reports entries fixed/discarded as today.

In-place mode (steps 3/5 differ): emit destructive warning to stderr (always),
skip preservation, then rename.

Dry-run path (FR6): unchanged — callers already short-circuit before the rename
(entry edit `main.go:752/758`, append `:805/823`, remove `:866/877`, and the
header-edit dry-run branch ~`:637-676`; verify the exact header line during
implementation). Add one line to each existing dry-run branch describing the
sibling *pattern* that would be written (e.g. "would preserve original as a
`.pre-fix-<timestamp>` sibling") rather than a concrete timestamp that won't match
the eventual real run; no filesystem effect.

`fixes` stack (FR5/D2): untouched. `createBackup` continues to run on its existing
`--backup` gate before the temp build in each path; the new sibling is additional.

## Interface Design

### New flag
```
--edit-in-place   (bool, default false)   Overwrite the index in place without
                                           preserving a .pre-fix sibling.
                                           Requires --force.
```

### Helper contracts
```go
// promoteRepairedIndex preserves the pre-repair original (default) then atomically
// replaces indexFile with tmpFile. In --edit-in-place mode it warns and skips
// preservation. On any preservation failure it returns before renaming, leaving
// indexFile intact. It prints the preservation notice / destructive warning itself.
func promoteRepairedIndex(tmpFile, indexFile string, options *ParsedOptions) error

// preserveOriginal copies indexFile to a timestamped .pre-fix sibling, opening the
// destination with O_WRONLY|O_CREATE|O_EXCL (refuses symlink/existing targets). On
// EEXIST it retries with a numeric suffix up to a bound, re-attempting the O_EXCL
// open per candidate (never Stat-then-open). Returns the sibling path.
func preserveOriginal(indexFile string) (string, error)

// siblingPreFixPath returns the deterministic timestamped sibling stem for indexFile.
func siblingPreFixPath(indexFile string) string

// validateEditInPlaceGate returns an error if --edit-in-place is set without --force.
func validateEditInPlaceGate(options *ParsedOptions) error
```

## Constraints
- Preserve the existing atomic-rename guarantee; preservation strictly precedes
  the rename (KD2).
- No new untrusted input: sibling path derived from the validated CLI index path.
- New write/copy code carries per-line gosec suppressions with rationale, matching
  the existing annotations in these files. Anticipated rules: **G304** (open from
  a variable path), **G703** (taint-tracked write destination — the sibling path
  flows from the index argument, the rule most likely to fire here), and **G302**
  for the `0644` mode on the `O_EXCL` open. The `0644` choice follows the existing
  precedent in these files (`entry_workflow_main.go:65`), and the rationale string
  should cite "`.dcfh/` index file, non-secret (metadata + hashes)" as the others
  do. Add `G306` only if any `os.WriteFile` site remains. Reconciles the
  requirements NFR4 list (G304/G306/G703) with the actual write primitive.
- British spelling in user-facing text.

## Decomposition Check
- [ ] Time >1 week? No.
- [ ] People >2? No.
- [ ] Complexity 3+ concerns? No — one helper + one flag + doc updates.
- [ ] Risk needing isolation? No.
- [ ] Independent parts? No — single shared helper.

**Decision**: Do not decompose.

## Validation
- [ ] Helper is unit-testable without invoking full commands (`siblingPreFixPath`
  pure; `preserveOriginal` filesystem-only; gate pure).
- [ ] Ordering invariant (sibling before rename) asserted.
- [ ] Integration points = the four rename sites, all replaced.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
*To be filled upon completion*

## Lessons Learned
*To be captured during implementation*
