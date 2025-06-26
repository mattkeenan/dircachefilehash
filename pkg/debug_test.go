package dircachefilehash

import (
	"os"
	"testing"
)

func TestParseDebugFlags(t *testing.T) {
	tests := []struct {
		name                  string
		input                 string
		expectedExtraValid    bool
		expectedMemoryLayout  bool
		expectedIndexChaining bool
	}{
		{
			name:                  "empty string",
			input:                 "",
			expectedExtraValid:    false,
			expectedMemoryLayout:  false,
			expectedIndexChaining: false,
		},
		{
			name:                  "single option",
			input:                 "extravalidation",
			expectedExtraValid:    true,
			expectedMemoryLayout:  false,
			expectedIndexChaining: false,
		},
		{
			name:                  "multiple options",
			input:                 "extravalidation,memorylayout,indexchaining",
			expectedExtraValid:    true,
			expectedMemoryLayout:  true,
			expectedIndexChaining: true,
		},
		{
			name:                  "options with values",
			input:                 "extravalidation:true,memorylayout:false,indexchaining:1",
			expectedExtraValid:    true,
			expectedMemoryLayout:  false,
			expectedIndexChaining: true,
		},
		{
			name:                  "mixed format",
			input:                 "extravalidation,memorylayout:false,indexchaining",
			expectedExtraValid:    true,
			expectedMemoryLayout:  false,
			expectedIndexChaining: true,
		},
		{
			name:                  "whitespace handling",
			input:                 " extravalidation , memorylayout , indexchaining ",
			expectedExtraValid:    true,
			expectedMemoryLayout:  true,
			expectedIndexChaining: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset debug flags
			debugFlags = DebugFlags{}
			
			ParseDebugFlags(tt.input)
			
			if debugFlags.extraValidation != tt.expectedExtraValid {
				t.Errorf("extraValidation: expected %v, got %v", tt.expectedExtraValid, debugFlags.extraValidation)
			}
			if debugFlags.memoryLayout != tt.expectedMemoryLayout {
				t.Errorf("memoryLayout: expected %v, got %v", tt.expectedMemoryLayout, debugFlags.memoryLayout)
			}
			if debugFlags.indexChaining != tt.expectedIndexChaining {
				t.Errorf("indexChaining: expected %v, got %v", tt.expectedIndexChaining, debugFlags.indexChaining)
			}
		})
	}
}

func TestInitDebugFlags(t *testing.T) {
	// Test explicit parameter takes precedence over environment
	os.Setenv("DCFH_DEBUG", "memorylayout")
	defer os.Unsetenv("DCFH_DEBUG")
	
	debugFlags = DebugFlags{}
	InitDebugFlags("extravalidation")
	
	if !debugFlags.extraValidation {
		t.Error("Expected extraValidation to be true from explicit parameter")
	}
	if debugFlags.memoryLayout {
		t.Error("Expected memoryLayout to be false (env var should be ignored)")
	}
	
	// Test environment variable when no explicit parameter
	debugFlags = DebugFlags{}
	InitDebugFlags("")
	
	if debugFlags.extraValidation {
		t.Error("Expected extraValidation to be false")
	}
	if !debugFlags.memoryLayout {
		t.Error("Expected memoryLayout to be true from environment variable")
	}
}

func TestDebugFlagAccessors(t *testing.T) {
	debugFlags = DebugFlags{
		extraValidation: true,
		memoryLayout:    false,
		indexChaining:   true,
	}
	
	if !IsExtraValidationEnabled() {
		t.Error("Expected IsExtraValidationEnabled to return true")
	}
	if IsMemoryLayoutEnabled() {
		t.Error("Expected IsMemoryLayoutEnabled to return false")
	}
	if !IsIndexChainingEnabled() {
		t.Error("Expected IsIndexChainingEnabled to return true")
	}
}

func TestParseBoolOption(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"true", true},
		{"TRUE", true},
		{"1", true},
		{"yes", true},
		{"on", true},
		{"false", false},
		{"FALSE", false},
		{"0", false},
		{"no", false},
		{"off", false},
		{"unknown", true}, // Default to true for unknown values
		{"", true},        // Default to true for empty values
	}
	
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseBoolOption(tt.input)
			if result != tt.expected {
				t.Errorf("parseBoolOption(%q) = %v, expected %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestLogDebugFlags(t *testing.T) {
	// Test with no flags enabled (should not log)
	debugFlags = DebugFlags{}
	LogDebugFlags() // Should not output anything
	
	// Test with flags enabled (would log to stderr, but we can't easily capture in test)
	debugFlags = DebugFlags{extraValidation: true, indexChaining: true}
	LogDebugFlags() // Should output to stderr
}