# Reconcile RelativePath pathStart offset - Testing Execution
**Task**: 24 (bugfix)

## Task Reference
- **Task ID**: internal-24
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: bugfix/24-reconcile-relativepath-pathstart-offset
- **Template Version**: 2.1

## Goal
Execute the e-testing-plan.md test cases and confirm the f-exec fixes hold,
including the failing-first baselines.

## Test Results

### Functional Tests

| Test ID | Test Case | Expected | Actual | Status | Notes |
|---------|-----------|----------|--------|--------|-------|
| TC-1 | `TestEntry_PathLength_MatchesRelativePath` | RelativePath==path; calculatePathLength==len(path); validateLayout no-panic; ValidateEntry==nil | all assertions hold for "a"/"abcdefg"/"some/relative/path.go" | PASS | primary pin `calculatePathLength==len(path)` |
| TC-2 | `TestEntry_ValidateEntry_RejectsCorruptSize` | ValidateEntry errors on Size-corrupted entry | returns non-nil error | PASS | downward corruption, in-bounds |

### Baseline (failing-first) — captured pre-fix in f-exec Step 2
| Assertion | Pre-fix result | Post-fix result |
|-----------|----------------|-----------------|
| `calculatePathLength` == len(path) | FAIL: 13/19/33 vs 1/7/21 (over-count 12) | PASS |
| `validateLayout` no-panic | FAIL: panics "offset 132, expected 136" | PASS |
| `ValidateEntry`==nil (TC-1) | passes vacuously (swallowed panic) — non-discriminating | PASS (genuine) |
| TC-2 rejects corrupt Size | FAIL: accepted (no-op) | PASS (errors) |

The two discriminating baselines (calculatePathLength, validateLayout no-panic,
TC-2) all flipped red→green across the fix, proving the tests exercise the bugs.

### Non-Functional Tests
- **Memory safety / race**: `go test -race -gcflags=all=-d=checkptr=0 ./pkg/format/`
  → `ok`. Also `go test -race ./pkg/format/` with **checkptr enabled** → `ok`,
  directly confirming TC-2's downward corruption performs no OOB read.
- **Full pre-commit gate** (f-exec commit): go fmt / go fix / gopls / golangci-lint
  (**0 issues**) / govulncheck (**0 vulnerabilities**) / `go test -race` across all
  packages → all PASS.
- **Lint (gosec floor)**: `golangci-lint run ./pkg/format/...` → 0 issues after the
  G115 suppression was removed.

### Regression
`go test ./pkg/...` → `ok` (pkg, pkg/format, pkg/fsdedupe). No regressions.

## Test Failures
None (post-fix). Baseline failures were expected and are documented above.

## Coverage Report
`go tool cover -func` (pkg/format) for the changed functions:
- `calculatePathLength` — **100.0%** (single delegating return).
- `validateLayout` — 66.7% (the pass path is now reachable; the remaining
  uncovered branches are the defensive misalignment/bad-size panics, not triggered
  by valid entries).
- `ValidateEntry` — 75.0% (both the `nil` success return and the size-mismatch
  error return — the latter was dead code before this task — are exercised; the
  zero-length-path and invalid-hash-type branches remain uncovered, pre-existing).
- `RelativePath` — 80.0% (unchanged).

No coverage regression elsewhere.

## Security Review

**State**: no findings

The single-caller invariant holds: `calculatePathLength` is referenced only at its definition (entry.go:138), its one production caller `ValidateEntry` (entry.go:168), and the new test (entry_test.go:25). This confirms the load-bearing invariant in the safety analysis.

I have full context. Let me complete my security review.

## Security review — task 24 (testing-exec)

I reviewed the changeset at `/tmp/-home-matt-repo-dircachefilehash-task-24/security-review-changeset-testing-exec.out`. It comprises five CWF process documents (`a-`/`c-`/`d-`/`e-`/`f-` plans) plus the production change to `pkg/format/entry.go` and the new test file `pkg/format/entry_test.go`. The testing-exec phase adds no new production code beyond what the implementation-exec reviewer already covered; the diff carries the same two-function change to `entry.go` and the same two-test file. I verified the on-disk production source (`/home/matt/repo/dircachefilehash/pkg/format/entry.go:65-191`) matches the diff and confirmed the single-caller invariant via LSP. I walked all five FR4 threat categories.

