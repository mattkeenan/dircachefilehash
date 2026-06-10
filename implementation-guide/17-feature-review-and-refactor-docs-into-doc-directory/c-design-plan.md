# Review and refactor docs into doc directory - Design
**Task**: 17 (feature)

## Task Reference
- **Task ID**: internal-17
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/17-review-and-refactor-docs-into-doc-directory
- **Template Version**: 2.1

## Goal
Decide, per doc, the minimal honest handling (verify / targeted-fix / historical
banner / rewrite), the `docs/` layout and link graph, and the reference-fix
inventory — so the implementation is mechanical.

## Design Priorities
Accuracy/Honesty → Minimal-rewrite ("the best part is no part") →
Discoverability → Reversibility → Consistency

## FR3 — Doc Inventory & Classification (the design's core input)
Each verdict is code-grounded (verified against `cmd/`, `pkg/`, and source — not
just `CLAUDE.md`, which is itself drifted; see Decision 6).

| Doc | Class | One-line justification (evidence) | Handling |
|-----|-------|-----------------------------------|----------|
| `README.md` | **NEEDS-REWRITE** | Built around the removed `DirectoryCache`/`FileEntry`/`NewDirectoryCache` library API (README:48,63–84,144–176); no `dcfhfind`/`dcfhfix`, no `--interactive-tree`; Quick Start won't compile. | Full rewrite around the CLI trio (Decision 2). |
| `ARCHITECTURE.md` | **NEEDS-REWRITE → targeted fixes** | High-level model (Hwang-Lin merge, context-tagged skiplists, atomic replace, status-writes-cache) is *current*; but cites deleted `BEIndexFileIOEntry` / `pkg/binary_entry_index_file.go` (line 60), a non-existent `AppendEntryToScanIndex` at `pkg/index.go:1008` (file is 994 lines; grep finds nothing), "four entry storage modes" (three impl + one conceptual), and a load-bearing RWMutex now "defensive" per `CLAUDE.md`. | Targeted fixes, keep as the current overview (Decision 3). |
| `ARCHITECTURE-IMPROVEMENTS.md` | **CURRENT** | All 12 findings match v0.13 code (recovery.go down to ~433 lines; `AppendEntryToScanIndex` deleted; `util.go` split; `BinaryEntryIterator`-only; items 11 stage 3–4 / 12 Fix-verb correctly deferred per BACKLOG). | Verify-as-current; optional BACKLOG cross-links (Decision 4). |
| `architecture-v0.7.md` | **HISTORICAL** | A design *spec/proposal* ("CRITICAL REQUIREMENT: ITERATIVE… MANDATORY", future-tense migration checkboxes, cookie/parking-skiplist pseudo-code) whose callback model differs from the shipped 4-stage channel pipeline (`pkg/pipeline_update.go`, `pipeline_status.go`: hashPool + reorderBuffer). | Historical banner (Decision 5). |
| `streaming-iterator-architecture.md` | **HISTORICAL (proposal, not shipped)** | Proposes iterator↔job-monitor notification coordination; the shipped `FilesystemScanIterator` (`pkg/iterator_filesystem.go`) does *not* subscribe — `algorithmHashManager.iteratorNotifyChans` exists and `RegisterIteratorNotification` (`pkg/algorithm_hash_manager.go:86`) has no production caller; entries are heap `BEScanEntry`, not mmap. | Historical banner (Decision 5) — not rewritten (would duplicate ARCHITECTURE.md). |
| `design.md` | **HISTORICAL (pre-v0.7 rationale)** | Design *philosophy* (git-like index, byte-ordered entries, avoid IO) still holds, but concrete claims are v0.6-era: file names `index`/`index.cache` (now `main.idx`/`cache.idx`), temp `index-<pid>-<tid>.tmp` (now timestamped `.idx`), `NewDirectoryCache` (now `NewMetaStore`), skiplist-merge pipeline (now 4-stage). | Historical banner + "what changed since" pointer (Decision 5) — not rewritten. |

