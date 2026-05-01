# POSIX Support: Replacing `mremap(2)` with Virtual-Address Pre-Reservation

## Status

Feasibility study, not a commitment to implement. Captures the design
thinking from a long working session so a future port to non-Linux
POSIX platforms (BSDs, macOS) has a starting point with the tradeoffs
already costed.

## Glossary

- **VA** — virtual address (space).
- **Pre-reserve** — `mmap(NULL, N, PROT_NONE, MAP_ANONYMOUS|MAP_PRIVATE)`
  once, then incrementally `mmap(reserveBase, growSize, PROT_RW,
  MAP_FIXED|MAP_SHARED, fd, 0)` to install real content over chunks of
  the reserved range.
- **Scan index** — `.dcfh/scan-PID-TID.idx`, the read-write mmap that
  collects new `binaryEntry` records during a scan. The only index
  type that grows during normal operation.
- **Reserve exhaustion** — the new failure mode introduced by
  pre-reserve: scan would need to grow past the pre-allocated VA range.

---

## Part 1: Requirements

### 1.1 Background and motivation

Today `pkg/` is implicitly Linux-only. The hard dependency is
`unix.Mremap` with `MREMAP_MAYMOVE`, used by
`appendEntryToScanIndex`'s growth path to expand the scan index's
read-write mmap as the scan emits new entries. `mremap(2)` is a Linux
extension — not in POSIX, not on FreeBSD/NetBSD/OpenBSD/macOS — and
its absence cascades into a wider portability story:

- `pkg/index.go:958` — `unix.Mremap` and `unix.MREMAP_MAYMOVE`
  undefined on every non-Linux GOOS.
- `pkg/index.go:345-346`, `pkg/binary_entry_scan.go:67-68`,
  `pkg/recovery.go:835`, `pkg/wire_handler.go:174-175,311-312`,
  `pkg/walker_wire.go:186-192` — `syscall.Stat_t` field names
  (`Mtim`/`Ctim`/`Dev` typing) differ between Linux and BSD-derived
  kernels. macOS uses `Mtimespec`/`Ctimespec`; `Dev` is `int32` vs
  `uint64`.

A successful port would lift `pkg/` to be GOOS-agnostic across all
mainstream POSIX targets, not just Linux.

### 1.2 Functional requirements

1. `pkg/` compiles and tests pass on **Linux, FreeBSD, NetBSD,
   OpenBSD, macOS** at minimum. Solaris/illumos welcome but not gated.
2. All existing `dcfh` commands (`init`, `status`, `update`, `dupes`,
   `snapshot`, `config`) behave identically on every supported
   platform. No platform-specific feature gaps in the daily workflow.
3. **Existing `main.idx` files remain readable**. No on-disk format
   change. Linux users with prior repos must be able to run a new
   build without index rebuild.
4. Same correctness guarantees as today: zero-copy reads of mmap'd
   indices, atomic main-index replacement, recovery from interrupted
   scans, hash-collision-proof dedupe (where applicable).

### 1.3 Non-functional requirements

1. **Single source tree**. Build-tag splits used only where the
   underlying kernel API genuinely diverges (e.g. `Statfs_t` field
   names). The bulk of `pkg/` should be tag-free.
2. **No regression on Linux**. Performance, memory footprint, and
   failure modes on Linux must be at parity or better.
3. **Simpler locking model**. The dual-level RWMutex subsystem
   (CLAUDE.md "Memory Protection and Locking Mechanism") exists
   solely because `mremap` may relocate the mapping. Replacing
   `mremap` should let us delete that subsystem rather than port it.
4. **Bounded virtual-address usage**. Pre-reserve trades VA for
   simplicity; the trade has to stay favourable on cgroup-constrained
   tenants and in default-`RLIMIT_AS` shells.
5. **Graceful failure on reserve exhaustion**. The new failure mode
   must surface as a clean error with a remediation hint, never as
   SIGSEGV/SIGBUS or silent data loss.

