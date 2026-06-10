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
- `dcfh diff <left-ref> <right-ref>` - Compare any two index references
- `dcfh subrepo <subcommand>` - Discover and manage nested repositories (find, add)
- `dcfh completion [bash|zsh]` - Generate a shell completion script
- `dcfh remote <root>` - Hidden SSH audit-mode endpoint (machine-invoked over ssh, not an end-user command)

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

## Security Review

Two distinct, complementary mechanisms guard this repo. They are not interchangeable.

### 1. Static analysis: `gosec` via golangci-lint

`gosec` runs as a linter inside `.golangci.yml` (not as a standalone binary), so it fires on every `golangci-lint` invocation — including the `.githooks/pre-commit` staged (`--new`) gate. It guards new code for all rules that are not explicitly excluded.

- **Rule excludes** (`linters.settings.gosec.excludes`) are architectural/intentional only: G103 (unsafe — the zero-copy/mmap design) and G401/G505 (SHA-1 — git-compatible content addressing). G115 (int-overflow conversion) and G304 (file-path-from-variable) are **no longer excluded**: G115 was re-enabled in task 3.3 once `Dev`/`Ino` widened to `uint64` (format v4), and G304's blanket exclude was converted to per-line `//nolint:gosec // G304` suppressions in task 6 (the triage audit), so the one untrusted-reachable open — `pkg/hash.go`, reached via the wire `RemoteHandler.hashOne` path — carries an explicit guard-citing rationale (`resolveRel`→`hasPathPrefix`) rather than being silently excluded. Note: gosec also emits **G703** ("path traversal via taint analysis") for `os.WriteFile` sites where the destination is taint-tracked (e.g. `pkg/recovery.go`, `pkg/snapshot.go`); these carry per-line G703 suppressions citing the base-name destination.
- **Test-only false positives** are scoped via an `exclusions.rules` entry `{linters: [gosec], path: _test\.go}`.
- **Production false positives** are suppressed per-line with `//nolint:gosec // Gxxx: <rationale>`, mirroring the existing `//nolint:govet` style. Every suppression carries a rationale; perms rules (G301/G302/G306) stay **active** so new over-permissive writes are still caught — the existing `.dcfh/` perm suppressions are non-secret metadata/hash files.
- Issue caps are lifted (`issues.max-same-issues: 0`, `max-issues-per-linter: 0`) so the security gate never silently hides a duplicate finding.

Setting `gosec.excludes` activates gosec's **full** ruleset — measure findings through `golangci-lint run ./...`, never standalone `gosec`.

### 2. Changeset review: CWF `cwf-security-reviewer-changeset` agent

For non-trivial changes, run the CWF security-review phase against the task's changeset. The CWF implementation-exec step (`/cwf-implementation-exec`) invokes the `cwf-security-reviewer-changeset` agent automatically and records the verdict in `f-implementation-exec.md`. This is a semantic review of the diff (FR4 threat categories: injection, secrets, auth, env-var handling, prompt-injection surface) — distinct from gosec's pattern matching, and distinct from the generic `/security-review` built-in.

**Apply both**: gosec is the always-on syntactic floor; the CWF changeset review is the per-task semantic check.

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
- `pkg/binary_entry.go` - `binaryEntry` struct + methods, `binaryEntryRef`, build-time layout assertions
- `pkg/time_encoding.go` - Wall-time encoding (`timeWall`, `timeFromWall`, `encodeWallTime`)
- `pkg/filenames.go` - Timestamped index filenames + `PathToSlug`
- `pkg/human_size.go` - `ParseHumanSize` / `FormatHumanSize` / `FormatHumanRate`
- `pkg/constants.go` - Constants used by external consumers (e.g., cmd/dcfh.go)
- `pkg/hash.go` - Hash algorithms, file hashing, symlink-target hashing
- `pkg/index.go` - Binary index file internals and binaryEntry management
- `pkg/index_loading.go` - Memoised index loading (main/cache/merged) into skiplists

**Layer 2: Data Structures & Algorithms**
- `pkg/skiplist.go` - Zero-copy skip list wrapper with context-aware operations and vectorio integration
- `pkg/ignore.go` - Ignore pattern matching (.dcfhignore support)
- `pkg/scan.go` - Directory walk (recursive DFS, sorted path queue, scan-time --ignore filter)
- `pkg/scan_types.go` - Scan-pipeline types (`scannedPath`, `mockFileInfo`)
- `pkg/scan_symlinks.go` - --symlinks policy engine (mode parsing, chain checks, follow decisions)
- `pkg/hwang_lin.go` - Hwang-Lin comparison driver

**Layer 3: Pipelines/Workflows**
- `pkg/pipeline.go` - Channel-based pipeline scaffolding (comparison → hash → reorder → write)
- `pkg/pipeline_status.go` - Status pipeline (cache refresh, dirty detection)
- `pkg/pipeline_update.go` - Update pipeline (atomic main-index replacement)
- `pkg/metastore.go` - Main MetaStore API and factory functions