## Key Decisions

### Decision 1 — Relocation by `git mv` into the existing `docs/`
- **Decision**: `git mv` the five root docs into `docs/`, keeping filenames. Root
  keeps only `README.md`, `CHANGELOG.md`, `BACKLOG.md`, `CLAUDE.md`.
- **Rationale**: `git mv` preserves `--follow` history (AC1); consolidating into
  the *existing* `docs/` (user-confirmed) avoids a parallel `doc/`. Keeping
  filenames (no `-v0.6` renames) minimises churn and broken external links; the
  historical *banner*, not the filename, carries the "superseded" signal.
- **Trade-off**: a rename would self-document staleness in `ls`, but at the cost
  of churn and `--follow` friction. Banner chosen.

### Decision 2 — README: full rewrite, CLI-first, source-grounded
- **Decision**: Replace the library-API README with a CLI-first one: what the
  three tools are; install/build (`make build`; goreleaser deb/rpm/tar.gz, Linux
  only); a runnable Quick Start (`dcfh init <dir>` → `status` → `update` →
  `dupes`); the `dcfh` daily subcommands; `--interactive-tree` (status/update,
  TTY-only); brief `dcfhfind`/`dcfhfix` sections; a "Documentation" section
  linking `docs/`. Ground every command/flag in `cmd/` source, not `CLAUDE.md`.
- **Rationale**: FR4; the front door must be correct. Source is ground truth
  because `CLAUDE.md` omits `completion`/`diff`/`remote`/`subrepo` and mislabels
  `--interactive-tree` as global.
- **Scope guard (R1)**: README stays an overview — depth defers to `docs/`. Daily
  commands documented inline; `diff`/`remote`/`subrepo`/`completion` get a brief
  mention pointing at `dcfh <cmd> help`, not full treatment. No `go` example
  survives that references the removed API (AC4 precedence clause).

### Decision 3 — ARCHITECTURE.md: targeted fixes, stays the current overview
- **Decision**: Fix the four stale spots, by line:
  (i) `ARCHITECTURE.md:57` "four entry storage modes" → "three concrete
  implementations + one conceptual (unimplemented) mode";
  (ii) `ARCHITECTURE.md:60` drop the `BEIndexFileIOEntry` /
  `binary_entry_index_file.go` row — but **keep the line-61 neighbour**
  (`binary_entry_index_file_mmap.go` / `BEIndexFileMmapEntry`, which is live);
  (iii) `ARCHITECTURE.md:137` strike the non-existent
  `AppendEntryToScanIndex@index.go:1008`;
  (iv) `ARCHITECTURE.md:186` (§6 "RWMutex on every mmap") reframe as "defensive,
  not load-bearing" per `CLAUDE.md`.
  Then re-verify the Layer-2 file/line table against current `pkg/`.
- **Coupling (the contradiction trap)**: the §6 reframe must be mirrored in
  `pkg/doc.go:65–67`, whose GoDoc still calls the RWMutex `` `mremap` `` -SIGSEGV
  protecting (load-bearing) — leaving it unedited makes the two docs disagree on
  the single subtlest claim in the set. Both move to "defensive" together (see
  the reference-fix inventory).
- **Rationale**: The mental model is current; only concrete references rotted.
  Fixing is cheaper and more useful than a banner-and-rewrite, and keeps a true
  "one-stop" contributor doc (FR5 arm (a): verified current).

### Decision 4 — ARCHITECTURE-IMPROVEMENTS.md: keep as current
- **Decision**: No content change required. The BACKLOG cross-links (items 11
  stage 3–4, 12 Fix-verb) are a **non-goal** for this task — excluded by default
  to keep scope minimal; add only if the review gate explicitly asks (same
  default-decision treatment as Decision 6).
- **Rationale**: It already aged accurately against the code (verified); "if
  cheap" is too subjective a gate to leave in an otherwise mechanical plan.