### 1.4 Out of scope

- **Windows.** A Windows port needs `MapViewOfFile`/
  `VirtualAlloc(MEM_RESERVE)` semantics that don't trivially mirror
  POSIX `mmap(MAP_FIXED)`. Treat as a separate feasibility study.
- **Real-time file event monitoring.** `fanotify(7)` (Linux),
  `FSEvents` (macOS), `kqueue+EVFILT_VNODE` (BSDs),
  `ReadDirectoryChangesW` (Windows) would be valuable for live-watch
  use cases (e.g. `/tmp` churn analysis on a build server) but live
  in a parallel subsystem to dcfh, not inside the index machinery.
- **tmpfs as a target.** dcfh's value proposition is "what changed
  since last time"; tmpfs doesn't survive reboot, so the answer is
  always "everything". User error, not a sizing edge case.
- **Network mounts (NFS/SMB/FUSE) as primary targets.** Their
  consistency and latency stories don't compose well with the
  snapshot-and-diff model. Run dcfh on the server side instead.
- **32-bit targets.** Pre-reserve assumes a 64-bit address space.
  dcfh already requires 64-bit (CLAUDE.md "System Requirements") so
  this is no new constraint.

---

## Part 2: Design

### 2.1 The core substitution

| | Today (Linux only) | Pre-reserve (portable) |
|---|---|---|
| Initial mmap | `mmap(NULL, initSize, PROT_RW, MAP_SHARED, fd, 0)` | `mmap(NULL, reserveSize, PROT_NONE, MAP_ANONYMOUS\|MAP_PRIVATE, -1, 0)` |
| Growth (per chunk) | `ftruncate(fd, newSize)` + `mremap(addr, oldSize, newSize, MREMAP_MAYMOVE)` | `ftruncate(fd, newSize)` + `mmap(reserveBase, newSize, PROT_RW, MAP_FIXED\|MAP_SHARED, fd, 0)` |
| Address stability | May move on each grow | Stable for the entire scan |
| Locks needed for relocation safety | Yes (dual-level RWMutex) | None |
| Failure modes | OOM / disk-full | OOM / disk-full / **reserve-exhausted** |
| Linux-portable | No | Yes |
| BSD/macOS-portable | No | Yes |

The pre-reserve approach uses only `mmap(2)` and `ftruncate(2)`, both
POSIX-mandated. There is no surface that differs between Linux and
BSDs/macOS in the growth path itself.

### 2.2 Why pre-reserve over the alternatives

Three families of approaches were considered:

#### 2.2.1 Pre-reserve VA + `MAP_FIXED` (chosen)

**How**: Reserve a generous VA chunk up front via `PROT_NONE`
anonymous mmap. The reservation commits VA but no physical memory or
swap. As the scan-index file grows, install real `MAP_SHARED` content
over chunks of the reserve via `MAP_FIXED`. Address never changes.

**Pros**:
- Single, contiguous logical mmap from the consumer's perspective —
  matches today's offset-from-base pointer arithmetic.
- No relocation, so the entire mmap-protection locking subsystem
  (CLAUDE.md §1-8 of Memory Protection) becomes unnecessary on every
  platform, including Linux.
- Hot-path append cost is unchanged (~zero overhead).
- One growth-time syscall (`mmap(MAP_FIXED)`) instead of one per
  growth (`mremap`), and it's POSIX-portable.

**Cons**:
- Reserves a chunk of VA up front. On 64-bit POSIX this is essentially
  free (256 TB of user VA on x86-64 with 4-level paging) but visible
  in `pmap`/`vmmap` and can interact with `RLIMIT_AS` / cgroup limits.
- Introduces a new failure mode — reserve exhaustion — when the
  heuristic under-sizes the reservation.

#### 2.2.2 `munmap` + `mmap` cycle

**How**: When growth needed, `munmap` the current mapping, `ftruncate`
the file, `mmap` fresh at a new address.

