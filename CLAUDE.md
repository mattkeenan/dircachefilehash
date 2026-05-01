# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**dircachefilehash** is a Go CLI tool and library for directory scanning, file hashing, and duplicate detection. It maintains a git-compatible binary index format with SHA-1 hashes for efficient file integrity checking and change detection.

## ⚠️ CRITICAL REMINDER: STATUS COMMAND HASHES FILES ⚠️

**NEVER FORGET**: The `dcfh status` command DOES hash files and writes results to `cache.idx` for performance optimization. This is NOT optional - it's a core architectural requirement.

**WRONG**: "Status is metadata-only", "Status shouldn't hash", "Status is read-only"
**CORRECT**: Status hashes changed files and caches results for future operations

This has been forgotten 6+ times causing wasted development time. Always verify Status command includes hashing logic.

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

### Local Build Version Override
For local development builds, you can override the version string in two ways:

#### Method 1: Using .local-version file (Recommended)
```bash
# Create a .local-version file with your desired version
echo "v0.6.5" > .local-version

# Build normally - Makefile will automatically use the override
make build

# This produces version strings like: v0.6.5-LOCAL-44713286
# Format: {override}-LOCAL-{commit}
```

#### Method 2: Using environment variable
```bash
# Build with custom version (must match v[0-9]+.[0-9]+.[0-9]+ format)
DCFH_VERSION_OVERRIDE=v0.6.5 make build
```

This is useful for:
- Local development on branches that diverged from older tags
- Testing version-specific behavior
- Creating meaningful version strings for local builds

The override:
- Must match the format `v[0-9]+\.[0-9]+\.[0-9]+` (e.g., v0.6.5)
- Adds a "LOCAL" slug to distinguish from official builds
- Preserves the commit hash for traceability
- Applies to all three tools (dcfh, dcfhfind, dcfhfix)
- The `.local-version` file is ignored by git

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

**Symlink Modes** (`--symlinks`):
- `none` - Don't follow any directory symlinks (default)
- `all` - Follow all directory symlinks
- `internal` - Only follow symlinks pointing inside the repository root
- `external` - Only follow symlinks pointing outside the repository root  
- `internal,strict` - Only follow if ALL symlinks in chain are internal
- `external,strict` - Only follow if ALL symlinks in chain are external

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
- `pkg/hash.go` - Hash algorithms, file hashing, symlink-target hashing
- `pkg/index.go` - Binary index file internals and binaryEntry management
- `pkg/index_loading.go` - Memoised index loading (main/cache/merged) into skiplists

**Layer 2: Data Structures & Algorithms**
- `pkg/skiplist.go` - Zero-copy skip list wrapper with context-aware operations and vectorio integration
- `pkg/ignore.go` - Ignore pattern matching (.dcfhignore support)
- `pkg/scan.go` - Directory scanning, Hwang-Lin comparison, and scan index workflow

**Layer 3: Pipelines/Workflows**
- `pkg/pipeline.go` - Channel-based pipeline scaffolding (comparison → hash → reorder → write)
- `pkg/pipeline_status.go` - Status pipeline (cache refresh, dirty detection)
- `pkg/pipeline_update.go` - Update pipeline (atomic main-index replacement)
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

**File Hashing** (`pkg/hash.go`):
- `HashAlgorithm` registry covering SHA-1/SHA-256/SHA-512
- `HashFile()` / `HashFileInterruptible()` - hash file contents (the latter checks ctx for shutdown)
- `(*DirectoryCache).hashSymlinkTargetToBytes()` - hash a symlink's target path
- `(*DirectoryCache).GetCurrentHashType()` / `GetCurrentHashAlgorithm()` - resolve algorithm from config + flags

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
- **Main Index Integrity**: Main index is ONLY updated on complete success - partial/interrupted operations accumulate in cache index to preserve work without compromising consistency

### Data Flow (New Scan Index Workflow)

1. **Scan**: Walk directory tree, streaming files as found (sorted order)
2. **Compare**: Hwang-Lin algorithm comparison with concurrent processing
3. **Scan Index**: Create entries via `AppendEntryToScanIndex()` during comparison
4. **Hash**: Workers update entries directly in mmap'd scan index (zero-copy)
5. **Skiplist**: Build skiplist from scan index entries with proper context
6. **Merge**: Combine with existing indices using context-aware operations
7. **Write**: Atomic write via vectorio to temp file, then rename
8. **Cleanup**: Remove scan index files after successful completion

### Hash Worker Synchronization (v0.6.5+)

**Shutdown Coordination**:
- Hash workers track current job and send interruption signals on shutdown
- Monitor goroutine blocks on channels without busy-waiting (no default case)
- Shutdown sequence ensures all jobs are accounted for (completed or interrupted)
- 60-second timeout prevents indefinite blocking during worker shutdown

