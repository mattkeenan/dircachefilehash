# Triage deferred gosec findings - Design
**Task**: 6 (discovery)

## Task Reference
- **Task ID**: internal-6
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: discovery/6-triage-deferred-gosec-findings
- **Template Version**: 2.1

## Goal
Define the method and artefacts for the triage: how ground truth is gathered (empirically,
not by assumption), where the inventory lives, the disposition decision procedure, and the
G304 trust-boundary framework.

## Design Priorities
Testability → Readability → Consistency → Simplicity → Reversibility

This is a discovery/audit task: the "architecture" is a measurement procedure plus a
documentation artefact, not runtime code. Reversibility matters most — every change
(config edit, comment correction, optional code guard) must be individually revertible.

## Key Decisions

### Decision 1 — Ground truth is gathered empirically, with the lightest sufficient mechanism
- **Decision**: Gather the authoritative `rule → file:line` dataset using two cheap,
  non-invasive moves rather than a tree-wide rewrite:
  1. **Surface exclude-hidden sites**: run golangci-lint with a scratch config copy in which
     the four blanket excludes (G103/G304/G401/G505) are removed. The sites currently relying
     on the *blanket exclude* (no `nolint` comment of their own) newly fire — this is exactly
     the historical "G304 ~27 sites" set. Capture golangci-lint JSON (`rule → file:line`).
  2. **Verify the suppressed sites' true rule**: the per-line-suppressed sites are already
     enumerable via `grep -rn "nolint:gosec" --include="*.go"` (≈62 production sites). Only the
     comments whose claimed rule is suspect need empirical confirmation — chiefly the four
     in-scope `G703` comments. Confirm each by removing *that specific* `nolint:gosec` comment
     in the scratch copy and re-running, observing the rule gosec actually emits.
- **Rationale**: The `G703` comments prove rationale text is unreliable (`G703` is not a gosec
  rule; affected sites span `os.Create`/`ReadFile` → G304 and `os.WriteFile(dst,…,mode)` → likely
  G306), so the true rule must come from the tool. But we do **not** need to re-emit every
  already-known suppressed site — grep already lists them. Targeting only the suspect comments
  avoids a fragile tree-wide `nolint` strip ("best part is no part").
- **Trade-offs**: Two small scratch edits (config minus excludes; remove ≤5 specific comments)
  instead of a sed pass over ~62 lines. Fewer mutations ⇒ fewer ways the scratch run diverges
  from reality. A full strip remains a documented fallback only if targeted removal proves
  insufficient.
- **Isolation & safety**: scratch edits happen in a disposable git **worktree outside the
  primary working tree** (so a stray `git add -A` in the real tree can never stage an
  excludes-removed config or a comment-stripped file); it is discarded after JSON capture.
  Follow the repo worktree/tmp-path conventions and avoid `git -C` (per project memory).
- **Failure guards**: trust the JSON only if the scratch run (a) exits without a
  tooling/execution error — lint findings are fine, a compile error or crash is not — and
  (b) the scratch tree still `go build ./...`s. The full gosec ruleset over the whole tree is
  slower than the `--new` gate, so raise the scratch run's `timeout` above the config's 3m.
- **Constraint honoured**: gosec is still invoked *via golangci-lint* (scratch run uses the
  same binary/config minus excludes) — no standalone `gosec` binary is introduced.

### Decision 2 — The inventory is a Markdown table in f-implementation-exec.md
- **Decision**: The triage inventory is a single Markdown table recorded in the exec doc
  (`f-implementation-exec.md`), one row per in-scope finding. Columns:
  `Rule | Site (file:line) | Construct | Path/data source (trusted|untrusted|n-a) | Current handling | Disposition | Rationale`.