**Pros**: No VA reserved up front.

**Cons**:
- New address every grow. Every existing pointer into the old mapping
  is invalid. Hash workers must reload base pointers post-grow,
  requiring tighter synchronisation than today (a hard quiesce, not
  just a write-lock window).
- Defeats the goal of dropping the locking subsystem — actually
  *increases* locking complexity.

**Verdict**: Worse than today on Linux; no portability win sufficient
to justify the regression.

#### 2.2.3 Chained mappings

**How**: Each `ftruncate` grows the file by a fixed delta; `mmap` just
the new tail at a fresh address. The "logical buffer" is a list of
mappings rather than one contiguous region.

**Pros**: Each new chunk's pointers stay valid forever.

**Cons**:
- Pointer arithmetic across chunk boundaries breaks. Every offset →
  pointer conversion needs a chunk lookup.
- Linux's default `vm.max_map_count` (65 530) would cap the scan
  size — at typical chunk sizes you'd hit it well before exhausting
  modern disks.
- Profiles poorly: each chunk lookup is a branch on the hot path.

**Verdict**: Solves portability at the cost of hot-path performance
and a kernel-tunable runtime ceiling.

#### 2.2.4 NetBSD's `mremap()`

NetBSD does have a `mremap()` syscall, but with a different signature
and no `MREMAP_MAYMOVE` equivalent. Source-incompatible with the Linux
version. Using it would require a NetBSD-specific build-tag split that
captures none of the simplification benefits and ports to nowhere
else.

**Verdict**: Not viable as a substrate for general POSIX support.

### 2.3 Sizing the reserve

Reserve size is a heuristic; the goal is "right order of magnitude",
not "tight fit". Generous over-reserve costs nothing on 64-bit;
under-reserve costs a clean abort with a flag suggestion.

#### 2.3.1 Three-layer fallback

```
1. prior_main_idx_size × 8                 (steady state, dominates)
2. (f_files - f_ffree) × ~140 B × slop     (first scan, ext4/XFS)
3. (f_blocks - f_bfree) × f_bsize × ~1%    (everything else)
```

Capped at `min(RLIMIT_AS / 2, 256 GB)`; user can override with
`--scan-reserve N`.

#### 2.3.2 Tradeoffs by signal

| Signal | Coverage | Accuracy | Why or why not |
|---|---|---|---|
| Prior `main.idx` size | Subsequent scans (≥99% of invocations) | Empirical, exact within scan-to-scan variation | Best signal whenever available; ignores filesystem semantics entirely. |
| `f_files - f_ffree` (used inodes) | First scan on ext4/XFS | Exact | Inode-count is the *correct* index-size driver (one entry per file), but reporting is filesystem-dependent. |
| `f_files - f_ffree` on btrfs/ZFS | First scan on dynamic-inode FS | Unreliable | Dynamic inode allocation means sentinel values, version-dependent semantics. Don't rely on it where it's wrong. |
| Disk usage × ~1% | Universal first-scan fallback | Order-of-magnitude estimator | Worse predictor than inode count (file count and content size are uncorrelated) but well-defined on every POSIX FS. |
| `--scan-reserve N` | Always | User-supplied | The escape hatch. Heuristic only needs to be "right most of the time" while this exists. |

#### 2.3.3 Mount enumeration

Cross-mount scanning is a corner case (matters only when
`--symlinks=internal/external` traverses a sub-mount). Three options:

- **GOOS-split mount enumeration** (`/proc/self/mountinfo` on Linux,
  `getmntinfo(3)` via `unix.Getfsstat()` on BSDs/macOS, `getmntent`
  on Solaris). Accurate but ~5 GOOS-specific source files.
- **POSIX stat-walk** (`stat()` everything, find sub-mounts via
  `st_dev` change). Universal but defeats the purpose — it's
  essentially a pre-scan minus the hashing.