**(a) Bash injection / unsafe command construction.** No shell-out, `system()`, backtick, or `exec` construction anywhere in the diff. The change is pure in-process Go. Not applicable.

**(b) Perl helpers consuming git/user output.** No Perl and no git-porcelain parsing in the changeset. Not applicable.

**(c) Prompt injection.** The added files are CWF plan markdown and Go source. None introduce a new untrusted-string → LLM-context flow, no `{arguments}` substitution surface, and no template interpolating task descriptions into a downstream model prompt. Not applicable.

**(d) Unsafe environment-variable handling.** No `os.Getenv` and no env var influencing any path/`chmod`/`rm`/clone operation. Not applicable.

**(e) Pattern-based risks (safe-here-but-risky-elsewhere).** This is the only category the change engages, because it touches `unsafe`-pointer code and memory-safety bounds. The change is a net memory-safety improvement (removes ~15 lines of duplicate unsafe arithmetic and a `//nolint:gosec` site, consolidating the path-offset to a single owner). Two invariant-dependent patterns are worth recording, consistent with the FR4(e) framing — neither is an actionable defect:

1. **`calculatePathLength` now panics on out-of-guard `Size` values (was tolerant).** The old body computed a clamped signed length and never panicked; the new `len(be.RelativePath())` inherits `RelativePath`'s hard panic guard (`Size < minEntrySize || Size > 65535`, entry.go:104). **Safe here because** the sole caller is `ValidateEntry`, whose `Size ∈ [minSize, 4096]` bound (entry.go:159–165) is a strict subset of that guard, and `ValidateEntry` additionally wraps its body in `recover()` (entry.go:146–153). I verified the single-caller invariant directly: LSP `findReferences` returns only the definition, the one `ValidateEntry` callsite, and the test. **Audit future uses** where `calculatePathLength` is called from a path that does not pre-bound `Size` to `[minSize, 4096]` — such a caller would turn a previously-silent length computation into a panic. The in-code comment (entry.go:135–137) records this trigger, which is the correct mitigation.

2. **Post-fix `RelativePath` reads `Size − Sizeof(Entry)` bytes past the struct start, so an inflated `Size` is an out-of-bounds read.** The corrected offset (path data at `Sizeof=144`) means a corrupt `Size` overstating the entry length makes the backward NUL scan and `unsafe.String` index past the entry's allocation. **Safe here because** production `ValidateEntry` callers operate on mmap-resident entries whose `Size` was written and validated by `EntrySerialiser`, and the negative test deliberately corrupts `Size` **downward** only (`entry_test.go:46`, `e.Size -= 8`) to stay in-bounds — the comment there documents that corrupting upward would trip `checkptr` under `-race`. This bounds dependency is intrinsic to the existing zero-copy reader and pre-exists this task; routing `calculatePathLength` through `RelativePath` surfaces it but does not widen it. **Audit future uses** that feed `RelativePath`/`ValidateEntry` an entry whose `Size` is corruption- or attacker-influenced without an independent upper bound tied to the backing buffer length — the guard is `Size ≤ 65535`, not "`Size ≤ remaining buffer", so a large-but-in-range `Size` on a short buffer is an OOB read.

The test file itself is sound from a memory-safety standpoint: it stays in-bounds (downward-only corruption, mixed mod-8 path lengths) and the plans pin it to the repo's `-race -d=checkptr=0` gate plus a checkptr-enabled run. No secrets, credentials, or sensitive data are introduced.

Conclusion: no actionable security findings. The diff reduces `unsafe` surface; the two residual `Size`-bound invariants are pre-existing, single-caller-verified (LSP-confirmed), and correctly documented in-code and in the test guards.

Relevant files:
- `/home/matt/repo/dircachefilehash/pkg/format/entry.go`
- `/home/matt/repo/dircachefilehash/pkg/format/entry_test.go`

```cwf-review
state: no findings
summary: Memory-safety-improving refactor (removes duplicate unsafe path-offset math); residual Size-bound invariants on RelativePath are pre-existing, single-caller-verified (LSP), and documented in-code — no actionable findings.
```

## Status
**Status**: Finished
**Next Action**: /cwf-retrospective
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Lessons Learned
*To be captured during retrospective*