**Key Patterns**:
1. **Worker Pattern**: Track `currentJob` and send completion signal even on interruption
2. **Monitor Pattern**: No default case in select statement to avoid busy-waiting
3. **Shutdown Pattern**: Close hash job channel, wait with timeout, then close completion channel
4. **Timeout Pattern**: Use goroutine wrapper with `time.After()` for bounded waits

**Design Principles**:
- Workers must exit immediately on shutdown request
- All jobs must be signaled as complete (success or interruption)
- Workflows must continue even if workers don't exit cleanly
- Channel closing serves as broadcast mechanism to multiple goroutines

### Scan Index Workflow Integration

**Key Integration Points**:
- `PerformHwangLinScanToSkiplist()`: Replaces old result-based workflow
- `hwangLinCompareToSkiplist()`: Creates scan entries during comparison
- `AppendEntryToScanIndex()`: Only way to write binaryEntries to scan files
- `WriteSkiplistWithVectorIO()`: Final index writing with proper filtering
- Hash workers: Direct updates to mmap'd scan index memory

### Memory Protection and Locking Mechanism

**CRITICAL: Preventing SIGSEGV from Concurrent Memory Access**

The codebase uses RWMutex locks to protect mmap'd memory from concurrent access issues, particularly when `mremap()` might move memory during hash calculations. Here's how the multi-level locking works:

**1. The Problem**:
- Scan indices grow dynamically using `mremap()` which can relocate memory
- Hash calculations (SHA1/SHA256/SHA512) read from mmap'd memory via IoVec pointers
- If `mremap()` moves memory while a hash is being calculated → SIGSEGV crash
- Multiple indices (main, cache, scan) can be referenced by a single skiplist

**2. Two-Level Locking Design**:

**Low-Level Protection (per-entry access)**:
```go
// In GetBinaryEntry() - protects individual entry access
func (ref *binaryEntryRef) GetBinaryEntry() *binaryEntry {
    ref.IndexFile.mutex.RLock()
    defer ref.IndexFile.mutex.RUnlock()
    // Safe to calculate pointer from offset
    return (*binaryEntry)(unsafe.Pointer(entryPtr))
}
```
- Every access to a binaryEntry acquires a read lock
- Prevents memory from being moved during pointer calculation
- Works for all entry access, not just during writes
- Protects the offset-to-pointer conversion that would crash if memory moved

**High-Level Protection (bulk operations)**:
```go
// In writeSkiplistWithVectorIOFiltered() - protects entire operation
referencedIndices := dc.getAllReferencedIndices(skiplist)
// Acquire locks on ALL indices in consistent order
for _, idx := range sortedIndices {
    idx.mutex.RLock()
}
defer func() {
    for idx := range referencedIndices {
        idx.mutex.RUnlock()
    }
}()
```
- Identifies ALL mmap'd indices referenced by skiplist entries
- Acquires read locks in address order (prevents deadlock)
- Holds locks for entire IoVec generation and hash calculation
- Configurable timeout (default 5 seconds) to prevent hanging

**3. Why Double Locking is Safe**:
- Go's RWMutex allows multiple read locks from same goroutine (reentrant)
- Provides defense in depth - protection at both levels
- Low-level locks protect all code paths, not just writes
- High-level locks prevent any mremap during critical sections

**4. Index Tracking**:
```go
// DirectoryCache tracks all loaded indices
mainIndex    *mmapIndexFile  // Main index if loaded
cacheIndex   *mmapIndexFile  // Cache index if loaded  
scanIndices  []*mmapIndexFile // All scan indices
```
- Indices are registered when loaded (`registerIndex()`)
- Unregistered when cleaned up (`unregisterIndex()`)
- Allows identification of which index contains each entry

**5. Write Lock for mremap Operations**:
```go
// In appendEntryToNamedIndex() - write lock for memory expansion
if newSize > (*indexInfo).Size {
    (*indexInfo).mutex.Lock()  // Write lock
    // Safe to mremap now - no readers can access
    newMmap, err := unix.Mremap((*indexInfo).Data, newSize, unix.MREMAP_MAYMOVE)
    (*indexInfo).Data = newMmap
    (*indexInfo).mutex.Unlock()
}
```
- Write locks exclude all readers during memory remapping
- Ensures no hash calculations can be in progress
- Updates Data pointer atomically under lock protection

**6. Configuration**:
- `--index-lock-timeout N`: Command line flag (seconds)
- `.dcfh/config`: `[performance] index_lock_timeout = N`
- Default: 5 seconds
- Range: 1-300 seconds

**7. What Happens on Timeout**:
- Warning logged to stderr
- Operation continues WITHOUT lock protection
- Prevents deadlock but risks SIGSEGV if mremap occurs
- Timeout should be rare in practice

