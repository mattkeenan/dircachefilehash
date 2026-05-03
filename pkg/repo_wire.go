package dircachefilehash

import (
	"context"
	"errors"
	"fmt"
)

// wireRepo implements Repo for ssh:// roots. The shared verbs (Diff,
// Apply, …) come from the embedded repoCore; wireRepo only adds the
// session that owns the ssh dial.
type wireRepo struct {
	repoCore
	session *wireSession
}

var _ Repo = (*wireRepo)(nil)

// createWireRepo creates a fresh invoker-side .dcfh at metaDir whose
// [repository] root is persisted as the remote ssh URI. The ssh dial
// itself is deferred until the first Diff/Apply.
func createWireRepo(_ context.Context, metaDir string, uri RepoURI) (*wireRepo, error) {
	if uri.Scheme != "ssh" {
		return nil, fmt.Errorf("wire repo requires ssh scheme, got %q", uri.Scheme)
	}
	remoteStr := uri.String()
	ms := CreateMetaStore(remoteStr, metaDir)
	if ms == nil || ms.MetaDir == "" {
		return nil, fmt.Errorf("failed to create wire repository at %s", metaDir)
	}
	if ms.GetConfig() == nil {
		_ = ms.Close()
		return nil, fmt.Errorf("no configuration created for %s", metaDir)
	}
	if err := ms.GetConfig().SetRepositoryRoot(remoteStr); err != nil {
		_ = ms.Close()
		return nil, fmt.Errorf("failed to persist repository root: %w", err)
	}
	return newWireRepo(ms, uri), nil
}

// newWireRepo wires ms through a fresh wireSession so Diff/Apply route
// their filesystem side over ssh. For Transport=="wire" the session
// dials lazily on first use; for Transport=="shell" a shellClient is
// constructed eagerly (each call spawns its own ssh) and handed in
// pre-built so Client() short-circuits the dial.
func newWireRepo(ms *MetaStore, uri RepoURI) *wireRepo {
	if uri.Transport == TransportShell {
		return newWireRepoWithClient(ms, uri, newShellClient(uri))
	}
	return newWireRepoWithClient(ms, uri, nil)
}

// newWireRepoWithClient is the shared constructor underpinning
// newWireRepo, ssh+shell factory wiring, and in-process wire tests.
// When preBuilt is non-nil it short-circuits the first dial — the wire
// variant leaves it nil (lazy ssh dial on first Walk/HashOne); the
// shell variant passes a ready shellClient; tests pass a wire client
// wired to a pipe pair.
func newWireRepoWithClient(ms *MetaStore, uri RepoURI, preBuilt WireDriver) *wireRepo {
	sess := &wireSession{uri: uri, client: preBuilt}
	w := &wireRepo{
		repoCore: repoCore{
			ms:         ms,
			walker:     &wireWalker{sess: sess},
			fileHasher: &wireHasher{sess: sess, ms: ms},
		},
		session: sess,
	}
	w.seedFromConfig()
	return w
}

// Close tears down the ssh session in addition to the embedded core's
// ms close. Both teardowns always run; errors are joined so a failure
// on one side doesn't mask a failure on the other.
func (w *wireRepo) Close() error {
	coreErr := w.repoCore.Close()
	var sessErr error
	if w.session != nil {
		sessErr = w.session.Close()
		w.session = nil
	}
	return errors.Join(coreErr, sessErr)
}
