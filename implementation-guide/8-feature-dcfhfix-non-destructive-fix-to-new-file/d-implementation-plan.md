# dcfhfix non-destructive fix-to-new-file - Implementation Plan
**Task**: 8 (feature)

## Task Reference
- **Task ID**: internal-8
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/8-dcfhfix-non-destructive-fix-to-new-file
- **Template Version**: 2.1

## Goal
Implement the design: one preservation helper threaded into the four rename
sites, the `--edit-in-place` flag + force gate, dry-run reporting, and doc/help
updates — all in `cmd/dcfhfix/`.

## Files to Modify / Create
| File | Change |
|------|--------|
| `cmd/dcfhfix/promote.go` | **NEW** — `promoteRepairedIndex`, `preserveOriginal`, `siblingPreFixPath`, `validateEditInPlaceGate`. |
| `cmd/dcfhfix/main.go` | Define `--edit-in-place` (by the other `DefineOption` calls, `main.go:27-34`); call the gate in `dispatchCommand` (body `:104-113`; `:86` is only the caller in `runMain`); un-blank `writeIndexWithModifiedHeader`'s `_ *ParsedOptions` (`:1485`, caller already passes `options` at `:660`) and replace its `os.Rename` (`:1501`) with `promoteRepairedIndex`; add the dry-run "would preserve" line in the header-edit dry-run **print** branch (`:643-646`); update entry/header help text and drop the stale `entry resort` write advertising (`:126,:172-173`). The stale `scan`-as-repairable-*input* lines (`:146-147`, dead since v0.7) are **not** a write claim and are out of AC5 scope — leave unless doing a separate doc sweep. |
| `cmd/dcfhfix/entry_workflow_main.go` | Replace `os.Rename` (`:55`) with `promoteRepairedIndex` (`options` already in scope). |
| `cmd/dcfhfix/entry_append_remove.go` | Replace `os.Rename` (`:142`, `:191`) with `promoteRepairedIndex`; add dry-run "would preserve" lines in the append/remove caller dry-run **print** branches (`main.go:830-833`, `:877-880`). |
| `cmd/dcfhfix/DESIGN.md` | Document the non-destructive default, the `.pre-fix-<ts>` sibling, and the force-gated `--edit-in-place` (FR7). |
| `cmd/dcfhfix/promote_test.go` | **NEW** — unit tests for the four helpers. |
| `cmd/dcfhfix/main_test.go` / `writepath_test.go` | Integration assertions across the write paths (default/in-place/dry-run/quiet/fixes-stack). |

## Implementation Steps (ordered)
1. **`promote.go` — pure naming**: `siblingPreFixPath(indexFile) string` →
   `indexFile + ".pre-fix-" + <UTC compact>` (`time.Now().UTC().Format("20060102T150405Z")`).
   No filesystem access (unit-testable in isolation).
