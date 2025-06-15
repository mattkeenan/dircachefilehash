# Directory Cache File Hash - Test Suite Documentation

This document describes the comprehensive test suite for the `dircachefilehash` package. The test suite is designed to validate all public functions, edge cases, performance characteristics, and concurrent safety.

## Test Files Overview

### Core Functionality Tests

#### `dircachefilehash_test.go`
**Main test file covering core functionality:**
- `NewDirectoryCache()` - Cache creation and initialization
- `ScanDirectory()` / `Update()` - Directory scanning and index creation
- `LoadIndex()` - Index loading and memory mapping
- `GetEntries()` - Entry retrieval and data access
- `Stats()` - Cache statistics
- `IsMmapped()` - Memory mapping verification
- `binaryEntry` methods - Path access, hash strings, entry sizes
- `Status()` - Change detection
- `StatusWithCallback()` - Custom status processing
- `FindDuplicates()` - Duplicate file detection
- `FindByHash()` - Hash-based file lookup
- `UpdatePaths()` - Incremental updates
- `Close()` - Resource cleanup

**Test scenarios:**
- Basic workflows
- Empty directories
- Error conditions
- File modifications and detection

#### `edge_cases_test.go`
**Edge cases and error conditions:**
- Non-existent directories
- Read-only directories
- Very long filenames
- Special characters in filenames
- Empty files
- Very large files
- Deep directory structures
- Symlinks and special files
- Concurrent access patterns
- File modification during scanning
- Corrupted index files
- Multiple cache instances
- Path length calculations
- Hash type constants
- Status result methods
- File status constants

#### `utils_test.go`
**Utility functions and internal structures:**
- Time wall functions (`timeWall`, `timeFromWall`, `encodeWallTime`)
- Hash type and size constants
- `binaryEntry` structure alignment and field offsets
- `PathLenToSize()` calculations
- Header and checksum size constants
- `IndexHeader` structure layout
- Internal data structures (`fileJob`, `fileResult`, `hashJob`, etc.)
- Memory alignment properties
- Zero-copy operation validation

### Specialized Test Suites

#### `skiplist_test.go`
**Skiplist data structure testing:**
- `NewSkiplistWrapper()` - Creation with various configurations
- `Insert()` / `InsertBatch()` - Entry insertion
- `GetSortedEntries()` - Sorted retrieval
- `ForEach()` - Iterator functionality
- `Find()` - Entry lookup
- `Copy()` - Structure copying
- `Merge()` / `Delete()` - Set operations
- Performance with large datasets
- Concurrent access safety

#### `integration_test.go`
**End-to-end workflow testing:**
- **FullWorkflowNewProject**: Complete project lifecycle simulation
- **FullWorkflowLargeProject**: Performance validation with 500+ files
- **WorkflowWithDuplicateDetection**: Duplicate file management
- **WorkflowWithIncrementalUpdates**: Incremental update patterns
- **WorkflowWithErrorRecovery**: Error handling and recovery
- **PerformanceBaseline**: Performance validation with 1000+ files
- **MemoryUsageValidation**: Memory efficiency testing

#### `concurrent_test.go`
**Concurrency and stress testing:**
- **ConcurrentRead**: Multiple readers accessing same cache
- **ConcurrentStatus**: Concurrent status checking
- **ConcurrentDuplicateDetection**: Parallel duplicate detection
- **ConcurrentMultipleCaches**: Multiple cache instances
- **LargeNumberOfFiles**: Stress testing with 5000+ files
- **RapidFileChanges**: Rapid modification scenarios
- **DeepDirectoryNesting**: 20+ level directory structures
- **MemoryEfficiencyTest**: Memory usage with 10,000+ files
- **RepeatedOperations**: Memory leak detection

#### `race_test.go`
**Race condition detection (requires `-race` flag):**
- **ConcurrentEntryAccess**: Concurrent memory-mapped data access
- **ConcurrentStatusChecks**: Parallel status operations
- **ConcurrentCacheOperations**: Mixed cache operations
- **ConcurrentSkiplistOperations**: Skiplist thread safety
- **ConcurrentMemoryAccess**: Memory boundary testing
- **HighContentionDataAccess**: Maximum contention scenarios
- **RapidStateChanges**: Fast state modification patterns

#### `examples_test.go`
**Usage examples and patterns:**
- `ExampleNewDirectoryCache` - Basic usage
- `ExampleDirectoryCache_Status` - Status checking
- `ExampleDirectoryCache_FindDuplicates` - Duplicate detection
- `ExampleDirectoryCache_UpdatePaths` - Incremental updates
- **BasicWorkflow**: Common usage pattern
- **MonitoringWorkflow**: File change monitoring
- **DuplicateCleanupWorkflow**: Duplicate management
- **IncrementalBackupWorkflow**: Backup scenarios
- **GracefulErrorHandling**: Error recovery patterns

#### `benchmark_test.go`
**Performance benchmarking:**
- `BenchmarkScanDirectory` - Directory scanning performance
- `BenchmarkLoadIndex` - Index loading performance
- `BenchmarkGetEntries` - Entry retrieval performance
- `BenchmarkStatus` - Status checking performance
- `BenchmarkStatusWithChanges` - Modified file detection
- `BenchmarkHasChangesQuick` - Fast change detection
- `BenchmarkFindDuplicates` - Duplicate detection performance
- `BenchmarkBinaryEntryMethods` - Memory-mapped data access
- `BenchmarkIterateEntries` - Entry iteration performance
- Various file count scenarios (100, 1000, 5000+ files)

