package main

import (
	"context"
	"fmt"

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
		ctx := cmd.Context()

		// Handle --list flag
		if configListFlag {
			if len(args) > 0 {
				return fmt.Errorf("cannot specify configuration keys with --list flag")
			}
			return handleConfigList(ctx)
		}

		switch len(args) {
		case 0:
			return cmd.Help()
		case 1:
			return handleConfigGet(ctx, args[0])
		case 2:
			return handleConfigSet(ctx, args[0], args[1])
		default:
			return fmt.Errorf("too many arguments for config command")
		}
	},
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.Flags().BoolVar(&configListFlag, "list", false, "list all configuration variables")
}

// configValues collects every supported key into an ordered (key, value)
// list. The value field is `any` so verbose.level survives as a JSON number
// rather than a stringified int.
func configValues(cfg *dcfh.AllConfig) []struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
} {
	return []struct {
		Key   string `json:"key"`
		Value any    `json:"value"`
	}{
		{"filehash.default", cfg.Hash.Default},
		{"output.format", cfg.Output.Format},
		{"verbose.level", cfg.Verbose.Level},
		{"verbose.debug", cfg.Verbose.Debug},
		{"symlink.mode", cfg.Symlink.Mode},
	}
}

// handleConfigList lists all configuration variables
func handleConfigList(ctx context.Context) error {
	_, metaDir, err := findDcfhRepo()
	if err != nil {
		return err
	}
	repo, err := dcfh.OpenRepo(ctx, metaDir)
	if err != nil {
		return err
	}
	defer func() { _ = repo.Close() }()

	allConfig, err := repo.Config().Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	values := configValues(allConfig)
	if getOutputFormat() == OutputJSON {
		outputJSON(struct {
			Items []struct {
				Key   string `json:"key"`
				Value any    `json:"value"`
			} `json:"items"`
		}{values})
		return nil
	}
	for _, kv := range values {
		fmt.Printf("%s=%v\n", kv.Key, kv.Value)
	}
	return nil
}

// handleConfigGet retrieves a specific configuration value
func handleConfigGet(ctx context.Context, key string) error {
	_, metaDir, err := findDcfhRepo()
	if err != nil {
		return err
	}
	repo, err := dcfh.OpenRepo(ctx, metaDir)
	if err != nil {
		return err
	}
	defer func() { _ = repo.Close() }()

	allConfig, err := repo.Config().Get(ctx)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	var value any
	switch key {
	case "filehash.default":
		value = allConfig.Hash.Default
	case "output.format":
		value = allConfig.Output.Format
	case "verbose.level":
		value = allConfig.Verbose.Level
	case "verbose.debug":
		value = allConfig.Verbose.Debug
	case "symlink.mode":
		value = allConfig.Symlink.Mode
	default:
		return fmt.Errorf("unknown configuration key: %s", key)
	}

	if getOutputFormat() == OutputJSON {
		outputJSON(struct {
			Key   string `json:"key"`
			Value any    `json:"value"`
		}{key, value})
		return nil
	}
	fmt.Println(value)
	return nil
}

// handleConfigSet sets a configuration value
func handleConfigSet(ctx context.Context, key, value string) error {
	_, metaDir, err := findDcfhRepo()
	if err != nil {
		return err
	}
	repo, err := dcfh.OpenRepo(ctx, metaDir)
	if err != nil {
		return err
	}
	defer func() { _ = repo.Close() }()

	if err := repo.Config().Set(ctx, key, value); err != nil {
		return err
	}

	if getOutputFormat() == OutputJSON {
		outputJSON(struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
			Key     string `json:"key"`
			Value   string `json:"value"`
		}{true, "Configuration updated", key, value})
		return nil
	}
	fmt.Printf("Configuration updated: %s = %s\n", key, value)
	return nil
}
