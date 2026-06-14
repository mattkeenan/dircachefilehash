package dircachefilehash

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unsafe"
)

// This file holds the dcfhfix header-edit machinery relocated from cmd/dcfhfix
// (task 28.2). header edit is a surgical write: it changes one header field and
// rewrites header+entry-bytes verbatim. It deliberately does NOT route through
// writeRepairedIndex (the entry-reserialise single-writer path) because that
// path cannot express version/flags/signature edits and always re-stamps the
// current layout — so header edit keeps its own writer, behaviour-preserved
// from the pre-28.2 cmd implementation. Confinement (RunFix) still bounds its
// write destination for the library path (D2/NFR4).

// ParseUint16 parses a uint16 from a decimal, hex (0x…), or octal (0…) string.
// Relocated from cmd/dcfhfix so both the header field editors here and the CLI
// entry validators share one parser.
func ParseUint16(value string) (uint16, error) {
	// Hex (with or without 0x prefix handled here as 0x only; bare hex is decimal)
	if strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0X") {
		val, err := strconv.ParseUint(value[2:], 16, 16)
		return uint16(val), err
	}
	// Octal (leading 0)
	if strings.HasPrefix(value, "0") && len(value) > 1 {
		val, err := strconv.ParseUint(value, 8, 16)
		return uint16(val), err
	}
	// Decimal
	val, err := strconv.ParseUint(value, 10, 16)
	return uint16(val), err
}

// headerFieldEditor is the per-field implementation of header edit. validate
// runs first (may reject with a formatted error); apply mutates the copied
// header in place. A nil apply means the field is validate-only (always
// rejected as not editable).
type headerFieldEditor struct {
	validate func(value string) error
	apply    func(h *indexHeader, value string)
}

var headerFieldEditors = map[string]headerFieldEditor{
	"signature": {
		validate: func(v string) error {
			if len(v) != 4 {
				return fmt.Errorf("signature must be exactly 4 characters, got %d", len(v))
			}
			return nil
		},
		apply: func(h *indexHeader, v string) { copy(h.Signature[:], v) },
	},
	"version": {
		validate: func(v string) error {
			if _, err := ParseUint32(v); err != nil {
				return fmt.Errorf("invalid version value: %w", err)
			}
			return nil
		},
		apply: func(h *indexHeader, v string) { val, _ := ParseUint32(v); h.Version = val },
	},
	"flags": {
		validate: func(v string) error {
			if _, err := ParseUint16(v); err != nil {
				return fmt.Errorf("invalid flags value: %w", err)
			}
			return nil
		},
		apply: func(h *indexHeader, v string) { val, _ := ParseUint16(v); h.Flags = val },
	},
	"checksum_type": {
		validate: func(v string) error {
			if _, err := ParseUint16(v); err != nil {
				return fmt.Errorf("invalid checksum_type value: %w", err)
			}
			return nil
		},
		apply: func(h *indexHeader, v string) { val, _ := ParseUint16(v); h.ChecksumType = val },
	},
	"entry_count": {validate: func(string) error {
		return fmt.Errorf("entry_count is auto-calculated and cannot be manually edited")
	}},
	"checksum": {validate: func(string) error {
		return fmt.Errorf("checksum is auto-calculated and cannot be manually edited")
	}},
	"byte_order": {validate: func(string) error {
		return fmt.Errorf("byte_order is fixed and cannot be edited")
	}},
}

// ValidateHeaderEdit checks that field is a known, editable header field and
// that value parses, without touching the index. Used for the dry-run preview
// and as the pre-write gate.
func ValidateHeaderEdit(field, value string) error {
	editor, ok := headerFieldEditors[field]
	if !ok {
		return fmt.Errorf("unknown header field: %s", field)
	}
	if err := editor.validate(value); err != nil {
		return err
	}
	if editor.apply == nil {
		return fmt.Errorf("field %q is not editable", field)
	}
	return nil
}

// ApplyHeaderEdit rewrites indexFile with field set to value: it copies the
// existing header, applies the edit, and writes the new header followed by the
// original entry bytes to a temp sibling, then atomically promotes it
// (preserving a .pre-fix sibling unless --edit-in-place). The entry bytes and
// (per the pre-28.2 behaviour) the checksum are NOT recomputed.
func ApplyHeaderEdit(indexFile, field, value string, options FixEntryFlags) error {
	if err := ValidateHeaderEdit(field, value); err != nil {
		return err
	}
	editor := headerFieldEditors[field]

	data, err := os.ReadFile(indexFile) //nolint:gosec // G304: index path confined to the resolved subject/MetaDir by RunFix (confineWriteDest) before this read; arbitrary-path reads are the read-only inspection ops, not header edit
	if err != nil {
		return fmt.Errorf("failed to read index file: %w", err)
	}
	if len(data) < V2HeaderSize {
		return fmt.Errorf("index file too small: %d bytes", len(data))
	}

	origHeader := (*indexHeader)(unsafe.Pointer(&data[0]))
	if string(origHeader.Signature[:]) != "dcfh" {
		return fmt.Errorf("invalid signature: %s", string(origHeader.Signature[:]))
	}

	newHeader := *origHeader
	editor.apply(&newHeader, value)
	newHeader.EntryCount = origHeader.EntryCount

	origHdrSize := HeaderSizeForVersion(origHeader.Version)

	tempFile := indexFile + ".tmp"
	defer func() {
		if _, statErr := os.Stat(tempFile); statErr == nil {
			_ = os.Remove(tempFile)
		}
	}()

	if err := writeHeaderAndEntries(&newHeader, data[origHdrSize:], tempFile); err != nil {
		return fmt.Errorf("failed to write index with custom header: %w", err)
	}

	if err := PromoteRepairedIndex(tempFile, indexFile, options); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// writeHeaderAndEntries writes a full HeaderSize header block followed by the
// supplied entry bytes to outputPath, fsync'd. It is the relocated
// writeIndexWithCustomHeader: a simplified writer that does not recalculate the
// footer checksum (preserved pre-28.2 behaviour).
func writeHeaderAndEntries(header *indexHeader, entryBytes []byte, outputPath string) error {
	file, err := os.Create(outputPath) //nolint:gosec // G304: temp sibling of the RunFix-confined subject path; same directory, validated before the write
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer func() { _ = file.Close() }()

	headerBytes := (*[HeaderSize]byte)(unsafe.Pointer(header))
	if _, err := file.Write(headerBytes[:]); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}

	if len(entryBytes) > 0 {
		if _, err := file.Write(entryBytes); err != nil {
			return fmt.Errorf("failed to write entries: %w", err)
		}
	}

	return file.Sync()
}
