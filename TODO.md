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

### Performance & Optimization
- [ ] Profile memory usage during large directory scans
- [ ] Optimize skiplist operations for better cache locality
- [ ] Benchmark vectorio vs traditional I/O patterns

### Testing & Validation
- [ ] Add comprehensive integration tests for edge cases
- [ ] Test concurrent scanning with multiple workers
- [ ] Validate atomic index replacement under failure conditions

### Documentation
- [ ] Update API documentation with current architecture
- [ ] Add usage examples for library consumers
- [ ] Document performance characteristics and tuning guidelines

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