# Scan edge-case integration tests - Implementation Execution
**Task**: 27 (chore)

## Task Reference
- **Task ID**: internal-27
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/27-scan-edge-case-integration-tests
- **Template Version**: 2.1

## Goal
Execute the implementation following the plan in d-implementation-plan.md and
e-testing-plan.md: add the three discovery→hash boundary-race tests
(grow / shrink / file→directory) reusing the `hashPreReadHook` seam.

## Execution Checklist
- [x] Read d-implementation-plan.md and e-testing-plan.md thoroughly
- [x] Verify all prerequisites met (helpers present on baseline)
- [x] Execute implementation steps sequentially
- [x] Update "Actual Results" for each step
- [x] Document any deviations from plan
- [x] Update status to "Finished" when complete

## Actual Results

### Step 1: Confirm helpers compile-resolve
- **Planned**: Verify `seedMainRepo`, `writeFile`, `withHashPreReadHook`,
  `runUpdate`, `assertLoadsClean`, `freshFind`, `IsHashEmpty`/`HashString`
  resolve in `pkg/scan_edge_cases_test.go`.
- **Actual**: All present and already used by TC-7/8/9 in the same file.
  `freshFind` confirmed at line 14, `withHashPreReadHook` at
  `pkg/fault_inject_test.go:45`.
- **Deviations**: None.

### Step 2: TC-10 — grow-before-hash tolerated
- **Planned**: Hook rewrites `z.txt` with strictly longer contents; assert nil
  error, clean load, entry present, `!IsHashEmpty()`.
- **Actual**: `TestScanEdge_GrowBeforeHash_Tolerated` added. Passes.
- **Deviations**: None.

### Step 3: TC-11 — shrink-before-hash tolerated
- **Planned**: Hook rewrites `z.txt` with strictly shorter (non-empty)
  contents; same coherence-only oracle.
- **Actual**: `TestScanEdge_ShrinkBeforeHash_Tolerated` added (seed
  "zulu-original-longer\n" → "z\n"). Passes.
- **Deviations**: None.

### Step 4: TC-12 — file→directory-before-hash tolerated
- **Planned**: Hook does `os.Remove` then `os.Mkdir`; entry stays on the
  non-symlink `HashOne` branch, read fails `EISDIR`, swallowed → empty hash,
  run succeeds. Inline note on single-file (one-shot hook) assumption.
- **Actual**: `TestScanEdge_FileToDirBeforeHash_Tolerated` added with the
  non-idempotency note in the doc comment. Asserts `IsHashEmpty()`. Passes.
- **Deviations**: None.

### Step 5: Validation
- **Planned**: ScanEdge green; full `./pkg/...` green; `-race` green; lint clean.
- **Actual**: All pass:
  - `go test ./pkg/ -run 'ScanEdge_(Grow|Shrink|FileToDir)' -v` → 3/3 PASS.
  - `go test ./pkg/...` → ok.
  - `go test -race -gcflags=all=-d=checkptr=0 ./pkg/ -run 'ScanEdge'` → ok.
  - `golangci-lint run ./pkg/` → clean.
- **Deviations**: One — see below.

## Deviations from Plan

**unparam finding on the reused `freshFind` helper.** Adding three more
all-`"z.txt"` call sites tipped the `unparam` linter over its threshold, so a
full-repo `golangci-lint run ./...` now reports
`freshFind - rel always receives "z.txt"` on the helper's (unchanged)
signature line. The pre-commit `--new` staged gate does **not** flag it (the
finding attaches to line 14, an unchanged line), but the repo standard measures
through the full `golangci-lint run ./...`. `freshFind` is a deliberately
general "find rel in a fresh on-disk load" test helper; removing the parameter
would expand the diff into the previously-clean TC-7/8/9 call sites for no
behavioural gain. Resolved per the repo's documented convention (per-line
`//nolint` with rationale) by adding
`//nolint:unparam // rel kept general for future edge-case tests` plus a
clarifying doc-comment line. Full audit clean afterward.

