# Add runnable Repo library usage examples - Implementation Plan
**Task**: 26 (chore)

## Task Reference
- **Task ID**: internal-26
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/26-add-runnable-repo-library-usage-examples
- **Template Version**: 2.1

## Goal
Add black-box `Example*` functions for the `Repo` library surface so godoc renders
runnable usage guidance for external consumers.

## Workflow
Patterns first → write examples → `go test`/`go vet`/`go doc` green → commit explains "why"

## Key Decisions (from survey + plan review)
- **Black-box test package**: examples live in `package dircachefilehash_test` and
  import `github.com/mattkeenan/dircachefilehash/pkg` aliased as `dircachefilehash`.
  This is the first external-test-package file in `pkg/` (all existing tests are
  white-box `package dircachefilehash`). Rationale: examples must exercise *only*
  exported API (proving the public surface is sufficient) and render in godoc with
  the `dircachefilehash.` qualifier exactly as a consumer writes it. Both packages
  can coexist in the same directory — standard Go. Note: the external package cannot
  reach concrete types (`*localRepo`) — work through the `Repo` **interface** value.
- **Single file**: put all examples + the helper in one new file
  `pkg/example_repo_test.go` (~8 example funcs + helper). Keeps the helper co-located
  with every caller; lower new-file count than splitting config/snapshots out.
- **Repo construction + cleanup contract**: `CreateRepo(ctx, root, "")` → local repo
  with `.dcfh` under `root` (returns the **`Repo` interface**, not `*Repo`);
  `OpenRepo(ctx, metaDir)` reopens. Mirror `repo_test.go` patterns. Examples can't
  take a `*testing.T` (no `t.TempDir()`), so a small unexported helper
  `mustExampleRepo() (repo dircachefilehash.Repo, cleanup func())` builds a temp dir
  via `os.MkdirTemp` + seeds **literal-named** files, `panic`-ing on setup error. The
  returned `cleanup` runs `repo.Close()` **then** `os.RemoveAll(dir)` (in that order);
  each example `defer cleanup()`. This avoids the temp-tree leak examples would
  otherwise cause and keeps the seed paths taint-free.
- **Determinism**: only `Example*` funcs with a trailing `// Output:` are executed
  and verified. Print **stable derived values** (counts, fixed file names) — never
  timestamps, temp paths, or unsorted slices. Sort any slice before printing.
- **Index-only ops need a prior `Apply`** (review catch): `Groups` and `Filter` read
  the index, not the filesystem, and a fresh `CreateRepo` has an empty main index.
  `ExampleRepo_Groups`/`ExampleRepo_Filter` must **seed → `Apply` → then call** the
  verb, or counts come back 0. `Filter` additionally **errors on empty `Actions`**
  (`repo_local.go:347`), so always pass `Actions: []FilterAction{&PrintAction{}}`.
- **Filter construction is clean, not awkward** (review correction): use the exported
  `dircachefilehash.MustNewNameTest("*.go", false)` constructor (`filter.go:155`,
  built for constant patterns) as `Expression` — no raw `gitignore.Pattern` literal.
  `PrintAction.Execute` writes matched paths to **stdout** via `fmt.Println`
  (`filter.go:577`), so with fixed seed names and the index's path-sorted iteration
  the printed paths are deterministic and can be asserted directly in `// Output:`
  (optionally alongside `*FilterResult.EntriesMatched`). No compile-only fallback
  needed.

## Files to Modify
### Primary Changes
- `pkg/example_repo_test.go` (**new**) — black-box examples for the whole consumer
  surface in one file: `ExampleCreateRepo`, `ExampleOpenRepo`, `ExampleRepo_Diff`,
  `ExampleRepo_Apply`, `ExampleRepo_Groups`, `ExampleRepo_Filter`, `ExampleRepo_Config`,
  `ExampleRepo_Snapshots`, plus the `mustExampleRepo` helper.

