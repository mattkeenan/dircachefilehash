# Fault-injection tests for atomic replacement - Testing Execution
**Task**: 23 (feature)

## Task Reference
- **Task ID**: internal-23
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/23-fault-injection-atomic-replacement
- **Template Version**: 2.1

## Goal
Execute the e-testing-plan.md matrix (TC-1..TC-9 + teeth T-A/T-B/T-C) and the
non-functional gates, recording results.

## Test Results

### Functional Tests (TC-1..TC-9)
All run via `go test ./pkg/ -run 'TestAtomic_|TestScanEdge_' -v`. All PASS.

| Test ID | Test (Go name) | FR | Expected | Status |
|---------|----------------|----|----------|--------|
| TC-1 | `TestAtomic_MainRenameFault_PreservesIndex` | FR2/3/4 | rename swallowed (nil); main.idx bytes unchanged; main temp retained + loads clean | PASS |
| TC-2 | `TestAtomic_MainOpenFault_SurfacesErrorNoResidue` | FR2/3/4 | non-nil error; main.idx intact; no temp residue (never created) | PASS |
| TC-3 | `TestAtomic_MainSyncFault_SurfacesErrorNoResidue` | FR2/3/4 | non-nil error; main.idx intact; temp removed (`!ok`) | PASS |
| TC-4 | `TestAtomic_CacheRenameFault_PreservesCacheTempRetained` | FR2/3/4 | cache.idx unchanged; cache temp retained + loads clean | PASS |
| TC-5 | `TestAtomic_CacheOpenFault_SurfacesErrorNoStalePromotion` | FR2/3/4 | non-nil error; cache.idx intact; no cache temp (open failed pre-creation) | PASS |
| TC-6 | `TestAtomic_CacheSyncFault_RetainedTempLoadsClean` | FR2/3/4 | non-nil error; cache.idx intact; retained cache temp loads clean | PASS |
| TC-7 | `TestScanEdge_DeleteBeforeHash_Tolerated` | FR5 | success exit; `z.txt` entry empty-hash; main.idx loads clean | PASS |
| TC-8 | `TestScanEdge_ModifyBeforeHash_Tolerated` | FR5 | success exit; `z.txt` coherent (non-empty) hash; main.idx loads clean | PASS |
| TC-9 | `TestScanEdge_MidScanCancel_NoPartialPromotion` | FR6 | non-nil (`ctx.Err`) error; main.idx unchanged; temp removed (no partial promotion) | PASS |

### Teeth checks (AC5 — anti-vacuous; each reverted immediately, NOT committed)
| ID | Break applied | Result | Verdict |
|----|---------------|--------|---------|
| T-A | disable FR6 guard (`if false && ctx.Err()…`) in `performPipelineScan` | TC-9 FAILED on the error assertion (cancelled run silently returned nil) | guard load-bearing ✓ |
| T-B | skip `finaliseMainIndex` `!ok` `os.Remove` (early `return`) | TC-3 (sync fault) FAILED on residue assertion. **TC-2 still passed** — correctly: an open fault never creates a temp, so the `!ok` remove is not its protector. TC-3 carries the teeth for the cleanup path | cleanup load-bearing ✓ |
| T-C | stub `withRenameFault` to delegate to real `os.Rename` | TC-1 + TC-4 FAILED (index actually promoted; bytes changed / no temp retained) | seam genuinely intercepts ✓ |

All three breaks were reverted before committing; the committed tree has the
production logic intact (verified: implementation-exec commit `d456ac01` passed
its full `-race` pre-commit gate).

### Non-Functional Tests
- **Reliability/determinism (NFR5)**: `go test -race -gcflags=all=-d=checkptr=0 ./pkg/...`
  — green. New tests looped `-count=20` under `-race` — green, **no flakes**. (A data
  race in the TC-9 cancel hook was found and fixed during exec: `sync.Once` now guards
  the one-shot `cancel()` across the multi-goroutine hash pool.)
- **Performance (NFR1)**: by inspection, the production diff adds one function-pointer
  indirection per primitive, one nil-check per hashed entry, and one `ctx.Err()` check
  per update — none on the hot byte path. No benchmark needed.
