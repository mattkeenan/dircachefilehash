# New Unified Architecture for DCFH Operations

## Overview

This document outlines a new unified architecture for DCFH that eliminates code duplication, improves performance, and enables powerful composable operations. The core innovation is a unified Hwang-Lin algorithm that works with pluggable iterators and callbacks.

## Problems with Current Architecture

### 1. Code Duplication
- `hwangLinCompareToSkiplist` (pkg/scan.go) - for building scan indices  
- `hwangLinStatus` (pkg/status.go) - for status comparison
- Multiple workflows that essentially do the same comparison algorithm

### 2. Performance Issues
- **Dupes inefficiency**: Full scan + index loading + separate iteration over all entries
- **Multiple passes**: Status, update, and dupes each require separate filesystem scans
- **Memory inefficiency**: Must load entire indices into memory for processing

### 3. Inflexibility
- Hard to combine operations (e.g., update + dupes in single pass)
- Difficult to compare different data sources (index files vs skiplists vs filesystem)
- No way to stream through large index files without loading entirely

## New Architecture Components

### 1. Core Interfaces

#### PathEntryIterator
Abstracts the source of file entries (skiplist, streaming index, filesystem scan):

```go
type PathEntryIterator interface {
    // Get next entry in sorted order (nil when exhausted)
    Next() (*binaryEntry, error)
    
    // Current path for comparison (must be sorted)
    CurrentPath() string
    
    // Check if iterator is exhausted  
    HasNext() bool
    
    // Iterator name for debugging
    Name() string
    
    // Cleanup resources
    Close() error
}
```

#### HwangLinCallback
Handles specific operations during comparison:

```go
type HwangLinCallback interface {
    // Called for files that exist in both sides (unchanged/modified)
    OnFileCompared(leftEntry, rightEntry *binaryEntry, isModified bool) error
    
    // Called for files that exist only on right side (added)
    OnFileAdded(rightEntry *binaryEntry) error
    
    // Called for files that exist only on left side (deleted)  
    OnFileDeleted(leftEntry *binaryEntry) error
    
    // Called when scan phase completes
    OnScanComplete() error
    
    // Called when hash jobs complete (if applicable)
    OnHashingComplete() error
    
    // Callback name for debugging
    Name() string
}
```

### 2. Iterator Implementations

#### SkiplistIterator
Iterates through an existing skiplist:
- **Use case**: When we already have indices loaded in memory
- **Memory**: References existing data (zero-copy)
- **Performance**: Very fast iteration

#### StreamingIndexIterator
Streams through an index file without loading entirely:
- **Use case**: Large index files that don't fit comfortably in memory
- **Memory**: Only current entry in memory
- **Performance**: I/O bound but memory efficient
- **Bonus**: Can build skiplist while streaming via callback

#### FilesystemScanIterator  
Scans filesystem and presents files as binaryEntry stream:
- **Use case**: Getting current filesystem state
- **Memory**: Only current scanned file in memory
- **Performance**: I/O bound (filesystem scan)
- **Integration**: Uses existing scanPath functionality

#### MergedIndexIterator
Merges multiple index files/skiplists into single sorted stream:
- **Use case**: Combining main.idx + cache.idx without loading both entirely
- **Memory**: Efficient multi-way merge
- **Performance**: Streams through multiple sources simultaneously

### 3. Callback Implementations

#### ScanCallback
Builds scan indices and submits hash jobs:
```go
type ScanCallback struct {
    scanSkiplist    *skiplistWrapper
    scanFileName    string
    hashJobManager  *simpleHashManager
    callStartChan   chan<- uint64
}
```

#### StatusCallback  
Collects status information:
```go
type StatusCallback struct {
    statusFunc  func(status FileStatus, path string, leftEntry, rightEntry *binaryEntry)
    results     *StatusResult
}
```

#### DupesCallback
Builds duplicate hash map incrementally:
```go
type DupesCallback struct {
    hashMap     map[string][]*binaryEntry
    mutex       sync.Mutex
    results     []DuplicateGroup
}
```

