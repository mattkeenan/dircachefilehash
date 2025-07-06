// Package dircachefilehash provides directory scanning, file hashing, and duplicate detection
// with a git-compatible binary index format for efficient file integrity checking.
//
// # Core API
//
// The main entry point is DirectoryCache, which manages file indexing for a directory:
//
//	dc := dircachefilehash.NewDirectoryCache("/path/to/dir", "/path/to/dir")
//	defer dc.Close()
//
// # Basic Operations
//
// Update the index with current directory state:
//
//	err := dc.Update(map[string]string{})
//
// Check for changes since last update:
//
//	result, err := dc.Status(map[string]string{})
//	if result.HasChanges() {
//		fmt.Printf("Found %d changes\n", result.TotalChanges())
//	}
//
// Find duplicate files:
//
//	groups, err := dc.FindDuplicates(map[string]string{})
//	for _, group := range groups {
//		fmt.Printf("Hash %s: %v\n", group.Hash, group.Files)
//	}
//
// # Configuration
//
// Enable debug output:
//
//	dircachefilehash.SetDebugFlags("scan,extravalidation")
//	dircachefilehash.SetVerboseLevel(2)
//
// # Note on Internal API
//
// Many types and functions in this package are internal implementation details
// and may change in future versions. External consumers should primarily use:
//   - DirectoryCache and its methods
//   - Result types: StatusResult, DuplicateGroup
//   - Configuration functions: SetDebugFlags, SetVerboseLevel
//
// Types like binaryEntryRef, skiplistWrapper, indexHeader, etc. are internal
// and should not be used directly by external consumers.
package dircachefilehash
