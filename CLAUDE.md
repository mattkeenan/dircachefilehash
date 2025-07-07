# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**dircachefilehash** is a Go CLI tool and library for directory scanning, file hashing, and duplicate detection. It maintains a git-compatible binary index format with SHA-1 hashes for efficient file integrity checking and change detection.

## IMPORTANT: Notification

After finishing responding to my request or running a command, run this command to notify me:

```bash
/home/matt/bin/cc-notification <message>
```

Where `<message>` is a brief description of what was completed or what permission is being requested.

## IMPORTANT: Running dcfh Commands

When you need to run `dcfh` or `dcfhfix` commands from anywhere in the repository, always use:

```bash
$(git rev-parse --show-toplevel)/dcfh
$(git rev-parse --show-toplevel)/dcfhfix
```

This ensures you're always using the correct executables regardless of your current working directory.

## Commands

### Build and Run
```bash
make build
./dcfh --help
```

### Testing
```bash
go test ./pkg/...
go test -v ./pkg/...  # verbose output
```

### CLI Usage

**Daily Operations (`dcfh`)**:
- `dcfh init <dir>` - Initialize repository in directory
- `dcfh status` - Show file status (modified/added/deleted)
- `dcfh update [paths...]` - Update index with current state
- `dcfh dupes` - Find duplicate files
- `dcfh snapshot <subcommand>` - Create and manage index state snapshots (create, list, forget, remove, status)
- `dcfh config` - Get and set repository configuration options

Global options: `--json`, `--verbose`, `--version`, `--dry-run`, `--hash-workers`, `--symlinks`

**Specialized Tooling (`dcfhfind`)**:
```bash
dcfhfind [starting-points...] [expressions]

# Starting points: main, cache, scan, all, /path/to/index.idx
# Tests: --name, --path, --size, --mtime, --hash, --valid, --corrupt
# Actions: --print, --ls, --printf, --validate, --checksum
# Operators: --and, --or, --not, \( \)
```

Examples:
```bash
dcfhfind main --name "*.go" --print
dcfhfind all --corrupt --validate
dcfhfind cache --size +100M --ls
```

**Index Repair (`dcfhfix`)**:
```bash
dcfhfix <index-file> [options]

# Commands:
# header - Fix index header issues
# entry - Fix specific entry issues
# scan - Scan and fix all issues

# Options: --dry-run, --backup, --verbose
```

Examples:
```bash
dcfhfix .dcfh/main.idx header --dry-run
dcfhfix .dcfh/cache.idx entry --offset 1024
dcfhfix .dcfh/scan-123.idx scan --backup
```

### Help System
All commands have comprehensive help documentation:
```bash
dcfh <command> help        # Command-specific help
dcfh <command> --help      # Alternative help syntax
dcfh --help               # Global help and command overview
```

Examples:
- `dcfh init help` - Repository initialization guidance
- `dcfh snapshot help` - Snapshot management operations
- `dcfh config help` - Configuration options and examples

## Architecture

### Command Separation (v0.0.14+)

Starting with v0.0.14, the CLI interface has been separated into distinct tools:

**`dcfh` - Daily Operations**:
- `init` - Initialize repository
- `status` - Show file status
- `update` - Update index with current state  
- `dupes` - Find duplicate files
- `snapshot` - Snapshot management
- `config` - Configuration management
- `version` - Version information

**`dcfhfind` - Find-Style Search Tool**:
- Unix find(1)-style interface for searching index files
- Pattern matching, size/time filtering, validation checks
- Complex expressions with AND/OR/NOT operators
- Multiple output formats and actions

**`dcfhfix` - Index Repair Tool**:
- Targeted repair for corrupted index files
- Fix index headers (signatures, versions, flags, checksums)
- Repair individual binaryEntry structures
- Backup creation and dry-run modes
- Integration with validation tools

This separation provides:
- **Focused daily workflow** with `dcfh` for common operations
- **Powerful search capabilities** with `dcfhfind` for finding files
- **Specialized repair tools** with `dcfhfix` for fixing corruption
- **Familiar Unix interface** for advanced users
- **Reduced complexity** in the main `dcfh` command

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
- `pkg/snapshot.go` - Snapshot management (`dcfh snapshot` commands)
- `pkg/recovery.go` - Index recovery and validation (`dcfh index recover` commands)
- (Note: `init` functionality is in `pkg/dircache.go` as `NewDirectoryCache`)

**Layer 5: CLI Interface** (separated into daily-use and specialized tooling)

