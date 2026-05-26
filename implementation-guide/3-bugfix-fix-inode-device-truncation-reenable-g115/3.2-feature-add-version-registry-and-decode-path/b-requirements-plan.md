# Add version registry and decode path - Requirements
**Task**: 3.2 (feature)

## Task Reference
- **Task ID**: internal-3.2
- **Task URL**: N/A (internal task)
- **Parent Task**: 3
- **Branch**: feature/3.2-add-version-registry-and-decode-path
- **Template Version**: 2.1

## Goal
Define what the version-dispatch seam must do: read any supported version, write the current
version, refuse the rest — all from a single owner in `pkg/format`, with no on-disk change.

## Functional Requirements
### Core Features
- **FR1 — Single-owned read dispatch**: `pkg/format` gains **one** owned function that maps an
  already-validated header version to a materialisation strategy. In 3.2 the observable outputs are
  `{current → zero-copy mmap cast; recognised legacy (v2) → same cast, since the entry layout is
  byte-identical across shipped versions; out-of-range → reject}` — a distinct legacy *entry*
  decoder is deferred to 3.3 (no divergent layout exists until v4). The **mmap index-file read
  paths** consult this function instead of open-coding the version decision: `pkg/index.go`'s two
  entry-walk loaders (`collectEntryRefs` ~:337 and the sibling `Index`-returning loader ~:614) and
  the header-only loader (~:150).
  - *Acceptance*: no `pkg/` mmap read path open-codes a `version`-conditioned materialisation
    branch — each calls the `format` resolver; v2 and v3 fixtures load byte-correctly through it.
    Write-side casts in `scan`/`pipeline`/`entry_serialiser` operate on freshly-built
    current-version buffers and are **out of scope** (not version-dispatched reads). dcfhfix's
    repair read path (`entry_workflow_main.go`) is a candidate second consumer; whether it routes
    through the resolver in 3.2 or 3.3 is a design decision.
- **FR2 — Single-owned write version**: the version stamped into a newly written index is sourced
  from `pkg/format`'s current-version owner, not chosen by the caller. `SetHeaderForWritableIndex`
  no longer accepts a divergent version from call sites.
  - *Acceptance*: no production caller passes a version literal/variable that can differ from
    `format.CurrentIndexVersion`; written indices carry the current version; round-trip unchanged.
- **FR3 — Version rejection is total and index-safe**: unknown, newer-than-current, and
  below-`MinIndexVersion` versions are refused with a clear error. The (untrusted) version byte is
  **never** used to raw-index a table/slice; dispatch uses map/switch-with-default after the
  `ValidateVersion` clamp.
  - *Acceptance*: negative tests for version `0`, `1`, `current+1`, and `0xFFFFFFFF` each return a
    descriptive error — no panic, no over-read, no out-of-range index.
- **FR4 — Behaviour & format invariance**: no on-disk format, field-width, or version-number
  change in 3.2. v2 and v3 indices read exactly as before; writes still emit current-version files.
  - *Acceptance*: full `go test ./pkg/... ./cmd/...` green incl. `-race`; a v2 and a v3 fixture
    re-serialise byte-identically (the 3.1 round-trip gate still holds).

### User Stories
- **As a** maintainer adding the v4 layout (Task 3.3) **I want** the version→decode decision in one
  tested place **so that** I register a new decoder instead of hunting open-coded casts on the load
  path.
- **As a** dcfh user opening an index written by a newer or corrupt tool **I want** a clear
  "unsupported version" error **so that** the tool never over-reads a wrongly-shaped entry.

## Non-Functional Requirements
### Performance (NFR1)
- Current-version load stays **zero-copy** (mmap cast); the dispatch decision is O(1) per load, not
  per entry — it gates the cast, it does not wrap each entry read.

### Usability (NFR2)
- Version-mismatch errors name the offending version and the supported range (mirrors today's
  `ValidateVersion` message), so a user can act without reading source.

