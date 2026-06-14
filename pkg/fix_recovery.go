package dircachefilehash

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// hasRecoveryOp reports whether any command in the batch is the recovery
// rebuild, which RunFix routes to the dedicated batch branch (LD1).
func hasRecoveryOp(cmds []FixCommand) bool {
	for _, c := range cmds {
		if c.Op == FixOpRecoveryRebuild {
			return true
		}
	}
	return false
}

// runRecoveryRebuild executes the multi-source recovery rebuild (data flow
// steps 3–9 of c-design-plan). It orders the surviving sources by precedence,
// merges them, guards the empty case, short-circuits a dry run, takes and reads
// back the pre-recovery snapshot, then atomically writes the rebuilt main.idx
// via the single-writer path. result carries the projected/applied counts even
// when the rebuild aborts (e.g. empty merge), mirroring the cap-trip contract.
func runRecoveryRebuild(ctx context.Context, refs []IndexRef, req FixRequest, writeRoot string, result *FixResult) error {
	// LD8: recovery is reachable ONLY through Repo.Fix, which always passes
	// MetaDir as writeRoot. The dcfhfix explicit-subject exemption (writeRoot=="")
	// must never reach a destructive multi-source rebuild — assert it first.
	if writeRoot == "" {
		return fmt.Errorf("recovery-rebuild requires a confinement root (library-only op)")
	}
	metaDir := writeRoot

	namedPaths := make([]string, 0, len(refs))
	for _, r := range refs {
		if r.Path != "" {
			namedPaths = append(namedPaths, r.Path)
		}
	}

	ordered, err := orderedSourcePaths(metaDir, namedPaths)
	if err != nil {
		return err
	}

	merged, checksumType, discarded, contributing := mergeSourcesIntoEntries(ordered, req.Flags)
	result.EntriesDiscarded = discarded

	// LD7 empty guard: never overwrite a recoverable index with a header-only
	// one. Abort before any snapshot/write, leaving every original intact.
	if len(merged) == 0 {
		return fmt.Errorf("recovery produced no entries; refusing to overwrite main.idx (%d discarded, originals intact)", discarded)
	}
	result.RepairsApplied = len(merged)

	// LD9 dry-run: report projected counts without touching the repo.
	if req.DryRun {
		return nil
	}

	// LD6 snapshot precondition + fatal readback of the contributing sources.
	ms := initMetaStoreBase("", metaDir)
	if err := ms.createPreRecoverySnapshot(req.Verbose); err != nil {
		return fmt.Errorf("pre-recovery snapshot failed: %w", err)
	}
	if err := verifyRecoverySnapshot(metaDir, contributing); err != nil {
		return err
	}

	// Honour cancellation right before the destructive write.
	if err := ctx.Err(); err != nil {
		return err
	}

	// LD8 confinement (defence in depth): the destination is the fixed,
	// structurally-in-MetaDir main.idx, never selector-derived.
	if _, err := confineWriteDest(ms.IndexFile, writeRoot); err != nil {
		return err
	}

	// LD5 single-writer atomic write. EditInPlace: the pre-recovery snapshot is
	// the backup of record (LD6), and main.idx may be missing (destroyed),
	// which PreserveOriginal cannot handle — so the per-file .pre-fix
	// preservation is skipped here.
	writeOpts := FixEntryFlags{Quiet: req.Flags.Quiet, EditInPlace: true, Force: true}
	if err := writeRepairedIndex(ms.IndexFile, checksumType, merged, writeOpts); err != nil {
		return fmt.Errorf("failed to write rebuilt index: %w", err)
	}
	return nil
}

// This file holds the multi-source recovery rebuild (task 28.3): reconstruct
// main.idx from a precedence-ordered merge of the surviving index sources
// (timestamped caches > cache.idx > main.idx). It is orchestration only — the
// per-source read reuses the fsck tolerant walk (collectForEdit with an empty
// pathSet, fix_entry_workflow.go), the atomic write reuses writeRepairedIndex
// (the single-writer path), and the pre-write backup reuses
// createPreRecoverySnapshot (recovery.go). The op-gated RunFix branch that
// drives this lives in fix_run.go (runRecoveryRebuild).

