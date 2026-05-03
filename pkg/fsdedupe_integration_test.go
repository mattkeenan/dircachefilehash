//go:build linux

package dircachefilehash

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mattkeenan/dircachefilehash/pkg/fsdedupe"
)

// toFSDedupeGroups adapts DuplicateGroup (index-side) to
// fsdedupe.Group (dedup-side). Matches the conversion the CLI layer
// will perform when wiring --fs-dedupe.
func toFSDedupeGroups(in []DuplicateGroup) []fsdedupe.Group {
	out := make([]fsdedupe.Group, len(in))
	for i, g := range in {
		out[i] = fsdedupe.Group{Hash: g.Hash, Files: g.Files}
	}
	return out
}

// TestFSDedupe_Integration_DryRun exercises the full pipeline —
// setupDupesRepo builds a known fixture, FindDuplicates returns
// authoritative groups, fsdedupe.Run plans the reclaim in dry-run.
// Because dry-run still per-device-probes (apply would skip on
// tmpfs, so dry-run reports the same), the test skips when
// $TMPDIR is not reflink-capable — matching the documented
// graceful-degrade contract for CI running on ext4.
func TestFSDedupe_Integration_DryRun(t *testing.T) {
	root := t.TempDir()
	if !fsdedupe.ProbeReflinkFS(root) {
		t.Skipf("no reflink-capable filesystem under %q; skipping", root)
	}

	// Fixture: three content pools. Pool "alpha" at 8 KiB × 3 copies
	// gives a 2-file reclaim (keep the source, dedupe 2 copies).
	// Pool "beta" at 4 KiB × 2 gives a 1-file reclaim. Pool
	// "singleton" has no dupes and must not appear in results.
	alpha := make([]byte, 8192)
	for i := range alpha {
		alpha[i] = byte(i % 251)
	}
	beta := make([]byte, 4096)
	for i := range beta {
		beta[i] = byte((i * 7) % 241)
	}
	content := map[string]string{
		"a1.bin":        string(alpha),
		"a2.bin":        string(alpha),
		"sub/a3.bin":    string(alpha),
		"b1.bin":        string(beta),
		"b2.bin":        string(beta),
		"singleton.bin": "not shared",
	}
	ms := setupDupesRepo(t, content)
	defer func() { _ = ms.Close() }()

	repoRoot := ms.RootDir

	ctx := context.Background()
	groups, err := ms.FindDuplicates(ctx, ms.scanRun(), map[string]string{}, DupeFilter{})
	if err != nil {
		t.Fatalf("FindDuplicates: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("want 2 dup groups (alpha, beta), got %d: %+v", len(groups), groups)
	}

	res, err := fsdedupe.Run(ctx, toFSDedupeGroups(groups), fsdedupe.Options{
		DryRun:   true,
		RepoRoot: repoRoot,
	})
	if err != nil {
		t.Fatalf("fsdedupe.Run (dry): %v", err)
	}

	// Alpha: 3 members, 2 targets × 8192 = 16384 planned.
	// Beta:  2 members, 1 target  × 4096 = 4096 planned.
	// Total: 20480.
	wantPlanned := uint64(16384 + 4096)
	if res.TotalPlanned != wantPlanned {
		t.Errorf("TotalPlanned=%d; want %d", res.TotalPlanned, wantPlanned)
	}
	if res.TotalReclaimed != 0 {
		t.Errorf("TotalReclaimed=%d; want 0 in dry-run", res.TotalReclaimed)
	}
	for _, g := range res.Groups {
		if g.Outcome != fsdedupe.OutcomePlanned {
			t.Errorf("group %s outcome=%s; want planned (files=%+v)", g.Hash, g.Outcome, g.Files)
		}
		if g.Source == "" {
			t.Errorf("group %s missing Source", g.Hash)
		}
		for _, f := range g.Files {
			if f.Outcome != fsdedupe.OutcomePlanned {
				t.Errorf("group %s file %s outcome=%s; want planned", g.Hash, f.Path, f.Outcome)
			}
		}
	}

	// Dry-run is read-only against user content: every fixture file
	// must still contain its original bytes.
	for rel, want := range content {
		got, err := os.ReadFile(filepath.Join(repoRoot, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if string(got) != want {
			t.Errorf("dry-run mutated %s", rel)
		}
	}
}

// TestFSDedupe_Integration_Apply reclaims real extents via the
// kernel. Uses the same fixture as the dry-run test for baseline
// parity; asserts the apply matches the plan and that post-apply
// reads still return the original bytes (reflinks are
// userspace-invisible).
func TestFSDedupe_Integration_Apply(t *testing.T) {
	root := t.TempDir()
	if !fsdedupe.ProbeReflinkFS(root) {
		t.Skipf("no reflink-capable filesystem under %q; skipping", root)
	}

	alpha := make([]byte, 8192)
	for i := range alpha {
		alpha[i] = byte(i % 251)
	}
	beta := make([]byte, 4096)
	for i := range beta {
		beta[i] = byte((i * 7) % 241)
	}
	content := map[string]string{
		"a1.bin":     string(alpha),
		"a2.bin":     string(alpha),
		"sub/a3.bin": string(alpha),
		"b1.bin":     string(beta),
		"b2.bin":     string(beta),
	}
	ms := setupDupesRepo(t, content)
	defer func() { _ = ms.Close() }()

	ctx := context.Background()
	groups, err := ms.FindDuplicates(ctx, ms.scanRun(), map[string]string{}, DupeFilter{})
	if err != nil {
		t.Fatalf("FindDuplicates: %v", err)
	}
	var streamed []fsdedupe.GroupResult
	res, err := fsdedupe.Run(ctx, toFSDedupeGroups(groups), fsdedupe.Options{
		RepoRoot: ms.RootDir,
		OnGroup: func(gr fsdedupe.GroupResult) {
			streamed = append(streamed, gr)
		},
	})
	if err != nil {
		t.Fatalf("fsdedupe.Run (apply): %v", err)
	}

	wantReclaimed := uint64(2*8192 + 1*4096)
	if res.TotalReclaimed != wantReclaimed {
		t.Errorf("TotalReclaimed=%d; want %d", res.TotalReclaimed, wantReclaimed)
	}
	for _, g := range res.Groups {
		if g.Outcome != fsdedupe.OutcomeOK {
			t.Errorf("group %s outcome=%s; want ok (files=%+v)", g.Hash, g.Outcome, g.Files)
		}
	}

	// Streamed view must match the batch view 1:1 — the cmd-layer's
	// fused print/dedupe loop relies on this equivalence.
	if len(streamed) != len(res.Groups) {
		t.Fatalf("streamed=%d, batch=%d; want equal", len(streamed), len(res.Groups))
	}
	for i, gr := range res.Groups {
		if streamed[i].Hash != gr.Hash {
			t.Errorf("group[%d]: streamed=%q, batch=%q", i, streamed[i].Hash, gr.Hash)
		}
	}

	// Reflinks are semantically invisible — content must still match.
	for rel, want := range content {
		got, err := os.ReadFile(filepath.Join(ms.RootDir, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if string(got) != want {
			t.Errorf("apply corrupted %s", rel)
		}
	}

	// A second apply must be idempotent: extents are already shared,
	// so bytes_deduped is 0 for every target but outcome stays ok.
	groups2, err := ms.FindDuplicates(ctx, ms.scanRun(), map[string]string{}, DupeFilter{})
	if err != nil {
		t.Fatalf("FindDuplicates (2nd): %v", err)
	}
	res2, err := fsdedupe.Run(ctx, toFSDedupeGroups(groups2), fsdedupe.Options{RepoRoot: ms.RootDir})
	if err != nil {
		t.Fatalf("fsdedupe.Run (re-apply): %v", err)
	}
	for _, g := range res2.Groups {
		if g.Outcome != fsdedupe.OutcomeOK {
			t.Errorf("re-apply group %s outcome=%s; want ok (idempotent)", g.Hash, g.Outcome)
		}
	}
}
