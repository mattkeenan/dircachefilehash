//go:build linux

package fsdedupe

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

// maxChunkBytes caps a single ioctl's src_length. The manpage notes
// "the maximum size of src_length is filesystem dependent and is
// typically 16 MiB"; exceeding it is silently clamped. Honour it
// upfront so bytes_deduped matches the requested length on the happy
// path and progress accounting stays simple.
const maxChunkBytes = 16 * 1024 * 1024

// fileInfo carries the minimum needed to order, partition, and open
// a group member. rel is the path as it appears in the index (forward
// slash, repo-relative); abs is rel joined against Options.RepoRoot.
type fileInfo struct {
	rel  string
	abs  string
	dev  uint64
	size uint64
	mode os.FileMode
}

// activeTarget is a per-target bookkeeping slot used during the
// ioctl loop. bytesDeduped is the running sum of successful
// bytes_deduped returns; alive flips to false as soon as the target
// is disqualified (DIFFERS, errno, or a fatal batch-level error).
type activeTarget struct {
	fd           int
	info         fileInfo
	bytesDeduped uint64
	alive        bool
	reason       string
}

// supportCache memoises per-device fsdedupe capability. The ioctl
// returns EOPNOTSUPP / EINVAL / ENOTTY on unsupported filesystems;
// one probe per device is plenty.
type supportCache struct {
	mu     sync.Mutex
	known  map[uint64]bool
	warned map[uint64]bool
}

func newSupportCache() *supportCache {
	return &supportCache{known: make(map[uint64]bool), warned: make(map[uint64]bool)}
}

// check returns whether dev supports fsdedupe. On first encounter it
// opens samplePath for reading and issues a 0-byte dedupe ioctl
// against a temporary scratch file on the same directory; EOPNOTSUPP
// / EINVAL / ENOTTY mark dev as unsupported. On unsupported hits the
// first call also emits one Logf message.
func (c *supportCache) check(dev uint64, samplePath string, logf func(string, ...any)) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ok, found := c.known[dev]; found {
		return ok
	}
	ok := probeDevice(samplePath)
	c.known[dev] = ok
	if !ok && !c.warned[dev] {
		c.warned[dev] = true
		if logf != nil {
			logf("fs-dedupe: skipping device %d: filesystem does not support FIDEDUPERANGE", dev)
		}
	}
	return ok
}

// probeDevice opens samplePath for reading and attempts a zero-length
// dedupe against a scratch file created in the same directory. The
// scratch file is unlinked immediately (still open via the fd), so
// cleanup needs no deferred rm.
func probeDevice(samplePath string) bool {
	src, err := os.OpenFile(samplePath, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return false
	}
	defer src.Close()

	dir := filepath.Dir(samplePath)
	scratch, err := os.CreateTemp(dir, ".dcfh-dedupe-probe-*")
	if err != nil {
		return false
	}
	_ = os.Remove(scratch.Name())
	defer scratch.Close()

	return ioctlProbeRecognised(int(src.Fd()), int(scratch.Fd()))
}

// ioctlProbeRecognised reports whether a zero-length FIDEDUPERANGE
// against (srcFd, dstFd) returned anything other than the
// "unsupported" errno triplet. Anything else — including success or
// an unrelated errno — means the ioctl is at least recognised by the
// filesystem; treat non-clamp errors at runtime rather than here.
func ioctlProbeRecognised(srcFd, dstFd int) bool {
	err := unix.IoctlFileDedupeRange(srcFd, &unix.FileDedupeRange{
		Info: []unix.FileDedupeRangeInfo{{Dest_fd: int64(dstFd)}},
	})
	return !errors.Is(err, unix.EOPNOTSUPP) &&
		!errors.Is(err, unix.EINVAL) &&
		!errors.Is(err, unix.ENOTTY)
}

// ProbeReflinkFS reports whether the filesystem under dir supports
// FIDEDUPERANGE. Intended for tests that need to skip when $TMPDIR
// is on ext4 / tmpfs. Writes two 4 KiB throwaway files inside dir
// and unlinks them before returning.
func ProbeReflinkFS(dir string) bool {
	a, err := os.CreateTemp(dir, ".fsdedupe-reflink-probe-a-*")
	if err != nil {
		return false
	}
	defer os.Remove(a.Name())
	defer a.Close()
	b, err := os.CreateTemp(dir, ".fsdedupe-reflink-probe-b-*")
	if err != nil {
		return false
	}
	defer os.Remove(b.Name())
	defer b.Close()
	return ioctlProbeRecognised(int(a.Fd()), int(b.Fd()))
}

