# Feasibility: kernel-level block dedup for `dcfh` via `FIDEDUPERANGE`

## Executive summary

**Recommendation: build, conditional on per-filesystem support detection.**

The Linux `FIDEDUPERANGE` ioctl (available since kernel 4.5, 2016) lets
userspace ask the kernel to verify two byte ranges are identical and, if so,
atomically share the underlying extents copy-on-write. Users keep independent
files; the filesystem keeps one copy of the blocks. Supported natively on
btrfs, XFS with `reflink=1` (default since 2018), and bcachefs; unsupported
on ext4 and ZFS.

Every critical enabler is already in place:

- `golang.org/x/sys/unix` v0.33.0 — the version `dcfh` already pins in
  `go.mod` — ships complete Go bindings (`IoctlFileDedupeRange`,
  `FileDedupeRange`, `FileDedupeRangeInfo`, `FILE_DEDUPE_RANGE_{SAME,DIFFERS}`).
  No new dependency, no hand-rolled ioctl.
- The kernel verifies byte-equality inside the ioctl before sharing extents.
  Hash collisions and concurrent modifications surface as
  `FILE_DEDUPE_RANGE_DIFFERS` with `bytes_deduped=0`; silent corruption is
  not reachable.
- On this repo's author's real index (179 038 duplicate groups, 658 839
  duplicate files), only **22 groups (0.012 %)** exceed the per-ioctl
  destination cap of 127 on a 4 KiB-page system, and only **43 (0.024 %)**
  exceed 64. Median group size is 3. Batching is trivial for the common
  case, and worst-case (31 948 files in one group) still only needs ~252
  ioctls.
- A smoke-test against the author's btrfs filesystem (appendix A) confirms
  end-to-end: 2 destinations × 4 MiB shared in a single ioctl, `btrfs
  filesystem du` shows `Exclusive=0, Set shared=4 MiB` post-dedupe, and a
  deliberate byte-flip on a destination is correctly refused with status
  `DIFFERS`.

The one load-bearing risk is **architectural, not technical**: `dcfh` has
never before mutated user-owned content. Every existing write path lives
under `.dcfh/`. Introducing dedup adds the first-ever capability to touch
user files. The proposed shape — a dedicated `dcfh dedupe` command with
independent `plan` (informational) and `apply` (write) subcommands,
mirroring `snapshot`'s multi-verb pattern, plus a `--include=cache|scan`
data-source ladder — keeps that capability explicit, opt-in, and distinct
from the read-only `dupes` report. Plan is **not** a prerequisite for
apply; plans only go stale and the kernel's verify step already forbids
corruption.

Rough effort: a new `pkg/dedupe/` helper (~300 lines — FS-detect, per-device
grouping, batcher, ioctl driver), a `cmd/dcfh/dedupe.go` (~200 lines), and
a btrfs-loopback integration test that skips when unavailable (~100 lines).
Zero changes to index format, skiplist, or the scan pipeline.

## Background

A reflink (also "copy-on-write clone") lets two file regions share the same
on-disk extents. Writes COW: the writer gets a new extent, the other file
is untouched. Reflinks are invisible to userspace semantics — files remain
independent, `stat(2)` reports full size for each.

`FIDEDUPERANGE` is the "verify then share" variant. Unlike `FICLONE` (which
trusts the caller and creates a new shared link), it compares the source
and destination ranges byte-for-byte inside the kernel, and shares only if
they match. That makes it the right primitive for a content-addressed dedup
tool like `dcfh`: we find candidates by hash; the kernel confirms before
touching anything.

```
struct file_dedupe_range {
    __u64 src_offset;       //  8 B
    __u64 src_length;       //  8 B
    __u16 dest_count;       //  2 B
    __u16 reserved1;        //  2 B
    __u32 reserved2;        //  4 B
                            // 24 B total
    struct file_dedupe_range_info info[0];
};
struct file_dedupe_range_info {
    __s64 dest_fd;          //  8 B
    __u64 dest_offset;      //  8 B
    __u64 bytes_deduped;    //  8 B  (out)
    __s32 status;           //  4 B  (out: SAME / DIFFERS / -errno)
    __u32 reserved;         //  4 B
                            // 32 B total
};
```

`ioctl(src_fd, FIDEDUPERANGE, &range)` — `src_fd` opened for reading, each
`dest_fd` opened for writing.

