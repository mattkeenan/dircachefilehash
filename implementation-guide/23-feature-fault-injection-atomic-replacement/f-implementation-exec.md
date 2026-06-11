# Fault-injection tests for atomic replacement - Implementation Execution
**Task**: 23 (feature)

## Task Reference
- **Task ID**: internal-23
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/23-fault-injection-atomic-replacement
- **Template Version**: 2.1

## Goal
Execute the implementation following d-implementation-plan.md and e-testing-plan.md:
the four-var/one-hook test seam, the FR6 cancelled-context bugfix, and the FR2-FR6
fault-injection + scan-edge tests, with production success-path behaviour unchanged.

## Actual Results

### Step 1: Production seam (behaviour-neutral) + FR6 bugfix
- **Planned**: Add `pkg/io_seam.go` (4 vars), swap 5 call sites, apply the FR6
  cancelled-context guard in `performPipelineScan`.
- **Actual**: Done as planned.
  - `pkg/io_seam.go` — `fsRename`/`fsOpenFile`/`fsSync` + nil `hashPreReadHook`.
  - `pipeline_update.go:189` `os.Rename`→`fsRename`; `status.go:139` `os.Rename`→`fsRename`;
    `temp_index_writer.go:30` `os.OpenFile`→`fsOpenFile` (nolint G302 kept verbatim);
    `temp_index_writer.go:205` `tiw.file.Sync()`→`fsSync(tiw.file)` (wrap kept).
  - `hash_pool.go` `hashEntry`: nil-guarded `hashPreReadHook(relPath)` right after
    `relPath` resolves, before the symlink/hash branch.
  - FR6 guard added in `performPipelineScan` after `RunUpdatePipeline`: `if ctx.Err()
    != nil { return ctx.Err() }` so a cancelled run keeps `operationSuccessful=false`
    and the deferred `finaliseMainIndex` takes the `!ok` (temp-removed) branch.
- **Result**: `go build ./...` clean; full `./pkg/...` suite green (seam inert).
- **Deviations**: None.

### Step 2: Shared test helpers
- **Planned**: `fault_inject_test.go` with `swapFn` + four `with*` installers + `errInjected`.
- **Actual**: Done as planned. `swapFn[T]` swaps a target var via pointer and restores
  on `t.Cleanup`. The four installers drive `fsRename`/`fsOpenFile`/`fsSync`/`hashPreReadHook`.
- **Deviations**: None.

### Step 3: Atomic-replacement faults (FR2/FR3/FR4) — `atomic_index_test.go`
- **Planned**: TC-1..TC-6 over main + cache paths per the Production Contract.
- **Actual**: All six implemented and passing.
  - Topology confirmed during exec: the no-paths `runUpdate`→`updateFullRepository`
    creates **only** the main temp writer (no cache refresh on the full-update path),
    and `runStatus`→`Diff(RefTypeFsScan)`→`refreshFsScanCache` creates **only** the
    cache temp writer. So an injected fault is cleanly isolated to one path.
  - `fsOpenFile` is referenced **only** by `NewTempIndexWriter`; mmap reads use
    `os.Open`, so the open fault never disturbs the read path.
- **Deviations**: **"loads clean" oracle changed** — see Deviation D1 below.

### Step 4: Scan edge cases (FR5/FR6) — `scan_edge_cases_test.go`
- **Planned**: TC-7 delete-before-hash, TC-8 modify-before-hash, TC-9 mid-scan cancel.
- **Actual**: All three implemented and passing.
  - TC-7: new file `z.txt`, hook `os.Remove`s it pre-hash → read fails → entry kept
    with empty hash (non-fatal tolerance, `hash_pool.go:87-94`); index loads clean.
  - TC-8: hook rewrites `z.txt` pre-hash → re-read succeeds → entry carries a coherent
    **non-empty** hash. See Deviation D2.
  - TC-9: 5 new files; hook cancels ctx on first hash; asserts `update` returns a
    non-nil (`ctx.Err()`-derived) error, `main.idx` byte-unchanged, temp removed.
- **Deviations**: D2 (TC-8 assertion), D3 (TC-9 hook concurrency).

