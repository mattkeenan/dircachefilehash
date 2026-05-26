package main

import (
	"fmt"
	"os"
	"unsafe"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
	"github.com/mattkeenan/dircachefilehash/pkg/format"
)

// entryOutcome is the result the per-entry handler returns to the
// top-level loop. Exactly one of validatedEntry / skip / stop / fatal
// will be set.
type entryOutcome struct {
	validatedEntry *ValidatedEntry // success — caller advances offset by Entry.Size
	skip           bool            // malformed but we could find the next entry; caller skipped offset
	stop           bool            // malformed and we can't resync; caller breaks out
	fatal          error           // abort the whole workflow
}

// processAllEntriesWorkflow walks every entry in data and either
// applies the user's CLI field-edit (for paths in pathSet) or copies
// the entry across unchanged. Corrupted entries are best-effort
// skipped until an unfixable-cap is hit.
func processAllEntriesWorkflow(data []byte, pathSet map[string]bool, field, value string, tmpIndexFile string, entriesFixed, entriesDiscarded *int, options *ParsedOptions) error {
	header := (*indexHeader)(unsafe.Pointer(&data[0]))
	hdrSize := dcfh.HeaderSizeForVersion(header.Version)
	entryCount := header.EntryCount
	entryData := data[hdrSize:]

	offset := 0
	unfixableCount := 0
	const unfixableMax = 100

	for i := uint32(0); i < entryCount && offset < len(entryData); i++ {
		outcome := processSingleEntry(entryData, &offset, int(i), pathSet, field, value, tmpIndexFile, entriesFixed, entriesDiscarded, options, header.Version)
		if outcome.fatal != nil {
			return outcome.fatal
		}
		if outcome.stop {
			break
		}
		if outcome.skip {
			unfixableCount++
			if unfixableCount > unfixableMax {
				return fmt.Errorf("too many unfixable entries (%d), aborting", unfixableCount)
			}
			continue
		}
		offset += int(outcome.validatedEntry.Entry.Size)
	}

	return nil
}

// processSingleEntry handles one entry. On structural corruption it
// advances *offset itself (via trySkipToNextEntry) and returns
// skip/stop; on success the caller is responsible for advancing by
// Entry.Size.
func processSingleEntry(entryData []byte, offset *int, i int, pathSet map[string]bool, field, value, tmpIndexFile string, entriesFixed, entriesDiscarded *int, options *ParsedOptions, version uint32) entryOutcome {
	ve, err := NewValidatedEntry(entryData, i, *offset, version)
	if err != nil {
		return handleCorruptedEntry(entryData, offset, i, err, entriesDiscarded, options, version)
	}

	if !pathSet[ve.Path] {
		if writeErr := appendValidatedEntryToTmpIndex(tmpIndexFile, ve); writeErr != nil {
			return entryOutcome{fatal: fmt.Errorf("failed to append valid entry %d: %v", i, writeErr)}
		}
		return entryOutcome{validatedEntry: ve}
	}

	fixed, cmdErr := ve.ApplyFieldFix(field, value)
	if cmdErr != nil {
		if !options.GetBool("quiet") {
			fmt.Fprintf(os.Stderr, "Warning: entry %d still broken after CLI command, discarding: %v\n", i, cmdErr)
		}
		*entriesDiscarded++
		if !trySkipToNextEntry(entryData, offset, version) {
			return entryOutcome{stop: true}
		}
		return entryOutcome{skip: true}
	}

	if writeErr := appendValidatedEntryToTmpIndex(tmpIndexFile, fixed); writeErr != nil {
		return entryOutcome{fatal: fmt.Errorf("failed to append command-fixed entry %d: %v", i, writeErr)}
	}
	*entriesFixed++
	return entryOutcome{validatedEntry: ve}
}

