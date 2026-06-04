package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// siblingPreFixPath returns the deterministic timestamped sibling stem for
// indexFile: "<indexFile>.pre-fix-<UTC>" with a compact sortable stamp. It is
// pure (no filesystem access) so it can be unit-tested in isolation.
func siblingPreFixPath(indexFile string) string {
	return indexFile + ".pre-fix-" + time.Now().UTC().Format("20060102T150405Z")
}

// maxPreFixCollisionSuffix bounds the numeric suffixes tried when the base
// timestamped sibling name is already taken (sub-second re-runs). Exhausting
// the bound is a hard refusal rather than an unbounded retry loop.
const maxPreFixCollisionSuffix = 100

// preserveOriginal copies indexFile to a timestamped .pre-fix sibling, opening
// the destination with O_WRONLY|O_CREATE|O_EXCL so a pre-existing destination
// (regular file, symlink, or directory) is refused rather than followed or
// truncated. On EEXIST it retries with a numeric suffix up to
// maxPreFixCollisionSuffix, re-attempting the O_EXCL open per candidate (never
// Stat-then-open, which would reintroduce a TOCTOU race). Returns the path of
// the sibling it created.
func preserveOriginal(indexFile string) (string, error) {
	src, err := os.Open(indexFile) //nolint:gosec // G304: repair-tool path from a user-supplied CLI argument (the index named on the command line); no trust boundary
	if err != nil {
		return "", fmt.Errorf("failed to open original index for preservation: %w", err)
	}
	defer func() { _ = src.Close() }()

	base := siblingPreFixPath(indexFile)
	for n := 0; n <= maxPreFixCollisionSuffix; n++ {
		cand := base
		if n > 0 {
			cand = fmt.Sprintf("%s-%d", base, n)
		}

		// O_EXCL refuses any pre-existing destination, closing the
		// "write through a planted symlink" hazard: a clash returns EEXIST and
		// we never follow or truncate whatever is there.
		dst, err := os.OpenFile(cand, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644) //nolint:gosec // G304/G302: .dcfh/ index sibling, path derived from the validated CLI index argument; 0644 non-secret (metadata + hashes)
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				// Classify the occupant without following it. A regular file is
				// a prior preserved copy from a sub-second re-run — advance to
				// the next numbered candidate. Anything else (symlink,
				// directory, device, ...) is refused outright so we never write
				// through or alongside an unexpected object (D4/FR2). The
				// Lstat is not a TOCTOU hazard: it only decides error-vs-advance
				// and we never non-exclusively open this path — the only writes
				// happen via O_EXCL on a *different* candidate.
				info, lerr := os.Lstat(cand)
				if lerr != nil {
					return "", fmt.Errorf("failed to inspect existing sibling %q: %w", cand, lerr)
				}
				if info.Mode().IsRegular() {
					continue
				}
				return "", fmt.Errorf("refusing to preserve original: %q already exists and is not a regular file (%s)", cand, info.Mode().Type())
			}
			return "", fmt.Errorf("failed to create preserved sibling %q: %w", cand, err)
		}

		// Copy then flush+close explicitly (no silent defer): a write, flush,
		// or close failure must propagate so the caller skips the rename and
		// the canonical index is left intact (NFR5). On any failure remove the
		// partial sibling before returning.
		if _, err := io.Copy(dst, src); err != nil {
			_ = dst.Close()
			_ = os.Remove(cand)
			return "", fmt.Errorf("failed to copy original to preserved sibling %q: %w", cand, err)
		}
		if err := dst.Sync(); err != nil {
			_ = dst.Close()
			_ = os.Remove(cand)
			return "", fmt.Errorf("failed to sync preserved sibling %q: %w", cand, err)
		}
		if err := dst.Close(); err != nil {
			_ = os.Remove(cand)
			return "", fmt.Errorf("failed to close preserved sibling %q: %w", cand, err)
		}
		return cand, nil
	}

	return "", fmt.Errorf("failed to preserve original: all %d collision candidates for %q already exist", maxPreFixCollisionSuffix, base)
}

// reportDryRunPreservation describes what promoteRepairedIndex would do under
// --dry-run without touching the filesystem: the destructive warning in
// --edit-in-place mode, or the sibling-preservation notice otherwise. It names
// the sibling pattern rather than a concrete timestamp (the real run stamps its
// own). The routine notice obeys --quiet; the destructive warning does not.
func reportDryRunPreservation(indexFile string, options *ParsedOptions) {
	if options.GetBool("edit-in-place") {
		fmt.Fprintf(os.Stderr, "WARNING: --edit-in-place would overwrite %s in place; the original would NOT be preserved.\n", indexFile)
		return
	}
	if !options.GetBool("quiet") {
		fmt.Printf("Would preserve original as a .pre-fix-<timestamp> sibling of %s\n", indexFile)
	}
}

// validateEditInPlaceGate returns an error if --edit-in-place is set without
// --force. It is pure (no I/O) and is invoked once at the dispatch chokepoint so
// the destructive opt-in is gated consistently across every subcommand.
func validateEditInPlaceGate(options *ParsedOptions) error {
	if options.GetBool("edit-in-place") && !options.GetBool("force") {
		return fmt.Errorf("--edit-in-place requires --force (it overwrites the index without preserving the original)")
	}
	return nil
}

// promoteRepairedIndex preserves the pre-repair original (default) then
// atomically replaces indexFile with tmpFile. In --edit-in-place mode it prints
// a destructive-action warning and skips preservation. On any preservation
// failure it returns before renaming, leaving indexFile intact (NFR5). It owns
// and prints the preservation notice and the destructive warning itself.
func promoteRepairedIndex(tmpFile, indexFile string, options *ParsedOptions) error {
	if options.GetBool("edit-in-place") {
		// Destructive opt-in (already gated on --force). The warning is a
		// safety message and is emitted regardless of --quiet.
		fmt.Fprintf(os.Stderr, "WARNING: --edit-in-place overwrites %s in place; the original is NOT preserved.\n", indexFile)
	} else {
		sibling, err := preserveOriginal(indexFile)
		if err != nil {
			return err
		}
		if !options.GetBool("quiet") {
			fmt.Printf("Original preserved at %s\n", sibling)
		}
	}

	if err := os.Rename(tmpFile, indexFile); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}
	return nil
}
