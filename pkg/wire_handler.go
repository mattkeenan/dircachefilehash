package dircachefilehash

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"sync"
	"syscall"
)

// RemoteHandler implements WireHandler for the `dcfh remote` subcommand.
// It is the server-side endpoint of the audit-mode wire protocol: the
// invoker runs `dcfh remote <root>` over ssh and drives it through
// ServeWire. The handler holds no dcfh index state — only a scan root
// and, optionally, a hash cache whose lifetime and location the invoker
// controls via ScanRequest.Cache / HashRequest.Cache.
type RemoteHandler struct {
	root  string
	cache *remoteHashCache // nil when no CacheModeLocal request has been seen
	// cachePath is the on-disk location for the hash cache; empty means
	// the cache, if any, stays in-memory for the session only.
	cachePath string
	cacheInit sync.Once
	cacheErr  error
}

// NewRemoteHandler creates a handler rooted at root. root must be an
// absolute directory path on the remote; all ScanRequest / HashRequest
// paths are resolved relative to it. cachePath, if non-empty, is the
// JSON file used to persist the hash cache across sessions when the
// invoker requests CacheModeLocal; pass "" to keep any cache in-memory.
func NewRemoteHandler(root, cachePath string) (*RemoteHandler, error) {
	if root == "" {
		return nil, errors.New("remote handler requires a root directory")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve root: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("stat root %s: %w", abs, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("root %s is not a directory", abs)
	}
	return &RemoteHandler{root: abs, cachePath: cachePath}, nil
}

// Close flushes the hash cache to disk if one was activated and a
// cachePath is configured. Safe to call whether or not caching was
// ever requested.
func (h *RemoteHandler) Close() error {
	if h.cache == nil || h.cachePath == "" {
		return nil
	}
	return h.cache.Save(h.cachePath)
}

// ServerInfo reports this remote's capabilities. Wire version is fixed;
// hash algos mirror what HashFiles can compute; Concurrency advertises
// the default parallelism the remote will use for HashFiles.
func (h *RemoteHandler) ServerInfo(_ context.Context) (*ServerCaps, error) {
	return &ServerCaps{
		WireVersion: WireVersion,
		DcfhVersion: RemoteDcfhVersion,
		HashAlgos:   []string{"sha1", "sha256", "sha512"},
		Concurrency: runtime.GOMAXPROCS(0),
	}, nil
}

// RemoteDcfhVersion is the version string reported by ServerInfo. It is
// a var (not a const) so the main package can overwrite it with the
// generated build version at init.
var RemoteDcfhVersion = "dev"

// ScanMetadata walks the configured root, applying the request's path
// filters and ignore patterns, and returns FileMeta entries sorted by
// relative path. Symlinks are recorded without being followed (audit
// stance: never traverse into untrusted targets); the Symlinks field
// is reserved for future mode-matching with the invoker's --symlinks.
func (h *RemoteHandler) ScanMetadata(ctx context.Context, req ScanRequest) (*ScanResponse, error) {
	ignorers, err := compileIgnorePatterns(req.Ignores)
	if err != nil {
		return nil, fmt.Errorf("compile ignores: %w", err)
	}
	roots, err := h.resolveScanRoots(req.Paths)
	if err != nil {
		return nil, err
	}

	var out []FileMeta
	for _, rootAbs := range roots {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := h.walkOne(ctx, rootAbs, ignorers, &out); err != nil {
			return nil, err
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return &ScanResponse{Files: out}, nil
}

// HashFiles hashes each path in req.Paths with req.Algo. Paths that fail
// (missing, permission denied) return their Err; the rest return their
// hex digest. Order matches req.Paths. When req.Cache == CacheModeLocal
// the handler consults and populates its hash cache keyed on stat+algo.
func (h *RemoteHandler) HashFiles(ctx context.Context, req HashRequest) (*HashResponse, error) {
	algo, err := GetHashAlgorithm(req.Algo)
	if err != nil {
		return nil, err
	}

	cache := h.cacheIfRequested(req.Cache)
	digests := make([]PathDigest, len(req.Paths))

	workers := max(runtime.GOMAXPROCS(0), 1)
	workers = min(workers, len(req.Paths))

	jobs := make(chan int, len(req.Paths))
	for i := range req.Paths {
		jobs <- i
	}
	close(jobs)

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 2*1024*1024)
			for i := range jobs {
				if err := ctx.Err(); err != nil {
					digests[i] = PathDigest{Path: req.Paths[i], Err: err.Error()}
					continue
				}
				digests[i] = h.hashOne(ctx, req.Paths[i], algo, buf, cache)
			}
		}()
	}
	wg.Wait()
	return &HashResponse{Digests: digests}, nil
}