// handleCorruptedEntry wraps the attempt-to-repair / skip-or-stop
// path for entries that failed structural validation.
func handleCorruptedEntry(entryData []byte, offset *int, i int, origErr error, entriesDiscarded *int, options *ParsedOptions, version uint32) entryOutcome {
	fixed, fixErr := attemptErrorFixAtOffsetValidated(entryData, i, *offset, origErr)
	if fixErr == nil {
		if !options.GetBool("quiet") {
			fmt.Printf("Fixed corrupted entry %d\n", i)
		}
		return entryOutcome{validatedEntry: fixed}
	}
	if !options.GetBool("quiet") {
		fmt.Fprintf(os.Stderr, "Warning: entry %d unfixable, discarding: %v\n", i, origErr)
	}
	*entriesDiscarded++
	if !trySkipToNextEntry(entryData, offset, version) {
		return entryOutcome{stop: true}
	}
	return entryOutcome{skip: true}
}

// trySkipToNextEntry attempts to find the next entry when current one is
// corrupted. version selects the entry-size floor used to recognise a plausible
// resync point: a legacy (v2/v3) file's entries are smaller than v4's, so using
// the source version's minimum avoids over-rejecting a legitimate legacy entry.
func trySkipToNextEntry(data []byte, offset *int, version uint32) bool {
	// If we can read the size field, try to use it to skip
	if *offset+4 <= len(data) {
		entrySize := *(*uint32)(unsafe.Pointer(&data[*offset]))
		if entrySize > 0 && entrySize < 4096 && *offset+int(entrySize) <= len(data) {
			*offset += int(entrySize)
			return true
		}
	}

	// If size is corrupted, try to find next entry by scanning for patterns
	// This is heuristic and may not always work
	for *offset < len(data)-4 {
		*offset += 8 // Try 8-byte aligned positions
		if *offset+4 > len(data) {
			break
		}

		// Check if this looks like a valid size field. The floor is the source
		// version's minimum entry size (legacy entries are smaller than v4), so a
		// legitimate legacy entry is not skipped past during resync.
		size := *(*uint32)(unsafe.Pointer(&data[*offset]))
		if size >= uint32(format.MinEntrySizeForVersion(version)) && size < 4096 { //nolint:gosec // G115: struct min size (~136-144), bounded non-negative
			// This might be a valid entry start
			return true
		}
	}

	return false // Cannot find next entry
}

// attemptErrorFixAtOffsetValidated tries to fix common corruption issues (ValidatedEntry version)
func attemptErrorFixAtOffsetValidated(_ []byte, _ int, _ int, originalErr error) (*ValidatedEntry, error) { //nolint:unparam // result 0 is always nil: stub awaiting corruption-fix implementation
	// For now, just return the original error - corruption fixing is complex
	// TODO: Implement specific corruption fixes based on error type
	return nil, fmt.Errorf("unfixable corruption: %w", originalErr)
}

// appendValidatedEntryToTmpIndex appends a ValidatedEntry to the temporary index file
func appendValidatedEntryToTmpIndex(tmpIndexFile string, ve *ValidatedEntry) error {
	// For now, write the binaryEntry directly to the temp file
	// In a complete implementation, this would use the scan index infrastructure

	// Open temp file for appending
	file, err := os.OpenFile(tmpIndexFile, os.O_WRONLY|os.O_APPEND, 0644) //nolint:gosec // G302: .dcfh/ index file, non-secret (metadata + hashes)
	if err != nil {
		return fmt.Errorf("failed to open temp index file: %v", err)
	}
	defer func() { _ = file.Close() }()

	// Write the binaryEntry as raw bytes
	// TODO: This is simplified - proper implementation would:
	// 1. Calculate correct entry size including variable path
	// 2. Write with proper alignment
	// 3. Handle path properly (variable length)
	// 4. Use the established scan index mechanisms

	entryBytes := (*[unsafe.Sizeof(*ve.Entry)]byte)(unsafe.Pointer(ve.Entry))
	_, err = file.Write(entryBytes[:])
	if err != nil {
		return fmt.Errorf("failed to write entry to temp index: %v", err)
	}

	// TODO: Also write the variable-length path data
	// For now, this is incomplete

	return nil
}
