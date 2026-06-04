# dcfhfix non-destructive fix-to-new-file - Testing Execution
**Task**: 8 (feature)

## Task Reference
- **Task ID**: internal-8
- **Branch**: feature/8-dcfhfix-non-destructive-fix-to-new-file
- **Template Version**: 2.1

## Goal
Execute e-testing-plan.md: unit coverage of the promote.go helpers + the per
write-path integration matrix + the message/quiet matrix, all green.

## Test Results

### Functional Tests
All in `cmd/dcfhfix/promote_test.go` (unit) and `promote_integration_test.go`
(integration). Run: `go test ./cmd/dcfhfix/...` → PASS.

| Test ID | Case | Test func | Status |
|---------|------|-----------|--------|
| TC-U1 | sibling naming shape | `TestSiblingPreFixPath_Shape` | PASS |
| TC-U2 | byte-identical sibling, original intact | `TestPreserveOriginal_ByteIdenticalOriginalIntact` | PASS |
| TC-U3 | refuse symlink dest, target untouched | `TestPreserveOriginal_RefusesSymlink` | PASS |
| TC-U4 | refuse directory dest | `TestPreserveOriginal_RefusesDirectory` | PASS |
| TC-U5 | EEXIST advances counter | `TestPreserveOriginal_EEXISTAdvancesCounter` | PASS |
| TC-U6 | bound exhaustion → hard refusal | `TestPreserveOriginal_BoundExhaustion` | PASS |
| TC-U7 | gate logic matrix | `TestValidateEditInPlaceGate` | PASS |
| TC-I1 (remove) | default preserves byte-identical sibling | `TestEntryRemove_DefaultPreservesSibling` | PASS |
| TC-I1 (header edit) | default preserves (un-blanked options path) | `TestHeaderEdit_DefaultPreservesSibling` | PASS |
| TC-I1 (entry edit) | default preserves | `TestEntryEdit_DefaultPreservesSibling` | PASS |
| TC-I1 (append) | default preserves | `TestEntryAppend_DefaultPreservesSibling` | PASS |
| TC-I2 (remove) | --edit-in-place suppresses sibling | `TestEntryRemove_EditInPlaceSuppressesSibling` | PASS |
| TC-I2 (header edit) | --edit-in-place suppresses sibling | `TestHeaderEdit_EditInPlaceSuppressesSibling` | PASS |
| TC-I3 | --force alone still preserves | `TestEntryRemove_ForceAlonePreservesSibling` | PASS |
| TC-I4 | lone --edit-in-place refused, fs untouched | `TestDispatchCommand_GateRefusesLoneEditInPlace` | PASS |
| TC-I5 | dry-run writes nothing, reports preserve | `TestEntryRemove_DryRunWritesNothing` | PASS |
| TC-I6 | dry-run + destructive preview | `TestEntryRemove_DryRunDestructivePreview` | PASS |
| TC-I7 | fixes stack + sibling coexist (FR5) | `TestEntryRemove_BackupStackCoexistsWithSibling` | PASS |
| TC-A1 | message/quiet matrix (4 sub-cases) | `TestPromoteRepairedIndex_MessageQuietMatrix` | PASS |
| — | promote default/in-place boundary | `TestPromoteRepairedIndex_{DefaultPreserves,EditInPlaceSuppresses}` | PASS |

### Non-Functional Tests
- **Reliability (NFR5)** — preserve-before-rename ordering: `TestPreserveOriginal_ByteIdenticalOriginalIntact`
  (original intact after copy), `TestPreserveOriginal_CopyFailureRemovesPartial`
  (copy failure removes the partial sibling, no rename), and
  `TestPromoteRepairedIndex_PreserveFailureSkipsRename` (failed preservation ⇒
  canonical untouched, temp not consumed). All PASS.
- **Security (NFR4)** — symlink/dir refusals (TC-U3/U4) are the load-bearing
  defences; `golangci-lint run ./...` → **0 issues** (gosec gate, with the
  rationale-bearing `//nolint:gosec // G304/G302` on the new O_EXCL open).
- **Usability (NFR2)** — message/quiet matrix (TC-A1) confirms the preservation
  notice obeys `--quiet` while the destructive warning survives it.

### Race detector
`go test -race` is run by the gate as
`GOFLAGS="-gcflags=all=-d=checkptr=0" go test -race -short ./...` (checkptr
deliberately disabled for the zero-copy unsafe arithmetic). Under that config
the new code is clean: `cmd/dcfhfix` and `pkg` PASS `-race`. A plain
`go test -race` (checkptr on) fails in the pre-existing zero-copy accessors
(`pkg/binary_entry.go`, `pkg/format/entry.go`) — unrelated to this task and now
tracked by a **Very High** backlog item ("Re-enable checkptr in the race gate").

## Coverage Report
`promote.go` function coverage (`go test -coverprofile`):
- `siblingPreFixPath` 100%, `reportDryRunPreservation` 100%,
  `validateEditInPlaceGate` 100%, `promoteRepairedIndex` 100%.
- `preserveOriginal` 75.8% — the remaining uncovered lines are the
  `Sync`/`Close`/`Lstat`-error defensive branches, which require a
  fault-injection seam not warranted at this scale (documented residual gap;
  the copy-error branch *is* covered via `CopyFailureRemovesPartial`).

## Regression
Full suite green: `go test ./...` (all packages OK), `go test -race -short`
(gate config) OK, `golangci-lint run ./...` → 0 issues.

## Status
**Status**: Finished
**Next Action**: /cwf-rollout
**Blockers**: None identified

## Security Review

**State**: no findings

no findings: empty changeset. The g-phase diff is test-only
(`promote_test.go`, `promote_integration_test.go`) plus a BACKLOG.md entry and
this wf file — no production code. The production change (`promote.go` + the
four rename-site rewrites) was semantically reviewed in the f-phase
(`cwf-security-reviewer-changeset`, no findings) and the gosec gate
(`golangci-lint run ./...` → 0 issues) covers it on every commit. Nothing new to
review here.

## Lessons Learned
- Manually running `go test -race ./...` enables checkptr, which the gate
  disables; a misconfigured manual run looked like a commit blocker before I
  checked `.githooks/pre-commit`. Read the gate config before assuming breakage.
