package main

import (
	"context"
	"fmt"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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

// registerRootPersistentFlags installs the root persistent flag
// dialect on pf. Called against rootCmd.PersistentFlags() at init
// time and against the per-RunE pflag.FlagSet built by the
// scope-marker preamble (which sets DisableFlagParsing=true and so
// must re-parse persistent flags itself).
func registerRootPersistentFlags(pf *pflag.FlagSet) {
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
}

var rootCmd = &cobra.Command{
	Use:   "dcfh",
	Short: "Directory Cache File Hash",
	Long:  "A fast file indexing, hashing, and duplicate detection tool",
	// Silence cobra's default usage and error printing — we handle it ourselves
	SilenceUsage:  true,
	SilenceErrors: true,
	Version:       getVersionString(),
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Scope-marker commands parse argv inside RunE; finalise there.
		if cmd.DisableFlagParsing {
			return nil
		}
		return finaliseRootFlags(cmd)
	},
}

// finaliseRootFlags applies post-parse normalisation: --json/-s
// aliasing, output-format validation, debug/verbose plumbing,
// --meta-dir resolution, and viper-driven config defaults. Expects
// every root persistent flag global to already hold its parsed value.
func finaliseRootFlags(cmd *cobra.Command) error {
	if flagJSON && flagOutput != "human" {
		return fmt.Errorf("cannot use both --json and --output flags together")
	}
	if flagJSON {
		flagOutput = "json"
	}
	if flagSymlinksShortAll {
		flagSymlinks = "all"
	}
	switch flagOutput {
	case "human", "json", "fdupes":
	default:
		return fmt.Errorf("invalid output format '%s'. Supported formats: human, json, fdupes", flagOutput)
	}

	dcfh.InitDebugFlags(flagDebug)
	if flagDebug != "" {
		dcfh.LogDebugFlags()
	}
	if flagVerbose > 0 {
		dcfh.SetVerboseLevel(flagVerbose)
	}

	if flagGlobalMetaDir != "" && cmd.Name() != "init" {
		uri, err := dcfh.ParseRepoURI(flagGlobalMetaDir)
		if err != nil {
			return fmt.Errorf("failed to parse --meta-dir: %w", err)
		}
		if uri.Scheme != "file" {
			return fmt.Errorf("%w: --meta-dir=%s; put remote URIs in [repository] root instead", dcfh.ErrRemoteNotImplemented, flagGlobalMetaDir)
		}
		rootDir, metaDir, err := dcfh.ResolveRepository(uri.Path)
		if err != nil {
			return fmt.Errorf("failed to resolve --meta-dir: %w", err)
		}
		cachedRepoRoot = rootDir
		cachedMetaDir = metaDir
	}

	applyViperDefaults(cmd)
	return nil
}

func init() {
	registerRootPersistentFlags(rootCmd.PersistentFlags())

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