// hashOne computes (or retrieves from cache) the hash of a single path.
// rel is relative to h.root. Populates cache on miss when cache != nil.
func (h *RemoteHandler) hashOne(ctx context.Context, rel string, algo *HashAlgorithm, buf []byte, cache *remoteHashCache) PathDigest {
	abs, err := h.resolveRel(rel)
	if err != nil {
		return PathDigest{Path: rel, Err: err.Error()}
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return PathDigest{Path: rel, Err: err.Error()}
	}
	if !info.Mode().IsRegular() {
		return PathDigest{Path: rel, Err: fmt.Sprintf("not a regular file: %s", rel)}
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return PathDigest{Path: rel, Err: "stat unavailable"}
	}
	key := hashCacheKey{
		Path:    abs,
		Size:    info.Size(),
		MtimeNs: st.Mtim.Nano(),
		CtimeNs: st.Ctim.Nano(),
		Algo:    algo.Name,
	}
	if cache != nil {
		if got, ok := cache.Get(key); ok {
			return PathDigest{Path: rel, Hash: got}
		}
	}
	sum, err := HashFileInterruptible(ctx, abs, algo, buf)
	if err != nil {
		return PathDigest{Path: rel, Err: err.Error()}
	}
	hashHex := hex.EncodeToString(sum)
	if cache != nil {
		cache.Put(key, hashHex)
	}
	return PathDigest{Path: rel, Hash: hashHex}
}

// cacheIfRequested returns the handler's hash cache, lazily loading it
// on first request, or nil when mode != CacheModeLocal. Lazy load means
// a purely-none session never touches the cache file.
func (h *RemoteHandler) cacheIfRequested(mode CacheMode) *remoteHashCache {
	if mode != CacheModeLocal {
		return nil
	}
	h.cacheInit.Do(func() {
		h.cache, h.cacheErr = loadHashCache(h.cachePath)
	})
	if h.cacheErr != nil {
		return nil
	}
	return h.cache
}

// resolveScanRoots produces absolute paths for the ScanRequest.Paths list.
// Empty input means "walk the whole root". Paths are cleaned and required
// to stay inside h.root (no ".." escapes); violations are errors so a
// malicious invoker can't pivot the remote outside its audit root.
func (h *RemoteHandler) resolveScanRoots(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return []string{h.root}, nil
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		abs, err := h.resolveRel(p)
		if err != nil {
			return nil, err
		}
		out = append(out, abs)
	}
	return out, nil
}

// resolveRel resolves rel against h.root, forbidding escapes.
func (h *RemoteHandler) resolveRel(rel string) (string, error) {
	abs := filepath.Join(h.root, rel)
	abs = filepath.Clean(abs)
	if abs != h.root && !hasPathPrefix(abs, h.root) {
		return "", fmt.Errorf("path %q escapes root", rel)
	}
	return abs, nil
}

// hasPathPrefix reports whether p lies at or below prefix. Avoids the
// classic "/foo" prefix-match-of "/foobar" bug.
func hasPathPrefix(p, prefix string) bool {
	if len(p) < len(prefix) {
		return false
	}
	if p[:len(prefix)] != prefix {
		return false
	}
	return len(p) == len(prefix) || p[len(prefix)] == filepath.Separator
}

