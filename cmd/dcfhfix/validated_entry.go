package main

import (
	"fmt"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

// ValidatedEntry holds a validated binaryEntry along with its extracted path
// This solves the problem of variable-length paths in the binary format
type ValidatedEntry struct {
	Entry *binaryEntry
	Path  string
}

// NewValidatedEntry creates a ValidatedEntry from safe accessor validation
func NewValidatedEntry(data []byte, entryIdx int, offset int) (*ValidatedEntry, error) {
	// First create safe accessor to validate the entry structure
	accessor, err := NewSafeEntryAccessor(data, entryIdx, offset)
	if err != nil {
		return nil, err
	}

	// Extract the path first (before creating the entry struct)
	pathStr, err := accessor.GetPath()
	if err != nil {
		return nil, err
	}

	// Validate path is reasonable
	if len(pathStr) == 0 {
		return nil, fmt.Errorf("path is empty")
	}
	if len(pathStr) > 4000 { // Reasonable maximum
		return nil, fmt.Errorf("path too long: %d characters", len(pathStr))
	}

	// Create the binaryEntry struct with all validated fields
	entry := &binaryEntry{}

	// Read all fields safely
	entry.Size, err = accessor.GetSize()
	if err != nil {
		return nil, err
	}

	entry.CTimeWall, err = accessor.GetCTimeWall()
	if err != nil {
		return nil, err
	}

	entry.MTimeWall, err = accessor.GetMTimeWall()
	if err != nil {
		return nil, err
	}

	entry.Dev, err = accessor.GetDev()
	if err != nil {
		return nil, err
	}

	entry.Ino, err = accessor.GetIno()
	if err != nil {
		return nil, err
	}

	entry.Mode, err = accessor.GetMode()
	if err != nil {
		return nil, err
	}

	entry.UID, err = accessor.GetUID()
	if err != nil {
		return nil, err
	}

	entry.GID, err = accessor.GetGID()
	if err != nil {
		return nil, err
	}

	entry.FileSize, err = accessor.GetFileSize()
	if err != nil {
		return nil, err
	}

	entry.EntryFlags, err = accessor.GetEntryFlags()
	if err != nil {
		return nil, err
	}

	entry.HashType, err = accessor.GetHashType()
	if err != nil {
		return nil, err
	}

	entry.Hash, err = accessor.GetHash()
	if err != nil {
		return nil, err
	}

	// Don't store the path in the Path field - it's variable length
	// We'll handle it separately in the ValidatedEntry

	return &ValidatedEntry{
		Entry: entry,
		Path:  pathStr,
	}, nil
}

// ApplyFieldFix applies a field modification to this validated entry
func (ve *ValidatedEntry) ApplyFieldFix(field, value string) (*ValidatedEntry, error) {
	// Create a copy to avoid modifying the original
	fixed := *ve.Entry

	switch field {
	case "ctime":
		wallTime, err := parseTimeValue(value)
		if err != nil {
			return nil, err
		}
		fixed.CTimeWall = wallTime
	case "mtime":
		wallTime, err := parseTimeValue(value)
		if err != nil {
			return nil, err
		}
		fixed.MTimeWall = wallTime
	case "mode":
		val, err := parseUint32(value)
		if err != nil {
			return nil, err
		}
		fixed.Mode = val
	case "uid":
		val, err := parseUint32(value)
		if err != nil {
			return nil, err
		}
		fixed.UID = val
	case "gid":
		val, err := parseUint32(value)
		if err != nil {
			return nil, err
		}
		fixed.GID = val
	case "file_size":
		val, err := parseUint64(value)
		if err != nil {
			return nil, err
		}
		fixed.FileSize = val
	case "flag_is_deleted":
		val, err := parseBoolValue(value)
		if err != nil {
			return nil, err
		}
		if val {
			fixed.EntryFlags |= dcfh.EntryFlagDeleted
		} else {
			fixed.EntryFlags &^= dcfh.EntryFlagDeleted
		}
	default:
		return nil, fmt.Errorf("unsupported field: %s", field)
	}

	return &ValidatedEntry{
		Entry: &fixed,
		Path:  ve.Path, // Path doesn't change
	}, nil
}
