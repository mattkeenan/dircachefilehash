# Fix primitive and CLI restructure - Design
**Task**: 28.2 (feature)

## Task Reference
- **Task ID**: internal-28.2
- **Task URL**: N/A (internal task)
- **Parent Task**: 28
- **Branch**: feature/28.2-fix-primitive-and-cli-restructure
- **Template Version**: 2.1

## Goal
Concrete architecture for the `Repo.Fix` primitive and the dcfhfix-as-thin-translator
restructure, refining parent task 28's D1/D2/D4/D6 for the **single-source** slice.
Multi-source recovery rebuild (parent D-flow "Recovery rebuild", FR8) is 28.3.

## Design Priorities
Testability → Readability → Consistency → Simplicity → Reversibility

## Architecture Preferences
Composition over inheritance. Interfaces over singletons. Explicit over implicit.

## Inherited architecture (parent task 28)
This subtask implements, unchanged in intent: **D1** (`Fix` mirrors `Filter`), **D2**
(selectors read-only; writes never selector-derived), **D4** (tagged `FixCommand`
list), **D6** (two construction paths into one `RunFix`; two-field synthesised
MetaStore for the repo-less CLI path). **D3** (single-writer path) and **FR9** already
landed in 28.1 (`pkg/fix_entry_workflow.go:writeRepairedIndex`, `newFixMetaStore`).
The decisions below (LD1–LD7) are the 28.2-local refinements that turn D1/D2/D4/D6
into code, resolving the open questions the plan reviews surfaced.

## Key Decisions

### LD1 — Two-layer split: `repoCore.Fix` (method) + `RunFix` (package fn) (Consistency, D1)
- **Decision**: mirror `Filter`/`RunFilter` exactly (`pkg/repo_local.go:346`, `pkg/filter_run.go:168`). `repoCore.Fix` does selector resolution (`ResolveIndexSelectors(r.ms.MetaDir, …)`), default-fill, and the D2 confinement assertion, then delegates to package-level `RunFix(ctx, refs, req, warnOut)` in a new `pkg/fix_run.go`. `RunFix` owns per-command execution and is the shared core for both callers (D6).
- **Rationale**: keeps the library surface uniform; the confinement check lives in the method that knows `MetaDir`, while `RunFix` stays caller-agnostic.
- **Trade-offs**: `RunFix` carries write behaviour `RunFilter` does not, so the symmetry is structural, not line-for-line (as parent D1 noted).

### LD2 — `FixCommand` tagged struct; `RunFix` routes by op family (Readability, D4, FR2)
- **Decision**: `FixRequest.Commands []FixCommand`; `FixCommand` is a flat tagged struct keyed by `Op FixOp` with typed payload fields (`Field`, `Value`, `Index`, `Append *EntryJSON`). Each `FixOp` has a compile-time **read-only vs write** classification (`fixOpIsWrite` map / `FixOp.IsWrite()`), the single source of truth the LD3 confinement check consults. All ten of today's operations are expressible (FR2 table below) so the library can drive the full surface — but `RunFix` **routes by family**, it does not force every op through the index single-writer path:
  - **index-mutating** (`header-edit`, `entry-edit`, `entry-append`, `entry-remove`): collect/validate → confine → `writeRepairedIndex` (28.1 single-writer) → `FixResult` repair counts.
  - **inspection** (`header-show`, `entry-show`, `fixes-list`): read-only; render via the existing inspection helpers; no repair counts, no write.
  - **backup-stack** (`fixes-pop`, `fixes-discard`, `fixes-clear`): manipulate the `.dcfh/fixes/` stack via the relocated stack helpers (LD6); classified **write** so the stack directory is confined, but they do **not** go through `writeRepairedIndex` — they are stack management, not index rewrites.