## Running the Tests

### Basic Test Execution

```bash
# Run all tests
go test

# Run tests with verbose output
go test -v

# Run specific test file
go test -run TestNewDirectoryCache

# Run tests matching pattern
go test -run "Test.*Status"

# Run short tests only (skips long-running tests)
go test -short
```

### Performance Testing

```bash
# Run all benchmarks
go test -bench=.

# Run specific benchmark
go test -bench=BenchmarkScanDirectory

# Run benchmarks with memory allocation stats
go test -bench=. -benchmem

# Run benchmarks multiple times for stability
go test -bench=. -count=5
```

### Race Condition Detection

```bash
# Run with race detector (recommended)
go test -race

# Run race-specific tests
go test -race -tags=race

# Run with race detector and verbose output
go test -race -v

# Long-running race detection
go test -race -tags=race -timeout=30m
```

### Stress Testing

```bash
# Run stress tests (long duration)
go test -run="Stress|Integration" -timeout=10m

# Memory pressure tests
go test -run="Memory" -timeout=5m

# Concurrent operation tests
go test -run="Concurrent" -timeout=5m
```

### Coverage Analysis

```bash
# Generate coverage report
go test -cover

# Detailed coverage analysis
go test -coverprofile=coverage.out
go tool cover -html=coverage.out

# Coverage by function
go test -covermode=count -coverprofile=coverage.out
go tool cover -func=coverage.out
```

## Test Categories

### Unit Tests
- Individual function testing
- Edge case validation
- Error condition handling
- Data structure verification

### Integration Tests
- End-to-end workflows
- Multi-component interaction
- Real-world scenarios
- Performance validation

### Stress Tests
- Large file counts (1000+ files)
- Deep directory structures
- Memory pressure scenarios
- Rapid state changes

### Concurrency Tests
- Thread safety validation
- Race condition detection
- Concurrent access patterns
- Resource contention

### Performance Tests
- Benchmarking critical paths
- Memory usage analysis
- Scalability testing
- Optimization validation

## Test Data Patterns

### File Structures
- **Small projects**: 5-20 files for basic testing
- **Medium projects**: 100-500 files for integration testing  
- **Large projects**: 1000-10000 files for stress testing
- **Deep structures**: Up to 20 directory levels
- **Long paths**: Filenames up to 255 characters

### Content Patterns
- Empty files
- Small text files (< 1KB)
- Medium files (1KB - 100KB)
- Large files (> 1MB)
- Duplicate content for deduplication testing
- Binary-like content patterns

### Modification Scenarios
- Single file modifications
- Bulk modifications
- File additions and deletions
- Timestamp-only changes
- Permission changes

## Performance Expectations

### Baseline Performance (on modern hardware)
- **Scanning**: 200+ files/second
- **Loading**: 1000+ files/second  
- **Status checking**: 500+ files/second
- **Memory usage**: < 100 bytes per file for cache structures

### Scalability Targets
- **1,000 files**: All operations < 1 second
- **10,000 files**: Scan < 10 seconds, Load < 1 second
- **100,000 files**: Scan < 60 seconds, Load < 5 seconds

## Continuous Integration

### Recommended CI Pipeline
```yaml
# Run fast tests first
- go test -short -race ./...

# Run full test suite
- go test -race -timeout=10m ./...

# Run performance tests
- go test -bench=. -benchtime=1s ./...

# Check coverage
- go test -cover ./...
```

### Test Environment Requirements
- **Disk space**: 100MB+ for temporary test files
- **Memory**: 512MB+ for large dataset tests
- **Filesystem**: Must support nanosecond timestamps
- **Permissions**: Ability to create/modify/delete files

## Debugging Failed Tests

### Common Issues
1. **Timestamp precision**: Some filesystems don't support nanosecond precision
2. **Path length limits**: Very long paths may fail on some systems
3. **Permission issues**: Tests may fail if run with insufficient permissions
4. **Concurrent access**: Race conditions may appear under high load

### Debug Flags
```bash
# Verbose output with test names
go test -v

# Print additional debug information
go test -v -args -debug

# Run single test for isolation
go test -run=TestSpecificFunction -v

# Increase timeout for slow systems
go test -timeout=30m
```

### Environment Variables
```bash
# Skip long-running tests
export SKIP_SLOW_TESTS=1

# Enable debug output
export DEBUG_CACHE=1

# Use smaller test datasets
export SMALL_DATASETS=1
```

## Contributing Tests

### Test Naming Conventions
- `Test<FunctionName>` for unit tests
- `Test<Scenario>Workflow` for integration tests  
- `Benchmark<Operation>` for performance tests
- `Example<Usage>` for documentation examples

### Test Structure
1. Setup test environment
2. Create test data
3. Execute operation
4. Validate results
5. Clean up resources

### Best Practices
- Use temporary directories for all file operations
- Clean up resources in defer statements
- Test both success and failure cases
- Include edge cases and boundary conditions
- Validate all return values
- Use table-driven tests for multiple scenarios
- Add benchmarks for performance-critical code

This comprehensive test suite ensures the reliability, performance, and safety of the `dircachefilehash` package across a wide range of usage scenarios and operating conditions.
