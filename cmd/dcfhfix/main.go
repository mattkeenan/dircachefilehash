//go:generate go run generate_version.go

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"

	dircachefilehash "github.com/mattkeenan/dircachefilehash/pkg"
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
	options.DefineOption("edit-in-place", "", OptionTypeBool, "false", "Overwrite the index in place without preserving a .pre-fix sibling (requires --force)")
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
	"entry":  {"dcfhfix <index-file> entry <show|edit|append|remove> [args...]", handleEntryCommand},
	"fixes":  {"dcfhfix <index-file> fixes <list|pop|discard|clear> [args...]", handleFixesCommand},
}

// fixFlags projects the CLI ParsedOptions onto the narrow FixEntryFlags shim
// consumed by the relocated pkg/ repair workflow and promote helpers.
func fixFlags(options *ParsedOptions) dircachefilehash.FixEntryFlags {
	return dircachefilehash.FixEntryFlags{
		Quiet:       options.GetBool("quiet"),
		EditInPlace: options.GetBool("edit-in-place"),
		Force:       options.GetBool("force"),
	}
}

// runFixWrite translates one CLI write subcommand into a single-command
// FixRequest and runs it through the shared RunFix core against the explicitly
// named subject (writeRoot "" — the explicit-subject exemption; the user named
// the file directly, so MetaDir confinement is not imposed). RunFix owns the
// pre-write backup and the single-writer index write.
func runFixWrite(indexFile string, cmd dircachefilehash.FixCommand, options *ParsedOptions) (*dircachefilehash.FixResult, error) {
	req := dircachefilehash.FixRequest{
		Commands: []dircachefilehash.FixCommand{cmd},
		Mode:     dircachefilehash.FixModeAuto,
		DryRun:   options.GetBool("dry-run"),
		Backup:   options.GetBool("backup"),
		Verbose:  options.GetInt("verbose"),
		Flags:    fixFlags(options),
	}
	refs := []dircachefilehash.IndexRef{{Path: indexFile, Type: dircachefilehash.RefTypeFile}}
	return dircachefilehash.RunFix(context.Background(), refs, req, "", os.Stderr)
}

