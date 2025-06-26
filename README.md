```go
// Default: .dcfh directory in same location as indexed directory
cache := dcfh.NewDirectoryCache("/home/user/documents", "")

// Custom: .dcfh directory in different location  
cache := dcfh.NewDirectoryCache("/home/user/# dircachefilehash

A Go package for scanning directories, hashing file contents, and maintaining a sorted index file compatible with git's dircache format. Useful for file integrity checking, change detection, and duplicate file identification.

## Features

- **Directory Scanning**: Recursively walks through directories to catalog all files
- **SHA-1 Hashing**: Computes SHA-1 hashes of file contents (git-compatible)
- **Git-Compatible Format**: Uses the same field structure as git's dircache index
- **Sorted Index**: Maintains entries sorted by hash for efficient lookups
- **Duplicate Detection**: Identifies files with identical content
- **Metadata Tracking**: Stores complete Unix file metadata (timestamps, permissions, ownership)
- **Binary Index Format**: Uses compact binary format with "dcfh" signature and checksums for integrity
- **Persistent Storage**: Compact binary index file with built-in corruption detection

## Installation

```bash
go get github.com/mattkeenan/dircachefilehash
```

## Quick Start

```go
package main

import (
    "fmt"
    "log"
    "os"
    
    "github.com/mattkeenan/dircachefilehash"
)

func main() {
    // Get user's home directory
    homeDir, err := os.UserHomeDir()
    if err != nil {
        log.Fatal(err)
    }
    
    // Create a new directory cache
    cache := dircachefilehash.NewDirectoryCache(homeDir, homeDir)
    
    // Scan directory and create index
    if err := cache.Update(); err != nil {
        log.Fatal(err)
    }
    
    // Get statistics
    fileCount, totalSize, _ := cache.Stats()
    fmt.Printf("Indexed %d files, total size: %d bytes\n", fileCount, totalSize)
}
```

## API Reference

### DirectoryCache

The main type for managing file caches.

#### Methods

- `NewDirectoryCache(rootDir, dcfhDir string) *DirectoryCache` - Creates a new cache instance
- `ScanDirectory() error` - Scans the directory and generates file entries with hashes
- `WriteIndex() error` - Writes the sorted index to file
- `LoadIndex() error` - Loads an existing index file
- `Update() error` - Convenience method that scans and writes in one call
- `GetEntries() []FileEntry` - Returns a copy of current entries
- `FindByHash(hash string) []FileEntry` - Finds entries with specified hash (binary search)
- `FindDuplicates() map[string][]FileEntry` - Returns groups of files with identical hashes
- `Stats() (int, int64, error)` - Returns file count and total size

### FileEntry

Represents a file with its hash and metadata, matching git's dircache format.

```go
type FileEntry struct {
    CTime        time.Time // Change time (metadata last changed)
    CTimeNano    int32     // Change time nanoseconds
    MTime        time.Time // Modification time (content last modified) 
    MTimeNano    int32     // Modification time nanoseconds
    Dev          uint32    // Device ID
    Ino          uint32    // Inode number
    Mode         uint32    // File mode (permissions and type)
    UID          uint32    // User ID (owner)
    GID          uint32    // Group ID
    Size         uint32    // File size in bytes
    Hash         string    // SHA-1 hash (40 hex chars)
    Flags        uint16    // Index flags
    PathLen      uint16    // Length of relative path (big-endian)
    RelativePath string    // Relative path from root directory
}
```

## Index File Format

The index file uses a binary format similar to git's index file:

```
Header (12 bytes):
  - Signature: "dcfh" (4 bytes)
  - Version: 1 (4 bytes, big-endian)
  - Entry Count: number of entries (4 bytes, big-endian)

