package dircachefilehash

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
)

// repoCore is the shared base embedded by the peer Repo impls
// (localRepo, wireRepo). All Repo verbs live here so each impl only
// adds what's unique to its transport (e.g. wireRepo's session).
type repoCore struct {
	ms *MetaStore

	walker      Walker
	fileHasher  Hasher
	symlinkMode string
	hashWorkers int

	// Set by configureFilters; read by scanRun().
	scanIgnore FilterExpr

	// "One scan at a time" guard around runUpdate; Apply takes it,
	// Diff/Groups don't (they don't write).
	scanMutex      sync.Mutex
	scanInProgress bool
	lastScanError  error
}

// localRepo implements Repo for filesystem-local roots. The local
// walker/hasher pair is wired in by newLocalRepo; everything else
// comes from the embedded repoCore.
type localRepo struct {
	repoCore
}

var _ Repo = (*localRepo)(nil)

// openRepoFromMetaDir opens a Repo for metaDir. A root URI with an ssh
// scheme returns a *wireRepo (peer impl in repo_wire.go); anything else
// returns a *localRepo.
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
		ms, derr := OpenMetaStore("", resolvedMeta)
		if derr != nil {
			return nil, fmt.Errorf("failed to open invoker-side .dcfh at %s: %w", resolvedMeta, derr)
		}
		return newWireRepo(ms, uri), nil
	}

	ms, err := OpenMetaStore(rootDir, resolvedMeta)
	if err != nil {
		return nil, err
	}
	return newLocalRepo(ms), nil
}

// createLocalRepo creates a new repository on disk. metaDir "" means
// ".dcfh" under rootDir (internal layout); otherwise metaDir is taken
// as-is (external layout, directory must end in ".dcfh" by convention).
func createLocalRepo(_ context.Context, rootDir, metaDir string) (*localRepo, error) {
	ms := CreateMetaStore(rootDir, metaDir)
	if ms == nil || ms.MetaDir == "" {
		return nil, fmt.Errorf("failed to create repository at %s", rootDir)
	}
	// If the caller supplied an explicit metaDir, this is an external
	// layout — persist the repository root in config so subsequent opens
	// can locate it.
	if metaDir != "" && ms.MetaDir != rootDir && ms.GetConfig() != nil {
		if err := ms.GetConfig().SetRepositoryRoot(rootDir); err != nil {
			_ = ms.Close()
			return nil, fmt.Errorf("failed to record repository root in config: %w", err)
		}
	}
	return newLocalRepo(ms), nil
}

func newLocalRepo(ms *MetaStore) *localRepo {
	l := &localRepo{
		repoCore: repoCore{
			ms:         ms,
			walker:     &localWalker{},
			fileHasher: &localHasher{ms: ms},
		},
	}
	l.seedFromConfig()
	return l
}

// seedFromConfig primes config-derived instrument fields so a
// freshly-built repo has usable defaults before any
// applyConfigOverrides call lands.
func (r *repoCore) seedFromConfig() {
	if cfg := r.ms.GetConfig(); cfg != nil {
		r.symlinkMode = cfg.GetSymlinkConfig().Mode
		r.hashWorkers = cfg.GetPerformanceConfig().HashWorkers
		return
	}
	r.symlinkMode = "none"
	r.hashWorkers = 2
}

func (r *repoCore) scanRun() *ScanRun {
	return &ScanRun{
		Store:       r.ms,
		Walker:      r.walker,
		FileHasher:  r.fileHasher,
		SymlinkMode: r.symlinkMode,
		HashWorkers: r.hashWorkers,
		ScanIgnore:  r.scanIgnore,
	}
}

func (r *repoCore) applyConfigOverrides(flags map[string]string) error {
	res, err := r.ms.ApplyConfigOverrides(flags)
	r.symlinkMode = res.SymlinkMode
	r.hashWorkers = res.HashWorkers
	return err
}

func (r *repoCore) Close() error {
	if r.ms != nil {
		err := r.ms.Close()
		r.ms = nil
		return err
	}
	return nil
}

func (r *repoCore) Info(_ context.Context) (*RepoInfo, error) {
	info := &RepoInfo{
		RootDir:   r.ms.RootDir,
		MetaDir:   r.ms.MetaDir,
		IndexFile: r.ms.IndexFile,
	}
	info.EntryCount = r.ms.Length()
	if ts, ok := r.ms.IndexTimestamp(); ok {
		info.IndexTimestamp = ts
	}
	return info, nil
}

