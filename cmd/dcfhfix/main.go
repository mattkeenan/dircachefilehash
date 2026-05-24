//go:generate go run generate_version.go

package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	dircachefilehash "github.com/mattkeenan/dircachefilehash/pkg"
)

// indexHeader represents the index file header structure
type indexHeader struct {
	Signature    [4]byte  // "dcfh" signature
	ByteOrder    uint64   // Byte order detection magic (0x0102030405060708)
	Version      uint32   // Index version (host order)
	EntryCount   uint32   // Number of entries (host order)
	Flags        uint16   // Index flags (host order)
	ChecksumType uint16   // Checksum algorithm type
	Checksum     [64]byte // Checksum of header+entries
}

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

	// Discover repository and resolve index file
	indexFile, err := dircachefilehash.ResolveIndexFile(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "dcfhfix: %v\n", err)
		os.Exit(1)
	}

	if err := dispatchCommand(args[1], indexFile, args[2:], options); err != nil {
		fmt.Fprintf(os.Stderr, "dcfhfix: %v\n", err)
		os.Exit(1)
	}
}

// commandTable maps each top-level subcommand to its sub-argument
// usage string and handler. Keeping them in one place means main()
// is a straight-line dispatch.
var commandTable = map[string]struct {
	subUsage string
	run      func(indexFile string, args []string, options *ParsedOptions) error
}{
	"header": {"dcfhfix <index-file> header <show|edit> [args...]", handleHeaderCommand},
	"entry":  {"dcfhfix <index-file> entry <show|edit|append|remove|resort> [args...]", handleEntryCommand},
	"fixes":  {"dcfhfix <index-file> fixes <list|pop|discard|clear> [args...]", handleFixesCommand},
}

func dispatchCommand(command, indexFile string, subArgs []string, options *ParsedOptions) error {
	h, ok := commandTable[command]
	if !ok {
		return fmt.Errorf("unknown command %q; try 'dcfhfix --help'", command)
	}
	if len(subArgs) < 1 {
		return fmt.Errorf("%s command requires subcommand\nUsage: %s", command, h.subUsage)
	}
	return h.run(indexFile, subArgs, options)
}

