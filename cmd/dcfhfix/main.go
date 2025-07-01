//go:generate go run generate_version.go

package main

import (
	"fmt"
	"os"
)

func main() {
	// Define global options using the same pattern as dcfh
	options := NewParsedOptions()
	
	// Define global options
	options.DefineOption("help", "h", OptionTypeBool, "false", "Show help message")
	options.DefineOption("version", "", OptionTypeBool, "false", "Show version information")
	options.DefineOption("verbose", "v", OptionTypeInt, "0", "Enable verbose output (can be repeated for more verbosity)")
	options.DefineOption("dry-run", "n", OptionTypeBool, "false", "Preview changes without modifying files")
	options.DefineOption("backup", "b", OptionTypeBool, "true", "Create backup before making changes")
	options.DefineOption("force", "f", OptionTypeBool, "false", "Force operations even if validation passes")
	options.DefineOption("quiet", "q", OptionTypeBool, "false", "Suppress non-error output")
	options.DefineOption("format", "", OptionTypeString, "human", "Output format for show commands (human|json)")
	
	// Parse command line arguments
	if err := options.Parse(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "dcfhfix: %v\n", err)
		fmt.Fprintf(os.Stderr, "Try 'dcfhfix --help' for more information.\n")
		os.Exit(1)
	}
	
	// Validate format option
	format := options.GetString("format")
	if format != "human" && format != "json" {
		fmt.Fprintf(os.Stderr, "dcfhfix: invalid format '%s', must be 'human' or 'json'\n", format)
		os.Exit(1)
	}
	
	// Handle version first (before help)
	if options.GetBool("version") {
		fmt.Printf("dcfhfix %s\n", getVersionString())
		os.Exit(0)
	}
	
	// Handle help
	if options.GetBool("help") || len(options.GetArgs()) == 0 {
		showHelp()
		os.Exit(0)
	}
	
	args := options.GetArgs()
	if len(args) < 2 {
		fmt.Fprintf(os.Stderr, "dcfhfix: missing command\n")
		fmt.Fprintf(os.Stderr, "Try 'dcfhfix --help' for more information.\n")
		os.Exit(1)
	}
	
	// Execute command - handle help specially
	if args[0] == "help" {
		if len(args) >= 2 {
			showCommandHelp([]string{"", "", args[1]})
		} else {
			showHelp()
		}
		return
	}
	
	// Normal commands: first argument is the index file
	indexFile := args[0]
	command := args[1]
	
	// Execute command
	switch command {
	case "header":
		if len(args) < 3 {
			fmt.Fprintf(os.Stderr, "dcfhfix: header command requires subcommand\n")
			fmt.Fprintf(os.Stderr, "Usage: dcfhfix <index-file> header <show|edit> [args...]\n")
			os.Exit(1)
		}
		err := handleHeaderCommand(indexFile, args[2:], options)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dcfhfix: %v\n", err)
			os.Exit(1)
		}
		
	case "entry":
		if len(args) < 3 {
			fmt.Fprintf(os.Stderr, "dcfhfix: entry command requires subcommand\n")
			fmt.Fprintf(os.Stderr, "Usage: dcfhfix <index-file> entry <show|edit|append|remove|resort> [args...]\n")
			os.Exit(1)
		}
		err := handleEntryCommand(indexFile, args[2:], options)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dcfhfix: %v\n", err)
			os.Exit(1)
		}
		
		
	default:
		fmt.Fprintf(os.Stderr, "dcfhfix: unknown command '%s'\n", command)
		fmt.Fprintf(os.Stderr, "Try 'dcfhfix --help' for more information.\n")
		os.Exit(1)
	}
}

