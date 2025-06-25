# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**dircachefilehash** is a Go CLI tool and library for directory scanning, file hashing, and duplicate detection. It maintains a git-compatible binary index format with SHA-1 hashes for efficient file integrity checking and change detection.

## Commands

### Build and Run
```bash
go build -o dcfh cmd/dcfh.go
./dcfh --help
```

### Testing
```bash
go test ./pkg/...
go test -v ./pkg/...  # verbose output
```

### CLI Usage
The main CLI commands are:
- `dcfh init <dir>` - Initialize repository in directory
- `dcfh status` - Show file status (modified/added/deleted)
- `dcfh update [paths...]` - Update index with current state
- `dcfh dupes` - Find duplicate files

Global options: `--json`, `--verbose`, `--version`

## Architecture

### Layered Architecture Overview

The codebase is organized in distinct layers, from low-level utilities to high-level workflows:

**Layer 1: Foundation**
- `pkg/util.go` - Utility functions and structs that don't belong elsewhere
- `pkg/constants.go` - Constants used by external consumers (e.g., cmd/dcfh.go)
- `pkg/file.go` - File hashing operations for scanned files (consider renaming to `filehash.go`)
- `pkg/index.go` - Binary index file internals and binaryEntry management

**Layer 2: Data Structures & Algorithms**
- `pkg/skiplist.go` - Zero-copy skip list wrapper with context-aware operations
- `pkg/ignore.go` - Ignore pattern matching (.dcfhignore support)
- `pkg/scan.go` - Directory scanning and Hwang-Lin comparison algorithm

**Layer 3: Middleware/Workflows**
- `pkg/middleware.go` (rename from `highlevel.go`) - Complex multi-step workflows, cache updates, index merging
- `pkg/dircache.go` - Main DirectoryCache API and factory functions

**Layer 4: Core Operations** (one file per CLI command)
- `pkg/status.go` - Status reporting (`dcfh status` command)
- `pkg/update.go` - Update operations (`dcfh update` command)
- `pkg/dupes.go` - Duplicate file detection (`dcfh dupes` command)
- (Note: `init` functionality is in `pkg/dircache.go` as `NewDirectoryCache`)

**Layer 5: CLI Interface**
- `cmd/dcfh.go` - Command-line interface with commands: init, status, update, dupes

### Layer 1: Foundation Components

**Utilities** (`pkg/util.go`):
- Core structs: `DirectoryCache`, `binaryEntry`, `fileJob`
- Time encoding functions: `timeWall()`, `timeFromWall()`, `encodeWallTime()`
- File naming: `generateTempFileName()`, `generateScanFileName()`, `generateTmpIndexFileName()`
- Goroutine ID extraction: `getGoroutineID()`
- Binary entry utilities: `IsDeleted()`, `SetDeleted()`, `RelativePath()`, `HashString()`

**Constants** (`pkg/constants.go`):
- Index format constants: `HeaderSize`, `ChecksumSize`, hash type constants
- Index flags: `IndexFlagSparse`
- Context identifiers: `MainContext`, `CacheContext`, `ScanContext`

**File Hashing** (`pkg/file.go`):
- `processFileJob()` - Process individual file scan jobs
- `hashFile()` - SHA-1 hash computation for file contents
- File metadata extraction from `syscall.Stat_t`

**Index Internals** (`pkg/index.go`):
- Binary format structs: `IndexHeader`, `MmapIndex`
- Low-level entry writing: `writeBinaryEntryToMmap()`, `writeProcessedEntryToMmap()`
- Index file operations: `WriteIndex()`, `WriteEntries()`, `WriteProcessedEntries()`
- Memory mapping: creation, loading, checksum verification
- File I/O: `LoadIndexFromFile()`, `createEmptyIndex()`

### Binary Index Format Details

**Index Structure**:
- Header: "dcfh" signature, version, entry count, flags
- Entries: 8-byte aligned binary entries with file metadata + SHA-1 hash
- Footer: SHA-1 checksum of entire file content

**Entry Types**:
- Regular entries: Active files with current metadata
- Deleted entries: Marked with deletion flag, retained for tracking
- Sparse entries: Used in cache/scan indices for partial updates

### Key Design Patterns

- **Zero-copy operations**: Skip list reuses existing entries when unchanged
- **Atomic updates**: Temporary files with atomic rename for index writes
- **Context-aware merging**: Different merge strategies for main/cache/scan contexts
- **Hwang-Lin algorithm**: Efficient comparison of sorted file lists
- **Pure file I/O**: No dependencies on external libraries for core operations

### Data Flow

1. **Scan**: Walk directory tree, collect file jobs
2. **Compare**: Use Hwang-Lin algorithm to identify changed files
3. **Hash**: Compute SHA-1 only for new/modified files  
4. **Merge**: Combine results using skip list operations
5. **Write**: Atomic write of binary index with integrity checks

## Development Notes

- Uses Go 1.24.3 with minimal external dependencies
- Binary format ensures fast startup and low memory usage
- Designed for Unix-like systems (uses `syscall.Stat_t`)
- Cleanup functions handle temporary files automatically
- Error handling prioritizes data integrity over performance