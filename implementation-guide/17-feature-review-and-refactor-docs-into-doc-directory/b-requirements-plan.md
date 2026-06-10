# Review and refactor docs into doc directory - Requirements
**Task**: 17 (feature)

## Task Reference
- **Task ID**: internal-17
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/17-review-and-refactor-docs-into-doc-directory
- **Template Version**: 2.1

## Goal
Define what a "docs that match the code, in one home" deliverable must achieve:
relocate the non-root docs into the existing `docs/`, fix every reference, and
bring the retained docs (README + architecture/design set) into an honest state
relative to the current v0.7 CLI codebase.

## Functional Requirements
### Core Features

- **FR1 — Relocation**: The five root architecture/design docs
  (`ARCHITECTURE.md`, `ARCHITECTURE-IMPROVEMENTS.md`, `architecture-v0.7.md`,
  `design.md`, `streaming-iterator-architecture.md`) are moved into the existing
  `docs/` directory using `git mv` (history preserved). The repository root
  retains only `README.md`, `CHANGELOG.md`, `BACKLOG.md`, and `CLAUDE.md` as
  Markdown.
  - *AC1*: `ls *.md` at root lists exactly those four files; the five docs above
    resolve under `docs/`; `git log --follow` shows continuous history for each
    moved file.

- **FR2 — Reference integrity**: Every internal reference to a moved doc is
  corrected for its new `docs/` location. References today are almost all
  **bare/inline-code basenames** (e.g. `` `architecture-v0.7.md` ``) and prose
  locators, **not** markdown `](...)` links — so a naive link sweep is not
  enough. This covers: `README.md`; `CLAUDE.md`; `pkg/doc.go` (the comment at
  line 32 reads "See ARCHITECTURE.md **at the repo root**" — the locator phrase
  must be corrected, not just the name); and the surviving docs themselves (e.g.
  the `ARCHITECTURE.md` deep-dive table at lines 256–263 listing its siblings).
  - *AC2*: (i) a grep for path-style references to the old root locations
    (e.g. `](ARCHITECTURE.md`, `(./design.md`) returns no in-scope hits;
    (ii) every **bare/inline-code mention** of a moved basename in an in-scope
    doc or in `pkg/doc.go` either resolves under `docs/` or is corrected — no
    "repo root" locator survives for a moved doc; (iii) any markdown links
    authored under FR6 resolve. Out-of-scope trees (`.cwf/**`, `.cwf-skills/**`,
    `.cwf-rules/**`, `implementation-guide/**`, `.claude/sessions/**`) are
    excluded — their `design.md` filename collisions are CWF-internal, not
    references to the moved doc.

- **FR3 — Inventory & classification (process input)**: As the basis for FR4/FR5,
  classify every in-scope doc as *current* (verified against code), *historical*
  (describes a superseded state), or *needs-rewrite* (materially wrong about the
  shipped tool). This is an internal work-product recorded in `c-design-plan.md`
  — it ships nothing itself; its outcomes are gated by AC4 (README clean) and AC5
  (banners present, claims match code). Listed as an FR for traceability, not as
  a separately shippable deliverable.
  - *AC3*: each of the six in-scope docs (the five moved + README) has an
    explicit classification in `c-design-plan.md` with a one-line code-grounded
    justification.

