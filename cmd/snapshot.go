package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

// handleSnapshot handles the snapshot command with various subcommands
func handleSnapshot(args []string) {
	if len(args) < 1 {
		showSnapshotUsage()
		os.Exit(1)
	}
	
	subcommand := args[0]
	
	switch subcommand {
	case "create":
		handleSnapshotCreate(args[1:])
	case "list", "ls":
		handleSnapshotList(args[1:])
	case "forget", "rm":
		handleSnapshotForget(args[1:])
	case "status":
		handleSnapshotStatus(args[1:])
	case "help", "-h", "--help":
		showSnapshotUsage()
	default:
		outputError(fmt.Sprintf("Unknown snapshot subcommand: %s", subcommand))
		showSnapshotUsage()
		os.Exit(1)
	}
}

// showSnapshotUsage displays usage information for the snapshot command
func showSnapshotUsage() {
	fmt.Fprintf(os.Stderr, "Usage: dcfh snapshot <subcommand> [options]\n\n")
	fmt.Fprintf(os.Stderr, "Snapshot management subcommands:\n")
	fmt.Fprintf(os.Stderr, "  create           Create a new snapshot of the current index state\n")
	fmt.Fprintf(os.Stderr, "  list, ls         List all available snapshots\n")
	fmt.Fprintf(os.Stderr, "  forget, rm       Remove snapshots based on retention policies\n")
	fmt.Fprintf(os.Stderr, "  status           Compare current state with snapshots\n")
	fmt.Fprintf(os.Stderr, "  help             Show this help message\n")
	fmt.Fprintf(os.Stderr, "\nExamples:\n")
	fmt.Fprintf(os.Stderr, "  dcfh snapshot create\n")
	fmt.Fprintf(os.Stderr, "  dcfh snapshot list\n")
	fmt.Fprintf(os.Stderr, "  dcfh snapshot forget --keep-daily=7\n")
	fmt.Fprintf(os.Stderr, "  dcfh snapshot status\n")
}

// Note: formatFileSize moved to common.go for shared use

// handleSnapshotCreate creates a new snapshot
func handleSnapshotCreate(args []string) {
	// Parse options
	var tags []string
	
	// Process arguments for tags
	for _, arg := range args {
		if strings.HasPrefix(arg, "--tag=") {
			tag := strings.TrimPrefix(arg, "--tag=")
			if tag != "" {
				tags = append(tags, tag)
			}
		} else if arg != "" {
			outputError(fmt.Sprintf("Unknown argument: %s", arg))
			os.Exit(1)
		}
	}
	
	// Find dcfh repository
	repoRoot, dcfhDir, err := findDcfhRepo()
	if err != nil {
		outputError(fmt.Sprintf("Failed to find dcfh repository: %v", err))
		os.Exit(1)
	}
	
	// Create snapshot repository
	sr := dcfh.NewSnapshotRepository(dcfhDir)
	
	// Create snapshot
	verbosity := options.GetInt("verbose")
	if verbosity >= 1 {
		fmt.Printf("Creating snapshot...\n")
	}
	
	metadata, err := sr.CreateSnapshot(repoRoot, tags)
	if err != nil {
		outputError(fmt.Sprintf("Failed to create snapshot: %v", err))
		os.Exit(1)
	}
	
	// Output result
	outputFormat := validateOutputFormat()
	if outputFormat == OutputJSON {
		jsonData, err := json.MarshalIndent(metadata, "", "  ")
		if err != nil {
			outputError(fmt.Sprintf("Failed to marshal JSON: %v", err))
			os.Exit(1)
		}
		fmt.Println(string(jsonData))
	} else {
		fmt.Printf("Snapshot created: %s\n", metadata.ID)
		fmt.Printf("Time: %s\n", metadata.Time.Format("2006-01-02 15:04:05 UTC"))
		fmt.Printf("Files: %d (%s)\n", metadata.Summary.FilesCount, formatFileSize(metadata.Summary.TotalSize))
		if len(metadata.Tags) > 0 {
			fmt.Printf("Tags: %s\n", strings.Join(metadata.Tags, ", "))
		}
		if verbosity >= 1 {
			fmt.Printf("Tree hash: %s\n", metadata.Tree)
		}
	}
}

