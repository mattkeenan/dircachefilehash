# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.6.5] - 2025-07-07

### Fixed
- Fixed signal handling to properly handle interrupted scans
  - Status command now correctly creates cache.idx when interrupted by SIGINT/SIGTERM
  - Update command now writes partial index data when interrupted instead of losing progress
  - Fixed race condition where hash job submission could cause "send on closed channel" panic
  - Fixed deadlock in scan channel when comparison exits early due to shutdown
  - Both status and update commands now handle partial data gracefully
- Added proper signal handling tests to verify shutdown behavior
  - Tests verify index files are created with partial data on interruption
  - Tests ensure process exits within specified time limits (typically <10ms)
  - Tests check for panics and race conditions during concurrent operations

### Changed
- Improved debug logging for interrupted operations
  - Added concise messages showing when scans are interrupted and partial data counts
  - Debug messages only appear with `--debug=scan` flag
- Added Development section to README.md
  - Documented that AI tools are used as a personal preference by the maintainer
  - Clarified that contributors can use any development tools they prefer

## [0.6.4] - 2025-07-06

### Changed
- Applied `gofmt -s` formatting to all Go source files for consistency

## [0.6.3] - 2025-07-06

### Fixed
- Added maintainer email to goreleaser configuration
- Updated .gitignore to properly exclude dcfhfix binary
- Corrected goreleaser nfpm configuration for debian package generation

## [0.6.2] - 2025-07-06

### Changed
- Updated goreleaser configuration to properly build all three tools
  - Fixed build configuration to handle multi-binary structure
  - All three binaries (dcfh, dcfhfind, dcfhfix) now included in single package
  - Corrected go generate hooks for each tool directory

## [0.6.1] - 2025-07-06

### Fixed
- Signal handling now properly interrupts filesystem scanning operations (brown paper bag fix)
  - Added shutdown channel checks to `scanPath`, `scanPathRecursive`, and `monitorJobs` functions
  - Process now exits within milliseconds of receiving SIGINT/SIGTERM instead of timing out
  - Ensures graceful shutdown during concurrent hash operations

## [0.6.0] - 2025-07-06

### Added
- Initial public release of dircachefilehash
- Three specialized CLI tools:
  - `dcfh` - Daily operations (init, status, update, dupes, snapshots)
  - `dcfhfind` - Unix find(1)-style search interface for index files
  - `dcfhfix` - Index repair and recovery tool
- Core features:
  - Binary index format with selectable hash algorithms (SHA-1, SHA-256, SHA-512)
  - Zero-copy skiplist operations for memory efficiency
  - Concurrent file scanning with configurable worker pools
  - Signal handling for graceful shutdown (SIGINT/SIGTERM)
  - Atomic updates via temporary files and rename
  - Memory-mapped file operations for performance
  - Snapshot system for index state preservation
  - Duplicate detection with fdupes-compatible output
  - JSON output support for automation
- Index format version 1 with:
  - Host byte order for performance
  - 64-bit file size support
  - Custom time format (34-bit seconds since 1885 + 30-bit nanoseconds)
  - Variable-length path storage
  - SHA-1 checksums for integrity
- Comprehensive test suite
- Makefile-based build system
- MIT License

### Technical Notes
- Requires Go 1.24.3 or later
- Unix-only (Linux, macOS, BSD)
- Time format supports dates from 1885 to ~2429