func dispatchCommand(command, indexFile string, subArgs []string, options *ParsedOptions) error {
	// Gate the destructive in-place opt-in once, before routing. Because every
	// subcommand (including read-only ones) routes through here, a lone
	// --edit-in-place is refused everywhere — intentional: the flag is
	// meaningless on reads and one chokepoint is simpler than per-write-path checks.
	if err := dircachefilehash.ValidateEditInPlaceGate(fixFlags(options)); err != nil {
		return err
	}

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
	fmt.Printf("      --edit-in-place Overwrite the index in place, no .pre-fix sibling (requires --force)\n")
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

	fmt.Printf("  # Manage fix backups\n")
	fmt.Printf("  dcfhfix main fixes list\n")
	fmt.Printf("  dcfhfix main fixes pop\n")
	fmt.Printf("  dcfhfix main fixes clear\n\n")

	fmt.Printf("Safety Features:\n")
	fmt.Printf("  - Non-destructive by default: the pre-repair index is preserved at a\n")
	fmt.Printf("    visible '.pre-fix-<timestamp>' sibling before the repaired index replaces it\n")
	fmt.Printf("    (opt out with --force --edit-in-place)\n")
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
	file, err := os.Open(filePath) //nolint:gosec // G304: repair-tool path from a user-supplied CLI argument (the index named on the command line); no trust boundary
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
	data, err := syscall.Mmap(int(file.Fd()), 0, int(stat.Size()), syscall.PROT_READ, syscall.MAP_PRIVATE) //nolint:gosec // G115: file descriptor (uintptr) to int, bounded on 64-bit
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

// The header field editor model and its surgical writer now live in
// pkg/fix_header.go (task 28.2); see dircachefilehash.ValidateHeaderEdit /
// ApplyHeaderEdit, driven through RunFix.

func headerEdit(indexFile string, field string, value string, options *ParsedOptions) error {
	if field == "json" {
		return headerEditJSON(indexFile, value, options)
	}

	// Validate up front so an unknown/invalid field is rejected before any
	// dry-run preview or write (RunFix re-validates on the real write).
	if err := dircachefilehash.ValidateHeaderEdit(field, value); err != nil {
		return err
	}

	if options.GetBool("dry-run") {
		fmt.Printf("Would edit header field '%s' to value '%s'\n", field, value)
		dircachefilehash.ReportDryRunPreservation(indexFile, fixFlags(options))
		return nil
	}

	if _, err := runFixWrite(indexFile, dircachefilehash.FixCommand{
		Op: dircachefilehash.FixOpHeaderEdit, Field: field, Value: value,
	}, options); err != nil {
		return fmt.Errorf("failed to write modified index: %v", err)
	}
	if !options.GetBool("quiet") {
		fmt.Printf("Updated header field '%s' to '%s'\n", field, value)
	}
	return nil
}

func headerEditJSON(indexFile string, jsonData string, options *ParsedOptions) error {
	// The shared core takes the pre-write backup (unless --dry-run) and returns
	// the preserved "not yet implemented" stub error.
	_, err := runFixWrite(indexFile, dircachefilehash.FixCommand{
		Op: dircachefilehash.FixOpHeaderEdit, Field: "json", Value: jsonData,
	}, options)
	return err
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
	"ctime":     func(v string) error { _, err := dircachefilehash.ParseTimeValue(v); return errWrap("ctime", err) },
	"mtime":     func(v string) error { _, err := dircachefilehash.ParseTimeValue(v); return errWrap("mtime", err) },
	"dev":       func(v string) error { _, err := dircachefilehash.ParseUint32(v); return errWrap("dev", err) },
	"ino":       func(v string) error { _, err := dircachefilehash.ParseUint32(v); return errWrap("ino", err) },
	"uid":       func(v string) error { _, err := dircachefilehash.ParseUint32(v); return errWrap("uid", err) },
	"gid":       func(v string) error { _, err := dircachefilehash.ParseUint32(v); return errWrap("gid", err) },
	"mode":      func(v string) error { _, err := dircachefilehash.ParseUint32(v); return errWrap("mode", err) },
	"file_size": func(v string) error { _, err := dircachefilehash.ParseInt64(v); return errWrap("file_size", err) },
	"hash_type": func(v string) error { _, err := dircachefilehash.ParseUint16(v); return errWrap("hash_type", err) },
	"hash":      func(v string) error { _, err := dircachefilehash.ParseHashValue(v); return errWrap("hash", err) },
	"flag_is_deleted": func(v string) error {
		_, err := dircachefilehash.ParseBoolValue(v)
		return errWrap("flag_is_deleted", err)
	},
	"path": func(string) error { return fmt.Errorf("path cannot be edited (would change entry identity)") },
	"size": func(string) error { return fmt.Errorf("size is auto-calculated and cannot be manually edited") },
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

	if options.GetBool("dry-run") {
		pathsDesc := fmt.Sprintf("%d paths", len(paths))
		if len(paths) <= 3 {
			pathsDesc = strings.Join(paths, ", ")
		}
		fmt.Printf("Would edit entry field '%s' to value '%s' for paths: %s\n", field, value, pathsDesc)
		dircachefilehash.ReportDryRunPreservation(indexFile, fixFlags(options))
		return nil
	}

	res, err := runFixWrite(indexFile, dircachefilehash.FixCommand{
		Op: dircachefilehash.FixOpEntryEdit, Field: field, Value: value, Paths: paths,
	}, options)
	if err != nil {
		return fmt.Errorf("failed to process entries: %v", err)
	}

	if res.RepairsApplied == 0 {
		return fmt.Errorf("no matching entries found for specified paths")
	}

	if !options.GetBool("quiet") {
		fmt.Printf("Updated field '%s' to '%s' for %d matching entries", field, value, res.RepairsApplied)
		if res.EntriesDiscarded > 0 {
			fmt.Printf(" (%d corrupted entries discarded)", res.EntriesDiscarded)
		}
		fmt.Println()
	}

	return nil
}

func entryEditJSON(indexFile string, jsonData string, paths []string, options *ParsedOptions) error {
	// The shared core takes the pre-write backup (unless --dry-run) and returns
	// the preserved "not yet implemented" stub error.
	_, err := runFixWrite(indexFile, dircachefilehash.FixCommand{
		Op: dircachefilehash.FixOpEntryEdit, Field: "json", Value: jsonData, Paths: paths,
	}, options)
	return err
}

func entryAppend(indexFile string, jsonData string, options *ParsedOptions) error {
	if options.GetBool("dry-run") {
		jsonDesc := fmt.Sprintf("%.40s...", jsonData)
		if len(jsonData) <= 40 {
			jsonDesc = jsonData
		}
		fmt.Printf("Would append entry from JSON: %s\n", jsonDesc)
		dircachefilehash.ReportDryRunPreservation(indexFile, fixFlags(options))
		return nil
	}

	res, err := runFixWrite(indexFile, dircachefilehash.FixCommand{
		Op: dircachefilehash.FixOpEntryAppend, Value: jsonData,
	}, options)
	if err != nil {
		return err
	}

	if !options.GetBool("quiet") {
		fmt.Printf("Added %d entry", res.RepairsApplied)
		if res.EntriesDiscarded > 0 {
			fmt.Printf(" (%d corrupted entries discarded)", res.EntriesDiscarded)
		}
		fmt.Println()
	}

	return nil
}

func entryRemove(indexFile string, paths []string, options *ParsedOptions) error {
	if len(paths) == 0 {
		return fmt.Errorf("no paths specified")
	}

	if options.GetBool("dry-run") {
		pathsDesc := fmt.Sprintf("%d paths", len(paths))
		if len(paths) <= 5 {
			pathsDesc = strings.Join(paths, ", ")
		}
		fmt.Printf("Would remove entries for paths: %s\n", pathsDesc)
		dircachefilehash.ReportDryRunPreservation(indexFile, fixFlags(options))
		return nil
	}

	res, err := runFixWrite(indexFile, dircachefilehash.FixCommand{
		Op: dircachefilehash.FixOpEntryRemove, Paths: paths,
	}, options)
	if err != nil {
		return fmt.Errorf("failed to process entries: %v", err)
	}

	if res.RepairsApplied == 0 {
		return fmt.Errorf("no matching entries found for specified paths")
	}

	if !options.GetBool("quiet") {
		fmt.Printf("Removed %d entries", res.RepairsApplied)
		if res.EntriesDiscarded > 0 {
			fmt.Printf(" (%d corrupted entries discarded)", res.EntriesDiscarded)
		}
		fmt.Println()
	}

	return nil
}

// Backup management presenters
//
// The backup-stack logic now lives in pkg/fix_backup.go (task 28.2, FR3). The
// edit handlers no longer take backups themselves — RunFix owns the pre-write
// backup. The functions here are thin CLI presenters for the fixes subcommands:
// they own the stdout rendering (human/JSON tables, dry-run notices, quiet
// handling) and call the pkg stack cores.

// Fixes command implementations

func fixesList(indexFile string, options *ParsedOptions) error {
	backups, err := dircachefilehash.ListBackups(indexFile)
	if err != nil {
		return fmt.Errorf("failed to list backups: %v", err)
	}

	if len(backups) == 0 {
		if !options.GetBool("quiet") {
			fmt.Printf("No backups found for %s\n", dircachefilehash.BackupIndexType(indexFile))
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
		fmt.Printf("Backup stack for %s (%d entries):\n\n", dircachefilehash.BackupIndexType(indexFile), len(backups))
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
	if options.GetBool("dry-run") {
		backups, err := dircachefilehash.ListBackups(indexFile)
		if err != nil {
			return fmt.Errorf("failed to list backups: %v", err)
		}
		if len(backups) == 0 {
			return fmt.Errorf("no backups available to restore")
		}
		latest := backups[0]
		fmt.Printf("Would restore backup from %s (%s: %s)\n",
			latest.Timestamp.Format("2006-01-02 15:04:05"),
			latest.Operation,
			latest.Description)
		return nil
	}

	latest, err := dircachefilehash.PopBackup(indexFile)
	if err != nil {
		return err
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
	if options.GetBool("dry-run") {
		backups, err := dircachefilehash.ListBackups(indexFile)
		if err != nil {
			return fmt.Errorf("failed to list backups: %v", err)
		}
		if len(backups) == 0 {
			return fmt.Errorf("no backups available to discard")
		}
		latest := backups[0]
		fmt.Printf("Would discard backup from %s (%s: %s)\n",
			latest.Timestamp.Format("2006-01-02 15:04:05"),
			latest.Operation,
			latest.Description)
		return nil
	}

	latest, err := dircachefilehash.DiscardBackup(indexFile)
	if err != nil {
		return err
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
	backups, err := dircachefilehash.ListBackups(indexFile)
	if err != nil {
		return fmt.Errorf("failed to list backups: %v", err)
	}

	if len(backups) == 0 {
		if !options.GetBool("quiet") {
			fmt.Printf("No backups to clear for %s\n", dircachefilehash.BackupIndexType(indexFile))
		}
		return nil
	}

	if options.GetBool("dry-run") {
		fmt.Printf("Would clear %d backup(s) for %s\n", len(backups), dircachefilehash.BackupIndexType(indexFile))
		return nil
	}

	cleared, err := dircachefilehash.ClearBackups(indexFile)
	if err != nil {
		return err
	}

	if !options.GetBool("quiet") {
		fmt.Printf("Cleared %d backup(s) for %s\n", cleared, dircachefilehash.BackupIndexType(indexFile))
	}

	return nil
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