## Measured data (this author's environment)

### Duplicate-group size distribution

Source: `dcfh --meta-dir ~/.dcfh dupes --json` (appendix B).

| metric          | value   |
|-----------------|---------|
| total groups    | 179 038 |
| total dup files | 658 839 |
| p50             | 3       |
| p95             | 6       |
| p99             | 8       |
| max             | 31 948  |
| groups > 128    | 22      |
| groups > 64     | 43      |
| groups > 16     | 523     |

Implication: the per-ioctl destination cap of 127 is irrelevant for >99.9 %
of groups. Even the single 31 948-file outlier (a stock-asset set) batches
into 252 ioctls; at typical ioctl latency that's well under a second.

### Per-ioctl destination cap

The manpage (`ioctl_fideduperange(2)`): *"The combined size of the struct
`file_dedupe_range` and the struct `file_dedupe_range_info` array must not
exceed the system page size."*

On x86_64 (4 KiB page): `(4096 - 24) / 32 = 127` destinations per ioctl.

On 16 KiB-page systems (Apple silicon Linux, some ARM64): `(16384 - 24) /
32 = 511` destinations per ioctl. Non-issue in practice but worth
detecting at runtime via `unix.Getpagesize()`.

### Per-range byte cap

Manpage: *"The maximum size of `src_length` is filesystem dependent and is
typically 16 MiB. This limit will be enforced silently by the filesystem."*

Meaning: when calling with `src_length > 16 MiB`, the kernel quietly clamps
and reports the actual `bytes_deduped` per destination. The implementation
must loop per source-offset until the full file is covered, reading
`bytes_deduped` after each call to advance.

Worst-case: a 10 GiB file at 16 MiB per ioctl = 640 ioctls per destination
pair. For most dcfh workloads (duplicate small/medium files), this loop
runs one iteration.

## Answers

### A. Platform / filesystem reach

**A1. Which filesystems.** Native support: btrfs (since Linux 3.12 for the
private `BTRFS_IOC_FILE_EXTENT_SAME`, generalised to `FIDEDUPERANGE` in
4.5), XFS when the filesystem was created with `reflink=1` (default since
xfsprogs 5.1, ~2018), bcachefs (since its mainline introduction), ocfs2.
No support: ext4, ext3, ext2, FAT, exFAT, NTFS (3g fuse), ZFS-native (has
its own dedup, incompatible), tmpfs, procfs, sysfs, most FUSE filesystems.
NFS: kernel-side CoW relay via reflink is a server-config decision and
should be treated as "attempt and gracefully fall back".

**A2. Runtime detection.** Three options:

- **`statfs(2)` / `Statfs_t.Type` allowlist**: fast, pre-open, but
  hard-codes magic numbers (`BTRFS_SUPER_MAGIC=0x9123683e`,
  `XFS_SUPER_MAGIC=0x58465342`, etc.) and misses XFS-without-reflink.
- **Attempt-and-fall-back**: open src + one dest, call
  `IoctlFileDedupeRange` with a 0-byte range, inspect error.
  `EOPNOTSUPP` / `EINVAL` / `ENOTTY` mean "not supported here". Slow per
  call but self-correcting; this is what the `golang.org/x/sys/unix`
  test suite itself does (`syscall_linux_test.go:1125`).
- **Probe once per device**, cache the (Dev → supported?) map. Recommended:
  groups are already partitioned by `Dev`, so one probe per device covers
  all its groups.

Recommendation: probe per device, cache, skip unsupported devices with a
single warning. `statfs` is not strictly needed.

**A3. User-base distribution.** Not investigated — we have no telemetry.
Qualitative guess: `dcfh`'s user base skews toward power users and
developers who are more likely to run btrfs / XFS-reflink on personal
machines than the population average. The author's own dev machine is
btrfs root, and `--meta-dir` already exists so users can target external
reflink-capable volumes. Even if the fraction is <25 %, the feature is
purely additive — non-reflink users pay nothing, and the plan command is
useful everywhere (it reports savings even when apply is a no-op).

### B. Kernel-interface constraints

**B4. Destination count cap.** 127 on 4 KiB-page systems, 511 on 16 KiB.
Real-world impact from the measured distribution: 22 / 179 038 groups
(0.012 %) exceed 127, needing batching. The extreme case (31 948-file
group) batches into ~252 ioctls, which is not a performance concern.

