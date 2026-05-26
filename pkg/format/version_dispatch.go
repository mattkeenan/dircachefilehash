package format

import "fmt"

// DecodeStrategy says how an index of a (validated) version is materialised.
type DecodeStrategy int

const (
	DecodeReject   DecodeStrategy = iota // unknown / out-of-range — fail closed
	DecodeZeroCopy                       // mmap cast (current layout only)
	DecodeHeap                           // transcode legacy (v2/v3) bytes into a v4 heap image
)

// StrategyForVersion maps an on-disk header version to its materialisation
// strategy. It is default-bearing: an unrecognised version is rejected with an
// error, never used to index a table or slice — so an attacker-controlled
// version byte cannot drive an out-of-range access.
//
// Callers must pass header.Version (the value read from the file), never
// ms.version. The dcfhfind validation path builds MetaStore{version: 0}
// (dcfhfind_support.go), for which ValidateVersion(0) is a no-op for ANY
// on-disk version; this resolver's default→reject arm is therefore the only
// real version gate on that path.
//
// The supported-range arm shares the MinIndexVersion/CurrentIndexVersion
// constants with ValidateVersion — the two are not redundant (ValidateVersion
// is the early signature/byte-order/version triple; this is the per-load
// materialisation authority), but Task 3.3 must bump both in lockstep when it
// raises CurrentIndexVersion to 4.
func StrategyForVersion(version uint32) (DecodeStrategy, error) {
	switch {
	case version == CurrentIndexVersion:
		return DecodeZeroCopy, nil
	case version >= MinIndexVersion && version < CurrentIndexVersion:
		// v4 widened Dev/Ino, so the legacy (v2/v3) entry layout now diverges
		// from the current one — legacy bytes must be transcoded into a v4 image
		// rather than cast in place.
		return DecodeHeap, nil
	default:
		return DecodeReject, fmt.Errorf("unsupported index version %d (supported %d-%d)",
			version, MinIndexVersion, CurrentIndexVersion)
	}
}
