package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
	"github.com/mattkeenan/dircachefilehash/pkg/fsdedupe"
)

// runFSDedupe is indirected via a package-level var so cmd-level
// tests can stub it without touching the real ioctl driver.
var runFSDedupe = fsdedupe.Run

const (
	flagExclusive       = "exclusive"
	flagMinSize         = "min-size"
	flagMaxSize         = "max-size"
	flagStartDate       = "start-date"
	flagEndDate         = "end-date"
	flagTZ              = "tz"
	flagIgnoreHardlinks = "ignore-hardlinks"
	flagFSDedupe        = "fs-dedupe"

	// dedupeDefaultMinSize: with --fs-dedupe set, filter out files
	// smaller than one block since dedup can reclaim nothing — they
	// already occupy a single (minimum) extent.
	dedupeDefaultMinSize uint64 = 4096
)

var (
	dupesExclusive       = yesNoFlag(true)
	dupesMinSizeStr      string
	dupesMaxSizeStr      string
	dupesStartDateStr    string
	dupesEndDateStr      string
	dupesTZ              string
	dupesIgnoreHardlinks bool
	dupesFSDedupe        bool
)

var dupesCmd = &cobra.Command{
	Use:   "dupes [paths...]",
	Short: "Find and display duplicate files",
	Long: `Find and display duplicate files in the repository.

Analyses the index to identify files with identical content (same hash)
but different paths. Groups duplicate files and shows file counts and
total duplicate space that could be reclaimed.

If one or more paths are given, results are restricted to those
subdirectories. With --exclusive=yes (the default) only groups whose
members are all inside the given paths are reported, matching the
behaviour of ` + "`fdupes -r sub/`" + `. With --exclusive=no, bucketing
spans the whole index and any group with at least one member inside
the given paths is reported.

File-level filters (--min-size, --max-size, --start-date, --end-date)
are applied before bucketing, so a group that loses members below the
≥2 threshold is never emitted. Sizes take binary suffixes (1K=1024,
1M=1024K, 1G, 1T). Dates accept partial ISO-8601 (YYYY, YYYY-MM,
YYYY-MM-DD, YYYY-MM-DDTHH[:MM[:SS]]) optionally suffixed with Z or
±hh[:mm]. --start-date is inclusive, --end-date is exclusive, so
--end-date 2027 includes all of 2026. Bare date-times are anchored in
--tz (an IANA zone) if set, otherwise the local zone (which honours
the TZ environment variable).

With -H / --ignore-hardlinks, entries that refer to the same underlying
inode (hardlinks to one on-disk file) collapse to a single representative
path inside each group. A group whose members are all hardlinks to one
inode therefore disappears — handy when you only care about content
duplicates that actually occupy extra storage.

With --fs-dedupe (Linux only), after listing duplicates dcfh asks the
kernel via FIDEDUPERANGE to share the underlying extents copy-on-write
on reflink-capable filesystems (btrfs, XFS with reflink=1, bcachefs).
Files remain independent byte-for-byte; a subsequent write triggers
COW. Combine with --dry-run to see the plan without changing anything.
--fs-dedupe implies --ignore-hardlinks (hardlinks already share blocks)
and defaults --min-size to 4096 (sub-block files buy nothing).`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		// Find the dcfh repository root
		repoRoot, metaDir, err := findDcfhRepo()
		if err != nil {
			if getOutputFormat() == OutputHuman {
				fmt.Fprintf(os.Stderr, "Run 'dcfh init <dir>' to initialise a repository\n")
			}
			return err
		}

		paths, err := normaliseDupePaths(repoRoot, args)
		if err != nil {
			return err
		}

		filter, err := buildDupeFilter(cmd, paths)
		if err != nil {
			return err
		}

		// Open existing repository via the Repo abstraction
		repo, err := dcfh.OpenRepo(ctx, metaDir)
		if err != nil {
			return fmt.Errorf("failed to open repository: %w", err)
		}
		defer func() { _ = repo.Close() }()

		duplicates, err := repo.Groups(ctx, dcfh.GroupsRequest{
			Options: buildOptions(),
			Filter:  filter,
		})
		if err != nil {
			return fmt.Errorf("failed to find duplicates: %w", err)
		}

		format := getOutputFormat()
		if len(duplicates) == 0 {
			if format == OutputJSON {
				output := DupesOutput{
					Repository:      repoRoot,
					DuplicateGroups: []dcfh.DuplicateGroup{},
					Summary: DuplicateSummary{
						GroupCount: 0,
						FileCount:  0,
					},
				}
				outputJSON(output)
			}
			return nil // No output if no duplicates found (like fdupes in text mode)
		}

		// duplicates arrive sorted: groups by hash, Files within each group
		// by path. pkg.FindDuplicates is authoritative for both orderings
		// (see pkg/dupes.go). Nothing to sort here.

		var dedupeResult *fsdedupe.Result
		if dupesFSDedupe {
			dedupeResult, err = runDedupe(ctx, repoRoot, duplicates)
			if err != nil {
				return err
			}
		}

		switch format {
		case OutputJSON:
			totalFiles := 0
			for _, group := range duplicates {
				totalFiles += len(group.Files)
			}
			outputJSON(DupesOutput{
				Repository:      repoRoot,
				DuplicateGroups: duplicates,
				Summary: DuplicateSummary{
					GroupCount: len(duplicates),
					FileCount:  totalFiles,
				},
				DedupeResult: dedupeResult,
			})

		case OutputFdupes:
			// fdupes format: absolute paths, one line per file, blank
			// line between groups. Joining a constant prefix preserves
			// sort order, so paths stay ordered without resorting.
			for i, group := range duplicates {
				for _, relPath := range group.Files {
					fmt.Println(filepath.Join(repoRoot, relPath))
				}
				if i < len(duplicates)-1 {
					fmt.Println()
				}
			}
		default: // OutputHuman
			for i, group := range duplicates {
				for _, relPath := range group.Files {
					fmt.Println(relPath)
				}
				if i < len(duplicates)-1 {
					fmt.Println()
				}
			}
		}

		if dedupeResult != nil && format != OutputJSON {
			printDedupeSummary(dedupeResult)
		}

		return nil
	},
}