func showHelp() {
	fmt.Printf("dcfhfix - repair and edit tool for dcfh index files\n\n")
	fmt.Printf("Usage: dcfhfix [OPTIONS] <index> <command> <subcommand> [args...]\n\n")

	fmt.Printf("Commands:\n")
	fmt.Printf("  header show                    Show index header as JSON\n")
	fmt.Printf("  header edit <field> <value>    Edit header field\n")
	fmt.Printf("  entry show <path>...           Show entries as JSON\n")
	fmt.Printf("  entry edit <field> <value> <path>...  Edit entry field\n")
	fmt.Printf("  entry append <json>            Append new entry from JSON\n")
	fmt.Printf("  entry remove <path>...         Remove entries by path\n")
	fmt.Printf("  entry resort                   Resort all entries by path\n")
	fmt.Printf("  fixes list                     List backup stack\n")
	fmt.Printf("  fixes pop                      Restore latest backup and remove from stack\n")
	fmt.Printf("  fixes discard                  Remove latest backup from stack without restoring\n")
	fmt.Printf("  fixes clear                    Clear all backups from stack\n")
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

	fmt.Printf("Index Types:\n")
	fmt.Printf("  main               Main index (.dcfh/main.idx)\n")
	fmt.Printf("  cache              Cache index (.dcfh/cache.idx)\n")
	fmt.Printf("  scan               All scan indices (.dcfh/scan-*.idx)\n")
	fmt.Printf("  scan-PID-TID       Specific scan index\n")
	fmt.Printf("  /path/to/file.idx  Direct file path\n\n")

	fmt.Printf("Examples:\n")
	fmt.Printf("  # Show header information\n")
	fmt.Printf("  dcfhfix main header show\n")
	fmt.Printf("  dcfhfix main header show --format=json\n\n")

	fmt.Printf("  # Edit header fields\n")
	fmt.Printf("  dcfhfix main header edit version 2\n")
	fmt.Printf("  dcfhfix main header edit flags 0\n")
	fmt.Printf("  dcfhfix main header edit json '{\"version\":2,\"flags\":0}'\n\n")

	fmt.Printf("  # Show specific entries\n")
	fmt.Printf("  dcfhfix main entry show src/main.go README.md\n")
	fmt.Printf("  dcfhfix main entry show src/main.go --format=json\n\n")

	fmt.Printf("  # Edit entry fields\n")
	fmt.Printf("  dcfhfix main entry edit uid 1000 src/app.go\n")
	fmt.Printf("  dcfhfix main entry edit mode 0644 file1.txt file2.txt\n")
	fmt.Printf("  dcfhfix main entry edit json '{\"uid\":1000,\"gid\":1000}' src/app.go\n\n")

	fmt.Printf("  # Remove entries\n")
	fmt.Printf("  dcfhfix main entry remove old-file.txt temp/\n\n")

	fmt.Printf("  # Resort index\n")
	fmt.Printf("  dcfhfix main entry resort\n\n")

	fmt.Printf("  # Manage fix backups\n")
	fmt.Printf("  dcfhfix main fixes list\n")
	fmt.Printf("  dcfhfix main fixes pop\n")
	fmt.Printf("  dcfhfix main fixes clear\n\n")

	fmt.Printf("Safety Features:\n")
	fmt.Printf("  - Creates FIFO backup stack by default (disable with --backup=false)\n")
	fmt.Printf("  - Easy rollback with 'fixes pop' command\n")
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
	case "fixes":
		showFixesHelp()
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
	fmt.Printf("  edit <field> <value> Edit individual header field\n")
	fmt.Printf("  edit json <json>     Edit header using JSON data\n\n")

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
	fmt.Printf("  edit json <json> <path>...     Edit entries using JSON data\n")
	fmt.Printf("  append <json>                  Add new entry from JSON\n")
	fmt.Printf("  remove <path>...               Remove entries by path\n\n")

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
	fmt.Printf("  dcfhfix .dcfh/main.idx entry edit hash abc123def456 src/file.c\n")
	fmt.Printf("  dcfhfix .dcfh/main.idx entry edit json '{\"uid\":1000,\"mode\":0644}' src/app.go\n\n")

	fmt.Printf("  # Manage entries\n")
	fmt.Printf("  dcfhfix .dcfh/main.idx entry remove temp.txt old/\n\n")

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

func showFixesHelp() {
	fmt.Printf("dcfhfix fixes - Manage backup stack for easy rollbacks\n\n")
	fmt.Printf("Usage: dcfhfix [OPTIONS] <index-file> fixes <subcommand> [args...]\n\n")

	fmt.Printf("Subcommands:\n")
	fmt.Printf("  list                List all backups in stack (newest first)\n")
	fmt.Printf("  pop                 Restore latest backup and remove from stack\n")
	fmt.Printf("  discard             Remove latest backup from stack without restoring\n")
	fmt.Printf("  clear               Remove all backups from stack\n\n")

	fmt.Printf("Options:\n")
	fmt.Printf("  All global options apply (--dry-run, --verbose, etc.)\n\n")

	fmt.Printf("Examples:\n")
	fmt.Printf("  # List current backups\n")
	fmt.Printf("  dcfhfix .dcfh/main.idx fixes list\n")
	fmt.Printf("  dcfhfix .dcfh/main.idx fixes list --format=json\n\n")

	fmt.Printf("  # Rollback last change\n")
	fmt.Printf("  dcfhfix .dcfh/main.idx fixes pop\n")
	fmt.Printf("  dcfhfix .dcfh/main.idx fixes pop --dry-run\n\n")

	fmt.Printf("  # Remove backup without restoring\n")
	fmt.Printf("  dcfhfix .dcfh/main.idx fixes discard\n\n")

	fmt.Printf("  # Clear all backups\n")
	fmt.Printf("  dcfhfix .dcfh/main.idx fixes clear\n\n")

	fmt.Printf("Backup Stack:\n")
	fmt.Printf("  - FIFO (First In, First Out) stack behaviour\n")
	fmt.Printf("  - Latest backup is always at top of stack\n")
	fmt.Printf("  - Backups stored in .dcfh/fixes/<index-type>/ directories\n")
	fmt.Printf("  - Each backup includes timestamp and operation metadata\n")
	fmt.Printf("  - Stack automatically managed during edit operations\n\n")

	fmt.Printf("Output Formats:\n")
	fmt.Printf("  human    Human-readable table format (default)\n")
	fmt.Printf("  json     Machine-readable JSON format\n\n")

	fmt.Printf("Notes:\n")
	fmt.Printf("  - Backups are index-type specific (main.idx, cache.idx, etc.)\n")
	fmt.Printf("  - Each edit operation creates one backup before changes\n")
	fmt.Printf("  - Use --backup=false to disable backup creation\n")
	fmt.Printf("  - Stack persists between dcfhfix sessions\n")
}

// Backup metadata structure
type BackupMetadata struct {
	Timestamp   time.Time `json:"timestamp"`
	Operation   string    `json:"operation"`
	Description string    `json:"description"`
	IndexFile   string    `json:"index_file"`
	BackupFile  string    `json:"backup_file"`
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
		// For JSON editing, only need 3 args total (edit, json, value)
		if args[1] == "json" && len(args) >= 3 {
			return headerEditJSON(indexFile, args[2], options)
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
	default:
		return fmt.Errorf("unknown entry subcommand: %s", subcommand)
	}
}

func handleFixesCommand(indexFile string, args []string, options *ParsedOptions) error {
	if len(args) < 1 {
		return fmt.Errorf("fixes command requires subcommand")
	}

	subcommand := args[0]
	switch subcommand {
	case "list":
		return fixesList(indexFile, options)
	case "pop":
		return fixesPop(indexFile, options)
	case "discard":
		return fixesDiscard(indexFile, options)
	case "clear":
		return fixesClear(indexFile, options)
	default:
		return fmt.Errorf("unknown fixes subcommand: %s", subcommand)
	}
}

// Helper function to get format
func getFormat(options *ParsedOptions) string {
	return options.GetString("format")
}

// Simple index file opener for dcfhfix (reads header and provides entry access)
type indexFileAccess struct {
	file   *os.File
	data   []byte
	header *indexHeader
}

func (ifa *indexFileAccess) Close() error {
	if ifa.data != nil {
		if err := syscall.Munmap(ifa.data); err != nil {
			return fmt.Errorf("failed to unmap index file: %v", err)
		}
	}
	if ifa.file != nil {
		return ifa.file.Close()
	}
	return nil
}

func openIndexFile(filePath string) (*indexFileAccess, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open index file: %v", err)
	}

	stat, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("failed to stat file: %v", err)
	}

	if stat.Size() < int64(dircachefilehash.V2HeaderSize) {
		_ = file.Close()
		return nil, fmt.Errorf("file too small: %d bytes", stat.Size())
	}

	// Memory map the file
	data, err := syscall.Mmap(int(file.Fd()), 0, int(stat.Size()), syscall.PROT_READ, syscall.MAP_PRIVATE)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("failed to mmap file: %v", err)
	}

	// Get header pointer
	header := (*indexHeader)(unsafe.Pointer(&data[0]))

	// Basic validation
	if string(header.Signature[:]) != "dcfh" {
		_ = syscall.Munmap(data)
		_ = file.Close()
		return nil, fmt.Errorf("invalid signature: %s", string(header.Signature[:]))
	}

	return &indexFileAccess{
		file:   file,
		data:   data,
		header: header,
	}, nil
}