### Step 5: Teeth + gate
- **Teeth checks** (each reverted immediately, not committed):
  - **T-A** (disable FR6 guard `if false && ctx.Err()...`): TC-9 FAILED on the
    error assertion → guard is load-bearing. Restored.
  - **T-B** (skip `finaliseMainIndex` `!ok` remove): TC-3 (sync fault) FAILED on the
    residue assertion. **TC-2 (open fault) still passed** — correctly, because an open
    fault means the temp was never created, so the `!ok` remove is not what protects
    it. TC-3 is the test with teeth for the cleanup path. Restored.
  - **T-C** (stub `withRenameFault` to call real `os.Rename`): TC-1 and TC-4 FAILED
    (index actually promoted) → the seam genuinely intercepts. Restored.
- **Gate**:
  - `go test ./pkg/...` — green.
  - `go test -race -gcflags=all=-d=checkptr=0 ./pkg/...` — green (after fixing D3).
  - `golangci-lint run ./...` — **0 issues** (gosec over `io_seam.go`).
  - NFR5 flake check: new tests `-count=20` under `-race` — green, no flakes.
  - NFR4: seam vars are assigned **only** at their `io_seam.go` initialisers (grep
    `fs(Rename|OpenFile|Sync)\s*=`); tests mutate via `swapFn`'s pointer, never a
    direct reassignment — no env/config/flag override vector.

## Deviations from Plan

- **D1 — "loads clean" uses the production loader, not `ValidateIndexHeader`.**
  The plan's FR4 wording ("retained temp loads + validates clean") was first coded
  with `ValidateIndexHeader(path, false, 0)`. That helper's `validateHeaderChecksum`
  does **not** match what `TempIndexWriter` writes — a *normally promoted* `main.idx`
  also fails it (verified empirically). The main/cache load path
  (`loadIndexFromFileWithTracking`) is the real "loads clean" oracle: signature +
  version + clean-flag `verifyHeaderChecksum` + per-entry structural validation.
  `assertLoadsClean` now loads via that path and `release()`s. No production change;
  this only corrects the test oracle. (`ValidateIndexHeader`'s divergence is a
  pre-existing repair-tool quirk, out of scope here — noted for the backlog.)

- **D2 — TC-8 asserts a coherent (non-empty) hash, not "hash empty".**
  e-testing-plan TC-8 said the modified entry's hash should be empty. That only holds
  when the *read* fails (the delete case). A content rewrite before hashing is
  re-read successfully, so the entry legitimately carries a coherent hash of the bytes
  read. FR5's own acceptance bullet is "success exit + index loads/validates clean" —
  which TC-8 asserts — plus we assert the entry is present and non-corrupt. This is
  truer to real behaviour than contriving an unreadable file. FR5 fully satisfied.

- **D3 — TC-9 cancel hook guarded with `sync.Once`.**
  The pre-hash hook runs on multiple hash-worker goroutines; the first draft used a
  plain `bool` flag, which `-race` flagged as a data race (and cascaded a spurious
  failure into the wire test). Replaced with `sync.Once` around `cancel()`. The
  determinism argument is unchanged — cancel still fires on the first hashed entry.

## Blockers Encountered
None. (D1 cost the most investigation — resolved by confirming the production loader
skips `validateHeaderChecksum` and uses `verifyHeaderChecksum` instead.)

## Deferral Check
- [x] All steps from d-implementation-plan.md executed.
- [x] All success criteria from a-task-plan.md met (seam, faults, scan edges, concrete
      cancel assertion, suite green incl. `-race`, seam-bypass teeth verified).
- [x] All requirements from b-requirements-plan.md addressed (FR1-FR6, NFR1-NFR5).
- [x] Design guidance in c-design-plan.md followed (package vars, write-failure
      carve-out via Sync, FR5 via existing tolerance, FR6 fresh test).
- [x] No planned work deferred beyond the one pre-approved gap.
- **Pre-approved known gaps** (design Decision / Constraints, not silently dropped):
  FR4-secondary (cleanup-failure self-heal) and vectorio write-failure (represented
  by the Sync fault).

## Status
**Status**: Finished
**Next Action**: /cwf-testing-exec 23
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Lessons Learned
- The repo has **two** checksum validators with different semantics: production
  load (`verifyHeaderChecksum`, matches the writer) vs the repair/dcfhfind path
  (`validateHeaderChecksum`, does not). Tests asserting "valid index" must use the
  production loader. Worth a backlog note to reconcile the two.
