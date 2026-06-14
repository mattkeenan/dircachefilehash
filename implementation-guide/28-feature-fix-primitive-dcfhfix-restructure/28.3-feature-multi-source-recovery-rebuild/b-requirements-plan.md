# Multi-source recovery rebuild - Requirements
**Task**: 28.3 (feature)

## Task Reference
- **Task ID**: internal-28.3
- **Task URL**: N/A (internal task)
- **Parent Task**: 28
- **Branch**: feature/28.3-multi-source-recovery-rebuild
- **Template Version**: 2.1

## Goal
Specify the multi-source recovery rebuild: how `main.idx` is reconstructed from a
merge of surviving index sources, driven through the `Repo.Fix` primitive, with
snapshot-protected atomicity. Concretises parent FR8 and the recovery slice of
NFR5/AC5 for this leaf subtask.

## Functional Requirements
### Core Features
- **FR1 — Multi-source merge**: a net-new `mergeSourcesIntoEntries` reads a set of
  resolved index refs, validates each through the lenient `ValidationProcessor`
  (`ValidationLenient` mode — not the deprecated `RecoveryValidationProcessor`
  wrapper), and produces one merged entry set keyed by relative path. A source with
  a readable header but truncated body contributes its readable validated
  **prefix**. *Net-new dependency note*: the existing entry loader
  (`collectEntryRefs`, `pkg/index.go:402`) hard-errors on a short read / size
  mismatch, so it yields **zero** entries for a truncated source, not a prefix.
  FR1's prefix behaviour therefore requires a **truncation-tolerant read path**
  that stops at the last fully-readable, validated entry — this is net-new code
  alongside `mergeSourcesIntoEntries`, not a reuse of the current loader.
  *Acceptance*: given main + cache + one timestamped cache, the merged set is the
  union of their validated entries by path; a truncated source contributes a
  concrete, asserted count.
- **FR2 — Deterministic precedence**: when the same relative path appears in more
  than one source, the conflict is resolved by a documented precedence —
  **timestamped caches (newest→oldest) > `cache.idx` > `main.idx`** (the
  cache-as-delta-over-main model). *Acceptance*: a fixture with the same path in two
  sources resolves to the higher-precedence source's entry, asserted by test;
  re-running yields the identical result (deterministic).
- **FR3 — Recovery rebuild as a `Fix` batch**: a `FixRequest` carrying multiple
  `IndexSelectors` plus a recovery operation rebuilds `main.idx`, surfaced through
  `Repo.Fix`. The merged set is written to `r.ms.IndexFile` via the single-writer
  path (`TempIndexWriter`/`EntrySerialiser`) and promoted by atomic rename.
  *Net-new dependency note*: `RunFix` today hard-rejects multi-ref input
  (`len(refs) != 1`, `pkg/fix_run.go:201`) and assumes the subject **is** the write
  target (`subject := refs[0].Path`). Recovery breaks both assumptions — it reads N
  source refs but writes one destination (`r.ms.IndexFile`) that is **not** one of
  the sources. So the recovery op must be a distinct, op-gated batch-level branch
  that lifts the single-ref guard, not a 10th per-subject command.
  *Acceptance*: a destroyed `main.idx` with an intact `cache.idx` yields a
  re-readable, checksum-valid `main.idx` after one `Repo.Fix` call.
- **FR4 — Snapshot precondition (hard)**: a pre-recovery snapshot of the existing
  `.idx` files is taken before any write; if the sources being rebuilt were **not**
  successfully snapshotted, the rebuild aborts with `r.ms.IndexFile` untouched.
  *Net-new dependency note*: `createPreRecoverySnapshot` (`pkg/recovery.go:350`) is
  **best-effort** — it swallows per-file copy errors (`continue // Non-fatal`) and
  returns `nil` even when nothing was copied, exposing no count to the caller. The
  recovery op must therefore wrap it with a fatal precondition (e.g. assert the
  relevant sources copied / read them back), not rely on its `error` return.
  *Acceptance*: with the snapshot of a required source forced to fail, `Fix` returns
  an error and the original `main.idx` is byte-unchanged.
- **FR5 — Empty guard (hard) + optional under-floor guard**: if the merged
  validated set is **empty**, the rebuild aborts **before** the rename, leaves all
  originals intact, and reports the discards — it never overwrites a recoverable
  index with a header-only/empty one. An **optional** stricter floor — merged count
  below a ratio of the pre-existing `main.idx`/`cache.idx` header `EntryCount` (read
  directly from those headers, since the snapshot exposes no count) — may also abort;
  if adopted in design it must be a concrete, tested threshold, not an implicit knob.
  *Acceptance*: a fixture whose sources all fail validation leaves `main.idx`
  unchanged and returns counts, not a truncated index.
- **FR6 — Result reporting**: `FixResult` reports repairs applied and entries
  discarded for the rebuild; truncated-source, failed-validation, and conflict-loser
  discards are all surfaced (not silently dropped). The three discard categories are
  counted over **disjoint** entry sets under a defined categorisation order (an entry
  is attributed to the first category that applies), so the total is well-defined.
  *Acceptance*: the discard count returned equals the number of entries dropped
  across the (disjoint) truncation + validation + conflict-resolution categories.

