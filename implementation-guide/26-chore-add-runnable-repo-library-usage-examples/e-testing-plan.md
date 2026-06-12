# Add runnable Repo library usage examples - Testing Plan
**Task**: 26 (chore)

## Task Reference
- **Task ID**: internal-26
- **Task URL**: N/A (internal task)
- **Parent Task**: N/A
- **Branch**: chore/26-add-runnable-repo-library-usage-examples
- **Template Version**: 2.1

## Goal
Define how the new `Example*` functions are verified. For this task the examples
*are* the test artefacts — the testing surface is Go's example machinery plus the
existing suite as a regression guard. No new production code, so no new behaviour
to unit-test.

## Test Strategy
### Test Levels
- **Example execution (primary)**: `go test ./pkg/...` compiles every `Example*`
  function and *executes* those with a trailing `// Output:` block, asserting stdout
  matches byte-for-byte. This is the load-bearing check — a drifted API, a wrong
  field, or non-deterministic output fails the build.
- **Compile coverage**: examples without `// Output:` (if any remain) still compile
  against the real exported surface, so signature drift cannot land silently.
- **Static (`go vet`)**: vet's `examples` analyser flags malformed example names
  (e.g. `ExampleRepo_Diff` must map to a real `Repo.Diff` method) and bad signatures.
- **Doc render (manual spot check)**: `go doc ./pkg <Symbol>` confirms each example
  attaches under its associated symbol as a consumer would see in godoc.
- **Regression**: full `go test ./pkg/...` + `make build`; existing white-box suite
  is unchanged and must stay green (proves the new external-test-package file and the
  black-box examples don't disturb the in-package tests).

### Test Coverage Targets
- Every method on the documented consumer surface has ≥1 `Example`:
  `CreateRepo`, `OpenRepo`, `Repo.Diff`, `Repo.Apply`, `Repo.Groups`, `Repo.Filter`,
  `Repo.Config` (Get+Set), `Repo.Snapshots` (Create+List).
- ≥1 executed (`// Output:`) example per deterministic verb; non-deterministic
  surfaces (snapshot IDs/timestamps) asserted via stable derived values (counts).
- No line/branch % target — this is documentation coverage of the public API, not
  code-path coverage of production logic.

## Test Cases
### Functional Test Cases
- **TC-1 — Examples execute and match output**
  - **Given**: `pkg/example_repo_test.go` with `// Output:`-bearing examples and a
    `mustExampleRepo` helper seeding literal-named files in an `os.MkdirTemp` root.
  - **When**: `go test ./pkg/...` runs.
  - **Then**: all example output blocks match; no `got/want` mismatch; exit 0.

- **TC-2 — Index-only verbs populated before assertion**
  - **Given**: `ExampleRepo_Groups` / `ExampleRepo_Filter` seed files then call `Apply`
    before `Groups`/`Filter`.
  - **When**: the examples execute.
  - **Then**: dupe-group count > 0 / `Filter` matches the seeded `*.go` files (not 0).
    A regression that drops the `Apply` step makes the `// Output:` block fail.

- **TC-3 — Filter request shape is valid**
  - **Given**: `ExampleRepo_Filter` passes a non-empty `Actions` slice
    (`[]FilterAction{&PrintAction{}}`) and `Expression: MustNewNameTest("*.go", false)`.
  - **When**: the example executes.
  - **Then**: no "FilterRequest requires at least one action" error; path-sorted
    output is deterministic.

- **TC-4 — Temp-dir lifecycle is clean**
  - **Given**: each example `defer cleanup()` where `cleanup` runs `Close()` then
    `os.RemoveAll(dir)`.
  - **When**: the suite runs (optionally under `-count=2`).
  - **Then**: no leaked temp trees, no "directory not empty"/Close-after-RemoveAll
    ordering error; examples are idempotent across repeated runs.

- **TC-5 — Doc association**
  - **Given**: example names follow `ExampleT_M` for `Repo` methods.
  - **When**: `go vet ./pkg/...` and `go doc ./pkg Repo` run.
  - **Then**: vet reports no example-naming issues; godoc lists the examples under
    their symbols.

### Non-Functional Test Cases
- **Determinism/Reliability**: re-run `go test ./pkg/... -count=2` — examples must
  pass identically (no timestamp/path/order leakage into `// Output:`).
- **Security**: seed file names stay literal constants (no variable/env-derived
  paths) — preserves the taint-free `os.MkdirTemp` write posture noted in the
  security review. (`_test.go` is gosec-scoped-out, but the invariant is kept anyway.)
- **Performance**: N/A — examples operate on a handful of tiny seeded files.

## Test Environment
### Setup Requirements
- Standard repo toolchain: Go 1.25+, `make build`, `go test`, `go vet`, `golangci-lint`.
- No external services, no network, no test DB — examples are fully self-contained
  via `os.MkdirTemp` (the project rule "tests touching a DB use a test DB" is N/A:
  dcfh has no DB; the "store" is the temp `.dcfh` index each example creates).

### Automation
- Runs in the existing `go test ./pkg/...` target and the pre-commit
  `golangci-lint --new` gate; no new CI wiring needed.

## Validation Criteria
- [ ] `go test ./pkg/...` passes with all `// Output:` examples executing.
- [ ] `go vet ./pkg/...` clean (example naming/signatures).
- [ ] `go doc ./pkg` spot-check: examples render under their symbols.
- [ ] `make build` succeeds; existing white-box suite still green (regression).
- [ ] `go test ./pkg/... -count=2` stable (determinism).
- [ ] Pre-commit `golangci-lint --new` clean on the new file.

## Status
**Status**: Finished
**Next Action**: /cwf-implementation-exec
**Blockers**: None identified

**See `.cwf/docs/workflow/workflow-steps.md#status-values` for valid status values**

## Actual Results
All planned test cases executed in g-testing-exec: 5/5 functional PASS, determinism
(`-count=2`) stable, documentation-coverage target met (8/8 surface methods). Full
results in `g-testing-exec.md`; task-level analysis in `j-retrospective.md`.

## Lessons Learned
See `j-retrospective.md` — `go vet`'s examples analyser (not the `go doc` CLI) is the
authoritative example↔symbol association check.
