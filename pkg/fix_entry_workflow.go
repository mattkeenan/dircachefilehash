package dircachefilehash

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"github.com/mattkeenan/dircachefilehash/pkg/format"
)

// This file holds the dcfhfix entry-repair workflow relocated from
// cmd/dcfhfix (entry_processor_workflow.go + entry_workflow_main.go +
// entry_append_remove.go). It is the fsck-style "assume the index may be
// corrupt, make forward progress" path: validate each entry, apply the
// requested field edit (or append/remove), best-effort skip corruption up to a
// cap, and write the survivors to a temp index that is atomically promoted over
// the subject.
//
// Writer (task 28.1, Approach A / design D3): survivors are collected into a
// []*ValidatedEntry, then written through the single production writer
// (TempIndexWriter + EntrySerialiser) — the same path scan/update use. This
// replaced the previous raw-O_APPEND writer that dropped each entry's
// variable-length path and hand-rolled the header/checksum. The writer's
// MetaStore is synthesised from the subject header's checksum_type so the
// output preserves it; legacy v2/v3 subjects are deliberately upgraded to the
// current (v4) layout (read-old / write-new). On any error before promotion the
// temp index is discarded, so the subject is never left partially rewritten.

// entryOutcome is the result the per-entry handler returns to the
// top-level loop. Exactly one of validatedEntry / skip / stop / fatal
// will be set.
type entryOutcome struct {
	validatedEntry *ValidatedEntry // success — caller advances offset by Entry.Size
	skip           bool            // malformed but we could find the next entry; caller skipped offset
	stop           bool            // malformed and we can't resync; caller breaks out
	fatal          error           // abort the whole workflow
}

// newFixMetaStore builds a no-I/O MetaStore for the repo-less repair path whose
// GetCurrentHashType() returns checksumType (the subject header's checksum_type)
// — so the production writer stamps the subject's checksum algorithm and header
// checksum_type rather than silently defaulting to SHA-256. It asserts the
// round-trip, failing loudly rather than re-hashing under the wrong algorithm.
func newFixMetaStore(metaDir string, checksumType uint16) (*MetaStore, error) {
	algo, err := GetHashAlgorithmByType(checksumType)
	if err != nil {
		return nil, fmt.Errorf("subject index has unsupported checksum type %d: %w", checksumType, err)
	}
	ms := initMetaStoreBase("", metaDir)
	ms.config = newConfigForHashType(algo.Name)
	if got := ms.GetCurrentHashType(); got != checksumType {
		return nil, fmt.Errorf("internal: synthesised hash type %d != subject checksum type %d", got, checksumType)
	}
	return ms, nil
}

// beScanEntryFromValidated builds a heap BEScanEntry carrying the validated
// entry's fields in the CURRENT (v4) wire layout, ready for EntrySerialiser.
// It mirrors NewBEScanEntry's buffer construction but sources every field from
// an already-decoded ValidatedEntry rather than a live FileInfo/Stat_t:
//   - Size is RECOMPUTED for the v4 layout (BESizeFromPathLen) — never copied
//     from ve.Entry.Size, which for a legacy v2/v3 subject reflects the smaller
//     legacy record size.
//   - CTimeWall/MTimeWall are already wall-encoded, so they are copied verbatim
//     (no re-encodeWallTime, which would corrupt the timestamps).
//   - The variable-length path is laid down after the fixed struct, exactly as
//     the production writer expects (this is the FR9 fix — the old writer
//     dropped it).
func beScanEntryFromValidated(ve *ValidatedEntry) *BEScanEntry {
	totalSize := BESizeFromPathLen(len(ve.Path))
	binaryData := make([]byte, totalSize)

	entry := (*binaryEntry)(unsafe.Pointer(&binaryData[0]))
	src := ve.Entry

	entry.Size = uint32(totalSize) //nolint:gosec // G115: totalSize = BESizeFromPathLen(path); bounded by path length, far below uint32 max
	entry.CTimeWall = src.CTimeWall
	entry.MTimeWall = src.MTimeWall
	entry.Dev = src.Dev
	entry.Ino = src.Ino
	entry.Mode = src.Mode
	entry.UID = src.UID
	entry.GID = src.GID
	entry.FileSize = src.FileSize
	entry.HashType = src.HashType
	entry.EntryFlags = src.EntryFlags
	copy(entry.Hash[:], src.Hash[:])

	// Copy path starting after the struct (matching NewBEScanEntry / RelativePath)
	pathOffset := int(unsafe.Sizeof(*entry))
	pathBytes := []byte(ve.Path)
	copy(binaryData[pathOffset:], pathBytes)
	if pathOffset+len(pathBytes) < len(binaryData) {
		binaryData[pathOffset+len(pathBytes)] = 0
	}

	return &BEScanEntry{
		BinaryEntryBase: NewBinaryEntryBase(BEScan),
		binaryData:      binaryData,
		relPath:         ve.Path,
	}
}

