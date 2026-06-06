package dircachefilehash

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// writeFile writes content to root/rel, creating parent dirs.
func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", rel, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

// newTestRepo creates an initialised local repo rooted at a fresh temp
// dir and returns it plus the root path.
func newTestRepo(t *testing.T) (*localRepo, string) {
	t.Helper()
	root := t.TempDir()
	ms := NewMetaStore(root, root)
	return newLocalRepo(ms), root
}

// TC-11 (KD3 enrichment): Apply with CollectChanges records the
// op-classified added/modified/deleted paths, including the full-update
// delete case (the deleted file is dropped from main but still reported).
func TestApply_CollectChanges(t *testing.T) {
	repo, root := newTestRepo(t)
	defer func() { _ = repo.Close() }()
	ctx := context.Background()

	// Baseline: index three files.
	writeFile(t, root, "keep.txt", "unchanged content\n")
	writeFile(t, root, "mod.txt", "original\n")
	writeFile(t, root, "sub/gone.txt", "doomed\n")
	if _, err := repo.Apply(ctx, ApplyRequest{}); err != nil {
		t.Fatalf("baseline apply: %v", err)
	}

	// Mutate: modify one (different length), add one, delete one.
	writeFile(t, root, "mod.txt", "changed to a longer string\n")
	writeFile(t, root, "added.txt", "brand new\n")
	if err := os.Remove(filepath.Join(root, "sub", "gone.txt")); err != nil {
		t.Fatalf("remove gone.txt: %v", err)
	}

	res, err := repo.Apply(ctx, ApplyRequest{CollectChanges: true})
	if err != nil {
		t.Fatalf("collecting apply: %v", err)
	}

	if !slices.Contains(res.Modified, "mod.txt") {
		t.Errorf("Modified = %v, want it to contain mod.txt", res.Modified)
	}
	if !slices.Contains(res.Added, "added.txt") {
		t.Errorf("Added = %v, want it to contain added.txt", res.Added)
	}
	if !slices.Contains(res.Deleted, "sub/gone.txt") {
		t.Errorf("Deleted = %v, want it to contain sub/gone.txt", res.Deleted)
	}
	// keep.txt must not appear in any change set.
	for _, set := range [][]string{res.Added, res.Modified, res.Deleted} {
		if slices.Contains(set, "keep.txt") {
			t.Errorf("keep.txt wrongly reported as changed: %v", set)
		}
	}
}

// With CollectChanges off, the change-set fields stay nil (the default,
// non-interactive path).
func TestApply_NoCollectByDefault(t *testing.T) {
	repo, root := newTestRepo(t)
	defer func() { _ = repo.Close() }()
	ctx := context.Background()

	writeFile(t, root, "a.txt", "alpha\n")
	res, err := repo.Apply(ctx, ApplyRequest{})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.Added != nil || res.Modified != nil || res.Deleted != nil {
		t.Errorf("change-set fields should be nil when CollectChanges is off, got A=%v M=%v D=%v",
			res.Added, res.Modified, res.Deleted)
	}
}

// TC-17 (integrity): the collector must not perturb the on-disk index.
// With no filesystem change between runs, a collect-off update and a
// collect-on update must produce byte-identical main.idx.
func TestApply_CollectChangesByteIdentical(t *testing.T) {
	repo, root := newTestRepo(t)
	defer func() { _ = repo.Close() }()
	ctx := context.Background()

	writeFile(t, root, "a.txt", "alpha\n")
	writeFile(t, root, "dir/b.txt", "beta\n")
	if _, err := repo.Apply(ctx, ApplyRequest{}); err != nil {
		t.Fatalf("seed apply: %v", err)
	}

	// Re-run with collect OFF (no disk change) and capture the bytes.
	if _, err := repo.Apply(ctx, ApplyRequest{}); err != nil {
		t.Fatalf("collect-off apply: %v", err)
	}
	off, err := os.ReadFile(repo.ms.IndexFile)
	if err != nil {
		t.Fatalf("read index after collect-off: %v", err)
	}

	// Re-run with collect ON (still no disk change) and compare.
	if _, err := repo.Apply(ctx, ApplyRequest{CollectChanges: true}); err != nil {
		t.Fatalf("collect-on apply: %v", err)
	}
	on, err := os.ReadFile(repo.ms.IndexFile)
	if err != nil {
		t.Fatalf("read index after collect-on: %v", err)
	}

	if !slices.Equal(off, on) {
		t.Errorf("main.idx differs between collect-off (%d bytes) and collect-on (%d bytes); collector perturbed serialisation",
			len(off), len(on))
	}
}

// PostRunTree end-to-end: after a full update that deleted a file, the
// tree (built from the post-run merged index + the enriched ChangeSet)
// surfaces the deletion via the union path and aggregates live counts.
func TestPostRunTree_EndToEnd(t *testing.T) {
	repo, root := newTestRepo(t)
	defer func() { _ = repo.Close() }()
	ctx := context.Background()

	writeFile(t, root, "live1.txt", "one\n")
	writeFile(t, root, "sub/live2.txt", "two\n")
	writeFile(t, root, "sub/gone.txt", "bye\n")
	if _, err := repo.Apply(ctx, ApplyRequest{}); err != nil {
		t.Fatalf("baseline apply: %v", err)
	}

	if err := os.Remove(filepath.Join(root, "sub", "gone.txt")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	res, err := repo.Apply(ctx, ApplyRequest{CollectChanges: true})
	if err != nil {
		t.Fatalf("collecting apply: %v", err)
	}

	tree, err := repo.PostRunTree(ctx, ChangeSet{
		Added:    res.Added,
		Modified: res.Modified,
		Deleted:  res.Deleted,
	})
	if err != nil {
		t.Fatalf("PostRunTree: %v", err)
	}

	// Two live files remain; one deletion is unioned in.
	if tree.Root.Stats.Files != 2 {
		t.Errorf("root live Files = %d, want 2", tree.Root.Stats.Files)
	}
	if tree.Root.Stats.Deleted != 1 {
		t.Errorf("root Deleted = %d, want 1 (gone.txt via union)", tree.Root.Stats.Deleted)
	}
	gone := descend(t, tree.Root, "sub/gone.txt")
	if gone.Cat != Deleted {
		t.Errorf("sub/gone.txt Cat = %v, want Deleted", gone.Cat)
	}
}