- **Security (NFR4)**: seam vars assigned **only** at their `io_seam.go` initialisers
  (`rg 'fs(Rename|OpenFile|Sync)\s*=' pkg/` → initialisers only; tests mutate via
  `swapFn`'s pointer, never a direct reassignment). No env/config/flag override vector.
  `golangci-lint run ./...` → **0 issues**.
- **Usability (NFR2)**: every failure assertion names the invariant it guards (e.g.
  "prior main.idx must survive injected rename fault").

## Test Failures
None in the final tree. (During exec: the FR4 "loads clean" oracle initially used
`ValidateIndexHeader`, which fails even on a normally-promoted index; switched to the
production loader `loadIndexFromFileWithTracking`. See f-implementation-exec.md D1.)

## Coverage Report
No numeric line target (per e-testing-plan: behavioural fault coverage of a
previously-uncovered path). Every Production-Contract row is asserted on both main
and cache paths; FR5 delete+modify and FR6 cancel are covered; teeth confirm none
pass vacuously.

## Validation Criteria (from e-testing-plan.md)
- [x] TC-1…TC-9 all pass.
- [x] Teeth T-A/T-B/T-C each fail on the deliberate break, pass after restore.
- [x] `go test ./pkg/...` and `go test -race -gcflags=all=-d=checkptr=0 ./pkg/...` green.
- [x] `golangci-lint run ./...` clean (gosec over `io_seam.go`).
- [x] AC1-AC5 satisfied; production diff = 5 seam swaps + `io_seam.go` + FR6 guard.

## Status
**Status**: Finished
**Next Action**: /cwf-rollout 23
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Lessons Learned
Run new concurrency tests under `-count=20 -race` before declaring them stable:
a single run passed TC-9, but the race detector flagged the multi-goroutine
cancel-hook write (fixed with `sync.Once`). Teeth checks must be read carefully —
TC-2 correctly stays green under the T-B break because an open fault never creates
a temp, so TC-3 is the test that actually guards the cleanup path. Pin
assertions about an existing subsystem's output (checksums, hashes) to what the
code actually produces, not the intuitive value (D1/D2). Full reflection in
j-retrospective.md.

## Security Review

**State**: no findings

I now have the full changeset. This is the testing-exec changeset for task 23. The production code edits are identical to what the implementation-exec review already covered (the seam swaps + FR6 guard). This testing-exec changeset adds the test files: `atomic_index_test.go`, `fault_inject_test.go`, `scan_edge_cases_test.go`, plus all the process markdown.

Let me reason through the threat categories.

**Production code surface** — identical to the prior implementation-exec review:
- `pkg/io_seam.go`: four unexported package-level seam vars defaulting to real `os` functions / nil.
- `pkg/pipeline_update.go`: `os.Rename`→`fsRename` plus the FR6 `ctx.Err()` guard.
- `pkg/status.go`: `os.Rename`→`fsRename`.
- `pkg/temp_index_writer.go`: `os.OpenFile`→`fsOpenFile`, `Sync()`→`fsSync`.
- `pkg/hash_pool.go`: nil-guarded `hashPreReadHook(relPath)`.

**New test surface** — the focus of this review:
- `pkg/fault_inject_test.go`: `swapFn[T]` generic + four `with*` installers + `errInjected`.
- `pkg/atomic_index_test.go`: TC-1..TC-6 plus scaffolding helpers.
- `pkg/scan_edge_cases_test.go`: TC-7..TC-9.

Reasoning through each category:

**(a) Bash injection / unsafe command construction** — No shell invocation, no `system`/`exec`/backtick construction anywhere in the diff. The test files call `os.Remove`, `os.WriteFile`, `os.ReadFile`, `filepath.Glob`, `filepath.Join` on `t.TempDir()`-derived paths. No string concatenation reaches a shell. Not applicable.

**(b) Perl helpers consuming git/user output** — No Perl in the changeset; all additions are Go. Not applicable.

**(c) Prompt injection via user-supplied strings** — No `{arguments}` substitution is introduced or altered. The process markdown is informational. `hashPreReadHook` receives `relPath` (a repo-relative path) and feeds only test-installed Go closures, never LLM context. Not applicable.

**(d) Unsafe environment-variable handling** — No `os.Getenv`/`ENV` reads added. The seam vars are unexported package-internal vars with no external input wired to them. I verified the only assignments are the `io_seam.go` initialisers (to real `os` funcs) and the `swapFn`-driven test swaps in `_test.go`, all `t.Cleanup`-restored. No env/config/flag override vector. The test paths are all `t.TempDir()`-derived, never user/production data. No concern.

**(e) Pattern-based risks** — Two patterns worth framing, both already correctly handled in the diff:

1. The seam-var indirection (carried over from implementation-exec): safe here because the four function-pointer seams are unexported, package-internal, default to the real `os` primitives, and have no production writer — only `_test.go` reassigns them via `swapFn` with `t.Cleanup` restore. The `io_seam.go` doc comment encodes the INVARIANT inline ("never assigned outside `_test.go` — a production assignment would turn these into a runtime index-write override vector"), which is exactly category (e)'s "do instead." Audit framing for future: audit any future change that assigns these vars (or `hashPreReadHook`) from non-test code, or exposes them via env/config/flag — that would convert a test seam into a path to force index-write failures or inject a pre-read side effect.

2. New to this testing-exec changeset: the `_ = os.Remove(zAbs)` / `_ = os.WriteFile(zAbs, ...)` mutations inside `hashPreReadHook` closures, and `readBytes`'s `os.ReadFile` with `//nolint:gosec // G304`. These operate on `filepath.Join(ms.RootDir, ...)` where `ms.RootDir` is a `t.TempDir()`. Safe here because the path is test-controlled and the hook only fires when the seam is installed by a test; the hook is nil in production. The G304/G306 nolint rationales correctly cite the test-temp invariant. Audit future uses where a `hashPreReadHook` closure or these helpers might be reused against a non-temp, partly-user-controlled path — but that cannot happen in production because the hook is nil there.

The G302 `//nolint:gosec` rationale at `temp_index_writer.go:30` was preserved verbatim across the `fsOpenFile` rename, so the static-analysis floor is unaffected. The new test-only `//nolint:gosec // G304` and `// G306` suppressions are scoped to `_test.go` and carry test-temp rationales, consistent with the repo's gosec posture (test files are excluded via `exclusions.rules`, and these add explicit per-line rationale on top).

The FR6 cancellation guard strengthens the atomic-replacement integrity invariant (prevents promoting a partial index on mid-scan cancel) and introduces no new input surface. The `sync.Once`-guarded cancel in TC-9 is a correct fix for the multi-goroutine hook race flagged under `-race`; it is test-only and has no security implication.

No actionable security findings. The only notes are the category-(e) pattern framings above, which the code already documents inline.

```cwf-review
state: no findings
summary: Testing-exec adds Go test files (fault-injection seam helpers + TC-1..TC-9) over t.TempDir() repos; no shell/Perl/env/prompt surface. Production edits are the same inert os-primitive seam swaps + FR6 cancel guard already reviewed. Pattern note (e) only: seam vars and test hooks are safe because package-internal/test-only and nil in production — audit any future non-test assignment or reuse against non-temp paths; G302/G304/G306 nolint rationales correctly preserved/scoped.
```