func (r *repoCore) Stats(_ context.Context) (*RepoStats, error) {
	count, size, err := r.ms.Stats()
	if err != nil {
		return nil, err
	}
	return &RepoStats{FileCount: count, TotalSize: size}, nil
}

// configureFilters wires per-call filter state on the repo:
// Ignores becomes the scan-time push-down predicate (r.scanIgnore);
// noIgnoreFile suppresses .dcfh/ignore via IgnoreManager; Prints +
// Ignores compose into the output-time predicate. The returned cleanup
// reverts r.scanIgnore and ignoreManager state — call defer cleanup()
// before invoking the underlying primitive so a reused repo doesn't
// leak per-request state into a later call.
//
// legacyFilter is the deprecated single-segment alias on Diff/Apply
// requests: when prints is empty and legacyFilter is non-zero it is
// promoted into a single print segment so callers that haven't migrated
// keep their pre-scope-marker semantics.
func (r *repoCore) configureFilters(prints, ignores []FilterOptions, legacyFilter FilterOptions, noIgnoreFile bool) (FilterExpr, func(), error) {
	if len(prints) == 0 && !legacyFilter.IsEmpty() {
		prints = []FilterOptions{legacyFilter}
	}

	pred, err := BuildPrintIgnoreTree(prints, ignores)
	if err != nil {
		return nil, nil, err
	}
	scanExpr, err := BuildScanIgnore(ignores)
	if err != nil {
		return nil, nil, err
	}

	var restores []func()
	rollback := func() {
		for i := len(restores) - 1; i >= 0; i-- {
			restores[i]()
		}
	}

	if scanExpr != nil {
		r.scanIgnore = scanExpr
		restores = append(restores, func() { r.scanIgnore = nil })
	}
	if noIgnoreFile && r.ms.ignoreManager != nil {
		r.ms.ignoreManager.SetSuppressFile(true)
		if rerr := r.ms.ignoreManager.Reload(); rerr != nil {
			rollback()
			return nil, nil, fmt.Errorf("reload ignore patterns: %w", rerr)
		}
		restores = append(restores, func() {
			r.ms.ignoreManager.SetSuppressFile(false)
			_ = r.ms.ignoreManager.Reload()
		})
	}

	return pred, rollback, nil
}

func (r *repoCore) Diff(ctx context.Context, req DiffRequest) (*StatusResult, error) {
	flags := req.Options.toFlags()
	// Paths are intentionally unused in the current Status pipeline; it
	// always diffs the whole tree. Keep the field on the request for
	// future use (Phase 1b+).
	_ = req.Paths
	pred, cleanup, err := r.configureFilters(req.Prints, req.Ignores, req.Filter, req.NoIgnoreFile)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	return runStatus(ctx, r.ms, r.scanRun(), flags, pred)
}

func (r *repoCore) DiffRefs(ctx context.Context, req DiffRefsRequest) (*StatusResult, error) {
	flags := req.Options.toFlags()
	_ = r.applyConfigOverrides(flags)
	left := req.Left
	if left == "" {
		left = "main"
	}
	right := req.Right
	if right == "" {
		right = "fs-scan"
	}
	leftRef, err := ParseIndexRef(r.ms.MetaDir, left)
	if err != nil {
		return nil, fmt.Errorf("parse left ref %q: %w", left, err)
	}
	rightRef, err := ParseIndexRef(r.ms.MetaDir, right)
	if err != nil {
		return nil, fmt.Errorf("parse right ref %q: %w", right, err)
	}
	pred, cleanup, err := r.configureFilters(req.Prints, req.Ignores, req.Filter, req.NoIgnoreFile)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	return Diff(ctx, r.ms, r.scanRun(), leftRef, rightRef, pred)
}