**B5. Per-ioctl byte-length cap.** Typically 16 MiB per `src_length`;
kernel clamps silently and reports actual `bytes_deduped`. The batcher
must loop: advance `src_offset` by `bytes_deduped`, repeat until the file
is fully covered or `bytes_deduped == 0` (which indicates `DIFFERS`).

**B6. Kernel-side verify makes hash collisions moot.** Manpage: *"If
even a single byte in the range does not match, the deduplication
operation request will be ignored and `status` set to
`FILE_DEDUPE_RANGE_DIFFERS`."* The dcfh hash (SHA-1/256/512) is used
only to *find candidates*; the kernel independently verifies before
sharing. Collision at the dcfh layer produces a no-op, never corruption.
Confirmed experimentally: appendix A shows a mutated destination correctly
returning `status=DIFFERS, bytes_deduped=0`.

**B7. Partial failure across batched ioctls.** Each ioctl is atomic for
its own (`src_range`, one-or-more `dest_range`) set — either every
destination in that call reports per-destination status, or the ioctl
returns an error and no destination was modified. Across multiple ioctls
for one group (e.g. 200 files split into two batches of 127 + 73), an
error on batch 2 leaves batch 1's dedups committed. The apply step must
report per-destination status; there is no rollback, but also no loss of
data (reflinks are semantically invisible, so a partial apply just means
less storage was reclaimed than planned).

### C. Permissions, ownership, file state

**C8. Write permission on destinations.** Required by the kernel:
`dest_fd` must be opened for writing. For read-only files the user owns,
three options:

- **Refuse**: simplest; user can `chmod u+w` first.
- **Silently chmod+ioctl+chmod**: invasive, races with concurrent
  readers' assumptions, violates principle of least surprise.
- **Skip and report**: probably right — the plan step already lists
  affected files; apply can emit "skipped (read-only): path" lines.

Recommendation: refuse by default with a clear error; add `--force-mode`
in a later iteration only if users actually ask.

**C9. Files owned by another user.** Non-root `dcfh dedupe` cannot open
them O_WRONLY. Detect at plan time via `access(path, W_OK)` / `unix.Faccessat`
and categorise as "skipped (not writable)". Do not attempt and report
per-ioctl failure, since that requires opening the source fd first.

**C10. Open-fd pressure.** A batch of 127 destinations + 1 source = 128
fds held open simultaneously. Default Linux `ulimit -n` is 1024, often
raised to 65 536 on modern distros. Non-issue in practice. Document the
cap in the help text. A `--max-fds N` flag is low-priority polish.

**C11. Hardlink interaction.** Hardlinks already share blocks — the whole
file's content is one inode, no dedup gain possible. The existing
`IgnoreHardlinks` filter (pkg/dupes.go:30, shipped last commit) is the
right gate: `dcfh dedupe` should set it implicitly, never offering to
"dedup" files that are already the same inode. This also avoids the
kernel refusing same-file dedup (`EINVAL` for overlapping ranges in one
file, per manpage).

### D. dcfh integration shape

**D12. Command surface.** `dcfh dedupe` as a top-level command with two
subcommands:

- `dcfh dedupe plan [paths...]` — dry-run. Reports per-filesystem dedup
  candidates, expected bytes reclaimed, skipped files and why.
- `dcfh dedupe apply [paths...]` — does it. Requires either `--yes`
  confirmation or a recent `plan` hash (timing-based staleness check
  later).

Mirrors `snapshot` (multi-verb mutation). Distinct from read-only `dupes`.

**D13. Source-file selection policy.** Propose: **first path-sorted
member per group** (deterministic; matches existing `dedupByInode`
behaviour). Policy flag `--source=path|largest|oldest|random` for later
if demanded. The choice rarely matters — all members are byte-identical;
the "donor" is just the fd we happen to open as `src_fd`. Kernel shares
extents equally regardless.

**D14. Filter reuse.** `DupeFilter` already carries `IgnoreHardlinks`,
`MinSize`, `MaxSize`, `Paths`/`Exclusive`, `StartTime`/`EndTime`. Dedup
inherits them verbatim via `GroupsRequest` (pkg/repo.go:90). No
`DedupeFilter` type needed; `dedupe plan`/`apply` just pass a `DupeFilter`
with `IgnoreHardlinks=true` forced and probably a sensible `MinSize`
default (dedup of <4 KiB files buys nothing — the extent is a single
block already).

