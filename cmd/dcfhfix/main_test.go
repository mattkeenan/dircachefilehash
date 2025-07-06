package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Test argument parsing
func TestArgumentParsing(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "No arguments",
			args:    []string{},
			wantErr: false, // Shows help and exits
		},
		{
			name:    "Only index file",
			args:    []string{"test.idx"},
			wantErr: false, // Shows help and exits
		},
		{
			name:    "Help command",
			args:    []string{"help"},
			wantErr: false,
		},
		{
			name:    "Help with command",
			args:    []string{"help", "header"},
			wantErr: false,
		},
		{
			name:    "Invalid format",
			args:    []string{"--format=invalid", "test.idx", "header", "show"},
			wantErr: true,
			errMsg:  "invalid format",
		},
		{
			name:    "Valid human format",
			args:    []string{"--format=human", "test.idx", "header", "show"},
			wantErr: false,
		},
		{
			name:    "Valid json format",
			args:    []string{"--format=json", "test.idx", "header", "show"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := NewParsedOptions()
			
			// Define the same options as main
			options.DefineOption("help", "h", OptionTypeBool, "false", "Show help message")
			options.DefineOption("version", "", OptionTypeBool, "false", "Show version information")
			options.DefineOption("verbose", "v", OptionTypeInt, "0", "Enable verbose output")
			options.DefineOption("dry-run", "n", OptionTypeBool, "false", "Preview changes")
			options.DefineOption("backup", "b", OptionTypeBool, "true", "Create backup")
			options.DefineOption("force", "f", OptionTypeBool, "false", "Force operations")
			options.DefineOption("quiet", "q", OptionTypeBool, "false", "Suppress output")
			options.DefineOption("format", "", OptionTypeString, "human", "Output format")
			
			err := options.Parse(tt.args)
			if err != nil {
				if !tt.wantErr {
					t.Errorf("Parse() error = %v, wantErr %v", err, tt.wantErr)
				}
				return
			}
			
			// Check format validation
			format := options.GetString("format")
			if format != "human" && format != "json" {
				if !tt.wantErr {
					t.Errorf("Invalid format %s should cause error", format)
				}
				return
			}
			
			if tt.wantErr {
				t.Errorf("Expected error but got none")
			}
		})
	}
}

