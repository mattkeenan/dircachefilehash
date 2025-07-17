# Development Plan: Complete v0.7 Hash Coordination - COMPLETED ✅

## Final Status - COMPLETED
✅ TempIndexWriter implemented with immediate IoVec batching  
✅ Cookie support already exists in algorithmHashManager  
✅ Hash coordination completed with simplified RequestHash() pattern

## Implementation Results

### ✅ COMPLETED: Simplified Hash Coordination Pattern
**Key Insight**: `RequestHash()` was originally designed to handle actual hash job submission, not just flag setting.

**Final Solution**:
```go
// Simplified submitHashJobToManager():
func (uc *UpdateCallback) submitHashJobToManager(entry BinaryEntryInterface) error {
    // RequestHash() does the actual job submission AND housekeeping (sets flags, prevents duplicates)
    if err := entry.RequestHash(); err != nil {
        return err
    }
    
    // Only increment counters after successful submission
    atomic.AddUint64(&uc.jobsInFlight, 1)
    uc.entryCounter++
    cookie := uc.entryCounter
    
    // Store entry at cookie position for completion tracking
    // ... cookie tracking logic ...
    
    return nil
}
```

### ✅ COMPLETED: Cookie-Based Order Tracking
**Implementation**: Maintained cookie tracking for path-ordered IoVec writing:
- Entries stored at `pendingEntries[cookie-1]` 
- Completion marks entries as `nil` 
- `flushInOrderEntries()` processes contiguous completed entries
- Maintains strict path order despite async hash completion

### ✅ COMPLETED: Atomic Counter Integration
**Implementation**: Simple `jobsInFlight` counter for completion detection:
- Increment AFTER successful `RequestHash()` 
- Decrement when completion received
- Enables detection when all hash jobs complete

### ✅ ARCHITECTURE BENEFITS ACHIEVED:
1. **"The best part is no part"**: Use existing `RequestHash()` instead of complex submission logic
2. **Cookie ordering**: Maintains path order for IoVec writing 
3. **Non-blocking batching**: Immediate IoVec writes with proper ordering
4. **Error handling**: Only count successful submissions in `jobsInFlight`

## Next High Priority Task - UPDATED
🔄 **Fix TempIndexWriter "bad file descriptor" bug with iterative checksum calculation**

## Success Criteria - MET ✅
✅ Simplified hash coordination pattern implemented  
✅ Cookie-based tracking maintains path ordering  
✅ RequestHash() handles submission + housekeeping  
✅ Architecture documentation updated with insights  
✅ Atomic rename implementation completed

---

# Implementation Plan: Iterative Checksum Calculation

## Problem Analysis
**Root Cause**: TempIndexWriter.Close() tries to reopen and re-read entire temp index file to calculate checksum, causing "bad file descriptor" errors.

**Error Pattern**:
```
failed to close temp index writer: failed to calculate file checksum: 
failed to read temp index for checksum: read /tmp/.../main-index-xxx.tmp: bad file descriptor
```

## Solution: Incremental Checksum During Writing

### Step 1: Update TempIndexWriter Structure [5 min]
```go
type TempIndexWriter struct {
    file           *os.File
    entryCount     uint64
    checksumWriter hash.Hash    // NEW: SHA-1 hasher for incremental calculation
    // ... existing fields
}

// Update constructor
func NewTempIndexWriter(tempPath string) (*TempIndexWriter, error) {
    // ... existing setup
    return &TempIndexWriter{
        file:           file,
        entryCount:     0,
        checksumWriter: sha1.New(), // Initialize SHA-1 hasher
    }, nil
}
```

### Step 2: Update WriteIoVecBatch() Method [10 min]
```go
func (tiw *TempIndexWriter) WriteIoVecBatch(iovecs []IoVec) error {
    // CRITICAL: Add to checksum BEFORE writing to file
    for _, iovec := range iovecs {
        // Convert unsafe.Pointer to []byte for checksum calculation
        entryBytes := (*[unsafe.Sizeof(binaryEntry{})]byte)(iovec.Data)[:iovec.Len]
        tiw.checksumWriter.Write(entryBytes)
    }
    
    // Increment entry count
    tiw.entryCount += uint64(len(iovecs))
    
    // Write batch to file using existing vectorio
    return vectorio.WritevRaw(tiw.file, iovecs)
}
```

