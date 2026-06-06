# interactive-tree gdu-style post-run view - Rollout
**Task**: 11 (feature)

## Task Reference
- **Task ID**: internal-11
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/11-{task-description}
- **Template Version**: 2.1

## Goal
Define deployment strategy and rollout plan for interactive-tree gdu-style post-run view.

> This is a single-binary CLI/library, not a hosted service. The web-style
> phased/canary/monitoring template does not apply; the sections below are
> adapted to how dcfh actually ships (tagged release via goreleaser, opt-in
> flag, branch-landing model).

## Deployment Strategy
### Release Type
- **Strategy**: Ship in the next tagged release. The feature is **opt-in** behind `--interactive-tree` (default-off) on `dcfh status`/`update`, and the non-interactive path is byte-for-byte unchanged (proven by TC-17 + the full `cmd/dcfh` suite). Blast radius for existing users is therefore zero until they pass the flag.
- **Rationale**: A local CLI has no users-percentage or canary surface. Opt-in + default-off **is** the gradual rollout: adoption is per-invocation and self-selected. No feature flag service, no server deploy.
- **Branch landing**: lands on `local-main` by squash (per the project branch-landing model); do **not** ff-merge top-level tasks to `main`. The public-branch port must drop AI/CWF references and carry clean commit messages (see CLAUDE.md "Branch Management").
- **Packaging**: `goreleaser release --snapshot --clean` builds deb/rpm/tar.gz; the three binaries (`dcfh`, `dcfhfind`, `dcfhfix`) build green via `make build`.

### Pre-Deployment Checklist
- [x] Code review completed (CWF plan reviews b/c/d; user review at the pre-exec gate; implementation-phase FR4(a–e) security review = **no findings**).
- [x] All tests passing — 22/22 mapped cases (unit + tcell `SimulationScreen` + real-TTY pty), full `go test ./...` green, `-race -d=checkptr=0` clean.
- [x] Security scan — `golangci-lint`/gosec gate **0 issues**; one justified `//nolint:gosec // G115` on the stdout-fd narrowing.
- [x] Performance — no concern: the viewer runs *after* the Hwang-Lin run, reads the index via one extra mmap reload (no second filesystem walk, no hashing), and is TTY-only. Non-interactive cost unchanged.
- [x] Documentation — `--interactive-tree` help on both commands; CLAUDE.md Dependencies updated (Go floor 1.25.0, tcell + x/term added).
- [ ] **Release notes / CHANGELOG** — add a "feature: `--interactive-tree` post-run viewer" entry at tag time (not yet cut; deferred to the release, not this task).
- [x] Rollback plan ready (below) — trivial given opt-in + default-off.

## Rollout Considerations (CLI-specific)
The only non-trivial rollout impact is the **toolchain floor**, not user behaviour:
- **Go minimum raised 1.24.3 → 1.25.0** (tcell requires ≥1.25). Any CI/build/release environment must have Go ≥ 1.25 before this lands. The committed toolchain is `go1.26.4`, so local builds are unaffected; verify the release pipeline's Go version.
- **New dependencies**: `github.com/gdamore/tcell/v2` (+ transitive `gdamore/encoding`, `lucasb-eyer/go-colorful`, `rivo/uniseg`) and `golang.org/x/term`. `go.mod`/`go.sum` updated and `go mod verify` clean. Supply-chain note: tcell is the established library gdu itself uses.

## Rollback Plan
### Triggers
- A terminal-corruption or teardown regression reported on a real TTY (the highest-risk area).
- The Go-1.25 floor breaks a required build/release environment.
- Any escape-safety regression (a crafted filename reaching the terminal raw).

### Procedure (low effort — opt-in feature)
1. **Mitigate first**: because the flag is default-off, advise affected users to simply not pass `--interactive-tree`; nothing else changes for them. No data is at risk (the viewer is strictly read-only).
2. **Revert**: `git revert 40475b6 d835895` (testing then implementation commit), or drop the task-11 squash from `local-main`. This also reverts the go.mod floor and the tcell dependency.
3. **Toolchain-only issue**: if *only* the Go floor is the problem, the feature can be kept by reverting and re-pinning, but tcell hard-requires 1.25, so a full revert is the clean path.
4. **Analyse**: capture the failing terminal/OS and re-open via `/cwf-maintenance`.

## Success Criteria
- [x] Builds and ships without altering the non-interactive path (byte-identical index + stdout).
- [x] Opt-in flag verified inert on non-TTY/JSON; viewer read-only.
- [ ] Release pipeline confirmed on Go ≥ 1.25 (verify at tag time).
- [ ] CHANGELOG entry added at release.

## Status
**Status**: Finished
**Next Action**: /cwf-maintenance 11
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
*To be filled upon completion*

## Lessons Learned
*To be captured during implementation*
