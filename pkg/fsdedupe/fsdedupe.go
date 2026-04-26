// Package fsdedupe reclaims disk blocks from duplicate files by asking
// the kernel to share their underlying extents copy-on-write. On
// platforms without a supported kernel primitive, Run returns
// ErrUnsupported.
//
// Maintainers: the build-tag predicate (linux vs !linux) tracks what
// golang.org/x/sys/unix exposes, not what we subjectively choose to
// support. When x/sys/unix adds IoctlFileDedupeRange on another
// platform, broaden fsdedupe_linux.go's tag and narrow
// fsdedupe_other.go's tag inversely.
package fsdedupe

import (
	"context"
	"errors"
)

// ErrUnsupported is returned by Run on platforms where fs-dedupe is
// not available (currently any GOOS other than linux). Callers should
// compare with errors.Is.
var ErrUnsupported = errors.New("fs-dedupe: not supported on this platform")

// Reason strings populated into FileResult.Reason and
// GroupResult.Reason. Exported so tests and JSON consumers can match
// on stable values rather than substring-grep against free-form text.
const (
	ReasonSymlink          = "symlink"
	ReasonNotRegular       = "not a regular file"
	ReasonNoStat           = "no syscall.Stat_t"
	ReasonUnsupportedFS    = "unsupported filesystem"
	ReasonNoPartners       = "no dedup partners on same device"
	ReasonSizeChanged      = "size changed since index"
	ReasonContentDiffers   = "content differs"
	ReasonSameInode        = "same inode"
	ReasonNoProgress       = "kernel reported zero progress"
	ReasonPermissionDenied = "permission denied"
	ReasonReadOnlyFile     = "read-only file"
	ReasonReadOnlyFS       = "read-only filesystem"
	ReasonTextFileBusy     = "text file busy"
)

// Group is the dedup-side view of a hash-match set produced by
// pkg.FindDuplicates. Files are repo-relative, forward-slash paths
// resolved against Options.RepoRoot at dedup time.
type Group struct {
	Hash  string
	Files []string
}

// Options configures a Run call. RepoRoot is prepended to each
// Group.Files entry to resolve an absolute filesystem path. DryRun
// reports what Run would do without calling the ioctl. Logf, if
// non-nil, receives one-shot informational messages such as
// "device N: filesystem does not support FIDEDUPERANGE".
type Options struct {
	DryRun   bool
	RepoRoot string
	Logf     func(format string, args ...any)

	// OnGroup, if non-nil, is invoked after each group's dedupe work
	// completes — before the next group begins. The same GroupResult
	// is also appended to Result.Groups, so callers that only want
	// the batch view can ignore this hook. Run holds no state across
	// the callback; panics propagate.
	OnGroup func(GroupResult)
}

// Outcome is the summary state for a file or group.
type Outcome string

const (
	// OutcomeOK: every targeted byte was deduped (possibly 0 if the
	// extents were already shared). Non-error, non-skip.
	OutcomeOK Outcome = "ok"
	// OutcomePartial: at least one target succeeded and at least one
	// was skipped or failed.
	OutcomePartial Outcome = "partial"
	// OutcomeSkipped: no ioctl ran — unsupported FS, cross-filesystem
	// group, all members unreadable, etc.
	OutcomeSkipped Outcome = "skipped"
	// OutcomeFailed: ioctl ran and returned an error that wasn't
	// per-target recoverable.
	OutcomeFailed Outcome = "failed"
	// OutcomePlanned: DryRun=true path. The file would be deduped
	// on a real run.
	OutcomePlanned Outcome = "planned"
)

// FileResult records the outcome for a single target file within a
// group. BytesDeduped is the sum across all ioctl calls for this
// file; in dry-run it is the file's full size. Reason is free-form
// human text for skipped/failed/partial cases.
type FileResult struct {
	Path         string  `json:"path"`
	Outcome      Outcome `json:"outcome"`
	BytesDeduped uint64  `json:"bytes_deduped"`
	Reason       string  `json:"reason,omitempty"`
}

// GroupResult records the outcome for one hash-group. Source is the
// repo-relative path of the chosen donor file, if one was selected.
// BytesReclaimed is the sum of FileResult.BytesDeduped for targets
// with Outcome in {OK, Partial, Planned}.
type GroupResult struct {
	Hash           string       `json:"hash"`
	Source         string       `json:"source,omitempty"`
	Outcome        Outcome      `json:"outcome"`
	BytesReclaimed uint64       `json:"bytes_reclaimed"`
	Files          []FileResult `json:"files"`
	Reason         string       `json:"reason,omitempty"`
}

// Result is the full output of a Run call. TotalReclaimed is the sum
// of GroupResult.BytesReclaimed for real (non-dry-run) groups;
// TotalPlanned is the analogous sum in dry-run mode.
// UnsupportedDevs carries a human-readable label for each device that
// the probe rejected.
type Result struct {
	Groups          []GroupResult `json:"groups"`
	TotalReclaimed  uint64        `json:"total_bytes_reclaimed"`
	TotalPlanned    uint64        `json:"total_bytes_planned,omitempty"`
	UnsupportedDevs []string      `json:"unsupported_devices,omitempty"`
}

// Run dedupes (or plans dedup of) each input group. On non-Linux
// platforms Run returns ErrUnsupported wrapped with the platform
// identifier; see package docstring for the build-tag contract.
//
// Groups are expected to already share a content hash; Run re-verifies
// via the kernel's own byte-equality check inside the ioctl, so hash
// collisions and stale indices cannot cause corruption. Groups whose
// members span multiple filesystems are partitioned by device and
// deduped independently per-device.
func Run(ctx context.Context, groups []Group, opts Options) (*Result, error) {
	return run(ctx, groups, opts)
}