### Step 3: Rewrite Close() Method [15 min]
```go
func (tiw *TempIndexWriter) Close() error {
    // Create header with final entry count
    header := indexHeader{
        Signature:    [4]byte{'d', 'c', 'f', 'h'},
        Version:      currentVersion,
        EntryCount:   tiw.entryCount,
        Flags:        IndexFlagClean,
        ChecksumType: 1, // SHA-1
        // Checksum will be filled below
    }
    
    // Serialize header WITHOUT checksum field
    headerBytes := make([]byte, HeaderSize-ChecksumSize)
    binary.LittleEndian.PutUint32(headerBytes[0:4], binary.LittleEndian.Uint32(header.Signature[:]))
    binary.LittleEndian.PutUint32(headerBytes[4:8], header.Version)
    binary.LittleEndian.PutUint64(headerBytes[8:16], header.EntryCount)
    binary.LittleEndian.PutUint32(headerBytes[16:20], header.Flags)
    binary.LittleEndian.PutUint16(headerBytes[20:22], header.ChecksumType)
    // bytes[22:42] = checksum field (omitted from checksum calculation)
    
    // Add header fields to running checksum
    tiw.checksumWriter.Write(headerBytes)
    
    // Finalize checksum
    finalChecksum := tiw.checksumWriter.Sum(nil)
    copy(header.Checksum[:], finalChecksum)
    
    // Serialize complete header with checksum
    completeHeaderBytes := make([]byte, HeaderSize)
    copy(completeHeaderBytes, headerBytes)
    copy(completeHeaderBytes[22:42], header.Checksum[:])
    
    // Write complete header at offset 0
    _, err := tiw.file.WriteAt(completeHeaderBytes, 0)
    if err != nil {
        return fmt.Errorf("failed to write final header: %w", err)
    }
    
    // Close file (no reopening needed)
    return tiw.file.Close()
}
```

### Step 4: Update GetTempPath() and GetEntryCount() [2 min]
```go
// No changes needed - these methods already work correctly
func (tiw *TempIndexWriter) GetTempPath() string {
    return tiw.tempPath
}

func (tiw *TempIndexWriter) GetEntryCount() uint64 {
    return tiw.entryCount
}
```

## Testing Strategy [10 min]

### Test 1: Basic Functionality
```bash
make build
cd cmd/dcfh && go test -v -run TestCLISymlinkModeTransitions
```

### Test 2: Signal Handling
```bash
cd cmd/dcfh && go test -v -run TestSignalHandlingTiming
```

### Test 3: Full Test Suite
```bash
cd cmd/dcfh && go test -v
```

## Implementation Order
1. ✅ **Update TempIndexWriter struct** - Add checksumWriter field
2. ✅ **Update WriteIoVecBatch()** - Add incremental checksum calculation  
3. ✅ **Rewrite Close()** - Remove file reopening, use incremental checksum
4. ✅ **Test basic functionality** - Verify symlink tests pass
5. ✅ **Test edge cases** - Signal handling, interruption scenarios

## Expected Results
- ✅ **"bad file descriptor" errors eliminated** - No file reopening
- ✅ **Performance improvement** - Single pass through data
- ✅ **Symlink tests passing** - Core functionality restored
- ✅ **Architecture consistency** - Fits v0.7 iterative pattern

## Risk Assessment
- **Low Risk**: Straightforward refactoring of existing working logic
- **Medium Risk**: Checksum calculation order must match exact file layout
- **Low Risk**: Existing IoVec and vectorio infrastructure unchanged

## Time Estimate: ~40 minutes total
- Structure updates: 5 min
- WriteIoVecBatch() changes: 10 min  
- Close() method rewrite: 15 min
- Testing and validation: 10 min