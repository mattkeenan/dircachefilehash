//go:build linux

package fsdedupe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// Unit tests for pkg/fsdedupe on Linux. End-to-end coverage —
// setupDupesRepo → FindDuplicates → Run — lives at
// pkg/fsdedupe_integration_test.go so the fixture matches the rest
// of the test suite. These tests cover units that don't need a
// full repo: outcome summarisation, per-target open failures, the
// non-Linux-free paths of the main code.

// TestRun_ReadOnlyTarget is the narrow unit-level case the
// integration test doesn't cover: a target mode-0444 must be
// skipped with a permission-flavoured reason while the rest of
// the group dedupes normally.
func TestRun_ReadOnlyTarget(t *testing.T) {
	dir := t.TempDir()
	if !ProbeReflinkFS(dir) {
		t.Skipf("no reflink-capable filesystem under %q; skipping", dir)
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root; O_RDWR on 0444 succeeds, case not exercisable")
	}

	data := make([]byte, 64<<10)
	for i := range data {
		data[i] = byte(i % 251)
	}
	for _, name := range []string{"a.bin", "b.bin", "ro.bin"} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := os.Chmod(filepath.Join(dir, "ro.bin"), 0o444); err != nil {
		t.Fatalf("chmod ro: %v", err)
	}

	res, err := Run(context.Background(), []Group{{
		Hash:  "h",
		Files: []string{"a.bin", "b.bin", "ro.bin"},
	}}, Options{RepoRoot: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var roFR *FileResult
	for i := range res.Groups[0].Files {
		if res.Groups[0].Files[i].Path == "ro.bin" {
			roFR = &res.Groups[0].Files[i]
		}
	}
	if roFR == nil {
		t.Fatalf("no FileResult for ro.bin in %+v", res.Groups[0].Files)
	}
	if roFR.Outcome != OutcomeSkipped {
		t.Errorf("ro.bin outcome=%s; want skipped", roFR.Outcome)
	}
	if !strings.Contains(roFR.Reason, "permission") && !strings.Contains(roFR.Reason, "read-only") {
		t.Errorf("ro.bin reason=%q; want permission- or read-only-flavoured", roFR.Reason)
	}
}

// TestOpenReason pins the grep-friendly reason strings Run uses in
// FileResult.Reason. Tests downstream of this package grep for these
// strings, so they're part of the package's public behaviour.
func TestOpenReason(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{os.ErrPermission, ReasonPermissionDenied},
		{unix.EACCES, ReasonPermissionDenied},
		{unix.EROFS, ReasonReadOnlyFS},
		{unix.ETXTBSY, ReasonTextFileBusy},
		{unix.ELOOP, ReasonSymlink},
	}
	for _, tc := range tests {
		if got := openReason(tc.err); got != tc.want {
			t.Errorf("openReason(%v)=%q; want %q", tc.err, got, tc.want)
		}
	}
	generic := errors.New("some other failure")
	if got := openReason(generic); !strings.HasPrefix(got, "open: ") {
		t.Errorf("openReason(generic)=%q; want prefix \"open: \"", got)
	}
}

// TestSummariseOutcome pins the group-level rollup logic that decides
// whether a GroupResult reads as ok, partial, skipped, failed, or
// planned based on its per-file outcomes.
func TestSummariseOutcome(t *testing.T) {
	tests := []struct {
		name   string
		files  []FileResult
		dryRun bool
		want   Outcome
	}{
		{"all ok", []FileResult{{Outcome: OutcomeOK}, {Outcome: OutcomeOK}}, false, OutcomeOK},
		{"all skipped", []FileResult{{Outcome: OutcomeSkipped}, {Outcome: OutcomeSkipped}}, false, OutcomeSkipped},
		{"mixed ok+skipped", []FileResult{{Outcome: OutcomeOK}, {Outcome: OutcomeSkipped}}, false, OutcomePartial},
		{"any failed taints", []FileResult{{Outcome: OutcomeOK}, {Outcome: OutcomeFailed}}, false, OutcomeFailed},
		{"all planned dry", []FileResult{{Outcome: OutcomePlanned}, {Outcome: OutcomePlanned}}, true, OutcomePlanned},
		{"planned+skip dry", []FileResult{{Outcome: OutcomePlanned}, {Outcome: OutcomeSkipped}}, true, OutcomePartial},
		{"failed in dry beats planned", []FileResult{{Outcome: OutcomePlanned}, {Outcome: OutcomeFailed}}, true, OutcomeFailed},
		{"empty", nil, false, OutcomeSkipped},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := summariseOutcome(tc.files, tc.dryRun); got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}
}

// TestPartitionByDev_MultipleDevs is a narrow unit check on the
// device-partitioning helper — the real multi-device behaviour is
// hard to exercise in tests (needs two mounted filesystems) but
// the grouping logic is plain in-memory data and worth pinning.
func TestPartitionByDev_MultipleDevs(t *testing.T) {
	got := partitionByDev([]fileInfo{
		{rel: "a", dev: 1},
		{rel: "b", dev: 2},
		{rel: "c", dev: 1},
		{rel: "d", dev: 2},
		{rel: "e", dev: 3},
	})
	if len(got[1]) != 2 || len(got[2]) != 2 || len(got[3]) != 1 {
		t.Errorf("partition sizes wrong: %+v", got)
	}
	if got[1][0].rel != "a" || got[1][1].rel != "c" {
		t.Errorf("dev=1 contents wrong: %+v", got[1])
	}
}