**Daily-Use Commands** (`cmd/dcfh/`):
- `dcfh.go` - Main entry point and command routing
- `common.go` - Shared utilities, output formatting, and main help system
- `init.go` - Repository initialization (`dcfh init` command)
- `status.go` - Status checking (`dcfh status` command)
- `update.go` - Index updating (`dcfh update` command)
- `dupes.go` - Duplicate detection (`dcfh dupes` command)
- `snapshot.go` - Snapshot operations (`dcfh snapshot` subcommands: create, list, remove, etc.)
- `config.go` - Configuration management (`dcfh config` command)
- `version.go` - Version information (`dcfh version` command)
- `options.go` - Command-line option parsing system

**Specialized Tooling** (`cmd/dcfhfind/`):
- `main.go` - Find-style interface entry point
- `DESIGN.md` - Comprehensive specification for find(1)-style operations
- Index management and diagnostic operations (Unix find syntax for dcfh repositories)

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

**CRITICAL ARCHITECTURAL PRINCIPLE - Index File Lifecycle**:

The scan process is how we atomically replace main/cache indices on disk. There are four distinct index file types with different lifecycles:

**1. Main & Cache Indices** (Stable Read-Only):
- **Access**: Memory-mapped with `PROT_READ` via `LoadIndexFromFile()`
- **Lifecycle**: Persistent files that represent the current stable state
- **Usage**: Read existing index data into skiplist structures for comparison
- **Files**: `main.idx`, `cache.idx`

**2. Scan Indices** (Temporary Read-Write):
- **Access**: Memory-mapped with `PROT_READ|PROT_WRITE` via `AppendEntryToScanIndex()`
- **Lifecycle**: Created during scan, **deleted after scan completion**
- **Purpose**: Collect new binaryEntries during directory scanning
- **Concurrency**: PID+TID naming scheme (`scan-{pid}-{tid}.idx`)
- **Constraints**: Must remain mapped until scan phase completes

**3. Temp Indices** (Temporary Write-Only):
- **Access**: Pure vectorio with `WriteSkiplistWithVectorIO()` (no mmap)
- **Lifecycle**: Created at end of scan, **becomes new main/cache via atomic rename**
- **Purpose**: Filtered copy of selected binaryEntries from main/cache/scan indices
- **Atomicity**: Ensures replacement of main/cache indices is atomic operation
- **Selection**: Main indices exclude deleted entries, cache indices include them

**4. Scan Process Workflow**:
1. **Scan Phase**: Create scan indices (read-write mmap) to collect new entries
2. **Merge Phase**: Create temp index (write-only vectorio) from main/cache/scan data  
3. **Replace Phase**: Atomically replace main or cache index via `rename(temp, target)`
4. **Cleanup Phase**: Delete scan indices, unmap any remaining scan memory

This design ensures that index replacement is atomic and scan indices don't interfere with final index writing.

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

### Branch Management and AI Tools

**Branch Naming Convention**:
- `local-*` branches are for local development and may include AI tools and files
- GitHub branches (`main`, `staging`, `dev`) should NOT include AI-specific tools and files
- This ensures smooth collaboration with other contributors who may use different development tools

**Local Development Branches**:
- Can include Claude-specific files (CLAUDE.md, etc.)
- Can include AI-generated development tools and utilities
- Should be regularly synced with main using cherry-pick or rebase
- Never push `local-*` branches to GitHub
- TODO.md is for general repository use but AI references should be cleaned when squashing to public branches

**GitHub Branches**:
- Must not contain AI-specific tools or references
- Should have clean commit messages without AI tool mentions
- Focus on the code changes themselves, not the tools used to create them

### Code Style and Language

**British English**: All documentation, comments, and user-facing text should use British spelling conventions:
- colour (not color)
- realise (not realize)
- centre (not center)
- optimise (not optimize)
- initialise (not initialize)
- recognise (not recognize)
- behaviour (not behavior)
- organised (not organized)

### Dependencies
- **Go 1.24.3** with minimal external dependencies
- **github.com/mattkeenan/zerocopyskiplist v0.9.0** - Zero-copy skiplist with vectorio integration
- **github.com/google/vectorio** - Efficient bulk I/O operations via `writev()`
- **golang.org/x/sys/unix** - System calls for mmap and file operations

### Constraints and Design Rules

