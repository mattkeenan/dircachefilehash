# BinaryEntryInterface Implementation Plan

## Overview

This document outlines the implementation of `BinaryEntryInterface` to unify access to binary entry data across all three data sources in the HwangLin unified architecture. This interface will coexist with the existing `binaryEntryRef` system during migration.

## Background

The unified HwangLin architecture needs to handle **four distinct data sources**:

1. **Skiplist (mmap-backed)** - In-memory merged view using mmap'd data (e.g., main+cache indices)
2. **Index file (read/write)** - Direct file access without skiplist creation
3. **Index file (mmap + iterative skiplist)** - Mmap'd index with skiplist built during HwangLin
4. **Scanning (mmap-backed)** - Ephemeral entries in scan index

**Justification for four sources**: The mmap vs read/write distinction is fundamental:
- **Memory management**: mmap requires mremap/munmap handling, read/write uses standard file I/O
- **Error handling**: mmap can fail due to munmap, read/write has different failure modes
- **Performance**: mmap enables zero-copy access, read/write requires data copying
- **Ephemeral nature**: Only mmap-backed entries can be ephemeral (address changes, disappearing)

**Key Architectural Rules**:
- **Skiplists**: Always use mmap() for zero-copy access
- **Index without skiplist**: Use read()/write() for direct file access
- **Index with iterative skiplist**: Use mmap() since skiplist is being built
- **Scanning**: Always mmap() since entries are ephemeral and updated in-place

## Current Problem

The existing `binaryEntryRef` assumes everything comes from mmap'd index files with offset-based references. This doesn't handle:
- **Skiplist entries**: Already in memory, but still mmap-backed (works with existing approach)
- **Read/write index entries**: Use standard file I/O, no mmap reference
- **Iterative skiplist entries**: Need mmap for building skiplist, but different from static skiplist
- **Scan entries**: Ephemeral mmap entries that can disappear or move
- **Error handling**: No mechanism for handling dereferencing failures

## Solution: BinaryEntryInterface

### Interface Definition

