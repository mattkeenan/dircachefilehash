# v0.7 Architecture Testing Plan

## 🧪 IMMEDIATE PRIORITIES

### Test Signal Handling (HIGH PRIORITY)

The WaitGroup-based OnComplete() implementation is complete and needs testing.

**Test Plan**:
1. **Check compilation first** - Run `go build ./pkg/...` to verify no remaining errors
2. **Update test to use new constructor** - Add shutdown channel parameter to `NewUpdateCallback()` calls
3. **Run signal handling test** - Execute `TestAdaptiveUpdateInterruption` 
4. **Verify graceful shutdown** - Confirm interrupt handling works correctly

**Required Test Updates**:
```go
// OLD constructor call in tests:
callback := NewUpdateCallback(dc, tempFile, hashManager)

// NEW constructor call needed:
callback := NewUpdateCallback(dc, tempFile, hashManager, shutdownChan)
```

**Test Location**: `cmd/dcfh/interruption_test.go`

**Expected Behavior**:
- Normal completion: WaitGroup waits for all hash jobs, then processes remaining entries
- Interrupt (^C): Shutdown channel cancels wait, processes available entries gracefully
- No busy loops or hangs in either scenario

### Compilation Check
- Run `go build ./pkg/...` 
- Fix any remaining constructor signature mismatches