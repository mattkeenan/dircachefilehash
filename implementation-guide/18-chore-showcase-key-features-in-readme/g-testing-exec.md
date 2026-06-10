# Showcase key features in README - Testing Execution
**Task**: 18 (chore)

## Task Reference
- **Task ID**: internal-18
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/18-showcase-key-features-in-readme
- **Template Version**: 2.1

## Goal
Execute the tests defined in e-testing-plan.md and verify implementation from d-implementation-plan.md.

## Execution Checklist
- [x] Read e-testing-plan.md and d-implementation-plan.md thoroughly
- [x] Verify test environment ready
- [x] Execute test cases sequentially
- [x] Record pass/fail for each test
- [x] Document failures with reproduction steps (none)
- [x] Update status

## Test Results

Static verification (no runtime code shipped — chore docs change). Each claim was
grepped against both the README and its source string; build/test run as the
regression guard.

### Functional Tests

| Test ID | Test Case | Expected | Actual | Status | Notes |
|---------|-----------|----------|--------|--------|-------|
| TC-1 | Features section present & teaser-only (SC1) | `## Features` before `## The tools`; 4 one-line bullets; no FS/glyph detail | `## Features` at L8, `## The tools` at L19; 4 bullets; FS list & glyph list grep clean inside the section | PASS | Detail correctly lives only in Edit B/C |
| TC-2 | Dedupe sold accurately (SC2) [critical] | Block-level via `FIDEDUPERANGE`, Linux-only, btrfs/XFS `reflink=1`/bcachefs, frees space without removing files, skips+reports unsupported devices, no `--exclusive`/no-op | All terms present in README and matched in `dupes.go`; "skips that device and reports it rather than failing" at L100; `no-op`/`--exclusive` absent | PASS | Selection conveyed by category + `dcfh dupes help` pointer |
| TC-3 | Interactive-tree sold as change tracking (SC3) [critical] | Change-tracking framing; glyphs `+`/`~`/`-`/`*`+colour; `z` hide; `c/f/a/m/d/n` sort + `r`; nav keys; TTY | Framing at L112; glyph colours match `render.go:316–329`; every key matches footer `render.go:154`; sort metric labels match `sort.go` | PASS | Sort metric labels corrected during exec (changed-bytes/files/added/modified/deleted/name) |
| TC-4 | No invented flags / no removed API (SC2/3/4) [critical] | Every named flag in `cmd/dcfh/`; zero `DirectoryCache`/`FileEntry`/`NewDirectoryCache`; no `--exclusive` | 6/6 flags exist in `cmd/dcfh/`; removed-API grep clean; `--exclusive` grep clean | PASS | — |
| TC-5 | Link integrity (SC4) | Every relative link resolves | 7/7 resolve (`docs/`, `docs/README.md`, `docs/ARCHITECTURE.md`, `cmd/dcfhfind/DESIGN.md`, `cmd/dcfhfix/DESIGN.md`, `LICENSE`) | PASS | — |
| TC-6 | `remote` still omitted (SC4) | Not presented as a user command | No `remote` in the command table; no `remote` mention anywhere in README | PASS | Stays `Hidden: true` |
| TC-7 | Docs-only / no regression (SC5) [critical] | `go build`/`go test` green; only `README.md` changed | Build OK; full test suite green; `git diff --name-only main...HEAD` (excl. workflow dir) = `README.md` | PASS | — |

### Non-Functional Tests
- **Usability**: TC-1/TC-3 — platform caveats (Linux-only, TTY) stated next to the
  feature, not buried. PASS.
- **Security**: docs-only; the safety-sensitive claim (dedupe non-destructive)
  verified accurate in TC-2; full review in `## Security Review` below.
- **Reliability**: TC-7 — green build/test; single-file, trivially revertible. PASS.
- **Performance**: N/A — no runtime change; not measured.

## Test Failures

