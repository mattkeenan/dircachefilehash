package dircachefilehash

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// dupesBaselineBudget caps how long a single FindDuplicates call is
// allowed to run before the benchmark skips the case with a clear
// "baseline exceeded budget" message. Overridable via
// DCFH_DUPES_BENCH_BUDGET (duration string). Post-fix all three
// shipped sizes sit well under the cap; the ceiling exists so a
// future regression (or a re-run against the pre-commit-B impl) is
// caught loudly instead of wedging CI.
const dupesBaselineBudget = 60 * time.Second

// dupesFixtureConfig parametrises the benchmark. The fixture produces
// DupeGroupCount distinct duplicate groups each containing FilesPerGroup
// files, plus Singletons unique files, for a total of
// DupeGroupCount*FilesPerGroup + Singletons files. Group count is the
// dimension the current quadratic CLI bubble sort is sensitive to; the
// fixture is designed to exercise it, not just produce one giant group.
type dupesFixtureConfig struct {
	Name           string
	DupeGroupCount int
	FilesPerGroup  int
	Singletons     int
	FileSize       int64
}

func (c dupesFixtureConfig) total() int {
	return c.DupeGroupCount*c.FilesPerGroup + c.Singletons
}

var (
	// Micro: ~1k files, ~400 small duplicate groups. Small enough to
	// complete even on the pre-fix quadratic path in < 1 s.
	dupesFixtureMicro = dupesFixtureConfig{
		Name: "Micro", DupeGroupCount: 400, FilesPerGroup: 2, Singletons: 200, FileSize: 4 * 1024,
	}
	// Small: ~10k files, ~4k groups. Pre-fix: tens of seconds of
	// quadratic work on the group-level bubble sort.
	dupesFixtureSmall = dupesFixtureConfig{
		Name: "Small", DupeGroupCount: 4000, FilesPerGroup: 2, Singletons: 2000, FileSize: 4 * 1024,
	}
	// Medium: ~100k files, ~40k groups. Pre-fix: budget-exceeded skip
	// is expected — this case is the headline motivation for the fix.
	dupesFixtureMedium = dupesFixtureConfig{
		Name: "Medium", DupeGroupCount: 40000, FilesPerGroup: 2, Singletons: 20000, FileSize: 4 * 1024,
	}
)

// buildDupesFixture writes a flat tree under root with the shape
// described by cfg. Group g uses seed g+1 (so every group has distinct
// content); each of the FilesPerGroup files in the group shares the
// same bytes. Singletons come after, seeded from an offset that doesn't
// collide with group seeds. Flat layout keeps fixture setup cheap —
// directory structure is not what's under test.
func buildDupesFixture(tb testing.TB, root string, cfg dupesFixtureConfig) {
	tb.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		tb.Fatalf("mkdir: %v", err)
	}
	// Write duplicate groups: DupeGroupCount groups × FilesPerGroup files.
	fileIdx := 0
	for g := range cfg.DupeGroupCount {
		data := generateDeterministicData(cfg.FileSize, int64(g+1))
		for k := range cfg.FilesPerGroup {
			name := fmt.Sprintf("d%06d_%02d.dat", g, k)
			if err := os.WriteFile(filepath.Join(root, name), data, 0o644); err != nil {
				tb.Fatalf("write %s: %v", name, err)
			}
			fileIdx++
		}
	}
	// Write singletons with seeds past the group-seed range.
	base := int64(cfg.DupeGroupCount + 1_000_000)
	for s := range cfg.Singletons {
		name := fmt.Sprintf("s%06d.dat", s)
		data := generateDeterministicData(cfg.FileSize, base+int64(s))
		if err := os.WriteFile(filepath.Join(root, name), data, 0o644); err != nil {
			tb.Fatalf("write %s: %v", name, err)
		}
	}
}

