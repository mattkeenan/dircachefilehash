# Development Plan: Fix v0.7 Signal Handling & Adaptive Test - IN PROGRESS 🔄

## Current Issue - IDENTIFIED ✅

**Problem**: Adaptive interrupt test is broken for v0.7 architecture because it looks for v0.6 scan index files that no longer exist.

**Root Cause**: 
- v0.6: Creates `scan-{pid}-{tid}.idx` files (detected by strace pattern `scan-\d+-\d+\.idx`)
- v0.7: Uses heap-allocated scan entries (no scan index files)
- Adaptive test fails because strace can't find scan file operations

## Solution: Enhanced Strace Analysis for v0.7 Architecture

### Current Strace Patterns (v0.6-focused):
```go
"signal":      regexp.MustCompile(`(kill|sigaction|SIGINT|signal\(2\)|rt_sigaction)`)
"scanOpen":    regexp.MustCompile(`open(at)?\(.*scan-\d+-\d+\.idx`)  // ❌ BROKEN for v0.7
"cacheWrite":  regexp.MustCompile(`(writev|write|pwrite64).*cache.*\.idx`)
"cacheRename": regexp.MustCompile(`rename\(".*cache.*\.tmp", ".*cache\.idx"\)`)
```

### Proposed Enhanced v0.7 Strace Analysis:

#### 1. Track File Descriptor to Index File Mapping
**Strategy**: Parse `open()` syscalls to map file descriptors to `.idx` files:
```
open("/path/to/repo/.dcfh/main-index-12345.tmp", O_WRONLY|O_CREAT) = 7
open("/path/to/repo/.dcfh/cache.idx", O_RDONLY) = 8
```
**Result**: `fd_7 -> main-index-temp`, `fd_8 -> cache.idx`

#### 2. Track Write Operations Before/After Signal
**Strategy**: Monitor all write syscalls to index file descriptors:
```
Timeline Analysis:
├── Pre-Signal: writev(7, [...], 10) = 1024  // Writing to temp index
├── Signal:     rt_sigaction(SIGINT, {...})  // Signal received
├── Post-Signal: close(7)                   // Cleanup
└── Post-Signal: unlink("main-index-12345.tmp") // Temp file removal
```

#### 3. Enhanced Strace Patterns for v0.7:
```go
patterns := map[string]*regexp.Regexp{
    // Signal detection
    "signal":        regexp.MustCompile(`(kill|sigaction|SIGINT|signal\(2\)|rt_sigaction)`),
    
    // v0.7: Any .idx file operations
    "indexOpen":     regexp.MustCompile(`open(at)?\([^,]*\.idx[^,]*,`),
    "tempIndexOpen": regexp.MustCompile(`open(at)?\([^,]*main-index-\d+\.tmp[^,]*,`),
    
    // Write operations (any fd)
    "writeOps":      regexp.MustCompile(`(write|writev|pwrite|pwritev)\(\d+,`),
    
    // File operations
    "close":         regexp.MustCompile(`close\(\d+\)`),
    "rename":        regexp.MustCompile(`rename\(.*\.tmp.*,.*\.idx`),
    "unlink":        regexp.MustCompile(`unlink\(.*\.tmp.*\)`),
}
```

#### 4. Strace Analysis Algorithm:
```go
type StraceAnalysis struct {
    fdToFile      map[int]string    // fd -> file path
    signalTime    time.Time         // When signal occurred
    preSignal     []WriteOp         // Writes before signal
    postSignal    []WriteOp         // Writes after signal
    interrupted   bool              // Successfully interrupted
}

type WriteOp struct {
    timestamp time.Time
    fd        int
    bytes     int
    filename  string
}

func analyzeStraceOutput(straceLog string) StraceAnalysis {
    // 1. Parse open() calls to build fd->file mapping
    // 2. Parse signal events to identify interruption point
    // 3. Parse write operations and classify as pre/post signal
    // 4. Determine if interrupt was successful based on:
    //    - Signal was delivered
    //    - Write operations occurred before signal
    //    - Cleanup operations occurred after signal
}
```

#### 5. v0.7 Interrupt Success Criteria:
```go
result.Interrupted = (
    result.SignalFound &&                    // Signal was delivered
    len(result.PreSignalWrites) > 0 &&       // Some work happened before signal
    (result.TempFileCleanup ||               // Cleanup occurred OR
     result.IndexWrites > 0)                 // Index writing in progress
)
```

#### 6. Implementation Steps:
1. **Update `createAnalysisPatterns()`** with v0.7-compatible regex patterns
2. **Enhance `analyzeInterruptResult()`** with fd tracking and timeline analysis
3. **Add timeline parsing** to classify operations before/after signal
4. **Update interrupt criteria** to match v0.7 behavior (temp index files instead of scan files)
5. **Add debug logging** to show strace analysis results for troubleshooting

### Expected Benefits:
- ✅ Works with v0.7 heap-allocated scan entries
- ✅ Tracks actual work progress via temp index writes
- ✅ Detects proper shutdown behavior (cleanup after signal)
- ✅ More accurate interrupt detection than scan file existence
- ✅ Future-proof for architecture changes

### Implementation Priority: HIGH
This fix is required to validate that the v0.7 signal handling implementation is working correctly.

---

# Previous Completed Work: v0.7 Hash Coordination - COMPLETED ✅

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