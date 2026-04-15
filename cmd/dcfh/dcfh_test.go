package main

import (
	"strings"
	"testing"
)

func TestOutputFormat_Constants(t *testing.T) {
	if OutputHuman != "human" {
		t.Errorf("Expected OutputHuman to be 'human', got '%s'", OutputHuman)
	}
	if OutputJSON != "json" {
		t.Errorf("Expected OutputJSON to be 'json', got '%s'", OutputJSON)
	}
}

func TestGetOutputFormat(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected OutputFormat
	}{
		{"human format", "human", OutputHuman},
		{"json format", "json", OutputJSON},
		{"fdupes format", "fdupes", OutputFdupes},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore global state
			oldOutput := flagOutput
			defer func() { flagOutput = oldOutput }()

			flagOutput = tt.output
			result := getOutputFormat()
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

// resetFlags resets all global flag variables to their defaults.
// Cobra/pflag maintain state across Execute() calls in tests.
func resetFlags() {
	flagOutput = "human"
	flagJSON = false
	flagVerbose = 0
	flagDebug = ""
	flagFilehash = ""
	flagSymlinks = "none"
	flagSymlinksShortAll = false
	flagHashWorkers = 0
	flagIndexLockTimeout = 0
	flagDryRun = false
}

func TestCobraFlagParsing(t *testing.T) {
	// Test that cobra correctly parses GNU longopt --option value (space-separated)
	// This was the primary motivation for the migration
	tests := []struct {
		name        string
		args        []string
		shouldError bool
		description string
	}{
		{
			name:        "bound argument with equals",
			args:        []string{"--output=json", "version"},
			shouldError: false,
			description: "--output=json should work",
		},
		{
			name:        "space-separated argument",
			args:        []string{"--output", "json", "version"},
			shouldError: false,
			description: "--output json (space-separated) should now work with cobra",
		},
		{
			name:        "boolean flag",
			args:        []string{"--json", "version"},
			shouldError: false,
			description: "--json should work",
		},
		{
			name:        "short option consumes next arg",
			args:        []string{"-o", "json", "version"},
			shouldError: false,
			description: "-o json should work",
		},
		{
			name:        "verbose count",
			args:        []string{"-vvv", "version"},
			shouldError: false,
			description: "-vvv should work",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetFlags()

			rootCmd.SetArgs(tt.args)
			err := rootCmd.Execute()

			if tt.shouldError && err == nil {
				t.Errorf("Expected error but got none for: %s", tt.description)
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Unexpected error for %s: %v", tt.description, err)
			}
		})
	}
}

func TestResticStyleForgetFlags(t *testing.T) {
	// Test that snapshot forget flags are properly scoped and don't conflict
	// with global -w (hash-workers)
	tests := []struct {
		name string
		args []string
	}{
		{
			name: "keep-daily with equals",
			args: []string{"snapshot", "forget", "--keep-daily=7"},
		},
		{
			name: "keep-weekly long form",
			args: []string{"snapshot", "forget", "--keep-weekly", "5"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetFlags()

			rootCmd.SetArgs(tt.args)
			err := rootCmd.Execute()
			// We expect errors because there's no repo, but the flag parsing
			// itself should work (no "unknown flag" errors)
			if err != nil {
				errStr := err.Error()
				// Flag parsing errors would say "unknown flag"
				if strings.Contains(errStr, "unknown flag") {
					t.Errorf("Flag parsing failed: %v", err)
				}
			}
		})
	}
}