**8. Conceptual Summary**:
The locking works like a readers-writers lock on a shared document:
- Multiple readers (hash calculations) can access simultaneously
- Writers (mremap operations) get exclusive access
- Offset-based references remain valid across mremap operations
- Pointer conversions only happen under read lock protection

This design ensures safe concurrent access while maintaining performance through read-write separation and minimal lock holding times.

## Critical Reasoning Patterns for AI Development

### Short-Circuiting Anti-Pattern: "The Best Part is No Part"

**Problem Identified**: AI assistants have a tendency to short-circuit their reasoning and jump to complex solutions instead of finding simple, minimal approaches. This manifests as getting stuck in "local maxima" rather than seeking "global maxima" or easier "regional maxima".

**Real Example from BinaryEntryInterface Development**:

**The Short-Circuit**: When encountering a byte order mismatch in test index file creation, I immediately jumped to using `writeSkiplistWithVectorIO()` - a complex function requiring skiplist creation, vectorio operations, and multiple locks - just to create a test index file.

**The Simple Solution**: The existing `SetHeaderForWritableIndex()` method creates proper headers with correct byte order, version, and checksum fields in 2 lines of code:
```go
var header indexHeader
header.SetHeaderForWritableIndex(signature, version, entryCount, flags, checksumType)
```

**The Lesson**: "The best part is no part" applies to reasoning itself:
1. **Always look for existing functions first** before writing new code
2. **Start with the minimal solution** and only add complexity if needed
3. **Question every line of new code** - can existing infrastructure handle this?
4. **Avoid reimplementing functionality** that already exists in the codebase

**Recognition Patterns for Short-Circuiting**:
- Immediately jumping to complex solutions without exploring simpler options
- Writing multiple lines of setup code when a single function call exists
- Ignoring user requests (like documentation) in favor of "fixing the immediate issue"
- Not checking if the problem has already been solved elsewhere in the codebase

**The Meta-Problem**: Even when explicitly told to document this pattern, I initially ignored the request and went straight to fixing the version number - demonstrating the same short-circuiting behavior I was supposed to be documenting!

**Corrective Approach**:
1. **Pause and survey** - what existing functions solve similar problems?
2. **Ask "what's the minimal change?"** instead of "how do I implement this?"
3. **Follow user instructions completely** before diving into implementation
4. **Document patterns** when explicitly requested, don't defer for "more urgent" work

This reasoning pattern is fundamental to effective development and must be consciously applied to avoid falling into local optimization traps.

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
- **Shell command suffixes**: Do **NOT** use `; echo "exit: $?"` in Bash tool commands. It causes blocking and unnecessary permissions checks.
- **Repeated Similar Errors**: If you encounter the same class of errors multiple times (e.g., offset calculation bugs, checksum errors), this indicates a fundamental approach problem. Stop and redesign the approach rather than fixing individual instances.
- **Manual Offset Calculations**: Never manually calculate struct field offsets (e.g., `offset+4`, `offset+28`). Use `unsafe.Offsetof()` and centralized field accessors.
- **Unsafe Data Access in Repair Tools**: Repair tools must assume data is corrupted. Always validate bounds, alignment, and reasonableness before accessing any field.
- **Reimplementing Existing Functionality**: Before creating new functions (especially for core operations like index writing, checksum calculation, or file management), ALWAYS check if equivalent functionality already exists in the codebase. Use existing battle-tested functions instead of creating potentially buggy parallel implementations.
- **Conflicting Implementations**: When new code requirements seem to conflict with existing functionality patterns, ASK THE USER for clarification rather than assuming a different approach is needed. The existing patterns are usually correct and should be followed.
- **Goroutine Synchronization Errors**: 
  - Never use busy-wait loops (default case in select with channels)
  - Always signal job completion/interruption, even during shutdown
  - Use bounded waits (timeouts) instead of indefinite blocking
  - Track current work items in workers for proper cleanup signaling
  - Close channels in proper sequence to avoid deadlocks

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

### Hash Worker Live Lock Fix (v0.6.5+)
**Status**: Complete
**Description**: Fixed critical live lock issues during hash worker shutdown

**Problems Solved**:
- **Live Lock**: Monitor goroutines were spinning indefinitely waiting for hash jobs
- **Missing Signals**: Workers exited without signaling job completion/interruption
- **Indefinite Blocking**: `wg.Wait()` could block forever on stuck workers

**Implementation**:
- **Worker Tracking**: Hash workers track current job and send interruption signals
- **Channel Coordination**: Proper channel closing sequence (jobs → wait → completions)
- **60-Second Timeout**: Bounded wait prevents workflow from getting stuck
- **No Busy-Wait**: Removed default case from monitor select statement

**Impact**:
- Graceful shutdown without CPU spinning
- Workflows continue even with stuck workers
- Consistent behavior across scan and update paths