**Layer 4: Core Operations** (one file per CLI command)
- `pkg/status.go` - Status reporting (`dcfh status` command)
- `pkg/update.go` - Update operations (`dcfh update` command)
- `pkg/dupes.go` - Duplicate file detection (`dcfh dupes` command)
- `pkg/snapshot.go` - Snapshot management (`dcfh snapshot` commands)
- `pkg/recovery.go` - Index recovery and validation (`dcfh index recover` commands)
- (Note: `init` functionality is in `pkg/metastore.go` as `NewMetaStore`)

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

**Binary entry** (`pkg/binary_entry.go`):
- `binaryEntry` struct (mmap-resident on-disk layout) + `binaryEntryRef` (offset-based handle)
- Flag methods: `IsDeleted` / `SetDeleted` / `ClearDeleted` / `IsHashed` / `SetHashed`
- Path access: `RelativePath` (zero-copy unsafe), `RelativePathModern` (Go 1.17+ `unsafe.Slice` variant)
- Validation: `validateLayout`, `ValidateEntry`, build-time layout assertions
- Sizing: `EntrySize`, `BESizeFromPathLen`
- Hash helpers: `HashString`, `IsHashEmpty`

**Time encoding** (`pkg/time_encoding.go`):
- `timeWall()` / `timeFromWall()` / `encodeWallTime()` — custom 34-bit-sec + 30-bit-nsec format with 1885 epoch (range 1885 → ~2429)

**File naming** (`pkg/filenames.go`):
- Methods on `MetaStore`: `GenerateTimestampedFileName`, `ScanForTimestampedCacheFiles`, `CleanupTimestampedCacheFiles`
- `PathToSlug` — kebab-case slug for external `.dcfh` directory naming

**Human-readable sizes** (`pkg/human_size.go`):
- `ParseHumanSize` (e.g. "2M", "512k"), `FormatHumanSize`, `FormatHumanRate`

**Constants** (`pkg/constants.go`):
- Index format constants: `HeaderSize`, `ChecksumSize`, hash type constants
- Index flags: `IndexFlagSparse`, `IndexFlagClean` (completion tracking)
- Context identifiers: `MainContext`, `CacheContext`, `ScanContext`, `TempContext`
- File naming: `MainIndex`, `CacheIndex`, `TempIndex` patterns

**File Hashing** (`pkg/hash.go`):
- `HashAlgorithm` registry covering SHA-1/SHA-256/SHA-512
- `HashFile()` / `HashFileInterruptible()` - hash file contents (the latter checks ctx for shutdown)
- `(*MetaStore).hashSymlinkTargetToBytes()` - hash a symlink's target path
- `(*MetaStore).GetCurrentHashType()` / `GetCurrentHashAlgorithm()` - resolve algorithm from config + flags

**Index Internals** (`pkg/index.go`):
- Binary format structs: `IndexHeader`, `MmapIndex`
- **Two I/O patterns** (v0.7):
  - **Main/Cache indices**: Read-only mmap via `loadIndexShared()`
  - **Temp indices**: Pure vectorio via `TempIndexWriter` / `WriteSkiplistWithVectorIO()`
- Memory mapping: loading, checksum verification, clean flag management
- Vectorio operations: `WriteMainIndexWithVectorIO()`, `WriteSkiplistWithVectorIO()`

### I/O Design and File Access Patterns

**CRITICAL ARCHITECTURAL PRINCIPLE - Index File Lifecycle**:

The pipeline atomically replaces main/cache indices on disk. As of v0.7
there are two on-disk index file types:

**1. Main & Cache Indices** (Stable Read-Only):
- **Access**: Memory-mapped with `PROT_READ` via the shared loader (`loadIndexShared`)
- **Lifecycle**: Persistent files representing the current stable state
- **Usage**: Read existing index data into skiplist structures for comparison
- **Files**: `main.idx`, `cache.idx`

**2. Temp Indices** (Transient Write-Only):
- **Access**: Pure vectorio via `TempIndexWriter.WriteSerialised()` /
  `WriteSkiplistWithVectorIO()` — never mmap'd for writing
- **Lifecycle**: Written once, becomes new main/cache via atomic rename
- **Purpose**: Single bulk write of the merged entry set
- **Atomicity**: Ensures replacement is a single rename
- **Selection**: Main indices exclude deleted entries, cache indices include them

**v0.7 in-memory scan entries**: New entries discovered during a scan
are heap-allocated `BEScanEntry` values flowing through pipeline
channels. There are no mmap-backed scan-*.idx files in v0.7 — that
machinery (mremap-grown writable mmaps) was removed because it was
no longer load-bearing once the channel pipeline shipped.

**Scan workflow** (channel pipeline):
1. **Walk**: Stream filesystem entries (sorted) via the Walker
2. **Compare**: Hwang-Lin against the existing main+cache merge
3. **Hash**: Pipeline workers fill in `BEScanEntry.Hash` lazily
4. **Serialise**: `EntrySerialiser.Serialise` produces wire bytes
5. **Write**: `TempIndexWriter.WriteSerialised` → atomic rename

### Binary Index Format Details

