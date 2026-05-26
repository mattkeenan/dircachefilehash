package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	dcfh "github.com/mattkeenan/dircachefilehash/pkg"
	"github.com/mattkeenan/dircachefilehash/pkg/fsdedupe"
)

// runFSDedupe is indirected via a package-level var so cmd-level
// tests can stub it without touching the real ioctl driver.
var runFSDedupe = fsdedupe.Run

const (
	flagExclusive       = "exclusive"
	flagIgnoreHardlinks = "ignore-hardlinks"
	flagFSDedupe        = "fs-dedupe"

	// dedupeDefaultMinSize: with --fs-dedupe set, filter out files
	// smaller than one block since dedup can reclaim nothing — they
	// already occupy a single (minimum) extent.
	dedupeDefaultMinSize uint64 = 4096
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

Filter flags compose via the scope-marker syntax (see
` + "`dcfh status --help`" + ` for the full grammar and gitignore-pattern
note). File-level filters apply before bucketing, so a group that
loses members below the ≥2 threshold is never emitted:

  dcfh dupes --min-size 1M                          — duplicates ≥ 1 MiB
  dcfh dupes --print --min-size 1M --ignore --name '*.bak'
                                                    — exclude .bak files
  dcfh dupes --no-ignore-file                       — bypass .dcfh/ignore

Sizes take binary suffixes (1K=1024, 1M=1024K, 1G, 1T). Dates accept
partial ISO-8601 (YYYY, YYYY-MM, YYYY-MM-DD, YYYY-MM-DDTHH[:MM[:SS]])
optionally suffixed with Z or ±hh[:mm]. --start-date is inclusive,
--end-date is exclusive. Bare date-times are anchored in --tz (an IANA
zone) if set, otherwise the local zone (which honours the TZ env var).

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
	Args:               cobra.ArbitraryArgs,
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		state, prints, ignores, positionals, noIgnoreFile, err := resolveScopes(args, cmdDupes)
		if err != nil {
			return err
		}
		if err := finaliseRootFlags(cmd); err != nil {
			return err
		}

		repoRoot, metaDir, err := findDcfhRepo()
		if err != nil {
			if getOutputFormat() == OutputHuman {
				fmt.Fprintf(os.Stderr, "Run 'dcfh init <dir>' to initialise a repository\n")
			}
			return err
		}

		paths, err := normaliseDupePaths(repoRoot, positionals)
		if err != nil {
			return err
		}

		filter, err := buildDupeFilter(state, prints, ignores, paths, noIgnoreFile)
		if err != nil {
			return err
		}

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

		// JSON mode is batch: dedupe (if requested) then emit one
		// structured object. Non-JSON modes stream per-group output via
		// the OnGroup callback below.
		if format == OutputJSON {
			var dedupeResult *fsdedupe.Result
			if state.fsDedupe {
				dedupeResult, err = runDedupe(ctx, repoRoot, duplicates, nil)
				if err != nil {
					return err
				}
			}
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
			return nil
		}

		// Per-group printer shared between the streaming dedupe path
		// and the plain listing path. Format choice is decided once
		// here, not per file.
		isFdupes := format == OutputFdupes
		printedGroups := 0
		printGroupListing := func(g dcfh.DuplicateGroup) {
			if printedGroups > 0 {
				fmt.Println()
			}
			printedGroups++
			for _, relPath := range g.Files {
				if isFdupes {
					fmt.Println(filepath.Join(repoRoot, relPath))
				} else {
					fmt.Println(relPath)
				}
			}
		}

		var dedupeResult *fsdedupe.Result
		if state.fsDedupe {
			// Recover the index-side group ordering inside the
			// callback: fsdedupe.GroupResult carries only the hash and
			// per-file outcomes, not the original sorted file list.
			groupsByHash := make(map[string]dcfh.DuplicateGroup, len(duplicates))
			for _, g := range duplicates {
				groupsByHash[g.Hash] = g
			}
			onGroup := func(gr fsdedupe.GroupResult) {
				printGroupListing(groupsByHash[gr.Hash])
				printGroupOutcome(gr)
			}
			dedupeResult, err = runDedupe(ctx, repoRoot, duplicates, onGroup)
			if err != nil {
				return err
			}
		} else {
			for _, g := range duplicates {
				printGroupListing(g)
			}
		}

		if dedupeResult != nil {
			fmt.Println()
			printDedupeFooter(dedupeResult)
		}

		return nil
	},
}