2. **`promote.go` — `preserveOriginal(indexFile) (string, error)`**: open the
   source with `os.Open` (same symlink-following semantics as the existing
   `copyFile`; reading the index the user named is the tool's job). Loop candidate
   names (`siblingPreFixPath`, then `-1…-100`); for each, `os.OpenFile(cand,
   O_WRONLY|O_CREATE|O_EXCL, 0644)` — on `EEXIST` try next, on other error return.
   On success `io.Copy`, then **explicitly capture and return** the `dst.Sync()`
   and `dst.Close()` errors (do **not** silent-`defer` Close — a flush/close
   failure must propagate so the caller skips the rename, NFR5). On any copy/sync
   error, `os.Remove` the partial sibling before returning. Exhausting the bound →
   error (no rename follows). Per-line `//nolint:gosec` G304/G703 on the open and
   G302 on the `0644` mode; mirror the in-file G304 rationale ("repair-tool path
   from a user-supplied CLI argument; no trust boundary", as `main.go:998`); the
   `0644`/G302 precedent is `saveMetadata` (`main.go:1018`), **not**
   `entry_workflow_main.go:65` (that is `os.Create`/G304). Do **not** add G306 (no
   `os.WriteFile` here). Never `os.Stat`-then-open (TOCTOU contract).
3. **`promote.go` — `validateEditInPlaceGate(options) error`**: if
   `GetBool("edit-in-place") && !GetBool("force")` → error
   ("`--edit-in-place` requires `--force`"). Pure, no I/O.
4. **`promote.go` — `promoteRepairedIndex(tmpFile, indexFile, options) error`**:
   if `GetBool("edit-in-place")`: print destructive warning to **stderr**
   (always, ignores `--quiet`); else `preserveOriginal` (return err before any
   rename) and, unless `--quiet`, print "Original preserved at <sibling>". Then
   `os.Rename(tmpFile, indexFile)`. Single owner of both messages.
5. **Flag + gate wiring (`main.go`)**: add the `DefineOption` line; call
   `validateEditInPlaceGate(options)` once in `dispatchCommand` (`:104-113`) before
   routing, returning its error (non-zero exit, no writes). **Accepted behaviour**:
   because `dispatchCommand` routes *all* subcommands, lone `--edit-in-place` is
   refused even on read-only commands (`header show`, `entry show`, `fixes list`).
   This is intentional — the flag is meaningless on reads, one chokepoint is
   simpler, and the refusal is harmless. Co-locate the temp file, sibling, and
   canonical index in the same directory (they are today) so the atomic-rename and
   preserve-before-rename invariants hold; do not relocate temps to `/tmp`.
6. **Thread the four rename sites**: replace each `os.Rename(tmp, indexFile)` with
   `promoteRepairedIndex(tmp, indexFile, options)`, **wrapping its error** in each
   site's existing message ("failed to replace original index file: %w") so the
   distinct per-path wording and any error-text test assertions survive. Un-blank
   `writeIndexWithModifiedHeader`'s options param and use it. (NFR3's "reuse
   `copyFile`" is deliberately superseded by KD3/D4 — the non-reuse is sanctioned,
   not a miss.)
7. **Dry-run reporting (FR6)**: add the "would preserve" line in each dry-run
   **print/early-return** branch (header `:643-646`, entry edit `:758-761`, append
   `:830-833`, remove `:877-880`) — **not** the `createBackup` guard branches.
   Name the sibling *pattern* (not a concrete timestamp), e.g. "Would preserve
   original as a `.pre-fix-<timestamp>` sibling". The `headerEditJSON` (`:683`) and
   `entryEditJSON` (`:812`) stubs return "not yet implemented" and never reach a
   rename — out of scope, add no reporting there. No filesystem effect.
8. **Docs/help (FR7)**: update `DESIGN.md` Safety Features + Command sections;
   refresh help text; remove the stale `entry resort` write advertising
   (`:126,:172-173`). (No stale `scan` *write* claim exists — see the Files table
   note; the `scan`-input lines at `:146-147` are out of AC5 scope.)
9. **Tests**: write `promote_test.go` and the integration assertions (see below).
10. **Validate**: `go build ./...`, `go test ./cmd/dcfhfix/...`,
    `golangci-lint run ./...` (gosec gate), and a grep proving no stale
    in-place-default / `scan` / `entry resort` write claims remain.

## Test Coverage
**`promote_test.go` (unit)**
- `siblingPreFixPath`: shape/suffix correctness.
- `preserveOriginal`: (a) creates a byte-identical sibling, original still intact
  afterwards (proves copy-before-rename ordering at unit level — AC6); (b) refuses
  when the resolved sibling pre-exists as a **symlink** and as a **directory**, and
  on the symlink-refusal path the symlink's **target file is left untouched** (no
  write traversed the link — AC1/D4); (c) `EEXIST` on the base name advances to
  `-1`; (d) exhausting the bound returns an error and leaves no partial sibling.
- `validateEditInPlaceGate`: edit-in-place w/o force → error; with force → nil;
  neither set → nil (AC2).

**Integration (`main_test.go` / `writepath_test.go`)** — for each real write path
(`header edit`, `entry edit`, `entry append`, `entry remove`):
- Default run → sibling exists, byte-identical to pre-run index; canonical holds
  the repaired index (AC1).
- `--force --edit-in-place` → canonical replaced, **no** sibling created (AC2).
- `--force` alone → canonical replaced **and** sibling present (AC2).
- `--dry-run` → filesystem byte-for-byte unchanged; "would preserve" message
  present (AC4/FR6).
- `--dry-run --force --edit-in-place` → previews the destructive path (warning
  shown, no "would preserve" line) and writes nothing; lone `--dry-run
  --edit-in-place` (no `--force`) is refused by the gate before any preview.
- Messages: preservation notice suppressed under `--quiet`; destructive warning
  still emitted under `--quiet` (AC3).
- `fixes` stack still gains an entry in default mode; existing `fixes` tests
  unchanged (AC4/FR5).

## Validation Criteria
- [ ] `go build ./...` clean.
- [ ] `go test ./cmd/dcfhfix/...` green (new + existing).
- [ ] `golangci-lint run ./...` passes; new write/copy code carries
  rationale-bearing gosec suppressions (AC7).
- [ ] All four write paths produce the sibling by default; `--force
  --edit-in-place` suppresses it; lone `--edit-in-place` refuses (AC1/AC2).
- [ ] `grep` shows no stale in-place-default or `entry resort` write claims in
  help or DESIGN (AC5).

## Decomposition Check
- [ ] Time >1 week? No. [ ] People >2? No. [ ] 3+ concerns? No (one helper + flag
  + docs). [ ] Risk isolation? No. [ ] Independent parts? No.

**Decision**: Do not decompose.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
*To be filled upon completion*

## Lessons Learned
*To be captured during implementation*