**Index Structure**:
- Header: "dcfh" signature, version, entry count, flags (including `IndexFlagClean`)
- Entries: 8-byte aligned binary entries with file metadata + SHA-1 hash
- Footer: SHA-1 checksum of entire file content

**Entry Types**:
- Regular entries: Active files with current metadata
- Deleted entries: Marked with deletion flag, retained for tracking
- Sparse entries: Used in cache indices for partial updates

### Key Design Patterns

- **Zero-copy operations**: Skip list reuses existing entries when unchanged
- **Atomic updates**: Temporary files with atomic rename for index writes
- **Context-aware merging**: Different merge strategies for main/cache/scan contexts
- **Hwang-Lin algorithm**: Efficient comparison of sorted file lists
- **Pure file I/O**: No dependencies on external libraries for core operations
- **Main Index Integrity**: Main index is ONLY updated on complete success - partial/interrupted operations accumulate in cache index to preserve work without compromising consistency

### Data Flow (v0.7 Channel Pipeline)

1. **Scan**: Walk directory tree, streaming files as found (sorted order)
2. **Compare**: Hwang-Lin against the existing main+cache merge
3. **Pipeline Entry**: Construct heap `BEScanEntry` for new/modified files
4. **Hash**: Workers fill `BEScanEntry.Hash` via `SetHash` (heap, no mmap)
5. **Serialise**: `EntrySerialiser.Serialise` produces wire-format bytes
6. **Write**: `TempIndexWriter.WriteSerialised` writes the temp index
7. **Rename**: Atomic rename promotes the temp file to main/cache

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

### Scan Workflow Integration (v0.7)

**Key Integration Points**:
- `pipeline_update.go` / `pipeline_status.go`: Pipeline scaffolding
- `hwang_lin.go`: Comparison driver that emits PipelineEntries
- `BEScanEntry`: Heap-allocated entry produced for new/modified files
- `EntrySerialiser.Serialise`: Wire-format bytes for the writer
- `TempIndexWriter.WriteSerialised`: Single bulk write of merged entries

### Memory Protection and Locking Mechanism

The `mmapIndexFile.mutex` RWMutex is still acquired on every entry
access (`GetBinaryEntry`, serialisation, etc.). In v0.7 it is
**defensive rather than load-bearing**: the dynamically-growing
mremap'd scan-index path that originally motivated the locking has
been removed, so the writer side of the lock has no producer in
production code.

The locks are kept for two reasons:
1. The `mmapIndexFile.Cleanup()` path takes the write lock to
   coordinate munmap with any in-flight readers (refcount-driven).
2. Removing every reader-side `mutex.RLock` would touch many files
   and isn't justified by current performance pressure.

Treat the locking as a no-op-in-practice guard. New code that mmaps
or unmaps an index file should still go through this path; new code
that simply reads through `binaryEntryRef.GetBinaryEntry()` does not
need to think about mremap.

The `--index-lock-timeout` flag and `[performance] index_lock_timeout`
config knob are kept for compatibility but rarely fire under normal
operation.

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
- BACKLOG.md is for general repository use but AI references should be cleaned when squashing to public branches

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
- **Go 1.25.0** with minimal external dependencies (floor raised from 1.24.3 by tcell, task 11)
- **github.com/mattkeenan/zerocopyskiplist v0.9.0** - Zero-copy skiplist with vectorio integration
- **github.com/google/vectorio** - Efficient bulk I/O operations via `writev()`
- **golang.org/x/sys/unix** - System calls for mmap and file operations
- **github.com/gdamore/tcell/v2** - Terminal UI for the `--interactive-tree` post-run viewer (`dcfh status`/`update`); the stack gdu uses
- **golang.org/x/term** - TTY detection for the interactive-tree guard

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
1. **Single Entry Writing Path**: `TempIndexWriter` is the only writer of
   binaryEntries to disk. Heap `BEScanEntry` values flow through the pipeline
   and are serialised via `EntrySerialiser.Serialise` before write.
2. **File Type Separation**:
   - Main/Cache: Read-only mmap
   - Temp: Pure vectorio (no mmap)
3. **Temp Index Flow**: Only vectorio → atomic rename for final index writing
4. **Filtering**: Main indices exclude deleted entries, cache indices include them

### System Requirements
- **Unix-like systems** (uses `syscall.Stat_t` and mmap)
- **64-bit architecture** (for safe pointer arithmetic)
- **File system** supporting atomic rename operations

### Performance Characteristics
- **Fast startup**: Binary format with mmap loading
- **Low memory usage**: Zero-copy operations with skiplist
- **Concurrent hashing**: Channel pipeline with bounded hash workers
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
<!-- CWF-PREAMBLE-START -->
> **CWF (Coding with Files) is installed in this project.**
> - Invoke CWF workflow steps using the `Skill` tool (e.g. `Skill("cwf-task-plan")`). Do not manually read or follow SKILL.md instructions directly.
> - All workflow steps are mandatory. If a step is genuinely inapplicable, mark it `Skipped` via the workflow process — do not silently omit it.
<!-- CWF-PREAMBLE-END -->