// runDedupe adapts dupes-report groups into fsdedupe's input shape
// and dispatches to the platform-specific backend. Non-Linux builds
// surface as fsdedupe.ErrUnsupported with a platform tag; translate
// that into a clear stderr message before returning a non-zero exit.
// onGroup, if non-nil, fires once per group as soon as fsdedupe
// finishes that group — used by the streaming output path.
func runDedupe(ctx context.Context, repoRoot string, duplicates []dcfh.DuplicateGroup, onGroup func(fsdedupe.GroupResult)) (*fsdedupe.Result, error) {
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
		OnGroup: onGroup,
	}
	res, err := runFSDedupe(ctx, groups, opts)
	if errors.Is(err, fsdedupe.ErrUnsupported) {
		fmt.Fprintf(os.Stderr, "--%s: %v\n", flagFSDedupe, err)
		return nil, err
	}
	return res, err
}

// printGroupOutcome renders a single GroupResult inline, immediately
// under its file listing. It writes one summary line per group plus
// one indented line per non-OK / non-Planned file.
func printGroupOutcome(g fsdedupe.GroupResult) {
	line := fmt.Sprintf("  → fs-dedupe: %s", g.Outcome)
	if g.BytesReclaimed > 0 {
		verb := "reclaimed"
		if flagDryRun {
			verb = "planned"
		}
		line += fmt.Sprintf(", %s %s", formatFileSize(int64(g.BytesReclaimed)), verb) //nolint:gosec // G115: byte total, bounded by storage size (<< int64 max)
	}
	if g.Reason != "" {
		line += fmt.Sprintf(" (%s)", g.Reason)
	}
	fmt.Println(line)
	for _, f := range g.Files {
		if f.Outcome == fsdedupe.OutcomeOK || f.Outcome == fsdedupe.OutcomePlanned {
			continue
		}
		detail := fmt.Sprintf("      %s: %s", f.Path, f.Outcome)
		if f.Reason != "" {
			detail += fmt.Sprintf(" — %s", f.Reason)
		}
		fmt.Println(detail)
	}
}

// printDedupeFooter writes the once-per-run trailing totals line and
// any UnsupportedDevs entries. Per-group output happens inline via
// printGroupOutcome from the OnGroup callback; this helper covers the
// strictly run-global view.
func printDedupeFooter(r *fsdedupe.Result) {
	for _, dev := range r.UnsupportedDevs {
		fmt.Printf("skipped device %s: filesystem does not support FIDEDUPERANGE\n", dev)
	}
	if flagDryRun {
		fmt.Printf("total planned: %s across %d groups\n",
			formatFileSize(int64(r.TotalPlanned)), len(r.Groups)) //nolint:gosec // G115: byte total, bounded by storage size (<< int64 max)
	} else {
		fmt.Printf("total reclaimed: %s across %d groups\n",
			formatFileSize(int64(r.TotalReclaimed)), len(r.Groups)) //nolint:gosec // G115: byte total, bounded by storage size (<< int64 max)
	}
}

func init() {
	registerHelpFlags(dupesCmd.Flags(), cmdDupes)
	rootCmd.AddCommand(dupesCmd)
}

// buildDupeFilter assembles a DupeFilter from the parsed segment-zero
// state and the per-segment FilterOptions slices. --fs-dedupe forces
// IgnoreHardlinks=true (the kernel rejects same-inode overlapping-range
// calls anyway, and reporting hardlinks as "deduped" would be noise)
// and adds a print-segment MinSize=4096 floor when no print segment
// already constrains MinSize (sub-block files reclaim nothing).
func buildDupeFilter(state *filterFlagsState, prints, ignores []dcfh.FilterOptions, paths []string, noIgnoreFile bool) (dcfh.DupeFilter, error) {
	f := dcfh.DupeFilter{
		Paths:           paths,
		Exclusive:       bool(state.exclusive),
		IgnoreHardlinks: state.ignoreHardlinks,
		NoIgnoreFile:    noIgnoreFile,
	}
	if state.fsDedupe {
		f.IgnoreHardlinks = true
		prints = applyFSDedupeDefaults(prints)
	}
	pred, err := dcfh.BuildPrintIgnoreTree(prints, ignores)
	if err != nil {
		return f, err
	}
	f.Predicate = pred
	return f, nil
}

// applyFSDedupeDefaults appends a synthetic print segment with
// MinSize=dedupeDefaultMinSize when no caller segment has already
// set MinSize. Returns prints unchanged if any segment constrains
// MinSize.
func applyFSDedupeDefaults(prints []dcfh.FilterOptions) []dcfh.FilterOptions {
	for _, p := range prints {
		if p.MinSize != nil {
			return prints
		}
	}
	floor := dedupeDefaultMinSize
	return append(prints, dcfh.FilterOptions{MinSize: &floor})
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