- **Rationale**: NFR2 requires the inventory be auditable as a standalone artefact without
  re-running tooling. A table in the exec doc co-locates evidence with the work record and
  is what the retrospective/close-out cite. No separate file or tool needed ("best part is
  no part").
- **Trade-offs**: A static table can drift from code over time; mitigated because it is a
  point-in-time triage record, not a live index — its job is to justify the end-state, after
  which the in-code comments are the living documentation.

### Decision 3 — Disposition decision procedure (applied per inventory row)
A fixed decision order, ordered to test the no-churn outcomes first so a correct existing
suppression is not needlessly re-litigated:
1. **Is the rule a settled architectural exclude** (G103 unsafe/mmap, G401/G505 SHA-1)?
   → `exclude` (confirm rationale only; do not relitigate).
2. **Is it already correctly handled** — an existing per-line suppression whose *empirically
   confirmed* rule ID matches the comment **and** whose rationale is sound? → `accept`, no
   change. (A site that fires only because the scratch run stripped a *correct* suppression
   lands here, not in step 4.)
3. **Does the flagged construct represent a real defect** — an unguarded path from untrusted
   input, an over-permissive *constant* write mode, or a *variable* write mode laundered from
   untrusted source-file metadata (e.g. `os.WriteFile(dst, …, srcInfo.Mode())` copying perms
   from a scanned file)? → `fix`.
4. **Else, is it a true false positive with a site-specific reason**? → `suppress` with a
   per-line `//nolint:gosec // Gxxx: <reason>` using the *empirically confirmed* rule ID
   (this also covers correcting a `G703`/mislabelled comment to its real rule).
Every row gets exactly one of `{exclude, accept, fix, suppress}` plus a one-line rationale.

### Decision 4 — G304 trust-boundary framework
For every live G304 site, classify the *origin* of the path variable:
- **Trusted**: user-supplied CLI argument or the user's own environment variable
  (`DCFH_CPUPROFILE`/`DCFH_MEMPROFILE`, scan roots the user typed). dcfh runs with the
  invoking user's privileges against paths they already control → no trust boundary crossed.
- **Untrusted**: path derived from index-file content, a `.dcfhignore`, or network/wire
  input — anything an attacker could influence to point outside the intended root.
- **Rule**: trusted → `suppress` ("user-controlled path, no trust boundary"). Untrusted →
  `fix` (add/keep an escape guard) is the default; `suppress` is permitted **only** when an
  existing guard provably prevents escape **and that guard is cited** in the rationale (e.g.
  `RemoteHandler.resolveRel` → `hasPathPrefix` in `pkg/wire_handler.go`; exact line from the
  empirical run). A suppression on an untrusted site with no cited guard is itself the
  FR4-class defect this audit exists to catch and is not allowed.
- **Policy choice (G304 blanket exclude: keep vs convert)** — *final call deferred to exec
  once the live count is known*: any untrusted live site (e.g. the wire-handler read) **forces
  convert** — a blanket exclude cannot silence a real trust-boundary site. "Keep" is therefore
  valid only if *every* live G304 site is trusted-by-construction and numerous; otherwise
  **convert** to per-line suppressions (auditable, and catches future unguarded sites). Either
  way, untrusted sites carry a layered, guard-citing per-line suppression (FR3 precedence).

## System Design

### Component Overview
- **Scratch-run harness**: a disposable worktree with a `.golangci.yml` variant (4 excludes
  removed) and the suspect `nolint:gosec` comments (the in-scope `G703` set) removed, plus a
  golangci-lint JSON run. Surfaces exclude-hidden sites and confirms suspect rule IDs.
  Disposable; never committed.
- **Suppression census**: `grep -rn "nolint:gosec" --include="*.go"` over the real tree
  (minus `_test.go`) — the authoritative list of already-suppressed sites and their claimed IDs.
- **Inventory table**: the curated, dispositioned record in `f-implementation-exec.md`,
  built from the scratch JSON ∪ the suppression census.
- **Applied changes** (in the real tree): corrected suppression comments, any `fix` code
  guards, the final `.golangci.yml` exclude set, and the CLAUDE.md/​backlog updates.

### Data Flow
1. Baseline run on the real tree (current config) → confirm starting state is gosec-green.
2. Suppression census: grep existing `nolint:gosec` sites + claimed IDs.
3. Scratch worktree: remove 4 excludes + remove suspect comments → golangci-lint JSON
   (guarded: run exits clean, scratch tree still builds).
4. Merge JSON (exclude-hidden + rule-confirmed sites) with the census → inventory; classify
   each row via Decision 3, G304 rows via Decision 4.
5. Apply dispositions to the **real** tree (comment fixes, guards, config) — exclude removal
   and its per-line suppressions in the **same changeset** (NFR5 atomic landing).
6. Re-run baseline on the real tree → must be gosec-clean modulo documented excludes.
7. Update CLAUDE.md + `.golangci.yml` comments; retire backlog item.

## Interface Design

### Inventory table (columns + one worked example row)
Columns: `Rule | Site (file:line) | Construct | Source (trusted|untrusted|n-a) | Current (excluded|suppressed(<claimed-id>)|unhandled) | Disposition (exclude|accept|fix|suppress) | Rationale`.

| Rule | Site | Construct | Source | Current | Disposition | Rationale |
|------|------|-----------|--------|---------|-------------|-----------|
| G304 | pkg/wire_handler.go:&lt;n&gt; | `os.ReadFile(path)` | untrusted | excluded | suppress | escape-guarded by `resolveRel`→`hasPathPrefix`; cite guard |

(Static point-in-time record; no code consumes this schema — the in-code comments are the
living documentation after close-out.)

### Final `.golangci.yml` gosec contract
- `gosec.excludes`: the settled architectural set after Decision 4 (G103, G401, G505 always;
  G304 present only if the "keep" policy is chosen). Each entry keeps an inline comment.
- Per-line suppressions: `//nolint:gosec // Gxxx: <rationale>`, rule ID empirically confirmed.

## Constraints
- gosec only via golangci-lint; scratch run uses the same toolchain minus excludes.
- Scratch edits (config + comment removal) live in a disposable worktree **outside** the
  primary working tree so a stray `git add` cannot stage an excludes-removed config or a
  comment-stripped file; the real tree is never committed in that state. Follow repo
  worktree/tmp-path conventions; avoid `git -C`.
- Architectural excludes G103/G401/G505 are confirmed, not reopened.
- gosec scope only; no cyclop/unparam work (separate backlog item).
- Note: `convert-index-v1-to-v2.go:65` is one of the five `G703` comments tree-wide, but it
  is the out-of-scope root utility — only **four** `G703` sites are in-scope for disposition;
  the fifth is recorded in the inventory's out-of-scope list.

## Decomposition Check
- [ ] **Time**: >1 week? No.
- [ ] **People**: >2 people? No.
- [ ] **Complexity**: 3+ distinct concerns? Method + G304 policy + close-out are sequential,
      share one artefact; no parallel split warranted.
- [ ] **Risk**: High-risk components needing isolation? G304 analysis is the only risk;
      handled by Decision 4, no subtask needed.
- [ ] **Independence**: Parts separable? No — every disposition depends on the one inventory.

**Conclusion**: 0 strong signals; single task.

## Validation
- [ ] Scratch run completed without tooling/execution error and the scratch tree still builds
- [ ] Method produces a complete `rule → site` dataset (no site hidden by exclude or nolint)
- [ ] Disposition procedure assigns exactly one disposition per row
- [ ] G304 framework classifies every live site by path origin; untrusted sites are `fix` or
      guard-cited `suppress` (never bare suppress)
- [ ] Final config + comments are individually revertible

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-plan 6
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All four decisions executed. Decision 1 (empirical ground truth) caught the G703/G306 error
and the env-site mislabel — its whole purpose. Decision 4 resolved to CONVERT (an untrusted
live site exists at hash.go via the wire path).

## Lessons Learned
The scratch-worktree mechanism was sound, but `cd`-ing into it from the primary session was
the hazard: edits/lint/tests ran in the worktree and were lost on removal. See j-retrospective.md.
