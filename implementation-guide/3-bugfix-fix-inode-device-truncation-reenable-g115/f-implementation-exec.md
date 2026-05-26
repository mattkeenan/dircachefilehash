# Fix inode device truncation re-enable G115 - Implementation Execution
**Task**: 3 (bugfix)

## Task Reference
- **Task ID**: internal-3
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: bugfix/3-fix-inode-device-truncation-reenable-g115
- **Template Version**: 2.1

## Goal
Parent **close-out** (d-implementation-plan.md Step 4): confirm all three subtasks are merged into
the parent branch, cite each subtask's gate result, and verify the parent `a-task-plan.md` success
criteria end-to-end on the merged tree. All per-call-site implementation was executed inside the
subtasks — this phase carries no new code.

## Execution Checklist
- [x] All subtasks merged into the parent branch (ff-only)
- [x] Each subtask's gate result cited from its `f-`/`g-` files
- [x] Parent success criteria (a-task-plan.md) verified end-to-end on the merged HEAD
- [x] Security review performed
- [x] Status set to Finished

## Subtask integration
Parent branch fast-forwarded `cbfa32f → fd3546e`; all three subtasks land with no merge commit:

| Subtask | Concern | Status | Gate result (from its `g-testing-exec.md`) |
|---------|---------|--------|--------------------------------------------|
| 3.1 (chore) | Extract `pkg/format`, migrate all fields, no width/version change | Finished | TC-1..TC-6 + header-size invariant pass; full regression green; gofmt clean; `golangci-lint run ./pkg/format/ ./cmd/dcfhfix/` = 0; static G115 63→52 |
| 3.2 (feature) | Version-aware read/write registry; dcfhfix consumes it | Finished | Version gate + read-old/write-current verified; negatives error cleanly; full `pkg`/`cmd` regression green; G115 == 52 (unchanged) |
| 3.3 (bugfix) | Widen Dev/Ino → uint64, v3→v4, fix dupes key + ingest casts, re-enable G115 | Finished | 10/10 TC pass; full-width dev/ino round-trip; v3 heap-decodes every post-Ino field; v4 golden anchor; dupes correct on >2³² inodes; race gate green; **0 G115 whole-tree** |

Each subtask's gate enforced the design's correctness-first ordering (no width/version change before
the encapsulation refactor was green; widen only after the version-aware decode path existed).

## Parent success-criteria verification (end-to-end, merged HEAD `fd3546e`)

- **SC1 — Dev/Ino stored/read as 64-bit; no `uint32(...)` truncation; dupes keys full-width**: ✅
  `pkg/dupes.go:253,256` use `map[[2]uint64]` keyed on `{e.Dev, e.Ino}`; the dev/ino ingest casts are
  gone (remaining `uint32(...)` in `binary_entry_scan.go` are Size/Mode/EntryFlags — Mode is genuinely
  32-bit `st_mode`, none are dev/ino). Verified by `TestDedupByInode_*`.
- **SC2 — on-disk field types defined in exactly one module; no hand-typed widths/offset tables
  survive**: ✅ `DevID`/`Inode` (and the rest of the vocabulary) are aliases owned solely by
  `pkg/format/vocabulary.go`; legacy 32-bit widths live in `v2_layout.go`/`v3_layout.go`; the widen is
  ring-fenced in `transcode.go`. The duplicate `binaryEntry`/`indexHeader` and the parallel offset
  table in `cmd/dcfhfix` were deleted in 3.1.
- **SC3 — v2/v3 read at original width, new writes produce v4, round-trip verified**: ✅ legacy indices
  route through the bounds-checked whole-region heap transcode (never cast in place); writes stamp v4.
  Verified by `TestLegacyLoad_V3_RoutesThroughHeapTranscode`, the v3/v4 goldens, and the dcfhfix v4
  write-stamp test.
