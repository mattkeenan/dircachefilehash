package dircachefilehash

// This file defines the public API and documents usage patterns

// InitDebugFlags initialises debug flags - for CLI compatibility
func InitDebugFlags(flagsStr string) {
	if flagsStr != "" {
		SetDebugFlags(flagsStr)
	}
}

// LogDebugFlags logs the current debug flag status - for CLI compatibility  
func LogDebugFlags() {
	// Log current debug state if verbose
	if globalVerboseLevel > 0 {
		VerboseLog(1, "Debug flags initialised")
	}
}

// GetDebugEnabled returns whether a debug flag is enabled - public alternative to IsDebugEnabled
func GetDebugEnabled(flag string) bool {
	return IsDebugEnabled(flag)
}

// GetVerbose returns the current verbose level - public alternative  
func GetVerbose() int {
	return GetVerboseLevel()
}