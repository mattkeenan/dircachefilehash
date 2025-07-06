package dircachefilehash

import (
	"strings"

	zcsl "github.com/mattkeenan/zerocopyskiplist"
)

// Context constants for skiplist operations
const (
	MainContext  = "main"
	CacheContext = "cache"
	ScanContext  = "scan"
	TempContext  = "temp"
)

// File constants
const (
	MainIndex  = "main.idx"
	CacheIndex = "cache.idx"
	TempIndex  = "temp-%d-%d.idx"
)

// Header and file format constants
const (
	HeaderSize          = 88 // signature(4) + byte_order(8) + version(4) + entry_count(4) + flags(2) + checksum_type(2) + checksum(64)
	ChecksumSize        = 64 // Maximum checksum size (512 bits)
	CurrentIndexVersion = 1  // Current index file format version
)

// Byte order magic for file format validation
const ByteOrderMagic uint64 = 0x0102030405060708

// Hash type constants
const (
	HashTypeSHA1   uint16 = 1 // SHA-1 (20 bytes)
	HashTypeSHA256 uint16 = 2 // SHA-256 (32 bytes)
	HashTypeSHA512 uint16 = 3 // SHA-512 (64 bytes)
)

// HashTypeName returns the human-readable name for a hash type
func HashTypeName(hashType uint16) string {
	switch hashType {
	case HashTypeSHA1:
		return "sha1"
	case HashTypeSHA256:
		return "sha256"
	case HashTypeSHA512:
		return "sha512"
	default:
		return "unknown"
	}
}

// HashTypeFromName returns the hash type constant from a name (case-insensitive)
func HashTypeFromName(name string) (uint16, bool) {
	switch strings.ToLower(name) {
	case "sha1":
		return HashTypeSHA1, true
	case "sha256":
		return HashTypeSHA256, true
	case "sha512":
		return HashTypeSHA512, true
	default:
		return 0, false
	}
}

// Hash size constants
const (
	HashSizeSHA1   = 20 // SHA-1 hash size in bytes
	HashSizeSHA256 = 32 // SHA-256 hash size in bytes
	HashSizeSHA512 = 64 // SHA-512 hash size in bytes
)

// Index header flags
const (
	IndexFlagSparse uint16 = 1 << 0 // Sparse index flag
	IndexFlagClean  uint16 = 1 << 1 // Index file is in clean/complete state
)

// Entry flags
const (
	EntryFlagDeleted uint16 = 1 << 0 // Entry marked as deleted
)

// Import merge strategies from zerocopyskiplist
const (
	MergeTheirs = zcsl.MergeTheirs
	MergeOurs   = zcsl.MergeOurs
	MergeError  = zcsl.MergeError
)
