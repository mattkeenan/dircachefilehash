package dircachefilehash

import (
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
		// Test that core API still works
		testDir := "test-api-validation"
		dc := NewDirectoryCache(testDir, testDir)
		defer dc.Close()
		
		// Basic API functions should work
		stats, size, err := dc.Stats()
		if err != nil {
			t.Errorf("Stats() failed: %v", err)
		}
		
		t.Logf("Stats: %d entries, %d bytes", stats, size)
		
		// Status should work
		result, err := dc.Status(nil, map[string]string{})
		if err != nil {
			t.Errorf("Status() failed: %v", err)
		}
		
		if result.HasChanges() {
			t.Logf("Found %d changes", result.TotalChanges())
		}
	})
}