package main

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

var configListFlag bool

var configCmd = &cobra.Command{
	Use:   "config [key] [value]",
	Short: "Get and set repository configuration options",
	Long: `Get and set repository configuration options.

Configuration keys:
  filehash.default     Default hash algorithm (sha1, sha256, sha512)
  output.format        Default output format (human, json, fdupes)
  verbose.level        Default verbose level (0-3)
  verbose.debug        Default debug flags (comma-separated)
  symlink.mode         Default symlink handling (all, internal, external, none)

Examples:
  dcfh config --list
  dcfh config filehash.default
  dcfh config filehash.default sha256
  dcfh config output.format fdupes`,
	Args: cobra.MaximumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Handle --list flag
		if configListFlag {
			if len(args) > 0 {
				return fmt.Errorf("cannot specify configuration keys with --list flag")
			}
			return handleConfigList()
		}

		switch len(args) {
		case 0:
			return cmd.Help()
		case 1:
			return handleConfigGet(args[0])
		case 2:
			return handleConfigSet(args[0], args[1])
		default:
			return fmt.Errorf("too many arguments for config command")
		}
	},
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.Flags().BoolVar(&configListFlag, "list", false, "list all configuration variables")
}

// handleConfigList lists all configuration variables
func handleConfigList() error {
	_, metaDir, err := findDcfhRepo()
	if err != nil {
		return err
	}

	config, err := dcfh.LoadConfig(metaDir)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	allConfig := config.GetAllConfig()

	fmt.Printf("filehash.default=%s\n", allConfig.Hash.Default)
	fmt.Printf("output.format=%s\n", allConfig.Output.Format)
	fmt.Printf("verbose.level=%d\n", allConfig.Verbose.Level)
	fmt.Printf("verbose.debug=%s\n", allConfig.Verbose.Debug)
	fmt.Printf("symlink.mode=%s\n", allConfig.Symlink.Mode)

	return nil
}

// handleConfigGet retrieves a specific configuration value
func handleConfigGet(key string) error {
	_, metaDir, err := findDcfhRepo()
	if err != nil {
		return err
	}

	config, err := dcfh.LoadConfig(metaDir)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	allConfig := config.GetAllConfig()

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
		return fmt.Errorf("unknown configuration key: %s", key)
	}

	return nil
}

// handleConfigSet sets a configuration value
func handleConfigSet(key, value string) error {
	_, metaDir, err := findDcfhRepo()
	if err != nil {
		return err
	}

	config, err := dcfh.LoadConfig(metaDir)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	switch key {
	case "filehash.default":
		if err := dcfh.ValidateHashAlgorithm(value); err != nil {
			return fmt.Errorf("invalid hash algorithm: %w", err)
		}
		if err := config.SetHashDefault(value); err != nil {
			return fmt.Errorf("failed to set filehash.default: %w", err)
		}
	case "output.format":
		if err := dcfh.ValidateOutputFormat(value); err != nil {
			return fmt.Errorf("invalid output format: %w", err)
		}
		if err := config.SetOutputFormat(value); err != nil {
			return fmt.Errorf("failed to set output.format: %w", err)
		}
	case "verbose.level":
		level, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid verbose level '%s': must be a number", value)
		}
		if err := dcfh.ValidateVerboseLevel(level); err != nil {
			return fmt.Errorf("invalid verbose level: %w", err)
		}
		if err := config.SetVerboseLevel(level); err != nil {
			return fmt.Errorf("failed to set verbose.level: %w", err)
		}
	case "verbose.debug":
		if err := dcfh.ValidateDebugFlags(value); err != nil {
			return fmt.Errorf("invalid debug flags: %w", err)
		}
		if err := config.SetDebugFlags(value); err != nil {
			return fmt.Errorf("failed to set verbose.debug: %w", err)
		}
	case "symlink.mode":
		if err := dcfh.ValidateSymlinkMode(value); err != nil {
			return fmt.Errorf("invalid symlink mode: %w", err)
		}
		if err := config.SetSymlinkMode(value); err != nil {
			return fmt.Errorf("failed to set symlink.mode: %w", err)
		}
	default:
		return fmt.Errorf("unknown configuration key: %s", key)
	}

	fmt.Printf("Configuration updated: %s = %s\n", key, value)

	return nil
}