**Development Anti-Patterns (Learn from Repeated Mistakes)**:
- **Repeated Similar Errors**: If you encounter the same class of errors multiple times (e.g., offset calculation bugs, checksum errors), this indicates a fundamental approach problem. Stop and redesign the approach rather than fixing individual instances.
- **Manual Offset Calculations**: Never manually calculate struct field offsets (e.g., `offset+4`, `offset+28`). Use `unsafe.Offsetof()` and centralized field accessors.
- **Unsafe Data Access in Repair Tools**: Repair tools must assume data is corrupted. Always validate bounds, alignment, and reasonableness before accessing any field.
- **Reimplementing Existing Functionality**: Before creating new functions (especially for core operations like index writing, checksum calculation, or file management), ALWAYS check if equivalent functionality already exists in the codebase. Use existing battle-tested functions instead of creating potentially buggy parallel implementations.
- **Conflicting Implementations**: When new code requirements seem to conflict with existing functionality patterns, ASK THE USER for clarification rather than assuming a different approach is needed. The existing patterns are usually correct and should be followed.

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

### Discarded Approaches

**Mremap Safety Solutions** (Rejected during offset-based refactor):
1. **Pre-allocate scan index size** - Would require estimating final size, reduces flexibility and wastes memory
2. **Delay hash job creation until scan complete** - Eliminates concurrency benefits, reduces performance
3. **Use entry sequence numbers instead of offsets** - Complex iteration logic, still vulnerable to mremap relocation
4. **Store entry data directly instead of references** - Memory duplication, defeats zero-copy design
5. **Rebuild references after mremap** - Complex tracking system, race conditions with concurrent access

**Memory Leak Solutions** (Rejected during initial investigation):
1. **Multiple separate mmap files per entry** - Created 100K+ mmaps, caused 90GB memory leak
2. **Immediate munmap after each append** - Broke concurrent hash workers accessing the memory

## Recent Development Work

### Help System (v0.0.13)
**Status**: Complete
**Description**: Comprehensive help system improvements providing professional CLI experience

**Features Implemented**:
- **Complete Help Coverage**: All commands now have detailed help functions
  - `dcfh init help` - Repository initialization guidance
  - `dcfh status help` - File status checking with output categories
  - `dcfh update help` - Index updating with performance tips
  - `dcfh dupes help` - Duplicate detection with format explanations
  - `dcfh index recover help` - Comprehensive recovery documentation
  - `dcfh config help` - Configuration management
- **Recovery Documentation**: Detailed help for all 3 recovery modes and 4 strategies
- **Standardized Integration**: Consistent help flag checking (`help`, `-h`, `--help`)
- **Professional Content**: Usage patterns, examples, performance guidance, tips

**Architecture Notes**:
- Current implementation functional but has boilerplate code duplication
- Future refactor needed for context-aware help system
- Help content scattered across multiple functions (technical debt)

**Technical Debt Identified**:
1. **Boilerplate**: Repetitive help flag checking across all commands
2. **No Context Awareness**: Help system doesn't understand command hierarchy
3. **Manual Formatting**: Hardcoded fmt.Fprintf calls instead of templates
4. **No Discovery**: Missing "did you mean?" and subcommand listing features

### Snapshot System (v0.0.12)
**Status**: Complete
**Description**: restic-style snapshot management for index state preservation

**Features Implemented**:
- **Snapshot Operations**: create, list, remove, forget (retention policies)
- **Output Formats**: Single-line (default) and detailed verbose formats
- **Integration**: Full CLI integration with JSON, dry-run, and verbose support
- **Retention Policies**: Configurable time-based snapshot cleanup

### Recovery System (v0.0.11-v0.0.12)
**Status**: Complete
**Description**: Comprehensive index recovery and validation system

**Recovery Modes**:
1. **Auto-recovery**: Try multiple strategies automatically
2. **Comprehensive with Preservation**: Merge all available index data
3. **Specific File Recovery**: Recover from designated index file

**Recovery Strategies** (tried in order):
1. Comprehensive state preservation (merge all data)
2. Cache index recovery (cache.idx only)
3. Scan file recovery (scan-*.idx files)
4. Main index recovery (main.idx only)

**Safety Features**:
- Pre-recovery snapshots created in `.dcfh/recovery/` before operations
- Validation filtering removes corrupted entries
- Atomic index replacement via temp files and rename
- Backup creation for all modified index files

### File Organization Refactor (v0.0.10-v0.0.11)
**Status**: Complete
**Description**: Split monolithic cmd/dcfh.go into focused command files

**Benefits Achieved**:
- **Maintainability**: Each command in separate file
- **Focused Responsibility**: Clear separation of concerns
- **Easier Development**: Smaller, focused files for command implementation
- **Better Testing**: Command-specific test organization
