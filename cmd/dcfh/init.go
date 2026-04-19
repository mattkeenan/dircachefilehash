package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

var flagInitExternal bool

var initCmd = &cobra.Command{
	Use:   "init <directory>",
	Short: "Initialise a new dcfh repository",
	Long: `Initialise a new dcfh repository in the specified directory.

Creates a new .dcfh directory structure with configuration files
and an empty main index. The directory must exist and not already
contain a .dcfh repository.

Use --external to create the .dcfh metadata directory outside the
scanned directory (similar to a git bare repository). This is useful
for indexing read-only directories or centralised index management.

Examples:
  dcfh init /home/matt/some/dir
    Creates /home/matt/some/dir/.dcfh/

  dcfh init --external /home/matt/some/dir
    Creates ./home-matt-some-dir.dcfh/ in the current directory

  dcfh init --external /home/matt/some/dir --meta-dir /storage/my-index.dcfh
    Creates /storage/my-index.dcfh/`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		directory := args[0]
		start := time.Now()

		// Convert to absolute path and resolve symlinks
		absDir, err := filepath.Abs(directory)
		if err != nil {
			return fmt.Errorf("failed to get absolute path for %s: %w", directory, err)
		}

		// Resolve symlinks to get the real path
		realDir, err := filepath.EvalSymlinks(absDir)
		if err != nil {
			// If symlink resolution fails, fall back to absolute path
			realDir = absDir
		}
		absDir = realDir

		// Check if directory exists
		info, err := os.Stat(absDir)
		if err != nil {
			return fmt.Errorf("failed to access directory %s: %w", absDir, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory", absDir)
		}

		var metaDir string
		if flagInitExternal {
			if flagGlobalMetaDir != "" {
				metaDir, err = filepath.Abs(flagGlobalMetaDir)
				if err != nil {
					return fmt.Errorf("failed to get absolute path for %s: %w", flagGlobalMetaDir, err)
				}
				if filepath.Ext(metaDir) != ".dcfh" {
					metaDir += ".dcfh"
				}
			} else {
				slug := dcfh.PathToSlug(absDir)
				metaDir, err = filepath.Abs(slug + ".dcfh")
				if err != nil {
					return fmt.Errorf("failed to get absolute path: %w", err)
				}
			}

			if _, err := os.Stat(metaDir); err == nil {
				return fmt.Errorf("external .dcfh directory already exists: %s", metaDir)
			}
		} else {
			if flagGlobalMetaDir != "" {
				return fmt.Errorf("--meta-dir can only be used with --external")
			}

			metaDir = filepath.Join(absDir, ".dcfh")
			if _, err := os.Stat(metaDir); err == nil {
				return fmt.Errorf(".dcfh directory already exists in %s", absDir)
			}
		}

		ctx := cmd.Context()

		// Internal layout passes empty metaDirSpec so CreateRepo uses the
		// default ".dcfh" under absDir; external passes the resolved meta dir.
		metaSpec := ""
		if flagInitExternal {
			metaSpec = metaDir
		}
		repo, err := dcfh.CreateRepo(ctx, absDir, metaSpec)
		if err != nil {
			return fmt.Errorf("failed to create repository: %w", err)
		}
		defer func() { _ = repo.Close() }()

		format := getOutputFormat()
		duration := time.Since(start)

		// Always show the initialisation message (git-style)
		fmt.Printf("Initialised empty dcfh repository in %s\n", metaDir)
		if flagInitExternal {
			fmt.Printf("  scanning: %s\n", absDir)
		}

		if format == OutputJSON {
			output := InitOutput{
				Success:     true,
				Message:     "Successfully initialised dcfh repository",
				Repository:  absDir,
				FileCount:   0,
				TotalSize:   0,
				TimeElapsed: duration.Round(time.Millisecond).String(),
			}
			outputJSON(output)
		} else if flagVerbose > 0 {
			fmt.Printf("✓ Repository structure created\n")
			fmt.Printf("✓ Configuration files initialised\n")
			fmt.Printf("✓ Run 'dcfh update' to scan and index files\n")
			fmt.Printf("✓ Completed in %v\n", duration.Round(time.Millisecond))
		}

		return nil
	},
}

func init() {
	initCmd.Flags().BoolVar(&flagInitExternal, "external", false, "create .dcfh directory outside the scanned directory")
	rootCmd.AddCommand(initCmd)
}
