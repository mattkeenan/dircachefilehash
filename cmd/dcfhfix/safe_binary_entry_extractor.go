package main

import (
	"fmt"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

// getBinaryEntryFromOffset safely extracts a complete binaryEntry from raw data at offset
// Returns the entry only if it passes ALL validation tests
func getBinaryEntryFromOffset(data []byte, entryIdx int, offset int) (*binaryEntry, error) {
	// First create safe accessor to validate the entry structure
	accessor, err := NewSafeEntryAccessor(data, entryIdx, offset)
	if err != nil {
		return nil, fmt.Errorf("entry structure invalid: %w", err)
	}
	
	// Create new binaryEntry struct and populate all fields safely
	entry := &binaryEntry{}
	
	// Read all fields through safe accessor
	entry.Size, err = accessor.GetSize()
	if err != nil {
		return nil, fmt.Errorf("size field corrupted: %w", err)
	}
	
	entry.CTimeWall, err = accessor.GetCTimeWall()
	if err != nil {
		return nil, fmt.Errorf("ctime field corrupted: %w", err)
	}
	
	entry.MTimeWall, err = accessor.GetMTimeWall()
	if err != nil {
		return nil, fmt.Errorf("mtime field corrupted: %w", err)
	}
	
	entry.Dev, err = accessor.GetDev()
	if err != nil {
		return nil, fmt.Errorf("dev field corrupted: %w", err)
	}
	
	entry.Ino, err = accessor.GetIno()
	if err != nil {
		return nil, fmt.Errorf("ino field corrupted: %w", err)
	}
	
	entry.Mode, err = accessor.GetMode()
	if err != nil {
		return nil, fmt.Errorf("mode field corrupted: %w", err)
	}
	
	entry.UID, err = accessor.GetUID()
	if err != nil {
		return nil, fmt.Errorf("uid field corrupted: %w", err)
	}
	
	entry.GID, err = accessor.GetGID()
	if err != nil {
		return nil, fmt.Errorf("gid field corrupted: %w", err)
	}
	
	entry.FileSize, err = accessor.GetFileSize()
	if err != nil {
		return nil, fmt.Errorf("file_size field corrupted: %w", err)
	}
	
	entry.EntryFlags, err = accessor.GetEntryFlags()
	if err != nil {
		return nil, fmt.Errorf("entry_flags field corrupted: %w", err)
	}
	
	entry.HashType, err = accessor.GetHashType()
	if err != nil {
		return nil, fmt.Errorf("hash_type field corrupted: %w", err)
	}
	
	entry.Hash, err = accessor.GetHash()
	if err != nil {
		return nil, fmt.Errorf("hash field corrupted: %w", err)
	}
	
	// Get path and copy into Path field (with null termination)
	pathStr, err := accessor.GetPath()
	if err != nil {
		return nil, fmt.Errorf("path field corrupted: %w", err)
	}
	
	// Validate path is reasonable
	if len(pathStr) == 0 {
		return nil, fmt.Errorf("path is empty")
	}
	if len(pathStr) > 4000 { // Reasonable maximum
		return nil, fmt.Errorf("path too long: %d characters", len(pathStr))
	}
	
	// Store path in a way that can be retrieved later
	// Note: The actual path is variable length and extends beyond the struct
	// For now, store the first 8 bytes in the Path field for compatibility
	if len(pathStr) > 0 {
		copyLen := len(pathStr)
		if copyLen > 8 {
			copyLen = 8
		}
		copy(entry.Path[:copyLen], pathStr)
	}
	
	return entry, nil
}

// attemptErrorFixAtOffset tries to fix common corruption issues
func attemptErrorFixAtOffset(data []byte, entryIdx int, offset int, originalErr error) (*binaryEntry, error) {
	// Based on the error type, attempt specific fixes
	errStr := originalErr.Error()
	
	switch {
	case contains(errStr, "size field corrupted"):
		// Try to reconstruct size from other fields if possible
		return attemptSizeFix(data, entryIdx, offset)
	case contains(errStr, "path field corrupted"):
		// Try to extract a valid path by finding null terminator
		return attemptPathFix(data, entryIdx, offset)
	case contains(errStr, "extends beyond data bounds"):
		// Try to truncate entry to fit within bounds
		return attemptBoundsFix(data, entryIdx, offset)
	default:
		// Cannot fix this type of corruption
		return nil, fmt.Errorf("unfixable corruption: %w", originalErr)
	}
}

// fixCommandAtOffset applies the user's fix command to a valid entry
func fixCommandAtOffset(entry *binaryEntry, field, value string) (*binaryEntry, error) {
	// Create a copy to avoid modifying the original
	fixed := *entry
	
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
	
	return &fixed, nil
}

// Helper functions for specific fix attempts
func attemptSizeFix(data []byte, entryIdx int, offset int) (*binaryEntry, error) {
	// Try to calculate reasonable size based on other entries or minimum size
	return nil, fmt.Errorf("size fix not implemented")
}

func attemptPathFix(data []byte, entryIdx int, offset int) (*binaryEntry, error) {
	// Try to find a valid path by scanning for null terminator
	return nil, fmt.Errorf("path fix not implemented")
}

func attemptBoundsFix(data []byte, entryIdx int, offset int) (*binaryEntry, error) {
	// Try to truncate entry to fit within available data
	return nil, fmt.Errorf("bounds fix not implemented")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr
}