// handleSnapshotList lists all snapshots
func handleSnapshotList(args []string) {
	// Find dcfh repository
	_, dcfhDir, err := findDcfhRepo()
	if err != nil {
		outputError(fmt.Sprintf("Failed to find dcfh repository: %v", err))
		os.Exit(1)
	}
	
	// Create snapshot repository
	sr := dcfh.NewSnapshotRepository(dcfhDir)
	
	// List snapshots
	snapshots, err := sr.ListSnapshots()
	if err != nil {
		outputError(fmt.Sprintf("Failed to list snapshots: %v", err))
		os.Exit(1)
	}
	
	// Output results
	outputFormat := validateOutputFormat()
	if outputFormat == OutputJSON {
		output := map[string]interface{}{
			"snapshots": snapshots,
			"count":     len(snapshots),
		}
		jsonData, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			outputError(fmt.Sprintf("Failed to marshal JSON: %v", err))
			os.Exit(1)
		}
		fmt.Println(string(jsonData))
	} else {
		if len(snapshots) == 0 {
			fmt.Println("No snapshots found.")
			return
		}
		
		fmt.Printf("Found %d snapshot(s):\n\n", len(snapshots))
		for _, snapshot := range snapshots {
			fmt.Printf("ID: %s\n", snapshot.ID)
			fmt.Printf("Time: %s\n", snapshot.Time.Format("2006-01-02 15:04:05 UTC"))
			fmt.Printf("Files: %d (%s)\n", snapshot.Summary.FilesCount, formatFileSize(snapshot.Summary.TotalSize))
			if len(snapshot.Tags) > 0 {
				fmt.Printf("Tags: %s\n", strings.Join(snapshot.Tags, ", "))
			}
			if snapshot.Hostname != "" {
				fmt.Printf("Host: %s", snapshot.Hostname)
				if snapshot.Username != "" {
					fmt.Printf(" (%s)", snapshot.Username)
				}
				fmt.Println()
			}
			fmt.Println()
		}
	}
}

