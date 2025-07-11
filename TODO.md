# TODO.md - Development Task Tracker

This file tracks upcoming development tasks for the dircachefilehash project.

## Current Session
**Created:** 2025-06-28
**Status:** Active

## High Priority Tasks

### Architecture & Code Quality
- [ ] Consider renaming `pkg/file.go` to `pkg/filehash.go` for clarity
- [ ] Consider renaming `pkg/middleware.go` to better reflect its workflow nature
- [ ] Review layer separation and ensure proper abstraction boundaries
- [ ] Export skiplist wrapper functions (Find, Insert, etc.) for low-level package access
  - Current skiplist wrapper is "high-level" within pkg but "low-level" for external users
  - External tools like dcfhfix need efficient O(log n) entry lookup instead of O(n) iteration
  - Should export: Find(), Insert(), ForEach(), and other core skiplist operations
  - Consider adding FindEntries() function that takes index path + paths array

### Performance & Optimization
- [ ] Profile memory usage during large directory scans
- [ ] Optimize skiplist operations for better cache locality
- [ ] Benchmark vectorio vs traditional I/O patterns
- [x] Fix live locks during shutdown
  - Fixed: Hash workers now always signal job completion even during shutdown
  - Fixed: Job submission checks for shutdown before adding new jobs
  - Fixed: Monitor goroutine no longer busy-waits (removed default case)
  - Ensured proper coordination between workers and monitor during shutdown
- [x] Implement mutex protection for mmap'd memory during hash calculations
  - Added RWMutex protection for all mmap'd index files (main, cache, scan)
  - Configurable timeout via --index-lock-timeout flag and config file
  - Prevents SIGSEGV crashes when mremap occurs during hash calculation
  - Tracks all referenced indices and acquires locks in consistent order

### Testing & Validation
- [ ] Add comprehensive integration tests for edge cases
- [ ] Test concurrent scanning with multiple workers
- [ ] Validate atomic index replacement under failure conditions

### Documentation
- [ ] Update API documentation with current architecture
- [ ] Add usage examples for library consumers
- [ ] Document performance characteristics and tuning guidelines
- [ ] Create CONFIG.md documenting all configuration settings
  - Document all sections in .dcfh/config file (filehash, performance, symlink, etc.)
  - Explain each setting with examples and valid values
  - Show command-line flag equivalents
  - Include precedence order (defaults → config file → command line)

## Medium Priority Tasks

### Features
- [ ] Add configuration validation on startup
- [ ] Implement dry-run mode for update operations
- [ ] Add progress reporting for long-running operations

### Error Handling
- [ ] Improve error messages with actionable suggestions
- [ ] Add recovery mechanisms for corrupted index files
- [ ] Handle edge cases in ignore pattern matching

### CLI Enhancements
- [ ] Add tab completion support
- [ ] Implement colored output for better readability
- [ ] Add configuration file validation command

## Low Priority Tasks

### Maintenance
- [ ] Clean up temporary files on interrupted operations
- [ ] Add metrics collection for performance monitoring
- [ ] Implement log rotation for verbose output
- [ ] Fix goreleaser deprecation warning: replace `nfpms.builds` with `nfpms.ids`
  - Current config uses deprecated `builds:` field under nfpms section
  - Should either use `ids:` to reference build IDs or remove field entirely to include all builds

### Platform Support
- [ ] Test on additional Unix variants
- [ ] Verify memory mapping behavior on different filesystems
- [ ] Test with various Go versions

## Completed Tasks
- [x] Implement unified GNU-ish command-line options parser
- [x] Add configurable hash worker count via CLI and config
- [x] Refactor version output with dedicated command
- [x] Modify 'dcfh init' to skip initial scan for config adjustment

## Notes
- Architecture follows layered design from Foundation → CLI Interface
- Critical constraint: Only `AppendEntryToScanIndex()` writes binaryEntries
- Index replacement must be atomic via temp files and rename operations
- Scan indices are temporary and deleted after completion