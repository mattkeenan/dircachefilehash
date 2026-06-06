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

// TC-5 (FR4/AC3): Apply with CollectChanges records the deleted file's
// last-known size on UpdateResult.DeletedSizes (the update path's source
// for deleted bytes, since the entry is gone after the rename).
func TestApply_CollectChangesDeletedSizes(t *testing.T) {
	repo, root := newTestRepo(t)
	defer func() { _ = repo.Close() }()
	ctx := context.Background()

	const goneContent = "doomed content of a known length\n"
	writeFile(t, root, "keep.txt", "stays\n")
	writeFile(t, root, "sub/gone.txt", goneContent)
	if _, err := repo.Apply(ctx, ApplyRequest{}); err != nil {
		t.Fatalf("baseline apply: %v", err)
	}

	if err := os.Remove(filepath.Join(root, "sub", "gone.txt")); err != nil {
		t.Fatalf("remove gone.txt: %v", err)
	}
	res, err := repo.Apply(ctx, ApplyRequest{CollectChanges: true})
	if err != nil {
		t.Fatalf("collecting apply: %v", err)
	}

	want := int64(len(goneContent))
	if got := res.DeletedSizes["sub/gone.txt"]; got != want {
		t.Errorf("DeletedSizes[sub/gone.txt] = %d, want %d", got, want)
	}
	// The change-set path-sets are unchanged from prior behaviour.
	if !slices.Contains(res.Deleted, "sub/gone.txt") {
		t.Errorf("Deleted = %v, want it to contain sub/gone.txt", res.Deleted)
	}
}

// With CollectChanges off, DeletedSizes stays nil too.
func TestApply_NoDeletedSizesByDefault(t *testing.T) {
	repo, root := newTestRepo(t)
	defer func() { _ = repo.Close() }()
	ctx := context.Background()

	writeFile(t, root, "gone.txt", "bye\n")
	if _, err := repo.Apply(ctx, ApplyRequest{}); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	if err := os.Remove(filepath.Join(root, "gone.txt")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	res, err := repo.Apply(ctx, ApplyRequest{})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.DeletedSizes != nil {
		t.Errorf("DeletedSizes should be nil when CollectChanges is off, got %v", res.DeletedSizes)
	}
}

// TC-4 (FR1/FR3/KD1/KD2/AC3): a modified file's ModifiedBytes is its
// post-change (current) size, and a deletion's DeletedBytes is identical,
// across BOTH the status refresh path and the update path. This guards
// the cache-refresh FileSize timing and the dual-source deleted-byte rule
// against a real temp-repo, not a literal builder.
func TestPostRunTree_CrossPathByteIdentity(t *testing.T) {
	const newModContent = "this modified body is deliberately longer than the original\n"
	const goneContent = "doomed payload\n"
	wantMod := int64(len(newModContent))
	wantDel := int64(len(goneContent))

	// seedAndMutate builds a fresh repo, indexes a baseline, then modifies
	// mod.txt and deletes gone.txt on disk. Returns the repo + root.
	seedAndMutate := func(t *testing.T) (*localRepo, string) {
		t.Helper()
		repo, root := newTestRepo(t)
		ctx := context.Background()
		writeFile(t, root, "mod.txt", "short\n")
		writeFile(t, root, "gone.txt", goneContent)
		if _, err := repo.Apply(ctx, ApplyRequest{}); err != nil {
			t.Fatalf("baseline apply: %v", err)
		}
		writeFile(t, root, "mod.txt", newModContent)
		if err := os.Remove(filepath.Join(root, "gone.txt")); err != nil {
			t.Fatalf("remove gone.txt: %v", err)
		}
		return repo, root
	}

	assertTree := func(t *testing.T, tree *Tree, path string) {
		t.Helper()
		mod := descend(t, tree.Root, "mod.txt")
		if mod.Stats.ModifiedBytes != wantMod {
			t.Errorf("%s: mod.txt ModifiedBytes = %d, want %d", path, mod.Stats.ModifiedBytes, wantMod)
		}
		gone := descend(t, tree.Root, "gone.txt")
		if gone.Stats.DeletedBytes != wantDel {
			t.Errorf("%s: gone.txt DeletedBytes = %d, want %d", path, gone.Stats.DeletedBytes, wantDel)
		}
	}

	ctx := context.Background()

	// Status path: Diff refreshes the cache (new size + deletion tombstone),
	// PostRunTree reads the merged index — no DeletedSizes needed (KD2).
	t.Run("status", func(t *testing.T) {
		repo, _ := seedAndMutate(t)
		defer func() { _ = repo.Close() }()
		st, err := repo.Diff(ctx, DiffRequest{})
		if err != nil {
			t.Fatalf("Diff: %v", err)
		}
		tree, err := repo.PostRunTree(ctx, ChangeSet{
			Added:    st.Added,
			Modified: st.Modified,
			Deleted:  st.Deleted,
		})
		if err != nil {
			t.Fatalf("PostRunTree: %v", err)
		}
		assertTree(t, tree, "status")
	})

	// Update path: Apply discards the deleted entry, so the deleted size
	// rides DeletedSizes; the modified size comes from the post-rename merge.
	t.Run("update", func(t *testing.T) {
		repo, _ := seedAndMutate(t)
		defer func() { _ = repo.Close() }()
		res, err := repo.Apply(ctx, ApplyRequest{CollectChanges: true})
		if err != nil {
			t.Fatalf("Apply: %v", err)
		}
		tree, err := repo.PostRunTree(ctx, ChangeSet{
			Added:        res.Added,
			Modified:     res.Modified,
			Deleted:      res.Deleted,
			DeletedSizes: res.DeletedSizes,
		})
		if err != nil {
			t.Fatalf("PostRunTree: %v", err)
		}
		assertTree(t, tree, "update")
	})
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
