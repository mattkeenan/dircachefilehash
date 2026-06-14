package dircachefilehash

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// This file holds the Repo.Fix primitive's shared core (task 28.2). RunFix is
// the single execution path for both repoCore.Fix (library) and the dcfhfix CLI
// translator, mirroring RunFilter/Filter. It routes each FixCommand by op
// family, confines write destinations (D2/NFR4), and gates writes on DryRun
// (LD5). The single-writer entry path (writeRepairedIndex, 28.1) and the
// surgical header writer (ApplyHeaderEdit) do the actual index mutation.

// FixOp identifies one dcfhfix operation. The full set covers every current
// subcommand (FR2); see the classification table in c-design-plan.md.
type FixOp string

const (
	FixOpHeaderShow   FixOp = "header-show"
	FixOpHeaderEdit   FixOp = "header-edit"
	FixOpEntryShow    FixOp = "entry-show"
	FixOpEntryEdit    FixOp = "entry-edit"
	FixOpEntryAppend  FixOp = "entry-append"
	FixOpEntryRemove  FixOp = "entry-remove"
	FixOpFixesList    FixOp = "fixes-list"
	FixOpFixesPop     FixOp = "fixes-pop"
	FixOpFixesDiscard FixOp = "fixes-discard"
	FixOpFixesClear   FixOp = "fixes-clear"

	// FixOpRecoveryRebuild reconstructs main.idx from a multi-source merge
	// (task 28.3). It is a write op by omission (not in readOnlyFixOps) and is
	// deliberately kept OUT of fixOpMutatesIndex: it takes its own pre-recovery
	// snapshot, so it must not also trigger the per-subject .pre-fix backup. It
	// reads N sources and writes one destination, so RunFix routes it to a
	// dedicated batch branch (runRecoveryRebuild) rather than the per-subject
	// command loop.
	FixOpRecoveryRebuild FixOp = "recovery-rebuild"
)

// readOnlyFixOps is the allow-list of ops that never write. Classification is
// fail-closed by construction: fixOpIsWrite treats any op NOT in this set
// (including a future op added without thought) as a write, so it is confined
// rather than silently escaping the D2 check (LD2).
var readOnlyFixOps = map[FixOp]bool{
	FixOpHeaderShow: true,
	FixOpEntryShow:  true,
	FixOpFixesList:  true,
}

// fixOpIsWrite reports whether op may write to disk. Fail-closed: unknown ops
// are treated as writes.
func fixOpIsWrite(op FixOp) bool { return !readOnlyFixOps[op] }

// fixOpMutatesIndex reports whether op rewrites the subject index (and so takes
// a pre-write backup). The fixes-stack ops are writes but manipulate the backup
// stack, not the index, so they are excluded.
func fixOpMutatesIndex(op FixOp) bool {
	switch op {
	case FixOpHeaderEdit, FixOpEntryEdit, FixOpEntryAppend, FixOpEntryRemove:
		return true
	default:
		return false
	}
}

// FixCommand is one tagged operation in a FixRequest batch. Only the fields a
// given Op consumes are read (Field/Value for edits, Paths for
// show/edit/remove, Value carries the JSON for append and for the json edit
// forms).
type FixCommand struct {
	Op    FixOp
	Field string   // header-edit / entry-edit field name (or "json")
	Value string   // header-edit / entry-edit value, or the JSON payload
	Paths []string // entry-show / entry-edit / entry-remove targets
}

// FixRequest drives RunFix. IndexSelectors reuses the dcfhfind selector
// vocabulary (single-source for 28.2). Mode delivers FixModeAuto; FixModeManual
// returns ErrManualModeUnimplemented. DryRun previews without writing; Backup
// gates pre-write preservation onto the .dcfh/fixes stack.
type FixRequest struct {
	Options        Options       `json:"options"`
	IndexSelectors []string      `json:"index_selectors"`
	Repository     string        `json:"repository,omitempty"`
	Commands       []FixCommand  `json:"-"`
	Mode           FixMode       `json:"-"`
	DryRun         bool          `json:"dry_run,omitempty"`
	Backup         bool          `json:"backup,omitempty"`
	Verbose        int           `json:"verbose,omitempty"`
	Flags          FixEntryFlags `json:"-"` // Quiet/EditInPlace/Force for the writer + promote
}

// FixResult summarises a Fix run. RepairsApplied counts applied edits/appends/
// removals (and header edits, and popped/cleared backups); EntriesDiscarded
// counts entries dropped as unfixable during a walk. (No BackupID — LD7; no
// per-file count — single-source.)
type FixResult struct {
	RepairsApplied   int `json:"repairs_applied"`
	EntriesDiscarded int `json:"entries_discarded"`
}

