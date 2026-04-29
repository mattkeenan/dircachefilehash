package dircachefilehash

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// makeRemoteFixture builds a small directory tree under t.TempDir and
// returns the root path. Used by ScanMetadata and HashFiles tests.
func makeRemoteFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writes := map[string]string{
		"a.txt":            "alpha",
		"b/c.txt":          "bravo-charlie",
		"b/d.bin":          "\x00\x01\x02\x03",
		"skip/excluded.md": "ignored",
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
	// Link into b/ so the scan has a symlink to record.
	if err := os.Symlink("c.txt", filepath.Join(root, "b", "link")); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestRemoteHandlerScanMetadataSorted(t *testing.T) {
	root := makeRemoteFixture(t)
	h, err := NewRemoteHandler(root, "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = h.Close() }()

	resp, err := h.ScanMetadata(context.Background(), ScanRequest{})
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 0, len(resp.Files))
	kinds := map[string]FileKind{}
	for _, f := range resp.Files {
		paths = append(paths, f.Path)
		kinds[f.Path] = f.Kind
	}
	if !sort.StringsAreSorted(paths) {
		t.Fatalf("paths not sorted: %v", paths)
	}
	want := []string{"a.txt", "b", "b/c.txt", "b/d.bin", "b/link", "skip", "skip/excluded.md"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
	if kinds["b"] != FileKindDir {
		t.Errorf("b kind = %s, want dir", kinds["b"])
	}
	if kinds["b/link"] != FileKindSymlink {
		t.Errorf("b/link kind = %s, want symlink", kinds["b/link"])
	}
	if kinds["a.txt"] != FileKindRegular {
		t.Errorf("a.txt kind = %s, want regular", kinds["a.txt"])
	}
}

func TestRemoteHandlerScanMetadataIgnores(t *testing.T) {
	root := makeRemoteFixture(t)
	h, _ := NewRemoteHandler(root, "")
	defer func() { _ = h.Close() }()

	resp, err := h.ScanMetadata(context.Background(), ScanRequest{
		Ignores: []string{"skip"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range resp.Files {
		if strings.HasPrefix(f.Path, "skip") {
			t.Errorf("skip/ entry leaked past ignore: %s", f.Path)
		}
	}
}

func TestRemoteHandlerScanMetadataSubpath(t *testing.T) {
	root := makeRemoteFixture(t)
	h, _ := NewRemoteHandler(root, "")
	defer func() { _ = h.Close() }()

	resp, err := h.ScanMetadata(context.Background(), ScanRequest{Paths: []string{"b"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range resp.Files {
		if !strings.HasPrefix(f.Path, "b") {
			t.Errorf("entry outside requested subpath: %s", f.Path)
		}
	}
}

func TestRemoteHandlerScanMetadataRejectsEscape(t *testing.T) {
	root := makeRemoteFixture(t)
	h, _ := NewRemoteHandler(root, "")
	defer func() { _ = h.Close() }()

	_, err := h.ScanMetadata(context.Background(), ScanRequest{Paths: []string{"../etc"}})
	if err == nil || !strings.Contains(err.Error(), "escapes root") {
		t.Fatalf("expected escape error, got %v", err)
	}
}

func TestRemoteHandlerHashFiles(t *testing.T) {
	root := makeRemoteFixture(t)
	h, _ := NewRemoteHandler(root, "")
	defer func() { _ = h.Close() }()

	resp, err := h.HashFiles(context.Background(), HashRequest{
		Paths: []string{"a.txt", "b/c.txt", "missing.file"},
		Algo:  "sha256",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Digests) != 3 {
		t.Fatalf("want 3 digests, got %d", len(resp.Digests))
	}

	want := sha256.Sum256([]byte("alpha"))
	if resp.Digests[0].Hash != hex.EncodeToString(want[:]) {
		t.Errorf("a.txt hash mismatch: got %s", resp.Digests[0].Hash)
	}
	if resp.Digests[2].Err == "" {
		t.Errorf("missing.file should report error, got hash %q", resp.Digests[2].Hash)
	}
}

func TestRemoteHandlerHashFilesCacheLocalPersists(t *testing.T) {
	root := makeRemoteFixture(t)
	cachePath := filepath.Join(t.TempDir(), "hashes.json")

	// First session: hash once, cache should be written on Close.
	h1, _ := NewRemoteHandler(root, cachePath)
	first, err := h1.HashFiles(context.Background(), HashRequest{
		Paths: []string{"a.txt"}, Algo: "sha256", Cache: CacheModeLocal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h1.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("cache file not written: %v", err)
	}

	// Second session: rehash the same file. The cache entry must be
	// hit (we can't observe "hit" directly, but we can verify the
	// answer is identical and the cache file is unchanged in size).
	preInfo, _ := os.Stat(cachePath)
	h2, _ := NewRemoteHandler(root, cachePath)
	second, err := h2.HashFiles(context.Background(), HashRequest{
		Paths: []string{"a.txt"}, Algo: "sha256", Cache: CacheModeLocal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h2.Close(); err != nil {
		t.Fatal(err)
	}
	if first.Digests[0].Hash != second.Digests[0].Hash {
		t.Fatalf("hash mismatch across sessions: %s vs %s",
			first.Digests[0].Hash, second.Digests[0].Hash)
	}
	postInfo, _ := os.Stat(cachePath)
	if preInfo.Size() != postInfo.Size() {
		t.Errorf("cache file grew on hit-only session: %d → %d",
			preInfo.Size(), postInfo.Size())
	}
}

func TestRemoteHandlerHashFilesCacheNoneIgnoresCache(t *testing.T) {
	root := makeRemoteFixture(t)
	cachePath := filepath.Join(t.TempDir(), "hashes.json")
	h, _ := NewRemoteHandler(root, cachePath)
	defer func() { _ = h.Close() }()

	_, err := h.HashFiles(context.Background(), HashRequest{
		Paths: []string{"a.txt"}, Algo: "sha256", Cache: CacheModeNone,
	})
	if err != nil {
		t.Fatal(err)
	}
	// With CacheModeNone, no cache file should be created — not even lazy-loaded.
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Errorf("cache file created despite CacheModeNone: err=%v", err)
	}
}

func TestRemoteHandlerHashFilesInvalidatesOnMtimeChange(t *testing.T) {
	root := makeRemoteFixture(t)
	cachePath := filepath.Join(t.TempDir(), "hashes.json")
	h, _ := NewRemoteHandler(root, cachePath)
	defer func() { _ = h.Close() }()

	first, err := h.HashFiles(context.Background(), HashRequest{
		Paths: []string{"a.txt"}, Algo: "sha256", Cache: CacheModeLocal,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Rewrite the file with different content; the cache key includes
	// mtime/size so this must force a fresh hash.
	p := filepath.Join(root, "a.txt")
	if err := os.WriteFile(p, []byte("alpha-rewritten"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Bump mtime explicitly in case the rewrite hit the same nanosecond
	// (coarse filesystems), otherwise the cache key collides on mtime.
	bumped := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(p, bumped, bumped)

	second, err := h.HashFiles(context.Background(), HashRequest{
		Paths: []string{"a.txt"}, Algo: "sha256", Cache: CacheModeLocal,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Digests[0].Hash == second.Digests[0].Hash {
		t.Errorf("expected different hash after file rewrite, both: %s",
			first.Digests[0].Hash)
	}
}

func TestRemoteHandlerServerInfo(t *testing.T) {
	root := makeRemoteFixture(t)
	h, _ := NewRemoteHandler(root, "")
	defer func() { _ = h.Close() }()

	caps, err := h.ServerInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if caps.WireVersion != WireVersion {
		t.Errorf("wire version: got %d want %d", caps.WireVersion, WireVersion)
	}
	if len(caps.HashAlgos) == 0 {
		t.Error("no hash algos advertised")
	}
	if caps.Concurrency <= 0 {
		t.Errorf("concurrency = %d", caps.Concurrency)
	}
}

func TestRemoteHandlerEndToEndViaPipes(t *testing.T) {
	root := makeRemoteFixture(t)
	h, _ := NewRemoteHandler(root, "")
	defer func() { _ = h.Close() }()

	ct, si, so := newPipePair()
	wait := runServer(t, h, si, so)
	client := NewWireClient(ct)

	caps, err := client.ServerInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if caps.WireVersion != WireVersion {
		t.Errorf("wire version mismatch: got %d", caps.WireVersion)
	}

	scan, err := client.ScanMetadata(context.Background(), ScanRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.Files) == 0 {
		t.Fatal("no files in scan")
	}

	want := sha256.Sum256([]byte("alpha"))
	hashes, err := client.HashFiles(context.Background(), HashRequest{
		Paths: []string{"a.txt"}, Algo: "sha256",
	})
	if err != nil {
		t.Fatal(err)
	}
	if hashes.Digests[0].Hash != hex.EncodeToString(want[:]) {
		t.Errorf("end-to-end hash mismatch")
	}

	_ = client.Close()
	_ = wait()
}

func TestNewRemoteHandlerRejectsBadRoot(t *testing.T) {
	if _, err := NewRemoteHandler("", ""); err == nil {
		t.Error("empty root should error")
	}
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if _, err := NewRemoteHandler(missing, ""); err == nil {
		t.Error("missing root should error")
	}
	file := filepath.Join(t.TempDir(), "f")
	_ = os.WriteFile(file, []byte("x"), 0o644)
	if _, err := NewRemoteHandler(file, ""); err == nil {
		t.Error("file-as-root should error")
	}
}
