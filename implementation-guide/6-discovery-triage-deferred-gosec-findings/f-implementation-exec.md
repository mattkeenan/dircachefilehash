# Triage deferred gosec findings - Implementation Execution
**Task**: 6 (discovery)

## Task Reference
- **Task ID**: internal-6
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: discovery/6-triage-deferred-gosec-findings
- **Template Version**: 2.1

## Goal
Execute the triage method from d-implementation-plan.md: gather empirical ground truth,
build and disposition the inventory, apply comment/config/code changes atomically, and close
out with a gosec-clean gate carrying no untriaged debt.

## Environment (measured at exec time)
- `golangci-lint` v2.11.2 (gosec linter), Go 1.26.0 toolchain.
- Baseline real-tree `golangci-lint run ./...`: **gosec-clean** (only out-of-scope
  `cyclop: 2`, `unparam: 1` — tracked under a separate backlog item).
- Scratch ground truth via a disposable worktree under
  `/tmp/-home-matt-repo-dircachefilehash-task-6/scratch-wt` (G304 removed from excludes;
  G103/G401/G505 retained so the full ruleset stays on). Build guard satisfied after
  `go generate` produced the gitignored `constants_version.go`; JSON captured clean.

## KEY DEVIATIONS FROM PLAN (the empirical method earned its keep)
1. **G703 IS a real gosec rule.** Phases a–e assumed `G703` was not a gosec rule and that
   the `os.WriteFile(dst,…,Mode())` sites would emit **G306**. Empirically, gosec v2.11.2
   emits **`G703: Path traversal via taint analysis`** for `pkg/recovery.go:414` and
   `pkg/snapshot.go:448`. Their existing `//nolint:gosec // G703` comments therefore cite the
   **correct** rule → disposition **accept** (not "correct to G306"). The destination origin
   was verified, not rubber-stamped: `dst = filepath.Join(<managed .dcfh dir>, entry.Name())`
   where `entry.Name()` is a base name (recovery.go:375, snapshot.go:134) — no traversal.
2. **The mislabelled comments were the `os.Create` env-var sites**, not the WriteFile ones.
   `cmd/dcfh/dcfh.go:26,43` emit **G304** (confirmed by stripping their comment in the
   scratch run), yet their comments said `G703`. These were corrected G703→G304.
3. **G304 live-site count = 21** (blanket-exclude-hidden), confirming the backlog's "27" is a
   stale Task-2 figure.

## Inventory (FR1) — ground-truth findings, in-scope production code

Scope: `pkg/**` + `cmd/{dcfh,dcfhfind,dcfhfix}/**`, excluding `_test.go`, fixtures, and the
root one-off `convert-index-v1-to-v2.go` (out-of-scope list below).

### G304 — converted from blanket exclude to per-line suppress (21 sites)
| Rule | Site | Construct | Source | Was | Disposition | Rationale |
|------|------|-----------|--------|-----|-------------|-----------|
| G304 | cmd/dcfhfix/entry_append_remove.go:97 | `os.ReadFile(indexFile)` | trusted (CLI arg) | excluded | suppress | repair-tool path named on the command line |
| G304 | cmd/dcfhfix/entry_append_remove.go:153 | `os.ReadFile(indexFile)` | trusted (CLI arg) | excluded | suppress | as above |
| G304 | cmd/dcfhfix/entry_workflow_main.go:17 | `os.ReadFile(indexFile)` | trusted (CLI arg) | excluded | suppress | as above |
| G304 | cmd/dcfhfix/entry_workflow_main.go:65 | `os.Create(tmpIndexFile)` | trusted (CLI arg) | excluded | suppress | temp index beside the CLI-named index |
| G304 | cmd/dcfhfix/main.go:479 | `os.Open(filePath)` | trusted (CLI arg) | excluded | suppress | repair-tool CLI path |
| G304 | cmd/dcfhfix/main.go:998 | `os.Open(src)` | trusted (CLI arg) | excluded | suppress | repair-tool CLI path |
| G304 | cmd/dcfhfix/main.go:1004 | `os.Create(dst)` | trusted (CLI arg) | excluded | suppress | repair-tool CLI path |
| G304 | cmd/dcfhfix/main.go:1026 | `os.ReadFile(path)` | trusted (CLI arg) | excluded | suppress | repair-tool CLI path |
| G304 | cmd/dcfhfix/main.go:1443 | `os.ReadFile(indexFile)` | trusted (CLI arg) | excluded | suppress | repair-tool CLI path |
| G304 | cmd/dcfhfix/main.go:1521 | `os.Create(outputPath)` | trusted (CLI arg) | excluded | suppress | repair-tool CLI path |
| G304 | pkg/index.go:122 | `os.Open(indexPath)` | trusted (.dcfh) | excluded | suppress | repo's own index file (repo discovery) |
| G304 | pkg/index.go:312 | `os.Open(filePath)` | trusted (.dcfh) | excluded | suppress | repo's own index file |
| G304 | pkg/index.go:610 | `os.Open(filePath)` | trusted (.dcfh) | excluded | suppress | repo's own index file |
| G304 | pkg/recovery.go:408 | `os.ReadFile(src)` | trusted (.dcfh) | excluded | suppress | .dcfh-internal index/backup during recovery |
| G304 | pkg/snapshot.go:432 | `os.ReadFile(src)` | trusted (.dcfh) | excluded | suppress | .dcfh-internal snapshot file |
| G304 | pkg/snapshot.go:512 | `os.ReadFile(metadataPath)` | trusted (.dcfh) | excluded | suppress | .dcfh-internal snapshot metadata |
| G304 | pkg/snapshot.go:525 | `os.ReadFile(tagsPath)` | trusted (.dcfh) | excluded | suppress | .dcfh-internal snapshot tags |
| G304 | pkg/fsdedupe/fsdedupe_linux.go:90 | `os.OpenFile(samplePath, O_RDONLY\|O_NOFOLLOW)` | trusted (scan result) | excluded | suppress | user-owned scan candidate; O_NOFOLLOW blocks symlink swap |
| **G304** | **pkg/hash.go:82** | `os.Open(filePath)` | **untrusted-reachable (guarded)** | excluded | **suppress (guard-cited)** | wire caller `hashOne` gated by `resolveRel`→`hasPathPrefix` (wire_handler.go:231); other callers pass user scan paths |
| **G304** | **pkg/hash.go:186** | `os.Open(filePath)` | **untrusted-reachable (guarded)** | excluded | **suppress (guard-cited)** | as above (interruptible variant) |
| G304 | pkg/wire_handler.go:398 | `os.ReadFile(path)` | trusted (operator cache) | excluded | suppress | `loadHashCache` path = `resolveRemoteCachePath` (CLI root + XDG dir), not wire-derived |