```go
// BinaryEntryInterface abstracts access to binary entry data
// regardless of storage mechanism (mmap, read/write, ephemeral)
type BinaryEntryInterface interface {
    // Field accessors (acquire read lock, can return errors for ephemeral entries)
    Size() (uint32, error)
    CTimeWall() (uint64, error)
    MTimeWall() (uint64, error)
    Dev() (uint32, error)
    Ino() (uint32, error)
    Mode() (uint32, error)
    UID() (uint32, error)
    GID() (uint32, error)
    FileSize() (uint64, error)
    HashType() (uint16, error)
    Hash() ([20]byte, error)
    EntryFlags() (uint32, error)
    
    // Derived methods (acquire read lock, can return errors for ephemeral entries)
    RelativePath() (string, error)
    HashString() (string, error)
    IsDeleted() (bool, error)
    
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

### Implementation Types

#### 1. SkiplistBinaryEntry (mmap-backed)
```go
// SkiplistBinaryEntry - mmap-backed entries in skiplist
type SkiplistBinaryEntry struct {
    ref   binaryEntryRef // mmap reference with offset
    mutex sync.RWMutex   // Per-entry locking for concurrent access
}
```
**Use case**: In-memory merged view using mmap'd data (e.g., main+cache indices)
**Storage**: mmap() - zero-copy access to persistent data
**Locking**: Per-entry RWMutex + underlying index-level RWMutex
**Memory management**: Managed by skiplist lifecycle, uses existing `mmapIndexFile` cleanup
**Error handling**: Can fail if underlying mmap is unmapped

#### 2. ReadWriteBinaryEntry (file I/O)
```go
// ReadWriteBinaryEntry - standard file I/O access
type ReadWriteBinaryEntry struct {
    entry *binaryEntry   // Copy of data in memory
    mutex sync.RWMutex   // Per-entry locking
}
```
**Use case**: Direct file access without skiplist creation
**Storage**: read()/write() - data copied to memory
**Locking**: Per-entry RWMutex only
**Memory management**: Entry data owned by this structure
**Error handling**: Standard I/O errors, no ephemeral failures

#### 3. IterativeSkiplistBinaryEntry (mmap-backed)
```go
// IterativeSkiplistBinaryEntry - mmap with iterative skiplist building
type IterativeSkiplistBinaryEntry struct {
    ref   binaryEntryRef // mmap reference with offset
    mutex sync.RWMutex   // Per-entry locking
}
```
**Use case**: Mmap'd index with skiplist built during HwangLin
**Storage**: mmap() - zero-copy access while building skiplist
**Locking**: Per-entry RWMutex + underlying index-level RWMutex
**Memory management**: Managed by iterative skiplist process
**Error handling**: Can fail if underlying mmap is unmapped

#### 4. ScanBinaryEntry (mmap-backed, ephemeral)
```go
// ScanBinaryEntry - ephemeral mmap entries for hash coordination
type ScanBinaryEntry struct {
    ref   binaryEntryRef // mmap reference with offset (can change/disappear)
    mutex sync.RWMutex   // Per-entry locking for hash worker coordination
}
```
**Use case**: Ephemeral entries during filesystem scanning
**Storage**: mmap() - ephemeral entries that can disappear or move (mremap)
**Locking**: Per-entry RWMutex + underlying index-level RWMutex
**Memory management**: Uses existing scan index cleanup (`cleanupCurrentScanFile()`)
**Error handling**: Can fail if mmap is unmapped or remapped during access

### Locking Strategy

**RWMutex-based cooperative locking**:
- **Read operations**: Multiple readers can access simultaneously
- **Write operations**: Exclusive access for modifications
- **Batch operations**: Manual locking for efficient multi-field access

```go
// Reading (multiple readers allowed)
func (e *ScanBinaryEntry) HashString() string {
    e.mutex.RLock()
    defer e.mutex.RUnlock()
    return hex.EncodeToString(e.hash[:])
}

// Writing (exclusive access)
func (e *ScanBinaryEntry) SetHash(hashBytes []byte, hashType uint16) error {
    e.mutex.Lock()
    defer e.mutex.Unlock()
    copy(e.hash[:], hashBytes)
    e.hashType = hashType
    return nil
}

