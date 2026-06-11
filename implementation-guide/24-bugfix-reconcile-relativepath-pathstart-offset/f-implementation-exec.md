# Reconcile RelativePath pathStart offset - Implementation Execution
**Task**: 24 (bugfix)

## Task Reference
- **Task ID**: internal-24
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: bugfix/24-reconcile-relativepath-pathstart-offset
- **Template Version**: 2.1

## Goal
Execute the fixes from d-implementation-plan.md: delegate `calculatePathLength` to
`RelativePath`, correct `validateLayout`'s Path-offset assertion, and land the
regression tests from e-testing-plan.md.

## Actual Results

### Step 1: Confirm invariants
- **Planned**: confirm single caller, `unsafe` still used, no test-file collision,
  note the benign `index.go:988` site.
- **Actual**: `calculatePathLength` has exactly one caller — `ValidateEntry`
  (`pkg/format/entry.go:177`). `unsafe.` appears 18× in entry.go (stays imported).
  No `pkg/format/entry_test.go` existed. `index.go:988` reviewed — prints the true
  offset, asserts nothing; out of scope.
- **Deviations**: none.

### Step 2: Write failing tests first (baseline)
- **Planned**: add `entry_test.go` (positive + negative), run pre-fix, record reds.
- **Actual**: created `pkg/format/entry_test.go`. Baseline run (pre-fix):
  - `calculatePathLength("a"/"abcdefg"/"some/relative/path.go") = 13/19/33`,
    want `1/7/21` → **over-count 12**, FAIL.
  - `validateLayout` panicked on every entry: *"Path field at offset 132, expected
    136"* → FAIL.
  - `ValidateEntry()==nil` (TC-1) did **not** fail — vacuous pass pre-fix, exactly
    as the design predicted (swallowed panic).
  - TC-2 `RejectsCorruptSize`: "ValidateEntry accepted a size-corrupted entry" →
    FAIL (confirms pre-fix no-op).
- **Deviations**: none — baselines matched predictions exactly.

### Step 3: Apply the minimal fixes
- **Planned**: edit `calculatePathLength` (delegate) and `validateLayout` (offset).
- **Actual**:
  - `calculatePathLength` body → `return len(be.RelativePath())`; deleted the
    duplicate unsafe arithmetic and both `//nolint:gosec // G115` sites; replaced
    the stale comment with a single contract comment citing task 24.
  - `validateLayout`: `expectedOffset := unsafe.Sizeof(*be) - 8` →
    `unsafe.Offsetof(be.Path)`; comment corrected to describe the real layout
    (4-byte tail padding; path data at `Sizeof`).
  - Post-fix focused run: both new tests **PASS**.
- **Deviations**: none.

### Step 4: Regression sweep
- **Planned**: full suite, race gate, lint.
- **Actual**:
  - `go test ./pkg/...` → `ok` (pkg, pkg/format, pkg/fsdedupe).
  - Race gate `go test -race -gcflags=all=-d=checkptr=0 ./pkg/format/` → `ok`.
  - **Extra**: plain `go test -race ./pkg/format/` (checkptr **enabled**) → `ok`,
    directly confirming the negative test's downward corruption stays in-bounds
    (the OOB concern raised in plan review).
  - `golangci-lint run ./pkg/format/...` → **0 issues** (clean after removing the
    G115 suppression).
- **Deviations**: none.

### Step 5: Documentation
- **Actual**: contract comment on `calculatePathLength`; corrected layout comment
  in `validateLayout`. No user-facing/API docs affected (unexported helpers, no
  on-disk format change).

## Files Changed
- `pkg/format/entry.go` — `calculatePathLength` (delegate) + `validateLayout`
  (offset fix) + comments.
- `pkg/format/entry_test.go` — new: `TestEntry_PathLength_MatchesRelativePath`,
  `TestEntry_ValidateEntry_RejectsCorruptSize`.

## Blockers Encountered
None.

## Deferral Check
- [x] All steps from d-implementation-plan.md executed.
- [x] All success criteria from a-task-plan.md met (calculatePathLength canonical;
      validateLayout no-panic; ValidateEntry genuinely accepts/rejects; tests pin
      all; stale comments removed; suite + race + lint green).
- [x] b-requirements: N/A for bugfix task type.
- [x] c-design-plan guidance (Decisions 1 + 4, corrected 12-byte model) followed.
- [x] No planned work deferred.

## Security Review

**State**: no findings

I have full context now. Let me complete my security review of this changeset.