func showHelp() {
	fmt.Printf("dcfhfix - repair and edit tool for dcfh index files\n\n")
	fmt.Printf("Usage: dcfhfix [OPTIONS] <index-file> <command> <subcommand> [args...]\n\n")
	
	fmt.Printf("Commands:\n")
	fmt.Printf("  header show                    Show index header as JSON\n")
	fmt.Printf("  header edit <field> <value>    Edit header field\n")
	fmt.Printf("  entry show <path>...           Show entries as JSON\n")
	fmt.Printf("  entry edit <field> <value> <path>...  Edit entry field\n")
	fmt.Printf("  entry append <json>            Append new entry from JSON\n")
	fmt.Printf("  entry remove <path>...         Remove entries by path\n")
	fmt.Printf("  entry resort                   Resort all entries by path\n")
	fmt.Printf("  help [command]                 Show help for command\n\n")
	
	fmt.Printf("Options:\n")
	fmt.Printf("  -h, --help          Show this help message\n")
	fmt.Printf("      --version       Show version information\n")
	fmt.Printf("  -v, --verbose       Enable verbose output (repeat for more)\n")
	fmt.Printf("  -n, --dry-run       Preview changes without modifying files\n")
	fmt.Printf("  -b, --backup        Create backup before changes (default: true)\n")
	fmt.Printf("  -f, --force         Force operations even if validation passes\n")
	fmt.Printf("  -q, --quiet         Suppress non-error output\n")
	fmt.Printf("      --format        Output format for show commands (human|json, default: human)\n\n")
	
	fmt.Printf("Examples:\n")
	fmt.Printf("  # Show header information\n")
	fmt.Printf("  dcfhfix .dcfh/main.idx header show\n")
	fmt.Printf("  dcfhfix .dcfh/main.idx header show --format=json\n\n")
	
	fmt.Printf("  # Edit header fields\n")
	fmt.Printf("  dcfhfix .dcfh/main.idx header edit version 2\n")
	fmt.Printf("  dcfhfix .dcfh/main.idx header edit flags 0\n")
	fmt.Printf("  dcfhfix .dcfh/main.idx header edit json '{\"version\":2,\"flags\":0}'\n\n")
	
	fmt.Printf("  # Show specific entries\n")
	fmt.Printf("  dcfhfix .dcfh/main.idx entry show src/main.go README.md\n")
	fmt.Printf("  dcfhfix .dcfh/main.idx entry show src/main.go --format=json\n\n")
	
	fmt.Printf("  # Edit entry fields\n")
	fmt.Printf("  dcfhfix .dcfh/main.idx entry edit uid 1000 src/app.go\n")
	fmt.Printf("  dcfhfix .dcfh/main.idx entry edit mode 0644 file1.txt file2.txt\n")
	fmt.Printf("  dcfhfix .dcfh/main.idx entry edit json '{\"uid\":1000,\"gid\":1000}' src/app.go\n\n")
	
	fmt.Printf("  # Remove entries\n")
	fmt.Printf("  dcfhfix .dcfh/main.idx entry remove old-file.txt temp/\n\n")
	
	fmt.Printf("  # Resort index\n")
	fmt.Printf("  dcfhfix .dcfh/main.idx entry resort\n\n")
	
	fmt.Printf("Safety Features:\n")
	fmt.Printf("  - Creates backups by default (disable with --backup=false)\n")
	fmt.Printf("  - Validates changes before applying\n")
	fmt.Printf("  - Dry-run mode shows what would be changed\n")
	fmt.Printf("  - Warnings for dangerous edits (path, size, hash)\n\n")
	
	fmt.Printf("Field Names:\n")
	fmt.Printf("  Header: signature, byte_order, version, entry_count, flags, checksum_type, checksum\n")
	fmt.Printf("  Entry:  ctime, mtime, dev, ino, mode, uid, gid, size, flags, hashtype, hash\n")
	fmt.Printf("  Special: json (for JSON object editing)\n\n")
	
	fmt.Printf("Output Formats:\n")
	fmt.Printf("  human    Human-readable table format (default)\n")
	fmt.Printf("  json     Machine-readable JSON format\n\n")
	
	fmt.Printf("Notes:\n")
	fmt.Printf("  - Entries are identified by their path only\n")
	fmt.Printf("  - Hashes must be hex strings without 0x prefix\n")
	fmt.Printf("  - Changing hashtype updates hash length validation\n")
	fmt.Printf("  - All changes written to temp file then renamed\n")
}

func showCommandHelp(args []string) {
	if len(args) < 3 {
		showHelp()
		return
	}
	
	command := args[2]
	switch command {
	case "header":
		showHeaderHelp()
	case "entry":
		showEntryHelp()
	default:
		fmt.Fprintf(os.Stderr, "dcfhfix: no help available for command '%s'\n", command)
		showHelp()
	}
}