**D15. Per-device grouping.** Dedup candidates must share a filesystem.
`binaryEntry.Dev uint32` is already populated (pkg/util.go:77). Partition
each post-bucket group by `Dev` before scheduling ioctls. Cleanest spot:
a new `splitGroupsByDevice` pass in `pkg/dedupe/`, *not* in
`emitHashGroup` — `dupes` reporting doesn't care about cross-FS
boundaries; only `dedupe` does.

**D16. Failure-reporting UX.** Apply output, one line per group:

```
[OK]   4 of 4 files deduped, reclaimed 12 MiB — group abc1234…
[WARN] 2 of 3 files deduped, 1 skipped (concurrent modification) — group def5678…
[SKIP] 0 of 2 files deduped (cross-filesystem, source dev=btrfs-1 dests=xfs-2) — group ghi9abc…
```

JSON mode: a `DedupeResult[]` with per-group status, per-destination
outcome, and running totals.

**D17. Stats.** Report two numbers:

- *Potential reclaim*: `Σ (group.count − 1) × file_size`. Computed pre-apply.
- *Actual reclaim*: `Σ bytes_deduped` across all ioctls. Computed from
  kernel returns.

They diverge when some extents were already shared (e.g. from a previous
`dedupe apply` or a `cp --reflink=auto`). Reporting both makes the "why
is this number smaller" question self-answering.

### E. Safety / rollback / user trust

**E18. No undo.** Correct — once extents are shared, you can't "un-share"
without a full rewrite. But: reflinks are userspace-invisible. `stat` still
reports the same sizes, `md5sum` still computes the same hashes, every
write COWs back to an independent extent. The only user-visible effect is
reduced `df` usage. `--dry-run` plan output is sufficient protection; no
pre-apply filesystem snapshot needed. (Btrfs/XFS users who want extra
belt-and-braces can take a filesystem snapshot manually before running
apply; document this in the command help.)

**E19. dcfh snapshots vs filesystem snapshots.** Clarify in docs:
`dcfh snapshot` captures *index state* (which files were known to dcfh at
that moment with which hashes), not the file data. It does not protect
against filesystem-level loss. Dedup does not interact with dcfh snapshots
at all — the index is unchanged by an `apply` run (paths still point to
the same inodes, which now share extents). The next `dcfh update` is a
no-op for deduped files (mtime/size/inode unchanged).

**E20. Concurrent modification.** Kernel verify step rejects
(`status=DIFFERS`). Confirmed experimentally (appendix A). No silent
corruption possible. The `plan` → `apply` gap is not a vulnerability;
worst case an apply reports more `DIFFERS` outcomes than plan predicted.

### F. Effort estimate

**F21. Ioctl wrapper.** None needed. `golang.org/x/sys/unix.IoctlFileDedupeRange`
exists at v0.33.0 (our pin). Our wrapper is just a batcher over the sys
call and a per-device/per-group orchestrator.

**F22. Test matrix.** Integration test requires a reflink-capable
filesystem. Options:

- **Skip-if-unavailable** (used by sys/unix itself): create two files,
  attempt a 0-byte dedupe, `t.Skip` on `EOPNOTSUPP` / `EINVAL` / `ENOTTY`.
  CI pipelines on btrfs-backed runners exercise the full path; others
  skip. Low friction, adequate coverage.
- **Loopback btrfs fixture**: `losetup` + `mkfs.btrfs` + `mount` in a
  CI-only helper. Requires privileged runner. Higher confidence; higher
  setup cost.

Recommend skip-if-unavailable; author's dev machine (btrfs) exercises it
locally on every `go test ./...`.

**F23. Ballpark code size.**

| component                               | LOC estimate |
|-----------------------------------------|--------------|
| `pkg/dedupe/fs_detect.go` (probe cache) |  80          |
| `pkg/dedupe/batcher.go` (ioctl loop)    | 180          |
| `pkg/dedupe/plan.go` (group → plan)     | 120          |
| `pkg/dedupe/plan_test.go`               | 150          |
| `pkg/dedupe/integration_test.go`        | 120          |
| `cmd/dcfh/dedupe.go` (plan + apply)     | 220          |
| `pkg/repo*.go` (Dedupe method)          |  60          |
| **total**                               | **~930 LOC** |