### Decision 5 — Historical banner for superseded design docs
- **Decision**: Prepend a one-block banner (immediately under the H1) to
  `architecture-v0.7.md`, `streaming-iterator-architecture.md`, and `design.md`.
  Template:
  > **Historical — superseded.** This document records an earlier
  > {design spec│proposal│pre-v0.7 design} and does **not** describe the shipped
  > code. For the current architecture see [`ARCHITECTURE.md`](ARCHITECTURE.md)
  > and the project `CLAUDE.md`. Kept for context/rationale.

  `design.md`'s banner additionally names the concrete renames (file names,
  `NewMetaStore`, 4-stage pipeline) so a reader isn't misled by the body.
- **Rationale**: FR5 arm (b). These are proposal/spec/rationale docs; rewriting
  them to re-describe the current pipeline would duplicate ARCHITECTURE.md and
  risk fresh inaccuracy ("the best part is no part"). A banner is honest, minimal,
  and reversible.
- **Trade-off**: the stale bodies remain on disk. Acceptable: the banner makes
  status unmistakable and the bodies retain historical value.

### Decision 6 — `CLAUDE.md` drift: flag, propose minimal correction, defer-able
- **Finding**: `CLAUDE.md` omits `completion`/`diff`/`remote`/`subrepo`, lists
  `--interactive-tree` as a global option (it's `status`/`update`-only), and
  under-lists global flags.
- **Decision**: This task does **not** depend on `CLAUDE.md` being right (README
  is source-grounded). Propose a *small, optional* in-scope correction to
  `CLAUDE.md`'s command list + `--interactive-tree` description; **flag for the
  review gate** since `CLAUDE.md` was scoped as "reference, stays at root" rather
  than a rewrite target. Default if review is silent: apply the minimal
  command-list/flag correction (cheap, and it's a wrong doc), nothing more.
- **Rationale**: honesty argues for fixing it; scope-discipline argues for keeping
  it minimal. The review gate decides the boundary.

## System Design
### `docs/` layout (after)
```
docs/
  README.md                          (NEW — index: one line + CURRENT/HISTORICAL per doc)
  ARCHITECTURE.md                    (moved; targeted fixes; CURRENT)
  ARCHITECTURE-IMPROVEMENTS.md       (moved; CURRENT)
  architecture-v0.7.md               (moved; HISTORICAL banner)
  streaming-iterator-architecture.md (moved; HISTORICAL banner)
  design.md                          (moved; HISTORICAL banner)
  changelog-old.md                   (unchanged — FR7)
  ssh-shell-mode.md                  (unchanged — FR7)
  feasibility/                       (unchanged — FR7)
```

### Link graph (FR6)
- `README.md` → a short "Documentation" section that points to `docs/README.md`
  (the index) and links the current starting point (`docs/ARCHITECTURE.md`)
  directly. It does **not** repeat the per-doc CURRENT/HISTORICAL tags — that list
  lives in exactly one place.
- `docs/README.md` is the **single tagged index**: each doc with a one-line
  purpose and a CURRENT/HISTORICAL marker, the markers **inherited verbatim from
  the FR3 table** (single source of truth — no second list to drift). README →
  ARCHITECTURE.md is 1 click; README → docs/README.md → any doc is ≤2 (AC6).
- Inside the docs: convert the `ARCHITECTURE.md` §5 deep-dive bare-name table
  (lines 256–263) to working markdown links — **mind the mixed targets**: the
  siblings stay bare-relative, but `cmd/dcfhfind/DESIGN.md` /
  `cmd/dcfhfix/DESIGN.md` need `../cmd/…` and `CLAUDE.md` / `BACKLOG.md` need `../`
  (they do not move into `docs/`). Per-target rule in the inventory above.

### Reference-fix inventory (FR2 — bare names, not links)
| Location | Current text | Fix |
|----------|--------------|-----|
| `pkg/doc.go:32` | "See ARCHITECTURE.md **at the repo root**" | "at the repo root" → "in `docs/`" (comment-only `.go` edit) |
| `pkg/doc.go:65–67` | RWMutex "protecting against `` `mremap` `` SIGSEGV" (load-bearing framing) | reframe to "now a defensive guard; the `` `mremap` `` scan path it once protected was removed" — mirror of the §6 fix (Decision 3 coupling) |
| `ARCHITECTURE.md:256–263` (§5 table) | bare inline-code names, **mixed targets** | convert to markdown links *by target*: **siblings** `architecture-v0.7.md` / `streaming-iterator-architecture.md` / `design.md` / `ARCHITECTURE-IMPROVEMENTS.md` → bare relative (same `docs/` dir); **`cmd/dcfhfind/DESIGN.md`** & **`cmd/dcfhfix/DESIGN.md`** → `../cmd/.../DESIGN.md`; **`CLAUDE.md`** & **`BACKLOG.md`** → `../CLAUDE.md` / `../BACKLOG.md` |
| `ARCHITECTURE-IMPROVEMENTS.md:4,175–176` | bare sibling names | links / confirm in-`docs/` |
| `architecture-v0.7.md:1155` | mentions `streaming-iterator-architecture.md` | link / confirm in-`docs/` |
| `README.md` | library-API prose | replaced wholesale (Decision 2) |
| `CHANGELOG.md:5` | `docs/changelog-old.md` | **unchanged** — already correct (FR7) |

### Data flow (reader journey)
New user → README (install + Quick Start, all valid) → "Documentation" →
`docs/README.md` → CURRENT overview (`ARCHITECTURE.md`) or a clearly-bannered
HISTORICAL doc. No path lands on uncaveated stale architecture.

## Interface Design
- No API/CLI/format surface. The only `.go` change is a comment in `pkg/doc.go`
  (NFR5; permitted by the revised "no behaviour change" constraint).

## Constraints
- Source is ground truth for the README (CLAUDE.md is drifted — Decision 6).
- `git mv` only; additive to `docs/`; preserve existing `docs/` contents (FR7).
- Rewrite pass must not touch `.cwf/**`/`.cwf-skills/**`/`.cwf-rules/**`/
  `implementation-guide/**`/`.claude/sessions/**`.
- British spelling in prose.

## Decomposition Check
- [ ] Time >1wk? No. — [ ] People >2? No. — [x] Complexity 3+? Yes (relocate /
  README rewrite / architecture reconciliation). — [ ] Risk isolation? No. —
  [x] Independence? Yes.
- **Result**: 2 signals; the design keeps it one task — the FR3 inventory above
  is the shared spine all three concerns hang off, so splitting would duplicate
  it. Revisit only if the review gate prefers independent landing.

## Validation
- [x] Classification verified by 4 parallel Explore agents against `cmd/`/`pkg/`.
- [x] Design review (4-agent map/reduce) — applied: split the §5 link rule by
  target (siblings vs `../cmd` vs `../root`), pulled `pkg/doc.go:65–67` into the
  RWMutex reframe to avoid a doc-vs-GoDoc contradiction, made `docs/README.md`
  the single tagged index (no duplicate list), tightened line citations, and
  demoted the Decision 4 cross-links to a non-goal.
- [ ] **Link-integrity gate (hand to e-testing-plan)**: a concrete post-change
  check that every relative link in the moved/edited docs resolves to an existing
  file — enumerate links and stat each target (not merely "no broken links").
  Covers AC2(iii)/AC6 and catches the §5 mixed-target failure mode.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
The six design Decisions were all executed as planned, except §6 (Decision 3) grew
from a reframe to a full paragraph replacement when its cited writer
`appendEntryToNamedIndex@pkg/index.go:967` proved non-existent — caught by
exec-time source verification. FR3 classification held up unchanged.

## Lessons Learned
The code-grounded FR3 classification table was the leverage point — it turned
implementation into a checklist. But a citation lifted from the doc under repair
(§6) was itself stale; plans quoting the artefact should flag such citations
"verify at exec". See `j-retrospective.md`.
