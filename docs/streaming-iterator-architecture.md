# Streaming Iterator Architecture with Async Hashing

> **Historical — superseded.** This document records an earlier proposal
> and does **not** describe the shipped code: the shipped
> `FilesystemScanIterator` does not subscribe to the hash-job monitor
> (`RegisterIteratorNotification` has no production caller), and scan
> entries are heap-allocated `BEScanEntry` values, not mmap-backed. For the
> current architecture see [`ARCHITECTURE.md`](ARCHITECTURE.md) and the
> project [`CLAUDE.md`](../CLAUDE.md). Kept for context/rationale.

## Overview

This document outlines the architecture for implementing a streaming `FilesystemScanIterator` that integrates with the existing hash job system to provide memory-efficient, ordered iteration through filesystem data with asynchronous hashing.

## Problem Statement

The unified architecture requires iterators that can:
1. **Stream entries** without loading entire datasets into memory
2. **Maintain sorted order** (required by Hwang-Lin algorithm)
3. **Provide fully hashed entries** for duplicate detection
4. **Handle async hashing** with proven concurrency and interrupt safety

The challenge is coordinating between:
- **Filesystem scanning** (discovers files in sorted path order)
- **Hash job system** (processes files concurrently, completes out of order)
- **Iterator interface** (must return entries in sorted order with valid hashes)

## Current System Analysis

### Existing Components
- **Hash job system**: Proven concurrent hashing with `simpleHashManager`
- **Job monitor**: Tracks job completion via `callFinishChan`
- **Skiplist**: Maintains sorted order by file path
- **Scan index**: Memory-mapped storage for binaryEntry data

### Key Insight: Two Different Orderings
- **Path order**: Files discovered and stored in sorted path order
- **Completion order**: Hash jobs complete in unpredictable order due to concurrency

## Solution Architecture

### Core Principle
The **job monitor** becomes the coordination point between async hash completion and ordered iteration. It maintains a **completed queue** of finished jobs and signals the iterator only when jobs can be released in sequential order.

### Key Abstract Concepts

#### 1. Dual-Mode Iterator Processing
The **skiplist iterator** handles two fundamentally different types of entries, but **only processes entries in sorted path order**:
- **Synchronous entries**: Unchanged files with existing valid hashes (from main/cache indices) - can be processed immediately in order
- **Asynchronous in-order entries**: Changed files that require hashing - must wait for hash completion but are guaranteed to arrive in sorted path order only

**Critical constraint**: The iterator **never** deals with out-of-order entries. It only processes entries when they are the next item in the sorted sequence, regardless of whether they are synchronous or asynchronous.

#### 2. Job Monitor Transformation
The **job monitor** performs a critical transformation:
- **Input**: Asynchronous, unordered hash job completions (jobs finish in unpredictable order due to concurrency)
- **Output**: Asynchronous, ordered completion notifications (jobs signaled to iterator in sequential JobID order)
- **Mechanism**: Completed queue buffers out-of-order completions until they can be released in sequence

This transformation is essential because:
- Hash workers complete jobs in unpredictable order (I/O timing, file sizes, CPU scheduling)
- Hwang-Lin algorithm requires strict sorted order
- Iterator must provide ordered results while maintaining async performance benefits

### Components

#### 0. BinaryEntryInterface (Unified Data Access)
**Purpose**: Unified interface for accessing binary entry data across all four data sources

The architecture requires handling four distinct data sources:
1. **Skiplist (mmap-backed)** - In-memory merged view using mmap'd data (e.g., main+cache indices)
2. **Index file (read/write)** - Direct file access without skiplist creation
3. **Index file (mmap + iterative skiplist)** - Mmap'd index with skiplist built during HwangLin
4. **Scanning (mmap-backed)** - Ephemeral entries in scan index

**Key architectural rules**:
- **Skiplists**: Always use mmap() for zero-copy access
- **Index without skiplist**: Use read()/write() for direct file access
- **Index with iterative skiplist**: Use mmap() since skiplist is being built
- **Scanning**: Always mmap() since entries are ephemeral and updated in-place

**Interface Definition**:
```go
type BinaryEntryInterface interface {
    // Field accessors (acquire read lock, can return errors for ephemeral entries)
    Size() (uint32, error)
    RelativePath() (string, error)
    HashString() (string, error)
    IsDeleted() (bool, error)
    // ... other fields with error returns ...
    
    // Setters (acquire write lock, can return errors for ephemeral entries)
    SetHash(hashBytes []byte, hashType uint16) error
    SetDeleted(deleted bool) error
    
    // Manual locking for batch operations
    RLock()
    RUnlock()
    Lock()
    Unlock()
    
    // Entry lifecycle
    IsValid() bool  // Quick check if entry is still accessible
}
```