- **Single statvfs + `--scan-reserve` flag** (chosen). One syscall on
  the scan root, accept the under-reserve when crossing mounts;
  expect users with cross-mount scans to override via the flag.
  Upgrade to GOOS-split when a real complaint surfaces.

**Decision**: Single statvfs at scan root for v1. Defer mount
enumeration to v2 (or v∞).

### 2.4 Locking simplifications enabled

The dual-level RWMutex subsystem (CLAUDE.md "Memory Protection and
Locking Mechanism") exists because `mremap` may relocate the mapping
mid-read. With pre-reserve, addresses are stable for the entire scan
lifetime. That reasoning collapses:

| Today's lock | Why needed | Status under pre-reserve |
|---|---|---|
| `binaryEntryRef.GetBinaryEntry()` per-entry RLock | "Memory mustn't be moved during offset → pointer conversion" | **Unnecessary**. Address stable from reserve time; offset → pointer is `base + offset`, always valid up to the entry-count high-water mark. |
| `writeSkiplistWithVectorIOFiltered`'s bulk RLock acquisition (sorted by address) | "Prevent mremap during IoVec generation/hash calculation" | **Unnecessary**. No mremap; no exclusion window required. |
| Write-lock around `unix.Mremap` in `appendEntryToNamedIndex` | "Exclude readers during relocation" | **Replaced**. Growth becomes `ftruncate` + `mmap(MAP_FIXED)`. The MAP_FIXED install is atomic under the kernel's `mmap_lock`; readers either see PROT_NONE (and shouldn't be reading past entry-count anyway) or the new mapping. No userspace lock needed. |
| 5-second lock timeout + "operation continues without protection" fallback | Last-ditch deadlock avoidance | **Vanishes**. No locks → nothing to time out. |

Performance implication on Linux: removing per-entry RLock/RUnlock
removes two atomic operations per `GetBinaryEntry()` call.
On a 2.7M-entry index iterated by N hash workers that's ~5.4M × N
atomics saved, plus elimination of the cache-line ping-pong on the
RWMutex's reader counter. Theoretical scan-time win is order
**5–15 % multi-core, more on multi-socket** — not benchmarked, but
the direction is unambiguous.

The **scan-only** read path benefits most because that's where the
RLock/RUnlock pair runs hottest. The merge-finalize path
(`writeSkiplistWithVectorIO`) is `writev`-only on the destination and
benefits from source-side stability without needing any locks at all.

### 2.5 Failure mode: reserve exhaustion

The bail contract sits cleanly on top of dcfh's existing scan-index
lifecycle:

1. **Prophylactic check** at the existing growth gate, before
   `ftruncate`. One additional `if` on the per-chunk-growth path
   (already rare). Zero cost on the per-entry append hot path.

   ```go
   if newSize > current {
       if newSize > reservedSize {
           return ErrReserveExhausted
       }
       ftruncate(fd, newSize)
       mmap(reserveBase, newSize, PROT_RW, MAP_FIXED|MAP_SHARED, fd, 0)
   }
   ```

2. **Standard scan shutdown**: `ErrReserveExhausted` propagates up the
   call stack like any other Go error. Hash worker pool drains via the
   existing shutdown protocol (close jobs channel, wait with timeout,
   close completion).

3. **Existing scan-index file is valid as far as it goes**. Each
   appended `binaryEntry` is individually correct: real file, real
   hash, real metadata. dcfh's recovery model (CLAUDE.md "Data Flow"
   §6, `pkg/recovery.go`) already takes the union of whatever
   scan/cache/main entries are available with per-entry validation.
   The early-terminated scan index is consumed as ordinary cached
   state on the next run — no special "partial scan" flag, no
   distinguished failure-recovery path.

4. **`main.idx` untouched**. The atomic-rename gate stays closed on
   the failure path. Existing "Main Index Integrity" invariant
   preserved for free.

