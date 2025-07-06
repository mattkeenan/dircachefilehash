# dircachefilehash

[![Go Reference](https://pkg.go.dev/badge/github.com/mattkeenan/dircachefilehash.svg)](https://pkg.go.dev/github.com/mattkeenan/dircachefilehash)
[![Go Report Card](https://goreportcard.com/badge/github.com/mattkeenan/dircachefilehash)](https://goreportcard.com/report/github.com/mattkeenan/dircachefilehash)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/badge/Go-1.24.3%2B-blue)](https://go.dev/)

A high-performance Go package and CLI toolset for directory scanning, file hashing, and index management. Designed for massive scale (10s of millions of files) with git-compatible binary index format, atomic operations, and comprehensive integrity checking.

## Features

- **Three Specialized CLI Tools**:
  - `dcfh` - Daily operations (init, status, update, dupes, snapshots)
  - `dcfhfind` - Unix find(1)-style search interface for index files
  - `dcfhfix` - Index repair and recovery tool
- **Multiple Hash Algorithms**: SHA-1, SHA-256, SHA-512 (configurable)
- **Binary Index Format**: Compact storage with "dcfh" signature and SHA-1 checksums
- **Zero-Copy Operations**: Memory-mapped files with skiplist for efficiency
- **Concurrent Processing**: Configurable worker pools for parallel hashing
- **Signal Handling**: Graceful shutdown with SIGINT/SIGTERM support
- **Atomic Updates**: Temporary files with rename for data integrity
- **Index Versioning**: Format version 1 with upgrade path support
- **Snapshot System**: Create/restore index states for backup/recovery
- **Duplicate Detection**: fdupes-compatible output formats
- **JSON Output**: Machine-parseable output for automation

## Installation

### As a Go Package

```bash
go get github.com/mattkeenan/dircachefilehash
```

### Building CLI Tools

```bash
# Clone the repository
git clone https://github.com/mattkeenan/dircachefilehash.git
cd dircachefilehash

# Build all tools
make build

# Or build individually
go build -o dcfh ./cmd/dcfh
go build -o dcfhfind ./cmd/dcfhfind
go build -o dcfhfix ./cmd/dcfhfix
```

## Quick Start

### CLI Usage

```bash
# Initialize a repository
dcfh init /path/to/directory

# Check status (modified/added/deleted files)
dcfh status

# Update the index
dcfh update

# Find duplicates
dcfh dupes

# Create a snapshot
dcfh snapshot create

# Search for files (Unix find-style)
dcfhfind main --name "*.go" --size +1M --print

# Repair corrupted index
dcfhfix main.idx scan --backup
```

### Go Package API

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
    
    // Update the index (scan + hash + write)
    if err := cache.Update(nil, nil); err != nil {
        log.Fatal(err)
    }
    
    // Get statistics
    fileCount, totalSize, _ := cache.Stats()
    fmt.Printf("Indexed %d files, total size: %d bytes\n", fileCount, totalSize)
    
    // Check status
    status, _ := cache.Status(nil, nil)
    fmt.Printf("Modified: %d, Added: %d, Deleted: %d\n", 
        len(status.Modified), len(status.Added), len(status.Deleted))
}
```

## API Reference

### DirectoryCache

The main type for managing file caches.

#### Core Methods

- `NewDirectoryCache(rootDir, dcfhDir string) *DirectoryCache` - Creates a new cache instance
- `Update(shutdownChan <-chan struct{}, flags map[string]string) error` - Scan, hash, and update index
- `Status(shutdownChan <-chan struct{}, flags map[string]string) (*StatusResult, error)` - Check file status
- `Stats() (int, int64, error)` - Returns file count and total size in bytes
- `FindDuplicates(flags map[string]string) ([]*DuplicateGroup, error)` - Find duplicate files
- `Close() error` - Clean up resources (unmap files, close handles)

### StatusResult

Result of a status check operation.

```go
type StatusResult struct {
    Modified []string // Files that have been modified
    Added    []string // Files that have been added
    Deleted  []string // Files that have been deleted
}
```

### DuplicateGroup

Groups of files with identical content.

```go
type DuplicateGroup struct {
    Hash      []byte   // The hash value (SHA-1/256/512)
    FileCount int      // Number of duplicate files
    FileSize  int64    // Size of each file
    Files     []string // List of file paths
}
```

## Index File Format

The index file uses a binary format with host byte order for performance:

```
Header (88 bytes):
  - Signature: "dcfh" (4 bytes)
  - ByteOrder: 0x0102030405060708 (8 bytes, validates host byte order)
  - Version: 1 (4 bytes, host order)
  - EntryCount: number of entries (4 bytes, host order)
  - Flags: index flags (2 bytes, host order)
  - ChecksumType: checksum algorithm (2 bytes, host order)
  - Checksum: SHA-1 of header+entries (64 bytes, supports up to SHA-512)

For each entry (variable length, 8-byte aligned):
  - Size: total entry size including padding (4 bytes, host order)
  - CTimeWall: change time in wall format (8 bytes, custom format)*
  - MTimeWall: modification time in wall format (8 bytes, custom format)*
  - Dev: device ID (4 bytes, host order)
  - Ino: inode number (4 bytes, host order)
  - Mode: file mode (4 bytes, host order)
  - UID: user ID (4 bytes, host order)
  - GID: group ID (4 bytes, host order)
  - FileSize: file size (8 bytes, host order, supports >4GB files)
  - EntryFlags: entry flags (2 bytes, host order)
  - HashType: hash algorithm (2 bytes, host order)
  - Hash: file hash (64 bytes, zero-padded for SHA-1/SHA-256)
  - Path: relative path (minimum 8 bytes, variable length)
  - Padding: zero bytes to align to 8-byte boundary

*Time Format: 34 bits seconds since 1885 + 30 bits nanoseconds
 (supports dates from 1885 to ~2429, avoiding the 2038 problem)
```

This binary format provides:
- **Compact storage**: Much smaller than text format
- **Fast parsing**: No string parsing overhead  
- **Integrity checking**: Built-in SHA-1 checksum
- **Custom format**: "dcfh" signature distinguishes from git index files

## Examples

### Finding Duplicate Files

```go
cache := dircachefilehash.NewDirectoryCache("/home/user/documents", "/home/user/documents")
defer cache.Close()

// Update index first
if err := cache.Update(nil, nil); err != nil {
    log.Fatal(err)
}

// Find duplicates
duplicates, err := cache.FindDuplicates(nil)
if err != nil {
    log.Fatal(err)
}

for _, group := range duplicates {
    fmt.Printf("Found %d files with size %d bytes:\n", group.FileCount, group.FileSize)
    for _, file := range group.Files {
        fmt.Printf("  %s\n", file)
    }
}
```

### Monitoring Directory Changes

```go
cache := dircachefilehash.NewDirectoryCache("/var/log", "/var/log")
defer cache.Close()

// Create initial index
if err := cache.Update(nil, nil); err != nil {
    log.Fatal(err)
}

// Later, check for changes
status, err := cache.Status(nil, nil)
if err != nil {
    log.Fatal(err)
}

if status.HasChanges() {
    fmt.Printf("Found changes:\n")
    fmt.Printf("  Modified: %v\n", status.Modified)
    fmt.Printf("  Added: %v\n", status.Added)
    fmt.Printf("  Deleted: %v\n", status.Deleted)
}
```

### Graceful Shutdown

```go
// Set up signal handling
sigChan := make(chan os.Signal, 1)
signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
shutdownChan := make(chan struct{})

go func() {
    <-sigChan
    close(shutdownChan)
}()

// Pass shutdown channel to long-running operations
cache := dircachefilehash.NewDirectoryCache("/large/directory", "/large/directory")
err := cache.Update(shutdownChan, nil)
if err != nil {
    if err.Error() == "operation interrupted by shutdown" {
        fmt.Println("Gracefully stopped")
    }
}
```

## Use Cases

- **File Integrity Monitoring**: Detect when files have been modified
- **Backup Verification**: Ensure backup copies match originals
- **Deduplication**: Find and remove duplicate files
- **Change Detection**: Monitor directories for file system changes
- **Content Indexing**: Build searchable indexes of file content hashes
- **Git Integration**: Work with git-compatible file metadata

## Architecture & Design

### Key Design Principles

- **Zero-Copy Operations**: Uses memory-mapped files and skiplist pointers to avoid data duplication
- **Atomic Updates**: All index updates use temp file + rename for crash safety
- **Host Byte Order**: Uses native byte order for performance (validated on load)
- **Concurrent Hashing**: Worker pool architecture with configurable parallelism
- **Memory Efficiency**: Streaming operations for large directories, bounded memory usage

### Index File Types

1. **main.idx**: Primary index containing all tracked files
2. **cache.idx**: Sparse index with changes since last update
3. **scan-*.idx**: Temporary files during scanning (PID/TID isolation)
4. **tmp-*.idx**: Temporary files for atomic updates

### Performance Optimizations

- **Hwang-Lin Algorithm**: Efficient sorted list comparison during updates
- **Skip List**: O(log n) lookups with zero-copy entry references
- **Memory Mapping**: Direct file access without read/copy overhead
- **Vectored I/O**: Bulk write operations using writev() system call

## Platform Compatibility

This package uses Unix system calls (`syscall.Stat_t`) to extract detailed file metadata. It's designed for Unix-like systems (Linux, macOS, BSD) and may require modifications for Windows compatibility.

### Time Limitations

The custom time format supports dates from 1885 to approximately 2429. Files with modification times before 1885 will cause underflow errors.

## Development

### Prerequisites

- Go 1.24 or later
- gotags for code navigation: `go install github.com/jstemmer/gotags@latest`

### Building

```bash
# Build all tools
make build

# Run tests
make test

# Clean build artifacts
make clean

# Build with verbose output
make build VERBOSE=1
```

### Testing

```bash
# Run all tests
go test ./pkg/...

# Run tests with verbose output  
go test -v ./pkg/...

# Run specific test
go test -run TestTimeConversion ./pkg/

# Run with race detector
go test -race ./pkg/...
```

### Performance Benchmarks

The repository includes comprehensive performance benchmarks for testing scalability with large datasets:

```bash
# Run small benchmark (1K files, ~5s)
./run_benchmarks.sh -t small

# Run medium benchmark (100K files, ~10s) 
./run_benchmarks.sh -t medium

# Run large benchmark (1M+ files, ~30s)
./run_benchmarks.sh -t large

# Run all benchmarks
./run_benchmarks.sh -t all

# Generate memory and CPU profiles
./run_benchmarks.sh -t medium --memprofile --cpuprofile
```

**Benchmark Configurations:**
- **Small**: 1,000 files (50 large >1MB, 950 small), 3 directory levels
- **Medium**: 100,000 files (1,000 large >5MB, 99,000 small), 5 directory levels  
- **Large**: 1,000,000 files (10,000 large >10MB, 990,000 small), 8 directory levels

**What's Measured:**
- Directory scanning and file hashing performance
- Index file read/write operations
- Memory usage and efficiency
- Status checking and duplicate detection

The benchmarks generate deterministic test datasets with varied file sizes and directory structures, providing reproducible performance measurements for optimization work.

### Code Navigation

The repository uses `gotags` for code navigation support. The `tags` file is automatically generated by the pre-commit hook.

To manually update tags:
```bash
gotags -R -f tags .

# Or use make
make tags
```

## Contributing

Contributions are welcome! Please feel free to submit pull requests, report bugs, or suggest features.

When contributing:
1. Fork the repository and create a feature branch
2. Ensure all tests pass with `go test ./pkg/...`
3. Update documentation as needed
4. Submit a pull request with a clear description of changes

### Reporting Issues

When reporting issues, please include:
- Version information (`dcfh version`)
- Operating system and architecture
- Steps to reproduce the issue
- Any relevant error messages or logs

## License

MIT License - see LICENSE file for details.
