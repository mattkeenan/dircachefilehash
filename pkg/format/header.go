package format

import (
	"fmt"
	"time"
)

// Header represents the index file header in host byte order
// (cast directly onto mmap'd memory).
//
// On-disk layout (64-bit little-endian):
//
//	Offset  Size  Field
//	0       4     Signature      "dcfh"
//	4       4     _Pad0          alignment padding for ByteOrder
//	8       8     ByteOrder      0x0102030405060708
//	16      4     Version        2 or 3
//	20      4     EntryCount
//	24      2     Flags
//	26      2     ChecksumType
//	28      64    Checksum       SHA-1/256/512 (v2 entry data starts at offset 88, inside unused tail)
//	92      4     _Pad1          alignment padding for Timestamp (v3 only)
//	96      8     Timestamp      Unix seconds (v3 only, not covered by checksum)
//	---     ---   ---
//	v2 total: 88 bytes used (entries start here; overlaps Checksum[60:64] + _Pad1)
//	v3 total: 104 bytes
type Header struct {
	Signature    [4]byte  // offset 0:   "dcfh" signature
	_            [4]byte  // offset 4:   alignment padding for ByteOrder (_Pad0)
	ByteOrder    uint64   // offset 8:   byte order detection magic - MUST be checked before other fields
	Version      uint32   // offset 16:  index version (host order)
	EntryCount   uint32   // offset 20:  number of entries (host order)
	Flags        FlagBits // offset 24:  index flags (host order)
	ChecksumType HashKind // offset 26:  checksum algorithm type
	Checksum     [64]byte // offset 28:  checksum of header+entries (up to 512-bit)
	_            [4]byte  // offset 92:  alignment padding for Timestamp (_Pad1)
	Timestamp    uint64   // offset 96:  unix timestamp of last write (v3+, not covered by checksum)
}

// headerSizeForVersion returns the header size for a given index version.
func headerSizeForVersion(version uint32) int {
	if version <= 2 {
		return V2HeaderSize
	}
	return HeaderSize
}

// HeaderSizeForVersion returns the header size for a given index version.
func HeaderSizeForVersion(version uint32) int {
	return headerSizeForVersion(version)
}

// ValidateSignature checks if the signature matches expected value
func (ih *Header) ValidateSignature(expected [4]byte) error {
	if ih.Signature != expected {
		return fmt.Errorf("invalid signature: got %q, expected %q",
			string(ih.Signature[:]), string(expected[:]))
	}
	return nil
}

// ValidateVersion checks if the version is supported.
// Pass expected=0 to accept any version (used by read-only tools like dcfhfind).
// Otherwise accepts versions in range [MinIndexVersion, expected].
func (ih *Header) ValidateVersion(expected uint32) error {
	if expected == 0 {
		return nil
	}
	if ih.Version < MinIndexVersion || ih.Version > expected {
		return fmt.Errorf("unsupported version: got %d, expected %d-%d", ih.Version, MinIndexVersion, expected)
	}
	return nil
}

// ValidateByteOrder checks if the byte order matches the host machine
func (ih *Header) ValidateByteOrder() error {
	if ih.ByteOrder != ByteOrderMagic {
		return fmt.Errorf("byte order mismatch: index file byte order 0x%016x does not match host byte order 0x%016x",
			ih.ByteOrder, ByteOrderMagic)
	}
	return nil
}

// SetHeader initialises the header fields in mmap'd memory
func (ih *Header) SetHeader(signature [4]byte, version uint32, entryCount uint32, flags FlagBits, checksumType HashKind) {
	ih.Signature = signature
	ih.ByteOrder = ByteOrderMagic
	ih.Version = version
	ih.EntryCount = entryCount
	ih.Flags = flags
	ih.ChecksumType = checksumType
	ih.Timestamp = uint64(time.Now().Unix()) //nolint:gosec // G115: Unix seconds, non-negative
}

// SetHeaderForWritableIndex initialises the header for write operations (scan/temp indices).
// The write version is owned here (always CurrentIndexVersion) rather than passed by the
// caller, so no normal index write can stamp a divergent version by mistake. The lower-level
// SetHeader keeps its explicit version for the repair tool and tests that legitimately write a
// chosen version. Automatically clears the Clean flag since we're opening for write.
func (ih *Header) SetHeaderForWritableIndex(signature [4]byte, entryCount uint32, baseFlags FlagBits, checksumType HashKind) {
	// For writable indices, ensure Clean flag is cleared (not clean during write operations)
	flags := baseFlags &^ IndexFlagClean
	ih.SetHeader(signature, CurrentIndexVersion, entryCount, flags, checksumType)
}

// IsClean returns true if this index file is in a clean/complete state.
func (ih *Header) IsClean() bool {
	return ih.Flags&IndexFlagClean != 0
}

// SetClean marks this index file as clean/complete (final operation).
func (ih *Header) SetClean() {
	ih.Flags |= IndexFlagClean
}

// ClearClean marks this index file as unclean/incomplete.
func (ih *Header) ClearClean() {
	ih.Flags &^= IndexFlagClean
}
