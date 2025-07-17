# v0.6 Deprecated Code Cleanup - Implementation Plan

## ✅ Option A: Complete Unified Dupes First (COMPLETED)

FindDuplicatesUnified implementation has been completed successfully:
- Uncommented and integrated DupesCallback with hwangLinUnified
- Updated CLI to use FindDuplicatesUnified instead of FindDuplicates
- Implementation compiles and integrates correctly with v0.7 architecture

## 🎯 CRITICAL ISSUE DISCOVERED: Hash Job Submission Gap

### Root Cause Analysis (2025-07-17)

**Issue**: v0.7 TempIndexWriter creates index files with only headers (88 bytes), no entries written

**Investigation Results**:
1. ✅ **TempIndexWriter works correctly** when tested directly
2. ✅ **Files are scanned correctly** - debug shows files found
3. ✅ **Hash "jobs" are "submitted"** - callback tracking works
4. ✅ **Entries are written to temp index** - IoVecs created and written
5. ❌ **BUT no actual hash jobs reach the hash manager**

### Debug Trace Evidence

From `dcfh --debug=hash -vvv update`:

```
[HASH-SUBMIT] Submitting hash job for entry: file1.txt
[HASH-REQUEST] Setting hashRequested flag (but NO actual job submission to manager)  ←← THE PROBLEM
[HASH-SUBMIT] Hash job submitted successfully, cookie=1, jobsInFlight=1              ←← MISLEADING
[UPDATE-WRITE] Writing batch of 1 IoVecs, total 256 bytes to temp index             ←← WRITTEN BUT WITH ZERO HASHES
```

**Missing**: No `[HASH-MANAGER]` debug messages, which means `SubmitHashJob()` is never called.

### The Critical Gap

**Current Broken Flow**:
1. `callback.submitHashJobToManager()` calls `entry.RequestHash()` ✅
2. `RequestHash()` only sets `hashRequested = true` flag ❌ (incomplete!)
3. Entries written to temp index with uncomputed (zero) hashes ❌
4. Hash workers never see any jobs ❌
5. No completion messages ever sent ❌
6. Index files contain entries but all have zero hashes ❌

**Missing Link**: `RequestHash()` should create `hashJobStart` objects and call `hashManager.SubmitHashJob()`

### Implementation Fix

**Problem**: The `RequestHash()` method in `BinaryEntryInterface` only sets a flag:

```go
func (base *BinaryEntryBase) RequestHash() error {
    base.hashRequested = true  // ← Only sets flag, no actual job submission!
    return nil
}
```

**Solution**: Callbacks need to actually submit hash jobs to the manager:

```go
func (uc *UpdateCallback) submitHashJobToManager(entry BinaryEntryInterface) error {
    // Current: Only calls RequestHash() (flag setting)
    if err := entry.RequestHash(); err != nil {
        return err
    }
    
    // MISSING: Create and submit actual hash job
    path, err := entry.RelativePath()
    if err != nil {
        return err
    }
    
    // Create hash job start structure
    hashJob := &hashJobStart{
        JobID:    uc.hashJobManager.GetNextJobID(),  // Need this method
        Cookie:   uc.entryCounter,
        FilePath: path,
        // ... other required fields
    }
    
    // Actually submit to hash manager
    uc.hashJobManager.SubmitHashJob(hashJob)
    
    // Existing tracking code...
}
```

### Implementation Steps

1. **Add GetNextJobID() method** to algorithmHashManager
2. **Create hashJobStart objects** in submitHashJobToManager() 
3. **Call SubmitHashJob()** on the hash manager
4. **Ensure BinaryEntryInterface** can provide file paths and data needed for jobs
5. **Test hash job submission** → processing → completion flow

### Expected Result

After fix:
- Hash jobs actually submitted to workers ✅
- Workers compute real hashes ✅  
- Completion messages sent back ✅
- Entries written with computed hashes ✅
- Index files contain valid entries ✅
- All v0.7 operations work correctly ✅

### Time Estimate

**2-3 hours** to implement:
1. **1 hour**: Add missing GetNextJobID() and job creation
2. **1 hour**: Integrate actual SubmitHashJob() calls
3. **1 hour**: Test and validate hash job flow works

**Risk Level**: **Medium-Low**
- Well-defined problem with clear solution
- Existing hash manager infrastructure is complete
- Only missing the connection between RequestHash() and SubmitHashJob()

### Success Criteria

**Debug trace should show**:
- `[HASH-MANAGER] SubmitHashJob called` messages ✅
- Hash workers processing jobs ✅
- `[HASH-COMPLETE] Received completion message` ✅
- Index files > 88 bytes with real entries ✅