# Byte-weighted default sort for interactive-tree - Requirements
**Task**: 12 (feature)

## Task Reference
- **Task ID**: internal-12
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/12-byte-weighted-default-sort-for-interactive-tree
- **Template Version**: 2.1

## Goal
Specify a byte-weighted change sort (`change_bytes`) as the new default
for the interactive-tree viewer — counting **added + modified + deleted**
bytes — the rename of the existing count sort to `change_files`, the
data-model additions that make per-node changed-byte sums available on
both the `status` and `update` paths, and the verification that no second
filesystem walk and no non-interactive regression are introduced.

**Scope decision (confirmed with user at requirements review)**:
`change_bytes` **includes deleted bytes**. This requires new plumbing —
per-category byte fields on `dcfh.Stats`, a per-deleted-path byte payload
on `dcfh.ChangeSet`, and capture of the left (pre-change) entry size in
the update comparison pass before the atomic rename discards it. The
alternative (added+modified only, zero plumbing) was rejected so that a
large deletion ranks as a large change.

## Functional Requirements
### Core Features
- **FR1 — `change_bytes` metric**: a node's `change_bytes` value is the
  sum of `AddedBytes + ModifiedBytes + DeletedBytes` over its subtree.
  - *AC*: `metric(node, sortChangeBytes)` returns that sum as `int64`; a
    directory's value equals the sum of its children's values.
- **FR2 — per-category byte fields exist on the tree (new data model)**:
  `dcfh.Stats` gains `AddedBytes`, `ModifiedBytes`, `DeletedBytes`
  (`int64`), aggregated up every directory beside the existing counts.
  Added/Modified bytes are the file's current size (live, from the merged
  index); Deleted bytes are the last-known size (see FR3).
  - *AC*: `leafStats` populates the matching byte field per category;
    `aggregate` sums all three; the existing live `Bytes` semantics
    (deleted excluded from `Files`/`Bytes`) are unchanged.
- **FR3 — deleted-byte data reaches the tree on both paths, consistently**:
  the last-known size of each deleted file is attributed to its node
  identically whether the viewer was launched from `status` or `update`.
  - `status`: the deleted entry survives in the merged/cache index with
    its `FileSize`; the builder reads it.
  - `update` (full): the entry is gone after the atomic rename, so the
    comparison goroutine captures the left entry's size at
    `OnLeftOnly`/`OnMatch` and threads it through `ChangeSet` as a
    per-deleted-path byte payload; the builder uses that for synthesised
    deleted nodes.
  - *AC*: a fixture deleting a known-size file yields the same
    `Stats.DeletedBytes` (and the same `change_bytes` ordering) under the
    status change-set and the update change-set.
- **FR4 — `ChangeSet` carries deleted sizes**: `dcfh.ChangeSet` gains a
  carrier for per-deleted-path byte sizes (e.g. a `DeletedBytes
  map[string]int64` companion to `Deleted`), populated by both call sites
  (`cmd/dcfh/status.go`, `cmd/dcfh/update.go`).
  - *AC*: both call sites populate the carrier; an absent/zero entry
    degrades gracefully to a size-0 deleted node (no panic, no nil-deref).
- **FR5 — `change_bytes` is the default**: a freshly opened viewer sorts
  by `change_bytes` descending.
  - *AC*: the header shows `sort:change_bytes(desc)` before any key press;
    `newModel(...)` initialises `sortKey = sortChangeBytes`.
- **FR6 — `change` renamed to `change_files`**: the existing count metric
  (Added+Modified+Deleted **counts**) is renamed `change_files`.
  - *AC*: `sortChangeFiles.label()` == `"change_files"` and
    `sortChangeBytes.label()` == `"change_bytes"`; no rendered label,
    header, footer legend, or help string contains the bare word
    `change` as a metric name. (Assertion is over `label()` output and
    rendered header/footer text, NOT a raw source grep — the identifiers
    `sortChange*` legitimately contain the substring.)
- **FR7 — runtime key map**: `c` selects `change_bytes`, `f` selects
  `change_files`; `a`/`m`/`d` select added/modified/deleted, `n` name,
  `r` reverse — all live, no data re-read.
  - *AC*: `keyForRune('c')` → `sortChangeBytes`; `keyForRune('f')` →
    `sortChangeFiles`; injecting each key in a SimulationScreen re-orders
    siblings without any `pkg`/index/filesystem access.
- **FR8 — header reflects direction**: pressing `r` flips the header's
  direction indicator for the active metric.
  - *AC*: after `r` the header reads `(asc)`; pressing `r` again → `(desc)`.
  (Static label content is covered by FR5/FR6.)