- **`edit json` forms**: `header edit json` / `entry edit json` map onto the same `header-edit` / `entry-edit` `Op` carrying the JSON in `Value` with `Field:"json"` — no separate carrier. FR4 behaviour-preservation includes preserving the **current implementation status**: `entry edit json` is a stub today (`main.go:835`, "not yet implemented"); the restructure preserves that error, it does not silently implement it.
- **Rationale**: a flat struct keeps the batch JSON-inspectable and the CLI→command translation mechanical; routing-by-family means `RunFix` stays one entry point (FR1/FR2 uniformity) without pretending a stdout-printing `show` or a stack `pop` is an index repair. Keying confinement off a typed property (not a re-derived per-call-site judgement) means a future `FixOp` added without a classification entry fails closed (treated as write).
- **Trade-offs**: unused fields per variant — acceptable for a small closed set.

### LD3 — Write-destination confinement helper, fail-closed (Security, D2, NFR4/AC7)
- **Decision**: a package-level `confineWriteDest(dest, root string) (string, error)` in `pkg/fix_run.go`:
  1. `absDest = filepath.Clean(absolute(dest))`; `absRoot = filepath.Clean(absolute(root))`. Reject if `filepath.Base(absDest)` is `..` or otherwise not a single path component (the raw selector string reaches here via the `RefTypeFile` fall-through — `Base` is the last guard before recombination).
  2. Resolve symlinks on the **existing parent** of `dest` (a write target — e.g. a new `.pre-fix-*` sibling — need not exist yet; `EvalSymlinks` on the full path would error). Recombine resolved-parent + `filepath.Base(dest)`.
  3. Assert `hasPathPrefix(resolvedDest, resolvedRoot)` (reuse `pkg/wire_handler.go:240` — already package-level, no method receiver). **Any resolve/abs error rejects the write** (fail-closed).