// runDedupe adapts dupes-report groups into fsdedupe's input shape
// and dispatches to the platform-specific backend. Non-Linux builds
// surface as fsdedupe.ErrUnsupported with a platform tag; translate
// that into a clear stderr message before returning a non-zero exit.
func runDedupe(ctx context.Context, repoRoot string, duplicates []dcfh.DuplicateGroup) (*fsdedupe.Result, error) {
	groups := make([]fsdedupe.Group, len(duplicates))
	for i, g := range duplicates {
		groups[i] = fsdedupe.Group{Hash: g.Hash, Files: g.Files}
	}
	opts := fsdedupe.Options{
		DryRun:   flagDryRun,
		RepoRoot: repoRoot,
		Logf: func(format string, args ...any) {
			fmt.Fprintf(os.Stderr, format+"\n", args...)
		},
	}
	res, err := runFSDedupe(ctx, groups, opts)
	if errors.Is(err, fsdedupe.ErrUnsupported) {
		fmt.Fprintf(os.Stderr, "--%s: %v\n", flagFSDedupe, err)
		return nil, err
	}
	return res, err
}

// printDedupeSummary writes human-readable per-group outcomes and a
// totals line for human/fdupes output modes. JSON mode carries the
// full structure inside DupesOutput.DedupeResult so it's skipped here.
func printDedupeSummary(r *fsdedupe.Result) {
	fmt.Println()
	fmt.Println("fs-dedupe:")
	for _, g := range r.Groups {
		line := fmt.Sprintf("  %s  %s", g.Hash, g.Outcome)
		if g.BytesReclaimed > 0 {
			line += fmt.Sprintf("  %s", formatFileSize(int64(g.BytesReclaimed)))
		}
		if g.Reason != "" {
			line += fmt.Sprintf("  (%s)", g.Reason)
		}
		fmt.Println(line)
		for _, f := range g.Files {
			if f.Outcome == fsdedupe.OutcomeOK || f.Outcome == fsdedupe.OutcomePlanned {
				continue
			}
			fmt.Printf("    %s  %s", f.Path, f.Outcome)
			if f.Reason != "" {
				fmt.Printf("  (%s)", f.Reason)
			}
			fmt.Println()
		}
	}
	for _, dev := range r.UnsupportedDevs {
		fmt.Printf("  skipped device %s: filesystem does not support FIDEDUPERANGE\n", dev)
	}
	if flagDryRun {
		fmt.Printf("  total planned: %s across %d groups\n",
			formatFileSize(int64(r.TotalPlanned)), len(r.Groups))
	} else {
		fmt.Printf("  total reclaimed: %s across %d groups\n",
			formatFileSize(int64(r.TotalReclaimed)), len(r.Groups))
	}
}

func init() {
	dupesCmd.Flags().Var(&dupesExclusive, flagExclusive,
		"restrict results to groups fully inside the given paths (yes|no, default yes)")
	dupesCmd.Flags().StringVar(&dupesMinSizeStr, flagMinSize, "",
		"minimum file size (inclusive); binary suffixes K/M/G/T (e.g. 1K=1024)")
	dupesCmd.Flags().StringVar(&dupesMaxSizeStr, flagMaxSize, "",
		"maximum file size (inclusive); binary suffixes K/M/G/T")
	dupesCmd.Flags().StringVar(&dupesStartDateStr, flagStartDate, "",
		"minimum mtime (inclusive); partial ISO-8601, e.g. 2026 or 2026-01-01T00")
	dupesCmd.Flags().StringVar(&dupesEndDateStr, flagEndDate, "",
		"maximum mtime (exclusive); partial ISO-8601")
	dupesCmd.Flags().StringVar(&dupesTZ, flagTZ, "",
		"IANA timezone for bare date-times (default: $TZ or system local)")
	dupesCmd.Flags().BoolVarP(&dupesIgnoreHardlinks, flagIgnoreHardlinks, "H", false,
		"collapse hardlinks to the same inode to one entry per group")
	dupesCmd.Flags().BoolVar(&dupesFSDedupe, flagFSDedupe, false,
		"reclaim disk blocks from duplicates via FIDEDUPERANGE (Linux only; combine with --dry-run to see the plan without changing anything)")
	rootCmd.AddCommand(dupesCmd)
}