### Supporting Changes
- **None.** The opportunistic filter-DSL godoc edit was **dropped after review**: the
  five AST nodes (`MMinTest`, `CTimeTest`, `CMinTest`, `OrExpression`, `NotExpression`)
  are already documented by deliberate **group comments** (`filter.go:445`, `:523`);
  the survey's "undocumented" finding was a heuristic miss. Adding per-type comments
  would duplicate/contradict the established convention. No production file changes —
  the task is purely the new `_test.go` file. (Package-level docs: already redone in
  Task 17, confirming the "Update API documentation" backlog item is fully superseded.)

## Implementation Steps
### Step 1: Setup
- [ ] Confirm import path/alias compile in a throwaway `package dircachefilehash_test`
      file (`dircachefilehash "github.com/mattkeenan/dircachefilehash/pkg"`).
- [ ] Write `mustExampleRepo() (dircachefilehash.Repo, func())` helper: `os.MkdirTemp`
      → seed literal-named files → `CreateRepo(ctx, dir, "")`; `cleanup` does
      `repo.Close()` then `os.RemoveAll(dir)`. Each example `defer cleanup()`.

### Step 2: Lifecycle + filesystem-facing verbs (`// Output:` on stable values)
- [ ] `ExampleCreateRepo` / `ExampleOpenRepo` — create, `Info`, `Close`, reopen via
      `OpenRepo(ctx, metaDir)`. Output: a fixed derived value (e.g. `Stats().FileCount`).
- [ ] `ExampleRepo_Diff` — seed files (no Apply yet → they show as Added), `Diff`,
      print `len(Added)` (deterministic count).
- [ ] `ExampleRepo_Apply` — `Apply` then `Stats`; print `FileCount` (pick one of the
      two FileCount sources — `UpdateResult` or `RepoStats` — don't print both).

### Step 3: Index-only verbs (seed → **Apply** → verb, then assert)
- [ ] `ExampleRepo_Groups` — seed two identical-content files, **`Apply`**, `Groups`,
      print dupe-group count.
- [ ] `ExampleRepo_Filter` — seed `*.go` files, **`Apply`**, then
      `Filter(ctx, FilterRequest{Expression: dircachefilehash.MustNewNameTest("*.go", false),
      Actions: []FilterAction{&PrintAction{}}, IndexSelectors: []string{"main"}})`.
      `// Output:` asserts the path-sorted printed paths (and/or `EntriesMatched`).

### Step 4: Ancillary surface
- [ ] `ExampleRepo_Config` — `Config().Get` then read `cfg.Hash.Default` (two-level
      deref; default config populates `Hash`), and `Config().Set(...)`.
- [ ] `ExampleRepo_Snapshots` — `Snapshots().Create` + `List`; print `len(list)`
      (snapshot IDs/timestamps are non-deterministic → count only).

### Step 5: Validation
- [ ] `go test ./pkg/...` (examples with `// Output:` execute and pass).
- [ ] `go vet ./pkg/...` (vet checks example function naming/signatures).
- [ ] `go doc ./pkg <Symbol>` spot-check that examples render under their symbols.
- [ ] Pre-commit gate (`golangci-lint --new`) clean on the new file.

## Test Coverage
**See e-testing-plan.md.** The examples *are* the test artefacts; the testing phase
defines how they're verified (executed `// Output:` examples + compile coverage + the
godoc-render spot check). No production behaviour changes, so existing suites are the
regression guard.

## Validation Criteria
**See e-testing-plan.md.**

## Scope Completion
Backlog retirement (both items) is a **rollout-phase** action via `backlog-manager
retire --task=26`, not an implementation edit — recorded here so it isn't dropped:
- retire `add-usage-examples-for-library-consumers` (done by this task)
- retire `update-api-documentation-with-current-architecture` (superseded by Task 17;
  note in the retire `--note`).

## Status
**Status**: Finished
**Next Action**: /cwf-testing-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
Plan executed verbatim — one new file `pkg/example_repo_test.go`, no supporting/
production changes (the filter-DSL godoc edit was correctly dropped here at review).
The two pre-code corrections baked into this plan (index-only verbs need a prior
`Apply`; `Filter` requires non-empty `Actions`) held in exec with no failed runs.
Full analysis in `j-retrospective.md`.

## Lessons Learned
See `j-retrospective.md` — plan review's Apply-before-verb and non-empty-Actions
catches were the highest-value part of this phase.
