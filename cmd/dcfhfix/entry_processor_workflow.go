package main

import (
	"fmt"
	"os"
	"unsafe"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

// processAllEntriesWorkflow implements your pseudocode pattern
func processAllEntriesWorkflow(data []byte, pathSet map[string]bool, field, value string, tmpIndexFile string, entriesFixed, entriesDiscarded *int, options *ParsedOptions) error {
	// Extract header information
	header := (*indexHeader)(unsafe.Pointer(&data[0]))
	entryCount := header.EntryCount
	entryData := data[dcfh.HeaderSize:]

	offset := 0
	unfixableEntryCount := 0
	unfixableEntryMax := 100 // Maximum unfixable entries before we give up

	for i := uint32(0); i < entryCount && offset < len(entryData); i++ {
		var validatedEntry *ValidatedEntry
		var err error

		// Try to get a validated entry from this offset
		validatedEntry, err = NewValidatedEntry(entryData, int(i), offset)
		if err != nil {
			// Entry has structural corruption - try to fix it
			fixedValidatedEntry, fixErr := attemptErrorFixAtOffsetValidated(entryData, int(i), offset, err)
			if fixErr != nil {
				// Entry is unfixable - discard it
				if !options.GetBool("quiet") {
					fmt.Fprintf(os.Stderr, "Warning: entry %d unfixable, discarding: %v\n", i, err)
				}
				*entriesDiscarded++
				unfixableEntryCount++

				if unfixableEntryCount > unfixableEntryMax {
					return fmt.Errorf("too many unfixable entries (%d), aborting", unfixableEntryCount)
				}

				// Try to skip to next entry (best effort)
				if !trySkipToNextEntry(entryData, &offset) {
					break // Cannot continue if we can't find next entry
				}
				continue
			}

			// Structural fix succeeded - use the fixed entry
			validatedEntry = fixedValidatedEntry
			if !options.GetBool("quiet") {
				fmt.Printf("Fixed corrupted entry %d\n", i)
			}
		}

		// At this point we have a structurally valid ValidatedEntry
		// Check if this entry's path matches the CLI target paths
		if pathSet[validatedEntry.Path] {
			// Apply the user's CLI command to this matching entry
			commandFixedEntry, cmdErr := validatedEntry.ApplyFieldFix(field, value)
			if cmdErr != nil {
				if !options.GetBool("quiet") {
					fmt.Fprintf(os.Stderr, "Warning: entry %d still broken after CLI command, discarding: %v\n", i, cmdErr)
				}
				*entriesDiscarded++
				// Try to skip to next entry
				if !trySkipToNextEntry(entryData, &offset) {
					break
				}
				continue
			}

			// CLI command succeeded - use the command-fixed entry
			err = appendValidatedEntryToTmpIndex(tmpIndexFile, commandFixedEntry)
			if err != nil {
				return fmt.Errorf("failed to append command-fixed entry %d: %v", i, err)
			}
			*entriesFixed++
		} else {
			// Path doesn't match CLI targets - use the original (structurally valid) entry
			err = appendValidatedEntryToTmpIndex(tmpIndexFile, validatedEntry)
			if err != nil {
				return fmt.Errorf("failed to append valid entry %d: %v", i, err)
			}
		}

		// Move to next entry using the valid entry's size
		offset += int(validatedEntry.Entry.Size)
	}

	return nil
}

// trySkipToNextEntry attempts to find the next entry when current one is corrupted
func trySkipToNextEntry(data []byte, offset *int) bool {
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

		// Check if this looks like a valid size field
		size := *(*uint32)(unsafe.Pointer(&data[*offset]))
		if size >= uint32(unsafe.Sizeof(binaryEntry{})) && size < 4096 {
			// This might be a valid entry start
			return true
		}
	}

	return false // Cannot find next entry
}

// extractPathFromEntry safely extracts the path from a valid binaryEntry
func extractPathFromEntry(entry *binaryEntry) string {
	// The path is stored after the fixed struct fields, null-terminated
	// For now, we'll use a simplified approach assuming the path was validated
	// during getBinaryEntryFromOffset

	// In the real implementation, the path extends beyond the Path[8] field
	// and is null-terminated. Since getBinaryEntryFromOffset already validated
	// the path, we can trust that it's properly structured.

	// This is a placeholder - we need to properly extract the variable-length path
	// For now, assume the accessor already validated and stored the path
	// TODO: Implement proper path extraction from the variable-length region
	return string(entry.Path[:]) // Temporary - needs proper implementation
}

// attemptErrorFixAtOffsetValidated tries to fix common corruption issues (ValidatedEntry version)
func attemptErrorFixAtOffsetValidated(data []byte, entryIdx int, offset int, originalErr error) (*ValidatedEntry, error) {
	// For now, just return the original error - corruption fixing is complex
	// TODO: Implement specific corruption fixes based on error type
	return nil, fmt.Errorf("unfixable corruption: %w", originalErr)
}

// appendValidatedEntryToTmpIndex appends a ValidatedEntry to the temporary index file
func appendValidatedEntryToTmpIndex(tmpIndexFile string, ve *ValidatedEntry) error {
	// For now, write the binaryEntry directly to the temp file
	// In a complete implementation, this would use the scan index infrastructure

	// Open temp file for appending
	file, err := os.OpenFile(tmpIndexFile, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open temp index file: %v", err)
	}
	defer file.Close()

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
