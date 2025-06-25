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
- `pkg/skiplist.go` - Zero-copy skip list wrapper with context-aware operations and vectorio integration
- `pkg/ignore.go` - Ignore pattern matching (.dcfhignore support)
- `pkg/scan.go` - Directory scanning, Hwang-Lin comparison, and scan index workflow

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
- Index flags: `IndexFlagSparse`, `IndexFlagClean` (completion tracking)
- Context identifiers: `MainContext`, `CacheContext`, `ScanContext`, `TempContext`
- File naming: `MainIndex`, `CacheIndex`, `TempIndex` patterns

**File Hashing** (`pkg/file.go`):
- `processFileJob()` - Process individual file scan jobs
- `hashFile()` - SHA-1 hash computation for file contents
- File metadata extraction from `syscall.Stat_t`

**Index Internals** (`pkg/index.go`):
- Binary format structs: `IndexHeader`, `MmapIndex`
- **Three distinct I/O patterns**:
  - **Main/Cache indices**: Read-only mmap via `LoadIndexFromFile()`
  - **Scan indices**: Read-write mmap with `AppendEntryToScanIndex()` (grows with ftruncate/mremap)  
  - **Temp indices**: Pure vectorio with `WriteSkiplistWithVectorIO()` for atomic writes
- Memory mapping: loading, checksum verification, clean flag management
- Vectorio operations: `WriteMainIndexWithVectorIO()`, `WriteSkiplistWithVectorIO()`

### I/O Design and File Access Patterns

**Three Distinct Index File Types with Optimized Access Patterns**:

**1. Main & Cache Indices** (Read-Only):
- **Access**: Memory-mapped with `PROT_READ` via `LoadIndexFromFile()`
- **Usage**: Read existing index data into skiplist structures
- **Constraints**: No direct writing - only loaded for merging operations
- **Files**: `main.idx`, `cache.idx`

**2. Scan Indices** (Append-Only Growth):
- **Access**: Memory-mapped with `PROT_READ|PROT_WRITE` 
- **Growth**: Dynamic expansion with `ftruncate()` and `mremap()` as needed
- **Writing**: **ONLY** via `AppendEntryToScanIndex()` function
- **Concurrency**: PID+TID naming scheme (`scan-{pid}-{tid}.idx`)
- **Updates**: Hash workers write directly to mmap'd memory (zero-copy)
- **Constraints**: `writeBinaryEntryToMmap()` is private and only called by `AppendEntryToScanIndex()`

**3. Temp Indices** (Pure Vectorio):
- **Access**: Standard file I/O with `O_CREAT|O_WRONLY` (no mmap)
- **Writing**: Pure vectorio with `WritevRaw()` for efficient bulk operations
- **Process**: Header → Entries → Checksum → Clean flag (all via vectorio)
- **Atomicity**: Temporary file with atomic rename over main/cache indices
- **Filtering**: Main indices exclude deleted entries, cache indices include them

### Binary Index Format Details

**Index Structure**:
- Header: "dcfh" signature, version, entry count, flags (including `IndexFlagClean`)
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

### Data Flow (New Scan Index Workflow)

1. **Scan**: Walk directory tree, streaming files as found (sorted order)
2. **Compare**: Hwang-Lin algorithm comparison with concurrent processing
3. **Scan Index**: Create entries via `AppendEntryToScanIndex()` during comparison
4. **Hash**: Workers update entries directly in mmap'd scan index (zero-copy)
5. **Skiplist**: Build skiplist from scan index entries with proper context
6. **Merge**: Combine with existing indices using context-aware operations
7. **Write**: Atomic write via vectorio to temp file, then rename
8. **Cleanup**: Remove scan index files after successful completion

### Scan Index Workflow Integration

**Key Integration Points**:
- `PerformHwangLinScanToSkiplist()`: Replaces old result-based workflow
- `hwangLinCompareToSkiplist()`: Creates scan entries during comparison
- `AppendEntryToScanIndex()`: Only way to write binaryEntries to scan files
- `WriteSkiplistWithVectorIO()`: Final index writing with proper filtering
- Hash workers: Direct updates to mmap'd scan index memory

## Development Notes

### Dependencies
- **Go 1.24.3** with minimal external dependencies
- **github.com/mattkeenan/zerocopyskiplist v0.9.0** - Zero-copy skiplist with vectorio integration
- **github.com/google/vectorio** - Efficient bulk I/O operations via `writev()`
- **golang.org/x/sys/unix** - System calls for mmap and file operations

### Constraints and Design Rules

**Critical Constraints (Must Be Enforced)**:
1. **Single Entry Writing Path**: `AppendEntryToScanIndex()` is the ONLY function that writes binaryEntries to index files
2. **Private Low-Level Function**: `writeBinaryEntryToMmap()` is private and only called by `AppendEntryToScanIndex()`
3. **File Type Separation**: 
   - Main/Cache: Read-only mmap
   - Scan: Read-write mmap with controlled growth
   - Temp: Pure vectorio (no mmap)
4. **Temp Index Flow**: Only vectorio → atomic rename for final index writing
5. **Filtering**: Main indices exclude deleted entries, cache indices include them

### System Requirements
- **Unix-like systems** (uses `syscall.Stat_t` and mmap)
- **64-bit architecture** (for safe pointer arithmetic)
- **File system** supporting atomic rename operations

### Performance Characteristics
- **Fast startup**: Binary format with mmap loading
- **Low memory usage**: Zero-copy operations with skiplist
- **Concurrent hashing**: PID+TID scan files for parallel processing
- **Atomic updates**: Temp files ensure data integrity
- **Efficient I/O**: vectorio for bulk writes, mmap for reads