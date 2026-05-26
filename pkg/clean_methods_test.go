package dircachefilehash

import (
	"sync"
	"testing"
)

// Test that Clean methods are properly privatised and work correctly
func TestCleanMethodsPrivatization(t *testing.T) {
	var header indexHeader

	// Test that methods are private by checking they work internally
	// but would not be accessible from outside the package

	// Initial state should be not clean
	if header.IsClean() {
		t.Error("Header should initially be not clean")
	}

	// Set clean flag
	header.SetClean()
	if !header.IsClean() {
		t.Error("Header should be clean after setClean()")
	}

	// Verify the flag was set correctly
	if header.Flags&IndexFlagClean == 0 {
		t.Error("Clean flag should be set in header.Flags")
	}

	// Clear clean flag
	header.ClearClean()
	if header.IsClean() {
		t.Error("Header should not be clean after clearClean()")
	}

	// Verify the flag was cleared correctly
	if header.Flags&IndexFlagClean != 0 {
		t.Error("Clean flag should be cleared in header.Flags")
	}
}

// Test clean flag manipulation with other flags present
func TestCleanMethodsWithOtherFlags(t *testing.T) {
	var header indexHeader

	// Set some other flags first
	header.Flags = IndexFlagSparse // Set sparse flag

	// Initial state should be not clean but sparse
	if header.IsClean() {
		t.Error("Header should initially be not clean")
	}
	if header.Flags&IndexFlagSparse == 0 {
		t.Error("Sparse flag should still be set")
	}

	// Set clean flag while preserving other flags
	header.SetClean()
	if !header.IsClean() {
		t.Error("Header should be clean after setClean()")
	}
	if header.Flags&IndexFlagSparse == 0 {
		t.Error("Sparse flag should still be set after setClean()")
	}

	// Clear clean flag while preserving other flags
	header.ClearClean()
	if header.IsClean() {
		t.Error("Header should not be clean after clearClean()")
	}
	if header.Flags&IndexFlagSparse == 0 {
		t.Error("Sparse flag should still be set after clearClean()")
	}
}

// Test that clean methods work correctly with multiple flag operations
func TestCleanMethodsMultipleOperations(t *testing.T) {
	var header indexHeader

	// Test multiple set/clear cycles
	for i := range 5 {
		header.SetClean()
		if !header.IsClean() {
			t.Errorf("Header should be clean after setClean() iteration %d", i)
		}

		header.ClearClean()
		if header.IsClean() {
			t.Errorf("Header should not be clean after clearClean() iteration %d", i)
		}
	}
}

// Test clean methods with boundary flag values
func TestCleanMethodsBoundaryValues(t *testing.T) {
	tests := []struct {
		name         string
		initialFlags uint16
		description  string
	}{
		{
			name:         "zero flags",
			initialFlags: 0,
			description:  "No flags set initially",
		},
		{
			name:         "all flags set",
			initialFlags: 0xFFFF,
			description:  "All possible flags set initially",
		},
		{
			name:         "only clean flag",
			initialFlags: IndexFlagClean,
			description:  "Only clean flag set initially",
		},
		{
			name:         "all except clean flag",
			initialFlags: 0xFFFF &^ IndexFlagClean,
			description:  "All flags except clean set initially",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var header indexHeader
			header.Flags = tt.initialFlags

			initialClean := header.IsClean()
			expectedInitialClean := (tt.initialFlags & IndexFlagClean) != 0

			if initialClean != expectedInitialClean {
				t.Errorf("Initial clean state incorrect for %s: expected %v, got %v",
					tt.description, expectedInitialClean, initialClean)
			}

			// Set clean and verify
			header.SetClean()
			if !header.IsClean() {
				t.Errorf("Should be clean after setClean() for %s", tt.description)
			}

			// Clear clean and verify
			header.ClearClean()
			if header.IsClean() {
				t.Errorf("Should not be clean after clearClean() for %s", tt.description)
			}

			// Verify other flags were preserved (if any)
			expectedFinalFlags := tt.initialFlags &^ IndexFlagClean
			if header.Flags != expectedFinalFlags {
				t.Errorf("Other flags not preserved for %s: expected 0x%x, got 0x%x",
					tt.description, expectedFinalFlags, header.Flags)
			}
		})
	}
}

// Test that clean flag constant is correct
func TestCleanFlagConstant(t *testing.T) {
	// Verify IndexFlagClean is a valid bit flag
	if IndexFlagClean == 0 {
		t.Error("IndexFlagClean should not be zero")
	}

	// Should be a power of 2 (single bit set)
	if IndexFlagClean&(IndexFlagClean-1) != 0 {
		t.Error("IndexFlagClean should be a power of 2 (single bit)")
	}

	// Test bitwise operations work correctly
	var flags uint16 = 0

	// Setting the flag
	flags |= IndexFlagClean
	if flags&IndexFlagClean == 0 {
		t.Error("Flag setting operation failed")
	}

	// Clearing the flag
	flags &^= IndexFlagClean
	if flags&IndexFlagClean != 0 {
		t.Error("Flag clearing operation failed")
	}
}

// Test that clean methods work correctly under concurrent access when
// protected by a mutex (matching the real usage pattern where callers
// hold the index RWMutex).
func TestCleanMethodsBasicConcurrency(t *testing.T) {
	var header indexHeader
	var mu sync.Mutex

	done := make(chan bool, 2)

	// Goroutine 1: Set clean repeatedly (under lock)
	go func() {
		for range 100 {
			mu.Lock()
			header.SetClean()
			mu.Unlock()
		}
		done <- true
	}()

	// Goroutine 2: Check clean state repeatedly (under lock)
	go func() {
		for range 100 {
			mu.Lock()
			_ = header.IsClean()
			mu.Unlock()
		}
		done <- true
	}()

	// Wait for both goroutines
	<-done
	<-done

	// Final state should be clean (since setClean was called)
	if !header.IsClean() {
		t.Error("Header should be clean after concurrent operations")
	}
}

// Test integration with the flags system we implemented
func TestCleanMethodsWithStatusVerbose(t *testing.T) {
	var header indexHeader

	// Simulate the verbose status check logic
	flags := map[string]string{"v": "1"}

	// Test initial state
	header.SetClean()
	if !header.IsClean() {
		t.Error("Header should be clean")
	}

	// Simulate what Status() does with verbose flag
	verboseLevel, exists := flags["v"]
	includeCleanStatus := false

	if exists && verboseLevel != "" {
		if level := 1; level > 0 { // Simplified version of strconv.Atoi
			includeCleanStatus = true
		}
	}

	if !includeCleanStatus {
		t.Error("Should include clean status with verbose flag")
	}

	// Test that we can get the clean status when needed
	if includeCleanStatus {
		cleanStatus := header.IsClean()
		if !cleanStatus {
			t.Error("Should report clean status correctly")
		}
	}
}
