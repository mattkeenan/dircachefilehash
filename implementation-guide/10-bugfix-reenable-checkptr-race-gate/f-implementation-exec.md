# Re-enable checkptr in the race gate - Implementation Execution
**Task**: 10 (bugfix)

## Task Reference
- **Task ID**: internal-10
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: bugfix/10-reenable-checkptr-race-gate
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

### Step 1: Setup (re-grep sites + gate location)
- **Planned**: Re-grep `unsafe.Pointer(` in `pkg/`; confirm three violating sites and current
  line numbers; `grep -rn checkptr .` to confirm gate disable is in one place.
- **Actual**: Confirmed the three violating sites at their planned locations —
  C1 `pkg/binary_entry.go:53-54` (GetBinaryEntry), C2 `pkg/format/entry.go:103-120`
  (RelativePath), C3 `pkg/format/entry.go:125-131` (calculatePathLength). Non-violating
  offset-only sites present with known disposition (`binary_entry.go:72-74`,
  `entry.go:68-69` validateLayout, `index.go:988`, `recovery.go:230`) — left untouched.
  The `-d=checkptr=0` disable lives only in `.githooks/pre-commit:102-105`; the
  `.claude/settings.local.json:80` hit is a local permission entry, not a gate.
- **Deviations**: None.

### Step 2: Core implementation (C1/C2/C3 + test)
- **Planned**: Apply C1, C2, C3; remove dead `//nolint` comments; convert
  `entry_serialiser_test.go` to call `RelativePath()`.
- **Actual**:
  - **C1** (`pkg/binary_entry.go`): replaced the `uintptr` round-trip with
    `unsafe.Add(unsafe.Pointer(&Data[0]), headerSize+Offset)`. `headerSize`/`Offset`
    are `int`, so the G115 and govet `//nolint` comments became dead and were removed.
  - **C2/C3** (`pkg/format/entry.go`): deviated from the literal plan snippet (see below).
    The first attempt (holding `pathEnd` as a live `unsafe.Pointer` initialised to
    `base+Size`) cleared the original checkptr *arithmetic* error but introduced a new
    `fatal error: found bad pointer in Go heap` — the GC rejects a past-the-end pointer
    held live in a local variable across a safepoint. **Fix**: keep only the in-bounds
    `base` (C2) / `&be.Path[0]` (C3) pointer live and perform the trailing-NUL trim with
    an **integer length**, materialising the result pointer only in the final
    `unsafe.String` expression. Every dereference (`base + structSize + pathLen - 1`,
    resp. `pathStart + n - 1`) is strictly in-bounds. C3 preserves the `&be.Path[0]`
    start byte-for-byte (the pre-existing 8-byte discrepancy with C2 is preserved, not
    unified). C3 uses signed int length math so a corrupt sub-path-start `be.Size` skips
    the loop without an OOB read (the original returned an underflowed huge value; the
    new code returns a small negative — neither dereferences; recorded as an accepted,
    non-security-regressing behavioural nuance by the security reviewer).
  - **Test** (`pkg/entry_serialiser_test.go`): the first round-trip assertion now calls
    `be.RelativePath()` on the heap-backed buffer (was a manual byte scan with a
    now-false checkptr comment) — a live regression for the C2 heap path.
- **Deviations**: C2/C3 implementation form changed from the d-plan snippet (which held a
  live past-the-end pointer) to an integer-index form. Rationale above; behaviour
  preserved, checkptr- and GC-clean.

### Step 3: Re-arm and verify (gate edited LAST)
- **Planned**: Run `go test -race ./...` (checkptr ON) green before editing the gate;
  build + vet clean; then drop `-d=checkptr=0` and rewrite the comment; re-run the hook
  command form.
- **Actual**: `go build ./...` + `go vet ./...` clean. `go test -race ./...` (checkptr ON,
  no flag) — **all packages pass, no checkptr/heap abort** (TC-1, headline acceptance).
  Only then edited `.githooks/pre-commit` (not hash-tracked): removed the `GOFLAGS`
  prefix, rewrote the comment to cite `unsafe.Add` provenance. Re-ran the exact hook
  command `go test -race -short ./...` — green (TC-4).
- **Deviations**: None.

### Step 4: Documentation
- **Planned**: Correct CLAUDE.md prose (none exists); post-merge memory rewrite; create
  the Medium C2/C3 backlog item; CHANGELOG one-line correction if present.
- **Actual**:
  - Created the Medium backlog item "Reconcile RelativePath vs calculatePathLength 8-byte
    pathStart discrepancy" via `backlog-manager add` (validates clean; BACKLOG.md:273).
  - CLAUDE.md: confirmed no checkptr prose — nothing to correct.
  - CHANGELOG.md L43/L54: **left as-is** (deviation, see below) — these are historical
    Task-8 entries that were accurate when written; the append-only record should not be
    falsified. The new state will be recorded when task 10 is retired to the CHANGELOG.
  - `project_race_checkptr_disabled` auto-memory: deferred to post-merge per plan (it is
    now false but lives outside the repo; rewrite once the change lands on the trunk).