#### CompareCallback
Shows differences between two index sources:
```go
type CompareCallback struct {
    outputFunc  func(operation string, path string, leftEntry, rightEntry *binaryEntry)
}
```

#### SkiplistBuilderCallback
Builds skiplist while streaming:
```go
type SkiplistBuilderCallback struct {
    skiplist    *skiplistWrapper
    context     string
}
```

### 4. Unified Core Function

```go
func (dc *DirectoryCache) hwangLinUnified(
    shutdownChan <-chan struct{},
    leftIterator PathEntryIterator,   // e.g., main index
    rightIterator PathEntryIterator,  // e.g., filesystem scan  
    callbacks []HwangLinCallback,     // operations to perform
) error
```

**Key features:**
- Single implementation of Hwang-Lin algorithm
- Works with any combination of iterator types
- Supports multiple callbacks in single pass
- Proper error handling and shutdown support
- Maintains sorted order guarantees

## Use Cases and Benefits

### 1. Current Operations (Improved)

#### Status Operation
**Before:**
```go
// Load main index → Load cache → Merge → Scan filesystem → Compare
mainSkiplist := dc.LoadMainIndex()
scanSkiplist := dc.updateCacheIndexWithWorkflow()  
hwangLinStatus(mainSkiplist, scanSkiplist, callback)
```

**After:**
```go
// Stream through main+cache while scanning filesystem
mergedIter := NewMergedIndexIterator("main.idx", "cache.idx")
scanIter := NewFilesystemScanIterator(dc, paths)
statusCallback := NewStatusCallback()

dc.hwangLinUnified(shutdownChan, mergedIter, scanIter, []HwangLinCallback{statusCallback})
```

#### Update Operation  
**Before:**
```go
// Load main+cache → Scan filesystem → Build scan index → Merge → Write
comparisonSkiplist := mainSkiplist.Copy()
comparisonSkiplist.Merge(cacheSkiplist)
scanSkiplist := dc.performHwangLinScanToSkiplist(comparisonSkiplist)
// Write merged result
```

**After:**
```go  
// Stream through main+cache while scanning and building scan index
mergedIter := NewMergedIndexIterator("main.idx", "cache.idx")
scanIter := NewFilesystemScanIterator(dc, paths)
scanCallback := NewScanCallback()

dc.hwangLinUnified(shutdownChan, mergedIter, scanIter, []HwangLinCallback{scanCallback})
```

#### Dupes Operation
**Before:**
```go
// Full scan workflow → Load all entries → Iterate through all entries
scanSkiplist := dc.updateCacheIndexWithWorkflow()
// Then iterate through ALL entries to build hash map
for entry := range scanSkiplist { ... }
```

**After:**
```go
// Build duplicate map during scan (no separate iteration needed)
mergedIter := NewMergedIndexIterator("main.idx", "cache.idx") 
scanIter := NewFilesystemScanIterator(dc, paths)
dupesCallback := NewDupesCallback()

dc.hwangLinUnified(shutdownChan, mergedIter, scanIter, []HwangLinCallback{dupesCallback})
```

### 2. New Powerful Operations

#### Combined Update + Dupes
```go
// Single pass: update cache.idx AND detect duplicates
mergedIter := NewMergedIndexIterator("main.idx", "cache.idx")
scanIter := NewFilesystemScanIterator(dc, paths) 
scanCallback := NewScanCallback()
dupesCallback := NewDupesCallback()

dc.hwangLinUnified(shutdownChan, mergedIter, scanIter, 
    []HwangLinCallback{scanCallback, dupesCallback})
```

#### Index File Comparison
```go
// Compare two index files without loading into memory
leftIter := NewStreamingIndexIterator("old-main.idx")
rightIter := NewStreamingIndexIterator("new-main.idx")
compareCallback := NewCompareCallback()

dc.hwangLinUnified(nil, leftIter, rightIter, []HwangLinCallback{compareCallback})
```

