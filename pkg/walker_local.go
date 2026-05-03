package dircachefilehash

import (
	"context"
	"path/filepath"
)

// localWalker delegates filesystem scanning to the metaStore's scanPath.
// Stateless — the per-call ScanRun carries everything walker-relevant;
// territory identity (RootDir) is reached via sr.Store.
type localWalker struct{}

func (lw *localWalker) Walk(ctx context.Context, paths []string, sr *ScanRun, resultChan chan<- *scannedPath) error {
	return sr.Store.scanPath(ctx, sr, paths, resultChan)
}

func (lw *localWalker) Close() error { return nil }

// localHasher hashes files on the local filesystem through the existing
// HashFileInterruptibleToBytes pipeline. Paths are resolved against
// ms.RootDir so callers pass repository-relative paths.
type localHasher struct{ ms *MetaStore }

func (lh *localHasher) HashOne(ctx context.Context, relPath string, buffer []byte) ([]byte, uint16, error) {
	filePath := filepath.Join(lh.ms.RootDir, relPath)
	return lh.ms.HashFileInterruptibleToBytes(ctx, filePath, buffer)
}

func (lh *localHasher) Close() error { return nil }
