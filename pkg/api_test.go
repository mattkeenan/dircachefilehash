package dircachefilehash

import (
	"context"
	"os"
	"testing"
)

// TestPublicAPI tests the public API functions work correctly
func TestPublicAPI(t *testing.T) {
	t.Run("DebugFunctions", func(t *testing.T) {
		// Test InitDebugFlags
		InitDebugFlags("scan,extravalidation")

		// Test GetDebugEnabled
		if !GetDebugEnabled("scan") {
			t.Errorf("Expected scan debug to be enabled")
		}

		// Test LogDebugFlags (should not crash)
		LogDebugFlags()
	})

	t.Run("VerboseFunctions", func(t *testing.T) {
		// Test SetVerboseLevel and GetVerbose
		SetVerboseLevel(2)

		if GetVerbose() != 2 {
			t.Errorf("Expected verbose level 2, got %d", GetVerbose())
		}

		// Reset to 0
		SetVerboseLevel(0)
	})

	t.Run("CoreAPI", func(t *testing.T) {
		// Test that NewDirectoryCache and API methods handle a non-existent
		// repo without panicking. Operations should return errors, not crash.
		testDir, err := os.MkdirTemp("", "api-test-*")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer func() { _ = os.RemoveAll(testDir) }()

		dc := NewDirectoryCache(testDir, testDir)
		defer func() { _ = dc.Close() }()

		// Stats and Status on uninitialised repo should not panic.
		// They may return errors or zero results — both are acceptable.
		stats, size, err := dc.Stats()
		if err != nil {
			t.Logf("Stats() returned error (acceptable for uninitialised repo): %v", err)
		} else {
			t.Logf("Stats: %d entries, %d bytes", stats, size)
		}

		result, err := dc.Status(context.Background(), dc.scanRun(), map[string]string{}, nil)
		if err != nil {
			t.Logf("Status() returned error (acceptable for uninitialised repo): %v", err)
		}
		if result != nil && result.HasChanges() {
			t.Logf("Found %d changes", result.TotalChanges())
		}
	})
}