None. All TC-1…TC-7 passed on first execution. (The one in-exec correction — sort
metric labels — was caught and fixed during f-implementation-exec, before this
phase; see f-implementation-exec.md Step 4.)

## Coverage Report

- **SC coverage**: SC1→TC-1; SC2→TC-2,TC-4; SC3→TC-3,TC-4; SC4→TC-4,TC-5,TC-6;
  SC5→TC-7. All five success criteria covered.
- **Critical paths** (TC-2, TC-3, TC-4, TC-7): 100% clean.
- **Regression**: `go build ./...` + `go test ./...` green; only `README.md`
  changed.

## Status
**Status**: Finished
**Next Action**: /cwf-retrospective
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Lessons Learned
A claim-to-source grep table is the right test shape for a "keep the docs honest"
chore: each row pins a README assertion to the exact source string it must match,
so accuracy regressions surface mechanically rather than by re-reading prose.

## Security Review

**State**: no findings

The changeset is the testing-exec phase for task 18, a documentation-only chore. Let me reason through the threat model.

This diff contains:
1. `README.md` additive prose (Features section, dedupe subsection, interactive-tree expansion).
2. Three CWF workflow planning files: `a-task-plan.md`, `d-implementation-plan.md`, `e-testing-plan.md` (the testing-exec changeset captures the planning artefacts plus the README and the prior `f-implementation-exec.md`).
3. The `f-implementation-exec.md` which embeds the prior implementation-exec security review verdict.

Reasoning through the threat categories:

**(a) Injection / command execution.** No source code is modified — no `.go`, shell, build, or config files. The README adds three fenced bash example blocks (`dcfh dupes --min-size 1M`, `dcfh dupes --fs-dedupe`, `dcfh dupes --fs-dedupe --dry-run`). These are illustrative literals with no shell metacharacters, substitutions, or pipes, and are not executed by any harness. No injection surface.

**(b) Secrets / credentials.** No tokens, keys, passwords, connection strings, or private hostnames added. References are to public kernel facilities (`FIDEDUPERANGE`) and filesystem names (btrfs, XFS, bcachefs). Baseline commit hashes in the plan headers are non-sensitive git metadata. Nothing sensitive committed.

**(c) Authentication / authorisation.** No auth, permission-check, or access-control code touched. The hidden `remote` SSH endpoint is deliberately kept omitted from user-facing copy (`Hidden: true`), the conservative choice — the docs correctly do not advertise a machine-only audit surface.

**(d) Environment-variable handling.** No env-var reads, writes, or propagation. No code at all.

**(e) Prompt-injection surface.** The CWF `.md` files are agent-authored planning artefacts read by downstream CWF steps. I checked them for instruction-injection or directive text aimed at steering a later agent: they contain ordinary plan/checklist prose (success criteria, source pins, test cases, status blocks) with no embedded imperatives directed at the reviewer or future automation, no "ignore previous instructions" / fake-system payloads, and no untrusted external content. The `f-implementation-exec.md` embeds a prior `cwf-review` verdict block in its prose; because that block is inside a quoted diff body and not the trailing verdict of *this* response, it does not interfere with the parser — I emit exactly one verdict block below. The README prose is inert.

One forward-looking note (not a finding): the README documents `--fs-dedupe` as non-destructive ("frees space without removing files"). That accuracy claim is load-bearing for user trust in a data-affecting command — safe here because it is documentation only and was grep-verified against `cmd/dcfh/dupes.go` / `pkg/fsdedupe`; audit future edits if the dedupe destructive/non-destructive boundary ever changes, so the README does not drift into mis-describing a data-loss-capable operation.

No actionable security concerns. The changeset adds no executable behaviour, no secrets, and no auth/env/injection surface.

```cwf-review
state: no findings
summary: Docs-only testing-exec changeset (README prose + CWF plan files); no code, secrets, auth, env, or prompt-injection surface introduced.
```
