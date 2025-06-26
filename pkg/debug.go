package dircachefilehash

import (
	"os"
	"strings"
)

// DebugFlags represents the available debug options
type DebugFlags struct {
	extraValidation bool // Enable extra validation of binaryEntry structures
	memoryLayout    bool // Log memory layout information
	indexChaining   bool // Validate index chaining consistency
	scanning        bool // Log detailed scanning progress
}

// Global debug flags instance
var debugFlags DebugFlags

// ParseDebugFlags parses debug options from a comma-separated string
// Format: "option1,option2,option3" or "option1:value,option2:value"
// Available options:
//   - extravalidation: Enable comprehensive binaryEntry validation
//   - memorylayout: Log memory layout and struct information
//   - indexchaining: Validate binaryEntry chaining consistency
//   - scanning: Log detailed scanning progress
func ParseDebugFlags(debugStr string) {
	if debugStr == "" {
		return
	}
	
	options := strings.Split(debugStr, ",")
	for _, option := range options {
		option = strings.TrimSpace(option)
		if option == "" {
			continue
		}
		
		// Handle option:value format
		parts := strings.SplitN(option, ":", 2)
		optionName := strings.ToLower(parts[0])
		optionValue := "true"
		if len(parts) > 1 {
			optionValue = parts[1]
		}
		
		switch optionName {
		case "extravalidation":
			debugFlags.extraValidation = parseBoolOption(optionValue)
		case "memorylayout":
			debugFlags.memoryLayout = parseBoolOption(optionValue)
		case "indexchaining":
			debugFlags.indexChaining = parseBoolOption(optionValue)
		case "scanning":
			debugFlags.scanning = parseBoolOption(optionValue)
		}
	}
}

// parseBoolOption converts string to bool, defaulting to true for simple option names
func parseBoolOption(value string) bool {
	switch strings.ToLower(value) {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	default:
		// Default to true for unknown values (enables simple "option" syntax)
		return true
	}
}

// InitDebugFlags initializes debug flags from environment variable or explicit string
func InitDebugFlags(debugStr string) {
	// First check explicit parameter
	if debugStr != "" {
		ParseDebugFlags(debugStr)
		return
	}
	
	// Then check environment variable
	if envDebug := os.Getenv("DCFH_DEBUG"); envDebug != "" {
		ParseDebugFlags(envDebug)
	}
}

// IsExtraValidationEnabled returns true if extra validation is enabled
func IsExtraValidationEnabled() bool {
	return debugFlags.extraValidation
}

// IsMemoryLayoutEnabled returns true if memory layout debugging is enabled
func IsMemoryLayoutEnabled() bool {
	return debugFlags.memoryLayout
}

// IsIndexChainingEnabled returns true if index chaining validation is enabled
func IsIndexChainingEnabled() bool {
	return debugFlags.indexChaining
}

// IsScanningEnabled returns true if scanning debug output is enabled
func IsScanningEnabled() bool {
	return debugFlags.scanning
}

// LogDebugFlags logs the current debug flag state (if any flags are enabled)
func LogDebugFlags() {
	if !debugFlags.extraValidation && !debugFlags.memoryLayout && !debugFlags.indexChaining && !debugFlags.scanning {
		return
	}
	
	var enabled []string
	if debugFlags.extraValidation {
		enabled = append(enabled, "extravalidation")
	}
	if debugFlags.memoryLayout {
		enabled = append(enabled, "memorylayout")
	}
	if debugFlags.indexChaining {
		enabled = append(enabled, "indexchaining")
	}
	if debugFlags.scanning {
		enabled = append(enabled, "scanning")
	}
	
	// Note: Using fmt would create circular import, so we write directly
	os.Stderr.WriteString("Debug flags enabled: " + strings.Join(enabled, ",") + "\n")
}