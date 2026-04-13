package dircachefilehash

import (
	"testing"
)

// TestFrameworkCompilation verifies that the test framework compiles correctly
func TestFrameworkCompilation(t *testing.T) {
	// Create test data
	testData := CreateTestData()
	if testData == nil {
		t.Fatal("CreateTestData() returned nil")
	}

	// Verify test data has expected values
	if testData.RelativePath != "test/file.txt" {
		t.Errorf("Expected RelativePath 'test/file.txt', got %q", testData.RelativePath)
	}

	if testData.FileSize != 1024 {
		t.Errorf("Expected FileSize 1024, got %d", testData.FileSize)
	}

	// Create deleted test data
	deletedData := CreateDeletedTestData()
	if deletedData == nil {
		t.Fatal("CreateDeletedTestData() returned nil")
	}

	if !deletedData.IsDeleted {
		t.Error("Expected deleted test data to have IsDeleted = true")
	}

	// Test implementation type enum
	if BESkiplist.String() != "BESkiplist" {
		t.Errorf("Expected BESkiplist.String() = 'BESkiplist', got %q", BESkiplist.String())
	}

	if BEScan.String() != "BEScan" {
		t.Errorf("Expected BEScan.String() = 'BEScan', got %q", BEScan.String())
	}

	// Test BinaryEntryBase
	base := NewBinaryEntryBase(BEScan)
	if base.ImplementationType() != BEScan {
		t.Errorf("Expected implementation type %v, got %v", BEScan, base.ImplementationType())
	}

	if !base.IsEphemeral() {
		t.Error("Expected BEScan to be ephemeral")
	}

	// Test locking doesn't panic
	base.RLock()
	base.RUnlock() //nolint:staticcheck // SA2001: intentional empty critical section - testing lock/unlock doesn't panic

	base.Lock()
	base.Unlock() //nolint:staticcheck // SA2001: intentional empty critical section - testing lock/unlock doesn't panic

	t.Log("Test framework compilation and basic functionality verified")
}

// TestBinaryEntryTestSuite tests the test suite structure
func TestBinaryEntryTestSuite(t *testing.T) {
	suite := &BinaryEntryTestSuite{
		Name:               "TestSuite",
		CreateEntry:        nil, // Would be implemented by concrete test
		CleanupEntry:       nil, // Would be implemented by concrete test
		SupportsSetHash:    true,
		SupportsSetDeleted: true,
		IsEphemeral:        false,
	}

	if suite.Name != "TestSuite" {
		t.Errorf("Expected suite name 'TestSuite', got %q", suite.Name)
	}

	if !suite.SupportsSetHash {
		t.Error("Expected SupportsSetHash to be true")
	}

	if !suite.SupportsSetDeleted {
		t.Error("Expected SupportsSetDeleted to be true")
	}

	if suite.IsEphemeral {
		t.Error("Expected IsEphemeral to be false")
	}

	t.Log("BinaryEntryTestSuite structure verified")
}