### G304 — comment corrected G703→G304 (2 sites, FR4)
| Rule | Site | Construct | Source | Was | Disposition | Rationale |
|------|------|-----------|--------|-----|-------------|-----------|
| G304 | cmd/dcfh/dcfh.go:26 | `os.Create(cpuprofile)` | trusted (user env) | suppressed (`G703` mislabel) | suppress (ID fixed) | user-owned `DCFH_CPUPROFILE` env var; no trust boundary |
| G304 | cmd/dcfh/dcfh.go:43 | `os.Create(memprofile)` | trusted (user env) | suppressed (`G703` mislabel) | suppress (ID fixed) | user-owned `DCFH_MEMPROFILE` env var; no trust boundary |

### G703 — accept (2 sites; rule ID correct, rationale verified)
| Rule | Site | Construct | Source | Was | Disposition | Rationale |
|------|------|-----------|--------|-----|-------------|-----------|
| G703 | pkg/recovery.go:414 | `os.WriteFile(dst, …, Mode())` | trusted (.dcfh base-name dst) | suppressed (`G703`) | accept | `dst = Join(recoveryDir, entry.Name())`; base name → no traversal |
| G703 | pkg/snapshot.go:448 | `os.WriteFile(dst, …, Mode())` | trusted (.dcfh base-name dst) | suppressed (`G703`) | accept | `dst = Join(snapshotDir, file)`; base name → no traversal |

### Architectural category excludes — accept (confirmed, not relitigated)
| Rule | Construct | Disposition | Rationale |
|------|-----------|-------------|-----------|
| G103 | use of `unsafe` | exclude | zero-copy / mmap design (pkg/binary_entry.go) |
| G401 | SHA-1 hashing | exclude | git-compatible content addressing |
| G505 | `crypto/sha1` import | exclude | same git-compatibility rationale |

### Pre-existing per-line suppressions — accept (rule IDs confirmed by the clean gate)
No change. Census (production, tree-wide, excl. `_test.go`): **G115×48, G301×7, G306×6,
G302×3, G204×2, G114×1, G108×1**. Each suppresses an **active** rule, so a wrong ID would let
the real rule fire — the gosec-clean baseline proves every ID matches its emitted rule.

### Out of scope (named per FR1)
- `convert-index-v1-to-v2.go:65` — root one-off migration utility (not shipped in a binary).
  Its `//nolint:gosec // G703` on `os.ReadFile(inputFile)` is itself a **mislabel** (real rule
  G304, as for the env-var `os.Create` sites), left untouched per scope. Its blanket
  `//nolint:gosec` suppresses the line regardless of ID, so it does not affect the gate.
- `*_test.go`, `cmd/dcfhfind/test-data/**` — gosec scoped off for tests via
  `exclusions.rules`.

### Per-rule reconciliation (FR1 AC)
- **G304**: 23 live emissions (21 blanket-hidden + 2 env comment-masked) → `suppress = 23`,
  `exclude = 0`, `accept = 0`, `fix = 0`. Blanket exclude removed; balances.
