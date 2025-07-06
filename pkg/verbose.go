package dircachefilehash

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

var globalVerboseLevel int
var debugFlags map[string]bool

// SetVerboseLevel sets the global verbose level
func SetVerboseLevel(level int) {
	globalVerboseLevel = level
}

// GetVerboseLevel returns the current verbose level
func GetVerboseLevel() int {
	return globalVerboseLevel
}

// VerboseEnter logs function entry at level 3+ and returns a defer function for exit logging
func VerboseEnter() func() {
	if globalVerboseLevel < 3 {
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
func VerboseLog(level int, format string, args ...interface{}) {
	if globalVerboseLevel >= level {
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
	debugFlags = make(map[string]bool)
	if flagsStr == "" {
		return
	}

	flags := strings.Split(flagsStr, ",")
	for _, flag := range flags {
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
	}
}

// IsDebugEnabled returns true if the specified debug flag is enabled
func IsDebugEnabled(flag string) bool {
	if debugFlags == nil {
		return false
	}
	return debugFlags[strings.ToLower(flag)]
}
