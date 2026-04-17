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
//
// See indexHeader struct in index.go for the full on-disk layout diagram.
// Sizes account for explicit alignment padding fields (_Pad0, _Pad1).
const (
	// V2HeaderSize is 88 bytes — this is technically a bug: the full v2 struct
	// is 96 bytes (including tail padding), so entry data starts 8 bytes inside
	// the Checksum[60:64] + struct padding region. This works because SHA-1 (20 bytes)
	// and SHA-256 (32 bytes) never use Checksum[60:]. Shipped in v2, now frozen.
	V2HeaderSize        = 88  // see comment above
	HeaderSize          = 104 // v2 fields + checksum(4 remaining) + pad(4) + timestamp(8)
	ChecksumSize        = 64  // Maximum checksum size (512 bits)
	CurrentIndexVersion = 3   // Current index file format version
	MinIndexVersion     = 2   // Minimum supported index version
	TimestampMinVersion = 3   // First version with Timestamp field in header
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
	EntryFlagHashed  uint16 = 1 << 1 // Entry has been hashed
)

// Import merge strategies from zerocopyskiplist
const (
	MergeTheirs = zcsl.MergeTheirs
	MergeOurs   = zcsl.MergeOurs
	MergeError  = zcsl.MergeError
)