- **G703**: 2 in-scope emissions → `accept = 2`. (convert-index out-of-scope.)
- **G103/G401/G505**: category `exclude` (architectural).
- **G115/G301/G306/G302/G204/G114/G108**: `accept` (pre-existing, IDs confirmed clean).

## G304 policy decision (FR3 / design Decision 4): CONVERT
At least one **untrusted-reachable** live G304 site exists — `pkg/hash.go:82/186`, reachable
from wire-supplied `req.Paths[i]` via `RemoteHandler.hashOne`. Per Decision 4, an untrusted
live site forces **convert**: a blanket exclude must not silence a trust-boundary site. The
exclude was removed and all 21 sites carry per-line suppressions; the two untrusted-reachable
opens cite the `resolveRel`→`hasPathPrefix` escape guard (wire_handler.go:231–239), which
rejects any path escaping the audit root. No bare suppress sits on an untrusted site (NFR4).
No code `fix` was required — the guard already exists and is now explicitly cited.

## Actual Results (by plan step)
### Step 1 — Baseline + census
- **Actual**: gosec-clean baseline confirmed. Census recorded above (~70 in-scope
  production `nolint:gosec`). G304 live count 21 (backlog's 27 stale).
### Step 2 — Scratch ground-truth run
- **Actual**: worktree created; G304 removed from excludes; 2 `os.WriteFile` comments removed
  → JSON showed **21 G304 + 2 G703**. A second scratch run also stripping the env `os.Create`
  comments confirmed those emit **G304**. Build guard passed (after `go generate`). Worktree
  discarded.
### Step 3 — Build + disposition inventory
- **Actual**: inventory above; per-site origin traced (esp. the `hashOne`/`resolveRel`
  guarded path vs the operator-cache `loadHashCache` read — confirmed distinct origins).
### Step 4 — Decide policy + apply (atomic)
- **Actual**: CONVERT. 21 per-line G304 suppressions added; 2 env comments corrected
  G703→G304; G304 removed from `.golangci.yml`; 2 G703 WriteFile comments kept (accept).
  All in one working changeset (uncommitted until this checkpoint) — lands atomically.
- **Deviation**: while editing, the two G703 WriteFile comments were transiently removed from
  the real tree and then restored verbatim (net zero on those lines); a `sed` substitution on
  the env-var comments stripped rather than replaced, so they were re-added cleanly. Verified
  final state by `grep`/`git diff`.
### Step 5 — Clean gate
- **Actual**: `golangci-lint run ./...` → **0 gosec findings** (only out-of-scope
  cyclop/unparam remain). `go build ./...` clean. `go test ./pkg/...` **green**.
  `go test ./cmd/...`: `cmd/dcfh` green; `cmd/dcfhfind` + `cmd/dcfhfix` show 2 failures
  (`dcfhfind executable not found`, `could not find .dcfh directory`) — **verified
  pre-existing on clean HEAD** via `git stash`; environmental, not regressions (changes are
  comment-only + one YAML line, cannot alter runtime).
### Step 6 — Documentation + close-out
- **Actual**: `.golangci.yml` excludes block documents the G304 conversion;
  `CLAUDE.md` Security Review section updated — stale G115 "real deferred bug" text removed,
  final exclude set (G103/G401/G505) + G304 conversion + G703 note recorded.
- **Deferred to user review**: backlog-item retirement (`backlog-manager retire`) — held until
  the user reviews this exec, per their request.

## Blockers Encountered
None. Two cmd test failures investigated and confirmed pre-existing/environmental.

## Deferral Check
- [x] All steps from d-implementation-plan.md executed (backlog retire intentionally held for
      user review — flagged, not silently dropped).
- [x] a-task-plan.md success criteria met (every exclude/suppression dispositioned; G304
      decided; comments reconciled; gate clean).
- [x] b-requirements FR1–FR6 addressed (FR4 corrected per empirical G703 finding).
- [x] c-design Decisions 1–4 followed.
- [x] No planned work deferred without surfacing it.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-exec 6
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Lessons Learned
- The empirical-ground-truth method was the whole point and it paid off: the plan's central
  FR4 premise ("G703 is not a real gosec rule") was wrong. Trusting the tool over remembered
  rule semantics flipped two dispositions correctly.
- `os.WriteFile` with a taint-tracked destination emits **G703** (taint path-traversal);
  `os.Open`/`ReadFile`/`Create` with a variable path emit **G304**. The construct predicts
  the rule.

## Security Review

**State**: no findings

no findings: empty changeset

(The `security-review-changeset` helper reviewed 0 files: this task touched only Go
application source and `.golangci.yml`, none of which fall under the CWF security-relevant
pathspec — CWF tooling/scripts/skills with shebang or CWF-internal prefixes. The substantive
security analysis for this task is the G304 trust-boundary classification recorded in the
inventory above; the always-on gosec gate is itself the syntactic floor and is clean.)