- **Confine every artefact, not just the final index**: a single Fix call can touch four disk paths — the final index (`dest`), the `.fix.tmp` sibling (`writeRepairedIndex`, `fix_entry_workflow.go:119`), the `.pre-fix-<ts>` sibling (`PreserveOriginal`/`PromoteRepairedIndex`, `fix_promote.go:30`), and the backup target (`createBackup` → `getBackupDir`, which walks **up** to find a `.dcfh`, `main.go:950` — *not* a sibling of `dest`). Each is asserted via `confineWriteDest` against the appropriate root before it is written; the upward-walked backup dir in particular must be independently bounded, since it is not derived from the confined `dest`.
- **Two entry points** (D6): `repoCore.Fix` confines every write artefact to `r.ms.MetaDir`. The CLI path repairs an **explicitly-named single subject**: it first canonicalises the subject (resolve symlinks on the subject's existing parent, mirroring step 2), then confines the temp/`.pre-fix` siblings to that resolved subject directory and the backup to the upward-resolved `.dcfh`. The explicit-subject exemption is one validated path the CLI sets, **not** a request flag a library consumer can set to disable confinement; `RunFix` distinguishes the two by which constructor populated the request (library → MetaDir root; CLI → resolved-subject-dir root), never by a selector value.
- **TOCTOU**: `PreserveOriginal` already refuses a planted symlink/dir at the preservation dest via `O_WRONLY|O_CREATE|O_EXCL` (`fix_promote.go:47`) and writes-then-renames; `confineWriteDest` layers path-prefix containment on top (defence in depth), it does not replace that guard.
- **Rationale**: turns the `RefTypeFile` fall-through (`pkg/filter_run.go:143` — any unknown string → arbitrary path) from a write-steering hazard into a read-only-only capability. Read commands (header/entry show) may target an arbitrary `RefTypeFile`; write commands route through `confineWriteDest`.
- **Trade-offs**: the CLI's legitimate "repair a file anywhere" need is why MetaDir confinement can't be the only mode; the explicit-subject mode is the bounded escape valve, tested on both sides (AC7).
- **gosec (impl-plan checklist item)**: the migrated G304/G703/G306 suppressions all copy the verbatim *"…user-supplied CLI argument … no trust boundary"* rationale — which becomes **false** the moment `repoCore.Fix` (a library entry point) reaches these functions. Each suppression's rationale is **rewritten** to cite the `confineWriteDest`/`MetaDir` invariant, not copy-pasted. Not a blanket re-exclude.

### LD4 — One shared cap predicate across the three walk loops (Reliability, NFR5/AC6)
- **Decision**: the cap is enforced in **three structurally different** functions — `processAllEntriesWorkflow` (edit, via `processSingleEntry`/`entryOutcome`, `fix_entry_workflow.go:230`), `processAllEntriesForAppend` (`:496`), and `processAllEntriesForRemoval` (`:534`). They are **not** copies of one loop: each has a distinct per-entry body (edit-by-field, keep-all-append, drop-by-path). The consolidation extracts only the shared **cap-check predicate** (the `unfixableCount > unfixableMax` guard) into one helper the three loops call; their collection bodies stay separate. The boundary is documented and tested **on all three paths**: the cap trips on the **101st** unfixable entry (`> 100`), not just on the edit path.
- **Rationale**: one predicate = one boundary behaviour, without conflating three operations into one walk (which would risk silently changing append/removal corruption-skip semantics).
- **Trade-offs**: a small shared helper vs three inline guards; justified by removing the divergence risk the review flagged.

### LD5 — Auto-fix / dry-run / Manual wiring (FR5/FR6)
- **`FixMode` is the existing enum, reused (not new)**: `FixMode`/`FixModeNone`/`FixModeAuto`/`FixModeManual` already exist at `pkg/recovery.go:46` (parent D5 mandated reuse). 28.2 delivers `FixModeAuto`; for the existing `FixModeManual` value, `RunFix` returns a **new** typed sentinel `ErrManualModeUnimplemented` (no write, non-zero CLI exit, asserted by test). No new mode type is introduced.
- **Dry-run requires a real collect/write split, not a free gate**: the public wrappers `ProcessEntriesWithWorkflow` (`:178`) and its append/removal siblings call `writeRepairedIndex` **unconditionally** — there is no existing seam between collect and write. So `RunFix` invokes the **pure** `processAll…` collector phase directly (bypassing the fused `ProcessEntriesWith*` entry points) and only then decides whether to call `writeRepairedIndex`. `DryRun` returns the would-be counts after collect and writes nothing — no `.fix.tmp`, no `.pre-fix-*`, no `fixes/` backup.
- **Rationale**: the pure decision phase (`processAllEntriesWorkflow`, no I/O) and the write (`writeRepairedIndex`) are already distinct functions in 28.1; the dry-run gate is the new control-flow split between them. This is a genuine restructure of the fused wrappers (budgeted in the implementation plan), not zero-cost reuse — designing it explicitly guards against dry-run accidentally writing by going through a fused wrapper.
- **Trade-offs**: shipping a `Manual` value that errors is mild dead-surface (parent D5), justified by keeping the enum stable.

### LD6 — CLI translation: handler → `FixCommand` batch → `RunFix` (Readability, D6, FR4)
- **Decision**: each `cmd/dcfhfix` handler (`handleHeaderCommand`/`handleEntryCommand`/`handleFixesCommand`, `main.go:393/416/452`) shrinks to: parse args → build a `FixRequest{IndexSelectors:[explicit subject], Commands:[…], Mode:Auto, DryRun, Backup}` → call the shared core. The repo-less path synthesises the two-field `*MetaStore` via 28.1's `newFixMetaStore(metaDir, subjectChecksumType)` (signature + checksum-type only; reads the subject header's `checksum_type`, asserts the round-trip). Inspection subcommands (show/list) stay read-only and keep their current output bytes/exit codes verbatim.
- **Rationale**: collapses to one orchestration core; the CLI becomes arg-parsing + command construction. `fixFlags()` (`main.go:106`) already projects `ParsedOptions` onto the narrow `FixEntryFlags`; the same projection feeds `DryRun`/`Backup`.
- **Trade-offs**: `RunFix` must accept the synthesised-MetaStore (CLI) and the real-repo (library) origins uniformly — handled by LD3's two-entry-point construction.