// writeRepairedIndex serialises the collected survivor entries through the
// production single-writer path (TempIndexWriter + EntrySerialiser) into a temp
// index beside the subject, then atomically promotes it over the subject
// (preserving a .pre-fix sibling unless --edit-in-place). checksumType is the
// subject header's checksum_type, preserved into the output. On ANY failure
// before the rename the temp index is removed — the subject is never left
// partially rewritten (no-partial-index invariant).
func writeRepairedIndex(indexFile string, checksumType uint16, entries []*ValidatedEntry, options FixEntryFlags) error {
	ms, err := newFixMetaStore(filepath.Dir(indexFile), checksumType)
	if err != nil {
		return err
	}

	tmpIndexFile := indexFile + ".fix.tmp"
	writer, err := NewTempIndexWriter(ms, tmpIndexFile)
	if err != nil {
		return fmt.Errorf("failed to create temp index writer: %w", err)
	}

	serialiser := NewEntrySerialiser()
	const batchSize = 64
	batch := make([][]byte, 0, batchSize)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := writer.WriteSerialised(batch); err != nil {
			return err
		}
		batch = batch[:0]
		return nil
	}

	serialiseErr := func() error {
		for _, ve := range entries {
			data, err := serialiser.Serialise(beScanEntryFromValidated(ve))
			if err != nil {
				return fmt.Errorf("failed to serialise entry %q: %w", ve.Path, err)
			}
			batch = append(batch, data)
			if len(batch) >= batchSize {
				if err := flush(); err != nil {
					return err
				}
			}
		}
		return flush()
	}()

	if serialiseErr != nil {
		_ = writer.Close()
		_ = os.Remove(tmpIndexFile)
		return serialiseErr
	}

	// Close finalises the header entry-count and footer checksum.
	if err := writer.Close(); err != nil {
		_ = os.Remove(tmpIndexFile)
		return fmt.Errorf("failed to finalise temp index: %w", err)
	}

	// Preserve the original (default) then atomically replace the subject.
	if err := PromoteRepairedIndex(tmpIndexFile, indexFile, options); err != nil {
		_ = os.Remove(tmpIndexFile)
		return fmt.Errorf("failed to replace original index file: %w", err)
	}

	return nil
}

// unfixableEntryCap bounds how many structurally-unfixable entries a repair
// walk tolerates before aborting. The three walk loops (edit/append/removal)
// share this single ceiling via capExceeded so their abort boundary cannot
// drift apart (task 28.2 LD4).
const unfixableEntryCap = 100

// capExceeded reports whether the running unfixable-entry count has passed the
// shared cap. The boundary is "> cap", i.e. the (cap+1)th unfixable entry trips
// it — preserved exactly from the pre-28.2 inline guards.
func capExceeded(unfixableCount int) bool {
	return unfixableCount > unfixableEntryCap
}

// collectForEdit reads indexFile, applies the field edit to entries whose path
// is in pathSet, and returns the surviving entries plus the subject's
// checksum_type — pure apart from the read, no index is written. This is the
// collect half of the LD5 collect/write split that lets RunFix gate the write
// on DryRun.
func collectForEdit(indexFile string, pathSet map[string]bool, field, value string, options FixEntryFlags) (collected []*ValidatedEntry, checksumType uint16, entriesFixed, entriesDiscarded int, err error) {
	data, err := os.ReadFile(indexFile) //nolint:gosec // G304: indexFile is the RunFix-confined subject (confineWriteDest against MetaDir) for the library path, or the explicitly-named CLI subject; never a raw selector
	if err != nil {
		return nil, 0, 0, 0, fmt.Errorf("failed to read index file: %w", err)
	}

	if len(data) < V2HeaderSize {
		return nil, 0, 0, 0, fmt.Errorf("index file too small: %d bytes", len(data))
	}

	checksumType = (*indexHeader)(unsafe.Pointer(&data[0])).ChecksumType

	if err := processAllEntriesWorkflow(data, pathSet, field, value, &collected, &entriesFixed, &entriesDiscarded, options); err != nil {
		// Return the partial counts alongside the error so a cap-trip surfaces
		// the discards through FixResult (NFR5/AC6); the caller does not write.
		return nil, checksumType, entriesFixed, entriesDiscarded, fmt.Errorf("failed to process entries: %w", err)
	}

	return collected, checksumType, entriesFixed, entriesDiscarded, nil
}