#### Status + Dupes  
```go
// Show status AND detect duplicates in single pass
mergedIter := NewMergedIndexIterator("main.idx", "cache.idx")
scanIter := NewFilesystemScanIterator(dc, paths)
statusCallback := NewStatusCallback()
dupesCallback := NewDupesCallback()

dc.hwangLinUnified(shutdownChan, mergedIter, scanIter,
    []HwangLinCallback{statusCallback, dupesCallback})
```

### 3. Memory and Performance Benefits

#### Before (Current Dupes)
1. **Full index loading**: Load main.idx + cache.idx into memory (~GB for large repos)
2. **Filesystem scan**: Separate scan process  
3. **Index building**: Build complete merged skiplist
4. **Separate iteration**: Iterate through ALL entries to build hash map
5. **Total time**: 10+ minutes for millions of files

#### After (New Architecture)
1. **Streaming**: Stream through indices without loading entirely
2. **Single pass**: Filesystem scan + duplicate detection simultaneously  
3. **Incremental building**: Build hash map during comparison, not after
4. **Memory efficient**: Only current entry in memory, not entire indices
5. **Total time**: Single pass through data (5x+ improvement expected)

## Migration Strategy

### Phase 1: Foundation (Week 1)

**Goal**: Implement core interfaces and basic iterators

**Tasks**:
1. Create `pkg/iterator.go` with `PathEntryIterator` interface
2. Create `pkg/callbacks.go` with enhanced `HwangLinCallback` interface  
3. Implement `SkiplistIterator` (simplest case)
4. Implement `FilesystemScanIterator` (reuse existing scanPath)
5. Implement basic `DupesCallback`
6. Create `hwangLinUnified` function (minimal version)

**Success criteria**:
- Basic framework compiles
- Simple test case works (SkiplistIterator + DupesCallback)
- No changes to existing functionality

### Phase 2: Dupes Migration (Week 2)

**Goal**: Migrate dupes to new architecture for immediate performance gains

**Tasks**:
1. Implement `StreamingIndexIterator` for index files
2. Implement `MergedIndexIterator` for main+cache combination
3. Create new `FindDuplicatesUnified` function using new architecture
4. Add CLI flag `--experimental-dupes` to test new implementation
5. Performance testing and comparison
6. Update dupes tests to work with both implementations

**Success criteria**:
- New dupes implementation works correctly
- Significant performance improvement (5x+ for large repos)
- All existing dupes tests pass
- Memory usage dramatically reduced

### Phase 3: Status Migration (Week 3)

**Goal**: Migrate status command to new architecture

**Tasks**:
1. Implement `StatusCallback` 
2. Implement `SkiplistBuilderCallback` (for building skiplists while streaming)
3. Create new `StatusUnified` function
4. Migrate `hwangLinStatus` logic to callback-based approach
5. Update status tests

**Success criteria**:
- Status command uses new architecture
- Performance improved (memory + speed)
- All existing status functionality preserved
- Tests pass

### Phase 4: Update Migration (Week 4)

**Goal**: Migrate update operations to new architecture

**Tasks**:
1. Implement `ScanCallback` (most complex - handles hash jobs)
2. Migrate `updateCacheIndexWithWorkflow` to use new architecture
3. Migrate `updateFullRepository` to use new architecture  
4. Update all update-related tests
5. Remove old `hwangLinCompareToSkiplist` function

**Success criteria**:
- All update operations use new architecture
- Performance maintained or improved
- All update tests pass
- Code duplication eliminated

### Phase 5: Combined Operations (Week 5)

**Goal**: Implement powerful combined operations

**Tasks**:
1. Implement callback dependency/priority system
2. Create `UpdateWithDuplicates` function
3. Create `StatusWithDuplicates` function  
4. Add CLI support for combined operations:
   - `dcfh update --with-dupes`
   - `dcfh status --with-dupes`
5. Performance testing for combined operations

**Success criteria**:
- Combined operations work correctly
- Significant performance gains over separate operations
- New CLI options work

