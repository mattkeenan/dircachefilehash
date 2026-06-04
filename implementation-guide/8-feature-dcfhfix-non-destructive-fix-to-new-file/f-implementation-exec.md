# dcfhfix non-destructive fix-to-new-file - Implementation Execution
**Task**: 8 (feature)

## Task Reference
- **Task ID**: internal-8
- **Branch**: feature/8-dcfhfix-non-destructive-fix-to-new-file
- **Template Version**: 2.1

## Goal
Execute d-implementation-plan.md: one preservation helper threaded into the four
rename sites, the force-gated `--edit-in-place` flag, dry-run reporting, docs/help.

## Actual Results (by plan step)

### Step 1-4: `promote.go` helpers (NEW)
- **Done**. `siblingPreFixPath` (pure, `…​.pre-fix-<UTC 20060102T150405Z>`),
  `preserveOriginal`, `validateEditInPlaceGate`, `promoteRepairedIndex`.
- **Deviation (substantive)**: `preserveOriginal` does **not** treat every
  `EEXIST` as "advance counter" (c-design KD4 / d-plan step 2 literal wording).
  On `EEXIST` it `Lstat`s the occupant: a **regular file** → advance to the next
  numbered candidate (sub-second re-run, prior preserved copy); a **symlink /
  directory / other non-regular** → **hard refusal, no write**. This reconciles a
  conflict between c-design (pure O_EXCL advance) and b-requirements FR2/D4 +
  e-testing TC-U3/U4 (which mandate a *refusal* on non-regular destinations).
  The requirements win. TOCTOU-safe: the `Lstat` only decides error-vs-advance;
  every write is still an `O_EXCL` create on a candidate path, never a
  stat-then-non-exclusive-open.

### Step 5: flag + gate wiring (`main.go`)
- **Done**. `--edit-in-place` defined next to `--force`; `validateEditInPlaceGate`
  called once at the top of `dispatchCommand`. Lone `--edit-in-place` (no
  `--force`) is refused for every subcommand incl. read-only — intentional, per
  plan.

### Step 6: four rename sites → `promoteRepairedIndex`
- **Done**. `entry_workflow_main.go` (entry edit), `entry_append_remove.go`
  (append, remove), and `writeIndexWithModifiedHeader` (header edit). The header
  site's `_ *ParsedOptions` un-blanked to `options` and threaded through. Each
  site keeps its existing error wrapping.

### Step 7: dry-run reporting (FR6)
- **Done**. New `reportDryRunPreservation` helper called in all four dry-run
  print branches. Default → "Would preserve original as a `.pre-fix-<timestamp>`
  sibling …" (obeys `--quiet`); `--edit-in-place` → destructive warning to stderr.
  JSON stubs (header/entry edit json) untouched — they never reach a rename.

### Step 8: docs/help (FR7, AC5)
- **Done**. Removed stale `entry resort` advertising (usage string, help line,
  example — `resort` has no handler, falls through to "unknown entry
  subcommand"). Added `--edit-in-place` to Options help and a "Non-destructive by
  default" Safety Features entry; updated `DESIGN.md` Safety Features + Entry
  Commands. Verified rendered `dcfhfix --help` shows the new lines and no
  `resort` (source grep + generated-output grep both clean).

### Step 9: tests
- **Done (exec scope)**: `promote_test.go` — TC-U1…U7 plus direct
  `promoteRepairedIndex` default-preserve / in-place-suppress tests.
  `promote_integration_test.go` — the **entry remove** write path end-to-end
  (default preserves byte-identical sibling; `--edit-in-place` suppresses;
  `--dry-run` writes nothing; dispatch gate refuses lone `--edit-in-place`).
- **Test note**: added a `withStableSecond` retry helper so the symlink/dir/
  EEXIST cases are robust against the second-resolution timestamp name crossing a
  UTC second boundary mid-test.
- **Scoping**: the per-path integration matrix for the *other three* paths
  (header edit, entry edit, append) is g-testing-exec's job (e-testing-plan
  TC-I1…I7). All four funnel through the identical `promoteRepairedIndex`
  boundary, which is directly unit-tested, so the residual per-path risk is low.

### Step 10: validate
- `go build ./...` clean. `go test ./...` green. `golangci-lint run ./...` →
  **0 issues** (gosec gate passed).
- **gosec note**: the anticipated **G703** did not fire. The only suppression
  needed on the new `O_EXCL` open was `//nolint:gosec // G304/G302` (path from
  variable + 0644 mode), matching the in-file precedents.

## Deferral Check
- [x] All d-plan steps executed.
- [x] a-task-plan success criteria met (default preserves + new-file semantics
  via visible sibling; force-gated in-place; messaging; docs; tests).
- [x] b-requirements FR1-FR7 / AC1-AC7 addressed (AC1/AC2/AC4/AC5/AC6/AC7 covered
  in exec; full per-path AC1 matrix completes in g-testing-exec).
- [x] c-design followed, with the documented KD3/KD4 reconciliation above.
- [x] No silent deferral: remaining work is the e-testing-plan matrix owned by
  g-testing-exec; no new follow-up task required.

## Status
**Status**: Finished
**Next Action**: /cwf-testing-exec
**Blockers**: None identified

## Security Review

**State**: no findings