// Header implementations
func headerShow(indexFile string, options *ParsedOptions) error {
	// Open the index file
	indexAccess, err := openIndexFile(indexFile)
	if err != nil {
		return err
	}
	defer func() { _ = indexAccess.Close() }()

	header := indexAccess.header

	format := getFormat(options)
	if format == "json" {
		// JSON output
		headerData := map[string]any{
			"signature":     string(header.Signature[:]),
			"byte_order":    fmt.Sprintf("0x%016x", header.ByteOrder),
			"version":       header.Version,
			"entry_count":   header.EntryCount,
			"flags":         fmt.Sprintf("0x%08x", header.Flags),
			"checksum_type": header.ChecksumType,
			"checksum":      fmt.Sprintf("%x", header.Checksum[:]),
		}

		data, err := json.MarshalIndent(headerData, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal header JSON: %v", err)
		}
		fmt.Printf("%s\n", data)
	} else {
		// Human-readable output
		fmt.Printf("Index Header Information:\n")
		fmt.Printf("  Signature:     %s\n", string(header.Signature[:]))
		fmt.Printf("  Byte Order:    0x%016x\n", header.ByteOrder)
		fmt.Printf("  Version:       %d\n", header.Version)
		fmt.Printf("  Entry Count:   %d\n", header.EntryCount)
		fmt.Printf("  Flags:         0x%08x\n", header.Flags)
		fmt.Printf("  Checksum Type: %d\n", header.ChecksumType)
		fmt.Printf("  Checksum:      %x\n", header.Checksum[:])
	}

	return nil
}

// headerFieldEditor is the per-field implementation of header edit.
// validate runs first (may reject with a formatted error); apply
// mutates the copied header in place. A nil apply means the field is
// validate-only (i.e., always rejected).
type headerFieldEditor struct {
	validate func(value string) error
	apply    func(h *indexHeader, value string)
}

var headerFieldEditors = map[string]headerFieldEditor{
	"signature": {
		validate: func(v string) error {
			if len(v) != 4 {
				return fmt.Errorf("signature must be exactly 4 characters, got %d", len(v))
			}
			return nil
		},
		apply: func(h *indexHeader, v string) { copy(h.Signature[:], v) },
	},
	"version": {
		validate: func(v string) error {
			if _, err := parseUint32(v); err != nil {
				return fmt.Errorf("invalid version value: %v", err)
			}
			return nil
		},
		apply: func(h *indexHeader, v string) { val, _ := parseUint32(v); h.Version = val },
	},
	"flags": {
		validate: func(v string) error {
			if _, err := parseUint16(v); err != nil {
				return fmt.Errorf("invalid flags value: %v", err)
			}
			return nil
		},
		apply: func(h *indexHeader, v string) { val, _ := parseUint16(v); h.Flags = val },
	},
	"checksum_type": {
		validate: func(v string) error {
			if _, err := parseUint16(v); err != nil {
				return fmt.Errorf("invalid checksum_type value: %v", err)
			}
			return nil
		},
		apply: func(h *indexHeader, v string) { val, _ := parseUint16(v); h.ChecksumType = val },
	},
	"entry_count": {validate: func(string) error {
		return fmt.Errorf("entry_count is auto-calculated and cannot be manually edited")
	}},
	"checksum": {validate: func(string) error {
		return fmt.Errorf("checksum is auto-calculated and cannot be manually edited")
	}},
	"byte_order": {validate: func(string) error {
		return fmt.Errorf("byte_order is fixed and cannot be edited")
	}},
}

func headerEdit(indexFile string, field string, value string, options *ParsedOptions) error {
	if field == "json" {
		return headerEditJSON(indexFile, value, options)
	}

	editor, ok := headerFieldEditors[field]
	if !ok {
		return fmt.Errorf("unknown header field: %s", field)
	}
	if err := editor.validate(value); err != nil {
		return err
	}
	if editor.apply == nil {
		// Validate-only fields rejected above.
		return fmt.Errorf("field %q is not editable", field)
	}

	if !options.GetBool("dry-run") {
		description := fmt.Sprintf("Edit header.%s = %s", field, value)
		if err := createBackup(indexFile, "header-edit", description, options); err != nil {
			return fmt.Errorf("failed to create backup: %v", err)
		}
	}
	if options.GetBool("dry-run") {
		fmt.Printf("Would edit header field '%s' to value '%s'\n", field, value)
		return nil
	}

	entryData, err := loadIndexIntoSkiplist(indexFile)
	if err != nil {
		return fmt.Errorf("failed to load index: %v", err)
	}
	currentHeader, err := getIndexHeader(indexFile)
	if err != nil {
		return fmt.Errorf("failed to read current header: %v", err)
	}

	newHeaderData := *currentHeader
	editor.apply(&newHeaderData, value)

	if err := writeIndexWithModifiedHeader(entryData, indexFile, &newHeaderData, options); err != nil {
		return fmt.Errorf("failed to write modified index: %v", err)
	}
	if !options.GetBool("quiet") {
		fmt.Printf("Updated header field '%s' to '%s'\n", field, value)
	}
	return nil
}

