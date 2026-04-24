package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
)

const (
	flagExclusive       = "exclusive"
	flagMinSize         = "min-size"
	flagMaxSize         = "max-size"
	flagStartDate       = "start-date"
	flagEndDate         = "end-date"
	flagTZ              = "tz"
	flagIgnoreHardlinks = "ignore-hardlinks"
)

var (
	dupesExclusive       = yesNoFlag(true)
	dupesMinSizeStr      string
	dupesMaxSizeStr      string
	dupesStartDateStr    string
	dupesEndDateStr      string
	dupesTZ              string
	dupesIgnoreHardlinks bool
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
duplicates that actually occupy extra storage.`,
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

		return nil
	},
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
	rootCmd.AddCommand(dupesCmd)
}

func buildDupeFilter(cmd *cobra.Command, paths []string) (dcfh.DupeFilter, error) {
	f := dcfh.DupeFilter{
		Paths:           paths,
		Exclusive:       bool(dupesExclusive),
		IgnoreHardlinks: dupesIgnoreHardlinks,
	}

	if cmd.Flags().Changed(flagMinSize) {
		n, err := parseSizeBound(dupesMinSizeStr)
		if err != nil {
			return f, fmt.Errorf("--%s: %w", flagMinSize, err)
		}
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
