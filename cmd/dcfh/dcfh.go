//go:generate go run generate_version.go

package main

import (
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime"
	"runtime/pprof"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

func main() {
	// Start pprof server if DCFH_PPROF environment variable is set
	if os.Getenv("DCFH_PPROF") != "" {
		go func() {
			log.Println("Starting pprof server on :6060")
			log.Println(http.ListenAndServe("localhost:6060", nil))
		}()
	}

	// CPU profiling: DCFH_CPUPROFILE=path writes a CPU profile
	if cpuprofile := os.Getenv("DCFH_CPUPROFILE"); cpuprofile != "" {
		f, err := os.Create(cpuprofile)
		if err != nil {
			log.Fatalf("Failed to create CPU profile: %v", err)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatalf("Failed to start CPU profile: %v", err)
		}
		defer func() {
			pprof.StopCPUProfile()
			_ = f.Close()
		}()
	}

	// Memory profiling: DCFH_MEMPROFILE=path writes a heap profile on exit
	if memprofile := os.Getenv("DCFH_MEMPROFILE"); memprofile != "" {
		defer func() {
			runtime.GC() // get accurate heap profile
			f, err := os.Create(memprofile)
			if err != nil {
				log.Fatalf("Failed to create memory profile: %v", err)
			}
			if err := pprof.WriteHeapProfile(f); err != nil {
				log.Fatalf("Failed to write memory profile: %v", err)
			}
			_ = f.Close()
		}()
	}

	// Set up signal handling for graceful shutdown
	shutdownChan := setupSignalHandler()

	// Initialise options definitions
	initialiseOptions()

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

	// Initialise debug flags early
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
		handleStatus(args[1:], shutdownChan)
	case "update":
		handleUpdate(args[1:], shutdownChan)
	case "dupes":
		handleDupes(args[1:], shutdownChan)
	case "snapshot":
		handleSnapshot(args[1:])
	case "subrepo":
		handleSubrepo(args[1:])
	case "config":
		handleConfig(args[1:])
	case "version":
		handleVersionCommand(args[1:])
	default:
		outputError(fmt.Sprintf("Unknown command: %s", command))
		os.Exit(1)
	}
}