// run is the linux implementation backing fsdedupe.Run.
func run(ctx context.Context, groups []Group, opts Options) (*Result, error) {
	result := &Result{Groups: make([]GroupResult, 0, len(groups))}
	cache := newSupportCache()
	maxDests := max((unix.Getpagesize()-24)/32, 1)

	for _, g := range groups {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		gr := processGroup(ctx, g, opts, cache, maxDests)
		result.Groups = append(result.Groups, gr)
		if opts.DryRun {
			result.TotalPlanned += gr.BytesReclaimed
		} else {
			result.TotalReclaimed += gr.BytesReclaimed
		}
		if opts.OnGroup != nil {
			opts.OnGroup(gr)
		}
	}

	// Flatten unsupported-device set for reporting.
	cache.mu.Lock()
	for dev, ok := range cache.known {
		if !ok {
			result.UnsupportedDevs = append(result.UnsupportedDevs, fmt.Sprintf("dev=%d", dev))
		}
	}
	cache.mu.Unlock()
	sort.Strings(result.UnsupportedDevs)

	return result, nil
}

// processGroup stats every member, drops symlinks and irregular
// files, partitions the rest by device, and dedupes each sub-group
// independently. Returns a single GroupResult whose Files slice
// carries every member's outcome (source-path members are recorded
// implicitly via GroupResult.Source and do not appear in Files).
func processGroup(ctx context.Context, g Group, opts Options, cache *supportCache, maxDests int) GroupResult {
	gr := GroupResult{Hash: g.Hash}

	infos, preSkipped := statAndFilter(g.Files, opts.RepoRoot)
	gr.Files = append(gr.Files, preSkipped...)

	byDev := partitionByDev(infos)
	devs := make([]uint64, 0, len(byDev))
	for dev := range byDev {
		devs = append(devs, dev)
	}
	slices.Sort(devs)

	for _, dev := range devs {
		members := byDev[dev]
		slices.SortFunc(members, func(a, b fileInfo) int {
			return cmp.Compare(a.rel, b.rel)
		})

		if !cache.check(dev, members[0].abs, opts.Logf) {
			for _, m := range members {
				gr.Files = append(gr.Files, FileResult{
					Path:    m.rel,
					Outcome: OutcomeSkipped,
					Reason:  ReasonUnsupportedFS,
				})
			}
			continue
		}

		if len(members) < 2 {
			gr.Files = append(gr.Files, FileResult{
				Path:    members[0].rel,
				Outcome: OutcomeSkipped,
				Reason:  ReasonNoPartners,
			})
			continue
		}

		src := members[0]
		targets := members[1:]
		if gr.Source == "" {
			gr.Source = src.rel
		}

		if opts.DryRun {
			for _, t := range targets {
				gr.Files = append(gr.Files, FileResult{
					Path:         t.rel,
					Outcome:      OutcomePlanned,
					BytesDeduped: t.size,
				})
				gr.BytesReclaimed += t.size
			}
			continue
		}

		results, reclaimed := dedupeSubgroup(ctx, src, targets, maxDests)
		gr.Files = append(gr.Files, results...)
		gr.BytesReclaimed += reclaimed
	}

	gr.Outcome = summariseOutcome(gr.Files, opts.DryRun)
	return gr
}

// statAndFilter runs os.Lstat on every group member. Symlinks,
// irregular files, and stat failures become FileResult skips
// directly; the rest are returned as fileInfo for downstream
// processing.
func statAndFilter(files []string, repoRoot string) ([]fileInfo, []FileResult) {
	infos := make([]fileInfo, 0, len(files))
	var skipped []FileResult
	for _, rel := range files {
		abs := filepath.Join(repoRoot, rel)
		st, err := os.Lstat(abs)
		if err != nil {
			skipped = append(skipped, FileResult{
				Path:    rel,
				Outcome: OutcomeSkipped,
				Reason:  fmt.Sprintf("stat: %v", err),
			})
			continue
		}
		if st.Mode()&os.ModeSymlink != 0 {
			skipped = append(skipped, FileResult{
				Path: rel, Outcome: OutcomeSkipped, Reason: ReasonSymlink,
			})
			continue
		}
		if !st.Mode().IsRegular() {
			skipped = append(skipped, FileResult{
				Path: rel, Outcome: OutcomeSkipped, Reason: ReasonNotRegular,
			})
			continue
		}
		sys, ok := st.Sys().(*syscall.Stat_t)
		if !ok {
			skipped = append(skipped, FileResult{
				Path: rel, Outcome: OutcomeSkipped, Reason: ReasonNoStat,
			})
			continue
		}
		infos = append(infos, fileInfo{
			rel:  rel,
			abs:  abs,
			dev:  sys.Dev,
			size: uint64(st.Size()),
			mode: st.Mode(),
		})
	}
	return infos, skipped
}

