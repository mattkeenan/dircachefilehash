package dircachefilehash

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// hashPool is a bounded worker pool that computes file hashes for PipelineEntry
// values. Workers read from input, compute the hash, call entry.SetHash(), and
// forward the entry to output. The pool uses context.Context for cancellation.
//
// Ownership: each PipelineEntry is owned by exactly one worker while being
// hashed, then ownership transfers to the output channel consumer.
type hashPool struct {
	dc      *DirectoryCache
	sr      *ScanRun
	input   <-chan *PipelineEntry
	output  chan<- *PipelineEntry
	workers int
}

// newHashPool creates a hash pool. The caller must close input when no more
// entries will be sent. The pool closes output after all workers finish.
// The hasher used per file comes from sr.FileHasher; dc is held only for
// hashSymlinkTargetToBytes (territory-side helper) and getHashBufferSize.
func newHashPool(dc *DirectoryCache, sr *ScanRun, input <-chan *PipelineEntry, output chan<- *PipelineEntry, workers int) *hashPool {
	if workers < 1 {
		workers = 1
	}
	return &hashPool{
		dc:      dc,
		sr:      sr,
		input:   input,
		output:  output,
		workers: workers,
	}
}

// Run starts the worker pool and blocks until all workers complete.
// It closes the output channel before returning.
func (hp *hashPool) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	wg.Add(hp.workers)

	// Collect the first error from any worker
	errCh := make(chan error, hp.workers)

	for range hp.workers {
		go func() {
			defer wg.Done()
			if err := hp.worker(ctx); err != nil {
				errCh <- err
			}
		}()
	}

	wg.Wait()
	close(hp.output)
	close(errCh)

	// Return the first error, if any
	for err := range errCh {
		return err
	}
	return nil
}

// worker processes entries from the input channel until it is closed or the
// context is cancelled.
func (hp *hashPool) worker(ctx context.Context) error {
	bufferSize, err := hp.dc.getHashBufferSize()
	if err != nil {
		return fmt.Errorf("failed to get hash buffer size: %w", err)
	}
	buffer := make([]byte, bufferSize)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case pe, ok := <-hp.input:
			if !ok {
				return nil // input closed
			}
			if err := hp.hashEntry(ctx, pe, buffer); err != nil {
				// Log hash errors but don't fail the pipeline — the entry
				// will proceed with an empty hash (matching existing behaviour
				// where hash failures are non-fatal).
				if IsDebugEnabled("hash") {
					VerboseLog(3, "[HASH-POOL] Hash failed: %v", err)
				}
			}

			select {
			case <-ctx.Done():
				return ctx.Err()
			case hp.output <- pe:
			}
		}
	}
}

// hashEntry computes the file hash and updates the entry. The buffer is
// pre-allocated per worker and reused across files to avoid GC pressure.
func (hp *hashPool) hashEntry(ctx context.Context, pe *PipelineEntry, buffer []byte) error {
	entry := pe.Entry

	relPath, err := entry.RelativePath()
	if err != nil {
		return fmt.Errorf("failed to get relative path: %w", err)
	}

	// Check for symlink — hash target path, not target contents
	mode, err := entry.Mode()
	if err != nil {
		return fmt.Errorf("failed to get file mode for %s: %w", relPath, err)
	}

	var hashBytes []byte
	var hashType uint16

	if os.FileMode(mode)&os.ModeSymlink != 0 {
		filePath := filepath.Join(hp.dc.RootDir, relPath)
		hashBytes, hashType, err = hp.dc.hashSymlinkTargetToBytes(filePath)
	} else {
		hashBytes, hashType, err = hp.sr.FileHasher.HashOne(ctx, relPath, buffer)
	}

	if err != nil {
		return fmt.Errorf("failed to hash %s: %w", relPath, err)
	}

	if setErr := entry.SetHash(hashBytes, hashType); setErr != nil {
		return fmt.Errorf("failed to set hash for %s: %w", relPath, setErr)
	}

	return nil
}