5. **User-facing error with empirical remediation**:
   ```
   error: scan exhausted --scan-reserve (4 GB) at 28,431,902 entries
   rerun with --scan-reserve 8G (or larger)
   ```
   The entry count comes from observed truth, not a re-estimate, so
   the suggestion is calibrated against the actual workload.

The deeper invariant: pre-reserve makes growth *bounded* but doesn't
change the "main.idx is only updated on full success" rule. Reserve
exhaustion is just early termination, which dcfh already handles.

### 2.6 Tradeoff summary

| Concern | Today (mremap) | Pre-reserve | Resolution |
|---|---|---|---|
| Portability | Linux-only | POSIX-portable | Pre-reserve. Primary motivation. |
| Locking complexity | Dual-level RWMutex + timeout fallback | None for relocation safety | Pre-reserve. Major simplification. |
| Hot-path append cost | RLock + atomic | Single indirect load | Pre-reserve. Faster, especially multi-core. |
| VA usage | Tight (~current size) | Generous (~8× steady state) | Pre-reserve, with `--scan-reserve` override. VA cheap on 64-bit. |
| Failure modes | OOM/disk-full | OOM/disk-full + **reserve-exhausted** | New failure handled cleanly via existing scan-index recovery. |
| First-scan sizing accuracy | N/A | Heuristic, generously over-reserved | Three-layer fallback + flag escape hatch. |
| Cross-mount sizing | N/A | Under-estimates without enumeration | Single statvfs in v1; flag override; GOOS-split enumeration deferred. |
| On-disk format | n/a | unchanged | Existing `main.idx` files remain readable. No migration. |

---

## Part 3: Implementation

### 3.1 Source layout

The bulk of `pkg/` becomes platform-agnostic. Build-tag splits remain
only where kernel APIs actually diverge.

**New files** (no build tag):

- `pkg/scan_growth.go` — pre-reserve and growth helpers:
  - `reserveScanVA(size uint64) (base unsafe.Pointer, err error)`
  - `growScanMapping(base unsafe.Pointer, fd int, oldSize, newSize uint64) error`
  - `releaseScanVA(base unsafe.Pointer, size uint64) error`
  - `func estimateScanReserve(repoMetaDir, scanRoot string, opts ReserveOpts) (uint64, error)`
  - `var ErrReserveExhausted = errors.New("scan exhausted pre-reserved virtual address range")`
- `pkg/scan_growth_test.go` — table-driven sizing-heuristic tests
  with mocked `statvfs` + prior-`main.idx` byte counts.

**Modified files**:

- `pkg/index.go` — replaces the `mremap` growth call site with
  `growScanMapping`. Removes `unix.Mremap` / `unix.MREMAP_MAYMOVE`
  imports. Drops `Build linux` constraint.
- `pkg/util.go` — `mmapIndexFile` struct loses `mutex sync.RWMutex`
  (no longer needed for relocation safety) but keeps any locking
  required by other invariants (e.g. unrelated bookkeeping). Audit
  every `mutex.Lock()` / `RLock()` call in the codebase, document why
  each remains or remove it.
- `pkg/binary_entry_index_file.go` — `GetBinaryEntry()` drops its
  per-call RLock/RUnlock, becomes a plain offset → pointer
  conversion.
- The temp-index writer (now in the pipeline layer:
  `pkg/pipeline_update.go` / `pkg/temp_index_writer.go`) — drop any
  bulk RLock acquisition phase around vectorio writes.
- `pkg/dircache.go` — removes `indexLockTimeout` field, tracking
  registration of indices for lock acquisition is no longer
  load-bearing (kept only for cleanup bookkeeping).

**Removed files**:

- None. Everything is rewrite-in-place; no source files are entirely
  retired.

**GOOS-split files** (new, only where genuinely required):

- `pkg/stat_linux.go` / `pkg/stat_bsd.go` / `pkg/stat_darwin.go` —
  `binaryEntry` ↔ `syscall.Stat_t` conversion helpers covering the
  `Mtim`/`Mtimespec`/`Ctim`/`Ctimespec` and `Dev` typing differences.
  These already have to exist for any cross-platform port; pre-reserve
  doesn't influence their shape.