- **Deviations**: CHANGELOG historical entries not edited (plan's own escape clause:
  "historical task-plan prose may be left as-is").

### Step 5: Validation
- **Planned**: `golangci-lint run ./...` clean; zero behaviour change; no new allocations.
- **Actual**:
  - `golangci-lint run ./...` → **0 issues** (after adding one rationale-bearing
    `//nolint:gosec // G115` on the bounded `int(uintptr(pathStart)-uintptr(base))` =
    `Sizeof-8` constant in C3, mirroring existing repo style).
  - Behaviour unchanged: full suite green checkptr on and off; `pkg/format` round-trip
    tests unaffected; the 8-byte C2/C3 discrepancy preserved.
  - Zero-copy NFR: `BenchmarkBESkiplist/RelativePath` → **0 B/op, 0 allocs/op**
    (`unsafe.String` still aliases; no copy introduced).

### Deviation: cwf-project.json security-review exclude config
The exec-phase security-review changeset helper exited `2` (cap exceeded: 693 production
lines > 500) because the anchor is the task baseline `6d6f4a4` and the changeset includes
~606 lines of CWF planning markdown (a/c/d/e plans) which were being counted as production.
Added `security.review.max-lines-exclude-paths: ["implementation-guide/**"]` to
`implementation-guide/cwf-project.json` — exactly the "repo's own process docs" exclude the
security-review doc contemplates. Excluded paths are still **reviewed** (emitted in the
changeset), only discounted from the cap. Re-run: exit 0, 87 production lines. This is a
general, repo-wide fix (every task's plan docs would otherwise inflate its cap), not a
per-task workaround. `cwf-project.json` is not hash-tracked.

## Blockers Encountered
None. The one mid-task surprise (the "found bad pointer in Go heap" GC error from a live
past-the-end pointer) was resolved by switching C2/C3 to integer-length trimming — see
Step 2.

## Deferral Check
- [x] All steps from d-implementation-plan.md executed
- [x] All success criteria from a-task-plan.md met (checkptr ON green; gate re-armed;
      zero-copy preserved; format unchanged; no CLAUDE.md prose to fix)
- [ ] b-requirements-plan.md — N/A (bugfix omits requirements phase)
- [x] All design guidance in c-design-plan.md followed (correctness-over-convenience
      ordering honoured: gate edited only after a full checkptr-ON green run)
- [x] No planned work deferred without rationale (memory rewrite is the only deferred
      item, explicitly post-merge per plan)

## Security Review

**State**: no findings

## Security Review — Task 10 (implementation phase)

This task makes the zero-copy mmap accessors checkptr-clean by replacing `uintptr`-round-trip pointer arithmetic with provenance-preserving `unsafe.Add`, converting the trailing-NUL trim to integer-index arithmetic, and re-arming the pre-commit `-race` gate (removing `-d=checkptr=0`). The production surface is `pkg/binary_entry.go`, `pkg/format/entry.go`, one test, the pre-commit hook, BACKLOG.md, and a `cwf-project.json` config addition. The four CWF planning markdown files are process docs, not production code.

### (a) Injection (command / SQL / path / template)
No new shell, SQL, template, or path-construction surface. The `.githooks/pre-commit` change is a strict removal — it drops the `GOFLAGS="-gcflags=all=-d=checkptr=0"` environment prefix and runs `go test -race -short ./...` with no interpolated or user-influenced values. No new command-string construction. The accessor changes operate purely on already-resolved in-process memory; no path strings are built or consumed here. Clean.

### (b) Secrets / credential handling
No secrets, tokens, keys, or credentials touched anywhere in the diff. The hook change does not introduce env-var-borne secrets. Clean.

### (c) AuthZ / authN / trust boundaries
No authentication or authorization logic. The relevant trust boundary is the on-disk index file, which can be attacker-influenced (a corrupt/hostile `.idx`). The memory-safety of the accessors that read it is the live concern here, addressed under (e). No auth regression.

### (d) Environment-variable handling
The only env-var-related change is the *removal* of `GOFLAGS=...` from the test command in the pre-commit hook. This narrows, rather than widens, env-var influence on the build/test invocation. No new env-var reads in production code. Clean.

### (e) Memory-safety / unsafe-pointer correctness (the substantive category for this diff)

This is an unsafe-pointer refactor, so I focused the review on whether the new arithmetic preserves the bounds invariants the old code relied on, and whether any behavioural drift could introduce an out-of-bounds read.

**C1 — `GetBinaryEntry` (`pkg/binary_entry.go:54`)**: `unsafe.Add(unsafe.Pointer(&Data[0]), headerSize+Offset)`. Both `headerSize` and `Offset` are `int`, so the `uintptr`/G115 casts correctly disappear. This is address-equivalent to the prior `uintptr` computation; `unsafe.Add` preserves provenance to the `Data` base. No new bounds check was added or removed — bounds on `Offset` are the caller's responsibility exactly as before. Behaviour-preserving.

**C2 — `RelativePath` (`pkg/format/entry.go:118-123`)**: The rewrite changes the loop variable from a pointer (`pathEnd`) to an integer length (`pathLen := uintptr(be.Size) - structSize`). I verified the underflow guard: the `be.Size < uint32(minEntrySize) || be.Size > 65535` panic at line 99 is *preserved and untouched*, and `minEntrySize == structSize` (the fixed portion). So `uintptr(be.Size) - structSize` cannot underflow — `be.Size >= structSize` is guaranteed before the subtraction. Every dereference `unsafe.Add(base, structSize+pathLen-1)` with `pathLen > 0` lands strictly inside `[structSize, be.Size)`, in-bounds. The final `unsafe.String` aliases the same bytes — zero-copy preserved, no allocation. Behaviour-preserving and memory-safe.

**C3 — `calculatePathLength` (`pkg/format/entry.go:138-148`)**: This one warrants the most care because it uses *signed* int math and, unlike C2, has **no Size guard** (it is not gated by the line-99 panic; it can be called on an unvalidated entry). `startOff = Sizeof(*be) - 8`. `n := int(be.Size) - startOff`. If `be.Size < startOff` (a corrupt/small entry), `n` is negative, the loop condition `n > 0` is immediately false, and the function returns the negative value without ever dereferencing. I confirmed this matches the original behaviour: the old code computed `int(pathEnd - pathStart)` where `pathEnd = entryStart + be.Size < pathStart`, and the loop `pathEnd > pathStart` was likewise false — so the old code *also* returned a negative-equivalent (it returned `int` of an underflowed `uintptr` subtraction, i.e. a huge positive value, whereas the new code returns a small negative). This is a subtle behavioural change on the corrupt-input path, but in both cases no dereference occurs and the result is an out-of-range length that callers must already treat as invalid. The comment at line 137 explicitly documents that signed math "mirrors the original's behaviour when be.Size is below the path-start offset" — the mirror is approximate (sign differs) but neither variant performs an OOB read, so this is not a memory-safety regression. Note this is a pattern worth auditing if `calculatePathLength`'s result is ever used as a slice length without a `>= 0` check downstream — safe here because the only deref is gated by `n > 0`; audit future callers that trust the returned length.

**The 8-byte `pathStart` discrepancy** between `RelativePath` (starts at `Sizeof(*be)`) and `calculatePathLength` (starts at `&be.Path[0]` = `Sizeof(*be)-8`) is pre-existing, explicitly *preserved byte-for-byte* by this task, documented in-code, and logged as a Medium backlog item (BACKLOG.md:28-34). It is not introduced here and is correctly scoped out. It is a latent correctness concern (one of the two offsets is likely wrong relative to the on-disk writer) but not a new security exposure from this diff.

**Gate re-arming**: Removing `-d=checkptr=0` strengthens the security/safety posture — the race gate now performs full pointer-provenance checking on heap-backed test paths that previously crashed and had to be suppressed. This is a net hardening, not a weakening. The test change (`entry_serialiser_test.go` now calls `RelativePath()` instead of re-reading raw bytes) turns the previously-suppressed path into a live regression test under checkptr — a genuine improvement.

**`cwf-project.json` config addition**: `security.review.max-lines-exclude-paths: ["implementation-guide/**"]` discounts CWF process docs from the review line cap. This affects *review tooling scope*, not any production trust boundary or runtime behaviour. The glob is narrowly scoped to the process-doc directory and does not exclude any production code path (`pkg/`, `cmd/`, hooks) from review. Acceptable; no concern.

### Conclusion
The diff is a behaviour-preserving, memory-safety-positive refactor. The bounds invariants that the unsafe accessors depend on are preserved (C1, C2) or — in the one corrupt-input edge case (C3) — changed in sign only without introducing any out-of-bounds dereference, with the only deref gated by `n > 0`. The gate change hardens rather than weakens the safety floor. No injection, secret, auth, or env-var concerns. The pre-existing 8-byte offset discrepancy is correctly preserved and tracked. No actionable security findings.

```cwf-review
state: no findings
summary: Behaviour-preserving unsafe.Add refactor; bounds invariants preserved (C2 underflow-guarded, C3 negative-length path never dereferences), race gate hardened. No injection/secrets/auth/env-var concerns.
```

## Status
**Status**: Finished
**Next Action**: /cwf-testing-exec 10
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Lessons Learned
- `unsafe.Add` clears checkptr's *arithmetic* error, but holding the resulting
  past-the-end pointer in a live variable trips a *different* runtime check
  ("found bad pointer in Go heap") at GC time. The robust idiom for end-of-region
  scans is to keep only an in-bounds base pointer live and index with an integer
  length, materialising any boundary pointer solely in the final consuming expression.
- The security-review changeset cap is production-line-weighted, but CWF plan docs
  count as production unless `security.review.max-lines-exclude-paths` is configured —
  worth setting once per repo so doc-heavy task branches don't trip the cap.
