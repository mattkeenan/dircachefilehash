package dircachefilehash

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// wireSession owns a lazily-dialled WireDriver for audit-mode repos.
// Shared between wireWalker and wireHasher so a single ssh subprocess
// serves both Diff's ScanMetadata and Apply's HashFiles traffic. Dial
// happens on first Walk/HashOne; until then Info/Config/Snapshots touch
// nothing off the invoker. Transport selection is owned by the factory:
// the wire variant dials ssh lazily, the shell variant hands in a pre-
// built shellClient so every Client() call returns immediately.
type wireSession struct {
	uri RepoURI

	mu     sync.Mutex
	client WireDriver
	closed bool
}

// Client returns the underlying WireDriver, dialling on first use when
// no pre-built client was supplied. The mutex is released around the
// dial so a concurrent Close can tear the session down without blocking
// on ssh startup latency; if two callers race the first dial, the
// loser's client is discarded.
func (s *wireSession) Client(ctx context.Context) (WireDriver, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, fmt.Errorf("wire session closed")
	}
	if s.client != nil {
		c := s.client
		s.mu.Unlock()
		return c, nil
	}
	s.mu.Unlock()

	t, err := dialSSH(ctx, s.uri, []string{"dcfh", "remote", s.uri.Path})
	if err != nil {
		return nil, err
	}
	fresh := NewWireClient(t)

	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.closed:
		_ = fresh.Close()
		return nil, fmt.Errorf("wire session closed")
	case s.client != nil:
		_ = fresh.Close()
		return s.client, nil
	default:
		s.client = fresh
		return fresh, nil
	}
}

func (s *wireSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.client == nil {
		return nil
	}
	return s.client.Close()
}

// wireWalker issues one ScanMetadata request per Walk and adapts the
// sorted []FileMeta response into the existing scannedPath channel so
// the downstream Hwang-Lin pipeline is unaware of the wire layer.
type wireWalker struct {
	sess *wireSession
	dc   *DirectoryCache
}

func (w *wireWalker) Walk(ctx context.Context, paths []string, resultChan chan<- *scannedPath) error {
	defer close(resultChan)

	client, err := w.sess.Client(ctx)
	if err != nil {
		return err
	}

	patterns := w.dc.ignoreManager.GetPatterns()
	ignores := make([]string, 0, len(patterns))
	for _, p := range patterns {
		ignores = append(ignores, p.String())
	}
	resp, err := client.ScanMetadata(ctx, ScanRequest{
		Paths:    paths,
		Symlinks: w.dc.symlinkMode,
		Ignores:  ignores,
	})
	if err != nil {
		return err
	}

	for _, meta := range resp.Files {
		select {
		case resultChan <- metaToScannedPath(meta, w.sess.uri.Path):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (w *wireWalker) Close() error { return nil }

// wireHasher hashes one file per call via a HashFiles wire round trip.
// TODO: batch HashFiles requests — every hash-pool worker currently
// serialises through WireClient.mu for one path at a time.
type wireHasher struct {
	sess *wireSession
	dc   *DirectoryCache

	algoOnce sync.Once
	algo     *HashAlgorithm
	algoErr  error
}

// resolveAlgo memoises the hash-algorithm lookup. Per-HashOne calls used
// to re-parse config for every file; the algorithm is fixed for the
// lifetime of a session, so once is enough.
func (h *wireHasher) resolveAlgo() (*HashAlgorithm, error) {
	h.algoOnce.Do(func() {
		h.algo, h.algoErr = h.dc.getDefaultHashAlgorithm()
	})
	return h.algo, h.algoErr
}

func (h *wireHasher) HashOne(ctx context.Context, relPath string, _ []byte) ([]byte, uint16, error) {
	client, err := h.sess.Client(ctx)
	if err != nil {
		return nil, 0, err
	}
	algo, err := h.resolveAlgo()
	if err != nil {
		return nil, 0, err
	}
	resp, err := client.HashFiles(ctx, HashRequest{Paths: []string{relPath}, Algo: algo.Name})
	if err != nil {
		return nil, 0, err
	}
	if len(resp.Digests) != 1 {
		return nil, 0, fmt.Errorf("wire hasher: expected 1 digest for %s, got %d", relPath, len(resp.Digests))
	}
	d := resp.Digests[0]
	if d.Err != "" {
		return nil, 0, fmt.Errorf("wire hash %s: %s", relPath, d.Err)
	}
	b, err := hex.DecodeString(d.Hash)
	if err != nil {
		return nil, 0, fmt.Errorf("decode wire hash for %s: %w", relPath, err)
	}
	return b, algo.TypeID, nil
}

func (h *wireHasher) Close() error { return nil }

// metaToScannedPath converts one wire FileMeta into the internal
// scannedPath used downstream by BEScanEntry and the Hwang-Lin
// callbacks. Ino is not on the wire and defaults to 0 — dcfh's
// comparison keys are (path, size, mtime, ctime), so this is sufficient.
func metaToScannedPath(m FileMeta, rootPath string) *scannedPath {
	info := &mockFileInfo{
		name:    filepath.Base(m.Path),
		size:    m.Size,
		mode:    os.FileMode(m.Mode),
		modTime: time.Unix(0, m.MtimeNs),
	}
	stat := &syscall.Stat_t{
		Dev:  m.Dev,
		Uid:  m.UID,
		Gid:  m.GID,
		Size: m.Size,
		Mode: m.Mode,
		Mtim: syscall.NsecToTimespec(m.MtimeNs),
		Ctim: syscall.NsecToTimespec(m.CtimeNs),
	}
	return &scannedPath{
		AbsPath:  filepath.Join(rootPath, m.Path),
		RelPath:  m.Path,
		Info:     info,
		StatInfo: stat,
	}
}
