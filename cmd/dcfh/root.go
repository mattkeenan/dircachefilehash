package main

import (
	"context"
	"fmt"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

var (
	// Global flag variables bound to rootCmd persistent flags
	flagOutput           string
	flagJSON             bool
	flagVerbose          int
	flagDebug            string
	flagFilehash         string
	flagSymlinks         string
	flagSymlinksShortAll bool
	flagHashWorkers      int
	flagIndexLockTimeout int
	flagDryRun           bool
	flagGlobalMetaDir    string

	// Cached repo discovery — populated by PersistentPreRunE, reused by commands
	cachedRepoRoot string
	cachedMetaDir  string
)

var rootCmd = &cobra.Command{
	Use:   "dcfh",
	Short: "Directory Cache File Hash",
	Long:  "A fast file indexing, hashing, and duplicate detection tool",
	// Silence cobra's default usage and error printing — we handle it ourselves
	SilenceUsage:  true,
	SilenceErrors: true,
	Version:       getVersionString(),
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Validate --json vs --output mutual exclusion:
		// --json + --output=<non-default> is a conflict
		if flagJSON && flagOutput != "human" {
			return fmt.Errorf("cannot use both --json and --output flags together")
		}

		// --json is an alias for --output=json
		if flagJSON {
			flagOutput = "json"
		}

		// -s is an alias for --symlinks=all
		if flagSymlinksShortAll {
			flagSymlinks = "all"
		}

		// Validate output format
		switch flagOutput {
		case "human", "json", "fdupes":
			// valid
		default:
			return fmt.Errorf("invalid output format '%s'. Supported formats: human, json, fdupes", flagOutput)
		}

		// Initialise debug flags early
		dcfh.InitDebugFlags(flagDebug)
		if flagDebug != "" {
			dcfh.LogDebugFlags()
		}

		// Set global verbose level
		if flagVerbose > 0 {
			dcfh.SetVerboseLevel(flagVerbose)
		}

		// If --dcfh-dir is set globally, resolve it and pre-populate cache.
		// Skip for init — the directory doesn't exist yet.
		if flagGlobalMetaDir != "" && cmd.Name() != "init" {
			absMetaDir, err := filepath.Abs(flagGlobalMetaDir)
			if err != nil {
				return fmt.Errorf("failed to resolve --meta-dir path: %w", err)
			}
			rootDir, metaDir, err := dcfh.ResolveRepository(absMetaDir)
			if err != nil {
				return fmt.Errorf("failed to resolve --meta-dir: %w", err)
			}
			cachedRepoRoot = rootDir
			cachedMetaDir = metaDir
		}

		// Viper config binding: apply config defaults for flags not explicitly set
		applyViperDefaults(cmd)

		return nil
	},
}

func init() {
	pf := rootCmd.PersistentFlags()

	pf.StringVarP(&flagOutput, "output", "o", "human", "output format: human, json, fdupes")
	pf.BoolVarP(&flagJSON, "json", "j", false, "output in JSON format (alias for --output=json)")
	pf.CountVarP(&flagVerbose, "verbose", "v", "verbose level (repeat for more: -v, -vv, -vvv)")
	pf.StringVarP(&flagDebug, "debug", "D", "", "debug options (comma-separated)")
	pf.StringVarP(&flagFilehash, "filehash", "f", "", "hash algorithm overrides (format: default:sha256)")
	pf.StringVar(&flagSymlinks, "symlinks", "none", "symlink handling: all, internal, external, none")
	pf.BoolVarP(&flagSymlinksShortAll, "follow-symlinks", "s", false, "follow symlinked directories (alias for --symlinks=all)")
	pf.IntVarP(&flagHashWorkers, "hash-workers", "w", 0, "number of concurrent hash workers (0=use config default)")
	pf.IntVar(&flagIndexLockTimeout, "index-lock-timeout", 0, "timeout in seconds for index memory locks (0=use config default)")
	pf.BoolVar(&flagDryRun, "dry-run", false, "show what would be done without actually doing it")
	pf.StringVar(&flagGlobalMetaDir, "meta-dir", "", "path to an external .dcfh directory (overrides auto-discovery)")

	// Register completion functions for flag values
	_ = rootCmd.RegisterFlagCompletionFunc("output", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"human", "json", "fdupes"}, cobra.ShellCompDirectiveNoFileComp
	})
	_ = rootCmd.RegisterFlagCompletionFunc("symlinks", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"all", "internal", "external", "none"}, cobra.ShellCompDirectiveNoFileComp
	})

	// Set version template to match existing format
	rootCmd.SetVersionTemplate("{{.Version}}\n")
}

// applyViperDefaults loads .dcfh/config via viper and applies defaults
// for any flags that were not explicitly set on the command line.
func applyViperDefaults(cmd *cobra.Command) {
	// Find the .dcfh directory
	_, metaDir, err := findDcfhRepo()
	if err != nil {
		// No repo found — skip config loading (e.g. running init or completion)
		return
	}

	configPath := filepath.Join(metaDir, "config")
	viper.SetConfigFile(configPath)
	viper.SetConfigType("ini")

	if err := viper.ReadInConfig(); err != nil {
		// Config file not found is fine (might not exist yet)
		return
	}

	// Apply config defaults only for flags not explicitly set by user
	if !cmd.Flags().Changed("hash-workers") {
		if v := viper.GetInt("performance.hash_workers"); v > 0 {
			flagHashWorkers = v
		}
	}
	if !cmd.Flags().Changed("index-lock-timeout") {
		if v := viper.GetInt("performance.index_lock_timeout"); v > 0 {
			flagIndexLockTimeout = v
		}
	}
	if !cmd.Flags().Changed("output") && !cmd.Flags().Changed("json") {
		if v := viper.GetString("output.format"); v != "" {
			flagOutput = v
		}
	}
}

// setupSignalContext creates a context that is cancelled on SIGINT, SIGTERM, or SIGPIPE.
// Used in main() to wire signal handling into cobra's context.
func setupSignalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGPIPE)
}