func buildDupeFilter(cmd *cobra.Command, paths []string) (dcfh.DupeFilter, error) {
	f := dcfh.DupeFilter{
		Paths:           paths,
		Exclusive:       bool(dupesExclusive),
		IgnoreHardlinks: dupesIgnoreHardlinks,
	}

	// --fs-dedupe implies --ignore-hardlinks: the kernel rejects
	// same-inode overlapping-range calls anyway, and reporting
	// hardlinks as "deduped" would be noise.
	if dupesFSDedupe {
		f.IgnoreHardlinks = true
	}

	if cmd.Flags().Changed(flagMinSize) {
		n, err := parseSizeBound(dupesMinSizeStr)
		if err != nil {
			return f, fmt.Errorf("--%s: %w", flagMinSize, err)
		}
		f.MinSize = &n
	} else if dupesFSDedupe {
		// Sub-block files already occupy a single minimum extent;
		// deduping them wastes ioctls and reclaims nothing.
		n := dedupeDefaultMinSize
		f.MinSize = &n
	}
	if cmd.Flags().Changed(flagMaxSize) {
		n, err := parseSizeBound(dupesMaxSizeStr)
		if err != nil {
			return f, fmt.Errorf("--%s: %w", flagMaxSize, err)
		}
		f.MaxSize = &n
	}

	var zone *time.Location
	if cmd.Flags().Changed(flagStartDate) || cmd.Flags().Changed(flagEndDate) {
		z, err := resolveZone(dupesTZ)
		if err != nil {
			return f, err
		}
		zone = z
	}
	if cmd.Flags().Changed(flagStartDate) {
		t, err := parsePartialDateTime(dupesStartDateStr, zone)
		if err != nil {
			return f, fmt.Errorf("--%s: %w", flagStartDate, err)
		}
		f.StartTime = t
	}
	if cmd.Flags().Changed(flagEndDate) {
		t, err := parsePartialDateTime(dupesEndDateStr, zone)
		if err != nil {
			return f, fmt.Errorf("--%s: %w", flagEndDate, err)
		}
		f.EndTime = t
	}

	if f.MinSize != nil && f.MaxSize != nil && *f.MinSize > *f.MaxSize {
		return f, fmt.Errorf("--min-size (%d) exceeds --max-size (%d)", *f.MinSize, *f.MaxSize)
	}
	if !f.StartTime.IsZero() && !f.EndTime.IsZero() && !f.StartTime.Before(f.EndTime) {
		return f, fmt.Errorf("--start-date (%s) is not before --end-date (%s)",
			f.StartTime.Format(time.RFC3339), f.EndTime.Format(time.RFC3339))
	}
	return f, nil
}

// yesNoFlag is a cobra/pflag bool-shaped flag that accepts only "yes"
// or "no". We use this rather than a plain BoolVar so misuse like
// `--exclusive=bogus` fails fast with a clear error, and so the
// command help documents the exact token set.
type yesNoFlag bool

func (f *yesNoFlag) String() string {
	if *f {
		return "yes"
	}
	return "no"
}

func (f *yesNoFlag) Set(s string) error {
	switch s {
	case "yes":
		*f = true
	case "no":
		*f = false
	default:
		return fmt.Errorf("must be \"yes\" or \"no\"")
	}
	return nil
}

func (f *yesNoFlag) Type() string { return "yes|no" }

// normaliseDupePaths converts user-supplied positional args to
// repo-relative directory prefixes suitable for FindDuplicates path
// filtering. Each result is forward-slash and terminated with "/".
// Args that collapse to "." (i.e. the repo root) are dropped — an
// empty result means "whole repo" which is the fast path. An arg that
// escapes the repo is a fatal error.
func normaliseDupePaths(repoRoot string, args []string) ([]string, error) {
	if len(args) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(args))
	for _, arg := range args {
		abs, err := filepath.Abs(arg)
		if err != nil {
			return nil, fmt.Errorf("resolve %q: %w", arg, err)
		}
		rel, err := filepath.Rel(repoRoot, abs)
		if err != nil {
			return nil, fmt.Errorf("resolve %q: %w", arg, err)
		}
		rel = filepath.ToSlash(filepath.Clean(rel))
		if rel == ".." || strings.HasPrefix(rel, "../") {
			return nil, fmt.Errorf("path %q is outside repository %q", arg, repoRoot)
		}
		if rel == "." {
			// Whole-repo arg: drop so the fast path kicks in.
			return nil, nil
		}
		out = append(out, rel+"/")
	}
	return out, nil
}