### Maintainability (NFR3)
- The seam is unit-testable without a real on-disk index — the resolver maps version → strategy as
  a pure function, testable over a table of version inputs including the rejection boundaries.
  (The single-owner property itself is FR1/FR2; not restated here.)

### Security (NFR4)
- Untrusted version byte: dispatch is default-bearing (per FR3) — never a raw array/slice index.
  Note: `ValidateVersion(0)` (the `dcfhfind` read-only mode, header.go:65) accepts **any** version,
  so the resolver's `default → reject` arm — not `ValidateVersion` — is the real safety boundary for
  that path; the design must route it through the resolver.
- Centralising the decision must not bypass `pkg/format.SafeEntry`'s non-bypassable bounds checks
  that the G103 exclusion relies on.
- gosec **G115 site count unchanged vs the 3.1 baseline (52)** at the 3.2 boundary, measured by the
  3.1 method (temporarily un-exclude G115 in `.golangci.yml`, `golangci-lint run ./...`, revert).
  3.2 adds no narrowing conversions while G115 is excluded.

### Reliability (NFR5)
- Fail-closed (per FR3) extends to a **truncated** index: a header whose entry region is shorter
  than `EntryCount` implies must error, never over-read — a distinct failure mode from an
  out-of-range version *value*. v2 and v3 data integrity (SHA-1 footer, 8-byte alignment) preserved.

## Constraints
- Inherited from parent (c-design-plan.md): **one place** for versioned format code; host-order
  zero-copy preserved for the current version; single version gate (no parallel second gate);
  **no width/version/behaviour change in 3.2**; British spelling; no superlatives.
- Builds on 3.1 (baseline `e6c966b`): `pkg/format` already owns version constants,
  `HeaderSizeForVersion`, and `ValidateVersion` — 3.2 extends, never duplicates, these.

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [ ] **Time**: <1 week (1–2 days). No.
- [ ] **People**: 1. No.
- [ ] **Complexity**: single concern (own the version-dispatch decision). No.
- [ ] **Risk**: contained by invariance + negative-path gates. No.
- [ ] **Independence**: not further separable usefully.

**Outcome**: 0 signals — single subtask. The live question is the *inverse* (fold 3.2 up into
3.3); recorded in a-task-plan.md as the pre-exec review decision, not a decomposition trigger.

## Acceptance Criteria
- [ ] AC1 (FR1/FR2 — ownership): `pkg/` mmap read paths route through the single `format` resolver
      (no open-coded version-conditioned materialisation branch on a read path); the write version
      is sourced from `pkg/format` (no caller passes a divergent version). Grep is supporting
      evidence, not the criterion.
- [ ] AC2 (FR1/FR4 — positive boundaries): `v == CurrentIndexVersion` resolves to the zero-copy
      cast and `v == MinIndexVersion` (v2) loads correctly through the resolver; v2 + v3 fixtures
      re-serialise byte-identically; full suite green incl. `-race`; zero-copy current path retained.
- [ ] AC3 (FR3/NFR4/NFR5 — negative boundaries): version-rejection negatives (0, 1, current+1,
      0xFFFFFFFF) **and** a truncated/short index each error cleanly **under `-race`** (so "no
      over-read" has a detection mechanism); no raw-index of the version byte.
- [ ] AC4 (NFR4 — static floor): G115 site count == 52 via the 3.1 un-exclude/run/revert method.

## Status
**Status**: Finished
**Next Action**: /cwf-design-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All functional and non-functional requirements satisfied — see the g-testing-exec.md NFR gates table
and TC-1..6 results. gosec G115 == 52 (unchanged vs 3.1 baseline); no on-disk format/width/version
change. See j-retrospective.md.

## Lessons Learned
The dcfhfind validation path builds `MetaStore{version:0}`, so `ValidateVersion` is a no-op there and
the version *resolver* is the only real version gate — the key security requirement to keep explicit
for 3.3. See j-retrospective.md.