func headerEditJSON(indexFile string, jsonData string, options *ParsedOptions) error {
	// Create backup before editing
	description := fmt.Sprintf("Edit header with JSON: %.50s...", jsonData)
	if len(jsonData) <= 50 {
		description = fmt.Sprintf("Edit header with JSON: %s", jsonData)
	}

	if !options.GetBool("dry-run") {
		err := createBackup(indexFile, "header-edit-json", description, options)
		if err != nil {
			return fmt.Errorf("failed to create backup: %v", err)
		}
	}

	return fmt.Errorf("header edit JSON not yet implemented")
}

func entryShow(indexFile string, paths []string, options *ParsedOptions) error {
	if len(paths) == 0 {
		return fmt.Errorf("no paths specified")
	}

	matchingEntries, notFoundPaths, err := dircachefilehash.FindEntries(indexFile, paths)
	if err != nil {
		return fmt.Errorf("failed to read index file: %v", err)
	}

	format := getFormat(options)
	if format == "json" {
		return displayEntriesJSON(matchingEntries, notFoundPaths, options)
	} else {
		return displayEntriesHuman(matchingEntries, notFoundPaths, options)
	}
}

// entryFieldValidators covers the fields entryEdit may be asked to
// write. Fields that are non-editable return an error from validate;
// entryEdit never reaches the underlying store for them.
var entryFieldValidators = map[string]func(value string) error{
	"ctime":           func(v string) error { _, err := parseTimeValue(v); return errWrap("ctime", err) },
	"mtime":           func(v string) error { _, err := parseTimeValue(v); return errWrap("mtime", err) },
	"dev":             func(v string) error { _, err := parseUint32(v); return errWrap("dev", err) },
	"ino":             func(v string) error { _, err := parseUint32(v); return errWrap("ino", err) },
	"uid":             func(v string) error { _, err := parseUint32(v); return errWrap("uid", err) },
	"gid":             func(v string) error { _, err := parseUint32(v); return errWrap("gid", err) },
	"mode":            func(v string) error { _, err := parseUint32(v); return errWrap("mode", err) },
	"file_size":       func(v string) error { _, err := parseUint64(v); return errWrap("file_size", err) },
	"hash_type":       func(v string) error { _, err := parseUint16(v); return errWrap("hash_type", err) },
	"hash":            func(v string) error { _, err := parseHashValue(v); return errWrap("hash", err) },
	"flag_is_deleted": func(v string) error { _, err := parseBoolValue(v); return errWrap("flag_is_deleted", err) },
	"path":            func(string) error { return fmt.Errorf("path cannot be edited (would change entry identity)") },
	"size":            func(string) error { return fmt.Errorf("size is auto-calculated and cannot be manually edited") },
}

// errWrap annotates the common "invalid X value: ..." pattern with
// the field name so per-field validators stay single-expression.
func errWrap(field string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("invalid %s value: %v", field, err)
}

func entryEdit(indexFile string, field string, value string, paths []string, options *ParsedOptions) error {
	if field == "json" {
		return entryEditJSON(indexFile, value, paths, options)
	}
	if len(paths) == 0 {
		return fmt.Errorf("no paths specified")
	}

	validate, ok := entryFieldValidators[field]
	if !ok {
		return fmt.Errorf("unknown entry field: %s", field)
	}
	if err := validate(value); err != nil {
		return err
	}

	pathsDesc := fmt.Sprintf("%d paths", len(paths))
	if len(paths) <= 3 {
		pathsDesc = strings.Join(paths, ", ")
	}
	if !options.GetBool("dry-run") {
		description := fmt.Sprintf("Edit entry.%s = %s for %s", field, value, pathsDesc)
		if err := createBackup(indexFile, "entry-edit", description, options); err != nil {
			return fmt.Errorf("failed to create backup: %v", err)
		}
	}
	if options.GetBool("dry-run") {
		fmt.Printf("Would edit entry field '%s' to value '%s' for paths: %s\n", field, value, pathsDesc)
		return nil
	}

	pathSet := make(map[string]bool, len(paths))
	for _, path := range paths {
		normalizedPath := filepath.Clean(path)
		if normalizedPath == "." {
			normalizedPath = ""
		}
		pathSet[normalizedPath] = true
	}

	// Process entries using safe workflow approach (never edit files directly)
	entriesFixed, entriesDiscarded, err := processEntriesWithWorkflow(indexFile, pathSet, field, value, options)
	if err != nil {
		return fmt.Errorf("failed to process entries: %v", err)
	}

	if entriesFixed == 0 {
		return fmt.Errorf("no matching entries found for specified paths")
	}

	if !options.GetBool("quiet") {
		fmt.Printf("Updated field '%s' to '%s' for %d matching entries", field, value, entriesFixed)
		if entriesDiscarded > 0 {
			fmt.Printf(" (%d corrupted entries discarded)", entriesDiscarded)
		}
		fmt.Println()
	}

	return nil
}

func entryEditJSON(indexFile string, jsonData string, paths []string, options *ParsedOptions) error {
	// Create backup before editing
	pathsDesc := fmt.Sprintf("%d paths", len(paths))
	if len(paths) <= 3 {
		pathsDesc = strings.Join(paths, ", ")
	}
	jsonDesc := fmt.Sprintf("%.30s...", jsonData)
	if len(jsonData) <= 30 {
		jsonDesc = jsonData
	}
	description := fmt.Sprintf("Edit entries with JSON %s for %s", jsonDesc, pathsDesc)

	if !options.GetBool("dry-run") {
		err := createBackup(indexFile, "entry-edit-json", description, options)
		if err != nil {
			return fmt.Errorf("failed to create backup: %v", err)
		}
	}

	return fmt.Errorf("entry edit JSON not yet implemented")
}