**Implementation Types**:
- **SkiplistBinaryEntry**: mmap-backed entries in skiplist
- **ReadWriteBinaryEntry**: Standard file I/O access
- **IterativeSkiplistBinaryEntry**: mmap with iterative skiplist building
- **ScanBinaryEntry**: Ephemeral mmap entries for hash coordination

**Locking Strategy**: RWMutex-based cooperative locking
- Read operations: Multiple readers allowed
- Write operations: Exclusive access
- Hash updates: Synchronous SetHash() with write lock

#### 1. Enhanced Job Monitor
**Current**: Tracks job completion, signals workflow completion  
**Enhanced**: Maintains completed queue and signals iterator for in-order completions

```go
type simpleHashManager struct {
    // ... existing fields ...
    
    // New fields for iterator coordination
    completedQueue    []uint64              // Jobs completed but waiting for order
    nextExpectedJobID uint64                // Next JobID expected in sequence
    iteratorNotifyChan chan<- uint64        // Channel to signal iterator
}
```

**Algorithm**:
1. **On job completion**:
   - If JobID == nextExpectedJobID → signal iterator immediately
   - Otherwise → add to completed queue (maintaining sorted order)
2. **After signaling**:
   - Check completed queue for consecutive next JobIDs
   - Signal iterator for each consecutive completion
   - Stop when gap in sequence is found
3. **Update nextExpectedJobID** after each signal

#### 2. Streaming FilesystemScanIterator
**Purpose**: Iterate through filesystem entries in sorted order with valid hashes

```go
type EnhancedFilesystemScanIterator struct {
    // ... existing fields ...
    
    // Hash coordination
    hashManager      *algorithmHashManager
    completionChan   chan uint64           // Receives completion notifications
    pendingJobs      map[uint64]BinaryEntryInterface // JobID → entry waiting for hash
    currentJobID     uint64                // Next JobID we're waiting for
    scanIndexFileName string               // Scan index file name
}
```

**Algorithm**:
1. **Initialize**: Register completion channel with job monitor
2. **For each filesystem entry**:
   - Add entry to scan index (in sorted path order)
   - Create ScanBinaryEntry with per-entry RWMutex
   - If entry needs hashing:
     - Submit hash job with JobID
     - Track JobID in pendingJobs map
     - Wait for completion notification
   - If entry has valid hash → return immediately
3. **On completion notification**:
   - Remove from pendingJobs map
   - Entry now has valid hash (updated via SetHash() by hash worker)
   - Return BinaryEntryInterface to caller

#### 3. Integration Points

**Data Source Integration**:
- **Skiplist entries**: SkiplistBinaryEntry (mmap-backed, unchanged entries, already hashed)
- **Read/write index entries**: ReadWriteBinaryEntry (file I/O, direct access without skiplist)
- **Iterative skiplist entries**: IterativeSkiplistBinaryEntry (mmap-backed, building skiplist during HwangLin)
- **Scan entries**: ScanBinaryEntry (mmap-backed, ephemeral, hashed asynchronously)
- **All sources**: Unified through BinaryEntryInterface with error handling

**Hash Job Submission**:
- Jobs submitted in sorted path order (JobID 1, 2, 3, ...)
- Job monitor expects completions in this sequence
- Out-of-order completions queued until their turn
- Hash workers call SetHash() on ScanBinaryEntry with write lock

**Memory Management**:
- Scan index grows as files are discovered
- Hash workers update entries in-place via SetHash() (mremap-safe)
- Each interface implementation handles its own cleanup
- Iterator releases entries after processing

## Detailed Flow

### Scenario: Files A, B, C, D discovered in order

1. **Discovery Phase**:
   - File A discovered → add to scan index → submit JobID 1
   - File B discovered → add to scan index → submit JobID 2  
   - File C discovered → add to scan index → submit JobID 3
   - File D discovered → add to scan index → submit JobID 4

2. **Hash Completion (out of order)**:
   - JobID 3 completes → job monitor adds to completed queue [3]
   - JobID 1 completes → job monitor signals iterator (JobID 1), checks queue
   - JobID 4 completes → job monitor adds to completed queue [3, 4]
   - JobID 2 completes → job monitor signals iterator (JobID 2), then flushes queue:
     - Signal iterator (JobID 3)
     - Signal iterator (JobID 4)
     - Completed queue now empty

3. **Iterator Response**:
   - Receives JobID 1 notification → returns File A (now hashed)
   - Receives JobID 2 notification → returns File B (now hashed)
   - Receives JobID 3 notification → returns File C (now hashed)
   - Receives JobID 4 notification → returns File D (now hashed)

## Implementation Strategy

### Migration Approach: Side-by-Side Implementation

