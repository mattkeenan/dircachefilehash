package main

import (
	"fmt"
	"os"
	"strconv"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

// handleConfig handles the "dcfh config" command and its subcommands
func handleConfig(args []string) {
	// Check for help request first (before parsing options)
	if len(args) > 0 && (args[0] == "help" || args[0] == "-h" || args[0] == "--help") {
		showConfigUsage()
		return
	}
	
	// Create options parser for config subcommand
	configOptions := NewParsedOptions()
	configOptions.DefineOption("list", "", OptionTypeBool, "", "list all configuration variables")
	configOptions.DefineOption("global", "", OptionTypeBool, "", "use global configuration (not implemented)")
	
	// Parse config-specific options
	if err := configOptions.Parse(args); err != nil {
		outputError(fmt.Sprintf("Error parsing config options: %v", err))
		os.Exit(1)
	}
	
	configArgs := configOptions.GetArgs()
	
	// Handle --list flag
	if configOptions.GetBool("list") {
		if len(configArgs) > 0 {
			outputError("Cannot specify configuration keys with --list flag")
			os.Exit(1)
		}
		handleConfigList()
		return
	}
	
	// Handle get/set operations
	switch len(configArgs) {
	case 0:
		// No args with no --list means show usage
		showConfigUsage()
		os.Exit(1)
	case 1:
		// Get operation: dcfh config key
		handleConfigGet(configArgs[0])
	case 2:
		// Set operation: dcfh config key value
		handleConfigSet(configArgs[0], configArgs[1])
	default:
		outputError("Too many arguments for config command")
		os.Exit(1)
	}
}

// showConfigUsage displays usage information for the config command
func showConfigUsage() {
	fmt.Fprintf(os.Stderr, "Usage: dcfh config [OPTIONS] [<key>] [<value>]\n\n")
	fmt.Fprintf(os.Stderr, "Options:\n")
	fmt.Fprintf(os.Stderr, "  --list       List all configuration variables\n")
	fmt.Fprintf(os.Stderr, "  --global     Use global configuration (not implemented)\n")
	fmt.Fprintf(os.Stderr, "\nConfiguration Keys:\n")
	fmt.Fprintf(os.Stderr, "  filehash.default     Default hash algorithm (sha1, sha256, sha512)\n")
	fmt.Fprintf(os.Stderr, "  output.format        Default output format (human, json, fdupes)\n")
	fmt.Fprintf(os.Stderr, "  verbose.level        Default verbose level (0-3)\n")
	fmt.Fprintf(os.Stderr, "  verbose.debug        Default debug flags (comma-separated)\n")
	fmt.Fprintf(os.Stderr, "  symlink.mode         Default symlink handling (all, contained, none)\n")
	fmt.Fprintf(os.Stderr, "\nExamples:\n")
	fmt.Fprintf(os.Stderr, "  dcfh config --list\n")
	fmt.Fprintf(os.Stderr, "  dcfh config filehash.default\n")
	fmt.Fprintf(os.Stderr, "  dcfh config filehash.default sha256\n")
	fmt.Fprintf(os.Stderr, "  dcfh config output.format fdupes\n")
}

// handleConfigList lists all configuration variables
func handleConfigList() {
	// Find the dcfh repository root
	_, dcfhDir, err := findDcfhRepo()
	if err != nil {
		outputError(err.Error())
		os.Exit(1)
	}
	
	// Load configuration
	config, err := dcfh.LoadConfig(dcfhDir)
	if err != nil {
		outputError(fmt.Sprintf("Failed to load configuration: %v", err))
		os.Exit(1)
	}
	
	allConfig := config.GetAllConfig()
	
	// List all configuration in git config format
	fmt.Printf("filehash.default=%s\n", allConfig.Hash.Default)
	fmt.Printf("output.format=%s\n", allConfig.Output.Format)
	fmt.Printf("verbose.level=%d\n", allConfig.Verbose.Level)
	fmt.Printf("verbose.debug=%s\n", allConfig.Verbose.Debug)
	fmt.Printf("symlink.mode=%s\n", allConfig.Symlink.Mode)
}

// handleConfigGet retrieves a specific configuration value
func handleConfigGet(key string) {
	// Find the dcfh repository root
	_, dcfhDir, err := findDcfhRepo()
	if err != nil {
		outputError(err.Error())
		os.Exit(1)
	}
	
	// Load configuration
	config, err := dcfh.LoadConfig(dcfhDir)
	if err != nil {
		outputError(fmt.Sprintf("Failed to load configuration: %v", err))
		os.Exit(1)
	}
	
	allConfig := config.GetAllConfig()
	
	// Get the requested configuration value
	switch key {
	case "filehash.default":
		fmt.Println(allConfig.Hash.Default)
	case "output.format":
		fmt.Println(allConfig.Output.Format)
	case "verbose.level":
		fmt.Println(allConfig.Verbose.Level)
	case "verbose.debug":
		fmt.Println(allConfig.Verbose.Debug)
	case "symlink.mode":
		fmt.Println(allConfig.Symlink.Mode)
	default:
		outputError(fmt.Sprintf("Unknown configuration key: %s", key))
		os.Exit(1)
	}
}

// handleConfigSet sets a configuration value
func handleConfigSet(key, value string) {
	// Find the dcfh repository root
	_, dcfhDir, err := findDcfhRepo()
	if err != nil {
		outputError(err.Error())
		os.Exit(1)
	}
	
	// Load configuration
	config, err := dcfh.LoadConfig(dcfhDir)
	if err != nil {
		outputError(fmt.Sprintf("Failed to load configuration: %v", err))
		os.Exit(1)
	}
	
	// Set the configuration value with validation
	switch key {
	case "filehash.default":
		if err := dcfh.ValidateHashAlgorithm(value); err != nil {
			outputError(fmt.Sprintf("Invalid hash algorithm: %v", err))
			os.Exit(1)
		}
		if err := config.SetHashDefault(value); err != nil {
			outputError(fmt.Sprintf("Failed to set filehash.default: %v", err))
			os.Exit(1)
		}
	case "output.format":
		if err := dcfh.ValidateOutputFormat(value); err != nil {
			outputError(fmt.Sprintf("Invalid output format: %v", err))
			os.Exit(1)
		}
		if err := config.SetOutputFormat(value); err != nil {
			outputError(fmt.Sprintf("Failed to set output.format: %v", err))
			os.Exit(1)
		}
	case "verbose.level":
		level, err := strconv.Atoi(value)
		if err != nil {
			outputError(fmt.Sprintf("Invalid verbose level '%s': must be a number", value))
			os.Exit(1)
		}
		if err := dcfh.ValidateVerboseLevel(level); err != nil {
			outputError(fmt.Sprintf("Invalid verbose level: %v", err))
			os.Exit(1)
		}
		if err := config.SetVerboseLevel(level); err != nil {
			outputError(fmt.Sprintf("Failed to set verbose.level: %v", err))
			os.Exit(1)
		}
	case "verbose.debug":
		if err := dcfh.ValidateDebugFlags(value); err != nil {
			outputError(fmt.Sprintf("Invalid debug flags: %v", err))
			os.Exit(1)
		}
		if err := config.SetDebugFlags(value); err != nil {
			outputError(fmt.Sprintf("Failed to set verbose.debug: %v", err))
			os.Exit(1)
		}
	case "symlink.mode":
		if err := dcfh.ValidateSymlinkMode(value); err != nil {
			outputError(fmt.Sprintf("Invalid symlink mode: %v", err))
			os.Exit(1)
		}
		if err := config.SetSymlinkMode(value); err != nil {
			outputError(fmt.Sprintf("Failed to set symlink.mode: %v", err))
			os.Exit(1)
		}
	default:
		outputError(fmt.Sprintf("Unknown configuration key: %s", key))
		showConfigUsage()
		os.Exit(1)
	}
	
	fmt.Printf("Configuration updated: %s = %s\n", key, value)
}