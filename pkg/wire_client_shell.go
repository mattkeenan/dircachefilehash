package dircachefilehash

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

// shellClient is the ssh+shell WireDriver: every RPC spawns a fresh ssh
// subprocess running a POSIX-shell pipeline on the remote. ScanMetadata
// invokes `find -printf` + `sort`; HashFiles invokes `<algo>sum`. No
// dcfh binary is required on the far side, so this mode works against
// locked-down hosts at the cost of one ssh-spawn per call.
//
// Connection reuse is expected to come from the invoker's ssh config
// (ControlMaster=auto, ControlPath=...) — see docs/ssh-shell-mode.md.
// We don't bake it into argv because every invoker already tunes it.
type shellClient struct {
	uri    RepoURI
	runner shellRunner
}

// shellRunner executes a POSIX-sh pipeline on the remote and returns
// its stdout. Production uses sshShellRunner; tests inject a local
// runner that execs `sh -c` so the parser and command-builder logic
// can be exercised without ssh.
type shellRunner func(ctx context.Context, pipeline string) ([]byte, error)

func newShellClient(uri RepoURI) *shellClient {
	return &shellClient{uri: uri, runner: sshShellRunner(uri)}
}

// sshShellRunner builds a shellRunner that pipes the pipeline to ssh as
// a single remote-command argv — sshd re-wraps it in the remote login
// shell's -c, so the pipeline executes verbatim. Stderr is forwarded to
// the invoker's stderr so host-key prompts and find/sha256sum warnings
// stay visible; only stdout is returned for parsing.
func sshShellRunner(uri RepoURI) shellRunner {
	return func(ctx context.Context, pipeline string) ([]byte, error) {
		argv, err := sshCommand(uri, []string{pipeline})
		if err != nil {
			return nil, err
		}
		cmd := exec.CommandContext(ctx, "ssh", argv...)
		cmd.Stderr = os.Stderr
		out, err := cmd.Output()
		if err != nil {
			return out, fmt.Errorf("ssh shell pipeline: %w", err)
		}
		return out, nil
	}
}

// ServerInfo returns hardcoded caps for the shell variant. There is no
// remote handshake — the "capabilities" are what the protocol can
// inherently do from POSIX + GNU find + coreutils. Concurrency 1
// because each call is one serial ssh invocation; the invoker's hash
// pool gets its parallelism from running multiple shellClient calls
// concurrently, not from the remote side.
func (c *shellClient) ServerInfo(_ context.Context) (*ServerCaps, error) {
	return &ServerCaps{
		WireVersion: WireVersion,
		DcfhVersion: RemoteDcfhVersion,
		HashAlgos:   []string{"sha1", "sha256", "sha512"},
		Concurrency: 1,
	}, nil
}

// Close is a no-op: each RPC owns its own ssh subprocess, there is no
// session-wide resource to release.
func (c *shellClient) Close() error { return nil }