- Test hooks that fire inside the hash-worker pool are inherently multi-goroutine —
  default to `sync.Once`/atomics, never a plain flag, or `-race` will (correctly) bite.

## Security Review

**State**: no findings

## Security review — task 23 (fault-injection seam + FR6 cancellation guard)

I reviewed the changeset at `/tmp/-home-matt-repo-dircachefilehash-task-23/security-review-changeset-implementation-exec.out`. The bulk is CWF process markdown under `implementation-guide/23-…/` (plan/requirements/design/impl/testing docs — informational, not executable). The executable surface is five Go production edits:

- `pkg/io_seam.go` (new): four package-level seam vars (`fsRename`, `fsOpenFile`, `fsSync`, `hashPreReadHook`).
- `pkg/pipeline_update.go:189`: `os.Rename` → `fsRename`; plus a new `if ctx.Err() != nil { return ctx.Err() }` guard in `performPipelineScan` (the FR6 cancellation fix).
- `pkg/status.go:139`: `os.Rename` → `fsRename`.
- `pkg/temp_index_writer.go:30,205`: `os.OpenFile` → `fsOpenFile`, `tiw.file.Sync()` → `fsSync(tiw.file)`.
- `pkg/hash_pool.go:115-117`: nil-guarded `hashPreReadHook(relPath)` call.

I reasoned through each threat category:

**(a) Bash injection / unsafe command construction** — No shell invocation, no `system`/`exec`/backtick construction anywhere in the diff. The Go edits call typed function variables, not shells. Not applicable.

**(b) Perl helpers consuming git/user output** — No Perl in the changeset; all production edits are Go. Not applicable.

**(c) Prompt injection via user-supplied strings** — The process docs contain task-description prose but no `{arguments}` substitution mechanism is introduced or altered. `hashPreReadHook` receives `relPath` (a repo-relative file path), which flows only into a test-installed Go closure, never into LLM context. No new untrusted-string-to-LLM path. Not applicable.

**(d) Unsafe environment-variable handling** — No `os.Getenv`/`ENV` reads are added. Critically, I verified the seam vars are **not** settable from any external input: they default to the real `os` functions in `io_seam.go`, and a grep for reassignments outside the initialisers found exactly zero in production code (the `_test.go` reassignment grep also returned empty, since the fault-installing tests are not part of this implementation-exec changeset). So the documented "test-only, no production override vector" invariant holds in the shipped code. No env-var concern.

**(e) Pattern-based risks (safe-here-but-risky-elsewhere)** — The seam-var-indirection pattern is the one worth framing. The four function-pointer seams are safe here because (i) they are unexported, package-internal, (ii) they are initialised to the real `os` primitives, and (iii) nothing in the production tree reassigns them — only `_test.go` does, via `t.Cleanup`-restored swaps. The `io_seam.go` doc comment encodes this as an explicit INVARIANT ("never assigned outside `_test.go` — a production assignment would turn these into a runtime index-write override vector"), which is exactly the right inline documentation per category (e)'s "do instead." Framing for future audits: **safe here because the seams are inert package-internal vars with no production writer and no external input wired to them; audit any future change that assigns these vars (or `hashPreReadHook`) from non-test code, or that exposes them via env/config/flag — that would convert a test seam into a path for an attacker to force index-write failures or inject a pre-read side effect.** This is a pattern note, not an actionable defect in the current diff.

On the FR6 cancellation guard: the `if ctx.Err() != nil { return ctx.Err() }` addition is a correctness/integrity hardening (prevents promoting a partial index on mid-scan cancel). It strengthens the atomic-replacement invariant rather than weakening any security boundary, and introduces no new input surface.

The G302 `//nolint:gosec` rationale comment at `temp_index_writer.go:30` was correctly preserved across the `fsOpenFile` rename, so the static-analysis floor is unaffected. The wire-handler write site (`wire_handler.go:445`, the one untrusted-reachable SSH path) was deliberately left unseamed per the design, keeping the seam off the untrusted surface — a sound scoping decision.

No actionable security findings. The only note is the category-(e) pattern framing above, which the code already documents inline.

```cwf-review
state: no findings
summary: Production diff is four os-primitive seam swaps (inert package-internal vars, no production writer or external input) plus an FR6 cancellation guard that strengthens index-write atomicity; G302 nolint rationale preserved. Pattern note (e) only: audit any future non-test assignment of the seam vars.
```
