# Triage deferred gosec findings - Implementation Plan
**Task**: 6 (discovery)

## Task Reference
- **Task ID**: internal-6
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: discovery/6-triage-deferred-gosec-findings
- **Template Version**: 2.1

## Goal
Execute the triage method from the design: gather ground truth, build and disposition the
inventory, apply the resulting comment/config/code changes, and close out — leaving a
gosec-clean gate with no untriaged debt.

## Workflow
Measure (ground truth) → Disposition (inventory) → Apply (real tree) → Verify (clean gate) → Close out.

## Environment facts (verified at plan time)
- `golangci-lint` v2.11.2; JSON via `--output.json.path=stdout`. Rule ID is in each issue's
  `Text` (e.g. `"G304: ..."`); `FromLinter == "gosec"`.
- Setting `gosec.excludes` activates gosec's **full** ruleset — so the scratch config removes
  **only G304**, keeping G103/G401/G505 in the key (full ruleset stays on; settled
  unsafe/SHA-1 noise stays suppressed). G103/G401/G505 are dispositioned at *category* level
  (`exclude`, architectural — confirmed), not enumerated per-site.
- tmp-path / worktree conventions: `.cwf/docs/conventions/tmp-paths.md`. Avoid `git -C`.

## Files to Modify (real tree — exec phase)
### Primary Changes
- `f-implementation-exec.md` — the triage inventory table (the main deliverable).
- `.golangci.yml` — final `gosec.excludes` set + inline comments reflecting the G304
  keep/convert decision (FR3). If converting, G304 removed from excludes here, atomically
  with the per-line suppressions below.