No production code changed — test-only, as planned.

## Blockers Encountered
None.

## Deferral Check
- [x] All steps from d-implementation-plan.md executed (Steps 1–5).
- [x] All success criteria from a-task-plan.md met (1→TC-10, 2→TC-11, 3→TC-12,
      plus determinism/`-race` and green-suite criteria).
- [ ] b/c phases — N/A (chore omits requirements and design).
- [x] No planned work deferred without approval. The walk-phase lstat-ENOENT
      seam was an **approved** deferral recorded in d-implementation-plan.md
      (Decision section); a backlog note is to be added in the retrospective.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Security Review

**State**: no findings

This changeset is entirely test code and CWF process documentation (markdown plan files). Let me review it against the threat model.

The changeset comprises:
1. Three markdown plan files (`a-task-plan.md`, `d-implementation-plan.md`, `e-testing-plan.md`) — CWF process docs.
2. A modification to `pkg/scan_edge_cases_test.go` — three new Go test functions plus a `freshFind` helper comment/nolint tweak.

Let me reason through the five threat categories.

**(a) Bash injection / unsafe command construction.** No shell command construction exists in the diff. The Go test code uses `os.WriteFile`, `os.Remove`, `os.Mkdir`, and `filepath.Join` with constant string literals (`"z.txt"`, `"ZULU-GROWN-..."`). No `exec.Command`, no `os/exec`, no `system`-style single-string invocation. The markdown files reference shell snippets only as quoted illustrations of test commands (`go test ...`), not executable helpers. Clean.

**(b) Perl helpers consuming git/user output.** No Perl in the changeset; no git-porcelain parsing. The modified file is Go test code. Not applicable.

**(c) Prompt injection via user-supplied strings.** The new test functions do not flow untrusted strings into LLM context. The plan-file markdown is authored CWF process documentation, not user free-text reaching a model verbatim. No `{arguments}` substitution surface is introduced. Clean.

**(d) Unsafe environment-variable handling.** No env-var reads in the diff. All paths are derived from `ms.RootDir` (a `t.TempDir()`-backed test root per the plan) joined with constant relative names. No `os.Getenv`, no `chmod`/`rm` driven by env. Clean.

**(e) Pattern-based risks (safe-here-but-risky-elsewhere).** Two patterns worth noting, both already self-documented in the diff and non-actionable:

1. The `os.WriteFile(zAbs, ..., 0o644)` and `os.Mkdir(zAbs, 0o755)` calls in the hooks write to `zAbs = filepath.Join(ms.RootDir, "z.txt")`. Safe here because `ms.RootDir` is a per-test isolated temp directory and `"z.txt"` is a constant literal — there is no traversal vector. The `//nolint:gosec` suppressions (G306/G301) mirror the existing TC-8 pattern and are scoped to test temp files. Audit future uses where the path component becomes a variable derived from scanned filenames or user input rather than a constant — the suppression rationale would no longer hold.

2. The TC-12 `os.Remove`+`os.Mkdir` non-atomic pair is safe because, as the inline comment states, `z.txt` is a single new entry hashed by exactly one worker so the hook fires once. The diff already flags this explicitly ("non-idempotent under concurrent hook invocation if generalised to many files"). This is the required safe-here framing, already authored into the test — no action needed.

The `freshFind` change is purely a comment expansion plus a `//nolint:unparam` directive; no behavioural change, no security surface.

Overall: the diff is test-only plus process documentation, with all file-system writes confined to per-test temp roots using constant path components, and no shell/Perl/env/injection surface. The two pattern observations are already self-documented in the code with correct safe-here framing and require no change. Clean.

```cwf-review
state: no findings
summary: Test-only Go changes plus CWF plan docs; FS writes confined to per-test temp roots with constant paths; no injection/secrets/auth/env/prompt-injection surface.
```

## Lessons Learned
*To be captured during retrospective*
