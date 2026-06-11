# Fault-injection tests for atomic replacement - Design
**Task**: 23 (feature)

## Task Reference
- **Task ID**: internal-23
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/23-fault-injection-atomic-replacement
- **Template Version**: 2.1

## Goal
Design the minimal test seams and test structure that let the suite force I/O
failures at the four replacement-path primitives and one per-entry hash failure,
asserting the verified Production Contract (b-requirements-plan.md) without
altering production behaviour.

## Design Priorities
Testability → Readability → Consistency → Simplicity → Reversibility

## Key Decisions

### Decision 1 — Seam mechanism: package-level function variables (not an interface)
- **Decision**: Express each seam as an unexported package-level `var` initialised
  to the real function, swapped from `_test.go` via a helper with `t.Cleanup`
  restore. No `FileOps` interface, no constructor threading, no DI container.
- **Rationale**: Only four call sites need injection, each a single direct `os.*`
  call. The `var fn = realFn` pattern is idiomatic Go (mirrors stdlib's
  `var timeNow = time.Now` testability seams) and is the smallest change that
  satisfies FR1. An interface would force signature changes through
  `NewTempIndexWriter`, `finaliseMainIndex`, and `finaliseStatusCache` for a
  test-only need — blast radius disproportionate to value ("best part is no part").
- **Trade-offs**: Package-global mutable state ⇒ seam-driven tests must not use
  `t.Parallel()` (NFR5). Accepted: Go runs a package's tests serially by default,
  and the helper restores via `t.Cleanup`, so the window is contained.
- **Reversibility**: High. Each seam is one var + one call-site edit; reverting is
  mechanical.

### Decision 2 — Two seam families, co-located in one documented file
- **Decision**: Place all seams in a new production file `pkg/io_seam.go` with a
  package doc block stating they exist solely for fault-injection tests, are inert
  in production (default to the real function / nil hook), and **are never assigned
  outside `_test.go`** — making the "no production override vector" invariant
  explicit inline (per the security review: a future env-gated production
  assignment would convert these into a genuine override vector).
  - **os-primitive seam** (FR2/FR3/FR4):
    - `var fsRename = os.Rename` — `func(string, string) error`
    - `var fsOpenFile = os.OpenFile` — `func(string, int, os.FileMode) (*os.File, error)`
    - `var fsSync = (*os.File).Sync` — method expression, `func(*os.File) error`
  - **pre-hash hook** (FR5):
    - `var hashPreReadHook func(relPath string)` — nil by default; called in
      `hashEntry` **after `entry.RelativePath()` resolves `relPath`** (so the hook
      receives a valid path) and before `HashOne`, guarded by
      `if hashPreReadHook != nil`.
- **Rationale**: Centralising the seams in one file makes the test surface
  discoverable (NFR2 "obvious and copyable") and keeps the production call sites a
  one-token change. The hash hook is a different shape (a test *action* injected at
  a pipeline stage, not an error-returning wrapper) so it is documented as a
  distinct family.
- **Trade-offs**: A production file that only serves tests. Mitigated by a clear
  doc comment; the alternative (scattering vars next to each call site) is less
  discoverable for the next maintainer.

### Decision 2a — Seam coverage of FR3 failure points (write-failure carve-out)
- **Decision**: FR3 names three open/write/sync failure points; the design seams
  `OpenFile` and `Sync` directly but **does not** add a dedicated seam for the
  `vectorio.WritevRaw` write call (`temp_index_writer.go:198,257`). The injected
  `Sync` fault is the representative "write-stage failure" — it drives the same
  error-propagation → `operationSuccessful=false` → `!ok` finalise branch that a
  raw write failure would, with identical integrity consequences.
- **Rationale**: Forcing `WritevRaw` to fail on a healthy fd requires a second seam
  family (wrapping the vectorio call at two sites) for marginal extra coverage over
  the `Sync` path that already exercises the identical downstream branch. "Best
  part is no part." Recorded as a deliberate, documented gap, not an oversight.
- **Trade-offs**: A vectorio-specific partial-write (header written, body torn) is
  not directly injected. Acceptable: the checksum-on-load guard already rejects a
  torn body, and the retained-temp loadability assertions (FR4) cover the
  structural-integrity question for the cases we *do* inject.

