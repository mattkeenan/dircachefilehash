# Adopt full Go pre-commit hook and security review - Plan
**Task**: 2 (chore)

## Task Reference
- **Task ID**: internal-2
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/2-adopt-full-go-pre-commit-hook-and-security-review
- **Baseline Commit**: a00577734ccc25d5e3382b95a2853381acace087
- **Template Version**: 2.1

## Goal
Close the one remaining Go static-analysis security gap by enabling `gosec` in the existing `.golangci.yml`, and adopt the CWF security-review phase as routine practice for this repo.

## Scope Note (why this is smaller than the title)
Investigation found the repo's pre-commit gates are already at the intended standard: `.githooks/pre-commit` (active via `core.hooksPath=.githooks`) already runs gofmt, go fix, gopls, golangci-lint, govulncheck and `go test -race` (with `checkptr=0` for the mmap/unsafe-pointer code), and `.golangci.yml` already enables staticcheck (via `default: standard`), complexity (`cyclop`/`gocognit`) and a tuned `govet`. The only missing piece is `gosec`. The earlier "adopt the gresearch hook wholesale" idea was dropped because gresearch ships a bare config and runs tools as standalone binaries — a net downgrade here. `godot` is deliberately excluded (excessive false positives).

## Success Criteria
- [ ] `gosec` added to `.golangci.yml` `linters.enable`; `golangci-lint run ./...` passes clean on the existing tree, with any unsafe-pointer/mmap findings handled via scoped path/line exclusions (mirroring the existing `unsafeptr`/`fieldalignment` handling) — not a blanket disable
- [ ] The active `.githooks/pre-commit` surfaces gosec findings through its existing golangci-lint step — proven by a deliberately-inserted insecure snippet being caught, then reverted
- [ ] No regression: full existing suite (gofmt, go fix, gopls, golangci-lint, govulncheck, `go test -race`) still passes on a clean tree
- [ ] CWF security-review phase adopted: documented in project docs (e.g. CLAUDE.md) and exercised once on this task's own changeset via the `cwf-security-reviewer-changeset` agent

## Original Estimate
**Effort**: <1 day
**Complexity**: Low
**Dependencies**: `gosec` and `golangci-lint` v2 (both confirmed installed); existing `.githooks/pre-commit` and `.golangci.yml` (extend, do not replace)

## Major Milestones
1. **gosec enabled**: `gosec` active in `.golangci.yml`, false positives triaged into scoped exclusions, lint green
2. **Hook verified**: pre-commit demonstrably catches a real gosec finding; full suite green on clean tree
3. **Security review adopted**: CWF security-review phase documented and run against this changeset

## Risk Assessment
### High Priority Risks
- **gosec floods on intentional `unsafe.Pointer` use** (G103 and related) across the mmap/zero-copy code in `pkg/binary_entry.go` etc.
  - **Mitigation**: scoped per-path/per-line exclusions in `.golangci.yml`, mirroring the existing `unsafeptr` `//nolint`/`fieldalignment` approach; never blanket-disable gosec

### Medium Priority Risks
- **Commit latency**: gosec lengthens the hook's golangci-lint step on every commit
  - **Mitigation**: rely on the hook's existing staged/`--new` fast-path; reassess only if latency becomes painful
- **Process drift**: "adopt CWF security phase" can quietly lapse as tribal knowledge
  - **Mitigation**: encode it in CLAUDE.md/project docs so it is discoverable and repeatable

## Dependencies
- `gosec`, `golangci-lint` v2 installed (confirmed present on this machine)
- Builds on the existing `.githooks/pre-commit` and `.golangci.yml` — must merge into them

## Constraints
- Must NOT replace the tuned `.golangci.yml` or the dcfh-specific hook logic (staged mode, re-stage-on-fix, `checkptr=0`)
- British spelling in prose; quality gates (compile + tests pass) hold on every commit

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [ ] **Time**: >1 week? No — under a day
- [ ] **People**: >2 people on different parts? No — single contributor
- [ ] **Complexity**: 3+ distinct concerns? No — one config change + one process change
- [ ] **Risk**: high-risk components needing isolation? No — gosec false positives are contained and reversible
- [ ] **Independence**: parts separable? Marginally (config vs process), but both are small and related

**Verdict**: 0 signals triggered — no decomposition; proceed as a single task.

## Status
**Status**: Finished
**Next Action**: /cwf-requirements-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All success criteria met: gosec enabled, `golangci-lint run ./...` clean of gosec (26 scoped suppressions, not a blanket disable), hook proven to catch a new gosec finding (G404/G204 probes), zero regression, CWF security-review process documented in CLAUDE.md. See f-/g-/j- files.

## Lessons Learned
Scope grew slightly (issue-cap lift, go1.26.3 toolchain bump) — both forced by what the tooling surfaced, not by the original plan. The "scoped exclusions not blanket disable" constraint held and proved correct.
