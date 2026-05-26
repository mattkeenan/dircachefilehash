package dircachefilehash

import "time"

// timeWall converts a time.Time to a uint64 wall time format for storage
// Uses custom format: 34 bits seconds since Jan 1, 1885 + 30 bits nanoseconds (no monotonic bit)
// NOTE: Does not handle files with dates before 1885 (will underflow)
// Range: Jan 1, 1885 to approximately year 2429
func timeWall(t time.Time) uint64 {
	// Use Jan 1, 1885 as epoch (like Go's monotonic case but without monotonic bit)
	// Unix epoch (1970-01-01) is 2682374400 seconds after Jan 1, 1885
	const unixTo1885 = 2682374400

	sec := t.Unix() + unixTo1885
	nsec := int64(t.Nanosecond())

	// Custom format: sec(34) + nsec(30) - gives us range until year ~2429
	wall := (uint64(sec) << 30) | uint64(nsec) //nolint:gosec // G115: 1885-epoch-offset seconds, non-negative for supported range
	return wall
}

// timeFromWall reconstructs a time.Time from wall time format
func timeFromWall(wall uint64) time.Time {
	// Extract components from our custom format
	const unixTo1885 = 2682374400

	// Extract nanoseconds (low 30 bits) and seconds (next 34 bits)
	nsec := int64(wall & 0x3FFFFFFF) // 30 bits for nanoseconds
	sec := int64(wall>>30) - unixTo1885

	return time.Unix(sec, nsec)
}

// encodeWallTime directly encodes seconds and nanoseconds into Go's wall time format
func encodeWallTime(sec int64, nsec int64) uint64 {
	// Convert Unix timestamp to 1885-based time
	const unixTo1885 = 2682374400
	offsetSec := sec + unixTo1885

	// Custom format: sec(34) + nsec(30)
	wall := (uint64(offsetSec) << 30) | uint64(nsec) //nolint:gosec // G115: 1885-epoch-offset seconds, non-negative for supported range
	return wall
}
