package dircachefilehash

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// newInProcessWireRepo spins a RemoteHandler over an io.Pipe pair and
// returns a wireRepo wired to it. root is the remote-side fixture;
// metaDir is the invoker-side .dcfh (must already exist). Delegates to
// the production newWireRepoWithClient so future wiring additions are
// exercised by this test automatically.
func newInProcessWireRepo(t *testing.T, root, metaDir string, uri RepoURI) *wireRepo {
	t.Helper()

	handler, err := NewRemoteHandler(root, "")
	if err != nil {
		t.Fatalf("NewRemoteHandler: %v", err)
	}
	t.Cleanup(func() { _ = handler.Close() })

	ct, si, so := newPipePair()
	wait := runServer(t, handler, si, so)
	t.Cleanup(func() { _ = wait() })

	ms, err := OpenMetaStore("", metaDir)
	if err != nil {
		t.Fatalf("OpenMetaStore(%s): %v", metaDir, err)
	}
	return newWireRepoWithClient(ms, uri, NewWireClient(ct))
}

func TestWireRepoDiffAndApplyAgainstInProcessRemote(t *testing.T) {
	ctx := context.Background()

	root := t.TempDir()
	writes := map[string]string{
		"a.txt":   "alpha",
		"b/c.txt": "bravo-charlie",
		"z.txt":   "zulu",
	}
	for rel, data := range writes {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	invokerMeta := filepath.Join(t.TempDir(), "audit.dcfh")
	uri := RepoURI{Scheme: "ssh", Host: "fixture", Path: root}
	bootstrap, err := createWireRepo(ctx, invokerMeta, uri)
	if err != nil {
		t.Fatalf("createWireRepo: %v", err)
	}
	if err := bootstrap.Close(); err != nil {
		t.Fatalf("bootstrap Close: %v", err)
	}

	repo := newInProcessWireRepo(t, root, invokerMeta, uri)
	defer func() { _ = repo.Close() }()

	diff, err := repo.Diff(ctx, DiffRequest{})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	for _, want := range []string{"a.txt", "b/c.txt", "z.txt"} {
		if !slices.Contains(diff.Added, want) {
			t.Errorf("Diff.Added missing %q; got %v", want, diff.Added)
		}
	}
	if len(diff.Modified) != 0 {
		t.Errorf("Diff.Modified should be empty for initial scan: %v", diff.Modified)
	}

	upd, err := repo.Apply(ctx, ApplyRequest{})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if upd.FileCount < 3 {
		t.Errorf("Apply.FileCount = %d, want >= 3", upd.FileCount)
	}

	// Second Diff on unchanged fixture must be a no-op.
	diff2, err := repo.Diff(ctx, DiffRequest{})
	if err != nil {
		t.Fatalf("Diff (post-apply): %v", err)
	}
	if len(diff2.Added)+len(diff2.Modified)+len(diff2.Deleted) != 0 {
		t.Errorf("second Diff should be clean; got added=%v modified=%v deleted=%v",
			diff2.Added, diff2.Modified, diff2.Deleted)
	}
}
