package dircachefilehash

import (
	"testing"
)

func TestSetDebugFlags(t *testing.T) {
	tests := []struct {
		name                  string
		input                 string
		expectedExtraValid    bool
		expectedMemoryLayout  bool
		expectedIndexChaining bool
		expectedScanning      bool
	}{
		{
			name:                  "empty string",
			input:                 "",
			expectedExtraValid:    false,
			expectedMemoryLayout:  false,
			expectedIndexChaining: false,
			expectedScanning:      false,
		},
		{
			name:                  "single option",
			input:                 "extravalidation",
			expectedExtraValid:    true,
			expectedMemoryLayout:  false,
			expectedIndexChaining: false,
			expectedScanning:      false,
		},
		{
			name:                  "multiple options",
			input:                 "extravalidation,memorylayout,indexchaining,scanning",
			expectedExtraValid:    true,
			expectedMemoryLayout:  true,
			expectedIndexChaining: true,
			expectedScanning:      true,
		},
		{
			name:                  "options with values",
			input:                 "extravalidation:true,memorylayout:false,indexchaining:1,scanning:0",
			expectedExtraValid:    true,
			expectedMemoryLayout:  false,
			expectedIndexChaining: true,
			expectedScanning:      false,
		},
		{
			name:                  "mixed format",
			input:                 "extravalidation,memorylayout:false,indexchaining",
			expectedExtraValid:    true,
			expectedMemoryLayout:  false,
			expectedIndexChaining: true,
			expectedScanning:      false,
		},
		{
			name:                  "whitespace handling",
			input:                 " extravalidation , memorylayout , indexchaining ",
			expectedExtraValid:    true,
			expectedMemoryLayout:  true,
			expectedIndexChaining: true,
			expectedScanning:      false,
		},
		{
			name:                  "case insensitive",
			input:                 "ExtraValidation,MEMORYLAYOUT,IndexChaining",
			expectedExtraValid:    true,
			expectedMemoryLayout:  true,
			expectedIndexChaining: true,
			expectedScanning:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset debug flags
			SetDebugFlags("")

			SetDebugFlags(tt.input)

			if IsDebugEnabled("extravalidation") != tt.expectedExtraValid {
				t.Errorf("extravalidation: expected %v, got %v", tt.expectedExtraValid, IsDebugEnabled("extravalidation"))
			}
			if IsDebugEnabled("memorylayout") != tt.expectedMemoryLayout {
				t.Errorf("memorylayout: expected %v, got %v", tt.expectedMemoryLayout, IsDebugEnabled("memorylayout"))
			}
			if IsDebugEnabled("indexchaining") != tt.expectedIndexChaining {
				t.Errorf("indexchaining: expected %v, got %v", tt.expectedIndexChaining, IsDebugEnabled("indexchaining"))
			}
			if IsDebugEnabled("scanning") != tt.expectedScanning {
				t.Errorf("scanning: expected %v, got %v", tt.expectedScanning, IsDebugEnabled("scanning"))
			}
		})
	}
}

func TestDebugFlagAccessors(t *testing.T) {
	SetDebugFlags("extravalidation,indexchaining")

	if !IsDebugEnabled("extravalidation") {
		t.Error("Expected IsDebugEnabled('extravalidation') to return true")
	}
	if IsDebugEnabled("memorylayout") {
		t.Error("Expected IsDebugEnabled('memorylayout') to return false")
	}
	if !IsDebugEnabled("indexchaining") {
		t.Error("Expected IsDebugEnabled('indexchaining') to return true")
	}
	if IsDebugEnabled("scanning") {
		t.Error("Expected IsDebugEnabled('scanning') to return false")
	}
}

func TestDebugFlagCaseInsensitive(t *testing.T) {
	SetDebugFlags("ExtraValidation")

	// Should work with different cases
	if !IsDebugEnabled("extravalidation") {
		t.Error("Expected lowercase flag name to work")
	}
	if !IsDebugEnabled("ExtraValidation") {
		t.Error("Expected mixed case flag name to work")
	}
	if !IsDebugEnabled("EXTRAVALIDATION") {
		t.Error("Expected uppercase flag name to work")
	}
}

func TestDebugFlagValueParsing(t *testing.T) {
	tests := []struct {
		input    string
		flag     string
		expected bool
	}{
		{"flag:true", "flag", true},
		{"flag:TRUE", "flag", true},
		{"flag:1", "flag", true},
		{"flag:yes", "flag", true},
		{"flag:on", "flag", true},
		{"flag:false", "flag", false},
		{"flag:FALSE", "flag", false},
		{"flag:0", "flag", false},
		{"flag:no", "flag", false},
		{"flag:off", "flag", false},
		{"flag:unknown", "flag", true}, // Default to true for unknown values
		{"flag", "flag", true},         // Default to true for simple flag names
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			SetDebugFlags(tt.input)
			result := IsDebugEnabled(tt.flag)
			if result != tt.expected {
				t.Errorf("SetDebugFlags(%q) then IsDebugEnabled(%q) = %v, expected %v", tt.input, tt.flag, result, tt.expected)
			}
		})
	}
}
