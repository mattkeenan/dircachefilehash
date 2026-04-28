package dircachefilehash

import (
	"context"
	"fmt"
	"os"
	"strconv"
)

// localRepo implements Repo by wrapping an open DirectoryCache. For ssh://
// roots the DirectoryCache's walker/hasher pair is swapped to wire-backed
// implementations sharing a single wireSession (held here so Close can
// tear it down). The Repo surface is identical between local and wire
// cases — only the filesystem side of the pipeline moves onto the wire.
type localRepo struct {
	dc      *DirectoryCache
	session *wireSession // non-nil for ssh:// repos
}

// openRepoFromMetaDir opens a Repo for metaDir. A root URI with an ssh
// scheme swaps in the wire-backed walker/hasher; anything else uses the
// default local pair established by initDirectoryCacheBase.
func openRepoFromMetaDir(_ context.Context, metaDir string) (Repo, error) {
	rootDir, resolvedMeta, err := ResolveRepository(metaDir)
	if err != nil {
		var derr error
		rootDir, resolvedMeta, derr = DiscoverRepository(metaDir)
		if derr != nil {
			return nil, fmt.Errorf("failed to resolve repository at %s: %w", metaDir, err)
		}
	}

	if IsRemote(rootDir) {
		uri, perr := ParseRepoURI(rootDir)
		if perr != nil {
			return nil, fmt.Errorf("invalid remote root %q: %w", rootDir, perr)
		}
		if uri.Scheme != "ssh" {
			return nil, fmt.Errorf("unsupported remote scheme %q in [repository] root", uri.Scheme)
		}
		dc, derr := OpenDirectoryCache("", resolvedMeta)
		if derr != nil {
			return nil, fmt.Errorf("failed to open invoker-side .dcfh at %s: %w", resolvedMeta, derr)
		}
		return newWireRepo(dc, uri), nil
	}

	dc, err := OpenDirectoryCache(rootDir, resolvedMeta)
	if err != nil {
		return nil, err
	}
	return &localRepo{dc: dc}, nil
}

// createLocalRepo creates a new repository on disk. metaDir "" means
// ".dcfh" under rootDir (internal layout); otherwise metaDir is taken
// as-is (external layout, directory must end in ".dcfh" by convention).
func createLocalRepo(_ context.Context, rootDir, metaDir string) (*localRepo, error) {
	dc := CreateDirectoryCache(rootDir, metaDir)
	if dc == nil || dc.MetaDir == "" {
		return nil, fmt.Errorf("failed to create repository at %s", rootDir)
	}
	// If the caller supplied an explicit metaDir, this is an external
	// layout — persist the repository root in config so subsequent opens
	// can locate it.
	if metaDir != "" && dc.MetaDir != rootDir && dc.GetConfig() != nil {
		if err := dc.GetConfig().SetRepositoryRoot(rootDir); err != nil {
			_ = dc.Close()
			return nil, fmt.Errorf("failed to record repository root in config: %w", err)
		}
	}
	return &localRepo{dc: dc}, nil
}

// createWireRepo creates a fresh invoker-side .dcfh at metaDir whose
// [repository] root is persisted as the remote ssh URI. The ssh dial
// itself is deferred until the first Diff/Apply.
func createWireRepo(_ context.Context, metaDir string, uri RepoURI) (*localRepo, error) {
	if uri.Scheme != "ssh" {
		return nil, fmt.Errorf("wire repo requires ssh scheme, got %q", uri.Scheme)
	}
	remoteStr := uri.String()
	dc := CreateDirectoryCache(remoteStr, metaDir)
	if dc == nil || dc.MetaDir == "" {
		return nil, fmt.Errorf("failed to create wire repository at %s", metaDir)
	}
	if dc.GetConfig() == nil {
		_ = dc.Close()
		return nil, fmt.Errorf("no configuration created for %s", metaDir)
	}
	if err := dc.GetConfig().SetRepositoryRoot(remoteStr); err != nil {
		_ = dc.Close()
		return nil, fmt.Errorf("failed to persist repository root: %w", err)
	}
	return newWireRepo(dc, uri), nil
}