func entryAppend(indexFile string, jsonData string, options *ParsedOptions) error {
	// Create backup before appending
	jsonDesc := fmt.Sprintf("%.40s...", jsonData)
	if len(jsonData) <= 40 {
		jsonDesc = jsonData
	}
	description := fmt.Sprintf("Append entry: %s", jsonDesc)

	if !options.GetBool("dry-run") {
		err := createBackup(indexFile, "entry-append", description, options)
		if err != nil {
			return fmt.Errorf("failed to create backup: %v", err)
		}
	}

	if options.GetBool("dry-run") {
		fmt.Printf("Would append entry from JSON: %s\n", jsonDesc)
		return nil
	}

	// Parse and validate the JSON entry
	newEntry, err := parseEntryFromJSON(jsonData)
	if err != nil {
		return fmt.Errorf("failed to parse JSON entry: %v", err)
	}

	// Process entries using the workflow to append the new entry
	entriesAdded, entriesDiscarded, err := processEntriesWithAppend(indexFile, newEntry, options)
	if err != nil {
		return fmt.Errorf("failed to process entries: %v", err)
	}

	if !options.GetBool("quiet") {
		fmt.Printf("Added %d entry", entriesAdded)
		if entriesDiscarded > 0 {
			fmt.Printf(" (%d corrupted entries discarded)", entriesDiscarded)
		}
		fmt.Println()
	}

	return nil
}

func entryRemove(indexFile string, paths []string, options *ParsedOptions) error {
	// Create backup before removing
	pathsDesc := fmt.Sprintf("%d paths", len(paths))
	if len(paths) <= 5 {
		pathsDesc = strings.Join(paths, ", ")
	}
	description := fmt.Sprintf("Remove entries: %s", pathsDesc)

	if !options.GetBool("dry-run") {
		err := createBackup(indexFile, "entry-remove", description, options)
		if err != nil {
			return fmt.Errorf("failed to create backup: %v", err)
		}
	}

	if len(paths) == 0 {
		return fmt.Errorf("no paths specified")
	}

	if options.GetBool("dry-run") {
		fmt.Printf("Would remove entries for paths: %s\n", pathsDesc)
		return nil
	}

	// Convert paths to a map for quick lookup
	pathSet := make(map[string]bool)
	for _, path := range paths {
		normalizedPath := filepath.Clean(path)
		if normalizedPath == "." {
			normalizedPath = ""
		}
		pathSet[normalizedPath] = true
	}

	// Process entries using the workflow to remove matching paths
	entriesRemoved, entriesDiscarded, err := processEntriesWithRemoval(indexFile, pathSet, options)
	if err != nil {
		return fmt.Errorf("failed to process entries: %v", err)
	}

	if entriesRemoved == 0 {
		return fmt.Errorf("no matching entries found for specified paths")
	}

	if !options.GetBool("quiet") {
		fmt.Printf("Removed %d entries", entriesRemoved)
		if entriesDiscarded > 0 {
			fmt.Printf(" (%d corrupted entries discarded)", entriesDiscarded)
		}
		fmt.Println()
	}

	return nil
}

// Backup management functions

// getIndexType extracts the index type from the file path (e.g., "main" from "main.idx")
func getIndexType(indexFile string) string {
	base := filepath.Base(indexFile)
	if before, ok := strings.CutSuffix(base, ".idx"); ok {
		return before
	}
	return "unknown"
}

// getBackupDir returns the backup directory for a specific index type
func getBackupDir(indexFile string) (string, error) {
	// Find .dcfh directory by walking up from index file
	dir := filepath.Dir(indexFile)
	for {
		dcfhDir := filepath.Join(dir, ".dcfh")
		if info, err := os.Stat(dcfhDir); err == nil && info.IsDir() {
			indexType := getIndexType(indexFile)
			backupDir := filepath.Join(dcfhDir, "fixes", indexType)
			return backupDir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached root
		}
		dir = parent
	}

	return "", fmt.Errorf("could not find .dcfh directory")
}

// createBackup creates a backup of the index file and returns the backup metadata
func createBackup(indexFile string, operation string, description string, options *ParsedOptions) error {
	if !options.GetBool("backup") {
		return nil // backup disabled
	}

	backupDir, err := getBackupDir(indexFile)
	if err != nil {
		return fmt.Errorf("failed to find backup directory: %v", err)
	}

	// Create backup directory if it doesn't exist
	if err := os.MkdirAll(backupDir, 0755); err != nil { //nolint:gosec // G301: .dcfh/ backup dir, non-secret (index backups)
		return fmt.Errorf("failed to create backup directory: %v", err)
	}

	// Generate backup filename with timestamp
	timestamp := time.Now()
	backupFilename := fmt.Sprintf("%d-%s.idx", timestamp.Unix(), timestamp.Format("20060102T150405"))
	backupPath := filepath.Join(backupDir, backupFilename)

	// Copy the index file to backup location
	if err := copyFile(indexFile, backupPath); err != nil {
		return fmt.Errorf("failed to create backup: %v", err)
	}

	// Create metadata
	metadata := &BackupMetadata{
		Timestamp:   timestamp,
		Operation:   operation,
		Description: description,
		IndexFile:   indexFile,
		BackupFile:  backupPath,
	}

	// Save metadata
	metadataPath := strings.TrimSuffix(backupPath, ".idx") + ".json"
	if err := saveMetadata(metadata, metadataPath); err != nil {
		// Remove the backup file if metadata save fails
		_ = os.Remove(backupPath)
		return fmt.Errorf("failed to save backup metadata: %v", err)
	}

	if options.GetInt("verbose") > 0 && !options.GetBool("quiet") {
		fmt.Printf("Created backup: %s\n", backupFilename)
	}

	return nil
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = srcFile.Close() }()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = dstFile.Close() }()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// saveMetadata saves backup metadata to a JSON file
func saveMetadata(metadata *BackupMetadata, path string) error {
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644) //nolint:gosec // G306: .dcfh/ index file, non-secret (metadata + hashes)
}

