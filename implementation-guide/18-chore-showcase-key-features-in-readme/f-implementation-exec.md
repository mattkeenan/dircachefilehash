# Showcase key features in README - Implementation Execution
**Task**: 18 (chore)

## Task Reference
- **Task ID**: internal-18
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/18-showcase-key-features-in-readme
- **Template Version**: 2.1

## Goal
Execute the implementation following the plan in d-implementation-plan.md and e-testing-plan.md.

## Execution Checklist
- [x] Read d-implementation-plan.md and e-testing-plan.md thoroughly
- [x] Verify all prerequisites met
- [x] Execute implementation steps sequentially
- [x] Update "Actual Results" for each step
- [x] Document any deviations from plan
- [x] Update status when complete

## Implementation Steps (from d-implementation-plan.md)

Three additive edits to `README.md`, each re-verified against source before
landing. No `.go` change.

## Actual Results

### Step 1: Setup & re-verify source pins
- **Planned**: Confirm branch/clean tree; re-read the README sections to edit;
  re-verify every claim against current source.
- **Actual**: On `chore/18-showcase-key-features-in-readme`, tree clean. Re-read
  `README.md` in full. Re-confirmed every source pin with the Read tool:
  `dupes.go:70–76` (FIDEDUPERANGE, Linux-only, btrfs/XFS `reflink=1`/bcachefs,
  implies `--ignore-hardlinks`, defaults `--min-size` 4096); skip-and-report at
  `dupes.go:281` ("skipped device %s: filesystem does not support FIDEDUPERANGE");
  footer `render.go:154`; glyphs `render.go:316–329` (`+` green / `~` blue / `-`
  red / `*` mixed); sort keys `sort.go:15–45` + `sort_test.go:90–96`.
- **Deviations**: None.

### Step 2: Edit A — `## Features` section (teasers only)
- **Planned**: Insert a 4-bullet teaser section after the lead-in, before
  `## The tools`; no FS list, no glyph/key list, no speed-mechanism re-explanation.
- **Actual**: Added the section with 4 one-line bullets (block-level dedupe,
  change-tracking tree, fast-by-design ~9×, snapshots/diffs/nested-repo). Detail
  deferred to Edits B/C as planned; adjective kept as "git-inspired".
- **Deviations**: None.

### Step 3: Edit B — `### Duplicate detection and dedupe`
- **Planned**: Canonical dedupe detail near the commands table: content-match +
  size/date/hardlink selection by category with a pointer to `dcfh dupes help`;
  `--fs-dedupe` paragraph (Linux-only, FIDEDUPERANGE, reflink FS, frees space
  without removing files, implies `--ignore-hardlinks`, min-size 4096,
  skips+reports unsupported devices); a fenced example. No `--exclusive`.
- **Actual**: Added as planned, including the 3-line example
  (`--min-size 1M`, `--fs-dedupe`, `--fs-dedupe --dry-run`). `--exclusive` and
  "no-op" wording absent (grep-confirmed).
- **Deviations**: None.

### Step 4: Edit C — expand `### Interactive tree viewer`
- **Planned**: Reframe as change-tracking; name glyphs `+`/`~`/`-`/`*` + colour;
  `z` hide-unchanged; `c/f/a/m/d/n` sort + `r` reverse; nav keys; keep TTY.
- **Actual**: Replaced the single sentence with the change-tracking framing,
  coloured glyphs, and the full key list.
- **Deviations**: First draft mislabelled the sort keys as "changes, files, size,
  mtime, dirs, name". Caught immediately by reading `sort.go` — the real metrics
  are change-bytes / change-files / added / modified / deleted / name. Corrected
  before validation. (Exactly the over-selling-drift risk the plan flagged;
  verify-before-land caught it.)