- **FR4 — README resync**: README is rewritten to describe the actual product:
  the `dcfh` / `dcfhfind` / `dcfhfix` CLI tools and the `--interactive-tree`
  viewer. It contains no reference to the removed `DirectoryCache` / `FileEntry`
  library API. Any code/usage block is valid against current code or is a CLI
  invocation. README links into `docs/` for depth rather than duplicating it.
  - *AC4*: README contains zero occurrences of `DirectoryCache`, `FileEntry`, or
    `NewDirectoryCache` (this clause **takes precedence** — any `go` block using
    the removed API is deleted or replaced with a CLI block, not "made to
    compile"); documents `dcfh`, `dcfhfind`, `dcfhfix`, and `--interactive-tree`;
    every surviving fenced example is a shell/CLI block or compiles against the
    current package; README has a "Documentation" pointer into `docs/`.

- **FR5 — Architecture/design honesty**: Every architecture/design doc retained
  under `docs/` is either (a) verified current against code, or (b) carries an
  explicit banner near its top marking it historical/superseded (with the
  superseding reference where known). No retained doc silently presents removed
  architecture (e.g. mmap-backed scan indices, `pkg/workflow.go`, the removed
  recovery orchestration) as current.
  - *AC5*: no in-scope `docs/` file asserts a removed mechanism as current
    without a historical banner; each *historical*-classified doc has such a
    banner; spot-checked claims (scan path, recovery surface, entry storage
    modes) match `CLAUDE.md` / the code.

- **FR6 — Discoverability (authoring the link graph)**: There is no clickable
  link graph today, so FR6 **creates one**: (a) a README "Documentation" section
  linking the key `docs/` files — this is the *same* artefact as the FR4 pointer,
  not a separate one; and (b) the bare-name sibling references inside the docs
  (the `ARCHITECTURE.md` deep-dive table) converted to working `docs/`-relative
  markdown links. A `docs/README.md` index listing each doc with a one-line
  purpose and current/historical marker is the recommended way to satisfy (a).
  - *AC6*: the links authored in (a) and (b) resolve, and each retained
    architecture/design doc is reachable from `README.md` in ≤2 clicks with its
    current-vs-historical status visible at the link (the ≤2-clicks target
    restates NFR2).

- **FR7 — Preserve existing `docs/`**: The move is additive — existing `docs/`
  contents (`changelog-old.md`, `feasibility/`, `ssh-shell-mode.md`) and the
  existing `CHANGELOG.md → docs/changelog-old.md` link are unchanged and
  unbroken.
  - *AC7*: those paths still exist and the CHANGELOG link still resolves after
    the change.

### User Stories
- **As a** prospective user landing on the GitHub repo **I want** a README that
  describes the actual `dcfh` CLI and how to run it **so that** I can install and
  use the tool without hitting a Quick Start that references a non-existent API.
- **As a** new contributor **I want** the architecture docs in one place and
  clearly marked current-or-historical **so that** I can build an accurate mental
  model without being misled by superseded designs.

## Non-Functional Requirements
### Performance (NFR1)
- Docs-only change: no effect on build or test wall-time, binary size, or runtime
  behaviour. Not separately measured beyond NFR5's green build/test gate.

### Usability (NFR2)
- A reader reaches the current architecture overview from the README in ≤2 clicks
  (FR6). Historical docs are visibly labelled so a reader never mistakes a
  superseded design for the shipped one (FR5).

### Maintainability (NFR3)
- One docs home (`docs/`) — no parallel `doc/`/`docs/` split, no duplicated doc
  trees. Consistent file naming within `docs/`. British spelling in prose, per
  project convention. The classification (FR3) gives a future maintainer a clear
  record of why each doc was kept/marked.

### Security (NFR4)
- Docs-only: introduces no secrets, credentials, or executable content; no change
  to any code path or the `gosec`/CWF security posture. If README/`CLAUDE.md`
  security text is touched, it must remain accurate (no weakening of the
  documented security review process).

### Reliability (NFR5)
- The change is reversible and history-preserving (`git mv`). `go build ./...`
  and `go test ./...` remain green. The only source edits permitted are
  comment/doc-string fixes (e.g. `pkg/doc.go`'s locator) — no logic, index
  format, CLI surface, or `--json` output change.

## Constraints
- **No behaviour change** — source edits are limited to comments/doc strings
  (e.g. the `pkg/doc.go` locator fix in FR2); no logic, API, index format, or
  output change (NFR5). "Docs-only" is meant in this sense, not "zero `.go` files
  touched".
- **Target** the existing `docs/` (plural), confirmed with the user; consolidate,
  do not create a parallel `doc/`.
- **`CLAUDE.md` stays at root** (required by Claude Code; also the authoritative
  current-architecture reference for the resync).
- **Out of scope (do not touch)**: `.cwf/**`, `.cwf-skills/**`, `.cwf-rules/**`,
  `implementation-guide/**`, `.claude/sessions/**`. The FR2 reference-correction
  pass must honour this exclusion in the **rewrite**, not just the verification
  grep — the `.cwf/**` hash-tracked files must never be touched (would trip
  `cwf-manage validate`).
- **Use `git mv`**; British spelling in prose.

## Decomposition Check
- [ ] **Time**: >1 week? No (1–2 days).
- [ ] **People**: >2 people? No.
- [x] **Complexity**: 3+ distinct concerns? Yes — relocate+re-link / README
  rewrite / architecture-doc reconciliation.
- [ ] **Risk**: isolation needed? No (docs-only, reversible).
- [x] **Independence**: parts separable? Yes.

**Result**: 2 signals — subtasks viable (17.1/17.2/17.3) but recommend a single
task with three milestones (shared inventory step, single owner, <1 week). Same
conclusion as a-task-plan; revisit at the review gate.

## Acceptance Criteria
- [ ] AC1–AC7 above pass (one per FR).
- [ ] `go build ./...` and `go test ./...` green; the only `.go` edit is the
  comment/locator fix in `pkg/doc.go` (no logic/behaviour change).
- [ ] No file under `.cwf/**`, `.cwf-skills/**`, `.cwf-rules/**`,
  `implementation-guide/**`, `.claude/sessions/**` modified by this task.

## Status
**Status**: Finished
**Next Action**: /cwf-design-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Every FR (FR1–FR7) and applicable NFR was satisfied and mapped to a passing
testing-exec check (TC-1…TC-9, 9/9 PASS). NFR1 (performance) N/A by design.

## Lessons Learned
Framing requirements around verifiable AC→check mappings made the testing phase a
direct translation. See `j-retrospective.md`.