## Security review — task 24 (implementation-exec)

I reviewed the changeset, which comprises four CWF process documents (`a-`/`c-`/`d-`/`e-` plans) and the production change to `pkg/format/entry.go`, plus the new test file `pkg/format/entry_test.go` (referenced by the plans; present in the working tree). I walked all five FR4 threat categories.

**(a) Bash injection / unsafe command construction.** No shell-out, no `system()`/backtick/`exec` construction anywhere in the diff. The change is pure in-process Go. Not applicable.

**(b) Perl helpers consuming git/user output.** No Perl, no git-porcelain parsing in the changeset. Not applicable.

**(c) Prompt injection.** The added files are plan markdown and Go source. None introduce a new untrusted-string → LLM-context flow, no `{arguments}` substitution surface, no template that interpolates task descriptions into a downstream model prompt. The plan docs are CWF-internal process artefacts. Not applicable.

**(d) Unsafe environment-variable handling.** No `os.Getenv`, no env var influencing any path/`chmod`/`rm`/clone operation. Not applicable.

**(e) Pattern-based risks (safe-here-but-risky-elsewhere).** This is the category the change actually engages, because it touches `unsafe`-pointer code and memory-safety bounds. My findings here are not defects — the change is a net memory-safety improvement — but two invariant-dependent patterns are worth recording so future edits don't silently void them:

1. **`calculatePathLength` now panics on out-of-guard `Size` values (was tolerant).** The old body computed a signed length and clamped via the trailing-NUL scan, so an out-of-range `Size` returned a (possibly wrong) number without panicking. The new `len(be.RelativePath())` inherits `RelativePath`'s hard panic guard (`Size < minEntrySize || Size > 65535`). This is **safe here because** the sole caller is `ValidateEntry`, whose `Size ∈ [minSize, 4096]` bound (entry.go:159–165) is a strict subset of `RelativePath`'s guard, and `ValidateEntry` additionally wraps its body in `recover()`. The single-caller invariant is verified: LSP/grep confirm `calculatePathLength` has exactly one production caller (entry.go:168) plus the test. **Audit future uses** where `calculatePathLength` is called from a path that does not pre-bound `Size` to `[minSize, 4096]` — such a caller would convert a previously-silent length computation into a panic. The in-code comment (entry.go:135–137) records this trigger correctly, which is the right mitigation given backticks/list-form don't apply here.

2. **Post-fix `RelativePath` reads `Size - Sizeof(Entry)` bytes past the struct start, so an inflated `Size` is now an out-of-bounds read.** The corrected offset (path data at `Sizeof=144`) means a corrupt `Size` that overstates the entry length makes the backward NUL scan and `unsafe.String` index past the entry's allocation. This is **safe here because** every production `ValidateEntry` caller operates on mmap-resident entries whose `Size` was written by `EntrySerialiser` and validated, and the test deliberately corrupts `Size` **downward** only (entry_test.go:48–60) to stay in-bounds — the comment there explicitly documents that corrupting upward would trip `checkptr` under `-race`. This bounds dependency is intrinsic to the zero-copy design and pre-exists this task; the change does not widen it (`RelativePath` already had this property). **Audit future uses** that feed `RelativePath`/`ValidateEntry` an entry whose `Size` is attacker- or corruption-influenced without an independent upper bound tied to the backing buffer length — the guard is `Size ≤ 65535`, not "`Size ≤ remaining buffer", so a large-but-in-range `Size` on a short buffer is an OOB read. This is a property of the existing reader, surfaced (not introduced) by routing `calculatePathLength` through it.

Neither of the above is an actionable defect in this diff: the change removes ~15 lines of duplicate `unsafe` arithmetic and a `//nolint:gosec` site, consolidates the path-offset to a single owner, and the documented test guards (downward-only corruption, mixed mod-8 lengths, race gate) are sound. The two items are recorded as the FR4(e) "safe here because X; audit future uses where X might not hold" framing the threat model invites, not as blocking concerns.

Conclusion: no actionable security findings. The diff reduces `unsafe` surface and the residual bounds invariants are pre-existing, documented, and correctly guarded at the only callsite.

```cwf-review
state: no findings
summary: Memory-safety-improving refactor (removes duplicate unsafe path-offset math); residual Size-bound invariants on RelativePath are pre-existing, single-caller-verified, and documented in-code — no actionable findings.
```

## Status
**Status**: Finished
**Next Action**: /cwf-testing-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Lessons Learned
*To be captured during retrospective*
