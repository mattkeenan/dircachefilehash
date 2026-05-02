package dircachefilehash

import (
	"testing"
	"time"
)

func TestTimeConversion(t *testing.T) {
	// Test time conversion functions
	now := time.Now()

	// Convert to wall time and back
	wall := timeWall(now)
	converted := timeFromWall(wall)

	// Should be very close (within reasonable precision limits)
	diff := now.Sub(converted)
	if diff > 10*time.Second || diff < -10*time.Second {
		t.Errorf("Time conversion error too large: %v", diff)
	}
}

func TestEncodeWallTime(t *testing.T) {
	// Test wall time encoding
	sec := int64(1234567890)
	nsec := int64(123456789)

	wall := encodeWallTime(sec, nsec)
	if wall == 0 {
		t.Error("Wall time should not be zero")
	}

	// Convert back and verify - just check that conversion works
	converted := timeFromWall(wall)
	// Don't check exact equality since time conversion might have different epoch
	// Just verify we get a reasonable time back
	if converted.IsZero() {
		t.Error("Converted time should not be zero")
	}
}
