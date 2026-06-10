# Showcase key features in README - Testing Plan
**Task**: 18 (chore)

## Task Reference
- **Task ID**: internal-18
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/18-showcase-key-features-in-readme
- **Template Version**: 2.1

## Goal
Verify the README feature-showcase edits are accurate against shipped code and
introduce no regression. Like task 17, this ships no runtime code — testing is
**static verification** (greps that match each claim to a source string, a link
sweep, and a build/test regression guard), not unit/integration tests.

## Test Strategy
### Test Levels
No runtime code changes, so the levels are:
- **Claim-accuracy checks**: for each sold feature, grep the README claim AND the
  source string it must match; both must be present (the anti-drift guard).
- **Negative checks**: grep that nothing *invented* slipped in (no flag absent from
  `cmd/dcfh/`, no removed library API, no `--exclusive` in the sell copy, no
  "no-op" wording for the unsupported-FS case).
- **Link-integrity sweep**: every relative `](…)` in `README.md` resolves.
- **Build/regression**: `go build ./...` + `go test ./...` (proves docs-only).

### Test Coverage Targets
- **Every SC (SC1–SC5) has at least one check** below.
- **Critical paths** = claim-accuracy for dedupe (SC2) + interactive-tree (SC3) and
  the invented-flag negative grep; all must be clean.
- **Regression**: full `go build`/`go test` green; only `README.md` changed.

## Test Cases
### Functional Test Cases

- **TC-1 — Features section present & teaser-only (SC1)**
  - **Given**: the edited README.
  - **When**: `grep -nE '^## Features' README.md`; read the section.
  - **Then**: a `## Features` section exists before `## The tools`; 4 one-line
    bullets; each maps to a shipped capability (dedupe, interactive-tree, speed,
    snapshots/diff/subrepo); the FIDEDUPERANGE/filesystem list and the glyph/key
    list do **not** appear here (they belong to Edit B/C — no duplicated detail).

- **TC-2 — Dedupe sold accurately (SC2) [critical]**
  - **Given**: the `### Duplicate detection and dedupe` subsection.
  - **When**: grep the README for `FIDEDUPERANGE`, `fs-dedupe`, the reflink FS names,
    and compare to `cmd/dcfh/dupes.go:70–76`; grep for the skip-and-report wording.
  - **Then**: README says block-level dedupe via `FIDEDUPERANGE`, **Linux-only**,
    COW extent sharing on btrfs / XFS reflink=1 / bcachefs, *frees space without
    removing files*; states the unsupported-FS case as **skips and reports the
    device** (matches `dupes.go:281`), NOT "no-op"; conveys size/date/hardlink-aware
    selection by category with a pointer to `dcfh dupes help`.

- **TC-3 — Interactive-tree sold as change tracking (SC3) [critical]**
  - **Given**: the expanded `### Interactive tree viewer` subsection.
  - **When**: read it; compare key/glyph claims to
    `cmd/dcfh/internal/tui/render.go:154` (footer) and `nodeStyle`.
  - **Then**: it states the viewer is *change-tracking* (not just disk usage);
    names the status glyphs `+`/`~`/`-`/`*`; `z` hide-unchanged; `c/f/a/m/d/n` sort
    + `r` reverse; keeps TTY-required and status/update-only. Every key/glyph matches
    the footer string / `nodeStyle` exactly.

- **TC-4 — No invented features/flags, no removed API (SC2/SC3/SC4) [critical]**
  - **Given**: all edits.
  - **When**: extract every `--flag` token the README newly names for `dupes`/tree;
    confirm each exists in `cmd/dcfh/` (grep). Grep README for
    `DirectoryCache|FileEntry|NewDirectoryCache` and for `--exclusive` in the sell
    copy and for "no-op"/"no op" near fs-dedupe.
  - **Then**: every named flag exists in source; zero removed-API hits; `--exclusive`
    absent from the sell copy; no "no-op" wording for the unsupported-FS case.

- **TC-5 — Link integrity (SC4)**
  - **Given**: edited README.
  - **When**: extract every relative `](…)` link (strip anchors), `test -e` each
    against the repo root.
  - **Then**: all resolve (in particular `docs/README.md`, `docs/ARCHITECTURE.md`,
    `cmd/dcfhfind/DESIGN.md`, `cmd/dcfhfix/DESIGN.md`, `LICENSE`).

- **TC-6 — `remote` still omitted (SC4)**
  - **Given**: edited README.
  - **When**: grep README for a `remote` user-command listing.
  - **Then**: `remote` is not presented as an end-user command (still `Hidden`).

- **TC-7 — Docs-only / no regression (SC5) [critical]**
  - **Given**: all edits.
  - **When**: `go build ./...`; `go test ./...`; `git diff --name-only main...HEAD`.
  - **Then**: build + tests green; the only non-workflow file changed is
    `README.md`; no `.go` source behaviour change.

### Non-Functional Test Cases
- **Usability (NFR-equivalent)**: TC-1/TC-3 — the sell is legible and the platform
  caveats (Linux-only, TTY) are stated next to the feature, not buried.
- **Security**: docs-only — no secrets/credentials/executable content added; the
  most safety-sensitive claim (dedupe is non-destructive) is verified accurate in
  TC-2. The exec-phase `cwf-security-reviewer-changeset` run is the gate (expected
  to be tiny — one prose file).
- **Reliability**: TC-7 (green build/test; single-file, trivially revertible).
- **Performance**: N/A — no runtime change; not measured.

## Test Environment
### Setup Requirements
- Local repo on the task branch; `git`, `grep`, `go` toolchain; `markdown-reader`
  for section reads. No database, no services, no network.
- The link sweep is the same shell one-liner used in task 17 (extract `](…)`,
  `test -e` each) — no new tooling.

### Automation
- Run ad hoc in g-testing-exec; `go build`/`go test` mirror the pre-commit gate.
  No CI change required (docs-only).

## Validation Criteria
- [ ] TC-1…TC-7 all pass (SC1–SC5 covered).
- [ ] Critical paths (TC-2, TC-3, TC-4, TC-7) 100% clean.
- [ ] `go build ./...` and `go test ./...` green; only `README.md` changed.
- [ ] No invented flag/feature; dedupe non-destructive claim verified.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
TC-1…TC-7 all executed and PASS (see `g-testing-exec.md`). Static verification
only — claim-to-source greps, link sweep (7/7), invented-flag/removed-API
negatives clean, `go build`/`go test` green, diff = `README.md`.

## Lessons Learned
The per-claim grep table (README claim AND its source string must both match) is
the right shape for a docs-accuracy chore and doubles as a future regression
guard. See `j-retrospective.md`.