- **SC4 — G115 removed from `gosec.excludes`; `golangci-lint run ./...` clean**: ✅ for its G115 intent —
  G115 is active and the whole-tree run reports **0 G115 findings** (the Dev/Ino class fixed
  structurally, 55 provably-safe sites annotated per-line). Caveat: the whole-tree run still reports **3
  pre-existing non-G115 findings** (`cyclop` ×2 in `cmd/dcfhfind/main.go:455` + `pkg/filter_run.go:75`;
  `unparam` ×1 in `pkg/binary_entry_scan_test.go:200`) in functions untouched by Task 3. These predate
  the task baseline (885a4ef), are already captured in BACKLOG ("Clear pre-existing full-tree
  golangci-lint failures"), and do not trip the enforcing `--new` staged gate used by
  `.githooks/pre-commit`. Not Task 3 regressions.
- **SC5 — full suite passes + new cross-version / full-width tests**: ✅ canonical race gate
  `GOFLAGS=-gcflags=all=-d=checkptr=0 go test -race -short ./...` green across every package
  (`cmd/dcfh`, `cmd/dcfhfind`, `cmd/dcfhfix`, `pkg`, `pkg/format`, `pkg/fsdedupe`).

## Deviations from plan
None at the parent level. The 3.2 boundary note (entry-tier decoder "may fold into 3.3") resolved as
planned: 3.2 delivered the header-tier version registry; the entry-tier widen + transcoder landed in 3.3.

## Blockers Encountered
None.

## Deferral Check
- [x] All d-implementation-plan.md steps executed (Steps 1–3 in subtasks; Step 4 close-out here)
- [x] All a-task-plan.md success criteria met (SC4 G115 intent met; 3 pre-existing non-G115 findings
      tracked in BACKLOG with prior user approval — not a silent deferral)
- [x] All c-design-plan.md guidance followed (single format module; host-order zero-copy fast path
      preserved; format invariants unchanged)
- [x] No planned work deferred without user approval

## Status
**Status**: Finished
**Next Action**: /cwf-testing-exec 3
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Parent branch ff-merged to `fd3546e` (all three subtasks). All five parent success criteria verified on
the merged HEAD: full-width dev/ino + full-width dupes key (SC1), single-module format ownership (SC2),
v2/v3→v4 heap-transcode round-trip (SC3), G115 active with 0 whole-tree findings (SC4), race suite green
(SC5). Three pre-existing non-G115 lint findings (cyclop ×2, unparam ×1) remain in untouched functions,
already backlogged; the enforcing `--new` gate is clean.

## Lessons Learned
A parent close-out over a fully-decomposed task is verification, not code: the honest move is to re-run
the canonical gates on the merged HEAD and cite each subtask's recorded gate, rather than re-deriving
results. The SC4 wording ("golangci-lint clean") needed an honest caveat — the G115 objective is fully
met, but 3 pre-existing non-G115 findings remain in untouched code, so the criterion is recorded as met
for its intent with the residual tracked in BACKLOG. Full synthesis in j-retrospective.md.

## Security Review

**State**: no findings

no findings: empty changeset

The `security-review-changeset --phase=implementation` helper emitted 0 files (anchor `885a4ef`). The
parent's diff is entirely Go (`pkg/format/*.go`, `pkg/dupes.go`, `pkg/binary_entry_scan.go`,
`cmd/dcfhfix/*`, tests), outside the helper's security-relevant pathspec (CWF-internal tooling +
shebang scripts), and the parent close-out adds no new code of its own. The actual untrusted-input
attack surface — the v2/v3 whole-region transcoder and the legacy parsing path — was reviewed
semantically at the subtask level: 3.3's `f-implementation-exec.md` ran a manual
`cwf-security-reviewer-changeset` pass over that ~699-line parsing diff (bounds checks, no in-place
cast of untrusted bytes, oversized-EntryCount allocation guard) with **no findings**. The standing Go
security floor is gosec via golangci-lint: whole-tree **0 G115** at merged HEAD.
