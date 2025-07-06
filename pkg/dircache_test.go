package dircachefilehash

import (
	"testing"
)

func TestExtractPidFromIndexFileName(t *testing.T) {
	testCases := []struct {
		filename string
		expected int
	}{
		{"scan-1234-5678.idx", 1234},
		{"tmp-9999-1111.idx", 9999},
		{"scan-0-0.idx", 0},
		{"tmp-42-999.idx", 42},
		{"invalid.idx", 0},
		{"scan-abc-def.idx", 0},
		{"scan-1234.idx", 0}, // Not enough parts
		{"scan-1234-5678", 0}, // No .idx suffix
		{"", 0},
	}

	for _, tc := range testCases {
		result := extractPidFromIndexFileName(tc.filename)
		if result != tc.expected {
			t.Errorf("extractPidFromIndexFileName(%q) = %d, expected %d", tc.filename, result, tc.expected)
		}
	}
}

func TestIsProcessRunning(t *testing.T) {
	// Test with PID 1 (init process, should always exist on Unix systems)
	if !isProcessRunning(1) {
		t.Errorf("PID 1 should be running on Unix systems")
	}
	
	// Test with an obviously invalid PID (very high number unlikely to be used)
	if isProcessRunning(999999) {
		t.Errorf("PID 999999 should not be running")
	}
}