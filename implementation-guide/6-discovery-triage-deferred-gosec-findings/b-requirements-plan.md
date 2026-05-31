# Triage deferred gosec findings - Requirements
**Task**: 6 (discovery)

## Task Reference
- **Task ID**: internal-6
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: discovery/6-triage-deferred-gosec-findings
- **Template Version**: 2.1

## Goal
Specify what a complete gosec triage must produce: a per-finding inventory with a
disposition for every blanket-exclude and per-line suppression, a resolved G304 policy,
and a gosec-clean gate — with no untriaged security debt left behind.

## Functional Requirements
### Core Features
- **FR1 — Ground-truth finding inventory**: Produce a table of every gosec finding the
  full ruleset reports against production code, enumerated by running `golangci-lint run ./...`
  with the four current excludes (G103/G304/G401/G505) temporarily lifted **and** with
  per-line `//nolint:gosec` directives temporarily disabled (e.g. `--no-config` override or
  a scratch run), so neither excludes nor suppressions hide a site. Each row records: rule
  ID, file:line, the construct flagged, and current handling (excluded / suppressed / unhandled).
  - **Scope definition**: "production code" = code that ships in a built binary —
    `pkg/**` (including the `wire_*`, `fsdedupe/`, `format/` trees) and
    `cmd/{dcfh,dcfhfind,dcfhfix}/**`. Explicitly *out of scope*: `*_test.go`,
    `cmd/dcfhfind/test-data/**` fixtures, and the one-off root utility
    `convert-index-v1-to-v2.go` — but each out-of-scope category is named in the inventory
    with a one-line "why excluded from triage" so the boundary is auditable, not silent.
  - **AC**: Every in-scope finding appears exactly once. Reconciliation is **per-rule**, not
    a single aggregate: for each rule ID, `emitted = fix + suppress + exclude + accept`. The
    suppression set (per-line `nolint:gosec`, summed tree-wide via `grep -rh … | wc -l`,
    excluding `_test.go`) and the lifted-exclude hit set are disjoint axes — the
    reconciliation states both and calls out any overlap (a `nolint` on an
    otherwise-excluded rule is dead and flagged as such).

- **FR2 — Disposition per finding**: Assign each inventory row exactly one disposition:
  (a) `fix` — change the code; (b) `suppress` — per-line `//nolint:gosec // Gxxx: <reason>`;
  (c) `exclude` — keep/add to `.golangci.yml gosec.excludes` as architectural; (d) `accept`
  — already correctly handled, no change. Each disposition carries a one-line rationale.
  - **AC**: No row is left undispositioned; every `fix` row names the concrete change.

- **FR3 — G304 policy decision**: Decide explicitly whether G304 (file-path-from-variable)
  stays a blanket exclude or is replaced by per-line suppressions at each genuine site.
  The decision enumerates every **live** G304 site (re-derived from the FR1 run — the
  backlog's "27 sites" is a historical Task-2 figure and is superseded), classifies each
  path's source as trusted (user CLI/env argument) or untrusted (derived from index-file
  content / network/wire input), and states the chosen policy with rationale.
  - **Named untrusted site (must be dispositioned explicitly)**: `pkg/wire_handler.go:398`
    `os.ReadFile(path)` is reachable from remote-supplied `paths` via `resolveScanRoots`.
    Its disposition must either be `fix`, or `suppress` citing the existing
    `resolveRel`/`hasPathPrefix` escape guard (`pkg/wire_handler.go:232`) as escape-proof
    rationale — not "dcfh is a file scanner". Env-var path sites
    (`cmd/dcfh/dcfh.go:26,43`, `DCFH_CPUPROFILE`/`DCFH_MEMPROFILE`) appear as their own rows
    with "user's own env var, no trust boundary" rationale.
  - **Precedence over FR2(c)**: if the blanket G304 exclude is retained, untrusted-input
    sites still require a layered per-line suppression with escape-proof rationale — a
    blanket exclude alone does **not** satisfy them (see NFR4).
  - **AC**: A reader can see, per live G304 site, why it is or is not a traversal risk; the
    chosen policy is recorded in both the design doc and `.golangci.yml` comments.

- **FR4 — Suppression-comment reconciliation**: Verify every existing production
  `//nolint:gosec` comment names the rule gosec actually emits for that line. Treat the
  five `G703` comments as a **defect class**, not a single typo: `G703` is not a real gosec
  rule, and the five sites map to *different* real rules — `os.Create`/`os.ReadFile` sites
  (`cmd/dcfh/dcfh.go:26,43`, `convert-index-v1-to-v2.go:65` if in scope) emit **G304**,
  whereas `os.WriteFile(dst, …, mode)` sites (`pkg/recovery.go:414`, `pkg/snapshot.go:448`)
  emit **G306**. The correct ID must be derived per-site from gosec's actual emission, never
  assumed to be one substitution.
  - **AC**: No retained suppression cites a rule ID gosec cannot emit (e.g. G703); each
    retained comment's rule ID matches the emitted rule ID; every correction is listed in
    the inventory with old→new ID.

- **FR5 — Clean gate**: After dispositions are applied, `golangci-lint run ./...` produces
  zero gosec findings that are not the result of a documented `exclude` disposition.
  - **AC**: A full-tree run is gosec-clean modulo documented excludes. (The `--new`
    staged-path pass and the atomic-landing requirement live in NFR5.)

- **FR6 — Documentation + backlog close-out**: Update CLAUDE.md's Security Review section
  and `.golangci.yml` comments to reflect the final policy, and retire the backlog item
  citing the inventory as evidence.
  - **AC**: CLAUDE.md no longer describes G115 as "a real deferred bug" (already fixed) and
    accurately lists the final exclude set + G304 policy; the backlog item is retired
    against task 6.

