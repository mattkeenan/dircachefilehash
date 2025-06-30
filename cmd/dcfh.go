//go:generate go run generate_version.go

package main

import (
	"fmt"
	"os"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

func main() {
	// Initialize options definitions
	initializeOptions()

	// Special handling for snapshot forget commands to avoid flag conflicts
	// Look for "snapshot forget" in arguments, allowing for global flags before them
	for i := 0; i < len(os.Args)-1; i++ {
		if os.Args[i] == "snapshot" && i+1 < len(os.Args) && os.Args[i+1] == "forget" {
			// Extract global flags before "snapshot" and subcommand flags after "forget"
			globalArgs := os.Args[1:i]
			subcommandArgs := os.Args[i+2:]
			handleSnapshotForgetSpecial(globalArgs, subcommandArgs)
			return
		}
	}

	// Parse command-line arguments
	if err := options.Parse(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Handle help flag
	if options.GetBool("help") {
		showUsage()
		return
	}

	// Handle version flag
	if options.GetBool("version") {
		handleVersion()
		return
	}

	// Initialize debug flags early
	debugStr := options.GetString("debug")
	dcfh.InitDebugFlags(debugStr)
	if debugStr != "" {
		dcfh.LogDebugFlags()
	}

	// Validate output format early (after flag parsing)
	validateOutputFormat()

	// Handle special verbose option repetition (-vvv)
	handleVerboseRepetition()

	// Get remaining arguments after options
	args := options.GetArgs()
	if len(args) < 1 {
		showUsage()
		os.Exit(1)
	}

	command := args[0]

	switch command {
	case "init":
		handleInit(args[1:])
	case "status":
		handleStatus(args[1:])
	case "update":
		handleUpdate(args[1:])
	case "dupes":
		handleDupes(args[1:])
	case "index":
		handleIndex(args[1:])
	case "snapshot":
		handleSnapshot(args[1:])
	case "config":
		handleConfig(args[1:])
	case "version":
		handleVersionCommand(args[1:])
	default:
		outputError(fmt.Sprintf("Unknown command: %s", command))
		os.Exit(1)
	}
}