// walkOne performs a depth-first walk of abs, emitting a FileMeta for
// each surviving entry. Sorts directory children so emitted entries are
// already lexicographically ordered within each directory (final global
// sort in ScanMetadata handles cross-root ordering).
func (h *RemoteHandler) walkOne(ctx context.Context, abs string, ignorers []*regexp.Regexp, out *[]FileMeta) error {
	info, err := os.Lstat(abs)
	if err != nil {
		return fmt.Errorf("lstat %s: %w", abs, err)
	}
	rel, err := filepath.Rel(h.root, abs)
	if err != nil {
		return err
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		// Root itself — don't emit; descend.
	} else {
		if pathIgnored(rel, ignorers) {
			return nil
		}
		meta, ok := makeFileMeta(rel, abs, info)
		if ok {
			*out = append(*out, meta)
		}
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil
	}

	entries, readErr := os.ReadDir(abs)
	if readErr != nil {
		// Tolerate unreadable subdirs mid-scan: the dir itself is
		// already recorded in FileMeta; skipping contents is the
		// closest dcfh gets to a "best-effort" scan.
		return nil //nolint:nilerr
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := h.walkOne(ctx, filepath.Join(abs, e.Name()), ignorers, out); err != nil {
			return err
		}
	}
	return nil
}

// makeFileMeta builds a FileMeta for one filesystem entry. Returns
// (meta, false) for entries dcfh doesn't index (device files, sockets).
func makeFileMeta(rel, abs string, info os.FileInfo) (FileMeta, bool) {
	m := FileMeta{
		Path:    rel,
		Size:    info.Size(),
		Mode:    uint32(info.Mode().Perm()) | uint32(info.Mode()&os.ModeType),
		MtimeNs: info.ModTime().UnixNano(),
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		m.UID = st.Uid
		m.GID = st.Gid
		m.Dev = st.Dev
		m.CtimeNs = st.Ctim.Nano()
	}
	switch {
	case info.Mode().IsRegular():
		m.Kind = FileKindRegular
	case info.IsDir():
		m.Kind = FileKindDir
	case info.Mode()&os.ModeSymlink != 0:
		m.Kind = FileKindSymlink
		if tgt, err := os.Readlink(abs); err == nil {
			m.LinkTarget = tgt
		}
	default:
		return FileMeta{}, false
	}
	return m, true
}

// compileIgnorePatterns turns the request's ignore strings into regexps
// matched against forward-slash relative paths (matching IgnoreManager
// semantics). Invalid patterns error the whole request so a bad rule is
// loud, not silent.
func compileIgnorePatterns(patterns []string) ([]*regexp.Regexp, error) {
	if len(patterns) == 0 {
		return nil, nil
	}
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("bad ignore pattern %q: %w", p, err)
		}
		out = append(out, re)
	}
	return out, nil
}

func pathIgnored(rel string, ignorers []*regexp.Regexp) bool {
	for _, re := range ignorers {
		if re.MatchString(rel) {
			return true
		}
	}
	return false
}

// hashCacheKey identifies a cached hash. Includes Algo so a later
// rehash under a different algorithm doesn't collide with a prior one.
type hashCacheKey struct {
	Path    string `json:"p"`
	Size    int64  `json:"s"`
	MtimeNs int64  `json:"m"`
	CtimeNs int64  `json:"c"`
	Algo    string `json:"a"`
}

func (k hashCacheKey) String() string {
	b, _ := json.Marshal(k)
	return string(b)
}

// remoteHashCache is a small map-backed hash cache, persisted as one
// JSON file. Thread-safe (HashFiles dispatches workers concurrently).
// Writes are batched and flushed by RemoteHandler.Close; we don't fsync
// mid-session because the cache is strictly a speedup — losing it just
// forces re-hashing, never data loss.
type remoteHashCache struct {
	mu      sync.Mutex
	entries map[string]string
	dirty   bool
}

func loadHashCache(path string) (*remoteHashCache, error) {
	c := &remoteHashCache{entries: make(map[string]string)}
	if path == "" {
		return c, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read hash cache %s: %w", path, err)
	}
	if len(data) == 0 {
		return c, nil
	}
	if err := json.Unmarshal(data, &c.entries); err != nil {
		return nil, fmt.Errorf("decode hash cache %s: %w", path, err)
	}
	return c, nil
}

func (c *remoteHashCache) Get(k hashCacheKey) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.entries[k.String()]
	return v, ok
}

func (c *remoteHashCache) Put(k hashCacheKey, hash string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[k.String()] = hash
	c.dirty = true
}

// Save writes the cache to path atomically (write temp, rename). No-op
// if nothing was inserted or updated.
func (c *remoteHashCache) Save(path string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.dirty {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(c.entries)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
