package dircachefilehash

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestFindEntries exercises the bulk path-lookup helper against a freshly
// written main.idx — it should return matching entries in path-sorted order
// and surface unknown paths in the not-found slice in their original form.
func TestFindEntries(t *testing.T) {
	testDir := t.TempDir()
	ms := NewMetaStore(testDir, testDir)
	defer func() { _ = ms.Close() }()

	files := map[string]string{
		"alpha.txt":     "alpha\n",
		"beta.txt":      "beta\n",
		"sub/gamma.txt": "gamma\n",
	}
	for rel, content := range files {
		full := filepath.Join(testDir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	if err := runUpdate(context.Background(), ms, ms.scanRun(), map[string]string{}); err != nil {
		t.Fatalf("update: %v", err)
	}

	mainIdx := filepath.Join(testDir, ".dcfh", "main.idx")

	t.Run("AllFound", func(t *testing.T) {
		want := []string{"alpha.txt", "beta.txt", "sub/gamma.txt"}
		found, missing, err := FindEntries(mainIdx, want)
		if err != nil {
			t.Fatalf("FindEntries: %v", err)
		}
		if len(missing) != 0 {
			t.Errorf("unexpected missing: %v", missing)
		}
		if len(found) != len(want) {
			t.Fatalf("found %d entries, want %d", len(found), len(want))
		}
		got := make([]string, len(found))
		for i, e := range found {
			got[i] = e.Path
		}
		// FindEntries returns path-sorted; confirm.
		sorted := append([]string(nil), got...)
		sort.Strings(sorted)
		for i := range got {
			if got[i] != sorted[i] {
				t.Errorf("results not sorted: got %v", got)
				break
			}
		}
	})

	t.Run("MixedFoundAndMissing", func(t *testing.T) {
		input := []string{"./alpha.txt", "missing.txt", "sub/gamma.txt", "ghost/file"}
		found, missing, err := FindEntries(mainIdx, input)
		if err != nil {
			t.Fatalf("FindEntries: %v", err)
		}
		if len(found) != 2 {
			t.Errorf("found %d entries, want 2", len(found))
		}
		// Missing slice preserves the original (unnormalised) input order.
		wantMissing := []string{"missing.txt", "ghost/file"}
		if len(missing) != len(wantMissing) {
			t.Fatalf("missing %v, want %v", missing, wantMissing)
		}
		for i, m := range missing {
			if m != wantMissing[i] {
				t.Errorf("missing[%d] = %q, want %q", i, m, wantMissing[i])
			}
		}
	})

	t.Run("DotNormalisation", func(t *testing.T) {
		// "./alpha.txt" must match the index's stored "alpha.txt".
		found, missing, err := FindEntries(mainIdx, []string{"./alpha.txt"})
		if err != nil {
			t.Fatalf("FindEntries: %v", err)
		}
		if len(missing) != 0 {
			t.Errorf("unexpected missing: %v", missing)
		}
		if len(found) != 1 || found[0].Path != "alpha.txt" {
			t.Errorf("got %+v, want single alpha.txt", found)
		}
	})

	t.Run("EmptyInput", func(t *testing.T) {
		found, missing, err := FindEntries(mainIdx, nil)
		if err != nil {
			t.Fatalf("FindEntries: %v", err)
		}
		if len(found) != 0 || len(missing) != 0 {
			t.Errorf("expected empty results, got found=%v missing=%v", found, missing)
		}
	})

	t.Run("BadIndexPath", func(t *testing.T) {
		_, _, err := FindEntries(filepath.Join(testDir, "does-not-exist.idx"), []string{"alpha.txt"})
		if err == nil {
			t.Fatalf("expected error for missing index file")
		}
	})
}