// newWireRepo wires dc through a fresh wireSession so Diff/Apply route
// their filesystem side over ssh. For Transport=="wire" the session
// dials lazily on first use; for Transport=="shell" a shellClient is
// constructed eagerly (each call spawns its own ssh) and handed in
// pre-built so Client() short-circuits the dial.
func newWireRepo(dc *DirectoryCache, uri RepoURI) *localRepo {
	if uri.Transport == TransportShell {
		return newWireRepoWithClient(dc, uri, newShellClient(uri))
	}
	return newWireRepoWithClient(dc, uri, nil)
}

// newWireRepoWithClient is the shared constructor underpinning newWireRepo,
// ssh+shell factory wiring, and in-process wire tests. When preBuilt is
// non-nil it short-circuits the first dial — the wire variant leaves it
// nil (lazy ssh dial on first Walk/HashOne); the shell variant passes a
// ready shellClient; tests pass a wire client wired to a pipe pair.
func newWireRepoWithClient(dc *DirectoryCache, uri RepoURI, preBuilt WireDriver) *localRepo {
	sess := &wireSession{uri: uri, client: preBuilt}
	dc.walker = &wireWalker{sess: sess, dc: dc}
	dc.fileHasher = &wireHasher{sess: sess, dc: dc}
	return &localRepo{dc: dc, session: sess}
}

func (l *localRepo) Close() error {
	var firstErr error
	if l.dc != nil {
		if err := l.dc.Close(); err != nil {
			firstErr = err
		}
		l.dc = nil
	}
	if l.session != nil {
		if err := l.session.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		l.session = nil
	}
	return firstErr
}

func (l *localRepo) Info(_ context.Context) (*RepoInfo, error) {
	info := &RepoInfo{
		RootDir:   l.dc.RootDir,
		MetaDir:   l.dc.MetaDir,
		IndexFile: l.dc.IndexFile,
	}
	info.EntryCount = l.dc.Length()
	if ts, ok := l.dc.IndexTimestamp(); ok {
		info.IndexTimestamp = ts
	}
	return info, nil
}

func (l *localRepo) Stats(_ context.Context) (*RepoStats, error) {
	count, size, err := l.dc.Stats()
	if err != nil {
		return nil, err
	}
	return &RepoStats{FileCount: count, TotalSize: size}, nil
}

func (l *localRepo) Diff(ctx context.Context, req DiffRequest) (*StatusResult, error) {
	flags := req.Options.toFlags()
	if err := l.dc.ApplyConfigOverrides(flags); err != nil {
		// Match existing behaviour in Status: fall back to applying
		// symlink mode directly if config isn't loaded.
		if symlinkMode, ok := flags["symlinks"]; ok {
			l.dc.symlinkMode = symlinkMode
		}
	}
	// Paths are intentionally unused in the current Status pipeline; it
	// always diffs the whole tree. Keep the field on the request for
	// future use (Phase 1b+).
	_ = req.Paths
	pred, err := BuildFilter(req.Filter)
	if err != nil {
		return nil, err
	}
	return l.dc.Status(ctx, flags, pred)
}

func (l *localRepo) DiffRefs(ctx context.Context, req DiffRefsRequest) (*StatusResult, error) {
	flags := req.Options.toFlags()
	if err := l.dc.ApplyConfigOverrides(flags); err != nil {
		if symlinkMode, ok := flags["symlinks"]; ok {
			l.dc.symlinkMode = symlinkMode
		}
	}
	left := req.Left
	if left == "" {
		left = "main"
	}
	right := req.Right
	if right == "" {
		right = "fs-scan"
	}
	leftRef, err := ParseIndexRef(l.dc.MetaDir, left)
	if err != nil {
		return nil, fmt.Errorf("parse left ref %q: %w", left, err)
	}
	rightRef, err := ParseIndexRef(l.dc.MetaDir, right)
	if err != nil {
		return nil, fmt.Errorf("parse right ref %q: %w", right, err)
	}
	pred, err := BuildFilter(req.Filter)
	if err != nil {
		return nil, err
	}
	return Diff(ctx, l.dc, leftRef, rightRef, pred)
}