// partitionByDev buckets infos by device. The return is a plain map;
// callers sort keys before iterating for deterministic output order.
func partitionByDev(infos []fileInfo) map[uint64][]fileInfo {
	m := make(map[uint64][]fileInfo)
	for _, fi := range infos {
		m[fi.dev] = append(m[fi.dev], fi)
	}
	return m
}

// dedupeSubgroup opens src once, then dedupes targets in waves of
// at most maxDests. Waves cap the open-fd count at maxDests+1 so
// large groups don't breach the default ulimit-n.
func dedupeSubgroup(ctx context.Context, src fileInfo, targets []fileInfo, maxDests int) ([]FileResult, uint64) {
	results := make([]FileResult, 0, len(targets))

	srcFile, err := os.OpenFile(src.abs, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		reason := fmt.Sprintf("source open: %v", err)
		for _, t := range targets {
			results = append(results, FileResult{
				Path: t.rel, Outcome: OutcomeSkipped, Reason: reason,
			})
		}
		return results, 0
	}
	defer srcFile.Close()

	// Re-stat the source to verify size hasn't drifted since the
	// index observation. If it has, we have no plausible donor range.
	if srcInfo, err := srcFile.Stat(); err == nil {
		if uint64(srcInfo.Size()) != src.size {
			reason := ReasonSizeChanged
			for _, t := range targets {
				results = append(results, FileResult{
					Path: t.rel, Outcome: OutcomeSkipped, Reason: reason,
				})
			}
			return results, 0
		}
	}

	var totalReclaimed uint64
	for i := 0; i < len(targets); i += maxDests {
		if err := ctx.Err(); err != nil {
			// Treat unvisited targets as skipped so the result covers
			// every input path exactly once.
			for _, t := range targets[i:] {
				results = append(results, FileResult{
					Path: t.rel, Outcome: OutcomeSkipped,
					Reason: fmt.Sprintf("context: %v", err),
				})
			}
			return results, totalReclaimed
		}
		end := min(i+maxDests, len(targets))
		waveResults, reclaimed := dedupeWave(ctx, srcFile, src.size, targets[i:end])
		results = append(results, waveResults...)
		totalReclaimed += reclaimed
	}
	return results, totalReclaimed
}

// dedupeWave opens up to len(wave) target fds (guaranteed ≤ maxDests
// by the caller), runs the chunked ioctl loop against srcFile, then
// closes every target fd before returning. Skips are recorded for
// targets whose open-time validation fails.
func dedupeWave(ctx context.Context, srcFile *os.File, srcSize uint64, wave []fileInfo) ([]FileResult, uint64) {
	results := make([]FileResult, 0, len(wave))
	active := make([]activeTarget, 0, len(wave))
	opened := make([]*os.File, 0, len(wave))
	defer func() {
		for _, f := range opened {
			_ = f.Close()
		}
	}()

	for _, t := range wave {
		if t.mode.Perm()&0o222 == 0 {
			results = append(results, FileResult{
				Path: t.rel, Outcome: OutcomeSkipped,
				Reason: ReasonReadOnlyFile,
			})
			continue
		}
		f, err := os.OpenFile(t.abs, os.O_RDWR|syscall.O_NOFOLLOW, 0)
		if err != nil {
			results = append(results, FileResult{
				Path:    t.rel,
				Outcome: OutcomeSkipped,
				Reason:  openReason(err),
			})
			continue
		}
		st, err := f.Stat()
		if err != nil {
			_ = f.Close()
			results = append(results, FileResult{
				Path: t.rel, Outcome: OutcomeSkipped,
				Reason: fmt.Sprintf("fstat: %v", err),
			})
			continue
		}
		if uint64(st.Size()) != t.size {
			_ = f.Close()
			results = append(results, FileResult{
				Path: t.rel, Outcome: OutcomeSkipped,
				Reason: ReasonSizeChanged,
			})
			continue
		}
		opened = append(opened, f)
		active = append(active, activeTarget{
			fd:    int(f.Fd()),
			info:  t,
			alive: true,
		})
	}

	if len(active) > 0 {
		runDedupeLoop(ctx, int(srcFile.Fd()), srcSize, active)
	}

	var reclaimed uint64
	for _, a := range active {
		reclaimed += a.bytesDeduped
		switch {
		case !a.alive && a.bytesDeduped == 0:
			results = append(results, FileResult{
				Path: a.info.rel, Outcome: OutcomeSkipped,
				Reason: a.reason,
			})
		case !a.alive:
			results = append(results, FileResult{
				Path: a.info.rel, Outcome: OutcomePartial,
				BytesDeduped: a.bytesDeduped, Reason: a.reason,
			})
		default:
			results = append(results, FileResult{
				Path: a.info.rel, Outcome: OutcomeOK,
				BytesDeduped: a.bytesDeduped,
			})
		}
	}
	return results, reclaimed
}