### 3.2 Configuration surface

**Removed flags**:

- `--index-lock-timeout` — meaningless without the locking
  subsystem. Removing it is a CLI breaking change but nobody can
  rely on tuning a system that no longer exists.
- The corresponding `[performance] index_lock_timeout` config key.

**Added flags**:

- `--scan-reserve N` — explicit reserve size override. Accepts the
  same binary suffixes as `--min-size` / `--max-size` (K/M/G/T).
  Default: heuristic per §2.3.1.
- (Optional, future) `--scan-reserve-policy {prior|inodes|disk|min|max}`
  — exposes which signal to prefer. Probably unnecessary; defer
  until someone asks.

**Kept flags**: every other dcfh flag is unaffected.

### 3.3 Sizing heuristic implementation

```go
type ReserveOpts struct {
    Override     uint64 // --scan-reserve, 0 = use heuristic
    AbsoluteCap  uint64 // safety cap, default 256 GB
    SlopFactor   float64 // multiplier on each signal, default 8.0 for prior, 4.0 for inodes
}

func estimateScanReserve(metaDir, scanRoot string, opts ReserveOpts) (uint64, error) {
    if opts.Override > 0 {
        return clamp(opts.Override, opts.AbsoluteCap), nil
    }

    // Layer 1: prior main.idx
    if size, ok := priorMainIdxSize(metaDir); ok {
        return clamp(uint64(float64(size)*opts.SlopFactor), opts.AbsoluteCap), nil
    }

    // Layers 2 + 3: statvfs at scan root
    var st unix.Statfs_t
    if err := unix.Statfs(scanRoot, &st); err != nil {
        return 0, err
    }

    usedInodes := saturating_sub(st.Files, st.Ffree)
    if isInodeSignalReliable(usedInodes, st.Files) {
        // Layer 2
        return clamp(usedInodes*entrySizeEstimate*4, opts.AbsoluteCap), nil
    }

    // Layer 3: disk usage × 1%
    usedBytes := (st.Blocks - st.Bfree) * uint64(st.Bsize)
    return clamp(usedBytes/100, opts.AbsoluteCap), nil
}

const entrySizeEstimate uint64 = 140 // ~80 B fixed + ~50 B avg path, padded
```

`isInodeSignalReliable` is a bit of a guess: e.g.
`usedInodes > 0 && usedInodes < 1<<40 && st.Files != math.MaxUint64`.
Filesystems like btrfs/ZFS that report sentinel values fail this
check and fall through to layer 3. Empirically this gives ext4/XFS
the accurate inode-count path while everything else gets the
disk-usage fallback.

`clamp(n, cap)` also clamps against `RLIMIT_AS / 2` to avoid
surprising users on cgroup'd hosts. Implementation:

```go
func clamp(n, absCap uint64) uint64 {
    n = min(n, absCap)
    var rl unix.Rlimit
    if err := unix.Getrlimit(unix.RLIMIT_AS, &rl); err == nil {
        if rl.Cur != unix.RLIM_INFINITY {
            n = min(n, rl.Cur/2)
        }
    }
    return max(n, 1<<30) // floor at 1 GB so tiny dirs still work
}
```

### 3.4 Growth path implementation

```go
func reserveScanVA(size uintptr) (unsafe.Pointer, error) {
    addr, err := unix.MmapPtr(
        -1, 0,
        nil, size,
        unix.PROT_NONE,
        unix.MAP_ANONYMOUS|unix.MAP_PRIVATE,
    )
    return addr, err
}

func growScanMapping(base unsafe.Pointer, fd int, newSize uintptr) error {
    if err := unix.Ftruncate(fd, int64(newSize)); err != nil {
        return err
    }
    _, err := unix.MmapPtr(
        fd, 0,
        base, newSize,
        unix.PROT_READ|unix.PROT_WRITE,
        unix.MAP_FIXED|unix.MAP_SHARED,
    )
    return err
}

func releaseScanVA(base unsafe.Pointer, size uintptr) error {
    return unix.MunmapPtr(base, size)
}
```

