package dircachefilehash

import (
	"context"
	"fmt"
)

// auditRepo implements Repo for audit mode: the invoker's .dcfh/ lives
// locally (main.idx, cache.idx, config, snapshots), while the root being
// audited lives on a remote host addressed by an ssh:// URI. All index
// decisions — Hwang–Lin, predicate evaluation, cache writes, snapshot
// history — run on the invoker. Only Diff and Apply cross the wire;
// everything else operates on local state and delegates to localRepo.
//
// The wire surface itself (ScanMetadata, HashFiles, ServerInfo) lands in
// a later commit of Phase 2. This scaffold establishes dispatch and the
// delegating shape so the rest can land incrementally.
type auditRepo struct {
	local  *localRepo
	remote RepoURI
}

// openAuditRepo opens the invoker-side .dcfh at metaDir. The remote is
// addressed by uri (ssh://[user@]host[:port]/path). Nothing crosses the
// wire in this scaffold; Diff/Apply return ErrRemoteNotImplemented.
func openAuditRepo(_ context.Context, metaDir string, uri RepoURI) (*auditRepo, error) {
	if uri.Scheme != "ssh" {
		return nil, fmt.Errorf("auditRepo requires ssh scheme, got %q", uri.Scheme)
	}
	dc, err := OpenDirectoryCache("", metaDir)
	if err != nil {
		return nil, fmt.Errorf("failed to open invoker-side .dcfh at %s: %w", metaDir, err)
	}
	return &auditRepo{local: &localRepo{dc: dc}, remote: uri}, nil
}

// createAuditRepo creates a fresh invoker-side .dcfh at metaDir with
// [repository] root set to the remote ssh URI, then returns an open
// auditRepo. No initial scan is performed; that happens on first Apply.
func createAuditRepo(_ context.Context, metaDir string, uri RepoURI) (*auditRepo, error) {
	if uri.Scheme != "ssh" {
		return nil, fmt.Errorf("auditRepo requires ssh scheme, got %q", uri.Scheme)
	}
	remoteStr := uri.String()
	dc := CreateDirectoryCache(remoteStr, metaDir)
	if dc == nil || dc.MetaDir == "" {
		return nil, fmt.Errorf("failed to create audit repository at %s", metaDir)
	}
	if dc.GetConfig() == nil {
		_ = dc.Close()
		return nil, fmt.Errorf("no configuration created for %s", metaDir)
	}
	if err := dc.GetConfig().SetRepositoryRoot(remoteStr); err != nil {
		_ = dc.Close()
		return nil, fmt.Errorf("failed to persist repository root: %w", err)
	}
	return &auditRepo{local: &localRepo{dc: dc}, remote: uri}, nil
}

func (a *auditRepo) Close() error {
	if a.local == nil {
		return nil
	}
	err := a.local.Close()
	a.local = nil
	return err
}

func (a *auditRepo) Info(ctx context.Context) (*RepoInfo, error) {
	return a.local.Info(ctx)
}

func (a *auditRepo) Stats(ctx context.Context) (*RepoStats, error) {
	return a.local.Stats(ctx)
}

func (a *auditRepo) Diff(_ context.Context, _ DiffRequest) (*StatusResult, error) {
	return nil, fmt.Errorf("%w: Diff against %s", ErrRemoteNotImplemented, a.remote)
}

func (a *auditRepo) Apply(_ context.Context, _ ApplyRequest) (*UpdateResult, error) {
	return nil, fmt.Errorf("%w: Apply against %s", ErrRemoteNotImplemented, a.remote)
}

func (a *auditRepo) Groups(ctx context.Context, req GroupsRequest) ([]DuplicateGroup, error) {
	return a.local.Groups(ctx, req)
}

func (a *auditRepo) Filter(ctx context.Context, req FilterRequest) (*FilterResult, error) {
	if req.Repository == "" {
		req.Repository = a.remote.String()
	}
	return a.local.Filter(ctx, req)
}

func (a *auditRepo) Snapshots() SnapshotRepo { return a.local.Snapshots() }
func (a *auditRepo) Config() ConfigRepo      { return a.local.Config() }