// handleSnapshotForget implements restic-style snapshot retention
func handleSnapshotForget(args []string) {
	// Find dcfh repository
	_, dcfhDir, err := findDcfhRepo()
	if err != nil {
		outputError(fmt.Sprintf("Failed to find dcfh repository: %v", err))
		os.Exit(1)
	}
	
	// Load configuration to get default retention policy
	config, err := dcfh.LoadConfig(dcfhDir)
	if err != nil {
		outputError(fmt.Sprintf("Failed to load config: %v", err))
		os.Exit(1)
	}
	snapshotConfig := config.GetSnapshotConfig()
	
	// Start with config defaults
	policy := dcfh.RetentionPolicy{
		Hourly:  snapshotConfig.KeepHourly,
		Daily:   snapshotConfig.KeepDaily,
		Weekly:  snapshotConfig.KeepWeekly,
		Monthly: snapshotConfig.KeepMonthly,
		Yearly:  snapshotConfig.KeepYearly,
	}
	// Check for global dry-run flag, with config fallback
	dryRun := options.GetBool("dry-run")
	if !dryRun {
		dryRun = snapshotConfig.DryRun
	}
	
	// Parse command line arguments to override config (using restic-style flags)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		
		// Handle long options with = syntax first
		if strings.Contains(arg, "=") {
			parts := strings.SplitN(arg, "=", 2)
			if len(parts) != 2 {
				outputError(fmt.Sprintf("Invalid option format: %s", arg))
				os.Exit(1)
			}
			option := parts[0]
			value := parts[1]
			
			switch option {
			case "--keep-hourly":
				if val, err := strconv.Atoi(value); err == nil {
					policy.Hourly = val
				} else {
					outputError(fmt.Sprintf("Invalid --keep-hourly value: %s", value))
					os.Exit(1)
				}
			case "--keep-daily":
				if val, err := strconv.Atoi(value); err == nil {
					policy.Daily = val
				} else {
					outputError(fmt.Sprintf("Invalid --keep-daily value: %s", value))
					os.Exit(1)
				}
			case "--keep-weekly":
				if val, err := strconv.Atoi(value); err == nil {
					policy.Weekly = val
				} else {
					outputError(fmt.Sprintf("Invalid --keep-weekly value: %s", value))
					os.Exit(1)
				}
			case "--keep-monthly":
				if val, err := strconv.Atoi(value); err == nil {
					policy.Monthly = val
				} else {
					outputError(fmt.Sprintf("Invalid --keep-monthly value: %s", value))
					os.Exit(1)
				}
			case "--keep-yearly":
				if val, err := strconv.Atoi(value); err == nil {
					policy.Yearly = val
				} else {
					outputError(fmt.Sprintf("Invalid --keep-yearly value: %s", value))
					os.Exit(1)
				}
			default:
				outputError(fmt.Sprintf("Unknown option: %s", option))
				os.Exit(1)
			}
		} else if arg == "-H" || arg == "--keep-hourly" {
			// Next argument should be the value
			if i+1 >= len(args) {
				outputError("Missing value for -H/--keep-hourly")
				os.Exit(1)
			}
			if val, err := strconv.Atoi(args[i+1]); err == nil {
				policy.Hourly = val
				i++ // Skip the value argument
			} else {
				outputError(fmt.Sprintf("Invalid -H/--keep-hourly value: %s", args[i+1]))
				os.Exit(1)
			}
		} else if arg == "-d" || arg == "--keep-daily" {
			if i+1 >= len(args) {
				outputError("Missing value for -d/--keep-daily")
				os.Exit(1)
			}
			if val, err := strconv.Atoi(args[i+1]); err == nil {
				policy.Daily = val
				i++
			} else {
				outputError(fmt.Sprintf("Invalid -d/--keep-daily value: %s", args[i+1]))
				os.Exit(1)
			}
		} else if arg == "-w" || arg == "--keep-weekly" {
			if i+1 >= len(args) {
				outputError("Missing value for -w/--keep-weekly")
				os.Exit(1)
			}
			if val, err := strconv.Atoi(args[i+1]); err == nil {
				policy.Weekly = val
				i++
			} else {
				outputError(fmt.Sprintf("Invalid -w/--keep-weekly value: %s", args[i+1]))
				os.Exit(1)
			}
		} else if arg == "-m" || arg == "--keep-monthly" {
			if i+1 >= len(args) {
				outputError("Missing value for -m/--keep-monthly")
				os.Exit(1)
			}
			if val, err := strconv.Atoi(args[i+1]); err == nil {
				policy.Monthly = val
				i++
			} else {
				outputError(fmt.Sprintf("Invalid -m/--keep-monthly value: %s", args[i+1]))
				os.Exit(1)
			}
		} else if arg == "-y" || arg == "--keep-yearly" {
			if i+1 >= len(args) {
				outputError("Missing value for -y/--keep-yearly")
				os.Exit(1)
			}
			if val, err := strconv.Atoi(args[i+1]); err == nil {
				policy.Yearly = val
				i++
			} else {
				outputError(fmt.Sprintf("Invalid -y/--keep-yearly value: %s", args[i+1]))
				os.Exit(1)
			}
		} else if strings.HasPrefix(arg, "--tag") {
			// TODO: Implement tag filtering for forget (skip for now)
			outputError("Tag filtering not yet implemented for forget command")
			os.Exit(1)
		} else if arg != "" {
			outputError(fmt.Sprintf("Unknown argument: %s", arg))
			os.Exit(1)
		}
	}
	
	// Create snapshot repository
	sr := dcfh.NewSnapshotRepository(dcfhDir)
	
	// Show retention policy
	verbosity := options.GetInt("verbose")
	if verbosity >= 1 || dryRun {
		fmt.Printf("Retention policy: hourly=%d, daily=%d, weekly=%d, monthly=%d, yearly=%d\n",
			policy.Hourly, policy.Daily, policy.Weekly, policy.Monthly, policy.Yearly)
		if dryRun {
			fmt.Println("DRY RUN: No snapshots will be removed")
		}
	}
	
	// Apply retention policy
	removed, err := sr.ForgetSnapshots(policy, dryRun)
	if err != nil {
		outputError(fmt.Sprintf("Failed to apply retention policy: %v", err))
		os.Exit(1)
	}
	
	// Output results
	outputFormat := validateOutputFormat()
	if outputFormat == OutputJSON {
		result := map[string]interface{}{
			"removed_count": len(removed),
			"removed":       removed,
			"dry_run":       dryRun,
			"policy":        policy,
		}
		jsonData, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			outputError(fmt.Sprintf("Failed to marshal JSON: %v", err))
			os.Exit(1)
		}
		fmt.Println(string(jsonData))
	} else {
		if len(removed) == 0 {
			fmt.Println("No snapshots removed.")
		} else {
			if dryRun {
				fmt.Printf("Would remove %d snapshot(s):\n", len(removed))
			} else {
				fmt.Printf("Removed %d snapshot(s):\n", len(removed))
			}
			for _, id := range removed {
				fmt.Printf("  %s\n", id)
			}
		}
	}
}

