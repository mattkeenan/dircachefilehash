# Adopt full Go pre-commit hook and security review - Implementation Execution
**Task**: 2 (chore)

## Task Reference
- **Task ID**: internal-2
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/2-{task-description}
- **Template Version**: 2.1

## Goal
Execute the implementation following the plan in d-implementation-plan.md and e-testing-plan.md.

## Execution Checklist
- [ ] Read d-implementation-plan.md and e-testing-plan.md thoroughly
- [ ] Verify all prerequisites met
- [ ] Execute implementation steps sequentially
- [ ] Update "Actual Results" for each step
- [ ] Document any deviations from plan
- [ ] Update status to "Implemented" when complete

## Implementation Steps (from d-implementation-plan.md)
See d-implementation-plan.md. All 5 steps executed; actual results below.

## Actual Results

### Step 1-2: Config (gosec enabled + excludes)
- **Planned**: add `gosec` to `linters.enable`; `gosec.excludes = [G103, G304, G401, G505, G115]`; add `{linters:[gosec], path: _test\.go}`.
- **Actual**: done exactly as planned. `golangci-lint config verify` passes.

### Step 3: Production nolints — **major deviation in count**
- **Planned**: ~12 per-line `//nolint:gosec` sites (tables D + E).
- **Actual**: **26 production sites**. The plan's measurement was taken with golangci-lint's default `max-same-issues: 3`, which **silently hid all but the first 3 instances of each rule**. The true firing set only became visible after running `--max-same-issues=0 --max-issues-per-linter=0`. All 26 were read individually and confirmed intentional/FP before suppression. Full list with rationale: see "Final disposition" below.

### Step 4: Verify
- **gosec → 0**: `golangci-lint run ./...` reports zero `(gosec)` lines.
- **Catches new code (TC-3)**: planted a `math/rand` snippet → gosec flagged `G404`; removed. Confirms excludes did not neuter gosec.
- **No regression (TC-5)**: `GOFLAGS="-gcflags=all=-d=checkptr=0" go test -race ./pkg/... ./cmd/...` — all packages pass.
- **Residual lint**: exactly the 3 pre-existing non-gosec issues (cyclop ×2, unparam ×1), already backlogged.

### Step 5: Document
- Added top-level `## Security Review` section to `CLAUDE.md` covering both mechanisms (gosec floor + CWF changeset review).

## Deviations from plan (rationale)
1. **26 nolint sites, not ~12** — `max-same-issues: 3` masked the rest during planning. No new judgement: every extra site is the same `.dcfh/`-perms or env-var/ssh-argv class already reasoned in the plan.
2. **Lifted the issue cap** (`issues.max-same-issues: 0`, `max-issues-per-linter: 0`) — NOT in the plan. Justified: a security gate that silently drops a 4th instance of an insecure pattern from `--new` review is a real gap. This is the change that made the true firing set visible.
3. **Two production G204 sites** (`pkg/wire_client_shell.go:53`, `pkg/wire_transport.go:49`) — the plan assumed G204 was test-only. Both are `exec.CommandContext(ctx, "ssh", argv...)`: fixed binary, argv from a validated `RepoURI` (the ssh wire transport). FP for risk; per-line nolint. The `_test\.go` gosec exclusion still covers the test-harness G204s.
4. **Rule relabels** — `convert-index:144` is G306 (perms), not G703 as the plan labelled it; `dcfh.go:26,43` are G703 (the plan had them, they were cap-hidden in the first run). `recovery.go:341` fired as planned.
5. **`cmd/dcfhfix/` perms sites** (×4) — not enumerated in the plan; same non-secret `.dcfh/` index/backup class, suppressed per-line (kept gosec active in dcfhfix rather than a blanket path exclude).

## Final disposition (26 production nolint sites, all verified by reading)
- **G108/G114** (opt-in localhost pprof): `cmd/dcfh/dcfh.go:9,20`
- **G703** (env-var path / CLI-arg / `DirEntry.Name()` base name): `cmd/dcfh/dcfh.go:26,43`, `convert-index-v1-to-v2.go:65`, `pkg/recovery.go:400`, `pkg/snapshot.go:448`
- **G204** (fixed `"ssh"` binary, validated argv): `pkg/wire_client_shell.go:53`, `pkg/wire_transport.go:49`
- **G301/G302/G306** (`.dcfh/` non-secret dirs/files): `cmd/dcfhfix/entry_processor_workflow.go:155`, `cmd/dcfhfix/entry_workflow_main.go:86`, `cmd/dcfhfix/main.go:969,1032`, `convert-index-v1-to-v2.go:144`, `pkg/binary_entry_index_file.go:87`, `pkg/ignore.go:176`, `pkg/metastore.go:272`, `pkg/recovery.go:341`, `pkg/snapshot.go:80,98,117,179,203`, `pkg/temp_index_writer.go:30`, `pkg/wire_handler.go:436,444`

## Blockers Encountered
1. **`max-same-issues` cap** — a measurement trap, not a blocker; resolved by lifting the cap.
2. **govulncheck blocked the commit** (`GO-2026-4971`) — a newly-published advisory (Windows-only `net` NUL-byte panic, reachable via the opt-in pprof `http.ListenAndServe`; dcfh is Unix-only) hitting unchanged code. Fixed in `net@go1.26.3`. The hook has no allowlist and `--no-verify` is forbidden, so the task's own "govulncheck passes on a clean tree" criterion required resolving it. **Resolution** (user-approved): added `toolchain go1.26.3` to `go.mod` (durable, project-wide; `GOTOOLCHAIN=...+auto` upgrades automatically). govulncheck now reports 0 vulnerabilities.

## Deferral Check
Before marking status=Finished, verify:
- [ ] All steps from d-implementation-plan.md executed
- [ ] All success criteria from a-task-plan.md met
- [ ] All requirements from b-requirements-plan.md addressed (if applicable)
- [ ] All design guidance in c-design-plan.md followed (if applicable)
- [ ] No planned work deferred without user approval
- [ ] If work deferred: Follow-up task created and linked

**If deferral required**: Get user approval, document rationale, create follow-up task.

## Security Review

**State**: no findings

no findings: empty changeset

The exec-phase `security-review-changeset` helper scopes its diff to CWF-internal
scripts and shebang-script files (its FR4 concern is workflow-tooling tampering /
prompt-injection surface, not general Go-source security — that is gosec's remit).
This task changed only Go source, `.golangci.yml` and `CLAUDE.md`, none of which
fall in that scope, so the changeset is empty and the subagent was not invoked
(per the skill's empty-changeset rule). The substantive security review for the Go
changes is the gosec wiring itself plus the per-finding code reading recorded above.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
*To be filled upon completion*

## Lessons Learned
*To be captured during retrospective*