// ProcessEntriesWithWorkflow implements the complete safe workflow.
// Returns (entriesFixed, entriesDiscarded, error).
func ProcessEntriesWithWorkflow(indexFile string, pathSet map[string]bool, field, value string, options FixEntryFlags) (int, int, error) {
	collected, checksumType, entriesFixed, entriesDiscarded, err := collectForEdit(indexFile, pathSet, field, value, options)
	if err != nil {
		return 0, 0, err
	}

	if err := writeRepairedIndex(indexFile, checksumType, collected, options); err != nil {
		return 0, 0, err
	}

	return entriesFixed, entriesDiscarded, nil
}

// processAllEntriesWorkflow walks every entry in data and either
// applies the user's CLI field-edit (for paths in pathSet) or keeps
// the entry unchanged, collecting survivors. Corrupted entries are
// best-effort skipped until an unfixable-cap is hit.
func processAllEntriesWorkflow(data []byte, pathSet map[string]bool, field, value string, collected *[]*ValidatedEntry, entriesFixed, entriesDiscarded *int, options FixEntryFlags) error {
	header := (*indexHeader)(unsafe.Pointer(&data[0]))
	hdrSize := HeaderSizeForVersion(header.Version)
	entryCount := header.EntryCount
	entryData := data[hdrSize:]

	offset := 0
	unfixableCount := 0

	for i := uint32(0); i < entryCount && offset < len(entryData); i++ {
		outcome := processSingleEntry(entryData, &offset, int(i), pathSet, field, value, collected, entriesFixed, entriesDiscarded, options, header.Version)
		if outcome.fatal != nil {
			return outcome.fatal
		}
		if outcome.stop {
			break
		}
		if outcome.skip {
			unfixableCount++
			if capExceeded(unfixableCount) {
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
// skip/stop; on success it appends the kept (or fixed) entry to
// collected and the caller advances by the original Entry.Size.
func processSingleEntry(entryData []byte, offset *int, i int, pathSet map[string]bool, field, value string, collected *[]*ValidatedEntry, entriesFixed, entriesDiscarded *int, options FixEntryFlags, version uint32) entryOutcome {
	ve, err := NewValidatedEntry(entryData, i, *offset, version)
	if err != nil {
		return handleCorruptedEntry(entryData, offset, i, err, entriesDiscarded, options, version)
	}

	if !pathSet[ve.Path] {
		*collected = append(*collected, ve)
		return entryOutcome{validatedEntry: ve}
	}

	fixed, cmdErr := ve.ApplyFieldFix(field, value)
	if cmdErr != nil {
		if !options.Quiet {
			fmt.Fprintf(os.Stderr, "Warning: entry %d still broken after CLI command, discarding: %v\n", i, cmdErr)
		}
		*entriesDiscarded++
		if !trySkipToNextEntry(entryData, offset, version) {
			return entryOutcome{stop: true}
		}
		return entryOutcome{skip: true}
	}

	*collected = append(*collected, fixed)
	*entriesFixed++
	return entryOutcome{validatedEntry: ve}
}

// handleCorruptedEntry wraps the attempt-to-repair / skip-or-stop
// path for entries that failed structural validation.
func handleCorruptedEntry(entryData []byte, offset *int, i int, origErr error, entriesDiscarded *int, options FixEntryFlags, version uint32) entryOutcome {
	fixed, fixErr := attemptErrorFixAtOffsetValidated(entryData, i, *offset, origErr)
	if fixErr == nil {
		if !options.Quiet {
			fmt.Printf("Fixed corrupted entry %d\n", i)
		}
		return entryOutcome{validatedEntry: fixed}
	}
	if !options.Quiet {
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

// EntryJSON represents the JSON format for entry append operations
type EntryJSON struct {
	Path          string  `json:"path"`
	FlagIsDeleted bool    `json:"flag_is_deleted"`
	FileSize      int64   `json:"file_size"`
	Mode          uint32  `json:"mode"`
	UID           uint32  `json:"uid"`
	GID           uint32  `json:"gid"`
	Dev           uint64  `json:"dev"`
	Ino           *uint64 `json:"ino,omitempty"`
	MTime         string  `json:"mtime"`
	CTime         string  `json:"ctime"`
	Hash          string  `json:"hash"`
	HashType      uint16  `json:"hash_type"`
}

// ParseEntryFromJSON parses JSON data into a ValidatedEntry
func ParseEntryFromJSON(jsonData string) (*ValidatedEntry, error) {
	var entryJSON EntryJSON
	if err := json.Unmarshal([]byte(jsonData), &entryJSON); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	// Validate required fields
	if entryJSON.Path == "" {
		return nil, fmt.Errorf("path is required")
	}
	if entryJSON.Hash == "" {
		return nil, fmt.Errorf("hash is required")
	}

	// Parse times
	mtime, err := ParseTimeValue(entryJSON.MTime)
	if err != nil {
		return nil, fmt.Errorf("invalid mtime: %w", err)
	}

	ctime, err := ParseTimeValue(entryJSON.CTime)
	if err != nil {
		return nil, fmt.Errorf("invalid ctime: %w", err)
	}

	// Parse and validate hash
	hashBytes, err := ParseHashValue(entryJSON.Hash)
	if err != nil {
		return nil, fmt.Errorf("invalid hash: %w", err)
	}

	// Create the binaryEntry
	entry := &binaryEntry{
		Size:       0, // Recomputed for the v4 layout when serialised (beScanEntryFromValidated)
		CTimeWall:  ctime,
		MTimeWall:  mtime,
		Dev:        entryJSON.Dev,
		Ino:        0, // Default if not provided
		Mode:       entryJSON.Mode,
		UID:        entryJSON.UID,
		GID:        entryJSON.GID,
		FileSize:   entryJSON.FileSize,
		EntryFlags: 0,
		HashType:   entryJSON.HashType,
	}

	// Set Ino if provided
	if entryJSON.Ino != nil {
		entry.Ino = *entryJSON.Ino
	}

	// Set deleted flag
	if entryJSON.FlagIsDeleted {
		entry.EntryFlags |= EntryFlagDeleted
	}

	// Set hash (copy into fixed-size array)
	copy(entry.Hash[:], hashBytes)

	return &ValidatedEntry{
		Entry: entry,
		Path:  entryJSON.Path,
	}, nil
}

// collectForAppend reads indexFile, keeps the existing valid entries, and
// appends newEntry — pure apart from the read (LD5 collect half).
func collectForAppend(indexFile string, newEntry *ValidatedEntry, options FixEntryFlags) (collected []*ValidatedEntry, checksumType uint16, entriesDiscarded int, err error) {
	data, err := os.ReadFile(indexFile) //nolint:gosec // G304: indexFile is the RunFix-confined subject (confineWriteDest against MetaDir) for the library path, or the explicitly-named CLI subject; never a raw selector
	if err != nil {
		return nil, 0, 0, fmt.Errorf("failed to read index file: %w", err)
	}

	if len(data) < V2HeaderSize {
		return nil, 0, 0, fmt.Errorf("index file too small: %d bytes", len(data))
	}

	header := (*indexHeader)(unsafe.Pointer(&data[0]))
	checksumType = header.ChecksumType

	collected = make([]*ValidatedEntry, 0, int(header.EntryCount)+1)
	if err := processAllEntriesForAppend(data, &collected, &entriesDiscarded, options); err != nil {
		return nil, checksumType, entriesDiscarded, fmt.Errorf("failed to process existing entries: %w", err)
	}
	collected = append(collected, newEntry)

	return collected, checksumType, entriesDiscarded, nil
}

// ProcessEntriesWithAppend processes entries and appends a new entry
func ProcessEntriesWithAppend(indexFile string, newEntry *ValidatedEntry, options FixEntryFlags) (int, int, error) {
	collected, checksumType, entriesDiscarded, err := collectForAppend(indexFile, newEntry, options)
	if err != nil {
		return 0, 0, err
	}

	if err := writeRepairedIndex(indexFile, checksumType, collected, options); err != nil {
		return 0, 0, err
	}

	return 1, entriesDiscarded, nil
}

// collectForRemoval reads indexFile and keeps the entries whose path is NOT in
// pathSet — pure apart from the read (LD5 collect half).
func collectForRemoval(indexFile string, pathSet map[string]bool, options FixEntryFlags) (collected []*ValidatedEntry, checksumType uint16, entriesRemoved, entriesDiscarded int, err error) {
	data, err := os.ReadFile(indexFile) //nolint:gosec // G304: indexFile is the RunFix-confined subject (confineWriteDest against MetaDir) for the library path, or the explicitly-named CLI subject; never a raw selector
	if err != nil {
		return nil, 0, 0, 0, fmt.Errorf("failed to read index file: %w", err)
	}

	if len(data) < V2HeaderSize {
		return nil, 0, 0, 0, fmt.Errorf("index file too small: %d bytes", len(data))
	}

	checksumType = (*indexHeader)(unsafe.Pointer(&data[0])).ChecksumType

	if err := processAllEntriesForRemoval(data, pathSet, &collected, &entriesRemoved, &entriesDiscarded, options); err != nil {
		return nil, checksumType, entriesRemoved, entriesDiscarded, fmt.Errorf("failed to process entries: %w", err)
	}

	return collected, checksumType, entriesRemoved, entriesDiscarded, nil
}

// ProcessEntriesWithRemoval processes entries and removes matching paths
func ProcessEntriesWithRemoval(indexFile string, pathSet map[string]bool, options FixEntryFlags) (int, int, error) {
	collected, checksumType, entriesRemoved, entriesDiscarded, err := collectForRemoval(indexFile, pathSet, options)
	if err != nil {
		return 0, 0, err
	}

	if err := writeRepairedIndex(indexFile, checksumType, collected, options); err != nil {
		return 0, 0, err
	}

	return entriesRemoved, entriesDiscarded, nil
}

// processAllEntriesForAppend collects the existing (valid) entries for an
// append operation, best-effort skipping corruption up to the unfixable cap.
func processAllEntriesForAppend(data []byte, collected *[]*ValidatedEntry, entriesDiscarded *int, options FixEntryFlags) error {
	header := (*indexHeader)(unsafe.Pointer(&data[0]))
	hdrSize := HeaderSizeForVersion(header.Version)
	entryCount := header.EntryCount
	entryData := data[hdrSize:]

	offset := 0
	unfixableEntryCount := 0

	for i := uint32(0); i < entryCount && offset < len(entryData); i++ {
		validatedEntry, err := NewValidatedEntry(entryData, int(i), offset, header.Version)
		if err != nil {
			if !options.Quiet {
				fmt.Fprintf(os.Stderr, "Warning: entry %d unfixable, discarding: %v\n", i, err)
			}
			*entriesDiscarded++
			unfixableEntryCount++

			if capExceeded(unfixableEntryCount) {
				return fmt.Errorf("too many unfixable entries (%d), aborting", unfixableEntryCount)
			}

			if !trySkipToNextEntry(entryData, &offset, header.Version) {
				break
			}
			continue
		}

		*collected = append(*collected, validatedEntry)
		offset += int(validatedEntry.Entry.Size)
	}

	return nil
}

// processAllEntriesForRemoval collects the entries that do NOT match a removal
// path, best-effort skipping corruption up to the unfixable cap.
func processAllEntriesForRemoval(data []byte, pathSet map[string]bool, collected *[]*ValidatedEntry, entriesRemoved, entriesDiscarded *int, options FixEntryFlags) error {
	header := (*indexHeader)(unsafe.Pointer(&data[0]))
	hdrSize := HeaderSizeForVersion(header.Version)
	entryCount := header.EntryCount
	entryData := data[hdrSize:]

	offset := 0
	unfixableEntryCount := 0

	for i := uint32(0); i < entryCount && offset < len(entryData); i++ {
		validatedEntry, err := NewValidatedEntry(entryData, int(i), offset, header.Version)
		if err != nil {
			if !options.Quiet {
				fmt.Fprintf(os.Stderr, "Warning: entry %d unfixable, discarding: %v\n", i, err)
			}
			*entriesDiscarded++
			unfixableEntryCount++

			if capExceeded(unfixableEntryCount) {
				return fmt.Errorf("too many unfixable entries (%d), aborting", unfixableEntryCount)
			}

			if !trySkipToNextEntry(entryData, &offset, header.Version) {
				break
			}
			continue
		}

		if pathSet[validatedEntry.Path] {
			*entriesRemoved++
			if !options.Quiet {
				fmt.Printf("Removing entry: %s\n", validatedEntry.Path)
			}
		} else {
			*collected = append(*collected, validatedEntry)
		}

		offset += int(validatedEntry.Entry.Size)
	}

	return nil
}
