# Re-enable checkptr in the race gate - Testing Execution
**Task**: 10 (bugfix)

## Task Reference
- **Task ID**: internal-10
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: bugfix/10-reenable-checkptr-race-gate
- **Template Version**: 2.1

## Goal
Execute the tests defined in e-testing-plan.md and verify implementation from d-implementation-plan.md.

## Execution Checklist
- [x] Read e-testing-plan.md and d-implementation-plan.md thoroughly
- [x] Verify test environment ready (local toolchain go1.26.4, checkptr default ON)
- [x] Execute test cases sequentially
- [x] Record pass/fail for each test
- [x] Document failures with reproduction steps
- [x] Update status to "Finished" when all pass

## Test Results

### Functional Tests

| Test ID | Test Case | Expected | Actual | Status | Notes |
|---------|-----------|----------|--------|--------|-------|
| TC-1 | `go test -race ./...` with checkptr **ON** (no `-d=checkptr=0`) | all packages pass; no checkptr/heap abort | `pkg`, `pkg/format`, `pkg/fsdedupe`, `cmd/dcfh`, `cmd/dcfhfind`, `cmd/dcfhfix` all `ok` | **PASS** | Headline acceptance. Baseline previously aborted at `binary_entry.go:54` then `format/entry.go:115`. |
| TC-2 | `pkg/entry_serialiser_test.go` calls `RelativePath()` on heap-backed entry under `-race` | path equals expected; no checkptr abort | `TestEntrySerialiserScanEntry` + all 5 serialiser tests PASS under `-race` | **PASS** | Direct C2 heap-path regression — the exact path that previously crashed under checkptr. |
| TC-3 | round-trip / path tests unchanged (checkptr on and off); 8-byte discrepancy preserved | identical results pre/post | `pkg/format` `ok` under `-race` (checkptr ON) and under `-d=checkptr=0` (OFF); `TestEntrySerialiserSizeMatchesEntrySize` subtests (`a.txt`, `longer/path/to/file.txt`, `x`) PASS | **PASS** | No test newly passes/fails from unifying C2/C3 — discrepancy preserved byte-for-byte. |
| TC-4 | edited `.githooks/pre-commit` race command (`go test -race -short ./...`) | exits 0 | exit code 0 | **PASS** | Also exercised live by the real pre-commit hook during the f-phase checkpoint commit (`29667d0`). |

### Non-Functional Tests

| NFR | Test | Expected | Actual | Status |
|-----|------|----------|--------|--------|
| Performance / zero-copy | `go test -bench=BenchmarkBESkiplist/RelativePath -benchmem ./pkg/` | 0 extra allocs/op (unsafe.String still aliases) | `32.77 ns/op  0 B/op  0 allocs/op` | **PASS** |
| Static — vet | `go vet ./...` | clean; suppressed `unsafeptr` warnings gone | VET CLEAN (dead `//nolint:govet` removed) | **PASS** |
| Static — lint | `golangci-lint run ./...` | 0 issues (gosec unaffected) | 0 issues | **PASS** |
| Reliability | `Size ∈ [minEntrySize,65535]` guard at `entry.go:99-101` retained | guard in place | guard preserved, untouched | **PASS** |

### Residual gate-disable check
`grep -rn "d=checkptr=0" .` (excluding `.git/`) finds the flag only in:
- `.claude/settings.local.json:80` — a **local permission** entry, not a gate (out of scope).
- `BACKLOG.md:209,232` — descriptive prose of the backlog item being resolved (retires with task 10).
- `CHANGELOG.md:43,54` — historical Task-8 entries, accurate as-of-then (left as-is).

`.githooks/pre-commit` contains **no** `-d=checkptr=0` — only the new explanatory comment
(L102-103) and `go test -race -short ./...` (L105). The gate runs default (ON) checkptr.
**PASS** — no residual gate disable.

## Test Failures
None. All functional and non-functional test cases pass.

