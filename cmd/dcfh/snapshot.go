package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

var snapshotCmd = &cobra.Command{
	Use:   "snapshot",
	Short: "Create and manage index state snapshots",
	Long: `Create and manage index state snapshots.

Subcommands:
  create    Create a new snapshot of the current index state
  list      List all available snapshots
  forget    Remove snapshots based on retention policies
  remove    Remove specific snapshots by ID
  status    Compare current state with snapshots`,
}

var snapshotCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new snapshot",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		tags, _ := cmd.Flags().GetStringSlice("tag")

		// Find dcfh repository
		repoRoot, dcfhDir, err := findDcfhRepo()
		if err != nil {
			return fmt.Errorf("failed to find dcfh repository: %w", err)
		}

		sr := dcfh.NewSnapshotRepository(dcfhDir)

		if flagVerbose >= 1 {
			fmt.Printf("Creating snapshot...\n")
		}

		metadata, err := sr.CreateSnapshot(repoRoot, tags)
		if err != nil {
			return fmt.Errorf("failed to create snapshot: %w", err)
		}

		outputFormat := getOutputFormat()
		if outputFormat == OutputJSON {
			outputJSON(metadata)
		} else {
			fmt.Printf("Snapshot created: %s\n", metadata.ID)
			fmt.Printf("Time: %s\n", metadata.Time.Format("2006-01-02 15:04:05 UTC"))
			fmt.Printf("Files: %d (%s)\n", metadata.Summary.FilesCount, formatFileSize(metadata.Summary.TotalSize))
			if len(metadata.Tags) > 0 {
				fmt.Printf("Tags: %s\n", strings.Join(metadata.Tags, ", "))
			}
			if flagVerbose >= 1 {
				fmt.Printf("Tree hash: %s\n", metadata.Tree)
			}
		}

		return nil
	},
}

var snapshotListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List all available snapshots",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		_, dcfhDir, err := findDcfhRepo()
		if err != nil {
			return fmt.Errorf("failed to find dcfh repository: %w", err)
		}

		sr := dcfh.NewSnapshotRepository(dcfhDir)

		snapshots, err := sr.ListSnapshots()
		if err != nil {
			return fmt.Errorf("failed to list snapshots: %w", err)
		}

		outputFormat := getOutputFormat()
		if outputFormat == OutputJSON {
			outputJSON(map[string]any{
				"snapshots": snapshots,
				"count":     len(snapshots),
			})
		} else {
			if len(snapshots) == 0 {
				fmt.Println("No snapshots found.")
				return nil
			}

			if flagVerbose == 0 {
				// Single-line format
				for _, snapshot := range snapshots {
					hashStr := snapshot.Tree
					if len(hashStr) > 8 {
						hashStr = hashStr[:8]
					}

					tagsStr := ""
					if len(snapshot.Tags) > 0 {
						tagsStr = fmt.Sprintf(" [%s]", strings.Join(snapshot.Tags, ","))
					}

					fmt.Printf("%-27s %s%s\n", snapshot.ID, hashStr, tagsStr)
				}
			} else {
				// Multi-line detailed format
				fmt.Printf("Found %d snapshot(s):\n\n", len(snapshots))
				for _, snapshot := range snapshots {
					fmt.Printf("ID: %s\n", snapshot.ID)
					fmt.Printf("Time: %s\n", snapshot.Time.Format("2006-01-02 15:04:05 UTC"))
					fmt.Printf("Files: %d (%s)\n", snapshot.Summary.FilesCount, formatFileSize(snapshot.Summary.TotalSize))
					fmt.Printf("Hash: %s\n", snapshot.Tree)
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

		return nil
	},
}

// Snapshot forget local flags
var (
	forgetKeepHourly  int
	forgetKeepDaily   int
	forgetKeepWeekly  int
	forgetKeepMonthly int
	forgetKeepYearly  int
)

var snapshotForgetCmd = &cobra.Command{
	Use:   "forget",
	Short: "Remove snapshots based on retention policies",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		_, dcfhDir, err := findDcfhRepo()
		if err != nil {
			return fmt.Errorf("failed to find dcfh repository: %w", err)
		}

		// Load configuration to get default retention policy
		config, err := dcfh.LoadConfig(dcfhDir)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
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

		// Override with CLI flags if explicitly set
		if cmd.Flags().Changed("keep-hourly") {
			policy.Hourly = forgetKeepHourly
		}
		if cmd.Flags().Changed("keep-daily") {
			policy.Daily = forgetKeepDaily
		}
		if cmd.Flags().Changed("keep-weekly") {
			policy.Weekly = forgetKeepWeekly
		}
		if cmd.Flags().Changed("keep-monthly") {
			policy.Monthly = forgetKeepMonthly
		}
		if cmd.Flags().Changed("keep-yearly") {
			policy.Yearly = forgetKeepYearly
		}

		// Check for global dry-run flag, with config fallback
		dryRun := flagDryRun
		if !dryRun {
			dryRun = snapshotConfig.DryRun
		}

		sr := dcfh.NewSnapshotRepository(dcfhDir)

		// Show retention policy
		if flagVerbose >= 1 || dryRun {
			fmt.Printf("Retention policy: hourly=%d, daily=%d, weekly=%d, monthly=%d, yearly=%d\n",
				policy.Hourly, policy.Daily, policy.Weekly, policy.Monthly, policy.Yearly)
			if dryRun {
				fmt.Println("DRY RUN: No snapshots will be removed")
			}
		}

		removed, err := sr.ForgetSnapshots(policy, dryRun)
		if err != nil {
			return fmt.Errorf("failed to apply retention policy: %w", err)
		}

		outputFormat := getOutputFormat()
		if outputFormat == OutputJSON {
			outputJSON(map[string]any{
				"removed_count": len(removed),
				"removed":       removed,
				"dry_run":       dryRun,
				"policy":        policy,
			})
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

		return nil
	},
}

var snapshotRemoveCmd = &cobra.Command{
	Use:   "remove <snapshot-id>...",
	Short: "Remove specific snapshots by ID",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, dcfhDir, err := findDcfhRepo()
		if err != nil {
			return fmt.Errorf("failed to find dcfh repository: %w", err)
		}

		repo := dcfh.NewSnapshotRepository(dcfhDir)
		outputFormat := getOutputFormat()

		var results []map[string]any
		var successCount, errorCount int

		for _, snapshotID := range args {
			if flagDryRun {
				if flagVerbose >= 1 {
					outputMessage(fmt.Sprintf("Would remove snapshot: %s", snapshotID))
				}
				if outputFormat == OutputJSON {
					results = append(results, map[string]any{
						"snapshot_id": snapshotID,
						"action":      "would_remove",
						"success":     true,
					})
				}
				successCount++
			} else {
				if flagVerbose >= 1 {
					outputMessage(fmt.Sprintf("Removing snapshot: %s", snapshotID))
				}

				err := repo.RemoveSnapshot(snapshotID)
				if err != nil {
					if outputFormat == OutputJSON {
						results = append(results, map[string]any{
							"snapshot_id": snapshotID,
							"action":      "remove",
							"success":     false,
							"error":       err.Error(),
						})
					} else {
						outputError(fmt.Sprintf("Failed to remove snapshot %s: %v", snapshotID, err))
					}
					errorCount++
				} else {
					if outputFormat == OutputJSON {
						results = append(results, map[string]any{
							"snapshot_id": snapshotID,
							"action":      "remove",
							"success":     true,
						})
					}
					successCount++
				}
			}
		}

		if outputFormat == OutputJSON {
			output := map[string]any{
				"operation":     "remove",
				"dry_run":       flagDryRun,
				"total_count":   len(args),
				"success_count": successCount,
				"error_count":   errorCount,
				"results":       results,
			}
			outputJSON(output)
		} else {
			if flagDryRun {
				outputMessage(fmt.Sprintf("Would remove %d snapshot(s)", successCount))
			} else {
				if errorCount == 0 {
					outputMessage(fmt.Sprintf("Successfully removed %d snapshot(s)", successCount))
				} else {
					outputMessage(fmt.Sprintf("Removed %d snapshot(s), %d errors", successCount, errorCount))
					return fmt.Errorf("%d snapshot(s) failed to remove", errorCount)
				}
			}
		}

		return nil
	},
}

var snapshotStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Compare current state with snapshots",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("snapshot status not yet implemented")
	},
}

func init() {
	rootCmd.AddCommand(snapshotCmd)

	snapshotCmd.AddCommand(snapshotCreateCmd)
	snapshotCmd.AddCommand(snapshotListCmd)
	snapshotCmd.AddCommand(snapshotForgetCmd)
	snapshotCmd.AddCommand(snapshotRemoveCmd)
	snapshotCmd.AddCommand(snapshotStatusCmd)

	// Snapshot create flags
	snapshotCreateCmd.Flags().StringSlice("tag", nil, "tag to add to the snapshot")

	// Snapshot forget flags — scoped to this command, no conflict with global -w
	snapshotForgetCmd.Flags().IntVarP(&forgetKeepHourly, "keep-hourly", "H", 0, "number of hourly snapshots to keep")
	snapshotForgetCmd.Flags().IntVarP(&forgetKeepDaily, "keep-daily", "d", 0, "number of daily snapshots to keep")
	// No -w shorthand: global -w is hash-workers. Use --keep-weekly or the long form.
	snapshotForgetCmd.Flags().IntVar(&forgetKeepWeekly, "keep-weekly", 0, "number of weekly snapshots to keep")
	snapshotForgetCmd.Flags().IntVarP(&forgetKeepMonthly, "keep-monthly", "m", 0, "number of monthly snapshots to keep")
	snapshotForgetCmd.Flags().IntVarP(&forgetKeepYearly, "keep-yearly", "y", 0, "number of yearly snapshots to keep")
}