### Decision 3 — FR5 exploits existing non-fatal hash tolerance; no new tolerance code
- **Decision**: The delete/modify-before-hash tests rely on the *existing*
  behaviour at `hash_pool.go:87-94` — when `HashOne`/`hashSymlinkTargetToBytes`
  fails, `hashEntry` returns the error **before reaching `SetHash` (line 135)**, so
  the worker logs it and forwards the entry **unchanged**; the pipeline does not
  abort. For a newly-discovered or modified file the entry's hash field is
  therefore left empty. The `hashPreReadHook` makes the failure *deterministic* by
  mutating the target file synchronously, immediately before the worker reads it.
- **Rationale**: The requirement is to *prove* per-entry tolerance, which already
  exists. Adding tolerance logic would be inventing the thing under test. The hook
  removes timing flakiness (Risk 2 / NFR5) by injecting the mutation at a known
  stage rather than racing the scheduler.
- **Assertion (precise, per robustness review):** assert the *observable* state, not
  an inferred call — the affected entry's hash is empty (`IsHashEmpty`,
  `pkg/binary_entry.go`), never a torn/partial hash; the run exits success; the
  index loads + validates clean. The modify variant asserts the same empty/zero
  hash, confirming no corrupt value was written.
- **Trade-offs**: If a future change makes hash failure fatal, these tests
  correctly break — that is the intended teeth.

### Decision 4 — FR6 mid-scan interrupt: fresh focused test, not un-skip
- **Decision**: Write a new context-cancellation test that cancels mid-pipeline and
  asserts the live `main.idx` is byte-unchanged and no promotion occurred. Leave
  `pkg/shutdown_test.go:13` skipped and out of scope; flag it for separate
  retirement (do not delete — audit trail).
- **Rationale**: The hash worker already honours `ctx.Done()` cleanly
  (`hash_pool.go:81-82,96-99`), so a focused cancel-and-assert test is small and
  reliable. `shutdown_test.go` is skipped because it depends on the pre-v0.7
  status-callback hash infrastructure; resurrecting it risks non-trivial rework
  that breaks the "test-only, ~2 days" sizing (Risk 3). A fresh test honours the
  requirement (FR6 explicitly allows the new-test fallback) at lower risk.
- **Trade-offs**: One more skipped legacy test left in the tree. Acceptable; its
  retirement is a separate concern, not this task's integrity goal.

## System Design

### Component Overview
- **`pkg/io_seam.go`** (new, production): holds the four seam vars + doc block.
  The only production change in the task. Inert by default.
- **Production call-site edits** (4 one-token swaps):
  - `pkg/pipeline_update.go:189`: `os.Rename(...)` → `fsRename(...)`
  - `pkg/status.go:139`: `os.Rename(...)` → `fsRename(...)`
  - `pkg/temp_index_writer.go:30`: `os.OpenFile(...)` → `fsOpenFile(...)`
  - `pkg/temp_index_writer.go:205`: `tiw.file.Sync()` → `fsSync(tiw.file)`,
    **keeping the existing `"failed to sync temp index: %w"` error wrap** so FR3
    error-surfacing prose still holds.
  - `pkg/hash_pool.go` (`hashEntry`, immediately after `entry.RelativePath()` at
    line 110-113): add nil-guarded `hashPreReadHook(relPath)` call.
- **`pkg/fault_inject_test.go`** (new): the shared install/restore helpers
  (`withRenameFault`, `withOpenFault`, `withSyncFault`, `withHashPreReadHook`),
  each capturing the prior value and registering `t.Cleanup`. The three
  os-primitive helpers funnel through one unexported generic installer
  (`swapFn`) to avoid three near-identical bodies; the hook helper is separate
  (different shape).
- **`pkg/atomic_index_test.go`** (new): FR2/FR3/FR4 tests over the main and cache
  paths — one sub-test per Production-Contract row.
- **`pkg/scan_edge_cases_test.go`** (new): FR5 (delete/modify-before-hash) and FR6
  (mid-scan cancel) tests.

### Data Flow (test exercising the main rename failure, illustrative)
1. Test builds a temp repo, runs one successful `update` → stable `main.idx` exists.
2. Test records the prior `main.idx` bytes (checksum).
3. Test installs `withRenameFault(t, EIO)` — an **unconditional** swap. No path
   predicate is needed: `update` is the only operation the test runs, and its sole
   `fsRename` call is the main-index promotion (`finaliseMainIndex`). A cache-path
   test symmetrically installs the fault and runs `status` instead. Because each
   seam var wraps only the replacement-path primitive (e.g. `fsOpenFile` wraps only
   the temp-writer open at `temp_index_writer.go:30`, not other `os.OpenFile`
   sites), an unconditional fault stays scoped to the operation under test —
   removing the discriminator-on-timestamped-slug fragility a predicate would face.