(For the record: during the f-phase, the first C2/C3 implementation form — holding a live
past-the-end `unsafe.Pointer` — produced `fatal error: found bad pointer in Go heap` under
`-race`. That was fixed in f-exec by switching to integer-length trimming before this
testing phase; it is not an open failure. See f-implementation-exec.md Step 2.)

## Coverage Report
No coverage-% target (refactor of existing lines, not new surface — per e-testing-plan.md).
Critical-path coverage confirmed exercised under checkptr-ON `-race`:
- **C1 `GetBinaryEntry`** — exercised via the skiplist/index read paths in `pkg` (TC-1).
- **C2 `RelativePath`** — exercised on a **heap-backed** entry by the converted serialiser
  test (TC-2), the precise `heapBacked` path where `checkptrBase` is non-zero and checkptr
  fires.
- **C3 `calculatePathLength`** — exercised via `ValidateEntry` paths in `pkg/format` (TC-3).

## Security Review

**State**: no findings

## Security Review — Task 10, Testing Phase

This is the testing-phase review. The production surface is unchanged since the implementation-phase review; the testing phase added only the `g-testing-exec.md` results doc (a process doc, correctly excluded from the changeset). I verified the production code in the working tree matches the changeset diff byte-for-byte (`pkg/binary_entry.go:52-55`, `pkg/format/entry.go:103-148`), the pre-commit hook is in its final claimed state (`.githooks/pre-commit:101-108`, no `-d=checkptr=0`), and a repo-wide grep confirms no residual `checkptr=0` lives in any shell/Makefile/CI config (only `.claude/settings.local.json`, a local permission, plus descriptive prose in BACKLOG/CHANGELOG — none of which is a gate).

### (a) Injection (command / SQL / path / template)
No new injection surface. The only command-shape change in the diff is the pre-commit hook, and it is a strict *removal* of an env-var prefix: `GOFLAGS="-gcflags=all=-d=checkptr=0" go test -race -short ./...` becomes plain `go test -race -short ./...`. No user-influenced or interpolated values are introduced. The testing phase ran the suite, benchmarks, and `golangci-lint` — read-only invocations against fixed targets. The accessor edits operate solely on already-resolved in-process memory and construct no path/SQL/shell/template strings. Clean.

### (b) Secrets / credential handling
No secrets, tokens, keys, or credentials anywhere in the diff or the testing artifacts. The hook change introduces no env-var-borne secret. Clean.

### (c) AuthZ / authN / trust boundaries
No auth logic. The one genuine trust boundary is the on-disk index file, which may be attacker-influenced (a corrupt or hostile `.idx`). The memory-safety of the unsafe accessors that read it is the live concern, handled under (e). The testing phase does not touch this boundary; it exercises the accessors on heap-backed entries, which is exactly the previously-uncovered path. No regression.

### (d) Environment-variable handling
The only env-var-related change is the *removal* of `GOFLAGS=...` from the test command. This narrows env-var influence on the test invocation rather than widening it. No new env-var reads in production code. Clean.

### (e) Memory-safety / unsafe-pointer correctness (substantive category)
I re-verified the three accessors against the working tree, since the testing-phase question is whether the production unsafe changes hold up under the now-armed checkptr gate.

- **C1 `GetBinaryEntry` (`pkg/binary_entry.go:54`)**: `unsafe.Add(unsafe.Pointer(&Data[0]), headerSize+Offset)`. `headerSize`/`Offset` are both `int`, so the dropped `uintptr`/G115 casts are correct. Address-equivalent to the prior computation, provenance preserved. Bounds on `Offset` remain the caller's responsibility exactly as before — no check added or removed. Holds up.