### User Stories
- **As a maintainer** I want every gosec exclude/suppression to carry a verified, accurate
  rationale **so that** the security gate is trustworthy rather than an accumulation of
  unreviewed silences.
- **As a future contributor** I want a single inventory documenting why each finding is
  handled the way it is **so that** I can add code without relitigating settled decisions
  or accidentally reintroducing a real risk.
- **As a security reviewer** I want G304's disposition justified per-site against a trust
  boundary **so that** "it's a file scanner" is not used to wave away a genuine traversal bug.

## Non-Functional Requirements
### Performance (NFR1)
- Not applicable: this task changes lint configuration, suppression comments, and docs.
  No runtime code path is altered for `fix`-dispositioned items unless a fix is purely a
  guard addition; any such fix must not regress existing benchmarks. No new performance
  targets are introduced.

### Usability (NFR2)
- The inventory must be readable as a standalone artefact (Markdown table in the design or
  exec doc) so a reviewer can audit dispositions without re-running tooling.
- Suppression comments follow the established `//nolint:gosec // Gxxx: <rationale>` form so
  the in-code experience is consistent with existing suppressions.

### Maintainability (NFR3)
- The final exclude set in `.golangci.yml` is minimal: a rule stays blanket-excluded only
  when per-line suppression would be impractical (many sites, identical architectural
  rationale). Prefer per-line suppression where site count is small and reasons differ.
- Each disposition's rationale is specific enough that a later reader need not re-derive it.

### Security (NFR4)
- The triage must not weaken the gate: no rule active today (G301/G302/G306 perms, G115,
  G204, etc.) may be moved to a blanket exclude as a shortcut.
- **Untrusted-input precedence** (the substantive security constraint): any G304 site whose
  path derives from untrusted input (index-file content, network/wire data) must be
  dispositioned `fix` or carry a per-line suppression whose rationale demonstrates the path
  cannot escape the intended root. This holds **even if** the G304 blanket exclude is
  retained — a blanket exclude does not discharge an untrusted-input site (see FR3).
- (The minimal-exclude-set and per-line-style requirements live in NFR3 and Constraints;
  not restated here.)

### Reliability (NFR5)
- Any `fix`-dispositioned code change must keep the full test suite green
  (`go test ./pkg/...` and the cmd suites) and must not alter on-disk index format or
  observable CLI behaviour.
- Config changes are validated by a clean `golangci-lint run ./...` before close-out, and
  the staged `--new` pre-commit path must pass on the changeset.
- **Atomic landing** (ordering hazard): if FR3 removes the G304 blanket exclude in favour of
  per-line suppressions, the exclude removal and *all* corresponding per-line suppressions
  must land in the same changeset, so no intermediate commit leaves the gate red.

## Constraints
- gosec runs **only** as a v2 linter inside golangci-lint; measurement uses
  `golangci-lint run ./...`, never a standalone `gosec` binary (the exclude key activates
  gosec's full ruleset — see CLAUDE.md Security Review).
- Permanent architectural excludes G103 (unsafe/mmap), G401/G505 (SHA-1 git-compat) are
  settled: confirm rationale only, do not relitigate or remove.
- Scope is gosec exclusively. Non-gosec lint debt (cyclop/unparam) is a separate backlog
  item and must not be folded in.
- British spelling in prose/comments; do not introduce a standalone gosec hook (config-driven
  linting only, per project convention).
- No secret, credential, or token handling is in scope; if the audit surfaces any such
  finding it is raised separately, never silently suppressed.

## Decomposition Check
- [ ] **Time**: >1 week? No.
- [ ] **People**: >2 people? No.
- [ ] **Complexity**: 3+ distinct concerns? The concerns (inventory, G304 policy, reconcile)
      are sequential and share one artefact; not independent enough to split.
- [ ] **Risk**: High-risk components needing isolation? G304 is the sole risk, contained
      within FR3.
- [ ] **Independence**: Parts separable? No — dispositions depend on the single inventory.

**Conclusion**: 0 strong signals; remains a single task.

## Acceptance Criteria
- [ ] AC1 (FR1): Complete in-scope finding inventory; per-rule reconciliation balances
      (`emitted = fix + suppress + exclude + accept`); out-of-scope categories named.
- [ ] AC2 (FR2): Every inventory row has exactly one disposition + rationale.
- [ ] AC3 (FR3): Every live G304 site has a trust-boundary classification; the
      `wire_handler.go:398` untrusted site is dispositioned explicitly; policy recorded.
- [ ] AC4 (FR4): No retained comment cites a non-emittable rule (no `G703`); all retained
      rule IDs match emitted IDs; corrections listed old→new.
- [ ] AC5 (FR5/NFR5): `golangci-lint run ./...` gosec-clean except documented excludes;
      `--new` pre-commit passes; any exclude-removal lands atomically with its suppressions.
- [ ] AC6a (FR6): `grep` of CLAUDE.md confirms the stale G115 "real deferred bug" wording is
      gone and the final exclude set + G304 policy are accurately described.
- [ ] AC6b (FR6): `.golangci.yml` comments reflect the final policy.
- [ ] AC6c (FR6): Backlog item retired against task 6 with the inventory cited as evidence.

## Status
**Status**: Finished
**Next Action**: /cwf-design-plan 6
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
FR1–FR6 satisfied. FR4 corrected empirically: G703 IS a real rule (taint path-traversal), so
the WriteFile sites are *accept*; the env-var os.Create sites were the real mislabels (→G304).
AC6c (backlog retirement) completed at retrospective.

## Lessons Learned
The "named untrusted site" (wire_handler.go) was correctly forced to a guard-citing
disposition; the requirement to classify per-site origin proved its worth. See j-retrospective.md.