### Phase 6: Advanced Features (Week 6)

**Goal**: Implement advanced use cases and cleanup

**Tasks**:
1. Implement `CompareCallback` for index file comparison
2. Add `dcfh compare` command for index comparison
3. Implement validation callbacks
4. Remove all old Hwang-Lin implementations
5. Update documentation
6. Performance benchmarking and optimization

**Success criteria**:
- All old code removed
- New advanced features working
- Complete documentation
- Performance benchmarks show improvements

## File Structure

### New Files
```
pkg/
├── iterator.go          # PathEntryIterator interface and basic iterators
├── callbacks.go         # HwangLinCallback interface and implementations  
├── unified.go           # hwangLinUnified function
├── iterator_skiplist.go # SkiplistIterator implementation
├── iterator_streaming.go# StreamingIndexIterator implementation
├── iterator_filesystem.go# FilesystemScanIterator implementation
├── iterator_merged.go   # MergedIndexIterator implementation
├── callback_scan.go     # ScanCallback implementation
├── callback_status.go   # StatusCallback implementation  
├── callback_dupes.go    # DupesCallback implementation
├── callback_compare.go  # CompareCallback implementation
└── unified_test.go      # Tests for new architecture
```

### Modified Files
```
pkg/
├── dupes.go            # Add FindDuplicatesUnified function
├── status.go           # Add StatusUnified function  
├── update.go           # Migrate to new architecture
├── workflow.go         # Migrate to new architecture
└── scan.go             # Remove old hwangLinCompareToSkiplist

cmd/dcfh/
├── dupes.go            # Add --experimental-dupes flag, then migrate
├── status.go           # Migrate to new architecture
├── update.go           # Add combined operation flags
└── compare.go          # New compare command
```

## Testing Strategy

### Unit Tests
- Test each iterator type independently
- Test each callback type independently  
- Test unified function with various iterator/callback combinations
- Mock iterators for deterministic testing

### Integration Tests  
- Test real-world scenarios with actual index files
- Performance regression tests
- Memory usage tests
- Combined operation tests

### Migration Tests
- Ensure new implementations produce identical results to old ones
- A/B testing during migration phase
- Rollback capability during early phases

## Performance Expectations

### Memory Usage
- **Current dupes (large repo)**: 2-4GB RSS (entire indices in memory)
- **New dupes**: ~100MB RSS (streaming, only current entry in memory)
- **Improvement**: 20-40x reduction in memory usage

### Time Performance  
- **Current dupes (3M files)**: 10+ minutes (multiple passes)
- **New dupes**: 2-3 minutes (single pass, no separate iteration)
- **Improvement**: 3-5x faster

### Combined Operations
- **update + dupes separately**: 15+ minutes
- **update + dupes combined**: 5-7 minutes  
- **Improvement**: 2-3x faster for combined operations

## Risk Mitigation

### Compatibility
- Keep old implementations during migration
- Feature flags for experimental features
- Extensive testing before removing old code

### Performance Regression
- Benchmark all operations before/after migration
- Performance tests in CI
- Rollback plan if performance degrades

### Complexity
- Phased migration approach
- Comprehensive documentation
- Code reviews for each phase

## Future Extensions

This architecture enables future enhancements:

### New Iterator Types
- **Network streams**: Compare local vs remote indices
- **Compressed indices**: Stream through compressed index files
- **Database backends**: Stream from SQL/NoSQL databases
- **Virtual filesystems**: Compare against cloud storage

### New Callback Types
- **Synchronization**: Sync files between repositories
- **Backup validation**: Verify backup integrity
- **Change tracking**: Log all file changes
- **Custom validation**: User-defined validation rules

### Advanced Operations
- **Three-way merge**: Compare three data sources simultaneously
- **Incremental updates**: Only process changed portions
- **Parallel processing**: Multiple iterators/callbacks in parallel
- **Streaming output**: Real-time results for long operations

This unified architecture provides a solid foundation for current performance needs while enabling powerful future capabilities.