// loadMetadata loads backup metadata from a JSON file
func loadMetadata(path string) (*BackupMetadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var metadata BackupMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, err
	}

	return &metadata, nil
}

// listBackups returns all backup metadata in chronological order (newest first)
func listBackups(indexFile string) ([]*BackupMetadata, error) {
	backupDir, err := getBackupDir(indexFile)
	if err != nil {
		return nil, err
	}

	// Check if backup directory exists
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		return []*BackupMetadata{}, nil // no backups
	}

	// Read all .json files in backup directory
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read backup directory: %v", err)
	}

	var backups []*BackupMetadata
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			metadataPath := filepath.Join(backupDir, entry.Name())
			metadata, err := loadMetadata(metadataPath)
			if err != nil {
				// Skip invalid metadata files
				continue
			}
			backups = append(backups, metadata)
		}
	}

	// Sort by timestamp, newest first
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].Timestamp.After(backups[j].Timestamp)
	})

	return backups, nil
}

// removeBackupFiles removes both the backup file and its metadata
func removeBackupFiles(metadata *BackupMetadata) error {
	// Remove backup file
	if err := os.Remove(metadata.BackupFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove backup file: %v", err)
	}

	// Remove metadata file
	metadataPath := strings.TrimSuffix(metadata.BackupFile, ".idx") + ".json"
	if err := os.Remove(metadataPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove metadata file: %v", err)
	}

	return nil
}

// Fixes command implementations

func fixesList(indexFile string, options *ParsedOptions) error {
	backups, err := listBackups(indexFile)
	if err != nil {
		return fmt.Errorf("failed to list backups: %v", err)
	}

	if len(backups) == 0 {
		if !options.GetBool("quiet") {
			fmt.Printf("No backups found for %s\n", getIndexType(indexFile))
		}
		return nil
	}

	format := getFormat(options)
	if format == "json" {
		data, err := json.MarshalIndent(backups, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %v", err)
		}
		fmt.Printf("%s\n", data)
	} else {
		// Human-readable format
		fmt.Printf("Backup stack for %s (%d entries):\n\n", getIndexType(indexFile), len(backups))
		fmt.Printf("%-20s %-15s %-30s\n", "Timestamp", "Operation", "Description")
		fmt.Printf("%-20s %-15s %-30s\n", strings.Repeat("-", 20), strings.Repeat("-", 15), strings.Repeat("-", 30))

		for i, backup := range backups {
			marker := " "
			if i == 0 {
				marker = "*" // mark the top of stack
			}
			fmt.Printf("%s%-19s %-15s %-30s\n",
				marker,
				backup.Timestamp.Format("2006-01-02 15:04:05"),
				backup.Operation,
				backup.Description)
		}
		fmt.Printf("\n* = top of stack (most recent)\n")
	}

	return nil
}

func fixesPop(indexFile string, options *ParsedOptions) error {
	backups, err := listBackups(indexFile)
	if err != nil {
		return fmt.Errorf("failed to list backups: %v", err)
	}

	if len(backups) == 0 {
		return fmt.Errorf("no backups available to restore")
	}

	latest := backups[0] // newest backup

	if options.GetBool("dry-run") {
		fmt.Printf("Would restore backup from %s (%s: %s)\n",
			latest.Timestamp.Format("2006-01-02 15:04:05"),
			latest.Operation,
			latest.Description)
		return nil
	}

	// Restore the backup
	if err := copyFile(latest.BackupFile, indexFile); err != nil {
		return fmt.Errorf("failed to restore backup: %v", err)
	}

	// Remove the backup files
	if err := removeBackupFiles(latest); err != nil {
		return fmt.Errorf("backup restored but failed to clean up backup files: %v", err)
	}

	if !options.GetBool("quiet") {
		fmt.Printf("Restored backup from %s (%s: %s)\n",
			latest.Timestamp.Format("2006-01-02 15:04:05"),
			latest.Operation,
			latest.Description)
	}

	return nil
}

func fixesDiscard(indexFile string, options *ParsedOptions) error {
	backups, err := listBackups(indexFile)
	if err != nil {
		return fmt.Errorf("failed to list backups: %v", err)
	}

	if len(backups) == 0 {
		return fmt.Errorf("no backups available to discard")
	}

	latest := backups[0] // newest backup

	if options.GetBool("dry-run") {
		fmt.Printf("Would discard backup from %s (%s: %s)\n",
			latest.Timestamp.Format("2006-01-02 15:04:05"),
			latest.Operation,
			latest.Description)
		return nil
	}

	// Remove the backup files
	if err := removeBackupFiles(latest); err != nil {
		return fmt.Errorf("failed to discard backup: %v", err)
	}

	if !options.GetBool("quiet") {
		fmt.Printf("Discarded backup from %s (%s: %s)\n",
			latest.Timestamp.Format("2006-01-02 15:04:05"),
			latest.Operation,
			latest.Description)
	}

	return nil
}