func (l *localRepo) Apply(ctx context.Context, req ApplyRequest) (*UpdateResult, error) {
	flags := req.Options.toFlags()
	if err := l.dc.Update(ctx, flags, req.Paths...); err != nil {
		return nil, err
	}
	count, size, err := l.dc.Stats()
	if err != nil {
		return nil, err
	}
	return &UpdateResult{
		FileCount:    count,
		TotalSize:    size,
		PathsUpdated: req.Paths,
	}, nil
}

func (l *localRepo) Groups(ctx context.Context, req GroupsRequest) ([]DuplicateGroup, error) {
	return l.dc.FindDuplicates(ctx, req.Options.toFlags(), req.Filter)
}

func (l *localRepo) Filter(ctx context.Context, req FilterRequest) (*FilterResult, error) {
	if len(req.Actions) == 0 {
		return nil, fmt.Errorf("FilterRequest requires at least one action")
	}
	selectors := req.IndexSelectors
	if len(selectors) == 0 {
		selectors = []string{"all"}
	}
	refs, err := ResolveIndexSelectors(l.dc.MetaDir, selectors)
	if err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		return nil, fmt.Errorf("no accessible index files found")
	}
	if req.Repository == "" {
		req.Repository = l.dc.RootDir
	}
	return RunFilter(ctx, refs, req, os.Stderr)
}

func (l *localRepo) Snapshots() SnapshotRepo { return &localSnapshotRepo{dc: l.dc} }
func (l *localRepo) Config() ConfigRepo      { return &localConfigRepo{dc: l.dc} }

// localSnapshotRepo wraps pkg/snapshot.go's SnapshotRepository.

type localSnapshotRepo struct {
	dc *DirectoryCache
}

func (s *localSnapshotRepo) repo() *SnapshotRepository {
	return NewSnapshotRepository(s.dc.MetaDir)
}

func (s *localSnapshotRepo) Create(_ context.Context, tags []string) (*SnapshotMetadata, error) {
	return s.repo().CreateSnapshot(s.dc.RootDir, tags)
}

func (s *localSnapshotRepo) List(_ context.Context) ([]*SnapshotMetadata, error) {
	return s.repo().ListSnapshots()
}

func (s *localSnapshotRepo) Prune(_ context.Context, policy RetentionPolicy, dryRun bool) ([]string, error) {
	return s.repo().ForgetSnapshots(policy, dryRun)
}

func (s *localSnapshotRepo) Delete(_ context.Context, id string) error {
	return s.repo().RemoveSnapshot(id)
}

// localConfigRepo wraps the Config loaded into the DirectoryCache.

type localConfigRepo struct {
	dc *DirectoryCache
}

func (c *localConfigRepo) Get(_ context.Context) (*AllConfig, error) {
	cfg := c.dc.GetConfig()
	if cfg == nil {
		return nil, fmt.Errorf("no configuration loaded for repository at %s", c.dc.MetaDir)
	}
	return cfg.GetAllConfig(), nil
}

func (c *localConfigRepo) Set(_ context.Context, key, value string) error {
	cfg := c.dc.GetConfig()
	if cfg == nil {
		return fmt.Errorf("no configuration loaded for repository at %s", c.dc.MetaDir)
	}
	switch key {
	case "filehash.default":
		if err := ValidateHashAlgorithm(value); err != nil {
			return err
		}
		return cfg.SetHashDefault(value)
	case "output.format":
		if err := ValidateOutputFormat(value); err != nil {
			return err
		}
		return cfg.SetOutputFormat(value)
	case "verbose.level":
		level, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid verbose level %q: %w", value, err)
		}
		if err := ValidateVerboseLevel(level); err != nil {
			return err
		}
		return cfg.SetVerboseLevel(level)
	case "verbose.debug":
		if err := ValidateDebugFlags(value); err != nil {
			return err
		}
		return cfg.SetDebugFlags(value)
	case "symlink.mode":
		if err := ValidateSymlinkMode(value); err != nil {
			return err
		}
		return cfg.SetSymlinkMode(value)
	default:
		return fmt.Errorf("unknown configuration key: %s", key)
	}
}
