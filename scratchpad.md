# v0.6 Deprecated Code Cleanup - Implementation Plan

## Option A: Complete Unified Dupes First

### Implementation Plan for FindDuplicatesUnified

**Current State Analysis**:
- `FindDuplicatesUnified()` has skeleton but lines 112-124 are commented out
- Need to integrate `DupesCallback` with `hwangLinUnified` algorithm
- Should leverage existing v0.7 unified architecture patterns

**Step 1: Analyze DupesCallback Requirements**
```go
// Current DupesCallback interface needs:
type DupesCallback struct {
    // Hash map for duplicate detection
    hashMap map[string][]string
    // Results accumulation
    duplicateGroups []DuplicateGroup
}

// Key methods:
- OnComparison() - detect when files have same hash
- OnLeftOnly() / OnRightOnly() - handle single files  
- OnComplete() - finalize results
- GetResults() - return duplicate groups
```

**Step 2: Implement DupesCallback Integration**
```go
func (dc *DirectoryCache) FindDuplicatesUnified(shutdownChan <-chan struct{}, flags map[string]string) ([]DuplicateGroup, error) {
    // Load merged main+cache indices (existing code works)
    mergedSkiplist, err := dc.LoadMergedMainCacheIndex()
    
    // Create iterators (existing code works)
    skiplistIterator := NewBinaryEntrySkiplistIterator(mergedSkiplist, "merged-main-cache")
    filesystemIterator := NewUnifiedFilesystemScanIterator(dc, []string{}, "filesystem-scan")
    
    // Create callback for duplicate detection
    dupesCallback := NewDupesCallback("unified-dupes")
    
    // Run unified Hwang-Lin algorithm 
    err = hwangLinUnified(skiplistIterator, filesystemIterator, dupesCallback, shutdownChan)
    
    // Extract results
    result := dupesCallback.GetResults()
    return result, err
}
```

**Step 3: Verify DupesCallback Compatibility**
- Check if existing `DupesCallback` methods match `HwangLinCallback` interface
- Ensure `OnComparison()` correctly detects hash matches
- Verify `GetResults()` returns proper `[]DuplicateGroup` format

**Step 4: Update CLI Integration**
```go
// In cmd/dcfh/dupes.go line 82:
// OLD: duplicates, err := cache.FindDuplicates(shutdownChan, flags)
// NEW: duplicates, err := cache.FindDuplicatesUnified(shutdownChan, flags)
```

**Step 5: Test and Validate**
- Run existing dupes tests against unified version
- Compare output format with original implementation
- Test interruption handling with shutdown channel
- Verify performance improvements (streaming vs loading)

### Expected Benefits

**Performance Improvements**:
- **Memory**: 20-40x reduction (streaming vs loading entire index)
- **Speed**: 3-5x faster (direct comparison vs building hash maps)
- **Scalability**: No memory limits for large repositories

**Architecture Benefits**:
- **Consistency**: All operations use unified hwangLinUnified algorithm
- **Maintainability**: Single comparison algorithm instead of duplicated logic
- **Testability**: Leverages existing unified architecture test patterns

### Implementation Estimate

**Time Required**: ~2-3 hours
1. **30 minutes**: Analyze DupesCallback interface compatibility
2. **60 minutes**: Implement FindDuplicatesUnified completion
3. **30 minutes**: Update CLI integration and basic testing
4. **30 minutes**: Validation and edge case testing

**Risk Level**: **Low**
- Existing DupesCallback is already implemented
- hwangLinUnified algorithm is proven and tested
- Iterator infrastructure is complete
- Fallback: can revert to FindDuplicates() if issues arise

### Success Criteria

**Functional Requirements**:
✅ FindDuplicatesUnified returns same results as FindDuplicates
✅ All output formats work correctly (human, JSON, fdupes)
✅ Interruption handling works with shutdown channel
✅ Performance is equal or better than original

**Code Quality Requirements**:
✅ Remove all TODO comments and commented code
✅ DupesCallback no longer shows as unreachable
✅ Unified architecture patterns followed consistently

### Next Steps After Completion

**Immediate Benefits**:
- Can safely remove FindDuplicates() (old v0.6 version)
- DupesCallback becomes reachable (accurate deadcode analysis)
- dupes command uses unified v0.7 architecture

**Phase 1 Cleanup Enabled**:
- Accurate deadcode analysis possible
- Safe removal of v0.6 iterator files
- Confident cleanup of deprecated functions

**Ready to proceed with Option A implementation?**