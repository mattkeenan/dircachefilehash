package format

import (
	"strings"
	"testing"
)

// TC-1: StrategyForVersion routes the current version to the zero-copy cast,
// legacy (v2/v3) versions to the heap transcode, and rejects everything else
// with a descriptive, range-naming error — never a panic, never an out-of-range
// index of the untrusted version byte.
func TestStrategyForVersion(t *testing.T) {
	tests := []struct {
		name    string
		version uint32
		want    DecodeStrategy
		wantErr bool
	}{
		{"min legacy v2", MinIndexVersion, DecodeHeap, false},
		{"legacy v3", 3, DecodeHeap, false},
		{"current v4", CurrentIndexVersion, DecodeZeroCopy, false},
		{"zero (dcfhfind no-op clamp)", 0, DecodeReject, true},
		{"below min", 1, DecodeReject, true},
		{"newer than current", CurrentIndexVersion + 1, DecodeReject, true},
		{"max uint32", 0xFFFFFFFF, DecodeReject, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := StrategyForVersion(tc.version)
			if got != tc.want {
				t.Errorf("StrategyForVersion(%d) strategy = %d, want %d", tc.version, got, tc.want)
			}
			if tc.wantErr {
				if err == nil {
					t.Fatalf("StrategyForVersion(%d): expected error, got nil", tc.version)
				}
				// Usability (NFR2): the error names the offending version and the
				// supported range, mirroring ValidateVersion.
				msg := err.Error()
				for _, want := range []string{
					itoa(tc.version), itoa(MinIndexVersion), itoa(CurrentIndexVersion),
				} {
					if !strings.Contains(msg, want) {
						t.Errorf("error %q missing %q (version/range must be named)", msg, want)
					}
				}
			} else if err != nil {
				t.Errorf("StrategyForVersion(%d): unexpected error: %v", tc.version, err)
			}
		})
	}
}

// itoa avoids importing strconv just for the message-content assertions.
func itoa(v uint32) string {
	if v == 0 {
		return "0"
	}
	var b [10]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}
