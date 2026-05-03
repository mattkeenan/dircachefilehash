package dircachefilehash

import "context"

// Walker produces sorted filesystem metadata under a root. The channel
// carries the existing *scannedPath type so downstream consumers
// (FilesystemScanIterator, Hwang-Lin callbacks) stay unchanged
// when the underlying filesystem source swaps between local syscalls
// and a wire-backed walker.
//
// Implementations MUST close resultChan when the walk completes, whether
// normally or due to context cancellation; streaming errors are reported
// by closing the channel and returning from Walk.
//
// sr carries the per-call instruments and scratch state (symlink mode,
// scan-time --ignore predicate, etc.). Walkers read what they need
// from sr; the per-repo identity lives in sr.Store.
type Walker interface {
	Walk(ctx context.Context, paths []string, sr *ScanRun, resultChan chan<- *scannedPath) error
	Close() error
}

// Hasher computes content hashes for files addressed by a path relative
// to the repository root. The buffer is caller-owned, pre-allocated per
// worker, and reused across calls to avoid GC pressure when hashing
// many files; local implementations MUST use it for their read loop.
// Wire implementations that don't read file bytes locally may leave it
// untouched.
//
// Returned hashType identifies the algorithm (SHA1=1, SHA256=2, SHA512=3)
// so callers can record it without a side-channel config lookup.
type Hasher interface {
	HashOne(ctx context.Context, relPath string, buffer []byte) ([]byte, uint16, error)
	Close() error
}