// primeDupesFixture builds the fixture (if missing) and indexes it.
// Returns (datasetDir, metaDir). The expensive parts — file creation
// and the first Update — run once per benchmark invocation, outside
// b.N.
func primeDupesFixture(tb testing.TB, cfg dupesFixtureConfig) (string, string) {
	tb.Helper()
	tempDir := tb.TempDir()
	datasetDir := filepath.Join(tempDir, "dataset")
	metaDir := filepath.Join(tempDir, "dcfh")

	buildDupesFixture(tb, datasetDir, cfg)

	cache := NewDirectoryCache(datasetDir, metaDir)
	if err := cache.Update(context.Background(), map[string]string{}); err != nil {
		tb.Fatalf("initial update: %v", err)
	}
	_ = cache.Close()
	return datasetDir, metaDir
}

// budgetFromEnv parses DCFH_DUPES_BENCH_BUDGET (a Go duration string)
// or returns dupesBaselineBudget if unset / invalid.
func budgetFromEnv() time.Duration {
	raw := os.Getenv("DCFH_DUPES_BENCH_BUDGET")
	if raw == "" {
		return dupesBaselineBudget
	}
	if d, err := time.ParseDuration(raw); err == nil && d > 0 {
		return d
	}
	return dupesBaselineBudget
}

// runDupesBench is the shared body of every BenchmarkFindDuplicates*
// variant. The first iteration is wrapped in a wall-clock budget so we
// skip early if the baseline would take longer than a user's patience
// to run even once.
func runDupesBench(b *testing.B, cfg dupesFixtureConfig) {
	b.Logf("fixture: %d files (%d groups × %d + %d singletons), %d bytes/file",
		cfg.total(), cfg.DupeGroupCount, cfg.FilesPerGroup, cfg.Singletons, cfg.FileSize)

	b.StopTimer()
	datasetDir, metaDir := primeDupesFixture(b, cfg)
	budget := budgetFromEnv()

	openCache := func() *DirectoryCache {
		dc, err := OpenDirectoryCache(datasetDir, metaDir)
		if err != nil {
			b.Fatalf("open cache: %v", err)
		}
		return dc
	}

	// Warm-up / budget check: run one iteration with a timeout; if it
	// blows past the budget we skip rather than letting go test spend
	// hours per b.N.
	warmCache := openCache()
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	start := time.Now()
	groups, err := warmCache.FindDuplicates(ctx, map[string]string{}, nil, true)
	elapsed := time.Since(start)
	cancel()
	_ = warmCache.Close()
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		b.Fatalf("warmup FindDuplicates: %v", err)
	}
	if errors.Is(err, context.DeadlineExceeded) || elapsed >= budget {
		b.Skipf("baseline exceeded %v at %d files, %d duplicate groups — this is the reason for the fix",
			budget, cfg.total(), cfg.DupeGroupCount)
	}
	b.Logf("warmup: %v, %d duplicate groups", elapsed, len(groups))
	b.StartTimer()

	for range b.N {
		cache := openCache()
		if _, err := cache.FindDuplicates(context.Background(), map[string]string{}, nil, true); err != nil {
			b.Fatalf("FindDuplicates: %v", err)
		}
		_ = cache.Close()
	}
}

// BenchmarkFindDuplicatesMicro benchmarks dupes on a 1k-file fixture —
// small enough to complete even on the pre-fix impl.
func BenchmarkFindDuplicatesMicro(b *testing.B) {
	runDupesBench(b, dupesFixtureMicro)
}

// BenchmarkFindDuplicatesSmall benchmarks dupes on a 10k-file fixture.
// Pre-fix may start to feel slow here.
func BenchmarkFindDuplicatesSmall(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping small dupes benchmark in short mode")
	}
	runDupesBench(b, dupesFixtureSmall)
}

// BenchmarkFindDuplicatesMedium benchmarks dupes on a 100k-file fixture.
// Pre-fix almost certainly blows the 60 s budget and is skipped —
// that's the signal we're fixing.
func BenchmarkFindDuplicatesMedium(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping medium dupes benchmark in short mode")
	}
	runDupesBench(b, dupesFixtureMedium)
}