// orderedSourcePaths assembles the precedence-ordered list of readable index
// source paths for a recovery rebuild. The order is the documented precedence
// (FR2): timestamped caches (newest→oldest) > cache.idx > main.idx > any other
// named source. namedRefPaths are the caller's resolved selectors' paths; the
// timestamped-cache lineage is auto-discovered (the selector vocabulary cannot
// name it — ResolveIndexSelectors "all" is main+cache+scan only).
//
// Every candidate is confinement-checked within metaDir (read-source
// confinement, NFR4): a named source resolving OUTSIDE metaDir is a hard error,
// not a silent skip — the merge must never read an attacker-steered path. A
// non-existent or non-regular candidate (e.g. a destroyed main.idx, or a
// symlink) is skipped, not read. The result is de-duplicated by canonical path.
func orderedSourcePaths(metaDir string, namedRefPaths []string) ([]string, error) {
	ms := initMetaStoreBase("", metaDir)
	timestamped, err := ms.ScanForTimestampedCacheFiles()
	if err != nil {
		return nil, fmt.Errorf("failed to scan timestamped caches: %w", err)
	}
	// ScanForTimestampedCacheFiles is chronological ascending; reverse for
	// newest-first so the newest cache wins the precedence keep-first.
	reversed := make([]string, 0, len(timestamped))
	for i := len(timestamped) - 1; i >= 0; i-- {
		reversed = append(reversed, timestamped[i])
	}

	// Slot the named refs into the precedence ladder by basename.
	var namedCache, namedMain, namedOther []string
	for _, p := range namedRefPaths {
		switch filepath.Base(p) {
		case CacheIndex:
			namedCache = append(namedCache, p)
		case MainIndex:
			namedMain = append(namedMain, p)
		default:
			namedOther = append(namedOther, p)
		}
	}

	candidates := make([]string, 0, len(reversed)+len(namedRefPaths))
	candidates = append(candidates, reversed...)
	candidates = append(candidates, namedCache...)
	candidates = append(candidates, namedMain...)
	candidates = append(candidates, namedOther...)

	seen := make(map[string]bool, len(candidates))
	ordered := make([]string, 0, len(candidates))
	for _, c := range candidates {
		// Reuse the confineWriteDest canonicalisation for read-source
		// confinement: it resolves symlinks of the parent and rejects any path
		// escaping metaDir (fail-closed). The leaf need not exist.
		confined, cerr := confineWriteDest(c, metaDir)
		if cerr != nil {
			return nil, fmt.Errorf("recovery source %q escapes MetaDir confinement: %w", c, cerr)
		}
		if seen[confined] {
			continue
		}
		seen[confined] = true
		// Lstat (not Stat): a source that is itself a symlink is skipped rather
		// than followed — we never read through an unexpected object.
		info, lerr := os.Lstat(confined)
		if lerr != nil || !info.Mode().IsRegular() {
			continue
		}
		ordered = append(ordered, confined)
	}
	return ordered, nil
}

// mergeSourcesIntoEntries folds the precedence-ordered sources into one merged
// entry set keyed by relative path. It reads each source via the fsck tolerant
// walk (so a truncated body contributes its readable validated prefix), keeps
// the FIRST occurrence of each path (highest precedence wins), drops deleted
// tombstones after the fold (main.idx excludes deleted entries — a higher-
// precedence tombstone suppresses a lower-precedence live entry), and sorts the
// survivors by path (the binary layout requires ascending order).
//
// checksumType is taken from the highest-precedence CONTRIBUTING source; a
// later source whose header checksum_type differs is skipped (its entries
// counted as discards), never re-hashed under the wrong algorithm. A source
// that fails to read at all is skipped tolerantly.
//
// contributing lists the sources that actually fed ≥1 entry — the snapshot
// readback (LD6) verifies only these, not the destroyed write target. Discard
// categories are disjoint (FR6): truncation/validation (in the per-source
// read), checksum-mismatch (source skip), conflict-loser (the fold), and
// deleted-filter (after the fold).
func mergeSourcesIntoEntries(orderedPaths []string, options FixEntryFlags) (
	merged []*ValidatedEntry, checksumType uint16, discarded int, contributing []string) {
	mergedMap := make(map[string]*ValidatedEntry)
	typeSet := false

	for _, path := range orderedPaths {
		// Empty pathSet: every entry is kept unchanged (including deleted
		// tombstones, needed for cross-source suppression). entriesFixed is
		// always 0 here and discarded.
		collected, ct, _, disc, cerr := collectForEdit(path, map[string]bool{}, "", "", options)
		discarded += disc
		if cerr != nil {
			// Wholly-unreadable source (too small / cap-exceeded): skip
			// tolerantly so one corrupt file cannot block recovery.
			continue
		}
		if len(collected) == 0 {
			continue
		}
		if typeSet && ct != checksumType {
			// Mixed checksum-type source: refuse its entries (counted as
			// discards) rather than re-hash under the wrong algorithm.
			discarded += len(collected)
			continue
		}
		if !typeSet {
			checksumType = ct
			typeSet = true
		}
		contributing = append(contributing, path)
		for _, ve := range collected {
			if _, exists := mergedMap[ve.Path]; exists {
				discarded++ // conflict-loser: a higher-precedence source already won this path
				continue
			}
			mergedMap[ve.Path] = ve
		}
	}

	merged = make([]*ValidatedEntry, 0, len(mergedMap))
	for _, ve := range mergedMap {
		if ve.Entry.IsDeleted() {
			discarded++ // tombstone won the conflict, now filtered from main.idx
			continue
		}
		merged = append(merged, ve)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].Path < merged[j].Path })

	return merged, checksumType, discarded, contributing
}

// verifyRecoverySnapshot is the fatal readback that turns the best-effort
// createPreRecoverySnapshot into a hard precondition (LD6/FR4): each
// contributing source must be present as a non-empty regular file under
// .dcfh/recovery/. A missing, empty, or non-regular (symlinked) copy aborts the
// rebuild before the destructive write, leaving main.idx untouched. Lstat (not
// Stat) so a planted symlink in recovery/ is rejected, not followed.
func verifyRecoverySnapshot(metaDir string, contributing []string) error {
	recoveryDir := filepath.Join(metaDir, "recovery")
	for _, src := range contributing {
		copyPath := filepath.Join(recoveryDir, filepath.Base(src))
		info, err := os.Lstat(copyPath)
		if err != nil {
			return fmt.Errorf("pre-recovery snapshot missing for %q: %w", filepath.Base(src), err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("pre-recovery snapshot for %q is not a regular file (%s)", filepath.Base(src), info.Mode().Type())
		}
		if info.Size() == 0 {
			return fmt.Errorf("pre-recovery snapshot for %q is empty", filepath.Base(src))
		}
	}
	return nil
}
