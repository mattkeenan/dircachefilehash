# dcfhfind Design Specification

## Overview

`dcfhfind` is a Unix `find(1)`-style command-line tool for searching and manipulating dcfh repository index files. It provides a powerful, composable interface for repository diagnostics, recovery operations, and data exploration.

## Design Philosophy

### Core Principles

1. **Unix find(1) Compatibility**: Familiar syntax and behavior patterns
2. **Orthogonal Operations**: Each expression does one specific thing
3. **Composable**: Complex queries through boolean logic and piping
4. **Low-level Access**: Direct manipulation of index structures
5. **Repository-aware**: Native understanding of dcfh index types and formats

### Command Structure

```bash
dcfhfind [starting-points...] [expressions]
```

**Key Differences from find(1)**:
- Uses `--flag` syntax instead of single `-` flags
- Starting points are dcfh-specific (main, cache, scan, all)
- Tests and actions are tailored for dcfh binary index structures
- Built-in understanding of binaryEntry fields and validation

## Starting Points Specification

### Index Type Resolution

| Starting Point | Description | Files Searched |
|---------------|-------------|----------------|
| `main` | Main index | `.dcfh/main.idx` |
| `cache` | Cache index | `.dcfh/cache.idx` |
| `scan` | All scan indices | `.dcfh/scan-*.idx` |
| `scan-PID-TID` | Specific scan index | `.dcfh/scan-PID-TID.idx` |
| `all` | All index files | main + cache + scan |
| `/path/to/file.idx` | Direct file path | Specified file |
| `.dcfh/*.idx` | Shell patterns | Pattern matches |

### Repository Discovery

- If no absolute path provided, search for `.dcfh` directory
- Support `--repo DIR` to specify repository root
- Auto-detect repository from current working directory (like git)

## Expression Types

### 1. Tests (Boolean Predicates)

#### Path Tests
```bash
--name PATTERN          # Filename glob match
--path PATTERN          # Full path glob match  
--regex PATTERN         # Path regex match
--iname PATTERN         # Case-insensitive name
--ipath PATTERN         # Case-insensitive path
```

#### Hash Tests
```bash
--hash HASH             # Exact hash match
--hash-prefix PREFIX    # Hash starts with prefix
--hash-type TYPE        # Hash algorithm (SHA1, SHA256, etc)
```

#### Time Tests
```bash
--mtime [-+]N           # Modified N*24 hours ago
--ctime [-+]N           # Changed N*24 hours ago
--mmin [-+]N            # Modified N minutes ago
--cmin [-+]N            # Changed N minutes ago
```

#### Size Tests
```bash
--size [-+]N[cwbkMG]    # Size comparison
--empty                 # Size is 0
```

#### Entry State Tests
```bash
--deleted               # Entry marked as deleted
--valid                 # Entry passes validation
--corrupt               # Entry fails validation
--missing               # File doesn't exist on disk
```

#### Permission Tests
```bash
--perm MODE             # Exact permissions
--perm -MODE            # All bits set
--perm /MODE            # Any bits set
```

#### Type Tests
```bash
--type TYPE             # File type (f,d,l,p,s,c,b)
--xtype TYPE            # Type after following links
```

#### Device Tests
```bash
--xdev                  # Don't cross devices
--dev DEVICE            # On specific device
```

#### Index Tests
```bash
--in-index TYPE         # Entry in specific index
--index-clean           # Index has clean flag set
```

### 2. Actions (Operations)

#### Output Actions
```bash
--print                 # Print path (default)
--print0                # Null-terminated paths
--printf FORMAT         # Custom format
--ls                    # Detailed listing
--fls FILE              # ls output to file
```

#### Validation Actions
```bash
--validate              # Validate entry
--checksum              # Verify hash
```

#### Modification Actions
```bash
--delete                # Mark as deleted
--update-hash           # Recompute hash
--fix {auto|manual|none} # Apply fixes (mandatory argument)
```

#### Export Actions
```bash
--export FILE           # Export to index
--extract DIR           # Extract to directory
```

### 3. Operators (Boolean Logic)

```bash
--and                   # Logical AND (implicit)
--or                    # Logical OR
--not, !                # Logical NOT
\( ... \)               # Grouping (shell escaping)
--prune                 # Skip directory
--quit                  # Exit immediately
```

### 4. Global Options

```bash
--maxdepth N            # Maximum depth
--mindepth N            # Minimum depth
--follow                # Follow symlinks
--xdev                  # Don't cross devices
--warn                  # Enable warnings
--nowarn                # Suppress warnings
```

## Printf Format Specification

Based on binaryEntry struct fields:

### Path and Name
- `%p` - Full path
- `%f` - Filename only (basename)
- `%h` - Directory name (dirname)
- `%P` - Path relative to starting point

### Size and Type
- `%s` - Size in bytes
- `%b` - Size in 512-byte blocks
- `%k` - Size in 1K blocks

### Permissions and Ownership
- `%m` - Permissions in octal
- `%M` - Permissions in symbolic
- `%u` - Numeric UID
- `%g` - Numeric GID
- `%U` - Username (resolved)
- `%G` - Group name (resolved)

