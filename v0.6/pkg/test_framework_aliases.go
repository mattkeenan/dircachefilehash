//go:build exclude

package dircachefilehash

// Import test framework types from v0.6 package for backward compatibility
import (
	v06 "github.com/mattkeenan/dircachefilehash/v0.6/pkg"
)

// Alias types from v0.6 package for test compatibility
type (
	BinaryEntryTestSuite = v06.BinaryEntryTestSuite
	TestEntryData        = v06.TestEntryData
)

// Alias functions from v0.6 package for test compatibility
var (
	CreateTestData = v06.CreateTestData
)