- **FR9 — no second filesystem walk**: `change_bytes` is computed from the
  already-loaded merged index (live sizes) plus the in-memory change-set
  (deleted sizes captured in-band by the existing comparison pass — NOT a
  re-stat of the now-absent path). No extra walk or stat.
  - *AC*: `PostRunTree`/`BuildTree` perform no filesystem access (same
    no-walk seam as task 11 TC-12), proven by test.

### User Stories
- **As a** user running `dcfh status --interactive-tree` on a large tree
  **I want** directories ordered by how many *bytes* changed (including
  large deletions) **so that** the heaviest changes surface first
  regardless of file count.
- **As a** user who preferred the file-count ordering **I want** a single
  key (`f`) to switch to `change_files` **so that** I keep the old view.

## Non-Functional Requirements
### Performance (NFR1)
- Byte aggregation is O(nodes) over the already-built tree; live re-sort
  stays a pure render-layer operation with no re-read (as task 11). No
  additional filesystem access (FR9 owns the testable no-walk AC).

### Usability (NFR2)
- The default answers "where did the most change happen by volume"
  (deletions included) without a key press; the `c`/`f` split is
  discoverable via the footer legend and `--help`. British spelling.

### Maintainability (NFR3)
- Reuses the existing `metric()`/`nodeLess()`/`sortNodes` comparator
  machinery; new byte sums live on `dcfh.Stats` beside the counts. The
  pure tree builder stays terminal- and filesystem-free, unit-testable
  with `treeEntry`/`ChangeSet` literals.

### Security (NFR4)
- No new untrusted-input path: sizes are `int64` from the index/change-set,
  rendered only via `FormatHumanSize` (numeric), never as labels;
  `sanitiseLabel` is untouched. Deleted size MUST be sourced from the
  index's last-known `FileSize` (or in-band capture), never a re-`stat` of
  the absent path (avoids a new filesystem reach / TOCTOU). The comparator
  carries `int64` end-to-end; any `int`↔`int64` conversion that gosec
  flags (G115) gets a per-line rationale.

### Reliability (NFR5)
- Comparator and byte sums are `int64` end-to-end — a subtree summing
  >2³¹ bytes must order correctly (no `int` truncation).
- Non-interactive `status`/`update` output and on-disk index bytes remain
  byte-identical (task 11 TC-17 still passes); the update-path size
  capture is written only by the single comparison goroutine and read only
  after `RunUpdatePipeline` returns (no new race; `-race` clean).

## Constraints
- Builds on task 11's `pkg/treeview.go`, `cmd/dcfh/internal/tui/{sort,render}.go`.
- KD2 single-source-of-truth (merged index + in-band change-set) and the
  byte-identity guarantee are hard constraints.
- No new third-party dependencies.

## Decomposition Check
- [ ] **Time**: >1 week? No (~0.5–1 day).
- [ ] **People**: >2 people? No.
- [ ] **Complexity**: 3+ concerns? Data-model + sort + cross-path size
      capture — related, one subsystem, one user-facing change.
- [ ] **Risk**: isolation needed? No; the one risk (cross-path deleted-byte
      consistency) is addressed by FR3/FR4, not isolatable code.
- [ ] **Independence**: separable usefully? No (rename + metric + default +
      the data plumbing they depend on are one atomic change).
**Conclusion**: No decomposition.

## Acceptance Criteria
- [ ] AC1 (FR1/FR5): default sort is `change_bytes(desc)`; header proves it.
- [ ] AC2 (FR6/FR8): `label()` outputs are `change_bytes`/`change_files`;
      direction flips with `r`; no bare `change` metric label remains.
- [ ] AC3 (FR2/FR3/FR4): `Stats.{Added,Modified,Deleted}Bytes` aggregate
      correctly; deleted-byte attribution is identical across status/update
      for a known-size deletion.
- [ ] AC4 (FR7): `c`/`f`/`a`/`m`/`d`/`n`/`r` re-sort live with no data re-read.
- [ ] AC5 (FR9/NFR1): byte sums computed with no extra filesystem access.
- [ ] AC6 (NFR5): `int64` end-to-end (>2³¹ subtree orders correctly);
      non-interactive output + index bytes byte-identical; `-race` clean.
- [ ] AC7: build, full test suite, and `golangci-lint` (gosec) all green.

## Status
**Status**: Finished
**Next Action**: /cwf-design-plan
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All FR1–FR9 / NFR1–NFR5 satisfied; AC1–AC7 verified by TC-1..TC-16 (all
PASS). The confirmed scope (deleted bytes included) shipped in full.

## Lessons Learned
The added+modified-only vs full-plumbing fork was the right thing to
surface at requirements review — the user's "include deleted" choice is
what makes a large deletion rank as a large change. See j-retrospective.md.