### LD7 — `BackupID` dropped (FR7/AC5 — the open decision, resolved)
- **Decision**: **drop** `FixResult.BackupID` for 28.2. `FixResult` is `{RepairsApplied, EntriesDiscarded}`. Backup discovery/rollback is `fixes-list` (enumerate the stack) + `fixes-pop` (LIFO restore) — both available as `FixCommand` variants, so the library has the same access as the CLI.
- **Rationale**: the parent design note made retention conditional on it exposing information `fixes-list` cannot, at low cost. Inspection of the code defeats both premises: (1) **cost is not near-zero** — `createBackup` (`main.go:972`) returns only `error`; its `backupPath` is internal, so surfacing an id needs a signature change threaded through the FR3 relocation. (2) The proposed id **conflated two unrelated artefacts** — the always-on `.pre-fix-<ts>` sibling (`PreserveOriginal`, independent of `--backup`) versus the `--backup`-gated `.dcfh/fixes/<type>/<ts>.idx` stack entry — with contradictory emptiness rules. An ambiguous "which artefact does this id name" is a correctness hazard for the very programmatic-rollback use it was meant to serve. The lean, unambiguous answer is to not add the field; the stack is already the rollback mechanism.
- **Trade-offs**: a programmatic caller rolls back via `fixes-list`+`fixes-pop` rather than a returned id. If a concrete in-tree consumer later needs a call-scoped id, it can be added then against a single, well-defined artefact. *(Resolves AC5; updates parent FR1/FR7's `BackupID` mention — reflected in b-requirements.)*

## System Design

### Component Overview
- **`pkg/fix_run.go`** (new): `FixRequest`/`FixResult`/`FixCommand`/`FixOp` types; `fixOpIsWrite` classification; `RunFix` orchestration + mode dispatch; `confineWriteDest`; `ErrManualModeUnimplemented`.
- **`pkg/fix_backup.go`** (new, relocation from `cmd/dcfhfix`, FR3): `createBackup`/`getBackupDir`/`listBackups`/`getIndexType`/`copyFile` + the fixes-stack ops (`fixesList`/`fixesPop`/`fixesDiscard`/`fixesClear`), exported as needed. Behaviour-preserving.
- **`pkg/fix_entry_workflow.go`** (reused + restructured, 28.1 origin): `writeRepairedIndex`, `newFixMetaStore` reused as-is. The three `processAllEntriesFor{Edit,Append,Removal}` collectors are split from their fused `ProcessEntriesWith*` write wrappers so `RunFix` can call the pure collect phase then gate the write (LD5); they share one cap-check predicate (LD4).
- **`repoCore.Fix`** (`pkg/repo_local.go`, new method beside `Filter`): selector-resolve + default-fill + MetaDir confinement → `RunFix`.
- **`pkg/repo.go`**: add `Fix(ctx, FixRequest) (*FixResult, error)` to the `Repo` interface beside `Filter`.
- **`cmd/dcfhfix/*`** (FR4): handlers shrink to arg-parse → `FixCommand` batch → shared core; subcommand surface, help, exit codes unchanged.

### Command → FixCommand classification (FR2)
| Subcommand            | `FixOp`         | Class      |
|-----------------------|-----------------|------------|
| header show           | `header-show`   | read-only  |
| header edit           | `header-edit`   | **write**  |
| entry show            | `entry-show`    | read-only  |
| entry edit            | `entry-edit`    | **write**  |
| entry append          | `entry-append`  | **write**  |
| entry remove          | `entry-remove`  | **write**  |
| fixes list            | `fixes-list`    | read-only  |
| fixes pop             | `fixes-pop`     | **write**  |
| fixes discard         | `fixes-discard` | **write**  |
| fixes clear           | `fixes-clear`   | **write**  |

Write ops route through `confineWriteDest` before touching disk; read ops do not.

### Data Flow (entry/header edit — single source)
1. Caller builds `FixRequest{IndexSelectors, Commands, Mode:Auto, DryRun, Backup}`.
2. `repoCore.Fix` resolves refs; for each write command asserts `confineWriteDest(dest, MetaDir)` (CLI: confined to the named subject's directory). Reject → return before any write.
3. Optional backup (`createBackup`, FR7) unless `DryRun`.
4. Entry path → `ProcessEntriesWithWorkflow` (pure collect/validate; single cap site, LD4).
5. `DryRun`? discard, report counts (no artefact written). Else serialise → `writeRepairedIndex` → temp → atomic rename (28.1 single-writer path).

## Interface Design

### Data Models
```go
type FixOp string // "header-show","header-edit","entry-show","entry-edit",
                  // "entry-append","entry-remove","fixes-list","fixes-pop",
                  // "fixes-discard","fixes-clear"

type FixCommand struct {
    Op     FixOp
    Field  string     // header-edit / entry-edit
    Value  string     // header-edit / entry-edit
    Index  int        // entry-edit / entry-remove (by index)
    Append *EntryJSON // entry-append — reuses pkg EntryJSON (relocated in 28.1)
}

type FixRequest struct {
    Options        Options      `json:"options"`
    IndexSelectors []string     `json:"index_selectors"`
    Repository     string       `json:"repository,omitempty"`
    Commands       []FixCommand `json:"-"`
    Mode           FixMode      `json:"-"` // Auto delivered; Manual → typed error
    DryRun         bool         `json:"dry_run,omitempty"`
    Backup         bool         `json:"backup,omitempty"`
}

type FixResult struct {
    RepairsApplied   int `json:"repairs_applied"`
    EntriesDiscarded int `json:"entries_discarded"`
}
```
*(No `BackupID` — LD7. No `IndexFilesProcessed`: single-source ⇒ tautological. A multi-source count, if ever needed, arrives with FR8 in 28.3.)*

### API
```go
// pkg/repo.go — on the Repo interface, beside Filter:
Fix(ctx context.Context, req FixRequest) (*FixResult, error)

// pkg/fix_run.go — shared core for repoCore.Fix and the dcfhfix CLI.
// writeRoot is the confinement root (caller-supplied: MetaDir for the library,
// resolved subject dir for the CLI) — NOT a FixRequest field, so a library
// consumer cannot set or relax it (LD3).
func RunFix(ctx context.Context, refs []IndexRef, req FixRequest, writeRoot string, warnOut io.Writer) (*FixResult, error)
```

## Constraints
- Single writer (`TempIndexWriter`/`EntrySerialiser`); main/cache read-only mmap; temp pure-vectorio — all preserved (D3, landed 28.1).
- No on-disk format change; produced indices satisfy the existing header/checksum/layout contract.
- No new third-party dependencies. British spelling in prose/comments.
- **Out of scope**: `mergeSourcesIntoEntries` + multi-source recovery rebuild (FR8 → 28.3); Manual interactive mode (deferred).

## Decomposition Check
- [ ] **Time**: 3-4 days, <1 week. No.
- [ ] **People**: Solo. No.
- [ ] **Complexity**: one cohesive concern (primitive + its CLI); parent already decomposed. No.
- [ ] **Risk**: LD3 confinement is the one high-risk item, isolated by its own helper + both-sides test. No.
- [ ] **Independence**: CLI depends on the primitive; not separable. No.

**Result: 0 of 5 → 28.2 correctly sized.**

## Validation
- [ ] Design review completed (4-agent map/reduce — see checkpoint).
- [ ] Architecture approved by user.
- [x] Integration points verified against source (`Filter`/`RunFilter` shape, `ResolveIndexSelectors` fall-through, `hasPathPrefix`, `ProcessEntriesWithWorkflow`, `newFixMetaStore` — all confirmed by Read).

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-plan 28.2
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
D1–D7 followed, with exec-time deviations (f-implementation-exec.md): header-edit
keeps its surgical writer (D-LD2's "index-mutating → writeRepairedIndex" grouping
was imprecise); `FixCommand` simplified to Op/Field/Value/Paths.

## Lessons Learned
The "all index-mutating ops route through writeRepairedIndex" grouping was too
coarse — that path normalises layout + recomputes checksum and so cannot express
header (version/flags/signature) edits. Surgical and bulk writers are distinct
tools; a sharper design pass would have separated them up front.