No changes to index format, skiplist, scan pipeline, callback layer, or
update/status commands.

## Proposed shape

```
dcfh dedupe plan  [paths...]  [filters]  [--include=cache|scan] [--json]
dcfh dedupe apply [paths...]  [filters]  [--include=cache|scan] [--yes] [--max-fds N]
```

Two standalone subcommands: `plan` is purely informational (what would
apply do, how many bytes would it reclaim), `apply` is the write path.
`plan` is **not** a prerequisite for `apply` — plans would only go stale
and the kernel's verify step already forbids corruption, so "run apply
directly, require `--yes` or TTY confirmation" is the safer ergonomic
choice. Folding into `dcfh update` was considered and rejected: `update`
is currently read-only against user content, and a failing ioctl must
never be able to leave the index half-written.

### Data-source ladder (`--include`)

All three modes reuse the existing Hwang-Lin comparison pipeline
(left = indexed view, right = scan stream) that `update`/`status`
already run on. Safety is identical in every mode — the kernel's
`FILE_DEDUPE_RANGE_DIFFERS` status catches any staleness — so this is
purely a freshness-vs-cost dial.

| mode                  | left (indexed)         | right (fresh) | cost    |
|-----------------------|------------------------|---------------|---------|
| *default* (no flag)   | `main.idx`             | —             | cheapest |
| `--include=cache`     | `merge(main, cache(s))`| —             | +cache scan |
| `--include=scan`      | `merge(main, cache(s))`| fresh scan    | +full scan |

`--include=scan` subsumes `--include=cache` (you can't freshly scan
without also reading the cache delta), so the flag is a single enum, not
a set.

### Filters

Every `DupeFilter` field carries over verbatim via `GroupsRequest`
(pkg/repo.go:90) — no `DedupeFilter` type is introduced. Supported
filters:

- `[paths...]` positional args + `--exclusive=yes|no`
- `--min-size N` / `--max-size N` (binary suffixes K/M/G/T)
- `--start-date` / `--end-date` (partial ISO-8601) + `--tz`
- `--ignore-hardlinks` is implicitly forced on (hardlinks already share
  blocks; re-sharing is a no-op that wastes ioctls and the kernel would
  reject overlapping-range calls within one inode anyway)

Proposed default: `MinSize=4096`. Dedup of sub-block files buys nothing
(they already share a single extent block) and inflates the ioctl count.

### Internal call path (mirrors `dcfh dupes`)

```
cmd/dcfh/dedupe.go
  └─ repo.Dedupe(ctx, DedupeRequest{Filter, Include, Apply bool}) DedupeResult
       └─ pkg/dedupe.Plan(ctx, groups []DuplicateGroup) DedupePlan
            └─ splitByDevice → probe each device → filter unsupported
       └─ (if Apply) pkg/dedupe.Execute(ctx, plan) DedupeResult
            └─ for each (device, group): batcher.Run(src, dests[])
                 └─ unix.IoctlFileDedupeRange per chunk ≤127 dests × 16 MiB
```

The `groups` input is obtained exactly as `dcfh dupes` already does,
parameterised by `Include` to select the data-source ladder above.

## Risks and unknowns

- **Real-world FS distribution** (A3): we have no telemetry. Easy to
  backfill by adding an `fs_type` histogram to `dcfh status` output later.
- **Very-large-file behaviour** beyond 10 GiB: not measured locally. The
  per-range loop scales linearly; worst-case latency is a function of
  file size, not `dcfh` logic. Worth a one-off experiment with a sparse
  100 GiB file before shipping `apply`.
- **XFS semantics** vs btrfs: experiment ran on btrfs only. Manpage and
  kernel source indicate identical behaviour; a one-off test on an XFS
  volume before release is prudent.
- **NFS / overlayfs / FUSE**: documented as "attempt and fall back", but
  worth ensuring the fall-back message is clear. Could be addressed in a
  second iteration.
- **Billion-file indices**: the measured distribution is from a 658k-dup
  index; at 100× scale the batching loop runs fine but the fd cap and
  ioctl latency add up. A `--max-concurrent-groups N` throttle may be
  prudent; defer until someone hits it.

## Appendix A — Smoke test transcript

Host: `Linux 6.17.5-061705-generic`, `/home` on btrfs:

```
$ df -T /home
Filesystem     Type   1K-blocks      Used Available Use% Mounted on
/dev/nvme0n1p2 btrfs 1999873024 413596208 1579938736  21% /
```

Test program (`/tmp/dedupe_smoke/main.go`): writes three 4 MiB files of
random data, calls `IoctlFileDedupeRange` with 2 destinations, measures
extent sharing with `btrfs filesystem du`, then mutates one destination
and re-runs to confirm kernel verify.

```
$ go run . /home/matt/tmp-dedupe-smoke
btrfs usage before dedupe:
     Total   Exclusive  Set shared  Filename
     0.00B       0.00B       0.00B  src.bin      [extents not yet flushed]
     0.00B       0.00B       0.00B  dst1.bin
     0.00B       0.00B       0.00B  dst2.bin
dest[0]: bytes_deduped=4194304 status=SAME
dest[1]: bytes_deduped=4194304 status=SAME
btrfs usage after dedupe:
     Total   Exclusive  Set shared  Filename
   4.00MiB       0.00B     4.00MiB  src.bin
   4.00MiB       0.00B     4.00MiB  dst1.bin
   4.00MiB       0.00B     4.00MiB  dst2.bin
mutated-dest call: bytes_deduped=0 status=1 (expect DIFFERS=1)
```

Interpretation:

- Both destinations shared successfully in a single ioctl.
- Post-dedupe, all three files report `Exclusive=0 B, Set shared=4 MiB` —
  i.e. no exclusive blocks, every block is shared.
- After deliberately writing a different byte to `dst1.bin`, re-calling
  the ioctl correctly returns `status=1` (`FILE_DEDUPE_RANGE_DIFFERS`),
  `bytes_deduped=0`. The kernel refused to share mismatched content.

Program: `/tmp/dedupe_smoke/main.go` (preserved for reproduction).

## Appendix B — Duplicate-group distribution

Command:

```
dcfh --meta-dir ~/.dcfh dupes --json |
  python3 -c 'import json,sys,collections;
             d=json.load(sys.stdin);
             g=d["duplicate_groups"];
             s=sorted([x["count"] for x in g], reverse=True);
             print("total:", len(s));
             print("max:", s[0]);
             print("p50/p95/p99:", s[len(s)//2], s[len(s)*5//100], s[len(s)//100]);
             print(">128:", sum(1 for x in s if x>128));
             print(">64:", sum(1 for x in s if x>64));
             print(">16:", sum(1 for x in s if x>16));
             print("top-20:", s[:20])'
```

Output (author's `~/.dcfh`, 2026-04-24):

```
total: 179038
max: 31948
p50/p95/p99: 3 6 8
>128: 22
>64: 43
>16: 523
top-20: [31948, 3945, 573, 536, 527, 270, 267, 229, 177, 174,
         172, 168, 155, 148, 147, 142, 141, 135, 135, 135]
```

## Appendix C — Reference links

- Manpage: `ioctl_fideduperange(2)` — `/usr/share/man/man2/ioctl_fideduperange.2.gz`
  (Linux man-pages 6.7, 2024-03-03).
- Kernel UAPI header: `include/uapi/linux/fs.h` — defines
  `struct file_dedupe_range`, `file_dedupe_range_info`,
  `FILE_DEDUPE_RANGE_SAME`, `FILE_DEDUPE_RANGE_DIFFERS`, and
  `FIDEDUPERANGE` itself.
- Go bindings: `golang.org/x/sys@v0.33.0/unix/ioctl_linux.go:188-250`
  (`FileDedupeRange`, `FileDedupeRangeInfo`, `IoctlFileDedupeRange`).
- Reference test: `golang.org/x/sys@v0.33.0/unix/syscall_linux_test.go:1074`
  (`TestIoctlFileDedupeRange`).
- dcfh grounding:
  - `pkg/util.go:77` — `binaryEntry.Dev uint32`.
  - `pkg/dupes.go:30` — `DupeFilter.IgnoreHardlinks`.
  - `pkg/dupes.go:214` — `dedupByInode` for the inode-equivalence primitive.
  - `pkg/index.go:1191` — existing `unix.Syscall` site (pattern reference).
  - `cmd/dcfh/snapshot.go` — multi-verb mutation-command pattern.
  - `go.mod` — `golang.org/x/sys v0.33.0` already pinned.