func (r *repoCore) Apply(ctx context.Context, req ApplyRequest) (*UpdateResult, error) {
	flags := req.Options.toFlags()
	// Prints / req.Filter have no output predicate to attach to on
	// update — pass nil so configureFilters only wires scanIgnore +
	// suppressFile.
	_, cleanup, err := r.configureFilters(nil, req.Ignores, FilterOptions{}, req.NoIgnoreFile)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	// Serialise concurrent Apply calls through this repo handle. The
	// guard used to live inside performPipelineScan; moving it here
	// matches the metaphor (Repo verbs are the level at which "one
	// scan at a time" is enforced).
	r.scanMutex.Lock()
	defer r.scanMutex.Unlock()
	if r.scanInProgress {
		if r.lastScanError != nil {
			return nil, r.lastScanError
		}
		// Match prior behaviour: treat a re-entrant call as a silent no-op.
		count, size, _ := r.ms.Stats()
		return &UpdateResult{FileCount: count, TotalSize: size, PathsUpdated: req.Paths}, nil
	}
	r.scanInProgress = true
	defer func() { r.scanInProgress = false }()

	// Attach a change collector only when the caller wants the
	// interactive-tree labels. nil keeps the non-interactive path
	// byte-for-byte unchanged (the canonical sink behaves exactly as
	// before — no recording, no serialisation perturbation).
	var collector *changeCollector
	if req.CollectChanges {
		collector = &changeCollector{}
	}

	if err := runUpdateCollecting(ctx, r.ms, r.scanRun(), flags, collector, req.Paths...); err != nil {
		r.lastScanError = err
		return nil, err
	}
	r.lastScanError = nil
	count, size, err := r.ms.Stats()
	if err != nil {
		return nil, err
	}
	res := &UpdateResult{
		FileCount:    count,
		TotalSize:    size,
		PathsUpdated: req.Paths,
	}
	if collector != nil {
		res.Added = collector.added
		res.Modified = collector.modified
		res.Deleted = collector.deleted
	}
	return res, nil
}

// PostRunTree builds the read-only interactive-tree view from the
// post-run merged index (an mmap index read, not a filesystem walk) and
// labels live entries via cs. See the Repo interface for the wireRepo
// scoping caveat.
func (r *repoCore) PostRunTree(_ context.Context, cs ChangeSet) (*Tree, error) {
	merged, err := r.ms.LoadMergedMainCacheIndex()
	if err != nil {
		return nil, fmt.Errorf("failed to load index for tree view: %w", err)
	}
	return BuildTree(merged, cs), nil
}

func (r *repoCore) Groups(ctx context.Context, req GroupsRequest) ([]DuplicateGroup, error) {
	filter := req.Filter
	if filter.Predicate == nil && (len(filter.Prints) > 0 || len(filter.Ignores) > 0) {
		pred, err := BuildPrintIgnoreTree(filter.Prints, filter.Ignores)
		if err != nil {
			return nil, err
		}
		filter.Predicate = pred
	}
	return runFindDuplicates(ctx, r.ms, r.scanRun(), req.Options.toFlags(), filter)
}

func (r *repoCore) Filter(ctx context.Context, req FilterRequest) (*FilterResult, error) {
	if len(req.Actions) == 0 {
		return nil, fmt.Errorf("FilterRequest requires at least one action")
	}
	selectors := req.IndexSelectors
	if len(selectors) == 0 {
		selectors = []string{"all"}
	}
	refs, err := ResolveIndexSelectors(r.ms.MetaDir, selectors)
	if err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		return nil, fmt.Errorf("no accessible index files found")
	}
	if req.Repository == "" {
		req.Repository = r.ms.RootDir
	}
	return RunFilter(ctx, refs, req, os.Stderr)
}

func (r *repoCore) Snapshots() SnapshotRepo { return &localSnapshotRepo{ms: r.ms} }
func (r *repoCore) Config() ConfigRepo      { return &localConfigRepo{ms: r.ms} }

// localSnapshotRepo wraps pkg/snapshot.go's SnapshotRepository.

type localSnapshotRepo struct {
	ms *MetaStore
}

func (s *localSnapshotRepo) repo() *SnapshotRepository {
	return NewSnapshotRepository(s.ms.MetaDir)
}

func (s *localSnapshotRepo) Create(_ context.Context, tags []string) (*SnapshotMetadata, error) {
	return s.repo().CreateSnapshot(s.ms.RootDir, tags)
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

// localConfigRepo wraps the Config loaded into the MetaStore.

type localConfigRepo struct {
	ms *MetaStore
}

func (c *localConfigRepo) Get(_ context.Context) (*AllConfig, error) {
	cfg := c.ms.GetConfig()
	if cfg == nil {
		return nil, fmt.Errorf("no configuration loaded for repository at %s", c.ms.MetaDir)
	}
	return cfg.GetAllConfig(), nil
}

func (c *localConfigRepo) Set(_ context.Context, key, value string) error {
	cfg := c.ms.GetConfig()
	if cfg == nil {
		return fmt.Errorf("no configuration loaded for repository at %s", c.ms.MetaDir)
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
