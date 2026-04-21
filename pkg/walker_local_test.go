package dircachefilehash

import (
	"bytes"
	"context"
	"crypto/sha256"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestLocalWalkerProducesSortedScanPaths(t *testing.T) {
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

	dc := CreateDirectoryCache(root, "")
	if dc == nil {
		t.Fatal("failed to create directory cache")
	}
	defer func() { _ = dc.Close() }()

	if dc.walker == nil {
		t.Fatal("walker should be populated by initDirectoryCacheBase")
	}

	ch := make(chan *scannedPath, 16)
	walkErr := make(chan error, 1)
	go func() { walkErr <- dc.walker.Walk(context.Background(), nil, ch) }()

	var got []string
	for sp := range ch {
		got = append(got, sp.RelPath)
	}
	if err := <-walkErr; err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if !slices.IsSorted(got) {
		t.Errorf("walker output not sorted: %v", got)
	}
	// Every written file must appear; intermediate dirs may also appear.
	for rel := range writes {
		if !slices.Contains(got, rel) {
			t.Errorf("expected %q in walker output, got %v", rel, got)
		}
	}
}

func TestLocalHasherMatchesHashFile(t *testing.T) {
	root := t.TempDir()
	content := []byte("hello walker/hasher seam")
	rel := "file.txt"
	if err := os.WriteFile(filepath.Join(root, rel), content, 0o644); err != nil {
		t.Fatal(err)
	}

	dc := CreateDirectoryCache(root, "")
	if dc == nil {
		t.Fatal("failed to create directory cache")
	}
	defer func() { _ = dc.Close() }()

	if dc.fileHasher == nil {
		t.Fatal("fileHasher should be populated by initDirectoryCacheBase")
	}

	// Set the algorithm to sha256 so the result is deterministic and
	// comparable without routing through dcfh's hash-type tables.
	if err := dc.GetConfig().SetHashDefault("sha256"); err != nil {
		t.Fatalf("SetHashDefault: %v", err)
	}

	buf := make([]byte, 64*1024)
	got, _, err := dc.fileHasher.HashOne(context.Background(), rel, buf)
	if err != nil {
		t.Fatalf("HashOne: %v", err)
	}

	want := sha256.Sum256(content)
	if !bytes.Equal(got, want[:]) {
		t.Errorf("hash mismatch: got %x, want %x", got, want)
	}
}