Note: the `security-review-changeset` helper emits an empty changeset for this
task — its classifier only includes CWF-internal-prefix + shebang-script paths,
and this diff is entirely application Go under `cmd/dcfhfix/`. Rather than record
the procedural empty-changeset no-findings, the `cwf-security-reviewer-changeset`
agent was run on the actual `cmd/dcfhfix` diff (anchor 4598a81 → worktree). gosec
remains the always-on syntactic floor (`golangci-lint run ./...` → 0 issues, with
rationale-bearing `//nolint:gosec // G304/G302` on the new `O_EXCL` open). Verbatim
agent output below.

I have everything I need. Let me reason through the FR4 threat categories.

### Review

The changeset adds `cmd/dcfhfix/promote.go` and routes all four index-replacement write-back sites (`entry_append_remove.go` ×2, `entry_workflow_main.go`, and `writeIndexWithModifiedHeader` in `main.go`) through the new `promoteRepairedIndex`. The default behaviour copies the pre-repair index to a timestamped `<index>.pre-fix-<UTC>` sibling before the atomic rename; `--edit-in-place` (gated on `--force`) skips preservation. I reviewed the full file at `cmd/dcfhfix/promote.go` and the wiring sites against the actual sources, not just the diff.

**(a) Injection (command/path/format).** No shell, `exec`, SQL, or template surface is introduced. The sibling path is built by string concatenation from `indexFile` plus a `time.Format` constant and an integer suffix — no metacharacter interpretation, no `filepath` traversal logic that could be subverted. `indexFile` originates from `args[0]` and is resolved upstream by `ResolveIndexFile` (main.go:81) before any promote code runs. The `fmt.Errorf`/`Fprintf` calls use literal format strings with `%q`/`%s` arguments, so attacker-controlled paths cannot smuggle format directives. No concern.

**(b) Secrets / sensitive data.** No credentials, tokens, or env-derived secrets touch this code. The preserved sibling is created `0644`, consistent with the repo's documented stance that `.dcfh/` index files are non-secret metadata + SHA-1 hashes (the `//nolint:gosec // G302` rationale matches CLAUDE.md's perms policy). No secret is logged; the printed messages contain only the user-supplied path. No concern.

**(c) Auth / access control.** Not an auth surface. The one access-control-adjacent decision — the `--edit-in-place` destructive opt-in — is gated correctly: `validateEditInPlaceGate` is invoked once at the dispatch chokepoint (main.go:110), before routing, so a lone `--edit-in-place` without `--force` is refused for every subcommand including read-only ones. The gate is pure and the failure path returns an error rather than proceeding. No bypass: every production write-back goes through `promoteRepairedIndex`, which re-reads the same `edit-in-place`/`force` options. No concern.

**(d) Env-var / config handling.** No environment variables or external config are read in the new code. Behaviour is driven entirely by parsed CLI options. No concern.

**(e) Prompt-injection / pattern-reuse surface.** No LLM/agent surface. The relevant pattern-level observation is the file-creation safety of `preserveOriginal`, and it is handled correctly for this tool:

- The destination is opened with `os.O_WRONLY|os.O_CREATE|os.O_EXCL`, so a pre-planted symlink, directory, or existing file at the candidate path returns `EEXIST` rather than being followed or truncated. This closes the classic "write through a planted symlink" hazard in a shared/predictable directory — the `.dcfh/` location and timestamp make the sibling name guessable.
- On `EEXIST`, the `os.Lstat` classification is *not* a TOCTOU hole: it only decides advance-vs-refuse, and the only actual writes happen via a fresh `O_EXCL` open on a *different* candidate. A regular file advances to the next numbered candidate; anything else is refused. This is the right ordering (never Stat-then-open the path it will write).
- The collision loop is bounded (`maxPreFixCollisionSuffix = 100`) with a hard refusal at exhaustion — no unbounded retry / resource-exhaustion path.
- Partial-write handling is sound: copy/sync/close failures each `os.Remove` the partial sibling and return before the rename, so on any preservation failure the canonical `indexFile` is left untouched (the rename is skipped). This preserves the "main index only replaced on complete success" invariant from CLAUDE.md.

One bounded, non-blocking observation worth recording for future reuse rather than this callsite: `preserveOriginal` is safe here because the destination sits in the same directory as a CLI-named index inside a repair tool with no trust boundary (the operator already controls `indexFile`). The `O_EXCL` + `Lstat`-classify pattern is the correct template; if this helper is ever lifted to a context where the *parent directory* is attacker-writable but the file is trusted, note that `O_EXCL` protects the final component only — an attacker-controlled intermediate directory component could still redirect via a swapped directory symlink. That does not apply here (path is operator-supplied and resolved by `ResolveIndexFile`), so it is not an actionable finding; audit future reuse where the directory is not operator-controlled.

The `//nolint:gosec` suppressions on the two `os.Open`/`os.OpenFile` calls carry accurate, specific rationales consistent with the repo's documented G304/G302 policy, and the underlying calls are in fact safe as argued. No silent blanket exclusion was introduced.

Nothing in the diff is actionable as a security defect. The diff is clean.

```cwf-review
state: no findings
summary: promote.go preservation path is sound — O_EXCL refuses planted siblings, Lstat-classify is not a TOCTOU hole, edit-in-place gated on --force at a single chokepoint, all four write-back sites rerouted, no injection/secret/env surface.
```

## Lessons Learned
- c-design and e-testing disagreed on EEXIST-vs-non-regular handling; the
  requirements (FR2/D4) were the tie-breaker. Worth catching such cross-phase
  conflicts during plan review rather than at exec.
