package main

import (
	"testing"
)

// Test basic option definition and parsing
func TestOptionDefinition(t *testing.T) {
	options := NewParsedOptions()

	// Test defining options
	options.DefineOption("test-string", "s", OptionTypeString, "default", "Test string option")
	options.DefineOption("test-bool", "b", OptionTypeBool, "false", "Test bool option")
	options.DefineOption("test-int", "i", OptionTypeInt, "0", "Test int option")

	// Test parsing simple options
	args := []string{"--test-string=value", "--test-bool", "--test-int=42"}
	err := options.Parse(args)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Test values
	if options.GetString("test-string") != "value" {
		t.Errorf("Expected string 'value', got %s", options.GetString("test-string"))
	}
	if !options.GetBool("test-bool") {
		t.Errorf("Expected bool true, got %v", options.GetBool("test-bool"))
	}
	if options.GetInt("test-int") != 42 {
		t.Errorf("Expected int 42, got %d", options.GetInt("test-int"))
	}
}

// Test short option parsing
func TestShortOptions(t *testing.T) {
	options := NewParsedOptions()

	options.DefineOption("verbose", "v", OptionTypeInt, "0", "Verbose level")
	options.DefineOption("help", "h", OptionTypeBool, "false", "Show help")
	options.DefineOption("quiet", "q", OptionTypeBool, "false", "Quiet mode")

	// Test combined short options
	args := []string{"-vvv", "-hq"}
	err := options.Parse(args)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Verbose should be 3 (repeated 3 times)
	if options.GetInt("verbose") != 3 {
		t.Errorf("Expected verbose level 3, got %d", options.GetInt("verbose"))
	}

	// Help and quiet should be true
	if !options.GetBool("help") {
		t.Errorf("Expected help true, got %v", options.GetBool("help"))
	}
	if !options.GetBool("quiet") {
		t.Errorf("Expected quiet true, got %v", options.GetBool("quiet"))
	}
}

// Test argument collection
func TestArgumentCollection(t *testing.T) {
	options := NewParsedOptions()

	options.DefineOption("format", "f", OptionTypeString, "human", "Format option")
	options.DefineOption("verbose", "v", OptionTypeBool, "false", "Verbose mode")

	args := []string{"--format=json", "file1.idx", "header", "show", "--verbose", "arg1", "arg2"}
	err := options.Parse(args)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Check options
	if options.GetString("format") != "json" {
		t.Errorf("Expected format 'json', got %s", options.GetString("format"))
	}
	if !options.GetBool("verbose") {
		t.Errorf("Expected verbose true, got %v", options.GetBool("verbose"))
	}

	// Check non-option arguments
	expectedArgs := []string{"file1.idx", "header", "show", "arg1", "arg2"}
	actualArgs := options.GetArgs()

	if len(actualArgs) != len(expectedArgs) {
		t.Errorf("Expected %d args, got %d", len(expectedArgs), len(actualArgs))
	}

	for i, expected := range expectedArgs {
		if i >= len(actualArgs) || actualArgs[i] != expected {
			t.Errorf("Expected arg[%d] = %s, got %s", i, expected,
				func() string {
					if i < len(actualArgs) {
						return actualArgs[i]
					}
					return "<missing>"
				}())
		}
	}
}

// Test boolean option variations
func TestBooleanOptions(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected bool
	}{
		{
			name:     "Boolean flag present",
			args:     []string{"--test-bool"},
			expected: true,
		},
		{
			name:     "Boolean flag absent",
			args:     []string{},
			expected: false,
		},
		{
			name:     "Boolean with explicit true",
			args:     []string{"--test-bool=true"},
			expected: true,
		},
		{
			name:     "Boolean with explicit false",
			args:     []string{"--test-bool=false"},
			expected: false,
		},
		{
			name:     "Boolean with 1",
			args:     []string{"--test-bool=1"},
			expected: true,
		},
		{
			name:     "Boolean with 0",
			args:     []string{"--test-bool=0"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := NewParsedOptions()
			options.DefineOption("test-bool", "t", OptionTypeBool, "false", "Test boolean")

			err := options.Parse(tt.args)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}

			if options.GetBool("test-bool") != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, options.GetBool("test-bool"))
			}
		})
	}
}

// Test error conditions
func TestOptionErrors(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*ParsedOptions)
		args    []string
		wantErr bool
	}{
		{
			name: "Unknown long option",
			setup: func(o *ParsedOptions) {
				o.DefineOption("known", "k", OptionTypeBool, "false", "Known option")
			},
			args:    []string{"--unknown"},
			wantErr: true,
		},
		{
			name: "Unknown short option",
			setup: func(o *ParsedOptions) {
				o.DefineOption("known", "k", OptionTypeBool, "false", "Known option")
			},
			args:    []string{"-u"},
			wantErr: true,
		},
		{
			name: "Invalid boolean value",
			setup: func(o *ParsedOptions) {
				o.DefineOption("test", "t", OptionTypeBool, "false", "Test option")
			},
			args:    []string{"--test=invalid"},
			wantErr: true,
		},
		{
			name: "Invalid integer value",
			setup: func(o *ParsedOptions) {
				o.DefineOption("test", "t", OptionTypeInt, "0", "Test option")
			},
			args:    []string{"--test=notanumber"},
			wantErr: true,
		},
		{
			name: "String option requires value",
			setup: func(o *ParsedOptions) {
				o.DefineOption("test", "t", OptionTypeString, "", "Test option")
			},
			args:    []string{"--test"},
			wantErr: true,
		},
		{
			name: "Integer option requires value",
			setup: func(o *ParsedOptions) {
				o.DefineOption("test", "t", OptionTypeInt, "0", "Test option")
			},
			args:    []string{"--test"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := NewParsedOptions()
			tt.setup(options)

			err := options.Parse(tt.args)
			if tt.wantErr && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

// Test default values
func TestDefaultValues(t *testing.T) {
	options := NewParsedOptions()

	options.DefineOption("string-opt", "s", OptionTypeString, "default-string", "String option")
	options.DefineOption("bool-opt", "b", OptionTypeBool, "true", "Bool option")
	options.DefineOption("int-opt", "i", OptionTypeInt, "42", "Int option")

	// Parse empty args
	err := options.Parse([]string{})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Check defaults
	if options.GetString("string-opt") != "default-string" {
		t.Errorf("Expected default string 'default-string', got %s", options.GetString("string-opt"))
	}
	if !options.GetBool("bool-opt") {
		t.Errorf("Expected default bool true, got %v", options.GetBool("bool-opt"))
	}
	if options.GetInt("int-opt") != 42 {
		t.Errorf("Expected default int 42, got %d", options.GetInt("int-opt"))
	}
}

// Test IsSet functionality
func TestIsSet(t *testing.T) {
	options := NewParsedOptions()

	options.DefineOption("set-option", "s", OptionTypeString, "default", "Set option")
	options.DefineOption("unset-option", "u", OptionTypeString, "default", "Unset option")

	args := []string{"--set-option=value"}
	err := options.Parse(args)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if !options.IsSet("set-option") {
		t.Errorf("Expected set-option to be set")
	}
	if options.IsSet("unset-option") {
		t.Errorf("Expected unset-option to not be set")
	}
}
