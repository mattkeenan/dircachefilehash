package dircachefilehash

import (
	"context"
	"path/filepath"
)

// localWalker delegates filesystem scanning to the owning DirectoryCache's
// scanPath. It is the default Walker on every DirectoryCache and what
// `ssh://` routes replace at the factory in commit 5.
type localWalker struct{ dc *DirectoryCache }

func (lw *localWalker) Walk(ctx context.Context, paths []string, resultChan chan<- *scannedPath) error {
	return lw.dc.scanPath(ctx, paths, resultChan)
}

func (lw *localWalker) Close() error { return nil }

// localHasher hashes files on the local filesystem through the existing
// HashFileInterruptibleToBytes pipeline. Paths are resolved against
// dc.RootDir so callers pass repository-relative paths.
type localHasher struct{ dc *DirectoryCache }

func (lh *localHasher) HashOne(ctx context.Context, relPath string, buffer []byte) ([]byte, uint16, error) {
	filePath := filepath.Join(lh.dc.RootDir, relPath)
	return lh.dc.HashFileInterruptibleToBytes(ctx, filePath, buffer)
}

func (lh *localHasher) Close() error { return nil }