// runDedupeLoop drives the per-chunk ioctl loop over a single wave
// of targets (guaranteed ≤ maxDests by the caller). It mutates
// active in place: bytesDeduped accumulates on success; alive flips
// to false on DIFFERS, per-target errno, or a fatal top-level error.
func runDedupeLoop(ctx context.Context, srcFd int, srcSize uint64, active []activeTarget) {
	aliveIdx := make([]int, 0, len(active))
	info := make([]unix.FileDedupeRangeInfo, 0, len(active))
	var srcOffset uint64
	for srcOffset < srcSize {
		if err := ctx.Err(); err != nil {
			return
		}

		chunkLen := min(srcSize-srcOffset, uint64(maxChunkBytes))

		aliveIdx = aliveIdx[:0]
		for i := range active {
			if active[i].alive {
				aliveIdx = append(aliveIdx, i)
			}
		}
		if len(aliveIdx) == 0 {
			return
		}

		info = info[:len(aliveIdx)]
		for j, idx := range aliveIdx {
			info[j] = unix.FileDedupeRangeInfo{
				Dest_fd:     int64(active[idx].fd),
				Dest_offset: srcOffset,
			}
		}
		dedupe := &unix.FileDedupeRange{
			Src_offset: srcOffset,
			Src_length: chunkLen,
			Info:       info,
		}

		err := unix.IoctlFileDedupeRange(srcFd, dedupe)
		if err != nil {
			reason := fmt.Sprintf("ioctl: %v", err)
			for _, idx := range aliveIdx {
				active[idx].alive = false
				active[idx].reason = reason
			}
			return
		}

		effective, madeProgress := applyDedupeWaveResults(dedupe.Info, aliveIdx, active)
		if !madeProgress {
			// Kernel returned zero progress for every surviving target.
			// Mark them so the caller classifies them as skipped (or
			// partial, if earlier chunks contributed) rather than OK.
			for _, idx := range aliveIdx {
				active[idx].alive = false
				active[idx].reason = ReasonNoProgress
			}
			return
		}
		srcOffset += effective
	}
}

// applyDedupeWaveResults folds the per-target Info entries returned by
// one IoctlFileDedupeRange call back into active. It returns the
// chunk's effective progress and whether any target made forward
// progress at all (used to decide if the loop should bail).
func applyDedupeWaveResults(infos []unix.FileDedupeRangeInfo, aliveIdx []int, active []activeTarget) (uint64, bool) {
	var effective uint64
	madeProgress := false
	for j, inf := range infos {
		idx := aliveIdx[j]
		switch {
		case inf.Status < 0:
			active[idx].alive = false
			active[idx].reason = "errno " + syscall.Errno(-inf.Status).Error()
		case inf.Status == unix.FILE_DEDUPE_RANGE_DIFFERS:
			active[idx].alive = false
			active[idx].reason = ReasonContentDiffers
		default:
			active[idx].bytesDeduped += inf.Bytes_deduped
			if !madeProgress && inf.Bytes_deduped > 0 {
				effective = inf.Bytes_deduped
				madeProgress = true
			}
		}
	}
	return effective, madeProgress
}

// openReason turns an open(2) error into a short, stable,
// grep-friendly reason string used across FileResult.Reason.
func openReason(err error) string {
	switch {
	case errors.Is(err, os.ErrPermission), errors.Is(err, syscall.EACCES):
		return ReasonPermissionDenied
	case errors.Is(err, syscall.EROFS):
		return ReasonReadOnlyFS
	case errors.Is(err, syscall.ETXTBSY):
		return ReasonTextFileBusy
	case errors.Is(err, syscall.ELOOP):
		return ReasonSymlink
	default:
		return fmt.Sprintf("open: %v", err)
	}
}

// summariseOutcome rolls per-file results into a single group
// outcome. An all-skipped group is skipped; a mix is partial; any
// failed file taints the whole group as failed.
func summariseOutcome(files []FileResult, dryRun bool) Outcome {
	var ok, skipped, failed, partial, planned int
	for _, f := range files {
		switch f.Outcome {
		case OutcomeOK:
			ok++
		case OutcomeSkipped:
			skipped++
		case OutcomeFailed:
			failed++
		case OutcomePartial:
			partial++
		case OutcomePlanned:
			planned++
		}
	}
	switch {
	case failed > 0:
		return OutcomeFailed
	case dryRun && planned > 0 && skipped == 0 && partial == 0:
		return OutcomePlanned
	case dryRun && planned > 0:
		return OutcomePartial
	case ok > 0 && skipped == 0 && partial == 0:
		return OutcomeOK
	case ok > 0 || partial > 0:
		return OutcomePartial
	default:
		return OutcomeSkipped
	}
}
