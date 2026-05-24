# Adopt full Go pre-commit hook and security review - Implementation Plan
**Task**: 2 (chore)

## Task Reference
- **Task ID**: internal-2
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/2-adopt-full-go-pre-commit-hook-and-security-review
- **Template Version**: 2.1

## Goal
Enable `gosec` in the existing `.golangci.yml` using a **precise** suppression strategy (keep rules active; suppress only proven false positives, each with a rationale), so gosec contributes **zero** findings to `golangci-lint run ./...` while still guarding new code. Document adoption of the CWF security-review phase.

## Workflow
Patterns first → measure via the real tool → minimal, justified suppressions → verify end-state → document → commit explains "why"

## CRITICAL measurement note (from plan review)
Measure gosec **through golangci-lint**, not standalone `gosec`. Setting `gosec.excludes` activates gosec's full ruleset, so the firing set is **13 rules** (G103, G108, G114, G115, G204, G301, G302, G304, G306, G401, G505, G602, G703). Verify against the **end-state** config and reconcile by **total gosec count → 0**.

## ⚠️ Exec correction (see f-implementation-exec.md)
The tables below were measured under golangci-lint's default `max-same-issues: 3`, which hid duplicate findings. The **real** firing set is **26 production sites** (not ~12), and exec additionally lifts the issue cap (`issues.max-same-issues: 0`). Notably G204 fires in **production** (`pkg/wire_*.go`, ssh transport), not only tests. The corrected, authoritative disposition is in `f-implementation-exec.md` § "Final disposition". The *strategy* (precise per-line suppression, rules stay active, gosec → 0) is unchanged.

## Code-level disposition (every finding read, not just tool-trusted)
Only one genuine bug exists (G115 truncation, already filed Very High). Everything else is intentional, a false positive, or low-risk — so suppressions are precise and documented, not blanket.

### A. Permanent rule excludes — architectural / intentional (`gosec.excludes`)
| Rule | Why |
|---|---|
| G103 | `unsafe` is the zero-copy/mmap design |
| G304 | file-path-from-variable — dcfh is a file scanner |
| G401, G505 | SHA-1 for git-compatible content addressing |

### B. Deferred bug exclude (`gosec.excludes`, backlog-tracked)
| Rule | Why |
|---|---|
| G115 | inode/device truncation → **Very High** backlog; re-enable once Dev/Ino widen to uint64 |

### C. Test-only false positives — `exclusions.rules` `{linters:[gosec], path: _test\.go}`
Covers G204 (×6, harnesses launching the built binary), G602 (×1, explicit bounds guard above the index), and the perm findings in `*_test.go` fixtures. Mirrors the existing `errorlint`/`cyclop` test exclusions.

### D. Production false positives — per-line `//nolint:gosec` with rationale
| Site | Rule | Rationale |
|---|---|---|
| `cmd/dcfh/dcfh.go:26,43` | G703 | path is a user's own `DCFH_*PROFILE` env var; no trust boundary |
| `convert-index-v1-to-v2.go:65,144` | G703 | CLI-arg paths to a one-off migration utility |
| `pkg/recovery.go:400`, `pkg/snapshot.go:448` | G703 | `dst = filepath.Join(dir, DirEntry.Name())`; `Name()` is a base name — no traversal |
| `cmd/dcfh/dcfh.go:9` | G108 | pprof handlers only reachable via the opt-in, `localhost`-only server |
| `cmd/dcfh/dcfh.go:20` | G114 | opt-in `localhost:6060` debug profiler, not a network endpoint |

### E. Production perms — `//nolint:gosec` now, revisit in the High backlog pass
| Site | Rule | Note |
|---|---|---|
| `pkg/binary_entry_index_file.go:87` | G302 | `.dcfh/` index file, non-secret (metadata+hashes) |
| `pkg/ignore.go:176`, `pkg/metastore.go:272`, `pkg/recovery.go:341` | G301 | `.dcfh/` dirs, non-secret |
| `pkg/snapshot.go:98` | G306 | `.dcfh/` snapshot file, non-secret |

Perms rules stay **active** (not excluded), so new over-permissive writes in production are still caught. The **High** backlog item is the deliberate later pass to re-confirm every suppression and decide whether to tighten `.dcfh/` perms to 0750/0600.

## Files to Modify
- `.golangci.yml` — add `gosec` to `linters.enable`; `gosec.excludes` = [G103, G304, G401, G505, G115]; add `{linters:[gosec], path: _test\.go}` to `exclusions.rules`.
- Go source (comment-only): add the documented `//nolint:gosec` lines from tables D and E (≈12 sites). No behavioural change.
- `CLAUDE.md` — "Security Review" subsection; keep the CWF `cwf-security-reviewer-changeset` agent distinct from the `/security-review` built-in.
- `.githooks/pre-commit` — unchanged (already runs golangci-lint).

## Pre-existing lint debt (discovered in review — NOT in scope)
Full `golangci-lint run ./...` is already red (masked by `--new`) on: `cmd/dcfhfind/main.go:455` cyclop, `pkg/filter_run.go:75` cyclop, `pkg/binary_entry_scan_test.go:200` unparam. Filed as a separate backlog item; full-tree/CI green is a follow-up.

## Success Criterion
- gosec contributes **zero** findings to `golangci-lint run ./...`; only the 3 pre-existing non-gosec issues remain.
- Every suppression carries a rationale; no rule is excluded that has a genuine production finding.
- `go test -race` and the rest of the hook still pass (nolint comments are behaviour-neutral).

## Implementation Steps
### Step 1: Setup & measure
- [ ] Confirm tools on PATH; add gosec + candidate config; measure `golangci-lint run ./...`, reconcile gosec total → 0

### Step 2: Config
- [ ] `gosec` in `linters.enable`; `gosec.excludes` = [G103, G304, G401, G505, G115] with commented permanent/deferred split
- [ ] `{linters:[gosec], path: _test\.go}` in `exclusions.rules`

### Step 3: Production nolints
- [ ] Add the documented `//nolint:gosec` lines from tables D and E; `gofmt` clean

### Step 4: Verify
- [ ] `golangci-lint run ./...` → zero `(gosec)` lines (only the 3 pre-existing remain)
- [ ] Pick a zero-finding active rule (candidate G404 `math/rand`); plant a snippet, confirm `golangci-lint run --new` fails, revert
- [ ] Full hook passes on clean tree (gofmt, go fix, gopls, golangci-lint, govulncheck, `go test -race`)

### Step 5: Document + first security pass
- [ ] Add "Security Review" subsection to `CLAUDE.md`
- [ ] Run `cwf-security-reviewer-changeset` against this task's diff
- [ ] Confirm backlog items intact (Very High truncation; High suppression-review pass; pre-existing-debt cleanup)

## Test Coverage / Validation Criteria
**See e-testing-plan.md**

## Scope Completion
Go source touched is comment-only (`//nolint` rationale). Genuine fix (G115 truncation) and the deliberate suppression-review pass remain backlog items per user decisions. Deferral documented.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-plan
**Blockers**: None identified

## Actual Results
Executed in f-implementation-exec.md. Real firing set was 26 production sites (cap had masked half); strategy (precise per-line suppression, rules stay active, gosec → 0) held unchanged. Added: issue-cap lift + go1.26.3 toolchain bump.

## Lessons Learned
- Plan review caught a measurement error (standalone `gosec` ≠ golangci-lint's bundled gosec; `excludes` activates the full ruleset). Then a per-finding code read showed all residuals bar G115 were FP/low-risk, which flipped the strategy from blanket-exclude to precise-suppress. Measure against the enforcement path; read the code before trusting a security linter's verdict.