### User Stories
- **As a** user with a damaged repository **I want** `main.idx` rebuilt from whatever
  index sources survive **so that** I recover state without `rm -rf .dcfh && dcfh init`
  destroying forensic evidence.
- **As a** library consumer **I want** the recovery rebuild driven through the same
  `Repo.Fix` batch API as every other repair **so that** recovery is not a separate,
  CLI-only code path.

## Non-Functional Requirements
### Performance (NFR1)
- Recovery is a single pass bounded by total readable index size; no quadratic
  re-scan of sources, no repeated full re-validation of the same source.

### Usability (NFR2)
- The rebuild reuses the existing plural `IndexSelectors` vocabulary and `FixRequest`
  shape; the precedence rule (FR2) is documented where a consumer can find it.

### Maintainability (NFR3)
- The `ValidationProcessor` validators and the `createPreRecoverySnapshot` **copy**
  helper are reused (the snapshot is wrapped with a fatal precondition per FR4, not
  modified). The net-new code is bounded and reviewable: `mergeSourcesIntoEntries`,
  the truncation-tolerant source read (FR1), and one op-gated recovery branch in
  `RunFix` (FR3) carrying the snapshot precondition + empty/floor guard. The merge
  function is unit-testable in isolation from the write path.

### Security (NFR4)
- The write destination is always `r.ms.IndexFile` — never derived from a selector.
  A selector (including the multi-source set) resolving outside the resolved
  `MetaDir` is rejected **before** any write (D2 confinement). The recovery op is
  reachable **only** through `Repo.Fix` (which passes `MetaDir` as `writeRoot`); it
  must never be exposed via the unconfined `writeRoot==""` CLI exemption
  (`pkg/fix_run.go:221`). Migrated gosec rationales stay true; `golangci-lint run
  ./...` stays clean; the CWF `cwf-security-reviewer-changeset` verdict is recorded.

### Reliability (NFR5)
- All writes use a new temp index + atomic rename via the single `TempIndexWriter`
  path; no in-place mutation of `main.idx`/`cache.idx`/timestamped caches.
- Fault-injection coverage (Task 23 atomic-replacement harness model) proves no
  partial or corrupt `main.idx` can be left behind on interruption at any point in
  the rebuild.
- Corruption-path semantics are explicit (the discard accounting and "abort, do not
  emit a partial index" choice are specified in FR5/FR6).

## Constraints
- Single writer (`TempIndexWriter`); main/cache read-only mmap; temp pure-vectorio —
  all preserved. No new mmap-for-write path.
- No on-disk format change; produced `main.idx` satisfies the existing
  header/checksum/layout contract.
- No new third-party dependencies. British spelling in prose/comments.

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [ ] **Time**: >1 week? No — ~2–3 days.
- [ ] **People**: >2 people? No — solo.
- [ ] **Complexity**: 3+ distinct concerns? No — one concern (multi-source merge +
  atomic rebuild); FR1–FR6 are facets of the single recovery path.
- [x] **Risk**: high-risk component needing isolation? Yes — this is already the
  isolated data-destructive leaf the parent split out; the fault-injection gate is
  its containment, not a reason to split further.
- [ ] **Independence**: separable parts? No — merge → wire → prove is one chain.

**Result: 1 of 5 → no further decomposition** (carried from the task plan).

## Acceptance Criteria
- [ ] AC1: destroyed `main.idx` + intact `cache.idx` → re-readable, checksum-valid
  `main.idx` via one `Repo.Fix` call (FR1, FR3).
- [ ] AC2: same-path conflict across sources resolves by the documented precedence,
  asserted and deterministic (FR2).
- [ ] AC3: a truncated-body source yields a concrete asserted entry count via the
  truncation-tolerant read (readable prefix) with discards reported (FR1, FR6).
- [ ] AC4: a forced snapshot failure of a required source aborts before write
  (original byte-unchanged); an empty merged set aborts before rename with originals
  intact (FR4, FR5). *(The optional under-floor abort gets its own assertion only if
  design adopts a concrete floor.)*
- [ ] AC5: fault injection proves atomicity — interruption mid-rebuild leaves no
  partial/corrupt `main.idx` (NFR5).
- [ ] AC6: selector resolving outside `MetaDir` rejected before write; `golangci-lint
  run ./...` clean; CWF changeset security verdict recorded (NFR4).

## Status
**Status**: Finished
**Next Action**: /cwf-design-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All FRs/NFRs/ACs satisfied; the AC1↔AC4 tension (rebuild-from-survivors vs
snapshot-must-exist) resolved by verifying only *contributing* sources in the
readback. See j-retrospective.md.

## Lessons Learned
NFR4 confinement reused 28.2's `confineWriteDest` unchanged; the data-destructive
NFR5 path was contained by the layered empty→snapshot→atomic guard. See
j-retrospective.md.
</content>
