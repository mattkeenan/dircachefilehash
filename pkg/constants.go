package dircachefilehash

import zcsl "github.com/mattkeenan/zerocopyskiplist"

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
	HeaderSize   = 24 // signature(4) + byte_order(8) + version(4) + entry_count(4) + flags(4)
	ChecksumSize = 20 // SHA-1 checksum size
)

// Byte order magic for file format validation
const ByteOrderMagic uint64 = 0x0102030405060708

// Hash type constants
const (
	HashTypeSHA1   uint16 = 1 // SHA-1 (20 bytes)
	HashTypeSHA256 uint16 = 2 // SHA-256 (32 bytes)
	HashTypeSHA512 uint16 = 3 // SHA-512 (64 bytes)
)

// Hash size constants
const (
	HashSizeSHA1   = 20 // SHA-1 hash size in bytes
	HashSizeSHA256 = 32 // SHA-256 hash size in bytes
	HashSizeSHA512 = 64 // SHA-512 hash size in bytes
)

// Index header flags
const (
	IndexFlagSparse uint32 = 1 << 0 // Sparse index flag
	IndexFlagClean  uint32 = 1 << 1 // Index file is in clean/complete state
)

// Entry flags
const (
	EntryFlagDeleted uint32 = 1 << 0 // Entry marked as deleted
)

// Import merge strategies from zerocopyskiplist
const (
	MergeTheirs = zcsl.MergeTheirs
	MergeOurs   = zcsl.MergeOurs
	MergeError  = zcsl.MergeError
)
