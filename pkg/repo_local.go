package dircachefilehash

import (
	"context"
	"fmt"
	"strconv"
)

// localRepo implements Repo by wrapping an open DirectoryCache. It is the
// default Repo in Phase 1 and the "data side" Repo in Phase 3 (colocated
// mode runs this same implementation inside the remote server process).
type localRepo struct {
	dc *DirectoryCache
}

// openLocalRepo opens an existing on-disk repository. metaDir may be any
// of: a .dcfh subdirectory, an external *.dcfh directory, or a path under
// a repository (handed to DiscoverRepository).
func openLocalRepo(_ context.Context, metaDir string) (*localRepo, error) {
	rootDir, resolvedMeta, err := ResolveRepository(metaDir)
	if err != nil {
		// metaDir wasn't itself a *.dcfh directory — fall back to discovery
		// from that starting point.
		var derr error
		rootDir, resolvedMeta, derr = DiscoverRepository(metaDir)
		if derr != nil {
			return nil, fmt.Errorf("failed to resolve repository at %s: %w", metaDir, err)
		}
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

func (l *localRepo) Close() error {
	if l.dc == nil {
		return nil
	}
	err := l.dc.Close()
	l.dc = nil
	return err
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

func (l *localRepo) Survey(ctx context.Context, req SurveyRequest) (*StatusResult, error) {
	flags := req.Options.toFlags()
	if err := l.dc.ApplyConfigOverrides(flags); err != nil {
		// Match existing behaviour in Status: fall back to applying
		// symlink mode directly if config isn't loaded.
		if symlinkMode, ok := flags["symlinks"]; ok {
			l.dc.symlinkMode = symlinkMode
		}
	}
	// Paths are intentionally unused in the current Status pipeline; it
	// always surveys the whole tree. Keep the field on the request for
	// future use (Phase 1b+).
	_ = req.Paths
	return l.dc.Status(ctx, flags)
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
	flags := req.Options.toFlags()
	return l.dc.FindDuplicatesUnified(ctx, flags)
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