// handleSnapshotForgetSpecial handles snapshot forget with manual flag parsing to avoid conflicts
func handleSnapshotForgetSpecial(globalArgs []string, subcommandArgs []string) {
	// Parse global flags manually
	
	verboseLevel := 0
	dryRun := false
	outputFormat := "human"
	
	// Parse global flags
	i := 0
	for i < len(globalArgs) {
		arg := globalArgs[i]
		
		// Global flags we need to handle
		if arg == "--dry-run" {
			dryRun = true
		} else if arg == "-v" || arg == "--verbose" {
			if i+1 < len(globalArgs) {
				if val, err := strconv.Atoi(globalArgs[i+1]); err == nil && val >= 0 {
					verboseLevel = val
					i++ // Skip value
				} else {
					verboseLevel = 1 // Default verbose
				}
			} else {
				verboseLevel = 1 // Default verbose
			}
		} else if strings.HasPrefix(arg, "--verbose=") {
			if val, err := strconv.Atoi(strings.TrimPrefix(arg, "--verbose=")); err == nil && val >= 0 {
				verboseLevel = val
			}
		} else if strings.HasPrefix(arg, "-v") && len(arg) > 2 {
			// Handle -vv, -vvv format
			verboseLevel = len(arg) - 1
		} else if arg == "-j" || arg == "--json" {
			outputFormat = "json"
		} else if strings.HasPrefix(arg, "--output=") {
			outputFormat = strings.TrimPrefix(arg, "--output=")
		} else if arg == "-o" || arg == "--output" {
			if i+1 < len(globalArgs) {
				outputFormat = globalArgs[i+1]
				i++ // Skip value
			}
		}
		i++
	}
	
	// Set up minimal global state
	dcfh.SetVerboseLevel(verboseLevel)
	
	// Find dcfh repository
	_, dcfhDir, err := findDcfhRepo()
	if err != nil {
		outputError(fmt.Sprintf("Failed to find dcfh repository: %v", err))
		os.Exit(1)
	}
	
	// Load configuration to get default retention policy
	config, err := dcfh.LoadConfig(dcfhDir)
	if err != nil {
		outputError(fmt.Sprintf("Failed to load config: %v", err))
		os.Exit(1)
	}
	snapshotConfig := config.GetSnapshotConfig()
	
	// Start with config defaults
	policy := dcfh.RetentionPolicy{
		Hourly:  snapshotConfig.KeepHourly,
		Daily:   snapshotConfig.KeepDaily,
		Weekly:  snapshotConfig.KeepWeekly,
		Monthly: snapshotConfig.KeepMonthly,
		Yearly:  snapshotConfig.KeepYearly,
	}
	
	// Apply config dry-run if not set by global flag
	if !dryRun {
		dryRun = snapshotConfig.DryRun
	}
	
	// Parse subcommand flags (same logic as handleSnapshotForget)
	for i := 0; i < len(subcommandArgs); i++ {
		arg := subcommandArgs[i]
		
		// Handle long options with = syntax first
		if strings.Contains(arg, "=") {
			parts := strings.SplitN(arg, "=", 2)
			if len(parts) != 2 {
				outputError(fmt.Sprintf("Invalid option format: %s", arg))
				os.Exit(1)
			}
			option := parts[0]
			value := parts[1]
			
			switch option {
			case "--keep-hourly":
				if val, err := strconv.Atoi(value); err == nil {
					policy.Hourly = val
				} else {
					outputError(fmt.Sprintf("Invalid --keep-hourly value: %s", value))
					os.Exit(1)
				}
			case "--keep-daily":
				if val, err := strconv.Atoi(value); err == nil {
					policy.Daily = val
				} else {
					outputError(fmt.Sprintf("Invalid --keep-daily value: %s", value))
					os.Exit(1)
				}
			case "--keep-weekly":
				if val, err := strconv.Atoi(value); err == nil {
					policy.Weekly = val
				} else {
					outputError(fmt.Sprintf("Invalid --keep-weekly value: %s", value))
					os.Exit(1)
				}
			case "--keep-monthly":
				if val, err := strconv.Atoi(value); err == nil {
					policy.Monthly = val
				} else {
					outputError(fmt.Sprintf("Invalid --keep-monthly value: %s", value))
					os.Exit(1)
				}
			case "--keep-yearly":
				if val, err := strconv.Atoi(value); err == nil {
					policy.Yearly = val
				} else {
					outputError(fmt.Sprintf("Invalid --keep-yearly value: %s", value))
					os.Exit(1)
				}
			default:
				outputError(fmt.Sprintf("Unknown option: %s", option))
				os.Exit(1)
			}
		} else if arg == "-H" || arg == "--keep-hourly" {
			if i+1 >= len(subcommandArgs) {
				outputError("Missing value for -H/--keep-hourly")
				os.Exit(1)
			}
			if val, err := strconv.Atoi(subcommandArgs[i+1]); err == nil {
				policy.Hourly = val
				i++
			} else {
				outputError(fmt.Sprintf("Invalid -H/--keep-hourly value: %s", subcommandArgs[i+1]))
				os.Exit(1)
			}
		} else if arg == "-d" || arg == "--keep-daily" {
			if i+1 >= len(subcommandArgs) {
				outputError("Missing value for -d/--keep-daily")
				os.Exit(1)
			}
			if val, err := strconv.Atoi(subcommandArgs[i+1]); err == nil {
				policy.Daily = val
				i++
			} else {
				outputError(fmt.Sprintf("Invalid -d/--keep-daily value: %s", subcommandArgs[i+1]))
				os.Exit(1)
			}
		} else if arg == "-w" || arg == "--keep-weekly" {
			if i+1 >= len(subcommandArgs) {
				outputError("Missing value for -w/--keep-weekly")
				os.Exit(1)
			}
			if val, err := strconv.Atoi(subcommandArgs[i+1]); err == nil {
				policy.Weekly = val
				i++
			} else {
				outputError(fmt.Sprintf("Invalid -w/--keep-weekly value: %s", subcommandArgs[i+1]))
				os.Exit(1)
			}
		} else if arg == "-m" || arg == "--keep-monthly" {
			if i+1 >= len(subcommandArgs) {
				outputError("Missing value for -m/--keep-monthly")
				os.Exit(1)
			}
			if val, err := strconv.Atoi(subcommandArgs[i+1]); err == nil {
				policy.Monthly = val
				i++
			} else {
				outputError(fmt.Sprintf("Invalid -m/--keep-monthly value: %s", subcommandArgs[i+1]))
				os.Exit(1)
			}
		} else if arg == "-y" || arg == "--keep-yearly" {
			if i+1 >= len(subcommandArgs) {
				outputError("Missing value for -y/--keep-yearly")
				os.Exit(1)
			}
			if val, err := strconv.Atoi(subcommandArgs[i+1]); err == nil {
				policy.Yearly = val
				i++
			} else {
				outputError(fmt.Sprintf("Invalid -y/--keep-yearly value: %s", subcommandArgs[i+1]))
				os.Exit(1)
			}
		} else if strings.HasPrefix(arg, "--tag") {
			outputError("Tag filtering not yet implemented for forget command")
			os.Exit(1)
		} else if arg != "" {
			outputError(fmt.Sprintf("Unknown argument: %s", arg))
			os.Exit(1)
		}
	}
	
	// Create snapshot repository
	sr := dcfh.NewSnapshotRepository(dcfhDir)
	
	// Show retention policy
	if verboseLevel >= 1 || dryRun {
		fmt.Printf("Retention policy: hourly=%d, daily=%d, weekly=%d, monthly=%d, yearly=%d\n",
			policy.Hourly, policy.Daily, policy.Weekly, policy.Monthly, policy.Yearly)
		if dryRun {
			fmt.Println("DRY RUN: No snapshots will be removed")
		}
	}
	
	// Apply retention policy
	removed, err := sr.ForgetSnapshots(policy, dryRun)
	if err != nil {
		outputError(fmt.Sprintf("Failed to apply retention policy: %v", err))
		os.Exit(1)
	}
	
	// Output results (simplified version)
	if outputFormat == "json" {
		result := map[string]interface{}{
			"removed_count": len(removed),
			"removed":       removed,
			"dry_run":       dryRun,
			"policy":        policy,
		}
		jsonData, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			outputError(fmt.Sprintf("Failed to marshal JSON: %v", err))
			os.Exit(1)
		}
		fmt.Println(string(jsonData))
	} else {
		if len(removed) == 0 {
			fmt.Println("No snapshots removed.")
		} else {
			if dryRun {
				fmt.Printf("Would remove %d snapshot(s):\n", len(removed))
			} else {
				fmt.Printf("Removed %d snapshot(s):\n", len(removed))
			}
			for _, id := range removed {
				fmt.Printf("  %s\n", id)
			}
		}
	}
}

// handleSnapshotStatus compares current state with snapshots (placeholder)
func handleSnapshotStatus(args []string) {
	// TODO: Implement snapshot comparison
	outputError("Snapshot status not yet implemented")
	os.Exit(1)
}