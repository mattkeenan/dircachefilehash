package dircachefilehash

import (
	"strings"

	"github.com/mattkeenan/dircachefilehash/pkg/format"
	zcsl "github.com/mattkeenan/zerocopyskiplist"
)

// Context constants for skiplist operations
const (
	MainContext  = "main"
	CacheContext = "cache"
	ScanContext  = "scan"
)

// ContextForIndexBasename returns the context tag appropriate to an index
// file's basename. Use this when the caller has only a path and needs to
// pick the right context for a skiplist that may later participate in a
// merge. Unknown basenames fall back to ScanContext (the safest tag for
// ad-hoc index files).
func ContextForIndexBasename(basename string) string {
	switch basename {
	case MainIndex:
		return MainContext
	case CacheIndex:
		return CacheContext
	default:
		return ScanContext
	}
}

// IndexTypeForBasename returns the user-facing index-type label for a
// basename, matching the RefType* vocabulary from pkg/openref.go.
// Unknown basenames return RefTypeFile.
func IndexTypeForBasename(basename string) string {
	switch {
	case basename == MainIndex:
		return RefTypeMain
	case basename == CacheIndex:
		return RefTypeCache
	case strings.HasPrefix(basename, "scan-") && strings.HasSuffix(basename, ".idx"):
		return RefTypeScan
	default:
		return RefTypeFile
	}
}

// File constants
const (
	MainIndex  = "main.idx"
	CacheIndex = "cache.idx"
	TempIndex  = "temp-%d-%d.idx"
)

// On-disk format constants are owned by pkg/format. Re-exported here so the
// core package and its existing call sites refer to a single source of truth.
const (
	V2HeaderSize        = format.V2HeaderSize
	HeaderSize          = format.HeaderSize
	ChecksumSize        = format.ChecksumSize
	CurrentIndexVersion = format.CurrentIndexVersion
	MinIndexVersion     = format.MinIndexVersion
	TimestampMinVersion = format.TimestampMinVersion
)

// ByteOrderMagic re-exports the format byte-order detection magic.
const ByteOrderMagic = format.ByteOrderMagic

// Hash type constants (re-exported from pkg/format).
const (
	HashTypeSHA1   = format.HashTypeSHA1
	HashTypeSHA256 = format.HashTypeSHA256
	HashTypeSHA512 = format.HashTypeSHA512
)

// HashTypeName returns the human-readable name for a hash type.
func HashTypeName(hashType uint16) string { return format.HashTypeName(hashType) }

// HashTypeFromName returns the hash type constant from a name (case-insensitive).
func HashTypeFromName(name string) (uint16, bool) { return format.HashTypeFromName(name) }

// Hash size constants (re-exported from pkg/format).
const (
	HashSizeSHA1   = format.HashSizeSHA1
	HashSizeSHA256 = format.HashSizeSHA256
	HashSizeSHA512 = format.HashSizeSHA512
)

// Index header flags (re-exported from pkg/format).
const (
	IndexFlagSparse = format.IndexFlagSparse
	IndexFlagClean  = format.IndexFlagClean
)

// Entry flags (re-exported from pkg/format).
const (
	EntryFlagDeleted = format.EntryFlagDeleted
	EntryFlagHashed  = format.EntryFlagHashed
)

// Import merge strategies from zerocopyskiplist
const (
	MergeTheirs = zcsl.MergeTheirs
	MergeOurs   = zcsl.MergeOurs
	MergeError  = zcsl.MergeError
)