// ScanMetadata runs `cd <root> && find <paths> -printf FMT 2>/dev/null |
// LC_ALL=C sort` on the remote and parses the output. Ignore patterns
// are applied client-side after parsing — shell can't meaningfully
// compile regex ignores remotely, but the invoker already has the
// IgnoreManager, so we just filter here.
func (c *shellClient) ScanMetadata(ctx context.Context, req ScanRequest) (*ScanResponse, error) {
	pipeline := buildFindPipeline(c.uri.Path, req.Paths)
	raw, err := c.runner(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	files, err := parseFindOutput(raw)
	if err != nil {
		return nil, fmt.Errorf("parse find output: %w", err)
	}
	ignorers, err := compileIgnorePatterns(req.Ignores)
	if err != nil {
		return nil, fmt.Errorf("compile ignores: %w", err)
	}
	if ignorers != nil {
		files = filterIgnored(files, ignorers)
	}
	return &ScanResponse{Files: files}, nil
}

// HashFiles runs `cd <root> && <algo>sum -- <paths>` on the remote and
// parses the output. Paths that fail (missing / unreadable) produce no
// output line; we surface them as PathDigest{Err} to preserve the 1:1
// order the caller expects. CacheMode is ignored — the shell variant
// is inherently stateless on the remote side.
func (c *shellClient) HashFiles(ctx context.Context, req HashRequest) (*HashResponse, error) {
	if len(req.Paths) == 0 {
		return &HashResponse{}, nil
	}
	tool, err := hashToolForAlgo(req.Algo)
	if err != nil {
		return nil, err
	}
	pipeline := buildHashPipeline(c.uri.Path, tool, req.Paths)
	raw, err := c.runner(ctx, pipeline)
	if err != nil {
		// sha256sum exits non-zero when any file errored; we still
		// parse whatever stdout we got and mark missing paths below.
		if len(raw) == 0 {
			return nil, err
		}
	}
	digests := parseHashOutput(raw, req.Paths)
	return &HashResponse{Digests: digests}, nil
}

// buildFindPipeline assembles the remote shell command for ScanMetadata.
// The `2>/dev/null` suppresses per-file permission errors (find keeps
// walking) so a partial scan succeeds — this matches the local walker's
// tolerant behaviour and RemoteHandler.walkOne's os.ReadDir swallow.
//
// %p is the full path find sees; for a no-paths request we start at
// "." and strip the "./" prefix in the parser. When explicit paths are
// given, %p is already relative to the root (we're already cd'd there).
func buildFindPipeline(root string, paths []string) string {
	starts := paths
	if len(starts) == 0 {
		starts = []string{"."}
	}
	quoted := make([]string, len(starts))
	for i, p := range starts {
		quoted[i] = shellQuote(p)
	}
	return fmt.Sprintf(
		"cd %s && find %s -printf '%s' 2>/dev/null | LC_ALL=C sort",
		shellQuote(root),
		strings.Join(quoted, " "),
		findPrintfFormat,
	)
}

// findPrintfFormat is the GNU find -printf template. Ten tab-separated
// fields, newline-terminated:
//
//	%p  path (relative to cd root)
//	%y  type letter (f|d|l|...)
//	%s  size in bytes
//	%m  octal permission bits
//	%U  numeric uid
//	%G  numeric gid
//	%T@ mtime seconds.nanoseconds
//	%C@ ctime seconds.nanoseconds
//	%D  device number
//	%l  link target (empty for non-symlinks)
//
// Paths containing embedded tab or newline are not representable — they
// are a known shell-mode limitation, documented in docs/ssh-shell-mode.md.
const findPrintfFormat = "%p\t%y\t%s\t%m\t%U\t%G\t%T@\t%C@\t%D\t%l\n"

// buildHashPipeline assembles the remote shell command for HashFiles.
// sha*sum is called with the relative paths after a chdir to root so
// its output lines carry the same relative paths the invoker passed,
// avoiding a server-side strip-prefix step.
func buildHashPipeline(root, tool string, paths []string) string {
	quoted := make([]string, len(paths))
	for i, p := range paths {
		quoted[i] = shellQuote(p)
	}
	return fmt.Sprintf("cd %s && %s -- %s",
		shellQuote(root), tool, strings.Join(quoted, " "))
}

// hashToolForAlgo maps a dcfh algorithm name to the matching coreutils
// binary. Only sha1/sha256/sha512 are supported — matching ServerCaps.
func hashToolForAlgo(algo string) (string, error) {
	switch strings.ToLower(algo) {
	case "sha1":
		return "sha1sum", nil
	case "sha256":
		return "sha256sum", nil
	case "sha512":
		return "sha512sum", nil
	default:
		return "", fmt.Errorf("shell mode: unsupported hash algo %q", algo)
	}
}

// shellQuote returns s wrapped in single quotes with embedded single
// quotes replaced by the POSIX-sh end-quote / escape / restart-quote
// trick so /bin/sh splits the result as one argv.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// parseFindOutput decodes the 10-field tab-separated output of
// findPrintfFormat into a sorted FileMeta slice. Unknown file types
// (block/char/socket/pipe) are silently skipped — matches the local
// walker's coverage. Re-sorts to guarantee lexicographic order even
// if the remote `sort` behaved unexpectedly.
func parseFindOutput(raw []byte) ([]FileMeta, error) {
	out := make([]FileMeta, 0, 256)
	sc := bufio.NewScanner(bytes.NewReader(raw))
	// Large max token so long-path or symlink-target lines don't trip
	// bufio's default 64 KiB ceiling.
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		meta, ok, err := parseFindLine(sc.Text())
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		out = append(out, meta)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// parseFindLine decodes a single find-printf record. Returns ok=false
// for entries that should be dropped silently (the starting "." record
// and unsupported file types); a non-nil error indicates the record
// shape is unexpected and the whole response should be rejected.
func parseFindLine(line string) (FileMeta, bool, error) {
	fields := strings.Split(line, "\t")
	if len(fields) != 10 {
		return FileMeta{}, false, fmt.Errorf("find line: want 10 fields, got %d: %q", len(fields), line)
	}
	path := fields[0]
	// Starting point "." has no leading-slash equivalent; trim the
	// "./" prefix find adds when we gave it ".". Explicit-path scans
	// don't hit this branch because the user's rel paths don't start
	// with "./".
	switch {
	case path == ".":
		return FileMeta{}, false, nil
	case strings.HasPrefix(path, "./"):
		path = path[2:]
	}

	kind, ok := findKindFromLetter(fields[1])
	if !ok {
		return FileMeta{}, false, nil
	}

	size, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		return FileMeta{}, false, fmt.Errorf("find size %q: %w", fields[2], err)
	}
	perm, err := strconv.ParseUint(fields[3], 8, 32)
	if err != nil {
		return FileMeta{}, false, fmt.Errorf("find perm %q: %w", fields[3], err)
	}
	uid, err := strconv.ParseUint(fields[4], 10, 32)
	if err != nil {
		return FileMeta{}, false, fmt.Errorf("find uid %q: %w", fields[4], err)
	}
	gid, err := strconv.ParseUint(fields[5], 10, 32)
	if err != nil {
		return FileMeta{}, false, fmt.Errorf("find gid %q: %w", fields[5], err)
	}
	mtimeNs, err := parseFindEpochNs(fields[6])
	if err != nil {
		return FileMeta{}, false, fmt.Errorf("find mtime %q: %w", fields[6], err)
	}
	ctimeNs, err := parseFindEpochNs(fields[7])
	if err != nil {
		return FileMeta{}, false, fmt.Errorf("find ctime %q: %w", fields[7], err)
	}
	dev, err := strconv.ParseUint(fields[8], 10, 64)
	if err != nil {
		return FileMeta{}, false, fmt.Errorf("find dev %q: %w", fields[8], err)
	}

	meta := FileMeta{
		Path:    path,
		Kind:    kind,
		Size:    size,
		Mode:    uint32(perm) | typeModeBits(kind),
		UID:     uint32(uid),
		GID:     uint32(gid),
		MtimeNs: mtimeNs,
		CtimeNs: ctimeNs,
		Dev:     dev,
	}
	if kind == FileKindSymlink {
		meta.LinkTarget = fields[9]
	}
	return meta, true, nil
}

// findKindFromLetter maps %y output to FileKind. Block/char/socket/pipe
// collapse to ok=false so they're dropped — the dcfh index doesn't
// model them, matching makeFileMeta's default-case skip.
func findKindFromLetter(y string) (FileKind, bool) {
	switch y {
	case "f":
		return FileKindRegular, true
	case "d":
		return FileKindDir, true
	case "l":
		return FileKindSymlink, true
	default:
		return "", false
	}
}

// typeModeBits returns the os.FileMode type bits matching a FileKind so
// the synthesised Mode mirrors makeFileMeta's `Perm | ModeType` layout.
func typeModeBits(k FileKind) uint32 {
	switch k {
	case FileKindDir:
		return uint32(os.ModeDir)
	case FileKindSymlink:
		return uint32(os.ModeSymlink)
	default:
		return 0
	}
}

// parseFindEpochNs decodes a `%T@` or `%C@` value — "seconds.nanoseconds"
// with variable fractional precision — into int64 Unix nanoseconds. GNU
// find emits up to 9 fractional digits on ext4/xfs; older filesystems
// may give fewer. Pads or truncates to nine so the nanosecond field is
// always correctly scaled.
func parseFindEpochNs(s string) (int64, error) {
	secPart, frac, hasFrac := strings.Cut(s, ".")
	sec, err := strconv.ParseInt(secPart, 10, 64)
	if err != nil {
		return 0, err
	}
	if !hasFrac {
		return sec * 1_000_000_000, nil
	}
	switch {
	case len(frac) > 9:
		frac = frac[:9]
	case len(frac) < 9:
		frac += strings.Repeat("0", 9-len(frac))
	}
	ns, err := strconv.ParseInt(frac, 10, 64)
	if err != nil {
		return 0, err
	}
	if sec < 0 {
		return sec*1_000_000_000 - ns, nil
	}
	return sec*1_000_000_000 + ns, nil
}

// filterIgnored drops FileMeta entries whose Path matches any ignorer,
// mirroring RemoteHandler.ScanMetadata's server-side behaviour for the
// wire variant. Order is preserved.
func filterIgnored(in []FileMeta, ignorers gitignore.Matcher) []FileMeta {
	out := in[:0]
	for _, m := range in {
		if pathIgnored(m.Path, ignorers) {
			continue
		}
		out = append(out, m)
	}
	return out
}

// parseHashOutput decodes `sha*sum` output into PathDigest entries in
// request order. Output lines are "<hex>  <path>" (two spaces, possibly
// with a leading "*" binary-mode marker on the path). Request paths with
// no matching output line get Err — they failed remotely. Order matches
// req.Paths.
func parseHashOutput(raw []byte, requested []string) []PathDigest {
	lookup := make(map[string]string, len(requested))
	for line := range bytes.SplitSeq(raw, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		hashBytes, pathBytes, ok := bytes.Cut(line, []byte("  "))
		if !ok {
			continue
		}
		hashHex := string(hashBytes)
		path := strings.TrimPrefix(string(pathBytes), "*")
		// Validate hex to reject malformed lines (coreutils can't emit
		// these, but tampered output shouldn't silently poison the map).
		if _, err := hex.DecodeString(hashHex); err != nil {
			continue
		}
		lookup[path] = hashHex
	}
	out := make([]PathDigest, len(requested))
	for i, p := range requested {
		if h, ok := lookup[p]; ok {
			out[i] = PathDigest{Path: p, Hash: h}
		} else {
			out[i] = PathDigest{Path: p, Err: "shell hash: no output line (missing or unreadable)"}
		}
	}
	return out
}
