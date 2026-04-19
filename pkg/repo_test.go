package dircachefilehash

import (
	"context"
	"errors"
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
			name:  "ssh basic",
			input: "ssh://host/path/foo.dcfh",
			want:  RepoURI{Scheme: "ssh", Host: "host", Path: "/path/foo.dcfh"},
		},
		{
			name:  "ssh with user and port",
			input: "ssh://user@host:2222/path/foo.dcfh",
			want:  RepoURI{Scheme: "ssh", User: "user", Host: "host", Port: "2222", Path: "/path/foo.dcfh"},
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

func TestOpenRepoRejectsSSH(t *testing.T) {
	_, err := OpenRepo(context.Background(), "ssh://host/path/foo.dcfh")
	if err == nil {
		t.Fatal("expected error for ssh:// URI")
	}
	if !errors.Is(err, ErrRemoteNotImplemented) {
		t.Fatalf("expected ErrRemoteNotImplemented, got: %v", err)
	}
}

func TestCreateRepoRejectsSSH(t *testing.T) {
	_, err := CreateRepo(context.Background(), "/tmp", "ssh://host/path/foo.dcfh")
	if err == nil {
		t.Fatal("expected error for ssh:// metaDirSpec")
	}
	if !errors.Is(err, ErrRemoteNotImplemented) {
		t.Fatalf("expected ErrRemoteNotImplemented, got: %v", err)
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
