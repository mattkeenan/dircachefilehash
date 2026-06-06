# interactive-tree gdu-style post-run view - Plan
**Task**: 11 (feature)

## Task Reference
- **Task ID**: internal-11
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: feature/11-interactive-tree-gdu-style-post-run-view
- **Baseline Commit**: b5bcbcd148d7b111df0c3b3ab9ef8fa648ab8148
- **Template Version**: 2.1

## Goal
Add a `--interactive-tree` option to `dcfh status` and `dcfh update` that, once the Hwang-Lin run completes, opens a gdu/Midnight-Commander-style full-screen TUI where the user can traverse the directory tree (left pane) and, given enough terminal width, inspect a before/after filesystem-stats comparison for the selected node (right pane) to make ad-hoc assessments of the resulting state (status) or change (update).

## Success Criteria
- [ ] `dcfh status --interactive-tree` and `dcfh update --interactive-tree` both launch the TUI after the normal command run completes; without the flag, behaviour is byte-for-byte unchanged.
- [ ] Left pane renders a navigable directory tree sourced from the command's own result set (no second filesystem walk), with keyboard navigation (up/down, enter/expand, parent/collapse, quit).
- [ ] When terminal width ≥ a defined threshold, a right pane shows a before/after stats comparison for the highlighted node; below the threshold the tree pane uses the full width and the right pane is suppressed (responsive, no crash/garbled output).
- [ ] Per-node stats reflect the correct semantics per command — status: last-indexed state vs current scan; update: pre-update index vs post-update index — and aggregate correctly up the tree (counts, total size, added/modified/deleted/unchanged).
- [ ] Viewer is non-destructive and read-only: navigating never mutates the index or filesystem; the flag is inert/declined gracefully when stdout is not an interactive TTY (e.g. piped, `--json`).

## Original Estimate
**Effort**: ~3-5 days
**Complexity**: Medium-High
**Dependencies**: A TUI library decision (deferred to design); access to pipeline result data structures from `pkg/status.go` / `pkg/update.go`.

## Major Milestones
1. **Data model**: Define a tree node + before/after stats aggregate, populated from existing status/update pipeline output (no extra walk).
2. **Viewer shell**: Full-screen TUI with responsive two-pane layout (left tree always; right stats pane width-gated), navigation, and clean teardown restoring the terminal.
3. **Command wiring**: `--interactive-tree` flag on both commands, TTY/`--json` guard, launch after the Hwang-Lin run.
4. **Stats semantics**: Correct before/after derivation and up-tree aggregation for each command's distinct meaning.

## Risk Assessment
### High Priority Risks
- **Risk 1**: New TUI dependency conflicts with the repo's "minimal external dependencies" ethos and zero-copy/mmap design.
  - **Mitigation**: Evaluate stdlib/thin options vs an established TUI lib in design; isolate all TUI code behind one package so the dependency surface is contained and the non-interactive path is untouched.
- **Risk 2**: Tree data is re-derived by re-walking the filesystem, duplicating logic and diverging from the command's actual result.
  - **Mitigation**: Treat "build tree from the command's existing result set" as a hard constraint; feed the viewer the same entries the command already computed.

### Medium Priority Risks
- **Risk 3**: Terminal/TTY edge cases — small widths, no TTY, terminal resize, non-UTF-8 — cause garbled output or panics.
  - **Mitigation**: Width-threshold gating for the right pane, explicit `isatty` guard with graceful decline, resize handling, and teardown that always restores terminal state.
- **Risk 4**: `update` mutates the index; capturing a faithful "before" snapshot for comparison is awkward after the fact.
  - **Mitigation**: Decide in design where/when the before-state is captured relative to the atomic replacement; prefer the in-memory pre-merge state already available in the pipeline.

## Dependencies
- Existing status/update pipeline result structures (`pkg/status.go`, `pkg/update.go`, `pkg/pipeline_status.go`, `pkg/pipeline_update.go`).
- TUI rendering approach — chosen in the design phase.

## Constraints
- Non-interactive path (no flag, or non-TTY/`--json`) must be unchanged.
- No second filesystem walk: the tree is built from data the command already produced.
- British spelling in user-facing text; follow existing `cmd/dcfh/options.go` flag-parsing patterns.
- Read-only viewer — out of scope: editing, deleting, or re-running actions from within the TUI.

## Decomposition Check
Review these signals to determine if this task should be broken into subtasks:
- [ ] **Time**: Will this take >1 week? Borderline-no (~3-5 days).
- [ ] **People**: Does this need >2 people? No.
- [x] **Complexity**: 3+ distinct concerns? Yes — TUI shell/rendering, tree+stats data model, and CLI wiring across two commands.
- [ ] **Risk**: High-risk components needing isolation? Contained within one new TUI package.
- [x] **Independence**: Can parts be worked on separately? Yes — data model, viewer shell, and command wiring are separable.

**Assessment**: 2 signals triggered (Complexity, Independence). Decomposition is optional — the concerns are separable but small and tightly related. Recommend proceeding as a single task through requirements; revisit subtask split at the design/implementation-plan boundary if the viewer shell grows.

## Status
**Status**: Finished
**Next Action**: /cwf-requirements-plan 11
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All five success criteria met: both commands launch the viewer post-run (opt-in, default-off, non-interactive path byte-identical); left tree + width-gated right stats pane; correct per-command before/after semantics with up-tree aggregation; read-only and TTY-gated. Delivered as a single task (no subtask split needed). See j-retrospective.md.

## Lessons Learned
The 2 decomposition signals (Complexity, Independence) were real but handled by *sequencing* (pure data layer → enrichment → render → wiring) rather than a subtask split — the right call. The "TUI library decision deferred to design" risk was the highest-leverage deferral.