func showHeaderHelp() {
	fmt.Printf("dcfhfix header - View and edit index headers\n\n")
	fmt.Printf("Usage: dcfhfix [OPTIONS] <index-file> header <subcommand> [args...]\n\n")
	
	fmt.Printf("Subcommands:\n")
	fmt.Printf("  show                Display header as JSON\n")
	fmt.Printf("  edit <field> <value> Edit individual header field\n\n")
	
	fmt.Printf("Options:\n")
	fmt.Printf("  All global options apply (--dry-run, --backup, etc.)\n\n")
	
	fmt.Printf("Examples:\n")
	fmt.Printf("  # Show current header\n")
	fmt.Printf("  dcfhfix .dcfh/main.idx header show\n")
	fmt.Printf("  dcfhfix .dcfh/main.idx header show --format=json\n\n")
	
	fmt.Printf("  # Edit individual fields\n")
	fmt.Printf("  dcfhfix .dcfh/main.idx header edit version 2\n")
	fmt.Printf("  dcfhfix .dcfh/main.idx header edit flags 0x0001\n")
	fmt.Printf("  dcfhfix .dcfh/main.idx header edit signature dcfh\n\n")
	
	fmt.Printf("  # Edit multiple fields with JSON\n")
	fmt.Printf("  dcfhfix .dcfh/main.idx header edit json '{\"version\":2,\"flags\":0}'\n")
	fmt.Printf("  dcfhfix .dcfh/main.idx header edit json '{\"entry_count\":1234}' --dry-run\n\n")
	
	fmt.Printf("Header Fields:\n")
	fmt.Printf("  signature      4-byte signature (string: 'dcfh')\n")
	fmt.Printf("  byte_order     Byte order magic (hex: 0x0102030405060708)\n")
	fmt.Printf("  version        Index format version (integer)\n")
	fmt.Printf("  entry_count    Number of entries (integer, auto-calculated)\n")
	fmt.Printf("  flags          Index flags (hex or integer)\n")
	fmt.Printf("  checksum_type  Checksum algorithm type (integer)\n")
	fmt.Printf("  checksum       File checksum (hex string, auto-calculated)\n")
	fmt.Printf("  json           JSON object for multiple fields\n\n")
	
	fmt.Printf("Output Formats:\n")
	fmt.Printf("  human    Human-readable table format (default)\n")
	fmt.Printf("  json     Machine-readable JSON format\n\n")
	
	fmt.Printf("Notes:\n")
	fmt.Printf("  - entry_count and checksum are auto-calculated on save\n")
	fmt.Printf("  - Warnings shown for size/checksum field edits\n")
	fmt.Printf("  - Use --force to bypass validation warnings\n")
}

func showEntryHelp() {
	fmt.Printf("dcfhfix entry - View and edit index entries\n\n")
	fmt.Printf("Usage: dcfhfix [OPTIONS] <index-file> entry <subcommand> [args...]\n\n")
	
	fmt.Printf("Subcommands:\n")
	fmt.Printf("  show <path>...                 Show entries as JSON\n")
	fmt.Printf("  edit <field> <value> <path>... Edit field for multiple entries\n")
	fmt.Printf("  append <json>                  Add new entry from JSON\n")
	fmt.Printf("  remove <path>...               Remove entries by path\n")
	fmt.Printf("  resort                         Resort all entries by path\n\n")
	
	fmt.Printf("Options:\n")
	fmt.Printf("  All global options apply (--dry-run, --backup, etc.)\n\n")
	
	fmt.Printf("Examples:\n")
	fmt.Printf("  # Show entries\n")
	fmt.Printf("  dcfhfix .dcfh/main.idx entry show src/main.go\n")
	fmt.Printf("  dcfhfix .dcfh/main.idx entry show src/main.go --format=json\n")
	fmt.Printf("  dcfhfix .dcfh/main.idx entry show 'src/*.go'\n\n")
	
	fmt.Printf("  # Edit entry fields\n")
	fmt.Printf("  dcfhfix .dcfh/main.idx entry edit uid 1000 src/app.go config.json\n")
	fmt.Printf("  dcfhfix .dcfh/main.idx entry edit mode 0644 '*.txt'\n")
	fmt.Printf("  dcfhfix .dcfh/main.idx entry edit hash abc123def456 src/file.c\n\n")
	
	fmt.Printf("  # Manage entries\n")
	fmt.Printf("  dcfhfix .dcfh/main.idx entry remove temp.txt old/\n")
	fmt.Printf("  dcfhfix .dcfh/main.idx entry resort\n\n")
	
	fmt.Printf("Entry Fields:\n")
	fmt.Printf("  ctime, mtime    Timestamps (Unix nanoseconds or ISO8601 string)\n")
	fmt.Printf("  dev, ino        Device/inode numbers (integer)\n")
	fmt.Printf("  mode            File permissions (octal like 0644 or integer)\n")
	fmt.Printf("  uid, gid        User/group IDs (integer)\n")
	fmt.Printf("  size            File size in bytes (integer)\n")
	fmt.Printf("  flags           Entry flags (hex or integer)\n")
	fmt.Printf("  hashtype        Hash algorithm (1=SHA1, 2=SHA256, 3=SHA512)\n")
	fmt.Printf("  hash            Hash value (hex string, no 0x prefix)\n")
	fmt.Printf("  json            JSON object for multiple fields\n\n")
	
	fmt.Printf("Output Formats:\n")
	fmt.Printf("  human    Human-readable table format (default)\n")
	fmt.Printf("  json     Machine-readable JSON format\n\n")
	
	fmt.Printf("Warnings:\n")
	fmt.Printf("  - Editing 'size' or 'hash' may hide file modifications\n")
	fmt.Printf("  - Path cannot be edited (use remove + append)\n")
	fmt.Printf("  - When editing hashtype, change type before hash value\n")
}