4. Test runs a second `update` that would promote a new temp index.
5. `finaliseMainIndex` calls `fsRename` → EIO → logs, returns, temp **retained**.
6. Assertions: prior `main.idx` bytes equal (FR2); operation returned nil for the
   rename path (FR3 carve-out); a `<main>-<ts>` temp remains and loads clean (FR4).

   *(If a future test must run an operation that legitimately touches two seamed
   primitives, a predicate variant can be added then — YAGNI for the current scope.)*

### Pre-hash hook flow (FR5 delete-before-hash, illustrative)
1. Test creates files A, B; runs initial `update`.
2. Test modifies A's content (so it is rediscovered) and installs
   `withHashPreReadHook(t, func(p){ if p==A { os.Remove(absA) } })`.
3. Test runs `update`. Walk discovers A; hash worker calls the hook → A removed →
   `HashOne` fails → logged, entry empty-hashed, pipeline continues (existing code).
4. Assertions: `update` exits success; resulting index loads + validates clean
   (FR5). The modify variant rewrites A instead of removing it.

## Interface Design

### Seam variables (production, `pkg/io_seam.go`)
```
var fsRename       = os.Rename            // func(oldpath, newpath string) error
var fsOpenFile     = os.OpenFile          // func(name string, flag int, perm os.FileMode) (*os.File, error)
var fsSync         = (*os.File).Sync      // func(f *os.File) error
var hashPreReadHook func(relPath string)  // nil in production; test-only injection point
```

### Test helper API (`pkg/fault_inject_test.go`)
```
// Each installs an unconditional fault and registers automatic restore via t.Cleanup.
func withRenameFault(t *testing.T, err error)
func withOpenFault(t *testing.T, err error)
func withSyncFault(t *testing.T, err error)
func withHashPreReadHook(t *testing.T, hook func(relPath string))
```
- Unconditional by default (see Data Flow rationale): the test runs only the
  operation whose primitive it is faulting, so no path predicate is required.
- All helpers `t.Helper()` and forbid `t.Parallel()` by contract (NFR5). As a cheap
  teeth-guard against accidental nesting, the generic installer may assert the var
  currently holds the real function before swapping.

## Constraints
- Production success path unchanged: each swap is a single function-pointer call;
  `hashPreReadHook` adds one nil check per entry (negligible, off the hot byte path).
- No new third-party dependency; helpers use stdlib `errors`/`testing` only.
- Temp dirs only; no production `.dcfh`. Unix-only assumptions acceptable.
- British spelling; match existing `*_test.go` idiom. Reuse the canonical repo
  setup/run sequence from `basic_integration_test.go` (`NewMetaStore(dir, dir)` +
  `runUpdate(ctx, ms, …)`, lines ~26/59) and the `with…(t *testing.T)` + `t.Helper()`
  helper shape from `ssh_auth_test.go:17` (`withIsolatedHome`) — do not invent a new
  repo builder.
- **FR4 retained-temp loadability under Sync fault**: the `Sync` fault fires at
  `temp_index_writer.go:205`, *after* the header (`:198`) and entry body
  (`WritevRaw`) are written — so a retained temp is structurally complete and the
  "loads + validates clean" assertion (FR4) is sound; only the fsync syscall is
  forced to fail.
- **FR4 secondary (cleanup-failure self-heal) is DEFERRED as a recorded known gap.**
  Seaming `CleanupTimestampedCacheFiles` (a `MetaStore` method, not an `os.*` call)
  needs a different mechanism than the four `os.*` vars and adds seam surface for a
  non-integrity case (the live index is already correctly promoted before cleanup
  runs). The requirement (FR4 secondary) permits the gap; the testing plan records
  it explicitly rather than leaving it conditional.

## Decomposition Check
- [ ] Time >1 week? No. — [ ] People >2? No. — [x] Complexity: 3+ concerns (two
  seam families + scan edge tests). — [ ] Risk isolation? No. — [x] Independence:
  atomic vs scan tests separable post-seam.
2 signals (unchanged from planning); task stays whole — the two seam families share
the same file and helper idiom, so a split would duplicate scaffolding.

## Validation
- [x] Design satisfies FR1-FR6 with a seam surface of 4 vars + 5 call-site edits.
- [x] Integration points verified against current code (line refs in Component
      Overview confirmed in design phase).
- [ ] Team review — via the four-agent plan review (Step 8) below.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-plan 23
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
*To be filled upon completion*

## Lessons Learned
*To be captured during implementation*