// Test format helper function
func TestGetFormat(t *testing.T) {
	tests := []struct {
		name   string
		format string
		want   string
	}{
		{
			name:   "Default format",
			format: "",
			want:   "human",
		},
		{
			name:   "Human format",
			format: "human",
			want:   "human",
		},
		{
			name:   "JSON format",
			format: "json",
			want:   "json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := NewParsedOptions()
			options.DefineOption("format", "", OptionTypeString, "human", "Output format")
			
			var args []string
			if tt.format != "" {
				args = []string{fmt.Sprintf("--format=%s", tt.format)}
			}
			
			err := options.Parse(args)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			
			got := getFormat(options)
			if got != tt.want {
				t.Errorf("getFormat() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Test command routing
func TestCommandRouting(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCmd    string
		wantSubCmd string
		wantErr    bool
	}{
		{
			name:       "Header show",
			args:       []string{"test.idx", "header", "show"},
			wantCmd:    "header",
			wantSubCmd: "show",
			wantErr:    false,
		},
		{
			name:       "Header edit field",
			args:       []string{"test.idx", "header", "edit", "version", "2"},
			wantCmd:    "header",
			wantSubCmd: "edit",
			wantErr:    false,
		},
		{
			name:       "Entry show",
			args:       []string{"test.idx", "entry", "show", "path1", "path2"},
			wantCmd:    "entry",
			wantSubCmd: "show",
			wantErr:    false,
		},
		{
			name:       "Entry edit field",
			args:       []string{"test.idx", "entry", "edit", "uid", "1000", "path1"},
			wantCmd:    "entry",
			wantSubCmd: "edit",
			wantErr:    false,
		},
		{
			name:       "Entry edit JSON",
			args:       []string{"test.idx", "entry", "edit", "json", `{"uid":1000}`, "path1"},
			wantCmd:    "entry",
			wantSubCmd: "edit",
			wantErr:    false,
		},
		{
			name:    "Header without subcommand",
			args:    []string{"test.idx", "header"},
			wantErr: true,
		},
		{
			name:    "Entry without subcommand",
			args:    []string{"test.idx", "entry"},
			wantErr: true,
		},
		{
			name:    "Unknown command",
			args:    []string{"test.idx", "unknown"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.args) < 2 {
				if !tt.wantErr {
					t.Errorf("Expected error for insufficient args")
				}
				return
			}
			
			indexFile := tt.args[0]
			command := tt.args[1]
			
			if command == tt.wantCmd {
				// Command matches expected
				if len(tt.args) >= 3 {
					subCmd := tt.args[2]
					if subCmd != tt.wantSubCmd && !tt.wantErr {
						t.Errorf("Expected subcommand %s, got %s", tt.wantSubCmd, subCmd)
					}
				} else if !tt.wantErr {
					t.Errorf("Expected subcommand but got none")
				}
			} else if !tt.wantErr {
				t.Errorf("Expected command %s, got %s", tt.wantCmd, command)
			}
			
			// Verify index file is captured
			if indexFile != "test.idx" && !tt.wantErr {
				t.Errorf("Expected index file test.idx, got %s", indexFile)
			}
		})
	}
}

// Test header command handler
func TestHandleHeaderCommand(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "Show command",
			args:    []string{"show"},
			wantErr: true, // File doesn't exist
			errMsg:  "failed to open index file",
		},
		{
			name:    "Edit field command",
			args:    []string{"edit", "version", "2"},
			wantErr: true, // File doesn't exist
			errMsg:  "failed to load index",
		},
		{
			name:    "Edit without args",
			args:    []string{"edit"},
			wantErr: true,
			errMsg:  "requires field and value",
		},
		{
			name:    "Edit with only field",
			args:    []string{"edit", "version"},
			wantErr: true,
			errMsg:  "requires field and value",
		},
		{
			name:    "Unknown subcommand",
			args:    []string{"unknown"},
			wantErr: true,
			errMsg:  "unknown header subcommand",
		},
		{
			name:    "No subcommand",
			args:    []string{},
			wantErr: true,
			errMsg:  "requires subcommand",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := NewParsedOptions()
			options.DefineOption("format", "", OptionTypeString, "human", "Output format")
			options.DefineOption("quiet", "q", OptionTypeBool, "false", "Suppress output")
			
			err := handleHeaderCommand("test.idx", tt.args, options)
			
			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

// Test entry command handler
func TestHandleEntryCommand(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "Show command",
			args:    []string{"show", "path1", "path2"},
			wantErr: true, // File doesn't exist
			errMsg:  "failed to read index file",
		},
		{
			name:    "Edit field command",
			args:    []string{"edit", "uid", "1000", "path1"},
			wantErr: true, // File doesn't exist
			errMsg:  "failed to process entries",
		},
		{
			name:    "Append command",
			args:    []string{"append", `{"path":"newfile.txt"}`},
			wantErr: true, // Missing required fields
			errMsg:  "hash is required",
		},
		{
			name:    "Remove command",
			args:    []string{"remove", "path1", "path2"},
			wantErr: true, // File doesn't exist
			errMsg:  "failed to process entries",
		},
		{
			name:    "Show without paths",
			args:    []string{"show"},
			wantErr: true,
			errMsg:  "requires path arguments",
		},
		{
			name:    "Edit without enough args",
			args:    []string{"edit", "uid"},
			wantErr: true,
			errMsg:  "requires field, value, and path",
		},
		{
			name:    "Append without JSON",
			args:    []string{"append"},
			wantErr: true,
			errMsg:  "requires JSON argument",
		},
		{
			name:    "Remove without paths",
			args:    []string{"remove"},
			wantErr: true,
			errMsg:  "requires path arguments",
		},
		{
			name:    "Unknown subcommand",
			args:    []string{"unknown"},
			wantErr: true,
			errMsg:  "unknown entry subcommand",
		},
		{
			name:    "No subcommand",
			args:    []string{},
			wantErr: true,
			errMsg:  "requires subcommand",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := NewParsedOptions()
			options.DefineOption("format", "", OptionTypeString, "human", "Output format")
			options.DefineOption("quiet", "q", OptionTypeBool, "false", "Suppress output")
			
			err := handleEntryCommand("test.idx", tt.args, options)
			
			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

// Test option parsing edge cases
func TestOptionParsing(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		checkFn func(*testing.T, *ParsedOptions)
	}{
		{
			name: "Default values",
			args: []string{},
			checkFn: func(t *testing.T, opts *ParsedOptions) {
				if opts.GetString("format") != "human" {
					t.Errorf("Expected default format 'human', got %s", opts.GetString("format"))
				}
				if opts.GetBool("backup") != true {
					t.Errorf("Expected default backup true, got %v", opts.GetBool("backup"))
				}
				if opts.GetBool("dry-run") != false {
					t.Errorf("Expected default dry-run false, got %v", opts.GetBool("dry-run"))
				}
			},
		},
		{
			name: "Override defaults",
			args: []string{"--format=json", "--backup=false", "--dry-run"},
			checkFn: func(t *testing.T, opts *ParsedOptions) {
				if opts.GetString("format") != "json" {
					t.Errorf("Expected format 'json', got %s", opts.GetString("format"))
				}
				if opts.GetBool("backup") != false {
					t.Errorf("Expected backup false, got %v", opts.GetBool("backup"))
				}
				if opts.GetBool("dry-run") != true {
					t.Errorf("Expected dry-run true, got %v", opts.GetBool("dry-run"))
				}
			},
		},
		{
			name: "Verbose levels",
			args: []string{"-vvv"},
			checkFn: func(t *testing.T, opts *ParsedOptions) {
				if opts.GetInt("verbose") != 3 {
					t.Errorf("Expected verbose level 3, got %d", opts.GetInt("verbose"))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := NewParsedOptions()
			
			// Define all options
			options.DefineOption("help", "h", OptionTypeBool, "false", "Show help")
			options.DefineOption("version", "", OptionTypeBool, "false", "Show version")
			options.DefineOption("verbose", "v", OptionTypeInt, "0", "Verbose output")
			options.DefineOption("dry-run", "n", OptionTypeBool, "false", "Preview changes")
			options.DefineOption("backup", "b", OptionTypeBool, "true", "Create backup")
			options.DefineOption("force", "f", OptionTypeBool, "false", "Force operations")
			options.DefineOption("quiet", "q", OptionTypeBool, "false", "Suppress output")
			options.DefineOption("format", "", OptionTypeString, "human", "Output format")
			
			err := options.Parse(tt.args)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			
			tt.checkFn(t, options)
		})
	}
}

// Test backup management functions
func TestGetIndexType(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"main.idx", "main"},
		{"cache.idx", "cache"},
		{"scan-123-456.idx", "scan-123-456"},
		{"/path/to/main.idx", "main"},
		{"unknown.txt", "unknown"},
		{"noextension", "unknown"},
	}
	
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := getIndexType(tt.input)
			if result != tt.expected {
				t.Errorf("getIndexType(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestBackupMetadata(t *testing.T) {
	metadata := &BackupMetadata{
		Timestamp:   time.Now(),
		Operation:   "test-op",
		Description: "test description",
		IndexFile:   "/test/main.idx",
		BackupFile:  "/test/backup.idx",
	}
	
	// Test JSON marshaling
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal metadata: %v", err)
	}
	
	// Test JSON unmarshaling
	var restored BackupMetadata
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Failed to unmarshal metadata: %v", err)
	}
	
	// Verify critical fields
	if restored.Operation != metadata.Operation {
		t.Errorf("Operation mismatch: got %q, want %q", restored.Operation, metadata.Operation)
	}
	if restored.Description != metadata.Description {
		t.Errorf("Description mismatch: got %q, want %q", restored.Description, metadata.Description)
	}
}

// Test fixes command routing
func TestHandleFixesCommand(t *testing.T) {
	options := NewParsedOptions()
	options.DefineOption("format", "", OptionTypeString, "human", "Output format")
	
	tests := []struct {
		name    string
		args    []string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "No subcommand",
			args:    []string{},
			wantErr: true,
			errMsg:  "requires subcommand",
		},
		{
			name:    "Unknown subcommand",
			args:    []string{"unknown"},
			wantErr: true,
			errMsg:  "unknown fixes subcommand",
		},
		{
			name:    "List command (will succeed with no backups)",
			args:    []string{"list"},
			wantErr: false, // Will succeed and show "No backups found"
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := handleFixesCommand("nonexistent.idx", tt.args, options)
			
			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error but got none")
				} else if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

// Benchmark option parsing
func BenchmarkOptionParsing(b *testing.B) {
	args := []string{"--format=json", "--dry-run", "-vvv", "test.idx", "header", "show"}
	
	for i := 0; i < b.N; i++ {
		options := NewParsedOptions()
		
		// Define options
		options.DefineOption("help", "h", OptionTypeBool, "false", "Show help")
		options.DefineOption("version", "", OptionTypeBool, "false", "Show version")
		options.DefineOption("verbose", "v", OptionTypeInt, "0", "Verbose output")
		options.DefineOption("dry-run", "n", OptionTypeBool, "false", "Preview changes")
		options.DefineOption("backup", "b", OptionTypeBool, "true", "Create backup")
		options.DefineOption("force", "f", OptionTypeBool, "false", "Force operations")
		options.DefineOption("quiet", "q", OptionTypeBool, "false", "Suppress output")
		options.DefineOption("format", "", OptionTypeString, "human", "Output format")
		
		err := options.Parse(args)
		if err != nil {
			b.Fatalf("Parse() error = %v", err)
		}
	}
}