func fixesClear(indexFile string, options *ParsedOptions) error {
	backups, err := listBackups(indexFile)
	if err != nil {
		return fmt.Errorf("failed to list backups: %v", err)
	}

	if len(backups) == 0 {
		if !options.GetBool("quiet") {
			fmt.Printf("No backups to clear for %s\n", getIndexType(indexFile))
		}
		return nil
	}

	if options.GetBool("dry-run") {
		fmt.Printf("Would clear %d backup(s) for %s\n", len(backups), getIndexType(indexFile))
		return nil
	}

	// Remove all backup files
	for _, backup := range backups {
		if err := removeBackupFiles(backup); err != nil {
			return fmt.Errorf("failed to remove backup from %s: %v",
				backup.Timestamp.Format("2006-01-02 15:04:05"), err)
		}
	}

	// Remove backup directory if it's empty
	backupDir, _ := getBackupDir(indexFile)
	_ = os.Remove(backupDir) // ignore error if directory not empty or doesn't exist

	if !options.GetBool("quiet") {
		fmt.Printf("Cleared %d backup(s) for %s\n", len(backups), getIndexType(indexFile))
	}

	return nil
}

// Helper functions for parsing values

func parseUint16(value string) (uint16, error) {
	// Handle hex values (with or without 0x prefix)
	if strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0X") {
		val, err := strconv.ParseUint(value[2:], 16, 16)
		return uint16(val), err
	}
	// Handle octal values (with 0 prefix)
	if strings.HasPrefix(value, "0") && len(value) > 1 {
		val, err := strconv.ParseUint(value, 8, 16)
		return uint16(val), err
	}
	// Handle decimal values
	val, err := strconv.ParseUint(value, 10, 16)
	return uint16(val), err
}

func parseUint32(value string) (uint32, error) {
	// Handle hex values (with or without 0x prefix)
	if strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0X") {
		val, err := strconv.ParseUint(value[2:], 16, 32)
		return uint32(val), err
	}
	// Handle octal values (with 0 prefix)
	if strings.HasPrefix(value, "0") && len(value) > 1 {
		val, err := strconv.ParseUint(value, 8, 32)
		return uint32(val), err
	}
	// Handle decimal values
	val, err := strconv.ParseUint(value, 10, 32)
	return uint32(val), err
}

// parseUint64 parses a string value as uint64 with support for hex/octal/decimal
func parseUint64(value string) (uint64, error) {
	// Handle hex values (with or without 0x prefix)
	if strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0X") {
		val, err := strconv.ParseUint(value[2:], 16, 64)
		return val, err
	}
	// Handle octal values (with 0 prefix)
	if strings.HasPrefix(value, "0") && len(value) > 1 {
		val, err := strconv.ParseUint(value, 8, 64)
		return val, err
	}
	// Handle decimal values
	val, err := strconv.ParseUint(value, 10, 64)
	return val, err
}

// parseTimeValue parses time in various formats and returns wall time
func parseTimeValue(value string) (uint64, error) {
	// Try ISO 8601 format first
	if t, err := time.Parse("2006-01-02T15:04:05.000000000Z", value); err == nil {
		return dircachefilehash.TimeToWall(t), nil
	}
	if t, err := time.Parse("2006-01-02T15:04:05Z", value); err == nil {
		return dircachefilehash.TimeToWall(t), nil
	}
	// Try Unix timestamp
	if timestamp, err := strconv.ParseInt(value, 10, 64); err == nil {
		t := time.Unix(timestamp, 0)
		return dircachefilehash.TimeToWall(t), nil
	}
	return 0, fmt.Errorf("invalid time format, use ISO 8601 (2006-01-02T15:04:05Z) or Unix timestamp")
}

// parseHashValue parses and validates a hash string
func parseHashValue(value string) ([]byte, error) {
	// Remove any 0x prefix
	if strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0X") {
		value = value[2:]
	}

	// Decode hex string
	hash, err := hex.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("invalid hex string: %v", err)
	}

	// Validate hash length (must be 20, 32, or 64 bytes for SHA1, SHA256, SHA512)
	if len(hash) != 20 && len(hash) != 32 && len(hash) != 64 {
		return nil, fmt.Errorf("invalid hash length %d, must be 20 (SHA1), 32 (SHA256), or 64 (SHA512) bytes", len(hash))
	}

	return hash, nil
}

// parseBoolValue parses various boolean representations
func parseBoolValue(value string) (bool, error) {
	switch strings.ToLower(value) {
	case "true", "1", "yes", "on":
		return true, nil
	case "false", "0", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean value: %s (use true/false, 1/0, yes/no, on/off)", value)
	}
}

// displayEntriesJSON displays entries in JSON format
func displayEntriesJSON(entries []*dircachefilehash.EntryInfo, notFoundPaths []string, options *ParsedOptions) error {
	// Convert entries to JSON-friendly format with ISO 8601 timestamps
	jsonEntries := make([]map[string]any, len(entries))
	for i, entry := range entries {
		mtime := dircachefilehash.TimeFromWall(entry.MTimeWall)
		ctime := dircachefilehash.TimeFromWall(entry.CTimeWall)

		jsonEntries[i] = map[string]any{
			"path":            entry.Path,
			"flag_is_deleted": entry.IsDeleted,
			"file_size":       entry.FileSize,
			"mode":            entry.Mode,
			"uid":             entry.UID,
			"gid":             entry.GID,
			"dev":             entry.Dev,
			"mtime":           mtime.UTC().Format("2006-01-02T15:04:05.000000000Z"),
			"ctime":           ctime.UTC().Format("2006-01-02T15:04:05.000000000Z"),
			"hash":            entry.HashStr,
			"hash_type":       entry.HashType,
		}
	}

	output := map[string]any{
		"entries": jsonEntries,
	}

	if len(notFoundPaths) > 0 && !options.GetBool("quiet") {
		output["not_found"] = notFoundPaths
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %v", err)
	}

	fmt.Printf("%s\n", data)
	return nil
}