// ErrManualModeUnimplemented is returned by RunFix when FixModeManual is
// requested: interactive mode is deferred (parent D5), and the sentinel lets
// callers distinguish "not implemented" from a real failure. No write occurs.
var ErrManualModeUnimplemented = errors.New("manual (interactive) fix mode is not implemented")

// confineWriteDest canonicalises a single-file write target and asserts it lies
// within root, resolving symlinks before comparing. The leaf need not exist
// (e.g. a new .pre-fix sibling): its existing parent is resolved and the base
// recombined. Any abs/resolve error rejects the write (fail-closed). It reuses
// hasPathPrefix (wire_handler.go).
func confineWriteDest(dest, root string) (string, error) {
	absDest, err := filepath.Abs(dest)
	if err != nil {
		return "", fmt.Errorf("cannot resolve write destination %q: %w", dest, err)
	}
	absDest = filepath.Clean(absDest)

	parent, err := filepath.EvalSymlinks(filepath.Dir(absDest))
	if err != nil {
		return "", fmt.Errorf("cannot resolve parent of write destination %q: %w", dest, err)
	}
	resolved := filepath.Join(parent, filepath.Base(absDest))

	resolvedRoot, err := resolveRoot(root)
	if err != nil {
		return "", err
	}
	if !hasPathPrefix(resolved, resolvedRoot) {
		return "", fmt.Errorf("write destination %q escapes confinement root %q", dest, root)
	}
	return resolved, nil
}

// confineWriteDir asserts that dir lies within root. dir may not exist yet (the
// backup stack dir is created by MkdirAll after this check), so the deepest
// existing ancestor is resolved and the remaining segments recombined under it
// — avoiding an EvalSymlinks error on the not-yet-created leaf that would
// fail-closed-reject every first backup. Fail-closed on any abs/resolve error.
func confineWriteDir(dir, root string) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("cannot resolve write directory %q: %w", dir, err)
	}
	absDir = filepath.Clean(absDir)

	existing := absDir
	for {
		if _, lerr := os.Lstat(existing); lerr == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			break // reached root of the filesystem
		}
		existing = parent
	}

	resolvedExisting, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return fmt.Errorf("cannot resolve write directory %q: %w", dir, err)
	}
	rel, err := filepath.Rel(existing, absDir)
	if err != nil {
		return fmt.Errorf("cannot resolve write directory %q: %w", dir, err)
	}
	resolved := filepath.Join(resolvedExisting, rel)

	resolvedRoot, err := resolveRoot(root)
	if err != nil {
		return err
	}
	if !hasPathPrefix(resolved, resolvedRoot) {
		return fmt.Errorf("write directory %q escapes confinement root %q", dir, root)
	}
	return nil
}

// resolveRoot canonicalises a confinement root (abs + symlink-resolved).
func resolveRoot(root string) (string, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("cannot resolve confinement root %q: %w", root, err)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(absRoot))
	if err != nil {
		return "", fmt.Errorf("cannot resolve confinement root %q: %w", root, err)
	}
	return resolved, nil
}

// RunFix executes the FixRequest's commands against the single resolved subject
// index. writeRoot is the confinement root: repoCore.Fix passes the repo's
// MetaDir (every write is bounded to it); the dcfhfix CLI passes "" for the
// explicit-named-subject exemption (the user named the file directly — no
// selector indirection — so confinement is skipped). writeRoot is NOT a
// FixRequest field, so a library consumer cannot relax it (D2/LD3).
func RunFix(ctx context.Context, refs []IndexRef, req FixRequest, writeRoot string, warnOut io.Writer) (*FixResult, error) {
	result := &FixResult{}

	if req.Mode == FixModeManual {
		return result, ErrManualModeUnimplemented
	}

	// Recovery rebuild is a batch-level op: it reads N sources and writes one
	// destination, so it bypasses the single-subject contract below. It cannot
	// be mixed with per-subject ops in one request (LD1).
	if hasRecoveryOp(req.Commands) {
		if len(req.Commands) != 1 {
			return result, fmt.Errorf("recovery-rebuild cannot be combined with other fix ops")
		}
		if err := runRecoveryRebuild(ctx, refs, req, writeRoot, result); err != nil {
			return result, err
		}
		return result, nil
	}

	if len(refs) != 1 {
		return result, fmt.Errorf("fix operates on a single index file (got %d); 28.2 is single-source", len(refs))
	}
	subject := refs[0].Path

	for _, cmd := range req.Commands {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := runOneFixCommand(cmd, subject, req, writeRoot, warnOut, result); err != nil {
			return result, err
		}
	}
	return result, nil
}