For each entry (variable length, padded to 8-byte boundary):
  - CTime: change time seconds (4 bytes, big-endian)
  - CTime Nano: change time nanoseconds (4 bytes, big-endian)  
  - MTime: modification time seconds (4 bytes, big-endian)
  - MTime Nano: modification time nanoseconds (4 bytes, big-endian)
  - Device: device ID (4 bytes, big-endian)
  - Inode: inode number (4 bytes, big-endian)
  - Mode: file mode (4 bytes, big-endian)
  - UID: user ID (4 bytes, big-endian)
  - GID: group ID (4 bytes, big-endian)
  - Size: file size (4 bytes, big-endian)
  - Hash: SHA-1 hash (20 bytes)
  - Flags: index flags (2 bytes, big-endian)
  - Path Length: length of path (2 bytes, big-endian)
  - Path: relative file path (variable length)
  - Null terminator: 0x00 (1 byte)
  - Padding: zero bytes to align to 8-byte boundary

Footer:
  - Checksum: SHA-1 of entire file content (20 bytes)
```

This binary format provides:
- **Compact storage**: Much smaller than text format
- **Fast parsing**: No string parsing overhead  
- **Integrity checking**: Built-in SHA-1 checksum
- **Custom format**: "dcfh" signature distinguishes from git index files

## Examples

### Finding Duplicate Files

```go
cache := dircachefilehash.NewDirectoryCache("/home/user/documents", "docs_index.txt")
cache.Update()

duplicates := cache.FindDuplicates()
for hash, files := range duplicates {
    fmt.Printf("Hash %s has %d duplicates:\n", hash[:8], len(files))
    for _, file := range files {
        fmt.Printf("  %s\n", file.RelativePath)
    }
}
```

### Loading and Querying Existing Index

```go
cache := dircachefilehash.NewDirectoryCache("/data", "data_index.txt")

// Load existing index
if err := cache.LoadIndex(); err != nil {
    log.Fatal(err)
}

// Find files by hash
hash := "da39a3ee5e6b4b0d3255bfef95601890afd80709"
matches := cache.FindByHash(hash)
fmt.Printf("Found %d files with hash %s\n", len(matches), hash[:8])
```

### Monitoring Directory Changes

```go
cache := dircachefilehash.NewDirectoryCache("/var/log", "log_index.txt")

// Create initial index
cache.Update()
oldEntries := cache.GetEntries()

// Later, rescan and compare
cache.ScanDirectory()
newEntries := cache.GetEntries()

// Compare old vs new entries to detect changes
// (Implementation depends on your specific needs)
```

## Use Cases

- **File Integrity Monitoring**: Detect when files have been modified
- **Backup Verification**: Ensure backup copies match originals
- **Deduplication**: Find and remove duplicate files
- **Change Detection**: Monitor directories for file system changes
- **Content Indexing**: Build searchable indexes of file content hashes
- **Git Integration**: Work with git-compatible file metadata

## Platform Compatibility

This package uses Unix system calls (`syscall.Stat_t`) to extract detailed file metadata. It's designed for Unix-like systems (Linux, macOS, BSD) and may require modifications for Windows compatibility.

## Development

### Prerequisites

- Go 1.21 or later
- gotags for code navigation: `go install github.com/jstemmer/gotags@latest`

### Setup

The repository includes automated tooling to maintain code quality:

**Pre-commit Hook**: Automatically generates/updates the `tags` file before each commit:
```bash
# The pre-commit hook is automatically installed in .git/hooks/
# It runs `gotags -R -f tags .` before each commit
```

**GitHub Actions**: 
- `ci.yml` - Runs tests, builds, and verifies code quality
- `tags-check.yml` - Ensures the tags file stays up to date

### Testing

```bash
# Run all tests
go test ./...

# Run tests with verbose output  
go test -v ./...

# Build and test CLI
go build -o dcfh cmd/dcfh.go
./dcfh --help
```

### Tags File

The `tags` file is automatically generated by `gotags` and provides code navigation for editors. It's maintained automatically by:

1. **Pre-commit hook** - Updates tags before each commit
2. **GitHub Actions** - Validates tags file is current on PRs/pushes

If you need to manually update tags:
```bash
gotags -R -f tags .
```

## Contributing

Contributions are welcome! Please feel free to submit pull requests, report bugs, or suggest features.

When contributing:
1. The pre-commit hook will automatically update the tags file
2. GitHub Actions will validate your changes
3. Ensure all tests pass with `go test ./...`

## License

MIT License - see LICENSE file for details.
# Test comment