To ensure safe migration without disrupting existing operations, we will create a **new algorithm-specific job monitor** alongside the existing `simpleHashManager`. This allows us to:

1. **Preserve existing functionality**: Core operations (status, update, etc.) continue using the proven `simpleHashManager`
2. **Develop new algorithm incrementally**: New unified operations use the enhanced job monitor
3. **Gradual migration**: Convert operations one by one after thorough testing
4. **Fallback capability**: Ability to revert if issues are discovered
5. **Comparative testing**: Run both implementations side-by-side for validation

### Phase 1: New Algorithm Job Monitor
1. Create `algorithmHashManager` (copy of `simpleHashManager`)
2. Add completed queue and ordering logic
3. Add iterator notification channel support
4. Implement completion ordering algorithm
5. Add registration/deregistration for iterator notifications

### Phase 2: Streaming Iterator Implementation
1. Create enhanced `FilesystemScanIterator` with hash coordination
2. Integrate with existing scan index infrastructure
3. Add completion channel monitoring
4. Implement blocking/waiting behavior for hash completion

### Phase 3: Integration and Testing
1. Update `FindDuplicatesUnified` to use streaming iterator
2. Comprehensive testing with various completion orders
3. Performance benchmarking vs existing implementation
4. Interrupt and error handling validation

## Benefits

### Performance
- **Memory efficiency**: Only current working set in memory
- **Streaming results**: No need to wait for complete scan
- **Concurrent hashing**: Leverages existing proven system
- **Reduced latency**: Results available as soon as hashed
- **Interface overhead**: Negligible on modern processors, IO-bound operations dominate

### Reliability
- **Proven concurrency**: Reuses existing hash job system
- **Interrupt safety**: Inherits all existing safety mechanisms
- **Ordered results**: Maintains Hwang-Lin algorithm requirements
- **Error handling**: Leverages existing error propagation
- **Concurrent safety**: RWMutex-based cooperative locking

### Architecture
- **Unified interface**: BinaryEntryInterface works across all three data sources
- **Minimal changes**: Builds on existing infrastructure
- **Composable**: Works with any callback in unified algorithm
- **Extensible**: Foundation for future streaming optimizations
- **Backward compatible**: Coexists with existing binaryEntryRef during migration
- **Flexible storage**: Handles mmap, memory, ephemeral data transparently

## Lessons Learned

### Key Insights from Architecture Discussion

1. **Hwang-Lin requires strict ordering**: The algorithm depends on sorted input sequences - this is non-negotiable.

2. **Don't reinvent proven systems**: The existing hash job system with `simpleHashManager` is battle-tested for concurrency and interruption. New solutions should build on this foundation.

3. **Coordination is the key challenge**: The real problem is coordinating between async hash completion (unordered) and iterator requirements (ordered).

4. **Event-driven beats polling**: Instead of polling for completion, use push notifications from the job monitor.

5. **Single source of truth**: The job monitor is the only place that knows completion order - make it the coordination point.

6. **Separate concerns**: 
   - Job monitor handles completion ordering
   - Iterator handles path ordering and blocking
   - Hash workers handle concurrent processing

### Architecture Principles Applied

1. **Leverage existing infrastructure**: Build on proven hash job system
2. **Maintain ordering invariants**: Ensure sorted output despite async processing
3. **Use event-driven coordination**: Avoid polling and busy-waiting
4. **Single responsibility**: Each component has a clear, focused role
5. **Composable design**: Iterator works with any callback in unified algorithm

### Common Pitfalls Avoided

1. **Not polling for completion**: Event-driven notifications are more efficient
2. **Not blocking until hashed**: Iterator must ensure entries have valid hashes
3. **Not maintaining path order**: Out-of-order results break Hwang-Lin algorithm
4. **Not reusing existing systems**: The hash job system is proven and reliable
5. **Not coordinating properly**: Job monitor is the natural coordination point

## Future Enhancements

### Potential Optimizations
1. **Batched notifications**: Signal multiple completions at once
2. **Configurable buffering**: Balance memory usage vs latency
3. **Parallel iteration**: Multiple iterators for different path ranges
4. **Adaptive scheduling**: Prioritize jobs based on iterator demand

### Extended Applications
1. **Status streaming**: Real-time status updates during scan
2. **Update streaming**: Incremental index updates
3. **Validation streaming**: Continuous integrity checking
4. **Export streaming**: Large dataset export without memory pressure

## Conclusion

The streaming iterator architecture provides a foundation for memory-efficient, high-performance operations while maintaining the reliability and safety of the existing hash job system. The key innovation is using the job monitor as a coordination point to bridge async hash completion with ordered iteration requirements.

This architecture enables the unified algorithm to achieve its performance goals (20-40x memory reduction, 3-5x speed improvement) while preserving the proven concurrency and interrupt handling of the existing system.