// runOneFixCommand confines (for writes), takes the pre-write backup (for
// index-mutating ops), then dispatches by op family.
func runOneFixCommand(cmd FixCommand, subject string, req FixRequest, writeRoot string, warnOut io.Writer, result *FixResult) error {
	dest := subject
	if fixOpIsWrite(cmd.Op) && writeRoot != "" {
		confined, err := confineWriteDest(subject, writeRoot)
		if err != nil {
			return err
		}
		dest = confined
	}

	// Pre-write backup for index-mutating ops, mirroring the CLI: backup
	// before the edit, unless dry-run. The json-stub edits below still take
	// this backup (preserving the pre-28.2 backup-then-error behaviour).
	if fixOpMutatesIndex(cmd.Op) && !req.DryRun && req.Backup {
		if writeRoot != "" {
			if err := confineBackupDir(dest, writeRoot); err != nil {
				return err
			}
		}
		if err := CreateBackup(dest, backupOperationName(cmd), backupDescription(cmd),
			true, req.Verbose, req.Flags.Quiet, warnOut); err != nil {
			return fmt.Errorf("failed to create backup: %w", err)
		}
	}

	switch cmd.Op {
	case FixOpHeaderShow, FixOpEntryShow, FixOpFixesList:
		// Read-only inspection: rendered by the caller via the existing
		// inspection helpers (LD2). No-op at the primitive level.
		return nil
	case FixOpHeaderEdit:
		return runHeaderEdit(cmd, dest, req, result)
	case FixOpEntryEdit:
		return runEntryEdit(cmd, dest, req, result)
	case FixOpEntryAppend:
		return runEntryAppend(cmd, dest, req, result)
	case FixOpEntryRemove:
		return runEntryRemove(cmd, dest, req, result)
	case FixOpFixesPop, FixOpFixesDiscard, FixOpFixesClear:
		return runFixesStack(cmd, dest, req, writeRoot, result)
	default:
		return fmt.Errorf("unknown fix op %q", cmd.Op)
	}
}

func runHeaderEdit(cmd FixCommand, dest string, req FixRequest, result *FixResult) error {
	if cmd.Field == "json" {
		// Preserved stub (the pre-write backup, if any, was taken by the
		// caller's mutating-backup block — backup-then-error behaviour).
		return fmt.Errorf("header edit JSON not yet implemented")
	}
	if err := ValidateHeaderEdit(cmd.Field, cmd.Value); err != nil {
		return err
	}
	if req.DryRun {
		return nil
	}
	if err := ApplyHeaderEdit(dest, cmd.Field, cmd.Value, req.Flags); err != nil {
		return err
	}
	result.RepairsApplied++
	return nil
}

func runEntryEdit(cmd FixCommand, dest string, req FixRequest, result *FixResult) error {
	if cmd.Field == "json" {
		return fmt.Errorf("entry edit JSON not yet implemented")
	}
	collected, checksumType, fixed, discarded, err := collectForEdit(dest, makePathSet(cmd.Paths), cmd.Field, cmd.Value, req.Flags)
	result.EntriesDiscarded += discarded // surfaced even on a cap-trip error (AC6)
	if err != nil {
		return err
	}
	if !req.DryRun {
		if err := writeRepairedIndex(dest, checksumType, collected, req.Flags); err != nil {
			return err
		}
	}
	result.RepairsApplied += fixed
	return nil
}

func runEntryAppend(cmd FixCommand, dest string, req FixRequest, result *FixResult) error {
	newEntry, err := ParseEntryFromJSON(cmd.Value)
	if err != nil {
		return fmt.Errorf("failed to parse JSON entry: %w", err)
	}
	collected, checksumType, discarded, err := collectForAppend(dest, newEntry, req.Flags)
	result.EntriesDiscarded += discarded // surfaced even on a cap-trip error (AC6)
	if err != nil {
		return err
	}
	if !req.DryRun {
		if err := writeRepairedIndex(dest, checksumType, collected, req.Flags); err != nil {
			return err
		}
	}
	result.RepairsApplied++
	return nil
}