### Step 5: Validation (mechanical)
- **Planned**: Link sweep, removed-API/invented-flag grep, build/test, diff scope.
- **Actual**: All clean — 7/7 relative links resolve; zero
  `DirectoryCache`/`FileEntry`/`NewDirectoryCache`; no `--exclusive`/`no-op` in
  copy; every named flag (`--fs-dedupe`/`--min-size`/`--max-size`/
  `--ignore-hardlinks`/`--dry-run`/`--interactive-tree`) exists in `cmd/dcfh/`;
  `go build ./...` OK; `go test ./...` green; `git diff --name-only` = `README.md`
  only.
- **Deviations**: None.

## Blockers Encountered

None.

## Deferral Check
Before marking status=Finished, verify:
- [x] All steps from d-implementation-plan.md executed
- [x] All success criteria from a-task-plan.md met (SC1–SC5; SC5 build/test green)
- [x] All requirements from b-requirements-plan.md addressed (N/A — chore, no b-)
- [x] All design guidance in c-design-plan.md followed (N/A — chore, no c-)
- [x] No planned work deferred without user approval
- [x] If work deferred: Follow-up task created and linked (none deferred)

**If deferral required**: Get user approval, document rationale, create follow-up task.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Lessons Learned
Re-verifying every claim against source at exec time (not trusting the plan's
pins) caught one factual error — the sort-key labels — before it shipped. The
plan's "measure twice" discipline paid off exactly where the task's headline
risk (sell-copy drifting ahead of code) lives.

## Security Review

**State**: no findings

This changeset is documentation-only. Let me review it against the security threat model.

The diff comprises:
1. `README.md` — additive prose: a new "Features" section, a "Duplicate detection and dedupe" subsection, and an expanded "Interactive tree viewer" subsection.
2. Three new CWF workflow planning files (`a-task-plan.md`, `d-implementation-plan.md`, `e-testing-plan.md`) under `implementation-guide/18-chore-showcase-key-features-in-readme/`.

Reasoning through the threat categories:

**(a) Injection / command execution.** No source code is changed — no `.go`, shell, config, or build files. The README adds three fenced bash examples (`dcfh dupes --min-size 1M`, `dcfh dupes --fs-dedupe`, `dcfh dupes --fs-dedupe --dry-run`). These are illustrative documentation, not executed by any build/test harness, and contain only literal, well-formed dcfh invocations with no shell metacharacters, substitutions, or pipes. No injection surface introduced.

**(b) Secrets / credentials.** No tokens, keys, passwords, connection strings, or hostnames are added. The prose references kernel ioctls (`FIDEDUPERANGE`) and filesystem names — public, non-sensitive. Nothing sensitive committed.

**(c) Authentication / authorisation.** No auth code, permission checks, or access-control surface is touched. The README mentions the hidden `remote` SSH endpoint only by deliberately *omitting* it from user-facing copy (the plan explicitly keeps it `Hidden`), which is the conservative choice.

**(d) Environment-variable handling.** No env-var reads, writes, or propagation logic changed. No code at all.

**(e) Prompt-injection surface.** The CWF workflow `.md` files are agent-authored planning artefacts that downstream CWF steps read. I checked their content for instruction-injection or directive-style text aimed at steering a later agent: they contain ordinary plan/checklist prose (success criteria, source-of-truth pins, test cases) with no embedded imperative commands directed at the reviewer or at future automation, no fenced "system"/"ignore previous" style payloads, and no external/untrusted-origin content. The README prose is likewise inert. One pattern worth a forward-looking note (not a finding): the README documents `--fs-dedupe` as a *non-destructive* operation that "frees space without removing files." That accuracy claim is load-bearing for user trust in a data-affecting command — safe here because it is documentation only and the plan grep-verifies it against `cmd/dcfh/dupes.go` and `pkg/fsdedupe`; audit future edits where the dedupe semantics or its destructive/non-destructive boundary change, so the README does not drift into mis-describing a data-loss-capable operation.

No actionable security concerns. The changeset adds no executable behaviour, no secrets, no auth/env surface, and no prompt-injection vector.

```cwf-review
state: no findings
summary: Docs-only changeset (README prose + CWF plan files); no code, secrets, auth, env, or injection surface introduced.
```