// Batch operations (manual locking)
func processEntry(entry BinaryEntryInterface) {
    entry.RLock()
    defer entry.RUnlock()
    
    // Multiple field accesses without re-locking
    path := entry.RelativePath()
    hash := entry.HashString()
    size := entry.FileSize()
    // ... process data
}
```

### Hash Update Coordination

**Synchronous SetHash() with existing coordination**:
- Hash workers call `SetHash()` directly on scan entries
- Coordination happens through `algorithmHashManager` completion queue
- Iterator waits for completion notifications before returning entries
- Updates are in-place in scan index mmap memory (existing pattern)

### Memory Management

**Each implementation handles its own cleanup**:
- **DirectBinaryEntry**: No cleanup needed
- **MmapBinaryEntry**: Uses existing `mmapIndexFile` cleanup mechanisms
- **SkiplistBinaryEntry**: Managed by skiplist lifecycle
- **ScanBinaryEntry**: Uses existing scan index cleanup (`cleanupCurrentScanFile()`)

No new lifecycle methods needed - existing patterns are sufficient.

## Migration Strategy

### Phase 1: Implementation (Current)
- [ ] Implement `BinaryEntryInterface` 
- [ ] Implement four wrapper types
- [ ] Add interface alongside existing `binaryEntryRef`
- [ ] Update iterator interfaces to return `BinaryEntryInterface`
- [ ] Update HwangLin algorithm to accept `BinaryEntryInterface`

### Phase 2: Enhanced Iterator Integration
- [ ] Update `EnhancedFilesystemScanIterator` to use interface
- [ ] Update `SkiplistIterator` to use interface
- [ ] Integration testing with unified algorithm

### Phase 3: Operation Migration
- [ ] Migrate `FindDuplicates` to use interface
- [ ] Migrate `Status` command to use interface
- [ ] Migrate `Update` operations to use interface

### Phase 4: Cleanup
- [ ] Remove `binaryEntryRef` system
- [ ] Remove old iterator implementations
- [ ] Remove old algorithm implementations

## Benefits

1. **Unified Interface**: All three data sources look identical to HwangLin
2. **Flexible Storage**: Handles mmap, memory, ephemeral data transparently
3. **Concurrent Safety**: Appropriate locking per implementation type
4. **Memory Efficiency**: No forced skiplist creation when not needed
5. **Hash Coordination**: Proper synchronization for scan entries
6. **Backward Compatibility**: Existing code continues to work during migration

## Use Case Examples

### Status Command (main+cache merge)
```go
// Load main index → skiplist → SkiplistBinaryEntry
mainSkiplist := loadMainIndexToSkiplist()
// Load cache index → skiplist → SkiplistBinaryEntry  
cacheSkiplist := loadCacheIndexToSkiplist()
// Merge into single skiplist
mergedSkiplist := mergeSkiplists(mainSkiplist, cacheSkiplist)
// HwangLin compares merged skiplist vs filesystem scan
```

### Streaming Duplicate Detection
```go
// Scan entries → ScanBinaryEntry (ephemeral, with locking)
scanIterator := NewEnhancedFilesystemScanIterator(...)
// Existing main index → MmapBinaryEntry (direct mmap access)
mainIterator := NewIndexFileIterator(...)
// No skiplist creation needed → memory efficient
hwangLinUnified(scanIterator, mainIterator, dupesCallback)
```

### Recovery Operations
```go
// Multiple index files → individual MmapBinaryEntry iterators
for _, indexFile := range corruptedFiles {
    iterator := NewIndexFileIterator(indexFile)
    // Process without creating full merged view
    hwangLinUnified(iterator, nil, recoveryCallback)
}
```

## Performance Considerations

- **Interface overhead**: Negligible on modern processors
- **IO dominance**: File system operations, hashing, mmap access dominate timing
- **Correctness priority**: Architectural benefits outweigh theoretical performance concerns
- **Batch operations**: Manual locking available for performance-critical paths

## Testing Strategy

1. **Unit tests** for each implementation type
2. **Integration tests** with unified algorithm
3. **Performance benchmarks** comparing old vs new approaches
4. **Memory leak tests** for scan entry cleanup
5. **Concurrent access tests** for locking correctness

## Implementation Files

- `pkg/binary_entry_interface.go` - Interface definition
- `pkg/binary_entry_direct.go` - DirectBinaryEntry implementation
- `pkg/binary_entry_mmap.go` - MmapBinaryEntry implementation
- `pkg/binary_entry_skiplist.go` - SkiplistBinaryEntry implementation
- `pkg/binary_entry_scan.go` - ScanBinaryEntry implementation
- `pkg/binary_entry_interface_test.go` - Comprehensive tests

## Documentation Updates

- Update `streaming-iterator-architecture.md` with interface details
- Update `new-architecture.md` with interface integration
- Update `CLAUDE.md` with architectural changes
- Create implementation examples in documentation

## Completion Criteria

- [ ] All four implementations working correctly
- [ ] Interface integrated with enhanced iterator
- [ ] Unified algorithm updated to use interface
- [ ] Comprehensive test coverage
- [ ] Documentation updated
- [ ] Performance benchmarks showing acceptable overhead
- [ ] Memory management validation
- [ ] Concurrent access validation

This implementation provides the unified abstraction needed for the HwangLin architecture while maintaining backward compatibility and leveraging existing proven patterns for memory management and coordination.