- The four in-scope `G703` comments — **note all `G703` comments are currently dead** (gosec
  has no G703, so they suppress nothing; the 2 `os.Create` sites pass only via the blanket
  G304 exclude, the 2 `os.WriteFile` sites pass because gosec evidently does not flag their
  variable mode — Step 2 confirms). Disposition per the empirical rule:
  - `cmd/dcfh/dcfh.go:26`, `cmd/dcfh/dcfh.go:43` (`os.Create` of env-var path) — expect G304
    (trusted: user's own env var).
  - `pkg/recovery.go:414`, `pkg/snapshot.go:448` (`os.WriteFile(dst, …, srcInfo.Mode())`) —
    expect G306. The mode is laundered from a source file; **confirm the source is
    `.dcfh/`-internal** before suppressing (Decision 3 step 3) — do not rubber-stamp.

### Supporting Changes (conditional on inventory findings)
- Per-line `//nolint:gosec // G304: <reason>` at each live G304 site **if** Decision 4 lands
  on "convert" — added in the *same changeset* as the exclude removal (NFR5 atomic landing).
  On "keep", the 2 dead env-var `G703` comments are **deleted** (the exclude silences those
  sites), not rewritten — only "convert" requires corrected per-line G304 suppressions there.
- Per-site path-origin trace before classifying any G304 site (see Step 3): the guarded
  untrusted read is `hashOne` → `HashFileInterruptible(ctx, abs, …)` (~`pkg/wire_handler.go:184`),
  where `abs` comes from `resolveRel` (~:231) / `hasPathPrefix` (~:242). By contrast
  `loadHashCache`'s `os.ReadFile(h.cachePath)` (~:398) takes a *config*-sourced path set in
  `NewRemoteHandler` (~:55) that does **not** pass through `resolveRel` — so these two sites
  have different origins and must be dispositioned separately, not lumped as "the wire read".
- A code `fix` (escape guard) **only if** a genuinely untrusted G304 site is found to lack a
  provable guard. Evidence so far: the wire rel-path read is already guarded ⇒ guard-citing
  `suppress`; expect no new code.
- `CLAUDE.md` Security Review section — remove the stale G115 "a real deferred bug" wording
  (and the stale listing of G115 as an exclude); state the final exclude set + G304 policy.
- `BACKLOG.md`/`CHANGELOG.md` — retire the item via the backlog-manager helper (close-out).

## Implementation Steps
### Step 1: Baseline + census (no mutation of real tree)
- [ ] Run `golangci-lint run ./...` on the real tree; confirm gosec-green starting state.
- [ ] `grep -rn "nolint:gosec" --include="*.go" . | grep -v _test.go` → suppression census
      (site + claimed ID). **Record the live count from this command** (≈73 at plan time, but
      volatile — use the measured value, never a remembered figure) as the FR1 reconciliation
      baseline. Likewise re-derive the live G304 site count (the backlog's "27" is stale).

### Step 2: Scratch ground-truth run (isolated, disposable)
- [ ] Create a disposable git worktree **outside** the primary tree (tmp-paths convention).
- [ ] In the worktree: remove `G304` from `.golangci.yml gosec.excludes` (keep G103/G401/G505
      so the full ruleset stays on). Remove **only the two `os.WriteFile` `G703` comments**
      (`recovery.go:414`, `snapshot.go:448`) — the two `os.Create` sites and `convert-index:65`
      fire from the G304-exclude removal alone, so their comments need not be touched.
- [ ] Run `golangci-lint run --output.json.path=stdout ./... > /tmp/<scratch>/gosec.json`
      with a raised timeout (>3m). **Guard**: confirm the run exited without an execution
      error and `go build ./...` still succeeds in the worktree; if not, do not trust the JSON.
- [ ] Parse JSON: collect every `FromLinter==gosec` issue → `rule, file:line, text`.
      Confirm the real rule for `recovery.go:414`/`snapshot.go:448` (G306 or nothing) and that
      `convert-index:65` emits G304 (records the dead-comment fact even though out of scope).
- [ ] Discard the worktree.

### Step 3: Build + disposition inventory (in f-implementation-exec.md)
- [ ] Merge scratch JSON (G304 exclude-hidden sites + confirmed WriteFile rules) with the
      Step-1 census into the inventory table (design Decision 2 columns).
- [ ] **Per-site origin trace**: for each live G304 site, trace the path variable back to its
      actual source in code (CLI arg, env var, config field, index/wire content) — do **not**
      assume a guard covers it. Specifically distinguish the `resolveRel`-guarded rel-path
      read (`hashOne`/`HashFileInterruptible`) from the config-sourced `loadHashCache` read.
- [ ] Classify every row via Decision 3 (order: exclude → accept → fix → suppress); classify
      each G304 row trusted/untrusted via Decision 4 from the traced origin.
- [ ] Reconcile per-rule from the **measured** census: `emitted = fix + suppress + exclude +
      accept`; record category-level `exclude` rows for G103/G401/G505; name the out-of-scope
      `G703` (convert-index) and fixtures/test files in the inventory's out-of-scope list.

### Step 4: Decide G304 policy + apply changes (real tree, atomic)
- [ ] Decide keep vs convert per Decision 4 (any untrusted live site ⇒ convert).
- [ ] Fix the two `os.WriteFile` `G703` comments → confirmed ID (expect G306) with accurate
      rationale, regardless of the G304 decision (these are not G304-exclude-gated).
- [ ] Handle the two env-var `os.Create` `G703` comments per the decision: on **convert**,
      rewrite to per-line `//nolint:gosec // G304: …`; on **keep**, delete them (dead — the
      blanket exclude silences the sites).
- [ ] If converting: remove G304 from `.golangci.yml` and add per-line `//nolint:gosec`
      suppressions at every live G304 site **in one commit** (with the comment rewrites above).
- [ ] Apply any `fix` guards required for untrusted sites lacking a provable guard.

### Step 5: Verify clean gate
- [ ] `golangci-lint run ./...` → gosec-clean modulo documented excludes.
- [ ] `go test ./pkg/...` + cmd suites green (any `fix` must not regress; format unchanged).
- [ ] Stage the changeset and confirm the `.githooks/pre-commit` `--new` path passes.
- [ ] **Abort path** (reversibility, design priority): if the gate cannot be made clean, the
      Step-4 changeset is reverted as a single unit (it was authored to land atomically) and
      the failing site is re-triaged — never leave a half-applied state where G304 fires
      unsuppressed.

### Step 6: Documentation + close-out
- [ ] Update `CLAUDE.md` Security Review: drop stale G115 text; record final excludes + G304 policy.
- [ ] Update `.golangci.yml` inline comments to match.
- [ ] Retire backlog item:
      `.cwf/scripts/command-helpers/backlog-manager retire --exact-title='Triage deferred gosec findings (perms, subprocess, pprof, http timeout, G304 paths)' --task=6 --note='<disposition summary>'`.

## Code Changes
### Before (representative — `G703` mislabel)
```go
if err := os.WriteFile(dst, sourceData, srcInfo.Mode()); err != nil { //nolint:gosec // G703: dst = filepath.Join(dir, DirEntry.Name()); Name() is a base name — no traversal
```
### After (rule ID corrected to the empirically-confirmed rule; rationale matches the real rule)
```go
if err := os.WriteFile(dst, sourceData, srcInfo.Mode()); err != nil { //nolint:gosec // G306: mode copied from already-validated source file under .dcfh/; not over-permissive
```
(Exact rule ID and wording set by the Step-2 empirical result — illustrative only.)

## Test Coverage
This task is config/comment/doc-centric; the primary "test" is the clean-gate verification
(Step 5). Any `fix` code change carries the existing suite as its regression guard.
`e-testing-plan.md` expands the gates below into Given/When/Then cases.

## Validation Criteria (self-contained; `e` expands)
- [ ] Scratch run guarded-valid (exited clean, worktree still builds) — else JSON not trusted.
- [ ] Inventory per-rule reconciliation balances against the **measured** census count.
- [ ] Every live G304 site has a traced origin + trusted/untrusted classification; no untrusted
      site carries a bare (non-guard-citing) suppress.
- [ ] `golangci-lint run ./...` gosec-clean modulo documented excludes.
- [ ] `go test ./pkg/...` + cmd suites green; on-disk format and CLI behaviour unchanged.
- [ ] `--new` pre-commit passes on the changeset; exclude-removal + suppressions landed atomically.
- [ ] `grep` confirms the stale G115 "real deferred bug" text is gone from CLAUDE.md.
- [ ] Backlog item retired against task 6.

## Scope Completion
**IMPORTANT**: Complete all planned dispositions before marking Finished. If the inventory
surfaces a finding whose fix is genuinely larger than this task (e.g. a real traversal bug
needing design work), get user approval, descope explicitly, and create a follow-up task —
do not silently suppress it.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-plan 6
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All 6 steps executed. Step 2 scratch run yielded 21 G304 + 2 G703 (real). Step 4 applied 21
per-line G304 suppressions + 2 env G703→G304 corrections + G304 exclude removal, atomically in
commit 90ff0f2. Step 5 gate gosec-clean; Step 6 docs + backlog close-out done.

## Lessons Learned
The per-site origin trace mattered: hash.go:82/186 (guarded wire path) vs loadHashCache:398
(operator-cache) are distinct origins, dispositioned separately as the plan required.
See j-retrospective.md.
