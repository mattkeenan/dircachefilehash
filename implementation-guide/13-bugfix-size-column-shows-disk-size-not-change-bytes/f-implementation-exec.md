# size column shows disk size not change bytes - Implementation Execution
**Task**: 13 (bugfix)

## Task Reference
- **Task ID**: internal-13
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: bugfix/13-size-column-shows-disk-size-not-change-bytes
- **Template Version**: 2.1

## Goal
Execute the implementation following the plan in d-implementation-plan.md and e-testing-plan.md.

## Execution Checklist
- [x] Read d-implementation-plan.md and e-testing-plan.md thoroughly
- [x] Verify all prerequisites met
- [x] Execute implementation steps sequentially
- [x] Update "Actual Results" for each step
- [x] Document any deviations from plan
- [x] Update status to "Implemented" when complete

## Actual Results

### Step 1: `columnText` helper (`sort.go`)
- **Planned**: Add `import "strconv"` and `columnText(n, key)` reusing
  `metric()`; remap `name`→`change_bytes` before reading the value.
- **Actual**: Added exactly as planned (`sort.go`). The `name` branch sets
  `key = sortChangeBytes` first, then `metric()` is called once; `change_bytes`
  formats via `dcfh.FormatHumanSize`, all other keys via `strconv.FormatInt`.
- **Deviations**: None.

### Step 2: Wire-up (`render.go:drawRow`)
- **Planned**: Replace `dcfh.FormatHumanSize(row.node.Stats.Bytes)` with
  `columnText(row.node, m.sortKey)`; rename `size`→`colVal`, `sizeX`→`colX`;
  update the comment.
- **Actual**: Done as planned. `Stats.Bytes` no longer read in `drawRow`; the
  stats pane (`drawStats`) still shows it as the live `Size:` line (unchanged).
- **Deviations**: None.

### Step 3: Tests
- **Planned**: `sort_test.go` per-key matrix (incl. `name`→change-bytes F1
  guard and the deleted-only discriminator); `render_test.go` wire-up test on
  the `docs` discriminating node + count toggle.
- **Actual**: Added `TestColumnText` (unit matrix, TC-1…TC-4 incl. explicit
  "name != 0 B" guard and the Stats.Bytes-0/change-bytes-900 discriminator) and
  `TestColumnTracksActiveSortMetric` (integration: `docs` row shows `900 B`
  under `change_bytes`, becomes the count `1` and drops bytes after `f`). Added
  a `rowLine` screen-line helper.
- **Deviations** (2, both minor cleanups in the file I was already editing):
  1. `rowLine` uses `strings.SplitSeq` (not `strings.Split`) per the
     `stringsseq` linter suggestion.
  2. Dropped the dead `h` parameter from the shared `newSimModel` helper
     (every caller passed 24) to clear a pre-existing full-tree `unparam`
     finding the new call site surfaced. Height is now fixed at 24 inside the
     helper; all 4 call sites updated. Pure test-helper refactor, no behaviour
     change.

### Step 4: Validate — all green
- `go build ./...`: OK.
- `go test ./cmd/... ./pkg/...`: all packages pass (tui 0.020s, dcfh 5.17s,
  pkg 0.234s).
- `golangci-lint run ./...`: **0 issues** (full tree, gosec floor included).
- Manual smoke: deferred to g-testing-exec / user review.

## Blockers Encountered

None. (One latent `unparam` finding on `newSimModel` surfaced by the new
caller; resolved by removing the dead parameter — see Step 3 deviation 2.)

## Deferral Check
Before marking status=Finished, verify:
- [ ] All steps from d-implementation-plan.md executed
- [ ] All success criteria from a-task-plan.md met
- [ ] All requirements from b-requirements-plan.md addressed (if applicable)
- [ ] All design guidance in c-design-plan.md followed (if applicable)
- [ ] No planned work deferred without user approval
- [ ] If work deferred: Follow-up task created and linked

**If deferral required**: Get user approval, document rationale, create follow-up task.

## Security Review

**State**: no findings

Changeset helper: `reviewed 8 files, 777 lines (132 production), anchor=597bb5f,
includes uncommitted` (exit 0, no warnings). Reviewer verdict (verbatim):

I have read the full changeset. Let me reason through the FR4 threat categories.

This is a render-layer bugfix in `cmd/dcfh/internal/tui`. The substantive code
changes are confined to two production files plus their tests and CWF workflow
markdown.

**(a) Injection (command/SQL/path/format-string).** The new production code in
`sort.go` is `columnText`, which calls `metric(n, key)` and formats the result
with either `dcfh.FormatHumanSize(v)` or `strconv.FormatInt(v, 10)`. Both inputs
are `int64` integers derived from in-memory `Stats` aggregates; neither is
interpolated into a shell command, SQL query, file path, or a format string used
as a template. The `render.go` change passes the result through
`drawText`/`fmt.Sprintf("%*s%s%s", ...)` where `%*s` width is an integer and the
operands are labels/markers, not attacker-controlled format strings. No command
execution, no path construction. Clean.

**(b) Secrets.** No credentials/tokens/keys touched; renders byte/integer counts;
nothing logged or persisted. Clean.

**(c) Auth / access control.** N/A — terminal UI render path, no privilege
boundary. Clean.

**(d) Env-var / config handling.** No env reads, no new config knobs. Clean.

**(e) Prompt-injection / pattern-based risk.** No LLM/agent surface; strings come
from internal integer aggregates. Pattern notes (non-actionable): `len()`-based
right-alignment safe-here because `columnText` output is ASCII (audit on
non-ASCII reuse); non-nil `n` precondition holds via `rebuildRows`; no new int
narrowing so G115 surface untouched.

```cwf-review
state: no findings
summary: Render-layer bugfix (columnText over trusted int64 Stats aggregates); no injection/secrets/auth/env/prompt-injection surface. ASCII-only len()-alignment noted as safe-here, audit on non-ASCII reuse.
```

## Status
**Status**: Finished
**Next Action**: /cwf-testing-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
*To be filled upon completion*

## Lessons Learned
*To be captured during retrospective*