### Times
- `%t` - Modification time (default format)
- `%T@` - Modification time as Unix timestamp
- `%Tk` - Modification time with strftime format k
- `%c` - Change time (default format)
- `%C@` - Change time as Unix timestamp
- `%Ck` - Change time with strftime format k

### Hash and Index Info
- `%H` - Hash value (hex)
- `%Y` - Hash type (SHA1, SHA256, etc)
- `%i` - Index source (main, cache, scan-12345-1)
- `%I` - Full index path
- `%F` - Entry flags

### Device and Special
- `%d` - Device number
- `%D` - Device number in hex
- `%n` - Number of hard links
- `%%` - Literal %
- `%Z` - Entry validation status

## Enhanced --ls Format

Traditional ls format with index source:

```
-rw-r--r-- 1 matt users 4096 Jun 30 12:34 [main] path/to/file.txt
drwxr-xr-x 2 matt users 4096 Jun 30 12:35 [cache] path/to/dir
-rw-r--r-- 1 matt users 8192 Jun 30 12:36 [scan-12345-1] path/to/other.dat
```

Format: `permissions links user group size mtime [index] path`

## Usage Examples

### Basic Searches
```bash
# Find all Go files in main index
dcfhfind main --name "*.go"

# Find large files across all indices  
dcfhfind all --size +100M --ls

# Find corrupted entries
dcfhfind scan --corrupt --print
```

### Complex Queries
```bash
# Find large media files not accessed recently
dcfhfind main \( --name "*.mp4" --or --name "*.avi" \) --size +100M --mtime +30

# Find and fix hash type issues
dcfhfind all --hash-type 0 --fix auto

# Cross-index comparison
dcfhfind cache --not --in-index main --printf "Only in cache: %p\n"
```

### Recovery Workflows
```bash
# Extract valid entries for recovery
dcfhfind all --valid --export recovered.idx

# Find missing files
dcfhfind main --missing --printf "Missing: %p\n"

# Diagnostic listing
dcfhfind all --deleted --ls > deleted-files.txt
```

### Performance Analysis
```bash
# Size distribution analysis
dcfhfind all --printf "%s\n" | awk '{sum+=$1} END {print sum}'

# Find duplicates by hash
dcfhfind all --printf "%H %i:%p\n" | sort | uniq -d

# Time-based analysis  
dcfhfind scan --mmin -60 --printf "%TY-%Tm-%Td %TH:%TM:%TS %p\n"
```

## Implementation Architecture

### Core Components

1. **Expression Parser**: Parse command line into AST
2. **Starting Point Resolver**: Resolve index types to file paths
3. **Index Reader**: Stream entries from index files
4. **Test Engine**: Evaluate boolean expressions against entries
5. **Action Engine**: Execute actions on matching entries
6. **Printf Formatter**: Format output using binaryEntry fields

### Key Classes/Structures

```go
type Expression interface {
    Evaluate(entry *binaryEntry, context *EvalContext) (bool, error)
}

type StartingPoint struct {
    Type IndexType  // main, cache, scan, file
    Path string     // resolved file path
}

type EvalContext struct {
    IndexPath   string
    IndexType   IndexType
    Repository  string
    Options     GlobalOptions
}
```

### Processing Pipeline

1. **Parse**: Command line → Expression AST
2. **Resolve**: Starting points → Index file paths
3. **Stream**: Index files → binaryEntry stream
4. **Evaluate**: Expression AST + Entry → boolean result
5. **Execute**: Actions on matching entries

## Package Dependencies

### Required Exports from pkg/

```go
// From index.go
- LoadIndexFromFile()
- ValidateEntry()
- ParseBinaryEntry()

// From recovery.go  
- ValidateEntryLogical()
- CreateCleanCopy()

// From util.go
- RelativePath()
- HashString()
- TimeFromWall()
```

## Compatibility Notes

### Differences from Unix find(1)

**Syntax Changes**:
- `--flag` instead of `-flag` (modern CLI convention)
- Starting points are index types, not directories
- Tests operate on index entries, not filesystem

**Additional Features**:
- Hash-based operations
- Index validation and repair
- Cross-index queries
- Repository-aware discovery

**Missing Features** (by design):
- Filesystem traversal (`-newer`, `-anewer`)
- Mount point handling (`-mount`)
- Interactive race conditions (`-ignore-readdir-race`)

## Future Extensions

### Planned Features
- Interactive mode (`--interactive`)
- Batch operations (`--batch FILE`)
- Index merging operations
- Statistical analysis (`--stats`)
- Export to other formats (`--export-csv`, `--export-json`)

### Plugin Architecture
- Custom test functions
- Custom action functions  
- Output format plugins
- Index format plugins

## Security Considerations

- Input validation for all patterns and paths
- Safe handling of malformed index files
- Protection against path traversal in export operations
- Proper error handling for permission issues

---

This design provides a powerful, Unix-familiar interface for dcfh repository management while leveraging the unique capabilities of the dcfh index format and validation system.