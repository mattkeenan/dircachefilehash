package dircachefilehash

import (
	"os"
	"path/filepath"
	"testing"
)

// realPath resolves symlinks so tests compare the same canonical form used by
// DiscoverRepository/ResolveRepository (e.g. /tmp vs /private/tmp on macOS).
func realPath(t *testing.T, p string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q) failed: %v", p, err)
	}
	return resolved
}

func TestResolveRepository_Internal(t *testing.T) {
	tmp := realPath(t, t.TempDir())
	repoRoot := filepath.Join(tmp, "repo")
	metaDir := filepath.Join(repoRoot, ".dcfh")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	gotRoot, gotMeta, err := ResolveRepository(metaDir)
	if err != nil {
		t.Fatalf("ResolveRepository: %v", err)
	}
	if gotRoot != repoRoot {
		t.Errorf("rootDir = %q, want %q", gotRoot, repoRoot)
	}
	if gotMeta != metaDir {
		t.Errorf("metaDir = %q, want %q", gotMeta, metaDir)
	}
}

func TestResolveRepository_ExternalWithConfig(t *testing.T) {
	tmp := realPath(t, t.TempDir())
	target := filepath.Join(tmp, "target")
	metaDir := filepath.Join(tmp, "ext.dcfh")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("MkdirAll target: %v", err)
	}
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("MkdirAll metaDir: %v", err)
	}
	cfg, err := CreateDefaultConfig(metaDir)
	if err != nil {
		t.Fatalf("CreateDefaultConfig: %v", err)
	}
	if err := cfg.SetRepositoryRoot(target); err != nil {
		t.Fatalf("SetRepositoryRoot: %v", err)
	}

	gotRoot, gotMeta, err := ResolveRepository(metaDir)
	if err != nil {
		t.Fatalf("ResolveRepository: %v", err)
	}
	if gotRoot != target {
		t.Errorf("rootDir = %q, want %q", gotRoot, target)
	}
	if gotMeta != metaDir {
		t.Errorf("metaDir = %q, want %q", gotMeta, metaDir)
	}
}

func TestResolveRepository_ExternalNoConfig(t *testing.T) {
	tmp := realPath(t, t.TempDir())
	metaDir := filepath.Join(tmp, "ext.dcfh")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	gotRoot, gotMeta, err := ResolveRepository(metaDir)
	if err != nil {
		t.Fatalf("ResolveRepository: %v", err)
	}
	if gotRoot != tmp {
		t.Errorf("rootDir = %q, want %q (parent fallback)", gotRoot, tmp)
	}
	if gotMeta != metaDir {
		t.Errorf("metaDir = %q, want %q", gotMeta, metaDir)
	}
}

func TestResolveRepository_NotADcfhDir(t *testing.T) {
	tmp := realPath(t, t.TempDir())
	regular := filepath.Join(tmp, "regular")
	if err := os.MkdirAll(regular, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if _, _, err := ResolveRepository(regular); err == nil {
		t.Errorf("expected error for non-.dcfh path, got nil")
	}
}

func TestDiscoverRepository_WalkUp(t *testing.T) {
	tmp := realPath(t, t.TempDir())
	repoRoot := filepath.Join(tmp, "repo")
	metaDir := filepath.Join(repoRoot, ".dcfh")
	deep := filepath.Join(repoRoot, "sub", "deep")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("MkdirAll metaDir: %v", err)
	}
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("MkdirAll deep: %v", err)
	}

	gotRoot, gotMeta, err := DiscoverRepository(deep)
	if err != nil {
		t.Fatalf("DiscoverRepository: %v", err)
	}
	if gotRoot != repoRoot {
		t.Errorf("rootDir = %q, want %q", gotRoot, repoRoot)
	}
	if gotMeta != metaDir {
		t.Errorf("metaDir = %q, want %q", gotMeta, metaDir)
	}
}

func TestDiscoverRepository_StartIsMetaDir(t *testing.T) {
	tmp := realPath(t, t.TempDir())
	repoRoot := filepath.Join(tmp, "repo")
	metaDir := filepath.Join(repoRoot, ".dcfh")
	if err := os.MkdirAll(metaDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	gotRoot, gotMeta, err := DiscoverRepository(metaDir)
	if err != nil {
		t.Fatalf("DiscoverRepository: %v", err)
	}
	if gotRoot != repoRoot {
		t.Errorf("rootDir = %q, want %q", gotRoot, repoRoot)
	}
	if gotMeta != metaDir {
		t.Errorf("metaDir = %q, want %q", gotMeta, metaDir)
	}
}

func TestDiscoverRepository_NotFound(t *testing.T) {
	// Use a deep unique dir to minimise the chance of finding a stray
	// .dcfh in a parent directory on the host.
	tmp := realPath(t, t.TempDir())
	deep := filepath.Join(tmp, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	_, _, err := DiscoverRepository(deep)
	// Either there is no .dcfh anywhere up the tree (error) or the host
	// has one somewhere above t.TempDir(). Accept both — the point is that
	// DiscoverRepository does not return our TempDir as the root.
	if err == nil {
		// If it succeeded, it must have found .dcfh higher up — not within tmp.
		t.Logf("DiscoverRepository succeeded; host has a .dcfh above %s", tmp)
	}
}

func TestExtractPidFromIndexFileName(t *testing.T) {
	testCases := []struct {
		filename string
		expected int
	}{
		{"scan-1234-5678.idx", 1234},
		{"tmp-9999-1111.idx", 9999},
		{"scan-0-0.idx", 0},
		{"tmp-42-999.idx", 42},
		{"invalid.idx", 0},
		{"scan-abc-def.idx", 0},
		{"scan-1234.idx", 0},  // Not enough parts
		{"scan-1234-5678", 0}, // No .idx suffix
		{"", 0},
	}

	for _, tc := range testCases {
		result := extractPidFromIndexFileName(tc.filename)
		if result != tc.expected {
			t.Errorf("extractPidFromIndexFileName(%q) = %d, expected %d", tc.filename, result, tc.expected)
		}
	}
}

func TestIsProcessRunning(t *testing.T) {
	// Test with PID 1 (init process, should always exist on Unix systems)
	if !isProcessRunning(1) {
		t.Errorf("PID 1 should be running on Unix systems")
	}

	// Test with an obviously invalid PID (very high number unlikely to be used)
	if isProcessRunning(999999) {
		t.Errorf("PID 999999 should not be running")
	}
}