`unix.MmapPtr` / `unix.MunmapPtr` exist on every supported GOOS (Linux,
*BSD, macOS) in `golang.org/x/sys/unix`. The flag set
(`MAP_ANONYMOUS|MAP_PRIVATE` for reserve, `MAP_FIXED|MAP_SHARED` for
install) is POSIX-mandated.

The append site changes minimally: where today it tests
`newSize > currentMappedSize` and calls into `mremap`, it now tests
the same condition and calls `growScanMapping`, returning
`ErrReserveExhausted` if `newSize > reserveSize`.

### 3.5 Locking deletions

Itemised, in order of where each lock is taken today:

1. `mmapIndexFile.mutex` — delete the field. Audit every
   `.mutex.Lock()` / `.RLock()` in the codebase; remove them all
   *unless* one is found to protect an invariant unrelated to
   relocation. (Best-guess: there are none. The lock was introduced
   purely for relocation safety.)
2. `binaryEntryRef.GetBinaryEntry()` — remove `RLock`/`RUnlock`
   wrapping the offset → pointer conversion.
3. `writeSkiplistWithVectorIOFiltered`'s sorted-RLock-acquisition
   block — delete entirely.
4. `appendEntryToNamedIndex`'s write-lock-on-grow block — replace
   with the unlocked `growScanMapping` call.
5. `DirectoryCache.indexLockTimeout` field, the `--index-lock-timeout`
   flag handler, the corresponding config-file accessor — delete.
6. `registerIndex`/`unregisterIndex` — keep (still used to track
   which indices need cleanup on shutdown) but document that they no
   longer participate in relocation safety.

CLAUDE.md "Memory Protection and Locking Mechanism" section is
deleted in the same commit; replaced with a one-paragraph note that
addresses are stable for the lifetime of each index.

### 3.6 Build-tag implications

| File / region | Today | Post-port |
|---|---|---|
| `pkg/index.go` | `// +build linux` (implicit via `unix.Mremap`) | No tag |
| `pkg/scan_growth.go` | n/a (new) | No tag |
| `pkg/util.go` | implicit Linux via `Stat_t` field access | No tag (stat helpers moved to `stat_*.go`) |
| `pkg/wire_handler.go`, `pkg/walker_wire.go` | implicit Linux | No tag (stat helpers moved) |
| `pkg/stat_linux.go` | n/a | `// +build linux` |
| `pkg/stat_bsd.go` | n/a | `// +build freebsd netbsd openbsd dragonfly` |
| `pkg/stat_darwin.go` | n/a | `// +build darwin` |

Net: most of `pkg/` stops being implicitly Linux-only. Only the small
stat-conversion helpers carry GOOS tags.

### 3.7 Testing

#### 3.7.1 Unit tests (no privileged operations)

- `TestEstimateScanReserve_PriorIndex` — table of `(prior_size,
  expected_reserve)` cases. Validates the steady-state path.
- `TestEstimateScanReserve_FreshFromInodes` — mocked `unix.Statfs_t`
  with ext4-shaped values; expects layer 2 to fire.
- `TestEstimateScanReserve_FreshFromDiskUsage` — mocked statfs with
  sentinel inode values; expects layer 3 to fire.
- `TestEstimateScanReserve_RLimitClamps` — mocked `RLIMIT_AS`;
  asserts the cap.
- `TestEstimateScanReserve_FlagOverride` — `--scan-reserve` wins
  unconditionally.

#### 3.7.2 Integration tests (Linux first, then BSDs)

- `TestScanGrowth_PreReservedMappingIsStable` — create a synthetic
  scan, force multiple growth events, verify `binaryEntry` pointers
  obtained early in the scan still resolve correctly post-growth.
  Already true under mremap by accident (with locking); under
  pre-reserve it's guaranteed by construction.
