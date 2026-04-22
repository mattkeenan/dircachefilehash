package dircachefilehash

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
)

func TestShellQuote(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"simple", `'simple'`},
		{"with space", `'with space'`},
		{"it's tricky", `'it'\''s tricky'`},
		{"", `''`},
		{"$HOME/`rm`", `'$HOME/` + "`rm`" + `'`},
	}
	for _, tc := range cases {
		if got := shellQuote(tc.in); got != tc.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseFindEpochNs(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"0", 0},
		{"1704729600", 1704729600_000_000_000},
		{"1704729600.5", 1704729600_500_000_000},
		{"1704729600.123456789", 1704729600_123_456_789},
		{"1704729600.1234567891", 1704729600_123_456_789}, // 10th digit truncated
		{"1704729600.0000000", 1704729600_000_000_000},
	}
	for _, tc := range cases {
		got, err := parseFindEpochNs(tc.in)
		if err != nil {
			t.Errorf("parseFindEpochNs(%q): unexpected err: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseFindEpochNs(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseFindOutput(t *testing.T) {
	// Fabricated find -printf output: one dir + one regular + one symlink.
	// Order is scrambled to verify the parser re-sorts.
	// Fields: path \t y \t size \t perm \t uid \t gid \t mtime \t ctime \t dev \t link
	raw := strings.Join([]string{
		"./b.txt\tf\t5\t644\t1000\t1000\t1704729600.0\t1704729600.0\t2049\t",
		"./a\td\t4096\t755\t1000\t1000\t1704729600.0\t1704729600.0\t2049\t",
		"./a/link\tl\t7\t777\t1000\t1000\t1704729600.0\t1704729600.0\t2049\t/etc/passwd",
		".\td\t4096\t755\t1000\t1000\t1704729600.0\t1704729600.0\t2049\t",
		"./sock\ts\t0\t660\t1000\t1000\t1704729600.0\t1704729600.0\t2049\t", // dropped
	}, "\n") + "\n"

	files, err := parseFindOutput([]byte(raw))
	if err != nil {
		t.Fatalf("parseFindOutput: %v", err)
	}

	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	wantPaths := []string{"a", "a/link", "b.txt"}
	if !reflect.DeepEqual(paths, wantPaths) {
		t.Errorf("paths = %v, want %v (sort + skip-root + skip-socket)", paths, wantPaths)
	}

	// Spot check the symlink record preserves its target.
	for _, f := range files {
		if f.Path == "a/link" {
			if f.Kind != FileKindSymlink {
				t.Errorf("a/link Kind = %s, want symlink", f.Kind)
			}
			if f.LinkTarget != "/etc/passwd" {
				t.Errorf("a/link LinkTarget = %q, want /etc/passwd", f.LinkTarget)
			}
			if f.Mode&uint32(os.ModeSymlink) == 0 {
				t.Errorf("a/link Mode missing ModeSymlink bit: %#o", f.Mode)
			}
		}
		if f.Path == "a" && f.Mode&uint32(os.ModeDir) == 0 {
			t.Errorf("a Mode missing ModeDir bit: %#o", f.Mode)
		}
	}
}

func TestParseFindLineRejectsMalformed(t *testing.T) {
	// Fewer than 10 fields must error — a truncated response shouldn't
	// silently produce partial results.
	if _, _, err := parseFindLine("onlyone"); err == nil {
		t.Error("parseFindLine: expected error for 1 field")
	}
}

func TestParseHashOutput(t *testing.T) {
	raw := []byte("deadbeef  a.txt\ncafebabe  sub/b.txt\n")
	requested := []string{"a.txt", "missing.txt", "sub/b.txt"}
	got := parseHashOutput(raw, requested)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].Path != "a.txt" || got[0].Hash != "deadbeef" || got[0].Err != "" {
		t.Errorf("got[0] = %+v", got[0])
	}
	if got[1].Path != "missing.txt" || got[1].Hash != "" || got[1].Err == "" {
		t.Errorf("got[1] = %+v, want Err set", got[1])
	}
	if got[2].Path != "sub/b.txt" || got[2].Hash != "cafebabe" {
		t.Errorf("got[2] = %+v", got[2])
	}
}

func TestParseHashOutputRejectsNonHex(t *testing.T) {
	// A line whose "hash" isn't hex should be dropped — if coreutils
	// output is ever tampered with, we don't want garbage propagated
	// into index entries.
	raw := []byte("NOT_HEX_AT_ALL  a.txt\ndeadbeef  b.txt\n")
	got := parseHashOutput(raw, []string{"a.txt", "b.txt"})
	if got[0].Err == "" {
		t.Error("a.txt: non-hex hash should yield Err, not silent accept")
	}
	if got[1].Hash != "deadbeef" {
		t.Errorf("b.txt: got %+v", got[1])
	}
}

func TestBuildFindPipelineQuotesPaths(t *testing.T) {
	p := buildFindPipeline("/awk ward/root", []string{"a", "b c"})
	// Must single-quote all three path-derived strings, not interpolate
	// unescaped.
	wantSubs := []string{
		"cd '/awk ward/root'",
		"find 'a' 'b c'",
		"| LC_ALL=C sort",
		"-printf '",
	}
	for _, s := range wantSubs {
		if !strings.Contains(p, s) {
			t.Errorf("pipeline missing %q: %s", s, p)
		}
	}
}

func TestBuildHashPipelineUsesAlgoTool(t *testing.T) {
	cases := []struct{ algo, tool string }{
		{"sha1", "sha1sum"},
		{"sha256", "sha256sum"},
		{"sha512", "sha512sum"},
	}
	for _, tc := range cases {
		got, err := hashToolForAlgo(tc.algo)
		if err != nil {
			t.Errorf("hashToolForAlgo(%q): %v", tc.algo, err)
			continue
		}
		if got != tc.tool {
			t.Errorf("hashToolForAlgo(%q) = %q, want %q", tc.algo, got, tc.tool)
		}
	}
	if _, err := hashToolForAlgo("md5"); err == nil {
		t.Error("md5 should be rejected")
	}
}

// requireShellFixture skips the test unless GNU find + coreutils are
// available, materialises the canonical 3-file tree under a temp root,
// and returns both. Factored out so the two end-to-end shell tests
// share one copy of the gate + layout.
func requireShellFixture(t *testing.T) (root string, writes map[string]string) {
	t.Helper()
	if _, err := exec.LookPath("find"); err != nil {
		t.Skip("find not on PATH")
	}
	if _, err := exec.LookPath("sha256sum"); err != nil {
		t.Skip("sha256sum not on PATH")
	}
	if out, err := exec.Command("find", "--version").Output(); err != nil || !strings.Contains(string(out), "GNU") {
		t.Skip("GNU find required for -printf")
	}
	root = t.TempDir()
	writes = map[string]string{
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
	return root, writes
}

// TestShellClientEndToEndLocal drives a shellClient via a local `sh`
// runner (no ssh) against a real fixture tree.
func TestShellClientEndToEndLocal(t *testing.T) {
	root, writes := requireShellFixture(t)

	sc := &shellClient{
		uri:    RepoURI{Scheme: "ssh", Transport: TransportShell, Host: "fixture", Path: root},
		runner: localShRunner(),
	}

	ctx := context.Background()
	resp, err := sc.ScanMetadata(ctx, ScanRequest{})
	if err != nil {
		t.Fatalf("ScanMetadata: %v", err)
	}
	var got []string
	for _, f := range resp.Files {
		if f.Kind == FileKindRegular {
			got = append(got, f.Path)
		}
	}
	sort.Strings(got)
	want := []string{"a.txt", "b/c.txt", "z.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("regular files = %v, want %v", got, want)
	}

	// Hash a.txt and missing.txt; the request must return both entries
	// in order, with missing.txt marked errored rather than silently
	// absent.
	hresp, err := sc.HashFiles(ctx, HashRequest{
		Paths: []string{"a.txt", "missing.txt"},
		Algo:  "sha256",
	})
	if err != nil {
		t.Fatalf("HashFiles: %v", err)
	}
	if len(hresp.Digests) != 2 {
		t.Fatalf("digests len = %d, want 2", len(hresp.Digests))
	}
	if hresp.Digests[0].Path != "a.txt" || hresp.Digests[0].Hash == "" {
		t.Errorf("digests[0] = %+v", hresp.Digests[0])
	}
	wantAlpha := sha256.Sum256([]byte(writes["a.txt"]))
	if hresp.Digests[0].Hash != hex.EncodeToString(wantAlpha[:]) {
		t.Errorf("a.txt hash = %q, want %s", hresp.Digests[0].Hash, hex.EncodeToString(wantAlpha[:]))
	}
	if hresp.Digests[1].Path != "missing.txt" || hresp.Digests[1].Err == "" {
		t.Errorf("digests[1] = %+v, want Err set", hresp.Digests[1])
	}
}

// localShRunner execs pipelines through /bin/sh locally — test-only, so
// the shell-protocol parser can be driven without ssh being available.
func localShRunner() shellRunner {
	return func(ctx context.Context, pipeline string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "sh", "-c", pipeline)
		cmd.Stderr = os.Stderr
		return cmd.Output()
	}
}

// TestShellRepoDiffAndApplyLocal is the shell analogue of
// TestWireRepoDiffAndApplyAgainstInProcessRemote: drives a full
// localRepo Diff/Apply cycle with a shellClient that runs its
// pipelines locally (no ssh), against a real fixture tree.
func TestShellRepoDiffAndApplyLocal(t *testing.T) {
	root, writes := requireShellFixture(t)
	ctx := context.Background()
	invokerMeta := filepath.Join(t.TempDir(), "audit.dcfh")
	uri := RepoURI{Scheme: "ssh", Transport: TransportShell, Host: "fixture", Path: root}
	bootstrap, err := createWireRepo(ctx, invokerMeta, uri)
	if err != nil {
		t.Fatalf("createWireRepo: %v", err)
	}
	if err := bootstrap.Close(); err != nil {
		t.Fatalf("bootstrap Close: %v", err)
	}

	dc, err := OpenDirectoryCache("", invokerMeta)
	if err != nil {
		t.Fatalf("OpenDirectoryCache: %v", err)
	}
	sc := &shellClient{uri: uri, runner: localShRunner()}
	repo := newWireRepoWithClient(dc, uri, sc)
	defer func() { _ = repo.Close() }()

	diff, err := repo.Diff(ctx, DiffRequest{})
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	for want := range writes {
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
	if upd.FileCount < len(writes) {
		t.Errorf("Apply.FileCount = %d, want >= %d", upd.FileCount, len(writes))
	}

	diff2, err := repo.Diff(ctx, DiffRequest{})
	if err != nil {
		t.Fatalf("Diff (post-apply): %v", err)
	}
	if len(diff2.Added)+len(diff2.Modified)+len(diff2.Deleted) != 0 {
		t.Errorf("second Diff should be clean; got added=%v modified=%v deleted=%v",
			diff2.Added, diff2.Modified, diff2.Deleted)
	}
}
