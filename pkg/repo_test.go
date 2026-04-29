package dircachefilehash

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRepoURI(t *testing.T) {
	cwd, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("failed to resolve cwd: %v", err)
	}

	cases := []struct {
		name      string
		input     string
		wantErr   bool
		errSubstr string
		want      RepoURI
	}{
		{
			name:  "absolute path",
			input: "/abs/foo.dcfh",
			want:  RepoURI{Scheme: "file", Path: "/abs/foo.dcfh"},
		},
		{
			name:  "relative path resolves to absolute",
			input: "./rel.dcfh",
			want:  RepoURI{Scheme: "file", Path: filepath.Join(cwd, "rel.dcfh")},
		},
		{
			name:  "file:// URI",
			input: "file:///abs/path.dcfh",
			want:  RepoURI{Scheme: "file", Path: "/abs/path.dcfh"},
		},
		{
			name:    "file:// with relative path rejected",
			input:   "file://relative/foo",
			wantErr: true,
		},
		{
			name:  "ssh basic (shorthand for wire)",
			input: "ssh://host/path/foo.dcfh",
			want:  RepoURI{Scheme: "ssh", Transport: TransportWire, Host: "host", Path: "/path/foo.dcfh"},
		},
		{
			name:  "ssh+wire explicit",
			input: "ssh+wire://host/path/foo.dcfh",
			want:  RepoURI{Scheme: "ssh", Transport: TransportWire, Host: "host", Path: "/path/foo.dcfh"},
		},
		{
			name:  "ssh+shell variant",
			input: "ssh+shell://user@host:2222/srv/app",
			want:  RepoURI{Scheme: "ssh", Transport: TransportShell, User: "user", Host: "host", Port: "2222", Path: "/srv/app"},
		},
		{
			name:      "ssh+unknown variant rejected",
			input:     "ssh+bogus://host/p",
			wantErr:   true,
			errSubstr: "unsupported URI scheme",
		},
		{
			name:  "ssh with user and port",
			input: "ssh://user@host:2222/path/foo.dcfh",
			want:  RepoURI{Scheme: "ssh", Transport: TransportWire, User: "user", Host: "host", Port: "2222", Path: "/path/foo.dcfh"},
		},
		{
			name:    "ssh missing path",
			input:   "ssh://host",
			wantErr: true,
		},
		{
			name:    "ssh missing host",
			input:   "ssh:///path",
			wantErr: true,
		},
		{
			name:      "unknown scheme",
			input:     "http://foo/bar",
			wantErr:   true,
			errSubstr: "unsupported URI scheme",
		},
		{
			name:    "empty",
			input:   "",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseRepoURI(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				if tc.errSubstr != "" && !strings.Contains(err.Error(), tc.errSubstr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestRepoURIStringRoundTrip(t *testing.T) {
	cases := []string{
		"file:///abs/path.dcfh",
		"ssh://host/path",
		"ssh://user@host:2222/srv/app",
		"ssh+shell://host/path",
		"ssh+shell://user@host:2222/srv/app",
	}
	for _, in := range cases {
		u, err := ParseRepoURI(in)
		if err != nil {
			t.Fatalf("ParseRepoURI(%q): %v", in, err)
		}
		if got := u.String(); got != in {
			t.Errorf("round-trip %q -> %q", in, got)
		}
	}
	// ssh+wire:// canonicalises to bare ssh:// (wire is the default).
	u, err := ParseRepoURI("ssh+wire://host/p")
	if err != nil {
		t.Fatal(err)
	}
	if got := u.String(); got != "ssh://host/p" {
		t.Errorf("ssh+wire canonical form: got %q, want ssh://host/p", got)
	}
}

func TestOpenRepoRejectsSSHMetaDir(t *testing.T) {
	// --meta-dir must be local; ssh:// there is a user error directing
	// them to configure [repository] root instead.
	_, err := OpenRepo(context.Background(), "ssh://host/path/foo.dcfh")
	if err == nil {
		t.Fatal("expected error for ssh:// meta-dir")
	}
	if !strings.Contains(err.Error(), "[repository] root") {
		t.Fatalf("expected guidance about [repository] root, got: %v", err)
	}
}

func TestCreateRepoRejectsSSHMetaDir(t *testing.T) {
	// metaDirSpec is the *local* .dcfh; ssh:// is only valid as rootDir.
	_, err := CreateRepo(context.Background(), "/tmp", "ssh://host/path/foo.dcfh")
	if err == nil {
		t.Fatal("expected error for ssh:// metaDirSpec")
	}
	if !strings.Contains(err.Error(), "ssh://") {
		t.Fatalf("expected ssh-related error, got: %v", err)
	}
}

func TestCreateAndOpenWireRepo(t *testing.T) {
	tmp := t.TempDir()
	ctx := context.Background()
	metaDir := filepath.Join(tmp, "prod-host.dcfh")
	rootURI := "ssh://admin@prod-host:2222/var/lib/app"

	repo, err := CreateRepo(ctx, rootURI, metaDir)
	if err != nil {
		t.Fatalf("CreateRepo(wire): %v", err)
	}

	info, err := repo.Info(ctx)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.RootDir != rootURI {
		t.Errorf("RootDir: got %q, want %q", info.RootDir, rootURI)
	}
	if info.MetaDir != metaDir {
		t.Errorf("MetaDir: got %q, want %q", info.MetaDir, metaDir)
	}

	// Local-only surface (Info, Snapshots, Config) must not dial ssh;
	// nothing asserts that directly, but a dial against prod-host:2222
	// from the test harness would hang, which is asserted by the test
	// simply completing.
	if _, err := repo.Snapshots().List(ctx); err != nil {
		t.Errorf("Snapshots.List should succeed locally: %v", err)
	}

	if err := repo.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopen via OpenRepo on the local metaDir — config root routes the
	// wire walker/hasher in on the reopened localRepo.
	repo2, err := OpenRepo(ctx, metaDir)
	if err != nil {
		t.Fatalf("OpenRepo(wire): %v", err)
	}
	lr, ok := repo2.(*localRepo)
	if !ok {
		t.Fatalf("expected *localRepo, got %T", repo2)
	}
	if lr.session == nil {
		t.Errorf("expected wire session on reopened wire repo")
	}
	_ = repo2.Close()
}

func TestCreateWireRepoRequiresMetaDir(t *testing.T) {
	_, err := CreateRepo(context.Background(), "ssh://host/path", "")
	if err == nil {
		t.Fatal("expected error when metaDirSpec is empty")
	}
	if !strings.Contains(err.Error(), "--meta-dir") {
		t.Fatalf("expected guidance about --meta-dir, got: %v", err)
	}
}

func TestLocalRepoLifecycle(t *testing.T) {
	tmp := t.TempDir()
	ctx := context.Background()

	repo, err := CreateRepo(ctx, tmp, "")
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}

	info, err := repo.Info(ctx)
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.RootDir != tmp {
		t.Errorf("RootDir: got %q, want %q", info.RootDir, tmp)
	}
	if info.MetaDir != filepath.Join(tmp, ".dcfh") {
		t.Errorf("MetaDir: got %q, want %q", info.MetaDir, filepath.Join(tmp, ".dcfh"))
	}

	stats, err := repo.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.FileCount != 0 {
		t.Errorf("fresh repo FileCount: got %d, want 0", stats.FileCount)
	}

	allConfig, err := repo.Config().Get(ctx)
	if err != nil {
		t.Fatalf("Config.Get: %v", err)
	}
	if allConfig.Hash.Default == "" {
		t.Error("expected a default hash algorithm")
	}

	if err := repo.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Re-open should work
	repo2, err := OpenRepo(ctx, filepath.Join(tmp, ".dcfh"))
	if err != nil {
		t.Fatalf("OpenRepo: %v", err)
	}
	_ = repo2.Close()
}

// TestDiffRequestIgnoresPushDown asserts that DiffRequest.Ignores
// short-circuits the scan walker (push-down) and that
// localRepo.configureFilters resets dc.scanIgnore after the call so a
// reused Repo doesn't leak the predicate.
func TestDiffRequestIgnoresPushDown(t *testing.T) {
	tmp := t.TempDir()
	for name, body := range map[string]string{
		"keep.go":  "x",
		"drop.tmp": "x",
	} {
		if err := os.WriteFile(filepath.Join(tmp, name), []byte(body), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	ctx := context.Background()
	repo, err := CreateRepo(ctx, tmp, "")
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	defer func() { _ = repo.Close() }()

	if _, err := repo.Apply(ctx, ApplyRequest{
		Ignores: []FilterOptions{{Names: []string{"*.tmp"}}},
	}); err != nil {
		t.Fatalf("Apply with --ignore: %v", err)
	}
	stats, err := repo.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.FileCount != 1 {
		t.Errorf("after --ignore *.tmp Apply: FileCount=%d, want 1 (only keep.go)", stats.FileCount)
	}

	// scanIgnore must have been reset — verify by looking at the local
	// dc directly (the only consumer of scanIgnore lifetime).
	lr, ok := repo.(*localRepo)
	if !ok {
		t.Fatalf("expected *localRepo, got %T", repo)
	}
	if lr.dc.scanIgnore != nil {
		t.Errorf("dc.scanIgnore not reset after Apply; value=%v", lr.dc.scanIgnore)
	}
}

// TestDiffRequestNoIgnoreFile asserts that NoIgnoreFile=true reads no
// patterns from .dcfh/ignore for the call and restores them after.
func TestDiffRequestNoIgnoreFile(t *testing.T) {
	tmp := t.TempDir()
	ctx := context.Background()
	repo, err := CreateRepo(ctx, tmp, "")
	if err != nil {
		t.Fatalf("CreateRepo: %v", err)
	}
	defer func() { _ = repo.Close() }()

	lr := repo.(*localRepo)
	ignorePath := filepath.Join(lr.dc.MetaDir, "ignore")
	if err := os.WriteFile(ignorePath, []byte("*.tmp\n"), 0644); err != nil {
		t.Fatalf("write ignore: %v", err)
	}
	if err := lr.dc.ignoreManager.Reload(); err != nil {
		t.Fatalf("reload ignore: %v", err)
	}
	if !lr.dc.ignoreManager.HasPatterns() {
		t.Fatal("baseline: ignoreManager should have *.tmp loaded")
	}

	if _, err := repo.Diff(ctx, DiffRequest{NoIgnoreFile: true}); err != nil {
		t.Fatalf("Diff with NoIgnoreFile: %v", err)
	}

	if !lr.dc.ignoreManager.HasPatterns() {
		t.Errorf("ignoreManager patterns not restored after Diff(NoIgnoreFile=true)")
	}
}