func runEntryRemove(cmd FixCommand, dest string, req FixRequest, result *FixResult) error {
	collected, checksumType, removed, discarded, err := collectForRemoval(dest, makePathSet(cmd.Paths), req.Flags)
	result.EntriesDiscarded += discarded // surfaced even on a cap-trip error (AC6)
	if err != nil {
		return err
	}
	if !req.DryRun {
		if err := writeRepairedIndex(dest, checksumType, collected, req.Flags); err != nil {
			return err
		}
	}
	result.RepairsApplied += removed
	return nil
}

// runFixesStack handles the backup-stack write ops. Under DryRun it is a no-op
// at the primitive level (the CLI renders the would-do preview). The stack
// directory is confined via confineWriteDir for the library path.
func runFixesStack(cmd FixCommand, dest string, req FixRequest, writeRoot string, result *FixResult) error {
	if req.DryRun {
		return nil
	}
	if writeRoot != "" {
		if err := confineBackupDir(dest, writeRoot); err != nil {
			return err
		}
	}
	switch cmd.Op {
	case FixOpFixesPop:
		if _, err := PopBackup(dest); err != nil {
			return err
		}
		result.RepairsApplied++
	case FixOpFixesDiscard:
		if _, err := DiscardBackup(dest); err != nil {
			return err
		}
		result.RepairsApplied++
	case FixOpFixesClear:
		n, err := ClearBackups(dest)
		if err != nil {
			return err
		}
		result.RepairsApplied += n
	}
	return nil
}

// confineBackupDir bounds the backup stack directory (BackupDir walks UP to a
// .dcfh, so it is not derived from dest's confined directory and must be
// independently checked — LD3). A missing .dcfh is not a confinement failure:
// it is surfaced downstream by CreateBackup / the stack op.
func confineBackupDir(subject, writeRoot string) error {
	dir, err := BackupDir(subject)
	if err != nil {
		return nil //nolint:nilerr // missing .dcfh is reported by the backup/stack op, not a confinement breach
	}
	return confineWriteDir(dir, writeRoot)
}

// makePathSet normalises CLI-style paths into the set the workflow collectors
// expect: filepath.Clean, with "." mapped to the empty (repo-root) path.
func makePathSet(paths []string) map[string]bool {
	pathSet := make(map[string]bool, len(paths))
	for _, path := range paths {
		normalised := filepath.Clean(path)
		if normalised == "." {
			normalised = ""
		}
		pathSet[normalised] = true
	}
	return pathSet
}

// backupOperationName maps a command to the operation tag recorded in backup
// metadata, preserving the pre-28.2 CLI strings (incl. the -json variants).
func backupOperationName(cmd FixCommand) string {
	switch cmd.Op {
	case FixOpHeaderEdit:
		if cmd.Field == "json" {
			return "header-edit-json"
		}
		return "header-edit"
	case FixOpEntryEdit:
		if cmd.Field == "json" {
			return "entry-edit-json"
		}
		return "entry-edit"
	default:
		return string(cmd.Op)
	}
}

// backupDescription reproduces the human descriptions the pre-28.2 CLI stored
// in backup metadata for each edit op.
func backupDescription(cmd FixCommand) string {
	switch cmd.Op {
	case FixOpHeaderEdit:
		if cmd.Field == "json" {
			if len(cmd.Value) <= 50 {
				return fmt.Sprintf("Edit header with JSON: %s", cmd.Value)
			}
			return fmt.Sprintf("Edit header with JSON: %.50s...", cmd.Value)
		}
		return fmt.Sprintf("Edit header.%s = %s", cmd.Field, cmd.Value)
	case FixOpEntryEdit:
		if cmd.Field == "json" {
			return fmt.Sprintf("Edit entries with JSON %s for %s", jsonDesc(cmd.Value, 30), pathsDesc(cmd.Paths, 3))
		}
		return fmt.Sprintf("Edit entry.%s = %s for %s", cmd.Field, cmd.Value, pathsDesc(cmd.Paths, 3))
	case FixOpEntryAppend:
		return fmt.Sprintf("Append entry: %s", jsonDesc(cmd.Value, 40))
	case FixOpEntryRemove:
		return fmt.Sprintf("Remove entries: %s", pathsDesc(cmd.Paths, 5))
	default:
		return string(cmd.Op)
	}
}

func pathsDesc(paths []string, inlineMax int) string {
	if len(paths) <= inlineMax {
		return strings.Join(paths, ", ")
	}
	return fmt.Sprintf("%d paths", len(paths))
}

func jsonDesc(jsonData string, max int) string {
	if len(jsonData) <= max {
		return jsonData
	}
	return fmt.Sprintf("%.*s...", max, jsonData)
}