- **C2 `RelativePath` (`pkg/format/entry.go:113-123`)**: The underflow guard is intact and untouched — the `be.Size < uint32(minEntrySize) || be.Size > 65535` panic at line 99 still fires *before* the arithmetic, and `minEntrySize` is the fixed-portion size, so `uintptr(be.Size) - structSize` (line 118) cannot underflow. Each dereference `unsafe.Add(base, structSize+pathLen-1)` with `pathLen > 0` lands strictly in `[structSize, be.Size)`, in-bounds. The integer-length trim keeps only the in-bounds `base` pointer live (avoiding the "bad pointer in Go heap" GC trap documented in f-exec). `unsafe.String` still aliases — TC-NFR confirms 0 B/op, 0 allocs/op. Memory-safe and behaviour-preserving.

- **C3 `calculatePathLength` (`pkg/format/entry.go:138-148`)**: This is the only site with signed-int math and, unlike C2, no Size guard. `startOff = Sizeof(*be) - 8`; `n := int(be.Size) - startOff`. For a corrupt sub-path-start `be.Size`, `n` is negative, the loop condition `n > 0` is immediately false, and the function returns the negative value without any dereference. The original code returned an underflowed huge positive value in the same scenario; both are out-of-range lengths a caller must already treat as invalid, and neither dereferences. This is a sign-only behavioural nuance on the corrupt-input path, not a memory-safety regression. **Pattern-based note (per category e):** safe here because the only dereference is gated by `n > 0` — but `calculatePathLength`'s negative return would be a slice-length bug if a future caller used it as a length without a `>= 0` check. Audit future callers that trust the returned length. This matches the framing recorded in the implementation-phase review and is unchanged.

- **The 8-byte `pathStart` discrepancy** between C2 (`Sizeof(*be)`) and C3 (`&be.Path[0]` = `Sizeof(*be)-8`) is pre-existing, preserved byte-for-byte by this task, documented in-code (`entry.go:128-132`), and tracked as a Medium backlog item (`BACKLOG.md:28-34`). Not introduced here; correctly scoped out. A latent correctness concern, not a new security exposure.

- **Gate re-arming (testing-phase relevance)**: Removing `-d=checkptr=0` strengthens the safety posture — the race gate now performs full pointer-provenance checking on the heap-backed paths that previously crashed and had to be papered over. The testing phase confirms this empirically: TC-1 (full `-race` suite, checkptr ON, all packages `ok`) and TC-2 (`entry_serialiser_test.go` now calls `RelativePath()` on a heap-backed buffer — turning the previously-suppressed path into a live regression). The test conversion removes a workaround and adds real coverage; it introduces no new production surface, no test skips, and no broadened excludes. Net hardening.

- **`cwf-project.json` config (`security.review.max-lines-exclude-paths: ["implementation-guide/**"]`)**: affects review-tooling line-cap accounting only, not any production trust boundary or runtime behaviour. The glob is scoped to the process-doc directory and excludes no production path (`pkg/`, `cmd/`, hooks) from review — excluded paths are still emitted in the changeset, only discounted from the cap. Acceptable.

### Conclusion
The testing-phase changes (the `RelativePath()` test conversion, the bench/vet/lint validation, and the re-armed `go test -race -short ./...` gate command) introduce no FR4 (a)–(e) concern and add no production surface. The production unsafe-pointer changes hold up: C1/C2 bounds invariants are preserved, C3's corrupt-input path differs in sign only and never dereferences, and the gate change hardens rather than weakens the safety floor. The one carry-forward item is the documented pattern note on `calculatePathLength`'s negative return (audit future callers) — already framed as a guarded, non-blocking observation, not a new finding. No actionable security findings.

```cwf-review
state: no findings
summary: Testing-phase adds no production surface; RelativePath() test conversion + re-armed checkptr -race gate are net hardening. Production unsafe.Add changes verified against tree; C2 underflow-guarded, C3 negative-length path never dereferences. No injection/secrets/auth/env-var concerns.
```

## Status
**Status**: Finished
**Next Action**: /cwf-rollout 10
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Lessons Learned
*To be captured during retrospective*
