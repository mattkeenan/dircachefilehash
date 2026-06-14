package dircachefilehash

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Value parsers shared by the dcfhfix repair workflow (ApplyFieldFix,
// ParseEntryFromJSON) and the cmd/dcfhfix field validators. They were relocated
// from cmd/dcfhfix/main.go alongside the entry workflow so the parsing lives
// next to the only code that produces ValidatedEntry values.

// ParseUint32 parses a string value as uint32 with support for hex (0x), octal
// (leading 0) and decimal representations.
func ParseUint32(value string) (uint32, error) {
	// Handle hex values (with or without 0x prefix)
	if strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0X") {
		val, err := strconv.ParseUint(value[2:], 16, 32)
		return uint32(val), err
	}
	// Handle octal values (with 0 prefix)
	if strings.HasPrefix(value, "0") && len(value) > 1 {
		val, err := strconv.ParseUint(value, 8, 32)
		return uint32(val), err
	}
	// Handle decimal values
	val, err := strconv.ParseUint(value, 10, 32)
	return uint32(val), err
}

// ParseInt64 parses a string value as int64 with support for hex/octal/decimal.
// Used for file_size, which is a signed int64 (off_t-style) on disk; parsing
// signed end-to-end avoids a uint64->int64 narrowing conversion.
func ParseInt64(value string) (int64, error) {
	// Handle hex values (with or without 0x prefix)
	if strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0X") {
		val, err := strconv.ParseInt(value[2:], 16, 64)
		return val, err
	}
	// Handle octal values (with 0 prefix)
	if strings.HasPrefix(value, "0") && len(value) > 1 {
		val, err := strconv.ParseInt(value, 8, 64)
		return val, err
	}
	// Handle decimal values
	val, err := strconv.ParseInt(value, 10, 64)
	return val, err
}

// ParseTimeValue parses time in various formats and returns wall time.
func ParseTimeValue(value string) (uint64, error) {
	// Try ISO 8601 format first
	if t, err := time.Parse("2006-01-02T15:04:05.000000000Z", value); err == nil {
		return TimeToWall(t), nil
	}
	if t, err := time.Parse("2006-01-02T15:04:05Z", value); err == nil {
		return TimeToWall(t), nil
	}
	// Try Unix timestamp
	if timestamp, err := strconv.ParseInt(value, 10, 64); err == nil {
		t := time.Unix(timestamp, 0)
		return TimeToWall(t), nil
	}
	return 0, fmt.Errorf("invalid time format, use ISO 8601 (2006-01-02T15:04:05Z) or Unix timestamp")
}

// ParseHashValue parses and validates a hash string.
func ParseHashValue(value string) ([]byte, error) {
	// Remove any 0x prefix
	if strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0X") {
		value = value[2:]
	}

	// Decode hex string
	hash, err := hex.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("invalid hex string: %w", err)
	}

	// Validate hash length (must be 20, 32, or 64 bytes for SHA1, SHA256, SHA512)
	if len(hash) != 20 && len(hash) != 32 && len(hash) != 64 {
		return nil, fmt.Errorf("invalid hash length %d, must be 20 (SHA1), 32 (SHA256), or 64 (SHA512) bytes", len(hash))
	}

	return hash, nil
}

// ParseBoolValue parses various boolean representations.
func ParseBoolValue(value string) (bool, error) {
	switch strings.ToLower(value) {
	case "true", "1", "yes", "on":
		return true, nil
	case "false", "0", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean value: %s (use true/false, 1/0, yes/no, on/off)", value)
	}
}
