# Project Context

## Architecture Overview
- **Layered design**: Foundation (util, constants, file, index) → Data Structures (skiplist, ignore, scan) → Workflows (middleware) → Operations (status, update, dupes) → CLI
- **Critical constraint**: Only `AppendEntryToScanIndex()` writes binaryEntries to index files
- **I/O patterns**: Main/cache (read-only mmap), scan (read-write mmap), temp (vectorio + atomic rename)
- **Zero-copy design**: Skip list reuses existing entries, no memory duplication

## Key Decisions
- **GNU-ish CLI parsing**: Custom options parser replacing Go's flag package (v0.0.9)
- **ISO 8601 UTC timestamps**: All session handling uses UTC for consistency
- **Binary index format**: Git-compatible with SHA-1 hashes, atomic updates via temp files
- **Scan workflow**: Temporary scan indices deleted after merge, ensures atomic replacement

## Important Files
- `pkg/index.go` - Binary index internals, three distinct I/O patterns
- `pkg/scan.go` - Directory scanning with Hwang-Lin comparison
- `cmd/options.go` - Unified CLI options parser
- `CLAUDE.md` - Complete architecture documentation
- `TODO.md` - Current development tasks