// displayEntriesHuman displays entries in human-readable format
func displayEntriesHuman(entries []*dircachefilehash.EntryInfo, notFoundPaths []string, options *ParsedOptions) error {
	if len(entries) == 0 {
		if !options.GetBool("quiet") {
			fmt.Printf("No entries found.\n")
		}
	} else {
		if !options.GetBool("quiet") {
			fmt.Printf("Found %d entries:\n\n", len(entries))
		}

		for _, entry := range entries {
			fmt.Printf("Path: %s\n", entry.Path)
			fmt.Printf("  Size: %d bytes\n", entry.FileSize)
			fmt.Printf("  Mode: %04o\n", entry.Mode&0o7777)
			fmt.Printf("  UID: %d\n", entry.UID)
			fmt.Printf("  GID: %d\n", entry.GID)
			fmt.Printf("  Dev: %d\n", entry.Dev)

			// Convert wall time to readable format
			mtime := dircachefilehash.TimeFromWall(entry.MTimeWall)
			ctime := dircachefilehash.TimeFromWall(entry.CTimeWall)
			fmt.Printf("  MTime: %s\n", mtime.Format("2006-01-02 15:04:05"))
			fmt.Printf("  CTime: %s\n", ctime.Format("2006-01-02 15:04:05"))

			fmt.Printf("  Hash Type: %d\n", entry.HashType)
			fmt.Printf("  Hash: %s\n", entry.HashStr)
			fmt.Printf("  Deleted: %t\n", entry.IsDeleted)
			fmt.Printf("\n")
		}
	}

	// Show not found paths
	if len(notFoundPaths) > 0 && !options.GetBool("quiet") {
		fmt.Printf("Paths not found in index:\n")
		for _, path := range notFoundPaths {
			fmt.Printf("  %s\n", path)
		}
		fmt.Printf("\n")
	}

	return nil
}

// Helper functions for proper dcfh pattern implementation

// loadIndexIntoSkiplist loads an index file and reads its data
func loadIndexIntoSkiplist(indexFile string) (*EntryData, error) {
	// Read the entire index file
	data, err := os.ReadFile(indexFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read index file: %w", err)
	}

	// Validate minimum size
	if len(data) < dircachefilehash.V2HeaderSize {
		return nil, fmt.Errorf("index file too small: %d bytes", len(data))
	}

	// Read header to get entry count
	header := (*indexHeader)(unsafe.Pointer(&data[0]))

	// Basic validation
	if string(header.Signature[:]) != "dcfh" {
		return nil, fmt.Errorf("invalid signature: %s", string(header.Signature[:]))
	}

	entryData := &EntryData{
		IndexFile:    indexFile,
		OriginalData: data,
		EntryCount:   header.EntryCount,
	}

	return entryData, nil
}

// getIndexHeader reads and returns the current header from an index file
func getIndexHeader(indexFile string) (*indexHeader, error) {
	// Open the index file
	indexAccess, err := openIndexFile(indexFile)
	if err != nil {
		return nil, err
	}
	defer func() { _ = indexAccess.Close() }()

	// Make a copy of the header to avoid returning a pointer to mmap'd memory
	headerCopy := *indexAccess.header
	return &headerCopy, nil
}

// writeIndexWithModifiedHeader writes an index with a modified header
func writeIndexWithModifiedHeader(entryData *EntryData, indexFile string, newHeader *indexHeader, _ *ParsedOptions) error {
	// Create temporary file path
	tempFile := indexFile + ".tmp"
	defer func() {
		// Clean up temp file if it still exists
		if _, err := os.Stat(tempFile); err == nil {
			_ = os.Remove(tempFile)
		}
	}()

	// Write the index with custom header
	if err := writeIndexWithCustomHeader(entryData, tempFile, newHeader); err != nil {
		return fmt.Errorf("failed to write index with custom header: %w", err)
	}

	// Atomic replace
	if err := os.Rename(tempFile, indexFile); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// EntryData holds the loaded index data
type EntryData struct {
	IndexFile    string
	OriginalData []byte
	EntryCount   uint32
}

// writeIndexWithCustomHeader writes an index with a custom header (simplified approach)
func writeIndexWithCustomHeader(entryData *EntryData, outputPath string, customHeader *indexHeader) error {
	// Set the entry count from the original data
	customHeader.EntryCount = entryData.EntryCount

	// Create output file
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer func() { _ = file.Close() }()

	// Write custom header
	headerBytes := (*[dircachefilehash.HeaderSize]byte)(unsafe.Pointer(customHeader))
	if _, err := file.Write(headerBytes[:]); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}

	// Write original entry data (skip original header using its version)
	origHeader := (*indexHeader)(unsafe.Pointer(&entryData.OriginalData[0]))
	origHdrSize := dircachefilehash.HeaderSizeForVersion(origHeader.Version)
	if len(entryData.OriginalData) > origHdrSize {
		entryBytes := entryData.OriginalData[origHdrSize:]
		if _, err := file.Write(entryBytes); err != nil {
			return fmt.Errorf("failed to write entries: %w", err)
		}
	}

	// Note: This simplified approach doesn't recalculate checksums
	// For a full implementation, we'd need to implement the vectorio approach

	return file.Sync()
}
