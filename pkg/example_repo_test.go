package dircachefilehash_test

// Runnable, godoc-rendered usage examples for the Repo library surface.
//
// These live in the external test package (dircachefilehash_test) and import
// the package under its consumer alias, so each example exercises only the
// exported API and renders in godoc exactly as a downstream consumer would
// write it. Examples carrying a trailing "// Output:" block are executed by
// `go test` and their stdout is asserted byte-for-byte; the rest are
// compile-checked against the real surface, so signature drift cannot land
// silently.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	dircachefilehash "github.com/mattkeenan/dircachefilehash/pkg"
)

// mustExampleRepo creates a throwaway repository in a fresh temp directory,
// seeds it with a few literal-named files, and returns the open Repo plus a
// cleanup function. cleanup closes the repo then removes the temp tree (in
// that order); each example defers it. It panics on any setup error —
// acceptable in example scaffolding, which has no *testing.T to fail on.
//
// The two .go files share identical content, so they form exactly one
// duplicate group; readme.txt is distinct. The names are literal constants —
// no variable- or environment-derived paths reach os.WriteFile.
func mustExampleRepo() (repo dircachefilehash.Repo, cleanup func()) {
	dir, err := os.MkdirTemp("", "dcfh-example-")
	if err != nil {
		panic(err)
	}
	seed := map[string]string{
		"alpha.go":   "package main\n",
		"beta.go":    "package main\n",
		"readme.txt": "hello\n",
	}
	for name, body := range seed {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			_ = os.RemoveAll(dir)
			panic(err)
		}
	}
	repo, err = dircachefilehash.CreateRepo(context.Background(), dir, "")
	if err != nil {
		_ = os.RemoveAll(dir)
		panic(err)
	}
	cleanup = func() {
		_ = repo.Close()
		_ = os.RemoveAll(dir)
	}
	return repo, cleanup
}

// ExampleCreateRepo creates a new repository on disk and reads its stats. A
// freshly created repository has an empty index until Apply records the
// current filesystem state.
func ExampleCreateRepo() {
	repo, cleanup := mustExampleRepo()
	defer cleanup()

	stats, err := repo.Stats(context.Background())
	if err != nil {
		panic(err)
	}
	fmt.Println("indexed files:", stats.FileCount)
	// Output:
	// indexed files: 0
}

// ExampleOpenRepo reopens an existing repository by pointing at its .dcfh
// metadata directory. Close releases the index handles; OpenRepo acquires a
// fresh session against the same on-disk state.
func ExampleOpenRepo() {
	dir, err := os.MkdirTemp("", "dcfh-example-")
	if err != nil {
		panic(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	ctx := context.Background()

	repo, err := dircachefilehash.CreateRepo(ctx, dir, "")
	if err != nil {
		panic(err)
	}
	metaDir := filepath.Join(dir, ".dcfh")
	if err := repo.Close(); err != nil {
		panic(err)
	}

	reopened, err := dircachefilehash.OpenRepo(ctx, metaDir)
	if err != nil {
		panic(err)
	}
	defer func() { _ = reopened.Close() }()

	info, err := reopened.Info(ctx)
	if err != nil {
		panic(err)
	}
	fmt.Println("reopened:", filepath.Base(info.MetaDir))
	// Output:
	// reopened: .dcfh
}

// ExampleRepo_Diff reports a structured delta between the index and the
// filesystem (the primitive behind `dcfh status`). Nothing has been Applied,
// so every seeded file is reported as Added.
func ExampleRepo_Diff() {
	repo, cleanup := mustExampleRepo()
	defer cleanup()

	status, err := repo.Diff(context.Background(), dircachefilehash.DiffRequest{})
	if err != nil {
		panic(err)
	}
	fmt.Println("added:", len(status.Added))
	// Output:
	// added: 3
}

// ExampleRepo_Apply records the current filesystem state into the index (the
// primitive behind `dcfh update`) and reports how many files were indexed.
func ExampleRepo_Apply() {
	repo, cleanup := mustExampleRepo()
	defer cleanup()

	result, err := repo.Apply(context.Background(), dircachefilehash.ApplyRequest{})
	if err != nil {
		panic(err)
	}
	fmt.Println("indexed files:", result.FileCount)
	// Output:
	// indexed files: 3
}

// ExampleRepo_Groups finds duplicate files (the primitive behind `dcfh
// dupes`). Groups reads the index rather than the filesystem, so the index
// must be populated with Apply first. alpha.go and beta.go share content and
// form one duplicate group.
func ExampleRepo_Groups() {
	repo, cleanup := mustExampleRepo()
	defer cleanup()

	ctx := context.Background()
	if _, err := repo.Apply(ctx, dircachefilehash.ApplyRequest{}); err != nil {
		panic(err)
	}

	groups, err := repo.Groups(ctx, dircachefilehash.GroupsRequest{})
	if err != nil {
		panic(err)
	}
	fmt.Println("duplicate groups:", len(groups))
	// Output:
	// duplicate groups: 1
}

// ExampleRepo_Filter searches an index with a predicate and runs actions on
// each match (the primitive behind `dcfhfind`). Filter reads the index, so
// Apply must run first. PrintAction writes each matched path to stdout in the
// index's path-sorted order; Actions must be non-empty.
func ExampleRepo_Filter() {
	repo, cleanup := mustExampleRepo()
	defer cleanup()

	ctx := context.Background()
	if _, err := repo.Apply(ctx, dircachefilehash.ApplyRequest{}); err != nil {
		panic(err)
	}

	result, err := repo.Filter(ctx, dircachefilehash.FilterRequest{
		IndexSelectors: []string{"main"},
		Expression:     dircachefilehash.MustNewNameTest("*.go", false),
		Actions:        []dircachefilehash.FilterAction{&dircachefilehash.PrintAction{}},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println("matched:", result.EntriesMatched)
	// Output:
	// alpha.go
	// beta.go
	// matched: 2
}

// ExampleRepo_Config reads and writes repository configuration. Get returns
// the full resolved config; Set validates and persists a single key.
func ExampleRepo_Config() {
	repo, cleanup := mustExampleRepo()
	defer cleanup()

	ctx := context.Background()
	cfg := repo.Config()

	all, err := cfg.Get(ctx)
	if err != nil {
		panic(err)
	}
	fmt.Println("default hash:", all.Hash.Default)

	if err := cfg.Set(ctx, "output.format", "json"); err != nil {
		panic(err)
	}
	all, err = cfg.Get(ctx)
	if err != nil {
		panic(err)
	}
	fmt.Println("output format:", all.Output.Format)
	// Output:
	// default hash: sha256
	// output format: json
}

// ExampleRepo_Snapshots captures and lists index-state snapshots. Snapshot
// IDs are timestamp-based and therefore non-deterministic, so the example
// asserts the count rather than the ID.
func ExampleRepo_Snapshots() {
	repo, cleanup := mustExampleRepo()
	defer cleanup()

	ctx := context.Background()
	if _, err := repo.Apply(ctx, dircachefilehash.ApplyRequest{}); err != nil {
		panic(err)
	}

	snaps := repo.Snapshots()
	if _, err := snaps.Create(ctx, []string{"example"}); err != nil {
		panic(err)
	}
	list, err := snaps.List(ctx)
	if err != nil {
		panic(err)
	}
	fmt.Println("snapshots:", len(list))
	// Output:
	// snapshots: 1
}
