package dircachefilehash

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

var globalVerboseLevel atomic.Int32

var debugMu sync.RWMutex
var debugFlags map[string]bool

// debugActive is a plain bool for the hot-path check. Debug flags are set
// once at startup from CLI args and never change during execution, so no
// atomic or lock is needed for reads.
var debugActive bool

// SetVerboseLevel sets the global verbose level
func SetVerboseLevel(level int) {
	globalVerboseLevel.Store(int32(level))
}

// GetVerboseLevel returns the current verbose level
func GetVerboseLevel() int {
	return int(globalVerboseLevel.Load())
}

// VerboseEnter logs function entry at level 3+ and returns a defer function for exit logging
func VerboseEnter() func() {
	if GetVerboseLevel() < 3 {
		return func() {} // No-op
	}

	// Get caller function name
	pc, _, _, ok := runtime.Caller(1)
	if !ok {
		return func() {}
	}

	funcName := runtime.FuncForPC(pc).Name()
	// Strip package prefix for cleaner output
	if idx := strings.LastIndex(funcName, "."); idx != -1 {
		funcName = funcName[idx+1:]
	}

	fmt.Fprintf(os.Stderr, "[TRACE] Entering function: %s\n", funcName)

	return func() {
		fmt.Fprintf(os.Stderr, "[TRACE] Exiting function: %s\n", funcName)
	}
}

// VerboseLog logs a message at the specified verbose level
func VerboseLog(level int, format string, args ...any) {
	if GetVerboseLevel() >= level {
		fmt.Fprintf(os.Stderr, "[VERBOSE-%d] ", level)
		fmt.Fprintf(os.Stderr, format, args...)
		if !strings.HasSuffix(format, "\n") {
			fmt.Fprintf(os.Stderr, "\n")
		}
	}
}

// SetDebugFlags sets the debug flags from a comma-separated string
// Supports both simple flags ("scan,extravalidation") and key:value format ("scan:true,extravalidation:false")
func SetDebugFlags(flagsStr string) {
	debugMu.Lock()
	defer debugMu.Unlock()

	debugFlags = make(map[string]bool)
	if flagsStr == "" {
		debugActive = false
		return
	}

	flags := strings.SplitSeq(flagsStr, ",")
	anyEnabled := false
	for flag := range flags {
		flag = strings.TrimSpace(flag)
		if flag == "" {
			continue
		}

		// Handle flag:value format
		parts := strings.SplitN(flag, ":", 2)
		flagName := strings.ToLower(parts[0])
		flagValue := true // Default to true for simple flag names

		if len(parts) > 1 {
			// Parse the value
			switch strings.ToLower(parts[1]) {
			case "true", "1", "yes", "on":
				flagValue = true
			case "false", "0", "no", "off":
				flagValue = false
			default:
				flagValue = true // Default to true for unknown values
			}
		}

		debugFlags[flagName] = flagValue
		if flagValue {
			anyEnabled = true
		}
	}
	debugActive = anyEnabled
}

// IsDebugEnabled returns true if the specified debug flag is enabled.
// Fast path: if no debug flags are set (the common case), returns false
// without acquiring any locks or allocating.
func IsDebugEnabled(flag string) bool {
	if !debugActive {
		return false
	}
	debugMu.RLock()
	defer debugMu.RUnlock()

	return debugFlags[strings.ToLower(flag)]
}