// Command handlers
func handleHeaderCommand(indexFile string, args []string, options *ParsedOptions) error {
	if len(args) < 1 {
		return fmt.Errorf("header command requires subcommand")
	}
	
	subcommand := args[0]
	switch subcommand {
	case "show":
		return headerShow(indexFile, options)
	case "edit":
		if len(args) < 3 {
			return fmt.Errorf("header edit requires field and value arguments")
		}
		return headerEdit(indexFile, args[1], args[2], options)
	default:
		return fmt.Errorf("unknown header subcommand: %s", subcommand)
	}
}

func handleEntryCommand(indexFile string, args []string, options *ParsedOptions) error {
	if len(args) < 1 {
		return fmt.Errorf("entry command requires subcommand")
	}
	
	subcommand := args[0]
	switch subcommand {
	case "show":
		if len(args) < 2 {
			return fmt.Errorf("entry show requires path arguments")
		}
		return entryShow(indexFile, args[1:], options)
	case "edit":
		if len(args) < 4 {
			return fmt.Errorf("entry edit requires field, value, and path arguments")
		}
		// For JSON editing, only need 3 args total (edit, json, value)
		if args[1] == "json" && len(args) >= 3 {
			return entryEditJSON(indexFile, args[2], args[3:], options)
		}
		return entryEdit(indexFile, args[1], args[2], args[3:], options)
	case "append":
		if len(args) < 2 {
			return fmt.Errorf("entry append requires JSON argument")
		}
		return entryAppend(indexFile, args[1], options)
	case "remove":
		if len(args) < 2 {
			return fmt.Errorf("entry remove requires path arguments")
		}
		return entryRemove(indexFile, args[1:], options)
	case "resort":
		return entryResort(indexFile, options)
	default:
		return fmt.Errorf("unknown entry subcommand: %s", subcommand)
	}
}

// Helper function to get format
func getFormat(options *ParsedOptions) string {
	return options.GetString("format")
}

// Stub implementations - will be implemented next
func headerShow(indexFile string, options *ParsedOptions) error {
	format := getFormat(options)
	if !options.GetBool("quiet") {
		if format == "json" {
			fmt.Printf("# JSON format header show not yet implemented\n")
		} else {
			fmt.Printf("# Human format header show not yet implemented\n")
		}
	}
	return fmt.Errorf("header show not yet implemented")
}

func headerEdit(indexFile string, field string, value string, options *ParsedOptions) error {
	if field == "json" {
		return headerEditJSON(indexFile, value, options)
	}
	return fmt.Errorf("header edit field not yet implemented")
}

func headerEditJSON(indexFile string, jsonData string, options *ParsedOptions) error {
	return fmt.Errorf("header edit JSON not yet implemented")
}

func entryShow(indexFile string, paths []string, options *ParsedOptions) error {
	format := getFormat(options)
	if !options.GetBool("quiet") {
		if format == "json" {
			fmt.Printf("# JSON format entry show not yet implemented for %d paths\n", len(paths))
		} else {
			fmt.Printf("# Human format entry show not yet implemented for %d paths\n", len(paths))
		}
	}
	return fmt.Errorf("entry show not yet implemented")
}

func entryEdit(indexFile string, field string, value string, paths []string, options *ParsedOptions) error {
	return fmt.Errorf("entry edit field not yet implemented")
}

func entryEditJSON(indexFile string, jsonData string, paths []string, options *ParsedOptions) error {
	return fmt.Errorf("entry edit JSON not yet implemented")
}

func entryAppend(indexFile string, jsonData string, options *ParsedOptions) error {
	return fmt.Errorf("entry append not yet implemented")
}

func entryRemove(indexFile string, paths []string, options *ParsedOptions) error {
	return fmt.Errorf("entry remove not yet implemented")
}

func entryResort(indexFile string, options *ParsedOptions) error {
	return fmt.Errorf("entry resort not yet implemented")
}