- `TestScanGrowth_ReserveExhaustionBailsCleanly` — set
  `--scan-reserve` artificially low, ensure the scan errors with
  `ErrReserveExhausted` without main.idx being touched, and that the
  partial scan-index file remains valid (recovery sweeps it on next
  run).
- `TestScanGrowth_PreReservedSurvivesContext` — context cancel
  mid-scan; verify cleanup does the right thing.

#### 3.7.3 CI matrix expansion

Today: Linux-only CI lane. After port: add macOS (GitHub Actions
`macos-latest` runner) and FreeBSD (e.g. `cross-platform-actions/
action@v0` qemu-driven). NetBSD/OpenBSD lanes optional.

### 3.8 Migration phasing

The work is not a single commit; sequence to keep `local-main` green.

- **Phase 1**: Implement `pkg/scan_growth.go` and `estimateScanReserve`
  with the new code path **gated behind a feature flag**
  (`DCFH_SCAN_RESERVE=1` env var). Default off. Existing mremap path
  untouched.
- **Phase 2**: Run the full test suite under `DCFH_SCAN_RESERVE=1` on
  Linux. Validate no regressions on representative scan workloads
  (the homedir-scale `170 GB → 380 MB main.idx` case is a useful
  benchmark). Tune the heuristic if needed.
- **Phase 3**: Flip the default to on, deprecate
  `--index-lock-timeout`. Mremap path remains as fallback for one
  release.
- **Phase 4**: Remove the mremap path and the locking subsystem.
  Single source path. CLAUDE.md updated.
- **Phase 5**: Add stat-helper GOOS splits, broaden the build matrix
  to FreeBSD/macOS. Each platform gets its own commit + CI lane to
  isolate failures.

Each phase is independently revertable. Phases 1-4 are Linux-only and
provide value (locking simplification, performance) even if the
BSD/macOS port never lands.

---

## Open questions

1. **Do we want a `--scan-reserve-policy` flag?** Exposing the layer
   selection (prior/inodes/disk) is API surface that's hard to remove
   later. Default is to skip it; revisit if support burden emerges.
2. **Should reserve survive a worker crash?** If a hash worker
   panics mid-scan and recovery wants to reuse the existing scan
   file, we'd want to reserve again at the same VA. Probably a
   non-issue (Go panics generally propagate up and abort the scan
   wholesale) but worth checking under `runtime.SetPanicOnFault(true)`.
3. **How do we communicate the locking-subsystem deletion?** It
   appears prominently in CLAUDE.md ("Memory Protection and Locking
   Mechanism") and in the `--index-lock-timeout` flag's user-visible
   surface. Migration notes in the relevant release.
4. **Is `RLIMIT_AS / 2` the right clamp?** A user with a 4 GB cgroup
   limit and a 1 GB heap would be capped at 2 GB reserve, which is
   probably fine but should be stress-tested.

## Critical files (today's references)

- `pkg/index.go:958` — current `unix.Mremap` call site.
- `pkg/index.go:345-346` — `Stat_t.Ctim`/`Mtim` access (Linux-shape).
- `pkg/binary_entry_index_file.go` (or wherever `GetBinaryEntry` lives) —
  per-entry RLock to delete.
- The temp-index writer (in `pkg/pipeline_update.go` /
  `pkg/temp_index_writer.go`) — any bulk RLock acquisition around
  vectorio writes to delete.
- `pkg/dircache.go` — `mainIndex`/`cacheIndex`/`scanIndices` tracking,
  `indexLockTimeout` field.
- `cmd/dcfh/options.go` — `--index-lock-timeout` flag definition;
  `--scan-reserve` flag to add.
- `CLAUDE.md` — "Memory Protection and Locking Mechanism" section to
  delete; "I/O Design and File Access Patterns" to update.
